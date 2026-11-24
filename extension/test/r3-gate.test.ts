// The requireReviewed gate fails closed: a merge, a cap, a short id, and several rows for one target.
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { reviewedState } from "../src/ui/sources.js";
import { reviewedLines } from "../src/ui/annotate.js";

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

