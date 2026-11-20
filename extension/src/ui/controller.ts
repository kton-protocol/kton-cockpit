// The workbench controller (PR 1: Home, Inbox and thread, Decision, Lanes, the sign dialog; C1).
// It wires the core (transport, identity, sign, intents, poll) to a UiHost. No VS Code here.

import { randomBytes } from "node:crypto";
import type { TemplateInfo, Transport } from "../transport/port.js";
import { TransportError, TransportNotSent } from "../transport/port.js";
import type { WindowIdentity, ShownSigner } from "../identity.js";
import { hasKey, showSigner, signerKeyId, statusText, statusTooltip, unverifiedWhy } from "../identity.js";
import type { ControlInfo, SignPlan } from "../sign.js";
import { DialogConsent, meaningField, plan, sign, templateFor, FOOTER } from "../sign.js";
import type { UiEntry, SlotContext } from "../intents.js";
import { checkIntent } from "../intents.js";
import { Scheduler } from "../poll.js";
import { sourceRef, sourceTarget } from "./sources.js";
import { artifactRef, artifactTarget } from "./artifacts.js";
import { PackagesArea, SLOT_CMD, type ActInto, type ExpectRevision } from "../composition/area.js";
import type { Subject } from "../composition/slots.js";
import type { Control, MirrorPackage, ViewerTree } from "../packages/model.js";
import type { TreeNode, UiHost } from "./host.js";
import type { Queries, Row } from "./queries.js";
import { idOf, kindOf, rowsOf } from "./queries.js";
import { composePage, decisionPage, parseOptions, resultsPage, signDialogDetail, signerLabel, threadPage } from "./pages.js";
import type { Surface } from "./surfaces.js";
import { ALL, areasByLane, surfaceAreas, surfaceFolder } from "./surfaces.js";

export interface WorkbenchConfig {
  /**
   * Query names feeding the views. decides and answers decide what is a decision and what is closed
   * (review check 7), so they are asked BY NAME from an admitted package (`ask {query: "sparql", name}`,
   * claims.allowedPackages): a query file in the checkout is never used for them. The others only
   * select what is shown and come from the query folder (kton.queries.dir) until `ui list` exists.
   */
  readonly queries: {
    /** Folder. Proposals: msg, area, title, text, why, options, recommended, from, fromTier, when. */
    readonly proposals: string;
    /** Package, by name. Claims of the decision template: d, answers, outcome, choice, s (signer IRI), by (label, display only), byTier, when. */
    readonly decides: string;
    /** Folder. Messages: msg, kind, area, title, text, why, from, fromTier, when. */
    readonly messages: string;
    /** Package, by name. Closing claims: d, answers, outcome, text, s, by, byTier, when. */
    readonly answers: string;
    /** Folder. Lanes and their areas: lane, areas. */
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
  /** The viewers the shell vendors from kton-web, with their tree ids (src/packages/vendored.ts). */
  readonly vendoredViewers?: ReadonlyMap<string, ViewerTree>;
  /** the workbench's own blocks (media/workbench), for the generic QM area (PR 3) */
  readonly builtinPackage?: MirrorPackage;
  /** A locked registry (S7.4): how often and how far apart a `say` is retried. */
  readonly lockRetry: { readonly attempts: number; readonly delayMs: number };
  /** kton.home.openOnStart: after the first good poll, open the review view of the one waiting decision that has one */
  readonly openOnStart?: boolean;
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

/** What a refusal before say asks of the person: nothing was signed, the changed thing must be seen again. */
const AGAIN = ". Nothing was signed: open it again and sign again";

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
  openSlot: SLOT_CMD,
  openEntry: "ktonWorkbench.openEntry",
  focusHome: "ktonWorkbench.home.focus",
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
  /** The admitted packages and their slots (C2); compositions in panels (C1.5 v3). */
  readonly packages: PackagesArea;

  constructor(
    private readonly t: Transport,
    private readonly host: UiHost,
    private readonly queries: Queries,
    private readonly identity: WindowIdentity,
    private readonly cfg: WorkbenchConfig = DEFAULT_CONFIG,
    scheduler?: Scheduler,
  ) {
    this.scheduler = scheduler ?? new Scheduler();
    this.packages = new PackagesArea(t, host, {
      act: (c, about, fields, title, expect, into) => this.signAct(c, about, fields, title, expect, into),
      person: () => ({ keyId: this.identity.person.keyId, personTiers: this.cfg.personTiers }),
      // open@1: a source at a commit (read-only, kton-src), a run artifact (read-only, kton-art), or a claim this
      // window knows. What was shown goes into the panel's looked-at list (opened); a refusal does not.
      open: async (target, opened) => {
        if (target["source"] !== undefined) {
          const t = sourceTarget(target["source"]);
          if (typeof t === "string") return `not opened: ${t}`;
          if (!this.host.openSource) return "source not available on this host";
          const r = await this.host.openSource(t);
          if (r.startsWith("Opened ")) opened?.(sourceRef(t));
          return r;
        }
        if (target["artifact"] !== undefined) {
          const t = artifactTarget(target["artifact"]);
          if (typeof t === "string") return `not opened: ${t}`;
          if (!this.host.openArtifact) return "run artifacts not available on this host";
          const r = await this.host.openArtifact(t);
          if (r.startsWith("Opened ") || r.startsWith("Shown ")) opened?.(artifactRef(t));
          return r;
        }
        return this.openTarget(target);
      },
      identityName: () => this.identity.hintName,
    }, cfg.vendoredViewers, cfg.builtinPackage);
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
    // the window always runs in test mode (status "TEST MODE"): its keys are test keys
    const why = unverifiedWhy(p, a, true);
    this.host.setStatus(
      "identity",
      statusText(p, a, { person: this.identity.person.keyName, agent: this.identity.agent.keyName }, true),
      `${why ? `${why}\n` : ""}${statusTooltip(p, a)}\nOne person per window; another identity means another window. Person key: decisions, always through the sign dialog. Agent key: records, no dialog. A missing key is never replaced by the other.`,
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

  /**
   * A query an admitted package brings, asked by its name: the cockpit takes the text from the
   * package, never from this checkout (review check 7, Q1).
   */
  private async named(name: string): Promise<Row[]> {
    this.host.log(`ask sparql --name ${name}`);
    const out = await this.t.ask({ query: "sparql", name });
    if (!out.ok) {
      const hint = out.refusal.code === "query.unknown" ? " (no admitted package brings it: install the vocabulary package with --allow)" : "";
      throw new Error(`the cockpit refused ${name}: ${out.refusal.reason}${hint}`);
    }
    return rowsOf(out.value);
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
  /** The user's Refresh: the poll job now, through the scheduler, so it never runs beside a poll. */
  refreshNow(): Promise<void> {
    return this.scheduler.runNow("inbox");
  }

  async refresh(): Promise<void> {
    try {
      // one poll's cockpit calls run side by side: the listing, the queries and the packages (ui list, which never
      // throws: its errors are shown in the Packages node), so a poll takes as long as its slowest call
      const packages = this.packages.refresh();
      const [tpl, proposals, decides, messages, answers, lanes] = await Promise.all([
        this.t.templates(),
        this.rows(this.cfg.queries.proposals),
        this.named(this.cfg.queries.decides),
        this.rows(this.cfg.queries.messages),
        this.named(this.cfg.queries.answers),
        this.queries.has(this.cfg.queries.lanes) ? this.rows(this.cfg.queries.lanes) : Promise.resolve([]),
      ]);
      if (!tpl.ok) throw new Error(`the cockpit refused its template listing: ${tpl.refusal.reason}`);
      Object.assign(this, { templates: tpl.value, proposals, decides, messages, answers, lanes });
      await packages;
      const st = this.packages.statusText();
      if (st) this.host.setStatus("slots", st, "status-item entries of the admitted packages");
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
    if (this.cfg.openOnStart && !this.startOpened) {
      this.startOpened = true;
      await this.openOnStart();
    }
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

  /**
   * Is this claim's signer a person (review check 7)? Its signer's key id (from ?s, the signer IRI)
   * is this window's person key id, or its tier (from the cockpit's answer) is a person tier. The
   * label (?by) is a key file name, unique only within a tier: it is shown, never compared (Q2).
   */
  private byPerson(r: Row): boolean {
    const kid = signerKeyId(r["s"]);
    if (kid !== undefined && this.identity.person.keyId !== "" && kid === this.identity.person.keyId) return true;
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
        `polling: inbox every ${this.cfg.pollSeconds}s · ${open.length} open${this.cfg.cockpitLabel ? ` · ${this.cfg.cockpitLabel}` : ""}`,
        "Polled by code, never by an LLM. One scheduler.",
      );
    }
    // proposals are not inbox messages: the decisions waiting have their own item, which shows them on Home
    this.host.setStatus("decisions", decisionsText(decisions.length), "Proposals waiting for your decision: Home → Decisions waiting", CMD.focusHome);
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
    let ok: boolean;
    try {
      ok = await this.host.runExternal(this.cfg.recorder.extensionId, this.cfg.recorder.command);
    } catch (e) {
      this.host.error(`The kton recorder could not open: ${(e as Error).message}`);
      return false;
    }
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
          // S3.7: a/b on the Home card open the same sign dialog (never one click); then the packages'
          // review-view entries for this proposal (C2.1)
          children: [...parseOptions(d["options"]).map((o) => ({
            id: `d:${d["msg"]}:${o.id}`,
            label: `${o.id} · ${o.label}`,
            description: [o.id === d["recommended"] ? "recommended" : "", cannot ? "signing disabled" : ""].filter(Boolean).join(" · "),
            ...(cannot ? { disabled: true, tooltip: cannot } : { tooltip: "Opens the sign dialog", command: { id: CMD.chooseOption, args: [d["msg"], o.id] } }),
          })), ...this.packages.slotNodes(claimSubject(d["msg"] ?? "", d["type"]))],
        })),
      },
      { id: "inbox", label: "Inbox", description: `${open.length} open`, command: { id: CMD.focusInbox, args: [] } },
      {
        id: "areas",
        label: "Open by area",
        description: String(areas.size),
        children: [...areas.entries()].sort().map(([a, n]) => ({ id: `a:${a}`, label: a, description: String(n) })),
      },
      { id: "recorder", label: "Recorder", tooltip: "Recorded runs: opens the kton recorder extension (it stays its own extension)", command: { id: CMD.openRecorder, args: [] } },
      ...this.packages.areaNodes(),
      ...this.packages.viewNodes(),
      ...this.packages.homeNodes(),
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
          { id: `l:${l}:terminal`, label: "Open in terminal", description: "comes with the workgroup extension", disabled: true, tooltip: "Not here yet: opening a lane's terminal comes with the workgroup extension (C1.7). Use the workgroup extension until then." },
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
   * status bar (S7.4); a timeout or any failure after the say was sent is "unknown whether signed", and the
   * registry decides whether a claim exists (S7.3); "Not signed" only when it never reached the cockpit.
   */
  private async signThroughDialog(intent: { kind: "sign"; about: string; fields: Readonly<Record<string, string>>; scope?: string; opened?: readonly string[] }, control: ControlInfo, title: string, expect?: ExpectRevision): Promise<SignResult> {
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
    // a panel's act: the template must come from the package revision the panel showed (else the panel is stale)
    if (expect && p.value.expect.revision !== undefined && p.value.expect.revision !== expect.revision) {
      this.host.error(`Not signed: package ${expect.package} changed since the panel was opened${AGAIN}.`);
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
    // what the person saw is what say signs: the expectations (template hash, revision, predicate) go with the say
    // request, and say refuses template.changed when the template it would sign with is another (SPEC §8.5)
    for (let attempt = 1; ; attempt++) {
      try {
        const out = await sign(this.t, p.value, consent);
        if (!out.ok) {
          // A refusal store.busy (cockpit v0.5.2) means the registry was busy and NOTHING was signed: retry.
          if (out.refusal.code === "store.busy" && attempt < this.cfg.lockRetry.attempts) {
            this.host.setStatus("poll", `registry locked: retrying say (${attempt}/${this.cfg.lockRetry.attempts - 1})`, out.refusal.reason);
            this.host.log(`registry busy, nothing signed, retry ${attempt}: ${out.refusal.reason}`);
            await sleep(this.cfg.lockRetry.delayMs);
            continue;
          }
          if (out.refusal.code === "template.changed") return fail(`Not signed: the template changed since the dialog showed it (${out.refusal.reason})${AGAIN}.`);
          return fail(`Not signed: the cockpit refused: ${out.refusal.reason}`);
        }
        if (out.value.pending) {
          // Signed and stored, not yet committed: the next write commits it. Never sign it again.
          const text = `Signed ${out.value.id}, not yet committed (${out.value.pending.reason || out.value.pending.code}); the next write commits it`;
          if (viaDialog) this.host.signOutcome(true, text);
          this.host.info(text);
          this.host.log(`signed, pending ${out.value.pending.code}: ${out.value.id}`);
          break;
        }
        if (viaDialog) this.host.signOutcome(true, `Signed ${out.value.id}`);
        this.host.info(`Signed ${out.value.id}`);
        break;
      } catch (e) {
        // "Not signed" only when the say never reached the cockpit (kton-review N2 on #196). After it was sent, a
        // failure (a timeout, a lost connection, an unreadable answer) leaves it unknown, and it is never retried:
        // the claim may already be signed (kton-review, cockpit v0.5.2).
        if (e instanceof TransportNotSent) return fail(`Not signed: ${describe(e)}`);
        return fail(`Unknown whether signed: ${describe(e)}. Check the registry before signing again.`);
      }
    }
    await this.scheduler.runNow("inbox");
    return "signed";
  }

  /**
   * sign@1 / record@1 from a package's composition: the act's control decides the key (O1): a decision
   * control through the one sign dialog with the person key, a record control with the agent key and no
   * dialog. Same path as every other signature (signThroughDialog), so the dialog has one call site.
   */
  async signAct(c: Control, about: string, fields: Readonly<Record<string, string>>, title: string, expect?: ExpectRevision, into?: ActInto): Promise<SignResult> {
    const control: ControlInfo = { package: c.package, name: c.name, template: c.template, kind: c.kind, ...(c.meaning !== undefined ? { meaning: c.meaning } : {}) };
    // the evidence this view session opened is offered for a template's lookedAt (after what the frame proposed), and
    // listed in the dialog; it is kept nowhere else and counted nowhere
    const tpl = templateFor(control, this.templates);
    const opened = tpl?.fields["lookedAt"] && into?.opened?.length ? into.opened : undefined;
    const proposed = { ...(into?.proposed ?? {}) };
    if (opened) proposed["lookedAt"] = [...new Set([...(proposed["lookedAt"] ?? "").split(/\s+/).filter(Boolean), ...opened])].join(" ");
    const asked = await this.askOpenFields(control, fields, proposed);
    if (!asked) return "cancelled";
    return this.signThroughDialog({ kind: "sign", about, fields: asked, ...(into?.scope ? { scope: into.scope } : {}), ...(opened ? { opened } : {}) }, control, title, expect);
  }

  /**
   * The fields a template requires that the act leaves open are asked of the person here, by the shell (a pick for
   * a field with values, else an input), never taken from a frame: a decision's open outcome is picked
   * (approved or rejected, C3.4), a note its text. Before the dialog, which then shows them. Undefined: cancelled.
   */
  private async askOpenFields(control: ControlInfo, fields: Readonly<Record<string, string>>, proposed: Readonly<Record<string, string>> = {}): Promise<Record<string, string> | undefined> {
    const tpl = templateFor(control, this.templates);
    const out: Record<string, string> = { ...fields };
    for (const [k, f] of Object.entries(tpl?.fields ?? {})) {
      // a field the control's meaning fixes is never asked, nor taken from a proposal: the meaning shown is the meaning
      // signed (F2/O1 on #187); the plan refuses any other value for it
      if (out[k] !== undefined || (tpl && k === meaningField(control, tpl)) || (control.meaning !== undefined && f.values?.includes(control.meaning))) continue;
      const p = proposed[k];
      // a proposed value of a field with fixed values is taken as one of them (the dialog shows it); a proposed
      // free value is only the input box's default; an optional field nobody proposed is left out
      if (f.values && p !== undefined && f.values.includes(p)) {
        out[k] = p;
        continue;
      }
      if (!f.required && p === undefined) continue;
      const v = f.values ? await this.host.pick(`${control.name}: ${k}`, f.values) : await this.host.input(`${control.name}: ${k}`, p);
      if (v === undefined || (v === "" && f.required)) return undefined;
      if (v !== "") out[k] = v;
    }
    return out;
  }

  /** open@1: a claim this window knows opens its thread or decision; anything else is named, not fetched. */
  private openTarget(target: Readonly<Record<string, unknown>>): string {
    const ref = String(target["hash"] ?? target["uri"] ?? target["id"] ?? "");
    const iri = /^sha256:[0-9a-f]{64}$/.test(ref) ? `https://kton.dev/o/${ref.slice(7)}` : ref;
    if (this.openDecisions().some((d) => d["msg"] === iri)) {
      this.openDecision(iri);
      return `Opened the decision ${ref}`;
    }
    if (this.messages.some((m) => m["msg"] === iri)) {
      this.openThread(iri);
      return `Opened the thread ${ref}`;
    }
    this.host.info(`Open: ${ref} (not a claim in this window; nothing was fetched)`);
    return `Named ${ref}; this window does not open it`;
  }

  /** whether the start's review view was looked for already (once per workbench) */
  private startOpened = false;

  /** The waiting decisions (in the current surface) a review-view entry applies to. */
  private decisionsFor(key: string): Row[] {
    const inSurface = this.areaFilter();
    return this.openDecisions().filter((d) => inSurface(d["area"]) && this.packages.reviewEntriesFor(claimSubject(d["msg"] ?? "", d["type"])).includes(key));
  }

  /**
   * A review-view entry clicked under Home → Packages: it needs its proposal. One waiting proposal it applies to: that
   * one opens; several: a pick; none: where it opens from.
   */
  async openEntry(key: string): Promise<void> {
    const ds = this.decisionsFor(key);
    let d: Row | undefined = ds[0];
    if (ds.length === 0) {
      this.host.info("This review view opens from its proposal under Decisions waiting; no waiting decision is one it applies to.");
      return;
    }
    if (ds.length > 1) {
      const labels = ds.map((x, i) => `${i + 1}. ${x["title"] ?? x["msg"]}`);
      const pick = await this.host.pick("Open the review view for which proposal?", labels);
      if (!pick) return;
      d = ds[labels.indexOf(pick)];
    }
    if (d) await this.packages.openSlot(key, claimSubject(d["msg"] ?? "", d["type"]));
  }

  /** kton.home.openOnStart: exactly one waiting decision with a review view: its (first) review view opens. */
  private async openOnStart(): Promise<void> {
    const inSurface = this.areaFilter();
    const withView = this.openDecisions()
      .filter((d) => inSurface(d["area"]))
      .map((d) => ({ d, keys: this.packages.reviewEntriesFor(claimSubject(d["msg"] ?? "", d["type"])) }))
      .filter((x) => x.keys.length > 0);
    const one = withView.length === 1 ? withView[0] : undefined;
    if (!one) return;
    this.host.log(`open on start: ${one.keys[0]} for ${one.d["msg"]} (kton.home.openOnStart)`);
    await this.packages.openSlot(one.keys[0] as string, claimSubject(one.d["msg"] ?? "", one.d["type"]));
  }

  /** A slot entry for a subject, from the Home tree or a command (C2.1). */
  async openSlot(key: string, subject: Subject): Promise<void> {
    await this.packages.openSlot(key, subject);
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

/** A claim of the feed as a slot subject: its IRI and its claim id (the registration claim, subject.claim). */
export function claimSubject(iri: string, type?: string): Subject {
  const id = idOf(iri);
  return { kind: "claim", iri, id, claim: id, ...(type ? { type } : {}) };
}

/** The status bar's decisions item: "1 decision waiting", "2 decisions waiting". */
export function decisionsText(n: number): string {
  return `${n} decision${n === 1 ? "" : "s"} waiting`;
}
