// The VS Code host of a composition (C1.5 v3, VERTICLE-HOSTS §11–16). The webview runs kton-web's
// renderer (core/compose.js, vendored) inside its composition frame; this side answers kton:request and
// performs intents. No VS Code here, so it is unit-tested; src/composition/panel.ts puts it in a webview.
//
// A frame is untrusted. A request is answered only for a use of the plan this host made, with that use's
// query and params (a frame may only refine what the use declares); an intent is performed only as an act
// the plan declares for that use, through that act's control, whose KIND (decision/record, from the package,
// ktonpkg Control.EffectiveKind) decides the key: a decision goes through the one sign dialog with the person
// key, a record is written with the agent key and no dialog. The intent cannot change the kind (S4.4), and a
// kind no act has (say, post) is refused (S4.6).
//
// kton-web's five adapter points (19.11.): (a) a request's vars arrive in m.vars, beside args; (b) an intent
// arrives as {intent: {id, kind, …}}; (c) a page viewer asks query@1 {input}: the host maps it to the installed
// package's named query through the slot entry and binds it from the closed set; (d) product@1 {hash} is
// answered like content@1; (e) graph@1 is {records, keys, names, attested?} (not provided yet: refused).
import { bindVars, typedTerm } from "../../media/kton-web/core/ktonpkg-mirror/plan.js";
import type { Control, Loaded } from "../packages/model.js";

export interface Term {
  readonly type: string;
  readonly value: string;
  readonly datatype?: string;
}
export interface PlanAct {
  readonly intent: string;
  readonly label: string;
  readonly about?: string;
  readonly fields: Readonly<Record<string, string>>;
  readonly control?: { readonly package: string; readonly name: string };
  readonly ray?: string;
}
export interface PlanUse {
  readonly key: string;
  readonly block: string;
  readonly requires: readonly string[];
  readonly optional: readonly string[];
  readonly query?: { readonly package: string; readonly name: string; readonly hash: string };
  readonly params?: Readonly<Record<string, Term>>;
  readonly content?: { readonly source: string; readonly path: string; readonly hash: string; readonly type: string; readonly binary?: boolean };
  readonly product?: { readonly path: string; readonly hash: string };
  readonly refine?: Readonly<Record<string, { readonly values?: readonly unknown[]; readonly type?: string }>>;
  readonly acts?: readonly PlanAct[];
  readonly bind?: unknown;
  readonly waiting?: readonly string[];
}
export interface Plan {
  readonly root: { readonly package: string; readonly composition: string };
  readonly subject: { readonly iri: string; readonly claim?: string; readonly id?: string };
  readonly uses: readonly PlanUse[];
}

/** What the capabilities are answered with: the cockpit through the transport, the package bytes by hash. */
export interface Backend {
  /** query@1: a named query of an admitted package (`cockpit ask {query: sparql, name, bind}`), SPARQL JSON. */
  query(name: string, bind: Readonly<Record<string, string>>): Promise<unknown>;
  /** content@1 / product@1 bytes of a package member, by hash (checked again here). */
  bytes(dir: string, hash: string): Promise<Uint8Array>;
  /** open@1: show a file, a claim or a screen in the window; resolves with what was opened. */
  open(target: Readonly<Record<string, unknown>>): Promise<string>;
  /** sign@1 / record@1 through the shell's one sign path (C1.4): the dialog for a decision, none for a record. */
  act(control: Control, about: string, fields: Readonly<Record<string, string>>, title: string): Promise<"signed" | "cancelled" | "failed">;
  /** run@1: a ray (workflow run) with its holes. */
  run(ray: string, holes: Readonly<Record<string, string>>): Promise<string>;
  /** a URL the frame may fetch for bytes above the content cap (or undefined: then above the cap is refused). */
  urlFor?(dir: string, hash: string): string | undefined;
}
export interface HostUi {
  post(msg: unknown): void;
  log(line: string): void;
}

/** content@1 / product@1 send base64 up to this many bytes (before encoding, H17); above it a url + hash. */
export const CONTENT_CAP = 1024 * 1024;
export const DATA_CAPS = ["query@1", "product@1", "graph@1", "content@1"] as const;
/** What this host provides; graph@1 is not yet (kton-web's graph asks it as {records, keys, names}). */
export const VSCODE_PROVIDES = ["query@1", "content@1", "product@1", "open@1", "sign@1", "record@1", "run@1"] as const;
/** Acts and the control kind each one must have (O1). open and run have no control. */
const KIND_FOR: Readonly<Record<string, "decision" | "record">> = { sign: "decision", record: "record" };

export class Refused extends Error {}

/** A frame's state without acts: frames must not report them (host-held, H8). */
export function withoutActs(state: unknown): Record<string, unknown> {
  if (!state || typeof state !== "object" || Array.isArray(state)) return {};
  const { acts: _drop, ...rest } = state as Record<string, unknown>;
  return rest;
}

/** Host-held act outcomes: in a set only, dropped on the first kton:state after the set that carried them (H16). */
export class Acts {
  private readonly entries = new Map<string, Record<string, unknown>>();
  private delivered = new Set<string>();
  record(id: string, entry: Record<string, unknown>): void {
    this.entries.set(id, entry);
  }
  forSet(): Record<string, unknown> {
    this.delivered = new Set(this.entries.keys());
    return Object.fromEntries(this.entries);
  }
  received(): void {
    for (const id of this.delivered) this.entries.delete(id);
    this.delivered = new Set();
  }
  get size(): number {
    return this.entries.size;
  }
}

/**
 * The plan's use a request names, and the params to run. A use that reads vars is checked against the plan
 * re-bound with the request's vars (the same pure bindVars the renderer runs; an invalid value falls back to
 * the default, W11). Only the use's declared refine params may vary, with a declared value or type.
 */
export function useFor(plan: Plan, cap: string, args: Readonly<Record<string, unknown>>, vars: unknown): { use: PlanUse; params: Readonly<Record<string, Term>> } {
  let u = plan.uses.find((x) => x.key === args["use"]);
  if (!u) throw new Refused(`no use ${JSON.stringify(args["use"])} in this plan`);
  if (u.bind) {
    if (vars !== undefined && (typeof vars !== "object" || vars === null || Array.isArray(vars))) throw new Refused("vars is an object {composition: {var: value}}");
    u = (bindVars(plan, (vars ?? {}) as Record<string, unknown>) as Plan).uses.find((x) => x.key === args["use"]) as PlanUse;
    if (u.waiting?.length) throw new Refused(`${u.key} waits for ${u.waiting.join(", ")}`);
  }
  if (!u.requires.includes(cap) && !u.optional.includes(cap)) throw new Refused(`${u.block} does not declare ${cap}`);
  let params: Record<string, Term> = { ...(u.params ?? {}) };
  if (cap === "query@1") {
    if (!u.query) throw new Refused(`${u.key} has no query`);
    const q = args["query"] as { package?: unknown; name?: unknown; hash?: unknown } | undefined;
    if (q !== undefined && (q.package !== u.query.package || q.name !== u.query.name || q.hash !== u.query.hash)) throw new Refused(`${u.key}: the query differs from the plan's`);
    if (args["params"] !== undefined && JSON.stringify(args["params"]) !== JSON.stringify(u.params ?? {})) throw new Refused(`${u.key}: the params differ from the plan's`);
    const refine = args["refine"];
    if (refine !== undefined) {
      if (typeof refine !== "object" || refine === null || Array.isArray(refine)) throw new Refused("refine is an object");
      for (const [k, v] of Object.entries(refine)) {
        const spec = u.refine?.[k];
        if (!spec) throw new Refused(`${u.key} does not declare refine ${k}`);
        if (spec.values && !spec.values.some((x) => JSON.stringify(x) === JSON.stringify(v))) throw new Refused(`${u.key}: ${JSON.stringify(v)} is not one of the declared values of ${k}`);
        try {
          params = { ...params, [k]: typedTerm(spec.type ?? "literal", v, `refine ${k}`) as Term };
        } catch (e) {
          throw new Refused((e as Error).message);
        }
      }
    }
  }
  return { use: u, params };
}

/** A term for `cockpit ask {bind}`, which binds plain literals: the term's lexical form. */
export function bindOf(params: Readonly<Record<string, Term>>): Record<string, string> {
  return Object.fromEntries(Object.entries(params).map(([k, t]) => [k, t.value]));
}

const sha256 = async (b: Uint8Array) => {
  const { createHash } = await import("node:crypto");
  return createHash("sha256").update(b).digest("hex");
};

/** content@1 and product@1 (d): {type, hash, size, bytes} up to the cap, else {type, hash, size, url}. */
export async function answerBytes(backend: Backend, loaded: Loaded, hash: string, type: string | undefined, what: string): Promise<Record<string, unknown>> {
  const blob = loaded.blobs.get(hash);
  if (!blob) throw new Refused(`${what}: ${hash} is no file of an admitted package`);
  const bytes = await backend.bytes(blob.dir, hash);
  const got = `sha256:${await sha256(bytes)}`;
  if (got !== hash) throw new Refused(`${what}: the bytes hash to ${got}, not ${hash}`);
  const base = { type: type ?? blob.type, hash, size: bytes.byteLength };
  if (bytes.byteLength <= CONTENT_CAP) return { ...base, bytes: Buffer.from(bytes).toString("base64") };
  const url = backend.urlFor?.(blob.dir, hash);
  if (!url) throw new Refused(`${what}: ${bytes.byteLength} bytes is above the ${CONTENT_CAP}-byte cap and this host has no url for it`);
  return { ...base, url };
}

interface Intent {
  readonly id: string;
  readonly kind: string;
  readonly from?: string;
  readonly about?: string;
  readonly control?: { readonly package?: unknown; readonly name?: unknown };
  readonly ray?: unknown;
  readonly [k: string]: unknown;
}

/** (b) An intent arrives as {intent: {id, kind, …}}; the id is the bridge's, shared with requests. */
function intentOf(m: Readonly<Record<string, unknown>>): Intent | undefined {
  const i = m["intent"];
  if (typeof i !== "object" || i === null) return undefined;
  const o = i as Record<string, unknown>;
  return typeof o["id"] === "string" && typeof o["kind"] === "string" ? (o as unknown as Intent) : undefined;
}

/** The host of one composition panel. */
export class CompositionHost {
  state: Record<string, unknown>;
  readonly acts = new Acts();
  /** every message this host received, by type (tests, and the e2e's "the host sees no sibling messages") */
  readonly seen: Array<{ type: string; cap?: string; kind?: string; use?: string }> = [];
  private setPending = false;

  constructor(
    readonly plan: Plan,
    private readonly loaded: Loaded,
    private readonly backend: Backend,
    private readonly ui: HostUi,
    initialState: unknown = {},
  ) {
    this.state = withoutActs(initialState);
  }

  async onMessage(raw: unknown): Promise<void> {
    if (typeof raw !== "object" || raw === null) return;
    const m = raw as Record<string, unknown>;
    if (typeof m["type"] !== "string") return;
    const intent = intentOf(m);
    this.seen.push({ type: m["type"], ...(typeof m["cap"] === "string" ? { cap: m["cap"] } : {}), ...(intent ? { kind: intent.kind, ...(intent.from ? { use: intent.from } : {}) } : {}) });
    switch (m["type"]) {
      case "kton:ready":
        // every ready gets set (a reloaded webview says ready again), with the state it last reported
        this.setPending = true;
        this.ui.post({ type: "kton:set", state: { ...this.state, acts: this.acts.forSet() }, plan: this.plan, host: { name: "vscode", provides: VSCODE_PROVIDES } });
        return;
      case "kton:state":
        this.state = withoutActs(m["state"]);
        if (this.setPending) this.acts.received();
        this.setPending = false;
        return;
      case "kton:request":
        // (a) vars beside args
        return this.request(String(m["id"]), String(m["cap"]), (m["args"] ?? {}) as Record<string, unknown>, m["vars"]);
      case "kton:intent":
        if (intent) return this.intent(intent);
        this.ui.log("composition: an intent without {id, kind} was dropped");
        return;
    }
  }

  private async request(id: string, cap: string, args: Record<string, unknown>, vars: unknown): Promise<void> {
    try {
      if (!(VSCODE_PROVIDES as readonly string[]).includes(cap)) throw new Refused(`${cap} is not provided by this host`);
      const { use: u, params } = useFor(this.plan, cap, args, vars);
      let result: unknown;
      if (cap === "query@1") result = await this.backend.query((u.query as { name: string }).name, bindOf(params));
      else if (cap === "content@1") {
        if (!u.content?.binary || args["hash"] !== u.content.hash) throw new Refused(`${u.key}: the content differs from the plan's`);
        result = await answerBytes(this.backend, this.loaded, u.content.hash, u.content.type, "content@1");
      } else if (cap === "product@1") {
        const hash = typeof args["hash"] === "string" ? args["hash"] : u.product?.hash;
        if (!hash || (u.product && hash !== u.product.hash)) throw new Refused(`${u.key}: the product differs from the plan's`);
        result = await answerBytes(this.backend, this.loaded, hash, undefined, "product@1");
      } else throw new Refused(`${cap} is an act: it is an intent, not a request`);
      this.ui.post({ type: "kton:data", id, result });
    } catch (e) {
      const why = e instanceof Error ? e.message : String(e);
      this.ui.log(`composition: ${cap} refused for ${JSON.stringify(args["use"])}: ${why}`);
      this.ui.post({ type: "kton:refused", what: id, why });
    }
  }

  /** The plan's act this intent claims to be, with its control, or why it is refused. */
  check(i: Intent): { act: PlanAct; control?: Control } {
    const u = this.plan.uses.find((x) => x.key === i.from);
    if (!u) throw new Refused(`no use ${JSON.stringify(i.from)} in this plan`);
    if (i.kind === "open") return { act: { intent: "open", label: "open", fields: {} } };
    if (!(i.kind in KIND_FOR) && i.kind !== "run") throw new Refused(`${i.kind} is not an act this shell performs`);
    const ctl = i.control;
    const act = (u.acts ?? []).find((a) => a.intent === i.kind && (i.kind === "run" ? a.ray === i.ray : a.control?.package === ctl?.package && a.control?.name === ctl?.name));
    if (!act) throw new Refused(`${u.key} declares no ${i.kind} act${i.kind === "run" ? ` for ray ${String(i.ray)}` : ` through ${String(ctl?.package)}/${String(ctl?.name)}`}: the intent cannot change the kind`);
    if (i.kind === "run") return { act };
    if (!act.about || i.about !== act.about) throw new Refused(`${u.key}: the intent is about another subject than the plan's act`);
    const control = this.loaded.controls.get(`${act.control?.package}/${act.control?.name}`);
    if (!control) throw new Refused(`${act.control?.package} has no control ${act.control?.name}`);
    if (control.kind !== KIND_FOR[i.kind]) throw new Refused(`control ${control.name} is a ${control.kind} control, not ${KIND_FOR[i.kind]}`);
    return { act, control };
  }

  private async intent(i: Intent): Promise<void> {
    const held = i.kind === "sign" || i.kind === "record" || i.kind === "run";
    const refuse = (why: string) => {
      this.ui.log(`composition: ${i.kind} refused: ${why}`);
      if (held) this.acts.record(i.id, { use: i.from, intent: i.kind, status: "refused", result: { why } });
      this.ui.post({ type: "kton:refused", what: i.id, why });
    };
    let checked: { act: PlanAct; control?: Control };
    try {
      checked = this.check(i);
    } catch (e) {
      return refuse((e as Error).message);
    }
    try {
      let result: Record<string, unknown>;
      if (i.kind === "open") result = { message: await this.backend.open(i) };
      else if (i.kind === "run") result = { message: await this.backend.run(checked.act.ray as string, checked.act.fields) };
      else {
        const c = checked.control as Control;
        const r = await this.backend.act(c, checked.act.about as string, checked.act.fields, `${checked.act.label}: ${this.plan.root.package}/${this.plan.root.composition}`);
        if (r === "failed") return refuse(c.kind === "decision" ? "not signed (see the sign dialog)" : "not recorded");
        result = r === "signed" ? { signed: true, key: c.kind === "decision" ? "person" : "agent", message: c.kind === "decision" ? "Signed with the person key (test-only)." : "Recorded with the agent key." } : { signed: false, message: "Not signed: the dialog was cancelled." };
      }
      if (held) this.acts.record(i.id, { use: i.from, intent: i.kind, status: "done", result });
      this.ui.post({ type: "kton:data", id: i.id, result });
    } catch (e) {
      refuse(e instanceof Error ? e.message : String(e));
    }
  }
}
