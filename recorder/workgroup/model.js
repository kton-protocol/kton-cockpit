// What the view shows, without VS Code: kw's list, the open messages per lane, and what is new.
// Kept apart from extension.js so it can be tested with plain node (model.test.js).
'use strict';

// parseKwList reads `kw list`: a "◆ <surface>  (tmux kw-<surface>: up|not started)" line per
// surface, then a line per lane with its state (● claude = running, ○ <cmd> = a window without
// claude, — = no window) and its session id.
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
    const l = line.match(/^ {3}(\S+)\s+(● \S+|○ \S+|—)\s*(\S*)/);
    if (l && cur) {
      const state = l[2] === '—' ? 'none' : l[2].startsWith('●') ? 'running' : 'window';
      cur.lanes.push({ name: l[1], state, session: l[3] || '' });
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

module.exports = { parseKwList, laneIndex, openByLane, sender, fresh, SSE };
