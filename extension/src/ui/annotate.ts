// Columns the shell adds to a block's answer, from the use's options, before the frame sees it. Generic: what is cited,
// and where decisions on one thing differ. Both read only the rows this window was answered; nothing else is fetched.
//
//   cite     "<query>" with citeColumns [...] and citeStructured "<column>" (optional): the text of these columns is scanned for
//            sha256 ids and hex prefixes of 8 or more digits; each is resolved against the rows of `query` (a named query of
//            the same package answering ?sha256 ?run ?name, optional ?mediaType ?claim: run-artifact claims) by the
//            artifact's sha256 or its claim id. A row whose `structured` column is set is cited from that column only
//            (structured citations take precedence over the text). Adds `cites` (packed like an evidence column: one line
//            per resolved artifact, images first as thumbnails) and `citesNote` (what did not resolve: ambiguous or unknown).
//   differs  "group|who|when|value" with differsLead [...] (optional): per value of `group` (e.g. an element), the rows in `when` order;
//            a row whose `value` differs from an earlier row by someone else gets `differs` ("differs from <who> (<value>,
//            <when>)"), and `differsLead` "yes" when its `who` is one of `lead` (whose later decision matters most).

type Binding = Record<string, { type: string; value: string; datatype?: string }>;
interface Answer {
  head?: { vars?: string[] };
  results?: { bindings?: Binding[] };
}

export interface CiteOptions {
  readonly columns: readonly string[];
  readonly query: string;
  readonly structured?: string;
}
export interface DiffersOptions {
  readonly group: string;
  readonly who: string;
  readonly when: string;
  readonly value: string;
  readonly lead?: readonly string[];
}

const strs = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string" && x !== "") : typeof v === "string" && v ? [v] : []);

export function citeOptions(options: Readonly<Record<string, unknown>> | undefined): CiteOptions | undefined {
  const q = options?.["cite"];
  if (typeof q !== "string" || !q) return undefined;
  const columns = strs(options?.["citeColumns"]);
  if (!columns.length) return undefined;
  const st = options?.["citeStructured"];
  return { columns, query: q, ...(typeof st === "string" && st ? { structured: st } : {}) };
}

export function differsOptions(options: Readonly<Record<string, unknown>> | undefined): DiffersOptions | undefined {
  const d = options?.["differs"];
  if (typeof d !== "string") return undefined;
  const [group, who, when, value] = d.split("|").map((x) => x.trim());
  if (!group || !who || !when || !value) return undefined;
  return { group, who, when, value, lead: strs(options?.["differsLead"]) };
}

const HEX = /(sha256:|https:\/\/kton\.dev\/o\/)?\b([0-9a-fA-F]{8,64})\b/g;

/** The hex ids a text cites, in order, once each (lowercased; a run of 8–64 hex digits that is not part of a longer word). */
export function citedIds(text: string): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(HEX)) {
    const h = (m[2] ?? "").toLowerCase();
    // digits only (e.g. a date, 20261123) count as an id only after "sha256:" or a kton IRI
    if (!m[1] && !/[a-f]/.test(h)) continue;
    if (!out.includes(h)) out.push(h);
  }
  return out;
}

interface Artifact {
  readonly sha256: string;
  readonly run: string;
  readonly name: string;
  readonly mediaType: string;
  readonly claim: string;
}

const hexOf = (v: string): string => /([0-9a-f]{64})$/.exec(v.toLowerCase())?.[1] ?? "";

/** The resolution of one cited id: one artifact, several, or none among the rows given. */
export function resolveCite(id: string, arts: readonly Artifact[]): { one: Artifact } | { many: number } | { none: true } {
  const hits = new Map<string, Artifact>();
  for (const a of arts) if (hexOf(a.sha256).startsWith(id) || (a.claim && hexOf(a.claim).startsWith(id))) hits.set(a.sha256, a);
  if (hits.size === 1) return { one: [...hits.values()][0] as Artifact };
  return hits.size ? { many: hits.size } : { none: true };
}

export function artifactsOf(answer: unknown): Artifact[] {
  const b = (answer as Answer).results?.bindings ?? [];
  return b
    .map((r) => ({ sha256: r["sha256"]?.value ?? "", run: r["run"]?.value ?? "", name: r["name"]?.value ?? "", mediaType: r["mediaType"]?.value ?? "", claim: r["claim"]?.value ?? "" }))
    .filter((a) => /^sha256:[0-9a-f]{64}$/.test(a.sha256) && a.run && a.name);
}

const lit = (value: string) => ({ type: "literal", value });

/** The answer with `cites` and `citesNote` on each row (see the header). Rows without a citation are left as they are. */
export function withCitations(answer: unknown, o: CiteOptions, arts: readonly Artifact[]): unknown {
  const a = answer as Answer;
  const rows = a.results?.bindings;
  if (!Array.isArray(rows)) return answer;
  const bindings = rows.map((r) => {
    const structured = o.structured ? (r[o.structured]?.value ?? "") : "";
    const ids = citedIds(structured || o.columns.map((c) => r[c]?.value ?? "").join("\n"));
    if (!ids.length) return r;
    const lines: string[] = [];
    const notes: string[] = [];
    const seen = new Set<string>();
    for (const id of ids) {
      const res = resolveCite(id, arts);
      if ("one" in res) {
        if (seen.has(res.one.sha256)) continue;
        seen.add(res.one.sha256);
        const image = /^image\//.test(res.one.mediaType);
        lines.push([String(lines.length + 1), image ? "assertion-screenshot" : "cited", res.one.sha256, res.one.run, res.one.name, res.one.mediaType, "", `cited as ${id.slice(0, 12)}`, ""].join("\t"));
      } else if ("many" in res) notes.push(`${id.slice(0, 12)} matches ${res.many} artifacts: cite more digits`);
      else notes.push(`${id.slice(0, 12)} is not a run artifact here`);
    }
    return { ...r, ...(lines.length ? { cites: lit(lines.join("\n")) } : {}), ...(notes.length ? { citesNote: lit(notes.join("; ")) } : {}), citesFrom: lit(structured ? "structured" : "text") };
  });
  const vars = [...new Set([...(a.head?.vars ?? []), "cites", "citesNote", "citesFrom"])];
  return { ...a, head: { ...(a.head ?? {}), vars }, results: { ...(a.results ?? {}), bindings } };
}

/** The answer with `differs` and `differsLead` on the rows whose decision differs from an earlier one by someone else. */
export function withDiffers(answer: unknown, o: DiffersOptions): unknown {
  const a = answer as Answer;
  const rows = a.results?.bindings;
  if (!Array.isArray(rows)) return answer;
  const v = (r: Binding, k: string) => r[k]?.value ?? "";
  const byGroup = new Map<string, number[]>();
  rows.forEach((r, i) => byGroup.set(v(r, o.group), [...(byGroup.get(v(r, o.group)) ?? []), i]));
  const extra = new Map<number, { differs: string; lead: boolean }>();
  for (const idx of byGroup.values()) {
    const sorted = [...idx].sort((x, y) => v(rows[x] as Binding, o.when).localeCompare(v(rows[y] as Binding, o.when)));
    sorted.forEach((i, pos) => {
      const r = rows[i] as Binding;
      const earlier = sorted.slice(0, pos).map((j) => rows[j] as Binding).filter((e) => v(e, o.who) !== v(r, o.who) && v(e, o.value) !== v(r, o.value));
      if (!earlier.length) return;
      const lead = (o.lead ?? []).includes(v(r, o.who));
      extra.set(i, { differs: `differs from ${earlier.map((e) => `${v(e, o.who)} (${v(e, o.value)}, ${v(e, o.when)})`).join("; ")}`, lead });
    });
  }
  const bindings = rows.map((r, i) => {
    const x = extra.get(i);
    return x ? { ...r, differs: lit(x.differs), ...(x.lead ? { differsLead: lit("yes") } : {}) } : r;
  });
  const vars = [...new Set([...(a.head?.vars ?? []), "differs", "differsLead"])];
  return { ...a, head: { ...(a.head ?? {}), vars }, results: { ...(a.results ?? {}), bindings } };
}

/**
 * changesBetween "repo|from|to|path" (the row's columns naming the mirrored repository, the two commits and the test
 * file) with changesReviews "<query>" (optional: ?approvedCommit ?by ?when, a review naming the commit it approves):
 * each such row gets `changes`, a JSON list of the commits between the two that touched the test file or a helper it
 * imports ({sha, subject, author, date, touched: [{path, via}], review}), and `changesNote` (none, capped, or why not).
 * Rows without all four values are left as they are.
 */
export interface ChangesOptions {
  readonly repo: string;
  readonly from: string;
  readonly to: string;
  readonly path: string;
  readonly reviews?: string;
}
export function changesOptions(options: Readonly<Record<string, unknown>> | undefined): ChangesOptions | undefined {
  const c = options?.["changesBetween"];
  if (typeof c !== "string") return undefined;
  const [repo, from, to, path] = c.split("|").map((x) => x.trim());
  if (!repo || !from || !to || !path) return undefined;
  const rv = options?.["changesReviews"];
  return { repo, from, to, path, ...(typeof rv === "string" && rv ? { reviews: rv } : {}) };
}

type ChangesFn = (repo: string, from: string, to: string, path: string) => Promise<import("./sources.js").Changes>;

/** The review of a commit among the review rows: one whose approvedCommit is a prefix of the sha (or the sha of it). */
export function reviewOf(sha: string, reviews: ReadonlyArray<Record<string, { value?: string }>>): string {
  const hits = reviews.filter((r) => {
    const a = (r["approvedCommit"]?.value ?? "").toLowerCase();
    return a.length >= 7 && (sha.startsWith(a) || a.startsWith(sha));
  });
  if (!hits.length) return "no review found";
  return hits.map((r) => `approved by ${r["by"]?.value ?? "?"}${r["when"]?.value ? ` on ${r["when"].value.slice(0, 10)}` : ""}`).join("; ");
}

export async function withChanges(answer: unknown, o: ChangesOptions, changes: ChangesFn | undefined, reviews: ReadonlyArray<Record<string, { value?: string }>> = []): Promise<unknown> {
  const a = answer as Answer;
  const rows = a.results?.bindings;
  if (!Array.isArray(rows)) return answer;
  const memo = new Map<string, Promise<import("./sources.js").Changes>>();
  const bindings = await Promise.all(rows.map(async (r) => {
    const [repo, from, to, path] = [o.repo, o.from, o.to, o.path].map((k) => r[k]?.value ?? "");
    if (!repo || !from || !to || !path) return r;
    if (!changes) return { ...r, changesNote: lit("changes not shown: no sources on this host") };
    const k = [repo, from, to, path].join("\n");
    let p = memo.get(k);
    if (!p) memo.set(k, (p = changes(repo, from, to, path)));
    const c = await p;
    if ("why" in c) return { ...r, changesNote: lit(`changes not shown: ${c.why}`) };
    const list = c.commits.map((x) => ({ ...x, repo, review: reviewOf(x.sha, reviews) }));
    const note = `${list.length ? `${list.length} commit${list.length === 1 ? "" : "s"}` : "no commit"} between ${from.slice(0, 7)} and ${to.slice(0, 7)} touched the test or one of its ${c.watched - 1} imported file${c.watched === 2 ? "" : "s"}${c.capped ? ` (${c.capped})` : ""}`;
    return { ...r, changes: lit(JSON.stringify(list)), changesNote: lit(note) };
  }));
  const vars = [...new Set([...(a.head?.vars ?? []), "changes", "changesNote"])];
  return { ...a, head: { ...(a.head ?? {}), vars }, results: { ...(a.results ?? {}), bindings } };
}
