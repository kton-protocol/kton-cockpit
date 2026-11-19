// The VS Code layer's behaviour, without VS Code: the controller against a fake transport and host.
// Test names start with the flow step of the shell flows (S1–S10) they stand for, where there is one.
import { test } from "node:test";
import assert from "node:assert/strict";
import { Workbench, DEFAULT_CONFIG, isLocked } from "../src/ui/controller.js";
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
import { Scheduler, type Clock } from "../src/poll.js";

const HEX = (c: string) => c.repeat(64);
const iri = (c: string) => `https://kton.dev/o/${HEX(c)}`;
const sha = (c: string) => `sha256:${HEX(c)}`;
const P1 = iri("1");
const P2 = iri("6");
const M1 = iri("2");
const M2 = iri("3");
const OPTIONS = JSON.stringify([{ id: "a", label: "Build 39", consequence: "the tested build" }, { id: "b", label: "Build 41", consequence: "newer, untested" }]);

const queries = new Map([
  ["proposals", "Q proposals"],
  ["decides", "Q decides"],
  ["messages", "Q messages"],
  ["answers", "Q answers"],
  ["lanes", "Q lanes"],
]);

const TEMPLATES: TemplateInfo[] = [
  { name: "decides", fields: { answers: { type: "ref", required: true }, outcome: { type: "enum", required: true, values: ["https://schema.org/AcceptAction", "https://schema.org/RejectAction"] }, choice: { type: "string" } } },
  { name: "done", fields: { answers: { type: "ref" }, outcome: { type: "string", required: true } } },
  { name: "requests", fields: { area: { type: "string", required: true }, title: { type: "string", required: true }, text: { type: "string", required: true }, why: { type: "string" } } },
  { name: "announces", fields: { area: { type: "string", required: true }, title: { type: "string", required: true }, text: { type: "string", required: true }, why: { type: "string", required: true } } },
];

const identity: WindowIdentity = {
  hintName: "qa-decider",
  person: { role: "person", keyName: "qa-decider", keyId: "qa-decider" },
  agent: { role: "agent", keyName: "qa-decider-agent", keyId: "qa-decider-agent" },
};

class FakeTransport implements Transport {
  says: SayRequest[] = [];
  templateList: TemplateInfo[] = TEMPLATES;
  sayError: Error | undefined;
  sayErrors: Error[] = [];
  askError: Error | undefined;
  answers: Record<string, Array<Record<string, string>>> = {
    "Q proposals": [
      { msg: P1, area: "qa", title: "Which build?", text: "Two candidates", why: "seed", options: OPTIONS, recommended: "a", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T10:00:00Z" },
      { msg: P2, area: "qa", title: "Agent decided this one", text: "t", why: "seed", options: OPTIONS, from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T10:30:00Z" },
    ],
    // an agent key's decides about P2: a record, never a decision (review check 7, S4.5)
    "Q decides": [{ d: iri("8"), answers: P2, outcome: "https://schema.org/AcceptAction", choice: "a", by: "qa-decider-agent-claims", byTier: "agent", when: "2026-11-18T11:00:00Z" }],
    "Q messages": [
      { msg: M1, kind: "https://example.org/v/requests", area: "qa", title: "Check the notes", text: "t", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T09:00:00Z" },
      { msg: M2, kind: "https://example.org/v/announces", area: "ops", title: "Answered one", text: "t", from: "qa-requestor-claims", fromTier: "self", when: "2026-11-18T08:00:00Z" },
    ],
    "Q answers": [{ d: iri("4"), answers: M2, outcome: "done", by: "qa-decider-claims", byTier: "self" }],
    "Q lanes": [{ lane: "active/qa-lane", areas: "qa" }, { lane: "active/ops-lane", areas: "ops,infra" }],
  };
  async ask(req: AskRequest): Promise<Outcome<AskResult>> {
    if (this.askError) throw this.askError;
    return { ok: true, value: { query: "sparql", ref: "", raw: "", sparql: { rows: this.answers[req.sparql ?? ""] ?? [] } } };
  }
  async say(req: SayRequest): Promise<Outcome<SayResult>> {
    const e = this.sayErrors.shift() ?? this.sayError;
    if (e) throw e;
    this.says.push(req);
    return { ok: true, value: { claimId: sha("9"), confirmation: "registered" } };
  }
  async templates(): Promise<Outcome<TemplateInfo[]>> {
    return { ok: true, value: this.templateList };
  }
  publish(): never { throw new Error("not used"); }
  workflowRun(): never { throw new Error("not used"); }
  install(): never { throw new Error("not used"); }
  list(): never { throw new Error("not used"); }
  doctor(): never { throw new Error("not used"); }
  version(): never { throw new Error("not used"); }
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
  async signDialog(title: string, detail: string): Promise<boolean> { this.dialogs.push({ title, detail }); return this.confirmAnswer; }
  signOutcome(ok: boolean, text: string): void { this.outcomes.push({ ok, text }); }
  async openWindow(folder: string): Promise<void> { this.windows.push(folder); }
  async runExternal(ext: string, cmd: string): Promise<boolean> { if (!this.installed.has(ext)) return false; this.external.push(cmd); return true; }
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

test("S1.1: status bar shows both keys, unverified until the cockpit has an identity verb, and test mode", async () => {
  const { h } = await setup();
  const s = h.status.get("identity") ?? "";
  assert.match(s, /person key: qa-decider · qa-decider \(unverified/);
  assert.match(s, /agent key: qa-decider · qa-decider-agent \(unverified/);
  assert.ok(!/qa-requestor/.test(s), "no name comes from a claim's signer");
  assert.equal(h.status.get("mode"), "TEST MODE");
  assert.match(h.status.get("poll") ?? "", /polling: inbox every 30s · 1 open · 2 decisions/);
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
  assert.match(noPerson.h.status.get("identity") ?? "", /person key: missing · agent key: qa-decider/);
  noPerson.h.confirmAnswer = true;
  assert.equal(await noPerson.wb.chooseOption(P1, "a"), "failed");
  assert.match(noPerson.h.errors.at(-1) ?? "", /no person key.*agent key never stands in/);
  assert.equal(noPerson.t.says.length, 0);

  const noAgent = await setup({}, new FakeTransport(), { ...identity, agent: { role: "agent", keyName: "", keyId: "" } });
  assert.match(noAgent.h.status.get("identity") ?? "", /person key: qa-decider · qa-decider \(unverified.*agent key: missing/);
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
  assert.match(term?.tooltip ?? "", /workgroup extension folds in/);
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
  ["S7.6 malformed JSON", "garbage", /Not signed: say: the answer is not JSON/],
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
  assert.match(h.outcomes.at(-1)?.text ?? "", /Not signed: say timed out after 4 s\. Whether a claim was made is for the registry to say/);
  assert.ok(!h.infos.some((i) => i.startsWith("Signed")));
});

test("S7.4: a locked registry is retried, shown in the status bar, and completes once released", async () => {
  const { t, h, wb } = await setup();
  const locked = new TransportError("the registry is locked by another process", 1, "nekton: store is locked\n");
  t.sayErrors = [locked, locked];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "signed");
  assert.deepEqual(h.statusHistory.filter((s) => s.startsWith("registry locked")), ["registry locked: retrying say (1/2)", "registry locked: retrying say (2/2)"]);
  assert.equal(t.says.length, 1);
  assert.ok(isLocked(locked));
  assert.ok(!isLocked(new TransportError("git commit failed", 1, "")));
});

test("S7.4: a lock that is not released gives up with a clear message and signs nothing", async () => {
  const { t, h, wb } = await setup();
  const locked = new TransportError("locked", 1, "store is locked");
  t.sayErrors = [locked, locked, locked];
  h.confirmAnswer = true;
  assert.equal(await wb.onDecisionIntent({ msg: P1 }, decisionIntent("b")), "failed");
  assert.match(h.errors.at(-1) ?? "", /Not signed: locked/);
  assert.equal(t.says.length, 0);
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
  t.answers["Q decides"] = [...(t.answers["Q decides"] ?? []), { d: iri("9"), answers: P2, outcome: "https://schema.org/AcceptAction", by: "qa-decider-claims", byTier: "self" }];
  await wb.refresh();
  assert.deepEqual(wb.openDecisions().map((d) => d["msg"]), [P1]);
  // and so does one by a signer of a configured person tier; an agent tier never does
  t.answers["Q decides"] = [{ d: iri("9"), answers: P1, outcome: "x", by: "someone-claims", byTier: "persons" }];
  const { wb: wb2 } = await setup({ personTiers: ["persons"] }, t);
  assert.deepEqual(wb2.openDecisions().map((d) => d["msg"]), [P2]);
});
