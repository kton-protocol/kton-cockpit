// Slots (C2.1): where a package's ui entries appear in the shell, and how an entry becomes something to show.
//
// Every entry that shows a viewer is a composition: the one it names (`composition`), or, for an entry that
// names a viewer (with a query and a control), a one-part composition the shell writes for it from the
// package's own block on that viewer. One renderer, one host, one set of checks either way.
//
// Unknown slot names (a newer package, version skew) are delivered and listed, never rendered and given no
// default place (C1.5).
import { planRounds } from "../../media/kton-web/core/ktonpkg-mirror/plan.js";
import type { Loaded, MirrorPackage, SlotEntry } from "../packages/model.js";
import type { Plan, Term } from "./host.js";
import { termsOf } from "./host.js";
import type { BoundTerm } from "../transport/port.js";

export const SLOTS = ["activity", "home-tile", "status-item", "review-view", "record-view", "run-action", "finding-source", "material-editor"] as const;
/** Slots this shell renders in PR 2. finding-source (C3) and material-editor (after P5) are listed, not shown. */
export const RENDERED = ["activity", "home-tile", "status-item", "review-view", "record-view", "run-action"] as const;

export interface Subject {
  readonly kind: "claim" | "foton" | "scope";
  readonly iri: string;
  readonly id: string;
  readonly claim?: string;
  readonly type?: string;
  readonly path?: string;
  readonly scope?: string;
}

/** Does an entry apply to this subject? `when` = {kind, type?}; no `when` applies to none (a viewer needs one). */
export function applies(e: SlotEntry, s: Subject): boolean {
  return !!e.when && e.when.kind === s.kind && (e.when.type === undefined || e.when.type === s.type);
}

/** Entries the shell renders, and the ones it only lists (unknown or not yet rendered slot names). */
export function partition(entries: readonly SlotEntry[]): { rendered: SlotEntry[]; listed: Array<SlotEntry & { why: string }> } {
  const rendered: SlotEntry[] = [];
  const listed: Array<SlotEntry & { why: string }> = [];
  for (const e of entries) {
    if ((RENDERED as readonly string[]).includes(e.slot)) rendered.push(e);
    else listed.push({ ...e, why: (SLOTS as readonly string[]).includes(e.slot) ? `slot ${e.slot} is not shown by this shell yet` : `unknown slot ${e.slot}: delivered, not rendered` });
  }
  return { rendered, listed };
}

/** The composition an entry shows: {package, composition} plus the packages to plan with (with a written one added). */
export function compositionOf(e: SlotEntry, loaded: Loaded): { ref: { package: string; composition: string }; packages: Record<string, MirrorPackage> } {
  if (e.composition) return { ref: { package: e.package, composition: e.composition }, packages: { ...loaded.packages } };
  const pkg = loaded.packages[e.package];
  if (!pkg) throw new Error(`${e.package} is not loaded`);
  if (!e.viewer) throw new Error(`${e.package}/${e.file} names neither a composition nor a viewer`);
  // the package's block on that viewer: the entry names the viewer, the block carries the pin and the types
  const viewer = e.viewer;
  const block = Object.entries(pkg.blocks).find(([name, b]) => (b.decl["viewer"] as { viewer?: string }).viewer === viewer || name === viewer || pkg.viewers[viewer]?.tree === (b.decl["viewer"] as { tree?: string }).tree);
  if (!block) throw new Error(`${e.package}/${e.file}: the package has no block on viewer ${e.viewer}`);
  const control = e.control ? pkg.controls[e.control] : undefined;
  if (e.control && !control) throw new Error(`${e.package}/${e.file}: the package has no control ${e.control}`);
  const kind = control ? ((control.decl["kind"] ?? "decision") === "record" ? "record" : "sign") : undefined;
  const name = `slot:${e.file}`;
  const use: Record<string, unknown> = { id: "view", use: block[0], ...(e.query ? { query: e.query } : {}), ...(e.inputs && e.query ? { params: e.inputs } : {}) };
  if (control && e.control) use["acts"] = [{ intent: kind, label: e.label, control: e.control }];
  const decl = { composition: name, kind: "view", parts: [{ id: "main", heading: e.label, blocks: [use] }] };
  const text = JSON.stringify(decl);
  const written = { ...pkg, compositions: { ...pkg.compositions, [name]: { decl, hash: `slot:${e.package}/${e.file}`, text } } };
  return { ref: { package: e.package, composition: name }, packages: { ...loaded.packages, [e.package]: written } };
}

/** Plan a composition of the given packages for a subject, with the root's params (terms). */
export async function planComposition(ref: { package: string; composition: string }, packages: Readonly<Record<string, MirrorPackage>>, subject: Subject, params: Readonly<Record<string, Term>>, identityName: string, query: (pkg: string, name: string, terms: Record<string, BoundTerm>) => Promise<unknown>): Promise<Plan> {
  const mirrorSubject = { iri: subject.iri, id: subject.id, ...(subject.claim ? { claim: subject.claim } : {}), kind: subject.kind, ...(subject.scope ? { scope: subject.scope } : {}) };
  return (await planRounds(ref, mirrorSubject, {}, packages, { identity: { name: identityName }, params }, async (n: { query: { package: string; name: string }; params: Record<string, Term> }) => {
    const r = (await query(n.query.package, n.query.name, termsOf(n.params))) as { results?: { bindings?: unknown[] } };
    return r.results?.bindings ?? [];
  })) as unknown as Plan;
}

/** Plan an entry for a subject: compose rounds answered with named queries through the transport. */
export async function planEntry(e: SlotEntry, subject: Subject, loaded: Loaded, identityName: string, query: (pkg: string, name: string, terms: Record<string, BoundTerm>) => Promise<unknown>, builtin?: MirrorPackage): Promise<Plan> {
  const { ref, packages: own } = compositionOf(e, loaded);
  // the workbench's own blocks are there for every composition, as the package "@workbench"
  const packages = builtin ? { ...own, [builtin.name]: builtin } : own;
  const mirrorSubject = { iri: subject.iri, id: subject.id, ...(subject.claim ? { claim: subject.claim } : {}) };
  return (await planRounds(ref, mirrorSubject, {}, packages, { identity: { name: identityName } }, async (n: { query: { package: string; name: string }; params: Record<string, Term> }) => {
    const r = (await query(n.query.package, n.query.name, termsOf(n.params))) as { results?: { bindings?: unknown[] } };
    return r.results?.bindings ?? [];
  })) as unknown as Plan;
}

/** A home-tile or status-item: its query's one number, or why there is none (an error tile, C2.1). */
export function oneNumber(answer: unknown): { n: number } | { why: string } {
  const b = (answer as { results?: { bindings?: Array<Record<string, { value?: string }>> } }).results?.bindings ?? [];
  const vals = b.length === 1 ? Object.values(b[0] ?? {}) : [];
  if (b.length !== 1 || vals.length !== 1) return { why: `the query answers ${b.length} row(s) of ${vals.length || "?"} value(s), not one number` };
  const n = Number(vals[0]?.value);
  return Number.isFinite(n) ? { n } : { why: `${JSON.stringify(vals[0]?.value)} is not a number` };
}
