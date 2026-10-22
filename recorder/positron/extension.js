// kton recorder for Positron.
//
//  - writes where each executed piece of R code came from (file, lines, Quarto chunk) next to
//    the recorder's logs; ktonwatch attaches it to the command by time (S6)
//  - a status bar switch: recording on/off for every R session at once (a PAUSED file the shim
//    and ktonrec look at), without restarting anything
//  - a "kton" side bar: the projects and their files (results, what came in, intermediates)
//  - a file's story as an editor tab, step by step from the start up to where kton already has
//    it; the person describes each step, then reproduces it or publishes it from there
//  - the big view (ktonwatch's viewer, started if it is not running) in the browser or a panel
//  - `cockpit publish` runs in a visible terminal; a reproduction runs the recipe in an empty
//    directory
//
// It runs in the remote (WSL) extension host, where R, the logs and ktonwatch live.
const vscode = require('vscode');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');
const crypto = require('crypto');
const { spawn, execFile } = require('child_process');

const cfg = () => vscode.workspace.getConfiguration('kton.recorder');
const home = os.homedir();

function traceDir() {
  const configured = cfg().get('traceDir');
  if (configured) return configured;
  const cache = process.env.XDG_CACHE_HOME || path.join(home, '.cache');
  return path.join(cache, 'kton-cockpit', 'trace');
}
const pauseFile = () => path.join(traceDir(), 'PAUSED');
const viewerUrl = () => (cfg().get('viewerUrl') || 'http://127.0.0.1:8378/').replace(/\/?$/, '/');
const bin = (name) => cfg().get(name + 'Path') || path.join(home, '.local', 'bin', name);
const ktonR = () => cfg().get('ktonRPath') || path.join(home, '.local', 'share', 'kton', 'r', 'kton-R');

let out;      // output channel
let status;   // status bar item
let panel;    // the observations panel

// ------------------------------------------------------------------ origins (S6)
function origin(e) {
  const meta = (e.attribution && e.attribution.metadata) || {};
  const loc = meta.codeLocation;
  const o = {
    ts_ns: Date.now() * 1e6, kind: e.attribution ? e.attribution.source : '',
    language: e.languageId || '', session: e.sessionId || '', execution: e.executionId || '', code: e.code || ''
  };
  if (loc && loc.uri) {
    o.file = loc.uri.fsPath || loc.uri.path;
    const s = loc.range && loc.range.start, en = loc.range && loc.range.end;
    // Positron sends 0:0–0:0 for a Quarto cell: no lines, ktonwatch finds the chunk by its code
    if (s && en && (s.line || en.line || s.character || en.character)) { o.start_line = s.line + 1; o.end_line = en.line + 1; }
  }
  return o;
}

// ------------------------------------------------------------------ recording switch
const recording = () => !fs.existsSync(pauseFile());
function showStatus() {
  const on = recording();
  status.text = on ? '$(record) kton' : '$(circle-slash) kton off';
  status.tooltip = on ? 'kton is recording (click: pause)' : 'kton is paused: nothing is recorded (click: resume)';
  status.backgroundColor = on ? undefined : new vscode.ThemeColor('statusBarItem.warningBackground');
}
function toggleRecording() {
  fs.mkdirSync(traceDir(), { recursive: true });
  if (recording()) fs.writeFileSync(pauseFile(), `paused ${new Date().toISOString()}\n`);
  else fs.rmSync(pauseFile(), { force: true });
  showStatus();
  vscode.window.setStatusBarMessage(recording() ? 'kton: recording' : 'kton: recording paused', 3000);
}

// ------------------------------------------------------------------ ktonwatch
function getJSON(url, timeout) {
  return new Promise((resolve, reject) => {
    const req = http.get(url, { timeout: timeout || 2000 }, (res) => {
      let b = ''; res.on('data', (c) => b += c);
      res.on('end', () => res.statusCode === 200 ? resolve(JSON.parse(b)) : reject(new Error(`${res.statusCode} ${url}`)));
    });
    req.on('timeout', () => req.destroy(new Error('timeout')));
    req.on('error', reject);
  });
}
async function ensureService() {
  try { await getJSON(viewerUrl() + 'api/config'); return true; } catch (e) { /* not running */ }
  const exe = bin('ktonwatch');
  if (!fs.existsSync(exe)) { vscode.window.showErrorMessage(`kton: ${exe} is missing (recorder/install.sh)`); return false; }
  // detached: the service outlives this window; the next one finds it running
  const p = spawn(exe, ['serve'], { detached: true, stdio: 'ignore' });
  p.unref();
  out.appendLine(`ktonwatch serve gestartet (pid ${p.pid})`);
  for (let i = 0; i < 20; i++) {
    await new Promise((r) => setTimeout(r, 250));
    try { await getJSON(viewerUrl() + 'api/config'); return true; } catch (e) { /* not yet */ }
  }
  vscode.window.showErrorMessage('kton: ktonwatch serve does not answer');
  return false;
}

// ------------------------------------------------------------------ the panel
async function openObservations() {
  if (!(await ensureService())) return;
  if (panel) { panel.reveal(); return; }
  const ext = await vscode.env.asExternalUri(vscode.Uri.parse(viewerUrl()));
  const src = ext.toString(true);
  const originOf = new URL(src).origin;
  panel = vscode.window.createWebviewPanel('ktonObservations', 'kton observations', vscode.ViewColumn.Beside,
    { enableScripts: true, retainContextWhenHidden: true });
  const nonce = crypto.randomBytes(16).toString('hex');
  panel.webview.html = `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; frame-src ${originOf}; script-src 'nonce-${nonce}'; style-src 'unsafe-inline';">
<style>html,body,iframe{margin:0;padding:0;border:0;width:100%;height:100%;overflow:hidden}</style></head>
<body><iframe src="${src}"></iframe>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi();
  // the viewer posts {kton:true, action, …}; only from the viewer's own origin
  window.addEventListener('message', (e) => { if (e.origin === ${JSON.stringify(originOf)} && e.data && e.data.kton) vscode.postMessage(e.data); });
</script></body></html>`;
  panel.onDidDispose(() => { panel = undefined; });
  panel.webview.onDidReceiveMessage((m) => {
    if (m.action === 'publish') publish(m.repo, m.text);
    else if (m.action === 'reproduce') reproduce(m.project, m.path, m.sha);
  });
}

// ------------------------------------------------------------------ publish: the verb, by hand, visibly
async function publish(repo, text) {
  const calls = (text.match(/^cockpit publish /gm) || []).length;
  if (!calls) return;
  const ok = await vscode.window.showWarningMessage(
    `Run ${calls} × cockpit publish in ${repo}? That signs, commits and pushes.`, { modal: true }, 'Run');
  if (ok !== 'Run') return;
  const cockpit = bin('cockpit');
  const term = vscode.window.terminals.find((t) => t.name === 'kton cockpit') ||
    vscode.window.createTerminal({ name: 'kton cockpit', cwd: repo });
  term.show();
  term.sendText(`cd ${JSON.stringify(repo)}`);
  for (const line of text.split('\n')) {
    if (line.startsWith('cockpit publish ')) term.sendText(JSON.stringify(cockpit) + line.slice('cockpit'.length));
    else if (line.trim()) term.sendText(line); // comments: shown, ignored by the shell
  }
}

// ------------------------------------------------------------------ reproduce
const sha256File = (p) => new Promise((resolve) => {
  const h = crypto.createHash('sha256'), s = fs.createReadStream(p);
  s.on('data', (c) => h.update(c)); s.on('end', () => resolve(h.digest('hex'))); s.on('error', () => resolve(null));
});
const run = (args, cwd, env) => new Promise((resolve) => {
  out.appendLine(`$ (${cwd}) ${args.join(' ')}`);
  const p = spawn('sh', args, { cwd, env });
  p.stdout.on('data', (d) => out.append(d.toString()));
  p.stderr.on('data', (d) => out.append(d.toString()));
  p.on('close', (code) => resolve(code));
});

// Only what the recipe starts from goes into an empty directory, and the recipe runs there.
// Same bytes: the recipe is complete. Different or missing: something was not recorded, or the
// computation is not deterministic.
// In containers when kton-repro and docker are there: each recorded call in a lean image of its
// own, inputs from the CAS, NONMEM runs with a known key put in place instead of computed.
const dockerUp = () => new Promise((resolve) => execFile('docker', ['version', '--format', '{{.Server.Version}}'], { timeout: 8000 }, (err) => resolve(!err)));

async function reproduceInContainers(project, target, sum) {
  const exe = bin('kton-repro');
  if (!fs.existsSync(exe) || !(await dockerUp())) return false;
  out.show(true);
  out.appendLine(`\n=== Reproduction in containers: ${path.relative(project, target)}`);
  const lines = [];
  const code = await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `kton: reproduziere ${path.basename(target)} in Containern` }, () => new Promise((resolve) => {
    const p = spawn(exe, ['-project', project, '-path', target, '-sha', sum]);
    const take = (d) => { const t = d.toString(); out.append(t); lines.push(...t.split('\n')); };
    p.stdout.on('data', take); p.stderr.on('data', take);
    p.on('close', resolve);
  }));
  const dir = (lines.find((l) => l.startsWith('kton-repro: ') && l.includes(' in ')) || '').split(' in ').pop().trim();
  const verdict = (lines.filter((l) => l.startsWith('= ')).pop() || lines.filter((l) => l.trim()).pop() || '').replace(/^= /, '');
  const show = code === 0 ? vscode.window.showInformationMessage : vscode.window.showWarningMessage;
  const pick = await show(`kton: ${verdict}`, ...(dir ? ['Open folder'] : []), 'Log');
  if (pick === 'Open folder') vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(dir));
  if (pick === 'Log') out.show();
  return true;
}

async function reproduce(project, target, sum) {
  if (await reproduceInContainers(project, target, sum)) return;
  out.show(true);
  let r;
  try {
    r = await getJSON(`${viewerUrl()}api/recipe?project=${encodeURIComponent(project)}&path=${encodeURIComponent(target)}&sha=${encodeURIComponent(sum)}`);
  } catch (e) { vscode.window.showErrorMessage('kton: no recipe for ' + target); return; }
  const rel = (p) => path.relative(project, p);
  const inside = (p) => !rel(p).startsWith('..') && !path.isAbsolute(rel(p));
  const stamp = new Date().toISOString().replace(/[:.]/g, '-');
  const dir = path.join(home, '.cache', 'kton-cockpit', 'repro', `${stamp}-${path.basename(target)}`);
  fs.mkdirSync(dir, { recursive: true });
  out.appendLine(`\n=== Reproduction of ${rel(target)} (sha256 ${sum.slice(0, 12)}) in ${dir}`);
  const notes = [];
  for (const s of r.sources || []) {
    if (!inside(s.path)) { notes.push(`${s.path} is outside and is read from there`); continue; }
    const to = path.join(dir, rel(s.path));
    fs.mkdirSync(path.dirname(to), { recursive: true });
    try { fs.copyFileSync(s.path, to); } catch (e) { notes.push(`${rel(s.path)} is missing: ${e.message}`); continue; }
    if (s.now !== 'same') notes.push(`${rel(s.path)} has other content today than at the time`);
  }
  // the directories the recipe writes into exist in the original too
  for (const st of r.steps || []) for (const o of st.outputs || []) if (inside(o.path)) fs.mkdirSync(path.join(dir, path.dirname(rel(o.path))), { recursive: true });
  for (const n of notes) out.appendLine('  note: ' + n);
  const env = Object.assign({}, process.env, { KTON_TRACE_HASH: dir });
  const v = r.verdict || {};
  const t0 = Date.now();
  await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `kton: reproduziere ${path.basename(target)}` }, async () => {
    if ((v.kind === 'scripts' || v.kind === 'scripts-with-gaps') && (v.scripts || []).length) {
      for (const s of v.scripts) {                       // the scripts you know, one after the other
        const code = await run([ktonR(), '--no-save', '-q', '-f', rel(s.path)], dir, env);
        if (code !== 0) { out.appendLine(`  ${rel(s.path)} exited with ${code}`); break; }
      }
    } else {                                              // the steps as recorded, as one script
      const script = r.script.replace(`setwd(${JSON.stringify(project)})`, `setwd(${JSON.stringify(dir)})`);
      fs.writeFileSync(path.join(dir, 'kton-rezept.R'), script);
      await run([ktonR(), '--no-save', '-q', '-f', 'kton-rezept.R'], dir, env);
    }
  });
  const got = await sha256File(path.join(dir, rel(target)));
  const result = { target: rel(target), recorded: sum, reproduced: got, same: got === sum, dir, seconds: Math.round((Date.now() - t0) / 1000), notes, verdict: v.kind };
  fs.writeFileSync(path.join(dir, 'kton-repro.json'), JSON.stringify(result, null, 1));
  const msg = got === null ? `not produced: the recipe did not write ${rel(target)}`
    : got === sum ? `reproduced ✓ the same bytes (sha256 ${sum.slice(0, 12)})`
    : `different: ${got.slice(0, 12)} instead of ${sum.slice(0, 12)}`;
  out.appendLine(`=== ${rel(target)}: ${msg}`);
  const show = got === sum ? vscode.window.showInformationMessage : vscode.window.showWarningMessage;
  const pick = await show(`kton: ${rel(target)} ${msg}`, 'Open folder', 'Log');
  if (pick === 'Open folder') vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(dir));
  if (pick === 'Log') out.show();
}

// ------------------------------------------------------------------ the side bar
// Projects → Results / Received / Intermediates → files. A file that a recorded step wrote
// opens its story; one that only came in opens in the editor.
const reproRoot = () => path.join(home, '.cache', 'kton-cockpit', 'repro');
const GROUPS = [['result', 'Results', 'files that came about and that nothing read afterwards'],
  ['source', 'Received', 'files that came from outside: raw data, scripts, drives'],
  ['intermediate', 'Intermediates', 'came about and were processed further']];
const NOW = { same: '', changed: 'different today', gone: 'deleted' };

class Tree {
  constructor() { this.ev = new vscode.EventEmitter(); this.onDidChangeTreeData = this.ev.event; this.files = {}; }
  refresh() { this.files = {}; this.ev.fire(); }
  async filesOf(project) {
    if (!this.files[project]) this.files[project] = await getJSON(`${viewerUrl()}api/files?project=${encodeURIComponent(project)}`, 10000).catch(() => []);
    return this.files[project] || [];
  }
  getTreeItem(e) { return e.item; }
  async getChildren(e) {
    if (!e) {
      if (!(await ensureService())) return [];
      const ps = (await getJSON(viewerUrl() + 'api/projects').catch(() => [])) || [];
      const ws = (vscode.workspace.workspaceFolders || []).map((f) => f.uri.fsPath);
      return ps.filter((p) => !p.path.startsWith(reproRoot() + '/')).map((p) => {
        const mine = ws.some((w) => p.path === w || p.path.startsWith(w + '/') || w.startsWith(p.path + '/'));
        const it = new vscode.TreeItem(path.basename(p.path), mine ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.Collapsed);
        it.description = `${p.sessions} sessions`;
        it.tooltip = `${p.path}\n${p.sessions} sessions, ${p.tasks} steps, last ${new Date(p.last_ns / 1e6).toLocaleString()}`;
        it.iconPath = new vscode.ThemeIcon('project');
        it.contextValue = 'project';
        return { kind: 'project', project: p.path, item: it };
      });
    }
    if (e.kind === 'project') {
      const all = await this.filesOf(e.project);
      return GROUPS.map(([role, label, tip]) => {
        const n = all.filter((f) => f.role === role).length;
        const it = new vscode.TreeItem(label, role === 'result' ? vscode.TreeItemCollapsibleState.Expanded : vscode.TreeItemCollapsibleState.Collapsed);
        it.description = String(n); it.tooltip = tip;
        it.iconPath = new vscode.ThemeIcon(role === 'result' ? 'star-empty' : role === 'source' ? 'arrow-down' : 'debug-step-over');
        return { kind: 'group', project: e.project, role, dir: '', n, item: it };
      }).filter((g) => g.n > 0);
    }
    // a group, or a directory inside one: directories with many files fold away
    const rel = (f) => (f.path.startsWith(e.project + '/') ? f.path.slice(e.project.length + 1) : f.path);
    const here = (await this.filesOf(e.project)).filter((f) => f.role === e.role && rel(f).startsWith(e.dir));
    const byDir = {};
    for (const f of here) {
      const rest = rel(f).slice(e.dir.length), i = rest.indexOf('/');
      const k = i < 0 ? '' : rest.slice(0, i + 1);
      (byDir[k] = byDir[k] || []).push(f);
    }
    const items = [];
    for (const [d, list] of Object.entries(byDir).sort()) {
      if (d && list.length > 3) {
        const it = new vscode.TreeItem(d, vscode.TreeItemCollapsibleState.Collapsed);
        it.description = String(list.length); it.iconPath = vscode.ThemeIcon.Folder;
        items.push({ kind: 'dir', project: e.project, role: e.role, dir: e.dir + d, item: it });
        continue;
      }
      for (const f of list) {
        const it = new vscode.TreeItem(rel(f).slice(e.dir.length), vscode.TreeItemCollapsibleState.None);
        it.resourceUri = vscode.Uri.file(f.path);
        const when = new Date(f.when_ns / 1e6).toLocaleString(undefined, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
        it.description = [NOW[f.now] || '', f.produced_by ? when : ''].filter(Boolean).join(' · ');
        it.tooltip = `${f.path}\nsha256 ${f.sha256.slice(0, 16)}…${f.now === 'changed' ? '\nThe file has other content today.' : f.now === 'gone' ? '\nThe file no longer exists.' : ''}`;
        if (f.produced_by) {
          it.contextValue = 'produced';
          it.command = { command: 'kton.story', title: 'Story', arguments: [{ project: e.project, file: f }] };
        } else {
          it.contextValue = 'received';
          if (f.now !== 'gone') it.command = { command: 'vscode.open', title: 'Open', arguments: [vscode.Uri.file(f.path)] };
        }
        items.push({ kind: 'file', project: e.project, file: f, item: it });
      }
    }
    return items;
  }
}

// the recorded version of a file open in the editor or picked in the explorer
async function recordedFile(uri) {
  const file = uri ? uri.fsPath : (vscode.window.activeTextEditor && vscode.window.activeTextEditor.document.uri.fsPath);
  if (!file) return null;
  if (!(await ensureService())) return null;
  const projects = await getJSON(viewerUrl() + 'api/projects').catch(() => []);
  const p = (projects || []).map((x) => x.path).filter((x) => file.startsWith(x + '/')).sort((a, b) => b.length - a.length)[0];
  if (!p) { vscode.window.showWarningMessage('kton: no observations for ' + file); return null; }
  const files = await getJSON(`${viewerUrl()}api/files?project=${encodeURIComponent(p)}`, 10000).catch(() => []);
  const f = (files || []).find((x) => x.path === file && x.produced_by);
  if (!f) { vscode.window.showWarningMessage('kton: no recorded step wrote ' + path.basename(file)); return null; }
  return { project: p, file: f };
}

// ------------------------------------------------------------------ what kton already has
// A step is in kton when the cockpit knows a producer for every file it wrote (verified records
// only, the cockpit's own rule). Asked per hash, in the repository the story belongs to.
const askProducer = (repo, sum) => new Promise((resolve) => {
  execFile(bin('cockpit'), ['ask', JSON.stringify({ query: 'producer', ref: 'sha256:' + sum })], { cwd: repo, timeout: 15000 }, (err, stdout) => {
    if (err) return resolve(false);
    try { resolve((JSON.parse(stdout).included || []).length > 0); } catch (e) { resolve(false); }
  });
});
async function publishedSteps(story) {
  const done = {};
  if (!fs.existsSync(path.join(story.repo, 'cockpit.config.json'))) return done;
  const sums = [...new Set(story.steps.flatMap((s) => s.hashes || []))];
  const known = {};
  await Promise.all(sums.map(async (h) => { known[h] = await askProducer(story.repo, h); }));
  for (const s of story.steps) if ((s.hashes || []).length && s.hashes.every((h) => known[h])) done[s.session + '/' + s.task] = true;
  return done;
}

// ------------------------------------------------------------------ the story, as an editor tab
const stories = new Map(); // path@sha → panel
const sq = (s) => "'" + String(s).replace(/'/g, "'\\''") + "'";

// the person's description and rationale of a line, as shell comments
function noteLines(n) {
  if (!n) return [];
  if (typeof n === 'string') n = { description: n };
  const out_ = [];
  for (const [f, label] of [['description', 'Description'], ['rationale', 'Rationale']]) {
    (n[f] || '').split('\n').filter((l) => l.trim()).forEach((l, i) => out_.push(`#   ${i ? ' '.repeat(label.length + 1) : label + ':'} ${l}`));
  }
  return out_;
}

// the lines handed to publish(): per step the person's words as comments, then the call
async function publishText(story, file, steps) {
  const lines = [`# Story of ${path.relative(story.repo, file.path)}`];
  for (const s of steps) {
    if (s.hand) { // not recorded, nothing to publish: the person's words stay with the story
      lines.push(`# ${s.sentence}`);
      lines.push(...noteLines(s.note));
      continue;
    }
    const d = await getJSON(`${viewerUrl()}api/draft?session=${encodeURIComponent(s.session)}&task=${encodeURIComponent(s.task)}`, 10000).catch(() => null);
    if (!d) continue;
    lines.push(`# ${s.where}: ${s.sentence}`);
    lines.push(...noteLines(s.note));
    const req = { inputs: (d.inputs || []).filter((f) => f.use).map((f) => f.path), outputs: (d.outputs || []).filter((f) => f.use).map((f) => f.path), cmd: d.cmd };
    if (!req.outputs.length) { // the files it wrote are not there with these bytes any more: nothing to sign
      lines.push('#   cannot be published: ' + ((d.outputs || []).map((f) => `${f.path}${f.note ? ' (' + f.note + ')' : ''}`).join(', ') || 'no output'));
      continue;
    }
    lines.push('cockpit publish ' + sq(JSON.stringify(req)));
  }
  return lines.join('\n');
}

async function openStory(context, project, file) {
  const id = file.path + '@' + file.sha256;
  if (stories.has(id)) { stories.get(id).reveal(); return; }
  if (!(await ensureService())) return;
  const media = vscode.Uri.file(path.join(context.extensionPath, 'media'));
  const p = vscode.window.createWebviewPanel('ktonStory', 'Story: ' + path.basename(file.path), vscode.ViewColumn.Active,
    { enableScripts: true, retainContextWhenHidden: true, localResourceRoots: [media] });
  p.iconPath = vscode.Uri.joinPath(media, 'kton.svg');
  stories.set(id, p);
  p.onDidDispose(() => stories.delete(id));
  const nonce = crypto.randomBytes(16).toString('hex');
  const w = p.webview;
  w.html = `<!doctype html><html lang="de"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${w.cspSource}; script-src 'nonce-${nonce}';">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="${w.asWebviewUri(vscode.Uri.joinPath(media, 'story.css'))}"></head>
<body><div id="root"><main><p class="muted">Putting the story together …</p></main></div>
<script nonce="${nonce}" src="${w.asWebviewUri(vscode.Uri.joinPath(media, 'story.js'))}"></script></body></html>`;
  let story = null;
  const notesKey = 'kton.notes';
  const allNotes = () => context.globalState.get(notesKey, {});
  const send = async () => {
    try {
      story = await getJSON(`${viewerUrl()}api/story?project=${encodeURIComponent(project)}&path=${encodeURIComponent(file.path)}&sha=${encodeURIComponent(file.sha256)}`, 20000);
    } catch (e) {
      w.html = `<!doctype html><body style="font-family:sans-serif;padding:20px">No story for ${path.basename(file.path)}: ${String(e.message).replace(/</g, '&lt;')}</body>`;
      return;
    }
    // notes are kept per line of the story: session/script, or hand/<file>@<hash>
    const n = allNotes(), mine = {}, sessions = new Set(story.steps.map((s) => s.session));
    for (const [k, v] of Object.entries(n)) if (sessions.has(k.split('/')[0]) && v && (typeof v === 'string' || v.description !== undefined)) mine[k] = v;
    const cockpitRepo = fs.existsSync(path.join(story.repo, 'cockpit.config.json'));
    w.postMessage({ type: 'story', story, notes: mine, cockpitRepo, published: await publishedSteps(story) });
  };
  w.onDidReceiveMessage(async (m) => {
    if (m.type === 'ready') send();
    else if (m.type === 'note') { const n = allNotes(); n[m.key] = m.note; await context.globalState.update(notesKey, n); }
    else if (m.type === 'reproduce') reproduce(project, file.path, file.sha256);
    else if (m.type === 'save') {
      const to = await vscode.window.showSaveDialog({ defaultUri: vscode.Uri.file(path.join(story ? story.repo : project, path.basename(file.path) + '.geschichte.md')), filters: { Markdown: ['md'] } });
      if (to) { fs.writeFileSync(to.fsPath, m.markdown); vscode.window.showTextDocument(to); }
    } else if (m.type === 'publish' && story) {
      await publish(story.repo, await publishText(story, file, m.steps));
      // the terminal does the work; look again a few times so the story catches up
      for (const ms of [5000, 15000, 40000]) setTimeout(async () => { if (stories.has(id)) w.postMessage({ type: 'published', published: await publishedSteps(story) }); }, ms);
    }
  });
}

async function openInBrowser() {
  if (!(await ensureService())) return;
  vscode.env.openExternal(await vscode.env.asExternalUri(vscode.Uri.parse(viewerUrl())));
}

// ------------------------------------------------------------------ activation
function activate(context) {
  out = vscode.window.createOutputChannel('kton');
  status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  status.command = 'kton.toggleRecording';
  showStatus(); status.show();
  const sync = setInterval(showStatus, 3000); // another window may have switched it
  context.subscriptions.push(out, status, { dispose: () => clearInterval(sync) });
  context.subscriptions.push(
    vscode.commands.registerCommand('kton.toggleRecording', toggleRecording),
    vscode.commands.registerCommand('kton.openObservations', openObservations),
    vscode.commands.registerCommand('kton.reproduceFile', async (uri) => {
      const r = await recordedFile(uri);
      if (r) reproduce(r.project, r.file.path, r.file.sha256);
    }),
    vscode.commands.registerCommand('kton.storyFile', async (uri) => {
      const r = await recordedFile(uri);
      if (r) openStory(context, r.project, r.file);
    }));
  const tree = new Tree();
  const view = vscode.window.createTreeView('kton.files', { treeDataProvider: tree, showCollapseAll: true });
  context.subscriptions.push(view, view.onDidChangeVisibility((e) => { if (e.visible) tree.refresh(); }),
    vscode.commands.registerCommand('kton.refresh', () => tree.refresh()),
    vscode.commands.registerCommand('kton.openInBrowser', openInBrowser),
    vscode.commands.registerCommand('kton.story', (e) => e && e.file && openStory(context, e.project, e.file)),
    vscode.commands.registerCommand('kton.reproduceItem', (e) => e && e.file && reproduce(e.project, e.file.path, e.file.sha256)),
    vscode.commands.registerCommand('kton.openFile', (e) => e && e.file && vscode.commands.executeCommand('vscode.open', vscode.Uri.file(e.file.path))));
  ensureService();

  let positron;
  try { positron = require('positron'); } catch (e) { return; } // plain VS Code: no origins
  if (!positron.runtime || !positron.runtime.onDidExecuteCode) return;
  const file = path.join(traceDir(), 'positron-exec.jsonl');
  fs.mkdirSync(traceDir(), { recursive: true });
  context.subscriptions.push(positron.runtime.onDidExecuteCode((e) => {
    if (!recording()) return;
    try { fs.appendFileSync(file, JSON.stringify(origin(e)) + '\n'); } catch (err) { /* never disturb the session */ }
  }));
}

function deactivate() {}
module.exports = { activate, deactivate, _test: { reproduce, publishedSteps, publishText, Tree, setOut: (o) => { out = o; } } };
