// Admitted packages and input bundles, read through a host-given reader. Plain ES module; the same in
// Node and the browser.
//
// A folder carries manifest.json ({package, members: [{path, role, sha256}]}), written by
// tools/manifest.mjs: a stand-in for ktonpkg's member list. Text members are read and their hash
// checked. Binary content (images) is NOT read here: plan takes its hash from the manifest, and the host
// checks the bytes against it when it answers content@1.
//
//   loadPackages(["qm-core", "qm-demo"], read, sha256)
//     read(path)   → Promise<string>   (path relative to the packages folder)
//     sha256(text) → Promise<hex>

import { typeKey } from "./plan.js";

const ROLE_KEY = { block: "blocks", composition: "compositions", query: "queries", content: "content", control: "controls", template: "templates", viewer: "viewers", "viewer-file": "viewers", chapters: "chapters" };
export const INPUT_SCHEMA = "kton.dev/composition-input/v0";
const BINARY = /\.(png|jpe?g|gif|webp|pdf)$/i;
const TYPES = { png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp", pdf: "application/pdf", md: "text/markdown", json: "application/json", mmd: "text/vnd.mermaid" };

export const isBinary = (path) => BINARY.test(path);
export const typeOfPath = (path) => TYPES[path.split(".").pop().toLowerCase()] ?? "application/octet-stream";

/**
 * ktonpkg.TreeID of a viewer folder (ktonpkg answer, round 4 F25): per file {path, hash: "sha256:<hex>"},
 * sorted by path; sha256 over the canonical JSON {"files":[...]} (keys sorted, no whitespace, no trailing
 * newline). `files` maps the slash path relative to the folder to the sha256 hex of the file's bytes.
 * ktonpkg skips .git/, bin/, *~ and *.exe; the viewer folders here hold none.
 */
export async function treeHash(files, sha256) {
  const entries = Object.keys(files)
    .sort((a, b) => (a < b ? -1 : a > b ? 1 : 0))
    .map((p) => `{"hash":${JSON.stringify("sha256:" + String(files[p]).replace(/^sha256:/, ""))},"path":${JSON.stringify(p)}}`);
  return `sha256:${await sha256(`{"files":[${entries.join(",")}]}`)}`;
}

export async function loadPackages(names, read, sha256) {
  const out = {};
  for (const name of names) out[name] = await loadBundle(name, (p) => read(`${name}/${p}`), sha256);
  return out;
}

/** One folder with a manifest: a package, or a document's chapters bundle (an input). */
export async function loadBundle(name, read, sha256) {
  const manifest = JSON.parse(await read("manifest.json"));
  if (manifest.package !== name) throw new Error(`${name}/manifest.json names ${manifest.package}`);
  const pkg = { name, blocks: {}, compositions: {}, queries: {}, content: {}, controls: {}, templates: {}, viewers: {}, chapters: null };
  const viewerFiles = {};
  for (const m of manifest.members) {
    const key = ROLE_KEY[m.role];
    if (!key) throw new Error(`${name}/${m.path}: unknown role ${m.role}`);
    if (m.role === "content" && isBinary(m.path)) {
      pkg.content[m.path] = { path: m.path, hash: `sha256:${m.sha256}`, binary: true, type: typeOfPath(m.path) };
      continue;
    }
    const text = await read(m.path);
    const hex = await sha256(text);
    if (hex !== m.sha256) throw new Error(`${name}/${m.path}: hash ${hex} differs from the manifest`);
    const entry = { path: m.path, hash: `sha256:${hex}`, text };
    if (m.role === "content") pkg.content[m.path] = { ...entry, type: typeOfPath(m.path) };
    else if (m.role === "chapters") pkg.chapters = { ...entry, decl: JSON.parse(text) };
    else if (m.role === "viewer" || m.role === "viewer-file") {
      const [, v, ...rest] = m.path.split("/");
      (viewerFiles[v] ??= {})[rest.join("/")] = hex;
      if (m.role === "viewer") pkg.viewers[v] = { ...pkg.viewers[v], ...entry, decl: JSON.parse(text) };
    }
    else {
      const base = m.path.split("/").pop().replace(/\.(json|rq)$/, "");
      pkg[key][base] = m.role === "query" ? entry : { ...entry, decl: JSON.parse(text) };
    }
  }
  for (const [v, files] of Object.entries(viewerFiles)) {
    const decl = pkg.viewers[v]?.decl;
    if (!decl) throw new Error(`${name}/viewers/${v}: no viewer.json`);
    if (typeof decl.module !== "string" || !(decl.module in files)) throw new Error(`${name}/viewers/${v}: viewer.json names module ${decl.module}, which the folder does not hold`);
    pkg.viewers[v].tree = await treeHash(files, sha256);
    pkg.viewers[v].module = `packages/${name}/viewers/${v}/${decl.module}`;
  }
  // install's check (ktonpkg round 5/5b): a block's events and typed options are a subset of its viewer's,
  // with equal types; the viewer gives each event its source (intent.<kind> or state.<key>)
  for (const [bn, b] of Object.entries(pkg.blocks)) {
    const v = pkg.viewers[b.decl.viewer?.viewer]?.decl;
    if (!v) continue; // a viewer from elsewhere (kton-web) is checked when its tree is fetched
    for (const [ev, t] of Object.entries(b.decl.emits ?? {})) {
      const ve = v.emits?.[ev];
      if (!ve) throw new Error(`${name}/ui/blocks/${bn}.json: event ${ev} is not one viewer ${v.viewer} emits`);
      if (typeKey(t) === null || typeKey(t) !== typeKey(ve.type)) throw new Error(`${name}/ui/blocks/${bn}.json: event ${ev} is ${JSON.stringify(t)}, viewer ${v.viewer} says ${JSON.stringify(ve.type)}`);
      if (!/^(intent|state)\.\w+$/.test(ve.from ?? "")) throw new Error(`${name}/viewers/${v.viewer}/viewer.json: event ${ev} has no source`);
    }
    for (const [opt, t] of Object.entries(b.decl.options ?? {})) {
      if (Array.isArray(b.decl.options)) throw new Error(`${name}/ui/blocks/${bn}.json: options are typed ({name: type})`);
      if (typeKey(t) === null || typeKey(t) !== typeKey(v.options?.[opt])) throw new Error(`${name}/ui/blocks/${bn}.json: option ${opt} is ${JSON.stringify(t)}, viewer ${v.viewer} says ${JSON.stringify(v.options?.[opt])}`);
    }
  }
  return pkg;
}

/**
 * A bound input (a document's chapters): {schema, class, files: {path: "sha256:…"}} (ktonpkg Round 3, F16).
 * The manifest's own hash is what plan records; every text file is read and checked against it; binary
 * content is checked by the host when it answers content@1.
 */
export async function loadInput(name, read, sha256) {
  const raw = await read("manifest.json");
  const manifest = JSON.parse(raw);
  if (manifest.schema !== INPUT_SCHEMA) throw new Error(`${name}/manifest.json: schema ${manifest.schema} is not ${INPUT_SCHEMA}`);
  if (typeof manifest.class !== "string") throw new Error(`${name}/manifest.json names no class`);
  const input = { name, manifest: { hash: `sha256:${await sha256(raw)}`, class: manifest.class }, chapters: null, content: {}, queries: {}, blocks: {}, compositions: {} };
  for (const [path, hash] of Object.entries(manifest.files ?? {})) {
    if (!/^sha256:[0-9a-f]{64}$/.test(hash)) throw new Error(`${name}/${path}: not a sha256 hash`);
    if (path.startsWith("content/") && isBinary(path)) {
      input.content[path] = { path, hash, binary: true, type: typeOfPath(path) };
      continue;
    }
    const text = await read(path);
    const got = `sha256:${await sha256(text)}`;
    if (got !== hash) throw new Error(`${name}/${path}: hash ${got} differs from the input manifest`);
    if (path === "chapters.json") input.chapters = { path, hash, text, decl: JSON.parse(text) };
    else if (path.startsWith("content/")) input.content[path] = { path, hash, text, type: typeOfPath(path) };
    else if (/^queries\/[^/]+\.rq$/.test(path)) input.queries[path.slice(8, -3)] = { path, hash, text };
    else throw new Error(`${name}/${path}: not a chapters, content or query file`);
  }
  if (!input.chapters) throw new Error(`${name}: the input manifest lists no chapters.json`);
  return input;
}

/** The role a member path has (ktonpkg's member roles as used here). */
export function roleOf(path) {
  if (/^ui\/blocks\/[^/]+\.json$/.test(path)) return "block";
  if (/^ui\/compositions\/[^/]+\.json$/.test(path)) return "composition";
  if (/^viewers\/[^/]+\/viewer\.json$/.test(path)) return "viewer";
  if (/^viewers\/[^/]+\/.+$/.test(path)) return "viewer-file";
  if (/^queries\/[^/]+\.rq$/.test(path)) return "query";
  if (/^controls\/[^/]+\.json$/.test(path)) return "control";
  if (/^templates\/[^/]+\.json$/.test(path)) return "template";
  if (path === "chapters.json") return "chapters";
  if (/^content\//.test(path)) return "content";
  return null;
}
