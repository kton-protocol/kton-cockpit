// The frontend protocol (SPEC §12.5, cockpit serve --stdio, kton-cockpit #186; released with cockpit v0.5.6): JSON-RPC
// 2.0 over the cockpit's stdin and stdout, framed as a language server frames it. Behind the same Transport interface
// as the command line; a setting chooses it (kton.cockpit.transport: stdio), the command line stays the default.
//
// - One connection per window (kton-cockpit on §12.5): `initialize` names the folder and no identity; each say names
//   its own `as` (any identity of the checkout's keyring), so the person key and the agent key share it.
// - The local checks of the command line (a sha256 subject, names, a lineage ref) run here too, before a request.
// - Refusals (error -32001, data {code, clause, reason}) become the same {ok: false, refusal} the command line's
//   exit 2 gives; any other error is a TransportError, as a failed command line run is.
// - A request that runs past the timeout is cancelled ($/cancelRequest) and reported as TransportTimeout.
// - An answer that is not JSON cannot name its request: every request waiting on the connection fails with
//   "<method>: the answer is not JSON", as the command line's non-JSON stdout does. A header without
//   Content-Length loses the framing: the same, and the process is dropped (the next request starts a new one).
// - A write the process cannot take (it is gone or closed its stdin: EPIPE comes later, as an 'error' event on
//   stdin, never as a throw): every waiting request fails and the process is dropped; the next request starts a new
//   one (kton-cockpit #196, S1).
// - A parsable message with an unknown id is ignored (§12.5), except the answer to a request this client failed
//   without its own answer (another answer not JSON, a timeout): that one is reported (onLateAnswer), so a say that
//   was signed after all is not lost (kton-review W1 on #196).
// - The end of the process is its 'close' (stdout drained), not its 'exit': an answer already in the pipe is read.
// - close() (a rebuilt window, deactivate) sends shutdown and exit and keeps reading: serve answers what it accepted
//   first, and those answers are delivered. What has no answer when the process ends is a TransportTimeout: unknown
//   whether it ran (kton-cockpit Y1 on #196).
// - Only TransportNotSent says a request never reached the cockpit (it could not start, the connection was not
//   running or closing, the write failed at once).
// - What the protocol has no method for (workflow run and list, install) goes to the command line.
// No events yet (part 2): the shell still polls.
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { CliTransport, bad, checkAsk, checkSay, checkShow, parseQueries, parseTemplates, type CliTransportOptions } from "./cli.js";
import { assertUiListing, type UiListing } from "../packages/listing.js";
import { assertSayBatchResult, assertAskResult, assertDoctorReport, assertPublishResult, assertSayResult } from "./schema.js";
import {
  TransportError,
  TransportNotSent,
  TransportTimeout,
  type AskRequest,
  type AskResult,
  type DoctorReport,
  type Outcome,
  type PublishRequest,
  type PublishResult,
  type QueryInfo,
  type Refusal,
  type SayRequest,
  type SayResult,
  type SayBatchResult,
  BATCH_UNSUPPORTED,
  type TemplateInfo,
} from "./port.js";

interface Pending {
  method: string;
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: NodeJS.Timeout;
}

/** The answer to a request this client had already failed (another answer not JSON, its timeout): it came after all. */
export interface LateAnswer {
  readonly method: string;
  readonly id: number;
  /** Why the client had failed it. */
  readonly failed: string;
  readonly result?: unknown;
  readonly error?: { readonly code?: number; readonly message?: string; readonly data?: unknown };
}

/** A refusal the server answered (error -32001): the command line's JSON refusal. */
class Refused extends Error {
  constructor(readonly refusal: Refusal) {
    super(refusal.reason);
  }
}

/** How many failed ids are remembered for a late answer (the oldest go first). */
const REMEMBER = 256;

/** One `cockpit serve --stdio` process, initialized for the folder; each say names its own identity. */
export class StdioConnection {
  private child: ChildProcessWithoutNullStreams | undefined;
  /** stdout not yet framed: the chunks, concatenated once a header or a whole body is there (not per chunk). */
  private chunks: Buffer[] = [];
  private buffered = 0;
  /** The body length the current header names, once it is read (null: still reading a header). */
  private need: number | null = null;
  private nextId = 1;
  private readonly pending = new Map<number, Pending>();
  /** Requests failed without their own answer: id -> method and why (W1). */
  private readonly failed = new Map<number, { method: string; why: string }>();
  private ready: Promise<void> | undefined;
  private closing = false;
  private stderr = "";
  /** Every method sent, newest last (the output channel, tests). */
  readonly calls: string[] = [];
  /** The methods `initialize` answered that the cockpit serves (its `methods`); empty when it names none. */
  private served: readonly string[] = [];

  constructor(
    private readonly opts: CliTransportOptions,
    private readonly onLate?: (a: LateAnswer) => void,
  ) {}

  /** Whether this connection's cockpit serves the method (starts the connection if needed). */
  async serves(method: string): Promise<boolean> {
    await this.start();
    return this.served.includes(method);
  }

  /** A request: its result, a Refused, a TransportTimeout (after cancelling it), a TransportNotSent or a TransportError. */
  async request(method: string, params: unknown): Promise<unknown> {
    if (this.closing) throw new TransportNotSent(`${method}: the cockpit connection is closing (the window was rebuilt)`, "");
    await this.start();
    return this.send(method, params);
  }

  /**
   * shutdown, then exit: serve answers every request it accepted first (§12.5), and those answers are still
   * delivered. The process is killed after the grace (the client's timeout and 5 s); what is still waiting then is a
   * TransportTimeout (unknown whether it ran).
   */
  close(graceMs = (this.opts.timeoutMs ?? 120_000) + 5000): void {
    if (this.closing) return;
    this.closing = true;
    const c = this.child;
    if (!c) return;
    try {
      this.write({ jsonrpc: "2.0", id: this.nextId++, method: "shutdown" }, c);
      this.write({ jsonrpc: "2.0", method: "exit" }, c);
    } catch {
      /* the process is gone: its end fails what waits */
    }
    setTimeout(() => {
      if (!c.killed && c.exitCode === null && c.signalCode === null) c.kill();
    }, graceMs).unref();
  }

  private start(): Promise<void> {
    if (this.ready) return this.ready;
    this.ready = new Promise<void>((resolve, reject) => {
      let child: ChildProcessWithoutNullStreams;
      // serve refuses a COCKPIT_REPO_DIR that redirects the folder (§12.5): the window's folder is the one it names
      const env: NodeJS.ProcessEnv = { ...process.env, ...(this.opts.env ?? {}) };
      delete env["COCKPIT_REPO_DIR"];
      try {
        child = spawn(this.opts.executable, ["serve", "--stdio"], { cwd: this.opts.cwd, env, shell: false, windowsHide: true });
      } catch (e) {
        this.ready = undefined;
        reject(new TransportNotSent(`cannot run ${this.opts.executable}: ${(e as Error).message}`, ""));
        return;
      }
      this.child = child;
      // S1: EPIPE (a gone process, a closed stdin) is an 'error' event on stdin, not a throw from write(): without
      // this listener it is an uncaught exception in the extension host and the request waits for exit or its timeout
      child.stdin.on("error", (e: Error) => {
        if (this.child !== child) return; // a dropped process's late EPIPE
        this.failWaiting(`cockpit serve --stdio: ${e.message}`);
        this.drop();
      });
      child.stdout.on("data", (d: Buffer) => {
        if (this.child === child) this.read(d); // a dropped process's late bytes are not this connection's
      });
      child.stderr.on("data", (d: Buffer) => {
        this.stderr = (this.stderr + d.toString("utf8")).slice(-8192);
      });
      let ended = false;
      const end = (why: string) => {
        if (ended) return;
        ended = true;
        const current = this.child === child || this.child === undefined;
        if (this.child === child) {
          this.child = undefined;
          this.ready = undefined;
          this.resetFraming();
        }
        // a dropped process that ends after its successor started: the successor's requests are not its own
        if (current)
          for (const [id, p] of this.pending) {
            clearTimeout(p.timer);
            this.pending.delete(id);
            // closing: serve had them and did not answer before it ended: unknown whether they ran
            p.reject(this.closing ? new TransportTimeout(`${p.method}: the cockpit ended before it answered (${why})`, this.stderr) : new TransportError(why, null, this.stderr));
          }
        reject(new TransportNotSent(why, this.stderr));
      };
      const ending = (code: number | null, signal: NodeJS.Signals | null) =>
        `cockpit serve --stdio ended (exit ${String(code ?? (signal ? `${signal}` : null))})${this.stderr ? `: ${this.stderr.trim().split("\n").pop()}` : ""}`;
      child.on("error", (e) => end(`cannot run ${this.opts.executable}: ${e.message}`));
      // Y2: 'close' comes once stdout is drained ('exit' may come first, with an answer still in the pipe). If a
      // grandchild holds the pipes open, 'exit' ends the connection 2 s later.
      child.on("close", (code, signal) => end(ending(code, signal)));
      child.on("exit", (code, signal) => setTimeout(() => end(ending(code, signal)), 2000).unref());
      // requestTimeoutMs is the client's own timeout, by design: the server's end of a request and the client's
      // $/cancelRequest race, and either outcome is reported correctly (a refusal or a TransportTimeout); for a say both
      // mean "the registry says whether it signed", as on the command line (kton-cockpit #196 review).
      this.send("initialize", { root: this.opts.cwd, ...(this.opts.timeoutMs ? { requestTimeoutMs: this.opts.timeoutMs } : {}) }).then(
        (init) => {
          const m = (init as { methods?: unknown } | null | undefined)?.methods;
          this.served = Array.isArray(m) ? m.filter((x): x is string => typeof x === "string") : [];
          resolve();
        },
        (e: Error) => {
          // initialize failed: the request that started the connection was never sent
          if (this.child === child) this.drop();
          reject(new TransportNotSent(e instanceof Refused ? `cockpit serve --stdio refused initialize: ${e.refusal.reason} (${e.refusal.code})` : e.message, this.stderr));
        },
      );
    });
    return this.ready;
  }

  private send(method: string, params: unknown): Promise<unknown> {
    const child = this.child;
    if (!child) return Promise.reject(new TransportNotSent(`${method}: cockpit serve --stdio is not running`, this.stderr));
    const id = this.nextId++;
    this.calls.push(method);
    const timeoutMs = this.opts.timeoutMs ?? 120_000;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        this.remember(id, method, "timed out");
        // the request runs on in the server: cancel it there; a -32800 is dropped, a real answer is reported (W1)
        if (this.child === child) {
          try {
            this.write({ jsonrpc: "2.0", method: "$/cancelRequest", params: { id } }, child);
          } catch {
            /* gone */
          }
        }
        reject(new TransportTimeout(`${method} timed out after ${Math.round(timeoutMs / 1000)} s (cancelled)`, this.stderr));
      }, timeoutMs);
      this.pending.set(id, { method, resolve, reject, timer });
      try {
        this.write({ jsonrpc: "2.0", id, method, ...(params !== undefined ? { params } : {}) }, child);
      } catch (e) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(new TransportNotSent(`${method}: cockpit serve --stdio: ${(e as Error).message}`, this.stderr));
      }
    });
  }

  private write(msg: unknown, child: ChildProcessWithoutNullStreams): void {
    const body = Buffer.from(JSON.stringify(msg), "utf8");
    child.stdin.write(Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`, "ascii"), body]));
  }

  private remember(id: number, method: string, why: string): void {
    this.failed.set(id, { method, why });
    if (this.failed.size > REMEMBER) this.failed.delete(this.failed.keys().next().value as number);
  }

  /** Fail every request waiting on this connection: an answer that names none of them went wrong. */
  private failWaiting(why: string): void {
    for (const [id, p] of this.pending) {
      clearTimeout(p.timer);
      this.pending.delete(id);
      this.remember(id, p.method, why);
      p.reject(new TransportError(`${p.method}: ${why}`, null, this.stderr));
    }
  }

  /** Drop the process without the shutdown handshake (its stream cannot be trusted any more). */
  private drop(): void {
    const c = this.child;
    this.child = undefined;
    this.ready = undefined;
    this.resetFraming();
    if (c && !c.killed) c.kill();
  }

  private resetFraming(): void {
    this.chunks = [];
    this.buffered = 0;
    this.need = null;
  }

  /** The buffered bytes as one buffer (one concat, only when a header or a whole body may be there). */
  private joined(): Buffer {
    if (this.chunks.length > 1) this.chunks = [Buffer.concat(this.chunks, this.buffered)];
    return this.chunks[0] ?? Buffer.alloc(0);
  }

  /** Content-Length framing: headers, a blank line, the JSON body. Chunks are collected and joined once the
   *  Content-Length is reached, so a large message costs linear time, not one concat per chunk. */
  private read(d: Buffer): void {
    this.chunks.push(d);
    this.buffered += d.length;
    for (;;) {
      if (this.need !== null && this.buffered < this.need) return;
      const buf = this.joined();
      const end = buf.indexOf("\r\n\r\n");
      if (end < 0) return;
      const header = buf.subarray(0, end).toString("ascii");
      const m = /Content-Length:\s*(\d+)/i.exec(header);
      if (!m) {
        // not the protocol: stdout must carry protocol messages only (SPEC §12.5); the framing is lost
        this.resetFraming();
        this.failWaiting("the answer is not the protocol (a header without Content-Length)");
        this.drop();
        return;
      }
      const total = end + 4 + Number(m[1]);
      if (buf.length < total) {
        this.need = total;
        return;
      }
      this.need = null;
      const body = buf.subarray(end + 4, total).toString("utf8");
      const rest = buf.subarray(total);
      this.chunks = rest.length ? [rest] : [];
      this.buffered = rest.length;
      let msg: { id?: unknown; result?: unknown; error?: { code?: number; message?: string; data?: { code?: unknown; clause?: unknown; reason?: unknown } } };
      try {
        msg = JSON.parse(body) as typeof msg;
      } catch {
        this.failWaiting("the answer is not JSON");
        continue;
      }
      // SPEC §12.5: a parsable message with an unknown id is ignored (no failure, nothing logged): a notification
      // (events: part 2), a string id we never sent, an id we cancelled after the timeout (its -32800 is dropped)
      if (typeof msg.id !== "number") continue;
      const p = this.pending.get(msg.id);
      if (!p) {
        // W1: the answer to a request this client failed without its own answer came after all: report it (a say
        // may have been signed); a -32800 is the cancel this client asked for. Any other unknown id is not ours.
        const f = this.failed.get(msg.id);
        if (f) {
          this.failed.delete(msg.id);
          if (msg.error?.code !== -32800) this.onLate?.({ method: f.method, id: msg.id, failed: f.why, ...(msg.error ? { error: msg.error } : { result: msg.result }) });
        }
        continue;
      }
      this.pending.delete(msg.id);
      clearTimeout(p.timer);
      if (!msg.error) p.resolve(msg.result);
      else if (msg.error.code === -32001 && typeof msg.error.data?.code === "string" && typeof msg.error.data.reason === "string") {
        const r = msg.error.data;
        p.reject(new Refused(typeof r.clause === "string" && r.clause ? { code: r.code as string, clause: r.clause, reason: r.reason as string } : { code: r.code as string, reason: r.reason as string }));
      } else p.reject(new TransportError(`cockpit serve --stdio: ${msg.error.message ?? "error"} (${String(msg.error.code)})`, msg.error.code ?? null, this.stderr));
    }
  }
}

/** The Transport over `cockpit serve --stdio`; what the protocol does not serve goes to the command line. */
export class StdioTransport extends CliTransport {
  /** One connection per window (kton-cockpit on §12.5): each say names its own `as`. */
  private conn: StdioConnection | undefined;
  /** The answer to a request already failed (another answer not JSON, a timeout) that came after all (W1). */
  onLateAnswer: ((a: LateAnswer) => void) | undefined;

  constructor(opts: CliTransportOptions) {
    super(opts);
  }

  private connection(): StdioConnection {
    return (this.conn ??= new StdioConnection(this.opts, (a) => this.onLateAnswer?.(a)));
  }

  dispose(): void {
    this.conn?.close();
    this.conn = undefined;
  }

  /** A method's answer as an Outcome: a refusal is {ok: false}, as the command line's exit 2 is. */
  private async outcomeOf<T>(method: string, params: unknown, check: (v: unknown) => T): Promise<Outcome<T>> {
    try {
      return { ok: true, value: check(await this.connection().request(method, params)) };
    } catch (e) {
      if (e instanceof Refused) return { ok: false, refusal: e.refusal };
      throw e;
    }
  }

  override ask(req: AskRequest): Promise<Outcome<AskResult>> {
    const why = checkAsk(req);
    if (why) return bad(why);
    return this.outcomeOf("ask", req, assertAskResult);
  }

  override say(req: SayRequest): Promise<Outcome<SayResult>> {
    const why = checkSay(req);
    if (why) return bad(why);
    return this.outcomeOf("say", req, assertSayResult);
  }

  /**
   * `say/batch` (params {claims: [say requests]}, result {claims, pending?}) once the cockpit lists it among the methods
   * `initialize` answered. Until then the batch is not offered: BATCH_UNSUPPORTED, nothing sent (a method not found is
   * the same answer), and the caller says one by one. The command line's `say --batch` is not used here: one transport,
   * one way to the cockpit.
   */
  override async sayBatch(reqs: readonly SayRequest[]): Promise<Outcome<SayBatchResult>> {
    if (reqs.length === 0) return bad("a batch needs at least one claim");
    for (const [i, r] of reqs.entries()) {
      const why = checkSay(r);
      if (why) return bad(`claim ${i + 1}: ${why}`);
    }
    const unsupported: Outcome<SayBatchResult> = { ok: false, refusal: { code: BATCH_UNSUPPORTED, reason: "this cockpit's serve --stdio has no say/batch: the claims are said one by one" } };
    if (!(await this.connection().serves("say/batch"))) return unsupported;
    try {
      return await this.outcomeOf("say/batch", { claims: reqs }, (v) => assertSayBatchResult(v, reqs.length));
    } catch (e) {
      if (e instanceof TransportError && e.exitCode === -32601) return unsupported; // method not found: it did not run
      throw e;
    }
  }

  override publish(req: PublishRequest): Promise<Outcome<PublishResult>> {
    return this.outcomeOf("publish", req, assertPublishResult);
  }

  override templates(): Promise<Outcome<TemplateInfo[]>> {
    return this.outcomeOf("say/templates", undefined, parseTemplates);
  }

  protected override queries(): Promise<Outcome<QueryInfo[]>> {
    return this.outcomeOf("ask/questions", undefined, parseQueries);
  }

  override uiList(): Promise<Outcome<UiListing>> {
    return this.outcomeOf("ui/list", undefined, assertUiListing);
  }

  /** `ui/cat` ({package, hashes}): the bytes base64, as `ui cat … --json` prints them. The caller re-hashes them. */
  override async uiCat(pkg: string, hash: string): Promise<Uint8Array> {
    if (!/^sha256:[0-9a-f]{64}$/.test(hash)) throw new TransportError(`${JSON.stringify(hash)} is not sha256:<hex>`, 0, "");
    if (pkg === "" || pkg.startsWith("-")) throw new TransportError(`${JSON.stringify(pkg)} is not a package`, 0, "");
    const out = await this.outcomeOf("ui/cat", { package: pkg, hashes: [hash] }, (v) => v);
    if (!out.ok) {
      const what = out.refusal.code === "ui.package" ? `package ${pkg} is not usable here (not admitted, failing its check or not installed)` : out.refusal.code === "ui.member" ? `package ${pkg} has no member ${hash}` : "refused";
      throw new TransportError(`ui cat: ${what}: ${out.refusal.reason}`, 2, "");
    }
    const b64 = memberBytes(out.value, hash);
    if (b64 === undefined) throw new TransportError(`ui cat: the answer has no bytes for ${hash}`, 0, "");
    return new Uint8Array(Buffer.from(b64, "base64"));
  }

  override graph(): ReturnType<CliTransport["graph"]> {
    return this.outcomeOf("show/snapshot", undefined, checkShow);
  }

  override async doctor(): Promise<DoctorReport> {
    const out = await this.outcomeOf("doctor", undefined, assertDoctorReport);
    if (!out.ok) throw new TransportError(`doctor: ${out.refusal.reason}`, 2, "");
    return out.value;
  }

  override async version(): Promise<Outcome<{ readonly text: string }>> {
    return this.outcomeOf("version", undefined, (v) => {
      const o = v as { cockpit?: unknown; revision?: unknown };
      if (typeof o?.cockpit !== "string") throw new TransportError("version: no cockpit field", 0, "");
      const rev = typeof o.revision === "string" && o.revision ? ` ${o.revision.slice(0, 12)}` : "";
      return { text: `cockpit ${o.cockpit}${rev} (serve --stdio)` };
    });
  }
}

/** The base64 bytes of one member in a `ui cat --json` answer: {members: [{hash, bytes}]} or {<hash>: bytes}. */
function memberBytes(v: unknown, hash: string): string | undefined {
  const o = v as { members?: Array<{ hash?: unknown; bytes?: unknown; base64?: unknown }> } & Record<string, unknown>;
  if (Array.isArray(o?.members)) {
    const m = o.members.find((x) => x.hash === hash);
    const b = m?.bytes ?? m?.base64;
    return typeof b === "string" ? b : undefined;
  }
  const b = o?.[hash];
  return typeof b === "string" ? b : undefined;
}
