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
