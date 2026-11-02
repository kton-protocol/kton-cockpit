// kton workgroup: the surfaces and their background Claude sessions in tmux, as a tree view
// (kton-coordination's networked working, §4.3 and §4.6).
//
// kw (kton-coordination/kw) is the backend: it lists, starts and attaches; nothing here talks to
// tmux except through it, apart from closing a terminal, which detaches. The open messages are the
// package query inbox-open-all over the lanes registry. ktonwatch's /v1/watch says when a record
// arrives there, so nothing polls; each one recounts, and a new message gets a notification with
// [Open] (or opens its lane, for the lanes in ktonWorkgroup.bringForward).
//
// A surface that runs as a Claude conversation of its own (sessions.json, _surface_sessions) opens
// in a VS Code window on its folder: the Claude extension runs every editor tab in the window's
// first workspace folder (setupPanel: cwd = workspaceFolders[0]) and keeps conversations per
// folder, so in any other window it would resume in the wrong folder, with the wrong CLAUDE.md.
// The window this opens finds its folder is a surface's and opens that conversation as a tab.
'use strict';
const vscode = require('vscode');
const cp = require('child_process');
const fs = require('fs');
const http = require('http');
const os = require('os');
const path = require('path');
const m = require('./model');

let cfg = () => vscode.workspace.getConfiguration('ktonWorkgroup');
const home = (p) => p.replace(/^~(?=\/|$)/, os.homedir());
const coord = () => home(cfg().get('coordination'));

function run(cmd, args, opts = {}) {
  return new Promise((resolve) => {
    cp.execFile(cmd, args, { maxBuffer: 64 << 20, ...opts }, (err, stdout, stderr) =>
      resolve({ ok: !err, out: String(stdout), err: String(stderr || (err && err.message) || '') }));
  });
}
const kw = (...args) => run(path.join(coord(), 'kw'), args, { cwd: coord() });

class Tree {
  constructor() {
    this.surfaces = [];
    this.open = {};      // lane → open messages
    this.watch = 'connecting';
    this.terminals = new Map(); // "surface" or "surface/lane" → terminal
    this.tabs = {};             // surface → {session, dir}: surfaces with a conversation of their own
    this._change = new vscode.EventEmitter();
    this.onDidChangeTreeData = this._change.event;
  }
  changed() { this._change.fire(); }

  async refreshState() {
    const r = await kw('list');
    if (r.ok) this.surfaces = m.parseKwList(r.out);
    this.tabs = readSurfaceSessions();
    this.changed();
  }

  // recount runs the package query and returns the messages that are new since the last count.
  async recount() {
    const r = await run('cockpit', ['ask', 'sparql', '--name', 'inbox-open-all', '--results-json'], { cwd: path.join(coord(), 'lanes') });
    if (!r.ok) return [];
    let idx = {};
    try { idx = m.laneIndex(JSON.parse(fs.readFileSync(path.join(coord(), 'surfaces.json'), 'utf8'))); } catch (e) { /* no surfaces */ }
    let now;
    try { now = m.openByLane(JSON.parse(r.out), idx); } catch (e) { return []; }
    const news = this.counted ? m.fresh(this.open, now) : [];
    this.open = now;
    this.counted = true;
    this.changed();
    return news;
  }

  front(key) {
    const t = this.terminals.get(key);
    return t && t.exitStatus === undefined;
  }

  getChildren(el) {
    if (!el) {
      const items = this.surfaces.map((s) => ({ kind: 'surface', s }));
      items.push({ kind: 'feed' });
      if (this.watch !== 'live') items.push({ kind: 'watch' });
      return items;
    }
    if (el.kind === 'surface') return el.s.lanes.map((l) => ({ kind: 'lane', s: el.s, l }));
    return [];
  }

  getTreeItem(el) {
    const n = (lane) => (this.open[lane] || []).length;
    if (el.kind === 'feed') {
      const it = new vscode.TreeItem('Feed (WORKGROUP.md)');
      it.iconPath = new vscode.ThemeIcon('list-flat');
      it.command = { command: 'ktonWorkgroup.feed', title: 'Open the feed' };
      return it;
    }
    if (el.kind === 'watch') {
      const it = new vscode.TreeItem(this.watch === 'connecting' ? 'connecting to ktonwatch …' : 'ktonwatch not reachable: counts refresh on Refresh');
      it.iconPath = new vscode.ThemeIcon(this.watch === 'connecting' ? 'loading~spin' : 'warning');
      it.tooltip = `${cfg().get('ktonwatch')}/v1/watch`;
      return it;
    }
    if (el.kind === 'surface') {
      const total = el.s.lanes.reduce((a, l) => a + n(l.name), 0);
      const it = new vscode.TreeItem(el.s.name, vscode.TreeItemCollapsibleState.Expanded);
      it.iconPath = new vscode.ThemeIcon('server-environment');
      it.description = [el.s.up ? el.s.tmux : 'not started', total ? `${total} open` : ''].filter(Boolean).join(' · ');
      it.contextValue = ['surface', el.s.up ? 'up' : 'down', this.front(el.s.name) ? 'front' : '',
        el.s.lanes.some((l) => l.state === 'none') ? 'startable' : '', this.tabs[el.s.name] ? 'tab' : ''].filter(Boolean).join('-');
      return it;
    }
    const { s, l } = el;
    const k = n(l.name);
    const it = new vscode.TreeItem(l.name);
    const state = { running: '● running', window: '○ window without claude', none: '— not started' }[l.state];
    it.description = [state, k ? `${k} open` : ''].filter(Boolean).join(' · ');
    it.iconPath = new vscode.ThemeIcon(k ? 'mail' : l.state === 'running' ? 'circle-filled' : 'circle-outline');
    it.tooltip = (this.open[l.name] || []).map((x) => `${x.title} (from ${x.from}, ${x.area})`).join('\n') || undefined;
    it.contextValue = ['lane', l.state === 'none' ? 'down' : 'up', this.front(`${s.name}/${l.name}`) || this.front(s.name) ? 'front' : '',
      l.state === 'none' ? 'startable' : ''].filter(Boolean).join('-');
    if (l.state !== 'none') it.command = { command: 'ktonWorkgroup.open', title: 'Open', arguments: [el] };
    return it;
  }
}

function readSurfaceSessions() {
  try { return m.surfaceSessions(JSON.parse(fs.readFileSync(path.join(coord(), 'sessions.json'), 'utf8'))); } catch (e) { return {}; }
}

const firstFolder = () => (vscode.workspace.workspaceFolders || [])[0];
const openTab = (session) => vscode.commands.executeCommand('claude-vscode.editor.open', session);

// openSurface opens a surface's own conversation: here, if this window is on its folder, else in
// a new window on that folder (same remote, so a WSL window opens a WSL window).
async function openSurface(tree, name) {
  const t = (tree.tabs[name] || readSurfaceSessions()[name]);
  if (!t) {
    vscode.window.showWarningMessage(`${name} has no conversation of its own in sessions.json (_surface_sessions)`);
    return;
  }
  const f = firstFolder();
  if (f && f.uri.fsPath.replace(/\/+$/, '') === t.dir) return openTab(t.session);
  const uri = f && f.uri.scheme !== 'file' ? f.uri.with({ path: t.dir }) : vscode.Uri.file(t.dir);
  await vscode.commands.executeCommand('vscode.openFolder', uri, { forceNewWindow: true });
}

// openTerminal brings a surface (or one of its lanes) to the foreground: a terminal attached to
// its tmux session through kw, with the lane's window selected. Several terminals on one surface
// share its current window, so one terminal per surface is kept and the window switched in it.
function openTerminal(tree, s, lane) {
  const key = s.name;
  if (tree.front(key)) {
    const t = tree.terminals.get(key);
    if (lane) cp.execFile('tmux', ['select-window', '-t', `${s.tmux}:${lane}`]);
    t.show();
    return;
  }
  const args = ['attach', s.name].concat(lane ? [lane] : []);
  const t = vscode.window.createTerminal({ name: `kw ${s.name}`, shellPath: path.join(coord(), 'kw'), shellArgs: args, cwd: coord() });
  tree.terminals.set(key, t);
  t.show();
  tree.changed();
}

// watch follows /v1/watch for the lanes registry. Each record recounts (a burst once); a dropped
// stream reconnects with its cursor, so nothing in between is missed.
function watch(tree, onNews, ctx) {
  let cursor = 'end', wait = 2000, timer = null, req = null, stopped = false;
  const recount = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => onNews(await tree.recount()), 800);
  };
  const connect = () => {
    if (stopped) return;
    let token = '';
    try { token = fs.readFileSync(home(cfg().get('tokenFile')), 'utf8').trim(); } catch (e) { /* none */ }
    const u = new URL(`${cfg().get('ktonwatch')}/v1/watch`);
    u.searchParams.set('repo', path.join(coord(), 'lanes'));
    u.searchParams.set('since', cursor);
    const retry = () => {
      req = null;
      if (stopped) return;
      tree.watch = 'down';
      tree.changed();
      setTimeout(connect, wait);
      wait = Math.min(wait * 2, 30000);
    };
    req = http.get(u, { headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' } }, (res) => {
      if (res.statusCode !== 200) { res.resume(); retry(); return; }
      const sse = new m.SSE((name, data) => {
        if (name === 'cursor') {
          try { cursor = JSON.parse(data).cursor; } catch (e) { /* keep */ }
          if (tree.watch !== 'live') { tree.watch = 'live'; wait = 2000; tree.changed(); }
        } else if (name === 'record') {
          recount();
        }
      });
      res.setEncoding('utf8');
      res.on('data', (c) => sse.push(c));
      res.on('end', retry);
      res.on('error', retry);
    });
    req.on('error', retry);
  };
  connect();
  ctx.subscriptions.push({ dispose() { stopped = true; clearTimeout(timer); if (req) req.destroy(); } });
}

function activate(ctx) {
  const tree = new Tree();
  const view = vscode.window.createTreeView('ktonWorkgroup.surfaces', { treeDataProvider: tree });
  ctx.subscriptions.push(view);

  const surfaceOf = (el) => (el && el.s) || tree.surfaces[0];
  const onNews = (news) => {
    const forward = new Set(cfg().get('bringForward') || []);
    for (const x of news) {
      const s = tree.surfaces.find((sf) => sf.lanes.some((l) => l.name === x.lane));
      if (!s) continue;
      if (forward.has(x.lane)) { openTerminal(tree, s, x.lane); continue; }
      vscode.window.showInformationMessage(`${x.lane}: ${x.title} (from ${x.from})`, 'Open').then((a) => {
        if (a === 'Open') openTerminal(tree, s, x.lane);
      });
    }
  };

  ctx.subscriptions.push(
    vscode.commands.registerCommand('ktonWorkgroup.openSurface', (el) => {
      const s = surfaceOf(el);
      if (s) return openSurface(tree, s.name);
    }),
    vscode.commands.registerCommand('ktonWorkgroup.open', (el) => {
      const s = surfaceOf(el);
      if (s) openTerminal(tree, s, el && el.kind === 'lane' ? el.l.name : undefined);
    }),
    vscode.commands.registerCommand('ktonWorkgroup.background', (el) => {
      // closing the terminal ends its tmux client: the session keeps running, detached
      const s = surfaceOf(el);
      const t = s && tree.terminals.get(s.name);
      if (t) t.dispose();
      tree.changed();
    }),
    vscode.commands.registerCommand('ktonWorkgroup.start', async (el) => {
      const s = surfaceOf(el);
      if (!s) return;
      const args = ['start', s.name].concat(el && el.kind === 'lane' ? [el.l.name] : []);
      const r = await kw(...args);
      if (!r.ok) vscode.window.showErrorMessage(`kw ${args.join(' ')}: ${r.err.trim()}`);
      await tree.refreshState();
    }),
    vscode.commands.registerCommand('ktonWorkgroup.refresh', async () => {
      await tree.refreshState();
      await tree.recount();
    }),
    vscode.commands.registerCommand('ktonWorkgroup.feed', () =>
      vscode.window.showTextDocument(vscode.Uri.file(path.join(coord(), 'WORKGROUP.md')))),
    vscode.window.onDidCloseTerminal((t) => {
      for (const [k, v] of tree.terminals) if (v === t) tree.terminals.delete(k);
      tree.changed();
    }),
    // tmux state is local and cheap: read again when the view or the window comes back
    view.onDidChangeVisibility((e) => { if (e.visible) tree.refreshState(); }),
    vscode.window.onDidChangeWindowState((e) => { if (e.focused) tree.refreshState(); }),
  );

  tree.refreshState();
  tree.recount();
  watch(tree, onNews, ctx);

  // a window on a surface's folder: its conversation, as a Claude tab (revealed if already open)
  const f = firstFolder();
  const here = f && m.surfaceOfFolder(readSurfaceSessions(), f.uri.fsPath);
  if (here) openTab(readSurfaceSessions()[here].session).then(undefined, (e) =>
    vscode.window.showWarningMessage(`${here}: could not open its conversation (${e && e.message ? e.message : e})`));
}

function deactivate() {}

module.exports = { activate, deactivate };
