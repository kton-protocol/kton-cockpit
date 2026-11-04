// What the view shows, without VS Code: kw's list, the open messages per lane, and what is new.
// Kept apart from extension.js so it can be tested with plain node (model.test.js).
'use strict';

// parseKwList reads `kw list`: a "◆ <surface>  (tmux kw-<surface>: up|not started)" line per
// surface, then a line per lane with its state (● claude = running, ○ <cmd> = a window without
// claude, — = no window), its session id, and @<host> when it runs on another machine.
function parseKwList(text) {
  const surfaces = [];
  let cur = null;
  for (const line of text.split('\n')) {
    const s = line.match(/^◆ (\S+)\s+\(tmux (\S+): (up|not started)\)/);
    if (s) {
      cur = { name: s[1], tmux: s[2], up: s[3] === 'up', lanes: [] };
      surfaces.push(cur);
      continue;
    }
    const l = line.match(/^ {3}(\S+)\s+(● \S+|○ \S+|—)\s*(.*)$/);
    if (l && cur) {
      const state = l[2] === '—' ? 'none' : l[2].startsWith('●') ? 'running' : 'window';
      const rest = l[3].trim().split(/\s+/).filter(Boolean);
      const at = rest.find((w) => w.startsWith('@'));
      cur.lanes.push({ name: l[1], state, session: rest.find((w) => !w.startsWith('@')) || '', host: at ? at.slice(1) : '' });
    }
  }
  return surfaces;
}

// laneOf maps the registry's lane path (active/cockpit-runtime) to its surface and lane name,
// from surfaces.json.
function laneIndex(surfacesJson) {
  const idx = {};
  for (const [surface, sf] of Object.entries(surfacesJson)) {
    if (surface.startsWith('_') || !sf || !sf.lanes) continue;
    for (const [lane, cfg] of Object.entries(sf.lanes)) {
      idx[cfg.lane] = { surface, lane };
    }
  }
  return idx;
}

// openByLane groups the rows of the package query inbox-open-all (SPARQL results JSON) by lane
// name: a list of messages {msg, title, area, from, when} per lane.
function openByLane(results, idx) {
  const out = {};
  for (const b of (results.results && results.results.bindings) || []) {
    const v = (k) => (b[k] ? b[k].value : '');
    const where = idx[v('lane')];
    if (!where) continue;
    (out[where.lane] = out[where.lane] || []).push({
      msg: v('msg'), title: v('title'), area: v('area'), from: sender(v('from')), when: v('when'),
    });
  }
  return out;
}

// sender is the lane a key belongs to: lane-<lane>-claims (the key file's name) → <lane>.
function sender(name) {
  return name.replace(/^lane-/, '').replace(/-claims$/, '');
}

// fresh lists the messages in now that were not in before, per lane.
function fresh(before, now) {
  const seen = new Set();
  for (const msgs of Object.values(before)) for (const m of msgs) seen.add(m.msg);
  const out = [];
  for (const [lane, msgs] of Object.entries(now)) {
    for (const m of msgs) if (!seen.has(m.msg)) out.push({ lane, ...m });
  }
  return out;
}

// SSE parses a server-sent event stream fed in chunks; onEvent(name, data) per event.
class SSE {
  constructor(onEvent) {
    this.onEvent = onEvent;
    this.buf = '';
  }
  push(chunk) {
    this.buf += chunk;
    let i;
    while ((i = this.buf.indexOf('\n\n')) >= 0) {
      const block = this.buf.slice(0, i);
      this.buf = this.buf.slice(i + 2);
      let name = 'message';
      const data = [];
      for (const line of block.split('\n')) {
        if (line.startsWith('event: ')) name = line.slice(7);
        else if (line.startsWith('data: ')) data.push(line.slice(6));
      }
      if (data.length) this.onEvent(name, data.join('\n'));
    }
  }
}

// surfaceSessions reads sessions.json's _surface_sessions: surface → {session, dir}, for the
// surfaces that run as a Claude conversation of their own.
function surfaceSessions(sessionsJson) {
  const out = {};
  for (const [name, v] of Object.entries((sessionsJson && sessionsJson._surface_sessions) || {})) {
    if (v && v.session && v.dir) out[name] = { session: v.session, dir: v.dir.replace(/\/+$/, '') };
  }
  return out;
}

// surfaceOfFolder is the surface whose folder is dir, if any.
function surfaceOfFolder(sessions, dir) {
  const d = (dir || '').replace(/\/+$/, '');
  return Object.keys(sessions).find((n) => sessions[n].dir === d);
}

// openFor lists the open messages routed to the lane whose folder is dir: a surface's own area
// (its lane is its folder relative to the workstreams, active/kton-coordination/surfaces/qm).
function openFor(results, dir) {
  const d = (dir || '').replace(/\/+$/, '');
  const out = [];
  for (const b of (results.results && results.results.bindings) || []) {
    const v = (k) => (b[k] ? b[k].value : '');
    const lane = v('lane');
    if (!lane || !(d === lane || d.endsWith('/' + lane))) continue;
    out.push({ msg: v('msg'), title: v('title'), area: v('area'), from: sender(v('from')), when: v('when') });
  }
  return out;
}

// doorbell is the line a surface's conversation gets about a new message: the content stays in
// the scope, the surface reads it from its inbox.
function doorbell(surface, x) {
  return `New message for ${surface} in area ${x.area} from ${x.from}: "${x.title}". Read it in your inbox and act on it.`;
}

// machineWarning says what is short on a machine of `kw stats --json`, or '': SCI-0350 crashed
// twice on 03.11. with its memory gone and its swap full.
const LOW_MEMORY_MB = 1536;
function machineWarning(m) {
  const mem = m && m.memory;
  if (!mem) return '';
  const out = [];
  if (mem.available_mb < LOW_MEMORY_MB) out.push(`${(mem.available_mb / 1024).toFixed(1)} GB memory available`);
  if (mem.swap_total_mb > 0 && mem.swap_free_mb / mem.swap_total_mb < 0.05) out.push('swap nearly full');
  return out.join(', ');
}

// machineLine is a machine's summary: memory used of total, swap, load.
function machineLine(m) {
  if (m.error) return m.error;
  const gb = (mb) => (mb / 1024).toFixed(1);
  const mem = m.memory;
  return `${gb(mem.total_mb - mem.available_mb)}/${gb(mem.total_mb)} GB · swap ${gb(mem.swap_total_mb - mem.swap_free_mb)}/${gb(mem.swap_total_mb)} GB · load ${m.load[0].toFixed(1)}`;
}

// lanesOn lists the lanes of a host from `kw stats --json`: the largest first, stopped ones last.
function lanesOn(stats, host) {
  return (stats.lanes || []).filter((l) => l.host === host)
    .sort((a, b) => (b.rss_mb || 0) - (a.rss_mb || 0) || a.lane.localeCompare(b.lane));
}

// wrap breaks text into lines of at most width characters, at spaces: a tree row shows one line
// and a decision has to be readable in the view.
function wrap(text, width = 64) {
  const lines = [];
  let cur = '';
  for (const w of String(text || '').split(/\s+/).filter(Boolean)) {
    if (cur && cur.length + 1 + w.length > width) { lines.push(cur); cur = w; } else cur = cur ? `${cur} ${w}` : w;
  }
  if (cur) lines.push(cur);
  return lines;
}

// freshDecisions lists the proposals in now that were not in before.
function freshDecisions(before, now) {
  const seen = new Set(before.map((d) => d.id));
  return now.filter((d) => !seen.has(d.id));
}

// sessionOf says where the session that asked is. A proposal's lane is the sender's name, which is
// a lane of kw (kw front), a surface with a conversation of its own (its Claude tab), or neither:
// kton-coordination has no entry in surfaces.json.
function sessionOf(lane, surfaces, tabs) {
  if (surfaces.some((s) => s.lanes.some((l) => l.name === lane))) return { lane };
  if (tabs[lane]) return { surface: lane };
  return null;
}

// decideArgs is the command line of ./decide; the logic of an answer stays in decisions.py.
function decideArgs(d, pick, text) {
  return [d.id, pick].concat(text ? [text] : []);
}

module.exports = { wrap, freshDecisions, sessionOf, decideArgs, machineWarning, machineLine, lanesOn, parseKwList, laneIndex, openByLane, sender, fresh, SSE, surfaceSessions, surfaceOfFolder, openFor, doorbell };
