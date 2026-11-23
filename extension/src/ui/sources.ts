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
    execFile("git", ["-C", dir, "show", `${t.commit}:${t.path}`], { encoding: "buffer", maxBuffer: 64 * 1024 * 1024, shell: false, windowsHide: true }, (err, stdout) => {
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
