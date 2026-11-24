// The requireReviewed gate fails closed: a merge, a cap, a short id, and several rows for one target.
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { reviewedState } from "../src/ui/sources.js";
import { reviewOf, reviewedLines, reviewedSha } from "../src/ui/annotate.js";
import { CompositionHost, type Backend, type Plan } from "../src/composition/host.js";
import type { Control, Loaded } from "../src/packages/model.js";

function mk(name: string) {
  const mirrors = mkdtempSync(join(tmpdir(), "gate-"));
  const dir = join(mirrors, name);
  mkdirSync(dir);
  const g = (args: string[], date?: string) => execFileSync("git", ["-C", dir, ...args], { encoding: "utf8", env: { ...process.env, GIT_AUTHOR_NAME: "t", GIT_AUTHOR_EMAIL: "t@example.org", GIT_COMMITTER_NAME: "t", GIT_COMMITTER_EMAIL: "t@example.org", ...(date ? { GIT_AUTHOR_DATE: date, GIT_COMMITTER_DATE: date } : {}) } }).trim();
  g(["init", "-q", "-b", "main"]);
  let n = 0;
  const commit = (msg: string, date: string) => {
    writeFileSync(join(dir, `f${n++}.txt`), msg);
    g(["add", "-A"]);
    g(["commit", "-q", "-m", msg], date);
    return g(["rev-parse", "HEAD"]);
  };
  return { mirrors, g, commit };
}
const approver = (set: Set<string>) => (sha: string) => (set.has(sha) ? "approved" : undefined);

for (const mApproved of [false, true]) {
  test(`merge, no base: an unreviewed commit behind the merge is listed and blocks (merge approved: ${mApproved})`, async () => {
    const { mirrors, g, commit } = mk("r1");
    const X = commit("X", "2026-01-01T00:00:00Z");
    const Y = commit("Y", "2026-01-02T00:00:00Z");
    g(["checkout", "-q", "-b", "side", X]);
    const Z = commit("Z", "2026-03-01T00:00:00Z");
    g(["checkout", "-q", "main"]);
    g(["merge", "-q", "--no-ff", "-m", "M", "side"], "2026-03-02T00:00:00Z");
    const M = g(["rev-parse", "HEAD"]);
    const s = await reviewedState(mirrors, "r1", M, approver(new Set([X, Z, ...(mApproved ? [M] : [])])));
    assert.ok(!("why" in s));
    if ("why" in s) return;
    assert.ok(s.commits.some((c) => c.sha === Y), "Y is listed");
    assert.ok(s.missing.includes(Y));
    assert.match(reviewedLines(s, "main").blocked ?? "", /no review/);
  });
}

test("with a base, more commits than the cap: the act is blocked, said in the reason", async () => {
  const { mirrors, g, commit } = mk("r3");
  const B = commit("B", "2026-01-01T00:00:00Z");
  const after: string[] = [];
  for (let i = 1; i <= 52; i++) after.push(commit(`c${i}`, new Date(Date.UTC(2026, 1, 1) + i * 3600e3).toISOString()));
  const head = g(["rev-parse", "HEAD"]);
  const s = await reviewedState(mirrors, "r3", head, approver(new Set(after.slice(1))), {}, 50, B);
  assert.ok(!("why" in s));
  if ("why" in s) return;
  assert.deepEqual(s.missing, [], "the newest 50 are all approved");
  assert.match(reviewedLines(s, "main").blocked ?? "", /not all commits under this rerun were checked/);
  // without a base: the same fail-closed rule
  const nb = await reviewedState(mirrors, "r3", head, () => undefined, {}, 5);
  assert.ok(!("why" in nb));
  if (!("why" in nb)) assert.match(reviewedLines(nb, "main").blocked ?? "", /./);
  // within the cap nothing changes
  const ok = await reviewedState(mirrors, "r3", head, () => "approved", {}, 100, B);
  assert.ok(!("why" in ok));
  if (!("why" in ok)) assert.equal(reviewedLines(ok, "main").blocked, undefined);
});

test("the gate counts only a full, equal sha; the display still matches a 7-hex prefix", () => {
  const A = "a1b2c3d" + "0".repeat(33);
  const other = "a1b2c3d" + "f".repeat(33);
  const prefix = [{ approvedCommit: { value: A.slice(0, 7) }, by: { value: "alice" } }];
  assert.equal(reviewedSha(other, prefix), "no review found");
  assert.equal(reviewedSha(A, prefix), "no review found", "a short approval does not open the gate, not even for its own commit");
  assert.match(reviewOf(A, prefix), /approved by alice/, "display keeps the prefix match");
  const full = [{ approvedCommit: { value: A.toUpperCase() }, by: { value: "alice" }, when: { value: "2026-03-01T00:00:00Z" } }];
  assert.equal(reviewedSha(A, full), "approved by alice on 2026-03-01");
  assert.equal(reviewedSha(other, full), "no review found");
});

const CLAIM = `sha256:${"1".repeat(64)}`;
const ROW = `https://kton.dev/o/${"a".repeat(64)}`;
for (const order of ["valued first", "valued last"]) {
  test(`requireReviewed with when: several rows for one target; the rule applies when any has a value (${order})`, async () => {
    const reviews: unknown[] = [];
    const plan = {
      root: { package: "p", composition: "c" },
      subject: { iri: `https://kton.dev/o/${"1".repeat(64)}`, claim: CLAIM },
      uses: [{ key: "runs#rows", block: "@workbench/rows", requires: ["query@1"], optional: ["record@1"], query: { package: "p", name: "q", hash: "h" }, params: {}, options: { target: "item", requireReviewed: "rerun:app|main|reviews|last-run|reviewScope" },
        acts: [{ intent: "record", label: "Rerun", control: { package: "p", name: "rerun" }, about: CLAIM, fields: {} }] }],
    } as unknown as Plan;
    const rerun: Control = { package: "p", name: "rerun", template: "rerun-requested", kind: "record" };
    const loaded = { controls: new Map([["p/rerun", rerun]]), byHash: new Map(), packages: {} } as unknown as Loaded;
    const valued = { item: { type: "uri", value: ROW }, reviewScope: { type: "literal", value: "app" } };
    const bare = { item: { type: "uri", value: ROW } };
    const backend = {
      query: async () => ({ head: { vars: ["item", "reviewScope"] }, results: { bindings: order === "valued first" ? [valued, bare] : [bare, valued] } }),
      act: async (...a: unknown[]) => (reviews.push(a[8]), "signed" as const),
    } as unknown as Backend;
    const h = new CompositionHost(plan, loaded, backend, { post: () => undefined, log: () => undefined });
    await h.onMessage({ type: "kton:request", id: "q#1", cap: "query@1", args: { use: "runs#rows" } });
    await h.onMessage({ type: "kton:intent", intent: { id: "r#1", kind: "record", from: "runs#rows", about: CLAIM, control: { package: "p", name: "rerun" }, target: ROW, fields: {} } });
    assert.equal(reviews.length, 1);
    assert.deepEqual(reviews[0], { control: "rerun", repo: "app", head: "main", reviews: "reviews", base: "last-run", when: "reviewScope", package: "p" });
  });
}
