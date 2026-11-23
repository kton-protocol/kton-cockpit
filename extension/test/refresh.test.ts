// Never a blank view after a signature: after a signature the cockpit rebuilds the federated index cold
// and every ask waits for it, so the decision view (reloaded when the sign dialog took its column) showed grey skeletons
// for about a minute. The host answers a re-ask at once with the last answer, marked refreshing, and posts the fresh one
// (or the error) when it has it; the signature is confirmed at once, before any refresh; the frame and the blocks keep
// the last content (design/refresh.js, the viewers' stamped()).
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { answerKey, CompositionHost, shortClaim, signedWhat, type Backend, type Plan, type Signed } from "../src/composition/host.js";
import type { Control, Loaded } from "../src/packages/model.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Any = any;
const media = (p: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", p)).href);
const CLAIM = `sha256:${"1".repeat(64)}`;
const MADE = `sha256:ab12cd34${"9".repeat(56)}`;
const control: Control = { package: "p", name: "spotcheck", template: "review-spotcheck", kind: "decision", title: "Judge a spot check" };
const answer = (n: number) => ({ head: { vars: ["n"] }, results: { bindings: [{ n: { type: "literal", value: String(n) } }] } });

function host() {
  const posted: Array<Record<string, Any>> = [];
  let n = 0;
  const pending: Array<{ resolve: (v: unknown) => void; reject: (e: Error) => void }> = [];
  let held = false; // while true, queries wait (the cold rebuild after a say)
  const plan = {
    root: { package: "p", composition: "decision" },
    subject: { iri: `https://kton.dev/o/${"1".repeat(64)}`, claim: CLAIM },
    uses: [
      { key: "top/summary#counts", block: "@workbench/figures", requires: ["query@1"], optional: [], query: { package: "p", name: "dm-figures", hash: "h" }, params: {}, options: {} },
      { key: "top/spot#cards", block: "@workbench/cards", requires: ["query@1"], optional: ["sign@1"], query: { package: "p", name: "spotchecks", hash: "h2" }, params: {}, options: { target: "target" }, acts: [{ intent: "sign", label: "Sign verdict…", control: { package: "p", name: "spotcheck" }, about: CLAIM, fields: { proposal: CLAIM } }] },
    ],
  } as unknown as Plan;
  const loaded = { controls: new Map([["p/spotcheck", control]]), byHash: new Map(), packages: {} } as unknown as Loaded;
  const backend = {
    query: async (_p: string, name: string) => {
      if (held) return new Promise((resolve, reject) => pending.push({ resolve, reject }));
      n += 1;
      return name === "spotchecks" ? { head: { vars: ["target"] }, results: { bindings: [{ target: { type: "uri", value: "https://example.org/el/item-2305%23e19" } }] } } : answer(120 - n);
    },
    act: async (_c: Control, _about: string, fields: Record<string, string>, _t: string, proposed: Record<string, string>, done?: (s: Signed) => void) => {
      done?.({ id: MADE, fields: { ...fields, ...proposed } });
      return "signed" as const;
    },
  } as unknown as Backend;
  const h = new CompositionHost(plan, loaded, backend, { post: (m) => posted.push(m as Record<string, Any>), log: () => {} }, {}, new Set(["top/spot#cards"]));
  return { h, posted, pending, hold: (v: boolean) => (held = v) };
}
const ask = (id: string, use = "top/summary#counts") => ({ type: "kton:request", id, cap: "query@1", args: { use } });
const sign = { type: "kton:intent", intent: { id: "i1", kind: "sign", from: "top/spot#cards", about: CLAIM, target: "https://example.org/el/item-2305%23e19", control: { package: "p", name: "spotcheck" }, fields: { verdict: "rule-judge-right" } } };
const tick = () => new Promise((r) => setTimeout(r, 0));

test("refresh: after a signature a re-ask is answered at once with the last answer, marked; the fresh one follows", async () => {
  const { h, posted, pending, hold } = host();
  await h.onMessage(ask("q1"));
  const first = posted.at(-1);
  assert.equal(first?.["type"], "kton:data");
  assert.equal(first?.["result"].results.bindings[0].n.value, "119");
  assert.equal("refreshing" in first?.["result"], false, "a first answer is plain");
  await h.onMessage(ask("q1b", "top/spot#cards"));
  await h.onMessage(sign);
  // the signature is confirmed at once, before any refresh: in the act's answer and as a message the frame shows
  const signed = posted.find((m) => m["type"] === "kton-workbench:signed");
  assert.equal(signed?.["claim"], MADE);
  assert.equal(signed?.["what"], "Sign verdict · item-2305#e19 · verdict rule-judge-right");
  const done = posted.find((m) => m["type"] === "kton:data" && m["id"] === "i1");
  assert.match(done?.["result"].message, /^Signed: Sign verdict · item-2305#e19 · verdict rule-judge-right \(claim ab12cd34\)\. Signed with the person key \(test-only\)\.$/);
  assert.equal(done?.["result"].claim, MADE);
  // the frame reloads (the dialog took its column): set carries the signature, the asks get the last answer now
  hold(true);
  await h.onMessage({ type: "kton:ready" });
  assert.deepEqual(posted.filter((m) => m["type"] === "kton:set").at(-1)?.["signed"], { what: signed?.["what"], claim: MADE, at: signed?.["at"] });
  await h.onMessage(ask("q2"));
  const marked = posted.find((m) => m["type"] === "kton-workbench:refreshing");
  const now = posted.find((m) => m["type"] === "kton:data" && m["id"] === "q2");
  assert.ok(now, "answered without waiting for the cockpit");
  assert.equal(now?.["result"].results.bindings[0].n.value, "119", "the last answer");
  assert.deepEqual(now?.["result"].refreshing, { since: signed?.["at"], token: marked?.["token"] });
  assert.equal(marked?.["use"], "top/summary#counts");
  assert.equal(pending.length, 1, "the fresh ask runs in the background");
  await h.onMessage(ask("q3"));
  assert.equal(pending.length, 1, "one refresh per answer at a time");
  assert.equal(posted.find((m) => m["type"] === "kton:data" && m["id"] === "q3")?.["result"].refreshing.token, marked?.["token"]);
  pending[0]?.resolve(answer(100));
  await tick();
  const fresh = posted.find((m) => m["type"] === "kton-workbench:answer");
  assert.deepEqual(fresh, { type: "kton-workbench:answer", use: "top/summary#counts", token: marked?.["token"], result: answer(100) });
  // now fresh: the next ask runs as before
  hold(false);
  await h.onMessage(ask("q4"));
  assert.equal("refreshing" in (posted.find((m) => m["type"] === "kton:data" && m["id"] === "q4")?.["result"] ?? {}), false);
});

test("refresh: a failed refresh posts the error; the frame keeps the last answer, marked stale", async () => {
  const { h, posted, pending, hold } = host();
  await h.onMessage(ask("q1"));
  await h.onMessage(ask("q1b", "top/spot#cards"));
  await h.onMessage(sign);
  hold(true);
  await h.onMessage(ask("q2"));
  pending[0]?.reject(new Error("the cockpit timed out"));
  await tick();
  const m = posted.find((x) => x["type"] === "kton-workbench:answer");
  assert.equal(m?.["error"], "the cockpit timed out");
  assert.equal("result" in (m ?? {}), false);
  assert.ok(Number.isFinite(m?.["since"]));
});

test("refresh: helpers: the answer key, the short claim, what was signed", () => {
  assert.notEqual(answerKey("u", {}), answerKey("u", {}, { poll: "2" }), "a refine is part of the key");
  assert.equal(shortClaim(MADE), "ab12cd34");
  assert.equal(signedWhat({ name: "rule-case", title: "Settle the rule case" }, {}, { target: "https://example.org/qa/cases#rule-case-9", fields: { answer: "exercised" } }), "Settle the rule case · rule-case-9 · answer exercised");
  assert.equal(signedWhat({ name: "decides" }, { label: "Choose and sign…" }, { fields: { lookedAt: "x ".repeat(40) } }), "Choose and sign", "long fields stay in the dialog and the claim");
});

test("refresh: the frame's texts and its store of fresh answers (design/refresh.js)", async () => {
  const r = await media("design/refresh.js");
  const since = new Date(2026, 10, 21, 14, 2, 0).getTime();
  assert.equal(r.updatingText(since, since + 37_000), "Updating after your signature (since 14:02, about 37 s). Showing the last answer until then.");
  assert.equal(r.updatingText(since, since + 150_000, false), "Updating (since 14:02, about 3 min). Showing the last answer until then.");
  assert.equal(r.staleText("the cockpit  timed out\n", since), "Not updated: the cockpit timed out. Showing the answer from before 14:02.");
  assert.equal(r.signedText({ what: "Sign verdict · item-2305#e19", claim: MADE, at: since }), "Signed: Sign verdict · item-2305#e19 (claim ab12cd34) at 14:02");
  const s = r.answerStore();
  const early = s.fresh("u", 2);
  assert.equal(s.put({ use: "u", token: 1, result: 1 }), true);
  assert.equal(s.has("u", 2), false);
  s.put({ use: "u", token: 2, result: 2 });
  assert.deepEqual(await early, { use: "u", token: 2, result: 2 }, "a waiter gets the answer for its token");
  assert.deepEqual(await s.fresh("u", 1), { use: "u", token: 2, result: 2 }, "a later answer serves an earlier token");
  assert.equal(s.put({ use: "u", token: 1, result: "old" }), false, "an older answer never replaces a newer one");
});

test("refresh: a block draws the last answer at once and the fresh one over it; a failed refresh leaves it (no skeleton)", async () => {
  const m = await media("viewers/figures/figures.js");
  const stale = { head: { vars: ["label", "value", "stamp"] }, results: { bindings: [{ label: { type: "literal", value: "unsettled" }, value: { type: "literal", value: "120" }, stamp: { type: "literal", value: "s" } }] }, refreshing: { since: 1, token: 7 } };
  const fresh = { head: stale.head, results: { bindings: [{ label: { type: "literal", value: "unsettled" }, value: { type: "literal", value: "119" }, stamp: { type: "literal", value: "s" } }] } };
  let give: (x: unknown) => void = () => {};
  (globalThis as Any).ktonWorkbench = { fresh: (use: string, token: number) => (assert.equal(use, "top/summary#counts"), assert.equal(token, 7), new Promise((r) => (give = r))) };
  try {
    const el = { innerHTML: "", querySelector: () => null, querySelectorAll: () => [], insertAdjacentHTML() {} };
    m.mount(el, { use: { key: "top/summary#counts", options: {} }, kit: { esc: (x: string) => String(x) }, request: async () => stale, intent: async () => ({}), setState: () => {} });
    await tick();
    assert.match(el.innerHTML, /120/, "the last answer, at once");
    assert.doesNotMatch(el.innerHTML, /data-area-loading/);
    give({ use: "top/summary#counts", token: 7, result: fresh });
    await tick();
    assert.match(el.innerHTML, /119/, "the fresh answer, drawn over it");
    // a failed refresh: the block keeps what it drew
    const el2 = { innerHTML: "", querySelector: () => null, querySelectorAll: () => [], insertAdjacentHTML() {} };
    m.mount(el2, { use: { key: "top/summary#counts", options: {} }, kit: { esc: (x: string) => String(x) }, request: async () => stale, intent: async () => ({}), setState: () => {} });
    await tick();
    give({ use: "top/summary#counts", token: 7, error: "timed out", since: 1 });
    await tick();
    assert.match(el2.innerHTML, /120/);
  } finally {
    delete (globalThis as Any).ktonWorkbench;
  }
});

test("the workbench's page modules all parse as ES modules (frame-design.js is only run in a page: a SyntaxError there drops every chip and the nav)", async () => {
  const { spawnSync } = await import("node:child_process");
  const { readdirSync, readFileSync } = await import("node:fs");
  const base = join(process.cwd(), "media", "workbench");
  const files = [...readdirSync(join(base, "design")).filter((f) => f.endsWith(".js")).map((f) => join(base, "design", f)), ...readdirSync(join(base, "viewers")).map((v) => join(base, "viewers", v, `${v}.js`))];
  assert.ok(files.length >= 10);
  for (const f of files) {
    const r = spawnSync(process.execPath, ["--input-type=module", "--check"], { input: readFileSync(f) });
    assert.equal(r.status, 0, `${f}: ${String(r.stderr).split("\n").slice(0, 5).join("\n")}`);
  }
});

test("host: the same query asked twice while it runs is one ask (a poll never stacks on a pending ask of its block)", async () => {
  const { CompositionHost } = await import("../src/composition/host.js");
  let calls = 0;
  let release!: () => void;
  const wait = new Promise<void>((r) => (release = r));
  const plan = { root: { package: "p", composition: "c" }, subject: { iri: "s" }, uses: [{ key: "c#b", block: "rows", requires: ["query@1"], optional: [], query: { package: "p", name: "q", hash: "h" } }] };
  const posted: Array<Record<string, unknown>> = [];
  const backend = { query: async () => { calls++; await wait; return { results: { bindings: [] } }; }, bytes: async () => new Uint8Array(), open: async () => "", act: async () => "cancelled" as const, run: async () => "" };
  const h = new CompositionHost(plan as never, { packages: {}, controls: new Map() } as never, backend as never, { post: (m) => posted.push(m as Record<string, unknown>), log: () => {} });
  const a = h.onMessage({ type: "kton:request", id: "1", cap: "query@1", args: { use: "c#b" } });
  const b = h.onMessage({ type: "kton:request", id: "2", cap: "query@1", args: { use: "c#b" } });
  await new Promise((r) => setTimeout(r, 5));
  release();
  await Promise.all([a, b]);
  assert.equal(calls, 1, "one ask for both");
  assert.equal(posted.filter((m) => m["type"] === "kton:data").length, 2, "both requests answered");
});
