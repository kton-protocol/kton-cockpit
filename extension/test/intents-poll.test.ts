import { test } from "node:test";
import assert from "node:assert/strict";
import { applies, bindInputs, checkIntent, type SlotContext, type UiEntry } from "../src/intents.js";
import { Scheduler, type Clock } from "../src/poll.js";
import { SUBJECT, SCOPE, CLAIM } from "./helpers.js";

const ctx: SlotContext = { subject: { id: SUBJECT, kind: "foton", type: "nk:document-source", path: "docs/report.Rmd", scope: SCOPE }, identityName: "person-a" };
const render: UiEntry = {
  package: "render",
  name: "review",
  slot: "review-view",
  when: { kind: "foton", type: "nk:document-source" },
  viewer: "render",
  ray: "render",
  control: "approve",
  inputs: { DOC: "subject.id", source: "subject.path" },
  intents: ["sign", "open"],
};

test("inputs bind only from the closed set of shell values", () => {
  const ok = bindInputs(render, ctx);
  assert.ok(ok.ok);
  assert.deepEqual(ok.value, { DOC: SUBJECT, source: "docs/report.Rmd" });
  const bad = bindInputs({ ...render, inputs: { X: "identity.key" } }, ctx);
  assert.ok(!bad.ok);
  assert.equal(bad.refusal.reason, "invalid-arguments");
});

test("when matches kind and type", () => {
  assert.ok(applies(render, ctx));
  assert.ok(!applies(render, { ...ctx, subject: { ...ctx.subject, kind: "claim" } }));
  assert.ok(!applies(render, { ...ctx, subject: { ...ctx.subject, type: "nk:other" } }));
});

test("an undeclared intent is outside the declaration, checked before any handler", () => {
  const r = checkIntent({ kind: "run", ray: "render", holes: {} }, render, ctx);
  assert.ok(!r.ok);
  assert.equal(r.refusal.reason, "outside-declaration");
  const u = checkIntent({ kind: "teleport" }, { ...render, intents: ["teleport" as never] }, ctx);
  assert.ok(!u.ok);
  assert.equal(u.refusal.reason, "not-implemented");
});

test("sign may only be about the slot's subject or its scope", () => {
  assert.ok(checkIntent({ kind: "sign", about: SUBJECT, fields: {} }, render, ctx).ok);
  assert.ok(checkIntent({ kind: "sign", about: SCOPE, fields: {} }, render, ctx).ok);
  const elsewhere = checkIntent({ kind: "sign", about: CLAIM, fields: {} }, render, ctx);
  assert.ok(!elsewhere.ok);
  assert.equal(elsewhere.refusal.reason, "invalid-arguments");
  const noControl = checkIntent({ kind: "sign", about: SUBJECT, fields: {} }, { ...render, control: undefined } as unknown as UiEntry, ctx);
  assert.ok(!noControl.ok);
  assert.equal(noControl.refusal.reason, "outside-declaration");
  assert.ok(!checkIntent({ kind: "sign", about: SUBJECT, fields: { n: 1 } }, render, ctx).ok);
});

test("run must name the entry's own ray", () => {
  const e = { ...render, intents: ["run" as const] };
  assert.ok(checkIntent({ kind: "run", ray: "render", holes: { source: "a" } }, e, ctx).ok);
  const other = checkIntent({ kind: "run", ray: "delete-everything", holes: {} }, e, ctx);
  assert.ok(!other.ok);
  assert.equal(other.refusal.reason, "outside-declaration");
});

class FakeClock implements Clock {
  t = 0;
  timers: Array<{ at: number; fn: () => void; id: number }> = [];
  private n = 0;
  now() {
    return this.t;
  }
  setTimeout(fn: () => void, ms: number) {
    const id = ++this.n;
    this.timers.push({ at: this.t + ms, fn, id });
    return id;
  }
  clearTimeout(h: unknown) {
    this.timers = this.timers.filter((x) => x.id !== h);
  }
  async advance(ms: number) {
    const end = this.t + ms;
    for (;;) {
      this.timers.sort((a, b) => a.at - b.at);
      const next = this.timers[0];
      if (!next || next.at > end) break;
      this.timers.shift();
      this.t = next.at;
      next.fn();
      await new Promise((r) => setImmediate(r));
    }
    this.t = end;
  }
}

test("one scheduler: runs, never overlaps a job, records errors, settable interval", async () => {
  const clock = new FakeClock();
  const s = new Scheduler(clock);
  let runs = 0;
  let release: () => void = () => {};
  s.add("inbox", 60_000, async () => {
    runs++;
    if (runs === 2) await new Promise<void>((r) => (release = r));
    if (runs === 3) throw new Error("index down");
  });
  await clock.advance(0);
  assert.equal(runs, 1);
  await clock.advance(60_000);
  assert.equal(runs, 2);
  await s.runNow("inbox");
  assert.equal(runs, 2, "a running job is not started twice");
  release();
  await new Promise((r) => setImmediate(r));
  await clock.advance(60_000);
  assert.equal(runs, 3);
  assert.equal(s.status()[0]?.lastError, "index down");
  s.setInterval("inbox", 5_000);
  await clock.advance(5_000);
  assert.equal(runs, 4);
  assert.equal(s.status()[0]?.lastError, undefined);
  assert.throws(() => s.add("inbox", 60_000, async () => {}));
  assert.throws(() => s.add("fast", 10, async () => {}));
  s.stop();
});

test("setInterval during a running tick leaves one timer chain", async () => {
  const clock = new FakeClock();
  const s = new Scheduler(clock);
  let runs = 0;
  let release: () => void = () => {};
  s.add("inbox", 10_000, async () => {
    runs++;
    if (runs === 1) await new Promise<void>((r) => (release = r));
  });
  await clock.advance(0);
  assert.equal(runs, 1, "first tick running");
  s.setInterval("inbox", 5_000);
  release();
  await new Promise((r) => setImmediate(r));
  await clock.advance(20_000);
  assert.equal(runs, 5, "one chain at 5 s: ticks at 5, 10, 15, 20 s after the first");
  assert.equal(clock.timers.length, 1, "exactly one pending timer");
  s.stop();
});
