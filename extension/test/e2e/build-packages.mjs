// Build the e2e's verticle packages with ktonpkg v0.9.11's scope CLI (cmd/scope), each sealed with a throwaway
// key that is deleted afterwards. Sources (test/e2e/packages/):
//   - kton-web-test/: kton-web's test package at the pinned kton-web commit, unchanged (vendored with the kit,
//     scripts/vendor-kton-web.mjs): a decision and a record control, a review-view entry (S4.2-4, S4.6).
//   - hostile/good/: ktonpkg v0.9.1's scope/testdata/hostile/good, with the table viewer it pins carried in
//     viewers/table/ (ktonpkg v0.9.1). The shell cases of S5.3 are built from it here:
//       hostile-good      the positive control
//       hostile-tampered  good, admitted, then tampered on disk by the seed (re-verify at load)
//       hostile-network   good, carrying viewer-trees/table-network (fetches example.org: the CSP blocks it)
//       hostile-pin       good, pinning a kton-web viewer at a commit with a tree no one has (refused at load)
//   - s6/: the S6 packages (PR 3): qa.area-one and qa.area-two (two QM areas), qa.register (the document
//     register), qa.rules (findings). qa.rules-suppliers, which qa.area-two names, is not built (S6.13).
//   - dryrun1-triage/: qm-packages areas/dryrun1-triage (a143ac3 + the run-name patch): dryrun1.triage and dryrun2.triage.
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
  // The cases keep good/'s names (approves, items, item-card): since cockpit v0.5.3 the vocabulary is per
  // package, and a name several admitted packages bring is offered as <package>/<name>, which the shell asks for.
  writeFileSync(join(src, "viewers", "table.json"), JSON.stringify(pin, null, 1));
  const block = JSON.parse(readFileSync(join(src, "ui", "blocks", "table.json"), "utf8"));
  block.viewer = pin;
  writeFileSync(join(src, "ui", "blocks", "table.json"), JSON.stringify(block, null, 1));
  // the review entry applies to a proposal (a claim); its query lists rdfs:labels, which every view has
  build(name, "1", src);
}

try {
  build("kton-web-test", "test", join(SRC, "kton-web-test"));
  // a record-view entry with kton-web's graph viewer as a leaf, pinned at the vendored commit and tree
  build("workbench-graph", "1", join(SRC, "workbench-graph"));
  hostile("hostile-good", "good");
  hostile("hostile-tampered", "good");
  hostile("hostile-network", "network");
  hostile("hostile-pin", "pin");
  // PR 3 (S6): the generic QM area's test packages (userinterface qm-workbench/s6-packages, ktonpkg v0.9.3 form)
  for (const n of ["qa.register", "qa.area-one", "qa.area-two", "qa.rules"]) build(n, "1", join(SRC, "s6", n));
  // the decision view for the dry run: qm-packages areas/dryrun1-triage at a143ac3 with userinterface's run-name patch
  // (the view ships in the triage package; ktonpkg wants a control's template in its own package), built as
  // dryrun1.triage, and as dryrun2.triage with its scope labels rewritten as build.sh RUN=dryrun2 does
  for (const run of ["dryrun1", "dryrun2"]) {
    const dm = mkdtempSync(join(tmpdir(), `${run}-`));
    cpSync(join(SRC, "dryrun1-triage"), dm, { recursive: true });
    if (run !== "dryrun1") {
      const files = [...readdirSync(join(dm, "queries")).filter((f) => /^dm-.*\.rq$|^triage-judge-agreement\.rq$/.test(f)).map((f) => join(dm, "queries", f)), join(dm, "ui", "review.json"), join(dm, "ui", "compositions", "decision.json")];
      for (const f of files) {
        const t = readFileSync(f, "utf8").replace(/dryrun1\.(mapping|triage|runs)\b/g, `${run}.$1`).replace(/Dry run: dryrun1/g, `Dry run: ${run}`).replace(/No data for dryrun1/g, `No data for ${run}`).replace(/\(dryrun1\)/g, `(${run})`);
        if (/dryrun1\.(mapping|triage|runs)/.test(t)) throw new Error(`${f}: a scope label still names dryrun1`);
        writeFileSync(f, t);
      }
    }
    build(`${run}.triage`, run, dm);
  }
} finally {
  rmSync(keys, { recursive: true, force: true });
}
