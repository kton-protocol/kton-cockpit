// The SPARQL texts the views read, from a folder (setting kton.queries.dir, *.rq), and the row helpers.
//
// Only query TEXTS come from a folder: they decide what the views show, never what may be signed.
// Templates come only from the cockpit's listing (`say --json`, review check 2): a template that exists
// only as a file in the checkout is not signable. A query folder stays until the queries come with an
// installed package the cockpit lists and runs by name (`ask --json` lists package queries on v0.5.0).

import { readdir, readFile } from "node:fs/promises";
import { join } from "node:path";
import type { AskResult } from "../transport/port.js";

export type Queries = ReadonlyMap<string, string>;

export async function loadQueries(dir: string): Promise<Queries> {
  const queries = new Map<string, string>();
  let files: string[] = [];
  try {
    files = (await readdir(dir)).sort();
  } catch {
    return queries;
  }
  for (const f of files) if (f.endsWith(".rq")) queries.set(f.slice(0, -3), await readFile(join(dir, f), "utf8"));
  return queries;
}

export type Row = Readonly<Record<string, string>>;

/**
 * The rows of a SPARQL answer, whichever form the cockpit used: the JSON-argument form answers
 * `{sparql: {vars, rows: [{var: value}]}}`, `--results-json` answers the W3C form.
 */
export function rowsOf(a: AskResult): Row[] {
  const s = a["sparql"] as { rows?: unknown } | undefined;
  if (s && Array.isArray(s.rows)) {
    return s.rows.map((r) => Object.fromEntries(Object.entries(r as Record<string, unknown>).map(([k, v]) => [k, String(v)])));
  }
  const b = a.results?.results.bindings ?? [];
  return b.map((r) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, v.value])));
}

/**
 * The W3C SPARQL JSON of an answer, whichever form the cockpit used: `--results-json` puts it at the top
 * (`results`), the JSON-argument form (`ask {query: sparql, name, …}`, what named queries use) under
 * `sparql.results`. Shape-checked; an answer with neither is an error, never "no rows".
 */
export function sparqlResultsOf(a: AskResult): { head: { vars: string[] }; results: { bindings: Array<Record<string, { type: string; value: string }>> } } {
  const r = (a.results ?? (a["sparql"] as { results?: unknown } | undefined)?.results) as { head?: { vars?: unknown }; results?: { bindings?: unknown } } | undefined;
  if (!r || !Array.isArray(r.head?.vars) || !Array.isArray(r.results?.bindings)) throw new Error("the cockpit's answer holds no SPARQL results (neither results nor sparql.results)");
  return r as ReturnType<typeof sparqlResultsOf>;
}

/** https://kton.dev/o/<hex> → sha256:<hex>; anything else unchanged. */
export function idOf(iri: string): string {
  const m = /^https:\/\/kton\.dev\/o\/([0-9a-f]{64})$/.exec(iri);
  return m ? `sha256:${m[1]}` : iri;
}

/** The last path segment of a predicate IRI: https://…/requests → requests. */
export function kindOf(iri: string): string {
  return iri.replace(/[#/]$/, "").split(/[#/]/).pop() ?? iri;
}
