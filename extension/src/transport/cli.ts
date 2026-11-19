// CliTransport (contract C1.2): the shell's transport until `cockpit serve --stdio` (v0.6.0).
//
// - The executable is a setting (`cockpit` on git, another binary for another backend).
// - It is always run with an argv array and `shell: false`. Field values come from viewers and
//   packages, so nothing ever passes through a shell (review check 4). No argv element that is a
//   flag VALUE or a positional may start with "-": names are checked against NAME, ids against SHA,
//   paths are refused when they start with "-", and field values travel as "key=value", which
//   starts with the key.
// - say uses the FLAG form (`cockpit say <template> <sha256:…> k=v … [--as NAME] [--scope NAME]
//   --json`). Since v0.5.1 every form resolves a file subject (#148) and refuses a link out of the
//   repository or a non-regular file; the shell keeps the flag form because it only ever passes
//   sha256 ids, which need no resolution, and each field travels as its own argv element.
// - ask and publish use the JSON argument form (cockpit/cli/main.go callVerb): one argv element
//   carries the request, unknown fields are refused by the cockpit, the answer is one JSON object.
// - Templates and queries come from the cockpit's own listings (`say --json`, `ask --json`), never
//   from files in the checkout (review check 2).
// - Exit codes (cockpit/cli/main.go): 0 answer, 1 failure (also an argument error found before the
//   handler), 2 refusal, 3 doctor not ready. A refusal is {code, clause, reason} on stdout under
//   `--json`; older builds wrote text on stderr. Both are read.

import { execFile, spawn } from "node:child_process";
import { createServer } from "node:net";
import { readFile } from "node:fs/promises";
import { isAbsolute, join, normalize, sep } from "node:path";
import { assertUiListing, type UiListing } from "../packages/listing.js";
import type {
  AskRequest,
  AskResult,
  DoctorReport,
  InstallRequest,
  Listing,
  Outcome,
  PublishRequest,
  PublishResult,
  QueryInfo,
  Refusal,
  SayRequest,
  SayResult,
  TemplateInfo,
  Transport,
  WorkflowRunRequest,
} from "./port.js";
import { OperatorConfirmation, TransportError, TransportTimeout } from "./port.js";
import { assertSayResult, assertAskResult, assertPublishResult, assertDoctorReport, assertObject } from "./schema.js";

export interface CliTransportOptions {
  /** Path or name of the cockpit executable. A setting, never derived from data. */
  readonly executable: string;
  /** The workspace folder the window opened; the cockpit's binding check runs against it. */
  readonly cwd: string;
  /** Extra environment (tests: registry, identity file). Merged over process.env. */
  readonly env?: Readonly<Record<string, string>>;
  /** How long one call may take; a call that takes longer is killed and reported as timed out. */
  readonly timeoutMs?: number;
  /**
   * Until cockpit v0.5.3 fills ui/blocks/compositions in `ui list --json`: a file holding that answer
   * (setting kton.ui.listFixture; the e2e writes it with ktonpkg v0.9.1). Unset: the real verb.
   */
  readonly uiListFixture?: string;
}

interface RunResult {
  readonly code: number | null;
  readonly stdout: string;
  readonly stderr: string;
}

export class CliTransport implements Transport {
  /** Every argv the transport ran, newest last. For the local output channel (O7) and tests. */
  readonly calls: string[][] = [];

  constructor(private readonly opts: CliTransportOptions) {}

  private run(args: readonly string[]): Promise<RunResult> {
    const argv = [...args];
    this.calls.push(argv);
    return new Promise((resolve, reject) => {
      execFile(
        this.opts.executable,
        argv,
        {
          cwd: this.opts.cwd,
          env: { ...process.env, ...(this.opts.env ?? {}) },
          shell: false,
          timeout: this.opts.timeoutMs ?? 120_000,
          maxBuffer: 64 * 1024 * 1024,
          windowsHide: true,
        },
        (err, stdout, stderr) => {
          if (err && typeof (err as NodeJS.ErrnoException).code === "string") {
            // ENOENT, EACCES, …: the executable could not be started at all.
            reject(new TransportError(`cannot run ${this.opts.executable}: ${err.message}`, null, String(stderr)));
            return;
          }
          const code = err ? ((err as { code?: number | null }).code ?? null) : 0;
          if (err && (err as { killed?: boolean }).killed) {
            reject(new TransportTimeout(`${argv[0] ?? "cockpit"} timed out after ${Math.round((this.opts.timeoutMs ?? 120_000) / 1000)} s`, String(stderr)));
            return;
          }
          resolve({ code, stdout: String(stdout), stderr: String(stderr) });
        },
      );
    });
  }

  /** Map one run to an Outcome: 0 → parsed value, 2 → refusal, anything else → TransportError. */
  private async outcome<T>(args: readonly string[], check: (v: unknown) => T): Promise<Outcome<T>> {
    const r = await this.run(args);
    if (r.code === 2) return { ok: false, refusal: parseRefusal(r) };
    if (r.code !== 0) {
      throw new TransportError(firstLine(r.stderr) || `exit ${String(r.code)}`, r.code, r.stderr);
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(r.stdout);
    } catch {
      throw new TransportError(`${args[0] ?? "cockpit"}: the answer is not JSON`, r.code, r.stderr);
    }
    return { ok: true, value: check(parsed) };
  }

  say(req: SayRequest): Promise<Outcome<SayResult>> {
    if (!NAME.test(req.template)) return bad(`template ${JSON.stringify(req.template)} is not a name`);
    if (!SHA.test(req.subject)) return bad("say takes a sha256 id as its subject, never a path");
    const args = ["say", req.template, req.subject];
    for (const k of Object.keys(req.fields ?? {}).sort()) {
      if (!NAME.test(k)) return bad(`field ${JSON.stringify(k)} is not a name`);
      args.push(`${k}=${(req.fields ?? {})[k] ?? ""}`);
    }
    if (req.as !== undefined) {
      if (!NAME.test(req.as)) return bad("as is not a key name");
      args.push("--as", req.as);
    }
    if (req.scope !== undefined) {
      if (!NAME.test(req.scope)) return bad(`scope ${JSON.stringify(req.scope)} is not a name`);
      args.push("--scope", req.scope);
    }
    if (req.expectHead !== undefined) {
      if (!SHA.test(req.expectHead)) return bad("expectHead is not a sha256 id");
      args.push("--expect-head", req.expectHead);
    }
    args.push("--json");
    return this.outcome(args, assertSayResult);
  }

  ask(req: AskRequest): Promise<Outcome<AskResult>> {
    if (LINEAGE.has(req.query) && (req.ref === undefined || !SHA.test(req.ref))) {
      return bad(`ask ${req.query} takes a sha256 id, never a path`);
    }
    return this.outcome(["ask", JSON.stringify(dropUndefined(req))], assertAskResult);
  }

  publish(req: PublishRequest): Promise<Outcome<PublishResult>> {
    return this.outcome(["publish", JSON.stringify(dropUndefined(req))], assertPublishResult);
  }

  workflowRun(req: WorkflowRunRequest): Promise<Outcome<Readonly<Record<string, unknown>>>> {
    if (!NAME.test(req.workflow)) return bad(`workflow ${JSON.stringify(req.workflow)} is not a name`);
    const args = ["workflow", "run", req.workflow];
    if (req.check) args.push("--check");
    for (const [k, v] of Object.entries(req.bind)) {
      if (!/^[A-Za-z_][A-Za-z0-9_-]*$/.test(k)) {
        return Promise.resolve({ ok: false, refusal: { code: "argument", reason: `hole name ${JSON.stringify(k)} is not a name` } });
      }
      args.push("--bind", `${k}=${v}`);
    }
    args.push("--json");
    return this.outcome(args, (v) => assertObject(v, "workflow run"));
  }

  install(req: InstallRequest, confirmation: OperatorConfirmation | undefined): Promise<Outcome<Readonly<Record<string, unknown>>>> {
    if (!(confirmation instanceof OperatorConfirmation)) {
      return Promise.resolve({
        ok: false,
        refusal: { code: "operator.unconfirmed", reason: "install is an operator command: a person confirms it in the window" },
      });
    }
    if (req.packageDir === "" || req.packageDir.startsWith("-")) return bad("a package folder must not start with '-'");
    const args = ["install", req.packageDir];
    if (req.allow) args.push("--allow");
    args.push("--json");
    return this.outcome(args, (v) => assertObject(v, "install"));
  }

  /** `cockpit ui list --json`, or the fixture in its shape until the verb lists declarations (v0.5.3). */
  async uiList(): Promise<Outcome<UiListing>> {
    if (this.opts.uiListFixture) {
      const raw = await readFile(this.opts.uiListFixture, "utf8");
      let v: unknown;
      try {
        v = JSON.parse(raw);
      } catch (e) {
        throw new TransportError(`ui list fixture: ${(e as Error).message}`, 0, "");
      }
      return { ok: true, value: assertUiListing(v) };
    }
    return this.outcome(["ui", "list", "--json"], assertUiListing);
  }

  /** blobs/<hh>/<hex> of an installed package under .kton/packages; nothing outside it is read. */
  async packageBlob(dir: string, hash: string): Promise<Uint8Array> {
    const m = /^sha256:([0-9a-f]{64})$/.exec(hash);
    if (!m) throw new TransportError(`${JSON.stringify(hash)} is not sha256:<hex>`, 0, "");
    const rel = normalize(dir);
    if (isAbsolute(rel) || !rel.startsWith(join(".kton", "packages") + sep) || rel.split(sep).includes("..")) throw new TransportError(`${JSON.stringify(dir)} is not an installed package's folder`, 0, "");
    const hex = m[1] as string;
    return readFile(join(this.opts.cwd, rel, "blobs", hex.slice(0, 2), hex));
  }

  /**
   * graph@1 (until the CLI prints them): `cockpit show 127.0.0.1:<free port>` for as long as it takes to read
   * /data/union.json, keys.json and names.json from this process, then stopped. Loopback only; the webview never
   * talks to it. Shape-checked: records a list, keys and names objects.
   */
  async graph(): Promise<{ records: unknown[]; keys: Record<string, unknown>; names: Record<string, unknown>; packages: Record<string, unknown> }> {
    const port = await new Promise<number>((resolve, reject) => {
      const srv = createServer();
      srv.once("error", reject);
      srv.listen(0, "127.0.0.1", () => {
        const a = srv.address();
        srv.close(() => (a && typeof a === "object" ? resolve(a.port) : reject(new Error("no free port"))));
      });
    });
    const argv = ["show", `127.0.0.1:${port}`];
    this.calls.push(argv);
    const child = spawn(this.opts.executable, argv, { cwd: this.opts.cwd, env: { ...process.env, ...(this.opts.env ?? {}) }, stdio: "ignore", shell: false });
    const base = `http://127.0.0.1:${port}/data/`;
    try {
      const end = Date.now() + (this.opts.timeoutMs ?? 120_000);
      for (;;) {
        try {
          const get = async (f: string) => {
            const r = await fetch(base + f);
            if (!r.ok) throw new TransportError(`cockpit show: ${f}: ${r.status} ${(await r.text()).slice(0, 200)}`, 0, "");
            return (await r.json()) as unknown;
          };
          const records = await get("union.json");
          if (!Array.isArray(records)) throw new TransportError("cockpit show: union.json is not a list of records", 0, "");
          const keys = assertObject(await get("keys.json"), "keys.json");
          const names = assertObject(await get("names.json"), "names.json");
          // the admitted packages' build keys (cockpit v0.5.3 serves /data/packages.json); before that: none
          const pr = await fetch(base + "packages.json");
          const packages = pr.ok ? (assertObject(await pr.json(), "packages.json") as Record<string, unknown>) : {};
          return { records, keys: keys as Record<string, unknown>, names: names as Record<string, unknown>, packages };
        } catch (e) {
          if (e instanceof TransportError) throw e;
          if (Date.now() > end) throw new TransportTimeout(`cockpit show did not answer on 127.0.0.1:${port}`, "");
          if (child.exitCode !== null) throw new TransportError(`cockpit show exited (${String(child.exitCode)})`, child.exitCode, "");
          await new Promise((r) => setTimeout(r, 150));
        }
      }
    } finally {
      child.kill();
    }
  }

  /** The templates this checkout's cockpit allows, with their fields (`cockpit say --json`, v0.5.0). */
  templates(): Promise<Outcome<TemplateInfo[]>> {
    return this.outcome(["say", "--json"], parseTemplates);
  }

  async list(): Promise<Outcome<Listing>> {
    const t = await this.templates();
    if (!t.ok) return t;
    const q = await this.outcome(["ask", "--json"], parseQueries);
    if (!q.ok) return q;
    const wf = await this.outcome(["workflow", "list", "--json"], parseWorkflowNames);
    if (!wf.ok) return wf;
    return { ok: true, value: { templates: t.value, queries: q.value, workflows: wf.value } };
  }

  async doctor(): Promise<DoctorReport> {
    const r = await this.run(["doctor", "--json"]);
    if (r.code !== 0 && r.code !== 3) {
      throw new TransportError(firstLine(r.stderr) || `doctor exit ${String(r.code)}`, r.code, r.stderr);
    }
    try {
      return assertDoctorReport(JSON.parse(r.stdout));
    } catch (e) {
      throw new TransportError(`doctor: ${(e as Error).message}`, r.code, r.stderr);
    }
  }

  async version(): Promise<Outcome<{ readonly text: string }>> {
    // `version --json` (since v0.5.0): {cockpit, revision, modified, go, kernels}.
    const r = await this.run(["version", "--json"]);
    if (r.code !== 0) throw new TransportError(firstLine(r.stderr) || "version failed", r.code, r.stderr);
    let v: { cockpit?: unknown; revision?: unknown };
    try {
      v = JSON.parse(r.stdout) as typeof v;
    } catch {
      throw new TransportError("version: the answer is not JSON", r.code, r.stderr);
    }
    if (typeof v.cockpit !== "string") throw new TransportError("version: no cockpit field", r.code, r.stderr);
    const rev = typeof v.revision === "string" && v.revision ? ` ${v.revision.slice(0, 12)}` : "";
    return { ok: true, value: { text: `cockpit ${v.cockpit}${rev}` } };
  }
}

const NAME = /^[A-Za-z][A-Za-z0-9_.-]*$/;
const SHA = /^sha256:[0-9a-f]{64}$/;
/** Queries whose ref is a record or content hash (cockpit/ask.go). */
const LINEAGE: ReadonlySet<string> = new Set(["record", "producer", "uses", "lineage", "reproductions", "about"]);

function bad(reason: string): Promise<{ ok: false; refusal: Refusal }> {
  return Promise.resolve({ ok: false, refusal: { code: "argument", reason } });
}

function parseRefusal(r: RunResult): Refusal {
  try {
    const v = JSON.parse(r.stdout) as Partial<Refusal>;
    if (typeof v.code === "string" && typeof v.reason === "string") {
      return v.clause ? { code: v.code, clause: v.clause, reason: v.reason } : { code: v.code, reason: v.reason };
    }
  } catch {
    /* refusals before v0.5.0: reason on stderr */
  }
  return { code: "refused", reason: r.stderr.trim() || "the cockpit refused without a reason" };
}

function firstLine(s: string): string {
  return s.trim().split("\n")[0] ?? "";
}

function dropUndefined<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as T;
}

function parseWorkflowNames(v: unknown): string[] {
  const arr = Array.isArray(v) ? v : Array.isArray((v as { workflows?: unknown }).workflows) ? (v as { workflows: unknown[] }).workflows : null;
  if (!arr) throw new TransportError("workflow list: expected a list", 0, "");
  return arr.map((w) => (typeof w === "string" ? w : String((w as { name?: unknown }).name ?? ""))).filter((n) => n !== "");
}

function parseTemplates(v: unknown): TemplateInfo[] {
  const arr = Array.isArray(v) ? v : (v as { templates?: unknown }).templates;
  if (!Array.isArray(arr)) throw new TransportError("template listing: expected a list", 0, "");
  return arr.map((t) => {
    const o = t as Partial<TemplateInfo> & { about?: unknown };
    if (typeof o.name !== "string" || typeof o.fields !== "object" || o.fields === null || Array.isArray(o.fields)) {
      throw new TransportError("template listing: an entry has no name or fields", 0, "");
    }
    for (const [k, f] of Object.entries(o.fields)) {
      if (typeof f !== "object" || f === null || typeof (f as { type?: unknown }).type !== "string") throw new TransportError(`template listing: ${o.name}.${k} has no type`, 0, "");
    }
    return { name: o.name, fields: o.fields, ...(typeof o.about === "string" ? { target: o.about } : {}), ...(typeof o.package === "string" ? { package: o.package } : {}) };
  });
}

function parseQueries(v: unknown): QueryInfo[] {
  const arr = Array.isArray(v) ? v : (v as { queries?: unknown }).queries;
  if (!Array.isArray(arr)) throw new TransportError("query listing: expected a list", 0, "");
  return arr.map((q) => {
    // `ask --json` names a package query's parameters `params` (v0.5.0, the shipped name).
    const o = q as { name?: unknown; params?: unknown; package?: unknown };
    if (typeof o.name !== "string") throw new TransportError("query listing: an entry has no name", 0, "");
    return { name: o.name, parameters: Array.isArray(o.params) ? o.params.map(String) : [], ...(typeof o.package === "string" ? { package: o.package } : {}) };
  });
}
