// What the window waits for: the queue's state over time, the status bar text, the views' banner.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { AskGate } from "../src/transport/gate.js";
import { indexStatusText, READY_MS } from "../src/ui/index-status.js";

const tick = () => new Promise((r) => setTimeout(r, 5));
const at = (hh: number, mm: number, ss = 0) => new Date(2026, 10, 23, hh, mm, ss).getTime();

test("gate state: at start cold; the first ask builds (since when), the others wait; ready when it answers; a timeout leaves it cold with the reason", async () => {
  let now = at(14, 2, 0);
  const g = new AskGate(2, () => now);
  const seen: string[] = [];
  g.onChange((s) => seen.push(`${s.cold ? "cold" : "warm"}/${s.buildingSince !== undefined ? "building" : "-"}/${s.inFlight}/${s.queued}`));
  assert.equal(g.state().cold, true, "a cold index on window start counts");
  let release!: (v: unknown) => void;
  const first = g.run(() => new Promise((r) => (release = r)));
  const second = g.run(async () => ({ ok: true }));
  await tick();
  assert.equal(g.state().buildingSince, at(14, 2, 0));
  assert.equal(g.state().queued, 1);
  now = at(14, 3, 10);
  release({ ok: true });
  await Promise.all([first, second]);
  assert.equal(g.state().cold, false);
  assert.equal(g.state().readyAt, at(14, 3, 10));
  assert.ok(seen.includes("cold/building/1/1"), seen.join(" "));
  g.markCold();
  now = at(14, 5, 0);
  await g.run(async () => ({ ok: false, refusal: { code: "query.timeout", reason: "the query ran longer than its timeout (110s)" } }));
  assert.equal(g.state().cold, true, "a timeout does not warm it");
  assert.equal(g.state().lastError, undefined, "a timeout is still building, not an error");
  assert.equal(g.state().buildingSince, at(14, 5, 0), "building since the first cold ask, across asks");
  now = at(14, 6, 0);
  await assert.rejects(g.run(async () => { throw new Error("the index daemon is starting (waited 30 s)"); }));
  assert.equal(g.state().buildingSince, at(14, 5, 0), "a daemon starting slowly is still building");
  await assert.rejects(g.run(async () => { throw new Error("cockpit: /q/store: Permission denied"); }));
  assert.deepEqual(g.state().lastError, { message: "cockpit: /q/store: Permission denied", at: at(14, 6, 0) }, "a real failure, with its reason");
  assert.equal(g.state().buildingSince, undefined);
  await g.run(async () => ({ ok: false, refusal: { code: "query.unknown", reason: "no query foo" } }));
  assert.equal(g.state().cold, false, "a refusal about the query itself means the index answered");
});

test("status bar: building with a spinner, since hh:mm and seconds; then ready briefly; n waiting; the failure", () => {
  const since = at(14, 2, 0);
  assert.equal(indexStatusText({ cold: true, buildingSince: since, inFlight: 1, queued: 3 }, since + 47_000)?.text, "$(sync~spin) kton: building the index… since 14:02 (47 s) · 3 waiting");
  assert.equal(indexStatusText({ cold: false, inFlight: 0, queued: 0, readyAt: since }, since + 1000)?.text, "$(check) kton: ready");
  assert.equal(indexStatusText({ cold: false, inFlight: 0, queued: 0, readyAt: since }, since + READY_MS + 1), undefined, "then the normal items");
  assert.equal(indexStatusText({ cold: false, inFlight: 2, queued: 3 }, since)?.text, "$(clock) kton: 3 waiting");
  assert.match(indexStatusText({ cold: true, inFlight: 0, queued: 0, lastError: { message: "the query ran longer than its timeout (110s)", at: since } }, since)?.text ?? "", /^\$\(warning\) kton: not loaded · the query ran longer/);
});

test("view banner: what is happening, since when and how long; a failure with its reason; nothing when nothing waits", async () => {
  const { indexBannerText } = await import(pathToFileURL(join(process.cwd(), "media", "workbench", "design", "refresh.js")).href);
  const since = at(14, 2, 0);
  assert.deepEqual(indexBannerText({ cold: true, buildingSince: since, inFlight: 1, queued: 2 }, since + 65_000), { kind: "building", text: "Loading data: the index is being built after a change (first load takes about 1–2 min; started 14:02, 65 s so far). 2 asks wait for it." });
  assert.equal(indexBannerText({ cold: true, inFlight: 0, queued: 0, lastError: { message: "the query ran  longer than its timeout", at: since } }, since).text, "Not loaded: the query ran longer than its timeout (at 14:02). The last answers stay shown.");
  assert.equal(indexBannerText({ cold: false, inFlight: 2, queued: 4 }, since).kind, "waiting");
  assert.equal(indexBannerText({ cold: false, inFlight: 1, queued: 0 }, since), null);
});

test("a daemon that is down is an error at once; a timeout is building until the limit, then an error; a bare 'daemon' is not building", async () => {
  let now = at(10, 0, 0);
  const g = new AskGate(2, () => now, 200_000);
  await assert.rejects(g.run(async () => { throw new Error("the index daemon is not running"); }));
  assert.equal(g.state().buildingSince, undefined, "down is not building");
  assert.equal(g.state().lastError?.unreachable, true);
  const t = indexStatusText(g.state(), now);
  assert.match(t?.text ?? "", /^\$\(error\) kton: index not reachable · the index daemon is not running/);
  assert.match(t?.tooltip ?? "", /cockpit doctor/);
  await g.run(async () => ({ ok: true }));
  assert.equal(g.state().lastError, undefined, "an answer clears it");

  g.markCold();
  now = at(10, 1, 0);
  await g.run(async () => ({ ok: false, refusal: { code: "x", reason: "the index daemon is unreachable: connection refused" } }));
  assert.equal(g.state().lastError?.unreachable, true, "a refusal that says down counts the same");
  await g.run(async () => ({ ok: true }));

  g.markCold();
  now = at(10, 2, 0);
  await assert.rejects(g.run(async () => { throw new Error("query timed out after 110 s (cancelled)"); }));
  assert.equal(g.state().buildingSince, at(10, 2, 0));
  assert.equal(g.state().lastError, undefined, "a first timeout is building");
  now = at(10, 2, 0) + 150_000;
  await assert.rejects(g.run(async () => { throw new Error("query timed out after 110 s (cancelled)"); }));
  assert.equal(g.state().lastError, undefined, "still inside the limit");
  now = at(10, 2, 0) + 201_000;
  await assert.rejects(g.run(async () => { throw new Error("query timed out after 110 s (cancelled)"); }));
  assert.equal(g.state().lastError?.unreachable, true, "building for longer than the limit is an error");
  assert.equal(g.state().buildingSince, undefined);
  assert.match(indexStatusText(g.state(), now)?.text ?? "", /index not reachable/);

  const h = new AskGate(2, () => now);
  await assert.rejects(h.run(async () => { throw new Error("index daemon refused: out of memory"); }));
  assert.equal(h.state().buildingSince, undefined);
  assert.equal(h.state().lastError?.message, "index daemon refused: out of memory");
  assert.match(indexStatusText(h.state(), now)?.text ?? "", /not loaded/);
});

test("view banner: an unreachable index says so and points at cockpit doctor", async () => {
  const { indexBannerText } = await import(pathToFileURL(join(process.cwd(), "media", "workbench", "design", "refresh.js")).href);
  const since = at(14, 2, 0);
  const b = indexBannerText({ cold: true, inFlight: 0, queued: 0, lastError: { message: "the index daemon is not running", at: since, unreachable: true } }, since);
  assert.match(b.text, /^Index not reachable: the index daemon is not running \(at 14:02\)\. Run `cockpit doctor`/);
});
