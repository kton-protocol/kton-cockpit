// plan(composition, subject, inputs, results, packages) → plan | {needs} (ktonpkg answer, rounds 2 and 3).
// Pure and deterministic: a prototype of what ktonpkg will do in Go (and may compile to wasm).
// Round 5 (VERTICLE-HOSTS §15, ktonpkg "Round 5"/"5b", kton-web a1e97e9): a composition declares typed `vars`;
// uses read them as `var.<name>` (params, options) and write them through `emits` (block events). Vars are
// plan input: plan(…, {state}) binds the current values (bindVars); a use's `ask` key holds the bound terms,
// so a renderer re-asks only uses whose key changed. Types match exactly; the one coercion is hash → iri.
// Round 3: a bound input is a manifest ({schema, class, files}); plan records the manifest's hash and takes
// the subject's class from it; a composition declares `class` and `inputs`; a block pins its viewer as
// {viewer, at, tree} and plan checks the tree against the loaded viewer folder.
//
// - parts come from the composition, and from bound inputs: {"from": "chapters"} expands the document's
//   chapters (an input bundle, hashed into the plan); unnamed parts (level jumps) become sections without
//   a heading;
// - expands nested compositions, `every` (repeat over a query's rows) and `eachPart` (own composition only);
// - binds params only from the closed set (subject.id, subject.iri, subject.claim, part.id, part.iri,
//   row.<var>, composition.id, identity.name, a declared parent param) as RDF terms;
// - a use's requirements = its pinned viewer's `requires` ∪ derived (query@1 with a query, content@1 with
//   binary content); `refine` declares which of the query's params a reader may vary, with values or a type;
// - refuses cycles, depth > 8, unbound $VARs, sources outside the closed set, unknown references, viewer
//   pins that do not match, undeclared options, bad refine declarations and unknown capabilities.
//
// `every` needs rows: when `results` lacks them, plan returns {needs: [{key, query, params}]}; the host
// answers them (key = queryKey(hash, params), the key fill uses too) and calls plan again.

import { queryKey, termText, varsOf } from "./sparql.js";

export const DATA_CAPS = ["query@1", "product@1", "graph@1", "content@1"];
export const ACT_CAPS = ["open@1", "run@1", "record@1", "sign@1", "communication@1"];
export const CAPABILITIES = [...DATA_CAPS, ...ACT_CAPS];
export const MAX_DEPTH = 8;
export const KINDS = ["view", "report", "deck", "page"];
const INTENTS = ["open", "run", "record", "sign"];
const REFINE_TYPES = ["iri", "literal", "integer"];
export const VAR_BASES = ["iri", "literal", "integer", "hash"];
const RESERVED_VARS = ["subject", "part", "row", "identity", "composition"];
/** Events a composition may wire (ktonpkg 5b): state changes and these intents. Acts never. */
export const WIRABLE_INTENTS = ["open", "reveal", "navigate", "select", "focus"];
export const NEVER_WIRABLE = ["sign", "record", "run", "post"];
/** kton v0.4.0 content IRI of a hash: the one coercion (hash → iri). */
export const contentIri = (hash) => `https://kton.dev/o/${String(hash).replace(/^sha256:/, "").toLowerCase()}`;

/** A type as a comparable key: "iri" | "literal" | "integer" | "hash" | "set<literal>"; null if not a type. */
export function typeKey(t) {
  if (typeof t === "string" && VAR_BASES.includes(t)) return t;
  if (t && typeof t === "object" && !Array.isArray(t) && VAR_BASES.includes(t.set) && Object.keys(t).length === 1) return `set<${t.set}>`;
  return null;
}
const IRI_RE = /^[a-z][a-z0-9+.-]*:[^\s<>"{}|\\^`]*$/i;
function baseOk(b, x) {
  if (b === "iri") return typeof x === "string" && IRI_RE.test(x);
  if (b === "literal") return typeof x === "string";
  if (b === "integer") return Number.isInteger(x);
  if (b === "hash") return typeof x === "string" && /^sha256:[0-9a-f]{64}$/.test(x);
  return false;
}
/** Does a value have this type (exactly; no coercion)? */
export function checkValue(t, v) {
  const k = typeKey(t);
  if (!k) return false;
  if (k.startsWith("set<")) return Array.isArray(v) && v.every((x) => baseOk(t.set, x)) && new Set(v).size === v.length;
  return baseOk(k, v);
}

export class PlanError extends Error {
  constructor(code, message) {
    super(message);
    this.name = "PlanError";
    this.code = code;
  }
}
const fail = (code, msg) => {
  throw new PlanError(code, msg);
};

/** Resolve "name" (same package) or {package, <kind>: name}; queries may also be {input, query}. */
function resolve(st, fromPkg, ref, kind) {
  const field = { compositions: "composition", blocks: "block", queries: "query" }[kind];
  let src, name;
  if (typeof ref === "string") [src, name] = [st.packages[fromPkg], ref];
  else if (ref && typeof ref === "object" && typeof ref[field] === "string" && typeof ref.package === "string") {
    src = st.packages[ref.package];
    name = ref[field];
    if (!src) fail("unknown-ref", `package ${ref.package} is not admitted (${field} ${name})`);
  } else if (ref && typeof ref === "object" && kind === "queries" && typeof ref.input === "string" && typeof ref.query === "string") {
    if (!(st.declaredInputs ?? []).includes(ref.input)) fail("unbound-input", `query ${ref.query} names input ${ref.input}, which the composition does not declare`);
    src = st.inputs[ref.input];
    name = ref.query;
    if (!src) fail("unknown-ref", `input ${ref.input} is not bound (query ${name})`);
  } else fail("reference", `a ${field} reference is "name" or {"package": …, "${field}": …}: ${JSON.stringify(ref)}`);
  const entry = src[kind][name];
  if (!entry) fail("unknown-ref", `${src.name} has no ${field} ${name}`);
  return { package: src.name, name, entry };
}

export function plan(root, subject, inputs = {}, results = {}, packages, opts = {}) {
  if (!subject || typeof subject.iri !== "string" || !subject.iri) fail("subject", "the subject needs an iri");
  if (!packages) fail("packages", "plan needs the admitted packages");
  // the subject carries the class of its input manifest (F18)
  const cls = inputs.chapters?.manifest?.class;
  if (cls && subject.class && subject.class !== cls) fail("subject", `the subject says class ${subject.class}, its chapters manifest ${cls}`);
  if (cls) subject = { ...subject, class: cls };
  const st = { packages, inputs, results, subject, identity: opts.identity, compositions: [], sections: [], uses: [], needs: [], vars: {}, publishes: [] };
  expandComposition(st, root.package, { package: root.package, composition: root.composition }, opts.params ?? {}, { depth: 1, chain: [], parentSection: null, level: 1, subject });
  if (st.needs.length) return { needs: st.needs };
  const top = st.compositions[0];
  const out = {
    plan: "plan-v1",
    root: { package: top.package, composition: top.name, hash: top.hash, kind: top.kind },
    subject,
    inputs: Object.fromEntries(Object.entries(inputs).map(([k, b]) => [k, { name: b.name, manifest: b.manifest?.hash ?? null, class: b.manifest?.class ?? null }])),
    compositions: st.compositions,
    sections: st.sections,
    uses: st.uses,
    vars: st.vars,
    publishes: st.publishes,
  };
  return bindVars(out, varValuesOf(out, opts.state));
}

/** The var values a (URL) state holds: state.vars for the root, state.parts.<…>.vars for nested compositions. */
export function varValuesOf(p, state) {
  const out = {};
  for (const compKey of Object.keys(p.vars ?? {})) {
    let node = state ?? {};
    for (const id of compKey ? compKey.split("/") : []) node = node?.parts?.[id];
    if (node?.vars) out[compKey] = node.vars;
  }
  return out;
}

/**
 * Bind var values into a plan (pure): values {compKey: {name: value}} over the declared defaults. An invalid
 * value (an untrusted URL) falls back to the default and is listed in varErrors. Per use: params and options
 * resolved; a use that reads an unset var is `waiting`; `ask` = queryKey of the bound query (null if waiting).
 * In a report/deck nothing is interactive: values are ignored, defaults hold.
 */
export function bindVars(p, values = {}) {
  const kinds = Object.fromEntries(p.compositions.map((c) => [c.key, c.kind]));
  const current = {};
  const varErrors = [];
  for (const [ck, decls] of Object.entries(p.vars ?? {})) {
    current[ck] = {};
    const interactive = !["report", "deck"].includes(kinds[ck]);
    for (const [name, d] of Object.entries(decls)) {
      const v = interactive ? values[ck]?.[name] : undefined;
      if (v !== undefined && v !== null && checkValue(d.type, v)) current[ck][name] = v;
      else {
        if (v !== undefined && v !== null) varErrors.push(`${ck || "(root)"}.${name}`);
        if (d.default !== undefined) current[ck][name] = d.default;
      }
    }
  }
  const resolve1 = (ph, waiting) => {
    const v = current[ph.comp]?.[ph.var];
    if (v === undefined) {
      waiting.add(`${ph.comp ? ph.comp + ":" : ""}${ph.var}`);
      return undefined;
    }
    if (ph.as === "option") return v;
    const t = typeKey(ph.vtype);
    const val = t === "hash" ? contentIri(v) : String(v);
    if (ph.suffix !== undefined) return { type: "uri", value: val + ph.suffix };
    if (t === "iri" || t === "hash") return { type: "uri", value: val };
    if (t === "integer") return { type: "literal", value: val, datatype: "http://www.w3.org/2001/XMLSchema#integer" };
    return { type: "literal", value: val };
  };
  const uses = p.uses.map((u) => {
    if (!u.bind) return u;
    const waiting = new Set();
    const params = { ...(u.params ?? {}) };
    for (const [k, ph] of Object.entries(u.bind.params ?? {})) params[k] = resolve1(ph, waiting);
    const options = { ...u.options };
    for (const [k, ph] of Object.entries(u.bind.options ?? {})) {
      const v = resolve1(ph, waiting);
      if (v === undefined) delete options[k];
      else options[k] = v;
    }
    const w = [...waiting];
    const out = { ...u, params, options, waiting: w };
    if (u.query) out.ask = Object.values(params).some((x) => x === undefined) ? null : queryKey(u.query.hash, params);
    return out;
  });
  return { ...p, uses, varValues: current, varErrors };
}

/** plan, answer its needs through runQuery(need) → bindings[], plan again; at most MAX_DEPTH + 1 rounds. */
export async function planRounds(root, subject, inputs, packages, opts, runQuery) {
  const results = {};
  for (let round = 0; round <= MAX_DEPTH; round++) {
    const p = plan(root, subject, inputs, results, packages, opts);
    if (!p.needs) return p;
    for (const n of p.needs) results[n.key] = await runQuery(n);
  }
  fail("depth", `every did not settle within ${MAX_DEPTH + 1} rounds`);
}

function expandComposition(st, fromPkg, ref, bound, ctx) {
  const c = resolve(st, fromPkg, ref, "compositions");
  const id = `${c.package}/${c.name}`;
  if (ctx.chain.includes(id)) fail("cycle", `composition cycle: ${[...ctx.chain, id].join(" → ")}`);
  if (ctx.depth > MAX_DEPTH) fail("depth", `compositions nest deeper than ${MAX_DEPTH}: ${[...ctx.chain, id].join(" → ")}`);
  const decl = c.entry.decl;
  if (decl.composition !== c.name) fail("declaration", `${id}: the file declares composition ${decl.composition}`);
  if (!KINDS.includes(decl.kind)) fail("declaration", `${id}: kind ${decl.kind} is not one of ${KINDS.join("|")}`);
  if (decl.class !== undefined && ctx.subject.fromVar) fail("subject-from-var", `${id} needs subject.class (a class check), but its subject comes from var.${ctx.subject.fromVar}: only subject.iri is known`);
  if (decl.class !== undefined && decl.class !== ctx.subject.class) fail("class", `${id} is for class ${decl.class}; the subject is ${ctx.subject.class ?? "of no class"}`);
  const declared = decl.params ?? [];
  for (const k of Object.keys(bound)) if (!declared.includes(k)) fail("param-source", `${id} declares no param ${k}`);
  for (const k of declared) if (!(k in bound)) fail("unbound-var", `${id}: param ${k} is not bound by its parent`);
  for (const k of decl.inputs ?? []) if (!st.inputs[k]?.manifest) fail("unbound-input", `${id} needs the input ${k} (a manifest)`);
  if ((decl.parts ?? []).filter((p) => p.from === "chapters").length > 1) fail("declaration", `${id}: at most one {"from": "chapters"} part`);
  const parts = expandFrom(st, decl.parts ?? [], id, decl);
  const ids = new Set();
  const walk = (ps) => {
    for (const p of ps) {
      if (typeof p.id !== "string" || !p.id) fail("declaration", `${id}: a part has no id`);
      if (ids.has(p.id)) fail("duplicate-part", `${id}: part id ${p.id} is not unique in its composition`);
      ids.add(p.id);
      walk(p.parts ?? []);
    }
  };
  walk(parts);
  const compKey = ctx.parentSection ?? "";
  // vars: named, typed, defaults type-checked; no shadowing of the closed set's prefixes
  const vars = {};
  for (const [name, d] of Object.entries(decl.vars ?? {})) {
    if (!/^[A-Za-z_]\w*$/.test(name) || RESERVED_VARS.includes(name)) fail("var", `${id}: var name ${name} is not allowed (shadows ${RESERVED_VARS.join("/")} or not a name)`);
    if (!typeKey(d?.type)) fail("var-type", `${id}: var ${name} has no valid type (iri, literal, integer, hash, or {"set": one of them})`);
    if (d.default !== undefined && !checkValue(d.type, d.default)) fail("var-type", `${id}: the default of var ${name} is not a ${typeKey(d.type)}`);
    vars[name] = { type: d.type, ...(d.default !== undefined ? { default: d.default } : {}) };
  }
  st.vars[compKey] = vars;
  // a nested composition publishes its own vars as events: emits {event: own var}
  for (const [ev, v] of Object.entries(decl.emits ?? {})) if (!vars[v]) fail("emits", `${id}: emits ${ev} names var ${v}, which it does not declare`);
  st.compositions.push({ key: compKey, package: c.package, name: c.name, hash: c.entry.hash, kind: decl.kind, parent: ctx.parentSection === null ? null : ctx.parentComp ?? "", params: bound });
  const cctx = { pkg: c.package, id, decl, params: bound, compKey, hash: c.entry.hash, kind: decl.kind, chain: [...ctx.chain, id], depth: ctx.depth, subject: ctx.subject, vars };
  const outer = st.declaredInputs;
  st.declaredInputs = decl.inputs ?? [];
  for (const part of parts) expandPart(st, part, cctx, ctx.level, ctx.parentSection, null);
  st.declaredInputs = outer;
}

/** {"from": "<input>"} → that input's parts, marked so their files and queries resolve in the input. */
function expandFrom(st, parts, id, decl) {
  return parts.flatMap((p) => {
    if (p.from === undefined) return [p];
    if (!(decl.inputs ?? []).includes(p.from)) fail("unbound-input", `${id}: parts from ${p.from}, which is not a declared input`);
    const mark = (q) => ({ ...q, input: p.from, parts: (q.parts ?? []).map(mark) });
    return st.inputs[p.from].chapters.decl.parts.map(mark);
  });
}

function scopeFor(cctx, part, row) {
  const s = cctx.subject.iri;
  return { part: { id: part.id, iri: typeof s === "string" ? `${s}/${part.id}` : { ...s, suffix: `/${part.id}` } }, row };
}

function expandPart(st, part, cctx, level, parentSection, row) {
  const key = parentSection ? `${parentSection}/${part.id}` : part.id;
  if (part.every !== undefined) {
    const rows = everyRows(st, part.every, cctx, scopeFor(cctx, part, row));
    if (!rows) return;
    rows.forEach((r, i) => {
      const heading = part.every.heading ? termText(bindSource(st, part.every.heading, scopeFor(cctx, part, r), cctx)) : part.heading;
      expandPart(st, { ...part, every: undefined, id: `${part.id}.${i + 1}`, heading }, cctx, level, parentSection, r);
    });
    return;
  }
  if (part.block !== undefined) fail("declaration", `${cctx.id}/${part.id}: a part holds "blocks" (uses) or a "composition", not "block"`);
  const scope = scopeFor(cctx, part, row);
  const section = { key, id: part.id, heading: part.unnamed ? "" : part.heading ?? "", level, parent: parentSection, composition: cctx.compKey, rules: part.rules ?? [], uses: [] };
  if (part.unnamed) section.unnamed = true;
  if (part.input) section.input = part.input;
  st.sections.push(section);
  (part.blocks ?? []).forEach((u, i) => expandUse(st, u, cctx, section, scope, String(i + 1), "part", part.input));
  if (!part.unnamed) (cctx.decl.eachPart ?? []).forEach((u, i) => expandUse(st, u, cctx, section, scope, `each${i + 1}`, "eachPart", null));
  if (part.composition !== undefined) {
    const bound = {};
    let subject = cctx.subject;
    for (const [k, src] of Object.entries(part.params ?? {})) {
      const t = bindSource(st, src, scope, cctx);
      if (k !== "subject") bound[k] = t;
      else {
        // a subject bound from a var gives only subject.iri (ktonpkg round 5, 5): id/claim/class stay unbound
        if (t.type !== "var" || !["iri", "hash"].includes(typeKey(t.vtype))) fail("subject-from-var", `${cctx.id}/${part.id}: params.subject binds an iri or hash var, not ${src}`);
        subject = { iri: t, fromVar: t.var };
      }
    }
    const before = st.compositions.length;
    expandComposition(st, cctx.pkg, part.composition, bound, { depth: cctx.depth + 1, chain: cctx.chain, parentSection: key, parentComp: cctx.compKey, level: level + 1, subject });
    // the parent's nested use wires the child's declared events to its own vars (types equal)
    const child = st.compositions[before];
    const childDecl = resolve(st, cctx.pkg, part.composition, "compositions").entry.decl;
    for (const [ev, pv] of Object.entries(part.emits ?? {})) {
      const cv = childDecl.emits?.[ev];
      if (!cv) fail("emits", `${cctx.id}/${part.id}: ${child.package}/${child.name} declares no event ${ev}`);
      if (!cctx.vars[pv]) fail("emits", `${cctx.id}/${part.id}: emits ${ev} → var ${pv}, which ${cctx.id} does not declare`);
      if (["report", "deck"].includes(cctx.kind)) continue; // inert
      const ct = typeKey(st.vars[child.key][cv].type);
      if (ct !== typeKey(cctx.vars[pv].type)) fail("var-type", `${cctx.id}/${part.id}: event ${ev} carries ${ct}, var ${pv} is ${typeKey(cctx.vars[pv].type)}`);
      st.publishes.push({ from: { comp: child.key, var: cv }, to: { comp: cctx.compKey, var: pv }, event: ev });
    }
  } else if (part.params !== undefined || part.emits !== undefined) fail("declaration", `${cctx.id}/${part.id}: params and emits on a part wire a nested composition; this part has none`);
  for (const child of part.parts ?? []) expandPart(st, child, cctx, level + 1, key, row);
}

/** The rows an `every` repeats over, or null after recording the query as a need. */
function everyRows(st, every, cctx, scope) {
  if (!every || typeof every !== "object" || every.as !== "row" || every.query === undefined) {
    fail("declaration", `${cctx.id}: every is {"query": …, "as": "row"} (repeat over rows); appending to each part is "eachPart"`);
  }
  const q = boundQuery(st, every.query, every.params, undefined, cctx, scope);
  if (Object.values(q.params).some((t) => t.type === "var")) fail("var", `${cctx.id}: an every's query cannot read a var (parts are fixed by plan)`);
  const key = queryKey(q.query.hash, q.params);
  if (st.results[key] === undefined) {
    if (!st.needs.some((n) => n.key === key)) st.needs.push({ key, query: q.query, params: q.params });
    return null;
  }
  return st.results[key];
}

function boundQuery(st, ref, params, refine, cctx, scope) {
  const q = resolve(st, cctx.pkg, ref, "queries");
  const bound = {};
  for (const [k, src] of Object.entries(params ?? {})) {
    bound[k] = bindSource(st, src, scope, cctx);
    if (bound[k].type === "var" && typeKey(bound[k].vtype).startsWith("set<")) fail("var-type", `${q.package}/${q.name}: $${k} is bound from var.${bound[k].var}, a set; a set never binds into a query param (ktonpkg round 5, 3)`);
  }
  const vars = varsOf(q.entry.text);
  const refined = {};
  for (const [k, spec] of Object.entries(refine ?? {})) {
    if (!vars.includes(k)) fail("refine", `${q.package}/${q.name} has no $${k} to refine`);
    if (k in bound) fail("refine", `$${k} is bound by params and refined; it is one or the other`);
    if (Array.isArray(spec?.values)) {
      if (!spec.values.length || !spec.values.every((v) => typeof v === "string")) fail("refine", `refine ${k}: values are a non-empty list of strings`);
      if (spec.default !== undefined && !spec.values.includes(spec.default)) fail("refine", `refine ${k}: the default is not one of the values`);
      refined[k] = { values: spec.values };
      bound[k] = { type: "literal", value: spec.default ?? spec.values[0] };
    } else if (REFINE_TYPES.includes(spec?.type)) {
      if (spec.default === undefined) fail("refine", `refine ${k}: a typed refine needs a default`);
      refined[k] = { type: spec.type };
      bound[k] = typedTerm(spec.type, spec.default, `refine ${k}`);
    } else fail("refine", `refine ${k} is {"values": [...]} or {"type": ${REFINE_TYPES.join("|")}}`);
  }
  const unbound = vars.filter((v) => !(v in bound));
  if (unbound.length) fail("unbound-var", `${q.package}/${q.name} uses $${unbound.join(", $")}, which ${unbound.length > 1 ? "are" : "is"} not bound`);
  const unused = Object.keys(bound).filter((k) => !vars.includes(k));
  if (unused.length) fail("unused-param", `${q.package}/${q.name} has no $${unused.join(", $")}`);
  return { query: { package: q.package, name: q.name, hash: q.entry.hash }, params: bound, ...(Object.keys(refined).length ? { refine: refined } : {}) };
}

/** A typed term from a reader's value: the only way a refine value reaches a query. */
export function typedTerm(type, value, what = "value") {
  if (typeof value !== "string") fail("refine", `${what}: a value is a string`);
  if (type === "iri") {
    if (!/^[a-z][a-z0-9+.-]*:[^\s<>"{}|\\^`]*$/i.test(value)) fail("refine", `${what}: not an IRI`);
    return { type: "uri", value };
  }
  if (type === "integer") {
    if (!/^-?\d+$/.test(value)) fail("refine", `${what}: not an integer`);
    return { type: "literal", value, datatype: "http://www.w3.org/2001/XMLSchema#integer" };
  }
  return { type: "literal", value };
}

function expandUse(st, u, cctx, section, scope, idx, origin, input) {
  if (u.block !== undefined && u.use === undefined) fail("declaration", `${section.key}: a use names its block with "use"; "block" is a declaration's name`);
  const id = u.id ?? idx;
  if (u.every !== undefined) {
    const rows = everyRows(st, u.every, cctx, scope);
    if (!rows) return;
    rows.forEach((r, i) => expandUse(st, { ...u, every: undefined, id: `${id}.${i + 1}` }, cctx, section, { ...scope, row: r }, idx, origin, input));
    return;
  }
  const b = resolve(st, cctx.pkg, u.use, "blocks");
  const decl = b.entry.decl;
  if (decl.block !== b.name) fail("declaration", `${b.package}/${b.name}: the file declares block ${decl.block}`);
  const pin = decl.viewer;
  if (!pin || typeof pin.viewer !== "string" || typeof pin.at !== "string" || !/^sha256:[0-9a-f]{64}$/.test(pin.tree ?? "")) fail("viewer", `block ${b.name}: the viewer pin is {viewer, at, tree}`);
  const viewer = st.packages[b.package].viewers[pin.viewer];
  if (!viewer) fail("viewer", `block ${b.name} pins viewer ${pin.viewer}, which ${b.package} does not have`);
  if (viewer.tree !== pin.tree) fail("viewer", `block ${b.name} pins viewer ${pin.viewer} at tree ${pin.tree}, the loaded folder is ${viewer.tree}`);
  // typed options (ktonpkg round 5, 2): static values type-checked; var.<name> must have the option's type and
  // be an accepted state key of the viewer (kton-web: the way in is kton:set {state: {key: value}})
  const optBind = {};
  const options = {};
  for (const [k, v] of Object.entries(u.options ?? {})) {
    const ot = decl.options?.[k];
    if (ot === undefined) fail("block-option", `${section.key}: block ${b.name} declares no option ${k}`);
    if (typeof v === "string" && /^var\./.test(v)) {
      const ph = bindSource(st, v, scope, cctx);
      if (typeKey(ph.vtype) !== typeKey(ot)) fail("var-type", `${section.key}: option ${k} is ${typeKey(ot)}, ${v} is ${typeKey(ph.vtype)}`);
      const acc = viewer.decl.accepts?.[k];
      if (!acc || typeKey(acc) !== typeKey(ot)) fail("var", `${section.key}: viewer ${pin.viewer} does not accept state key ${k} (${typeKey(ot)}), so ${v} cannot reach it`);
      optBind[k] = { ...ph, as: "option" };
    } else {
      if (!checkValue(ot, v)) fail("block-option", `${section.key}: option ${k} of block ${b.name} is ${typeKey(ot)}; ${JSON.stringify(v)} is not`);
      options[k] = v;
    }
  }
  const requires = new Set([...(viewer.decl.requires ?? []), ...(decl.requires ?? [])]);
  const key = `${section.key}#${id}`;
  if (st.uses.some((x) => x.key === key)) fail("duplicate-use", `use id ${id} appears twice in ${section.key}`);
  const entry = { key, id, section: section.key, origin, block: `${b.package}/${b.name}`, viewer: pin.viewer, module: viewer.module, tree: viewer.tree, options, empty: u.empty ?? (cctx.kind === "report" ? "omit" : "show") };
  if (!["omit", "show"].includes(entry.empty)) fail("declaration", `${key}: empty is omit|show`);
  if (u.scope !== undefined) {
    if (typeof u.scope === "string") entry.scope = u.scope;
    else if (u.scope?.from && (st.declaredInputs ?? []).includes(u.scope.from) && st.inputs[u.scope.from]?.chapters?.decl.scopes?.[u.scope.slot]) entry.scope = st.inputs[u.scope.from].chapters.decl.scopes[u.scope.slot];
    else fail("unknown-ref", `${key}: no scope ${JSON.stringify(u.scope)} in the inputs`);
  }
  if (u.caption !== undefined) entry.caption = u.caption;
  if (u.file !== undefined) {
    const holder = input ? st.inputs[input] : st.packages[cctx.pkg];
    const c = holder.content[u.file];
    if (!c) fail("unknown-ref", `${key}: ${holder.name} has no content file ${u.file}`);
    entry.content = { source: holder.name, path: c.path, hash: c.hash, type: c.type, ...(c.binary ? { binary: true } : { text: c.text }) };
    if (c.binary) requires.add("content@1");
  }
  if (u.query !== undefined) {
    const qref = input && typeof u.query === "string" ? { input, query: u.query } : u.query;
    Object.assign(entry, boundQuery(st, qref, u.params, u.refine, cctx, scope));
    requires.add("query@1");
  } else if (u.params !== undefined || u.refine !== undefined) fail("declaration", `${key}: params and refine belong to a query; this use has none`);
  if (u.product !== undefined) {
    if (typeof u.product.path !== "string" || !/^sha256:[0-9a-f]{64}$/.test(u.product.hash ?? "")) fail("declaration", `${key}: product is {path, hash: sha256:…}`);
    entry.product = { path: u.product.path, hash: u.product.hash };
    requires.add("product@1");
  }
  for (const cap of [...requires, ...(viewer.decl.optional ?? [])]) if (!CAPABILITIES.includes(cap)) fail("capability", `viewer ${pin.viewer}: unknown capability ${cap}`);
  entry.requires = [...requires].sort();
  entry.optional = viewer.decl.optional ?? [];
  if (u.acts !== undefined) entry.acts = u.acts.map((a) => bindAct(st, a, cctx, scope, key));
  // emits {event: var}: the event is declared by the block (with its type) and by the viewer (with its source);
  // the var is declared here with the same type; acts that sign or write are never wirable (ktonpkg 5b)
  const emits = {};
  for (const [ev, vname] of Object.entries(u.emits ?? {})) {
    if (NEVER_WIRABLE.includes(ev)) fail("emits", `${key}: ${ev} is an act and never wirable`);
    const bt = decl.emits?.[ev];
    if (bt === undefined) fail("emits", `${key}: block ${b.name} declares no event ${ev}`);
    const src = viewer.decl.emits?.[ev]?.from ?? "";
    const m = /^(intent|state)\.(\w+)$/.exec(src);
    if (!m) fail("emits", `${key}: viewer ${pin.viewer} gives event ${ev} no source (intent.<kind> or state.<key>)`);
    if (m[1] === "intent" && !WIRABLE_INTENTS.includes(m[2])) fail("emits", `${key}: event ${ev} is the intent ${m[2]}, which is never wirable`);
    const d = cctx.vars?.[vname];
    if (!d) fail("var", `${key}: emits ${ev} → var ${vname}, which ${cctx.id} does not declare`);
    if (typeKey(bt) !== typeKey(d.type)) fail("var-type", `${key}: event ${ev} carries ${typeKey(bt)}, var ${vname} is ${typeKey(d.type)}`);
    emits[ev] = { var: vname, comp: cctx.compKey, type: d.type, from: src };
  }
  if (Object.keys(emits).length && !["report", "deck"].includes(cctx.kind)) entry.emits = emits;
  if (u.up === true) entry.up = true;
  const parBind = Object.fromEntries(Object.entries(entry.params ?? {}).filter(([, t]) => t?.type === "var"));
  if (Object.keys(parBind).length || Object.keys(optBind).length) entry.bind = { params: parBind, options: optBind };
  else if (entry.query) entry.ask = queryKey(entry.query.hash, entry.params);
  st.uses.push(entry);
  section.uses.push(key);
}

function bindAct(st, a, cctx, scope, key) {
  if (!INTENTS.includes(a.intent)) fail("declaration", `${key}: act ${a.intent} is not one of ${INTENTS.join("|")}`);
  const fields = {};
  for (const [k, src] of Object.entries(a.fields ?? {})) fields[k] = bindSource(st, src, scope, cctx).value;
  const out = { intent: a.intent, label: a.label ?? a.intent, fields };
  if (a.control !== undefined) out.control = typeof a.control === "string" ? { package: cctx.pkg, name: a.control } : a.control;
  if (a.ray !== undefined) out.ray = a.ray;
  if (a.intent === "sign" || a.intent === "record") {
    if (cctx.subject.fromVar) fail("subject-from-var", `${key}: a ${a.intent} act needs subject.claim, but ${cctx.id}'s subject comes from var.${cctx.subject.fromVar}`);
    if (!cctx.subject.claim) fail("unbound-var", `${key}: a ${a.intent} act is about subject.claim, which this subject lacks`);
    if (!out.control) fail("declaration", `${key}: a ${a.intent} act goes through a control`);
    out.about = cctx.subject.claim;
  }
  return out;
}

/** A param source from the closed set → an RDF term (SPARQL JSON form). */
export function bindSource(st, src, scope, cctx) {
  if (typeof src !== "string") fail("param-source", `a param source is a string: ${JSON.stringify(src)}`);
  const lit = (value) => ({ type: "literal", value: String(value) });
  const s = cctx.subject;
  switch (src) {
    case "subject.iri":
      return typeof s.iri === "string" ? { type: "uri", value: s.iri } : s.iri;
    case "subject.id":
    case "subject.claim":
    case "subject.class":
      if (s.fromVar) fail("subject-from-var", `${cctx.id} reads ${src}, but its subject comes from var.${s.fromVar}: only subject.iri is known`);
      if (!s[src.slice(8)]) fail("unbound-var", `${src} is not known for this subject`);
      return lit(s[src.slice(8)]);
    case "part.id":
      if (!scope?.part) fail("param-source", "part.id outside a part");
      return lit(scope.part.id);
    case "part.iri":
      if (!scope?.part) fail("param-source", "part.iri outside a part");
      return typeof scope.part.iri === "string" ? { type: "uri", value: scope.part.iri } : scope.part.iri;
    case "composition.id":
      return lit(cctx.hash);
    case "identity.name":
      if (!st.identity?.name) fail("unbound-var", "identity.name is not known to this plan (no identity given)");
      return lit(st.identity.name);
  }
  const vr = /^var\.([A-Za-z_]\w*)$/.exec(src);
  if (vr) {
    const d = cctx.vars?.[vr[1]];
    if (!d) fail("var", `${src}: ${cctx.id} declares no var ${vr[1]}`);
    if (["report", "deck"].includes(cctx.kind) && d.default === undefined) fail("var-default", `${src}: a var read in a ${cctx.kind} needs a default`);
    return { type: "var", var: vr[1], comp: cctx.compKey, vtype: d.type };
  }
  const row = /^row\.([A-Za-z_]\w*)$/.exec(src);
  if (row) {
    if (!scope?.row) fail("param-source", `${src} outside an every`);
    const t = scope.row[row[1]];
    if (!t) fail("unbound-var", `${src}: the row has no ${row[1]}`);
    return t;
  }
  if (/^[A-Za-z_]\w*$/.test(src)) {
    if (!(cctx.decl.params ?? []).includes(src)) fail("param-source", `${src} is not a param of ${cctx.id} (and not in the closed set)`);
    return cctx.params[src];
  }
  fail("param-source", `${src} is not in the closed set (subject.id, subject.iri, subject.claim, part.id, part.iri, row.<var>, composition.id, identity.name, a parent param)`);
}
