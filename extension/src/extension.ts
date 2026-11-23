// The VS Code adapter: implements UiHost and activates the workbench. Kept thin; the behaviour is in
// src/ui/controller.ts and tested without VS Code. Ids live under `ktonWorkbench.` so the recorder
// extension (its own `kton.` commands and `kton` view container) can run beside it.

import { SupportWriter } from "./support/view-now.js";
import { AskGate, gated } from "./transport/gate.js";
import { indexStatusText } from "./ui/index-status.js";
import * as vscode from "vscode";
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { inside, writeTree } from "./packages/store.js";
import { approvalStaleness, readAtCommit, sourceUri, SRC_SCHEME, targetOf, type SourceTarget } from "./ui/sources.js";
import { ART_SCHEME, artifactOf, artifactUri, readArtifact, showAs, THUMB_MAX_BYTES, type ArtifactTarget } from "./ui/artifacts.js";

const artifactsDir = (): string => vscode.workspace.getConfiguration("kton").get<string>("artifacts.dir") ?? "";
const sourcesDir = (): string => vscode.workspace.getConfiguration("kton").get<string>("sources.dir") ?? "";
/** kton.sources.aliases: a data repository name → its mirror folder's name, where the two differ */
const sourceAliases = (): Record<string, string> => {
  const a = vscode.workspace.getConfiguration("kton").get<Record<string, unknown>>("sources.aliases") ?? {};
  return Object.fromEntries(Object.entries(a).filter((e): e is [string, string] => typeof e[1] === "string"));
};

/** Run artifacts, read-only: every read checks the bytes against the hash the URI carries (from the claim). */
class ArtifactFs implements vscode.FileSystemProvider {
  private readonly emitter = new vscode.EventEmitter<vscode.FileChangeEvent[]>();
  readonly onDidChangeFile = this.emitter.event;
  private async bytes(uri: vscode.Uri): Promise<Uint8Array> {
    const t = artifactOf({ path: uri.path, query: uri.query });
    if (typeof t === "string") throw vscode.FileSystemError.FileNotFound(`not shown: ${t}`);
    const r = await readArtifact(artifactsDir(), t);
    if (!("bytes" in r)) throw vscode.FileSystemError.NoPermissions(r.why);
    return r.bytes;
  }
  async stat(uri: vscode.Uri): Promise<vscode.FileStat> {
    const b = await this.bytes(uri);
    return { type: vscode.FileType.File, ctime: 0, mtime: 0, size: b.byteLength, permissions: vscode.FilePermission.Readonly };
  }
  readFile(uri: vscode.Uri): Promise<Uint8Array> {
    return this.bytes(uri);
  }
  watch(): vscode.Disposable {
    return new vscode.Disposable(() => undefined);
  }
  readDirectory(): never {
    throw vscode.FileSystemError.NoPermissions("read-only");
  }
  createDirectory(): never {
    throw vscode.FileSystemError.NoPermissions("read-only");
  }
  writeFile(): never {
    throw vscode.FileSystemError.NoPermissions("read-only");
  }
  delete(): never {
    throw vscode.FileSystemError.NoPermissions("read-only");
  }
  rename(): never {
    throw vscode.FileSystemError.NoPermissions("read-only");
  }
}

const STALE_TITLE = " (stale: the last poll failed)";
import { compositionPage, leafPage } from "./composition/page.js";
import { loadVendored } from "./packages/vendored.js";
import { loadBuiltin } from "./composition/qm-area.js";
import { startMermaidServer } from "./composition/mermaid-server.js";
import type { Subject } from "./composition/slots.js";
import type { LeafViewer, LoadedViewer, MirrorPackage, ViewerTree } from "./packages/model.js";
import { CliTransport } from "./transport/cli.js";
import { idlePolicy } from "./poll.js";
import { StdioTransport } from "./transport/stdio.js";
import { claimKeyId, readIdentityHint, type WindowIdentity } from "./identity.js";
import type { DoctorReport } from "./transport/port.js";
import { loadQueries } from "./ui/queries.js";
import { loadSurfaces } from "./ui/surfaces.js";
import { CMD, DEFAULT_CONFIG, Workbench } from "./ui/controller.js";
import { artifactImagePage, signDialogPage } from "./ui/pages.js";
import { workbenchCommands } from "./ui/commands.js";
import type { StatusId, TreeNode, UiHost, ViewId } from "./ui/host.js";

class Tree implements vscode.TreeDataProvider<TreeNode> {
  private nodes: readonly TreeNode[] = [];
  private shown = "";
  private readonly changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  set(nodes: readonly TreeNode[]): void {
    // A poll that changed nothing does not redraw: a redraw between mouse-down and mouse-up loses the
    // click (and the view flickers every interval).
    const next = JSON.stringify(nodes);
    if (next === this.shown) return;
    this.shown = next;
    this.nodes = nodes;
    this.changed.fire();
  }
  getChildren(n?: TreeNode): TreeNode[] {
    return [...(n ? (n.children ?? []) : this.nodes)];
  }
  getTreeItem(n: TreeNode): vscode.TreeItem {
    const it = new vscode.TreeItem(n.label, n.children && n.children.length ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.None);
    it.id = n.id;
    if (n.description !== undefined) it.description = n.description;
    if (n.tooltip !== undefined) it.tooltip = n.tooltip;
    if (n.disabled) {
      it.description = n.description ?? "disabled";
      it.iconPath = new vscode.ThemeIcon("circle-slash");
    } else if (n.command) {
      it.command = { command: n.command.id, title: n.label, arguments: [...n.command.args] };
    }
    return it;
  }
}

class VsHost implements UiHost {
  approvalStaleness(repo: string, approved: string, head: string, paths: readonly string[]) {
    return approvalStaleness(sourcesDir(), repo, approved, head, paths, sourceAliases());
  }
  async openSource(t: SourceTarget): Promise<string> {
    const r = await readAtCommit(sourcesDir(), t, sourceAliases());
    if (!("bytes" in r)) return r.why;
    const u = sourceUri(t);
    const doc = await vscode.workspace.openTextDocument(vscode.Uri.from({ scheme: SRC_SCHEME, path: u.path, query: u.query, fragment: u.fragment }));
    const line = Math.max(0, (t.line ?? 1) - 1);
    // beside the view, without taking the focus: the decision view stays open (its session's looked-at list)
    await vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Beside, preserveFocus: true, selection: new vscode.Range(line, 0, line, 0) });
    if (t.diff) return `Opened the change of ${t.repo} ${t.commit.slice(0, 12)}${t.path ? ` to ${t.path}` : ""} (read-only)`;
    return `Opened ${t.repo}:${t.path}${t.line ? `#L${t.line}` : ""} at ${t.commit.slice(0, 12)} (read-only)`;
  }
  /** A thumbnail: the artifact's bytes as a data URL, only after they hash to its sha256, and only up to THUMB_MAX_BYTES. */
  async artifactThumb(t: ArtifactTarget): Promise<string | undefined> {
    if (!artifactsDir()) return undefined;
    const r = await readArtifact(artifactsDir(), t);
    if (!("bytes" in r) || r.bytes.byteLength > THUMB_MAX_BYTES || !/^image\/(png|jpeg|gif|webp)$/.test(t.mediaType ?? "")) return undefined;
    return `data:${t.mediaType};base64,${r.bytes.toString("base64")}`;
  }
  /** A run artifact: checked against its sha256 before anything is shown; an image in the preview, text read-only. */
  async openArtifact(t: ArtifactTarget): Promise<string> {
    const r = await readArtifact(artifactsDir(), t);
    if (!("bytes" in r)) {
      this.out.appendLine(`run artifact not shown: ${r.why}`);
      return r.why;
    }
    const u = artifactUri(t);
    const uri = vscode.Uri.from({ scheme: ART_SCHEME, path: u.path, query: u.query });
    const what = `${t.run}/${t.name} (${t.sha256.slice(0, 19)}…, ${r.bytes.byteLength} bytes, hash checked)`;
    const how = showAs(t);
    if (how === "image") {
      this.showPanel(`artifact:${t.sha256}`, t.name.split("/").pop() ?? t.name, artifactImagePage(`${t.run}: ${t.name.split("/").pop() ?? t.name}`, what, t.mediaType ?? "image/png", r.bytes.toString("base64"), randomBytes(16).toString("base64")), undefined, true, vscode.ViewColumn.Beside);
      return `Opened ${what}`;
    }
    if (how === "text") {
      const doc = await vscode.workspace.openTextDocument(uri);
      await vscode.window.showTextDocument(doc, { preview: true, viewColumn: vscode.ViewColumn.Beside, preserveFocus: true });
      return `Opened ${what}`;
    }
    // a trace or a video: no viewer here; its hash and size, and where a trace viewer can open it
    const text = `Shown ${what}: open it with the Playwright trace viewer (npx playwright show-trace <file>) from ${artifactsDir()}/${t.run}/${t.name}`;
    void vscode.window.showInformationMessage(text);
    return text;
  }
  private readonly status = new Map<StatusId, vscode.StatusBarItem>();
  private readonly panels = new Map<string, vscode.WebviewPanel>();
  /** The latest handler per panel: a rebuilt workbench (changed setting) takes over open panels. */
  private readonly handlers = new Map<string, (msg: unknown) => void>();
  readonly trees: Record<ViewId, Tree> = { home: new Tree(), inbox: new Tree(), lanes: new Tree() };
  readonly out = vscode.window.createOutputChannel("kton cockpit workbench");

  /** The window's ask queue on screen: a status bar item and the views' banner, refreshed each second while it builds. */
  private indexTimer: ReturnType<typeof setInterval> | undefined;
  private indexGate: AskGate | undefined;
  watchIndex(gate: AskGate): void {
    this.indexGate = gate;
    gate.onChange(() => this.showIndex());
    if (!this.indexTimer) {
      this.indexTimer = setInterval(() => this.showIndex(true), 2000);
      this.ctx.subscriptions.push({ dispose: () => this.indexTimer && clearInterval(this.indexTimer) });
    }
    this.showIndex();
  }
  private lastIndexText = "";
  private showIndex(tick = false): void {
    const g = this.indexGate;
    if (!g) return;
    const st = g.state();
    const t = indexStatusText(st, Date.now());
    const s = this.status.get("index");
    if (s) {
      if (t) {
        s.text = t.text;
        s.tooltip = t.tooltip;
        s.show();
      } else s.hide();
    }
    // the views: on a change, and each tick while something waits (a panel opened since gets it too)
    const text = JSON.stringify(st);
    if (!tick || st.buildingSince !== undefined || st.queued > 0 || text !== this.lastIndexText) {
      this.lastIndexText = text;
      for (const p of this.panels.values()) void p.webview.postMessage({ type: "kton-workbench:index", state: st });
    }
  }

  /** the Mermaid frame's URL as the webview reaches it (asExternalUri of 127.0.0.1), once the server runs */
  mermaidFrame: string | undefined;
  /** per-window view state (CompositionUi.memo): VS Code's workspace state of this window, never a registry */
  readonly memo = {
    get: (key: string): unknown => this.ctx.workspaceState.get(key),
    set: (key: string, value: unknown): void => void this.ctx.workspaceState.update(key, value),
  };
  /** the support file (kton.support.viewFile): the active composition panel's view, debounced; empty setting: none */
  private readonly supportWriter = new SupportWriter(() => vscode.workspace.getConfiguration("kton").get<string>("support.viewFile") ?? "");
  private readonly supportBuild = new Map<string, () => Record<string, unknown>>();
  private supportActive = "";
  support(key: string, build: () => Record<string, unknown>): void {
    this.supportBuild.set(key, build);
    if (!this.supportActive || this.supportActive === key) {
      this.supportActive = key;
      this.supportWriter.changed(build);
    }
  }
  constructor(private readonly ctx: vscode.ExtensionContext) {
    const order: StatusId[] = ["surface", "identity", "mode", "decisions", "poll", "index", "slots"];
    order.forEach((id, i) => {
      const s = vscode.window.createStatusBarItem(`ktonWorkbench.${id}`, vscode.StatusBarAlignment.Left, 100 - i);
      s.name = `kton workbench ${id}`;
      ctx.subscriptions.push(s);
      this.status.set(id, s);
    });
    ctx.subscriptions.push(this.out);
  }
  setStatus(id: StatusId, text: string, tooltip?: string, command?: string): void {
    const s = this.status.get(id);
    if (!s) return;
    s.text = text;
    s.tooltip = tooltip;
    s.command = command;
    s.show();
  }
  setTree(view: ViewId, nodes: readonly TreeNode[]): void {
    this.trees[view].set(nodes);
  }
  openPanels(): string[] {
    return [...this.panels.keys()];
  }
  showPanel(key: string, title: string, html: string, onMessage?: (msg: unknown) => void, reveal = true, column: vscode.ViewColumn = vscode.ViewColumn.Active): void {
    if (onMessage) this.handlers.set(key, onMessage);
    else this.handlers.delete(key);
    let p = this.panels.get(key);
    if (!p) {
      p = vscode.window.createWebviewPanel("ktonWorkbench.panel", title, { viewColumn: column, preserveFocus: column === vscode.ViewColumn.Beside }, { enableScripts: !!onMessage, localResourceRoots: [] });
      const panel = p;
      this.panels.set(key, panel);
      panel.onDidDispose(() => {
        this.panels.delete(key);
        this.handlers.delete(key);
      });
      panel.webview.onDidReceiveMessage((m: unknown) => this.handlers.get(key)?.(m));
    }
    p.title = title;
    p.webview.html = html;
    if (reveal) p.reveal(undefined, column === vscode.ViewColumn.Beside);
  }
  /**
   * A composition panel: the vendored kton-web frame, a CSP the host owns, and exactly the viewer trees the
   * plan uses, written (already checked against their tree ids) to a folder named by tree id.
   */
  showComposition(key: string, title: string, viewers: readonly LoadedViewer[], onMessage: (msg: unknown) => void): { post: (msg: unknown) => void; urls: ReadonlyMap<string, string> } {
    const kit = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "kton-web");
    const design = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "workbench", "design");
    const store = vscode.Uri.joinPath(this.ctx.globalStorageUri, "viewers");
    // the trees first, each path checked to stay under its tree's folder (F1 on #174): a refusal opens no panel
    const written = viewers.map((v) => {
      const dir = join(store.fsPath, v.tree.replace(/^sha256:/, ""));
      writeTree(dir, v.files);
      return { v, module: inside(dir, String(v.decl["module"])) };
    });
    this.handlers.set(key, onMessage);
    let p = this.panels.get(key);
    if (!p) {
      p = vscode.window.createWebviewPanel("ktonWorkbench.composition", title, vscode.ViewColumn.Active, { enableScripts: true, retainContextWhenHidden: false, localResourceRoots: [kit, store, design] });
      const panel = p;
      this.panels.set(key, panel);
      panel.onDidDispose(() => {
        this.panels.delete(key);
        this.handlers.delete(key);
        this.supportBuild.delete(key);
        if (this.supportActive === key) this.supportActive = "";
      });
      // the support file follows the panel in front
      panel.onDidChangeViewState((e) => {
        if (!e.webviewPanel.active) return;
        this.supportActive = key;
        const b = this.supportBuild.get(key);
        if (b) this.supportWriter.changed(b);
      });
      panel.webview.onDidReceiveMessage((m: unknown) => this.handlers.get(key)?.(m));
    }
    const webview = p.webview;
    const urls = new Map<string, string>();
    for (const { v, module } of written) urls.set(v.module, webview.asWebviewUri(vscode.Uri.file(module)).toString());
    p.title = title;
    webview.html = compositionPage(title, randomBytes(16).toString("base64url"), {
      cspSource: webview.cspSource,
      frame: webview.asWebviewUri(vscode.Uri.joinPath(kit, "viewers", "compose", "frame.js")).toString(),
      css: webview.asWebviewUri(vscode.Uri.joinPath(kit, "core", "compose.css")).toString(),
      design: { css: webview.asWebviewUri(vscode.Uri.joinPath(design, "workbench.css")).toString(), js: webview.asWebviewUri(vscode.Uri.joinPath(design, "frame-design.js")).toString() },
      ...(this.mermaidFrame ? { mermaidFrame: this.mermaidFrame } : {}),
    });
    p.reveal();
    const panel = p;
    return { post: (m) => void panel.webview.postMessage(m), urls };
  }
  /** A leaf: the vendored viewer's own page (its tree was checked against the package's pin at load). */
  showLeaf(key: string, title: string, leaf: LeafViewer, onMessage: (msg: unknown) => void): { post: (msg: unknown) => void } {
    const kit = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "kton-web");
    this.handlers.set(key, onMessage);
    let p = this.panels.get(key);
    if (!p) {
      p = vscode.window.createWebviewPanel("ktonWorkbench.leaf", title, vscode.ViewColumn.Active, { enableScripts: true, localResourceRoots: [kit] });
      const panel = p;
      this.panels.set(key, panel);
      panel.onDidDispose(() => {
        this.panels.delete(key);
        this.handlers.delete(key);
      });
      panel.webview.onDidReceiveMessage((m: unknown) => this.handlers.get(key)?.(m));
    }
    const dir = vscode.Uri.joinPath(kit, ...leaf.dir.replace(/^kton-web\//, "").split("/"));
    const html = readFileSync(vscode.Uri.joinPath(dir, "index.html").fsPath, "utf8");
    p.title = title;
    p.webview.html = leafPage(html, randomBytes(16).toString("base64url"), p.webview.cspSource, `${p.webview.asWebviewUri(dir).toString()}/`);
    p.reveal();
    const panel = p;
    return { post: (m) => void panel.webview.postMessage(m) };
  }
  /** The open sign dialog after its Sign was pressed: it shows "Signing…" and then the outcome. */
  private signing: vscode.WebviewPanel | undefined;
  signDialog(title: string, detail: string, person: string, view?: Parameters<UiHost["signDialog"]>[3]): Promise<boolean> {
    this.signing?.dispose();
    this.signing = undefined;
    return new Promise((resolve) => {
      const panel = vscode.window.createWebviewPanel("ktonWorkbench.sign", title, vscode.ViewColumn.Active, { enableScripts: true, localResourceRoots: [] });
      let done = false;
      panel.webview.onDidReceiveMessage((m: unknown) => {
        if (done) return;
        const o = m as { type?: unknown; confirmed?: unknown };
        done = true;
        if (o?.type === "sign" && o.confirmed === true) {
          // Signed only after this: the panel stays to show the outcome (S7.3).
          this.signing = panel;
          resolve(true);
        } else {
          resolve(false);
          panel.dispose();
        }
      });
      panel.onDidDispose(() => {
        if (this.signing === panel) this.signing = undefined;
        if (!done) {
          done = true;
          resolve(false);
        }
      });
      panel.webview.html = signDialogPage(title, detail, person, randomBytes(16).toString("base64url"), view);
    });
  }
  signOutcome(ok: boolean, text: string): void {
    void this.signing?.webview.postMessage({ type: "outcome", ok, text });
  }
  closePanel(key: string): void {
    this.panels.get(key)?.dispose();
  }
  /** A stale panel says so in its title (the last ui list poll failed); it stays open and usable. */
  markStale(key: string, stale: boolean): void {
    const p = this.panels.get(key);
    if (!p) return;
    const base = p.title.endsWith(STALE_TITLE) ? p.title.slice(0, -STALE_TITLE.length) : p.title;
    p.title = stale ? `${base}${STALE_TITLE}` : base;
  }
  pick(title: string, items: readonly string[]): Promise<string | undefined> {
    return Promise.resolve(vscode.window.showQuickPick([...items], { title }));
  }
  input(prompt: string, value?: string): Promise<string | undefined> {
    return Promise.resolve(vscode.window.showInputBox(value === undefined ? { prompt } : { prompt, value }));
  }
  info(text: string): void {
    void vscode.window.showInformationMessage(text);
  }
  error(text: string): void {
    void vscode.window.showErrorMessage(text);
  }
  log(line: string): void {
    this.out.appendLine(`${new Date().toISOString()} ${line}`);
  }
  async openWindow(folder: string): Promise<void> {
    await vscode.commands.executeCommand("vscode.openFolder", vscode.Uri.file(folder), { forceNewWindow: true });
  }
  async runExternal(extensionId: string, command: string): Promise<boolean> {
    if (!vscode.extensions.getExtension(extensionId)) return false;
    await vscode.commands.executeCommand(command);
    return true;
  }
}

let wb: Workbench | undefined;
let stdio: StdioTransport | undefined;
/** The viewers vendored from kton-web, read and tree-hashed once (src/packages/vendored.ts). */
let vendored: ReadonlyMap<string, ViewerTree> = new Map();
let builtin: MirrorPackage | undefined;
/** Builds are numbered: a setting changed twice in a row starts two, and only the last may run. */
let generation = 0;

async function build(host: VsHost): Promise<void> {
  const mine = ++generation;
  wb?.stop();
  wb = undefined;
  stdio?.dispose();
  stdio = undefined;
  const folder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
  const cfg = vscode.workspace.getConfiguration("kton");
  if (!folder) {
    host.setStatus("identity", "kton: open a folder");
    return;
  }
  const hint = await readIdentityHint(folder);
  // Keys come only from the settings: a missing one stays missing (no fallback to the other key or to
  // the identity file's name).
  const personKey = cfg.get<string>("identity.personKey") || "";
  const agentKey = cfg.get<string>("identity.agentKey") || "";
  const executable = cfg.get<string>("cockpit.path") || "cockpit";
  const timeout = Math.max(1, cfg.get<number>("cockpit.timeoutSeconds") ?? 120);
  // the command line (default), or the frontend protocol: cockpit serve --stdio (SPEC §12.5, cockpit v0.5.6)
  const opts = { executable, cwd: folder, timeoutMs: timeout * 1000 };
  // one ask queue per window: at most 2 reads of the cockpit at a time, a cold index awaited by one (transport/gate.ts)
  const gate = new AskGate();
  const transport = gated(cfg.get<string>("cockpit.transport") === "stdio" ? (stdio = new StdioTransport(opts)) : new CliTransport(opts), gate);
  // what the window waits for: the status bar and a banner in every view
  host.watchIndex(gate);
  if (stdio) {
    // kton-review W1 on #196: an answer to a request already failed came after all. A say among them may have
    // signed: name the claim and refresh, so the claim shows (it is never signed again).
    stdio.onLateAnswer = (a) => {
      const claim = (a.result as { claimId?: unknown } | undefined)?.claimId;
      if (a.method === "say" && typeof claim === "string") {
        host.log(`say answered after all (it had failed: ${a.failed}): signed ${claim}`);
        host.info(`The say that failed (${a.failed}) was signed after all: ${claim}`);
        void wb?.refreshNow();
      } else host.log(`${a.method} answered after all (it had failed: ${a.failed})${a.error ? `: error ${String(a.error.code)} ${a.error.message ?? ""}` : ""}`);
    };
  }
  // Each key's id from its public half in the keyring (never a label), checked against doctor's answer
  // when doctor speaks for the same key. Without a person key id only person tiers count as persons.
  let doctor: DoctorReport | undefined;
  try {
    doctor = await transport.doctor();
  } catch {
    doctor = undefined;
  }
  const [pk, ak] = await Promise.all([claimKeyId(hint, personKey, doctor), claimKeyId(hint, agentKey, doctor)]);
  for (const [role, name, r] of [["person", personKey, pk], ["agent", agentKey, ak]] as const) {
    if (!name) continue;
    host.log(r.ok ? `${role} key ${name}: key id ${r.keyId} from ${name}-claims.pub${r.checkedByDoctor ? ", agrees with doctor" : ""}` : `${role} key ${name}: no key id (${r.why})`);
    if (!r.ok && role === "person" && hint) host.error(`The person key's id is unknown (${r.why}): only signers in kton.identity.personTiers count as persons here.`);
  }
  const identity: WindowIdentity = {
    hintName: hint?.sessionId ?? "",
    person: { role: "person", keyName: personKey, keyId: pk.ok ? pk.keyId : "" },
    agent: { role: "agent", keyName: agentKey, keyId: ak.ok ? ak.keyId : "" },
  };
  const queries = await loadQueries(cfg.get<string>("queries.dir") || `${folder}/.kton/queries`);
  const surfaces = await loadSurfaces(cfg.get<string>("surfaces.file") || undefined);
  const pattern = cfg.get<string>("surfaces.folderPattern");
  const initial = cfg.get<string>("surfaces.current");
  // A newer build started while this one read its files: it owns the window (one poller, C1.6).
  if (mine !== generation) return;
  wb = new Workbench(transport, host, queries, identity, {
    ...DEFAULT_CONFIG,
    personTiers: cfg.get<string[]>("identity.personTiers") ?? [],
    inboxAreas: (cfg.get<string[]>("inbox.areas") ?? []).filter((x) => typeof x === "string" && x !== ""),
    relayKeys: (cfg.get<string[]>("inbox.relayKeys") ?? []).filter((x) => typeof x === "string" && /^[0-9a-f]{16}$/.test(x)),
    testKeys: cfg.get<boolean>("identity.testKeys") ?? true,
    vendoredViewers: vendored,
    ...(builtin ? { builtinPackage: builtin } : {}),
    pollSeconds: Math.max(1, cfg.get<number>("poll.seconds") ?? 30),
    idle: idlePolicy(cfg.get<number>("poll.idleAfterMinutes") ?? 5, cfg.get<number>("poll.pauseAfterMinutes") ?? 30),
    // once per window: a rebuild after a changed setting does not open it again
    openOnStart: firstBuild && (cfg.get<boolean>("home.openOnStart") ?? true),
    cockpitLabel: executable.split(/[\\/]/).pop() ?? executable,
    surfaces,
    ...(pattern ? { surfaceFolderPattern: pattern } : {}),
    ...(initial ? { initialSurface: initial } : {}),
  });
  firstBuild = false;
  await wb.start(host.openPanels());
  // the idle backoff: a window without the focus slows its polls, then stops them (a tab left open polls for nobody)
  wb.setFocused(vscode.window.state.focused);
}
/** the window's first workbench (kton.home.openOnStart applies to it only) */
let firstBuild = true;

export async function activate(ctx: vscode.ExtensionContext): Promise<void> {
  vendored = loadVendored(join(ctx.extensionUri.fsPath, "media", "kton-web"));
  builtin = loadBuiltin(join(ctx.extensionUri.fsPath, "media", "workbench"));
  const host = new VsHost(ctx);
  // H13: the sandboxed Mermaid frame from 127.0.0.1, reached through asExternalUri; without it diagrams say why
  try {
    const m = await startMermaidServer(join(ctx.extensionUri.fsPath, "media", "kton-web"));
    ctx.subscriptions.push({ dispose: () => m.close() });
    // map the server's root (code-server maps a port to its /proxy/<port>/ path), then name the frame below it
    const root = (await vscode.env.asExternalUri(vscode.Uri.parse(`http://127.0.0.1:${m.port}/`))).toString(true);
    host.mermaidFrame = new URL("mermaid/frame.html", root.endsWith("/") ? root : `${root}/`).toString();
    host.out.appendLine(`the Mermaid frame: ${host.mermaidFrame}`);
  } catch (e) {
    host.out.appendLine(`the Mermaid frame is not served: ${(e as Error).message}`);
  }
  ctx.subscriptions.push(
    vscode.window.registerTreeDataProvider("ktonWorkbench.home", host.trees.home),
    // evidence on demand: files as they were at a commit, from the local mirrors, read-only (no worktree)
    vscode.workspace.registerTextDocumentContentProvider(SRC_SCHEME, {
      provideTextDocumentContent: async (uri) => {
        const t = targetOf({ path: uri.path, query: uri.query, fragment: uri.fragment });
        if (typeof t === "string") return `not shown: ${t}`;
        const r = await readAtCommit(sourcesDir(), t, sourceAliases());
        return "bytes" in r ? r.bytes.toString("utf8") : r.why;
      },
    }),
    // run artifacts: read-only, each read checked against the claim's sha256
    vscode.workspace.registerFileSystemProvider(ART_SCHEME, new ArtifactFs(), { isReadonly: true, isCaseSensitive: true }),
    vscode.window.registerTreeDataProvider("ktonWorkbench.inbox", host.trees.inbox),
    vscode.window.registerTreeDataProvider("ktonWorkbench.lanes", host.trees.lanes),
    ...workbenchCommands(() => wb).map(([id, run]) => vscode.commands.registerCommand(id, run as (...args: unknown[]) => unknown)),
    vscode.window.onDidChangeWindowState((st) => wb?.setFocused(st.focused)),
    // A changed setting (cockpit path, queries, surfaces, keys) rebuilds the workbench in place.
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("kton")) void build(host);
    }),
  );
  await build(host);
}

export function deactivate(): void {
  wb?.stop();
  stdio?.dispose();
}
