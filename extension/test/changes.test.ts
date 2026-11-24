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

test("reviewedState: from head back to the newest proper ancestor with an approval of its own; each commit after it with its review or none; a later commit's approval does not cover its parent", async () => {
  const { reviewedState } = await import("../src/ui/sources.js");
  const { requireReviewedOf, reviewedLines } = await import("../src/ui/annotate.js");
  const { mirrors, c0, c1, c2, c3 } = repo();
  const approved = new Map([[c0, "approved by Reviewer on 2026-11-20"], [c3, "approved by Reviewer on 2026-11-23"]]);
  const s = await reviewedState(mirrors, "app", c3, (sha) => approved.get(sha));
  assert.ok(!("why" in s));
  if ("why" in s) return;
  assert.equal(s.base, c0, "the last reviewed state");
  assert.deepEqual(s.commits.map((c) => [c.sha, c.review ?? "none"]), [[c3, "approved by Reviewer on 2026-11-23"], [c2, "none"], [c1, "none"]], "head's approval does not cover its parents");
  assert.deepEqual(s.missing, [c2, c1]);
  const l = reviewedLines(s, "main");
  assert.equal(l.blocked, `2 commits under this rerun have no review: ${c2.slice(0, 7)}, ${c1.slice(0, 7)}`);
  assert.match(l.lines[0] ?? "", /^main: 3 commits under this rerun$/);
  assert.ok(l.lines.includes(`${c2.slice(0, 7)} an unrelated helper: no review`));
  assert.equal(l.lines.at(-1), `since ${c0.slice(0, 7)}`);
  // a base (the state the last run ran at): every commit after it, even when a later one is approved
  const fromRun = await reviewedState(mirrors, "app", c3, (sha) => approved.get(sha), {}, 50, c1);
  assert.ok(!("why" in fromRun) && fromRun.base === c1 && JSON.stringify(fromRun.commits.map((c) => c.sha)) === JSON.stringify([c3, c2]) && JSON.stringify(fromRun.missing) === JSON.stringify([c2]));
  assert.match(((await reviewedState(mirrors, "app", c1, () => undefined, {}, 50, c3)) as { why: string }).why, /is not an ancestor/);
  const all = await reviewedState(mirrors, "app", c3, (sha) => (sha === c2 || sha === c1 ? "approved by R on 2026-11-21" : undefined));
  assert.ok(!("why" in all) && all.base === c2 && all.missing.length === 1, "head itself without a review");
  const none = await reviewedState(mirrors, "app", c3, () => undefined, {}, 2);
  assert.ok(!("why" in none) && none.base === undefined && /no reviewed ancestor within 2 commits/.test(none.capped ?? ""), "no reviewed ancestor within the cap: said");
  assert.match(((await reviewedState(mirrors, "app", "no-such-branch", () => undefined)) as { why: string }).why, /has no no-such-branch/);
  assert.match(((await reviewedState(mirrors, "app", "-x", () => undefined)) as { why: string }).why, /not plain/);
  assert.equal(reviewedLines({ commits: [{ sha: c3, subject: "s", author: "a", date: "d", review: "approved by R on 2026-11-23" }], missing: [], base: c2 }, "main").blocked, undefined);
  assert.deepEqual(requireReviewedOf({ requireReviewed: "rerun-request:app|main|reviews" }), { control: "rerun-request", repo: "app", head: "main", reviews: "reviews" });
  assert.deepEqual(requireReviewedOf({ requireReviewed: "app|main|reviews|last-run" }), { repo: "app", head: "main", reviews: "reviews", base: "last-run" });
  assert.equal(requireReviewedOf({ requireReviewed: "app|main" }), undefined);
});

test("withChanges: full ids are cached across refreshes (git runs once); a branch name is asked every time", async () => {
  const lit = (v: string) => ({ type: "literal", value: v });
  const row = (from: string, to: string) => ({ head: { vars: [] }, results: { bindings: [{ repo: lit("app"), before: lit(from), now: lit(to), file: lit("t.ts") }] } });
  const opts = { repo: "repo", from: "before", to: "now", path: "file" } as const;
  let calls = 0;
  const fn = async () => { calls++; return { commits: [], watched: 1 } as never; };
  const [a, b] = ["a".repeat(40), "b".repeat(40)];
  const ask = (from: string, to: string) => withChanges(row(from, to), opts as never, fn);
  await ask(a, b);
  await ask(a, b);
  assert.equal(calls, 1, "the second refresh with full ids does not run git again");
  await ask("main", b);
  await ask("main", b);
  assert.equal(calls, 3, "a branch name is not cached");
  await ask(a.slice(0, 7), b);
  await ask(a.slice(0, 7), b);
  assert.equal(calls, 5, "a short id is not cached");
});

function scratch() {
  const mirrors = mkdtempSync(join(tmpdir(), "mirrors-"));
  const dir = join(mirrors, "app");
  mkdirSync(dir, { recursive: true });
  const git = (env: Record<string, string>, ...a: string[]) => execFileSync("git", ["-C", dir, ...a], { encoding: "utf8", env: { ...process.env, GIT_AUTHOR_NAME: "Dev One", GIT_AUTHOR_EMAIL: "d@example.org", GIT_COMMITTER_NAME: "Dev One", GIT_COMMITTER_EMAIL: "d@example.org", ...env } }).trim();
  git({}, "init", "-q", "-b", "main");
  const commit = (msg: string, files: Record<string, string>, date: string) => {
    for (const [p, t] of Object.entries(files)) writeFileSync(join(dir, p), t);
    git({}, "add", "-A");
    git({ GIT_AUTHOR_DATE: date, GIT_COMMITTER_DATE: date }, "commit", "-q", "-m", msg);
    return git({}, "rev-parse", "HEAD");
  };
  return { mirrors, git, commit, dir };
}

test("reviewedState without a base: a side commit of a merge that is older than the reviewed one is still listed and blocks", async () => {
  const { reviewedState } = await import("../src/ui/sources.js");
  const { reviewedLines } = await import("../src/ui/annotate.js");
  const { mirrors, git, commit } = scratch();
  const c0 = commit("base", { "a.txt": "0\n" }, "2026-11-01T10:00:00Z");
  git({}, "checkout", "-q", "-b", "side");
  const s = commit("side work, backdated", { "s.txt": "1\n" }, "2026-11-02T10:00:00Z");
  git({}, "checkout", "-q", "main");
  const r = commit("main work, reviewed", { "m.txt": "1\n" }, "2026-11-10T10:00:00Z");
  git({ GIT_AUTHOR_DATE: "2026-11-11T10:00:00Z", GIT_COMMITTER_DATE: "2026-11-11T10:00:00Z" }, "merge", "-q", "--no-ff", "-m", "merge side", "side");
  const m = git({}, "rev-parse", "HEAD");
  const approved = new Map([[c0, "approved by Reviewer on 2026-11-01"], [r, "approved by Reviewer on 2026-11-10"], [m, "approved by Reviewer on 2026-11-11"]]);
  const st = await reviewedState(mirrors, "app", m, (sha) => approved.get(sha));
  assert.ok(!("why" in st));
  if ("why" in st) return;
  assert.equal(st.base, r, "the newest approved proper ancestor");
  assert.ok(st.commits.map((c) => c.sha).includes(s), "the side commit is listed");
  assert.deepEqual(st.missing, [s], "only the side commit has no review");
  assert.match(reviewedLines(st, "main").blocked ?? "", /1 commit under this rerun has no review/);
});

test("changesBetween: a path with glob characters watches only that file", async () => {
  const { changesBetween } = await import("../src/ui/sources.js");
  const { mirrors, commit } = scratch();
  const c0 = commit("base", { "a1.spec.ts": "x\n", "a[1].spec.ts": "y\n", "b.spec.ts": "z\n" }, "2026-11-01T10:00:00Z");
  const c1 = commit("touches the sibling only", { "a1.spec.ts": "x2\n" }, "2026-11-02T10:00:00Z");
  const c2 = commit("touches the test", { "a[1].spec.ts": "y2\n" }, "2026-11-03T10:00:00Z");
  const r = await changesBetween(mirrors, "app", c0, c2, "a[1].spec.ts");
  assert.ok(!("why" in r));
  if ("why" in r) return;
  assert.deepEqual(r.commits.map((c) => c.sha), [c2], "the sibling a1.spec.ts is not matched by [1]");
  void c1;
});
