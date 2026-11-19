// Vendor kton-web's composition kit at a pinned commit into media/kton-web/ (C1.5 v3: every host vendors
// core/compose.js and core/host.js; nothing is fetched at run time).
//
//   node scripts/vendor-kton-web.mjs <kton-web checkout at the pinned commit>
//   node scripts/vendor-kton-web.mjs --check      (what `npm test` does: the files match PIN.json)
//
// Only the files below are taken, unchanged. PIN.json records the commit and each file's sha256; a
// unit test fails while a vendored file differs from its pin. To move the pin: check out the new commit,
// change COMMIT, run this script, and review the diff of media/kton-web/.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const REPO = "https://github.com/gitmick/kton-web";
export const BRANCH = "compose-v3";
export const COMMIT = "39242b9f0bfce4f78d35f25d291c79877aaeeed5";
export const FILES = [
  // the v3 bridge, the renderer and its kit
  "core/host.js",
  "core/origins.js",
  "core/compose.js",
  "core/compose-kit.js",
  "core/compose.css",
  "core/mermaid-client.js",
  "core/leaf.js",
  // the ktonpkg JS mirror (compose → query → fill) until ktonpkg v0.10.0 ships the binding
  "core/ktonpkg-mirror/plan.js",
  "core/ktonpkg-mirror/sparql.js",
  "core/ktonpkg-mirror/packages.js",
  "core/ktonpkg-mirror/PIN.md",
  // the composition frame
  "viewers/compose/frame.js",
  "viewers/compose/index.html",
];

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = join(HERE, "..", "media", "kton-web");
const sha = (p) => createHash("sha256").update(readFileSync(p)).digest("hex");

export function check(out = OUT) {
  const pin = JSON.parse(readFileSync(join(out, "PIN.json"), "utf8"));
  const bad = [];
  if (pin.commit !== COMMIT) bad.push(`PIN.json pins ${pin.commit}, the script ${COMMIT}`);
  for (const f of FILES) {
    let got;
    try {
      got = sha(join(out, f));
    } catch {
      got = "missing";
    }
    if (got !== pin.files[f]) bad.push(`${f}: ${got} != ${pin.files[f]}`);
  }
  for (const f of Object.keys(pin.files)) if (!FILES.includes(f)) bad.push(`${f} is pinned but not vendored`);
  return bad;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const arg = process.argv[2];
  if (arg === "--check") {
    const bad = check();
    if (bad.length) {
      console.error(bad.join("\n"));
      process.exit(1);
    }
    console.log(`media/kton-web matches ${COMMIT}`);
  } else {
    const head = execFileSync("git", ["-C", arg, "rev-parse", "HEAD"], { encoding: "utf8" }).trim();
    if (head !== COMMIT) throw new Error(`${arg} is at ${head}, not the pinned ${COMMIT}`);
    const files = {};
    for (const f of FILES) {
      mkdirSync(dirname(join(OUT, f)), { recursive: true });
      copyFileSync(join(arg, f), join(OUT, f));
      files[f] = sha(join(OUT, f));
    }
    writeFileSync(join(OUT, "PIN.json"), JSON.stringify({ repo: REPO, branch: BRANCH, commit: COMMIT, files }, null, 2) + "\n");
    console.log(`vendored ${FILES.length} files of ${REPO}@${COMMIT}`);
  }
}
