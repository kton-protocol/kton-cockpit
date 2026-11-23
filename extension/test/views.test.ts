// Views (C2.1): an `activity` entry that names a composition and no area is a place of its own, a view not hung on a
// proposal or a record. It is listed under "Views", opened with its own place as the subject, and planned with the
// workbench's blocks ("@workbench") like an area. Fixture: test/e2e/packages/views/qa.views.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { loadBuiltin } from "../src/composition/qm-area.js";
import { isView } from "../src/composition/slots.js";
import { PackagesArea, viewSubject, type ActPath, type CompositionUi } from "../src/composition/area.js";
import { CompositionHost, useFor } from "../src/composition/host.js";
import type { Loaded } from "../src/packages/model.js";
import type { UiListing } from "../src/packages/listing.js";
import type { Transport } from "../src/transport/port.js";
import { listed, PKGS, reader } from "./pkg-helpers.js";

const builtin = () => loadBuiltin(join(process.cwd(), "media", "workbench"));
const ITEM = `https://kton.dev/o/${"3".repeat(64)}`;

function views() {
  const fx = [listed(join(PKGS, "views", "qa.views"), "qa.views")];
  const shown: Array<{ key: string; title: string }> = [];
  const errors: string[] = [];
  const asked: Array<{ name: string; terms: unknown }> = [];
  const posts: unknown[] = [];
  const t = {
    uiList: async () => ({ ok: true, value: { packages: fx.map((f) => f.pkg) } as UiListing }),
    uiCat: async (pkg: string, h: string) => reader(...fx)(pkg, h),
    list: async () => ({ ok: true, value: { queries: [] } }),
    ask: async (req: { name: string; bindTerms?: unknown }) => {
      asked.push({ name: req.name, terms: req.bindTerms });
      return { ok: true, value: { results: { head: { vars: [] }, results: { bindings: [] } } } };
    },
  } as unknown as Transport;
  const ui: CompositionUi = {
    showComposition: (key, title) => (shown.push({ key, title }), { post: (m) => posts.push(m), urls: new Map() }),
    showLeaf: () => ({ post: () => undefined }),
    closePanel: () => undefined,
    openPanels: () => [],
    info: () => undefined,
    error: (x) => errors.push(x),
    log: () => undefined,
  };
  const path: ActPath = { act: async () => "signed", open: async () => "opened", identityName: () => "qa-decider" };
  return { area: new PackagesArea(t, ui, path, new Map(), builtin()), shown, errors, asked, posts };
}

test("views: an activity entry with a composition and no area is listed under Views, with its own place as the subject", async () => {
  const w = views();
  await w.area.refresh();
  const l = w.area.loaded as Loaded;
  assert.deepEqual(l.entries.filter(isView).map((e) => e.label), ["Items (a view)"], "an activity with only a query is no view");
  const node = w.area.viewNodes()[0];
  assert.equal(node?.label, "Views");
  assert.deepEqual(node?.children?.map((c) => [c.label, c.description]), [["Items (a view)", "qa.views"]]);
  const e = l.entries.find(isView);
  assert.ok(e);
  assert.deepEqual(node?.children?.[0]?.command?.args, [`qa.views/${e.file}`, { kind: "scope", iri: `https://kton.dev/scope/${encodeURIComponent(`qa.views/${e.file}`)}`, id: `qa.views/${e.file}` }]);
  assert.deepEqual(w.area.areaNodes(), [], "a view is no QM area");
});

test("views: the view plans with the workbench's blocks; a pick feeds the next block's param; a declared refine pages", async () => {
  const w = views();
  await w.area.refresh();
  const e = (w.area.loaded as Loaded).entries.find(isView);
  assert.ok(e);
  const host = (await w.area.openSlot(`qa.views/${e.file}`, viewSubject(e))) as CompositionHost;
  assert.ok(host instanceof CompositionHost, w.errors.join("; "));
  assert.deepEqual(w.shown.at(-1), { key: `composition:qa.views/${e.file}`, title: "Items (a view)" });
  const use = (id: string) => host.plan.uses.find((u) => u.key.endsWith(`#${id}`));
  assert.deepEqual(host.plan.uses.map((u) => [u.key.split("#").pop(), u.block]), [["items", "@workbench/rows"], ["history", "@workbench/rows"], ["elements", "@workbench/rows"]]);
  // the history waits for the pick; with the picked claim it is asked with that IRI
  const history = use("history") as unknown as { key: string; waiting?: string[]; bind?: { params: Record<string, { comp: string }> } };
  assert.deepEqual(history.waiting, ["item"]);
  const comp = history.bind?.params["item"]?.comp as string;
  await host.onMessage({ type: "kton:request", id: "h#1", cap: "query@1", args: { use: history.key }, vars: { [comp]: { item: ITEM } } });
  assert.deepEqual(w.asked.at(-1), { name: "item-history", terms: { item: { type: "iri", value: ITEM } } });
  // paging: the declared refine params may vary (a part of the declared values, a page key); anything else is refused
  const elements = use("elements") as unknown as { key: string };
  await host.onMessage({ type: "kton:request", id: "e#1", cap: "query@1", args: { use: elements.key } });
  assert.deepEqual(w.asked.at(-1), { name: "elements", terms: { part: { type: "literal", value: "A" }, after: { type: "literal", value: "" } } });
  await host.onMessage({ type: "kton:request", id: "e#2", cap: "query@1", args: { use: elements.key, refine: { part: "B", after: "e2" } } });
  assert.deepEqual(w.asked.at(-1), { name: "elements", terms: { part: { type: "literal", value: "B" }, after: { type: "literal", value: "e2" } } });
  assert.throws(() => useFor(host.plan, "query@1", { use: elements.key, refine: { part: "C" } }, undefined), /not one of the declared values/);
  assert.throws(() => useFor(host.plan, "query@1", { use: (use("items") as { key: string }).key, refine: { after: "x" } }, undefined), /does not declare refine after/);
});

test("views: the rows block offers paging and refine choices as options it accepts", () => {
  const rows = builtin().viewers["rows"];
  const accepts = (rows?.decl["accepts"] ?? {}) as Record<string, unknown>;
  assert.equal(accepts["page"], "literal");
  assert.equal(accepts["pageSize"], "literal");
});

// Row acts: a view with no record of its own acts about a row it showed (option target), through
// another package's control pinned to its sealed revision (ktonpkg v0.9.13 `from`), into the entry's records scope.
function rowActs(withOwner = true) {
  const fx = [listed(join(PKGS, "views", "qa.rowacts"), "qa.rowacts"), ...(withOwner ? [listed(join(PKGS, "views", "qa.owner"), "qa.owner")] : [])];
  const acts: Array<{ control: string; about: string; fields: unknown; scope?: string }> = [];
  const errors: string[] = [];
  const posts: Array<Record<string, unknown>> = [];
  const logs: string[] = [];
  const UNIT = `https://kton.dev/o/${"7".repeat(64)}`;
  const t = {
    uiList: async () => ({ ok: true, value: { packages: fx.map((f) => f.pkg) } as UiListing }),
    uiCat: async (pkg: string, h: string) => reader(...fx)(pkg, h),
    list: async () => ({ ok: true, value: { queries: [] } }),
    ask: async () => ({ ok: true, value: { results: { head: { vars: ["test", "result", "stamp", "claim"] }, results: { bindings: [{ test: { type: "literal", value: "item-1829 STP-002" }, result: { type: "literal", value: "error" }, stamp: { type: "literal", value: "qa.runs · r1" }, claim: { type: "uri", value: UNIT } }] } } } }),
  } as unknown as Transport;
  const ui: CompositionUi = {
    showComposition: () => ({ post: (m) => posts.push(m as Record<string, unknown>), urls: new Map() }),
    showLeaf: () => ({ post: () => undefined }),
    closePanel: () => undefined,
    openPanels: () => [],
    info: () => undefined,
    error: (x) => errors.push(x),
    log: (l) => logs.push(l),
  };
  const path: ActPath = {
    act: async (c, about, fields, _t, _e, into) => {
      acts.push({ control: `${c.package}/${c.name}`, about, fields, ...(into?.scope ? { scope: into.scope } : {}) });
      into?.signed?.({ id: `sha256:${"9".repeat(64)}`, fields: { ...fields, test: "item-1829 STP-002" } });
      return "signed";
    },
    open: async () => "opened",
    identityName: () => "role-a",
  };
  return { area: new PackagesArea(t, ui, path, new Map(), builtin()), acts, errors, posts, logs, UNIT };
}

test("row acts: a claimless view's act is about the picked row, through another package's pinned control, into the entry's records scope; the nudge is printed", async () => {
  const w = rowActs();
  await w.area.refresh();
  const e = (w.area.loaded as Loaded).entries.find((x) => x.package === "qa.rowacts");
  assert.ok(e);
  const host = (await w.area.openSlot(`qa.rowacts/${e.file}`, viewSubject(e))) as CompositionHost;
  assert.ok(host instanceof CompositionHost, w.errors.join("; "));
  const u = host.plan.uses[0] as unknown as { key: string; acts: Array<{ control: { package: string; name: string; pin: { at: string } } }> };
  assert.deepEqual([u.acts[0]?.control.package, u.acts[0]?.control.name, u.acts[0]?.control.pin.at], ["qa.owner", "rerun-request", "1"], "the foreign control, with its pin");
  const act = { intent: "record", control: { package: "qa.owner", name: "rerun-request" } };
  // without a picked row there is nothing to be about
  await host.onMessage({ type: "kton:intent", intent: { id: "i0", kind: "record", from: u.key, control: act.control, about: (u.acts[0] as unknown as { about: string }).about, fields: {} } });
  assert.match(JSON.stringify(w.posts.at(-1)), /its acts are about a row/);
  assert.equal(w.acts.length, 0);
  // a row this block was not shown is refused; a served one signs about it
  await host.onMessage({ type: "kton:request", id: "q1", cap: "query@1", args: { use: u.key } });
  await host.onMessage({ type: "kton:intent", intent: { id: "i1", kind: "record", from: u.key, control: act.control, about: (u.acts[0] as unknown as { about: string }).about, target: `https://kton.dev/o/${"8".repeat(64)}`, fields: {} } });
  assert.match(JSON.stringify(w.posts.at(-1)), /not a row this block was shown/);
  await host.onMessage({ type: "kton:intent", intent: { id: "i2", kind: "record", from: u.key, control: act.control, about: (u.acts[0] as unknown as { about: string }).about, target: w.UNIT, fields: { test: "item-1829 STP-002", reason: "capture mode" } } });
  // the choice the use declares ("reason=capture mode") is an act field; the row's test is only proposed
  assert.deepEqual(w.acts, [{ control: "qa.owner/rerun-request", about: `sha256:${"7".repeat(64)}`, fields: { reason: "capture mode" }, scope: "qa.requests" }]);
  assert.ok(w.logs.some((l) => l.endsWith(`./send lane-a requests area=area-b title="rerun: item-1829 STP-002" text="rerun request claim sha256:${"9".repeat(64)}; test item-1829 STP-002" why="row acts"`)), w.logs.join("\n"));
  // a value no choice declares is not an act field (only proposed: a record takes none)
  await host.onMessage({ type: "kton:intent", intent: { id: "i3", kind: "record", from: u.key, control: act.control, about: (u.acts[0] as unknown as { about: string }).about, target: w.UNIT, fields: { reason: "made up" } } });
  assert.deepEqual(w.acts.at(-1)?.fields, {});
});

test("row acts: with the owner package not in use, the act is refused and names the pin", async () => {
  const w = rowActs(false);
  await w.area.refresh();
  const e = (w.area.loaded as Loaded).entries.find((x) => x.package === "qa.rowacts");
  assert.ok(e);
  const host = (await w.area.openSlot(`qa.rowacts/${e.file}`, viewSubject(e))) as CompositionHost;
  assert.ok(host instanceof CompositionHost, w.errors.join("; "));
  const u = host.plan.uses[0] as unknown as { key: string; acts: Array<{ about: string }> };
  await host.onMessage({ type: "kton:request", id: "q1", cap: "query@1", args: { use: u.key } });
  await host.onMessage({ type: "kton:intent", intent: { id: "i1", kind: "record", from: u.key, control: { package: "qa.owner", name: "rerun-request" }, about: u.acts[0]?.about, target: w.UNIT, fields: {} } });
  assert.match(JSON.stringify(w.posts.at(-1)), /qa\.owner@1 is not in use here/);
  assert.equal(w.acts.length, 0);
});

test("row acts: a malformed pin refuses the package; the rows block accepts target, choices, propose and nudge", async () => {
  const { withForeignControls } = await import("../src/packages/model.js");
  assert.throws(() => withForeignControls({ parts: [{ blocks: [{ acts: [{ intent: "record", control: "c", from: { package: "p", at: "1", head: "x" } }] }] }] }, "self", "f"), /from must be/);
  assert.throws(() => withForeignControls({ parts: [{ blocks: [{ acts: [{ intent: "record", control: "c", from: { package: "self", at: "1", head: `sha256:${"a".repeat(64)}` } }] }] }] }, "self", "f"), /from must be/);
  const accepts = (builtin().viewers["rows"]?.decl["accepts"] ?? {}) as Record<string, unknown>;
  assert.deepEqual([accepts["target"], accepts["choices"], accepts["propose"], accepts["nudge"]], ["literal", { set: "literal" }, { set: "literal" }, "literal"]);
});

test("acts once: a block that draws its own act buttons gets its acts as blockActs, and no act for the renderer's own bar", async () => {
  const w = rowActs();
  await w.area.refresh();
  const e = (w.area.loaded as Loaded).entries.find((x) => x.package === "qa.rowacts");
  assert.ok(e);
  const host = (await w.area.openSlot(`qa.rowacts/${e.file}`, viewSubject(e))) as CompositionHost;
  await host.onMessage({ type: "kton:ready" });
  const set = w.posts.find((m) => m["type"] === "kton:set") as { plan: { uses: Array<{ acts?: unknown[]; blockActs?: Array<{ label: string }> }> } };
  assert.deepEqual(set.plan.uses[0]?.acts, [], "compose.js draws no bar of its own for it");
  assert.deepEqual(set.plan.uses[0]?.blockActs?.map((a) => a.label), ["Request rerun"], "the block draws each act once");
  assert.equal(host.plan.uses[0]?.acts?.length, 1, "the host's plan keeps the act: intents are checked against it");
  for (const v of ["cards", "figures", "rows", "requests"]) assert.equal(builtin().viewers[v]?.decl["rendersActs"], true, v);
});
