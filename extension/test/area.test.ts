// The packages area around its panels (follow-ups to #174): a panel of an old revision never signs; a failed poll
// closes nothing and marks the panels stale; a panel is never closed under an act in progress; tiles ask the
// package's own named query; a leaf's product@1 is its own package's only; the member batch stops at a refusal.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { PackagesArea, openedOf, type ActPath, type CompositionUi, type ExpectRevision } from "../src/composition/area.js";
import { CompositionHost, LeafHost, type Backend } from "../src/composition/host.js";
import { loadPackages, READ_WIDTH, type Control, type Loaded } from "../src/packages/model.js";
import type { Transport } from "../src/transport/port.js";
import type { UiListing } from "../src/packages/listing.js";
import { listed, PKGS, reader, type Fixture } from "./pkg-helpers.js";

const SUBJECT = { kind: "claim" as const, iri: `https://kton.dev/o/${"1".repeat(64)}`, id: `sha256:${"1".repeat(64)}`, claim: `sha256:${"1".repeat(64)}` };
const good = (label = "1", edit?: (p: string, b: Buffer) => Buffer) => listed(join(PKGS, "hostile", "good"), "hostile-good", label, edit);

/** A cockpit whose ui list the test sets (a fixture, or a failure), and what the area asked it. */
function cockpit(first: Fixture) {
  const state: { fx: Fixture | undefined; asked: string[]; queries: string[] } = { fx: first, asked: [], queries: [] };
  const t = {
    uiList: async () => {
      if (!state.fx) throw new Error("the cockpit cannot be reached");
      return { ok: true, value: { packages: [state.fx.pkg] } as UiListing };
    },
    uiCat: async (pkg: string, h: string) => reader(state.fx as Fixture)(pkg, h),
    list: async () => ({ ok: true, value: { queries: state.queries.map((name) => ({ name })) } }),
    ask: async (req: { name: string }) => {
      state.asked.push(req.name);
      return { ok: true, value: { results: { head: { vars: ["n"] }, results: { bindings: [{ n: { type: "literal", value: "3" } }] } } } };
    },
  } as unknown as Transport;
  return { t, state };
}

function windowUi() {
  const log = { closed: [] as string[], stale: [] as Array<[string, boolean]>, errors: [] as string[], info: [] as string[] };
  const ui: CompositionUi = {
    showComposition: () => ({ post: () => undefined, urls: new Map() }),
    showLeaf: () => ({ post: () => undefined }),
    closePanel: (k) => log.closed.push(k),
    markStale: (k, s) => log.stale.push([k, s]),
    openPanels: () => [],
    info: (x) => log.info.push(x),
    error: (x) => log.errors.push(x),
    log: () => undefined,
  };
  return { ui, log };
}

function actPath(answer: () => Promise<"signed" | "cancelled" | "failed"> = async () => "signed") {
  const acts: Control[] = [];
  const expects: Array<ExpectRevision | undefined> = [];
  const path: ActPath = {
    act: async (c, _about, _fields, _title, expect) => {
      acts.push(c);
      expects.push(expect);
      return answer();
    },
    open: async () => "opened",
    identityName: () => "qa-decider",
  };
  return { path, acts, expects };
}

const signIntent = (h: CompositionHost, n: number) => ({ type: "kton:intent", intent: { id: `s#t.${n}`, kind: "sign", from: h.plan.uses[0]?.key, about: SUBJECT.claim, control: { package: "hostile-good", name: "approve" } } });

async function opened(path: ActPath) {
  const c = cockpit(good("1"));
  const w = windowUi();
  const area = new PackagesArea(c.t, w.ui, path);
  await area.refresh();
  const key = `hostile-good/${area.loaded?.entries[0]?.file}`;
  const host = (await area.openSlot(key, SUBJECT)) as CompositionHost;
  assert.ok(host instanceof CompositionHost);
  return { ...c, ...w, area, host, panel: `composition:${key}` };
}

test("stale panels: after an upgrade (same name, new revision) the old panel is closed, and its control never signs", async () => {
  const { path, acts } = actPath();
  const o = await opened(path);
  await o.host.onMessage(signIntent(o.host, 1));
  assert.equal(acts.length, 1, "at revision 1 the act goes through");
  o.state.fx = good("2");
  await o.area.refresh();
  assert.deepEqual(o.log.closed, [o.panel]);
  assert.match(o.log.info.at(-1) ?? "", /package hostile-good changed revision/);
  // the old panel's frame is still there for a moment: its intent is refused, nothing reaches the sign path
  await o.host.onMessage(signIntent(o.host, 2));
  assert.equal(acts.length, 1, "no sign with the old revision's control");
  assert.match(o.log.errors.at(-1) ?? "", /Not signed: Review: the panel is closed/);
});

test("stale panels: an upgrade the area has not polled yet is still refused at the act (the panel's revision is checked then)", async () => {
  const { path, acts } = actPath();
  const o = await opened(path);
  // a second area instance would see revision 2; here the loaded packages change under the open panel
  o.state.fx = good("2");
  const reloaded = await loadPackages({ packages: [o.state.fx.pkg] }, reader(o.state.fx));
  o.area.loaded = reloaded;
  await o.host.onMessage(signIntent(o.host, 1));
  assert.equal(acts.length, 0);
  assert.match(o.log.errors.at(-1) ?? "", /package hostile-good changed revision/);
});

test("failed poll: a failed ui list closes no panel; the panels are marked stale, and current again on the next good poll", async () => {
  const { path, acts } = actPath();
  const o = await opened(path);
  o.state.fx = undefined;
  await o.area.refresh();
  assert.deepEqual(o.log.closed, [], "nothing closed");
  assert.deepEqual(o.log.stale, [[o.panel, true]]);
  assert.match(o.area.error ?? "", /cannot be reached/);
  await o.host.onMessage(signIntent(o.host, 1));
  assert.equal(acts.length, 1, "the panel keeps the last good packages");
  o.state.fx = good("1");
  await o.area.refresh();
  assert.deepEqual(o.log.stale, [[o.panel, true], [o.panel, false]]);
  assert.deepEqual(o.log.closed, []);
});

test("a panel with a sign dialog open is never closed under it: it closes when the dialog is answered; the act carries the revision it was offered at", async () => {
  let answer: (r: "failed") => void = () => undefined;
  const { path, acts, expects } = actPath(() => new Promise((r) => (answer = r)));
  const o = await opened(path);
  const r1 = o.area.loaded?.packages["hostile-good"]?.revision;
  const signing = o.host.onMessage(signIntent(o.host, 1));
  await new Promise((r) => setImmediate(r));
  assert.equal(acts.length, 1, "the dialog is open");
  // the sign path confirms this revision fresh before say (R1 on #177; controller tests): after revision 2, no claim
  assert.deepEqual(expects[0], { package: "hostile-good", revision: r1 });
  o.state.fx = good("2");
  await o.area.refresh();
  assert.deepEqual(o.log.closed, [], "not closed under the dialog");
  answer("failed");
  await signing;
  assert.deepEqual(o.log.closed, [o.panel], "closed once the dialog was answered");
});

test("tiles ask the package's named query: a name two packages bring goes as <package>/<name>", async () => {
  const tile = (p: string, b: Buffer) => (p === "ui/review.json" ? Buffer.from(JSON.stringify({ slot: "home-tile", label: "Items", query: "items" })) : b);
  const c = cockpit(good("1", tile));
  c.state.queries = ["hostile-good/items", "other/items"];
  const w = windowUi();
  const area = new PackagesArea(c.t, w.ui, actPath().path);
  await area.refresh();
  assert.deepEqual(c.state.asked, ["hostile-good/items"]);
  assert.match(JSON.stringify(area.homeNodes()), /"label":"Items","description":"3"/);
  // no clash: the bare name, as the cockpit offers it
  c.state.queries = ["items"];
  c.state.asked.length = 0;
  const again = new PackagesArea(c.t, w.ui, actPath().path);
  await again.refresh();
  assert.deepEqual(c.state.asked, ["items"]);
});

test("a leaf's product@1 serves its own entry's package only, never another admitted package's file", async () => {
  const bytes = (s: string) => new Uint8Array(Buffer.from(s));
  const hash = (s: string) => `sha256:${createHash("sha256").update(s).digest("hex")}`;
  const loaded = {
    packages: { mine: { name: "mine", id: "id-mine", revision: "r1" }, other: { name: "other", id: "id-other", revision: "r1" } },
    byHash: new Map([
      [hash("own"), new Map([["id-mine", { pkg: "id-mine", hash: hash("own"), type: "text/plain" }]])],
      [hash("theirs"), new Map([["id-other", { pkg: "id-other", hash: hash("theirs"), type: "text/plain" }]])],
    ]),
    controls: new Map(),
  } as unknown as Loaded;
  const backend = { bytes: async (_p: string, h: string) => bytes(h === hash("own") ? "own" : "theirs") } as unknown as Backend;
  const posted: unknown[] = [];
  const h = new LeafHost({ decl: { requires: [], optional: ["product@1"] }, slot: "record-view" }, { label: "L", package: "mine" }, { id: SUBJECT.id, closed: {} }, loaded, backend, { post: (m) => posted.push(m), log: () => undefined });
  await h.onMessage({ type: "kton:request", id: "p#x.1", cap: "product@1", args: { hash: hash("own") } });
  assert.match(JSON.stringify(posted.at(-1)), /"type":"kton:data"/);
  await h.onMessage({ type: "kton:request", id: "p#x.2", cap: "product@1", args: { hash: hash("theirs") } });
  assert.match(JSON.stringify(posted.at(-1)), /kton:refused.*is no file of package mine/);
});

test("the member batch stops at the first refusal: no member is read after it", async () => {
  const f = listed(join(PKGS, "kton-web-test"), "kton-web-test", "test");
  assert.ok(f.pkg.members.length > 2 * READ_WIDTH, "enough members to tell");
  const first = f.pkg.members[0]?.hash as string;
  f.blobs.set(first, Buffer.from("tampered"));
  let reads = 0;
  const read = reader(f);
  const l = await loadPackages({ packages: [f.pkg] }, async (p, h) => {
    reads++;
    return read(p, h);
  });
  assert.equal(l.refusals[0]?.code, "package.tampered");
  assert.ok(reads <= READ_WIDTH, `${reads} reads: only those already started when the refusal came`);
});

/** The hostile-good review entry with entry scopes (ktonpkg v0.9.11: scopes.records on review-view / record-view). */
async function openedWithScopes(path: ActPath, records: string) {
  const fx = good("1");
  (fx.pkg as unknown as { ui: Array<Record<string, unknown>> }).ui = fx.pkg.ui.map((e) => ({ ...e, scopes: { records } }));
  const c = cockpit(fx);
  const w = windowUi();
  const area = new PackagesArea(c.t, w.ui, path);
  await area.refresh();
  const key = `hostile-good/${area.loaded?.entries[0]?.file}`;
  const host = await area.openSlot(key, SUBJECT);
  return { ...c, ...w, area, host, key };
}

test("entry scopes: a review-view's scopes.records is where its acts sign (the area's records-scope path); a non-label is refused", async () => {
  const intos: unknown[] = [];
  const path: ActPath = {
    act: async (_c, _a, _f, _t, _e, into) => (intos.push(into), "signed"),
    open: async () => "opened",
    identityName: () => "qa-decider",
  };
  const o = await openedWithScopes(path, "dryrun1.triage");
  assert.ok(o.host instanceof CompositionHost);
  await o.host.onMessage(signIntent(o.host, 1));
  assert.deepEqual(intos, [{ scope: "dryrun1.triage" }]);
  for (const bad of ["https://kton.dev/scope/x", "../x", "sha256:abc", ""]) {
    const b = await openedWithScopes(path, bad);
    assert.equal(b.host, undefined, `${JSON.stringify(bad)} opens nothing`);
    assert.match(b.log.errors.at(-1) ?? "", /scopes\.records .* is not a scope label/);
  }
});

test("looked-at: the evidence a panel opened goes with its acts (in memory, per panel); a refused open is not listed", async () => {
  const intos: Array<{ opened?: readonly string[] } | undefined> = [];
  const path: ActPath = {
    act: async (_c, _a, _f, _t, _e, into) => (intos.push(into), "signed"),
    open: async (target, opened) => {
      const s = target["source"] as { path?: string } | undefined;
      if (s?.path === "refused.md") return "not opened: does not hash";
      opened?.(`kton-src:/r/${s?.path}?c`);
      return "Opened";
    },
    identityName: () => "qa-decider",
  };
  const o = await openedWithScopes(path, "dryrun1.triage");
  const h = o.host as CompositionHost;
  const open = (n: number, p: string) => h.onMessage({ type: "kton:intent", intent: { id: `o#${n}`, kind: "open", from: h.plan.uses[0]?.key, source: { repo: "r", commit: "c", path: p } } });
  await open(1, "a.md");
  await open(2, "refused.md");
  await open(3, "b.md");
  await open(4, "a.md");
  assert.deepEqual(openedOf(o.area, o.key), ["kton-src:/r/a.md?c", "kton-src:/r/b.md?c"]);
  await h.onMessage(signIntent(h, 5));
  assert.deepEqual(intos.at(-1), { scope: "dryrun1.triage", opened: ["kton-src:/r/a.md?c", "kton-src:/r/b.md?c"] });
  // a new view session (the panel opened again) starts with nothing looked at
  const again = (await o.area.openSlot(o.key, SUBJECT)) as CompositionHost;
  assert.deepEqual(openedOf(o.area, o.key), []);
  await again.onMessage(signIntent(again, 6));
  assert.deepEqual(intos.at(-1), { scope: "dryrun1.triage" });
});
