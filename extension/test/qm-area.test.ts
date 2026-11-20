// The generic QM area (PR 3, SHELL-FLOWS §S6) over the S6 test packages (test/e2e/packages/s6, ktonpkg v0.9.3 form).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { approvalCounts, areaComposition, BUILTIN, loadBuiltin, recordComposition, registerOf, withBuiltin } from "../src/composition/qm-area.js";
import { planComposition, type Subject } from "../src/composition/slots.js";
import { PackagesArea, areaSubject, type ActPath, type CompositionUi } from "../src/composition/area.js";
import { CompositionHost } from "../src/composition/host.js";
import { loadPackages, type Control, type Loaded, type SlotEntry } from "../src/packages/model.js";
import type { AreaDecl, UiListing } from "../src/packages/listing.js";
import type { Transport } from "../src/transport/port.js";
import { listed, PKGS, reader, type Fixture } from "./pkg-helpers.js";

const S6 = join(PKGS, "s6");
const NAMES = ["qa.area-one", "qa.area-two", "qa.register", "qa.rules"];
const fixtures = (): Fixture[] => NAMES.map((n) => listed(join(S6, n), n));
const builtin = () => loadBuiltin(join(process.cwd(), "media", "workbench"));
const PERSON_KID = "d049e71b4bf0c861";
const AGENT_KID = "40061b17878d7517";

async function s6(only = NAMES): Promise<Loaded> {
  const fx = fixtures().filter((f) => only.includes(f.pkg.name));
  const l = await loadPackages({ packages: fx.map((f) => f.pkg) }, reader(...fx));
  assert.deepEqual(l.refusals, []);
  return l;
}
const areaOf = (l: Loaded, pkg: string) => l.entries.find((e) => e.package === pkg && e.slot === "activity") as SlotEntry & { area: AreaDecl };

/** A composition's shape without its names: part ids, blocks, option keys, param keys, acts and emits. */
function shape(decl: Record<string, unknown>): unknown {
  return (decl["parts"] as Array<{ id: string; blocks: Array<Record<string, unknown>> }>).map((p) => ({
    id: p.id,
    blocks: p.blocks.map((b) => ({ id: b["id"], use: b["use"], hasQuery: !!b["query"], options: Object.keys((b["options"] ?? {}) as object).sort(), params: Object.keys((b["params"] ?? {}) as object).sort(), emits: b["emits"], acts: ((b["acts"] ?? []) as Array<{ intent: string }>).map((a) => a.intent) })),
  }));
}

test("S6.6: no area-specific code: no name of any S6 package, query, control or template appears in the shell's source", () => {
  const names = new Set<string>(NAMES);
  for (const n of NAMES) {
    for (const d of ["queries", "controls", "templates"]) {
      try {
        for (const f of readdirSync(join(S6, n, d))) names.add(f.replace(/\.(rq|json)$/, ""));
      } catch {
        /* the package has no such folder */
      }
    }
  }
  names.delete("aliases");
  // the register is found by its two query names, the same for every area (the training package has them too)
  names.delete("documents");
  names.delete("document");
  const words = ["deviation", "evaluation", "supplier", "QA-", "qa:", ...NAMES];
  const files: string[] = [];
  const walk = (d: string) => {
    for (const f of readdirSync(d)) {
      const p = join(d, f);
      if (statSync(p).isDirectory()) walk(p);
      else files.push(p);
    }
  };
  walk(join(process.cwd(), "src"));
  walk(join(process.cwd(), "media", "workbench"));
  for (const f of files) {
    const t = readFileSync(f, "utf8");
    // a code path for one area would compare against its names: none appears as a string literal
    for (const n of names) assert.ok(!["\"", "'", "`"].some((q) => t.includes(`${q}${n}${q}`)), `${f} has the literal ${n}`);
    for (const w of words) assert.ok(!t.includes(w), `${f} names ${w}`);
  }
});

test("S6.6: both areas get the same composition, by the same code: equal shape, only the names differ", async () => {
  const l = await s6();
  const one = areaComposition(areaOf(l, "qa.area-one"), l);
  const two = areaComposition(areaOf(l, "qa.area-two"), l);
  // area-two's findings package is not installed: the findings part is a notice there (S6.13); the rest is equal
  const strip = (d: Record<string, unknown>) => (shape(d) as Array<{ id: string }>).filter((p) => p.id !== "findings");
  assert.deepEqual(strip(two.decl), strip(one.decl));
  assert.deepEqual(shape(recordComposition(areaOf(l, "qa.area-two"), l).decl), shape(recordComposition(areaOf(l, "qa.area-one"), l).decl));
  assert.notEqual(JSON.stringify(one.decl), JSON.stringify(two.decl), "the names differ");
});

test("S6.1/S6.16: the QM areas are listed with their labels, ordered by order then package name", async () => {
  const l = await s6();
  const area = new PackagesArea({} as Transport, {} as CompositionUi, {} as ActPath, new Map(), builtin());
  area.loaded = l;
  const node = area.areaNodes()[0];
  assert.equal(node?.label, "QM areas");
  assert.deepEqual(node?.children?.map((c) => [c.label, c.description]), [["Supplier evaluations", "qa.area-two"], ["Deviations", "qa.area-one"]]);
  assert.deepEqual(node?.children?.[1]?.command?.args?.[1], { kind: "scope", iri: "https://kton.dev/scope/qa.deviations", id: "qa.deviations", scope: "qa.deviations" });
});

test("S6.2-4, 7, 9, 13-15: the area's parts come from the declaration; the reads, register and findings resolve by name", async () => {
  const l = await s6();
  assert.equal(registerOf("qa.area-one", l), "qa.register");
  const one = areaComposition(areaOf(l, "qa.area-one"), l).decl as { parts: Array<{ id: string; heading: string; blocks: Array<Record<string, unknown>> }>; vars: unknown };
  const parts = Object.fromEntries(one.parts.map((p) => [p.id, p]));
  assert.deepEqual(Object.keys(parts), ["governing", "records", "findings", "raci", "training", "documents", "approvals"]);
  assert.deepEqual(parts["records"]?.blocks[0]?.["query"], { package: "qa.area-one", query: "deviations" });
  assert.equal((parts["records"]?.blocks[0]?.["options"] as { open: string }).open, "item", "list → record");
  // findings: every finding source of the named package, kept to the area's rule ids (S6.3)
  assert.deepEqual(parts["findings"]?.blocks.map((b) => [(b["query"] as { query: string }).query, (b["options"] as { rules: string[] }).rules]), [
    ["major-deviation-owner", ["QA-R-MAJOR-OWNER", "QA-R-CLOSED-DATE"]],
    ["closed-deviation-date", ["QA-R-MAJOR-OWNER", "QA-R-CLOSED-DATE"]],
  ]);
  // the reader's param ($person, from subject.id) is a var a RACI row sets; the training query reads it (S6.7, S6.10)
  assert.deepEqual(one.vars, { person: { type: "iri" } });
  assert.deepEqual(parts["raci"]?.blocks[0]?.["emits"], { select: "person" });
  assert.deepEqual(parts["training"]?.blocks[0]?.["params"], { person: "var.person" });
  assert.deepEqual(parts["training"]?.blocks[0]?.["optional"], ["person"]);
  // documents through the register, kept to the area's keys, shown as links (S6.4)
  assert.deepEqual((parts["documents"]?.blocks[0]?.["options"] as { keys: string[]; open: string }), { keys: ["QA-SOP-10", "QA-FOR-10"], open: "document", columns: ["key", "title", "revision", "state", "effectiveDate"] });
  // governing SOP's state from the register's `document` ($key) (S6.15)
  assert.deepEqual(parts["governing"]?.blocks[0]?.["params"], { key: "governing" });
  const two = areaComposition(areaOf(l, "qa.area-two"), l);
  assert.deepEqual(two.params, { governing: { type: "literal", value: "QA-SOP-20" } });
  const p2 = Object.fromEntries((two.decl["parts"] as Array<{ id: string; blocks: Array<Record<string, unknown>> }>).map((p) => [p.id, p]));
  // S6.13: a findings package that is not installed is said to be so, never "no findings"
  assert.match(JSON.stringify(p2["findings"]), /The findings package qa.rules-suppliers is not installed: its findings are not known/);
  // S6.14: an empty records scope reads as planned
  assert.equal((p2["records"]?.blocks[0]?.["options"] as { emptyText: string }).emptyText, "Planned, 0 records (scope qa.evaluations).");
});

test("plan: the area and record compositions plan on PR 2's renderer contract; the record carries the area's actions", async () => {
  const l = await s6();
  const e = areaOf(l, "qa.area-one");
  const asked: string[] = [];
  const q = async (pkg: string, name: string) => (asked.push(`${pkg}/${name}`), { results: { bindings: [] } });
  const a = areaComposition(e, l);
  const ap = await planComposition({ package: BUILTIN, composition: String(a.decl["composition"]) }, withBuiltin(l, builtin(), a), areaSubject(e), a.params, "qa-decider", q);
  assert.deepEqual(ap.uses.map((u) => u.key.split("#").pop()), ["governing", "records", "findings-1", "findings-2", "raci", "training", "documents", "approvals"]);
  const training = ap.uses.find((u) => u.key.endsWith("#training")) as unknown as { waiting?: string[]; queryOptional?: string[]; params: object };
  assert.ok(!training.waiting?.length, "training does not wait: unfiltered until a person is picked (S6.7)");
  assert.deepEqual(training.queryOptional, ["person"]);
  assert.deepEqual(training.params, {}, "no filter bound until the pick");
  const subject: Subject = { kind: "claim", iri: "https://example.org/qa/deviation/D-002", id: `sha256:${"2".repeat(64)}`, claim: `sha256:${"2".repeat(64)}` };
  const r = recordComposition(e, l);
  const rp = await planComposition({ package: BUILTIN, composition: String(r.decl["composition"]) }, withBuiltin(l, builtin(), r), subject, r.params, "qa-decider", q);
  const rec = rp.uses.find((u) => u.key.endsWith("#record"));
  assert.deepEqual(rec?.params, { record: { type: "uri", value: subject.iri } });
  assert.deepEqual(rec?.acts?.map((x) => [x.intent, x.label, x.control?.name, x.about]), [
    ["sign", "Approve", "approves", subject.claim],
    ["record", "Add a note", "note", subject.claim],
  ]);
  assert.deepEqual(rp.uses.map((u) => u.key.split("#").pop()), ["record", "approvals", "history"]);
  assert.deepEqual(asked, [], "planning asks nothing (no every); the frame asks through the host");
});

test("S6.11/S6.12: an approval counts only as `approved` by a person key or a person tier; rejected and agent approvals do not", () => {
  const me = { keyId: PERSON_KID, personTiers: ["persons"] };
  const signer = (kid: string) => `https://kton.dev/agent/${kid}`;
  assert.deepEqual(approvalCounts({ outcome: "approved", signer: signer(PERSON_KID) }, me), { counts: true, why: "yes: person key" });
  assert.deepEqual(approvalCounts({ outcome: "rejected", signer: signer(PERSON_KID) }, me), { counts: false, why: "no: rejected" });
  assert.deepEqual(approvalCounts({ outcome: "approved", signer: signer(AGENT_KID) }, me), { counts: false, why: "no: not signed by a person key" });
  assert.deepEqual(approvalCounts({ outcome: "approved", signer: signer("ffffffffffffffff"), tier: "persons" }, me), { counts: true, why: "yes: tier persons" });
  assert.deepEqual(approvalCounts({ outcome: "approved", signer: signer("ffffffffffffffff"), tier: "agent" }, me), { counts: false, why: "no: not signed by a person key" });
});

/** The packages area with the S6 packages and a cockpit that answers with fixed rows. */
function areaWith(fx: Fixture[], rows: Record<string, Array<Record<string, string>>>) {
  const shown: Array<{ key: string; title: string }> = [];
  const errors: string[] = [];
  const asked: Array<{ name: string; terms: unknown }> = [];
  const acts: Array<{ control: Control; about: string; fields: unknown; expect: unknown; into: unknown }> = [];
  const t = {
    uiList: async () => ({ ok: true, value: { packages: fx.map((f) => f.pkg) } as UiListing }),
    uiCat: async (pkg: string, h: string) => reader(...fx)(pkg, h),
    list: async () => ({ ok: true, value: { queries: [] } }),
    ask: async (req: { name: string; bindTerms?: unknown }) => {
      asked.push({ name: req.name, terms: req.bindTerms });
      const r = rows[req.name] ?? [];
      const vars = [...new Set(r.flatMap((x) => Object.keys(x)))];
      return { ok: true, value: { results: { head: { vars }, results: { bindings: r.map((x) => Object.fromEntries(Object.entries(x).map(([k, v]) => [k, { type: /^https?:/.test(v) ? "uri" : "literal", value: v }]))) } } } };
    },
  } as unknown as Transport;
  const posts: unknown[] = [];
  const ui: CompositionUi = {
    showComposition: (key, title) => (shown.push({ key, title }), { post: (m) => posts.push(m), urls: new Map() }),
    showLeaf: () => ({ post: () => undefined }),
    closePanel: () => undefined,
    openPanels: () => [],
    info: () => undefined,
    error: (x) => errors.push(x),
    log: () => undefined,
  };
  const path: ActPath = {
    act: async (control, about, fields, _title, expect, into) => (acts.push({ control, about, fields, expect, into }), "signed"),
    open: async () => "opened elsewhere",
    identityName: () => "qa-decider",
    person: () => ({ keyId: PERSON_KID, personTiers: [] }),
  };
  return { area: new PackagesArea(t, ui, path, new Map(), builtin()), shown, errors, asked, acts, posts };
}

test("S6.2/S6.5/S6.12: open the area, a list row opens its record; an act there goes into the records scope with the revision; approvals carry `counts`", async () => {
  const D2 = `https://kton.dev/o/${"2".repeat(64)}`;
  const w = areaWith(fixtures(), {
    deviations: [{ item: "https://example.org/qa/deviation/D-002", claim: D2, identifier: "D-002" }],
    deviation: [{ item: "https://example.org/qa/deviation/D-002", claim: D2, identifier: "D-002" }],
    "deviation-approvals": [
      { claim: `https://kton.dev/o/${"a".repeat(64)}`, record: D2, outcome: "approved", signer: `https://kton.dev/agent/${PERSON_KID}` },
      { claim: `https://kton.dev/o/${"b".repeat(64)}`, record: D2, outcome: "approved", signer: `https://kton.dev/agent/${AGENT_KID}` },
    ],
  });
  await w.area.refresh();
  const e = areaOf(w.area.loaded as Loaded, "qa.area-one");
  const key = `qa.area-one/${e.file}`;
  const host = (await w.area.openSlot(key, areaSubject(e))) as CompositionHost;
  assert.ok(host instanceof CompositionHost, w.errors.join("; "));
  assert.deepEqual(w.shown.at(-1), { key: `composition:${key}`, title: "Deviations" });
  const use = (h: CompositionHost, id: string) => h.plan.uses.find((u) => u.key.endsWith(`#${id}`))?.key as string;
  // approvals: the shell's column `counts` (S6.12)
  await host.onMessage({ type: "kton:request", id: "ap#1", cap: "query@1", args: { use: use(host, "approvals") } });
  const answer = (w.posts.at(-1) as { result: { head: { vars: string[] }; results: { bindings: Array<Record<string, { value: string }>> } } }).result;
  assert.ok(answer.head.vars.includes("counts"));
  assert.deepEqual(answer.results.bindings.map((b) => b["counts"]?.value), ["yes: person key", "no: not signed by a person key"]);
  // list → record (S6.2): the open intent opens the record composition; its subject is the record query's claim,
  // not the one the frame names
  await host.onMessage({ type: "kton:intent", intent: { id: "o#1", kind: "open", from: use(host, "records"), uri: "https://example.org/qa/deviation/D-002", claim: `https://kton.dev/o/${"9".repeat(64)}` } });
  assert.deepEqual(w.asked.at(-1), { name: "deviation", terms: { record: { type: "iri", value: "https://example.org/qa/deviation/D-002" } } });
  assert.deepEqual(w.shown.at(-1), { key: `composition:${key}#record`, title: "Deviations: D-002" });
  const rec = w.area.hostOf(`${key}#record`) as CompositionHost;
  assert.equal(rec.plan.subject.claim, `sha256:${"2".repeat(64)}`);
  // S6.5: the note (record control) and approves (decision) go to the sign path with the records scope and revision
  for (const name of ["note", "approves"]) {
    await rec.onMessage({ type: "kton:intent", intent: { id: `${name}#1`, kind: name === "note" ? "record" : "sign", from: use(rec, "record"), about: `sha256:${"2".repeat(64)}`, control: { package: "qa.area-one", name } } });
  }
  assert.deepEqual(w.acts.map((a) => [a.control.name, a.control.kind, a.control.meaning, a.about, a.into]), [
    ["note", "record", undefined, `sha256:${"2".repeat(64)}`, { scope: "qa.deviations" }],
    ["approves", "decision", "approved", `sha256:${"2".repeat(64)}`, { scope: "qa.deviations" }],
  ]);
  assert.deepEqual(w.acts[0]?.expect, { package: "qa.area-one", revision: (w.area.loaded as Loaded).packages["qa.area-one"]?.revision });
  // S6.10: the record query is bound to the record (subject.iri), not to all
  await rec.onMessage({ type: "kton:request", id: "r#1", cap: "query@1", args: { use: use(rec, "record") } });
  assert.deepEqual(w.asked.at(-1), { name: "deviation", terms: { record: { type: "iri", value: "https://example.org/qa/deviation/D-002" } } });
});
