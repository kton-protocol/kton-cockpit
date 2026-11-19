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

import { execFile } from "node:child_process";
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
}

interface RunResult {
  readonly code: number | null;
  readonly stdout: string;
  readonly stderr: string;
  /** stdout as bytes (ui cat) */
  readonly bytes?: Uint8Array;
}

export class CliTransport implements Transport {
  /** Every argv the transport ran, newest last. For the local output channel (O7) and tests. */
  readonly calls: string[][] = [];

  constructor(private readonly opts: CliTransportOptions) {}

  private run(args: readonly string[], binary = false): Promise<RunResult> {
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
          encoding: "buffer",
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
          resolve({ code, stdout: String(stdout), stderr: String(stderr), ...(binary ? { bytes: new Uint8Array(stdout as Buffer) } : {}) });
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
    if (!QNAME.test(req.template)) return bad(`template ${JSON.stringify(req.template)} is not a name`);
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

  /** `cockpit ui list --json` (v0.5.4): packages, admission, refusals, ui entries, blocks, compositions, members. */
  uiList(): Promise<Outcome<UiListing>> {
    return this.outcome(["ui", "list", "--json"], assertUiListing);
  }

  /**
   * `cockpit ui cat <package> <sha256:hash>` (v0.5.4): one member's bytes of a usable package, from stdout. The
   * caller checks them against the hash again; the shell reads no package folder itself (C1.5).
   */
  async uiCat(pkg: string, hash: string): Promise<Uint8Array> {
    if (!/^sha256:[0-9a-f]{64}$/.test(hash)) throw new TransportError(`${JSON.stringify(hash)} is not sha256:<hex>`, 0, "");
    if (pkg === "" || pkg.startsWith("-")) throw new TransportError(`${JSON.stringify(pkg)} is not a package`, 0, "");
    const r = await this.run(["ui", "cat", pkg, hash], true);
    if (r.code === 2) {
      // v0.5.4: ui.package = the package is not usable here (not admitted, failing its check, not installed);
      // ui.member = the package has no member with that hash
      const why = parseRefusal(r);
      const what = why.code === "ui.package" ? `package ${pkg} is not usable here (not admitted, failing its check or not installed)` : why.code === "ui.member" ? `package ${pkg} has no member ${hash}` : "refused";
      throw new TransportError(`ui cat: ${what}: ${why.reason}`, 2, r.stderr);
    }
    if (r.code !== 0) throw new TransportError(firstLine(r.stderr) || `ui cat: exit ${String(r.code)}`, r.code, r.stderr);
    return r.bytes ?? new Uint8Array();
  }

  /**
   * graph@1's data: `cockpit show --json` (v0.5.3), the snapshot a viewer fetches: every record with the ring to
   * verify them (union, keys, names) and the admitted packages' build keys, labelled ({keyid: {key, packages:
   * [{package, id, dir}]}}: a label, never trust). Shape-checked; packages is passed on unchanged.
   */
  graph(): Promise<Outcome<{ records: unknown[]; keys: Record<string, unknown>; names: Record<string, unknown>; packages: Record<string, unknown> }>> {
    return this.outcome(["show", "--json"], (v) => {
      const o = assertObject(v, "show") as Record<string, unknown>;
      if (!Array.isArray(o["union"])) throw new TransportError("show: union is not a list of records", 0, "");
      const keys = assertObject(o["keys"] ?? {}, "show keys") as Record<string, unknown>;
      const names = assertObject(o["names"] ?? {}, "show names") as Record<string, unknown>;
      const packages = assertObject(o["packages"] ?? {}, "show packages") as Record<string, unknown>;
      for (const [k, e] of Object.entries(packages)) {
        const pk = e as { key?: unknown; packages?: unknown };
        if (typeof pk !== "object" || pk === null || typeof pk.key !== "string" || !Array.isArray(pk.packages)) throw new TransportError(`show: packages.${k} is not {key, packages: [...]}`, 0, "");
      }
      return { records: o["union"] as unknown[], keys, names, packages };
    });
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
/** a name, or <package>/<name> as the cockpit offers a name two sources bring (v0.5.3, per-package vocabulary) */
const QNAME = /^([A-Za-z][A-Za-z0-9_.-]*\/)?[A-Za-z][A-Za-z0-9_.-]*$/;
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
