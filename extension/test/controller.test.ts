// The VS Code layer's behaviour, without VS Code: the controller against a fake transport and host.
// Test names start with the flow step of the shell flows (S1–S10) they stand for, where there is one.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Workbench, DEFAULT_CONFIG, claimSubject } from "../src/ui/controller.js";
import { listed, PKGS, reader } from "./pkg-helpers.js";
import type { TreeNode, UiHost, StatusId, ViewId } from "../src/ui/host.js";
import { rowsOf } from "../src/ui/queries.js";
import { decisionPage, esc, signDialogPage } from "../src/ui/pages.js";
import { loadSurfaces, surfaceFolder } from "../src/ui/surfaces.js";
import { CliTransport } from "../src/transport/cli.js";
import { mkdtempSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { AskRequest, AskResult, Outcome, SayRequest, SayResult, TemplateInfo, Transport } from "../src/transport/port.js";
import { TransportError, TransportTimeout } from "../src/transport/port.js";
import type { WindowIdentity } from "../src/identity.js";
import type { UiListing } from "../src/packages/listing.js";
import { Scheduler, type Clock } from "../src/poll.js";
import { workbenchCommands } from "../src/ui/commands.js";
import { CMD } from "../src/ui/controller.js";

const HEX = (c: string) => c.repeat(64);
const iri = (c: string) => `https://kton.dev/o/${HEX(c)}`;
const sha = (c: string) => `sha256:${HEX(c)}`;
const P1 = iri("1");
const P2 = iri("6");
const M1 = iri("2");
const M2 = iri("3");
const OPTIONS = JSON.stringify([{ id: "a", label: "Build 39", consequence: "the tested build" }, { id: "b", label: "Build 41", consequence: "newer, untested" }]);

// The query folder holds only display queries; decides and answers come by name from a package.
const queries = new Map([
  ["proposals", "Q proposals"],
  ["messages", "Q messages"],
  ["lanes", "Q lanes"],
]);
const PKID = "1111222233334444"; // the window's person claim key id
const AKID = "aaaabbbbccccdddd"; // its agent claim key id
const agentIri = (kid: string) => `https://kton.dev/agent/${kid}`;

const TEMPLATES: TemplateInfo[] = [
  { name: "decides", fields: { answers: { type: "ref", required: true }, outcome: { type: "enum", required: true, values: ["https://schema.org/AcceptAction", "https://schema.org/RejectAction"] }, choice: { type: "string" } } },
  { name: "done", fields: { answers: { type: "ref" }, outcome: { type: "string", required: true } } },
  { name: "requests", fields: { area: { type: "string", required: true }, title: { type: "string", required: true }, text: { type: "string", required: true }, why: { type: "string" } } },
  { name: "announces", fields: { area: { type: "string", required: true }, title: { type: "string", required: true }, text: { type: "string", required: true }, why: { type: "string", required: true } } },
];

const identity: WindowIdentity = {
  hintName: "qa-decider",
  person: { role: "person", keyName: "qa-decider", keyId: PKID },
  agent: { role: "agent", keyName: "qa-decider-agent", keyId: AKID },
};

class FakeTransport implements Transport {
  says: SayRequest[] = [];
  templateList: TemplateInfo[] = TEMPLATES;
  sayError: Error | undefined;
  sayErrors: Error[] = [];
  /** Queued answers for say (refusals, pending); each call takes one before the default answer. */
  sayOutcomes: Outcome<SayResult>[] = [];
  sayCalls = 0;
  askError: Error | undefined;
  answers: Record<string, Array<Record<string, string>>> = {
    "Q proposals": [
      { msg: P1, area: "qa", title: "Which build?", text: "Two candidates", why: "seed", options: OPTIONS, recommended: "a", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T10:00:00Z" },
      { msg: P2, area: "qa", title: "Agent decided this one", text: "t", why: "seed", options: OPTIONS, from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T10:30:00Z" },
    ],
    // an agent key's decides about P2: a record, never a decision (review check 7, S4.5)
    "N decides": [{ d: iri("8"), answers: P2, outcome: "https://schema.org/AcceptAction", choice: "a", s: agentIri(AKID), by: "qa-decider-agent-claims", byTier: "agent", when: "2026-11-18T11:00:00Z" }],
    "Q messages": [
      { msg: M1, kind: "https://example.org/v/requests", area: "qa", title: "Check the notes", text: "t", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T09:00:00Z" },
      { msg: M2, kind: "https://example.org/v/announces", area: "ops", title: "Answered one", text: "t", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T08:00:00Z" },
    ],
    "N answers": [{ d: iri("4"), answers: M2, outcome: "done", s: agentIri(PKID), by: "qa-decider-claims", byTier: "self" }],
    "Q lanes": [{ lane: "active/qa-lane", areas: "qa" }, { lane: "active/ops-lane", areas: "ops,infra" }],
  };
  asked: AskRequest[] = [];
  async ask(req: AskRequest): Promise<Outcome<AskResult>> {
    if (this.askError) throw this.askError;
    this.asked.push(req);
    // a named query (from an admitted package) answers under "N <name>", a query text under itself
    const key = req.name !== undefined ? `N ${req.name}` : (req.sparql ?? "");
    return { ok: true, value: { query: "sparql", ref: "", raw: "", sparql: { rows: this.answers[key] ?? [] } } };
  }
  /** the --expect-* values of every say (SPEC §8.5), kept apart from says so they read as before */
  expects: Array<Pick<SayRequest, "expectTemplate" | "expectRevision" | "expectPredicate">> = [];
  async say(full: SayRequest): Promise<Outcome<SayResult>> {
    const { expectTemplate, expectRevision, expectPredicate, ...req } = full;
    this.expects.push({ ...(expectTemplate ? { expectTemplate } : {}), ...(expectRevision ? { expectRevision } : {}), ...(expectPredicate ? { expectPredicate } : {}) });
    const e = this.sayErrors.shift() ?? this.sayError;
    if (e) throw e;
    this.sayCalls++;
    const queued = this.sayOutcomes.shift();
    if (queued) {
      if (queued.ok) this.says.push(req);
      return queued;
    }
    this.says.push(req);
    return { ok: true, value: { claimId: sha("9"), confirmation: "registered" } };
  }
  /** a cockpit before v0.5.5: say --json lists no hash and no revision */
  legacyListing = false;
  async templates(): Promise<Outcome<TemplateInfo[]>> {
    if (this.legacyListing) return { ok: true, value: this.templateList.map(({ hash: _h, revision: _r, ...t }) => t) };
    return { ok: true, value: this.templateList.map((t) => ({ hash: `sha256:${"e".repeat(64)}`, ...(t.package !== undefined ? { revision: `sha256:${"f".repeat(64)}` } : {}), ...t })) };
  }
  publish(): never { throw new Error("not used"); }
  workflowRun(): never { throw new Error("not used"); }
  install(): never { throw new Error("not used"); }
  list(): never { throw new Error("not used"); }
  doctor(): never { throw new Error("not used"); }
  version(): never { throw new Error("not used"); }
  async uiList(): Promise<Outcome<UiListing>> { return { ok: true, value: { packages: [] } }; }
  uiCat(_pkg: string, _h: string): Promise<Uint8Array> { return Promise.reject(new Error("not used")); }
  graph(): never { throw new Error("not used"); }
}

class FakeHost implements UiHost {
  status = new Map<StatusId, string>();
  statusHistory: string[] = [];
  statusCommand = new Map<StatusId, string | undefined>();
  windows: string[] = [];
  installed = new Set<string>();
  external: string[] = [];
  trees: Record<string, readonly TreeNode[]> = {};
  panels: Array<{ key: string; html: string; onMessage?: (m: unknown) => void }> = [];
  closed: string[] = [];
  dialogs: Array<{ title: string; detail: string }> = [];
  outcomes: Array<{ ok: boolean; text: string }> = [];
  confirmAnswer = false;
  picks: string[] = [];
  inputs: string[] = [];
  infos: string[] = [];
  errors: string[] = [];
  logs: string[] = [];
  setStatus(id: StatusId, text: string, _t?: string, command?: string): void { this.status.set(id, text); this.statusHistory.push(text); this.statusCommand.set(id, command); }
  setTree(view: ViewId, nodes: readonly TreeNode[]): void { this.trees[view] = nodes; }
  reveals: boolean[] = [];
  showPanel(key: string, _t: string, html: string, onMessage?: (m: unknown) => void, reveal = true): void {
    this.panels.push(onMessage ? { key, html, onMessage } : { key, html });
    this.reveals.push(reveal);
  }
  openPanels(): string[] { return [...new Set(this.panels.map((p) => p.key))].filter((k) => !this.closed.includes(k)); }
  closePanel(key: string): void { this.closed.push(key); }
  compositions: Array<{ key: string; onMessage: (m: unknown) => void; posted: unknown[]; viewers: number }> = [];
  showComposition(key: string, _t: string, viewers: readonly { module: string }[], onMessage: (m: unknown) => void) {
    const c = { key, onMessage, posted: [] as unknown[], viewers: viewers.length };
    this.compositions.push(c);
    return { post: (m: unknown) => void c.posted.push(m), urls: new Map(viewers.map((v) => [v.module, `served:${v.module}`])) };
  }
  showLeaf(key: string, _t: string, leaf: { viewer: string }, onMessage: (m: unknown) => void) {
    const c = { key, onMessage, posted: [] as unknown[], viewers: 1, leaf: leaf.viewer };
    this.compositions.push(c);
    return { post: (m: unknown) => void c.posted.push(m) };
  }
  async signDialog(title: string, detail: string): Promise<boolean> { this.dialogs.push({ title, detail }); return this.confirmAnswer; }
  signOutcome(ok: boolean, text: string): void { this.outcomes.push({ ok, text }); }
  async openWindow(folder: string): Promise<void> { this.windows.push(folder); }
  externalFails?: string;
  async runExternal(ext: string, cmd: string): Promise<boolean> { if (!this.installed.has(ext)) return false; if (this.externalFails) throw new Error(this.externalFails); this.external.push(cmd); return true; }
  async pick(): Promise<string | undefined> { return this.picks.shift(); }
  async input(): Promise<string | undefined> { return this.inputs.shift(); }
  info(t: string): void { this.infos.push(t); }
  error(t: string): void { this.errors.push(t); }
  log(l: string): void { this.logs.push(l); }
}

const manualClock: Clock = { now: () => 0, setTimeout: () => 0, clearTimeout: () => undefined };

const SURFACES = [
  { name: "core", lanes: ["active/qa-lane"], folder: "/surfaces/core" },
  { name: "ops", lanes: ["active/ops-lane"] },
];

async function setup(cfg: Partial<typeof DEFAULT_CONFIG> = {}, t: Transport = new FakeTransport(), id: WindowIdentity = identity) {
  const h = new FakeHost();
  const wb = new Workbench(t, h, queries, id, { ...DEFAULT_CONFIG, surfaces: SURFACES, lockRetry: { attempts: 3, delayMs: 1 }, ...cfg }, new Scheduler(manualClock));
  await wb.start();
  await wb.refresh();
  return { t: t as FakeTransport, h, wb };
}
const decisionIntent = (choice: string, extra: Record<string, string> = {}) => ({ kind: "sign", about: sha("1"), fields: { answers: sha("1"), outcome: "https://schema.org/AcceptAction", choice, ...extra } });

test("S1.1: status bar shows both keys, \"test\" (unverified: test keys, no identity verb) and test mode; decisions waiting apart from the inbox", async () => {
  const { h } = await setup();
  const s = h.status.get("identity") ?? "";
  assert.equal(s, "person key qa-decider · agent key qa-decider-agent · test");
  assert.ok(!/qa-requestor/.test(s), "no name comes from a claim's signer");
  assert.equal(h.status.get("mode"), "TEST MODE");
  assert.match(h.status.get("poll") ?? "", /^polling: inbox every 30s · 1 open$/);
  // proposals are not inbox messages: their own item, singular/plural, a click shows Home's Decisions waiting
  assert.equal(h.status.get("decisions"), "2 decisions waiting");
  assert.equal(h.statusCommand.get("decisions"), "ktonWorkbench.home.focus");
});

test("S1.3: a signer the view gives no tier for is shown as unknown, never by its label", async () => {
  const t = new FakeTransport();
  t.answers["Q messages"] = [{ msg: M1, kind: "https://example.org/v/requests", area: "qa", title: "From outside", text: "t", from: "outsider-claims", when: "x" }];
  const { h, wb } = await setup({}, t);
  assert.match((h.trees["inbox"] ?? [])[0]?.description ?? "", /unknown signer/);
  assert.ok(!/outsider/.test((h.trees["inbox"] ?? [])[0]?.description ?? ""));
  wb.openThread(M1);
  assert.match(h.panels.at(-1)?.html ?? "", /from unknown signer/);
});

test("S1.5: no identity file: reading works, every signing control is disabled and nothing reaches say", async () => {
  const { t, h, wb } = await setup({}, new FakeTransport(), { ...identity, hintName: "" });
  assert.equal(h.status.get("identity"), "no identity file: reading only");
  assert.equal((h.trees["inbox"] ?? []).length, 1, "reading works");
  const opt = (h.trees["home"] ?? [])[0]?.children?.[0]?.children?.[0];
  assert.equal(opt?.disabled, true);
  wb.openDecision(P1);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="cannot-sign"[^>]*>This checkout has no identity file/);
  assert.match(h.panels.at(-1)?.html ?? "", /data-choice="a"[^>]* disabled/);
  h.confirmAnswer = true;
  assert.equal(await wb.chooseOption(P1, "a"), "failed");
  assert.equal(h.dialogs.length, 0);
  assert.equal(t.says.length, 0);
});

test("S1.5: after a rebuild (identity file back) the open panels are rendered again, in place, with the new identity", async () => {
  const before = await setup({}, new FakeTransport(), { ...identity, hintName: "" });
  before.wb.openDecision(P1);
  before.wb.stop();
  // the host's panels stay open; the new workbench is told which
  const h = before.h;
  const wb = new Workbench(new FakeTransport(), h, queries, identity, { ...DEFAULT_CONFIG, surfaces: SURFACES }, new Scheduler(manualClock));
  await wb.start(h.openPanels());
  const n = h.panels.length;
  await wb.refresh();
  assert.equal(h.panels.length, n + 1, "rendered again after the first good poll");
  assert.equal(h.panels.at(-1)?.key, `decision:${P1}`);
  assert.ok(!/cannot-sign/.test(h.panels.at(-1)?.html ?? ""), "signing is enabled again");
  assert.equal(h.reveals.at(-1), false, "in place: the focus does not move");
  await wb.refresh();
  assert.equal(h.panels.length, n + 1, "only once");
});

test("S1.7: a missing person or agent key is named, and neither key stands in for the other", async () => {
  const noPerson = await setup({}, new FakeTransport(), { ...identity, person: { role: "person", keyName: "", keyId: "" } });
  assert.match(noPerson.h.status.get("identity") ?? "", /^person key missing · agent key qa-decider-agent · test$/);
  noPerson.h.confirmAnswer = true;
  assert.equal(await noPerson.wb.chooseOption(P1, "a"), "failed");
  assert.match(noPerson.h.errors.at(-1) ?? "", /no person key.*agent key never stands in/);
  assert.equal(noPerson.t.says.length, 0);

  const noAgent = await setup({}, new FakeTransport(), { ...identity, agent: { role: "agent", keyName: "", keyId: "" } });
  assert.match(noAgent.h.status.get("identity") ?? "", /^person key qa-decider · agent key missing · test$/);
  noAgent.h.picks = ["announces"];
  assert.equal(await noAgent.wb.sayCommand(), "failed");
  assert.match(noAgent.h.errors.at(-1) ?? "", /no agent key.*person key never stands in/);
  assert.equal(noAgent.t.says.length, 0);
  noAgent.h.confirmAnswer = true;
  assert.equal(await noAgent.wb.chooseOption(P1, "b"), "signed", "decisions still work with the person key");
  assert.equal(noAgent.t.says[0]?.as, "qa-decider");
});

test("S2.1/S2.2: Home counts decisions and open messages; Inbox leaves out answered ones; a thread shows its claim id", async () => {
  const { h, wb } = await setup();
  const home = h.trees["home"] ?? [];
  assert.equal(home[0]?.description, "2");
  assert.equal(home[0]?.children?.[0]?.command?.id, "ktonWorkbench.openDecision");
  assert.equal(home[1]?.description, "1 open");
  assert.deepEqual((h.trees["inbox"] ?? []).map((n) => n.label), ["Check the notes"]);
  wb.openThread(M1);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="claim-id">sha256:2{64}</);
});

test("S2.3/S2.6: answer with done through the dialog with the person key; Cancel keeps the text and signs nothing", async () => {
  const { t, h, wb } = await setup();
  wb.openThread(M1);
  const page = h.panels.at(-1);
  assert.ok(page?.onMessage, "an open message can be answered");
  assert.match(page?.html ?? "", /data-testid="answer-form"/);
  const row = { msg: M1, title: "Check the notes" };
  const answer = { kind: "sign", about: sha("2"), fields: { answers: sha("2"), outcome: "Checked; fine." } };
  h.confirmAnswer = false;
  assert.equal(await wb.onThreadIntent(row, answer), "cancelled");
  assert.equal(t.says.length, 0);
  assert.match(h.panels.at(-1)?.html ?? "", /Checked; fine\./, "the draft is kept");
  h.confirmAnswer = true;
  assert.equal(await wb.onThreadIntent(row, answer), "signed");
  assert.deepEqual(t.says[0], { template: "done", subject: sha("2"), fields: { answers: sha("2"), outcome: "Checked; fine." }, as: "qa-decider" });
  assert.match(h.dialogs.at(-1)?.detail ?? "", /Template: done/);
});

test("S2.3: an empty outcome is named on the page; no dialog", async () => {
  const { t, h, wb } = await setup();
  assert.equal(await wb.onThreadIntent({ msg: M1 }, { kind: "sign", about: sha("2"), fields: { answers: sha("2"), outcome: "  " } }), "failed");
  assert.equal(h.dialogs.length, 0);
  assert.equal(t.says.length, 0);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="answer-error">outcome is required/);
});

test("S2.5/S2.6: compose blocks Send while a required field is empty and names it; Cancel keeps the draft; Sign sends it", async () => {
  const { t, h, wb } = await setup();
  await wb.composeCommand("requests");
  const tpl = TEMPLATES.find((x) => x.name === "requests") as TemplateInfo;
  const values = { about: sha("5"), area: "qa", title: "", text: "Please look" };
  assert.equal(await wb.onCompose(tpl, { kind: "compose", values }), "failed");
  assert.equal(h.dialogs.length, 0);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="missing-title">is required/);
  assert.match(h.panels.at(-1)?.html ?? "", /value="Please look"/, "the typed values stay");
  h.confirmAnswer = false;
  assert.equal(await wb.onCompose(tpl, { kind: "compose", values: { ...values, title: "Look at this" } }), "cancelled");
  assert.equal(t.says.length, 0);
  assert.match(h.panels.at(-1)?.html ?? "", /value="Look at this"/);
  assert.match(h.panels.at(-1)?.html ?? "", /The draft is kept/);
  h.confirmAnswer = true;
  assert.equal(await wb.onCompose(tpl, { kind: "compose", values: { ...values, title: "Look at this" } }), "signed");
  assert.deepEqual(t.says[0], { template: "requests", subject: sha("5"), fields: { area: "qa", title: "Look at this", text: "Please look" }, as: "qa-decider" });
  assert.deepEqual(h.closed, ["compose:requests"]);
});

test("S3.2/S3.3: options with consequences and the recommendation; rules labelled as not enforced", async () => {
  const { h, wb } = await setup();
  wb.openDecision(P1);
  const html = h.panels.at(-1)?.html ?? "";
  assert.match(html, /data-testid="consequence-a">the tested build/);
  assert.match(html, /data-testid="consequence-b">newer, untested/);
  assert.match(html, /a · Build 39<\/b><span class="rec" data-testid="recommended">recommended/);
  assert.match(html, /data-testid="guard"><b>Rules<\/b><div class="muted">Rules are shown for information only: they are not enforced/);
});

test("S3.4/S3.5/S4.1: a decision is signed only after the dialog, with the person key, showing what will be signed", async () => {
  const { t, h, wb } = await setup();
  const row = { msg: P1, title: "Which build?" };
  h.confirmAnswer = false;
  assert.equal(await wb.onDecisionIntent(row, decisionIntent("a")), "cancelled");
  assert.equal(t.says.length, 0, "closing the dialog signs nothing");
  const d = h.dialogs.at(-1)?.detail ?? "";
  assert.match(d, /Template: decides/);
  assert.match(d, /choice = a/);
  assert.match(d, /Signs with: person key "qa-decider"/);
  assert.match(d, /not yet a QMS e-signature/);
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent(row, decisionIntent("b")), "signed");
  assert.equal(t.says[0]?.as, "qa-decider");
  assert.deepEqual(t.says[0]?.fields, { answers: sha("1"), outcome: "https://schema.org/AcceptAction", choice: "b" });
  assert.deepEqual(h.outcomes.at(-1), { ok: true, text: `Signed ${sha("9")}` });
});

test("S3.7/S4.1: a/b on the Home card open the same dialog (never one click) and sign with the person key", async () => {
  const { t, h, wb } = await setup();
  const card = (h.trees["home"] ?? [])[0]?.children?.[0];
  assert.deepEqual(card?.children?.map((n) => [n.label, n.description, n.command?.id, n.command?.args]), [
    ["a · Build 39", "recommended", "ktonWorkbench.chooseOption", [P1, "a"]],
    ["b · Build 41", "", "ktonWorkbench.chooseOption", [P1, "b"]],
  ]);
  h.confirmAnswer = false;
  assert.equal(await wb.chooseOption(P1, "a"), "cancelled");
  assert.equal(h.dialogs.length, 1);
  assert.equal(t.says.length, 0);
  h.confirmAnswer = true;
  assert.equal(await wb.chooseOption(P1, "b"), "signed");
  assert.equal(h.dialogs.length, 2);
  assert.equal(t.says[0]?.as, "qa-decider");
});

test("S4.1: the command palette's say with a decision template opens the one dialog and signs with the person key", async () => {
  const { t, h, wb } = await setup();
  h.picks = ["decides"];
  h.inputs = [sha("1"), sha("1"), "https://schema.org/AcceptAction", "b"];
  h.confirmAnswer = true;
  assert.equal(await wb.sayCommand(), "signed");
  assert.equal(h.dialogs.length, 1);
  assert.equal(t.says[0]?.as, "qa-decider");
});

test("O1: a record from the command palette signs with the agent key and needs no dialog", async () => {
  const { t, h, wb } = await setup();
  h.picks = ["announces"];
  h.inputs = [sha("5"), "qa", "Note", "text", "why"];
  assert.equal(await wb.sayCommand(), "signed");
  assert.equal(h.dialogs.length, 0);
  assert.equal(t.says[0]?.as, "qa-decider-agent");
});

test("intents from the page are untrusted: another subject or another kind is refused without a dialog", async () => {
  const { t, h, wb } = await setup();
  const row = { msg: P1 };
  assert.equal(await wb.onDecisionIntent(row, { kind: "sign", about: sha("7"), fields: {} }), "failed");
  assert.equal(await wb.onDecisionIntent(row, { kind: "run", ray: "x", holes: {} }), "failed");
  assert.equal(await wb.onDecisionIntent(row, { kind: "sign", about: sha("1"), fields: { as: "someone-else" } }), "failed");
  assert.equal(h.dialogs.length, 0);
  assert.equal(t.says.length, 0);
});

test("a required field the page did not send is refused before the dialog", async () => {
  const { t, h, wb } = await setup();
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, { kind: "sign", about: sha("1"), fields: { answers: sha("1"), choice: "b" } }), "failed");
  assert.equal(h.dialogs.length, 0);
  assert.equal(t.says.length, 0);
  assert.match(h.errors.at(-1) ?? "", /outcome is required/);
});

test("the decision page escapes text and only posts sign intents about its own proposal", () => {
  const html = decisionPage({ msg: P1, title: "<script>alert(1)</script>", options: JSON.stringify([{ id: "a", label: "<b>x</b>" }]) }, "n0nce");
  assert.ok(!html.includes("<script>alert"));
  assert.ok(html.includes(esc("<b>x</b>")));
  assert.match(html, /script-src 'nonce-n0nce'/);
  assert.match(html, new RegExp(`about:"sha256:${HEX("1")}"`));
});

test("rowsOf reads both answer forms", () => {
  assert.deepEqual(rowsOf({ query: "sparql", ref: "", raw: "", sparql: { rows: [{ a: "1" }] } }), [{ a: "1" }]);
  assert.deepEqual(
    rowsOf({ query: "sparql", ref: "", raw: "", results: { head: { vars: ["a"] }, results: { bindings: [{ a: { type: "literal", value: "2" } }] } } }),
    [{ a: "2" }],
  );
});

test("S4.9: sign dialog page: Sign is disabled until the confirmation is ticked, never the default, Enter is swallowed", () => {
  const html = signDialogPage("Sign decision", "Template: decides", "qa-decider (qa-decider)", "n0");
  assert.match(html, /<button id="sign" type="button" data-testid="sign-button" disabled>Sign<\/button>/);
  assert.ok(!/autofocus/.test(html), "nothing is focused by default");
  assert.match(html, /I know I sign this now as qa-decider \(qa-decider\)/);
  assert.match(html, /if\(e\.key==='Enter'\)\{e\.preventDefault\(\)/);
  assert.match(html, /if\(box\.checked&&!signBtn\.disabled\)\{/);
  assert.match(html, /vscode\.postMessage\(\{type:'sign',confirmed:true\}\)/);
});

test("S9: surface switch: same window, the views show only the surface's lanes and areas", async () => {
  const { h, wb } = await setup();
  assert.equal(h.status.get("surface"), "surface: all");
  assert.equal(h.statusCommand.get("surface"), "ktonWorkbench.switchSurface");
  assert.equal((h.trees["inbox"] ?? []).length, 1);
  await wb.switchSurface("ops");
  assert.equal(h.status.get("surface"), "surface: ops");
  assert.equal((h.trees["inbox"] ?? []).length, 0, "the qa request is not on the ops surface");
  assert.deepEqual((h.trees["lanes"] ?? []).map((n) => n.label), ["ops-lane"]);
  await wb.switchSurface("core");
  assert.deepEqual((h.trees["inbox"] ?? []).map((n) => n.label), ["Check the notes"]);
  assert.equal(h.windows.length, 0, "switching never opens a window");
});

test("S9: Lanes view: open counts per lane; Open in terminal is shown disabled with a reason", async () => {
  const { h } = await setup();
  const lanes = h.trees["lanes"] ?? [];
  assert.deepEqual(lanes.map((n) => [n.label, n.description]), [["ops-lane", "0 open · ops, infra"], ["qa-lane", "1 open · qa"]]);
  const term = lanes[0]?.children?.[0];
  assert.equal(term?.disabled, true);
  assert.match(term?.tooltip ?? "", /comes with the workgroup extension/);
  assert.equal(term?.description, "comes with the workgroup extension");
  assert.equal(term?.command, undefined);
});

test("open a surface session in a new window on its folder; without a folder it says why", async () => {
  const { h, wb } = await setup({ surfaceFolderPattern: "/work/surfaces/${surface}" });
  assert.equal(await wb.openSurfaceWindow("core"), true);
  assert.equal(await wb.openSurfaceWindow("ops"), true);
  assert.deepEqual(h.windows, ["/surfaces/core", "/work/surfaces/ops"]);
  const { h: h2, wb: wb2 } = await setup();
  assert.equal(await wb2.openSurfaceWindow("ops"), false);
  assert.match(h2.errors.at(-1) ?? "", /No folder is known for surface ops/);
  assert.equal(surfaceFolder({ name: "x", lanes: [] }, "no-placeholder"), undefined);
});

test("surfaces file in the workgroup format", async () => {
  const d = mkdtempSync(join(tmpdir(), "surf-"));
  const f = join(d, "surfaces.json");
  writeFileSync(f, JSON.stringify({ _doc: "x", core: { lanes: { a: { lane: "active/a", dir: "/x" } }, folder: "/f" }, support: { lanes: {} } }));
  assert.deepEqual(await loadSurfaces(f), [{ name: "core", lanes: ["active/a"], folder: "/f" }, { name: "support", lanes: [] }]);
  assert.deepEqual(await loadSurfaces(join(d, "missing.json")), []);
});

test("S10: recorder handover: runs the recorder's command if installed, else says it is not installed", async () => {
  const { h, wb } = await setup();
  assert.equal(await wb.openRecorder(), false);
  assert.match(h.infos.at(-1) ?? "", /recorder extension is not installed/);
  h.installed.add("kton.kton-recorder");
  assert.equal(await wb.openRecorder(), true);
  assert.deepEqual(h.external, ["kton.openObservations"]);
  h.externalFails = "observations unavailable";
  assert.equal(await wb.openRecorder(), false, "a rejected handover is caught");
  assert.match(h.errors.at(-1) ?? "", /recorder could not open: observations unavailable/);
});

/** A cockpit stand-in for S7 that answers the views but misbehaves on say. */
function faultyCockpit(mode: "refuse" | "garbage"): string {
  const d = mkdtempSync(join(tmpdir(), "faulty-"));
  const f = join(d, "cockpit");
  const say = mode === "refuse" ? 'echo "the template decides is not allowed here" >&2; exit 2' : 'echo "this is not json"; exit 0';
  writeFileSync(f, `#!/bin/sh\nif [ "$1" = say ] && [ "$2" != --json ]; then ${say}; fi\nif [ "$1" = say ]; then echo '${JSON.stringify(TEMPLATES)}'; exit 0; fi\necho '{"query":"sparql","ref":"","raw":"","sparql":{"rows":[]}}'\n`);
  chmodSync(f, 0o755);
  return f;
}

for (const [label, exe, expect] of [
  ["S7.1 missing cockpit", "/nonexistent/cockpit", /Not signed: cannot run \/nonexistent\/cockpit.*kton\.cockpit\.path/],
  ["S7.5 a refusal", "refuse", /Not signed: the cockpit refused: the template decides is not allowed here/],
  ["S7.6 malformed JSON", "garbage", /Unknown whether signed: say: the answer is not JSON\. Check the registry before signing again/],
] as const) {
  test(`${label}: a clear message, shown in the dialog too, and nothing signed`, async () => {
    const { h, wb } = await setup();
    const path = exe.startsWith("/") ? exe : faultyCockpit(exe as "refuse" | "garbage");
    // Same window, the cockpit broken underneath: swap the transport the way a changed setting does.
    (wb as unknown as { t: Transport }).t = new CliTransport({ executable: path, cwd: tmpdir() });
    h.confirmAnswer = true;
    assert.equal(await wb.onDecisionIntent({ msg: P1, title: "Which build?" }, decisionIntent("a")), "failed");
    assert.match(h.errors.at(-1) ?? "", expect);
    assert.equal(h.outcomes.at(-1)?.ok, false);
    assert.ok(!h.infos.some((i) => i.startsWith("Signed")));
  });
}

test("S7.1: a missing cockpit shows one clear error, not one per poll, and the views say no data", async () => {
  const t = new CliTransport({ executable: "/nonexistent/cockpit", cwd: tmpdir() });
  const h = new FakeHost();
  const wb = new Workbench(t, h, queries, identity, { ...DEFAULT_CONFIG, surfaces: SURFACES }, new Scheduler(manualClock));
  await wb.start();
  await wb.refresh();
  await wb.refresh();
  assert.equal(h.status.get("poll"), "polling: error · no data");
  assert.equal(h.errors.length, 1);
  assert.match(h.errors[0] ?? "", /cannot read the cockpit: cannot run \/nonexistent\/cockpit.*kton\.cockpit\.path/);
  assert.deepEqual((h.trees["home"] ?? []).map((n) => n.label), ["No data: the cockpit cannot be read"]);
  assert.deepEqual((h.trees["inbox"] ?? []).map((n) => n.label), ["No data: the cockpit cannot be read"]);
});

test("S7.2: ask answers non-JSON after good data: an error, and the last good data stays, marked stale", async () => {
  const { t, h, wb } = await setup();
  t.askError = new TransportError("ask: the answer is not JSON", 0, "");
  await wb.refresh();
  assert.match(h.errors.at(-1) ?? "", /cannot read the cockpit: ask: the answer is not JSON/);
  assert.match(h.status.get("poll") ?? "", /polling: error · stale since \d\d:\d\d:\d\d/);
  const home = h.trees["home"] ?? [];
  assert.equal(home[0]?.label, "Stale: the last poll failed");
  assert.deepEqual((h.trees["inbox"] ?? []).map((n) => n.label), ["Check the notes"], "the data is kept");
  assert.match((h.trees["inbox"] ?? [])[0]?.description ?? "", /stale$/);
  t.askError = undefined;
  await wb.refresh();
  assert.notEqual((h.trees["home"] ?? [])[0]?.label, "Stale: the last poll failed");
});

test("S7.3: say times out: the dialog shows it failed, and the registry decides; the shell claims nothing", async () => {
  const { t, h, wb } = await setup();
  t.sayError = new TransportTimeout("say timed out after 4 s", "");
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("a")), "failed");
  assert.equal(h.outcomes.at(-1)?.ok, false);
  assert.match(h.outcomes.at(-1)?.text ?? "", /Unknown whether signed: say timed out after 4 s\. Check the registry before signing again/);
  assert.ok(!h.infos.some((i) => i.startsWith("Signed")));
});

test("S7.4: a refusal store.busy (nothing signed) is retried, shown in the status bar, and completes once released", async () => {
  const { t, h, wb } = await setup();
  const busy: Outcome<SayResult> = { ok: false, refusal: { code: "store.busy", reason: "the registry is busy (git lock)" } };
  t.sayOutcomes = [busy, busy];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "signed");
  assert.deepEqual(h.statusHistory.filter((s) => s.startsWith("registry locked")), ["registry locked: retrying say (1/2)", "registry locked: retrying say (2/2)"]);
  assert.equal(t.says.length, 1, "one claim signed, after two busy refusals that signed nothing");
});

test("S7.4: a busy registry that is not released gives up with a clear message and signs nothing", async () => {
  const { t, h, wb } = await setup();
  const busy: Outcome<SayResult> = { ok: false, refusal: { code: "store.busy", reason: "the registry is busy (git lock)" } };
  t.sayOutcomes = [busy, busy, busy];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "failed");
  assert.match(h.errors.at(-1) ?? "", /Not signed: the cockpit refused: the registry is busy/);
  assert.equal(t.says.length, 0);
});

test("S7.8: a say answered with pending store.busy is signed, shown as not yet committed, and never retried", async () => {
  const { t, h, wb } = await setup();
  t.sayOutcomes = [{ ok: true, value: { claimId: sha("8"), confirmation: "registered", pending: { code: "store.busy", reason: "the registry was busy; the next write commits it" } } }];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "signed");
  assert.equal(t.sayCalls, 1, "say is called once: a pending claim is never signed again");
  assert.equal(h.outcomes.at(-1)?.ok, true);
  assert.match(h.outcomes.at(-1)?.text ?? "", /Signed sha256:8+, not yet committed/);
  assert.ok(!h.errors.some((e) => /Not signed/.test(e)), "never told 'not signed' about a signed claim");
});

for (const code of ["store", "template.not-allowed"]) {
  test(`S7.4: a refusal with code ${code} is never retried (it may come after the claim was signed); say is called once`, async () => {
    const { t, h, wb } = await setup();
    t.sayOutcomes = [{ ok: false, refusal: { code, reason: `refused: ${code}` } }];
    h.confirmAnswer = true;
    assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "failed");
    assert.equal(t.sayCalls, 1, "exactly one say: only store.busy is retried");
    assert.ok(!h.statusHistory.some((s) => s.startsWith("registry locked")), "no retry shown");
    assert.match(h.errors.at(-1) ?? "", new RegExp(`Not signed: the cockpit refused: refused: ${code.replace(".", "\\.")}`));
  });
}

test("S7.4: an error that mentions a lock is NOT retried (the claim may be signed); the registry decides", async () => {
  const { t, h, wb } = await setup();
  t.sayErrors = [new TransportError("git commit failed: index.lock exists", 1, "fatal: Unable to create '.git/index.lock': File exists.\n")];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "failed");
  assert.equal(t.sayErrors.length, 0, "the one queued error was used");
  assert.equal(t.sayCalls, 0, "no second call: never retried");
  assert.match(h.errors.at(-1) ?? "", /^Unknown whether signed: git commit failed: index\.lock exists\. Check the registry before signing again\.$/);
});

test("review check 7: a decides signed by an agent key is not shown as a decision (S4.5); a person's decides is", async () => {
  const t = new FakeTransport();
  const { h, wb } = await setup({}, t);
  // P2 has only an agent key's decides: still open, and its agent claim shows as a record, not a decision
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P1, P2]);
  assert.equal((h.trees["home"] ?? [])[0]?.description, "2");
  wb.openDecision(P2);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="agent-record">Recorded by qa-decider-agent-claims \(agent\): <b>a<\/b>\. <span class="muted">Not a decision/);
  // a decides by this window's person key closes it
  t.answers["N decides"] = [...(t.answers["N decides"] ?? []), { d: iri("9"), answers: P2, outcome: "https://schema.org/AcceptAction", s: agentIri(PKID), by: "qa-decider-claims", byTier: "self" }];
  await wb.refresh();
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P1]);
  // and so does one by a signer of a configured person tier; an agent tier never does
  t.answers["N decides"] = [{ d: iri("9"), answers: P1, outcome: "x", s: agentIri("9999888877776666"), by: "someone-claims", byTier: "persons" }];
  const { wb: wb2 } = await setup({ personTiers: ["persons"] }, t);
  assert.deepEqual(wb2.openDecisions().map((d) => d["msg"]), [P2]);
});

test("review check 7 (Q1): a decides.rq in the checkout is never used for the decision filter; decides and answers are asked by name", async () => {
  const t = new FakeTransport();
  // the checkout's folder brings a tampered decides.rq (and answers.rq) whose rows would make P2 a person's decision
  const tampered = new Map([...queries, ["decides", "TAMPERED decides"], ["answers", "TAMPERED answers"]]);
  t.answers["TAMPERED decides"] = [{ d: iri("8"), answers: P2, outcome: "x", s: agentIri(PKID), by: "qa-decider-claims", byTier: "persons" }];
  t.answers["TAMPERED answers"] = [{ d: iri("7"), answers: M1, outcome: "x", s: agentIri(PKID), by: "qa-decider-claims", byTier: "self" }];
  const h = new FakeHost();
  const wb = new Workbench(t, h, tampered, identity, { ...DEFAULT_CONFIG, surfaces: SURFACES, personTiers: ["persons"] }, new Scheduler(manualClock));
  await wb.start();
  await wb.refresh();
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P1, P2], "P2 stays open: only an agent key decided it");
  assert.equal((h.trees["inbox"] ?? []).length, 1, "M1 stays open: the tampered answers.rq did not close it");
  assert.ok(!t.asked.some((a) => a.sparql?.startsWith("TAMPERED")), "the checkout's text never reached the cockpit");
  assert.deepEqual(t.asked.filter((a) => a.name !== undefined).map((a) => a.name).sort(), ["answers", "decides"]);
  assert.ok(t.asked.filter((a) => a.name !== undefined).every((a) => a.sparql === undefined), "by name, with no query text");
});

test("review check 7 (Q2): an agent-tier key carrying the person key's label is a record, not a decision", async () => {
  const t = new FakeTransport();
  // a different key (another key id) in tier agent whose file is also named qa-decider-claims
  t.answers["N decides"] = [{ d: iri("9"), answers: P1, outcome: "https://schema.org/AcceptAction", choice: "b", s: agentIri("0123456789abcdef"), by: "qa-decider-claims", byTier: "agent" }];
  const { h, wb } = await setup({ personTiers: ["self"] }, t);
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P1, P2], "P1 is still open");
  wb.openDecision(P1);
  assert.match(h.panels.at(-1)?.html ?? "", /data-testid="agent-record">Recorded by qa-decider-claims \(agent\)/);
  // the same label with the window's person key id is the person's decision
  t.answers["N decides"] = [{ ...t.answers["N decides"]![0]!, s: agentIri(PKID) }];
  await wb.refresh();
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P2]);
  // and without a person key id (it could not be derived), only a person tier counts
  const noId = await setup({ personTiers: [] }, t, { ...identity, person: { ...identity.person, keyId: "" } });
  assert.deepEqual(noId.wb.openDecisions().map((d) => d["msg"]), [P1, P2]);
});

// ---- PR 2: packages and compositions (C2, C1.5 v3) through the controller, with kton-web's test package
class PackagesTransport extends FakeTransport {
  fixtures = [listed(join(PKGS, "kton-web-test"), "kton-web-test", "test")];
  override async uiList(): Promise<Outcome<UiListing>> { return { ok: true, value: { packages: this.fixtures.map((f) => f.pkg) } }; }
  override uiCat(pkg: string, h: string): Promise<Uint8Array> { return reader(...this.fixtures)(pkg, h); }
}
const withTestPackage = () => {
  const t = new PackagesTransport();
  // as v0.5.5 lists it: the template's hash and the sealed revision of the package it came from
  t.templateList = [...TEMPLATES, { name: "test-note", fields: { about: { type: "string", required: true } }, package: "kton-web-test", hash: `sha256:${"e".repeat(64)}`, revision: t.fixtures[0]?.pkg.revision as string }];
  return t;
};
/** Open kton-web-test's composition for P1 and let its frame say ready. */
async function openTestShell(h: FakeHost, wb: Workbench) {
  await wb.openSlot("kton-web-test/ui/test-shell.json", claimSubject(P1));
  const c = h.compositions.at(-1);
  assert.ok(c, "a composition panel opened");
  c.onMessage({ type: "kton:ready", requires: [], optional: [] });
  await new Promise((r) => setImmediate(r));
  return c;
}
const intent = async (c: { onMessage: (m: unknown) => void }, i: Record<string, unknown>) => {
  c.onMessage({ type: "kton:intent", intent: i });
  for (let n = 0; n < 5; n++) await new Promise((r) => setImmediate(r));
};

test("S5.1: a package's review-view entry is offered on a proposal; it opens the composition with its checked viewers", async () => {
  const { h, wb } = await setup({}, withTestPackage());
  const home = h.trees["home"] ?? [];
  const p1 = home[0]?.children?.find((d) => d.label === "Which build?");
  const review = p1?.children?.find((n) => n.command?.id === "ktonWorkbench.openSlot");
  assert.equal(review?.label, "Review · Test shell");
  assert.ok(home.some((n) => n.label === "Packages" && /1 admitted/.test(n.description ?? "")));
  const c = await openTestShell(h, wb);
  const set = c.posted[0] as { type: string; plan: { uses: Array<{ module: string }> }; host: { provides: string[] } };
  assert.equal(set.type, "kton:set");
  assert.ok(set.plan.uses.every((u) => u.module.startsWith("served:packages/kton-web-test@test/viewers/")), "the frame imports modules from where the host serves the checked trees");
  assert.ok(set.host.provides.includes("sign@1") && set.host.provides.includes("record@1"));
  assert.equal(c.viewers, 3, "exactly the viewers the plan uses (probe, needs, diagram)");
});

test("S4.2: a decision act goes through the one sign dialog and signs with the person key", async () => {
  const { t, h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  h.confirmAnswer = true;
  await intent(c, { id: "acts/probe#x.1", kind: "sign", from: "acts#probe", about: sha("1"), control: { package: "kton-web-test", name: "test-decision" } });
  assert.equal(h.dialogs.length, 1, "the sign dialog was shown");
  assert.equal(t.says.at(-1)?.template, "test-note");
  assert.equal(t.says.at(-1)?.as, "qa-decider", "the person key");
  assert.equal(t.says.at(-1)?.subject, sha("1"));
  assert.deepEqual(c.posted.at(-1), { type: "kton:data", id: "acts/probe#x.1", result: { signed: true, key: "person", message: "Signed with the person key (test-only).", claim: `sha256:${"9".repeat(64)}` } });
});

test("S4.3: a record act is written with the agent key and no dialog", async () => {
  const { t, h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  await intent(c, { id: "r#x.1", kind: "record", from: "acts#probe", about: sha("1"), control: { package: "kton-web-test", name: "test-record" } });
  assert.equal(h.dialogs.length, 0, "no dialog");
  assert.equal(t.says.at(-1)?.as, "qa-decider-agent", "the agent key");
});

test("S4.4 / S4.6: an intent cannot change the kind, and say is no act; neither reaches the cockpit", async () => {
  const { t, h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  h.confirmAnswer = true;
  await intent(c, { id: "k#x.1", kind: "record", from: "acts#probe", about: sha("1"), control: { package: "kton-web-test", name: "test-decision" } });
  assert.match(JSON.stringify(c.posted.at(-1)), /kton:refused.*declares no record act through kton-web-test\/test-decision: the intent cannot change the kind/);
  await intent(c, { id: "s#x.1", kind: "say", from: "acts#probe", text: "hello" });
  assert.match(JSON.stringify(c.posted.at(-1)), /say is not an act this shell performs/);
  await intent(c, { id: "a#x.1", kind: "sign", from: "acts#probe", about: sha("2"), control: { package: "kton-web-test", name: "test-decision" } });
  assert.match(JSON.stringify(c.posted.at(-1)), /about another subject than the plan's act/);
  await intent(c, { id: "w#x.1", kind: "sign", from: "wired#echo", about: sha("1"), control: { package: "kton-web-test", name: "test-decision" } });
  assert.match(JSON.stringify(c.posted.at(-1)), /wired#echo declares no sign act/);
  assert.equal(t.says.length, 0);
  assert.equal(h.dialogs.length, 0);
});

test("held acts: an outcome is replayed in the next set after a reload, and dropped once the frame reported its state", async () => {
  const { h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  await intent(c, { id: "r#x.1", kind: "record", from: "acts#probe", about: sha("1"), control: { package: "kton-web-test", name: "test-record" } });
  c.onMessage({ type: "kton:ready" }); // the webview was discarded and came back
  const set = c.posted.at(-1) as { state: { acts: Record<string, { status: string }> } };
  assert.equal(set.state.acts["r#x.1"]?.status, "done");
  c.onMessage({ type: "kton:state", state: { parts: {}, acts: { forged: {} } } });
  c.onMessage({ type: "kton:ready" });
  const again = c.posted.at(-1) as { state: Record<string, unknown> & { acts: Record<string, unknown> } };
  assert.deepEqual(again.state.acts, {}, "dropped after delivery; a frame's own acts are never taken");
});

test("S5.4: a package that is no longer admitted closes its open panel at the next poll", async () => {
  const t = withTestPackage();
  const { h, wb } = await setup({}, t);
  await openTestShell(h, wb);
  t.fixtures = [];
  await wb.refresh();
  assert.deepEqual(h.closed, ["composition:kton-web-test/ui/test-shell.json"]);
  assert.match(h.infos.at(-1) ?? "", /no longer admitted or loaded/);
});

test("S5.3 in the Home tree: a refused package is shown with its reason, a newer slot name is listed and not rendered", async () => {
  const t = withTestPackage();
  const f = t.fixtures[0] as ReturnType<typeof listed>;
  t.fixtures = [{ ...f, pkg: { ...f.pkg, ui: [...f.pkg.ui, { file: "ui/sidebar.json", slot: "sidebar-ad", label: "Ad" }] } }, { pkg: { ...f.pkg, name: "broken", dir: ".kton/packages/broken@1", refused: [{ file: "", code: "package.check", reason: "the seal does not verify" }] }, blobs: new Map() }];
  const { h } = await setup({}, t);
  const pk = (h.trees["home"] ?? []).find((n) => n.label === "Packages");
  const labels = (pk?.children ?? []).map((n) => `${n.label} | ${n.description}`);
  assert.ok(labels.includes("broken | refused: package.check"), labels.join("\n"));
  assert.ok(labels.includes("kton-web-test: Ad | not rendered"), labels.join("\n"));
  const why = (pk?.children ?? []).find((n) => n.label === "broken")?.children?.[0]?.label;
  assert.equal(why, "the seal does not verify");
});

test("H8: an act that ends after the frame reloaded (the dialog took its column) reaches the new frame in a fresh set", async () => {
  const { h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  let release: (v: boolean) => void = () => undefined;
  h.signDialog = (title: string, detail: string) => {
    h.dialogs.push({ title, detail });
    return new Promise<boolean>((r) => (release = r));
  };
  c.onMessage({ type: "kton:intent", intent: { id: "s#old.1", kind: "sign", from: "acts#probe", about: sha("1"), control: { package: "kton-web-test", name: "test-decision" } } });
  await new Promise((r) => setImmediate(r));
  c.onMessage({ type: "kton:ready" }); // the webview was discarded while the dialog was open
  const before = c.posted.length;
  release(true);
  for (let n = 0; n < 8; n++) await new Promise((r) => setImmediate(r));
  const sets = c.posted.slice(before).filter((m) => (m as { type: string }).type === "kton:set") as Array<{ state: { acts: Record<string, { status: string }> } }>;
  assert.equal(sets.length, 1, "a fresh set after the act ended");
  assert.equal(sets[0]?.state.acts["s#old.1"]?.status, "done");
});

test("status bar (MH's demo): both key names, distinct and short; the hint and the reason are in the tooltip", async () => {
  const id = { ...identity, hintName: "demo-person", person: { ...identity.person, keyName: "demo-person" }, agent: { ...identity.agent, keyName: "demo-person-agent" } };
  const { h } = await setup({}, new FakeTransport(), id);
  const s = h.status.get("identity") ?? "";
  assert.equal(s, "person key demo-person · agent key demo-person-agent · test");
  assert.ok(s.length < 80, `${s.length} characters`);
  assert.ok(!/identity verb|\(/.test(s), "no reason in the bar");
});

test("slot items (MH's demo): the entry's label once, with its package; no item repeats its first word", async () => {
  const t = withTestPackage();
  const f = t.fixtures[0] as ReturnType<typeof listed>;
  // a second package whose review-view entry is labelled "Review", like the hostile fixtures, and a record-view
  t.fixtures = [f, { ...f, pkg: { ...f.pkg, name: "other", dir: ".kton/packages/other@1", ui: [{ ...(f.pkg.ui[0] as UiListing["packages"][number]["ui"][number]), label: "Review" }, { ...(f.pkg.ui[0] as UiListing["packages"][number]["ui"][number]), file: "ui/rec.json", slot: "record-view", label: "Provenance" }] } }];
  const { h } = await setup({}, t);
  const p1 = (h.trees["home"] ?? [])[0]?.children?.find((d) => d.label === "Which build?");
  const items = (p1?.children ?? []).filter((n) => n.command?.id === "ktonWorkbench.openSlot").map((n) => n.label);
  assert.deepEqual(items.sort(), ["Record · Provenance", "Review", "Review · Test layout", "Review · Test shell"]);
  for (const l of items) {
    const w = l.split(/[\s·:]+/).filter(Boolean).map((x) => x.toLowerCase());
    assert.ok(w.length === new Set(w).size || w[0] !== w[1], `${l} repeats a word`);
    assert.ok(!new RegExp(`^(\\w+)\\W+\\1\\b`, "i").test(l), `${l} repeats its first word`);
  }
});

test("layout (ktonpkg v0.9.5): the set names host.narrow; the frame's layout state (tab, ratio) is host-held like vars, never acts, and comes back after a reload", async () => {
  const { h, wb } = await setup({}, withTestPackage());
  const c = await openTestShell(h, wb);
  const set = c.posted[0] as { host: { narrow?: number } };
  assert.equal(set.host.narrow, 640);
  c.onMessage({ type: "kton:state", state: { parts: { main: { layout: { tab: "approvals" } }, side: { layout: { ratio: 0.35 } } }, vars: { picked: "B" }, acts: { forged: { status: "done" } } } });
  c.onMessage({ type: "kton:ready" }); // the webview came back
  const again = c.posted.at(-1) as { state: Record<string, unknown> & { acts: Record<string, unknown> } };
  assert.deepEqual(again.state["parts"], { main: { layout: { tab: "approvals" } }, side: { layout: { ratio: 0.35 } } });
  assert.deepEqual(again.state["vars"], { picked: "B" });
  assert.deepEqual(again.state.acts, {}, "a frame's own acts are never taken");
});

test("the user's Refresh runs the poll job through the scheduler: never a second poll beside one", async () => {
  const { t, wb } = await setup();
  let n = 0;
  const templates = t.templates.bind(t);
  t.templates = async () => {
    n++;
    await new Promise((r) => setImmediate(r));
    return templates();
  };
  await Promise.all([wb.refreshNow(), wb.refreshNow()]);
  assert.equal(n, 1, "the second Refresh was skipped while the first ran");
  await wb.refreshNow();
  assert.equal(n, 2);
});

test("the registered Refresh command reaches scheduler.runNow (the command table extension.ts registers)", async () => {
  const { wb } = await setup();
  const ran: string[] = [];
  const runNow = wb.scheduler.runNow.bind(wb.scheduler);
  wb.scheduler.runNow = async (name: string) => {
    ran.push(name);
    return runNow(name);
  };
  const refresh = workbenchCommands(() => wb).find(([id]) => id === CMD.refresh)?.[1] as () => Promise<void>;
  assert.ok(refresh, "Refresh is registered");
  await refresh();
  assert.deepEqual(ran, ["inbox"]);
});

// kton-review R1 on #177, kton-cockpit Y1 on #187: say resolves the template NAME when it signs; the dialog showed what it
// meant then. The plan carries say --json's hash, revision and predicate; say refuses template.changed (SPEC §8.5).
const H1 = `sha256:${"1".repeat(64)}`;
const H2 = `sha256:${"2".repeat(64)}`;
const R1 = `sha256:${"a".repeat(64)}`;
const QM_V1: TemplateInfo = { name: "approves", package: "qm", predicate: "https://example.org/qm/approves-for-release", hash: H1, revision: R1, fields: { outcome: { type: "string" } } };
const QM_V2: TemplateInfo = { ...QM_V1, predicate: "https://example.org/qm/rejects-release", hash: H2, revision: `sha256:${"b".repeat(64)}` };
const APPROVE = { package: "qm", name: "approve", template: "approves", kind: "decision" as const };
/** A cockpit that, like v0.5.5, refuses template.changed when the template it would sign with is not the expected one. */
function qmCockpit() {
  const t = new FakeTransport();
  t.templateList = [...TEMPLATES, QM_V1];
  const say = t.say.bind(t);
  t.say = async (req: SayRequest) => {
    const now = t.templateList.find((x) => x.name === req.template);
    if (req.expectTemplate && now?.hash !== req.expectTemplate) return { ok: false, refusal: { code: "template.changed", reason: `${req.template} is ${String(now?.hash)}, not ${req.expectTemplate}` } };
    return say(req);
  };
  return { t };
}

test("R1/Y1: approves means approves-for-release in the dialog and rejects-release after the upgrade: say refuses template.changed, no claim, sign again", async () => {
  const { t } = qmCockpit();
  const { h, wb } = await setup({}, t);
  h.signDialog = async (title: string, detail: string) => {
    h.dialogs.push({ title, detail });
    t.templateList = [...TEMPLATES, QM_V2]; // qm v2 installed while the dialog is open
    return true;
  };
  assert.equal(await wb.signAct(APPROVE, sha("2"), { outcome: "ok" }, "Approve"), "failed");
  assert.match(h.dialogs[0]?.detail ?? "", /Says: https:\/\/example\.org\/qm\/approves-for-release/);
  assert.match(h.dialogs[0]?.detail ?? "", /Template as listed: sha256:1111111111/, "the dialog names the template it signs with");
  assert.equal(t.says.length, 0, "no claim");
  assert.match(h.outcomes.at(-1)?.text ?? "", /Not signed: the template changed since the dialog showed it \(.*\)\. Nothing was signed: open it again and sign again/);
});

test("Y1: every say carries the expectations as say --json listed them; a panel's act from another revision is refused before the dialog", async () => {
  const { t } = qmCockpit();
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  assert.equal(await wb.signAct(APPROVE, sha("2"), { outcome: "ok" }, "Approve", { package: "qm", revision: R1 }), "signed");
  assert.deepEqual(t.says.at(-1), { template: "approves", subject: sha("2"), fields: { outcome: "ok" }, as: "qa-decider" });
  assert.deepEqual(t.expects.at(-1), { expectTemplate: H1, expectRevision: R1, expectPredicate: "https://example.org/qm/approves-for-release" });
  const dialogs = h.dialogs.length;
  assert.equal(await wb.signAct(APPROVE, sha("2"), { outcome: "ok" }, "Approve", { package: "qm", revision: `sha256:${"c".repeat(64)}` }), "failed");
  assert.equal(h.dialogs.length, dialogs, "no dialog for a stale panel");
  assert.match(h.errors.at(-1) ?? "", /package qm changed since the panel was opened/);
});

// PR 3 (S6.5): an area's actions through the one sign path
const AREA_APPROVES: TemplateInfo = { name: "rec-approves", package: "area", predicate: "https://example.org/area/approves", fields: { outcome: { type: "enum", values: ["approved", "rejected"], required: true } } };
const AREA_NOTE: TemplateInfo = { name: "rec-note", package: "area", predicate: "https://example.org/area/note", fields: { text: { type: "string", required: true } } };

test("F2/O1/N2: the meaning shown is the meaning signed: Approve (meaning approved) fixes outcome = approved; it cannot sign rejected", async () => {
  const t = new FakeTransport();
  t.templateList = [...TEMPLATES, AREA_APPROVES, AREA_NOTE];
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  const approves = { package: "area", name: "approves", template: "rec-approves", kind: "decision" as const, meaning: "approved" };
  // nothing is asked: the control's meaning fixes the outcome; the dialog and the claim agree
  assert.equal(await wb.signAct(approves, sha("2"), {}, "Approve", undefined, { scope: "area.records" }), "signed");
  assert.match(h.dialogs.at(-1)?.detail ?? "", /outcome = approved[\s\S]*Meaning: approved \(signed as outcome = approved\)[\s\S]*Into scope: area\.records[\s\S]*Signs with: person key/);
  assert.deepEqual(t.says.at(-1), { template: "rec-approves", subject: sha("2"), fields: { outcome: "approved" }, as: "qa-decider", scope: "area.records" });
  // asking for rejected is refused: no dialog, no claim
  const says = t.says.length;
  const dialogs = h.dialogs.length;
  assert.equal(await wb.signAct(approves, sha("2"), { outcome: "rejected" }, "Approve", undefined, { scope: "area.records" }), "failed");
  assert.match(h.errors.at(-1) ?? "", /means approved: its outcome is approved, not rejected/);
  assert.equal(t.says.length, says);
  assert.equal(h.dialogs.length, dialogs);
  // a record control: its text is asked, no dialog, the agent key, the same scope
  h.inputs = ["Waiting for the supervisor."];
  const note = { package: "area", name: "note", template: "rec-note", kind: "record" as const };
  assert.equal(await wb.signAct(note, sha("2"), {}, "Add a note", undefined, { scope: "area.records" }), "signed");
  assert.equal(h.dialogs.length, dialogs, "no dialog for a record");
  assert.deepEqual(t.says.at(-1), { template: "rec-note", subject: sha("2"), fields: { text: "Waiting for the supervisor." }, as: "qa-decider-agent", scope: "area.records" });
  // a meaning no field carries is the act's: shown as such, never as a claim field
  const reviewed = { package: "area", name: "reviewed-note", template: "rec-note", kind: "decision" as const, meaning: "reviewed" };
  h.inputs = ["Looked at it."];
  assert.equal(await wb.signAct(reviewed, sha("2"), {}, "Review", undefined, {}), "signed");
  assert.match(h.dialogs.at(-1)?.detail ?? "", /Act: reviewed \(the control's meaning, not a field of the claim\)/);
  assert.deepEqual(t.says.at(-1)?.fields, { text: "Looked at it." });
});

test("N1 on #187: a listing without the template's hash, or without a package template's revision, signs nothing (cockpit >= v0.5.5)", async () => {
  const t = new FakeTransport();
  t.legacyListing = true;
  t.templateList = [...TEMPLATES, AREA_APPROVES];
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  const approves = { package: "area", name: "approves", template: "rec-approves", kind: "decision" as const, meaning: "approved" };
  assert.equal(await wb.signAct(approves, sha("2"), {}, "Approve", undefined, {}), "failed");
  assert.match(h.errors.at(-1) ?? "", /lists no hash for the template rec-approves: signing needs cockpit v0\.5\.5 or later/);
  assert.equal(t.says.length, 0);
  assert.equal(h.dialogs.length, 0, "no dialog either");
  // a hash but no revision for a package template
  t.legacyListing = false;
  t.templateList = [...TEMPLATES, AREA_APPROVES];
  t.templates = async () => ({ ok: true, value: t.templateList.map((x) => ({ ...x, hash: `sha256:${"e".repeat(64)}` })) });
  await wb.refresh();
  assert.equal(await wb.signAct(approves, sha("2"), {}, "Approve", undefined, {}), "failed");
  assert.match(h.errors.at(-1) ?? "", /lists no package revision for the template rec-approves: signing needs cockpit v0\.5\.5 or later/);
  assert.equal(t.says.length, 0);
});

test("N2 on #187: a meaning that is a value of two fields is refused, not taken as the act's", async () => {
  const t = new FakeTransport();
  const TWO: TemplateInfo = { name: "rec-two", package: "area", fields: { outcome: { type: "enum", values: ["approved", "rejected"], required: true }, status: { type: "enum", values: ["approved", "open"] } } };
  t.templateList = [...TEMPLATES, TWO];
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  const c = { package: "area", name: "approves", template: "rec-two", kind: "decision" as const, meaning: "approved" };
  assert.equal(await wb.signAct(c, sha("2"), {}, "Approve", undefined, {}), "failed");
  assert.match(h.errors.at(-1) ?? "", /means approved, a value of several fields of rec-two \(outcome, status\)/);
  assert.equal(t.says.length, 0);
});

test("proposed fields: a value of a field with fixed values is taken (the dialog shows it); a free value is only the input's default", async () => {
  const t = new FakeTransport();
  const SPOT: TemplateInfo = { name: "triage-spotcheck", package: "dryrun1.triage", fields: { verdict: { type: "enum", values: ["rule-judge-right", "second-model-right", "spec-unclear"], required: true }, proposal: { type: "string", required: true }, note: { type: "string" } } };
  const DEC: TemplateInfo = { name: "triage-decides", package: "dryrun1.triage", fields: { choice: { type: "string", required: true }, lookedAt: { type: "string" } } };
  t.templateList = [...TEMPLATES, SPOT, DEC];
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  const spot = { package: "dryrun1.triage", name: "spotcheck", template: "triage-spotcheck", kind: "decision" as const, meaning: "reviewed" };
  assert.equal(await wb.signAct(spot, iri("a"), { proposal: iri("1") }, "Verdict", undefined, { proposed: { verdict: "second-model-right" } }), "signed");
  assert.match(h.dialogs.at(-1)?.detail ?? "", /verdict = second-model-right/);
  assert.deepEqual(t.says.at(-1)?.fields, { proposal: iri("1"), verdict: "second-model-right" });
  // a proposed value outside the fixed values is asked
  h.picks = ["spec-unclear"];
  assert.equal(await wb.signAct(spot, iri("a"), { proposal: iri("1") }, "Verdict", undefined, { proposed: { verdict: "made-up" } }), "signed");
  assert.equal(t.says.at(-1)?.fields?.["verdict"], "spec-unclear");
  // a free field: the proposal is the input's default; what the person leaves is what is signed
  const inputs: Array<[string, string | undefined]> = [];
  (h as unknown as { input: (p: string, v?: string) => Promise<string | undefined> }).input = async (p: string, v?: string) => (inputs.push([p, v]), p.endsWith("choice") ? "a" : v);
  const dec = { package: "dryrun1.triage", name: "decides", template: "triage-decides", kind: "decision" as const };
  assert.equal(await wb.signAct(dec, iri("1"), {}, "Choose", undefined, { proposed: { lookedAt: `${iri("a")} ${iri("b")}` } }), "signed");
  assert.deepEqual(inputs, [["decides: choice", undefined], ["decides: lookedAt", `${iri("a")} ${iri("b")}`]]);
  assert.deepEqual(t.says.at(-1)?.fields, { choice: "a", lookedAt: `${iri("a")} ${iri("b")}` });
});

test("looked-at: the view session's evidence opens are offered for lookedAt and listed in the dialog; a template without lookedAt gets none", async () => {
  const t = new FakeTransport();
  const SPOT: TemplateInfo = { name: "triage-spotcheck", package: "dryrun1.triage", fields: { verdict: { type: "enum", values: ["rule-judge-right", "second-model-right", "spec-unclear"], required: true }, proposal: { type: "string", required: true } } };
  const DEC: TemplateInfo = { name: "triage-decides", package: "dryrun1.triage", fields: { choice: { type: "string", required: true }, lookedAt: { type: "string" } } };
  t.templateList = [...TEMPLATES, SPOT, DEC];
  const { h, wb } = await setup({}, t);
  h.confirmAnswer = true;
  const opened = ["kton-src:/improve/A.java?c0ffee0#L127", `run-artifact:p2/out/x.png@sha256:${"a".repeat(64)}`];
  (h as unknown as { input: (p: string, v?: string) => Promise<string | undefined> }).input = async (p: string, v?: string) => (p.endsWith("choice") ? "a" : v);
  const dec = { package: "dryrun1.triage", name: "decides", template: "triage-decides", kind: "decision" as const };
  assert.equal(await wb.signAct(dec, iri("1"), {}, "Choose", undefined, { scope: "dryrun1.triage", proposed: { lookedAt: iri("s") }, opened }), "signed");
  assert.equal(t.says.at(-1)?.fields?.["lookedAt"], `${iri("s")} ${opened.join(" ")}`);
  assert.equal(t.says.at(-1)?.scope, "dryrun1.triage");
  const detail = h.dialogs.at(-1)?.detail ?? "";
  assert.match(detail, /Into scope: dryrun1\.triage/);
  assert.match(detail, /Evidence you opened in this view \(2\):\n {2}kton-src:\/improve\/A\.java\?c0ffee0#L127 {2}\(in lookedAt\)\n {2}run-artifact:p2\/out\/x\.png@sha256:a{64} {2}\(in lookedAt\)/);
  // the person takes one out of lookedAt: the dialog says so
  (h as unknown as { input: (p: string, v?: string) => Promise<string | undefined> }).input = async (p: string) => (p.endsWith("choice") ? "a" : opened[0]);
  assert.equal(await wb.signAct(dec, iri("1"), {}, "Choose", undefined, { opened }), "signed");
  assert.match(h.dialogs.at(-1)?.detail ?? "", /x\.png@sha256:a{64} {2}\(not in lookedAt: you took it out\)/);
  // a spot check has no lookedAt: nothing is offered or listed
  const spot = { package: "dryrun1.triage", name: "spotcheck", template: "triage-spotcheck", kind: "decision" as const, meaning: "reviewed" };
  assert.equal(await wb.signAct(spot, iri("e"), { proposal: iri("1") }, "Verdict", undefined, { proposed: { verdict: "spec-unclear" }, opened }), "signed");
  assert.doesNotMatch(h.dialogs.at(-1)?.detail ?? "", /Evidence you opened/);
  assert.equal(t.says.at(-1)?.fields?.["lookedAt"], undefined);
});

test("open@1 evidence: a shown source or artifact is reported to the panel's looked-at list; a refused one is not", async () => {
  const { h, wb } = await setup();
  const shown: string[] = [];
  const answers = { source: "Opened improve:a/B.java#L127 at fa218aa8 (read-only)", artifact: "Opened p2/x.png (hash checked)" };
  (h as unknown as Record<string, unknown>)["openSource"] = async () => answers.source;
  (h as unknown as Record<string, unknown>)["openArtifact"] = async () => answers.artifact;
  const open = (wb.packages as unknown as { path: { open: (t: Record<string, unknown>, cb: (r: string) => void) => Promise<string> } }).path.open;
  const h64 = `sha256:${"b".repeat(64)}`;
  await open({ source: { repo: "improve", commit: "fa218aa8", path: "a/B.java", line: 127 } }, (r) => shown.push(r));
  await open({ artifact: { run: "p2", name: "x.png", sha256: h64, mediaType: "image/png" } }, (r) => shown.push(r));
  answers.source = "source not available on this host: improve has no a/C.java at fa218aa8";
  answers.artifact = "p2/y.png does not hash to sha256:…: refused, nothing shown";
  await open({ source: { repo: "improve", commit: "fa218aa8", path: "a/C.java" } }, (r) => shown.push(r));
  await open({ artifact: { run: "p2", name: "y.png", sha256: h64 } }, (r) => shown.push(r));
  assert.match(await open({ artifact: { run: "../p2", name: "y.png", sha256: h64 } }, (r) => shown.push(r)), /not opened: run is not a run name/);
  assert.deepEqual(shown, ["kton-src:/improve/a/B.java?fa218aa8#L127", `run-artifact:p2/x.png@${h64}`]);
});

// ---- MH's first screen in the dry-run workbench (20.11.)
const oneProposal = (t: FakeTransport) => {
  t.answers["Q proposals"] = (t.answers["Q proposals"] ?? []).filter((p) => p["msg"] === P1);
  return t;
};
const REVIEW_KEY = "kton-web-test/ui/test-shell.json";

test("MH 1: a review-view entry under Home → Packages opens its review view: one waiting proposal directly, several by a pick, none says where", async () => {
  // two waiting proposals (P1, and P2 which only an agent key decided): a pick
  const two = await setup({}, withTestPackage());
  const pk = (two.h.trees["home"] ?? []).find((n) => n.label === "Packages");
  const entry = pk?.children?.find((n) => n.label === "kton-web-test")?.children?.find((n) => n.description === "review-view");
  assert.deepEqual(entry?.command, { id: "ktonWorkbench.openEntry", args: [REVIEW_KEY] });
  two.h.picks.push("1. Which build?");
  await workbenchCommands(() => two.wb).find(([id]) => id === CMD.openEntry)?.[1]?.(REVIEW_KEY as never);
  assert.equal(two.h.compositions.length, 1, "the picked proposal's review view opened");
  // exactly one: no pick
  const one = await setup({}, oneProposal(withTestPackage()));
  await one.wb.openEntry(REVIEW_KEY);
  assert.equal(one.h.compositions.length, 1);
  assert.equal(one.h.picks.length, 0);
  // none waiting: it says where it opens from, opens nothing
  const none = withTestPackage();
  none.answers["Q proposals"] = [];
  const z = await setup({}, none);
  await z.wb.openEntry(REVIEW_KEY);
  assert.equal(z.h.compositions.length, 0);
  assert.match(z.h.infos.at(-1) ?? "", /opens from its proposal under Decisions waiting/);
});

test("MH 2: on start, exactly one waiting decision with a review view opens it (kton.home.openOnStart); off, or two, nothing opens", async () => {
  const on = await setup({ openOnStart: true }, oneProposal(withTestPackage()));
  assert.equal(on.h.compositions.length, 1, "the one decision's review view opened");
  assert.equal(on.h.compositions[0]?.key, `composition:${REVIEW_KEY}`);
  await on.wb.refresh();
  assert.equal(on.h.compositions.length, 1, "once per start, not per poll");
  const off = await setup({ openOnStart: false }, oneProposal(withTestPackage()));
  assert.equal(off.h.compositions.length, 0);
  const two = await setup({ openOnStart: true }, withTestPackage());
  assert.equal(two.h.compositions.length, 0, "two waiting decisions: nothing opens by itself");
});

test("MH 3: the decisions item says 1 decision waiting (singular) and is not part of the inbox count", async () => {
  const { h } = await setup({}, oneProposal(new FakeTransport()));
  assert.equal(h.status.get("decisions"), "1 decision waiting");
  assert.match(h.status.get("poll") ?? "", /· 1 open$/);
});

test("MH 4: guard rules (finding-source) on Home: passed (0 findings), findings (n), or not run, with the full rule name", async () => {
  const t = withTestPackage();
  const f = t.fixtures[0] as ReturnType<typeof listed>;
  const rule = (file: string, label: string, query: string) => ({ file, slot: "finding-source", label, query });
  t.fixtures = [{ ...f, pkg: { ...f.pkg, ui: [...f.pkg.ui, rule("ui/r1.json", "triage-reviewer-not-author: a review signed by a key that also signed the reviewed claim", "r-pass"), rule("ui/r2.json", "triage-ranked-before-assigned", "r-find"), rule("ui/r3.json", "triage-evidence-vs-reach", "r-error")] } }];
  // the named queries answer in the JSON-argument form (sparql.results), as the cockpit does
  const sparqlOf = (n: number) => ({ ok: true as const, value: { query: "sparql", ref: "", raw: "", sparql: { results: { head: { vars: ["rule", "focus"] }, results: { bindings: Array.from({ length: n }, (_x, i) => ({ rule: { type: "literal", value: "x" }, focus: { type: "literal", value: `f${i}` } })) } } } } });
  const ask = t.ask.bind(t);
  t.ask = async (req) => (req.name === "r-error" ? { ok: false, refusal: { code: "query.timeout", reason: "the query took over 30 s" } } : req.name === "r-pass" ? sparqlOf(0) : req.name === "r-find" ? sparqlOf(2) : ask(req));
  const { h } = await setup({}, t);
  const rules = (h.trees["home"] ?? []).find((n) => n.label === "Guard rules");
  const lines = (rules?.children ?? []).map((n) => `${n.label} | ${n.description}`);
  assert.deepEqual(lines, [
    "triage-reviewer-not-author: a review signed by a key that also signed the reviewed claim | passed (0 findings)",
    "triage-ranked-before-assigned | findings (2)",
    "triage-evidence-vs-reach | not run",
  ]);
  assert.match(rules?.children?.[2]?.tooltip ?? "", /Not run: .*took over 30 s/);
  const pk = (h.trees["home"] ?? []).find((n) => n.label === "Packages");
  assert.ok(!(pk?.children ?? []).some((n) => /not rendered/.test(n.description ?? "") && /triage/.test(n.label)), "no rule is listed as a cut-off 'not rendered'");
});

test("MH 5 + 6: the identity item's tooltip says why the keys are not verified; the recorder row is short with a tooltip", async () => {
  const h = new FakeHost();
  const tips = new Map<string, string | undefined>();
  h.setStatus = (id, text, tip) => { h.status.set(id, text); tips.set(id, tip); };
  const wb = new Workbench(new FakeTransport(), h, queries, identity, { ...DEFAULT_CONFIG, surfaces: SURFACES }, new Scheduler(manualClock));
  await wb.start();
  await wb.refresh();
  assert.match(tips.get("identity") ?? "", /^Test keys; this cockpit has no identity verb, so the name isn't verified\./);
  const rec = (h.trees["home"] ?? []).find((n) => n.id === "recorder");
  assert.equal(rec?.label, "Recorder");
  assert.match(rec?.tooltip ?? "", /Recorded runs/);
});

test("inbox areas (dry run 1 mirror): kton.inbox.areas shows only those areas' messages and proposals; a relayed copy names its lane as the sender, its text without the prefix", async () => {
  const t = new FakeTransport();
  t.answers["Q proposals"] = [
    { msg: P1, area: "dryrun1-testdev", title: "Rerun with capture?", text: "[from dryrun1-testdev-review · 2026-11-20T17:01:00Z] Should ics1829 run again?", options: OPTIONS, from: "dryrun1-mirror-claims", fromTier: "mirror", when: "2026-11-20T17:05:00Z" },
    { msg: P2, area: "dryrun1-spec-review", title: "Not mine", text: "t", options: OPTIONS, from: "dryrun1-mirror-claims", fromTier: "mirror", when: "2026-11-20T17:06:00Z" },
  ];
  t.answers["N decides"] = [];
  t.answers["Q messages"] = [
    { msg: M1, kind: "https://example.org/v/requests", area: "dryrun1-testdev", title: "Classify the 42", text: "[from dryrun1-personas · 2026-11-20T16:00:00Z] Please classify today's 42 failures.", from: "dryrun1-mirror-claims", fromTier: "mirror", when: "2026-11-20T16:02:00Z" },
    { msg: M2, kind: "https://example.org/v/requests", area: "qa", title: "Another area", text: "t", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T08:00:00Z" },
  ];
  t.answers["N answers"] = [];
  const { h, wb } = await setup({ inboxAreas: ["dryrun1-testdev"] }, t);
  const inbox = h.trees["inbox"] ?? [];
  assert.deepEqual(inbox.map((n) => n.label), ["Classify the 42"], "only the persona's area");
  assert.equal(inbox[0]?.description, "requests · dryrun1-testdev · dryrun1-personas (via dryrun1-mirror-claims (mirror))", "the lane is the sender; the mirror that relayed it stays named");
  assert.equal(h.status.get("decisions"), "1 decision waiting", "a propose of the area is a decision waiting; another area's is not");
  assert.deepEqual(wb.openDecisions().map((d) => [d["text"], d["from"], d["when"]]).filter(([, , w]) => w === "2026-11-20T17:01:00Z"), [["Should ics1829 run again?", "dryrun1-testdev-review", "2026-11-20T17:01:00Z"]], "the prefix is not text; the original time is the time");
  const { relayed } = await import("../src/ui/controller.js");
  assert.deepEqual(relayed({ text: "no prefix", from: "a", fromTier: "self" }), { text: "no prefix", from: "a", fromTier: "self" }, "anything else as it is");
});
