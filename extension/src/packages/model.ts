// The admitted packages as the composition library needs them (the ktonpkg JS mirror's package model),
// built from `ui list` (declarations, member hashes) and the package's own bytes, each checked against its
// hash at load (C1.5: re-verify at load, not only at install). A package that fails any check renders
// nothing and is listed with its reason; the others are unaffected.
//
// Viewers: a block pins {viewer, at, tree}. A tree the package carries (viewers/<viewer>/, ktonpkg v0.9.1)
// is checked here: its TreeID must be the pin, and the block may emit and take only what its viewer.json
// declares (emits, accepts), with equal types. A viewer fetched from kton-web (at = a 40-hex commit) must be
// one this shell vendors at that tree; anything else is refused with the reason.
import { createHash } from "node:crypto";
import type { Member, Refusal, UiEntry, UiListing, UiPackage } from "./listing.js";

export interface ViewerTree {
  /** viewer.json */
  readonly decl: Readonly<Record<string, unknown>>;
  readonly tree: string;
  /** path → bytes, relative to the viewer folder */
  readonly files: ReadonlyMap<string, Uint8Array>;
}
export interface LoadedViewer {
  readonly decl: Readonly<Record<string, unknown>>;
  readonly tree: string;
  /** the module path the plan carries: relative to the composition base the host serves */
  readonly module: string;
  /** where its files are served from, relative to the base, and the files */
  readonly dir: string;
  readonly files: ReadonlyMap<string, Uint8Array>;
}
export interface Control {
  readonly package: string;
  readonly name: string;
  readonly template: string;
  /** ktonpkg Control.EffectiveKind(): decision when the control says none */
  readonly kind: "decision" | "record";
  readonly about?: string;
  /** what the act means (C3.4), declared on the control (ktonpkg v0.9.2) */
  readonly meaning?: string;
  /** the control's title (shown in the sign dialog: what the act does, e.g. how long it takes) */
  readonly title?: string;
}
/** A viewer that runs as a page in its own frame (viewer.json frame: leaf), pinned by a viewers/<name>.json declaration. */
export interface LeafViewer {
  readonly package: string;
  /** the declaration's name (what a ui entry's `viewer` says) */
  readonly name: string;
  readonly viewer: string;
  readonly decl: Readonly<Record<string, unknown>>;
  readonly tree: string;
  /** the folder under the vendored kit (kton-web/viewers/<v>) */
  readonly dir: string;
}
export interface MirrorPackage {
  readonly name: string;
  /** the package id (`ui cat`'s first argument) and the revision it was loaded at */
  readonly id: string;
  readonly revision: string;
  readonly blocks: Record<string, { decl: Record<string, unknown>; hash: string }>;
  readonly compositions: Record<string, { decl: Record<string, unknown>; hash: string; text: string }>;
  readonly queries: Record<string, { path: string; hash: string; text: string }>;
  readonly content: Record<string, { path: string; hash: string; type: string; text?: string; binary?: boolean }>;
  readonly controls: Record<string, { decl: Record<string, unknown> }>;
  readonly templates: Record<string, { decl: Record<string, unknown> }>;
  readonly viewers: Record<string, LoadedViewer>;
  readonly leaves: Record<string, LeafViewer>;
}
export interface SlotEntry extends UiEntry {
  readonly package: string;
  readonly revision: string;
}
export interface PackageRefusal extends Refusal {
  /** the package's name, or its dir when it has none */
  readonly package: string;
}
export interface Loaded {
  readonly packages: Readonly<Record<string, MirrorPackage>>;
  readonly entries: readonly SlotEntry[];
  readonly controls: ReadonlyMap<string, Control>;
  readonly refusals: readonly PackageRefusal[];
  /** installed, not admitted (claims.allowedPackages): shown, never loaded */
  readonly notAdmitted: readonly string[];
  /** the content hash of each member a package's content role holds, for content@1 */
  readonly byHash: ReadonlyMap<string, ReadonlyMap<string, PackageFile>>;
}
/** A content member of a package: by content hash, then package id (two packages may carry the same bytes). */
export interface PackageFile {
  readonly pkg: string;
  readonly hash: string;
  readonly type: string;
}
/** A member's bytes by package (its id) and content hash: `cockpit ui cat`. */
export type ReadBlob = (pkg: string, hash: string) => Promise<Uint8Array>;

const sha = (b: Uint8Array) => createHash("sha256").update(b).digest("hex");
const KTON_WEB_COMMIT = /^[0-9a-f]{40}$/;
const TYPES: Record<string, string> = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", pdf: "application/pdf", md: "text/markdown", json: "application/json", svg: "image/svg+xml", txt: "text/plain", mmd: "text/vnd.mermaid" };
const BINARY = /\.(png|jpe?g|gif|webp|pdf)$/i;
export const typeOfPath = (p: string) => TYPES[p.split(".").pop()?.toLowerCase() ?? ""] ?? "application/octet-stream";

/**
 * ktonpkg.TreeID: sha256 over the canonical JSON {"files":[{"hash":"sha256:<hex>","path":"<path>"},…]}, files
 * sorted by path, no whitespace. ktonpkg skips .git/, bin/, *~ and *.exe.
 */
export function treeId(files: ReadonlyMap<string, Uint8Array>): string {
  const entries = [...files.keys()]
    .filter((p) => !/(^|\/)(\.git|bin)\//.test(p) && !p.endsWith("~") && !p.endsWith(".exe"))
    .sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
    .map((p) => `{"hash":${JSON.stringify(`sha256:${sha(files.get(p) as Uint8Array)}`)},"path":${JSON.stringify(p)}}`);
  return `sha256:${createHash("sha256").update(`{"files":[${entries.join(",")}]}`).digest("hex")}`;
}

/** A var/option/event type as a comparable key: "literal" | "iri" | "integer" | "hash" | "set:<base>". */
export function typeKey(t: unknown): string | null {
  if (typeof t === "string") return ["literal", "iri", "integer", "hash"].includes(t) ? t : null;
  if (t && typeof t === "object" && !Array.isArray(t)) {
    const s = (t as Record<string, unknown>)["set"];
    if (typeof s === "string" && ["literal", "iri", "integer", "hash"].includes(s)) return `set:${s}`;
  }
  return null;
}

class Refuse extends Error {
  constructor(
    readonly code: string,
    readonly file: string,
    reason: string,
  ) {
    super(reason);
  }
}

/** Parse a member's JSON; a malformed one refuses the package. */
function json(bytes: Uint8Array, file: string): Record<string, unknown> {
  try {
    const v = JSON.parse(Buffer.from(bytes).toString("utf8")) as unknown;
    if (typeof v !== "object" || v === null || Array.isArray(v)) throw new Error("not an object");
    return v as Record<string, unknown>;
  } catch (e) {
    throw new Refuse("package.malformed", file, `${file} is not a JSON object: ${(e as Error).message}`);
  }
}

const ROLE_DIR: Record<string, string> = { ui: "ui", block: "ui/blocks", composition: "ui/compositions", query: "queries", control: "controls", template: "templates", viewer: "viewers" };
/** The member's path in the package: content and viewer files are named by their path already. */
const fileOf = (m: Member) => (m.role === "content" || m.role === "viewer-file" ? m.name : `${ROLE_DIR[m.role] ?? m.role}/${m.name}${m.role === "query" ? ".rq" : ".json"}`);

/**
 * Why a member name is not a plain path, or undefined. The one member-name rule of ktonpkg v0.9.9
 * (scope/profile.go memberNameProblem, checked there on write and on read; the cockpit relies on it): a relative
 * path of non-empty segments separated by "/"; no "." or ".." segment; no leading or trailing "/"; no backslash;
 * no colon anywhere (on NTFS "a:b" writes the alternate data stream b of a). The shell checks it again before it
 * writes any member to disk (F1 on #174). Same order and wording as ktonpkg.
 */
export function nameProblem(name: string): string | undefined {
  if (/[\\:]/.test(name)) return "has a backslash or a colon";
  for (const seg of name.split("/")) {
    if (seg === "") return "has an empty segment or a leading or trailing /";
    if (seg === "." || seg === "..") return `has a ${seg} segment`;
  }
  return undefined;
}

/** How many `ui cat` reads run at once. */
export const READ_WIDTH = 8;

/**
 * A package's member bytes, each re-verified against its admitted hash. One `ui cat` per member, READ_WIDTH at a
 * time; the first refusal (unreadable or tampered) stops the batch: no member is read after it. The multi-member
 * `ui cat` (cockpit #176) replaces `fetch` below and keeps the checks: one place to change.
 */
async function readMembers(p: UiPackage, read: ReadBlob): Promise<Map<Member, Uint8Array>> {
  const bytes = new Map<Member, Uint8Array>();
  const check = (m: Member, b: Uint8Array) => {
    // re-verify at load: the bytes must still be the bytes the package was admitted with
    if (`sha256:${sha(b)}` !== m.hash) throw new Refuse("package.tampered", fileOf(m), `${fileOf(m)} changed after admission: its bytes no longer hash to ${m.hash}`);
    bytes.set(m, b);
  };
  const fetch = async (m: Member): Promise<Uint8Array> => {
    try {
      return await read(p.package, m.hash);
    } catch (e) {
      throw new Refuse("package.unreadable", fileOf(m), `${fileOf(m)} cannot be read: ${(e as Error).message}`);
    }
  };
  const queue = [...p.members];
  let refused = false;
  await Promise.all(Array.from({ length: Math.min(READ_WIDTH, queue.length) }, async () => {
    for (let m = queue.shift(); m && !refused; m = queue.shift()) {
      try {
        check(m, await fetch(m));
      } catch (e) {
        refused = true;
        queue.length = 0;
        throw e;
      }
    }
  }));
  return bytes;
}

async function loadOne(p: UiPackage, read: ReadBlob, vendored: ReadonlyMap<string, ViewerTree>, byHash: Map<string, { pkg: string; hash: string; type: string }>): Promise<{ pkg: MirrorPackage; controls: Control[] }> {
  if (p.members.length === 0) throw new Refuse("package.members", "", "the cockpit lists no members for this package (ui list gives members only for a usable package, cockpit v0.5.4 or later): nothing can be loaded");
  // names first, before anything is read: a member name that is not a plain path refuses the package (names become
  // paths: viewer files, content, <role dir>/<name>.json, queries/<name>.rq). Exactly one exemption: cockpit lists a
  // package's alias file as the member {role: aliases, name: ""}.
  for (const m of p.members) {
    if (m.role === "aliases" && m.name === "") continue;
    const why = nameProblem(m.name);
    if (why) throw new Refuse(m.role === "viewer-file" ? "viewer.file" : "member.name", "", `member ${JSON.stringify(m.name)} (${m.role}) ${why}: the package is not loaded`);
  }
  const bytes = await readMembers(p, read);
  const of = (role: string) => p.members.filter((m) => m.role === role);
  const text = (m: Member) => Buffer.from(bytes.get(m) as Uint8Array).toString("utf8");
  const pkg: MirrorPackage = { name: p.name, id: p.package, revision: p.revision, blocks: {}, compositions: {}, queries: {}, content: {}, controls: {}, templates: {}, viewers: {}, leaves: {} };
  for (const m of of("query")) pkg.queries[m.name] = { path: `queries/${m.name}.rq`, hash: m.hash, text: text(m) };
  for (const m of of("template")) pkg.templates[m.name] = { decl: json(bytes.get(m) as Uint8Array, fileOf(m)) };
  for (const m of of("composition")) {
    // the composition's hash as ui list names it must be the member whose bytes were read
    const listed = p.compositions.find((c) => c["name"] === m.name)?.["hash"];
    if (listed !== undefined && listed !== m.hash) throw new Refuse("composition.hash", fileOf(m), `${fileOf(m)}: ui list names hash ${String(listed)}, the member is ${m.hash}`);
    pkg.compositions[m.name] = { decl: withForeignControls({ composition: m.name, ...json(bytes.get(m) as Uint8Array, fileOf(m)) }, p.name, fileOf(m)), hash: m.hash, text: text(m) };
  }
  for (const m of of("content")) {
    const type = typeOfPath(m.name);
    pkg.content[m.name] = BINARY.test(m.name) ? { path: m.name, hash: m.hash, type, binary: true } : { path: m.name, hash: m.hash, type, text: text(m) };
    byHash.set(m.hash, { pkg: p.package, hash: m.hash, type });
  }
  const controls: Control[] = [];
  for (const m of of("control")) {
    const d = json(bytes.get(m) as Uint8Array, fileOf(m));
    const say = d["say"] as { template?: unknown } | undefined;
    if (typeof say?.template !== "string") throw new Refuse("control.format", fileOf(m), `${fileOf(m)} says no template (the Control format is {kind, say: {template}, about})`);
    const kind = d["kind"] === undefined || d["kind"] === "" ? "decision" : d["kind"];
    if (kind !== "decision" && kind !== "record") throw new Refuse("control.format", fileOf(m), `${fileOf(m)}: kind ${JSON.stringify(kind)} is neither decision nor record`);
    pkg.controls[m.name] = { decl: d };
    controls.push({ package: p.name, name: m.name, template: say.template, kind, ...(typeof d["about"] === "string" ? { about: d["about"] } : {}), ...(typeof d["meaning"] === "string" ? { meaning: d["meaning"] } : {}), ...(typeof d["title"] === "string" && d["title"] ? { title: d["title"] } : {}) });
  }
  // viewer declarations (viewers/<name>.json: {viewer, at, tree}) and the trees the package carries
  const declared = new Map(of("viewer").map((m) => [m.name, json(bytes.get(m) as Uint8Array, fileOf(m))]));
  const carried = new Map<string, Map<string, Uint8Array>>();
  for (const m of of("viewer-file")) {
    // ktonpkg names a carried file viewers/<viewer>/<path>
    const [top, v, ...rest] = m.name.split("/");
    if (top !== "viewers") throw new Refuse("viewer.file", m.name, `${m.name} is not under viewers/`);
    if (!v || rest.length === 0) throw new Refuse("viewer.file", fileOf(m), `${fileOf(m)} is not inside a viewer folder`);
    if (!carried.has(v)) carried.set(v, new Map());
    carried.get(v)?.set(rest.join("/"), bytes.get(m) as Uint8Array);
  }
  // leaf viewers: a declaration pinned at a kton-web commit whose vendored viewer.json says frame: leaf
  for (const [dn, pin] of declared) {
    const file = `viewers/${dn}.json`;
    if (typeof pin["viewer"] !== "string" || typeof pin["at"] !== "string" || typeof pin["tree"] !== "string") throw new Refuse("viewer.missing", file, `${file} is not a pin {viewer, at, tree}`);
    if (!KTON_WEB_COMMIT.test(pin["at"])) continue;
    const v = vendored.get(pin["viewer"]);
    if (!v) throw new Refuse("viewer-pin-mismatch", file, `${file} pins viewer ${pin["viewer"]} at kton-web ${pin["at"].slice(0, 12)}, which this shell does not carry`);
    if (v.tree !== pin["tree"]) throw new Refuse("viewer-pin-mismatch", file, `${file} pins viewer ${pin["viewer"]} at tree ${pin["tree"]}; the tree here is ${v.tree}`);
    if (v.decl["frame"] === "leaf") pkg.leaves[dn] = { package: p.name, name: dn, viewer: pin["viewer"], decl: v.decl, tree: v.tree, dir: `kton-web/viewers/${pin["viewer"].replace(/^kton-web\//, "")}` };
  }
  for (const m of of("block")) {
    const decl = json(bytes.get(m) as Uint8Array, fileOf(m));
    const file = fileOf(m);
    const raw = decl["viewer"];
    const pin = (typeof raw === "string" ? declared.get(raw) : raw) as { viewer?: unknown; at?: unknown; tree?: unknown } | undefined;
    if (!pin || typeof pin.viewer !== "string" || typeof pin.at !== "string" || typeof pin.tree !== "string") throw new Refuse("viewer.missing", file, `${file} pins no viewer {viewer, at, tree}${typeof raw === "string" ? ` (viewers/${raw}.json)` : ""}`);
    const name = pin.viewer;
    let tree: ViewerTree;
    const folder = carried.get(name) ?? (typeof raw === "string" ? carried.get(raw) : undefined);
    if (folder) {
      const vj = folder.get("viewer.json");
      if (!vj) throw new Refuse("viewer.manifest", `viewers/${name}/viewer.json`, `the carried viewer ${name} has no viewer.json`);
      tree = { decl: json(vj, `viewers/${name}/viewer.json`), tree: treeId(folder), files: folder };
    } else if (KTON_WEB_COMMIT.test(pin.at)) {
      const v = vendored.get(name);
      if (!v) throw new Refuse("viewer-pin-mismatch", file, `${file} pins viewer ${name} at kton-web ${pin.at.slice(0, 12)}, which this shell does not carry`);
      tree = v;
    } else throw new Refuse("viewer.missing", file, `${file} pins viewer ${name} at ${pin.at}, and the package carries no viewers/${name}/`);
    if (tree.tree !== pin.tree) throw new Refuse("viewer-pin-mismatch", file, `${file} pins viewer ${name} at tree ${pin.tree}; the tree here is ${tree.tree}`);
    // a viewer.json names its viewer with the key `viewer`, as ktonpkg's pins do (kton-web's mirror refuses it too)
    if (typeof tree.decl["viewer"] !== "string" || tree.decl["viewer"] === "") throw new Refuse("viewer.manifest", file, `viewer ${name}'s viewer.json names no viewer (the key is "viewer")`);
    const module = tree.decl["module"];
    if (typeof module !== "string" || !tree.files.has(module)) throw new Refuse("viewer.module", file, `viewer ${name}'s viewer.json names module ${String(module)}, which its tree does not hold`);
    // install's check, here at load: the block emits and takes only what viewer.json declares, types equal
    const vEmits = (tree.decl["emits"] ?? {}) as Record<string, { from?: unknown; type?: unknown }>;
    for (const [ev, t] of Object.entries((decl["emits"] ?? {}) as Record<string, unknown>)) {
      const ve = vEmits[ev];
      if (!ve) throw new Refuse("viewer.emits", file, `${file}: event ${ev} is not one viewer ${name} emits`);
      if (typeKey(t) === null || typeKey(t) !== typeKey(ve.type)) throw new Refuse("viewer.emits", file, `${file}: event ${ev} is ${JSON.stringify(t)}, viewer ${name} says ${JSON.stringify(ve.type)}`);
    }
    const vAccepts = (tree.decl["accepts"] ?? {}) as Record<string, unknown>;
    for (const [opt, t] of Object.entries((decl["options"] ?? {}) as Record<string, unknown>)) {
      if (typeKey(t) === null || typeKey(t) !== typeKey(vAccepts[opt])) throw new Refuse("viewer.options", file, `${file}: option ${opt} is ${JSON.stringify(t)}, viewer ${name} accepts ${JSON.stringify(vAccepts[opt])}`);
    }
    const dir = folder ? `packages/${p.name}@${p.label}/viewers/${name}` : `kton-web/viewers/${name.replace(/^kton-web\//, "")}`;
    pkg.viewers[name] = { decl: tree.decl, tree: tree.tree, module: `${dir}/${module}`, dir, files: tree.files };
    pkg.blocks[m.name] = { decl: { ...decl, viewer: { viewer: name, at: pin.at, tree: pin.tree } }, hash: m.hash };
  }
  return { pkg, controls };
}

/** Load every admitted package of a listing. Never throws for a package: its refusal is listed instead. */
export async function loadPackages(listing: UiListing, read: ReadBlob, vendored: ReadonlyMap<string, ViewerTree> = new Map()): Promise<Loaded> {
  const packages: Record<string, MirrorPackage> = {};
  const entries: SlotEntry[] = [];
  const controls = new Map<string, Control>();
  const refusals: PackageRefusal[] = [];
  const notAdmitted: string[] = [];
  const byHash = new Map<string, Map<string, PackageFile>>();
  for (const p of listing.packages) {
    const who = p.name || p.dir;
    if (p.refused.length) {
      for (const r of p.refused) refusals.push({ ...r, package: who });
      continue;
    }
    if (!p.allowed) {
      notAdmitted.push(who);
      continue;
    }
    try {
      const own = new Map<string, { pkg: string; hash: string; type: string }>();
      const { pkg, controls: cs } = await loadOne(p, read, vendored, own);
      packages[p.name] = pkg;
      for (const c of cs) controls.set(`${c.package}/${c.name}`, c);
      // keyed by (hash, package): a second package with the same bytes does not take the first one's place
      for (const [k, v] of own) {
        if (!byHash.has(k)) byHash.set(k, new Map());
        byHash.get(k)?.set(v.pkg, v);
      }
      for (const e of p.ui) entries.push({ ...e, package: p.name, revision: p.revision });
    } catch (e) {
      if (!(e instanceof Refuse)) throw e;
      refusals.push({ package: who, file: e.file, code: e.code, reason: e.message });
    }
  }
  entries.sort((a, b) => (a.order ?? 0) - (b.order ?? 0) || a.package.localeCompare(b.package) || a.file.localeCompare(b.file));
  return { packages, entries, controls, refusals, notAdmitted, byHash };
}

/** Another package's sealed revision an act's control is in (ktonpkg v0.9.13 `from`: {package, at, head}). */
export interface ControlPin {
  readonly package: string;
  readonly at: string;
  readonly head: string;
}

/**
 * Acts on another package's control (ktonpkg v0.9.13): `{control: "<name>", from: {package, at, head}}` becomes
 * `{control: {package, name, pin}}`, the form the JS mirror carries through as it is (it knows only a control of the
 * composition's own package by name). The cockpit (#206) serves the view only while the owner is in use at that pin,
 * so the owner's control is among the loaded controls; the host refuses the act when it is not. A malformed pin
 * refuses the package.
 */
export function withForeignControls(decl: Record<string, unknown>, self: string, file: string): Record<string, unknown> {
  const act = (a: unknown): unknown => {
    if (!a || typeof a !== "object" || Array.isArray(a)) return a;
    const x = a as Record<string, unknown>;
    if (x["from"] === undefined) return a;
    const f = x["from"] as Record<string, unknown> | null;
    const ok = f && typeof f === "object" && typeof f["package"] === "string" && /^[A-Za-z][A-Za-z0-9_.-]*$/.test(f["package"]) && f["package"] !== self && typeof f["at"] === "string" && f["at"] !== "" && typeof f["head"] === "string" && /^sha256:[0-9a-f]{64}$/.test(f["head"]);
    if (!ok || typeof x["control"] !== "string" || !x["control"]) throw new Refuse("control.from", file, `${file}: an act's from must be {package (another package), at, head: sha256:<hex>} with a control name`);
    const { from: _from, ...rest } = x;
    const pin: ControlPin = { package: f["package"] as string, at: f["at"] as string, head: f["head"] as string };
    return { ...rest, control: { package: pin.package, name: x["control"], pin } };
  };
  const use = (u: unknown): unknown => {
    if (!u || typeof u !== "object" || Array.isArray(u)) return u;
    const x = u as Record<string, unknown>;
    return Array.isArray(x["acts"]) ? { ...x, acts: x["acts"].map(act) } : u;
  };
  const part = (pt: unknown): unknown => {
    if (!pt || typeof pt !== "object" || Array.isArray(pt)) return pt;
    const x = pt as Record<string, unknown>;
    return { ...x, ...(Array.isArray(x["blocks"]) ? { blocks: x["blocks"].map(use) } : {}), ...(Array.isArray(x["parts"]) ? { parts: x["parts"].map(part) } : {}) };
  };
  return { ...decl, ...(Array.isArray(decl["parts"]) ? { parts: decl["parts"].map(part) } : {}), ...(Array.isArray(decl["eachPart"]) ? { eachPart: decl["eachPart"].map(use) } : {}) };
}
