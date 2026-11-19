// Build the e2e's verticle packages with ktonpkg v0.9.5's scope CLI (cmd/scope), each sealed with a throwaway
// key that is deleted afterwards. Sources (test/e2e/packages/):
//   - kton-web-test/: kton-web's test package at the pinned kton-web commit, unchanged (vendored with the kit,
//     scripts/vendor-kton-web.mjs): a decision and a record control, a review-view entry (S4.2-4, S4.6).
//   - hostile/good/: ktonpkg v0.9.1's scope/testdata/hostile/good, with the table viewer it pins carried in
//     viewers/table/ (ktonpkg v0.9.1). The shell cases of S5.3 are built from it here:
//       hostile-good      the positive control
//       hostile-tampered  good, admitted, then tampered on disk by the seed (re-verify at load)
//       hostile-network   good, carrying viewer-trees/table-network (fetches example.org: the CSP blocks it)
//       hostile-pin       good, pinning a kton-web viewer at a commit with a tree no one has (refused at load)
//
//   node build-packages.mjs <scope CLI> <out dir>
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { cpSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const [scope, out] = process.argv.slice(2);
const HERE = dirname(fileURLToPath(import.meta.url));
const SRC = join(HERE, "packages");
const KTON_WEB_COMMIT = "39242b9f0bfce4f78d35f25d291c79877aaeeed5";
const keys = mkdtempSync(join(tmpdir(), "pkgkeys-"));

/** ktonpkg.TreeID of a folder. */
function treeId(dir) {
  const files = [];
  const walk = (d) => {
    for (const f of readdirSync(d)) {
      const p = join(d, f);
      if (statSync(p).isDirectory()) walk(p);
      else files.push(relative(dir, p).split("\\").join("/"));
    }
  };
  walk(dir);
  files.sort();
  const entries = files.map((p) => `{"hash":${JSON.stringify(`sha256:${createHash("sha256").update(readFileSync(join(dir, p))).digest("hex")}`)},"path":${JSON.stringify(p)}}`);
  return `sha256:${createHash("sha256").update(`{"files":[${entries.join(",")}]}`).digest("hex")}`;
}

function build(name, label, src) {
  const r = execFileSync(scope, ["verticle", join(out, name), name, label, "--src", src, "--key", join(keys, `${name}.key`)], { encoding: "utf8" });
  const j = JSON.parse(r);
  if (j.problems?.length) throw new Error(`${name}: ${j.problems.join("; ")}`);
  console.log(`${name}@${label} ${j.package}`);
}

function hostile(name, variant) {
  const src = mkdtempSync(join(tmpdir(), `${name}-`));
  cpSync(join(SRC, "hostile", "good"), src, { recursive: true });
  let pin;
  if (variant === "pin") {
    rmSync(join(src, "viewers", "table"), { recursive: true });
    pin = { viewer: "kton-web/table", at: KTON_WEB_COMMIT, tree: `sha256:${"0".repeat(64)}` };
  } else {
    if (variant === "network") cpSync(join(SRC, "hostile", "viewer-trees", "table-network", "index.js"), join(src, "viewers", "table", "index.js"));
    pin = { viewer: "table", at: `${name}@1`, tree: treeId(join(src, "viewers", "table")) };
  }
  // The cockpit's vocabulary is one namespace over every admitted package: two that offer the same template or
  // query name make it refuse every named query. Each case gets its own names (approves, items, item-card).
  const tag = name.replace(/^hostile-/, "");
  const NAMES = { approves: `${tag}-approves`, items: `${tag}-items`, "item-card": `${tag}-item-card` };
  const rename = (rel, to) => cpSync(join(src, rel), join(src, to));
  rename("templates/approves.json", `templates/${NAMES.approves}.json`);
  rmSync(join(src, "templates", "approves.json"));
  for (const q of ["items", "item-card"]) {
    rename(`queries/${q}.rq`, `queries/${NAMES[q]}.rq`);
    rmSync(join(src, "queries", `${q}.rq`));
  }
  for (const rel of ["templates/" + NAMES.approves + ".json", "controls/approve.json", "ui/review.json", "ui/compositions/list.json"]) {
    const t = readFileSync(join(src, rel), "utf8").replace(/"(approves|items|item-card)"/g, (_m, n) => JSON.stringify(NAMES[n]));
    writeFileSync(join(src, rel), t);
  }
  writeFileSync(join(src, "viewers", "table.json"), JSON.stringify(pin, null, 1));
  const block = JSON.parse(readFileSync(join(src, "ui", "blocks", "table.json"), "utf8"));
  block.viewer = pin;
  writeFileSync(join(src, "ui", "blocks", "table.json"), JSON.stringify(block, null, 1));
  // the review entry applies to a proposal (a claim); its query lists rdfs:labels, which every view has
  build(name, "1", src);
}

try {
  // kton-web-test as vendored, plus one review-view entry for its test-layout composition (the layout steps)
  const kwt = mkdtempSync(join(tmpdir(), "kton-web-test-"));
  cpSync(join(SRC, "kton-web-test"), kwt, { recursive: true });
  writeFileSync(join(kwt, "ui", "test-layout.json"), JSON.stringify({ slot: "review-view", label: "Test layout", when: { kind: "claim" }, composition: "test-layout", order: 11 }, null, 1));
  build("kton-web-test", "test", kwt);
  // a record-view entry with kton-web's graph viewer as a leaf, pinned at the vendored commit and tree
  build("workbench-graph", "1", join(SRC, "workbench-graph"));
  hostile("hostile-good", "good");
  hostile("hostile-tampered", "good");
  hostile("hostile-network", "network");
  hostile("hostile-pin", "pin");
} finally {
  rmSync(keys, { recursive: true, force: true });
}
