// The workbench's look (qm-workbench/design/DESIGN.md, MH 20.11.): the logic behind it, without a browser. The blocks are
// ES modules in media/workbench; their pure helpers are named exports, and a block that only writes innerHTML is mounted
// on a stand-in element.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { compositionPage } from "../src/composition/page.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Any = any;
const media = (p: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", p)).href);
const esc = (s: string) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string);
const kit = { esc };
/** Mount a block on a stand-in element; returns the element (its innerHTML is what the block drew). */
export async function mountOn(viewer: string, options: Record<string, unknown>, extra: Record<string, unknown> = {}): Promise<{ innerHTML: string }> {
  const m = await media(`viewers/${viewer}/${viewer}.js`);
  const el = { innerHTML: "" };
  m.mount(el, { use: { options, ...extra }, kit, request: () => new Promise(() => {}), intent: async () => ({}), setState: () => {} });
  return el;
}

test("design: the composition page loads the workbench stylesheet after compose.css (and the frame decorator by nonce, when given)", () => {
  const html = compositionPage("T", "N0nce", { cspSource: "https://w", frame: "https://w/frame.js", css: "https://w/compose.css", design: { css: "https://w/workbench.css", js: "https://w/frame-design.js" } });
  assert.ok(html.indexOf("compose.css") < html.indexOf("workbench.css"), "the workbench's look overrides the vendored one");
  assert.match(html, /<script type="module" nonce="N0nce" src="https:\/\/w\/frame-design\.js"><\/script>/);
  assert.doesNotMatch(html, /style=/, "no style attributes (the nonce CSP)");
  assert.doesNotMatch(compositionPage("T", "N", { cspSource: "https://w", frame: "https://w/f.js", css: "https://w/c.css", design: { css: "https://w/wb.css" } }), /<script[^>]*src="[^"]*frame-design/);
});

test("design: notice tones: the banner keeps its text (Dry run: <run>) and shows the hash as a chip; sub, head, empty", async () => {
  const b = await mountOn("notice", { tone: "banner", text: "Dry run: dryrun2", stamp: "test keys · does not settle 2c50ddd6" });
  assert.match(b.innerHTML, /wb-banner/);
  assert.equal(b.innerHTML.replace(/<[^>]+>/g, ""), "Dry run: dryrun2 · test keys · does not settle 2c50ddd6");
  assert.match(b.innerHTML, /<button type="button" class="chip mono" data-tip="sha256:2c50ddd6…[^"]*">2c50ddd6<\/button>/);
  const s = await mountOn("notice", { tone: "sub", text: "Your scope, by area", nav: "Where" });
  assert.match(s.innerHTML, /class="kton-notice wb-sub"[^>]*data-nav-label="Where"/);
  const h = await mountOn("notice", { tone: "head", eyebrow: "Decision", text: "Can it be the basis?", lede: "And who settles." });
  assert.match(h.innerHTML, /<h1 class="wb-title">Can it be the basis\?<\/h1><p class="wb-lede">And who settles\.<\/p>/);
  const e = await mountOn("notice", { tone: "empty", text: "Not compared yet", how: "Not data yet", chips: ["cost", "residual"] });
  assert.match(e.innerHTML, /class="kton-notice empty"[\s\S]*<b>Not compared yet<\/b><p>Not data yet<\/p><div class="wb-dash-chips"><span>cost<\/span><span>residual<\/span><\/div>/);
  const p = await mountOn("notice", { text: "<b>plain</b>" });
  assert.match(p.innerHTML, /&lt;b&gt;plain&lt;\/b&gt;/, "escaped");
});

test("design step 3: figures: the guard states the relation only when the numbers hold it; scope tags; tiles", async () => {
  const f = await media("viewers/figures/figures.js");
  const figs = [{ value: "215", scope: "in scope (MH 10.11.)" }, { value: "254", scope: "all judged" }, { value: "39", scope: "outside your scope" }];
  assert.equal(f.relationText(figs, "1+3=2"), "215 in scope (MH 10.11.) + 39 outside your scope = 254 all judged.");
  assert.equal(f.relationText(figs, "1+2=3"), undefined, "215 + 254 is not 39: no relation is claimed");
  assert.equal(f.relationText(figs, "1+3=4"), undefined);
  assert.equal(f.relationText(figs, undefined), undefined);
  assert.equal(f.relationText([{ value: "1,200" }, { value: "1,300" }, { value: "100" }], "3+1=2"), "100 + 1,200 = 1,300.");
  assert.deepEqual([...f.pairs(["in scope (MH 10.11.)|in scope", "plain"])], [["in scope (MH 10.11.)", "in scope"], ["plain", "plain"]]);
  assert.equal(f.stampAttrs({ stamp: { value: "sha256:b610b5c6" }, stampLabel: { value: "mapping round 9 · b610b5c6" } }, (b: Any) => b?.value ?? "", esc), 'data-stamp="sha256:b610b5c6" data-stamp-label="mapping round 9 · b610b5c6"');
});

test("design step 4: estimate: positions on the stated scale, labels that never overlap, ticks, verdicts", async () => {
  const e = await media("viewers/estimate/estimate.js");
  const dom = e.domainOf("0,8", 6.1, 5);
  assert.deepEqual(dom, [0, 8]);
  assert.deepEqual(e.domainOf(undefined, 6.1, 5), [0, 8], "auto: ceil(max(high, target) × 1.3)");
  assert.deepEqual(e.domainOf("8,0", 6.1, 5), [0, 8], "a reversed domain is not taken");
  assert.deepEqual(e.positions({ rate: 4.5, low: 3.2, high: 6.1, target: 5 }, { rate: 4.4, low: 3.2, high: 6.0 }, dom), { lo: 40, hi: 76.25, pt: 56.25, t: 62.5, rpt: 55, rlo: 40, rhi: 75 });
  assert.equal(e.pos(12, dom), 100, "clamped to the bar");
  assert.deepEqual(e.labelSides(4.5, 5), { point: "end", target: "start" }, "point left of itself, target right of its line");
  assert.deepEqual(e.labelSides(7.8, 5), { point: "start", target: "end" }, "above the target they swap");
  assert.deepEqual(e.ticks([0, 8]), [0, 2, 4, 6, 8]);
  assert.deepEqual(e.ticks([0, 20]), [0, 5, 10, 15, 20]);
  const v = e.verdicts({ rate: 4.5, high: 6.1, target: 5 });
  assert.deepEqual([v.point.ok, v.upper.ok, v.upper.text], [true, false, "upper bound 6.1 % does not"]);
});
