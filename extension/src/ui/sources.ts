// Evidence on demand: open a file exactly as it was at a given commit, read-only, from the local
// git mirrors (kton.sources.dir), never from a checkout or worktree: `git show <commit>:<path>`, and the bytes must
// hash to the sha256 the data names. Shown through a read-only content provider (scheme kton-src). Nothing writable.
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { join } from "node:path";

export const SRC_SCHEME = "kton-src";

/**
 * What an open@1 source target names: a repository (by its data name), a commit, a path, a line, the file's hash; or,
 * with diff, the commit's change (git show <commit>, optionally only path's part): what a reviewer reviews.
 */
export interface SourceTarget {
  readonly repo: string;
  readonly commit: string;
  /** "" only for a whole commit's diff */
  readonly path: string;
  readonly line?: number;
  readonly sha256?: string;
  readonly diff?: true;
}

const NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const COMMIT = /^[0-9a-f]{7,64}$/;

/** A source target from untrusted input (a frame's open intent), or why not. */
export function sourceTarget(v: unknown): SourceTarget | string {
  const o = v as Record<string, unknown> | null;
  if (!o || typeof o !== "object") return "a source target is {repo, commit, path, line?, sha256?}";
  const { repo, commit, path, line, sha256, diff } = o as Record<string, unknown>;
  if (typeof repo !== "string" || !NAME.test(repo)) return "repo is not a repository name";
  if (typeof commit !== "string" || !COMMIT.test(commit)) return "commit is not a commit id";
  if (diff !== undefined && diff !== true && diff !== "true") return "diff is true or absent";
  const isDiff = diff === true || diff === "true";
  if (isDiff && (path === undefined || path === "")) {
    if (line !== undefined || sha256 !== undefined) return "a commit's diff takes no line and no sha256";
    return { repo, commit, path: "", diff: true };
  }
  if (typeof path !== "string" || !path || path.startsWith("/") || path.startsWith("-") || path.split("/").some((s) => s === ".." || s === "") || /[\\:]/.test(path)) return "path is not a plain repository path";
  if (isDiff) {
    if (line !== undefined || sha256 !== undefined) return "a commit's diff takes no line and no sha256";
    return { repo, commit, path, diff: true };
  }
  const ln = typeof line === "string" ? Number(line) : line;
  if (ln !== undefined && (!Number.isInteger(ln) || (ln as number) < 1)) return "line is not a line number";
  if (sha256 !== undefined && (typeof sha256 !== "string" || !/^sha256:[0-9a-f]{64}$/.test(sha256))) return "sha256 is not sha256:<hex>";
  return { repo, commit, path, ...(ln !== undefined ? { line: ln as number } : {}), ...(typeof sha256 === "string" ? { sha256 } : {}) };
}

/**
 * The mirror folder of a data repository name: the alias (kton.sources.aliases) where the data names a repository
 * otherwise than its mirror folder, else the name itself; never anything but a plain folder name.
 */
export function mirrorDir(mirrors: string, repo: string, aliases: Readonly<Record<string, string>> = {}): string {
  const a = Object.prototype.hasOwnProperty.call(aliases, repo) ? aliases[repo] : undefined;
  return join(mirrors, a !== undefined && NAME.test(a) ? a : repo);
}

/** The bytes of path at commit in the mirror, checked against sha256; or why not ("source not available on this host"). */
export function readAtCommit(mirrors: string, t: SourceTarget, aliases: Readonly<Record<string, string>> = {}): Promise<{ bytes: Buffer } | { why: string }> {
  if (!mirrors) return Promise.resolve({ why: "source not available on this host (no mirrors configured: kton.sources.dir)" });
  const dir = mirrorDir(mirrors, t.repo, aliases);
  if (t.diff) {
    // the commit's change as git shows it (no colour, no external diff driver, no textconv), read from the mirror's
    // objects only; nothing is checked out. The commit id was checked to be hex, the path to be a plain path.
    const args = ["-C", dir, "show", "--no-color", "--no-ext-diff", "--no-textconv", "--format=fuller", "--stat", "--patch", t.commit, "--", ...(t.path ? [t.path] : [])];
    return new Promise((resolve) => {
      execFile("git", args, { encoding: "buffer", maxBuffer: 64 * 1024 * 1024, shell: false, windowsHide: true }, (err, stdout) => {
        if (err) return resolve({ why: `source not available on this host: ${t.repo} has no commit ${t.commit.slice(0, 12)}` });
        resolve({ bytes: stdout as Buffer });
      });
    });
  }
  return new Promise((resolve) => {
    // the blob's bytes as stored: no textconv filter of the mirror's config turns them into something else
    execFile("git", ["-C", dir, "show", "--no-textconv", `${t.commit}:${t.path}`], { encoding: "buffer", maxBuffer: 64 * 1024 * 1024, shell: false, windowsHide: true }, (err, stdout) => {
      if (err) return resolve({ why: `source not available on this host: ${t.repo} has no ${t.path} at ${t.commit.slice(0, 12)}` });
      const bytes = stdout as Buffer;
      if (t.sha256 && `sha256:${createHash("sha256").update(bytes).digest("hex")}` !== t.sha256) {
        return resolve({ why: `${t.repo}:${t.path} at ${t.commit.slice(0, 12)} does not hash to ${t.sha256.slice(0, 19)}…: not shown` });
      }
      resolve({ bytes });
    });
  });
}

/** The read-only document URI of a target (kton-src:/<repo>/<path>?<commit>#L<line>). */
export function sourceUri(t: SourceTarget): { path: string; query: string; fragment: string } {
  if (t.diff) return { path: `/${t.repo}/${t.path ? `${t.path}.diff` : `${t.commit}.diff`}`, query: `${t.commit}&diff`, fragment: "" };
  return { path: `/${t.repo}/${t.path}`, query: `${t.commit}${t.sha256 ? `&${t.sha256}` : ""}`, fragment: t.line ? `L${t.line}` : "" };
}

/** The target a kton-src URI names (the content provider reads it back). */
export function targetOf(uri: { path: string; query: string; fragment: string }): SourceTarget | string {
  const [, repo, ...rest] = uri.path.split("/");
  const [commit, sha256] = uri.query.split("&");
  if (sha256 === "diff") {
    const p = rest.join("/").replace(/\.diff$/, "");
    return sourceTarget({ repo, commit, diff: true, ...(p && p !== commit ? { path: p } : {}) });
  }
  const line = /^L(\d+)$/.exec(uri.fragment)?.[1];
  return sourceTarget({ repo, commit, path: rest.join("/"), ...(line ? { line: Number(line) } : {}), ...(sha256 ? { sha256 } : {}) });
}

/** The reference an open leaves in a view session's looked-at list (the source at its commit and line). lookedAt is
 * space-separated, so the path is URI-encoded (a spec path has spaces). */
export function sourceRef(t: SourceTarget): string {
  if (t.diff) return `kton-src:/${t.repo}/${encodeURI(t.path)}?${t.commit}&diff`;
  return `kton-src:/${t.repo}/${encodeURI(t.path)}?${t.commit}${t.line ? `#L${t.line}` : ""}`;
}

/** Whether an approval given on one commit still holds at another: the approved files changed between them, or not. */
export type Staleness = { readonly stale: boolean; readonly files: readonly string[]; readonly paths: readonly string[] } | { readonly why: string };

/**
 * An approval given on `approved` claimed for `head` (e.g. a verdict that names an earlier commit): read-only, from the mirror's objects,
 * `git diff --name-only <approved>..<head> -- <paths>`. The paths are the approval's own (a structured field) or, when
 * it names none, the files the approved commit changed (`git show --name-only <approved>`). Stale when any of them
 * changed; the changed files are named.
 */
export async function approvalStaleness(mirrors: string, repo: string, approved: string, head: string, paths: readonly string[] = [], aliases: Readonly<Record<string, string>> = {}): Promise<Staleness> {
  if (!mirrors) return { why: "source not available on this host (no mirrors configured: kton.sources.dir)" };
  if (!NAME.test(repo) || !COMMIT.test(approved) || !COMMIT.test(head)) return { why: "repo or commit is not plain" };
  for (const p of paths) if (!p || p.startsWith("/") || p.startsWith("-") || p.split("/").some((x) => x === ".." || x === "")) return { why: `path ${JSON.stringify(p)} is not a plain repository path` };
  const dir = mirrorDir(mirrors, repo, aliases);
  const git = (args: string[]) =>
    new Promise<string | undefined>((resolve) => execFile("git", ["-C", dir, ...args], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024, shell: false, windowsHide: true }, (err, out) => resolve(err ? undefined : String(out))));
  const lines = (x: string) => x.split("\n").map((l) => l.trim()).filter(Boolean);
  let watched = [...paths];
  if (!watched.length) {
    const own = await git(["show", "--no-color", "--name-only", "--format=", approved, "--"]);
    if (own === undefined) return { why: `source not available on this host: ${repo} has no commit ${approved.slice(0, 12)}` };
    watched = lines(own);
  }
  if (!watched.length) return { stale: false, files: [], paths: [] };
  const changed = await git(["diff", "--no-color", "--no-ext-diff", "--name-only", `${approved}..${head}`, "--", ...watched]);
  if (changed === undefined) return { why: `source not available on this host: ${repo} has no ${approved.slice(0, 12)}..${head.slice(0, 12)}` };
  const files = lines(changed);
  return { stale: files.length > 0, files, paths: watched };
}

/** What a test depends on and what changed in it between two commits (changesBetween). */
export interface ChangedCommit {
  readonly sha: string;
  readonly subject: string;
  readonly author: string;
  readonly date: string;
  /** each touched file of the watched set, with how the test reaches it ("the test file", or the import chain) */
  readonly touched: ReadonlyArray<{ readonly path: string; readonly via: string }>;
}
export type Changes = { readonly commits: readonly ChangedCommit[]; readonly watched: number; readonly capped?: string } | { readonly why: string };
export const CHANGES_CAPS = { depth: 4, files: 60, commits: 50 } as const;

/** The relative module specifiers a TS/JS text imports (static, side-effect, dynamic and require), in order, once each. */
export function relativeImports(text: string): string[] {
  const out: string[] = [];
  const res = [/\bfrom\s*['"]([^'"\n]+)['"]/g, /\bimport\s*['"]([^'"\n]+)['"]/g, /\b(?:require|import)\s*\(\s*['"]([^'"\n]+)['"]\s*\)/g];
  for (const re of res) for (const m of text.matchAll(re)) {
    const s = m[1] ?? "";
    if ((s.startsWith("./") || s.startsWith("../")) && !out.includes(s)) out.push(s);
  }
  return out;
}

/** posix-normalised join of a file's folder and a relative specifier; undefined when it leaves the repository. */
export function joinRel(fromFile: string, spec: string): string | undefined {
  const parts = fromFile.split("/").slice(0, -1);
  for (const seg of spec.split("/")) {
    if (seg === "" || seg === ".") continue;
    if (seg === "..") {
      if (!parts.length) return undefined;
      parts.pop();
    } else parts.push(seg);
  }
  return parts.join("/");
}

/** The file a relative import names, among the repository's files: as written, with .ts/.js/.mjs, a .js written for .ts, or an index. */
export function resolveImport(fromFile: string, spec: string, exists: (p: string) => boolean): string | undefined {
  const base = joinRel(fromFile, spec);
  if (base === undefined) return undefined;
  const tries = [base, `${base}.ts`, `${base}.js`, `${base}.mjs`, ...(/\.(m?js)$/.test(base) ? [base.replace(/\.(m?js)$/, ".ts")] : []), `${base}/index.ts`, `${base}/index.js`];
  return tries.find((p) => /\.(ts|js|mjs)$/.test(p) && exists(p));
}

/**
 * The commits between two commits that touched a test file or a helper it imports. Read-only git over the mirror (no
 * checkout): the files at `to` (git ls-tree), the test's relative imports (.ts/.js/.mjs) followed transitively up to
 * caps.depth and caps.files (said when capped), then `git log from..to -- <the test and its imports>` with each
 * commit's touched files, each named with how the test reaches it.
 */
export async function changesBetween(mirrors: string, repo: string, from: string, to: string, testPath: string, aliases: Readonly<Record<string, string>> = {}, caps: { depth: number; files: number; commits: number } = CHANGES_CAPS): Promise<Changes> {
  if (!mirrors) return { why: "source not available on this host (no mirrors configured: kton.sources.dir)" };
  if (!NAME.test(repo) || !COMMIT.test(from) || !COMMIT.test(to)) return { why: "repo or commit is not plain" };
  if (!testPath || testPath.startsWith("/") || testPath.startsWith("-") || testPath.split("/").some((x) => x === ".." || x === "")) return { why: `path ${JSON.stringify(testPath)} is not a plain repository path` };
  const dir = mirrorDir(mirrors, repo, aliases);
  const git = (args: string[]) =>
    new Promise<string | undefined>((resolve) => execFile("git", ["-C", dir, ...args], { encoding: "utf8", maxBuffer: 32 * 1024 * 1024, shell: false, windowsHide: true }, (err, out) => resolve(err ? undefined : String(out))));
  const tree = await git(["--literal-pathspecs", "ls-tree", "-r", "--name-only", to]);
  if (tree === undefined) return { why: `source not available on this host: ${repo} has no commit ${to.slice(0, 12)}` };
  const files = new Set(tree.split("\n").filter(Boolean));
  if (!files.has(testPath)) return { why: `${testPath} is not in ${repo} at ${to.slice(0, 12)}` };
  // the test and what it imports, breadth first; each file's importer, so its chain from the test can be named
  const parent = new Map<string, string | undefined>([[testPath, undefined]]);
  let level = [testPath];
  let capped: string | undefined;
  for (let depth = 1; level.length && !capped; depth++) {
    const next: string[] = [];
    for (const f of level) {
      const text = await git(["--literal-pathspecs", "show", "--no-textconv", `${to}:${f}`]);
      if (text === undefined) continue;
      for (const spec of relativeImports(text)) {
        const p = resolveImport(f, spec, (x) => files.has(x));
        if (!p || parent.has(p)) continue;
        if (depth > caps.depth) {
          capped = `imports followed ${caps.depth} levels deep; deeper ones are not watched`;
          break;
        }
        if (parent.size >= caps.files) {
          capped = `${caps.files} files watched; more imports are not`;
          break;
        }
        parent.set(p, f);
        next.push(p);
      }
      if (capped) break;
    }
    level = next;
  }
  const short = (f: string) => f.split("/").pop() ?? f;
  const viaOf = (f: string): string => {
    if (f === testPath) return "the test file";
    const chain: string[] = [];
    for (let c: string | undefined = parent.get(f); c !== undefined && c !== testPath; c = parent.get(c)) chain.unshift(short(c));
    return chain.length ? `a helper the test imports via ${chain.join(" → ")}` : "a helper the test imports";
  };
  const via = new Map([...parent.keys()].map((f) => [f, viaOf(f)]));
  const log = await git(["--literal-pathspecs", "log", "--no-color", `--max-count=${caps.commits + 1}`, "--format=%x1e%H%x1f%s%x1f%an%x1f%aI", "--name-only", `${from}..${to}`, "--", ...via.keys()]);
  if (log === undefined) return { why: `source not available on this host: ${repo} has no ${from.slice(0, 12)}..${to.slice(0, 12)}` };
  const commits: ChangedCommit[] = [];
  for (const rec of log.split("\x1e").map((r) => r.trim()).filter(Boolean)) {
    const [head = "", ...names] = rec.split("\n");
    const [sha = "", subject = "", author = "", date = ""] = head.split("\x1f");
    const touched = names.map((n) => n.trim()).filter((n) => via.has(n)).map((path) => ({ path, via: via.get(path) as string }));
    commits.push({ sha, subject, author, date, touched });
  }
  if (commits.length > caps.commits) {
    commits.length = caps.commits;
    capped = [capped, `the newest ${caps.commits} commits shown`].filter(Boolean).join("; ");
  }
  return { commits, watched: via.size, ...(capped ? { capped } : {}) };
}

/** The commits a run at `head` would contain since the last reviewed state, each with its review (requireReviewed). */
export interface ReviewedState {
  /** the newest proper ancestor of head with an approval, if one was found within the cap */
  readonly base?: string;
  readonly commits: ReadonlyArray<{ readonly sha: string; readonly subject: string; readonly author: string; readonly date: string; readonly review?: string }>;
  /** the commits after base up to head with no approval of their own */
  readonly missing: readonly string[];
  readonly capped?: string;
  /** more commits than the cap: the oldest were not checked (an act under the rule is blocked, fail closed) */
  readonly truncated?: boolean;
}

/**
 * With a base (the state the last run ran at): every commit in base..head. Without one: walk back from head (read-only
 * `git log` over the mirror) to the newest proper ancestor that has an approval of its own (the last reviewed state); every commit after it up to head is listed with its approval or none. An approval
 * covers only the commit it names (a later commit's approval does not cover its parents). No approved ancestor within
 * `cap` commits: all of them are listed and the cap is said.
 */
export async function reviewedState(mirrors: string, repo: string, head: string, reviewOf: (sha: string) => string | undefined, aliases: Readonly<Record<string, string>> = {}, cap = 50, base?: string): Promise<ReviewedState | { why: string }> {
  if (!mirrors) return { why: "source not available on this host (no mirrors configured: kton.sources.dir)" };
  const REF = /^[A-Za-z0-9][A-Za-z0-9._/-]*$/;
  if (!NAME.test(repo) || !REF.test(head) || head.includes("..") || (base !== undefined && (!REF.test(base) || base.includes("..")))) return { why: "repo, head or base is not plain" };
  const dir = mirrorDir(mirrors, repo, aliases);
  const git = (args: string[]) => new Promise<string | undefined>((resolve) => execFile("git", ["-C", dir, ...args], { encoding: "utf8", maxBuffer: 16 * 1024 * 1024, shell: false, windowsHide: true }, (err, o) => resolve(err ? undefined : String(o))));
  if (base !== undefined) {
    // a given base (the state the last run ran at): every commit in base..head, each needing its own review
    if ((await git(["merge-base", "--is-ancestor", base, head])) === undefined) return { why: `${base} is not an ancestor of ${head} in ${repo} (or ${repo} lacks one of them)` };
    const list = await git(["log", "--no-color", `--max-count=${cap + 1}`, "--format=%H%x1f%s%x1f%an%x1f%aI", `${base}..${head}`, "--"]);
    if (list === undefined) return { why: `source not available on this host: ${repo} has no ${base}..${head}` };
    const cs = list.split("\n").filter(Boolean).map((l) => {
      const [sha = "", subject = "", author = "", date = ""] = l.split("\x1f");
      const review = reviewOf(sha);
      return { sha, subject, author, date, ...(review ? { review } : {}) };
    });
    const capped = cs.length > cap ? `the newest ${cap} commits of ${base.slice(0, 12)}..${head}` : undefined;
    const commits = cs.slice(0, cap);
    const full = (await git(["rev-parse", base]))?.trim() ?? base;
    return { base: full, commits, missing: commits.filter((c) => !c.review).map((c) => c.sha), ...(capped ? { capped, truncated: true } : {}) };
  }
  const out = await git(["log", "--no-color", `--max-count=${cap + 1}`, "--format=%H%x1f%s%x1f%an%x1f%aI", head, "--"]);
  if (out === undefined) return { why: `source not available on this host: ${repo} has no ${head}` };
  const all = out.split("\n").filter(Boolean).map((l) => {
    const [sha = "", subject = "", author = "", date = ""] = l.split("\x1f");
    return { sha, subject, author, date };
  });
  const commits: Array<ReviewedState["commits"][number]> = [];
  let found: string | undefined;
  for (const [i, c] of all.entries()) {
    const review = reviewOf(c.sha);
    if (i > 0 && review) {
      found = c.sha;
      break;
    }
    if (i >= cap) break;
    commits.push({ ...c, ...(review ? { review } : {}) });
  }
  // the walk follows commit dates, which differ from history at a merge (a side commit older than the reviewed one is
  // behind it in the log): with the base found, list base..head as a given base does, every commit outside its history
  if (found) return reviewedState(mirrors, repo, head, reviewOf, aliases, cap, found);
  const missing = commits.filter((c) => !c.review).map((c) => c.sha);
  return { commits, missing, capped: all.length > cap ? `no reviewed ancestor within ${cap} commits of ${head}` : `no reviewed ancestor of ${head}`, ...(all.length > cap ? { truncated: true } : {}) };
}
