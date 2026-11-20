// Packages in the shell (C2): load the admitted packages from `ui list`, show what they declare and what
// was refused (with the reason), put their slot entries in their places, and open compositions in panels
// with a CompositionHost each. Pure: VS Code is behind CompositionUi.
import type { Transport } from "../transport/port.js";
import type { Control, LeafViewer, Loaded, LoadedViewer, MirrorPackage, SlotEntry, ViewerTree } from "../packages/model.js";
import { loadPackages } from "../packages/model.js";
import type { TreeNode } from "../ui/host.js";
import { sparqlResultsOf } from "../ui/queries.js";
import { CompositionHost, LeafHost, type Backend, type Plan } from "./host.js";
import type { BoundTerm } from "../transport/port.js";
import { applies, oneNumber, partition, planComposition, planEntry, type Subject } from "./slots.js";
import { approvalCounts, areaComposition, BUILTIN, recordComposition, recordTerms, withBuiltin } from "./qm-area.js";
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
}
/** The package revision an act was offered at: the sign path confirms it is still the installed one before say. */
export interface ExpectRevision {
  readonly package: string;
  readonly revision: string;
}
export interface ActPath {
  /** The shell's one sign path (Workbench.signAct): the dialog for a decision control, the agent key for a record. */
  act(control: Control, about: string, fields: Readonly<Record<string, string>>, title: string, expect?: ExpectRevision, into?: { readonly scope?: string }): Promise<"signed" | "cancelled" | "failed">;
  open(target: Readonly<Record<string, unknown>>): Promise<string>;
  identityName(): string;
  /** The window's person key id and the person tiers: whether an approval counts is decided over them (S6.12). */
  person?(): { readonly keyId: string; readonly personTiers: readonly string[] };
}

export const SLOT_CMD = "ktonWorkbench.openSlot";

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
        this.queryNames = new Set(l?.ok ? l.value.queries.map((q) => q.name) : []);
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
    return async (c, about, fields, title) => {
      const h = mine();
      const why = !h || this.hosts.get(key) !== h ? "the panel is closed or was opened again" : (h.retired ?? this.outdated(h) ?? (h.revisions.has(c.package) ? undefined : `control ${c.package}/${c.name} is not one this panel was opened with`));
      if (why) {
        this.ui.error(`Not signed: ${label}: ${why}.`);
        return "failed";
      }
      const p = h as OpenPanel;
      p.busy++;
      try {
        return await this.path.act(c, about, fields, title, { package: c.package, revision: p.revisions.get(c.package) as string }, scope ? { scope } : {});
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
    for (const e of this.loaded?.entries ?? []) {
      if ((e.slot !== "home-tile" && e.slot !== "status-item") || !e.query) continue;
      try {
        const terms = Object.fromEntries(Object.entries(this.closedSet(e, undefined)).map(([k, v]) => [k, { type: "literal" as const, value: v }]));
        const n = oneNumber(await this.named(e.package, e.query, terms));
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

  /** A package's named query: by <package>/<name> when the cockpit offers it so (two packages bring the name). */
  private async named(pkg: string, name: string, terms: Readonly<Record<string, BoundTerm>>): Promise<unknown> {
    const q = this.queryNames.has(`${pkg}/${name}`) ? `${pkg}/${name}` : name;
    const out = await this.t.ask({ query: "sparql", name: q, ...(Object.keys(terms).length ? { bindTerms: terms } : {}), format: "sparql-results+json" });
    if (!out.ok) throw new Error(`the cockpit refused ${q}: ${out.refusal.reason}`);
    // the JSON-argument form answers under sparql.results (cockpit v0.5.x); read there, never as "no rows"
    return sparqlResultsOf(out.value);
  }

  /** Home: the packages, what they declare, what was refused and why, and the tiles. */
  homeNodes(): TreeNode[] {
    if (this.error) return [{ id: "pkgs", label: "Packages", description: "cannot be read", tooltip: this.error, children: [{ id: "pkgs:error", label: this.error }] }];
    const l = this.loaded;
    if (!l || (Object.keys(l.packages).length === 0 && l.refusals.length === 0 && l.notAdmitted.length === 0)) return [];
    const { rendered, listed } = partition(l.entries);
    const children: TreeNode[] = [
      ...Object.keys(l.packages).sort().map((name) => ({ id: `pkg:${name}`, label: name, description: "admitted", children: rendered.filter((e) => e.package === name).map((e) => ({ id: `slot:${entryKey(e)}`, label: e.label, description: e.slot })) })),
      ...l.refusals.map((r, i) => ({ id: `ref:${i}:${r.package}`, label: r.package, description: `refused: ${r.code}`, tooltip: r.reason, children: [{ id: `ref:${i}:why`, label: r.reason, description: r.file }] })),
      ...l.notAdmitted.map((n) => ({ id: `na:${n}`, label: n, description: "installed, not admitted" })),
      ...listed.map((e) => ({ id: `listed:${entryKey(e)}`, label: `${e.package}: ${e.label}`, description: "not rendered", tooltip: e.why })),
    ];
    const tiles = rendered.filter((e) => e.slot === "home-tile").map((e) => ({ id: `tile:${entryKey(e)}`, label: e.label, description: this.numbers.get(entryKey(e)) ?? "…" }));
    return [...tiles, { id: "pkgs", label: "Packages", description: `${Object.keys(l.packages).length} admitted${l.refusals.length ? ` · ${l.refusals.length} refused` : ""}`, children }];
  }

  /** status-item entries: label: number, for the status bar. */
  statusText(): string | undefined {
    const e = (this.loaded?.entries ?? []).filter((x) => x.slot === "status-item");
    return e.length ? e.map((x) => `${x.label}: ${this.numbers.get(entryKey(x)) ?? "…"}`).join(" · ") : undefined;
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
    if (!applies(e, s)) {
      this.ui.error(`${e.label} does not accept this subject (${s.kind}${s.type ? ` ${s.type}` : ""}).`);
      return undefined;
    }
    const loaded = this.loaded;
    if (e.area) return this.openArea(key, { ...e, area: e.area }, s, loaded, "area");
    const leaf = e.viewer ? loaded.packages[e.package]?.leaves[e.viewer] : undefined;
    if (leaf) return this.openLeaf(key, e, s, leaf, loaded);
    let plan: Plan;
    try {
      plan = await planEntry(e, s, loaded, this.path.identityName(), (p, n, b) => this.named(p, n, b));
    } catch (err) {
      this.ui.error(`${e.label} cannot be shown: ${(err as Error).message}`);
      return undefined;
    }
    return this.showPlanned(`${PANEL}${key}`, e.label, e.package, plan, loaded, loaded.packages, {});
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
        const claim = rows.length === 1 ? /^https:\/\/kton\.dev\/o\/([0-9a-f]{64})$/.exec(rows[0]?.["claim"]?.value ?? "")?.[1] : undefined;
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
    const backend: Backend = {
      query: async (p, name, terms) => {
        const r = await this.named(p, name, terms);
        return annotate && p === annotate.package && name === annotate.query ? this.withCounts(r) : r;
      },
      bytes: (p, hash) => this.t.uiCat(p, hash),
      open: opts.open ?? ((target) => this.path.open(target)),
      act: this.actFor(panel, label, () => me, opts.scope),
      run: async (ray, holes) => {
        const out = await this.t.workflowRun({ workflow: ray, bind: holes });
        if (!out.ok) throw new Error(`the cockpit refused ${ray}: ${out.refusal.reason}`);
        return `ran ${ray}`;
      },
    };
    host = new CompositionHost(plan, loaded, backend, { post: shown.post, log: (l) => this.ui.log(l) });
    const acting = plan.uses.flatMap((u) => (u.acts ?? []).map((a) => a.control?.package)).filter((x): x is string => !!x);
    me = { host, package: pkg, revisions: this.revisionsOf(loaded, [pkg, ...acting, ...(opts.revisions ?? [])]), busy: 0 };
    this.open(panel, me);
    return host;
  }

  /** An approvals answer with the column `counts`: whether each approval counts, and why (approvalCounts). */
  private withCounts(r: unknown): unknown {
    const person = this.path.person?.() ?? { keyId: "", personTiers: [] };
    const a = r as { head?: { vars?: string[] }; results?: { bindings?: Array<Record<string, { value?: string }>> } };
    const bindings = (a.results?.bindings ?? []).map((b) => ({ ...b, counts: { type: "literal", value: approvalCounts({ outcome: b["outcome"]?.value, signer: b["signer"]?.value, tier: b["tier"]?.value }, person).why } }));
    return { ...a, head: { ...a.head, vars: [...(a.head?.vars ?? []), "counts"] }, results: { ...a.results, bindings } };
  }

  /** The QM areas (activity entries with an area), in their order (order, then package name: C2.5, S6.16). */
  areaNodes(): TreeNode[] {
    const areas = (this.loaded?.entries ?? []).filter((e) => e.slot === "activity" && e.area);
    if (!areas.length) return [];
    return [{ id: "qm-areas", label: "QM areas", description: `${areas.length}`, children: areas.map((e) => ({ id: `area:${entryKey(e)}`, label: e.label, description: e.package, tooltip: `${e.package} (${e.file})${e.area?.scopes?.records ? ` · records ${e.area.scopes.records}` : ""}`, command: { id: SLOT_CMD, args: [entryKey(e), areaSubject(e)] } })) }];
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
      open: (target) => this.path.open(target),
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
    me = { host, package: e.package, revisions: this.revisionsOf(loaded, [e.package]), busy: 0 };
    this.open(panel, me);
    return host;
  }

  /** The host of an open panel (tests, and a panel's messages). */
  hostOf(key: string): CompositionHost | LeafHost | undefined {
    return this.hosts.get(`${PANEL}${key}`)?.host;
  }
}

/**
 * A slot entry's label for a tree item: the entry's own label, once; the slot kind ("Review", "Record") is put in
 * front only when the label does not already say it ("Review" stays "Review", "Test shell" becomes "Review · Test shell").
 */
/** An area's subject: its records scope (when {kind: scope}); the area's queries do not read it. */
export function areaSubject(e: SlotEntry): Subject {
  const scope = e.area?.scopes?.records ?? `${e.package}/${e.file}`;
  return { kind: "scope", iri: `https://kton.dev/scope/${encodeURIComponent(scope)}`, id: scope, scope };
}

export function slotLabel(e: { readonly slot: string; readonly label: string }): string {
  const kind = e.slot === "review-view" ? "Review" : e.slot === "record-view" ? "Record" : e.slot;
  return new RegExp(`\\b${kind}\\b`, "i").test(e.label) ? e.label : `${kind} · ${e.label}`;
}
