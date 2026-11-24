// The status bar's line from a query (with its freshness), and the columns the shell adds to an answer: what a text
// cites (resolved against run-artifact rows) and where decisions on one thing differ.
import { test } from "node:test";
import assert from "node:assert/strict";
import { statusLine } from "../src/composition/slots.js";
import { artifactsOf, citedIds, citeOptions, differsOptions, resolveCite, withCitations, withDiffers } from "../src/ui/annotate.js";

const ans = (rows: Array<Record<string, string>>) => ({ head: { vars: rows[0] ? Object.keys(rows[0]) : [] }, results: { bindings: rows.map((r) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, { type: "literal", value: v }]))) } });
const rows = (a: unknown) => ((a as { results: { bindings: Array<Record<string, { value: string }>> } }).results.bindings).map((r) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, v.value])));
const hhmm = (iso: string) => { const d = new Date(iso); return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`; };

test("statusLine: ?text with {col} and {col|time}; a check older than staleAfterMinutes shows the stale text, never the old line", () => {
  const row = { text: "target-a: up since {since|time}, {version}, last run {lastRun}", since: "2026-11-23T12:09:31Z", version: "1.2.3", lastRun: "run-7", checkedAt: "2026-11-23T14:45:01Z", staleAfterMinutes: "65" };
  const now = new Date("2026-11-23T15:00:00Z");
  const l = statusLine(ans([row]), now);
  assert.equal(l?.text, `target-a: up since ${hhmm(row.since)}, 1.2.3, last run run-7`);
  assert.equal(l?.stale, false);
  assert.match(l?.tooltip ?? "", /checkedAt: 2026-11-23T14:45:01Z/);
  const late = statusLine(ans([{ ...row, staleText: "target-a: status check stopped" }]), new Date("2026-11-23T15:51:00Z"));
  assert.equal(late?.text, `target-a: status check stopped (last check ${hhmm(row.checkedAt)})`);
  assert.equal(late?.stale, true);
  assert.equal(statusLine(ans([{ ...row, staleText: "" }]), new Date("2026-11-23T16:00:00Z"))?.text, `status check stopped (last check ${hhmm(row.checkedAt)})`, "default stale text; never \"down\"");
  assert.equal(statusLine(ans([{ n: "3" }])), undefined, "a number item stays a number item");
  assert.equal(statusLine(ans([{ text: "a" }, { text: "b" }]))?.text, "a · b");
});

const A = (c: string) => `sha256:${c.repeat(64)}`;
const ARTS = artifactsOf(ans([
  { sha256: A("a"), run: "run-1", name: "unit-1/shot.png", mediaType: "image/png", claim: `https://kton.dev/o/${"1".repeat(64)}` },
  { sha256: `sha256:abcd1234${"0".repeat(56)}`, run: "run-1", name: "unit-1/log.txt", mediaType: "text/plain", claim: "" },
  { sha256: `sha256:abcd1234${"f".repeat(56)}`, run: "run-2", name: "unit-2/log.txt", mediaType: "text/plain", claim: "" },
]));

test("citedIds: sha256 ids, kton IRIs and hex runs of 8+; digits only need the prefix", () => {
  assert.deepEqual(citedIds(`see aaaaaaaa and sha256:${"b".repeat(64)}, 20261123, sha256:12345678 and abc1234 and ${"c".repeat(70)}`), ["aaaaaaaa", "b".repeat(64), "12345678"]);
});

test("resolveCite: one artifact by its sha256 or its claim, several (ambiguous), none", () => {
  assert.equal(("one" in resolveCite("aaaaaaaa", ARTS) && resolveCite("aaaaaaaa", ARTS)) ? "one" : "", "one");
  assert.ok("one" in resolveCite("11111111", ARTS), "by the artifact claim's id");
  assert.deepEqual(resolveCite("abcd1234", ARTS), { many: 2 });
  assert.deepEqual(resolveCite("deadbeef", ARTS), { none: true });
});

test("withCitations: a text's citations as a packed artifacts column (images as thumbnails), what did not resolve in a note; structured first", () => {
  const o = citeOptions({ cite: "artifacts", citeColumns: ["text", "why"], citeStructured: "evidence" });
  assert.deepEqual(o, { columns: ["text", "why"], query: "artifacts", structured: "evidence" });
  assert.equal(citeOptions({ cite: "artifacts", citeColumns: [] }), undefined);
  const out = rows(withCitations(ans([
    { text: "Fixed, see aaaaaaaa and abcd1234.", why: "also deadbeef" },
    { text: "aaaaaaaa", why: "", evidence: "abcd12340000" },
    { text: "no citation", why: "" },
  ]), o as never, ARTS));
  const lines = (out[0]?.["cites"] ?? "").split("\n").map((l) => l.split("\t"));
  assert.deepEqual(lines.map((l) => [l[1], l[2], l[3], l[4], l[7]]), [["assertion-screenshot", A("a"), "run-1", "unit-1/shot.png", "cited as aaaaaaaa"]]);
  assert.equal(out[0]?.["citesNote"], "abcd1234 matches 2 artifacts: cite more digits; deadbeef is not a run artifact here");
  assert.equal(out[0]?.["citesFrom"], "text");
  assert.deepEqual((out[1]?.["cites"] ?? "").split("\t").slice(1, 5), ["cited", `sha256:abcd1234${"0".repeat(56)}`, "run-1", "unit-1/log.txt"], "the structured citation, not the text");
  assert.equal(out[1]?.["citesFrom"], "structured");
  assert.equal(out[2]?.["cites"], undefined);
});

test("withDiffers: a later decision by someone else that differs is flagged, and marked when its signer leads", () => {
  const d = differsOptions({ differs: "element|who|when|verdict", differsLead: ["Lead"] });
  assert.ok(d);
  const out = rows(withDiffers(ans([
    { element: "e1", who: "Lead", when: "2026-11-23T12:00:00Z", verdict: "gap" },
    { element: "e1", who: "Reviewer", when: "2026-11-22T12:00:00Z", verdict: "covered" },
    { element: "e1", who: "Reviewer", when: "2026-11-21T12:00:00Z", verdict: "gap" },
    { element: "e2", who: "Reviewer", when: "2026-11-22T12:00:00Z", verdict: "covered" },
    { element: "e2", who: "Other", when: "2026-11-23T12:00:00Z", verdict: "covered" },
  ]), d as never));
  assert.equal(out[0]?.["differs"], "differs from Reviewer (covered, 2026-11-22T12:00:00Z)");
  assert.equal(out[0]?.["differsLead"], "yes");
  assert.equal(out[1]?.["differs"], undefined, "the same signer changing its mind is not a conflict");
  assert.equal(out[4]?.["differs"], undefined, "the same value");
  assert.equal(differsOptions({ differs: "x" }), undefined);
});

test("the rows block accepts the options the shell reads (a plan refuses an option a viewer does not accept)", async () => {
  const { readFileSync } = await import("node:fs");
  const acc = JSON.parse(readFileSync("media/workbench/viewers/rows/viewer.json", "utf8")).accepts as Record<string, unknown>;
  assert.equal(acc["cite"], "literal");
  assert.deepEqual(acc["citeColumns"], { set: "literal" });
  assert.equal(acc["citeStructured"], "literal");
  assert.equal(acc["differs"], "literal");
  assert.deepEqual(acc["differsLead"], { set: "literal" });
  assert.deepEqual(acc["askEach"], { set: "literal" });
});
