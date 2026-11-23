// Packages in the shell (C2): load the admitted packages from `ui list`, show what they declare and what
// was refused (with the reason), put their slot entries in their places, and open compositions in panels
// with a CompositionHost each. Pure: VS Code is behind CompositionUi.
import type { Transport } from "../transport/port.js";
import type { Control, LeafViewer, Loaded, LoadedViewer, MirrorPackage, SlotEntry, ViewerTree } from "../packages/model.js";
import { loadPackages } from "../packages/model.js";
import type { TreeNode } from "../ui/host.js";
import { sparqlResultsOf } from "../ui/queries.js";
import { supportView } from "../support/view-now.js";
import { CompositionHost, LeafHost, type AskEach, type Backend, type CheckAgainst, type Plan, type Signed } from "./host.js";
import type { BoundTerm } from "../transport/port.js";
import { applies, isView, oneNumber, partition, planComposition, planEntry, statusLine, type Subject } from "./slots.js";
import { approvalCounts, areaComposition, BUILTIN, recordComposition, recordTerms, signerKeyId, withBuiltin } from "./qm-area.js";
import { SIGNER_QUERY } from "../identity.js";
import { withThumbs } from "../ui/artifacts.js";
import { artifactsOf, changesOptions, citeOptions, differsOptions, withChanges, withCitations, withDiffers } from "../ui/annotate.js";
import type { AreaDecl } from "../packages/listing.js";

export interface CompositionUi {
  /** Open (or update) a composition panel; returns how to post to its frame. */
  showComposition(key: string, title: string, viewers: readonly LoadedViewer[], onMessage: (msg: unknown) => void): { post: (msg: unknown) => void; urls: ReadonlyMap<string, string> };
  /** A leaf panel: the vendored viewer's own page (tree-checked at start) with the host's head and CSP. */
  showLeaf(key: string, title: string, leaf: LeafViewer, onMessage: (msg: unknown) => void): { post: (msg: unknown) => void };
  closePanel(key: string): void;
  /** Mark an open panel stale (the last ui list poll failed) or current again. */
  markStale?(key: string, stale: boolean): void;
  openPanels(): string[];
  info(text: string): void;
  error(text: string): void;
  log(line: string): void;
  /**
   * Per-window view state (VS Code workspace state), for a view whose blocks keep their place (viewer.json
   * `keepsState`, e.g. the queue's position and skipped items): read when the panel opens, written on each change.
   * Kept in the window only, never in a registry.
   */
  memo?: { get(key: string): unknown; set(key: string, value: unknown): void };
  /** the panel's view changed: `build` makes the support file's content (kton.support.viewFile), when it is the active one */
  support?(key: string, build: () => Record<string, unknown>): void;
}
/** The package revision an act was offered at: the sign path confirms it is still the installed one before say. */
export interface ExpectRevision {
  readonly package: string;
  readonly revision: string;
}
export interface ActPath {
  /** The shell's one sign path (Workbench.signAct): the dialog for a decision control, the agent key for a record. */
  act(control: Control, about: string, fields: Readonly<Record<string, string>>, title: string, expect?: ExpectRevision, into?: ActInto): Promise<"signed" | "cancelled" | "failed">;
  /** open@1; `opened` is called with a reference to the evidence when it was shown (a source, a run artifact) */
  open(target: Readonly<Record<string, unknown>>, opened?: (ref: string) => void): Promise<string>;
  identityName(): string;
  /** The window's person key id and the person tiers: whether an approval counts is decided over them (S6.12). */
  person?(): { readonly keyId: string; readonly personTiers: readonly string[] };
  /** Whether an approval given on one commit still holds at another (read-only git over the mirrors). */
  staleness?(repo: string, approved: string, head: string, paths: readonly string[]): Promise<import("../ui/sources.js").Staleness>;
  /** The commits between two commits that touched a test file or a helper it imports (option changesBetween). */
  changes?(repo: string, from: string, to: string, path: string): Promise<import("../ui/sources.js").Changes>;
  /** A run artifact's thumbnail as a data URL, after its sha256 is checked (or undefined: shown as a link only). */
  thumb?(t: import("../ui/artifacts.js").ArtifactTarget): Promise<string | undefined>;
}

/** Where an act goes and what goes with it: the records scope, the frame's proposals, the panel's evidence opens. */
export interface ActInto {
  readonly scope?: string;
  readonly proposed?: Readonly<Record<string, string>>;
  /** the evidence this panel opened in this session (in memory only; never counted, only offered to lookedAt) */
  readonly opened?: readonly string[];
  /** called with the claim id and the signed fields once the act is signed (a block's nudge line is built from them) */
  readonly signed?: (s: Signed) => void;
  /** fields the shell asks per key (a use's option askEach) */
  readonly asks?: readonly AskEach[];
  /** what the values are checked against before the dialog (a use's option checkAgainst), with its query's rows */
  readonly check?: CheckAgainst & { readonly rows: () => Promise<Array<Record<string, string>>> };
}

export const SLOT_CMD = "ktonWorkbench.openSlot";
/** A review-view entry clicked under Home → Packages: the shell finds its proposal (Workbench.openEntry). */
export const ENTRY_CMD = "ktonWorkbench.openEntry";

/** A guard rule's state on Home: its finding-source query run, or why it did not run. */
export type RuleState = { readonly n: number } | { readonly why: string };
/** A guard rule's line on Home: passed (0 findings), findings (n), or not run (the detail goes in the tooltip). */
export function ruleText(s: RuleState | undefined): string {
  if (!s) return "…";
  if ("why" in s) return "not run";
  return s.n === 0 ? "passed (0 findings)" : `findings (${s.n})`;
}
/** A view's query may run this long in the cockpit (its default 30 s is shorter than a cold build of a federated index). */
export const ASK_TIMEOUT_S = 110;
/** A scope label (as `say --scope` takes it), never an IRI or a path. */
export const SCOPE_LABEL = /^[A-Za-z][A-Za-z0-9_.-]*$/;

const PANEL = "composition:";
const entryKey = (e: SlotEntry) => `${e.package}/${e.file}`;

/** An open panel: its host, and the (name, revision) of every package it was opened with and may act through. */
interface OpenPanel {
  readonly host: CompositionHost | LeafHost;
  readonly package: string;
  readonly revisions: ReadonlyMap<string, string>;
  /** acts in progress (a sign dialog may be open): the panel is never closed under one */
  busy: number;
  /** why the panel is to close once no act is in progress */
  retired?: string;
  /** the evidence opened from this panel, in order, once each (this view session only) */
  readonly opened: string[];
}

export class PackagesArea {
  loaded: Loaded | undefined;
  error: string | undefined;
  private listingText = "";
  private readonly hosts = new Map<string, OpenPanel>();
  /** the last ui list poll failed: the panels keep the last good packages and are marked stale */
  private stale = false;
  /** home-tile / status-item numbers, by entry key */
  private readonly numbers = new Map<string, string>();
  /** status-item entries that answer a line (?text): the line and its tooltip, by entry key */
  private readonly lines = new Map<string, { text: string; tooltip: string; stale: boolean }>();
  /** finding-source entries (guard rules): their state at the last poll, by entry key */
  private readonly rules = new Map<string, RuleState>();

  constructor(
    private readonly t: Transport,
    private readonly ui: CompositionUi,
    private readonly path: ActPath,
    /** the viewers this shell vendors from kton-web, by name, with their tree ids */
    private readonly vendored: ReadonlyMap<string, ViewerTree> = new Map(),
    /** the workbench's own blocks (media/workbench): the generic QM area is planned with them (PR 3) */
    private readonly builtin?: MirrorPackage,
  ) {}

  /**
   * Re-read `ui list`; reload the packages only when it changed. A panel whose package left, or changed revision,
   * is closed (S5.4), but never while one of its acts is in progress. A failed poll closes nothing: the panels keep
   * the last good packages and are marked stale, as the views are.
   */
  async refresh(): Promise<void> {
    let failed = false;
    try {
      const out = await this.t.uiList();
      if (!out.ok) throw new Error(`the cockpit refused ui list: ${out.refusal.reason}`);
      const text = JSON.stringify(out.value);
      if (text !== this.listingText || !this.loaded) {
        this.loaded = await loadPackages(out.value, (pkg, h) => this.t.uiCat(pkg, h), this.vendored);
        this.listingText = text;
        const l = await Promise.resolve().then(() => this.t.list()).catch(() => undefined);
        // a name several packages bring is offered as <package>/<name> (by design, kton-cockpit); the listing may name it
        // that way, or plainly with its package: both are read
        this.queryNames = new Set(l?.ok ? l.value.queries.map((q) => q.name) : []);
        this.queryOwners = new Map();
        for (const q of l?.ok ? l.value.queries : []) {
          const slash = q.name.indexOf("/");
          const base = slash < 0 ? q.name : q.name.slice(slash + 1);
          const owner = q.package ?? (slash < 0 ? undefined : q.name.slice(0, slash));
          if (owner) this.queryOwners.set(base, new Set([...(this.queryOwners.get(base) ?? []), owner]));
        }
        for (const r of this.loaded.refusals) this.ui.log(`package ${r.package} refused: ${r.file ? `${r.file}: ` : ""}${r.reason} (${r.code})`);
      }
      this.error = undefined;
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      this.listingText = "";
      failed = true;
    }
    if (failed !== this.stale) {
      this.stale = failed;
      for (const key of this.hosts.keys()) this.ui.markStale?.(key, failed);
    }
    if (!failed) {
      for (const [key, h] of [...this.hosts]) {
        const why = this.outdated(h);
        if (why) this.retire(key, h, why);
      }
    }
    await this.tiles();
  }

  /** Why an open panel no longer matches the loaded packages, or undefined. */
  private outdated(h: OpenPanel): string | undefined {
    for (const [name, revision] of h.revisions) {
      const now = this.loaded?.packages[name];
      if (!now) return `package ${name} is no longer admitted or loaded`;
      if (now.revision !== revision) return `package ${name} changed revision; open it again`;
    }
    return undefined;
  }

  /** Close a panel now, or after its act in progress (a sign dialog stays until the person answers it). */
  private retire(key: string, h: OpenPanel, why: string): void {
    h.retired = why;
    // under an act, later; a panel opened again over this one stays (this one's acts are refused already)
    if (h.busy > 0 || this.hosts.get(key) !== h) return;
    this.hosts.delete(key);
    this.ui.closePanel(key);
    this.ui.info(`Closed ${key.slice(PANEL.length)}: ${why}.`);
  }

  /**
   * The act path of one open panel: refused when the panel was opened at another revision of a package than the one
   * loaded now (or is closing), so an old revision's control never signs. While the act runs the panel is busy.
   */
  private actFor(key: string, label: string, mine: () => OpenPanel | undefined, scope?: string): Backend["act"] {
    return async (c, about, fields, title, proposed, signed, asks, check) => {
      const h = mine();
      const why = !h || this.hosts.get(key) !== h ? "the panel is closed or was opened again" : (h.retired ?? this.outdated(h) ?? (h.revisions.has(c.package) ? undefined : `control ${c.package}/${c.name} is not one this panel was opened with`));
      if (why) {
        this.ui.error(`Not signed: ${label}: ${why}.`);
        return "failed";
      }
      const p = h as OpenPanel;
      p.busy++;
      try {
        const r = await this.path.act(c, about, fields, title, { package: c.package, revision: p.revisions.get(c.package) as string }, { ...(scope ? { scope } : {}), ...(proposed && Object.keys(proposed).length ? { proposed } : {}), ...(p.opened.length ? { opened: [...p.opened] } : {}), ...(signed ? { signed } : {}), ...(asks?.length ? { asks } : {}), ...(check ? { check: { ...check, rows: async () => {
          const a = (await this.named(check.package, check.query, {})) as { results?: { bindings?: Array<Record<string, { value?: string }>> } };
          return (a.results?.bindings ?? []).map((b) => Object.fromEntries(Object.entries(b).map(([k, v]) => [k, v.value ?? ""])));
        } } } : {}) });
        // a record is written with the agent key and no dialog: where it went is said, as the dialog says it for a
        // decision (the records scope is the package's declaration; kton-review N1 on #187)
        if (r === "signed" && c.kind === "record" && scope) this.ui.info(`${label}: recorded into scope ${scope} with the agent key (no dialog).`);
        return r;
      } finally {
        p.busy--;
        if (p.retired && p.busy === 0) this.retire(key, p, p.retired);
      }
    };
  }

  /** The (name, revision) of the named packages as loaded now. */
  private revisionsOf(loaded: Loaded, names: Iterable<string>): Map<string, string> {
    const out = new Map<string, string>();
    for (const n of names) {
      const p = loaded.packages[n];
      if (p) out.set(n, p.revision);
    }
    return out;
  }

  private async tiles(): Promise<void> {
    this.numbers.clear();
    this.lines.clear();
    this.rules.clear();
    for (const e of this.loaded?.entries ?? []) {
      if (e.slot !== "finding-source") continue;
      if (!e.query) {
        this.rules.set(entryKey(e), { why: "the entry names no query" });
        continue;
      }
      try {
        const terms = Object.fromEntries(Object.entries(this.closedSet(e, undefined)).map(([k, v]) => [k, { type: "literal" as const, value: v }]));
        const r = (await this.named(e.package, e.query, terms)) as { results?: { bindings?: unknown[] } };
        const b = r.results?.bindings;
        this.rules.set(entryKey(e), Array.isArray(b) ? { n: b.length } : { why: "the answer has no rows (no data)" });
      } catch (err) {
        this.rules.set(entryKey(e), { why: (err as Error).message });
      }
    }
    for (const e of this.loaded?.entries ?? []) {
      if ((e.slot !== "home-tile" && e.slot !== "status-item") || !e.query) continue;
      try {
        const terms = Object.fromEntries(Object.entries(this.closedSet(e, undefined)).map(([k, v]) => [k, { type: "literal" as const, value: v }]));
        const answer = await this.named(e.package, e.query, terms);
        // a status-item may answer a line (?text) instead of one number: shown as it is, with its freshness
        const line = e.slot === "status-item" ? statusLine(answer) : undefined;
        if (line) {
          this.lines.set(entryKey(e), line);
          continue;
        }
        const n = oneNumber(answer);
        this.numbers.set(entryKey(e), "n" in n ? String(n.n) : `error: ${n.why}`);
      } catch (err) {
        this.numbers.set(entryKey(e), `error: ${(err as Error).message}`);
      }
    }
  }

  /** inputs of an entry bound from the closed set (C2.1): subject.*, identity.name. */
  private closedSet(e: SlotEntry, s: Subject | undefined): Record<string, string> {
    const out: Record<string, string> = {};
    for (const [k, src] of Object.entries(e.inputs ?? {})) {
      const v = src === "identity.name" ? this.path.identityName() : s && src.startsWith("subject.") ? (s as unknown as Record<string, string | undefined>)[src.slice(8)] : undefined;
      if (v === undefined) throw new Error(`${entryKey(e)}: input ${k} = ${src} has no value here`);
      out[k] = v;
    }
    return out;
  }

  /** The names the cockpit offers its named queries under (`ask --json`): a clashing one as <package>/<name>. */
  private queryNames = new Set<string>();
  /** the packages that bring each query name (from `ask --json`'s package field or a qualified name) */
  private queryOwners = new Map<string, Set<string>>();

  /** A package's named query: by <package>/<name> when the cockpit offers it so (two packages bring the name). */
  private async named(pkg: string, name: string, terms: Readonly<Record<string, BoundTerm>>, supersede?: string): Promise<unknown> {
    const owners = this.queryOwners.get(name);
    const q = this.queryNames.has(`${pkg}/${name}`) || (owners !== undefined && owners.size > 1 && owners.has(pkg)) ? `${pkg}/${name}` : name;
    const out = await this.t.ask({ query: "sparql", name: q, ...(Object.keys(terms).length ? { bindTerms: terms } : {}), format: "sparql-results+json", timeoutSeconds: ASK_TIMEOUT_S }, supersede !== undefined ? { supersede } : undefined);
    if (!out.ok) throw new Error(`the cockpit refused ${q}: ${out.refusal.reason}`);
    // the JSON-argument form answers under sparql.results (cockpit v0.5.x); read there, never as "no rows"
    return sparqlResultsOf(out.value);
  }

  /** Home: the packages, what they declare, what was refused and why, and the tiles. */
  homeNodes(): TreeNode[] {
    if (this.error) return [{ id: "pkgs", label: "Packages", description: "cannot be read", tooltip: this.error, children: [{ id: "pkgs:error", label: this.error }] }];
    const l = this.loaded;
    if (!l || (Object.keys(l.packages).length === 0 && l.refusals.length === 0 && l.notAdmitted.length === 0)) return [];
    const { rendered, listed: all } = partition(l.entries);
    // guard rules (finding-source) have their own node with their state; the rest of the listed ones as before
    const findings = all.filter((e) => e.slot === "finding-source");
    const listed = all.filter((e) => e.slot !== "finding-source");
    const children: TreeNode[] = [
      ...Object.keys(l.packages).sort().map((name) => ({ id: `pkg:${name}`, label: name, description: "admitted", children: rendered.filter((e) => e.package === name).map((e) => ({ id: `slot:${entryKey(e)}`, label: e.label, description: e.slot, ...(e.slot === "review-view" ? { tooltip: "A review view opens for a waiting proposal: click to open it for one", command: { id: ENTRY_CMD, args: [entryKey(e)] } } : {}) })) })),
      ...l.refusals.map((r, i) => ({ id: `ref:${i}:${r.package}`, label: r.package, description: `refused: ${r.code}`, tooltip: r.reason, children: [{ id: `ref:${i}:why`, label: r.reason, description: r.file }] })),
      ...l.notAdmitted.map((n) => ({ id: `na:${n}`, label: n, description: "installed, not admitted" })),
      ...listed.map((e) => ({ id: `listed:${entryKey(e)}`, label: `${e.package}: ${e.label}`, description: "not rendered", tooltip: e.why })),
    ];
    const tiles = rendered.filter((e) => e.slot === "home-tile").map((e) => ({ id: `tile:${entryKey(e)}`, label: e.label, description: this.numbers.get(entryKey(e)) ?? "…" }));
    const rules: TreeNode[] = findings.length
      ? [{
          id: "rules",
          label: "Guard rules",
          description: String(findings.length),
          children: findings.map((e) => {
            const s = this.rules.get(entryKey(e));
            const why = s && "why" in s ? `\nNot run: ${s.why}` : "";
            return { id: `rule:${entryKey(e)}`, label: e.label, description: ruleText(s), tooltip: `${e.label}\n${e.package} · query ${e.query ?? "(none)"}${s && "n" in s ? `\n${s.n} finding(s) at the last poll` : ""}${why}` };
          }),
        }]
      : [];
    return [...tiles, ...rules, { id: "pkgs", label: "Packages", description: `${Object.keys(l.packages).length} admitted${l.refusals.length ? ` · ${l.refusals.length} refused` : ""}`, children }];
  }

  /** status-item entries: label: number, for the status bar. */
  statusText(): string | undefined {
    const e = (this.loaded?.entries ?? []).filter((x) => x.slot === "status-item");
    return e.length ? e.map((x) => this.lines.get(entryKey(x))?.text ?? `${x.label}: ${this.numbers.get(entryKey(x)) ?? "…"}`).join(" · ") : undefined;
  }

  /** The status bar's tooltip for the status-item entries: each one's label and, for a line, its row. */
  statusTooltip(): string {
    const e = (this.loaded?.entries ?? []).filter((x) => x.slot === "status-item");
    return e.map((x) => {
      const l = this.lines.get(entryKey(x));
      return l ? `${x.label} (${x.package})${l.stale ? " · stale" : ""}\n${l.tooltip}` : `${x.label} (${x.package})`;
    }).join("\n\n") || "status-item entries of the admitted packages";
  }

  /** The keys of the review-view entries that apply to a subject, in their order. */
  reviewEntriesFor(s: Subject): string[] {
    return (this.loaded?.entries ?? []).filter((e) => e.slot === "review-view" && applies(e, s)).map(entryKey);
  }

  /**
   * The decision control of a package whose review view applies to this proposal, when that package brings one that
   * signs a `…decides` template (e.g. a review package's decides → its own <name>-decides): a decision taken on such a proposal signs in
   * that package's vocabulary, not in the window's generic one (a guard counts a decision only in the vocabulary it expects).
   */
  decisionControlFor(s: Subject): Control | undefined {
    const pkgs = new Set((this.loaded?.entries ?? []).filter((e) => e.slot === "review-view" && applies(e, s)).map((e) => e.package));
    for (const c of this.loaded?.controls.values() ?? []) if (pkgs.has(c.package) && c.kind === "decision" && /(^|-)decides$/.test(c.template)) return c;
    return undefined;
  }

  /** The review-view / record-view entries for a subject: children of its node (a click opens the panel). */
  slotNodes(s: Subject): TreeNode[] {
    return (this.loaded?.entries ?? []).filter((e) => (e.slot === "review-view" || e.slot === "record-view") && applies(e, s)).map((e) => ({ id: `rv:${s.id}:${entryKey(e)}`, label: slotLabel(e), description: e.package, tooltip: `${e.slot} of ${e.package} (${e.file})`, command: { id: SLOT_CMD, args: [entryKey(e), s] } }));
  }

  /** Open an entry for a subject in its panel (a second open with a new subject updates it, C1.5). */
  async openSlot(key: string, s: Subject): Promise<CompositionHost | LeafHost | undefined> {
    const e = this.loaded?.entries.find((x) => entryKey(x) === key);
    if (!e || !this.loaded) {
      this.ui.error(`No admitted package declares ${key}.`);
      return undefined;
    }
    // an area (activity + area) opens on its records scope, from the QM areas node: a `when` it declares must say
    // scope; one without a `when` (ktonpkg does not ask for it on an activity) applies to its scope subject
    if (!(e.area && e.when === undefined && s.kind === "scope") && !applies(e, s)) {
      this.ui.error(`${e.label} does not accept this subject (${s.kind}${s.type ? ` ${s.type}` : ""}).`);
      return undefined;
    }
    const loaded = this.loaded;
    if (e.area) return this.openArea(key, { ...e, area: e.area }, s, loaded, "area");
    const leaf = e.viewer ? loaded.packages[e.package]?.leaves[e.viewer] : undefined;
    if (leaf) return this.openLeaf(key, e, s, leaf, loaded);
    // the entry's records scope (ktonpkg v0.9.11, the area's shape): its acts sign into it, as an area's do
    const records = e.scopes?.records;
    if (records !== undefined && !SCOPE_LABEL.test(records)) {
      this.ui.error(`${e.label} cannot be shown: its scopes.records ${JSON.stringify(records)} is not a scope label`);
      return undefined;
    }
    let plan: Plan;
    try {
      plan = await planEntry(e, s, loaded, this.path.identityName(), (p, n, b) => this.named(p, n, b), this.builtin);
    } catch (err) {
      this.ui.error(`${e.label} cannot be shown: ${(err as Error).message}`);
      return undefined;
    }
    return this.showPlanned(`${PANEL}${key}`, e.label, e.package, plan, loaded, this.builtin ? { ...loaded.packages, [BUILTIN]: this.builtin } : loaded.packages, { ...(records ? { scope: records } : {}) });
  }

  /**
   * An area (S6): the built-in area composition for the scope, or the record composition for one record
   * (opened from the list, list → record). The same code for every area; the declaration says what to show.
   */
  private async openArea(key: string, e: SlotEntry & { area: AreaDecl }, s: Subject, loaded: Loaded, which: "area" | "record"): Promise<CompositionHost | undefined> {
    if (!this.builtin) {
      this.ui.error(`${e.label} cannot be shown: this shell carries no area blocks.`);
      return undefined;
    }
    const built = which === "area" ? areaComposition(e, loaded) : recordComposition(e, loaded);
    const packages = withBuiltin(loaded, this.builtin, built);
    let plan: Plan;
    try {
      plan = await planComposition({ package: BUILTIN, composition: String(built.decl["composition"]) }, packages, s, built.params, this.path.identityName(), (p, n, b) => this.named(p, n, b));
    } catch (err) {
      this.ui.error(`${e.label} cannot be shown: ${(err as Error).message}`);
      return undefined;
    }
    const a = e.area;
    if (a.scopes?.records !== undefined && !SCOPE_LABEL.test(a.scopes.records)) {
      this.ui.error(`${e.label} cannot be shown: its scopes.records ${JSON.stringify(a.scopes.records)} is not a scope label`);
      return undefined;
    }
    const panel = which === "area" ? `${PANEL}${key}` : `${PANEL}${key}#record`;
    const label = which === "area" ? e.label : `${e.label}: ${s.iri.split("/").pop() ?? s.iri}`;
    return this.showPlanned(panel, label, e.package, plan, loaded, packages, {
      revisions: built.packages,
      ...(a.scopes?.records ? { scope: a.scopes.records } : {}),
      // list → record: a row of the area's list opens that record. Its subject (what the actions are about) is the
      // record's latest claim as the area's record query answers it here, never a claim the frame names (S6.8).
      open: async (target) => {
        const uri = typeof target["uri"] === "string" ? target["uri"] : "";
        const terms = which === "area" && /^https?:\/\//.test(uri) ? recordTerms(a, loaded, e.package, uri) : undefined;
        if (!terms || !a.record) return this.path.open(target);
        const r = (await this.named(e.package, a.record, terms)) as { results?: { bindings?: Array<Record<string, { value?: string }>> } };
        const rows = r.results?.bindings ?? [];
        // one current record: every row names the same claim (a record query may answer one row per field)
        const claims = [...new Set(rows.map((x) => x["claim"]?.value ?? ""))];
        const claim = rows.length >= 1 && claims.length === 1 ? /^https:\/\/kton\.dev\/o\/([0-9a-f]{64})$/.exec(claims[0] as string)?.[1] : undefined;
        if (!claim) return `${uri} is not one current record of ${e.label} (${rows.length} rows): nothing opened`;
        const opened = await this.openArea(key, e, { kind: "claim", iri: uri, id: `sha256:${claim}`, claim: `sha256:${claim}` }, loaded, "record");
        return opened ? `Opened ${uri}` : `${uri} cannot be shown`;
      },
      // whether an approval counts is the shell's decision over ?signer (S6.12): a column added to the answer
      ...(a.approvals ? { annotate: { package: e.package, query: a.approvals } } : {}),
    });
  }

  /** Show a planned composition in its panel, with a host whose backend is the window's. */
  private showPlanned(
    panel: string,
    label: string,
    pkg: string,
    plan0: Plan,
    loaded: Loaded,
    packages: Readonly<Record<string, MirrorPackage>>,
    opts: { revisions?: readonly string[]; scope?: string; open?: Backend["open"]; annotate?: { package: string; query: string } },
  ): CompositionHost | undefined {
    let plan = plan0;
    const viewers = new Map<string, LoadedViewer>();
    for (const p of Object.values(packages)) for (const v of Object.values(p.viewers)) viewers.set(v.module, v);
    const used = [...new Set(plan.uses.map((u) => (u as { module?: string }).module))].map((m) => viewers.get(m ?? "")).filter((v): v is LoadedViewer => !!v);
    let host: CompositionHost | undefined;
    let me: OpenPanel | undefined;
    let shown: ReturnType<CompositionUi["showComposition"]>;
    try {
      shown = this.ui.showComposition(panel, label, used, (m) => void host?.onMessage(m));
    } catch (err) {
      this.ui.error(`${label} cannot be shown: ${(err as Error).message}`);
      return undefined;
    }
    // the frame imports each module from where the host serves its checked tree
    plan = { ...plan, uses: plan.uses.map((u) => ({ ...u, module: shown.urls.get((u as { module?: string }).module ?? "") ?? "" })) } as Plan;
    const annotate = opts.annotate;
    const cites = new Map<string, Promise<ReturnType<typeof artifactsOf>>>();
    const backend: Backend = {
      query: async (p, name, terms, use) => {
        // a block's newer ask drops its older one still waiting in the window's queue (a reader moving through items)
        const r0 = await this.named(p, name, terms, use !== undefined ? `${panel}#${use}` : undefined);
        // columns the shell adds from the use's options: what a text cites (resolved against a named query of the same
        // package, asked once per panel) and where decisions on one thing differ
        const uo = (plan0.uses.find((u) => u.key === use) as { options?: Record<string, unknown> } | undefined)?.options;
        const co = citeOptions(uo);
        const dof = differsOptions(uo);
        let r = r0;
        if (co) {
          const k = `${p}/${co.query}`;
          let arts = cites.get(k);
          if (!arts) {
            arts = this.named(p, co.query, {}).then(artifactsOf);
            cites.set(k, arts);
            arts.catch(() => cites.delete(k));
          }
          r = withCitations(r, co, await arts);
        }
        if (dof) r = withDiffers(r, dof);
        const cb = changesOptions(uo);
        if (cb) {
          let reviews: Array<Record<string, { value?: string }>> = [];
          if (cb.reviews) {
            const rv = (await this.named(p, cb.reviews, {})) as { results?: { bindings?: Array<Record<string, { value?: string }>> } };
            reviews = rv.results?.bindings ?? [];
          }
          r = await withChanges(r, cb, this.path.changes, reviews);
        }
        const shown = await this.withApprovals(annotate && p === annotate.package && name === annotate.query ? await this.withCounts(r) : r);
        return this.path.thumb ? withThumbs(shown, this.path.thumb) : shown;
      },
      bytes: (p, hash) => this.t.uiCat(p, hash),
      open: opts.open ?? ((target) => this.path.open(target, (ref) => me && noteOpened(me, ref))),
      act: this.actFor(panel, label, () => me, opts.scope),
      run: async (ray, holes) => {
        const out = await this.t.workflowRun({ workflow: ray, bind: holes });
        if (!out.ok) throw new Error(`the cockpit refused ${ray}: ${out.refusal.reason}`);
        return `ran ${ray}`;
      },
    };
    // blocks that draw their own act buttons (viewer.json rendersActs): their acts go to the frame as blockActs only
    const drawsActs = new Set(plan0.uses.filter((u) => viewers.get((u as { module?: string }).module ?? "")?.decl["rendersActs"] === true).map((u) => u.key));
    // a view with a block that keeps its place (viewer.json keepsState) starts from the window's last state of it
    const keeps = used.some((v) => v.decl["keepsState"] === true) && this.ui.memo ? this.ui.memo : undefined;
    const keepKey = `kton.viewState:${panel}`;
    const support = this.ui.support ? (build: () => Record<string, unknown>) => this.ui.support?.(panel, build) : undefined;
    const view = { package: plan.root.package, composition: plan.root.composition, entry: label };
    const run = opts.scope && /^[A-Za-z0-9_-]+\./.test(opts.scope) ? opts.scope.slice(0, opts.scope.indexOf(".")) : undefined;
    host = new CompositionHost(
      plan,
      loaded,
      backend,
      {
        post: shown.post,
        log: (l) => this.ui.log(l),
        ...(keeps ? { keep: (st: unknown) => keeps.set(keepKey, st) } : {}),
        ...(support ? { changed: () => support(() => supportView({ at: new Date(), identity: this.path.identityName(), view, ...(run ? { run } : {}), state: host?.state, blocks: host?.shown() ?? [], opened: me?.opened ?? [] })) } : {}),
      },
      keeps?.get(keepKey) ?? {},
      drawsActs,
    );
    const acting = plan.uses.flatMap((u) => (u.acts ?? []).map((a) => a.control?.package)).filter((x): x is string => !!x);
    me = { host, package: pkg, revisions: this.revisionsOf(loaded, [pkg, ...acting, ...(opts.revisions ?? [])]), busy: 0, opened: [] };
    this.open(panel, me);
    return host;
  }

  /**
   * An approvals answer with the column `counts`: whether each approval counts, and why (approvalCounts). Each
   * signer's tier is the cockpit's answer for its key id (the shell's own signer query, as for the window's keys),
   * never a column of the package's query.
   */
  private async withCounts(r: unknown): Promise<unknown> {
    const person = this.path.person?.() ?? { keyId: "", personTiers: [] };
    const a = r as { head?: { vars?: string[] }; results?: { bindings?: Array<Record<string, { value?: string }>> } };
    const kids = [...new Set((a.results?.bindings ?? []).map((b) => signerKeyId(b["signer"]?.value)).filter((k): k is string => !!k))];
    const tiers = new Map<string, string>();
    for (const kid of kids) {
      const out = await this.t.ask({ query: "sparql", sparql: SIGNER_QUERY, bind: { KEYID: kid }, format: "sparql-results+json", limit: 2 });
      const rows = out.ok ? sparqlResultsOf(out.value).results.bindings : [];
      if (rows.length === 1 && rows[0]?.["tier"]?.value) tiers.set(kid, rows[0]["tier"].value);
    }
    const bindings = (a.results?.bindings ?? []).map((b) => ({ ...b, counts: { type: "literal", value: approvalCounts({ outcome: b["outcome"]?.value, signer: b["signer"]?.value }, person, (k) => tiers.get(k)).why } }));
    return { ...a, head: { ...a.head, vars: [...(a.head?.vars ?? []), "counts"] }, results: { ...a.results, bindings } };
  }

  /**
   * Rows that name the commit a verdict approves (?approvedCommit, with ?repo and the reviewed ?commit) get
   * ?approval: which commit the verdict approves (and "commit from text" when it was read out of the
   * free-text why, ?approvedFrom "text"), and whether it is stale: the approved files (?approvedPaths, space-separated,
   * else the approved commit's own files) changed up to the reviewed commit, named. Read-only git over the mirrors.
   */
  private async withApprovals(answer: unknown): Promise<unknown> {
    const a = answer as { head?: { vars?: string[] }; results?: { bindings?: Array<Record<string, { type: string; value: string }>> } };
    const b = a.results?.bindings ?? [];
    if (!b.some((r) => r["approvedCommit"]?.value)) return answer;
    const bindings = await Promise.all(
      b.map(async (r) => {
        const approved = r["approvedCommit"]?.value;
        const repo = r["repo"]?.value;
        const head = r["commit"]?.value;
        if (!approved || !repo || !head) return r;
        const from = r["approvedFrom"]?.value === "text" ? " (commit from text)" : "";
        const paths = (r["approvedPaths"]?.value ?? "").split(/\s+/).filter(Boolean);
        let text = `approves ${approved.slice(0, 12)}${from}`;
        let stale = "";
        if (approved.startsWith(head) || head.startsWith(approved)) text += " · the reviewed commit itself";
        else if (!this.path.staleness) text += ` · not checked against ${head.slice(0, 12)} (no sources on this host)`;
        else {
          const s = await this.path.staleness(repo, approved, head, paths);
          if ("why" in s) text += ` · not checked against ${head.slice(0, 12)}: ${s.why}`;
          else if (s.stale) {
            stale = "true";
            text += ` · STALE for ${head.slice(0, 12)}: approved files changed since: ${s.files.join(", ")}`;
          } else text += ` · holds for ${head.slice(0, 12)}: none of its ${s.paths.length} approved file(s) changed`;
        }
        return { ...r, approval: { type: "literal", value: text }, ...(stale ? { stale: { type: "literal", value: stale } } : {}) };
      }),
    );
    return { ...a, head: { ...a.head, vars: [...new Set([...(a.head?.vars ?? []), "approval", "stale"])] }, results: { ...a.results, bindings } };
  }

  /** The QM areas (activity entries with an area), in their order (order, then package name: C2.5, S6.16). */
  areaNodes(): TreeNode[] {
    const areas = (this.loaded?.entries ?? []).filter((e) => e.slot === "activity" && e.area);
    if (!areas.length) return [];
    return [{ id: "qm-areas", label: "QM areas", description: `${areas.length}`, children: areas.map((e) => ({ id: `area:${entryKey(e)}`, label: e.label, description: e.package, tooltip: `${e.package} (${e.file})${e.area?.scopes?.records ? ` · records ${e.area.scopes.records}` : ""}`, command: { id: SLOT_CMD, args: [entryKey(e), areaSubject(e)] } })) }];
  }

  /**
   * The views: `activity` entries that name a composition and no area (isView), in their order (order, then package
   * name, as the areas). A click opens the composition with the view's own place as its subject (viewSubject).
   */
  viewNodes(): TreeNode[] {
    const views = (this.loaded?.entries ?? []).filter(isView);
    if (!views.length) return [];
    return [{ id: "views", label: "Views", description: `${views.length}`, children: views.map((e) => ({ id: `view:${entryKey(e)}`, label: e.label, description: e.package, tooltip: `${e.package} (${e.file})`, command: { id: SLOT_CMD, args: [entryKey(e), viewSubject(e)] } })) }];
  }

  /** A panel opened (or opened again over an older one, which is replaced: its acts are refused from now on). */
  private open(key: string, p: OpenPanel): void {
    const old = this.hosts.get(key);
    if (old && old !== p) old.retired = "the panel was opened again";
    this.hosts.set(key, p);
    if (this.stale) this.ui.markStale?.(key, true);
  }

  private backend(key: string, label: string, mine: () => OpenPanel | undefined): Backend {
    return {
      query: (pkg, name, terms) => this.named(pkg, name, terms),
      bytes: (pkg, hash) => this.t.uiCat(pkg, hash),
      open: (target) => this.path.open(target, (ref) => { const m = mine(); if (m) noteOpened(m, ref); }),
      act: this.actFor(key, label, mine),
      run: async (ray, holes) => {
        const out = await this.t.workflowRun({ workflow: ray, bind: holes });
        if (!out.ok) throw new Error(`the cockpit refused ${ray}: ${out.refusal.reason}`);
        return `ran ${ray}`;
      },
      graph: async () => {
        const out = await this.t.graph();
        if (!out.ok) throw new Error(`the cockpit refused show: ${out.refusal.reason}`);
        return out.value;
      },
    };
  }

  /** A page viewer in its own frame for one entry and subject (graph in record-view). */
  private openLeaf(key: string, e: SlotEntry, s: Subject, leaf: LeafViewer, loaded: Loaded): LeafHost {
    const panel = `${PANEL}${key}`;
    const control = e.control ? loaded.controls.get(`${e.package}/${e.control}`) : undefined;
    const closed: Record<string, string> = { "subject.id": s.id, "subject.iri": s.iri, "subject.kind": s.kind, ...(s.claim ? { "subject.claim": s.claim } : {}), ...(s.type ? { "subject.type": s.type } : {}), "identity.name": this.path.identityName() };
    let host: LeafHost | undefined;
    let me: OpenPanel | undefined;
    const shown = this.ui.showLeaf(panel, e.label, leaf, (m) => void host?.onMessage(m));
    host = new LeafHost({ decl: leaf.decl, slot: e.slot }, { ...(e.inputs ? { inputs: e.inputs } : {}), ...(control ? { control } : {}), label: e.label, package: e.package }, { id: s.id, ...(s.claim ? { claim: s.claim } : {}), closed }, loaded, this.backend(panel, e.label, () => me), { post: shown.post, log: (l) => this.ui.log(l) });
    me = { host, package: e.package, revisions: this.revisionsOf(loaded, [e.package]), busy: 0, opened: [] };
    this.open(panel, me);
    return host;
  }

  /** The host of an open panel (tests, and a panel's messages). */
  hostOf(key: string): CompositionHost | LeafHost | undefined {
    return this.hosts.get(`${PANEL}${key}`)?.host;
  }
}

/** Evidence opened from a panel: kept once, in order, in memory. */
function noteOpened(p: OpenPanel, ref: string): void {
  if (!p.opened.includes(ref)) p.opened.push(ref);
}

/** The evidence a panel opened in this session (tests). */
export function openedOf(area: PackagesArea, key: string): readonly string[] {
  return (area as unknown as { hosts: Map<string, OpenPanel> }).hosts.get(`${PANEL}${key}`)?.opened ?? [];
}

/**
 * An area's subject: its records scope (when {kind: scope}). Its IRI (https://kton.dev/scope/<name>) is the shell's
 * own name for the panel's subject: it is never signed, asked or shown as a kton IRI, and no area query reads it
 * (a scope's own id is its seed claim, which ui list does not give the shell).
 */
export function areaSubject(e: SlotEntry): Subject {
  const scope = e.area?.scopes?.records ?? `${e.package}/${e.file}`;
  return { kind: "scope", iri: `https://kton.dev/scope/${encodeURIComponent(scope)}`, id: scope, scope };
}

/**
 * A view's subject: the view's own place (<package>/<file>), {kind: scope}. Its IRI is the shell's own name for the
 * panel's subject, as an area's: never signed, asked or shown as a kton IRI.
 */
export function viewSubject(e: SlotEntry): Subject {
  const id = `${e.package}/${e.file}`;
  return { kind: "scope", iri: `https://kton.dev/scope/${encodeURIComponent(id)}`, id };
}

/**
 * A slot entry's label for a tree item: the entry's own label, once; the slot kind ("Review", "Record") is put in
 * front only when the label does not already say it ("Review" stays "Review", "Test shell" becomes "Review · Test shell").
 */
export function slotLabel(e: { readonly slot: string; readonly label: string }): string {
  const kind = e.slot === "review-view" ? "Review" : e.slot === "record-view" ? "Record" : e.slot;
  return new RegExp(`\\b${kind}\\b`, "i").test(e.label) ? e.label : `${kind} · ${e.label}`;
}
