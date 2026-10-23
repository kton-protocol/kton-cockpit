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
//  - a doctor badge in the status bar: `cockpit doctor` for the cockpit repository of the file
//    in front, ready / not ready / failing at a glance, the steps in its tooltip
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
let doctor;   // the doctor badge

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

// ------------------------------------------------------------------ the doctor badge
// The cockpit repository a file belongs to: the nearest directory above it with a
// cockpit.config.json, at most up to the workspace folder it is in.
function cockpitRepoOf(file) {
  if (!file) return null;
  const ws = vscode.workspace.getWorkspaceFolder(vscode.Uri.file(file));
  const stop = ws ? ws.uri.fsPath : path.parse(file).root;
  for (let d = path.dirname(file); ; d = path.dirname(d)) {
    if (fs.existsSync(path.join(d, 'cockpit.config.json'))) return d;
    if (d === stop || d === path.dirname(d)) return null;
  }
}
// What `cockpit doctor` said: ready or not, and its numbered steps; `!` marks the blocking ones.
function parseDoctor(text, code) {
  const r = { state: code === 0 ? 'unknown' : 'failed', steps: [], text };
  for (const l of text.split('\n')) {
    if (/^ready:/.test(l)) r.state = 'ready';
    else if (/^not ready:/.test(l)) r.state = 'not-ready';
    const m = l.match(/^\s*(!?)\s*\d+\.\s+(.*)$/);
    if (m) r.steps.push({ must: m[1] === '!', text: m[2] });
  }
  if (code !== 0) r.error = text.trim().split('\n').pop();
  return r;
}
// `cockpit doctor --json` (kton-cockpit SPEC §6): {ready, steps:[{must,text}], fields}; exit 3 is
// "ran, not ready". null when the cockpit is older and does not know --json.
function parseDoctorJSON(stdout, code) {
  let j;
  try { j = JSON.parse(stdout); } catch (e) { return null; }
  if (!j || typeof j.ready !== 'boolean' || (code !== 0 && code !== 3)) return null;
  return { state: j.ready ? 'ready' : 'not-ready', steps: (j.steps || []).map((x) => ({ must: !!x.must, text: String(x.text || '') })) };
}
const doctorRun = (repo, args) => new Promise((resolve) => execFile(bin('cockpit'), ['doctor', ...args], { cwd: repo, timeout: 15000 }, (err, stdout, stderr) =>
  resolve({ stdout: String(stdout || ''), stderr: String(stderr || ''), code: err ? (typeof err.code === 'number' ? err.code : 1) : 0 })));
let doctorRepo = null, doctorLast = null;
async function runDoctor(force) {
  const ed = vscode.window.activeTextEditor;
  const file = ed && ed.document.uri.scheme === 'file' ? ed.document.uri.fsPath : null;
  const repo = cockpitRepoOf(file) || (vscode.workspace.workspaceFolders || []).map((f) => f.uri.fsPath)
    .find((w) => fs.existsSync(path.join(w, 'cockpit.config.json'))) || null;
  if (!repo) { doctorRepo = null; doctor.hide(); return; }
  if (repo === doctorRepo && !force) return;
  doctorRepo = repo;
  const j = await doctorRun(repo, ['--json']);
  let d = parseDoctorJSON(j.stdout, j.code);
  if (!d) { const r = await doctorRun(repo, []); d = parseDoctor(r.stdout + r.stderr, r.code === 3 ? 0 : r.code); }
  if (repo !== doctorRepo) return; // another file came to the front meanwhile
  doctorLast = Object.assign(d, { repo });
  const must = d.steps.filter((s) => s.must).length;
  doctor.text = d.state === 'ready' ? '🩺 $(check)' : d.state === 'not-ready' ? `🩺 $(warning) ${must}` : '🩺 $(error)';
  doctor.backgroundColor = d.state === 'ready' ? undefined : new vscode.ThemeColor(d.state === 'not-ready' ? 'statusBarItem.warningBackground' : 'statusBarItem.errorBackground');
  const tip = new vscode.MarkdownString();
  tip.appendMarkdown(`**cockpit doctor** · ${path.basename(repo)}\n\n`);
  tip.appendMarkdown(d.state === 'ready' ? 'ready: publish · say · ask' : d.state === 'not-ready' ? 'not ready: the verbs fail or mislead until the steps marked ! are done'
    : d.state === 'failed' ? 'doctor failed: ' + (d.error || '') : 'doctor said neither ready nor not ready');
  if (d.steps.length) tip.appendMarkdown('\n\n' + d.steps.map((s, i) => `${s.must ? '**!**' : '·'} ${i + 1}. ${s.text.replace(/\n/g, ' ')}`).join('\n\n'));
  tip.appendMarkdown('\n\n*Click: the full report*');
  doctor.tooltip = tip;
  doctor.show();
}
async function showDoctor() {
  if (!doctorLast) return;
  const r = await doctorRun(doctorLast.repo, []); // the report as a person reads it
  out.clear();
  out.appendLine(`$ (${doctorLast.repo}) cockpit doctor`);
  out.append(r.stdout + r.stderr);
  out.show(true);
  runDoctor(true); // and look again: the report may be older than the last change
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
// a JSON body to ktonwatch (PUT/POST)
function sendJSON(method, url, body, timeout) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(body);
    const req = http.request(url, { method, timeout: timeout || 5000, headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(data) } }, (res) => {
      let b = ''; res.on('data', (c) => b += c);
      res.on('end', () => res.statusCode === 200 ? resolve(b ? JSON.parse(b) : null) : reject(new Error(`${res.statusCode} ${b.trim()}`)));
    });
    req.on('timeout', () => req.destroy(new Error('timeout')));
    req.on('error', reject);
    req.end(data);
  });
}

// ------------------------------------------------------------------ notes: drafts of description and rationale
// Kept by ktonwatch (/api/notes), so the story and the MCP tools (Claude drafting for the person)
// see the same drafts. Earlier extensions kept them in this window's globalState: moved over once.
const noteOf = (v) => (typeof v === 'string' ? { description: v } : v || {});
async function notesFor(sessions) {
  const q = [...sessions, 'hand'].map((s) => 'prefix=' + encodeURIComponent(s + '/')).join('&');
  return (await getJSON(`${viewerUrl()}api/notes?${q}`, 5000).catch(() => ({}))) || {};
}
const putNote = (key, note) => sendJSON('PUT', viewerUrl() + 'api/notes', Object.assign({ key, by: 'you' }, noteOf(note)));
async function moveOldNotes(context) {
  if (context.globalState.get('kton.notesMoved')) return;
  const old = context.globalState.get('kton.notes', {});
  if (!Object.keys(old).length) { await context.globalState.update('kton.notesMoved', true); return; }
  if (!(await ensureService())) return; // next time
  const have = (await getJSON(viewerUrl() + 'api/notes', 5000).catch(() => null));
  if (!have) return;
  let moved = 0;
  for (const [k, v] of Object.entries(old)) {
    const n = noteOf(v);
    if (have[k] || !(n.description || n.rationale)) continue;
    try { await putNote(k, n); moved++; } catch (e) { out.appendLine(`kton: note ${k} not moved: ${e.message}`); return; }
  }
  await context.globalState.update('kton.notesMoved', true);
  if (moved) out.appendLine(`kton: ${moved} notes moved to ktonwatch (/api/notes)`);
}

async function ensureService() {
  try { await getJSON(viewerUrl() + 'api/config'); return true; } catch (e) { /* not running */ }
  const exe = bin('ktonwatch');
  if (!fs.existsSync(exe)) { vscode.window.showErrorMessage(`kton: ${exe} is missing (recorder/install.sh)`); return false; }
  // detached: the service outlives this window; the next one finds it running
  const p = spawn(exe, ['serve'], { detached: true, stdio: 'ignore' });
  p.unref();
  out.appendLine(`ktonwatch serve started (pid ${p.pid})`);
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
  const says = (text.match(/^cockpit claim /gm) || []).length;
  const ok = await vscode.window.showWarningMessage(
    `Run ${calls} × cockpit publish${says ? ` and ${says} × cockpit say (description, rationale)` : ''} in ${repo}? That signs, commits and pushes.`, { modal: true }, 'Run');
  if (ok !== 'Run') return;
  const cockpit = bin('cockpit');
  // the lines below are POSIX shell: the terminal is bash, whatever the person's default is
  // bash, whose quoting the publish lines are written for: a "kton cockpit" terminal an older
  // version opened in another shell is not reused
  const term = vscode.window.terminals.find((t) => t.name === 'kton cockpit' && (t.creationOptions || {}).shellPath === '/bin/bash') ||
    vscode.window.createTerminal({ name: 'kton cockpit', cwd: repo, shellPath: '/bin/bash' });
  term.show();
  term.sendText(`cd ${JSON.stringify(repo)}`);
  const c = JSON.stringify(cockpit);
  // `cockpit claim <template> '<text>'` is ours: a say with that template, its field "value",
  // about the foton the publish before it returned (its JSON answer, kept in KTON_OUT and shown)
  const claim = "import json,sys; o=json.loads(sys.argv[1]); print(json.dumps({'template':sys.argv[2],'subject':o['fotonId'],'fields':{'value':json.loads(sys.argv[3])}}))";
  for (const line of text.split('\n')) {
    if (line.startsWith('cockpit publish ')) term.sendText(`KTON_OUT=; KTON_OUT=$(${c}${line.slice('cockpit'.length)}) && printf '%s\\n' "$KTON_OUT"`);
    else if (line.startsWith('cockpit claim ')) {
      const [, template, text] = line.match(/^cockpit claim (\S+) (.*)$/s);
      term.sendText(`[ -n "$KTON_OUT" ] && ${c} say "$(python3 -c ${sq(claim)} "$KTON_OUT" ${template} ${text})"`);
    }
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
  // containers replay recorded tool calls (Rscript, nmfe, quarto …); a file a console command
  // made has none, and is reproduced from its recipe instead
  const q = `project=${encodeURIComponent(project)}&path=${encodeURIComponent(target)}&sha=${encodeURIComponent(sum)}`;
  try { await getJSON(`${viewerUrl()}api/reproplan?${q}`, 10000); } catch (e) {
    if (!/^404 /.test(e.message)) { // ktonwatch failed: say so, do not quietly take the other path
      vscode.window.showErrorMessage(`kton: no reproduction plan: ${e.message}`);
      return true;
    }
    out.appendLine(`\nkton: no recorded tool call wrote ${path.relative(project, target)}; reproducing it from its recipe instead`);
    return false;
  }
  out.show(true);
  out.appendLine(`\n=== Reproduction in containers: ${path.relative(project, target)}`);
  const lines = [];
  const code = await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `kton: reproducing ${path.basename(target)} in containers` }, () => new Promise((resolve) => {
    const p = spawn(exe, ['-project', project, '-path', target, '-sha', sum]);
    const take = (d) => { const t = d.toString(); out.append(t); lines.push(...t.split('\n')); };
    p.stdout.on('data', take); p.stderr.on('data', take);
    p.on('close', resolve);
  }));
  const dir = (lines.find((l) => l.startsWith('kton-repro: ') && l.includes(' in ')) || '').split(' in ').pop().trim();
  const verdict = (lines.filter((l) => l.startsWith('= ')).pop() || lines.filter((l) => l.trim()).pop() || '').replace(/^= /, '');
  const show = code === 0 ? vscode.window.showInformationMessage : vscode.window.showWarningMessage;
  show(`kton: ${verdict}`, ...(dir ? ['Open folder'] : []), 'Log').then((pick) => {
    if (pick === 'Open folder') vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(dir));
    if (pick === 'Log') out.show();
  });
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
  await vscode.window.withProgress({ location: vscode.ProgressLocation.Notification, title: `kton: reproducing ${path.basename(target)}` }, async () => {
    if ((v.kind === 'scripts' || v.kind === 'scripts-with-gaps') && (v.scripts || []).length) {
      for (const s of v.scripts) {                       // the scripts you know, one after the other
        const code = await run([ktonR(), '--no-save', '-q', '-f', rel(s.path)], dir, env);
        if (code !== 0) { out.appendLine(`  ${rel(s.path)} exited with ${code}`); break; }
      }
    } else {                                              // the steps as recorded, as one script
      const script = r.script.replace(`setwd(${JSON.stringify(project)})`, `setwd(${JSON.stringify(dir)})`);
      fs.writeFileSync(path.join(dir, 'kton-recipe.R'), script);
      await run([ktonR(), '--no-save', '-q', '-f', 'kton-recipe.R'], dir, env);
    }
  });
  const got = await sha256File(path.join(dir, rel(target)));
  // every step's outputs, not only the target: the story shows which step gave the same bytes
  const steps = [];
  for (const st of r.steps || []) {
    const outputs = [];
    for (const o of st.outputs || []) {
      if (!inside(o.path)) continue;
      const g = await sha256File(path.join(dir, rel(o.path)));
      outputs.push({ path: o.path, recorded: o.sha256, reproduced: g || '', same: g === o.sha256 });
    }
    steps.push({ session: st.session, task: st.id, outputs });
  }
  const result = { project, target, recorded: sum, reproduced: got || '', same: got === sum, when: new Date().toISOString(), dir,
    seconds: Math.round((Date.now() - t0) / 1000), notes, verdict: v.kind, steps };
  fs.writeFileSync(path.join(dir, 'kton-repro.json'), JSON.stringify(result, null, 1));
  for (const s of steps) for (const o of s.outputs) if (!o.same) out.appendLine(`  ${rel(o.path)}: ${o.reproduced ? 'different bytes' : 'not produced'}`);
  const msg = got === null ? `not produced: the recipe did not write ${rel(target)}`
    : got === sum ? `reproduced ✓ the same bytes (sha256 ${sum.slice(0, 12)})`
    : `different: ${got.slice(0, 12)} instead of ${sum.slice(0, 12)}`;
  out.appendLine(`=== ${rel(target)}: ${msg}`);
  // not awaited: whoever called (the story) refreshes now, not when the message is closed
  const show = got === sum ? vscode.window.showInformationMessage : vscode.window.showWarningMessage;
  show(`kton: ${rel(target)} ${msg}`, 'Open folder', 'Log').then((pick) => {
    if (pick === 'Open folder') vscode.commands.executeCommand('revealFileInOS', vscode.Uri.file(dir));
    if (pick === 'Log') out.show();
  });
}

// a deviating output of a reproduction: the recorded bytes beside the reproduced ones. The
// recorded version is the project's file if it still has those bytes, else the CAS's copy;
// it is copied into the reproduction folder, so the diff never opens the CAS itself.
const casFile = (sum) => path.join(process.env.XDG_CACHE_HOME || path.join(home, '.cache'), 'kton-cockpit', 'cas', 'sha256', sum.slice(0, 2), sum);
async function diffReproduced(project, m) {
  const rel = path.relative(project, m.path);
  if (rel.startsWith('..') || path.isAbsolute(rel)) { vscode.window.showWarningMessage(`kton: ${m.path} is not in the project`); return; }
  const reproduced = path.join(m.dir, rel);
  let recorded = null;
  if ((await sha256File(m.path)) === m.recorded) recorded = m.path;
  else if (fs.existsSync(casFile(m.recorded))) recorded = casFile(m.recorded);
  if (!recorded) { vscode.window.showWarningMessage(`kton: the recorded ${rel} (${m.recorded.slice(0, 12)}) is not kept, nothing to compare with`); return; }
  if (!fs.existsSync(reproduced)) { vscode.window.showWarningMessage(`kton: the reproduction did not produce ${rel}`); return; }
  const left = path.join(m.dir, '.kton-recorded', rel);
  fs.mkdirSync(path.dirname(left), { recursive: true });
  fs.rmSync(left, { force: true }); // a copy from the CAS is read-only: a second diff copies anew
  fs.copyFileSync(recorded, left);
  vscode.commands.executeCommand('vscode.diff', vscode.Uri.file(left), vscode.Uri.file(reproduced),
    `${path.basename(rel)}: recorded ↔ reproduced`);
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
// concurrent asks in one federated repository need cockpit v0.3.0 (SPEC §9.8)
const askCockpit = (repo, q) => new Promise((resolve) => {
  execFile(bin('cockpit'), ['ask', JSON.stringify(q)], { cwd: repo, timeout: 15000 }, (err, stdout) => {
    if (err) return resolve(null);
    try { resolve(JSON.parse(stdout)); } catch (e) { resolve(null); }
  });
});
// the verified records whose output these bytes are
const producersOf = async (repo, sum) => {
  const a = await askCockpit(repo, { query: 'producer', ref: 'sha256:' + sum });
  return (a && a.included) || [];
};
// the input hashes a record names
const recordInputs = async (repo, id) => {
  const a = await askCockpit(repo, { query: 'record', ref: id });
  return new Set(((a && a.record && a.record.inputs) || []).map((i) => String(i.hash || '').replace(/^sha256:/, '')));
};
// A step is in kton when, for each file it wrote, a verified record has those bytes as output AND
// names only inputs this step read with the same bytes: the same computation, not only the same
// result. Same output bytes from other inputs (a changed script whose change did not reach this
// file) are told apart: "same" (the result is in kton, this run of it is not).
async function publishedSteps(story) {
  const done = {}, same = {};
  if (!fs.existsSync(path.join(story.repo, 'cockpit.config.json'))) return { done, same };
  const sums = [...new Set(story.steps.flatMap((s) => s.hashes || []))];
  const producers = {};
  await Promise.all(sums.map(async (h) => { producers[h] = await producersOf(story.repo, h); }));
  const ids = [...new Set(Object.values(producers).flat())];
  const inputs = {};
  await Promise.all(ids.map(async (id) => { inputs[id] = await recordInputs(story.repo, id); }));
  for (const s of story.steps) {
    const outs = s.hashes || [];
    if (!outs.length || !outs.every((h) => producers[h].length)) continue;
    const key = s.session + '/' + s.task;
    if (!s.in_hashes) { done[key] = true; continue; } // an older ktonwatch says nothing about inputs: outputs only, as before
    const read = new Set(s.in_hashes);
    if (outs.every((h) => producers[h].some((id) => [...inputs[id]].every((x) => read.has(x))))) done[key] = true;
    else same[key] = true;
  }
  return { done, same };
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
// whether the repository allows a claim template (claims.allowedTemplates of its config)
function allowsTemplate(repo, name) {
  try { return (JSON.parse(fs.readFileSync(path.join(repo, 'cockpit.config.json'), 'utf8')).claims || {}).allowedTemplates.includes(name); } catch (e) { return false; }
}

async function publishText(story, file, steps) {
  const lines = [`# Story of ${path.relative(story.repo, file.path)}`];
  // a step's description and rationale become signed claims about its foton (templates
  // "describes" and "rationale", as cockpit init writes them) where the repository allows
  // them; elsewhere they stay comments
  const allowed = { describes: allowsTemplate(story.repo, 'describes'), rationale: allowsTemplate(story.repo, 'rationale') };
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
    const n = typeof s.note === 'string' ? { description: s.note } : (s.note || {});
    // see publish(): a say about the foton just published, one per template
    // the text as JSON: one line, whatever it holds (the script is sent line by line)
    if (allowed.describes && (n.description || '').trim()) lines.push('cockpit claim describes ' + sq(JSON.stringify(n.description.trim())));
    if (allowed.rationale && (n.rationale || '').trim()) lines.push('cockpit claim rationale ' + sq(JSON.stringify(n.rationale.trim())));
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
  w.html = `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src ${w.cspSource}; script-src 'nonce-${nonce}';">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="${w.asWebviewUri(vscode.Uri.joinPath(media, 'story.css'))}"></head>
<body><div id="root"><main><p class="muted">Putting the story together …</p></main></div>
<script nonce="${nonce}" src="${w.asWebviewUri(vscode.Uri.joinPath(media, 'story.js'))}"></script></body></html>`;
  let story = null;
  const send = async () => {
    try {
      story = await getJSON(`${viewerUrl()}api/story?project=${encodeURIComponent(project)}&path=${encodeURIComponent(file.path)}&sha=${encodeURIComponent(file.sha256)}`, 20000);
    } catch (e) {
      w.html = `<!doctype html><body style="font-family:sans-serif;padding:20px">No story for ${path.basename(file.path)}: ${String(e.message).replace(/</g, '&lt;')}</body>`;
      return;
    }
    // notes are kept per line of the story: session/task, or hand/<file>@<hash>
    const mine = await notesFor(new Set(story.steps.map((s) => s.session)));
    const cockpitRepo = fs.existsSync(path.join(story.repo, 'cockpit.config.json'));
    const ps = await publishedSteps(story);
    w.postMessage({ type: 'story', story, notes: mine, cockpitRepo, published: ps.done, sameResult: ps.same });
  };
  // back in view: read again, so drafts Claude wrote meanwhile (kton_note) show
  p.onDidChangeViewState((e) => { if (e.webviewPanel.visible && story) send(); });
  w.onDidReceiveMessage(async (m) => {
    if (m.type === 'ready') send();
    else if (m.type === 'note') putNote(m.key, m.note).catch((e) => vscode.window.showWarningMessage(`kton: the note was not kept: ${e.message}`));
    else if (m.type === 'reproduce') { await reproduce(project, file.path, file.sha256); send(); } // the result belongs in the story
    else if (m.type === 'diff') diffReproduced(project, m);
    else if (m.type === 'inkton' && story) openInKton(project, story.repo, file.sha256, path.basename(file.path));
    else if (m.type === 'save') {
      const to = await vscode.window.showSaveDialog({ defaultUri: vscode.Uri.file(path.join(story ? story.repo : project, path.basename(file.path) + '.story.md')), filters: { Markdown: ['md'] } });
      if (to) { fs.writeFileSync(to.fsPath, m.markdown); vscode.window.showTextDocument(to); }
    } else if (m.type === 'publish' && story) {
      await publish(story.repo, await publishText(story, file, m.steps));
      // the terminal does the work; look again a few times so the story catches up
      for (const ms of [5000, 15000, 40000]) setTimeout(async () => { if (stories.has(id)) { const ps = await publishedSteps(story); w.postMessage({ type: 'published', published: ps.done, sameResult: ps.same }); } }, ms);
    }
  });
}

// ------------------------------------------------------------------ the file in kton (kton-web)
// kton-web's graph viewer, in the editor beside the story, in its editor profile (?view=extension):
// the graph, each step's command, description and rationale, inputs and outputs without hashes,
// and the file actions below. Its records come as a portion of the cockpit's store around the file
// (?select=, expandable on click), joined by file hash with the project's recorded graph
// (?graph=): recorded only, signed only, both. ktonwatch serves all of it from one origin: kton-web
// (/web/), the recorded graph (/kton/graph.json) and the cockpit's data handler
// (/kton/reg/<id>/data/select: through its index, with the trust tiers). kton-web verifies every
// record itself. A second file of the same project re-centres the tab instead of reloading it.
const ktonPanels = new Map(); // repo → {panel, project, selection}
const FILE_ACTIONS = [
  { id: 'open', label: 'Open', on: 'both' },
  { id: 'story', label: 'Story', on: 'output' },
  { id: 'today', label: 'Compare with today', on: 'both' },
  { id: 'copy', label: 'Copy into my project', on: 'both' },
];
const panelOf = (project) => [...ktonPanels.values()].find((x) => !project || x.project === project);
async function openInKton(project, repo, sum, name) {
  if (!(await ensureService())) return;
  const shown = ktonPanels.get(repo);
  if (shown && shown.project === project) { // the viewer finds the file, asking the store if it must
    shown.panel.reveal(vscode.ViewColumn.Beside);
    shown.panel.title = `kton: ${name || path.basename(repo)}`;
    shown.panel.webview.postMessage({ type: 'set', state: { focus: 'sha256:' + sum } });
    return shown;
  }
  const p0 = ((await getJSON(viewerUrl() + 'api/projects').catch(() => [])) || []).find((x) => x.path === project);
  const reg = p0 && p0.registry;
  const g = '/kton/graph.json?project=' + encodeURIComponent(project);
  const q = (reg ? 'select=' + encodeURIComponent(`/kton/reg/${reg}/data/select`) + '&' : '') +
    'graph=' + encodeURIComponent(g) + '&focus=' + encodeURIComponent('sha256:' + sum) + '&mode=anc&view=extension';
  const src = (await vscode.env.asExternalUri(vscode.Uri.parse(viewerUrl() + 'web/viewers/graph/?' + q))).toString(true);
  const originOf = new URL(src).origin;
  let panel = shown && shown.panel;
  if (!panel) {
    panel = vscode.window.createWebviewPanel('ktonGraph', 'kton', vscode.ViewColumn.Beside, { enableScripts: true, retainContextWhenHidden: true });
    panel.onDidDispose(() => ktonPanels.delete(repo));
    panel.webview.onDidReceiveMessage((m) => {
      // the viewer reports every change of state: keep what the person selected (kton_selection)
      if (m.type === 'kton:state' && m.state && ktonPanels.get(repo)) ktonPanels.get(repo).selection = m.state.selection || [];
      ktonIntent(project, m);
    });
  } else panel.reveal(vscode.ViewColumn.Beside);
  const entry = { panel, project, selection: [] };
  ktonPanels.set(repo, entry);
  panel.title = `kton: ${name || path.basename(repo)}`;
  const nonce = crypto.randomBytes(16).toString('hex');
  // the viewer takes messages from its own origin and from ?host=, the webview's, which only the
  // webview knows: so the iframe's address is completed in here
  panel.webview.html = `<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; frame-src ${originOf}; script-src 'nonce-${nonce}'; style-src 'unsafe-inline';">
<style>html,body,iframe{margin:0;padding:0;border:0;width:100%;height:100%;overflow:hidden}</style></head>
<body><iframe id="kweb"></iframe>
<script nonce="${nonce}">
  const vscode = acquireVsCodeApi(), viewer = ${JSON.stringify(originOf)}, frame = document.getElementById('kweb');
  // until the viewer is ready, what the extension sends waits
  let ready = false; const waiting = [];
  const post = (state) => frame.contentWindow.postMessage({type: 'kton:set', state}, viewer);
  const set = (state) => ready ? post(state) : waiting.push(state);
  frame.src = ${JSON.stringify(src)} + '&host=' + encodeURIComponent(window.origin);
  window.addEventListener('message', (e) => {
    if (e.origin === viewer && e.data && e.data.type === 'kton:ready') {
      ready = true;
      post({view: {actions: ${JSON.stringify(FILE_ACTIONS)}, select: {multi: true}}});              // the editor's file actions; multi-select
      while (waiting.length) post(waiting.shift());
    } else if (e.origin === viewer && e.data && /^kton:/.test(e.data.type || '')) vscode.postMessage(e.data); // intents, state, refusals
    else if (e.data && e.data.type === 'set') set(e.data.state);                                     // from the extension
  });
</script></body></html>`;
  if (!reg) vscode.window.setStatusBarMessage('kton: no cockpit repository for this project: only what was recorded is shown', 5000);
  return entry;
}

// what the viewer asks the editor to do (kton-web core/host.js): {type:'kton:intent', intent:{type:'act',
// action, file:{name, hash, role}, node}} for a file action; a link comes as an intent with a uri
async function ktonIntent(project, m) {
  if (m.type === 'kton:refused') { vscode.window.setStatusBarMessage(`kton: the viewer cannot ${m.what || 'do that'}${m.why ? ': ' + m.why : ''}`, 6000); return; }
  if (m.type !== 'kton:intent' || !m.intent) return;
  const it = m.intent;
  if (!(it.type === 'act' && it.file && it.file.hash)) { if (it.uri) vscode.env.openExternal(vscode.Uri.parse(it.uri)); return; }
  const sum = String(it.file.hash).replace(/^sha256:/, '');
  // the file of this project with these bytes, as recorded; else one by its name
  const files = (await getJSON(`${viewerUrl()}api/files?project=${encodeURIComponent(project)}`, 10000).catch(() => [])) || [];
  const f = files.find((x) => x.sha256 === sum) || files.find((x) => x.path === path.join(project, it.file.name || ''));
  const onDisk = f && fs.existsSync(f.path) && (await sha256File(f.path)) === sum;
  const kept = fs.existsSync(casFile(sum)) ? casFile(sum) : null;
  // the recorded bytes as a file the editor can show, named like the original
  const recordedCopy = () => {
    const to = path.join(reproRoot(), '.kton-recorded', sum.slice(0, 12), path.basename(it.file.name || sum));
    fs.mkdirSync(path.dirname(to), { recursive: true });
    if (!fs.existsSync(to)) fs.copyFileSync(kept, to);
    return to;
  };
  switch (it.action) {
    case 'open':
      if (onDisk) return vscode.commands.executeCommand('vscode.open', vscode.Uri.file(f.path));
      if (kept) { vscode.commands.executeCommand('vscode.open', vscode.Uri.file(recordedCopy())); vscode.window.setStatusBarMessage('kton: the recorded version (the file has other bytes today, or is elsewhere)', 5000); return; }
      return vscode.window.showWarningMessage(`kton: ${it.file.name} with these bytes is not here, and no copy is kept`);
    case 'story':
      if (f && f.produced_by) return openStory(extContext, project, f);
      return vscode.window.showWarningMessage(`kton: no recorded step of this project wrote ${it.file.name}`);
    case 'copy': {
      const r = await copyIntoProject(project, sum, it.file.name, it.file.name);
      if (!r.ok && r.message !== 'declined') vscode.window.showWarningMessage('kton: ' + r.message);
      return;
    }
    case 'today': {
      const now = f && fs.existsSync(f.path) ? f.path : null;
      if (!now) return vscode.window.showWarningMessage(`kton: ${it.file.name} is not in this project today`);
      if (onDisk) return vscode.window.setStatusBarMessage(`kton: ${it.file.name} has the same bytes today`, 5000);
      if (!kept) return vscode.window.showWarningMessage(`kton: the version in the graph (${sum.slice(0, 12)}) is not kept, nothing to compare with`);
      return vscode.commands.executeCommand('vscode.diff', vscode.Uri.file(recordedCopy()), vscode.Uri.file(now), `${path.basename(now)}: in the graph ↔ today`);
    }
  }
}

// ------------------------------------------------------------------ Claude
// The Claude extension in Positron is Claude Code: it reads Claude Code's MCP configuration. This
// adds ktonwatch's MCP tools there, in a visible terminal; the token is read from its file by the
// shell, so it does not show on screen.
function connectClaude() {
  const tokenFile = path.join(home, '.config', 'kton', 'mcp-token');
  if (!fs.existsSync(tokenFile)) { vscode.window.showErrorMessage(`kton: no ${tokenFile} yet: ktonwatch serve makes it (restart it, or open the kton side bar)`); return; }
  const url = viewerUrl() + 'mcp';
  const term = vscode.window.createTerminal({ name: 'kton: connect Claude', shellPath: '/bin/bash' });
  term.show();
  term.sendText('claude mcp remove kton --scope user >/dev/null 2>&1; ' +
    `claude mcp add --scope user --transport http kton ${url} --header "Authorization: Bearer $(cat ${tokenFile})" && ` +
    'echo && echo "kton is connected: start a new Claude conversation, then ask e.g. \"which of my recorded steps have no description?\""');
}

// the cockpit repository a project is in (cockpitRepoOf takes a file and starts in its folder)
const repoOfProject = (project) => cockpitRepoOf(path.join(project, '.kton-probe'));

// ------------------------------------------------------------------ copy a file over
// The bytes of a file by its hash: kept in the CAS, or a file of a recorded project with these
// bytes, or the file a record of the repository's federation names (another analyst's script or
// dataset) in that source's folder. Always checked against the hash.
async function findBytes(project, sum) {
  if (fs.existsSync(casFile(sum)) && (await sha256File(casFile(sum))) === sum) return { file: casFile(sum), from: 'the recorder\'s copy' };
  for (const p of (await getJSON(viewerUrl() + 'api/projects').catch(() => [])) || []) {
    const files = (await getJSON(`${viewerUrl()}api/files?project=${encodeURIComponent(p.path)}`, 10000).catch(() => [])) || [];
    for (const f of files) if (f.sha256 === sum && fs.existsSync(f.path) && (await sha256File(f.path)) === sum) return { file: f.path, from: f.path };
  }
  // the target's repository first, then those of the other recorded projects: each asks kton
  // through its own federation and trust tiers
  const repos = [...new Set([repoOfProject(project),
    ...((await getJSON(viewerUrl() + 'api/projects').catch(() => [])) || []).map((p) => repoOfProject(p.path))].filter(Boolean))];
  for (const repo of repos) {
    const found = await findInRepo(repo, sum);
    if (found) return found;
  }
  return null;
}
async function findInRepo(repo, sum) {
  let cfg = {};
  try { cfg = JSON.parse(fs.readFileSync(path.join(repo, 'cockpit.config.json'), 'utf8')); } catch (e) { return null; }
  const dirs = [repo, ...(((cfg.federation || {}).sources) || []).filter((x) => x.kind === 'dir' && x.path).map((x) => path.isAbsolute(x.path) ? x.path : path.join(repo, x.path))];
  const ids = new Set();
  for (const q of ['producer', 'uses']) {
    const a = await askCockpit(repo, { query: q, ref: 'sha256:' + sum });
    for (const id of (a && a.included) || []) ids.add(id);
  }
  for (const id of ids) {
    const a = await askCockpit(repo, { query: 'record', ref: id });
    const rec = (a && a.record) || {};
    for (const f of [...(rec.inputs || []), ...(rec.outputs || [])]) {
      if (String(f.hash || '').replace(/^sha256:/, '') !== sum || !f.path) continue;
      for (const d of dirs) {
        const cand = path.join(d, f.path);
        if (fs.existsSync(cand) && (await sha256File(cand)) === sum) return { file: cand, from: cand };
      }
    }
  }
  return null;
}
async function copyIntoProject(project, sum, name, to) {
  const target = path.join(project, to || name || sum.slice(0, 12));
  const rel = path.relative(project, target);
  if (rel.startsWith('..') || path.isAbsolute(rel)) return { ok: false, message: `${to} is not inside the project` };
  const src = await findBytes(project, sum);
  if (!src) return { ok: false, message: `no copy of ${name || sum.slice(0, 12)} (sha256 ${sum.slice(0, 12)}) is reachable here: not kept, not in a recorded project, not in a federated source's folder` };
  const exists = fs.existsSync(target);
  if (exists && (await sha256File(target)) === sum) return { ok: true, message: `${rel} has these bytes already` };
  const pick = await vscode.window.showInformationMessage(
    `Copy ${name || 'the file'} (sha256 ${sum.slice(0, 12)}) from ${src.from} into ${path.basename(project)}/${rel}?` +
    (exists ? ` ${rel} exists with other content and will be replaced.` : ''), { modal: true }, exists ? 'Replace' : 'Copy');
  if (!pick) return { ok: false, message: 'declined' };
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.copyFileSync(src.file, target);
  fs.chmodSync(target, 0o644); // a copy from the CAS is read-only
  vscode.window.setStatusBarMessage(`kton: copied into ${rel}`, 5000);
  return { ok: true, message: `copied into ${rel} (from ${src.from})`, data: { path: target } };
}

// ------------------------------------------------------------------ the assistant's requests (ktonwatch /api/ui/events)
// What an assistant asks through the MCP tools and only the editor can do. One subscription per
// window, again after ktonwatch restarts; each event is answered (POST /api/ui/answer).
async function uiDo(context, ev) {
  const a = ev.args || {};
  switch (ev.action) {
    case 'open_story': {
      const files = (await getJSON(`${viewerUrl()}api/files?project=${encodeURIComponent(a.project)}`, 10000).catch(() => [])) || [];
      const f = files.find((x) => x.path === a.path && x.sha256 === a.sha256 && x.produced_by);
      if (!f) return { ok: false, message: `no recorded step wrote ${path.relative(a.project, a.path)} with these bytes` };
      await openStory(context, a.project, f);
      return { ok: true, message: `opened the story of ${path.relative(a.project, a.path)}` };
    }
    case 'open_graph': {
      const repo = repoOfProject(a.project) || a.project;
      const e = await openInKton(a.project, repo, a.sha256, a.path ? path.basename(a.path) : a.sha256.slice(0, 12));
      return e ? { ok: true, message: 'the graph shows it' } : { ok: false, message: 'the graph could not be opened' };
    }
    case 'select': {
      let e = panelOf(a.project);
      if (!e) {
        const first = (a.selection || [])[0];
        if (!first) return { ok: true, message: 'no graph open, nothing to clear' };
        e = await openInKton(a.project, repoOfProject(a.project) || a.project, first.replace(/^sha256:/, ''), 'selection');
        if (!e) return { ok: false, message: 'the graph could not be opened' };
      }
      e.panel.reveal(vscode.ViewColumn.Beside);
      e.panel.webview.postMessage({ type: 'set', state: { view: { select: { multi: true } }, selection: a.selection || [] } });
      return { ok: true, message: `${(a.selection || []).length} selected` };
    }
    case 'get_selection': {
      const e = panelOf(a.project);
      if (!e) return { ok: false, message: 'no graph is open' };
      return { ok: true, message: `${(e.selection || []).length} step(s) selected`, data: { project: e.project, selection: e.selection || [] } };
    }
    case 'copy':
      return copyIntoProject(a.project, a.sha256, a.name, a.to);
  }
  return { ok: false, message: 'unknown action ' + ev.action };
}
function listenUI(context) {
  let stopped = false, req;
  const connect = () => {
    if (stopped) return;
    req = http.get(viewerUrl() + 'api/ui/events', (res) => {
      if (res.statusCode !== 200) { res.resume(); return setTimeout(connect, 10000); }
      let buf = '';
      res.on('data', (c) => {
        buf += c;
        let i;
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const chunk = buf.slice(0, i); buf = buf.slice(i + 2);
          const line = chunk.split('\n').find((l) => l.startsWith('data: '));
          if (!line) continue;
          let ev; try { ev = JSON.parse(line.slice(6)); } catch (e) { continue; }
          uiDo(context, ev).catch((e) => ({ ok: false, message: String(e.message || e) }))
            .then((ans) => sendJSON('POST', viewerUrl() + 'api/ui/answer', Object.assign({ id: ev.id }, ans)).catch(() => { /* another window answered */ }));
        }
      });
      res.on('end', () => setTimeout(connect, 3000));
      res.on('error', () => setTimeout(connect, 3000));
    });
    req.on('error', () => setTimeout(connect, 10000));
  };
  connect();
  context.subscriptions.push({ dispose: () => { stopped = true; if (req) req.destroy(); } });
}

async function openInBrowser() {
  if (!(await ensureService())) return;
  vscode.env.openExternal(await vscode.env.asExternalUri(vscode.Uri.parse(viewerUrl())));
}

// ------------------------------------------------------------------ activation
let extContext; // for what opens a story from outside one (the viewer's actions)
function activate(context) {
  extContext = context;
  out = vscode.window.createOutputChannel('kton');
  status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  status.command = 'kton.toggleRecording';
  showStatus(); status.show();
  const sync = setInterval(showStatus, 3000); // another window may have switched it
  context.subscriptions.push(out, status, { dispose: () => clearInterval(sync) });
  doctor = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 99);
  doctor.command = 'kton.doctor';
  const cfgWatch = vscode.workspace.createFileSystemWatcher('**/{cockpit.config.json,keys/*,registry/keys/*,templates/*}');
  const again = () => runDoctor(true);
  context.subscriptions.push(doctor, cfgWatch, cfgWatch.onDidChange(again), cfgWatch.onDidCreate(again), cfgWatch.onDidDelete(again),
    vscode.window.onDidChangeActiveTextEditor(() => runDoctor(false)),
    vscode.workspace.onDidChangeWorkspaceFolders(again),
    vscode.commands.registerCommand('kton.doctor', showDoctor),
    vscode.commands.registerCommand('kton.connectClaude', connectClaude));
  moveOldNotes(context);
  ensureService().then(() => listenUI(context));
  runDoctor(true);
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
module.exports = { activate, deactivate, _test: { listenUI, findBytes, copyIntoProject, ktonIntent, parseDoctor, parseDoctorJSON, cockpitRepoOf, reproduce, publishedSteps, publishText, Tree, setOut: (o) => { out = o; } } };
