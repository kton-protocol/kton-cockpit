// The viewers this shell carries from kton-web (media/kton-web/viewers/, vendored at the pinned commit): a
// package may pin one of them at a kton-web commit ({viewer, at: <40 hex>, tree}); the pin must name this
// tree. Read once at start; the tree id is computed here, never taken from a file.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { treeId, type ViewerTree } from "./model.js";

function files(dir: string): Map<string, Uint8Array> {
  const out = new Map<string, Uint8Array>();
  const walk = (d: string) => {
    for (const f of readdirSync(d)) {
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
