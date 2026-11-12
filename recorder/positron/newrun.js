// "New run" (MH 11.11., joint proposal "parent = version"): copy a run folder that holds one
// process's files to the next free number at the same level, with only that process's inputs,
// renaming only what points at the run itself. The model content stays byte for byte; the
// scientist edits it and runs the copy as usual. Without vscode, so it can be tested with node
// (newrun.test.js); extension.js wires it to the explorer.
'use strict';
const path = require('path');

// nextName: run6 among run1…run7 → run8 (the first number past the highest at that level, so a
// sibling branch is never taken); the digits keep their width (run06 → run07).
function nextName(name, siblings) {
  const m = name.match(/^(.*?)(\d+)$/);
  if (!m) return null;
  const [, prefix, digits] = m;
  let max = parseInt(digits, 10);
  for (const s of siblings) {
    const sm = s.match(/^(.*?)(\d+)$/);
    if (sm && sm[1] === prefix) max = Math.max(max, parseInt(sm[2], 10));
  }
  return prefix + String(max + 1).padStart(digits.length, '0');
}

// renameRef: in a path or file name, the run's folder segment and a name ending in the run's
// number (sdtab6 for run6) follow the new run; everything else stays.
function renameRef(ref, oldName, newName) {
  const oldNum = oldName.match(/(\d+)$/)[1], newNum = newName.match(/(\d+)$/)[1];
  return ref.split('/').map((seg) => {
    if (seg === oldName) return newName;
    const m = seg.match(/^(.*?[^\d])(\d+)(\.[^/]*)?$/);
    if (m && m[2] === oldNum) return m[1] + newNum + (m[3] || '');
    return seg;
  }).join('/');
}

// renameCtl: the copy of a control stream. Renamed: $TABLE/$EST … FILE= paths and the header's
// `; model:`; `; based_on:` names the run copied from; the copied run's own result lines
// (`; final:`, `; note: Result …`) are dropped. Nothing else changes (MH 11.11.).
function renameCtl(text, oldName, newName) {
  const changes = [];
  const lines = text.split('\n');
  const out = [];
  let hasBasedOn = false, modelAt = -1;
  for (const line of lines) {
    let l = line;
    if (/^;\s*final:/i.test(l)) { changes.push(`dropped "${l.trim()}" (it was ${oldName}'s)`); continue; }
    if (/^;\s*note:\s*Result/i.test(l)) { changes.push(`dropped ${oldName}'s result note`); continue; }
    const mm = l.match(/^(;\s*model:\s*)(\S+)(.*)$/);
    if (mm) {
      l = mm[1] + newName + mm[3];
      modelAt = out.length;
      if (mm[2] !== newName) changes.push(`; model: ${mm[2]} → ${newName}`);
    }
    const bm = l.match(/^(;\s*based_on:\s*)(.*)$/);
    if (bm) {
      hasBasedOn = true;
      if (bm[2].trim() !== oldName) { changes.push(`; based_on: ${bm[2].trim()} → ${oldName}`); l = bm[1] + oldName; }
    }
    l = l.replace(/(\bFILE\s*=\s*)("?)([^\s"]+)\2/gi, (all, k, q, ref) => {
      const nref = renameRef(ref, oldName, newName);
      if (nref !== ref) changes.push(`FILE=${ref} → ${nref}`);
      return k + q + nref + q;
    });
    out.push(l);
  }
  if (!hasBasedOn) {
    const line = `; based_on:    ${oldName}`;
    out.splice(modelAt >= 0 ? modelAt + 1 : 0, 0, line);
    changes.push(`added "${line.trim()}"`);
  }
  return { text: out.join('\n'), changes };
}

// plan decides whether a folder holds one process's files, from ktonwatch's file versions of the
// project (/api/files: path, role, produced_by, read_by) and the recorded call of each step
// (cmdOf(session, task)). The files to copy are the folder's files no step produced that the
// writing call read: for NONMEM, the control stream. A folder no recorded step wrote into (a
// model written, never run) qualifies when it holds exactly one control stream.
// Returns {ok, reason, inputs: [repo-relative paths], call}.
function plan(folderRel, files, cmdOf, listing) {
  const inFolder = (p) => path.dirname(p) === folderRel;
  const key = (r) => r.session + '/' + r.task;
  const writers = new Map();
  for (const f of files) if (inFolder(f.path) && f.produced_by) writers.set(key(f.produced_by), f.produced_by);
  const ctls = listing.filter((n) => /\.(ctl|mod)$/i.test(n));
  if (!writers.size) {
    if (ctls.length === 1) return { ok: true, inputs: [path.join(folderRel, ctls[0])], call: '' };
    return { ok: false, reason: ctls.length ? `${ctls.length} control streams and no recorded run in ${folderRel}` : `no recorded run and no control stream in ${folderRel}` };
  }
  const calls = new Set([...writers.values()].map((r) => cmdOf(r.session, r.task)));
  if (calls.size > 1) return { ok: false, reason: `${calls.size} different recorded calls wrote into ${folderRel}: ${[...calls].join(' | ')}` };
  const steps = new Set(writers.keys());
  const inputs = [...new Set(files.filter((f) => inFolder(f.path) && !f.produced_by &&
    (f.read_by || []).some((r) => steps.has(key(r)))).map((f) => f.path))].sort();
  if (!inputs.length) return { ok: false, reason: `the recorded run in ${folderRel} read none of its files from there` };
  return { ok: true, inputs, call: [...calls][0] };
}

// recordDerivation: what New run did, in <root>/.kton/derivations.json, keyed by the new control
// stream's project-relative path: {from, action: "new-run", at}. The improve shim takes based-on from
// it before the header (cockpit-improve-backend, 12.11.: MH's run4 copied from run2 got the demo's
// header, based_on run3); the same path made again replaces its entry. fsMod: fs, for tests.
function recordDerivation(root, toRel, fromRel, fsMod = require('fs'), now = new Date()) {
  const file = path.join(root, '.kton', 'derivations.json');
  let all = {};
  try { all = JSON.parse(fsMod.readFileSync(file, 'utf8')) || {}; } catch (e) { all = {}; }
  all[toRel] = { from: fromRel, action: 'new-run', at: now.toISOString().replace(/\.\d+Z$/, 'Z') };
  fsMod.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = file + '.tmp';
  fsMod.writeFileSync(tmp, JSON.stringify(all, null, 2) + '\n');
  fsMod.renameSync(tmp, file);
  return all;
}

module.exports = { nextName, renameRef, renameCtl, plan, recordDerivation };
