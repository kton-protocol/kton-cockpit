// Reproduce a file's whole lineage LOCALLY (git backend; MH 17.11., demo): every recorded step again, in an
// empty folder, in the recorded order and with the recorded call, each on the outputs the reproduction made
// upstream; NONMEM through the project's own tools/nonmem/nmfe75 (the licensed container). Then each output
// compared with the recorded bytes: R outputs byte for byte, NONMEM's listing, XML and CPU file through the
// project's tools/nonmem/normalize-nonmem.sh when it is there. The result has the same form as
// `cockpit-improve reproduce --workflow` (improve backend), so the story shows it the same way: per step
// reproduces / differs / failed, where a difference began, and both versions side by side (reproducedPath,
// normalized). Nothing of it is recorded (no LD_PRELOAD, no trace log) or published.
// Without vscode, so it can be run and tested with node; extension.js wires it to the story.
'use strict';
const cp = require('child_process');
const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');
const http = require('http');

const getJSON = (url, ms = 60000) => new Promise((resolve, reject) => {
  const req = http.get(url, (res) => {
    let b = '';
    res.on('data', (c) => { b += c; });
    res.on('end', () => { if (res.statusCode !== 200) return reject(new Error(`${res.statusCode} ${b.trim()}`)); try { resolve(JSON.parse(b)); } catch (e) { reject(e); } });
  });
  req.on('error', reject);
  req.setTimeout(ms, () => req.destroy(new Error('timeout')));
});
const sha = (p) => { try { return crypto.createHash('sha256').update(fs.readFileSync(p)).digest('hex'); } catch (e) { return ''; } };
const bare = (h) => String(h || '').replace(/^sha256:/, '');
const casFile = (sum) => path.join(process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache'), 'kton-cockpit', 'cas', 'sha256', sum.slice(0, 2), sum);
const NONMEM_TEXT = /\.(lst|xml|cpu)$/i;

// the recorded bytes of a file version: the working file while it still has them, else the content store
function recordedBytes(repo, rel, sum) {
  const w = path.join(repo, rel);
  if (sha(w) === sum) return w;
  if (fs.existsSync(casFile(sum)) && sha(casFile(sum)) === sum) return casFile(sum);
  return null;
}

function run(cmd, cwd, env, onLine) {
  return new Promise((resolve) => {
    const p = cp.spawn('/bin/bash', ['-c', cmd], { cwd, env });
    let tail = '';
    const take = (d) => { const t = d.toString(); tail = (tail + t).slice(-4000); if (onLine) onLine(t); };
    p.stdout.on('data', take); p.stderr.on('data', take);
    p.on('error', (e) => resolve({ code: 127, tail: String(e.message) }));
    p.on('close', (code) => resolve({ code, tail }));
  });
}

// plan: the target's lineage as ktonwatch tells it: the story's steps in data-flow order, each with its
// draft (the recorded call and the inputs and outputs it names) and the recorded bytes of each
async function plan(viewer, project, target, sum) {
  const q = `project=${encodeURIComponent(project)}`;
  const story = await getJSON(`${viewer}api/story?${q}&path=${encodeURIComponent(target)}&sha=${encodeURIComponent(sum)}`);
  const files = await getJSON(`${viewer}api/files?${q}`);
  const key = (r) => r && `${r.session}/${r.task}`;
  const reads = {}, writes = {};
  for (const f of files) {
    for (const r of f.read_by || []) (reads[key(r)] = reads[key(r)] || {})[f.path] = f.sha256;
    if (f.produced_by) (writes[key(f.produced_by)] = writes[key(f.produced_by)] || {})[f.path] = f.sha256;
  }
  const rel = (p) => (path.isAbsolute(p) ? path.relative(project, p) : p);
  const steps = [];
  for (const s of story.steps || []) {
    if (s.hand || !(s.writes || []).length) continue;
    const k = `${s.session}/${s.task}`;
    const d = await getJSON(`${viewer}api/draft?session=${encodeURIComponent(s.session)}&task=${encodeURIComponent(s.task)}`).catch(() => null);
    if (!d || !d.cmd) continue;
    const r = {}, w = {};
    for (const [p, h] of Object.entries(reads[k] || {})) r[rel(p)] = h;
    for (const [p, h] of Object.entries(writes[k] || {})) w[rel(p)] = h;
    const inputs = (d.inputs || []).filter((f) => f.use).map((f) => ({ path: f.path, sha256: r[f.path] || '' }));
    const outputs = (d.outputs || []).filter((f) => f.use).map((f) => ({ path: f.path, sha256: w[f.path] || '' })).filter((o) => o.sha256);
    if (!outputs.length) continue;
    steps.push({ key: k, call: d.cmd, inputs, outputs });
  }
  return { repo: story.repo || project, steps };
}

// reproduce: run the plan in a fresh folder and compare. onProgress({i, n, call, state}).
async function reproduce(o) {
  const { viewer, project, target, sum, name } = o;
  const onProgress = o.onProgress || (() => {});
  const startedAt = new Date().toISOString().replace(/\.\d+Z$/, 'Z');
  const p = await plan(viewer, project, target, sum);
  const repo = p.repo;
  const work = path.join(o.workRoot || path.join(os.homedir(), '.cache', 'kton-cockpit', 'repro-local'), name);
  fs.rmSync(work, { recursive: true, force: true });
  fs.mkdirSync(work, { recursive: true });
  // the environment the steps need that is no recorded input: the project's tools and R settings
  for (const e of ['tools', '.Renviron']) if (fs.existsSync(path.join(repo, e))) fs.cpSync(path.join(repo, e), path.join(work, e), { recursive: true });
  const env = { ...process.env, PATH: `${path.join(os.homedir(), '.local', 'bin')}:${process.env.PATH}` };
  for (const v of ['LD_PRELOAD', 'KTON_TRACE_LOG', 'KTON_TRACE_HASH']) delete env[v];
  const produced = new Set(p.steps.flatMap((s) => s.outputs.map((x) => x.path)));
  const norm = path.join(work, 'tools', 'nonmem', 'normalize-nonmem.sh');
  const differing = new Set(), failedOut = new Set();
  const steps = [];
  let i = 0;
  for (const s of p.steps) {
    i++;
    onProgress({ i, n: p.steps.length, call: s.call, state: 'started' });
    // inputs nobody upstream made: the recorded bytes into the folder
    const missing = [];
    for (const inp of s.inputs) {
      if (produced.has(inp.path) && steps.some((st) => st.outputs.some((x) => x.path === inp.path))) continue; // made upstream here
      const dst = path.join(work, inp.path);
      if (fs.existsSync(dst)) continue;
      const src = inp.sha256 ? recordedBytes(repo, inp.path, inp.sha256) : path.join(repo, inp.path);
      if (!src || !fs.existsSync(src)) { missing.push(inp.path); continue; }
      fs.mkdirSync(path.dirname(dst), { recursive: true });
      fs.copyFileSync(src, dst);
      fs.chmodSync(dst, 0o644);
    }
    for (const out of s.outputs) fs.mkdirSync(path.dirname(path.join(work, out.path)), { recursive: true });
    const t0 = Date.now();
    const r = missing.length ? { code: 2, tail: `recorded inputs not available: ${missing.join(', ')}` } : await run(s.call, work, env);
    const outputs = [];
    for (const out of s.outputs) {
      const repPath = path.join(work, out.path);
      const got = sha(repPath);
      const o2 = { path: out.path, recorded: 'sha256:' + out.sha256, reproduced: got ? 'sha256:' + got : '', same: got === out.sha256, how: 'bytes' };
      if (!o2.same && got && NONMEM_TEXT.test(out.path) && fs.existsSync(norm)) {
        const rec = recordedBytes(repo, out.path, out.sha256);
        if (rec) {
          const nd = path.join(work, '.normalized');
          fs.mkdirSync(nd, { recursive: true });
          const a = path.join(nd, out.path.replace(/\//g, '__') + '.recorded'), b = path.join(nd, out.path.replace(/\//g, '__') + '.reproduced');
          const ra = await run(`sh ${JSON.stringify(norm)} ${JSON.stringify(rec)} ${JSON.stringify(a)}`, work, env);
          const rb = await run(`sh ${JSON.stringify(norm)} ${JSON.stringify(repPath)} ${JSON.stringify(b)}`, work, env);
          if (ra.code === 0 && rb.code === 0) {
            o2.how = 'normaliser';
            o2.same = sha(a) === sha(b);
            o2.normalized = { recorded: a, reproduced: b, script: 'tools/nonmem/normalize-nonmem.sh' };
          }
        }
      }
      if (!o2.same) {
        o2.reproducedPath = got ? repPath : undefined;
        // where the difference began: an input this step read that already differed upstream
        const ups = s.inputs.map((x) => x.path).filter((x) => differing.has(x) || failedOut.has(x));
        if (ups.length) o2.becauseOf = ups;
      }
      outputs.push(o2);
    }
    const state = r.code !== 0 && outputs.every((x) => !x.reproduced) ? 'failed' : outputs.every((x) => x.same) ? 'reproduces' : 'differs';
    for (const x of outputs) if (!x.same) (x.reproduced ? differing : failedOut).add(x.path);
    const nSame = outputs.filter((x) => x.same).length;
    steps.push({
      i, call: s.call, state, outputs, original: { name: 'recorded', key: s.key },
      detail: state === 'failed' ? `the step did not run: ${r.tail.trim().split('\n').slice(-3).join(' | ')}` : `${nSame} of ${outputs.length} outputs the same (${Math.round((Date.now() - t0) / 1000)} s)`,
      environment: { recorded: { where: 'local (recorded)' }, reproduced: { where: 'local, an empty folder', ...(/^nmfe75\b/.test(s.call) ? { tool: 'NONMEM 7.5.1 container (tools/nonmem/nmfe75)' } : {}) } },
    });
    onProgress({ i, n: p.steps.length, call: s.call, state });
  }
  const count = (st) => steps.filter((x) => x.state === st).length;
  const result = {
    tree: name, target: path.relative(repo, target), local: true, startedAt, finishedAt: new Date().toISOString().replace(/\.\d+Z$/, 'Z'), folder: work,
    steps, summary: { n: steps.length, reproduces: count('reproduces'), differs: count('differs'),
      differsOwn: steps.filter((x) => x.state === 'differs' && x.outputs.every((o2) => o2.same || !o2.becauseOf)).length, failed: count('failed') },
  };
  const dir = path.join(repo, '.cockpit-improve', 'reproduce');
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, name + '.json'), JSON.stringify(result, null, 2) + '\n');
  return result;
}

module.exports = { plan, reproduce };
