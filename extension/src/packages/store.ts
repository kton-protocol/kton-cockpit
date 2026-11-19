// Writing a package's viewer tree to the shell's store (globalStorage/viewers/<tree>). Every path comes from
// package data, so every path is checked to stay under the tree's folder, and all are checked before anything is
// written: a tree with one escaping path writes nothing (review on #174, F1; the load already refuses such names).

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve, sep } from "node:path";

/** The absolute path of `rel` under `dir`, or an error if it would land anywhere else. */
export function inside(dir: string, rel: string): string {
  const root = resolve(dir);
  const p = resolve(root, rel);
  if (!p.startsWith(root + sep)) throw new Error(`${JSON.stringify(rel)} is not inside the viewer store: nothing is written`);
  return p;
}

/** Write a checked tree: every path is checked first, then the files are written. */
export function writeTree(dir: string, files: ReadonlyMap<string, Uint8Array>): void {
  const targets = [...files].map(([rel, bytes]) => [inside(dir, rel), bytes] as const);
  for (const [p, bytes] of targets) {
    mkdirSync(dirname(p), { recursive: true });
    writeFileSync(p, bytes);
  }
}
