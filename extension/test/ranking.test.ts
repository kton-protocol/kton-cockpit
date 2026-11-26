// Ranking speed: batch signing (several claims, one sign dialog), a queue that proposes fields from the row's columns,
// and a view that asks again when claims arrive from outside the window.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { DialogConsent, plan, planBatch, signBatch, type ControlInfo, type SignPlan } from "../src/sign.js";
import { signBatchBody, signBatchDetail, signDialogPage } from "../src/ui/pages.js";
import { CompositionHost, defaultsOf, MAX_BATCH, type Backend, type BatchItem, type BatchOutcome, type Plan } from "../src/composition/host.js";
import { BATCH_UNSUPPORTED, TransportError, TransportNotSent, type Outcome, type SayBatchResult, type SayRequest, type SayResult, type TemplateInfo, type Transport } from "../src/transport/port.js";
import type { WindowIdentity } from "../src/identity.js";
import type { Control, Loaded } from "../src/packages/model.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Any = any;
const media = (p: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", p)).href);
const HEX = (c: string) => c.repeat(64);
const sha = (c: string) => `sha256:${HEX(c)}`;
const TPL: TemplateInfo[] = [
  { name: "rank", package: "rk", hash: sha("e"), revision: sha("f"), fields: { item: { type: "string", required: true }, grade: { type: "enum", required: true, values: ["a", "b", "c"] }, round: { type: "string" } } },
];
const control: ControlInfo = { package: "rk", name: "rank", template: "rank", kind: "decision", title: "Rank an item" };
const identity: WindowIdentity = { hintName: "pers", person: { role: "person", keyName: "pers", keyId: "1111222233334444" }, agent: { role: "agent", keyName: "pers-agent", keyId: "aaaabbbbccccdddd" } };
const plans = (n: number, grades = ["a", "b", "c"]): SignPlan[] =>
  Array.from({ length: n }, (_, i) => {
    const p = plan({ kind: "sign", about: sha(String(i + 1)), fields: { item: `i${i}`, grade: grades[i % grades.length] as string } }, control, TPL, identity);
    assert.ok(p.ok);
    return (p as { value: SignPlan }).value;
  });
const signer = { verified: true, name: "Pers", tier: "person", hint: "pers", why: "" } as Any;

class SayT {
  says: SayRequest[] = [];
  outcomes: Array<Outcome<SayResult> | Error> = [];
  async say(req: SayRequest): Promise<Outcome<SayResult>> {
    const o = this.outcomes.shift();
    if (o instanceof Error) throw o;
    if (o && !o.ok) return o;
    this.says.push(req);
    return o ?? { ok: true, value: { claimId: sha(String(this.says.length)), confirmation: "registered" } };
  }
}

test("batch: the digest covers the ordered array of the plans' shown objects; the order, a field or one plan fewer changes it", () => {
  const a = planBatch(plans(3));
  assert.ok(a.ok);
  const same = planBatch(plans(3));
  assert.ok(same.ok && same.value.digest === a.value.digest, "the same plans, the same digest");
  const ps = plans(3);
  const reversed = planBatch([...ps].reverse());
  assert.ok(reversed.ok && reversed.value.digest !== a.value.digest, "order is covered");
  const fewer = planBatch(plans(2));
  assert.ok(fewer.ok && fewer.value.digest !== a.value.digest);
  const other = planBatch(plans(3, ["c", "b", "a"]));
  assert.ok(other.ok && other.value.digest !== a.value.digest, "a field value is covered");
  assert.ok(ps.every((p) => p.digest !== a.value.digest), "and it is not a single plan's digest");
});

test("batch: nothing is planned that would not pass a single say; an empty batch, a path as subject and two identical plans are refused, naming the claim", () => {
  const empty = planBatch([]);
  assert.ok(!empty.ok && empty.refusal.code === "batch.empty");
  const ps = plans(3);
  const bad = plan({ kind: "sign", about: "some/path", fields: { item: "x", grade: "a" } }, control, TPL, identity);
  assert.ok(bad.ok);
  const refused = planBatch([ps[0] as SignPlan, (bad as { value: SignPlan }).value, ps[2] as SignPlan]);
  assert.ok(!refused.ok);
  assert.match(refused.refusal.reason, /claim 2 of 3 \(some\/path\): .*never a path/);
  const dup = planBatch([ps[0] as SignPlan, ps[1] as SignPlan, ps[0] as SignPlan]);
  assert.ok(!dup.ok && dup.refusal.code === "batch.duplicate");
  assert.match(dup.refusal.reason, /claims 1 and 3 of 3/);
});

test("batch: the person key is used only with the dialog's consent for exactly these plans; without it say is never called", async () => {
  const t = new SayT();
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const none = await signBatch(t as unknown as Transport, b.value, undefined);
  assert.ok(!none.ok && none.refusal.code === "dialog.required");
  const other = planBatch(plans(2));
  assert.ok(other.ok);
  const wrong = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromBatchDialog(other.value));
  assert.ok(!wrong.ok && wrong.refusal.code === "dialog.mismatch");
  // a single plan's consent is not a batch's
  const single = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromDialog((b.value.plans[0] as SignPlan)));
  assert.ok(!single.ok && single.refusal.code === "dialog.mismatch");
  assert.equal(t.says.length, 0, "no say reached the cockpit");
  const ok = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(ok.ok);
  assert.equal(ok.value.signed.length, 3);
  assert.deepEqual(t.says.map((s) => s.fields?.["item"]), ["i0", "i1", "i2"], "in the order the dialog showed");
  assert.ok(t.says.every((s) => s.as === "pers" && s.expectTemplate === sha("e") && s.expectRevision === sha("f")), "each say carries the person key and the expectations");
});

test("batch: a plan that changes between the dialog and signBatch (consent was for the original digest) signs nothing", async () => {
  const t = new SayT();
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const consent = DialogConsent.fromBatchDialog(b.value);
  // after the dialog showed the original plans, the second one is swapped for a sound plan with other values
  const swapped = plans(3, ["c", "c", "c"])[1] as SignPlan;
  (b.value.plans as SignPlan[])[1] = swapped;
  const r = await signBatch(t as unknown as Transport, b.value, consent);
  assert.ok(!r.ok && r.refusal.code === "dialog.mismatch");
  assert.match(r.refusal.reason, /changed after the dialog/);
  assert.equal(t.says.length, 0, "no say reached the cockpit");
});

test("batch: the first refusal ends the run: those before it stay signed and are reported, the ones after it are not tried; store.busy is retried", async () => {
  const t = new SayT();
  const b = planBatch(plans(4));
  assert.ok(b.ok);
  t.outcomes = [
    { ok: false, refusal: { code: "store.busy", reason: "busy" } },
    { ok: true, value: { claimId: sha("1"), confirmation: "x" } },
    { ok: true, value: { claimId: sha("2"), confirmation: "x" } },
    { ok: false, refusal: { code: "template.changed", reason: "the template changed" } },
  ];
  const retried: number[] = [];
  const r = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value), { attempts: 3, delayMs: 0, wait: async () => undefined, onRetry: (i) => retried.push(i) });
  assert.ok(r.ok);
  assert.deepEqual(r.value.signed.map((s) => s.index), [0, 1]);
  assert.deepEqual(r.value.stopped, { index: 2, code: "template.changed", reason: "the template changed", unknown: false });
  assert.deepEqual(retried, [0]);
  assert.equal(t.says.length, 2, "the fourth was not tried");
  // a lost connection after the say was sent: unknown whether it signed; one that never left: known not
  const u = new SayT();
  u.outcomes = [{ ok: true, value: { claimId: sha("1"), confirmation: "x" } }, new Error("timed out")];
  const lost = await signBatch(u as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(lost.ok && lost.value.stopped?.unknown === true && lost.value.stopped.index === 1);
  const n = new SayT();
  n.outcomes = [new TransportNotSent("cannot run cockpit", "")];
  const never = await signBatch(n as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(never.ok && never.value.stopped?.unknown === false && never.value.signed.length === 0);
});

test("batch: the dialog lists all the claims in order with one confirmation, and the dialog page takes a batch view", () => {
  const ps = plans(3);
  const body = signBatchBody(ps, signer, "Rank");
  assert.match(body, /data-batch="3"/);
  assert.match(body, /<h1>Sign 3 claims<\/h1>/);
  for (const n of [1, 2, 3]) assert.match(body, new RegExp(`data-batch-item="${n}"`));
  assert.ok(body.indexOf('data-batch-item="1"') < body.indexOf('data-batch-item="2"') && body.indexOf('data-batch-item="2"') < body.indexOf('data-batch-item="3"'));
  assert.match(body, /data-field="grade"[^]*?<span class="v mono">a</);
  assert.match(signBatchDetail(ps, signer), /You sign 3 claims with one confirmation[^]*--- 1 of 3 ---[^]*--- 3 of 3 ---/);
  const page = signDialogPage("Rank", signBatchDetail(ps, signer), "pers (pers)", "n", { batch: ps, signer });
  assert.match(page, /data-testid="sign-confirm"/);
  assert.equal((page.match(/data-testid="sign-button"/g) ?? []).length, 1, "one Sign button for all");
});

// ---- the host: a batch intent

const ROWS = ["a", "b", "c"].map((c) => `https://kton.dev/o/${HEX(c)}`);
const CLAIM = sha("1");
const rankControl: Control = { package: "p", name: "rank", template: "rank", kind: "decision" } as Control;

function batchHost(options: Record<string, unknown> = {}, backendOver: Partial<Backend> = {}) {
  const posted: Array<Record<string, Any>> = [];
  const calls: Array<{ items: readonly BatchItem[]; title: string }> = [];
  const planObj = {
    root: { package: "p", composition: "rank" },
    subject: { iri: `https://kton.dev/o/${HEX("1")}`, claim: CLAIM },
    uses: [{ key: "r#q", block: "@workbench/queue", requires: ["query@1"], optional: ["sign@1"], query: { package: "p", name: "q", hash: "h" }, params: {}, options: { target: "item", choices: ["grade=a|A", "grade=b|B"], ...options },
      acts: [{ intent: "sign", label: "Rank", control: { package: "p", name: "rank" }, about: CLAIM, fields: { item: "x" } }] }],
  } as unknown as Plan;
  const loaded = { controls: new Map([["p/rank", rankControl]]), byHash: new Map(), packages: {} } as unknown as Loaded;
  const backend = {
    query: async () => ({ head: { vars: ["item"] }, results: { bindings: ROWS.map((r) => ({ item: { type: "uri", value: r } })) } }),
    act: async () => "failed" as const,
    actBatch: async (_c: Control, items: readonly BatchItem[], title: string, each?: (i: number, s: { id: string; fields: Record<string, string> }) => void) => {
      calls.push({ items, title });
      items.forEach((it: BatchItem, i: number) => each?.(i, { id: sha(String(i + 2)), fields: { ...it.fields, ...it.proposed } }));
      return { result: "signed", signed: items.map((it: BatchItem, i: number) => ({ index: i, id: sha(String(i + 2)), fields: { ...it.fields, ...it.proposed } })) } satisfies BatchOutcome;
    },
    ...backendOver,
  } as unknown as Backend;
  const h = new CompositionHost(planObj, loaded, backend, { post: (m) => posted.push(m as Record<string, Any>), log: () => undefined });
  const serve = () => h.onMessage({ type: "kton:request", id: "q1", cap: "query@1", args: { use: "r#q" } });
  const batch = (extra: Record<string, unknown>) => h.onMessage({ type: "kton:intent", intent: { id: "b1", kind: "sign", from: "r#q", about: CLAIM, control: { package: "p", name: "rank" }, fields: {}, ...extra } });
  return { h, posted, calls, serve, batch };
}
const last = (posted: Array<Record<string, Any>>, type: string) => posted.filter((m) => m["type"] === type).at(-1);

test("batch intent: rows this block was shown, one dialog; the package's declared choices are fields, a row's other values are proposals; the signed rows are held by row", async () => {
  const { h, posted, calls, serve, batch } = batchHost();
  await serve();
  await batch({ targets: [ROWS[0], ROWS[1]], fieldsFor: { [ROWS[0] as string]: { grade: "a", round: "2" }, [ROWS[1] as string]: { grade: "b" } } });
  assert.equal(calls.length, 1, "one call for all rows: one dialog");
  assert.deepEqual(calls[0]?.items, [
    { about: sha("a"), fields: { item: "x", grade: "a" }, proposed: { round: "2" } },
    { about: sha("b"), fields: { item: "x", grade: "b" }, proposed: {} },
  ]);
  const data = last(posted, "kton:data");
  assert.equal(data?.["result"].signed, true);
  assert.equal(data?.["result"].count, 2);
  assert.deepEqual(h.signedActs.map((a) => a.target), [ROWS[0], ROWS[1]]);
  assert.equal(posted.filter((m) => m["type"] === "kton-workbench:signed").length, 2);
});

test("batch intent: a row the block was not shown, a row twice, none, too many, a record act and a use that asks per key are refused before anything is signed", async () => {
  const t = batchHost();
  await t.serve();
  const why = () => last(t.posted, "kton:refused")?.["why"] as string;
  await t.batch({ targets: [ROWS[0], "https://kton.dev/o/" + HEX("9")] });
  assert.match(why(), /not a row this block was shown/);
  await t.batch({ targets: [ROWS[0], ROWS[0]] });
  assert.match(why(), /names a row twice/);
  await t.batch({ targets: [] });
  assert.match(why(), /names no row/);
  await t.batch({ targets: Array.from({ length: MAX_BATCH + 1 }, (_, i) => `r${i}`) });
  assert.match(why(), new RegExp(`at most ${MAX_BATCH}`));
  assert.equal(t.calls.length, 0);
  const each = batchHost({ askEach: ["expected=item|a,b"] });
  await each.serve();
  await each.batch({ targets: [ROWS[0]] });
  assert.match(last(each.posted, "kton:refused")?.["why"], /asks per key[^]*no batch/);
  const rev = batchHost({ requireReviewed: "app|main|reviews" });
  await rev.serve();
  await rev.batch({ targets: [ROWS[0]] });
  assert.match(last(rev.posted, "kton:refused")?.["why"], /no batch/);
  assert.equal(each.calls.length + rev.calls.length, 0);
});

test("batch intent: a failure after some were signed is partial: the signed rows stay held, the result says so; a failure of the first signs none and is refused", async () => {
  const part = batchHost({}, {
    actBatch: async (_c: Control, items: readonly BatchItem[], _t: string, each?: (i: number, s: { id: string; fields: Record<string, string> }) => void) => {
      each?.(0, { id: sha("7"), fields: { ...items[0]?.fields } });
      return { result: "failed", signed: [{ index: 0, id: sha("7"), fields: {} }], why: "Signed 1 of 2. Claim 2 of 2 was refused: no" } as BatchOutcome;
    },
  } as Partial<Backend>);
  await part.serve();
  await part.batch({ targets: [ROWS[0], ROWS[1]], fieldsFor: { [ROWS[0] as string]: { grade: "a" }, [ROWS[1] as string]: { grade: "b" } } });
  const r = last(part.posted, "kton:data")?.["result"];
  assert.equal(r.signed, false);
  assert.equal(r.partial, true);
  assert.equal(r.count, 1);
  assert.deepEqual(part.h.signedActs.map((a) => a.target), [ROWS[0]], "only the signed row is held as decided");
  const none = batchHost({}, { actBatch: async () => ({ result: "failed", signed: [], why: "claim 2 of 2: grade must be one of a|b|c" }) as BatchOutcome } as Partial<Backend>);
  await none.serve();
  await none.batch({ targets: [ROWS[0]] });
  assert.match(last(none.posted, "kton:refused")?.["why"], /grade must be one of/);
  const gone = batchHost({});
  (gone.h as Any).backend.actBatch = undefined;
  await gone.serve();
  await gone.batch({ targets: [ROWS[0]] });
  assert.match(last(gone.posted, "kton:refused")?.["why"], /does not sign several claims at once/);
});

test("R3: the use's option defaults, \"[control:]field=value\", read per control", () => {
  assert.deepEqual(defaultsOf({ defaults: ["grade=b", "other:round=2", "rank:round=3", "bad"] }, "rank"), { grade: "b", round: "3" });
  assert.deepEqual(defaultsOf({ defaults: "grade=a" }, "rank"), { grade: "a" });
  assert.deepEqual(defaultsOf({}, "rank"), {});
});

// ---- R4: a view asks again when claims arrive from outside

test("R4: after claims came from outside, the host sends its frame a new set and answers the re-ask from the last answer, marked refreshing (not 'after your signature'), then posts the fresh one", async () => {
  const posted: Array<Record<string, Any>> = [];
  let n = 0;
  const pending: Array<(v: unknown) => void> = [];
  let held = false;
  const answer = (k: number) => ({ head: { vars: ["n"] }, results: { bindings: [{ n: { type: "literal", value: String(k) } }] } });
  const planObj = { root: { package: "p", composition: "c" }, subject: { iri: `https://kton.dev/o/${HEX("1")}`, claim: CLAIM }, uses: [{ key: "u#q", block: "@workbench/figures", requires: ["query@1"], optional: [], query: { package: "p", name: "q", hash: "h" }, params: {}, options: {} }] } as unknown as Plan;
  const backend = { query: async () => (held ? new Promise((r) => pending.push(r)) : answer(++n)) } as unknown as Backend;
  const h = new CompositionHost(planObj, { controls: new Map(), byHash: new Map(), packages: {} } as unknown as Loaded, backend, { post: (m) => posted.push(m as Record<string, Any>), log: () => undefined });
  assert.equal(h.externalChange(), false, "a view never drawn has nothing to refresh");
  await h.onMessage({ type: "kton:ready" });
  const ask = (id: string) => h.onMessage({ type: "kton:request", id, cap: "query@1", args: { use: "u#q" } });
  await ask("a1");
  const sets = () => posted.filter((m) => m["type"] === "kton:set").length;
  const before = sets();
  assert.equal(h.externalChange(), true);
  assert.equal(sets(), before + 1, "the frame draws anew and asks again");
  held = true;
  await ask("a2");
  const marked = last(posted, "kton-workbench:refreshing");
  assert.equal(marked?.["external"], true);
  const now = posted.find((m) => m["type"] === "kton:data" && m["id"] === "a2");
  assert.equal(now?.["result"].results.bindings[0].n.value, "1", "the last answer at once");
  assert.equal(now?.["result"].refreshing.token, marked?.["token"]);
  pending[0]?.(answer(2));
  await new Promise((r) => setTimeout(r, 0));
  assert.equal(last(posted, "kton-workbench:answer")?.["result"].results.bindings[0].n.value, "2", "the fresh answer follows");
  // no busy loop: nothing asked again by itself
  assert.equal(pending.length, 1);
});

// ---- R1: the queue proposes fields from the row's columns

const b = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { value: v }]));
const esc = (s: string) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string);
const tick = () => new Promise((r) => setTimeout(r, 10));

async function queue(options: Record<string, unknown>, intent: (i: Any) => Promise<Any>, rows: Array<Record<string, string>>) {
  const m = await media("viewers/queue/queue.js");
  const handlers = new Map<string, () => void>();
  const el: Any = { innerHTML: "", isConnected: true, querySelector: (sel: string) => (sel.startsWith("[data-sign-all]") || sel === "[data-sign-all]" ? { addEventListener: (_e: string, f: () => void) => handlers.set("sign-all", f) } : null), querySelectorAll: () => [] };
  const doc = new EventTarget();
  const prev = (globalThis as Any).document;
  (globalThis as Any).document = doc;
  (globalThis as Any).ktonWorkbench = { signedActs: () => [] };
  const handle = m.mount(el, { use: { key: "q", options: { key: "key", title: "key", target: "target", ...options }, blockActs: [{ intent: "sign", control: { package: "p", name: "rank" }, about: CLAIM }] }, kit: { esc }, state: {}, request: async () => ({ results: { bindings: rows.map(b) } }), intent, setState: () => undefined });
  await tick();
  const press = async (key: string) => {
    doc.dispatchEvent(Object.assign(new Event("keydown"), { key }));
    await tick();
  };
  const done = () => {
    handle.destroy?.();
    (globalThis as Any).document = prev;
  };
  return { el, press, handlers, done };
}
const QROWS = [
  { key: "i1", target: ROWS[0] as string, round: "2", cls: "K1", pos: "3" },
  { key: "i2", target: ROWS[1] as string, round: "2", cls: "K2" },
];

test("R1: the queue proposes a field from a column of the row, as the rows block does: key 1 accepts the proposal (a choice with no value), fields like round, class, position come from the row", async () => {
  const intents: Any[] = [];
  const q = await queue({ choices: ["grade=|Accept the proposal", "grade=c|Not ranked"], propose: ["round=round", "class=cls", "position=pos", "grade=pos"] }, async (i) => (intents.push(i), { result: { signed: true, message: "Signed." } }), QROWS);
  try {
    await q.press("1");
    assert.equal(intents.length, 1);
    assert.deepEqual(intents[0].fields, { round: "2", class: "K1", position: "3", grade: "3" }, "the row's columns are proposed; the accept choice sets no value of its own");
    assert.equal(intents[0].target, ROWS[0]);
    // the second item has no position: that field is not proposed; a choice with a value still sets its own
    await q.press("2");
    assert.deepEqual(intents[1].fields, { round: "2", class: "K2", grade: "c" });
    assert.match(q.el.innerHTML, /Nothing open in this group|Item/);
  } finally {
    q.done();
  }
});

// ---- R2 in the queue: decisions wait in a summary and are signed together

test("R2: with option batch a decision is kept for the summary (no dialog), the summary lists it and can take it back, and 'Sign all' sends ONE intent for all rows with their fields", async () => {
  const intents: Any[] = [];
  const q = await queue({ choices: ["grade=a|A", "grade=b|B"], propose: ["round=round"], batch: "Sign all" }, async (i) => (intents.push(i), { result: { signed: true, message: "Signed 2 claims.", claims: [sha("5"), sha("6")] } }), QROWS);
  try {
    await q.press("1");
    await q.press("2");
    assert.equal(intents.length, 0, "nothing is signed by a choice");
    assert.match(q.el.innerHTML, /data-pending="2"/);
    assert.match(q.el.innerHTML, /data-testid="queue-sign-all"[^>]*>Sign all 2</);
    assert.match(q.el.innerHTML, /data-pending-item="i1"/);
    q.handlers.get("sign-all")?.();
    await tick();
    assert.equal(intents.length, 1);
    assert.deepEqual(intents[0].targets, [ROWS[0], ROWS[1]]);
    assert.deepEqual(intents[0].fieldsFor, { [ROWS[0] as string]: { round: "2", grade: "a" }, [ROWS[1] as string]: { round: "2", grade: "b" } });
    assert.equal(intents[0].kind, "sign");
    assert.doesNotMatch(q.el.innerHTML, /data-pending=/, "signed: the summary is empty");
  } finally {
    q.done();
  }
});

class BatchT extends SayT {
  batches: SayRequest[][] = [];
  answers: Array<Outcome<SayBatchResult> | Error> = [];
  async sayBatch(reqs: readonly SayRequest[]): Promise<Outcome<SayBatchResult>> {
    this.batches.push([...reqs]);
    const o = this.answers.shift();
    if (o instanceof Error) throw o;
    return o ?? { ok: true, value: { claims: reqs.map((_, i) => ({ claimId: sha(String(i + 1)), confirmation: "one batch" })) } };
  }
}

test("batch write: a transport that offers sayBatch gets ONE call with the N requests in order, and the ids come back in order", async () => {
  const t = new BatchT();
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const r = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(r.ok);
  assert.equal(t.batches.length, 1);
  assert.equal(t.says.length, 0, "no single say");
  assert.deepEqual(t.batches[0]?.map((s) => s.fields?.["item"]), ["i0", "i1", "i2"]);
  assert.ok(t.batches[0]?.every((s) => s.as === "pers" && s.expectTemplate === sha("e") && s.expectRevision === sha("f")));
  assert.deepEqual(r.value.signed.map((s) => [s.index, s.id]), [[0, sha("1")], [1, sha("2")], [2, sha("3")]]);
  assert.equal(r.value.stopped, undefined);
  // the batch's pending marks every claim of it
  t.answers = [{ ok: true, value: { claims: [0, 1, 2].map((i) => ({ claimId: sha(String(i + 1)), confirmation: "x" })), pending: { code: "store.busy", reason: "next write commits" } } }];
  const p = await signBatch(t as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(p.ok && p.value.signed.every((s) => s.pending?.code === "store.busy"));
});

test("batch write: a refusal signs nothing and is reported whole; store.busy is retried; an unknown outcome is not retried", async () => {
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const consent = DialogConsent.fromBatchDialog(b.value);
  const t = new BatchT();
  t.answers = [{ ok: false, refusal: { code: "argument", reason: "claim 2: nope" } }];
  const r = await signBatch(t as unknown as Transport, b.value, consent, { attempts: 3, delayMs: 0, wait: async () => undefined });
  assert.ok(r.ok && r.value.signed.length === 0);
  assert.deepEqual(r.value.stopped, { index: 0, code: "argument", reason: "claim 2: nope", unknown: false, whole: true });
  assert.equal(t.batches.length, 1, "a refusal is not retried");
  const busy = new BatchT();
  busy.answers = [{ ok: false, refusal: { code: "store.busy", reason: "busy" } }];
  const again: number[] = [];
  const ok = await signBatch(busy as unknown as Transport, b.value, consent, { attempts: 3, delayMs: 0, wait: async () => undefined, onRetry: (i) => again.push(i) });
  assert.ok(ok.ok && ok.value.signed.length === 3 && !ok.value.stopped);
  assert.equal(busy.batches.length, 2);
  for (const e of [new TransportError("the answer is not JSON", 0, ""), new Error("timed out")]) {
    const u = new BatchT();
    u.answers = [e];
    const lost = await signBatch(u as unknown as Transport, b.value, consent, { attempts: 3, delayMs: 0, wait: async () => undefined });
    assert.ok(lost.ok && lost.value.signed.length === 0);
    assert.equal(lost.value.stopped?.unknown, true);
    assert.equal(lost.value.stopped?.whole, true);
    assert.equal(u.batches.length, 1, "never retried");
    assert.equal(u.says.length, 0, "and not said one by one either");
  }
  const n = new BatchT();
  n.answers = [new TransportNotSent("cannot run cockpit", "")];
  const never = await signBatch(n as unknown as Transport, b.value, consent);
  assert.ok(never.ok && never.value.stopped?.unknown === false && never.value.signed.length === 0);
  // an answer with the wrong number of claims is unknown, not a success
  const w = new BatchT();
  w.answers = [{ ok: true, value: { claims: [{ claimId: sha("1"), confirmation: "x" }] } }];
  const wrong = await signBatch(w as unknown as Transport, b.value, consent);
  assert.ok(wrong.ok && wrong.value.stopped?.unknown === true && wrong.value.signed.length === 0);
});

test("batch write: the dialog checks are before it: no consent or changed plans never reach sayBatch; unsupported falls back to N says", async () => {
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const t = new BatchT();
  const none = await signBatch(t as unknown as Transport, b.value, undefined);
  assert.ok(!none.ok && none.refusal.code === "dialog.required");
  assert.equal(t.batches.length, 0);
  const u = new BatchT();
  u.answers = [{ ok: false, refusal: { code: BATCH_UNSUPPORTED, reason: "no say/batch" } }];
  const r = await signBatch(u as unknown as Transport, b.value, DialogConsent.fromBatchDialog(b.value));
  assert.ok(r.ok && r.value.signed.length === 3 && !r.value.stopped);
  assert.equal(u.says.length, 3, "said one by one");
});

test("batch write: a refusal after the check phase is unknown, never retried; one before storing is nothing signed", async () => {
  const b = planBatch(plans(3));
  assert.ok(b.ok);
  const consent = DialogConsent.fromBatchDialog(b.value);
  const cases: Array<[string, string, boolean]> = [
    ["anchor", "the claim was registered but anchoring it failed: x", true],
    ["kernel", "(2 claim(s) are stored, not committed)", true],
    ["binding", "no repository", false],
  ];
  for (const [code, reason, unknown] of cases) {
    const t = new BatchT();
    t.answers = [{ ok: false, refusal: { code, reason } }];
    const r = await signBatch(t as unknown as Transport, b.value, consent, { attempts: 3, delayMs: 0, wait: async () => {} });
    assert.ok(r.ok);
    assert.equal(r.value.signed.length, 0);
    assert.equal(r.value.stopped?.unknown, unknown, code);
    assert.equal(r.value.stopped?.code, code);
    assert.equal(t.batches.length, 1, "no retry");
    assert.equal(t.says.length, 0, "never re-said one by one");
  }
});
