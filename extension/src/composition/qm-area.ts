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

/**
 * The document register: the area's own package if it brings the queries `documents` and `document`, else the one
 * admitted package that does. Two or more such packages: none is picked (no order decides; kton-review A2), and the
 * area says which ones.
 */
export function registerOf(pkg: string, loaded: Loaded): { register?: string; ambiguous?: string[] } {
  const has = (p: MirrorPackage | undefined) => !!p?.queries["documents"] && !!p.queries["document"];
  if (has(loaded.packages[pkg])) return { register: pkg };
  const all = Object.keys(loaded.packages).filter((n) => has(loaded.packages[n]));
  return all.length === 1 ? { register: all[0] as string } : all.length > 1 ? { ambiguous: all.sort() } : {};
}

/**
 * The record query's terms for a record IRI, to find the record's latest claim before the record opens: each of its
 * $VARs must be declared from subject.iri (the record's IRI). Undefined otherwise: nothing opens.
 */
export function recordTerms(area: AreaDecl, loaded: Loaded, pkg: string, iri: string): Record<string, { type: "iri"; value: string }> | undefined {
  const text = area.record ? loaded.packages[pkg]?.queries[area.record]?.text : undefined;
  if (text === undefined) return undefined;
  const vars = varsOf(text) as string[];
  if (!vars.length || vars.some((v) => area.params?.[v] !== "subject.iri")) return undefined;
  return Object.fromEntries(vars.map((v) => [v, { type: "iri" as const, value: iri }]));
}

/**
 * The column a list row opens its record by: the list query's first projected variable other than ?claim (the
 * record's IRI, whatever the package calls it: ?item, ?record, …). Aggregates `(… AS ?x)` are skipped. Undefined
 * when the query projects none (SELECT *, or ?claim only): the rows then open nothing.
 */
export function listColumn(text: string): string | undefined {
  const body = text.split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
  const m = /\bSELECT\s+(?:DISTINCT\s+|REDUCED\s+)?([\s\S]*?)\bWHERE\b/i.exec(body);
  if (!m) return undefined;
  let proj = m[1] as string;
  // drop parenthesised expressions, innermost first
  for (let prev = ""; prev !== proj; ) [prev, proj] = [proj, proj.replace(/\([^()]*\)/g, " ")];
  return [...proj.matchAll(/[?$]([A-Za-z_]\w*)/g)].map((x) => x[1] as string).find((v) => v !== "claim");
}

/**
 * The rule ids of an area (findings.rules) a finding source checks: those its query text names as a string literal
 * (`BIND("<rule id>" AS ?rule)`, the one finding shape). Comment lines do not count.
 */
export function rulesChecked(text: string, rules: readonly string[]): string[] {
  const body = text.split("\n").filter((l) => !/^\s*#/.test(l)).join("\n");
  return rules.filter((r) => body.includes(`"${r}"`) || body.includes(`'${r}'`));
}

/** The param sources the area may declare: the record (the closed set's subject.*) and the window's identity. */
const SOURCES = ["subject.iri", "subject.id", "subject.claim", "subject.type", "subject.kind", "identity.name"];
/** Why the area's params cannot be bound as declared, or undefined (kton-cockpit N1, kton-review N3). */
export function paramProblem(area: AreaDecl): string | undefined {
  const bad = Object.entries(area.params ?? {}).filter(([, s]) => !SOURCES.includes(s));
  return bad.length ? `params ${bad.map(([k, s]) => `${k} = ${s}`).join(", ")}: the shell binds ${SOURCES.join(", ")} only` : undefined;
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
 * A use of `rows` on a named query of a package, with params for exactly the $VARs its text has, each bound from its
 * declared source:
 * - in a record's view, every declared source (subject.* is the record; identity.name the window's identity);
 * - in the area's view, identity.name; a subject.* source is the record's, which the area view has none of, so that
 *   $VAR stays unbound (an optional param, ktonpkg v0.9.10: the query runs without that filter).
 * Undefined when a $VAR is not declared at all (or, in the area view, only the record's IRI can fill it).
 */
function rowsUse(loaded: Loaded, area: AreaDecl, pkg: string, query: string, id: string, options: Record<string, unknown>, where: "area" | "record"): Record<string, unknown> | undefined {
  const text = loaded.packages[pkg]?.queries[query]?.text;
  if (text === undefined) return undefined;
  const params: Record<string, string> = {};
  const optional: string[] = [];
  for (const v of varsOf(text) as string[]) {
    const src = area.params?.[v];
    if (!src || !SOURCES.includes(src)) return undefined;
    if (where === "record" || src === "identity.name") params[v] = src;
    else if (src === "subject.iri") return undefined;
    else optional.push(v);
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
  const parts: unknown[] = [];
  const params: Record<string, Term> = {};
  const { register, ambiguous } = registerOf(pkg, loaded);
  const noRegister = ambiguous ? `Two or more admitted packages bring a document register (${ambiguous.join(", ")}): none is picked` : "No document register is installed";
  if (paramProblem(a)) parts.push(part("params", "Params", [notice("params", `This area declares ${paramProblem(a)}: queries that use them are not shown.`)]));
  // the governing SOP and its state (a draft is shown as one, S6.15)
  if (a.governing) {
    if (register) {
      used.add(register);
      params["governing"] = lit(a.governing);
      // the register's `document` query takes the key ($key), bound from the composition's param
      const ok = JSON.stringify(varsOf(loaded.packages[register]?.queries["document"]?.text ?? "")) === JSON.stringify(["key"]);
      parts.push(part("governing", `Governing: ${a.governing}`, [ok ? { id: "governing", use: { package: BUILTIN, block: "rows" }, query: { package: register, query: "document" }, params: { key: "governing" }, options: { columns: ["title", "revision", "state", "effectiveDate"], card: "yes", emptyText: `${a.governing} is not in the document register.` }, empty: "show" } : notice("governing", `The register ${register} cannot look up ${a.governing}.`)]));
    } else parts.push(part("governing", `Governing: ${a.governing}`, [notice("governing", `${noRegister}: the governing SOP's state is not known.`)]));
  }
  const records = a.scopes?.records;
  const openBy = e.query ? listColumn(loaded.packages[pkg]?.queries[e.query]?.text ?? "") : undefined;
  const list = e.query ? rowsUse(loaded, a, pkg, e.query, "records", { ...(openBy ? { open: openBy } : {}), emptyText: records ? `Planned, 0 records (scope ${records}).` : "No records." }, "area") : undefined;
  parts.push(part("records", records ? `Records · ${records}` : "Records", [list ?? notice("records", `The list query ${e.query ?? "(none)"} is not one this area can show.`)]));
  if (a.findings) {
    const fp = loaded.packages[a.findings.package];
    const sources = fp ? loaded.entries.filter((x) => x.package === a.findings?.package && x.slot === "finding-source" && x.query) : [];
    if (!fp) parts.push(part("findings", "Findings", [notice("findings", `The findings package ${a.findings.package} is not installed: its findings are not known.`)]));
    else {
      used.add(fp.name);
      const rules = a.findings.rules ?? [];
      const checked = new Set<string>();
      // per finding source: its rows kept to the area's rules, and the state of each area rule it checks (passed,
      // findings (n), or not run when its query fails); a source checking none of them is still shown, unstated
      const blocks = sources
        .map((s, i) => {
          const mine = rulesChecked(fp.queries[s.query as string]?.text ?? "", rules);
          const u = rowsUse(loaded, a, fp.name, s.query as string, `findings-${i + 1}`, { caption: s.label, columns: ["rule", "focus", "message"], rules, ...(mine.length ? { ruleState: mine } : {}), emptyText: `${s.label}: no findings.` }, "area");
          if (u) for (const r of mine) checked.add(r);
          return u;
        })
        .filter((x): x is Record<string, unknown> => !!x);
      // a rule of the area that no shown source checks has not run: said so, never counted as passed
      const notRun = rules.filter((r) => !checked.has(r));
      if (notRun.length) blocks.push(notice("findings-not-run", `Not run (no finding source of ${fp.name} this area can show checks it): ${notRun.join(", ")}.`));
      parts.push(part("findings", `Findings · ${a.findings.package}`, blocks.length ? blocks : [notice("findings", `${a.findings.package} declares no finding source this area can show.`)]));
    }
  }
  if (a.raci) {
    const r = rowsUse(loaded, a, pkg, a.raci, "raci", {}, "area");
    if (r) parts.push(part("raci", "RACI", [r]));
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
    } else parts.push(part("documents", "Documents", [notice("documents", `${noRegister}: ${a.documents.join(", ")}.`)]));
  }
  if (a.approvals) {
    const r = rowsUse(loaded, a, pkg, a.approvals, "approvals", { columns: ["record", "outcome", "signer", "counts"] }, "area");
    if (r) parts.push(part("approvals", "Approvals", [r]));
  }
  const decl = { composition: `area:${pkg}/${e.file}`, kind: "view", subject: { kind: "scope" }, params: Object.keys(params), parts };
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
    // ApprovalsShape (?claim ?record ?outcome ?signer) does not fix which column names the record's claim: a row
    // belongs to this version when either does (S6: ?record; the training package: ?claim, ?approval the confirmation)
    const r = rowsUse(loaded, a, pkg, a.approvals, "approvals", { columns: ["outcome", "signer", "counts"], match: ["record", "claim"], emptyText: "No approvals of this version." }, "record");
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

/** The row an approvals query answers ({?claim ?record ?outcome ?signer}). Any ?tier it binds is never read. */
export interface Approval {
  readonly outcome?: string | undefined;
  readonly signer?: string | undefined;
}

/** The key id in a signer IRI (https://kton.dev/agent/<key id>), or undefined. */
export const signerKeyId = (signer: string | undefined): string | undefined => /^https:\/\/kton\.dev\/agent\/([0-9a-f]{16})$/.exec(signer ?? "")?.[1];

/**
 * Does an approval count (S6.12, C1.3)? Only an outcome `approved` signed by a person: the window's person key (by
 * key id, from the signer IRI) or a key whose tier, as the COCKPIT answers it for that key id (tierOf), is a person
 * tier. The package's query never says who signed as what (ktonpkg ApprovalsShape): a ?tier it binds is ignored.
 * A rejected outcome is shown as rejected (S6.11).
 */
export function approvalCounts(a: Approval, person: { readonly keyId: string; readonly personTiers: readonly string[] }, tierOf: (keyId: string) => string | undefined): { counts: boolean; why: string } {
  if (a.outcome !== "approved") return { counts: false, why: a.outcome ? `no: ${a.outcome}` : "no: no outcome" };
  const kid = signerKeyId(a.signer);
  if (kid && person.keyId && kid === person.keyId) return { counts: true, why: "yes: person key" };
  const tier = kid ? tierOf(kid) : undefined;
  if (tier && person.personTiers.includes(tier)) return { counts: true, why: `yes: tier ${tier}` };
  return { counts: false, why: tier ? `no: tier ${tier} is no person tier` : "no: not signed by a person key" };
}
