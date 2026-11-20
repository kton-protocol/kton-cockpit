// The frontend protocol (SPEC §12.5, cockpit serve --stdio, kton-cockpit #186): JSON-RPC 2.0 over the cockpit's
// stdin and stdout, framed as a language server frames it. Behind the same Transport interface as the command line;
// a setting chooses it (kton.cockpit.transport: stdio), the command line stays the default.
//
// - One connection per window (kton-cockpit on §12.5): `initialize` names the folder and no identity; each say names
//   its own `as` (any identity of the checkout's keyring), so the person key and the agent key share it.
// - Refusals (error -32001, data {code, clause, reason}) become the same {ok: false, refusal} the command line's
//   exit 2 gives; any other error is a TransportError, as a failed command line run is.
// - A request that runs past the timeout is cancelled ($/cancelRequest) and reported as TransportTimeout; its
//   late answer (-32800) is dropped.
// - What the protocol has no method for (workflow run and list, install) goes to the command line.
// No events yet (part 2): the shell still polls.
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { CliTransport, checkShow, parseQueries, parseTemplates, type CliTransportOptions } from "./cli.js";
import { assertUiListing, type UiListing } from "../packages/listing.js";
import { assertAskResult, assertDoctorReport, assertPublishResult, assertSayResult } from "./schema.js";
import {
  TransportError,
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
  type TemplateInfo,
} from "./port.js";

interface Pending {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: NodeJS.Timeout;
}

/** A refusal the server answered (error -32001): the command line's JSON refusal. */
class Refused extends Error {
  constructor(readonly refusal: Refusal) {
    super(refusal.reason);
  }
}

/** One `cockpit serve --stdio` process, initialized for the folder and (optionally) one signing identity. */
export class StdioConnection {
  private child: ChildProcessWithoutNullStreams | undefined;
  private buf = Buffer.alloc(0);
  private nextId = 1;
  private readonly pending = new Map<number, Pending>();
  private ready: Promise<void> | undefined;
  private stderr = "";
  /** Every method sent, newest last (the output channel, tests). */
  readonly calls: string[] = [];

  constructor(
    private readonly opts: CliTransportOptions,
    private readonly as: string | undefined,
  ) {}

  /** A request: its result, a Refused, a TransportTimeout (after cancelling it) or a TransportError. */
  async request(method: string, params: unknown): Promise<unknown> {
    await this.start();
    return this.send(method, params);
  }

  close(): void {
    const c = this.child;
    this.child = undefined;
    this.ready = undefined;
    if (c && !c.killed) {
      try {
        this.write({ jsonrpc: "2.0", id: this.nextId++, method: "shutdown" }, c);
        this.write({ jsonrpc: "2.0", method: "exit" }, c);
      } catch {
        /* the process is gone */
      }
      setTimeout(() => c.kill(), 2000).unref();
    }
  }

  private start(): Promise<void> {
    if (this.ready) return this.ready;
    this.ready = new Promise<void>((resolve, reject) => {
      let child: ChildProcessWithoutNullStreams;
      try {
        child = spawn(this.opts.executable, ["serve", "--stdio"], { cwd: this.opts.cwd, env: { ...process.env, ...(this.opts.env ?? {}) }, shell: false, windowsHide: true });
      } catch (e) {
        this.ready = undefined;
        reject(new TransportError(`cannot run ${this.opts.executable}: ${(e as Error).message}`, null, ""));
        return;
      }
      this.child = child;
      child.stdout.on("data", (d: Buffer) => this.read(d));
      child.stderr.on("data", (d: Buffer) => {
        this.stderr = (this.stderr + d.toString("utf8")).slice(-8192);
      });
      const fail = (why: string) => {
        if (this.child === child) {
          this.child = undefined;
          this.ready = undefined;
        }
        for (const [id, p] of this.pending) {
          clearTimeout(p.timer);
          p.reject(new TransportError(why, null, this.stderr));
          this.pending.delete(id);
        }
        reject(new TransportError(why, null, this.stderr));
      };
      child.on("error", (e) => fail(`cannot run ${this.opts.executable}: ${e.message}`));
      child.on("exit", (code) => fail(`cockpit serve --stdio ended (exit ${String(code)})${this.stderr ? `: ${this.stderr.trim().split("\n").pop()}` : ""}`));
      this.send("initialize", { root: this.opts.cwd, ...(this.as ? { as: this.as } : {}), ...(this.opts.timeoutMs ? { requestTimeoutMs: this.opts.timeoutMs } : {}) }).then(
        () => resolve(),
        (e: Error) => {
          this.close();
          reject(e instanceof Refused ? new TransportError(`cockpit serve --stdio refused initialize: ${e.refusal.reason} (${e.refusal.code})`, 2, "") : e);
        },
      );
    });
    return this.ready;
  }

  private send(method: string, params: unknown): Promise<unknown> {
    const child = this.child;
    if (!child) return Promise.reject(new TransportError("cockpit serve --stdio is not running", null, this.stderr));
    const id = this.nextId++;
    this.calls.push(method);
    const timeoutMs = this.opts.timeoutMs ?? 120_000;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        // the request runs on in the server: cancel it there; its answer (-32800) is dropped
        try {
          this.write({ jsonrpc: "2.0", method: "$/cancelRequest", params: { id } }, child);
        } catch {
          /* gone */
        }
        reject(new TransportTimeout(`${method} timed out after ${Math.round(timeoutMs / 1000)} s (cancelled)`, this.stderr));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
      try {
        this.write({ jsonrpc: "2.0", id, method, ...(params !== undefined ? { params } : {}) }, child);
      } catch (e) {
        clearTimeout(timer);
        this.pending.delete(id);
        reject(new TransportError(`cockpit serve --stdio: ${(e as Error).message}`, null, this.stderr));
      }
    });
  }

  private write(msg: unknown, child: ChildProcessWithoutNullStreams): void {
    const body = Buffer.from(JSON.stringify(msg), "utf8");
    child.stdin.write(Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`, "ascii"), body]));
  }

  /** Content-Length framing: headers, a blank line, the JSON body. */
  private read(d: Buffer): void {
    this.buf = Buffer.concat([this.buf, d]);
    for (;;) {
      const end = this.buf.indexOf("\r\n\r\n");
      if (end < 0) return;
      const header = this.buf.subarray(0, end).toString("ascii");
      const m = /Content-Length:\s*(\d+)/i.exec(header);
      if (!m) {
        // not the protocol: stdout must carry protocol messages only (SPEC §12.5)
        this.buf = Buffer.alloc(0);
        return;
      }
      const len = Number(m[1]);
      if (this.buf.length < end + 4 + len) return;
      const body = this.buf.subarray(end + 4, end + 4 + len).toString("utf8");
      this.buf = this.buf.subarray(end + 4 + len);
      let msg: { id?: unknown; result?: unknown; error?: { code?: number; message?: string; data?: { code?: unknown; clause?: unknown; reason?: unknown } } };
      try {
        msg = JSON.parse(body) as typeof msg;
      } catch {
        continue;
      }
      if (typeof msg.id !== "number") continue; // a notification (events: part 2) or a string id we never sent
      const p = this.pending.get(msg.id);
      if (!p) continue; // cancelled after the timeout: dropped
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
  private readonly conns = new Map<string, StdioConnection>();

  constructor(opts: CliTransportOptions) {
    super(opts);
  }

  /** The connection for a signing identity ("" = none named). */
  connection(as: string | undefined): StdioConnection {
    const k = as ?? "";
    let c = this.conns.get(k);
    if (!c) {
      c = new StdioConnection(this.opts, as);
      this.conns.set(k, c);
    }
    return c;
  }

  dispose(): void {
    for (const c of this.conns.values()) c.close();
    this.conns.clear();
  }

  /** A method's answer as an Outcome: a refusal is {ok: false}, as the command line's exit 2 is. */
  private async outcomeOf<T>(method: string, params: unknown, check: (v: unknown) => T, as?: string): Promise<Outcome<T>> {
    try {
      return { ok: true, value: check(await this.connection(as).request(method, params)) };
    } catch (e) {
      if (e instanceof Refused) return { ok: false, refusal: e.refusal };
      throw e;
    }
  }

  override ask(req: AskRequest): Promise<Outcome<AskResult>> {
    return this.outcomeOf("ask", req, assertAskResult);
  }

  override say(req: SayRequest): Promise<Outcome<SayResult>> {
    return this.outcomeOf("say", req, assertSayResult);
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
