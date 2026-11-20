// Evidence on demand (MH, qm 4ead2ff8): open a file exactly as it was at a given commit, read-only, from the local
// git mirrors (kton.sources.dir), never from a checkout or worktree: `git show <commit>:<path>`, and the bytes must
// hash to the sha256 the data names. Shown through a read-only content provider (scheme kton-src). Nothing writable.
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { join } from "node:path";

export const SRC_SCHEME = "kton-src";

/** What an open@1 source target names: a repository (by its data name), a commit, a path, a line, the file's hash. */
export interface SourceTarget {
  readonly repo: string;
  readonly commit: string;
  readonly path: string;
  readonly line?: number;
  readonly sha256?: string;
}

const NAME = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const COMMIT = /^[0-9a-f]{7,64}$/;

/** A source target from untrusted input (a frame's open intent), or why not. */
export function sourceTarget(v: unknown): SourceTarget | string {
  const o = v as Record<string, unknown> | null;
  if (!o || typeof o !== "object") return "a source target is {repo, commit, path, line?, sha256?}";
  const { repo, commit, path, line, sha256 } = o as Record<string, unknown>;
  if (typeof repo !== "string" || !NAME.test(repo)) return "repo is not a repository name";
  if (typeof commit !== "string" || !COMMIT.test(commit)) return "commit is not a commit id";
  if (typeof path !== "string" || !path || path.startsWith("/") || path.split("/").some((s) => s === ".." || s === "") || /[\\:]/.test(path)) return "path is not a plain repository path";
  const ln = typeof line === "string" ? Number(line) : line;
  if (ln !== undefined && (!Number.isInteger(ln) || (ln as number) < 1)) return "line is not a line number";
  if (sha256 !== undefined && (typeof sha256 !== "string" || !/^sha256:[0-9a-f]{64}$/.test(sha256))) return "sha256 is not sha256:<hex>";
  return { repo, commit, path, ...(ln !== undefined ? { line: ln as number } : {}), ...(typeof sha256 === "string" ? { sha256 } : {}) };
}

/** Where a data repository name lives among the mirrors (verification's "ext-core" is improve-workbench-ext-core). */
export const REPO_ALIASES: Readonly<Record<string, string>> = { "ext-core": "improve-workbench-ext-core" };

/** The bytes of path at commit in the mirror, checked against sha256; or why not ("source not available on this host"). */
export function readAtCommit(mirrors: string, t: SourceTarget): Promise<{ bytes: Buffer } | { why: string }> {
  if (!mirrors) return Promise.resolve({ why: "source not available on this host (no mirrors configured: kton.sources.dir)" });
  const dir = join(mirrors, REPO_ALIASES[t.repo] ?? t.repo);
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
  return { path: `/${t.repo}/${t.path}`, query: `${t.commit}${t.sha256 ? `&${t.sha256}` : ""}`, fragment: t.line ? `L${t.line}` : "" };
}

/** The target a kton-src URI names (the content provider reads it back). */
export function targetOf(uri: { path: string; query: string; fragment: string }): SourceTarget | string {
  const [, repo, ...rest] = uri.path.split("/");
  const [commit, sha256] = uri.query.split("&");
  const line = /^L(\d+)$/.exec(uri.fragment)?.[1];
  return sourceTarget({ repo, commit, path: rest.join("/"), ...(line ? { line: Number(line) } : {}), ...(sha256 ? { sha256 } : {}) });
}
