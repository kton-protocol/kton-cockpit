// The support file: what the active view shows, small and without key material; nothing without a path.
import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_ROWS, SupportWriter, atomicWrite, picked, supportView } from "../src/support/view-now.js";
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ key: { value: `item-1#e${i}` }, total: { value: String(n) }, stamp: { value: "sha256:0a1b2c3d" }, stampLabel: { value: "mapping round 3 · 0a1b2c3d" }, keyFile: { value: "/home/x/.kton-keys/a.key" }, note: { value: "-----BEGIN PRIVATE KEY-----" } }));

test("support view: the view, the selected item, the queue's place, rows as answered (capped, the rest counted), stamps, opened; no key material", () => {
  const v = supportView({
    at: new Date("2026-11-23T12:00:00Z"),
    identity: "Tester (test)",
    view: { package: "qa.review-surface", composition: "review", entry: "Review (tester) · run-a" },
    run: "run-a",
    state: { parts: { queue: { uses: { q: { item: "item-1#e3", position: { n: 4, of: 412, group: "R14", item: "item-1#e3" }, at: { R14: { skipped: [] } } } } } } },
    blocks: [{ use: "review/queue#q", block: "queue", query: "qa.review-surface/item-queue", params: { GROUP: "R14" }, answer: { head: { vars: ["key", "total", "stamp", "stampLabel", "keyFile", "note"] }, results: { bindings: rows(MAX_ROWS + 5) } } }, { use: "review/rules#r", block: "rules" }],
    opened: ["source:specs@49966c23:specifications/x.md#12"],
  });
  assert.equal(v["run"], "run-a");
  assert.deepEqual(v["selected"], { item: "item-1#e3" });
  assert.deepEqual(v["queue"], { n: 4, of: 412, group: "R14", item: "item-1#e3" });
  const b = (v["blocks"] as Array<Record<string, unknown>>)[0] as Record<string, unknown>;
  assert.equal((b["rows"] as unknown[]).length, MAX_ROWS);
  assert.equal(b["rowCount"], MAX_ROWS + 5);
  assert.equal(b["more"], 5);
  assert.deepEqual(b["stamps"], ["sha256:0a1b2c3d · mapping round 3 · 0a1b2c3d"]);
  const text = JSON.stringify(v);
  assert.doesNotMatch(text, /PRIVATE KEY|\.kton-keys|keyFile/, "no key material, no key paths");
  assert.equal((v["blocks"] as Array<Record<string, unknown>>)[1]?.["note"], "not answered yet");
  assert.deepEqual(v["opened"], ["source:specs@49966c23:specifications/x.md#12"]);
  assert.deepEqual(picked({ element: "none" }), {}, "a var's default none is no selection");
});

test("support view: written debounced, the last change wins; an empty setting writes nothing", async () => {
  const writes: string[] = [];
  let path = "/tmp/x/view-now.json";
  const w = new SupportWriter(() => path, (_p, t) => writes.push(t), 20);
  w.changed(() => ({ n: 1 }));
  w.changed(() => ({ n: 2 }));
  await new Promise((r) => setTimeout(r, 50));
  assert.deepEqual(writes.map((t) => JSON.parse(t)), [{ n: 2 }]);
  path = "";
  w.changed(() => ({ n: 3 }));
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(writes.length, 1);
});

test("the support file is written with mode 0600, also over a temp file left from before", () => {
  const dir = mkdtempSync(join(tmpdir(), "support-view-"));
  try {
    const path = join(dir, "sub", "view.json");
    atomicWrite(path, "{}\n");
    assert.equal(statSync(path).mode & 0o777, 0o600);
    writeFileSync(`${path}.tmp-${process.pid}`, "", { mode: 0o644 });
    atomicWrite(path, "{\"a\":1}\n");
    assert.equal(statSync(path).mode & 0o777, 0o600);
    assert.equal(readFileSync(path, "utf8"), "{\"a\":1}\n");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
