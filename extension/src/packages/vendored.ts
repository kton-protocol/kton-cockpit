// The viewers this shell carries from kton-web (media/kton-web/viewers/, vendored at the pinned commit): a
// package may pin one of them at a kton-web commit ({viewer, at: <40 hex>, tree}); the pin must name this
// tree. Read once at start; the tree id is computed here, never taken from a file.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { treeId, type MirrorPackage, type ViewerTree } from "./model.js";

/** The built-in package's name: not a ktonpkg package name (those have no "@"), so it never clashes. */
export const BUILTIN = "@workbench";

function files(dir: string): Map<string, Uint8Array> {
  const out = new Map<string, Uint8Array>();
  const walk = (d: string) => {
    for (const f of readdirSync(d).sort()) {
      const p = join(d, f);
      if (statSync(p).isDirectory()) walk(p);
      else out.set(relative(dir, p).split("\\").join("/"), readFileSync(p));
    }
  };
  walk(dir);
  return out;
}

/** viewer name (viewer.json "viewer", e.g. kton-web/graph) → its tree; folders without a viewer.json are not viewers. */
export function loadVendored(kit: string): Map<string, ViewerTree> {
  const out = new Map<string, ViewerTree>();
  const root = join(kit, "viewers");
  for (const v of readdirSync(root)) {
    const fs = files(join(root, v));
    const vj = fs.get("viewer.json");
    if (!vj) continue;
    const decl = JSON.parse(Buffer.from(vj).toString("utf8")) as Record<string, unknown>;
    if (typeof decl["viewer"] !== "string") continue;
    out.set(decl["viewer"], { decl, tree: treeId(fs), files: fs });
  }
  return out;
}

/** The workbench's blocks: one per viewer under media/workbench/viewers, its options typed as the viewer accepts. */
export function loadBuiltin(dir: string): MirrorPackage {
  const viewers: Record<string, MirrorPackage["viewers"][string]> = {};
  const blocks: Record<string, MirrorPackage["blocks"][string]> = {};
  for (const v of readdirSync(join(dir, "viewers")).sort()) {
    const fs = files(join(dir, "viewers", v));
    const decl = JSON.parse(Buffer.from(fs.get("viewer.json") as Uint8Array).toString("utf8")) as Record<string, unknown>;
    const name = String(decl["viewer"]);
    const tree = treeId(fs);
    const module = String(decl["module"]);
    if (!fs.has(module)) throw new Error(`built-in viewer ${name} names module ${module}, which its folder does not hold`);
    viewers[name] = { decl, tree, module: `workbench/viewers/${v}/${module}`, dir: `workbench/viewers/${v}`, files: fs };
    const emits = Object.fromEntries(Object.entries((decl["emits"] ?? {}) as Record<string, { type: unknown }>).map(([k, e]) => [k, e.type]));
    blocks[name] = { decl: { block: name, viewer: { viewer: name, at: `${BUILTIN}@builtin`, tree }, requires: [], accepts: [], options: decl["accepts"] ?? {}, emits }, hash: `builtin:${tree}` };
  }
  return { name: BUILTIN, id: BUILTIN, revision: "builtin", blocks, compositions: {}, queries: {}, content: {}, controls: {}, templates: {}, viewers, leaves: {} };
}

