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

test("design step 5: rows: groups keep query order and sum, numbers by type, the range spec and mini-bar positions", async () => {
  const r = await media("viewers/rows/rows.js");
  const b = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).map(([k, v]) => [k, { type: "literal", value: v }]));
  // dm-areas, 20.11.: 4 groups, the unsettled per group 4/3/84/17/107 = 215
  const rows = [
    b({ gxp: "access control", area: "usersGroups", unsettled: "3" }), b({ gxp: "access control", area: "rightsManagement", unsettled: "1" }),
    b({ gxp: "audit trail", area: "auditTrail", unsettled: "3" }),
    b({ gxp: "data integrity", area: "repository", unsettled: "70" }), b({ gxp: "data integrity", area: "steps", unsettled: "8" }), b({ gxp: "data integrity", area: "purge", unsettled: "4" }), b({ gxp: "data integrity", area: "filesViewingEditing", unsettled: "2" }),
    b({ gxp: "e-signature", area: "review", unsettled: "17" }),
    b({ gxp: "none", area: "vscode-extension", unsettled: "49" }), b({ gxp: "none", area: "CLI", unsettled: "30" }), b({ gxp: "none", area: "analysis", unsettled: "20" }), b({ gxp: "none", area: "search", unsettled: "3" }), b({ gxp: "none", area: "workspace", unsettled: "2" }), b({ gxp: "none", area: "reporting", unsettled: "1" }), b({ gxp: "none", area: "workbench", unsettled: "1" }), b({ gxp: "none", area: "Notification", unsettled: "1" }),
  ];
  const g = r.groupRows(rows, "gxp", "unsettled");
  assert.deepEqual(g.map((x: Any) => [x.group, x.rows.length, x.sum]), [["access control", 2, 4], ["audit trail", 1, 3], ["data integrity", 4, 84], ["e-signature", 1, 17], ["none", 8, 107]]);
  assert.equal(g.reduce((a: number, x: Any) => a + x.sum, 0), 215);
  assert.deepEqual(g[2].rows.map((x: Any) => x.area.value), ["repository", "steps", "purge", "filesViewingEditing"], "query order within a group");
  assert.ok(r.isNumeric("n", [{ n: { type: "literal", value: "235", datatype: "http://www.w3.org/2001/XMLSchema#integer" } }]));
  assert.ok(r.isNumeric("rate", [{ rate: { type: "literal", value: "1.7" } }]));
  assert.ok(!r.isNumeric("rate", [{ rate: { type: "literal", value: "1.7 % (0.7–4.3, n=235)" } }]), "formatted text is not a number");
  assert.ok(r.isNumeric("x", [{ x: { value: "a" } }], ["x"]), "named numeric");
  assert.deepEqual(r.rangeSpec("rate,low,high,n", "0,20,5"), { rate: "rate", low: "low", high: "high", n: "n", lo: 0, hi: 20, target: 5 });
  assert.equal(r.rangeSpec("rate", "0,20,5"), undefined);
  assert.equal(r.barPos(1.7, 0, 20), 8.5);
  assert.equal(r.barPos(37.7, 0, 20), 100, "clamped");
});

test("design step 5: rows draws group headers, too few muted, a total, no stamp column with stamp: section", async () => {
  const m = await media("viewers/rows/rows.js");
  const answer = { head: { vars: ["gxp", "area", "unsettled", "rate", "low", "high", "n", "enough", "stamp"] }, results: { bindings: [
    { gxp: { value: "data integrity" }, area: { value: "repository" }, unsettled: { value: "70" }, rate: { value: "1.7" }, low: { value: "0.7" }, high: { value: "4.3" }, n: { value: "235" }, enough: { value: "true" }, stamp: { value: "sha256:b610b5c6" } },
    { gxp: { value: "data integrity" }, area: { value: "purge" }, unsettled: { value: "4" }, rate: { value: "0" }, low: { value: "0" }, high: { value: "41" }, n: { value: "7" }, enough: { value: "false" }, stamp: { value: "sha256:b610b5c6" } },
  ] } };
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  m.mount(el, { use: { options: { columns: ["area", "unsettled", "rate", "low", "n", "stamp"], group: "gxp", groupSum: "unsettled", range: "rate,low,high,n", scale: "0,20,5", measurable: "enough", total: "unsettled", stamp: "section", labels: ["area|Area", "unsettled|Unsettled"] } }, kit, request: async () => answer, intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  const h = el.innerHTML as string;
  assert.match(h, /<tr class="grp" data-group="data integrity"><th colspan="5" scope="colgroup">Data integrity <span class="muted">2 areas · 74 unsettled<\/span><\/th><\/tr>/);
  assert.doesNotMatch(h, /data-head="stamp"/, "no stamp column");
  assert.match(h, /data-stamp="sha256:b610b5c6"/, "each row keeps its stamp");
  assert.match(h, /class="few">too few to measure/);
  assert.match(h, /<span class="rv">1\.7 %<\/span><span class="mb" aria-hidden="true" data-lo="3\.5" data-hi="21\.5" data-pt="8\.5" data-t="25">/);
  assert.match(h, /<th class="r" data-total>74<\/th>/);
  assert.match(h, /Rate <span class="muted">\(scale 0–20 %, dashed = target 5 %\)<\/span>/);
});

test("design step 6: source chips: once per part, the round's label (MH 20.11.), the scope where no round stamps it", async () => {
  const s = await media("design/stamps.js");
  const round = { stamp: "sha256:b610b5c6", label: "mapping round 9 · b610b5c6", full: `https://kton.dev/o/b610b5c6${"f".repeat(56)}`, scope: "dryrun1.mapping" };
  const c = s.chipFor(round);
  assert.equal(c.text, "source: mapping round 9 · b610b5c6");
  assert.match(c.tip, /mapping round 9 · b610b5c6 \(scope dryrun1\.mapping\)\nsha256:b610b5c6f{56}$/, "scope and full hash in the tooltip");
  const runs = { stamp: "dryrun1.runs · p2-rest-1", scope: "dryrun1.runs" };
  assert.equal(s.chipFor(runs).text, "source: dryrun1.runs", "no round: the scope");
  assert.equal(s.fullHash("https://kton.dev/o/" + "a".repeat(64)), `sha256:${"a".repeat(64)}`);
  const r = s.sectionStamps([
    { id: "spec", stamps: [round, round] },
    { id: "runs", stamps: [runs, { ...runs, stamp: "dryrun1.runs · p2-rest-2" }, runs, runs, runs] },
    { id: "none", stamps: [] },
    { id: "mixed", stamps: [round, { stamp: "sha256:00000000" }] },
  ]);
  assert.equal(r.section, round, "the section's source: one vote per block, the first block's on a tie (many runs do not outvote the evidence)");
  assert.equal(r.blocks.spec, null, "equal to the section: no block chip");
  assert.equal(r.blocks.runs?.scope, "dryrun1.runs", "another source: the block's own chip (one for all its runs)");
  assert.equal(r.blocks.none, null);
  assert.equal(r.blocks.mixed, "mixed", "a block that mixes sources keeps its row stamps");
  assert.deepEqual(s.sectionStamps([]), { section: null, blocks: {} });
});

test("design step 7: cards: short test names and decoded locations, verdict pills, the segmented choice gates the one primary", async () => {
  const c = await media("viewers/cards/cards.js");
  assert.equal(c.shortTest("improve-rest-test:com/scinteco/improve/integrationtests/resources/StepRunTest.kt#a step can be run with a clean inventory"), "StepRunTest.kt · a step can be run with a clean inventory");
  assert.equal(c.shortTest("improver:test-reviews.R#deleteReviewer returns FALSE for invalid review ID|ics369,ics2045"), "test-reviews.R · deleteReviewer returns FALSE for invalid review ID");
  assert.equal(c.shortTest("improve-test:Basic/Workbench/iat340%20Views.rsc"), "iat340 Views.rsc", "decoded, never %20");
  assert.equal(c.shortTest("plain text"), "plain text");
  assert.equal(c.locLabel("improve-rest-test/src/test/kotlin/com/scinteco/improve/integrationtests/BasicTest.kt:749"), "improve-rest-test · …/integrationtests/BasicTest.kt:749");
  assert.deepEqual(["asserted", "exercised", "no-relation", "unclear", "covered"].map(c.verdictClass), ["v-asserted", "v-exercised", "v-norel", "v-unclear", "v-asserted"]);
  const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
  const row = { element: { value: "ics2041#e6" }, gxp: { value: "data integrity" }, target: { value: "https://x/e6" }, statement: { value: "The export body" }, earlier: { value: "unclear" }, stamp: { value: "sha256:b610b5c6" } };
  c.mount(el, { use: { key: "spot#cards", options: { title: "element", group: "gxp", badge: "earlier|earlier: ", quote: "statement", fields: ["testName|Test"], compare: ["judge1|Judge 1 · rule judge"], target: "target", choices: ["verdict=rule-judge-right|Rule judge is right", "verdict=spec-unclear|Spec unclear"], choiceLegend: "Your verdict", layout: "pager", pick: "element|Show evidence" }, blockActs: [{ intent: "sign", label: "Sign verdict…", control: { name: "spotcheck" } }] }, kit, request: async () => ({ results: { bindings: [row] } }), intent: async () => ({}), setState: () => {} });
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

test("design step 8: rule cases: effects per answer, the resolves line, a stacked bar to scale", async () => {
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

test("design step 9: one primary per object: the first sign act; record acts and the rest secondary", async () => {
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

test("design step 10: every block: a skeleton while it loads (busy), an empty state that says what and how, an error callout", async () => {
  for (const v of ["figures", "estimate", "rows", "evidence", "requests", "cards"]) {
    const loading = await mountOn(v, { emptyText: "No data for dryrun2", emptyHow: "Counts come from dryrun2.mapping." });
    assert.match(loading.innerHTML, /class="wb-skel [a-z]+" data-area-loading aria-busy="true"/, `${v}: skeleton`);
    assert.doesNotMatch(loading.innerHTML, /<button/, `${v}: no act while it loads`);
    const el: Any = { innerHTML: "", querySelectorAll: () => [], querySelector: () => null };
    const m = await media(`viewers/${v}/${v}.js`);
    m.mount(el, { use: { options: { emptyText: "No data for dryrun2", emptyHow: "Counts come from dryrun2.mapping.", title: "x" } }, kit, request: async () => ({ head: { vars: [] }, results: { bindings: [] } }), intent: async () => ({}), setState: () => {} });
    await new Promise((r) => setTimeout(r, 10));
    assert.match(el.innerHTML, /<div class="empty" data-area-empty>[\s\S]*<b>No data for dryrun2<\/b><p>Counts come from dryrun2\.mapping\.<\/p>/, `${v}: empty says what and how`);
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
  const spec = b({ kind: "spec", repo: "improve-specs", commit: "49966c23eceb754264ea6c36296a6ca2ff6214b8", path: "specifications/Rights%20Management/ics1000%20Publish%20rights.md", line: "31", label: "Open spec: ics1000 Publish rights.md at line 31" });
  assert.deepEqual(e.itemTitle(spec, v), { title: "ics1000 Publish rights.md", sub: "line 31", loc: "improve-specs · specifications/Rights Management/ · at 49966c23eceb" }, "never %20");
  const test = b({ kind: "test", repo: "improve-rest-test", commit: "d74be51196a3", path: "src/test/kotlin/BasicTest.kt", line: "749", label: "Open test at line 749: improve-rest-test:com/x/ResourceAclTest.kt#an acl for a publish user" });
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
  assert.equal(r.shortId("ext-core-e2e:uat/ics1824.spec.ts#RBN-002: Explorer toolbar"), "ics1824.spec.ts · RBN-002: Explorer toolbar");
  assert.equal(r.shortId("lane:improve-rest-test:ReviewsTest.kt#entries of another review"), "ReviewsTest.kt · entries of another review");
  assert.equal(r.shortId(`https://kton.dev/o/${"a".repeat(64)}`), "sha256:aaaaaaaaaaaa…");
  assert.equal(r.shortId("https://scinteco.com/ns/improve/spec/ics2041/6da9"), "6da9");
  assert.deepEqual(r.groupNotes(["lane harness 1: login | lane harness 3: New (+)", "lane harness 1: login", ""]), ["lane harness 1: login", "lane harness 3: New (+)"]);
  const sha = (c: string) => `sha256:${c.repeat(64)}`;
  const ev = r.parseEvidence([["2", "assertion-screenshot", sha("b"), "r", "n2", "image/jpeg", "1", "a.ts:9", "two"], ["1", "assertion-screenshot", sha("a"), "r", "n1", "image/jpeg", "1", "a.ts:3", "one"], ["", "trace", sha("c"), "r", "t.zip", "application/zip", "1", "", ""], ["", "bad", "nohash"]].map((x) => x.join("\t")).join("\n"));
  assert.deepEqual(ev.map((x: Any) => [x.assertion, x.kind, x.site]), [["1", "assertion-screenshot", "a.ts:3"], ["2", "assertion-screenshot", "a.ts:9"], ["", "trace", ""]]);
});
