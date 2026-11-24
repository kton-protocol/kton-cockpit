// Workgroup: set up (MH 16.11.) — what a workgroup member's machine needs, step by step, in place of the
// README: an SSH key for the hub, the connection through the gate, the package (kw, delivery, the person's
// surfaces.d file, send/inbox wrappers), the repositories and lane folders, and the start of the surface's
// lanes. Without vscode, so it can be tested with node (setup.test.js); extension.js shows it as a page.
'use strict';
const cp = require('child_process');
const fs = require('fs');
const os = require('os');
const path = require('path');

const GATE = '/home/lanes/kton-coordination/tools/wg-gate';
const home = (p) => String(p || '').replace(/^~(?=\/|$)/, os.homedir());

function run(cmd, args, opts = {}) {
  return new Promise((resolve) => {
    cp.execFile(cmd, args, { maxBuffer: 64 << 20, encoding: opts.encoding || 'utf8', ...opts }, (err, stdout, stderr) =>
      resolve({ ok: !err, out: stdout, err: String(stderr || (err && err.message) || '').trim() }));
  });
}

// defaults: the member's own user name, the hub, the key and where things go; each can be changed on the page
function defaults() {
  return {
    user: os.userInfo().username, host: 'ai1.hc.scintecodev.internal', key: '~/.ssh/id_ed25519',
    target: '~/kton-coordination', repos: '~/git-repos', cloneFrom: 'git@bitbucket.org:scinteco/',
  };
}

// 1 the SSH key: made when missing (no passphrase prompt: the page can't answer one), its public half shown
async function ensureKey(o) {
  const key = home(o.key);
  let created = false;
  if (!fs.existsSync(key)) {
    fs.mkdirSync(path.dirname(key), { recursive: true, mode: 0o700 });
    const r = await run('ssh-keygen', ['-q', '-t', 'ed25519', '-N', '', '-C', `${o.user}@${os.hostname()} kton workgroup`, '-f', key]);
    if (!r.ok) return { ok: false, error: r.err };
    created = true;
  }
  let pub = '';
  try { pub = fs.readFileSync(key + '.pub', 'utf8').trim(); } catch (e) { return { ok: false, error: `no ${key}.pub` }; }
  return { ok: true, created, pub };
}

const sshArgs = (o) => ['-i', home(o.key), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', `${o.user}@${o.host}`];
const gate = (o, ...args) => run('ssh', [...sshArgs(o), 'sudo', '-u', 'lanes', GATE, ...args], args[0] === 'bundle' ? { encoding: 'buffer' } : {});

// 2 the connection: the gate answers with the person's lanes and surfaces
async function checkLanes(o) {
  const r = await gate(o, 'lanes');
  if (r.ok) return { ok: true, lanes: String(r.out).split('\n').filter(Boolean) };
  const why = /timed out|No route|Could not resolve|Network is unreachable/i.test(r.err) ? ' — is the VPN on?'
    : /Permission denied/i.test(r.err) ? ' — has MH added your public key on the hub?'
    : /no workgroup setup/i.test(r.err) ? ' — MH has not set up your lanes yet' : '';
  return { ok: false, error: r.err + why };
}

// 3 the package: the gate's bundle into the target, and the files that say who this is and how to reach the hub.
// A key other than the default one gets an ssh alias (kton-hub-<user>), since the wrappers and the delivery
// call plain `ssh $(cat .hub)`.
async function install(o) {
  const target = home(o.target);
  const r = await gate(o, 'bundle');
  if (!r.ok || !r.out || !r.out.length) return { ok: false, error: r.err || 'empty bundle' };
  fs.mkdirSync(target, { recursive: true });
  const tmp = path.join(os.tmpdir(), `kton-bundle-${process.pid}.tar`);
  fs.writeFileSync(tmp, r.out);
  const x = await run('tar', ['-xf', tmp, '-C', target]);
  fs.unlinkSync(tmp);
  if (!x.ok) return { ok: false, error: x.err };
  let hub = `${o.user}@${o.host}`, alias = null;
  if (home(o.key) !== home('~/.ssh/id_ed25519')) {
    alias = `kton-hub-${o.user}`;
    const conf = home('~/.ssh/config');
    const have = fs.existsSync(conf) ? fs.readFileSync(conf, 'utf8') : '';
    if (!new RegExp(`^Host ${alias}$`, 'm').test(have)) {
      fs.appendFileSync(conf, `\n# kton workgroup (Workgroup: set up)\nHost ${alias}\n  HostName ${o.host}\n  User ${o.user}\n` +
        `  IdentityFile ${home(o.key)}\n  IdentitiesOnly yes\n`, { mode: 0o600 });
    }
    hub = alias;
  }
  localize(o);
  fs.writeFileSync(path.join(target, '.person'), o.user + '\n');
  fs.writeFileSync(path.join(target, '.hub'), hub + '\n');
  fs.writeFileSync(path.join(target, '.gate'), GATE + '\n');
  return { ok: true, target, hub, alias, files: fs.readdirSync(target).sort() };
}

// localize: the local copy of surfaces.d/<user>.json with this machine's absolute paths (kw takes them as
// they are: a '~/…' lane folder made tmux start the lane in the home folder, and --add-dir skipped every
// repository); repositories named under ~/git-repos point to the chosen repos folder
function localize(o) {
  const f = path.join(home(o.target), 'surfaces.d', `${o.user}.json`);
  const c = JSON.parse(fs.readFileSync(f, 'utf8'));
  const repo = (a) => (/\/git-repos\//.test(a) ? path.join(home(o.repos), path.basename(home(a).replace(/\/+$/, ''))) : home(a));
  for (const sf of Object.values(c)) {
    if (!sf || !sf.lanes) continue;
    if (sf.surface && sf.surface.dir) sf.surface.dir = home(sf.surface.dir);
    for (const l of Object.values(sf.lanes)) {
      if (l.dir) l.dir = home(l.dir);
      if (l.add) l.add = l.add.map(repo);
    }
  }
  fs.writeFileSync(f, JSON.stringify(c, null, 2) + '\n');
  return c;
}

// trust: Claude Code asks once per folder whether to trust it, and a lane started in tmux waits at that
// question (a nudge typed into it answers 'No, exit'). The person set these folders up for their lanes,
// so they are marked trusted in their ~/.claude.json, as answering the question would.
function trust(dirs) {
  const f = home('~/.claude.json');
  let c = {};
  try { c = JSON.parse(fs.readFileSync(f, 'utf8')); } catch (e) { c = {}; }
  c.projects = c.projects || {};
  for (const d of dirs) c.projects[d] = { ...(c.projects[d] || {}), hasTrustDialogAccepted: true };
  const tmp = `${f}.kton-${process.pid}`;
  fs.writeFileSync(tmp, JSON.stringify(c, null, 2), { mode: 0o600 });
  fs.renameSync(tmp, f);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// the person's surfaces, lanes, their folders and the repositories they add (surfaces.d/<user>.json)
function readSetup(o) {
  const f = path.join(home(o.target), 'surfaces.d', `${o.user}.json`);
  return JSON.parse(fs.readFileSync(f, 'utf8'));
}
function planRepos(o) {
  const c = readSetup(o);
  const repos = new Map(), dirs = new Set();
  for (const sf of Object.values(c)) {
    if (!sf || !sf.lanes) continue;
    if (sf.surface && sf.surface.dir) dirs.add(home(sf.surface.dir));
    for (const l of Object.values(sf.lanes)) {
      if (l.dir) dirs.add(home(l.dir));
      for (const a of l.add || []) {
        const p = home(a).replace(/\/+$/, '');
        // a repository named under ~/git-repos lands in the chosen repos folder
        const at = /\/git-repos\//.test(a) ? path.join(home(o.repos), path.basename(p)) : p;
        repos.set(at, { path: at, name: path.basename(p), exists: fs.existsSync(path.join(at, '.git')),
          url: (c.repos && c.repos[path.basename(p)]) || `${o.cloneFrom}${path.basename(p)}.git` });
      }
    }
  }
  return { repos: [...repos.values()], dirs: [...dirs].sort() };
}
// 4 clone what is missing, make the folders
async function cloneAndMake(o, urls = {}) {
  const p = planRepos(o), done = [];
  for (const d of p.dirs) { fs.mkdirSync(d, { recursive: true }); done.push({ dir: d, ok: true }); }
  for (const r of p.repos) {
    if (r.exists) { done.push({ repo: r.name, ok: true, note: 'already there' }); continue; }
    fs.mkdirSync(path.dirname(r.path), { recursive: true });
    const c = await run('git', ['clone', '-q', urls[r.name] || r.url, r.path]);
    done.push({ repo: r.name, ok: c.ok, note: c.ok ? 'cloned' : c.err });
  }
  return { ok: done.every((d) => d.ok), done };
}

// the first words a lane gets: who it is and how it talks to the others (through the gate)
function identity(o, surface, lane, cfg) {
  return `You are the kton lane '${lane}' (registry lane ${cfg.lane}) of ${o.user}'s surface '${surface}', working in ${cfg.dir}. ` +
    `Read your messages with ${o.target}/inbox ${lane} --full and answer or ask with ${o.target}/send ${lane} <template> field=value …; ` +
    'both go through the gate on the hub and reach only your own lanes. Work only in your folder and the repositories listed for you.';
}
// 5 start the surfaces' lanes with kw and give each lane its identity once
async function start(o) {
  const target = home(o.target), kw = path.join(target, 'kw'), c = localize(o), out = [];
  trust(planRepos(o).dirs);
  for (const [surface, sf] of Object.entries(c)) {
    if (!sf || !sf.lanes) continue;
    const s = await run(kw, ['start', surface], { cwd: target });
    out.push({ step: `kw start ${surface}`, ok: s.ok, note: (s.ok ? String(s.out) : s.err).trim() });
    if (!s.ok) continue;
    await sleep(o.bootMs == null ? 12000 : o.bootMs); // the lanes' Claude has started before it hears its first words
    for (const [lane, cfg] of Object.entries(sf.lanes)) {
      const n = await run(kw, ['nudge', lane, identity(o, surface, lane, cfg)], { cwd: target });
      out.push({ step: `identity → ${lane}`, ok: n.ok, note: (n.ok ? String(n.out) : n.err).trim() });
    }
  }
  return { ok: out.every((x) => x.ok), out, surfaces: Object.entries(c).filter(([, sf]) => sf && sf.surface)
    .map(([n, sf]) => ({ name: n, dir: home(sf.surface.dir) })) };
}

module.exports = { GATE, home, defaults, ensureKey, sshArgs, checkLanes, install, localize, trust, readSetup, planRepos, cloneAndMake, identity, start };
