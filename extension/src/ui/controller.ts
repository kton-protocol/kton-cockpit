// The workbench controller (PR 1: Home, Inbox and thread, Decision, Lanes, the sign dialog; C1).
// It wires the core (transport, identity, sign, intents, poll) to a UiHost. No VS Code here.

import { randomBytes } from "node:crypto";
import type { TemplateInfo, Transport } from "../transport/port.js";
import { TransportError, TransportTimeout } from "../transport/port.js";
import type { WindowIdentity, ShownSigner } from "../identity.js";
import { hasKey, showSigner, statusText } from "../identity.js";
import type { ControlInfo } from "../sign.js";
import { DialogConsent, plan, sign, FOOTER } from "../sign.js";
import type { UiEntry, SlotContext } from "../intents.js";
import { checkIntent } from "../intents.js";
import { Scheduler } from "../poll.js";
import type { TreeNode, UiHost } from "./host.js";
import type { Queries, Row } from "./queries.js";
import { idOf, kindOf, rowsOf } from "./queries.js";
import { composePage, decisionPage, parseOptions, resultsPage, signDialogDetail, signerLabel, threadPage } from "./pages.js";
import type { Surface } from "./surfaces.js";
import { ALL, areasByLane, surfaceAreas, surfaceFolder } from "./surfaces.js";

export interface WorkbenchConfig {
  /** Query names (files in the query folder) feeding the views. */
  readonly queries: {
    /** Proposals: msg, area, title, text, why, options, recommended, from, fromTier, when. */
    readonly proposals: string;
    /** Claims of the decision template: d, answers, outcome, choice, by, byTier, when. */
    readonly decides: string;
    /** Messages: msg, kind, area, title, text, why, from, fromTier, when. */
    readonly messages: string;
    /** Closing claims: d, answers, outcome, text, by, byTier, when. */
    readonly answers: string;
    /** Lanes and their areas: lane, areas. */
    readonly lanes: string;
  };
  /** The control a decision is signed with (O1: kind decision → person key, dialog). */
  readonly decideControl: ControlInfo;
  /** The control a message is answered with (S2: a person-key decision through the dialog). */
  readonly answerControl: ControlInfo;
  /** Templates that are decisions when signed from the command palette; all others are records. */
  readonly decisionTemplates: readonly string[];
  /**
   * Trust tiers whose signers are persons (review check 7): a decision counts only when it is signed by
   * this window's person key or by a signer of one of these tiers. Any other signer's claim is a record.
   */
  readonly personTiers: readonly string[];
  readonly pollSeconds: number;
  readonly surfaces: readonly Surface[];
  /** Folder of a surface without its own `folder`, with ${surface}. */
  readonly surfaceFolderPattern?: string;
  readonly initialSurface?: string;
  /** The cockpit executable's name, shown in the polling status (which cockpit the window reads). */
  readonly cockpitLabel?: string;
  /** The recorder stays its own extension; the shell hands over to it. */
  readonly recorder: { readonly extensionId: string; readonly command: string };
  /** A locked registry (S7.4): how often and how far apart a `say` is retried. */
  readonly lockRetry: { readonly attempts: number; readonly delayMs: number };
}

export const DEFAULT_CONFIG: WorkbenchConfig = {
  queries: { proposals: "proposals", decides: "decides", messages: "messages", answers: "answers", lanes: "lanes" },
  decideControl: { package: "workbench", name: "decide", template: "decides", kind: "decision" },
  answerControl: { package: "workbench", name: "answer", template: "done", kind: "decision" },
  decisionTemplates: ["decides"],
  personTiers: [],
  pollSeconds: 30,
  surfaces: [],
  recorder: { extensionId: "kton.kton-recorder", command: "kton.openObservations" },
  lockRetry: { attempts: 10, delayMs: 1000 },
};

/** The decision panel's slot entry: it may only emit sign intents, signed with the decide control. */
const DECISION_ENTRY: UiEntry = { package: "workbench", name: "decision", slot: "review-view", control: "decide", intents: ["sign"] };
/** The thread panel's entry: it may only emit the answer (sign) intent about its own message. */
const THREAD_ENTRY: UiEntry = { package: "workbench", name: "thread", slot: "thread", control: "answer", intents: ["sign"] };

export const CMD = {
  refresh: "ktonWorkbench.refresh",
  openDecision: "ktonWorkbench.openDecision",
  chooseOption: "ktonWorkbench.chooseOption",
  openThread: "ktonWorkbench.openThread",
  focusInbox: "ktonWorkbench.inbox.focus",
  switchSurface: "ktonWorkbench.switchSurface",
  openSurfaceWindow: "ktonWorkbench.openSurfaceWindow",
  openRecorder: "ktonWorkbench.openRecorder",
  ask: "ktonWorkbench.ask",
  say: "ktonWorkbench.say",
  compose: "ktonWorkbench.compose",
} as const;

const SHA = /^sha256:[0-9a-f]{64}$/;
type SignResult = "signed" | "cancelled" | "failed";

export class Workbench {
  private proposals: Row[] = [];
  private decides: Row[] = [];
  private messages: Row[] = [];
  private answers: Row[] = [];
  private lanes: Row[] = [];
  private templates: TemplateInfo[] = [];
  private surface: string;
  private lastError: string | undefined;
  /** When the views last had good data; set and the last poll failed: the data is stale (S7.2). */
  private goodAt: Date | undefined;
  private stale = false;
  private readonly drafts = new Map<string, Record<string, string>>();
  /** Panels left open by the workbench this one replaced: rendered again after the first good poll. */
  private reopen: readonly string[] = [];
  /** Rendering again in place (after a rebuild): do not move the focus to each panel. */
  private quiet = false;
  readonly scheduler: Scheduler;

  constructor(
    private readonly t: Transport,
    private readonly host: UiHost,
    private readonly queries: Queries,
    private readonly identity: WindowIdentity,
    private readonly cfg: WorkbenchConfig = DEFAULT_CONFIG,
    scheduler?: Scheduler,
  ) {
    this.scheduler = scheduler ?? new Scheduler();
    this.surface = cfg.surfaces.some((s) => s.name === cfg.initialSurface) ? (cfg.initialSurface as string) : ALL;
  }

  /**
   * openPanels: the keys of panels a replaced workbench left open (a changed setting rebuilds it). They
   * are rendered again with this window's identity and data after the first good poll, so a panel never
   * keeps showing what the old identity could or could not do.
   */
  async start(openPanels: readonly string[] = []): Promise<void> {
    this.reopen = openPanels;
    await this.showIdentity();
    this.host.setStatus("mode", "TEST MODE", FOOTER);
    this.showSurface();
    this.scheduler.add("inbox", this.cfg.pollSeconds * 1000, () => this.refresh());
  }

  stop(): void {
    this.scheduler.stop();
  }

  get currentSurface(): string {
    return this.surface;
  }

  /** S1: both keys, each unverified (no identity verb yet) or named as missing; no identity file: read only. */
  private async showIdentity(): Promise<void> {
    if (!this.identity.hintName) {
      this.host.setStatus("identity", "no identity file: reading only", "This checkout has no .kton/local/identity.json: reading works, signing is disabled. Another identity means another window.");
      return;
    }
    const p = hasKey(this.identity.person) ? await this.signer("person") : "missing";
    const a = hasKey(this.identity.agent) ? await this.signer("agent") : "missing";
    this.host.setStatus(
      "identity",
      statusText(p, a),
      "One person per window; another identity means another window. Person key: decisions, always through the sign dialog. Agent key: records, no dialog. A missing key is never replaced by the other.",
    );
  }

  private signer(role: "person" | "agent"): Promise<ShownSigner> {
    const k = role === "person" ? this.identity.person : this.identity.agent;
    return showSigner(this.t, k.keyId, `${this.identity.hintName} · ${k.keyName}`);
  }

  /** Why this window cannot sign a claim of this kind, or undefined if it can (S1.5, S1.7). */
  cannotSign(kind: "decision" | "record"): string | undefined {
    if (!this.identity.hintName) return "This checkout has no identity file (.kton/local/identity.json): reading works, signing is disabled.";
    const k = kind === "decision" ? this.identity.person : this.identity.agent;
    if (hasKey(k)) return undefined;
    return kind === "decision"
      ? "This window has no person key (setting kton.identity.personKey): decisions are disabled. The agent key never stands in for it."
      : "This window has no agent key (setting kton.identity.agentKey): record writes are disabled. The person key never stands in for it.";
  }

  private async rows(name: string): Promise<Row[]> {
    const sparql = this.queries.get(name);
    if (sparql === undefined) throw new Error(`query ${name} is not in the query folder (setting kton.queries.dir)`);
    this.host.log(`ask sparql ${name}`);
    const out = await this.t.ask({ query: "sparql", sparql });
    if (!out.ok) throw new Error(`the cockpit refused ${name}: ${out.refusal.reason}`);
    return rowsOf(out.value);
  }

  /** One poll: templates, proposals, decision claims, messages, answers and lanes; then the views. */
  async refresh(): Promise<void> {
    try {
      const tpl = await this.t.templates();
      if (!tpl.ok) throw new Error(`the cockpit refused its template listing: ${tpl.refusal.reason}`);
      const [proposals, decides, messages, answers, lanes] = await Promise.all([
        this.rows(this.cfg.queries.proposals),
        this.rows(this.cfg.queries.decides),
        this.rows(this.cfg.queries.messages),
        this.rows(this.cfg.queries.answers),
        this.queries.has(this.cfg.queries.lanes) ? this.rows(this.cfg.queries.lanes) : Promise.resolve([]),
      ]);
      Object.assign(this, { templates: tpl.value, proposals, decides, messages, answers, lanes });
    } catch (e) {
      const msg = describe(e);
      this.stale = this.goodAt !== undefined;
      this.host.setStatus(
        "poll",
        `polling: error${this.stale ? ` · stale since ${time(this.goodAt)}` : " · no data"}${this.cfg.cockpitLabel ? ` · ${this.cfg.cockpitLabel}` : ""}`,
        msg,
      );
      // One notification per distinct error, not one per poll.
      if (msg !== this.lastError) this.host.error(`The workbench cannot read the cockpit: ${msg}`);
      this.lastError = msg;
      this.render();
      return;
    }
    this.lastError = undefined;
    this.stale = false;
    this.goodAt = new Date();
    this.render();
    if (this.reopen.length) {
      const keys = this.reopen;
      this.reopen = [];
      this.renderPanels(keys);
    }
  }

  /** Render open thread, decision and compose panels again, in place. */
  renderPanels(keys: readonly string[]): void {
    this.quiet = true;
    try {
      for (const key of keys) {
        const i = key.indexOf(":");
        const kind = key.slice(0, i);
        const id = key.slice(i + 1);
        if (kind === "decision" && this.openDecisions().some((d) => d["msg"] === id)) this.openDecision(id);
        else if (kind === "thread" && this.messages.some((m) => m["msg"] === id)) this.openThread(id);
        else if (kind === "compose") {
          const tpl = this.templates.find((t) => t.name === id);
          if (tpl) this.showCompose(tpl, {});
        }
      }
    } finally {
      this.quiet = false;
    }
  }

  /** Is this claim's signer a person (review check 7)? This window's person key, or a person tier. */
  private byPerson(r: Row): boolean {
    const by = r["by"] ?? "";
    const pk = this.identity.person.keyName;
    if (pk && (by === pk || by === `${pk}-claims`)) return true;
    return !!r["byTier"] && this.cfg.personTiers.includes(r["byTier"]);
  }

  /** Open decisions: proposals no person has decided. Claims of the decision template by others are records. */
  openDecisions(): Row[] {
    const decided = new Set(this.decides.filter((d) => this.byPerson(d)).map((d) => d["answers"]));
    return this.proposals.filter((p) => !decided.has(p["msg"]));
  }

  private records(msg: string): Row[] {
    return this.decides.filter((d) => d["answers"] === msg && !this.byPerson(d));
  }

  private render(): void {
    if (this.goodAt === undefined && this.lastError !== undefined) {
      // S7.1: no data is not "nothing open".
      const none: TreeNode = { id: "nodata", label: "No data: the cockpit cannot be read", tooltip: this.lastError };
      this.host.setTree("home", [none]);
      this.host.setTree("inbox", [{ ...none, id: "nodata-inbox" }]);
      this.host.setTree("lanes", [{ ...none, id: "nodata-lanes" }]);
      return;
    }
    const answered = new Set(this.answers.map((a) => a["answers"]).filter((x): x is string => !!x));
    const inSurface = this.areaFilter();
    const open = this.messages.filter((m) => !answered.has(m["msg"] ?? "") && inSurface(m["area"]));
    const decisions = this.openDecisions().filter((d) => inSurface(d["area"]));
    this.host.setTree("home", this.homeNodes(open, decisions));
    this.host.setTree("inbox", this.inboxNodes(open));
    this.host.setTree("lanes", this.laneNodes(open));
    if (!this.stale) {
      this.host.setStatus(
        "poll",
        `polling: inbox every ${this.cfg.pollSeconds}s · ${open.length} open · ${decisions.length} decisions${this.cfg.cockpitLabel ? ` · ${this.cfg.cockpitLabel}` : ""}`,
        "Polled by code, never by an LLM. One scheduler.",
      );
    }
  }

  private areaFilter(): (area: string | undefined) => boolean {
    const s = this.cfg.surfaces.find((x) => x.name === this.surface);
    const areas = surfaceAreas(s, areasByLane(this.lanes));
    return (a) => areas === undefined || (a !== undefined && areas.has(a));
  }

  private showSurface(): void {
    this.host.setStatus("surface", `surface: ${this.surface}`, "Switch the surface the views show (same window). A surface's session opens in its own window.", CMD.switchSurface);
  }

  /** The view-level switch: same window, other filter. */
  async switchSurface(name?: string): Promise<void> {
    const choice = name ?? (await this.host.pick("Show which surface?", [ALL, ...this.cfg.surfaces.map((s) => s.name)]));
    if (!choice || (choice !== ALL && !this.cfg.surfaces.some((s) => s.name === choice))) return;
    this.surface = choice;
    this.showSurface();
    this.render();
  }

  /** A surface's session runs in a window on its folder. */
  async openSurfaceWindow(name?: string): Promise<boolean> {
    const choice = name ?? (await this.host.pick("Open which surface's session?", this.cfg.surfaces.map((s) => s.name)));
    const s = this.cfg.surfaces.find((x) => x.name === choice);
    if (!s) return false;
    const folder = surfaceFolder(s, this.cfg.surfaceFolderPattern);
    if (!folder) {
      this.host.error(`No folder is known for surface ${s.name}: set "folder" in the surfaces file or kton.surfaces.folderPattern.`);
      return false;
    }
    await this.host.openWindow(folder);
    return true;
  }

  /** Recorder handover: it stays its own extension. */
  async openRecorder(): Promise<boolean> {
    const ok = await this.host.runExternal(this.cfg.recorder.extensionId, this.cfg.recorder.command);
    if (!ok) this.host.info("The kton recorder extension is not installed. Recorded runs are shown there; it stays its own extension.");
    return ok;
  }

  private homeNodes(open: readonly Row[], decisions: readonly Row[]): TreeNode[] {
    const areas = new Map<string, number>();
    for (const m of open) areas.set(m["area"] ?? "?", (areas.get(m["area"] ?? "?") ?? 0) + 1);
    const cannot = this.cannotSign("decision");
    const nodes: TreeNode[] = [
      {
        id: "decisions",
        label: "Decisions waiting",
        description: String(decisions.length),
        children: decisions.map((d) => ({
          id: `d:${d["msg"]}`,
          label: d["title"] ?? "(no title)",
          description: d["area"] ?? "",
          command: { id: CMD.openDecision, args: [d["msg"]] },
          // S3.7: a/b on the Home card open the same sign dialog (never one click)
          children: parseOptions(d["options"]).map((o) => ({
            id: `d:${d["msg"]}:${o.id}`,
            label: `${o.id} · ${o.label}`,
            description: [o.id === d["recommended"] ? "recommended" : "", cannot ? "signing disabled" : ""].filter(Boolean).join(" · "),
            ...(cannot ? { disabled: true, tooltip: cannot } : { tooltip: "Opens the sign dialog", command: { id: CMD.chooseOption, args: [d["msg"], o.id] } }),
          })),
        })),
      },
      { id: "inbox", label: "Inbox", description: `${open.length} open`, command: { id: CMD.focusInbox, args: [] } },
      {
        id: "areas",
        label: "Open by area",
        description: String(areas.size),
        children: [...areas.entries()].sort().map(([a, n]) => ({ id: `a:${a}`, label: a, description: String(n) })),
      },
      { id: "recorder", label: "Recorded runs (recorder)", tooltip: "Opens the kton recorder extension", command: { id: CMD.openRecorder, args: [] } },
    ];
    if (this.stale) nodes.unshift({ id: "stale", label: "Stale: the last poll failed", description: `data from ${time(this.goodAt)}`, tooltip: this.lastError ?? "" });
    return nodes;
  }

  private inboxNodes(open: readonly Row[]): TreeNode[] {
    return [...open]
      .sort((a, b) => (b["when"] ?? "").localeCompare(a["when"] ?? ""))
      .map((m) => ({
        id: `m:${m["msg"]}`,
        label: m["title"] ?? "(no title)",
        description: `${kindOf(m["kind"] ?? "")} · ${m["area"] ?? ""} · ${signerLabel(m["from"], m["fromTier"])}${this.stale ? " · stale" : ""}`,
        command: { id: CMD.openThread, args: [m["msg"]] },
      }));
  }

  /** The lanes of the current surface, with the open messages of their areas. */
  private laneNodes(open: readonly Row[]): TreeNode[] {
    const s = this.cfg.surfaces.find((x) => x.name === this.surface);
    const byLane = areasByLane(this.lanes);
    const lanes = [...byLane.keys()].filter((l) => !s || s.lanes.includes(l)).sort();
    return lanes.map((l) => {
      const areas = byLane.get(l) ?? [];
      const n = open.filter((m) => areas.includes(m["area"] ?? "")).length;
      return {
        id: `l:${l}`,
        label: l.split("/").pop() ?? l,
        description: `${n} open · ${areas.join(", ")}`,
        tooltip: l,
        children: [
          { id: `l:${l}:terminal`, label: "Open in terminal", description: "not yet available", disabled: true, tooltip: "Comes when the workgroup extension folds in; use the workgroup extension until then." },
        ],
      };
    });
  }

  openThread(msg: string, view: { draft?: string; error?: string } = {}): void {
    const row = this.messages.find((m) => m["msg"] === msg);
    if (!row) return this.host.error("That message is no longer in the feed.");
    const ans = this.answers.filter((a) => a["answers"] === msg);
    const cannot = this.cannotSign(this.cfg.answerControl.kind);
    this.host.showPanel(
      `thread:${msg}`,
      row["title"] ?? "Message",
      threadPage(row, ans, nonce(), { answerable: ans.length === 0, ...(cannot ? { cannotSign: cannot } : {}), ...view }),
      ans.length === 0 ? (m) => void this.onThreadIntent(row, m) : undefined,
      !this.quiet,
    );
  }

  /** S2.3: the answer from the thread page, untrusted: checked, planned, shown in the dialog, then signed. */
  async onThreadIntent(row: Row, raw: unknown): Promise<SignResult> {
    const id = idOf(row["msg"] ?? "");
    const ctx: SlotContext = { subject: { id, kind: "claim" }, identityName: this.identity.hintName };
    const intent = checkIntent(raw, THREAD_ENTRY, ctx);
    if (!intent.ok || intent.value.kind !== "sign") {
      this.host.log(`refused intent: ${intent.ok ? "not a sign intent" : `${intent.refusal.reason}: ${intent.refusal.detail}`}`);
      return "failed";
    }
    const outcome = intent.value.fields["outcome"] ?? "";
    if (!outcome.trim()) {
      this.openThread(row["msg"] ?? "", { error: "outcome is required" });
      return "failed";
    }
    const r = await this.signThroughDialog(intent.value, this.cfg.answerControl, `Answer: ${row["title"] ?? id}`);
    if (r === "cancelled") this.openThread(row["msg"] ?? "", { draft: outcome });
    return r;
  }

  openDecision(msg: string): void {
    const row = this.openDecisions().find((d) => d["msg"] === msg);
    if (!row) return this.host.error("That decision is no longer open.");
    const cannot = this.cannotSign("decision");
    this.host.showPanel(
      `decision:${msg}`,
      row["title"] ?? "Decision",
      decisionPage(row, nonce(), { records: this.records(msg), ...(cannot ? { cannotSign: cannot } : {}) }),
      (m) => void this.onDecisionIntent(row, m),
      !this.quiet,
    );
  }

  /** S3.7: an option on the Home card: the same checked intent and the same dialog as the Decision panel. */
  async chooseOption(msg: string, option: string): Promise<SignResult> {
    const row = this.openDecisions().find((d) => d["msg"] === msg);
    if (!row) {
      this.host.error("That decision is no longer open.");
      return "failed";
    }
    const id = idOf(msg);
    return this.onDecisionIntent(row, { kind: "sign", about: id, fields: { answers: id, outcome: "https://schema.org/AcceptAction", choice: option } });
  }

  /** An intent from the decision page or the Home card: untrusted. Checked, planned, shown in the dialog, then signed. */
  async onDecisionIntent(row: Row, raw: unknown): Promise<SignResult> {
    const id = idOf(row["msg"] ?? "");
    const ctx: SlotContext = { subject: { id, kind: "claim" }, identityName: this.identity.hintName };
    const intent = checkIntent(raw, DECISION_ENTRY, ctx);
    if (!intent.ok) {
      this.host.log(`refused intent: ${intent.refusal.reason}: ${intent.refusal.detail}`);
      return "failed";
    }
    if (intent.value.kind !== "sign") return "failed";
    return this.signThroughDialog(intent.value, this.cfg.decideControl, `Sign decision: ${row["title"] ?? id}`);
  }

  /**
   * Plan, dialog (for the person key), sign. Never throws. A locked registry is retried and shown in the
   * status bar (S7.4); a timeout is reported as failed, and the registry decides whether a claim exists (S7.3).
   */
  private async signThroughDialog(intent: { kind: "sign"; about: string; fields: Readonly<Record<string, string>> }, control: ControlInfo, title: string): Promise<SignResult> {
    const cannot = this.cannotSign(control.kind);
    if (cannot) {
      this.host.error(`Not signed: ${cannot}`);
      return "failed";
    }
    const p = plan(intent, control, this.templates, this.identity);
    if (!p.ok) {
      this.host.error(`Not signed: ${p.refusal.reason}`);
      return "failed";
    }
    let consent: DialogConsent | undefined;
    const viaDialog = p.value.keyRole === "person" || p.value.needsDialog;
    if (viaDialog) {
      const who = await this.signer(p.value.keyRole);
      const yes = await this.host.signDialog(title, signDialogDetail(p.value, who), `${this.identity.hintName} (${this.identity.person.keyName})`);
      if (!yes) {
        this.host.log(`not signed: the dialog was closed (${p.value.template} about ${p.value.subject})`);
        this.host.info("Not signed.");
        return "cancelled";
      }
      consent = DialogConsent.fromDialog(p.value);
    }
    const fail = (text: string): SignResult => {
      if (viaDialog) this.host.signOutcome(false, text);
      this.host.error(text);
      return "failed";
    };
    for (let attempt = 1; ; attempt++) {
      try {
        const out = await sign(this.t, p.value, consent);
        if (!out.ok) return fail(`Not signed: the cockpit refused: ${out.refusal.reason}`);
        if (viaDialog) this.host.signOutcome(true, `Signed ${out.value.id}`);
        this.host.info(`Signed ${out.value.id}`);
        break;
      } catch (e) {
        if (e instanceof TransportTimeout) {
          return fail(`Not signed: ${e.message}. Whether a claim was made is for the registry to say: check it before signing again.`);
        }
        if (e instanceof TransportError && isLocked(e) && attempt < this.cfg.lockRetry.attempts) {
          this.host.setStatus("poll", `registry locked: retrying say (${attempt}/${this.cfg.lockRetry.attempts - 1})`, firstLine(e.stderr) || e.message);
          this.host.log(`registry locked, retry ${attempt}: ${firstLine(e.stderr) || e.message}`);
          await sleep(this.cfg.lockRetry.delayMs);
          continue;
        }
        return fail(`Not signed: ${describe(e)}`);
      }
    }
    await this.scheduler.runNow("inbox");
    return "signed";
  }

  /** Command palette: kton: ask. */
  async askCommand(): Promise<void> {
    const name = await this.host.pick("Ask which question?", [...this.queries.keys()]);
    if (!name) return;
    try {
      const rows = await this.rows(name);
      this.host.showPanel(`ask:${name}`, `ask ${name}`, resultsPage(`ask ${name}`, rows, nonce()));
    } catch (e) {
      this.host.error(describe(e));
    }
  }

  /** The templates as the cockpit lists them now (never a file in the checkout). */
  private async listedTemplates(): Promise<TemplateInfo[] | undefined> {
    try {
      const out = await this.t.templates();
      if (!out.ok) {
        this.host.error(`The cockpit refused its template listing: ${out.refusal.reason}`);
        return undefined;
      }
      this.templates = out.value;
      return out.value;
    } catch (e) {
      this.host.error(describe(e));
      return undefined;
    }
  }

  private kindOfTemplate(name: string): "decision" | "record" {
    return this.cfg.decisionTemplates.includes(name) ? "decision" : "record";
  }

  /** Command palette: kton: say. Decision templates go through the dialog with the person key; others are records. */
  async sayCommand(): Promise<SignResult> {
    const templates = await this.listedTemplates();
    if (!templates) return "failed";
    const name = await this.host.pick("Say which template?", templates.map((t) => t.name));
    if (!name) return "cancelled";
    const tpl = templates.find((t) => t.name === name);
    if (!tpl) return "failed";
    const cannot = this.cannotSign(this.kindOfTemplate(name));
    if (cannot) {
      this.host.error(`Not signed: ${cannot}`);
      return "failed";
    }
    const about = await this.host.input("About (a sha256 id)");
    if (!about || !SHA.test(about)) {
      this.host.error("Not signed: the subject must be a sha256 id.");
      return "failed";
    }
    const fields: Record<string, string> = {};
    for (const [k, f] of Object.entries(tpl.fields)) {
      const v = await this.host.input(`${k}${f.required ? " (required)" : ""}${f.values ? ` — one of ${f.values.join("|")}` : ""}`);
      if (v === undefined) return "cancelled";
      if (v !== "") fields[k] = v;
    }
    const control: ControlInfo = { package: "command palette", name: "say", template: name, kind: this.kindOfTemplate(name) };
    return this.signThroughDialog({ kind: "sign", about, fields }, control, `Sign ${name}`);
  }

  /**
   * Compose a claim (S2.5, S2.6): a form with the listed template's fields. A required field left empty
   * blocks Send and is named; Cancel in the dialog keeps the draft. Until a package declares control
   * kinds (C2), a composed message is a person-key decision through the dialog.
   */
  async composeCommand(name?: string): Promise<void> {
    const templates = await this.listedTemplates();
    if (!templates) return;
    const choices = templates.filter((t) => !this.cfg.decisionTemplates.includes(t.name)).map((t) => t.name);
    const choice = name ?? (await this.host.pick("Compose which template?", choices));
    const tpl = templates.find((t) => t.name === choice);
    if (!tpl) return;
    this.showCompose(tpl, {});
  }

  private showCompose(tpl: TemplateInfo, view: { error?: string; missing?: string[] }): void {
    const cannot = this.cannotSign("decision");
    this.host.showPanel(
      `compose:${tpl.name}`,
      `Compose ${tpl.name}`,
      composePage(tpl, nonce(), { values: this.drafts.get(tpl.name) ?? {}, ...view, ...(cannot ? { cannotSign: cannot } : {}) }),
      (m) => void this.onCompose(tpl, m),
      !this.quiet,
    );
  }

  /** The compose form's Send: untrusted values, checked against the listed template before any dialog. */
  async onCompose(tpl: TemplateInfo, raw: unknown): Promise<SignResult> {
    const m = raw as { kind?: unknown; values?: unknown };
    if (m?.kind !== "compose" || typeof m.values !== "object" || m.values === null) return "failed";
    const values: Record<string, string> = {};
    for (const [k, v] of Object.entries(m.values as Record<string, unknown>)) {
      if (k === "about" || tpl.fields[k]) values[k] = String(v ?? "");
    }
    this.drafts.set(tpl.name, values);
    const missing = ["about", ...Object.entries(tpl.fields).filter(([, f]) => f.required).map(([k]) => k)].filter((k) => !(values[k] ?? "").trim());
    if (missing.length) {
      this.showCompose(tpl, { missing, error: `Not sent: ${missing.join(", ")} ${missing.length > 1 ? "are" : "is"} required.` });
      return "failed";
    }
    const about = values["about"] ?? "";
    if (!SHA.test(about)) {
      this.showCompose(tpl, { missing: ["about"], error: "Not sent: about must be a sha256 id." });
      return "failed";
    }
    const fields = Object.fromEntries(Object.entries(values).filter(([k, v]) => k !== "about" && v !== ""));
    const control: ControlInfo = { package: "workbench", name: "compose", template: tpl.name, kind: "decision" };
    const r = await this.signThroughDialog({ kind: "sign", about, fields }, control, `Send ${tpl.name}`);
    if (r === "signed") {
      this.drafts.delete(tpl.name);
      this.host.closePanel(`compose:${tpl.name}`);
    } else this.showCompose(tpl, { error: r === "cancelled" ? "Not sent: the dialog was cancelled. The draft is kept." : "Not sent. The draft is kept." });
    return r;
  }
}

/** A message a person can act on: what broke, without a stack. */
export function describe(e: unknown): string {
  if (e instanceof TransportError) {
    if (e.exitCode === null && /cannot run/.test(e.message)) return `${e.message}. Check the setting kton.cockpit.path.`;
    return e.message;
  }
  return e instanceof Error ? e.message : String(e);
}

/**
 * Does the cockpit say the registry is locked? On v0.5.0 there is no refusal code for it (asked of
 * kton-cockpit); the shell recognises the words until there is one.
 */
export function isLocked(e: TransportError): boolean {
  return /\block(ed)?\b/i.test(`${e.message}\n${e.stderr}`);
}

function firstLine(s: string): string {
  return s.trim().split("\n")[0] ?? "";
}

function time(d: Date | undefined): string {
  return d ? d.toISOString().slice(11, 19) : "?";
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function nonce(): string {
  return randomBytes(16).toString("base64url");
}
