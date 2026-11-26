// Rows that fit: nothing scrolls sideways. The column tiers (cols, line2, details), the acts column's side, the
// disclosure, the narrow rule and the short hashes, without a browser (the block mounted on a stand-in element).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type Any = any;
const media = (p: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", p)).href);
const esc = (s: string) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string);
const VARS = ["a", "b", "c", "d", "e", "f", "g"];
const HEX = "0123456789abcdef0123456789abcdef01234567";

async function draw(options: Record<string, unknown>, width = 0, acts: Any[] = []): Promise<string> {
  const m = await media("viewers/rows/rows.js");
  const row = (n: number) => Object.fromEntries([...VARS.map((v) => [v, { value: `${v}${n}` }]), ["h", { value: HEX }], ["claim", { value: `https://example.org/r/${n}` }]]);
  const el: Any = { innerHTML: "", clientWidth: width, querySelectorAll: () => [], querySelector: () => null };
  m.mount(el, { use: { options, acts }, kit: { esc }, request: async () => ({ head: { vars: [...VARS, "h", "claim"] }, results: { bindings: [row(1), row(2)] } }), intent: async () => ({}), setState: () => {} });
  await new Promise((r) => setTimeout(r, 10));
  return el.innerHTML as string;
}
const heads = (h: string) => [...h.matchAll(/<th scope="col"[^>]*data-head="([^"]+)"/g)].map((x) => x[1]);

test("rows fit: tiers: no option puts the first 4 columns on line 1 and the rest in the details", async () => {
  const { tiers } = await media("viewers/rows/rows.js");
  assert.deepEqual(tiers(VARS), { line1: ["a", "b", "c", "d"], line2: [], details: [{ col: "e" }, { col: "f" }, { col: "g" }] });
  assert.deepEqual(tiers(["a", "b"]), { line1: ["a", "b"], line2: [], details: [] });
});

test("rows fit: tiers: explicit cols, line2 and details; a column sits in one tier; unknown columns are dropped", async () => {
  const { tiers } = await media("viewers/rows/rows.js");
  const known = (c: string) => VARS.includes(c);
  assert.deepEqual(tiers(VARS, { cols: ["a", "b", "c", "d", "e"], line2: ["f|Label F"], details: ["g|Gee", "zz"] }, false, known), {
    line1: ["a", "b", "c", "d", "e"], line2: [{ col: "f", label: "Label F" }], details: [{ col: "g", label: "Gee" }],
  });
  // only details given: the rest is line 1 (never more than the shown columns)
  assert.deepEqual(tiers(VARS, { details: ["e", "f", "g"] }, false, known).line1, ["a", "b", "c", "d"]);
  // only line2 given
  assert.deepEqual(tiers(VARS, { line2: ["d", "e"] }, false, known), { line1: ["a", "b", "c", "f", "g"], line2: [{ col: "d" }, { col: "e" }], details: [] });
  // a column named in two tiers stays in the first (cols, line2, details)
  const t = tiers(VARS, { cols: ["a", "b"], line2: ["b", "c"], details: ["c", "d"] }, false, known);
  assert.deepEqual([t.line1, t.line2.map((e: Any) => e.col), t.details.map((e: Any) => e.col)], [["a", "b"], ["c"], ["d"]]);
});

test("rows fit: tiers: narrow moves the line 1 columns beyond the first 2 to the front of line 2", async () => {
  const { tiers } = await media("viewers/rows/rows.js");
  assert.deepEqual(tiers(VARS, {}, true), { line1: ["a", "b"], line2: [{ col: "c" }, { col: "d" }], details: [{ col: "e" }, { col: "f" }, { col: "g" }] });
  assert.deepEqual(tiers(VARS, { cols: ["a", "b", "c"], line2: ["f"] }, true, (c: string) => VARS.includes(c)), { line1: ["a", "b"], line2: [{ col: "c" }, { col: "f" }], details: [] });
  assert.deepEqual(tiers(["a", "b"], {}, true).line1, ["a", "b"], "two columns stay");
});

test("rows fit: the table has line 1's headers, a hidden details row behind a button, and no line 2 without line2 columns", async () => {
  const h = await draw({});
  assert.deepEqual(heads(h), ["a", "b", "c", "d"]);
  assert.match(h, /<button type="button" class="disc" data-disc="0" aria-expanded="false" aria-controls="det-\d+-0" aria-label="Details">/);
  assert.match(h, /<tr class="det" id="det-\d+-0" data-det="0" hidden><td colspan="4"><dl class="kv det-dl"><dt>e<\/dt><dd data-col="e" data-detail="e"><span>e1<\/span><\/dd><dt>f<\/dt>/);
  assert.doesNotMatch(h, /class="l2"/);
  assert.equal((h.match(/<tr data-row=/g) ?? []).length, 2);
});

test("rows fit: line2 is one muted line under the row with labels inline; the acts cell spans both lines", async () => {
  const acts = [{ intent: "record", control: { name: "note" } }];
  const h = await draw({ cols: ["a", "b", "c"], line2: ["d|Dee", "e"], details: ["f"], target: "claim" }, 0, acts);
  assert.match(h, /<tr class="l2" data-l2="0"><td colspan="3"><div class="l2g"><span class="l2i" data-col="d"><span class="l2l">Dee<\/span> <span>d1<\/span><\/span><span class="l2i" data-col="e"><span class="l2l">e<\/span> <span>e1<\/span><\/span><\/div><\/td><\/tr>/);
  assert.match(h, /<td data-acts class="acts" rowspan="2">/);
  assert.equal((h.match(/class="l2"/g) ?? []).length, 2, "one second line per row, never a third");
  assert.match(h, /<tr class="det"[^>]*><td colspan="4">/, "details span the line 1 columns and the acts");
});

test("rows fit: actsAt last (default) puts the acts column after the data, first before it", async () => {
  const acts = [{ intent: "record", control: { name: "note" } }];
  const last = await draw({ target: "claim" }, 0, acts);
  assert.ok(last.indexOf('data-head="d"') < last.indexOf('class="acts-h"'));
  assert.ok(last.indexOf('data-col="d"') < last.indexOf("data-acts"));
  const first = await draw({ target: "claim", actsAt: "first" }, 0, acts);
  assert.ok(first.indexOf('class="acts-h"') < first.indexOf('data-head="a"'));
  assert.match(first, /<tr data-row="0"[^>]*><td data-acts class="acts"><div class="acts-stack">/);
  assert.match(first, /data-actw="\d+"/);
});

test("rows fit: narrow (under 560 px): the line 1 columns beyond the first 2 are on line 2", async () => {
  const wide = await draw({ cols: ["a", "b", "c", "d"] }, 800);
  assert.deepEqual(heads(wide), ["a", "b", "c", "d"]);
  assert.doesNotMatch(wide, /class="l2"/);
  const narrow = await draw({ cols: ["a", "b", "c", "d"] }, 400);
  assert.deepEqual(heads(narrow), ["a", "b"]);
  assert.match(narrow, /class="tbl-wrap narrow"/);
  assert.match(narrow, /<tr class="l2" data-l2="0"><td colspan="2"><div class="l2g"><span class="l2i" data-col="c">/);
});

test("rows fit: a hash of 16 hex characters or more is shown as its first 8, the full value in the tooltip and copyable", async () => {
  const m = await media("viewers/rows/rows.js");
  assert.equal(m.shortHex(HEX), "01234567…");
  assert.equal(m.shortHex(`sha256:${HEX}`), "01234567…");
  assert.ok(m.isLongHex(HEX) && m.isLongHex(`sha256:${HEX}`) && !m.isLongHex("0123456789abcde") && !m.isLongHex("not a hash 0123456789abcdef"));
  const h = await draw({ cols: ["a", "h"] });
  assert.match(h, new RegExp(`<span class="hash" tabindex="0" role="button" title="${HEX}" data-copy="${HEX}">01234567…</span>`));
});

test("rows fit: the stylesheet lets nothing scroll sideways: no overflow-x on the table wrap or the nav, a fixed layout", () => {
  const css = readFileSync(join(process.cwd(), "media", "workbench", "design", "workbench.css"), "utf8");
  assert.doesNotMatch(css, /overflow-x:\s*auto/);
  assert.match(css, /\.tbl-wrap table \{ table-layout: fixed; \}/);
  assert.match(css, /\.tbl-wrap th, \.tbl-wrap td \{ overflow-wrap: anywhere; \}/);
  assert.match(css, /\.wb-nav \{[^}]*flex-wrap: wrap;/);
});

test("rows fit: the viewer declares the tier options", () => {
  const acc = JSON.parse(readFileSync(join(process.cwd(), "media", "workbench", "viewers", "rows", "viewer.json"), "utf8")).accepts as Record<string, unknown>;
  for (const k of ["cols", "line2", "details", "actsAt"]) assert.ok(k in acc, k);
});
