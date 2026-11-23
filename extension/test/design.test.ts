// The workbench's look: the logic behind it, without a browser. The blocks are
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
  const el = { innerHTML: "", querySelector: () => null, querySelectorAll: () => [] };
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

test("design: notice tones: the banner keeps its text (Trial: <run>) and shows the hash as a chip; sub, head, empty", async () => {
  const b = await mountOn("notice", { tone: "banner", text: "Trial: run-b", stamp: "test keys · does not settle 2c50ddd6" });
  assert.match(b.innerHTML, /wb-banner/);
  assert.equal(b.innerHTML.replace(/<[^>]+>/g, ""), "Trial: run-b · test keys · does not settle 2c50ddd6");
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

test("design: figures: the guard states the relation only when the numbers hold it; scope tags; tiles", async () => {
  const f = await media("viewers/figures/figures.js");
  const figs = [{ value: "20", scope: "in scope (as agreed)" }, { value: "25", scope: "all judged" }, { value: "5", scope: "outside your scope" }];
  assert.equal(f.relationText(figs, "1+3=2"), "20 in scope (as agreed) + 5 outside your scope = 25 all judged.");
  assert.equal(f.relationText(figs, "1+2=3"), undefined, "20 + 25 is not 5: no relation is claimed");
  assert.equal(f.relationText(figs, "1+3=4"), undefined);
  assert.equal(f.relationText(figs, undefined), undefined);
  assert.equal(f.relationText([{ value: "1,200" }, { value: "1,300" }, { value: "100" }], "3+1=2"), "100 + 1,200 = 1,300.");
  assert.deepEqual([...f.pairs(["in scope (as agreed)|in scope", "plain"])], [["in scope (as agreed)", "in scope"], ["plain", "plain"]]);
  assert.equal(f.stampAttrs({ stamp: { value: "sha256:0a1b2c3d" }, stampLabel: { value: "mapping round 3 · 0a1b2c3d" } }, (b: Any) => b?.value ?? "", esc), 'data-stamp="sha256:0a1b2c3d" data-stamp-label="mapping round 3 · 0a1b2c3d"');
});

test("design: estimate: positions on the stated scale, labels that never overlap, ticks, verdicts", async () => {
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

test("design: rows: groups keep query order and sum, numbers by type, the range spec and mini-bar positions", async () => {
  const r = await media("viewers/rows/rows.js");
  const b = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { type: "literal", value: v }]));
  // 5 groups, the unsettled per group 4/3/74/12/98 = 191
  const rows = [
    b({ group: "alpha", area: "north", unsettled: "3" }), b({ group: "alpha", area: "south", unsettled: "1" }),
    b({ group: "beta", area: "east", unsettled: "3" }),
    b({ group: "gamma", area: "one", unsettled: "60" }), b({ group: "gamma", area: "two", unsettled: "8" }), b({ group: "gamma", area: "three", unsettled: "4" }), b({ group: "gamma", area: "four", unsettled: "2" }),
    b({ group: "delta", area: "west", unsettled: "12" }),
    b({ group: "none", area: "p1", unsettled: "40" }), b({ group: "none", area: "p2", unsettled: "30" }), b({ group: "none", area: "p3", unsettled: "20" }), b({ group: "none", area: "p4", unsettled: "3" }), b({ group: "none", area: "p5", unsettled: "2" }), b({ group: "none", area: "p6", unsettled: "1" }), b({ group: "none", area: "p7", unsettled: "1" }), b({ group: "none", area: "p8", unsettled: "1" }),
  ];
  const g = r.groupRows(rows, "group", "unsettled");
  assert.deepEqual(g.map((x: Any) => [x.group, x.rows.length, x.sum]), [["alpha", 2, 4], ["beta", 1, 3], ["gamma", 4, 74], ["delta", 1, 12], ["none", 8, 98]]);
  assert.equal(g.reduce((a: number, x: Any) => a + x.sum, 0), 191);
  assert.deepEqual(g[2].rows.map((x: Any) => x.area.value), ["one", "two", "three", "four"], "query order within a group");
  assert.ok(r.isNumeric("n", [{ n: { type: "literal", value: "235", datatype: "http://www.w3.org/2001/XMLSchema#integer" } }]));
  assert.ok(r.isNumeric("rate", [{ rate: { type: "literal", value: "1.7" } }]));
  assert.ok(!r.isNumeric("rate", [{ rate: { type: "literal", value: "1.7 % (0.7–4.3, n=235)" } }]), "formatted text is not a number");
  assert.ok(r.isNumeric("x", [{ x: { value: "a" } }], ["x"]), "named numeric");
  assert.deepEqual(r.rangeSpec("rate,low,high,n", "0,20,5"), { rate: "rate", low: "low", high: "high", n: "n", lo: 0, hi: 20, target: 5 });
  assert.equal(r.rangeSpec("rate", "0,20,5"), undefined);
  assert.equal(r.barPos(1.7, 0, 20), 8.5);
  assert.equal(r.barPos(37.7, 0, 20), 100, "clamped");
});

test("design: rows draws group headers, too few muted, a total, no stamp column with stamp: section", async () => {
  const m = await media("viewers/rows/rows.js");
  const answer = { head: { vars: ["group", "area", "unsettled", "rate", "low", "high", "n", "enough", "stamp"] }, results: { bindings: [
    { group: { value: "gamma" }, area: { value: "one" }, unsettled: { value: "70" }, rate: { value: "1.7" }, low: { value: "0.7" }, high: { value: "4.3" }, n: { value: "235" }, enough: { value: "true" }, stamp: { value: "sha256:0a1b2c3d" } },
    { group: { value: "gamma" }, area: { value: "three" }, unsettled: { value: "4" }, rate: { value: "0" }, low: { value: "0" }, high: { value: "41" }, n: { value: "7" }, enough: { value: "false" }, stamp: { value: "sha256:0a1b2c3d" } },
  ] } };
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  m.mount(el, { use: { options: { columns: ["area", "unsettled", "rate", "low", "n", "stamp"], group: "group", groupSum: "unsettled", range: "rate,low,high,n", scale: "0,20,5", measurable: "enough", total: "unsettled", stamp: "section", labels: ["area|Area", "unsettled|Unsettled"] } }, kit, request: async () => answer, intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  const h = el.innerHTML as string;
  assert.match(h, /<tr class="grp" data-group="gamma"><th colspan="5" scope="colgroup">Gamma <span class="muted">2 areas · 74 unsettled<\/span><\/th><\/tr>/);
  assert.doesNotMatch(h, /data-head="stamp"/, "no stamp column");
  assert.match(h, /data-stamp="sha256:0a1b2c3d"/, "each row keeps its stamp");
  assert.match(h, /class="few">too few to measure/);
  assert.match(h, /<span class="rv">1\.7 %<\/span><span class="mb" aria-hidden="true" data-lo="3\.5" data-hi="21\.5" data-pt="8\.5" data-t="25">/);
  assert.match(h, /<th class="r" data-total>74<\/th>/);
  assert.match(h, /Rate <span class="muted">\(scale 0–20 %, dashed = target 5 %\)<\/span>/);
});

test("design: source chips: once per part, the round's label, the scope where no round stamps it", async () => {
  const s = await media("design/stamps.js");
  const round = { stamp: "sha256:0a1b2c3d", label: "mapping round 3 · 0a1b2c3d", full: `https://kton.dev/o/0a1b2c3d${"f".repeat(56)}`, scope: "qa.mapping" };
  const c = s.chipFor(round);
  assert.equal(c.text, "source: mapping round 3 · 0a1b2c3d");
  assert.match(c.tip, /mapping round 3 · 0a1b2c3d \(scope qa\.mapping\)\nsha256:0a1b2c3df{56}$/, "scope and full hash in the tooltip");
  const runs = { stamp: "qa.runs · p2-rest-1", scope: "qa.runs" };
  assert.equal(s.chipFor(runs).text, "source: qa.runs", "no round: the scope");
  assert.equal(s.fullHash("https://kton.dev/o/" + "a".repeat(64)), `sha256:${"a".repeat(64)}`);
  const r = s.sectionStamps([
    { id: "spec", stamps: [round, round] },
    { id: "runs", stamps: [runs, { ...runs, stamp: "qa.runs · p2-rest-2" }, runs, runs, runs] },
    { id: "none", stamps: [] },
    { id: "mixed", stamps: [round, { stamp: "sha256:00000000" }] },
  ]);
  assert.equal(r.section, round, "the section's source: one vote per block, the first block's on a tie (many runs do not outvote the evidence)");
  assert.equal(r.blocks.spec, null, "equal to the section: no block chip");
  assert.equal(r.blocks.runs?.scope, "qa.runs", "another source: the block's own chip (one for all its runs)");
  assert.equal(r.blocks.none, null);
  assert.equal(r.blocks.mixed, "mixed", "a block that mixes sources keeps its row stamps");
  assert.deepEqual(s.sectionStamps([]), { section: null, blocks: {} });
  const perRun = s.sectionStamps([{ id: "runs", stamps: [{ stamp: "r1 · run.json sha256:1" }, { stamp: "r2 · run.json sha256:2" }] }]);
  assert.equal(perRun.section, null, "rows with a stamp each: no part chip naming one of them");
  assert.equal(perRun.blocks.runs, "mixed");
});

test("design: the section nav highlights the section on screen, not the last link clicked", async () => {
  const { shownSection } = await media("design/nav.js");
  // the decision view's nav, scrolled so that Rule cases' head has passed the sticky bar (line 80); Evidence is above it
  const at = (tops: number[]) => ["summary", "spot", "where", "evidence", "capture", "rules", "options", "sign"].map((id, i) => ({ id, top: tops[i] as number, bottom: tops.find((t, j) => j > i && t > (tops[i] as number)) ?? (tops[i] as number) + 400 }));
  const scrolled = at([-2400, -2400, -1800, -1200, -700, 40, 1300, 1700]);
  assert.equal(shownSection(scrolled, { line: 80, viewH: 900 }), "rules", "the section whose head passed the bar last");
  assert.equal(shownSection(scrolled, { line: 80, viewH: 900, clicked: "evidence" }), "rules", "a link clicked earlier does not keep the highlight");
  assert.equal(shownSection(at([100, 100, 700, 1300, 1900, 2400, 3600, 4000]), { line: 80, viewH: 900 }), "summary", "at the top: the first section");
  assert.equal(shownSection(at([-40, -40, 700, 1300, 1900, 2400, 3600, 4000]), { line: 80, viewH: 900 }), "summary", "side by side (equal tops): the first of them");
  // at the bottom of the page the last sections never reach the bar: the one clicked if it is on screen, else the last on screen
  const bottom = at([-4000, -4000, -3400, -2800, -2200, -600, 300, 700]);
  assert.equal(shownSection(bottom, { line: 80, viewH: 900, atBottom: true }), "sign");
  assert.equal(shownSection(bottom, { line: 80, viewH: 900, atBottom: true, clicked: "options" }), "options");
  assert.equal(shownSection(bottom, { line: 80, viewH: 900, atBottom: true, clicked: "evidence" }), "sign", "a clicked section off screen does not win");
  assert.equal(shownSection([], { line: 80 }), "");
  assert.equal(shownSection([{ id: "x", top: NaN, bottom: NaN }], { line: 80 }), "", "unmeasured sections are skipped");
});

test("design: cards: short test names and decoded locations, verdict pills, the segmented choice gates the one primary", async () => {
  const c = await media("viewers/cards/cards.js");
  assert.equal(c.shortTest("api-tests:com/example/app/integrationtests/resources/StepRunTest.kt#a step can be run with a clean inventory"), "StepRunTest.kt · a step can be run with a clean inventory");
  assert.equal(c.shortTest("rlib:test-reviews.R#deleteReviewer returns FALSE for invalid review ID|item-369,item-2045"), "test-reviews.R · deleteReviewer returns FALSE for invalid review ID");
  assert.equal(c.shortTest("ui-tests:Basic/Workbench/t340%20Views.rsc"), "t340 Views.rsc", "decoded, never %20");
  assert.equal(c.shortTest("plain text"), "plain text");
  assert.equal(c.locLabel("api-tests/src/test/kotlin/com/example/app/integrationtests/BasicTest.kt:749"), "api-tests · …/integrationtests/BasicTest.kt:749");
  assert.deepEqual(["asserted", "exercised", "no-relation", "unclear", "covered"].map(c.verdictClass), ["v-asserted", "v-exercised", "v-norel", "v-unclear", "v-asserted"]);
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  const row = { element: { value: "item-41#e6" }, group: { value: "gamma" }, target: { value: "https://x/e6" }, statement: { value: "The export body" }, earlier: { value: "unclear" }, stamp: { value: "sha256:0a1b2c3d" } };
  c.mount(el, { use: { key: "spot#cards", options: { title: "element", group: "group", badge: "earlier|earlier: ", quote: "statement", fields: ["testName|Test"], compare: ["judge1|Judge 1 · rule judge"], target: "target", choices: ["verdict=rule-judge-right|Rule judge is right", "verdict=spec-unclear|Spec unclear"], choiceLegend: "Your verdict", layout: "pager", pick: "element|Show evidence" }, blockActs: [{ intent: "sign", label: "Sign verdict…", control: { name: "spotcheck" } }] }, kit, request: async () => ({ results: { bindings: [row] } }), intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  const h = el.innerHTML as string;
  assert.match(h, /<dd class="empty-line" data-col="testName">No test recorded\.<\/dd>/, "a missing value is said, never \" · :\" (bug 6)");
  assert.match(h, /<div class="jh">Judge 1 · rule judge<\/div><p class="empty-line">No verdict recorded\.<\/p>/);
  assert.match(h, /<fieldset class="seg" data-choices="0"><legend>Your verdict<\/legend>/);
  assert.equal((h.match(/class="primary"/g) ?? []).length, 1, "one primary per card");
  assert.match(h, /<button type="button" class="primary" data-card="0" data-act="0" disabled data-needs-choice data-testid="card-0-spotcheck">Sign verdict…<\/button>/, "disabled until a choice");
  assert.match(h, /class="ghost" data-pick="0"[^>]*>Show evidence</);
  assert.match(h, /<span class="badge" data-badge>earlier: unclear<\/span>/);
  assert.doesNotMatch(h, /class="pager"/, "one card: no pager");
});

test("design: rule cases: effects per answer, the resolves line, a stacked bar to scale", async () => {
  const c = await media("viewers/cards/cards.js");
  const e = c.parseEffects("asserted|78|0|0|78;exercised|0|78|0|78;no-relation|0|22|56|78;bad|x|1|1|1");
  assert.deepEqual(e.map((x: Any) => [x.answer, x.covered, x.exercisedOnly, x.gap, x.resolved]), [["asserted", 78, 0, 0, 78], ["exercised", 0, 78, 0, 78], ["no-relation", 0, 22, 56, 78]]);
  assert.equal(c.resolvesLine(e, 78), "Resolves 78 of 78 elements, whatever the answer");
  assert.equal(c.resolvesLine(c.parseEffects("asserted|28|0|0|28;no-relation|0|2|26|29"), 29), "Resolves 28–29 of 29 elements, depending on the answer");
  assert.equal(c.resolvesLine(c.parseEffects("asserted|1|0|0|1"), 1), "Resolves 1 of 1 element, whatever the answer");
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  c.mount(el, { use: { options: { title: "title", effects: "effects", touched: "touched" } }, kit, request: async () => ({ results: { bindings: [{ title: { value: "Rule case 10" }, effects: { value: "no-relation|0|2|26|28" }, touched: { value: "29" }, stamp: { value: "s" } }] } }), intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  assert.match(el.innerHTML, /<td>no relation<div class="stack" aria-hidden="true"><i class="s-ex" data-w="6\.9"><\/i><i class="s-gap" data-w="89\.7"><\/i><\/div><\/td><td>0<\/td><td>2<\/td><td>26<\/td>/);
});

test("design: one primary per object: the first sign act; record acts and the rest secondary", async () => {
  for (const v of ["figures", "rows", "requests"]) {
    const m = await media(`viewers/${v}/${v}.js`);
    assert.deepEqual(m.actClasses([{ intent: "sign" }, { intent: "record" }]), ["primary", "secondary"], v);
    assert.deepEqual(m.actClasses([{ intent: "record" }, { intent: "sign" }, { intent: "sign" }]), ["secondary", "primary", "secondary"], v);
    assert.deepEqual(m.actClasses([{ intent: "record" }]), ["secondary"], v);
  }
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  (await media("viewers/figures/figures.js")).mount(el, { use: { options: {}, blockActs: [{ intent: "sign", label: "Choose and sign…", control: { name: "decides" } }, { intent: "record", label: "Ask a question", control: { name: "question" } }] }, kit, request: async () => ({ results: { bindings: [{ value: { value: "0" }, label: { value: "questions asked" }, stamp: { value: "s" } }] } }), intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  assert.match(el.innerHTML, /class="primary" data-act="0" data-testid="act-decides">Choose and sign…<\/button><button type="button" class="secondary" data-act="1" data-testid="act-question">Ask a question</);
});

test("design: every block: a skeleton while it loads (busy), an empty state that says what and how, an error callout", async () => {
  for (const v of ["figures", "estimate", "rows", "evidence", "requests", "cards"]) {
    const loading = await mountOn(v, { emptyText: "No data for run-b", emptyHow: "Counts come from run-b.mapping." });
    assert.match(loading.innerHTML, /class="wb-skel [a-z]+" data-area-loading aria-busy="true"/, `${v}: skeleton`);
    assert.doesNotMatch(loading.innerHTML, /<button/, `${v}: no act while it loads`);
    const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
    const m = await media(`viewers/${v}/${v}.js`);
    m.mount(el, { use: { options: { emptyText: "No data for run-b", emptyHow: "Counts come from run-b.mapping.", title: "x" } }, kit, request: async () => ({ head: { vars: [] }, results: { bindings: [] } }), intent: async () => ({}), setState: () => {} });
    await new Promise((r) => setTimeout(r, 10));
    assert.match(el.innerHTML, /<div class="empty" data-area-empty>[\s\S]*<b>No data for run-b<\/b><p>Counts come from run-b\.mapping\.<\/p>/, `${v}: empty says what and how`);
    const er: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null, insertAdjacentHTML: () => {} };
    m.mount(er, { use: { options: {} }, kit, request: async () => { throw new Error("the cockpit refused <x>"); }, intent: async () => ({}), setState: () => {} });
    await new Promise((r) => setTimeout(r, 10));
    assert.match(er.innerHTML, /<div class="callout error" data-area-error role="alert"><p>the cockpit refused &lt;x&gt;<\/p><\/div>/, `${v}: error callout`);
  }
});

test("design: evidence: decoded names with line and location, the mismatch flag (bug 5), runs collapsed when passing without artifacts", async () => {
  const e = await media("viewers/evidence/evidence.js");
  const v = (b: Any) => (b && typeof b.value === "string" ? b.value : "");
  const b = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).map(([k, x]) => [k, { value: x }]));
  const spec = b({ kind: "spec", repo: "specs", commit: "49966c23eceb754264ea6c36296a6ca2ff6214b8", path: "specifications/Rights%20Management/item-1000%20Publish%20rights.md", line: "31", label: "Open spec: item-1000 Publish rights.md at line 31" });
  assert.deepEqual(e.itemTitle(spec, v), { title: "item-1000 Publish rights.md", sub: "line 31", loc: "specs · specifications/Rights Management/ · at 49966c23eceb" }, "never %20");
  const test = b({ kind: "test", repo: "api-tests", commit: "d74be51196a3", path: "src/test/kotlin/BasicTest.kt", line: "749", label: "Open test at line 749: api-tests:com/x/ResourceAclTest.kt#an acl for a publish user" });
  assert.equal(e.mismatch(test, v), "Opens BasicTest.kt line 749, not the test in ResourceAclTest.kt.");
  assert.equal(e.mismatch(b({ ...Object.fromEntries(Object.entries(test).map(([k, x]: Any) => [k, x.value])), path: "src/x/ResourceAclTest.kt" }), v), "", "the same file: no flag");
  const runs = [b({ run: "r1", testName: "t", outcome: "failed", sha256: "sha256:" + "a".repeat(64), name: "x.png", label: "Open screenshot: run r1 (failed)", artifact: "screenshot" }), b({ run: "r2", testName: "t", outcome: "passed", note: "no", passingWhy: "a passing test leaves no screenshot" }), b({ run: "r3", testName: "t", outcome: "passed", passingWhy: "a passing test leaves no screenshot" })];
  const h = e.runsTable(esc, runs, v, () => "", true);
  assert.equal((h.match(/data-evidence="artifact"/g) ?? []).length, 1, "passing runs without artifacts are not listed one per line (bug 13)");
  assert.match(h, /<p class="kton-muted" data-passing="2">2 passing runs without an artifact: a passing test leaves no screenshot/);
  assert.match(h, /data-label="Open screenshot: run r1 \(failed\)"/);
});

test("design: thumbnails: hash-checked data URLs in assertion order, at most per row and per answer; nothing else changes", async () => {
  const { withThumbs } = await import("../src/ui/artifacts.js");
  const sha = (c: string) => `sha256:${c.repeat(64)}`;
  const entry = (n: string, kind: string, c: string, mt = "image/jpeg") => [n, kind, sha(c), "run1", `out/${c}.jpg`, mt, "100", "a.spec.ts:12", "expect visible"].join("\t");
  const answer = { head: { vars: ["test", "evidence"] }, results: { bindings: [
    { test: { type: "literal", value: "t1" }, evidence: { type: "literal", value: [entry("2", "assertion-screenshot", "b"), entry("1", "assertion-screenshot", "a"), entry("", "error-context", "c", "text/markdown")].join("\n") } },
    { test: { type: "literal", value: "t2" }, run: { type: "literal", value: "run1" }, name: { type: "literal", value: "x.png" }, sha256: { type: "literal", value: sha("d") }, mediaType: { type: "literal", value: "image/png" }, artifact: { type: "literal", value: "screenshot" } },
  ] } };
  const asked: string[] = [];
  const out = (await withThumbs(answer, async (t) => (asked.push(t.sha256), t.sha256.endsWith("b") ? undefined : `data:${t.mediaType};base64,QQ==`), { perRow: 4, total: 2 })) as Any;
  const es = out.results.bindings[0].evidence.value.split("\n").map((l: string) => l.split("\t"));
  assert.deepEqual(es.map((x: string[]) => [x[0], x[1], x[9]]), [["1", "assertion-screenshot", "data:image/jpeg;base64,QQ=="], ["2", "assertion-screenshot", ""], ["", "error-context", ""]], "assertion order; a refused hash gets none; text is never a thumbnail");
  assert.equal(out.results.bindings[1].thumb.value, "data:image/png;base64,QQ==");
  assert.deepEqual(asked, [sha("a"), sha("b"), sha("d")]);
  const capped = (await withThumbs(answer, async () => "data:image/png;base64,QQ==", { perRow: 1, total: 1 })) as Any;
  assert.equal(capped.results.bindings[1].thumb, undefined, "the answer's cap");
  const plain = { head: { vars: ["x"] }, results: { bindings: [{ x: { type: "literal", value: "1" } }] } };
  assert.equal(await withThumbs(plain, async () => "data:"), plain, "an answer without artifacts is returned as it is");
});

test("design: rows: short test ids, group notes once, the packed evidence in assertion order", async () => {
  const r = await media("viewers/rows/rows.js");
  assert.equal(r.shortId("ui-e2e:ui/item-24.spec.ts#T-002: the toolbar"), "item-24.spec.ts · T-002: the toolbar");
  assert.equal(r.shortId("lane:api-tests:ReviewsTest.kt#entries of another review"), "ReviewsTest.kt · entries of another review");
  assert.equal(r.shortId(`https://kton.dev/o/${"a".repeat(64)}`), "sha256:aaaaaaaaaaaa…");
  assert.equal(r.shortId("https://example.org/qa/spec/item-41/6da9"), "6da9");
  assert.deepEqual(r.groupNotes(["lane harness 1: login | lane harness 3: New (+)", "lane harness 1: login", ""]), ["lane harness 1: login", "lane harness 3: New (+)"]);
  const sha = (c: string) => `sha256:${c.repeat(64)}`;
  const ev = r.parseEvidence([["2", "assertion-screenshot", sha("b"), "r", "n2", "image/jpeg", "1", "a.ts:9", "two"], ["1", "assertion-screenshot", sha("a"), "r", "n1", "image/jpeg", "1", "a.ts:3", "one"], ["", "trace", sha("c"), "r", "t.zip", "application/zip", "1", "", ""], ["", "bad", "nohash"]].map((x) => x.join("\t")).join("\n"));
  assert.deepEqual(ev.map((x: Any) => [x.assertion, x.kind, x.site]), [["1", "assertion-screenshot", "a.ts:3"], ["2", "assertion-screenshot", "a.ts:9"], ["", "trace", ""]]);
});

test("design: the sign dialog's structure keeps every line's text, decodes the references, and Sign stays gated", async () => {
  const { signDialogPage, refLabel } = await import("../src/ui/pages.js");
  assert.deepEqual(refLabel("kton-src:/specs/specifications/Rights%20Management/item-1000%20Publish%20rights.md?49966c23eceb754264ea6c36296a6ca2ff6214b8#L31"), { title: "item-1000 Publish rights.md", sub: "line 31", loc: "specs · specifications/Rights Management/ · at 49966c23eceb" });
  assert.deepEqual(refLabel(`run-artifact:p2-e2e-1/out/a/test-failed-1.png@sha256:${"c".repeat(64)}`), { title: "test-failed-1.png", sub: "screenshot, hash checked", loc: `run p2-e2e-1 · out/a/ · sha256 ${"c".repeat(12)}` });
  const refs = ["kton-src:/app/A%20B.java?fa218aa8#L127", `run-artifact:r/x.png@sha256:${"d".repeat(64)}`];
  const plan: Any = { control: { package: "qa.review", name: "decides", template: "review-decides", kind: "decision", title: "Choose and sign" }, template: "review-decides", subject: `https://kton.dev/o/${"4".repeat(64)}`, fields: { choice: "a", lookedAt: refs.join(" ") }, meaning: "approved", scope: "qa.review", opened: refs, as: "qa-decider", keyRole: "person", needsDialog: true, footer: "A kton signature is not yet a QMS e-signature: every signature here is test-only.", rules: "Rules shown for information.", predicate: "https://example.org/qa/decide/decides", expect: { template: `sha256:${"e".repeat(64)}`, revision: `sha256:${"f".repeat(64)}` }, digest: "x" };
  const html = signDialogPage("Choose and sign: qa.review/decision", "detail", "qa (qa-decider)", "n0", { plan, signer: { verified: false, hint: "qa", why: "no identity verb yet" } as Any });
  const text = html.replace(/<script[\s\S]*<\/script>/, "").replace(/<[^>]+>/g, "").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/\s+/g, " ");
  for (const t of ["Template: review-decides", "choice = a", "Act: approved (the control's meaning, not a field of the claim)", "Into scope: qa.review", 'person key "qa-decider"', "Evidence you opened in this view (2)", "(in lookedAt)", `About: https://kton.dev/o/${"4".repeat(64)}`, "not yet a QMS e-signature"]) assert.ok(text.includes(t), `the dialog still says ${t}`);
  assert.match(html, /data-testid="sign-detail"/);
  assert.equal((html.match(/data-in-lookedat/g) ?? []).length, 2);
  assert.match(html, /<div class="t" title="kton-src:\/app\/A%20B\.java\?fa218aa8#L127" tabindex="0">A B\.java <span class="muted">· line 127<\/span>/, "decoded name, full reference in the tooltip");
  assert.match(html, /lookedAt<span class="hide"> = <\/span><\/span><span class="v">2 references: the evidence below/, "lookedAt is not printed twice");
  assert.match(html, /<button id="sign" type="button" data-testid="sign-button" disabled>Sign<\/button><button id="cancel"/);
  assert.match(html, /cancelBtn\.focus\(\)/, "Cancel has the initial focus");
  assert.doesNotMatch(html, /autofocus|style="/);
});

test("design: rows show a label beside an IRI (the IRI in the tooltip), and the label column is not a column of its own", async () => {
  const m = await media("viewers/rows/rows.js");
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  m.mount(el, { use: { options: {} }, kit, request: async () => ({ head: { vars: ["owner", "ownerLabel"] }, results: { bindings: [{ owner: { value: "https://example.org/person/p1" }, ownerLabel: { value: "Ann Example" } }] } }), intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  assert.match(el.innerHTML, /<td data-col="owner"><span data-tip="https:\/\/example\.org\/person\/p1">Ann Example<\/span><\/td>/);
  assert.doesNotMatch(el.innerHTML, /data-head="ownerLabel"/);
});
