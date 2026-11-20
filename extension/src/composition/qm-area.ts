// The generic QM area (PR 3, C3): an `activity` entry's `area` object becomes a view on PR 2's renderer. Two
// built-in compositions, written here from the declaration alone and planned like any package's:
//   - the area (subject: the records scope): records (list → record), findings, RACI, training, documents,
//     approvals, with the governing SOP's state at the top;
//   - a record (subject: the record's latest claim): its fields with the area's actions, its approvals and its
//     history.
// Both use the workbench's own blocks (`rows`, `notice`), carried in the extension under media/workbench and
// tree-checked like any viewer. Nothing here names a package, a query, a field or a template of any area
// (S6.6): every name comes from the declaration, and every area is planned by the same code.
import { varsOf } from "../../media/kton-web/core/ktonpkg-mirror/sparql.js";
import type { Loaded, MirrorPackage, SlotEntry } from "../packages/model.js";
import type { AreaDecl } from "../packages/listing.js";
import type { Term } from "./host.js";

export { BUILTIN, loadBuiltin } from "../packages/vendored.js";
import { BUILTIN } from "../packages/vendored.js";

/** The document register: the area's own package, else the first admitted package that brings `documents` and `document`. */
export function registerOf(pkg: string, loaded: Loaded): string | undefined {
  const has = (p: MirrorPackage | undefined) => !!p?.queries["documents"] && !!p.queries["document"];
  if (has(loaded.packages[pkg])) return pkg;
  return Object.keys(loaded.packages).sort().find((n) => has(loaded.packages[n]));
}

/** The record query's terms for a record IRI: each of its $VARs the area binds from subject.iri, as that IRI. */
export function recordTerms(area: AreaDecl, loaded: Loaded, pkg: string, iri: string): Record<string, { type: "iri"; value: string }> | undefined {
  const text = area.record ? loaded.packages[pkg]?.queries[area.record]?.text : undefined;
  if (text === undefined) return undefined;
  const roles = paramRoles(area);
  const vars = varsOf(text) as string[];
  if (!vars.length || vars.some((v) => !roles.record.includes(v))) return undefined;
  return Object.fromEntries(vars.map((v) => [v, { type: "iri" as const, value: iri }]));
}

/** The record's params (bound to the record's IRI) and the reader's (each a var a RACI row click sets). */
function paramRoles(area: AreaDecl): { record: string[]; picked: string[] } {
  const params = Object.entries(area.params ?? {});
  return { record: params.filter(([, s]) => s === "subject.iri").map(([k]) => k), picked: params.filter(([, s]) => s !== "subject.iri").map(([k]) => k) };
}

interface Built {
  readonly decl: Record<string, unknown>;
  /** the composition's params, as terms */
  readonly params: Record<string, Term>;
  /** every package the composition reads or acts through (for the revision check) */
  readonly packages: string[];
}

const lit = (value: string): Term => ({ type: "literal", value });

/**
 * A use of `rows` on a named query of a package, with params for exactly the $VARs its text has: the record
 * params from the subject (only where `record` is true), the others from their vars. Undefined when a $VAR has no
 * source here (a record query has no place in the area list, for one).
 */
function rowsUse(loaded: Loaded, area: AreaDecl, pkg: string, query: string, id: string, options: Record<string, unknown>, where: "area" | "record"): Record<string, unknown> | undefined {
  const text = loaded.packages[pkg]?.queries[query]?.text;
  if (text === undefined) return undefined;
  const roles = paramRoles(area);
  const params: Record<string, string> = {};
  const optional: string[] = [];
  for (const v of varsOf(text) as string[]) {
    if (roles.record.includes(v)) {
      if (where !== "record") return undefined;
      params[v] = "subject.iri";
    } else if (roles.picked.includes(v)) {
      // a reader's pick filters; until there is one the query runs unfiltered (ktonpkg v0.9.10 optional params)
      params[v] = `var.${v}`;
      optional.push(v);
    } else return undefined;
  }
  return { id, use: { package: BUILTIN, block: "rows" }, query: { package: pkg, query }, ...(Object.keys(params).length ? { params } : {}), ...(optional.length ? { optional } : {}), options, empty: "show" };
}

const notice = (id: string, text: string) => ({ id, use: { package: BUILTIN, block: "notice" }, options: { text } });
const part = (id: string, heading: string, blocks: unknown[]) => ({ id, heading, blocks });

/** The area composition for an `activity` entry with an `area`. */
export function areaComposition(e: SlotEntry & { area: AreaDecl }, loaded: Loaded): Built {
  const a = e.area;
  const pkg = e.package;
  const used = new Set([pkg]);
  const roles = paramRoles(a);
  const parts: unknown[] = [];
  const params: Record<string, Term> = {};
  const register = registerOf(pkg, loaded);
  // the governing SOP and its state (a draft is shown as one, S6.15)
  if (a.governing) {
    if (register) {
      used.add(register);
      params["governing"] = lit(a.governing);
      // the register's `document` query takes the key ($key), bound from the composition's param
      const ok = JSON.stringify(varsOf(loaded.packages[register]?.queries["document"]?.text ?? "")) === JSON.stringify(["key"]);
      parts.push(part("governing", `Governing: ${a.governing}`, [ok ? { id: "governing", use: { package: BUILTIN, block: "rows" }, query: { package: register, query: "document" }, params: { key: "governing" }, options: { columns: ["title", "revision", "state", "effectiveDate"], card: "yes", emptyText: `${a.governing} is not in the document register.` }, empty: "show" } : notice("governing", `The register ${register} cannot look up ${a.governing}.`)]));
    } else parts.push(part("governing", `Governing: ${a.governing}`, [notice("governing", "No document register is installed: the governing SOP's state is not known.")]));
  }
  const records = a.scopes?.records;
  const list = e.query ? rowsUse(loaded, a, pkg, e.query, "records", { open: "item", emptyText: records ? `Planned, 0 records (scope ${records}).` : "No records." }, "area") : undefined;
  parts.push(part("records", records ? `Records · ${records}` : "Records", [list ?? notice("records", `The list query ${e.query ?? "(none)"} is not one this area can show.`)]));
  if (a.findings) {
    const fp = loaded.packages[a.findings.package];
    const sources = fp ? loaded.entries.filter((x) => x.package === a.findings?.package && x.slot === "finding-source" && x.query) : [];
    if (!fp) parts.push(part("findings", "Findings", [notice("findings", `The findings package ${a.findings.package} is not installed: its findings are not known.`)]));
    else {
      used.add(fp.name);
      const blocks = sources.map((s, i) => rowsUse(loaded, a, fp.name, s.query as string, `findings-${i + 1}`, { columns: ["rule", "focus", "message"], rules: a.findings?.rules ?? [], emptyText: `${s.label}: no findings.` }, "area")).filter((x): x is Record<string, unknown> => !!x);
      parts.push(part("findings", `Findings · ${a.findings.package}`, blocks.length ? blocks : [notice("findings", `${a.findings.package} declares no finding source this area can show.`)]));
    }
  }
  if (a.raci) {
    const r = rowsUse(loaded, a, pkg, a.raci, "raci", roles.picked.length ? { pick: roles.picked[0] as string } : {}, "area");
    if (r) parts.push(part("raci", "RACI", [roles.picked.length ? { ...r, emits: { select: roles.picked[0] } } : r]));
  }
  if (a.training) {
    const t = rowsUse(loaded, a, pkg, a.training, "training", { emptyText: "No training obligations." }, "area");
    if (t) parts.push(part("training", "Training", [t]));
  }
  if (a.documents?.length) {
    if (register) {
      used.add(register);
      const d = rowsUse(loaded, a, register, "documents", "documents", { keys: [...a.documents], open: "document", columns: ["key", "title", "revision", "state", "effectiveDate"] }, "area");
      parts.push(part("documents", "Documents", [d ?? notice("documents", `The register ${register} cannot list ${a.documents.join(", ")}.`)]));
    } else parts.push(part("documents", "Documents", [notice("documents", `No document register is installed: ${a.documents.join(", ")}.`)]));
  }
  if (a.approvals) {
    const r = rowsUse(loaded, a, pkg, a.approvals, "approvals", { columns: ["record", "outcome", "signer", "counts"] }, "area");
    if (r) parts.push(part("approvals", "Approvals", [r]));
  }
  const vars = Object.fromEntries(roles.picked.map((k) => [k, { type: "iri" }]));
  const decl = { composition: `area:${pkg}/${e.file}`, kind: "view", subject: { kind: "scope" }, params: Object.keys(params), ...(Object.keys(vars).length ? { vars } : {}), parts };
  return { decl, params, packages: [...used] };
}

/** The record composition: one record of an area (subject: its latest claim), with the area's actions. */
export function recordComposition(e: SlotEntry & { area: AreaDecl }, loaded: Loaded): Built {
  const a = e.area;
  const pkg = e.package;
  const parts: unknown[] = [];
  const acts = (a.actions ?? []).flatMap((name) => {
    const c = loaded.controls.get(`${pkg}/${name}`);
    return c ? [{ intent: c.kind === "record" ? "record" : "sign", label: String(loaded.packages[pkg]?.controls[name]?.decl["title"] ?? name), control: { package: pkg, name } }] : [];
  });
  const rec = a.record ? rowsUse(loaded, a, pkg, a.record, "record", { card: "yes", emptyText: "This record has no current version." }, "record") : undefined;
  parts.push(part("record", "Record", [rec ? { ...rec, ...(acts.length ? { acts } : {}) } : notice("record", `The record query ${a.record ?? "(none)"} is not one this area can show.`)]));
  if (a.approvals) {
    const r = rowsUse(loaded, a, pkg, a.approvals, "approvals", { columns: ["outcome", "signer", "counts"], match: "record", emptyText: "No approvals of this version." }, "record");
    if (r) parts.push(part("approvals", "Approvals", [r]));
  }
  if (a.history) {
    const r = rowsUse(loaded, a, pkg, a.history, "history", { columns: ["claim", "replaces", "replacedBy", "when"] }, "record");
    if (r) parts.push(part("history", "History", [r]));
  }
  const decl = { composition: `area-record:${pkg}/${e.file}`, kind: "view", subject: { kind: "claim" }, params: [], parts };
  return { decl, params: {}, packages: [pkg] };
}

/** The packages to plan with: the admitted ones, the workbench's own, and the written composition in it. */
export function withBuiltin(loaded: Loaded, builtin: MirrorPackage, built: Built): Record<string, MirrorPackage> {
  const name = String(built.decl["composition"]);
  const text = JSON.stringify(built.decl);
  return { ...loaded.packages, [BUILTIN]: { ...builtin, compositions: { [name]: { decl: built.decl, hash: `builtin:${name}`, text } } } };
}

/** The row an approvals query answers ({?claim ?record ?outcome ?signer}) and whether it counts. */
export interface Approval {
  readonly outcome?: string | undefined;
  readonly signer?: string | undefined;
  readonly tier?: string | undefined;
}

/**
 * Does an approval count (S6.12, C1.3)? Only an outcome `approved` signed by a person: the window's person key
 * (by key id, from the signer IRI) or a key in a person tier. The query carries no tier of its own; an agent
 * key's approval is a record, never an approval. A rejected outcome is shown as rejected (S6.11).
 */
export function approvalCounts(a: Approval, person: { readonly keyId: string; readonly personTiers: readonly string[] }): { counts: boolean; why: string } {
  if (a.outcome !== "approved") return { counts: false, why: a.outcome ? `no: ${a.outcome}` : "no: no outcome" };
  const kid = /^https:\/\/kton\.dev\/agent\/([0-9a-f]{16})$/.exec(a.signer ?? "")?.[1];
  if (kid && person.keyId && kid === person.keyId) return { counts: true, why: "yes: person key" };
  if (a.tier && person.personTiers.includes(a.tier)) return { counts: true, why: `yes: tier ${a.tier}` };
  return { counts: false, why: "no: not signed by a person key" };
}
