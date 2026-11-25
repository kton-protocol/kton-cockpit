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
import { bindVars, typedTerm, varValuesOf } from "../../media/kton-web/core/ktonpkg-mirror/plan.js";
import type { Control, Loaded } from "../packages/model.js";
import type { BoundTerm } from "../transport/port.js";
import { requireReviewedOf } from "../ui/annotate.js";

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
  /** the mirror's queryKey of the bound query; null while a (non-optional) param waits for its var */
  readonly ask?: string | null;
}
export interface Plan {
  readonly root: { readonly package: string; readonly composition: string };
  readonly subject: { readonly iri: string; readonly claim?: string; readonly id?: string };
  readonly uses: readonly PlanUse[];
}

/** What the capabilities are answered with: the cockpit through the transport, the package bytes by hash. */
export interface Backend {
  /**
   * query@1: a named query of an admitted package (`cockpit ask {query: sparql, name, bindTerms}`), SPARQL JSON;
   * the package says whose query it is when two packages bring the name.
   */
  query(pkg: string, name: string, terms: Readonly<Record<string, BoundTerm>>, use?: string): Promise<unknown>;
  /** content@1 / product@1 bytes of a package member, by package id and hash (`ui cat`; checked again here). */
  bytes(pkg: string, hash: string): Promise<Uint8Array>;
  /** open@1: show a file, a claim or a screen in the window; resolves with what was opened. */
  open(target: Readonly<Record<string, unknown>>): Promise<string>;
  /**
   * sign@1 / record@1 through the shell's one sign path (C1.4): the dialog for a decision, none for a record. `signed`
   * is called with the claim id and the fields as signed (what the person may have edited), once it is signed.
   */
  act(control: Control, about: string, fields: Readonly<Record<string, string>>, title: string, proposed?: Readonly<Record<string, string>>, signed?: (s: Signed) => void, asks?: readonly AskEach[], check?: CheckAgainst, review?: import("../ui/annotate.js").RequireReviewed & { readonly package: string }): Promise<"signed" | "cancelled" | "failed">;
  /** run@1: a ray (workflow run) with its holes. */
  run(ray: string, holes: Readonly<Record<string, string>>): Promise<string>;
  /** graph@1 (e): the registry's records with the ring to verify them: {records, keys, names}. */
  graph?(): Promise<{ readonly records: readonly unknown[]; readonly keys: unknown; readonly names: unknown; readonly packages: unknown }>;
  /** a URL the frame may fetch for bytes above the content cap (or undefined: then above the cap is refused). */
  urlFor?(pkg: string, hash: string): string | undefined;
}
/** A signature as made: its claim id and the fields signed. */
export interface Signed {
  readonly id: string;
  readonly fields: Readonly<Record<string, string>>;
}
export interface HostUi {
  post(msg: unknown): void;
  log(line: string): void;
  /** the frame's state after each change, for a view that keeps it in the window (CompositionUi.memo); never acts */
  keep?(state: Record<string, unknown>): void;
  /** the view changed (an answer, a state): the support file follows the active panel (kton.support.viewFile) */
  changed?(): void;
}

/** content@1 / product@1 send base64 up to this many bytes (before encoding, H17); above it a url + hash. */
export const CONTENT_CAP = 1024 * 1024;
export const DATA_CAPS = ["query@1", "product@1", "graph@1", "content@1"] as const;
/** What this host provides; graph@1 is not yet (kton-web's graph asks it as {records, keys, names}). */
export const VSCODE_PROVIDES = ["query@1", "content@1", "product@1", "open@1", "sign@1", "record@1", "run@1"] as const;
/**
 * The width (CSS px of the composition frame's holder) below which a layout that declares `narrow` switches to
 * it (ktonpkg v0.9.5 layout; kton-web reads host.narrow). 640 suits a VS Code editor column: a full editor at
 * a common window width is wider, a side-by-side half on a laptop or a narrow window is not. A layout without
 * `narrow` never changes with width.
 */
export const NARROW_PX = 820;
/** A leaf (a page viewer in its own frame) is also given graph@1. */
export const LEAF_PROVIDES = ["graph@1", "query@1", "product@1", "open@1", "sign@1"] as const;
/** Acts and the control kind each one must have (O1). open and run have no control. */
const KIND_FOR: Readonly<Record<string, "decision" | "record">> = { sign: "decision", record: "record" };

export class Refused extends Error {}

/** The claim an act of a claimless subject (a view's own place) is planned about: never signed; such an act is only
 * about a row its block was shown (option target). */
export const ROW_ONLY = `sha256:${"0".repeat(64)}`;

/**
 * The unbound-parameter guard (kton-review on ktonpkg v0.9.11): an optional $VAR left unbound is a free SPARQL variable,
 * so a FILTER that uses it vanishes and the query answers unfiltered. These are the use's declared optional params
 * (queryOptional) that the params given lack.
 */
export function unboundParams(u: PlanUse, params: Readonly<Record<string, unknown>>): string[] {
  return ((u as { queryOptional?: readonly string[] }).queryOptional ?? []).filter((k) => params[k] === undefined);
}
/** Does a use feed a signature or a record: an act with intent sign or record, or a field it proposes (option propose)? */
export function feedsAct(u: PlanUse): boolean {
  const propose = (u as { options?: Record<string, unknown> }).options?.["propose"];
  const proposes = Array.isArray(propose) ? propose.length > 0 : typeof propose === "string" && propose !== "";
  return proposes || (u.acts ?? []).some((a) => a.intent === "sign" || a.intent === "record");
}
const notSet = (names: readonly string[]) => `${names.join(", ")} ${names.length > 1 ? "are" : "is"} not set`;
/** What a guarded use shows instead of its answer: it neither runs its query nor offers an act. */
export const cannotShow = (names: readonly string[]) => `cannot show: ${notSet(names)}`;
/** An answer run with unbound optional params, stamped so: `unfiltered` names them (blocks show "unfiltered: <p> not set"). */
/** The key of a use's answer in the host's memory: the use, its params and its refine values. */
export function answerKey(use: string, params: Readonly<Record<string, Term>>, refine?: unknown): string {
  return JSON.stringify([use, params, refine ?? null]);
}

/** "sha256:1234abcd…" → "1234abcd" (the claim's short id; the full one is in the notification and the tooltip). */
export function shortClaim(id: string): string {
  return String(id).replace(/^sha256:/, "").slice(0, 8);
}

/**
 * What a signature was, in a few words: the act (or the control's title), what it is about (the row's target, its last
 * segment decoded) and the fields the person chose, e.g. "Sign verdict… · item-12#e4 · verdict right".
 */
/**
 * The choices a use declares for an act (options.choices "[control:]field=value|Label"): only a non-empty value, and only
 * a choice for this act's control or for every act.
 */
export function declaredChoices(u: PlanUse | undefined, act: PlanAct): Array<{ field: string; value: string }> {
  const raw = (u as { options?: Record<string, unknown> } | undefined)?.options?.["choices"];
  const list = Array.isArray(raw) ? raw.filter((x): x is string => typeof x === "string") : [];
  const name = act.control?.name;
  return list.flatMap((c) => {
    const cfv = c.includes("|") ? c.slice(0, c.indexOf("|")) : c;
    const eq = cfv.indexOf("=");
    if (eq < 1) return [];
    const left = cfv.slice(0, eq);
    const value = cfv.slice(eq + 1);
    const colon = left.indexOf(":");
    const control = colon < 0 ? undefined : left.slice(0, colon);
    const field = colon < 0 ? left : left.slice(colon + 1);
    return value !== "" && field !== "" && (control === undefined || control === name) ? [{ field, value }] : [];
  });
}

export function signedWhat(c: { name: string; title?: string }, act: { label?: string }, i: Readonly<Record<string, unknown>>, made?: { fields: Readonly<Record<string, string>> }): string {
  const name = (act.label ?? c.title ?? c.name).replace(/…$/, "");
  const target = typeof i["target"] === "string" && i["target"] ? lastSegment(i["target"]) : "";
  const fields = (i["fields"] ?? {}) as Record<string, unknown>;
  const chosen = Object.keys(fields)
    // what was signed when the signature is known (a proposal the person changed or that was not taken is not named)
    .map((k) => [k, made ? (made.fields[k] ?? "") : String(fields[k])] as const)
    .filter(([, v]) => v && v.length <= 60)
    .map(([k, v]) => `${k} ${v}`);
  return [name, target, ...chosen].filter(Boolean).join(" · ");
}

function lastSegment(iri: string): string {
  // the last path segment, then its fragment if it has one (…/cases#case-3), decoded (…/item-12%23e4)
  const seg = iri.replace(/\/+$/, "").split("/").pop() ?? iri;
  const s = seg.includes("#") ? seg.slice(seg.lastIndexOf("#") + 1) : seg;
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

export function stampUnfiltered(answer: unknown, names: readonly string[]): unknown {
  return answer && typeof answer === "object" && !Array.isArray(answer) ? { ...(answer as Record<string, unknown>), unfiltered: [...names] } : answer;
}

/**
 * A use's `nudge` option filled in after its act signed: {claim} is the claim id, {<field>} a signed field. The values go
 * into a shell double-quoted string (", \\, $ and ` escaped; line breaks as spaces). Undefined when a placeholder has no
 * value. The shell sends no lane message; it prints the line for the person (or a lane) to send.
 */
export function nudgeLine(template: unknown, s: Signed): string | undefined {
  if (typeof template !== "string" || !template) return undefined;
  const esc = (v: string) => v.replace(/[\\"$`]/g, (c) => `\\${c}`).replace(/[\r\n]+/g, " ");
  let missing = false;
  const line = template.replace(/\{([A-Za-z][A-Za-z0-9_]*)\}/g, (_m, k: string) => {
    const v = k === "claim" ? s.id : s.fields[k];
    if (v === undefined) missing = true;
    return esc(v ?? "");
  });
  return missing ? undefined : line;
}

/**
 * The nudge template for an act through a control: the use's `nudges` entry "<control>|<line>" for that control, else
 * its `nudge` (one line for every act of the use).
 */
export function nudgeFor(options: Readonly<Record<string, unknown>> | undefined, control: string): string | undefined {
  const many = options?.["nudges"];
  if (Array.isArray(many)) {
    for (const x of many) if (typeof x === "string" && x.startsWith(`${control}|`)) return x.slice(control.length + 1);
  }
  return typeof options?.["nudge"] === "string" ? options["nudge"] : undefined;
}

/**
 * A field the shell asks per key (a use's option `askEach`, "[control:]field=keyField[|v1,v2,…]"): its value is a JSON
 * object {key: value} over the keys in the act's `keyField` (" ;; "-separated). With values, each key's value is picked
 * among them (or none); without, it is typed (empty: none). The shell asks it, never the frame (e.g. what a rerun expects
 * of each test, and the condition that excludes one).
 */
export interface AskEach {
  readonly field: string;
  readonly key: string;
  readonly values?: readonly string[];
}
export function askEachOf(options: Readonly<Record<string, unknown>> | undefined, control: string): AskEach[] {
  const raw = options?.["askEach"];
  const list = Array.isArray(raw) ? raw : typeof raw === "string" ? [raw] : [];
  const out: AskEach[] = [];
  for (const x of list) {
    if (typeof x !== "string") continue;
    const m = /^(?:([^:=|]+):)?([A-Za-z_][A-Za-z0-9_]*)=([A-Za-z_][A-Za-z0-9_]*)(?:\|(.+))?$/.exec(x.trim());
    if (!m || (m[1] !== undefined && m[1] !== control)) continue;
    const values = m[4]?.split(",").map((v) => v.trim()).filter(Boolean);
    out.push({ field: m[2] as string, key: m[3] as string, ...(values?.length ? { values } : {}) });
  }
  return out;
}

/**
 * What a sign act's values are checked against before the dialog (a use's option checkAgainst,
 * "[control:]query|keyColumn|valueColumn|field|keyField[|noteField]"): the query's current answer (a named query of the
 * use's package, e.g. counts derived from the signed claims), its row whose keyColumn equals the act's keyField, and its
 * valueColumn against the act's field. The dialog lists every difference and the query's values for every key; signing
 * stays possible, and the differences are added to noteField when the template has it.
 */
export interface CheckAgainst {
  readonly package: string;
  readonly query: string;
  readonly keyColumn: string;
  readonly valueColumn: string;
  readonly field: string;
  readonly keyField: string;
  readonly noteField?: string;
}
export function checkAgainstOf(options: Readonly<Record<string, unknown>> | undefined, control: string, pkg: string): CheckAgainst | undefined {
  const raw = options?.["checkAgainst"];
  if (typeof raw !== "string") return undefined;
  const m = /^(?:([^:|]+):)?(.+)$/.exec(raw.trim());
  if (!m || (m[1] !== undefined && m[1] !== control)) return undefined;
  const [query, keyColumn, valueColumn, field, keyField, noteField] = (m[2] ?? "").split("|").map((x) => x.trim());
  if (!query || !keyColumn || !valueColumn || !field || !keyField) return undefined;
  return { package: pkg, query, keyColumn, valueColumn, field, keyField, ...(noteField ? { noteField } : {}) };
}

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
    // ktonpkg v0.10.0 (Compose): only a query PARAM that is still waiting holds the ask (the mirror's ask is null then);
    // a var read by an option waits on its own, and a param that has a value never blocks
    if (cap === "query@1" && u.query && u.ask === null) throw new Refused(`${u.key} waits for ${(u.waiting ?? []).join(", ")}`);
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

/** A planned param as a typed term for `cockpit ask {bindTerms}` (v0.5.3): an IRI stays an IRI, a typed literal keeps its datatype. */
export function termsOf(params: Readonly<Record<string, Term>>): Record<string, BoundTerm> {
  return Object.fromEntries(
    Object.entries(params).map(([k, t]) => [k, t.type === "uri" ? { type: "iri", value: t.value } : { type: "literal", value: t.value, ...(t.datatype ? { datatype: t.datatype } : {}) }]),
  );
}

const sha256 = async (b: Uint8Array) => {
  const { createHash } = await import("node:crypto");
  return createHash("sha256").update(b).digest("hex");
};

/** content@1 and product@1 (d): {type, hash, size, bytes} up to the cap, else {type, hash, size, url}. */
export async function answerBytes(backend: Backend, loaded: Loaded, hash: string, type: string | undefined, what: string, only?: { readonly id: string; readonly name: string }): Promise<Record<string, unknown>> {
  // the plan pins the hash, so any package holding these bytes serves them; a leaf asks for its own package's file
  const holders = loaded.byHash.get(hash);
  if (!holders?.size) throw new Refused(`${what}: ${hash} is no file of an admitted package`);
  const blob = only ? holders.get(only.id) : [...holders.values()][0];
  if (!blob) throw new Refused(`${what}: ${hash} is no file of package ${only?.name}`);
  const bytes = await backend.bytes(blob.pkg, hash);
  const got = `sha256:${await sha256(bytes)}`;
  if (got !== hash) throw new Refused(`${what}: the bytes hash to ${got}, not ${hash}`);
  const base = { type: type ?? blob.type, hash, size: bytes.byteLength };
  if (bytes.byteLength <= CONTENT_CAP) return { ...base, bytes: Buffer.from(bytes).toString("base64") };
  const url = backend.urlFor?.(blob.pkg, hash);
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

/**
 * The host of a leaf: a page viewer (graph, render, feed) in its own frame, for one slot entry and subject.
 * It gets no plan: after ready, a set with the state its slot binds (viewer.json slots: graph's record-view binds
 * focus = the subject). Its data comes as answers: graph@1 (e), query@1 {input} (c: the viewer.json input's query,
 * bound from the closed set through the slot entry's inputs), product@1 {hash} (d). Acts: open is the shell's;
 * approve goes through the entry's control (its kind decides the key); navigate is answered as done (unwired).
 */
export class LeafHost {
  readonly seen: Array<{ type: string; cap?: string; kind?: string }> = [];
  constructor(
    private readonly leaf: { readonly decl: Readonly<Record<string, unknown>>; readonly slot: string },
    private readonly entry: { readonly inputs?: Readonly<Record<string, string>>; readonly control?: Control; readonly label: string; readonly package?: string },
    private readonly subject: { readonly id: string; readonly claim?: string; readonly closed: Readonly<Record<string, string>> },
    private readonly loaded: Loaded,
    private readonly backend: Backend,
    private readonly ui: HostUi,
  ) {}

  /** The state the slot binds: viewer.json slots.<slot>.binds, e.g. {focus: "subject"} → focus = the subject's id. */
  initialState(): Record<string, unknown> {
    const binds = ((this.leaf.decl["slots"] as Record<string, { binds?: Record<string, string> }> | undefined)?.[this.leaf.slot]?.binds ?? {}) as Record<string, string>;
    return Object.fromEntries(Object.entries(binds).filter(([, v]) => v === "subject").map(([k]) => [k, this.subject.id]));
  }

  async onMessage(raw: unknown): Promise<void> {
    if (typeof raw !== "object" || raw === null) return;
    const m = raw as Record<string, unknown>;
    if (typeof m["type"] !== "string") return;
    const intent = intentOf(m);
    this.seen.push({ type: m["type"], ...(typeof m["cap"] === "string" ? { cap: m["cap"] } : {}), ...(intent ? { kind: intent.kind } : {}) });
    if (m["type"] === "kton:ready") return void this.ui.post({ type: "kton:set", state: this.initialState(), host: { name: "vscode", provides: LEAF_PROVIDES } });
    if (m["type"] === "kton:request") return this.request(String(m["id"]), String(m["cap"]), (m["args"] ?? {}) as Record<string, unknown>);
    if (m["type"] === "kton:intent" && intent) return this.intent(intent);
  }

  private declares(cap: string): boolean {
    const d = this.leaf.decl;
    return [...((d["requires"] ?? []) as string[]), ...((d["optional"] ?? []) as string[])].includes(cap);
  }

  private async request(id: string, cap: string, args: Record<string, unknown>): Promise<void> {
    try {
      if (!this.declares(cap)) throw new Refused(`this viewer does not declare ${cap}`);
      let result: unknown;
      if (cap === "graph@1") {
        if (!this.backend.graph) throw new Refused("graph@1 is not provided by this host");
        const g = await this.backend.graph();
        // packages: cockpit show's admitted packages' build keys {keyid: {package, id, dir, key}}, passed on unchanged
        result = { records: g.records, keys: g.keys, names: g.names, packages: g.packages };
      } else if (cap === "query@1") {
        const input = String(args["input"] ?? "");
        const spec = ((this.leaf.decl["inputs"] ?? {}) as Record<string, { query?: string; bind?: Record<string, string> }>)[input];
        if (!spec?.query) throw new Refused(`the viewer declares no input ${JSON.stringify(input)}`);
        const bind: Record<string, string> = {};
        for (const k of Object.keys(spec.bind ?? {})) {
          const src = this.entry.inputs?.[k];
          const v = src ? this.subject.closed[src] : undefined;
          if (v === undefined) throw new Refused(`input ${input}: ${k} is not bound by the slot entry from the closed set`);
          bind[k] = v;
        }
        if (!this.entry.package) throw new Refused(`input ${input}: the slot entry names no package to ask the query of`);
        result = await this.backend.query(this.entry.package, spec.query, Object.fromEntries(Object.entries(bind).map(([k, v]) => [k, { type: "literal" as const, value: v }])));
      } else if (cap === "product@1") {
        // a leaf is served its own entry's package only, never another admitted package's files
        const own = this.entry.package ? this.loaded.packages[this.entry.package] : undefined;
        if (!own) throw new Refused("product@1: the slot entry names no loaded package");
        const hash = String(args["hash"] ?? "");
        result = await answerBytes(this.backend, this.loaded, hash, undefined, "product@1", { id: own.id, name: own.name });
      } else throw new Refused(`${cap} is not provided to a leaf`);
      this.ui.post({ type: "kton:data", id, result });
    } catch (e) {
      const why = e instanceof Error ? e.message : String(e);
      this.ui.log(`leaf: ${cap} refused: ${why}`);
      this.ui.post({ type: "kton:refused", what: id, why });
    }
  }

  private async intent(i: Intent): Promise<void> {
    const refuse = (why: string) => this.ui.post({ type: "kton:refused", what: i.id, why });
    try {
      if (i.kind === "open") return this.ui.post({ type: "kton:data", id: i.id, result: { message: await this.backend.open(i) } });
      if (i.kind === "navigate") return this.ui.post({ type: "kton:data", id: i.id, result: { message: "noted" } });
      if (i.kind === "approve") {
        const c = this.entry.control;
        if (!c || !this.subject.claim) return refuse("this slot entry names no control to approve with");
        // an approval is a decision: only a decision control (person key, through the sign dialog) may carry it. A
        // record control would let an untrusted frame write with the agent key, with no dialog and no click (review on #174)
        if (c.kind !== "decision") return refuse(`control ${c.package}/${c.name} is a ${c.kind} control: an approval is a decision, signed through the dialog`);
        const r = await this.backend.act(c, this.subject.claim, {}, `${this.entry.label}: approve`);
        return r === "failed" ? refuse("not signed") : this.ui.post({ type: "kton:data", id: i.id, result: { signed: r === "signed" } });
      }
      refuse(`${i.kind} is not an act this shell performs`);
    } catch (e) {
      refuse(e instanceof Error ? e.message : String(e));
    }
  }
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
  /** how many times the frame said ready: an act that ends after a reload must reach the new frame (H8) */
  private loads = 0;
  /**
   * The last answer of each use (key: use + params) and when its query started; never a skeleton after a signature:
   * after a signature here the cockpit may rebuild a federated index cold, and every ask waits for
   * it. A re-ask (the frame reloads when the sign dialog took its column) is answered at once with the last answer,
   * marked `refreshing`; the fresh one follows as a kton-workbench:answer message (or the error that kept it stale).
   */
  private readonly answers = new Map<string, { result: unknown; epoch: number }>();
  private readonly refreshes = new Map<string, number>();
  /** the queries running now, by answer key: a second ask of the same shares the first */
  private readonly pending = new Map<string, Promise<unknown>>();
  /** the last answer each use got, with its params: what the view shows now (the support file) */
  private readonly lastByUse = new Map<string, { result: unknown; params: Readonly<Record<string, Term>> }>();
  /** The view's blocks as answered now (for the support file): each use's last answer and its params as text. */
  shown(): Array<{ use: string; block: string; query?: string; params?: Record<string, string>; answer?: unknown }> {
    return this.plan.uses.map((u) => {
      const l = this.lastByUse.get(u.key);
      return {
        use: u.key,
        block: u.block,
        ...(u.query ? { query: `${u.query.package}/${u.query.name}` } : {}),
        ...(l ? { params: Object.fromEntries(Object.entries(l.params).map(([k, t]) => [k, t.value])), answer: l.result } : {}),
      };
    });
  }
  private refreshSeq = 0;
  /** how many signatures this view made (an answer asked before the last one is old), when the last was (ms), and what */
  private epoch = 0;
  private signedAt = 0;
  private lastSigned: { what: string; claim: string; at: number } | undefined;
  /** every act this view signed, with its use and row: sent with each set (blocks read it, ktonWorkbench.signedActs) */
  readonly signedActs: Array<{ use: string; target?: string; claim: string; at: number; fields: Record<string, string> }> = [];

  constructor(
    readonly plan: Plan,
    private readonly loaded: Loaded,
    private readonly backend: Backend,
    private readonly ui: HostUi,
    initialState: unknown = {},
    /**
     * The uses whose block draws its own act buttons (viewer.json `rendersActs`): the frame gets their acts as
     * `blockActs` and `acts` empty, so the composition renderer adds no second, target-less act bar.
     */
    private readonly rendersActs: ReadonlySet<string> = new Set(),
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
        this.loads++;
        this.sendSet();
        return;
      case "kton:state":
        this.state = withoutActs(m["state"]);
        this.ui.keep?.(this.state);
        this.ui.changed?.();
        if (this.setPending) this.acts.received();
        this.setPending = false;
        // a var change that guards a use (or frees it) changes which acts the frame may offer: a new set
        if (this.loads > 0 && JSON.stringify([...this.guarded()]) !== this.sentGuard) this.sendSet();
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
      if (cap === "query@1") {
        const q = u.query as { package: string; name: string };
        // the unbound-parameter guard: a use that feeds a sign act or proposes a field never runs unfiltered; any
        // other use runs, and its answer says it is unfiltered
        const unbound = unboundParams(u, params);
        if (unbound.length && feedsAct(u)) throw new Refused(cannotShow(unbound));
        const k = answerKey(u.key, params, args["refine"]);
        const last = this.answers.get(k);
        if (last && last.epoch < this.epoch) {
          // asked again after a signature: the last answer now, marked; the fresh one follows (never a blank view)
          const token = this.refresh(k, u, q, params, unbound);
          this.ui.post({ type: "kton-workbench:refreshing", use: u.key, since: this.signedAt, token });
          result = { ...(last.result as object), refreshing: { since: this.signedAt, token } };
        } else {
          const epoch = this.epoch;
          // the same query (use, params, refine) asked again while it runs: the one ask is shared, never a second
          let p = this.pending.get(k);
          if (!p) {
            p = this.backend.query(q.package, q.name, termsOf(params), u.key);
            this.pending.set(k, p);
            const done = () => this.pending.get(k) === p && this.pending.delete(k);
            p.then(done, done);
          }
          result = await p;
          this.remember(u, result);
          this.answers.set(k, { result, epoch });
          this.lastByUse.set(u.key, { result, params });
          this.ui.changed?.();
          if (unbound.length) result = stampUnfiltered(result, unbound);
        }
      }
      else if (cap === "content@1") {
        if (!u.content?.binary || args["hash"] !== u.content.hash) throw new Refused(`${u.key}: the content differs from the plan's`);
        result = await answerBytes(this.backend, this.loaded, u.content.hash, u.content.type, "content@1");
      } else if (cap === "product@1") {
        // only the product the plan names for this use (review on #174): a use without one is served nothing
        if (!u.product) throw new Refused(`${u.key}: the plan names no product for this use`);
        const hash = typeof args["hash"] === "string" ? args["hash"] : u.product.hash;
        if (hash !== u.product.hash) throw new Refused(`${u.key}: the product differs from the plan's`);
        result = await answerBytes(this.backend, this.loaded, hash, undefined, "product@1");
      } else throw new Refused(`${cap} is an act: it is an intent, not a request`);
      this.ui.post({ type: "kton:data", id, result });
    } catch (e) {
      const why = e instanceof Error ? e.message : String(e);
      // a newer ask of the same block replaced this one in the window's queue (transport/gate.ts): the newer one
      // answers the block, so this is no error to show (matched by name: the error may cross module copies)
      if (e instanceof Error && e.name === "Superseded") return this.ui.log(`composition: ${cap} for ${JSON.stringify(args["use"])} superseded by a newer ask`);
      this.ui.log(`composition: ${cap} refused for ${JSON.stringify(args["use"])}: ${why}`);
      this.ui.post({ type: "kton:refused", what: id, why });
    }
  }

  /**
   * Ask a use's query again in the background (once per key at a time): the fresh answer replaces the last one and goes
   * to the frame as {type: "kton-workbench:answer", use, token, result}; a failure as {…, error, since}: the frame keeps
   * the last answer, marked stale with the reason. Returns the token the frame matches the answer by.
   */
  private refresh(k: string, u: PlanUse, q: { package: string; name: string }, params: Readonly<Record<string, Term>>, unbound: readonly string[]): number {
    const running = this.refreshes.get(k);
    if (running !== undefined) return running;
    const token = ++this.refreshSeq;
    this.refreshes.set(k, token);
    const epoch = this.epoch;
    const since = this.signedAt;
    void this.backend.query(q.package, q.name, termsOf(params)).then(
      (fresh) => {
        this.refreshes.delete(k);
        this.remember(u, fresh);
        this.answers.set(k, { result: fresh, epoch });
        this.lastByUse.set(u.key, { result: fresh, params });
        this.ui.changed?.();
        this.ui.post({ type: "kton-workbench:answer", use: u.key, token, result: unbound.length ? stampUnfiltered(fresh, unbound) : fresh });
      },
      (e: unknown) => {
        this.refreshes.delete(k);
        const why = e instanceof Error ? e.message : String(e);
        this.ui.log(`composition: refresh of ${u.key} after a signature failed: ${why}`);
        this.ui.post({ type: "kton-workbench:answer", use: u.key, token, error: why, since });
      },
    );
    return token;
  }

  private sendSet(): void {
    this.setPending = true;
    // the state is host-held as the frame reported it: vars and layout ({tab, ratio}), never acts (H8: held apart);
    // the last signature of this view rides along, so a frame reloaded after the dialog still confirms it
    this.ui.post({ type: "kton:set", state: { ...this.state, acts: this.acts.forSet() }, plan: this.framePlan(), host: { name: "vscode", provides: VSCODE_PROVIDES, narrow: NARROW_PX }, ...(this.lastSigned ? { signed: this.lastSigned } : {}), ...(this.signedActs.length ? { signedActs: this.signedActs } : {}) });
  }

  /** the guarded uses as last sent to the frame (JSON of [key, params] pairs) */
  private sentGuard = "[]";
  /**
   * The uses that feed an act (sign or record, or a proposed field) while a declared optional param is unbound under the
   * vars the frame last reported: key → the unbound params. Their query is refused and their acts are not offered.
   */
  guarded(): Map<string, string[]> {
    const p = this.plan as unknown as { compositions?: unknown[]; vars?: unknown; uses: readonly PlanUse[] };
    const uses = p.compositions && p.vars ? ((bindVars(p, varValuesOf(p, this.state)) as Plan).uses) : p.uses;
    const out = new Map<string, string[]>();
    for (const u of uses) {
      const unbound = unboundParams(u, u.params ?? {});
      if (unbound.length && feedsAct(u)) out.set(u.key, unbound);
    }
    return out;
  }
  /** The plan as the frame gets it: a guarded use without its acts (it offers none) and its options say why. */
  framePlan(): Plan {
    const g = this.guarded();
    this.sentGuard = JSON.stringify([...g]);
    if (!g.size && !this.rendersActs.size) return this.plan;
    return {
      ...this.plan,
      uses: this.plan.uses.map((u0) => {
        const u = g.has(u0.key) ? ({ ...u0, acts: [], options: { ...((u0 as { options?: object }).options ?? {}), unset: g.get(u0.key) } } as PlanUse) : u0;
        return this.rendersActs.has(u.key) ? ({ ...u, acts: [], blockActs: u.acts ?? [] } as PlanUse) : u;
      }),
    } as Plan;
  }

  /**
   * An act held for the frame. If the frame reloaded while the act ran (the sign dialog took its column), the
   * new frame never asked for this id: it gets the outcome in a fresh set (H8), not as a kton:data it would drop.
   */
  private hold(i: Intent, loadsAtStart: number, entry: Record<string, unknown>): void {
    this.acts.record(i.id, entry);
    if (this.loads !== loadsAtStart) this.sendSet();
  }

  /**
   * The values a use's `target` column took in the answers this host gave that use: the only rows an act may
   * be about instead of the plan's subject (a spot check signs about the element the card showed).
   */
  private readonly served = new Map<string, Set<string>>();
  /** the rows this host served per use, by their target value (a rule may depend on a row's column) */
  private readonly servedRows = new Map<string, Map<string, Array<Record<string, string>>>>();

  private remember(u: PlanUse, result: unknown): void {
    const col = (u as { options?: Record<string, unknown> }).options?.["target"];
    if (typeof col !== "string") return;
    const b = (result as { results?: { bindings?: Array<Record<string, { value?: string }>> } }).results?.bindings ?? [];
    const set = this.served.get(u.key) ?? new Set<string>();
    const rows = this.servedRows.get(u.key) ?? new Map<string, Array<Record<string, string>>>();
    // all rows of this answer for a target (several may share one); a later answer replaces them
    const now = new Map<string, Array<Record<string, string>>>();
    for (const row of b)
      if (typeof row[col]?.value === "string") {
        const t = row[col]?.value as string;
        set.add(t);
        now.set(t, [...(now.get(t) ?? []), Object.fromEntries(Object.entries(row).map(([k, v]) => [k, v?.value ?? ""]))]);
      }
    for (const [t, list] of now) rows.set(t, list);
    this.served.set(u.key, set);
    this.servedRows.set(u.key, rows);
  }

  /**
   * What the act is about: the plan's subject, or a row this host served to the use (option `target`, the act's
   * intent names it as `target`). Anything else is refused.
   */
  private aboutOf(i: Intent, act: PlanAct): string {
    if (i["target"] === undefined) {
      if (act.about === ROW_ONLY) throw new Refused(`${i.from}: this view has no record of its own; its acts are about a row (pick one)`);
      return act.about as string;
    }
    const u = this.plan.uses.find((x) => x.key === i.from);
    const t = i["target"];
    if (typeof t !== "string" || !this.served.get(u?.key ?? "")?.has(t)) throw new Refused(`${i.from}: the act's target is not a row this block was shown`);
    // a claim's node IRI is said as its id
    const claim = /^https:\/\/kton\.dev\/o\/([0-9a-f]{64})$/.exec(t);
    return claim ? `sha256:${claim[1]}` : t;
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
    const g = this.guarded().get(u.key);
    if (g) throw new Refused(`${u.key}: ${cannotShow(g)}; it offers no act`);
    if (!act.about || i.about !== act.about) throw new Refused(`${u.key}: the intent is about another subject than the plan's act`);
    const control = this.loaded.controls.get(`${act.control?.package}/${act.control?.name}`);
    if (!control) {
      // another package's control (ktonpkg v0.9.13): the cockpit serves the view only while its owner is in use at the pin
      const pin = (act.control as { pin?: { at?: string } } | undefined)?.pin;
      throw new Refused(pin ? `${act.control?.package}@${String(pin.at)} is not in use here: its control ${act.control?.name} cannot sign` : `${act.control?.package} has no control ${act.control?.name}`);
    }
    if (control.kind !== KIND_FOR[i.kind]) throw new Refused(`control ${control.name} is a ${control.kind} control, not ${KIND_FOR[i.kind]}`);
    return { act, control };
  }

  private async intent(i: Intent): Promise<void> {
    const held = i.kind === "sign" || i.kind === "record" || i.kind === "run";
    const loadsAtStart = this.loads;
    const refuse = (why: string) => {
      this.ui.log(`composition: ${i.kind} refused: ${why}`);
      this.ui.post({ type: "kton:refused", what: i.id, why });
      if (held) this.hold(i, loadsAtStart, { use: i.from, intent: i.kind, status: "refused", result: { why } });
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
        // a choice the use declares for this act (options.choices "field=value|Label", the button the person pressed) is
        // the package's value, checked against the plan here; any other field a frame proposes is offered to the
        // person, never signed unseen: a value of a field with fixed values shows in the dialog, any other is the
        // default of the shell's input box (Workbench.askOpenFields); a record, which has no dialog, takes none
        const given = Object.entries((i["fields"] ?? {}) as Record<string, unknown>).filter((e): e is [string, string] => typeof e[1] === "string" && !(e[0] in checked.act.fields));
        const declared = declaredChoices(this.plan.uses.find((x) => x.key === i.from), checked.act);
        const chosen = Object.fromEntries(given.filter(([k, v]) => declared.some((d) => d.field === k && d.value === v)));
        const proposed = Object.fromEntries(given.filter(([k]) => !(k in chosen)));
        const actFields = { ...checked.act.fields, ...chosen };
        let made: Signed | undefined;
        const useOf = this.plan.uses.find((x) => x.key === i.from) as { options?: Record<string, unknown>; query?: { package?: string } } | undefined;
        const asks = askEachOf(useOf?.options, c.name);
        const check = checkAgainstOf(useOf?.options, c.name, useOf?.query?.package ?? this.plan.root.package);
        const rr0 = requireReviewedOf(useOf?.options);
        // a rule with `when` applies to a row's act when any row this host served for that target has a value there
        // (several rows for one target: fail closed)
        const servedFor = typeof i["target"] === "string" ? this.servedRows.get(i.from ?? "")?.get(i["target"] as string) : undefined;
        const rr = rr0 && rr0.when && typeof i["target"] === "string" && !servedFor?.some((x) => x[rr0.when as string]) ? undefined : rr0;
        const review = rr ? { ...rr, package: useOf?.query?.package ?? this.plan.root.package } : undefined;
        const r = await this.backend.act(c, this.aboutOf(i, checked.act), actFields, `${checked.act.label}: ${this.plan.root.package}/${this.plan.root.composition}`, proposed, (s) => (made = s), asks.length ? asks : undefined, check, review);
        if (r === "failed") return refuse(c.kind === "decision" ? "not signed (see the sign dialog)" : "not recorded");
        const keyLine = c.kind === "decision" ? "Signed with the person key (test-only)." : "Recorded with the agent key.";
        if (r === "signed") {
          // confirmed at once, before any refresh: in the view (the act's own line, and the frame's banner,
          // also after a reload) and as the notification the sign path shows with the full claim id
          this.epoch += 1;
          this.signedAt = Date.now();
          const what = signedWhat(c, checked.act, i, made);
          this.lastSigned = { what, claim: made?.id ?? "", at: this.signedAt };
          // what this view signed, by use and row (target), host-held for the view's life: a block drops a decided row
          // at once, before the cockpit's answer has the signature (a cold index can take minutes after a say)
          const act = { use: String(i.from ?? ""), ...(typeof i["target"] === "string" ? { target: i["target"] } : {}), claim: made?.id ?? "", at: this.signedAt, fields: { ...(made?.fields ?? proposed) } };
          this.signedActs.push(act);
          this.ui.post({ type: "kton-workbench:signed", ...this.lastSigned, act });
        }
        result = r === "signed" ? { signed: true, key: c.kind === "decision" ? "person" : "agent", message: made ? `${c.kind === "decision" ? "Signed" : "Recorded"}: ${signedWhat(c, checked.act, i, made)} (claim ${shortClaim(made.id)}). ${keyLine}` : keyLine, ...(made ? { claim: made.id } : {}) } : { signed: false, message: "Not signed: the dialog was cancelled." };
        // the use's nudge (option): the shell sends no lane message; it prints the exact line in its output channel
        const use = this.plan.uses.find((x) => x.key === i.from) as { options?: Record<string, unknown> } | undefined;
        const nudge = nudgeFor(use?.options, c.name);
        if (r === "signed" && made && nudge !== undefined) {
          const line = nudgeLine(nudge, made);
          this.ui.log(line ? `nudge (send it; the shell sends no lane message): ${line}` : `nudge: a placeholder of ${i.from}'s nudge has no value; nothing to print`);
          if (line) result = { ...result, nudge: line };
        }
      }
      this.ui.post({ type: "kton:data", id: i.id, result });
      if (held) this.hold(i, loadsAtStart, { use: i.from, intent: i.kind, status: "done", result });
    } catch (e) {
      refuse(e instanceof Error ? e.message : String(e));
    }
  }
}
