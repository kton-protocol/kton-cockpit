// Packages in the shell (C2): load the admitted packages from `ui list`, show what they declare and what
// was refused (with the reason), put their slot entries in their places, and open compositions in panels
// with a CompositionHost each. Pure: VS Code is behind CompositionUi.
import type { Transport } from "../transport/port.js";
import type { Control, Loaded, LoadedViewer, SlotEntry } from "../packages/model.js";
import { loadPackages } from "../packages/model.js";
import type { TreeNode } from "../ui/host.js";
import { CompositionHost, type Backend, type Plan } from "./host.js";
import { applies, oneNumber, partition, planEntry, type Subject } from "./slots.js";

export interface CompositionUi {
  /** Open (or update) a composition panel; returns how to post to its frame. */
  showComposition(key: string, title: string, viewers: readonly LoadedViewer[], onMessage: (msg: unknown) => void): { post: (msg: unknown) => void; urls: ReadonlyMap<string, string> };
  closePanel(key: string): void;
  openPanels(): string[];
  info(text: string): void;
  error(text: string): void;
  log(line: string): void;
}
export interface ActPath {
  /** The shell's one sign path (Workbench.signAct): the dialog for a decision control, the agent key for a record. */
  act(control: Control, about: string, fields: Readonly<Record<string, string>>, title: string): Promise<"signed" | "cancelled" | "failed">;
  open(target: Readonly<Record<string, unknown>>): Promise<string>;
  identityName(): string;
}

export const SLOT_CMD = "ktonWorkbench.openSlot";

const PANEL = "composition:";
const entryKey = (e: SlotEntry) => `${e.package}/${e.file}`;

export class PackagesArea {
  loaded: Loaded | undefined;
  error: string | undefined;
  private listingText = "";
  private readonly hosts = new Map<string, { host: CompositionHost; package: string }>();
  /** home-tile / status-item numbers, by entry key */
  private readonly numbers = new Map<string, string>();

  constructor(
    private readonly t: Transport,
    private readonly ui: CompositionUi,
    private readonly path: ActPath,
  ) {}

  /** Re-read `ui list`; reload the packages only when it changed. Panels of a package that left are closed (S5.4). */
  async refresh(): Promise<void> {
    try {
      const out = await this.t.uiList();
      if (!out.ok) throw new Error(`the cockpit refused ui list: ${out.refusal.reason}`);
      const text = JSON.stringify(out.value);
      if (text !== this.listingText || !this.loaded) {
        this.loaded = await loadPackages(out.value, (d, h) => this.t.packageBlob(d, h));
        this.listingText = text;
        for (const r of this.loaded.refusals) this.ui.log(`package ${r.package} refused: ${r.file ? `${r.file}: ` : ""}${r.reason} (${r.code})`);
      }
      this.error = undefined;
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      this.loaded = undefined;
      this.listingText = "";
    }
    for (const [key, h] of [...this.hosts]) {
      if (!this.loaded?.packages[h.package]) {
        this.hosts.delete(key);
        this.ui.closePanel(key);
        this.ui.info(`Closed ${key.slice(PANEL.length)}: package ${h.package} is no longer admitted or loaded.`);
      }
    }
    await this.tiles();
  }

  private async tiles(): Promise<void> {
    this.numbers.clear();
    for (const e of this.loaded?.entries ?? []) {
      if ((e.slot !== "home-tile" && e.slot !== "status-item") || !e.query) continue;
      try {
        const n = oneNumber(await this.query(e.query, this.closedSet(e, undefined)));
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

  private async query(name: string, bind: Readonly<Record<string, string>>): Promise<unknown> {
    const out = await this.t.ask({ query: "sparql", name, ...(Object.keys(bind).length ? { bind } : {}), format: "sparql-results+json" });
    if (!out.ok) throw new Error(`the cockpit refused ${name}: ${out.refusal.reason}`);
    return out.value.results ?? { head: { vars: [] }, results: { bindings: [] } };
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
    return (this.loaded?.entries ?? []).filter((e) => (e.slot === "review-view" || e.slot === "record-view") && applies(e, s)).map((e) => ({ id: `rv:${s.id}:${entryKey(e)}`, label: `${e.slot === "review-view" ? "Review" : "Record"}: ${e.label}`, description: e.package, command: { id: SLOT_CMD, args: [entryKey(e), s] } }));
  }

  /** Open an entry for a subject in its panel (a second open with a new subject updates it, C1.5). */
  async openSlot(key: string, s: Subject): Promise<CompositionHost | undefined> {
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
    let plan: Plan;
    try {
      plan = await planEntry(e, s, loaded, this.path.identityName(), (n, b) => this.query(n, b));
    } catch (err) {
      this.ui.error(`${e.label} cannot be shown: ${(err as Error).message}`);
      return undefined;
    }
    const panel = `${PANEL}${key}`;
    const viewers = new Map<string, LoadedViewer>();
    for (const p of Object.values(loaded.packages)) for (const v of Object.values(p.viewers)) viewers.set(v.module, v);
    const used = [...new Set(plan.uses.map((u) => (u as { module?: string }).module))].map((m) => viewers.get(m ?? "")).filter((v): v is LoadedViewer => !!v);
    let host: CompositionHost | undefined;
    const shown = this.ui.showComposition(panel, e.label, used, (m) => void host?.onMessage(m));
    // the frame imports each module from where the host serves its checked tree
    plan = { ...plan, uses: plan.uses.map((u) => ({ ...u, module: shown.urls.get((u as { module?: string }).module ?? "") ?? "" })) } as Plan;
    const backend: Backend = {
      query: (name, bind) => this.query(name, bind),
      bytes: (dir, hash) => this.t.packageBlob(dir, hash),
      open: (target) => this.path.open(target),
      act: (c, about, fields, title) => this.path.act(c, about, fields, title),
      run: async (ray, holes) => {
        const out = await this.t.workflowRun({ workflow: ray, bind: holes });
        if (!out.ok) throw new Error(`the cockpit refused ${ray}: ${out.refusal.reason}`);
        return `ran ${ray}`;
      },
    };
    host = new CompositionHost(plan, loaded, backend, { post: shown.post, log: (l) => this.ui.log(l) });
    this.hosts.set(panel, { host, package: e.package });
    return host;
  }

  /** The host of an open panel (tests, and a panel's messages). */
  hostOf(key: string): CompositionHost | undefined {
    return this.hosts.get(`${PANEL}${key}`)?.host;
  }
}
