// What changed between two runs of a test: the commits that touched the test file or a helper it imports (transitively,
// relative .ts/.js/.mjs imports at the later commit), by read-only git over a mirror; each with how the test reaches it
// and the review that approved it.
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { changesBetween, joinRel, relativeImports, resolveImport } from "../src/ui/sources.js";
import { changesOptions, reviewOf, withChanges } from "../src/ui/annotate.js";

function repo() {
  const mirrors = mkdtempSync(join(tmpdir(), "mirrors-"));
  const dir = join(mirrors, "app");
  mkdirSync(join(dir, "e2e", "uat"), { recursive: true });
  mkdirSync(join(dir, "e2e", "lib"), { recursive: true });
  const git = (...a: string[]) => execFileSync("git", ["-C", dir, ...a], { encoding: "utf8", env: { ...process.env, GIT_AUTHOR_NAME: "Dev One", GIT_AUTHOR_EMAIL: "d@example.org", GIT_COMMITTER_NAME: "Dev One", GIT_COMMITTER_EMAIL: "d@example.org" } }).trim();
  const commit = (msg: string, files: Record<string, string>) => {
    for (const [p, t] of Object.entries(files)) writeFileSync(join(dir, p), t);
    git("add", "-A");
    git("commit", "-q", "-m", msg);
    return git("rev-parse", "HEAD");
  };
  git("init", "-q");
  const c0 = commit("base", {
    "e2e/uat/a.spec.ts": "import { test } from '../lib/fixtures';\nimport {\n  open,\n} from '../lib/explorer';\ntest('A-1', () => open());\n",
    "e2e/lib/fixtures.ts": "export { test } from '@playwright/test';\nimport './scenario.js';\n",
    "e2e/lib/scenario.ts": "export const s = 1;\n",
    "e2e/lib/explorer.ts": "import { s } from './deep/util';\nexport const open = () => s;\n",
    "e2e/lib/other.ts": "export const x = 1;\n",
  });
  mkdirSync(join(dir, "e2e", "lib", "deep"), { recursive: true });
  const c1 = commit("timing-safe lookups in the helpers", { "e2e/lib/scenario.ts": "export const s = 2;\n", "e2e/lib/deep/util.ts": "export const s = 3;\n" });
  const c2 = commit("an unrelated helper", { "e2e/lib/other.ts": "export const x = 2;\n" });
  const c3 = commit("the test waits longer", { "e2e/uat/a.spec.ts": "import { test } from '../lib/fixtures';\nimport {\n  open,\n} from '../lib/explorer';\ntest('A-1', async () => open());\n" });
  return { mirrors, c0, c1, c2, c3 };
}

test("relativeImports and resolveImport: static (also over lines), side-effect, dynamic, require; .js written for .ts; out of the repo: none", () => {
  assert.deepEqual(relativeImports("import a from './a';\nimport {\n x,\n} from \"../b\";\nimport './c.js';\nconst d = await import('./d');\nrequire('./e.mjs');\nimport x from 'pkg';"), ["./a", "../b", "./c.js", "./d", "./e.mjs"]);
  assert.equal(joinRel("e2e/uat/a.spec.ts", "../lib/x"), "e2e/lib/x");
  assert.equal(joinRel("a.ts", "../../x"), undefined);
  const files = new Set(["e2e/lib/x.ts", "e2e/lib/y/index.ts", "e2e/lib/z.ts"]);
  assert.equal(resolveImport("e2e/uat/a.spec.ts", "../lib/x", (p) => files.has(p)), "e2e/lib/x.ts");
  assert.equal(resolveImport("e2e/uat/a.spec.ts", "../lib/y", (p) => files.has(p)), "e2e/lib/y/index.ts");
  assert.equal(resolveImport("e2e/uat/a.spec.ts", "../lib/z.js", (p) => files.has(p)), "e2e/lib/z.ts");
  assert.equal(resolveImport("e2e/uat/a.spec.ts", "../lib/none", (p) => files.has(p)), undefined);
});

test("changesBetween: the commits that touched the test or a helper it imports (transitively), each touched file with how the test reaches it; an unrelated helper is not listed", async () => {
  const { mirrors, c0, c1, c3 } = repo();
  const r = await changesBetween(mirrors, "app", c0, c3, "e2e/uat/a.spec.ts");
  assert.ok(!("why" in r));
  if ("why" in r) return;
  assert.deepEqual(r.commits.map((c) => c.sha), [c3, c1], "newest first; the unrelated helper's commit is not there");
  assert.equal(r.watched, 5, "the test, fixtures, scenario, explorer and deep/util");
  assert.deepEqual(r.commits[0]?.touched, [{ path: "e2e/uat/a.spec.ts", via: "the test file" }]);
  assert.deepEqual(new Set(r.commits[1]?.touched.map((t) => `${t.path} · ${t.via}`)), new Set(["e2e/lib/scenario.ts · a helper the test imports via fixtures.ts", "e2e/lib/deep/util.ts · a helper the test imports via explorer.ts"]));
  assert.equal(r.commits[1]?.subject, "timing-safe lookups in the helpers");
  assert.equal(r.commits[1]?.author, "Dev One");
  assert.equal(r.capped, undefined);
  const shallow = await changesBetween(mirrors, "app", c0, c3, "e2e/uat/a.spec.ts", {}, { depth: 1, files: 60, commits: 50 });
  assert.ok(!("why" in shallow) && /imports followed 1 levels deep/.test(shallow.capped ?? ""), "a cap is said");
  const few = await changesBetween(mirrors, "app", c0, c3, "e2e/uat/a.spec.ts", {}, { depth: 4, files: 2, commits: 50 });
  assert.ok(!("why" in few) && /2 files watched/.test(few.capped ?? ""));
  assert.match(((await changesBetween(mirrors, "app", c0, "0".repeat(40), "e2e/uat/a.spec.ts")) as { why: string }).why, /has no commit/);
  assert.match(((await changesBetween(mirrors, "app", c0, c3, "../x")) as { why: string }).why, /not a plain repository path/);
  assert.match(((await changesBetween("", "app", c0, c3, "e2e/uat/a.spec.ts")) as { why: string }).why, /no mirrors configured/);
});

test("withChanges: rows naming a repo, two commits and a test get the commits with their review ('approved by … on …' or 'no review found') and a note", async () => {
  const { mirrors, c0, c1, c3 } = repo();
  const o = changesOptions({ changesBetween: "repo|before|now|file", changesReviews: "reviews" });
  assert.deepEqual(o, { repo: "repo", from: "before", to: "now", path: "file", reviews: "reviews" });
  assert.equal(changesOptions({ changesBetween: "a|b" }), undefined);
  const reviews = [{ approvedCommit: { value: c1.slice(0, 7) }, by: { value: "Reviewer" }, when: { value: "2026-11-21T09:00:00Z" } }];
  assert.equal(reviewOf(c1, reviews), "approved by Reviewer on 2026-11-21");
  assert.equal(reviewOf(c3, reviews), "no review found");
  const lit = (v: string) => ({ type: "literal", value: v });
  const ans = { head: { vars: ["test"] }, results: { bindings: [{ test: lit("A-1"), repo: lit("app"), before: lit(c0), now: lit(c3), file: lit("e2e/uat/a.spec.ts") }, { test: lit("B-1") }] } };
  const out = (await withChanges(ans, o as never, (repo, from, to, path) => changesBetween(mirrors, repo, from, to, path), reviews)) as typeof ans & { results: { bindings: Array<Record<string, { value: string }>> } };
  const list = JSON.parse(out.results.bindings[0]?.["changes"]?.value ?? "[]") as Array<{ sha: string; review: string; repo: string }>;
  assert.deepEqual(list.map((x) => [x.sha, x.review, x.repo]), [[c3, "no review found", "app"], [c1, "approved by Reviewer on 2026-11-21", "app"]]);
  assert.match(out.results.bindings[0]?.["changesNote"]?.value ?? "", /^2 commits between [0-9a-f]{7} and [0-9a-f]{7} touched the test or one of its 4 imported files$/);
  assert.equal(out.results.bindings[1]?.["changes"], undefined, "a row without the columns is left as it is");
  const none = (await withChanges(ans, o as never, undefined)) as typeof out;
  assert.equal(none.results.bindings[0]?.["changesNote"]?.value, "changes not shown: no sources on this host");
});

test("rows renders the changes: each commit opens its diff read-only, with its touched files and review", async () => {
  const { changesCell } = (await import("../media/workbench/viewers/rows/rows.js")) as unknown as { changesCell: (esc: (s: string) => string, json: string) => string };
  const esc = (s: string) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
  const html = changesCell(esc, JSON.stringify([{ sha: "a".repeat(40), subject: "helpers", author: "Dev One", date: "2026-11-21T10:00:00Z", repo: "app", touched: [{ path: "e2e/lib/scenario.ts", via: "a helper the test imports via fixtures.ts" }], review: "no review found" }]));
  assert.match(html, /data-src="\[&quot;app&quot;,&quot;a{40}&quot;\]"/);
  assert.match(html, />aaaaaaa<\/button> helpers <span class="muted">· Dev One · 2026-11-21<\/span>/);
  assert.match(html, /touched: scenario\.ts \(a helper the test imports via fixtures\.ts\)/);
  assert.match(html, /class="pill warn" data-review>no review found/);
  assert.equal(changesCell(esc, ""), "");
});
