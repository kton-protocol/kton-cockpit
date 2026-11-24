// Which template a decision signs (a guard counts a decision only in the vocabulary it expects): a package's decision
// control when its review view applies (only the fields that template has), the proposal's own vocabulary's decides
// (<ns>proposes → <ns>decides), else the window's own; never another vocabulary's. The dialog says which and why.
import { test } from "node:test";
import assert from "node:assert/strict";
import { chooseDecideControl, vocabularyOf } from "../src/ui/controller.js";
import type { TemplateInfo } from "../src/transport/port.js";

const fallback = { package: "workbench", name: "decide", template: "decides", kind: "decision" as const };
const fields = { answers: "sha256:" + "a".repeat(64), outcome: "https://schema.org/AcceptAction", choice: "b", conditions: "[]" };
const tpl = (name: string, predicate: string, f: string[], pkg?: string) => ({ name, predicate, hash: "h", ...(pkg ? { package: pkg, revision: "r" } : {}), fields: Object.fromEntries(f.map((k) => [k, { type: "string" }])) }) as unknown as TemplateInfo;
const ONE = "https://example.org/vocab-one/";
const TWO = "https://example.org/vocab-two#";
const templates = [
  tpl("decides", "https://example.org/workbench-e2e/decides", ["answers", "outcome", "choice"]),
  tpl("vocab-one/decides", `${ONE}decides`, ["answers", "outcome", "choice", "text"], "vocab-one"),
  tpl("review-decides", `${TWO}decides`, ["choice", "lookedAt"], "review.pkg"),
];

test("vocabularyOf: up to the last / or #", () => {
  assert.equal(vocabularyOf(`${ONE}proposes`), ONE);
  assert.equal(vocabularyOf(`${TWO}decides`), TWO);
  assert.equal(vocabularyOf("proposes"), undefined);
});

test("a proposal of a vocabulary is decided in that vocabulary's decides, says why, and keeps only that template's fields", () => {
  const r = chooseDecideControl({ type: `${ONE}proposes` }, fields, templates, undefined, fallback);
  assert.deepEqual([r.control?.package, r.control?.template], ["vocab-one", "decides"]);
  assert.deepEqual(r.fields, { answers: fields.answers, outcome: fields.outcome, choice: "b" }, "conditions is no field of that template");
  assert.match(r.why ?? "", /proposal in the vocabulary https:\/\/example\.org\/vocab-one\/ \(its decisions are https:\/\/example\.org\/vocab-one\/decides\)/);
  assert.equal(r.control?.why, r.why, "the why goes into the dialog with the control");
});

test("a proposal with a package review view is decided with that package's control, with the fields its template has", () => {
  const r = chooseDecideControl({ type: `${ONE}proposes` }, fields, templates, { package: "review.pkg", name: "decides", template: "review-decides", kind: "decision" }, fallback);
  assert.deepEqual([r.control?.package, r.control?.name, r.control?.template], ["review.pkg", "decides", "review-decides"]);
  assert.deepEqual(r.fields, { choice: "b" });
  assert.match(r.why ?? "", /review\.pkg's review view applies to this proposal/);
});

test("the window's own vocabulary keeps the window's decides control; a proposal whose vocabulary has no decides here offers none", () => {
  const own = chooseDecideControl({ type: "https://example.org/workbench-e2e/proposes" }, fields, templates, undefined, fallback);
  assert.deepEqual([own.control?.package, own.control?.name, own.control?.template], ["workbench", "decide", "decides"]);
  const none = chooseDecideControl({ type: "https://example.org/vocab-three/proposes" }, fields, templates, undefined, fallback);
  assert.equal(none.control, undefined, "never another vocabulary's decides");
  assert.match(none.cannot ?? "", /No decision template of this proposal's vocabulary \(https:\/\/example\.org\/vocab-three\/decides\) is installed here/);
  const untyped = chooseDecideControl({}, fields, templates, undefined, fallback);
  assert.equal(untyped.control?.template, "decides");
  assert.match(untyped.why ?? "", /window's own decision template/);
});
