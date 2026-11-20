// The VS Code adapter: implements UiHost and activates the workbench. Kept thin; the behaviour is in
// src/ui/controller.ts and tested without VS Code. Ids live under `ktonWorkbench.` so the recorder
// extension (its own `kton.` commands and `kton` view container) can run beside it.

import * as vscode from "vscode";
import { randomBytes } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { inside, writeTree } from "./packages/store.js";

const STALE_TITLE = " (stale: the last poll failed)";
import { compositionPage, leafPage } from "./composition/page.js";
import { loadVendored } from "./packages/vendored.js";
import { loadBuiltin } from "./composition/qm-area.js";
import { startMermaidServer } from "./composition/mermaid-server.js";
import type { Subject } from "./composition/slots.js";
import type { LeafViewer, LoadedViewer, MirrorPackage, ViewerTree } from "./packages/model.js";
import { CliTransport } from "./transport/cli.js";
import { StdioTransport } from "./transport/stdio.js";
import { claimKeyId, readIdentityHint, type WindowIdentity } from "./identity.js";
import type { DoctorReport } from "./transport/port.js";
import { loadQueries } from "./ui/queries.js";
import { loadSurfaces } from "./ui/surfaces.js";
import { CMD, DEFAULT_CONFIG, Workbench } from "./ui/controller.js";
import { signDialogPage } from "./ui/pages.js";
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
  private readonly status = new Map<StatusId, vscode.StatusBarItem>();
  private readonly panels = new Map<string, vscode.WebviewPanel>();
  /** The latest handler per panel: a rebuilt workbench (changed setting) takes over open panels. */
  private readonly handlers = new Map<string, (msg: unknown) => void>();
  readonly trees: Record<ViewId, Tree> = { home: new Tree(), inbox: new Tree(), lanes: new Tree() };
  readonly out = vscode.window.createOutputChannel("kton cockpit workbench");

  /** the Mermaid frame's URL as the webview reaches it (asExternalUri of 127.0.0.1), once the server runs */
  mermaidFrame: string | undefined;
  constructor(private readonly ctx: vscode.ExtensionContext) {
    const order: StatusId[] = ["surface", "identity", "mode", "poll", "slots"];
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
  showPanel(key: string, title: string, html: string, onMessage?: (msg: unknown) => void, reveal = true): void {
    if (onMessage) this.handlers.set(key, onMessage);
    else this.handlers.delete(key);
    let p = this.panels.get(key);
    if (!p) {
      p = vscode.window.createWebviewPanel("ktonWorkbench.panel", title, vscode.ViewColumn.Active, { enableScripts: !!onMessage, localResourceRoots: [] });
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
    if (reveal) p.reveal();
  }
  /**
   * A composition panel: the vendored kton-web frame, a CSP the host owns, and exactly the viewer trees the
   * plan uses, written (already checked against their tree ids) to a folder named by tree id.
   */
  showComposition(key: string, title: string, viewers: readonly LoadedViewer[], onMessage: (msg: unknown) => void): { post: (msg: unknown) => void; urls: ReadonlyMap<string, string> } {
    const kit = vscode.Uri.joinPath(this.ctx.extensionUri, "media", "kton-web");
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
      p = vscode.window.createWebviewPanel("ktonWorkbench.composition", title, vscode.ViewColumn.Active, { enableScripts: true, retainContextWhenHidden: false, localResourceRoots: [kit, store] });
      const panel = p;
      this.panels.set(key, panel);
      panel.onDidDispose(() => {
        this.panels.delete(key);
        this.handlers.delete(key);
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
  signDialog(title: string, detail: string, person: string): Promise<boolean> {
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
      panel.webview.html = signDialogPage(title, detail, person, randomBytes(16).toString("base64url"));
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
  const transport = cfg.get<string>("cockpit.transport") === "stdio" ? (stdio = new StdioTransport(opts)) : new CliTransport(opts);
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
    vendoredViewers: vendored,
    ...(builtin ? { builtinPackage: builtin } : {}),
    pollSeconds: Math.max(1, cfg.get<number>("poll.seconds") ?? 30),
    cockpitLabel: executable.split(/[\\/]/).pop() ?? executable,
    surfaces,
    ...(pattern ? { surfaceFolderPattern: pattern } : {}),
    ...(initial ? { initialSurface: initial } : {}),
  });
  await wb.start(host.openPanels());
}

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
    vscode.window.registerTreeDataProvider("ktonWorkbench.inbox", host.trees.inbox),
    vscode.window.registerTreeDataProvider("ktonWorkbench.lanes", host.trees.lanes),
    ...workbenchCommands(() => wb).map(([id, run]) => vscode.commands.registerCommand(id, run as (...args: unknown[]) => unknown)),
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
