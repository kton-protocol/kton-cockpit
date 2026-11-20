// Evidence on demand: a file as it was at a commit, from a git mirror, checked against its sha256 (kton-src).
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readAtCommit, sourceTarget, sourceUri, targetOf } from "../src/ui/sources.js";

function mirror() {
  const root = mkdtempSync(join(tmpdir(), "mirrors-"));
  const repo = join(root, "improve-workbench-ext-core");
  mkdirSync(join(repo, "test"), { recursive: true });
  const git = (...a: string[]) => execFileSync("git", ["-C", repo, ...a], { encoding: "utf8" }).trim();
  git("init", "-q");
  git("config", "user.email", "t@example.org");
  git("config", "user.name", "t");
  writeFileSync(join(repo, "test", "a.spec.ts"), "line 1\nline 2 at the round's commit\n");
  git("add", "-A");
  git("commit", "-q", "-m", "round 9");
  const commit = git("rev-parse", "HEAD");
  writeFileSync(join(repo, "test", "a.spec.ts"), "changed later\n");
  git("commit", "-q", "-am", "later");
  const sha = `sha256:${createHash("sha256").update("line 1\nline 2 at the round's commit\n").digest("hex")}`;
  return { root, commit, sha };
}

test("evidence: the file as it was at the round's commit, not as checked out; ext-core is improve-workbench-ext-core", async () => {
  const m = mirror();
  const r = await readAtCommit(m.root, { repo: "ext-core", commit: m.commit, path: "test/a.spec.ts", line: 2, sha256: m.sha });
  assert.ok("bytes" in r);
  assert.equal(r.bytes.toString(), "line 1\nline 2 at the round's commit\n");
});

test("evidence: a hash mismatch, an unknown commit or a missing mirror shows nothing and says why", async () => {
  const m = mirror();
  const bad = await readAtCommit(m.root, { repo: "ext-core", commit: m.commit, path: "test/a.spec.ts", sha256: `sha256:${"0".repeat(64)}` });
  assert.match("why" in bad ? bad.why : "", /does not hash to sha256:0000/);
  const gone = await readAtCommit(m.root, { repo: "ext-core", commit: "1234567", path: "test/a.spec.ts" });
  assert.match("why" in gone ? gone.why : "", /source not available on this host/);
  const none = await readAtCommit(m.root, { repo: "improve", commit: m.commit, path: "x" });
  assert.match("why" in none ? none.why : "", /source not available on this host/);
  assert.match("why" in (await readAtCommit("", { repo: "improve", commit: m.commit, path: "x" })) ? "no mirrors" : "", /no mirrors/);
});

test("evidence: a frame's source target is checked (no climbing path, no option-looking repo); the URI round-trips", () => {
  for (const bad of [{ repo: "-x", commit: "1234567", path: "a" }, { repo: "r", commit: "HEAD", path: "a" }, { repo: "r", commit: "1234567", path: "../etc/passwd" }, { repo: "r", commit: "1234567", path: "a", line: 0 }]) assert.equal(typeof sourceTarget(bad), "string", JSON.stringify(bad));
  const t = sourceTarget({ repo: "improve-specs", commit: "49966c2", path: "specifications/x y/ics1 a.md", line: "31", sha256: `sha256:${"c".repeat(64)}` });
  assert.notEqual(typeof t, "string");
  assert.deepEqual(targetOf(sourceUri(t as Exclude<typeof t, string>)), t);
});

test("evidence: a source open's looked-at reference names the repo, path, commit and line", async () => {
  const { sourceRef } = await import("../src/ui/sources.js");
  assert.equal(sourceRef({ repo: "improve", commit: "fa218aa8", path: "a/B.java", line: 127 }), "kton-src:/improve/a/B.java?fa218aa8#L127");
  assert.equal(sourceRef({ repo: "improve", commit: "fa218aa8", path: "a/B.java" }), "kton-src:/improve/a/B.java?fa218aa8");
  // lookedAt is space-separated: a spec path with spaces stays one reference
  assert.equal(sourceRef({ repo: "improve-specs", commit: "49966c23", path: "Rights Management/ics1000 Publish rights.md", line: 31 }), "kton-src:/improve-specs/Rights%20Management/ics1000%20Publish%20rights.md?49966c23#L31");
});
