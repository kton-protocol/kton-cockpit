// SPARQL helpers shared by plan, fill and the hosts' query@1. Plain ES module, no dependencies.
//
// Terms use the W3C SPARQL JSON form: {type: "uri"|"literal"|"bnode", value, datatype?, "xml:lang"?}.

/** The $VARs a query uses. Comment lines are skipped, so a variable named only in a comment is not required. */
export function varsOf(text) {
  const body = text
    .split("\n")
    .filter((l) => !/^\s*#/.test(l))
    .join("\n");
  return [...new Set([...body.matchAll(/\$([A-Za-z_]\w*)/g)].map((m) => m[1]))].sort();
}

/** A term as SPARQL text: IRIs in <>, literals quoted (with datatype or language). */
export function termToSparql(t) {
  if (t.type === "uri") {
    if (/[\s<>"{}|\\^`]/.test(t.value)) throw new Error(`not a usable IRI: ${t.value}`);
    return `<${t.value}>`;
  }
  if (t.type === "literal") {
    const s = `"${t.value.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n").replace(/\r/g, "\\r")}"`;
    if (t["xml:lang"]) return `${s}@${t["xml:lang"]}`;
    return t.datatype ? `${s}^^<${t.datatype}>` : s;
  }
  throw new Error(`cannot bind a ${t.type} term`);
}

/**
 * Bind params into a query the way zusammensetzen does: every `$name` is replaced by the term
 * (`$document` → `<…#SOP-02>`, `$chapter` → `<…#SOP-02/roles-and-responsibilities>`).
 * Every variable the query uses must be bound, except those named in optional (ktonpkg v0.9.10): an
 * unbound optional `$name` is left as it is, a free SPARQL variable, so the query decides what "no
 * filter" means.
 */
export function bindQuery(text, params, optional = []) {
  const missing = varsOf(text).filter((v) => !(v in params) && !optional.includes(v));
  if (missing.length) throw new Error(`unbound $${missing.join(", $")}`);
  return text.replace(/\$([A-Za-z_]\w*)/g, (m, v) => (v in params ? termToSparql(params[v]) : m));
}

/** The text zusammensetzen shows for a term: an IRI's part after '#', a literal's value. */
export function termText(t) {
  if (!t) return "";
  if (t.type === "uri") return t.value.includes("#") ? t.value.slice(t.value.lastIndexOf("#") + 1) : t.value;
  return t.value;
}

/** Rows of a SPARQL JSON answer: {vars, rows: [{var: term}]}. */
export function rowsOf(answer) {
  const vars = answer?.head?.vars ?? [];
  const rows = answer?.results?.bindings ?? [];
  return { vars, rows };
}

/** The key of a bound query: its hash plus its params, the key plan's needs and fill's results use. */
export function queryKey(hash, params) {
  return `${hash}?${Object.keys(params ?? {})
    .sort()
    .map((k) => `${k}=${termToSparql(params[k])}`)
    .join("&")}`;
}
