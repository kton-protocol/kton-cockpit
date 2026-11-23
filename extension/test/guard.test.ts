// The unbound-parameter guard (kton-review on ktonpkg v0.9.11): an optional $VAR left unbound is a free variable, so a
// FILTER using it vanishes and the query answers unfiltered. A use that feeds a sign act or proposes a field refuses to
// run and offers no act; any other use runs, stamped "unfiltered". And the nudge line a use prints after its act.
import { test } from "node:test";
import assert from "node:assert/strict";
import { CompositionHost, feedsAct, nudgeLine, unboundParams, type Backend, type Plan, type PlanUse, type Signed } from "../src/composition/host.js";
import type { Control, Loaded } from "../src/packages/model.js";

const CLAIM = `sha256:${"1".repeat(64)}`;
const control: Control = { package: "p", name: "approves", template: "approves", kind: "decision" };
const PERSON = { type: "literal", value: "https://example.org/person/p1" };

function host(use: Partial<PlanUse> & Record<string, unknown>, signed?: Signed) {
  const asked: Array<{ name: string; terms: unknown }> = [];
  const acts: unknown[] = [];
  const posted: Array<Record<string, unknown>> = [];
  const logs: string[] = [];
  const plan = {
    root: { package: "p", composition: "c" },
    subject: { iri: `https://kton.dev/o/${"1".repeat(64)}`, claim: CLAIM },
    uses: [{ key: "area#training", block: "@workbench/rows", requires: ["query@1"], optional: ["sign@1"], query: { package: "p", name: "obligations", hash: "h" }, params: {}, queryOptional: ["person"], options: {}, ...use }],
  } as unknown as Plan;
  const loaded = { controls: new Map([["p/approves", control]]), byHash: new Map(), packages: {} } as unknown as Loaded;
  const backend = {
    query: async (_p: string, name: string, terms: unknown) => (asked.push({ name, terms }), { head: { vars: ["trainee"] }, results: { bindings: [{ trainee: { type: "uri", value: "https://example.org/person/p1" } }] } }),
    act: async (_c: Control, about: string, fields: unknown, _t: string, proposed: unknown, done?: (s: Signed) => void) => {
      acts.push({ about, fields, proposed });
      if (signed) done?.(signed);
      return "signed" as const;
    },
  } as unknown as Backend;
  const h = new CompositionHost(plan, loaded, backend, { post: (m) => posted.push(m as Record<string, unknown>), log: (l) => logs.push(l) });
  return { h, asked, acts, posted, logs };
}
const ask = { type: "kton:request", id: "q1", cap: "query@1", args: { use: "area#training" } };
const SIGN_ACT = [{ intent: "sign", label: "Approve", control: { package: "p", name: "approves" }, about: CLAIM, fields: {} }];
const sign = { type: "kton:intent", intent: { id: "i1", kind: "sign", from: "area#training", about: CLAIM, control: { package: "p", name: "approves" }, fields: {} } };

test("guard: a use that feeds a sign act refuses to run its query while a declared param is unbound, and offers no act", async () => {
  const { h, asked, acts, posted } = host({ acts: SIGN_ACT });
  await h.onMessage(ask);
  assert.equal(asked.length, 0, "the query never runs unfiltered");
  assert.deepEqual(posted.at(-1), { type: "kton:refused", what: "q1", why: "cannot show: person is not set" });
  // the frame's plan has no act for it, and an intent naming the act is refused: nothing is signed
  await h.onMessage({ type: "kton:ready" });
  const set = posted.find((m) => m["type"] === "kton:set") as { plan: Plan };
  assert.deepEqual(set.plan.uses[0]?.acts, []);
  assert.deepEqual((set.plan.uses[0] as unknown as { options: { unset: string[] } }).options.unset, ["person"]);
  await h.onMessage(sign);
  assert.equal(acts.length, 0);
  assert.match(JSON.stringify(posted.filter((m) => m["type"] === "kton:refused").at(-1)), /cannot show: person is not set; it offers no act/);
});

test("guard: a use that proposes a field (option propose) is guarded like one with a sign or record act", async () => {
  assert.equal(feedsAct({ key: "k", block: "b", requires: [], optional: [], options: { propose: ["lookedAt=lookedAt"] } } as unknown as PlanUse), true);
  assert.equal(feedsAct({ key: "k", block: "b", requires: [], optional: [], acts: [{ intent: "record", label: "n", fields: {} }] } as unknown as PlanUse), true);
  assert.equal(feedsAct({ key: "k", block: "b", requires: [], optional: [], acts: [{ intent: "open", label: "o", fields: {} }] } as unknown as PlanUse), false);
  const { h, asked, posted } = host({ options: { propose: ["lookedAt=lookedAt"] } });
  await h.onMessage(ask);
  assert.equal(asked.length, 0);
  assert.equal(posted.at(-1)?.["why"], "cannot show: person is not set");
});

test("guard: any other use with an unbound optional param runs, its answer stamped unfiltered (S6.7's training)", async () => {
  const { h, asked, posted } = host({});
  await h.onMessage(ask);
  assert.equal(asked.length, 1);
  assert.deepEqual(asked[0]?.terms, {}, "the param stays unbound: nothing stands in for it");
  const data = posted.at(-1) as { type: string; result: { unfiltered: string[]; results: { bindings: unknown[] } } };
  assert.equal(data.type, "kton:data");
  assert.deepEqual(data.result.unfiltered, ["person"]);
  assert.equal(data.result.results.bindings.length, 1);
  assert.deepEqual(unboundParams({ queryOptional: ["a", "b"] } as unknown as PlanUse, { a: PERSON }), ["b"]);
});

test("guard: bound, everything is as before: the query runs filtered, no stamp, the act is offered and signs", async () => {
  const { h, asked, acts, posted } = host({ acts: SIGN_ACT, params: { person: PERSON } });
  await h.onMessage(ask);
  assert.deepEqual(asked[0]?.terms, { person: { type: "literal", value: PERSON.value } });
  const data = posted.at(-1) as { type: string; result: Record<string, unknown> };
  assert.equal(data.type, "kton:data");
  assert.equal("unfiltered" in data.result, false);
  await h.onMessage({ type: "kton:ready" });
  const set = posted.find((m) => m["type"] === "kton:set") as { plan: Plan };
  assert.equal(set.plan.uses[0]?.acts?.length, 1);
  await h.onMessage(sign);
  assert.equal(acts.length, 1);
});

test("nudge: after the act signed, the use's nudge line is printed with the claim id and the signed fields, shell-quoted", async () => {
  const test1 = 'ui-e2e:ui/item-24.spec.ts#T-002: the toolbar shows a "Create" button';
  const nudge = './send lane-a requests area=area-b title="capture now: {test}" text="request claim {claim}; test {test}" why="the decision view"';
  const made: Signed = { id: `sha256:${"c".repeat(64)}`, fields: { test: test1, proposal: "https://kton.dev/o/x" } };
  const want = `./send lane-a requests area=area-b title="capture now: ${test1.replace(/"/g, '\\"')}" text="request claim sha256:${"c".repeat(64)}; test ${test1.replace(/"/g, '\\"')}" why="the decision view"`;
  assert.equal(nudgeLine(nudge, made), want);
  assert.equal(nudgeLine("a {missing}", made), undefined, "a placeholder without a value prints nothing");
  assert.equal(nudgeLine("x={test}", { id: "i", fields: { test: "$(rm -rf /) `x` \\" } }), "x=\\$(rm -rf /) \\`x\\` \\\\");
  const { h, logs, posted } = host({ queryOptional: [], acts: SIGN_ACT, options: { nudge } }, made);
  await h.onMessage(sign);
  assert.ok(logs.some((l) => l.endsWith(want)), "the exact line is in the output channel");
  const data = posted.find((m) => m["type"] === "kton:data") as { result: { claim: string; nudge: string } };
  assert.equal(data.result.claim, made.id);
  assert.equal(data.result.nudge, want);
});

test("nudge: a use with acts through several controls names a line per control (nudges), else its one nudge", async () => {
  const { nudgeFor } = await import("../src/composition/host.js");
  const o = { nudges: ["rerun-request|./send a rerun {claim}", "capture|./send a capture {claim}"], nudge: "./send any {claim}" };
  assert.equal(nudgeFor(o, "capture"), "./send a capture {claim}");
  assert.equal(nudgeFor(o, "rerun-request"), "./send a rerun {claim}");
  assert.equal(nudgeFor(o, "other"), "./send any {claim}");
  assert.equal(nudgeFor({}, "capture"), undefined);
});
