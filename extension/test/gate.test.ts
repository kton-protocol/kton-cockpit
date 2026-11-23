// The window's one ask queue (against a flood of parallel asks): at most 2 reads in flight, a cold
// index awaited by one ask, writes mark it cold and are not queued; polls back off; a re-ask shares a running one.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { AskGate, gated, MAX_IN_FLIGHT, READS, WRITES } from "../src/transport/gate.js";
import { MAX_BACKOFF_MS, nextDelay } from "../src/poll.js";
import type { Transport } from "../src/transport/port.js";

const tick = () => new Promise((r) => setTimeout(r, 5));
function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((a, b) => ((resolve = a), (reject = b)));
  return { promise, resolve, reject };
}

test("gate: cold, one ask runs alone and the others wait for it; warm, at most 2 at a time, first come first served", async () => {
  const g = new AskGate();
  assert.equal(MAX_IN_FLIGHT, 2);
  let running = 0, peak = 0;
  const order: number[] = [];
  const gates = Array.from({ length: 8 }, () => deferred<void>());
  const all = gates.map((d, i) => g.run(async () => { running++; peak = Math.max(peak, running); order.push(i); await d.promise; running--; return i; }));
  await tick();
  assert.equal(running, 1, "cold: the first ask alone");
  assert.equal(g.queued, 7);
  gates[0]?.resolve();
  await tick();
  assert.equal(g.isCold, false, "the first answer warms it");
  assert.equal(running, 2, "warm: two at a time");
  for (const d of gates.slice(1)) { d.resolve(); await tick(); }
  assert.deepEqual(await Promise.all(all), [0, 1, 2, 3, 4, 5, 6, 7]);
  assert.deepEqual(order, [0, 1, 2, 3, 4, 5, 6, 7], "in order of asking");
  assert.equal(peak, 2);
  assert.equal(g.peak, 2);
});

test("gate: a failed ask leaves it cold (the next runs alone); a write passes the queue and marks it cold", async () => {
  const g = new AskGate();
  await assert.rejects(g.run(async () => { throw new Error("index not built"); }));
  assert.equal(g.isCold, true);
  await g.run(async () => 1);
  assert.equal(g.isCold, false);
  const slow = deferred<unknown>();
  let says = 0, asks = 0;
  const t = { ask: () => { asks++; return slow.promise; }, say: async () => { says++; return { ok: true }; } } as unknown as Transport;
  const w = gated(t, g);
  const a1 = w.ask({} as never), a2 = w.ask({} as never), a3 = w.ask({} as never);
  await tick();
  assert.equal(asks, 2, "two reads in flight, the third waits");
  await w.say({} as never);
  assert.equal(says, 1, "the say did not wait behind the reads");
  assert.equal(g.isCold, true, "a write marks the index cold");
  slow.resolve({ ok: true });
  await Promise.all([a1, a2, a3]);
  assert.equal(asks, 3);
});

test("poll backoff: the interval while quick; x2 per slow run up to 5 min; back to the interval when quick again", () => {
  assert.equal(nextDelay(30_000, 30_000, 2_000), 30_000);
  assert.equal(nextDelay(30_000, 30_000, 40_000), 60_000);
  assert.equal(nextDelay(30_000, 60_000, 70_000), 120_000);
  assert.equal(nextDelay(30_000, 240_000, 250_000), MAX_BACKOFF_MS);
  assert.equal(nextDelay(30_000, MAX_BACKOFF_MS, 400_000), MAX_BACKOFF_MS);
  assert.equal(nextDelay(30_000, MAX_BACKOFF_MS, 1_000), 30_000);
});

test("requests block: no poll while hidden (no layout box, page hidden, gone); its delay backs off the same way", async () => {
  const m = await import(pathToFileURL(join(process.cwd(), "media", "workbench", "viewers", "requests", "requests.js")).href);
  assert.equal(m.hidden({ isConnected: false }), true);
  assert.equal(m.hidden({ isConnected: true, getClientRects: () => [] }), true, "a hidden tab or collapsed part");
  assert.equal(m.hidden({ isConnected: true, getClientRects: () => [{}] }), false);
  assert.equal(m.pollDelay(10_000, 0, 3_000), 10_000);
  assert.equal(m.pollDelay(10_000, 10_000, 12_000), 20_000);
  assert.equal(m.pollDelay(10_000, 200_000, 250_000), 300_000);
});

test("gate: a newer ask of the same key drops the older one still waiting; one running is not touched", async () => {
  const g = new AskGate();
  await g.run(async () => 0); // warm
  const hold = [deferred<void>(), deferred<void>()];
  const r1 = g.run(async () => { await hold[0]?.promise; return "a"; }, "x");
  const r2 = g.run(async () => { await hold[1]?.promise; return "b"; }, "y");
  const w1 = g.run(async () => "item 1", "q");
  const w2 = g.run(async () => "item 2", "q");
  const w3 = g.run(async () => "item 3", "q");
  await assert.rejects(w1, /superseded/);
  await assert.rejects(w2, /superseded/);
  hold[0]?.resolve();
  hold[1]?.resolve();
  assert.deepEqual(await Promise.all([r1, r2, w3]), ["a", "b", "item 3"], "the running ones finish; only the last of q runs");
});

test("gated: every method of Transport is a read through the queue or a write that marks it cold; none bypasses the queue", async () => {
  // a Record over keyof Transport: a method added to Transport later does not compile here until it is listed
  const methods: Record<keyof Transport, true> = { ask: true, say: true, publish: true, workflowRun: true, install: true, templates: true, list: true, doctor: true, version: true, uiList: true, uiCat: true, graph: true };
  // and at the type level: no key of Transport is outside READS and WRITES
  type Gated = (typeof READS)[number] | (typeof WRITES)[number];
  const none: [Exclude<keyof Transport, Gated>] extends [never] ? true : false = true;
  assert.equal(none, true);
  const reads = new Set<string>(READS), writes = new Set<string>(WRITES);
  for (const m of Object.keys(methods)) assert.ok(reads.has(m) !== writes.has(m), `${m} is in exactly one of READS and WRITES`);
  assert.deepEqual([...reads, ...writes].sort(), Object.keys(methods).sort(), "READS and WRITES name no method Transport does not have");
  // and gated() wraps each one
  const calls: string[] = [];
  const t = Object.fromEntries(Object.keys(methods).map((m) => [m, async () => (calls.push(m), { ok: true })])) as unknown as Transport;
  const w = gated(t, new AskGate()) as unknown as Record<string, (...a: unknown[]) => Promise<unknown>>;
  for (const m of Object.keys(methods)) await w[m]?.({});
  assert.deepEqual(calls.sort(), Object.keys(methods).sort());
});

test("gate: a read running across a write leaves the index cold; the next read that starts after the write warms it", async () => {
  const g = new AskGate();
  await g.run(async () => 0);
  assert.equal(g.isCold, false);
  const slow = deferred<void>();
  const across = g.run(async () => { await slow.promise; return "old"; });
  await tick();
  g.markCold(); // a write lands while the read runs
  slow.resolve();
  assert.equal(await across, "old");
  assert.equal(g.isCold, true, "the read started before the write: it does not warm the index");
  // while cold, one at a time: the next read runs alone and warms it
  let running = 0, peak = 0;
  const hold = deferred<void>();
  const next = [0, 1].map(() => g.run(async () => { running++; peak = Math.max(peak, running); await hold.promise; running--; }));
  await tick();
  assert.equal(running, 1, "cold: one read alone");
  hold.resolve();
  await Promise.all(next);
  assert.equal(peak, 1);
  assert.equal(g.isCold, false, "a read started after the write warms it");
});
