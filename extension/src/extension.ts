// The VS Code adapter: implements UiHost and activates the workbench. Kept thin; the behaviour is in
// src/ui/controller.ts and tested without VS Code. Ids live under `ktonWorkbench.` so the recorder
// extension (its own `kton.` commands and `kton` view container) can run beside it.

import * as vscode from "vscode";
import { randomBytes } from "node:crypto";
import { CliTransport } from "./transport/cli.js";
import { readIdentityHint, type WindowIdentity } from "./identity.js";
import { loadQueries } from "./ui/queries.js";
import { loadSurfaces } from "./ui/surfaces.js";
import { CMD, DEFAULT_CONFIG, Workbench } from "./ui/controller.js";
import { signDialogPage } from "./ui/pages.js";
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

  constructor(ctx: vscode.ExtensionContext) {
    const order: StatusId[] = ["surface", "identity", "mode", "poll"];
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
/** Builds are numbered: a setting changed twice in a row starts two, and only the last may run. */
let generation = 0;

async function build(host: VsHost): Promise<void> {
  const mine = ++generation;
  wb?.stop();
  wb = undefined;
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
  const identity: WindowIdentity = {
    hintName: hint?.sessionId ?? "",
    person: { role: "person", keyName: personKey, keyId: personKey },
    agent: { role: "agent", keyName: agentKey, keyId: agentKey },
  };
  const executable = cfg.get<string>("cockpit.path") || "cockpit";
  const timeout = Math.max(1, cfg.get<number>("cockpit.timeoutSeconds") ?? 120);
  const transport = new CliTransport({ executable, cwd: folder, timeoutMs: timeout * 1000 });
  const queries = await loadQueries(cfg.get<string>("queries.dir") || `${folder}/.kton/queries`);
  const surfaces = await loadSurfaces(cfg.get<string>("surfaces.file") || undefined);
  const pattern = cfg.get<string>("surfaces.folderPattern");
  const initial = cfg.get<string>("surfaces.current");
  // A newer build started while this one read its files: it owns the window (one poller, C1.6).
  if (mine !== generation) return;
  wb = new Workbench(transport, host, queries, identity, {
    ...DEFAULT_CONFIG,
    personTiers: cfg.get<string[]>("identity.personTiers") ?? [],
    pollSeconds: Math.max(1, cfg.get<number>("poll.seconds") ?? 30),
    cockpitLabel: executable.split(/[\\/]/).pop() ?? executable,
    surfaces,
    ...(pattern ? { surfaceFolderPattern: pattern } : {}),
    ...(initial ? { initialSurface: initial } : {}),
  });
  await wb.start(host.openPanels());
}

export async function activate(ctx: vscode.ExtensionContext): Promise<void> {
  const host = new VsHost(ctx);
  ctx.subscriptions.push(
    vscode.window.registerTreeDataProvider("ktonWorkbench.home", host.trees.home),
    vscode.window.registerTreeDataProvider("ktonWorkbench.inbox", host.trees.inbox),
    vscode.window.registerTreeDataProvider("ktonWorkbench.lanes", host.trees.lanes),
    vscode.commands.registerCommand(CMD.refresh, () => wb?.refresh()),
    vscode.commands.registerCommand(CMD.openDecision, (msg: string) => wb?.openDecision(msg)),
    vscode.commands.registerCommand(CMD.chooseOption, (msg: string, option: string) => wb?.chooseOption(msg, option)),
    vscode.commands.registerCommand(CMD.openThread, (msg: string) => wb?.openThread(msg)),
    vscode.commands.registerCommand(CMD.switchSurface, (name?: string) => wb?.switchSurface(name)),
    vscode.commands.registerCommand(CMD.openSurfaceWindow, (name?: string) => wb?.openSurfaceWindow(name)),
    vscode.commands.registerCommand(CMD.openRecorder, () => wb?.openRecorder()),
    vscode.commands.registerCommand(CMD.ask, () => wb?.askCommand()),
    vscode.commands.registerCommand(CMD.say, () => wb?.sayCommand()),
    vscode.commands.registerCommand(CMD.compose, () => wb?.composeCommand()),
    // A changed setting (cockpit path, queries, surfaces, keys) rebuilds the workbench in place.
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("kton")) void build(host);
    }),
  );
  await build(host);
}

export function deactivate(): void {
  wb?.stop();
}
