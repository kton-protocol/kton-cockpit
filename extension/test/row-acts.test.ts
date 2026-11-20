// Acts about a served row (cards: a spot check is about the element its card showed) and fields a frame proposes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { CompositionHost, type Backend, type Plan } from "../src/composition/host.js";
import type { Control, Loaded } from "../src/packages/model.js";

const CLAIM = `sha256:${"1".repeat(64)}`;
const ROW = `https://kton.dev/o/${"a".repeat(64)}`;
const OTHER = `https://kton.dev/o/${"b".repeat(64)}`;
const control: Control = { package: "p", name: "spotcheck", template: "triage-spotcheck", kind: "decision" };

function host() {
  const acts: Array<{ about: string; fields: unknown; proposed: unknown }> = [];
  const posted: Array<Record<string, unknown>> = [];
  const plan = {
    root: { package: "p", composition: "c" },
    subject: { iri: `https://kton.dev/o/${"1".repeat(64)}`, claim: CLAIM },
    uses: [{ key: "spot#cards", block: "@workbench/cards", requires: ["query@1"], optional: ["sign@1"], query: { package: "p", name: "q", hash: "h" }, params: {}, options: { target: "item" },
      acts: [{ intent: "sign", label: "Verdict", control: { package: "p", name: "spotcheck" }, about: CLAIM, fields: { proposal: `https://kton.dev/o/${"1".repeat(64)}` } }] }],
  } as unknown as Plan;
  const loaded = { controls: new Map([["p/spotcheck", control]]), byHash: new Map(), packages: {} } as unknown as Loaded;
  const backend = {
    query: async () => ({ head: { vars: ["item"] }, results: { bindings: [{ item: { type: "uri", value: ROW } }] } }),
    act: async (_c: Control, about: string, fields: unknown, _t: string, proposed: unknown) => (acts.push({ about, fields, proposed }), "signed" as const),
  } as unknown as Backend;
  const h = new CompositionHost(plan, loaded, backend, { post: (m) => posted.push(m as Record<string, unknown>), log: () => undefined });
  return { h, acts, posted };
}
const sign = (target: string, fields: object = { verdict: "rule-judge-right" }) => ({ type: "kton:intent", intent: { id: `s#${target}`, kind: "sign", from: "spot#cards", about: CLAIM, control: { package: "p", name: "spotcheck" }, target, fields } });

test("an act about a row: only a row the host served to that block; the frame's fields are proposals, the plan's stay", async () => {
  const { h, acts, posted } = host();
  // before the block was answered, no row is served: refused
  await h.onMessage(sign(ROW));
  assert.equal(acts.length, 0);
  assert.match(JSON.stringify(posted.at(-1)), /not a row this block was shown/);
  await h.onMessage({ type: "kton:request", id: "q#1", cap: "query@1", args: { use: "spot#cards" } });
  await h.onMessage(sign(OTHER));
  assert.equal(acts.length, 0, "a row the block was not shown");
  await h.onMessage(sign(ROW, { verdict: "rule-judge-right", proposal: "https://evil.example/" }));
  assert.deepEqual(acts, [{ about: `sha256:${"a".repeat(64)}`, fields: { proposal: `https://kton.dev/o/${"1".repeat(64)}` }, proposed: { verdict: "rule-judge-right" } }], "the plan's proposal field stays; the verdict is a proposal");
});
