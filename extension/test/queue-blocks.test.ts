// The queue, rules and throughput blocks: the queue (one item at a time, by keyboard, skipped
// items at the end), the rule candidates (exclusion before signing) and the throughput figure.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Any = any;
const media = (p: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", p)).href);
const esc = (s: string) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string);
const kit = { esc };
const b = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { value: v }]));
const tick = () => new Promise((r) => setTimeout(r, 10));

test("queue: the open items in order, the decided left out, the skipped at the end in the order skipped", async () => {
  const { queueOrder } = await media("viewers/queue/queue.js");
  const rows = ["a", "b", "c", "d"].map((k) => ({ k }));
  const keys = (l: Any[]) => l.map((r) => r.k).join("");
  assert.equal(keys(queueOrder(rows, (r: Any) => r.k, { done: [], skipped: [] })), "abcd");
  assert.equal(keys(queueOrder(rows, (r: Any) => r.k, { done: ["c"], skipped: ["b", "a"] })), "dba");
  assert.equal(keys(queueOrder(rows, (r: Any) => r.k, { done: ["b"], skipped: ["b"] })), "acd", "a decided skipped item does not come back");
});

test("queue: the current item with its fields, its kind, its stamp, where it is, the keys and the decisions", async () => {
  const m = await media("viewers/queue/queue.js");
  const el: Any = { innerHTML: "", isConnected: true, querySelector: () => null, querySelectorAll: () => [] };
  const states: Any[] = [];
  const rows = [b({ key: "item-1#e1", kind: "K1", statement: "Shows <b>x</b>.", gClass: "K1.1b", total: "3", stamp: "sha256:0a1b2c3d", stampLabel: "mapping round 3 · 0a1b2c3d", target: "https://e/1" }), b({ key: "item-1#e2", kind: "K1", total: "3", stamp: "s", target: "https://e/2" })];
  m.mount(el, {
    use: { key: "q", options: { key: "key", title: "key", kind: "kind", quote: "statement", total: "total", target: "target", fields: ["gClass|Class", "judges|Judges"], choices: ["verdict=confirmed|Confirm", "verdict=not-a-gap|Not a gap"] }, blockActs: [{ intent: "sign", control: { name: "decide-item" } }] },
    kit, state: {}, request: async () => ({ results: { bindings: rows } }), intent: async () => ({}), setState: (s: Any) => states.push(s),
  });
  await tick();
  assert.match(el.innerHTML, /data-pos aria-live="polite">Item 1 of 2 in this group</, "all pages loaded: the open items counted");
  assert.match(el.innerHTML, /<span class="badge" data-kind>K1<\/span><h4 class="id" data-item-id>item-1#e1<\/h4>/);
  assert.match(el.innerHTML, /<blockquote class="quote" data-quote>Shows &lt;b&gt;x&lt;\/b&gt;\.<\/blockquote>/);
  assert.match(el.innerHTML, /<dt>Class<\/dt><dd class="kton-field" data-col="gClass">K1\.1b<\/dd><dt>Judges<\/dt><dd class="empty-line" data-col="judges">No judges recorded\.<\/dd>/);
  assert.match(el.innerHTML, /data-stamp-label="mapping round 3 · 0a1b2c3d"/);
  assert.match(el.innerHTML, /data-testid="queue-decide-confirmed"><kbd>1<\/kbd> Confirm<\/button>/);
  assert.match(el.innerHTML, /Keys: → or j next · ← or k previous · s skip/);
  await new Promise((r) => setTimeout(r, 350));
  assert.equal(states.at(-1).item, "item-1#e1", "the first item is emitted (once the reader stops moving)");
});

test("rules: settles n items = what they collapse to; the caveat; a signed rule names its members as decided by it", async () => {
  const m = await media("viewers/rules/rules.js");
  const el: Any = { innerHTML: "", querySelector: () => null, querySelectorAll: () => [] };
  const opts = { id: "candidate", title: "rule", target: "target", size: "size", settles: "settles", collapses: "collapsesTo", answer: "proposedAnswer", caveat: "caveat", examples: "examples", members: "members", excludeField: "excluded", decidedBy: "by", decidedVerdict: "verdict", excluded: "excluded", choices: ["verdict=accepted|Accept…"] };
  const rows = [
    b({ candidate: "R14", rule: "Gap, group A", size: "240", settles: "240", collapsesTo: "31 specs", proposedAnswer: "extend the spec's tests", caveat: "the area map decides R14 vs R15", examples: "item-1#e1; item-2#e3", members: "item-1#e1 ;; item-2#e3", target: "https://kton.dev/o/" + "a".repeat(64), stamp: "sha256:0a1b2c3d" }),
    b({ candidate: "R02", rule: "Covered by a run", size: "3", members: "a ;; b ;; c", by: "Tester (test)", verdict: "accepted", excluded: "b", target: "https://kton.dev/o/" + "b".repeat(64), stamp: "s" }),
  ];
  m.mount(el, { use: { key: "r", options: opts, blockActs: [{ intent: "sign", control: { name: "decide-rule" } }] }, kit, state: {}, request: async () => ({ results: { bindings: rows } }), intent: async () => ({}), setState: () => {} });
  await tick();
  assert.match(el.innerHTML, /Settles 240 items = 31 specs: extend the spec&#39;s tests/);
  assert.match(el.innerHTML, /data-caveat role="status"><p><b>Check the area map\.<\/b> the area map decides R14 vs R15/);
  assert.match(el.innerHTML, /data-testid="rule-R14-accepted"/);
  assert.match(el.innerHTML, /2 members: decided by rule R02, signed by Tester \(test\)\. Taken out \(1, back in the queue\): <span class="mono">b<\/span>/);
  assert.doesNotMatch(el.innerHTML, /data-testid="rule-C02-accepted"/, "a decided rule offers no second signature");
});

test("throughput: per hour by rule and one by one over the first to the last signature; one signature has no rate", async () => {
  const { throughput } = await media("viewers/throughput/throughput.js");
  const t = throughput([b({ when: "2026-11-23T10:00:00Z", kind: "rule", count: "2746" }), b({ when: "2026-11-23T10:10:00Z", kind: "item", count: "1" }), b({ when: "2026-11-23T10:30:00Z", kind: "item", count: "1" })]);
  assert.equal(t.hours, 0.5);
  assert.deepEqual([t.rule.items, t.item.items, t.signatures], [2746, 2, 3]);
  assert.equal(throughput([b({ when: "2026-11-23T10:00:00Z", kind: "item", count: "1" })]).hours, null);
});
