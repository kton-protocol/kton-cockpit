// Run artifacts on demand (test-execution's dryrun1.runs, coordinator 20.11.): a screenshot, an error context or a trace a
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
  return `run-artifact:${t.run}/${t.name}@${t.sha256}`;
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
