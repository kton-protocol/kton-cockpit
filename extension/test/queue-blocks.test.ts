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

test("rules: the overview lists the open rules (settles n items = what they collapse to, the caveat flag) and the decided ones apart, with who signed; the flow shows one rule in full", async () => {
  const m = await media("viewers/rules/rules.js");
  const opts = { id: "candidate", title: "rule", target: "target", size: "size", settles: "settles", collapses: "collapsesTo", answer: "proposedAnswer", caveat: "caveat", examples: "examples", members: "members", excludeField: "excluded", decidedBy: "by", decidedAt: "at", decidedVerdict: "verdict", excluded: "excluded", choices: ["verdict=accepted|Accept…"] };
  const rows = [
    b({ candidate: "R14", rule: "Gap, group A", size: "240", settles: "240", collapsesTo: "31 specs", proposedAnswer: "extend the spec's tests", caveat: "the area map decides R14 vs R15", examples: "item-1#e1; item-2#e3", members: "item-1#e1 ;; item-2#e3", target: "https://kton.dev/o/" + "a".repeat(64), stamp: "sha256:0a1b2c3d" }),
    b({ candidate: "R02", rule: "Covered by a run", size: "3", members: "a ;; b ;; c", by: "Tester (test)", at: "2026-11-23T13:10:00Z", verdict: "accepted", excluded: "b", target: "https://kton.dev/o/" + "b".repeat(64), stamp: "s" }),
    b({ candidate: "R01", rule: "Outside the scope", size: "75", members: "x", target: "https://kton.dev/o/" + "c".repeat(64), stamp: "s" }),
  ];
  const mountWith = async (state: Any, signed: Any[] = []) => {
    (globalThis as Any).ktonWorkbench = { signedActs: (use: string) => signed.filter((x) => x.use === use) };
    const el: Any = { innerHTML: "", querySelector: () => null, querySelectorAll: () => [] };
    m.mount(el, { use: { key: "r", options: opts, blockActs: [{ intent: "sign", control: { name: "decide-rule" } }] }, kit, state, request: async () => ({ results: { bindings: rows } }), intent: async () => ({}), setState: () => {} });
    await tick();
    return el.innerHTML as string;
  };
  const ov = await mountWith({});
  assert.match(ov, /data-open-count>2 open · 1 decided</);
  assert.match(ov, /Settles 240 items = 31 specs: extend the spec&#39;s tests/);
  assert.match(ov, /data-caveat-flag[^>]*>check the area map</);
  assert.match(ov, /data-testid="rules-start"/);
  assert.match(ov, /<summary>Decided \(1\)<\/summary>[\s\S]*R02<\/span> <b>Covered by a run<\/b> · accepted · signed by Tester \(test\) at 2026-11-23T13:10:00Z/);
  assert.doesNotMatch(ov, /data-rule-line="R02"/, "a decided rule is not in the open list");
  // signed in this view, before the cockpit answers: decided at once
  const ov2 = await mountWith({}, [{ use: "r", target: "https://kton.dev/o/" + "c".repeat(64), claim: "sha256:" + "d".repeat(64), at: Date.parse("2026-11-23T14:00:00Z"), fields: { verdict: "accepted" } }]);
  assert.match(ov2, /data-open-count>1 open · 2 decided</);
  assert.match(ov2, /R01<\/span> <b>Outside the scope<\/b> · accepted · signed by you, in this view at 2026-11-23 14:00 \(the cockpit's answer follows\)/);
  const fl = await mountWith({ mode: "flow", cur: "R14" });
  assert.match(fl, /data-pos aria-live="polite">Rule 1 of 2 open</);
  assert.match(fl, /data-caveat role="status"><p><b>Check the area map\.<\/b> the area map decides R14 vs R15/);
  assert.match(fl, /data-testid="rule-R14-accepted"/);
  assert.match(fl, /data-member="item-2#e3" aria-label="Keep item-2#e3 in the rule" checked/);
  assert.match(fl, /data-example="item-2#e3" title="Show item-2#e3 beside this rule">item-2#e3</, "a member opens beside too");
  assert.match(fl, /Drawn examples \(click or Enter: its item screen beside this rule\)/);
  assert.doesNotMatch(fl, /data-beside=/, "nothing beside until one is clicked");
  // an example shown beside: what is beside, take out, open in the item queue, close (Esc)
  const bs = await mountWith({ mode: "flow", cur: "R14", pick: "item-2#e3" });
  assert.match(bs, /data-beside="item-2#e3"[\s\S]*the item screen of <span class="mono">item-2#e3<\/span>/);
  assert.match(bs, /data-testid="beside-takeout">Take this member out of the rule</);
  assert.match(bs, /data-testid="beside-queue">Open in item queue</);
  assert.match(bs, /data-testid="beside-close">Close \(Esc\)</);
  const bs2 = await mountWith({ mode: "flow", cur: "R14", pick: "item-2#e3", excluded: { R14: ["item-2#e3"] } });
  assert.match(bs2, /data-testid="beside-takeout">Put it back into the rule</, "taken out from the panel, the same list");
  delete (globalThis as Any).ktonWorkbench;
});

test("queue: a jump from another block (the rule card's Open in item queue) shows that item in its group, with Back to rule", async () => {
  const m = await media("viewers/queue/queue.js");
  const el: Any = { innerHTML: "", isConnected: true, querySelector: () => null, querySelectorAll: () => [] };
  const rows = ["item-1#e1", "item-1#e2", "item-1#e3"].map((k) => b({ key: k, total: "3", stamp: "s", target: "https://e/" + k }));
  const asked: Any[] = [];
  m.mount(el, {
    use: { key: "q", options: { key: "key", title: "key", total: "total", group: "GROUP", jump: JSON.stringify({ group: "R14", item: "item-1#e3", from: "R14", n: 1 }) }, refine: { GROUP: { values: ["person", "R14"] } } },
    kit, state: {}, request: async (_c: string, a: Any) => (asked.push(a), { results: { bindings: rows } }), intent: async () => ({}), setState: () => {},
  });
  await tick();
  assert.equal(asked[0]?.refine?.GROUP, "R14", "the rule's group");
  assert.match(el.innerHTML, /data-queue-item="item-1#e3"/);
  assert.match(el.innerHTML, /Item 3 of 3 in this group/);
  assert.match(el.innerHTML, /data-testid="queue-back">Back to rule R14</);
});

test("throughput: per hour by rule and one by one over the first to the last signature; one signature has no rate", async () => {
  const { throughput } = await media("viewers/throughput/throughput.js");
  const t = throughput([b({ when: "2026-11-23T10:00:00Z", kind: "rule", count: "2746" }), b({ when: "2026-11-23T10:10:00Z", kind: "item", count: "1" }), b({ when: "2026-11-23T10:30:00Z", kind: "item", count: "1" })]);
  assert.equal(t.hours, 0.5);
  assert.deepEqual([t.rule.items, t.item.items, t.signatures], [2746, 2, 3]);
  assert.equal(throughput([b({ when: "2026-11-23T10:00:00Z", kind: "item", count: "1" })]).hours, null);
});
