// A package source folder (test/e2e/packages/…) as `ui list` would list it once installed: members by role,
// name and content hash (as ktonpkg names them), the ui entries, blocks and compositions, and the blobs by
// hash. The same shape test/e2e/uilist-fixture writes from a real installed package.
import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import type { Member, UiPackage } from "../src/packages/listing.js";

export const PKGS = join(process.cwd(), "test", "e2e", "packages");
const hash = (b: Uint8Array) => `sha256:${createHash("sha256").update(b).digest("hex")}`;

function files(dir: string): string[] {
  const out: string[] = [];
  const walk = (d: string) => {
    for (const f of readdirSync(d)) {
      const p = join(d, f);
      if (statSync(p).isDirectory()) walk(p);
      else out.push(relative(dir, p).split("\\").join("/"));
    }
  };
  walk(dir);
  return out.sort();
}

function roleOf(p: string): { role: string; name: string } | undefined {
  let m: RegExpExecArray | null;
  if ((m = /^ui\/blocks\/([^/]+)\.json$/.exec(p))) return { role: "block", name: m[1] as string };
  if ((m = /^ui\/compositions\/([^/]+)\.json$/.exec(p))) return { role: "composition", name: m[1] as string };
  if ((m = /^ui\/([^/]+)\.json$/.exec(p))) return { role: "ui", name: m[1] as string };
  if ((m = /^viewers\/([^/]+)\.json$/.exec(p))) return { role: "viewer", name: m[1] as string };
  if (/^viewers\/[^/]+\/.+$/.test(p)) return { role: "viewer-file", name: p };
  if ((m = /^queries\/([^/]+)\.rq$/.exec(p))) return { role: "query", name: m[1] as string };
  if ((m = /^controls\/([^/]+)\.json$/.exec(p))) return { role: "control", name: m[1] as string };
  if ((m = /^templates\/([^/]+)\.json$/.exec(p))) return { role: "template", name: m[1] as string };
  if (/^content\//.test(p)) return { role: "content", name: p };
  return undefined;
}

export interface Fixture {
  readonly pkg: UiPackage;
  readonly blobs: Map<string, Uint8Array>;
}

/** List a package folder; `edit` may change a file's bytes before hashing (a variant). */
export function listed(dir: string, name: string, label = "1", edit: (path: string, b: Buffer) => Buffer = (_p, b) => b): Fixture {
  const blobs = new Map<string, Uint8Array>();
  const members: Member[] = [];
  const ui: UiPackage["ui"][number][] = [];
  const blocks: Record<string, unknown>[] = [];
  const compositions: Record<string, unknown>[] = [];
  for (const p of files(dir)) {
    const r = roleOf(p);
    if (!r) continue;
    const b = edit(p, readFileSync(join(dir, p)));
    const h = hash(b);
    blobs.set(h, b);
    members.push({ ...r, hash: h });
    const j = () => JSON.parse(b.toString("utf8")) as Record<string, unknown>;
    if (r.role === "ui") ui.push({ ...(j() as unknown as UiPackage["ui"][number]), file: p });
    if (r.role === "block") blocks.push({ ...j(), name: r.name });
    if (r.role === "composition") compositions.push({ name: r.name, kind: j()["kind"], hash: h });
  }
  return {
    pkg: { name, package: hash(Buffer.from(name)), revision: hash(Buffer.from(`${name}@${label}`)), label, dir: `.kton/packages/${name}@${label}`, allowed: true, ui, blocks, compositions, refused: [], members },
    blobs,
  };
}

/** A blob reader over fixtures (as CliTransport.packageBlob reads blobs/<hh>/<hex>). */
export function reader(...fx: Fixture[]): (dir: string, h: string) => Promise<Uint8Array> {
  return async (dir, h) => {
    const f = fx.find((x) => x.pkg.dir === dir);
    const b = f?.blobs.get(h);
    if (!b) throw new Error(`no blob ${h} in ${dir}`);
    return b;
  };
}
