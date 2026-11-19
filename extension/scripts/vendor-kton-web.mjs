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
export const COMMIT = "056e05a";
export const FILES = [
  // kton-web's licence (Apache-2.0 since f59275b, MH 19.11.)
  "LICENSE",
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
  // the sandboxed Mermaid frame (served from 127.0.0.1, H13) with Mermaid 11.4.1 (MIT), and the diagram block
  "mermaid/frame.html",
  "mermaid/frame.js",
  "vendor/mermaid/mermaid.min.js",
  "vendor/mermaid/LICENSE",
  "vendor/mermaid/VENDORED.json",
  "viewers/diagram/diagram.js",
  "viewers/diagram/viewer.json",
  // the graph viewer as a leaf (record-view), its imports and the kernel loader
  "viewers/graph/index.html",
  "viewers/graph/app.js",
  "viewers/graph/viewer.json",
  "core/kton.css",
  "core/logo.png",
  "core/annotate.js",
  "core/grouping.js",
  "core/join.js",
  "core/layout.js",
  "core/nav.js",
  "core/path.js",
  "core/profile.js",
  "core/query.js",
  "core/questions.js",
  "core/registry.js",
  "core/select.js",
  "core/selection.js",
  "core/viewstate.js",
  "runtime/kton.js",
  "runtime/wasm_exec.js",
  // built by kton-web's wasm/build.sh full (not in the repository): see WASM below
  "runtime/wasm/manifest.json",
  "runtime/wasm/kton-full.wasm",
];
/** kton-web's test package (S4.2-4/4.6), vendored unchanged into the e2e's package sources. */
export const TEST_PACKAGE = [
  "content/flow.mmd",
  "kit-viewers.json",
  "ui/blocks/diagram.json",
  "viewers/diagram/diagram.js",
  "viewers/diagram/viewer.json",
  "controls/test-decision.json",
  "controls/test-record.json",
  "manifest.json",
  "templates/test-note.json",
  "ui/blocks/needs.json",
  "ui/blocks/probe.json",
  "ui/compositions/test-shell.json",
  "ui/test-shell.json",
  "viewers/needs/needs.js",
  "viewers/needs/viewer.json",
  "viewers/probe/probe.js",
  "viewers/probe/viewer.json",
].map((f) => `tests/packages/kton-web-test/${f}`);

/** How runtime/wasm/ was built: kton-web's own recipe at COMMIT, with the kernel it pins. */
export const WASM = {
  recipe: "wasm/fetch-kernel.sh && GOFLAGS=-buildvcs=false wasm/build.sh full",
  kernel: "https://github.com/kton-protocol/kton e6d0bcaa0c7d0515f11789e4b3fd26a069a9cb3b (v0.4.0)",
  image: "golang:1.25@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80",
};

const HERE = dirname(fileURLToPath(import.meta.url));
const OUT = join(HERE, "..", "media", "kton-web");
const TEST_OUT = join(HERE, "..", "test", "e2e", "packages", "kton-web-test");
const outOf = (f) => (f.startsWith("tests/packages/kton-web-test/") ? join(TEST_OUT, f.slice("tests/packages/kton-web-test/".length)) : join(OUT, f));

const sha = (p) => createHash("sha256").update(readFileSync(p)).digest("hex");

export function check(out = OUT) {
  const pin = JSON.parse(readFileSync(join(out, "PIN.json"), "utf8"));
  const bad = [];
  if (!String(pin.commit).startsWith(COMMIT)) bad.push(`PIN.json pins ${pin.commit}, the script ${COMMIT}`);
  for (const f of [...FILES, ...TEST_PACKAGE]) {
    let got;
    try {
      got = sha(out === OUT ? outOf(f) : join(out, f));
    } catch {
      got = "missing";
    }
    if (got !== pin.files[f]) bad.push(`${f}: ${got} != ${pin.files[f]}`);
  }
  for (const f of Object.keys(pin.files)) if (!FILES.includes(f) && !TEST_PACKAGE.includes(f)) bad.push(`${f} is pinned but not vendored`);
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
    if (!head.startsWith(COMMIT)) throw new Error(`${arg} is at ${head}, not the pinned ${COMMIT}`);
    const files = {};
    for (const f of [...FILES, ...TEST_PACKAGE]) {
      mkdirSync(dirname(outOf(f)), { recursive: true });
      copyFileSync(join(arg, f), outOf(f));
      files[f] = sha(outOf(f));
    }
    writeFileSync(join(OUT, "PIN.json"), JSON.stringify({ repo: REPO, branch: BRANCH, commit: head, license: "Apache-2.0 (LICENSE)", wasm: WASM, files }, null, 2) + "\n");
    console.log(`vendored ${FILES.length} files of ${REPO}@${COMMIT}`);
  }
}
