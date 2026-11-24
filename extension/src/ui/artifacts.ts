// Run artifacts on demand: a screenshot, an error context or a trace a
// test run left. The bytes are foton outputs no cockpit verb serves; they are read from a read-only artifacts root
// (kton.artifacts.dir) at <run>/<name>, and shown only when they hash to the sha256 the run-artifact claim names.
// Shown through a read-only file system (scheme kton-art). Nothing writable.
import { createHash } from "node:crypto";
import { readFile, realpath } from "node:fs/promises";
import { isAbsolute, join, relative, sep } from "node:path";

export const ART_SCHEME = "kton-art";

/** What an open@1 artifact target names: the run (a folder name), the output name inside it, its hash, its type. */
export interface ArtifactTarget {
  readonly run: string;
  readonly name: string;
  readonly sha256: string;
  readonly mediaType?: string;
}

const RUN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;
const HASH = /^sha256:[0-9a-f]{64}$/;
const TYPE = /^[a-z]+\/[A-Za-z0-9.+-]+$/;

/** A plain relative path: no absolute path, no drive, no backslash, no "." or ".." or empty segment, no control character. */
function plainPath(p: string): boolean {
  return p.length > 0 && p.length <= 1024 && !p.startsWith("/") && !isAbsolute(p) && !/[\\:\0-\x1f]/.test(p) && p.split("/").every((s) => s !== "" && s !== "." && s !== "..");
}

/** An artifact target from untrusted input (a frame's open intent), or why not. */
export function artifactTarget(v: unknown): ArtifactTarget | string {
  const o = v as Record<string, unknown> | null;
  if (!o || typeof o !== "object") return "an artifact target is {run, name, sha256, mediaType?}";
  const { run, name, sha256, mediaType } = o;
  if (typeof run !== "string" || !RUN.test(run)) return "run is not a run name";
  if (typeof name !== "string" || !plainPath(name)) return "name is not a plain path inside the run";
  if (typeof sha256 !== "string" || !HASH.test(sha256)) return "sha256 is not sha256:<hex>: nothing is shown without the hash to check";
  if (mediaType !== undefined && (typeof mediaType !== "string" || !TYPE.test(mediaType))) return "mediaType is not a media type";
  return { run, name, sha256, ...(typeof mediaType === "string" ? { mediaType } : {}) };
}

/** The file's path under the root, resolved (symlinks too) and checked to stay under it; or why not. */
async function under(root: string, t: ArtifactTarget): Promise<string | { why: string }> {
  let base: string;
  try {
    base = await realpath(root);
  } catch {
    return { why: `run artifacts not available on this host (the artifacts root ${root} cannot be read)` };
  }
  let file: string;
  try {
    file = await realpath(join(base, t.run, t.name));
  } catch {
    return { why: `run artifact not found: ${t.run}/${t.name}` };
  }
  const rel = relative(base, file);
  if (rel === "" || rel.startsWith("..") || isAbsolute(rel) || rel.split(sep).includes("..")) return { why: `${t.run}/${t.name} resolves outside the artifacts root: not shown` };
  return file;
}

/** The artifact's bytes, checked against its sha256; or why not (nothing is shown then). */
export async function readArtifact(root: string, t: ArtifactTarget): Promise<{ bytes: Buffer } | { why: string }> {
  if (!root) return { why: "run artifacts not available on this host (no artifacts root configured: kton.artifacts.dir)" };
  const file = await under(root, t);
  if (typeof file !== "string") return file;
  let bytes: Buffer;
  try {
    bytes = await readFile(file);
  } catch {
    return { why: `run artifact not readable: ${t.run}/${t.name}` };
  }
  const got = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  if (got !== t.sha256) return { why: `${t.run}/${t.name} does not hash to ${t.sha256.slice(0, 19)}… (it is ${got.slice(0, 19)}…): refused, nothing shown` };
  return { bytes };
}

/** How the artifact is shown: an image preview, read-only text, or only its hash and size (a trace, a video). */
export function showAs(t: ArtifactTarget): "image" | "text" | "info" {
  const type = t.mediaType ?? "";
  if (/^image\/(png|jpeg|gif|webp)$/.test(type) || /\.(png|jpe?g|gif|webp)$/i.test(t.name)) return "image";
  if (/^text\//.test(type) || /\.(md|txt|log|json|jsonl)$/i.test(t.name)) return "text";
  return "info";
}

/** The reference an open leaves in a view session's looked-at list. */
export function artifactRef(t: ArtifactTarget): string {
  // lookedAt is space-separated: the name is URI-encoded
  return `run-artifact:${t.run}/${encodeURI(t.name)}@${t.sha256}`;
}

/** The read-only URI of an artifact (kton-art:/<run>/<name>?<sha256>). */
export function artifactUri(t: ArtifactTarget): { path: string; query: string } {
  return { path: `/${t.run}/${t.name}`, query: `${t.sha256}${t.mediaType ? `&${t.mediaType}` : ""}` };
}

/** The target a kton-art URI names (the file system reads it back). */
export function artifactOf(uri: { path: string; query: string }): ArtifactTarget | string {
  const [, run, ...rest] = uri.path.split("/");
  const [sha256, mediaType] = uri.query.split("&");
  return artifactTarget({ run, name: rest.join("/"), sha256, ...(mediaType ? { mediaType } : {}) });
}

/** Thumbnails in a query answer: how many, and how large an image may be to travel as one. */
export interface ThumbLimits {
  readonly perRow: number;
  readonly total: number;
}
export const THUMB_LIMITS: ThumbLimits = { perRow: 4, total: 24 };
/** The bytes a thumbnail may have (a data URL in the answer; larger images are links only). */
export const THUMB_MAX_BYTES = 400_000;
const THUMB_KINDS = /^(assertion-screenshot|screenshot)$/;

/** A packed evidence entry (ch-failures ?evidence): assertion, kind, sha256, run, name, mediaType, bytes, site, text[, thumb]. */
export function evidenceEntries(packed: string): string[][] {
  return packed
    .split("\n")
    .filter((l) => l.trim() !== "")
    .map((l) => l.split("\t"));
}

/**
 * Add hash-checked thumbnails to an answer, for the blocks to show (the frame has no other way to the bytes):
 * - a packed ?evidence column: each assertion screenshot (in assertion order) gets its data URL as a tenth field;
 * - a row with ?run ?name ?sha256 whose ?artifact (or ?kind) is a screenshot: a ?thumb binding.
 * `thumb` reads the artifact and checks its sha256 (readArtifact); it answers undefined for anything it will not show.
 * At most `perRow` per row and `total` per answer; nothing else in the answer changes.
 */
export async function withThumbs(answer: unknown, thumb: (t: ArtifactTarget) => Promise<string | undefined>, lim: ThumbLimits = THUMB_LIMITS): Promise<unknown> {
  const a = answer as { head?: { vars?: string[] }; results?: { bindings?: Array<Record<string, { type: string; value: string }>> } };
  const rows = a.results?.bindings;
  if (!Array.isArray(rows)) return answer;
  let left = lim.total;
  let added = false;
  const out = [];
  for (const r of rows) {
    const row = { ...r };
    // a column of packed artifacts: the evidence column, or what a text cites (ui/annotate.ts)
    const col = ["evidence", "cites"].find((c) => typeof r[c]?.value === "string" && (r[c]?.value ?? "").includes("\t"));
    const ev = col ? r[col]?.value : undefined;
    if (col && typeof ev === "string") {
      const es = evidenceEntries(ev).sort((x, y) => (Number(x[0]) || 1e9) - (Number(y[0]) || 1e9));
      let n = 0;
      for (const e of es) {
        const [, kind, sha256, run, name, mediaType] = e;
        let url = "";
        if (left > 0 && n < lim.perRow && kind === "assertion-screenshot" && /^image\//.test(mediaType ?? "")) {
          const t = artifactTarget({ run, name, sha256, mediaType });
          if (typeof t !== "string") url = (await thumb(t)) ?? "";
          if (url) (n++, left--);
        }
        e.length = 9;
        e.push(url);
      }
      row[col] = { type: "literal", value: es.map((e) => e.map((x) => x ?? "").join("\t")).join("\n") };
      added = true;
    } else {
      const kind = r["artifact"]?.value ?? r["kind"]?.value ?? "";
      if (left > 0 && THUMB_KINDS.test(kind) && r["sha256"] && r["run"] && r["name"] && /^image\//.test(r["mediaType"]?.value ?? "")) {
        const t = artifactTarget({ run: r["run"].value, name: r["name"].value, sha256: r["sha256"].value, mediaType: r["mediaType"]?.value });
        const url = typeof t === "string" ? undefined : await thumb(t);
        if (url) {
          row["thumb"] = { type: "literal", value: url };
          left--;
          added = true;
        }
      }
    }
    out.push(row);
  }
  if (!added) return answer;
  const vars = a.head?.vars ?? [];
  return { ...a, head: { ...a.head, vars: vars.includes("thumb") ? vars : [...vars, "thumb"] }, results: { ...a.results, bindings: out } };
}
