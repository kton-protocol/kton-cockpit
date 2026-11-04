// node recorder/workgroup/model.test.js
'use strict';
const assert = require('assert');
const m = require('./model');

const list = `◆ kton  (tmux kw-kton: up)
   kton                       ● claude     e212ef5a
   cockpit-runtime            ● claude     25ee7d3c
   kton-example               —
   odd                        ○ bash
   away                       ● claude     9c6ba7da  @ai1
   there                      —              @SCI-313
◆ qm  (tmux kw-qm: not started)
   cockpit                    —            b39fe9b3
`;
const s = m.parseKwList(list);
assert.strictEqual(s.length, 2);
assert.deepStrictEqual(s[0].lanes.map((l) => l.state), ['running', 'running', 'none', 'window', 'running', 'none']);
assert.deepStrictEqual([s[0].lanes[4].host, s[0].lanes[4].session], ['ai1', '9c6ba7da']);
assert.deepStrictEqual([s[0].lanes[5].host, s[0].lanes[5].session], ['SCI-313', ''], 'a host is no session id');
assert.strictEqual(s[0].lanes[1].session, '25ee7d3c');
assert.strictEqual(s[1].up, false);
assert.strictEqual(s[1].lanes[0].session, 'b39fe9b3');

const idx = m.laneIndex({
  _doc: 'x',
  kton: { lanes: { 'cockpit-runtime': { lane: 'active/cockpit-runtime' }, kton: { lane: 'active/kton' } } },
  qm: { lanes: { cockpit: { lane: 'active/cockpit' } } },
});
assert.deepStrictEqual(idx['active/cockpit'], { surface: 'qm', lane: 'cockpit' });

const row = (lane, msg, title) => ({
  lane: { value: lane }, msg: { value: msg }, title: { value: title }, area: { value: 'a' },
  from: { value: 'lane-kton-coordination-claims' }, when: { value: '2026-11-02T12:00:00Z' },
});
const before = m.openByLane({ results: { bindings: [row('active/cockpit-runtime', 'm1', 'one'), row('active/elsewhere', 'm9', 'x')] } }, idx);
assert.deepStrictEqual(Object.keys(before), ['cockpit-runtime'], 'a lane no surface has is left out');
assert.strictEqual(before['cockpit-runtime'][0].from, 'kton-coordination');
const now = m.openByLane({ results: { bindings: [row('active/cockpit-runtime', 'm1', 'one'), row('active/cockpit', 'm2', 'two')] } }, idx);
assert.deepStrictEqual(m.fresh(before, now).map((x) => [x.lane, x.msg]), [['cockpit', 'm2']]);

const got = [];
const sse = new m.SSE((name, data) => got.push([name, JSON.parse(data)]));
sse.push('event: cursor\ndata: {"cursor":"c1"}\n\nevent: rec');
sse.push('ord\ndata: {"id":"x"}\n\n: ping\n\n');
assert.deepStrictEqual(got, [['cursor', { cursor: 'c1' }], ['record', { id: 'x' }]]);
const ss = m.surfaceSessions({ kton: {}, _surface_sessions: { qm: { session: 's1', dir: '/c/surfaces/qm/' }, bad: { dir: '/x' } } });
assert.deepStrictEqual(ss, { qm: { session: 's1', dir: '/c/surfaces/qm' } });
assert.strictEqual(m.surfaceOfFolder(ss, '/c/surfaces/qm'), 'qm');
assert.strictEqual(m.surfaceOfFolder(ss, '/c'), undefined);
const res = { results: { bindings: [row('active/kton-coordination/surfaces/qm', 'q1', 'for qm'), row('active/kton-coordination', 'c1', 'for coordination'), row('active/cockpit', 'x', 'other')] } };
const mine = m.openFor(res, '/mnt/c/dev/workstreams/active/kton-coordination/surfaces/qm/');
assert.deepStrictEqual(mine.map((x) => x.msg), ['q1'], 'only the surface folder\'s own lane, not its parent');
assert.ok(m.doorbell('qm', mine[0]).includes('"for qm"'));
const low = { load: [3.3, 3, 3], memory: { total_mb: 11960, available_mb: 1275, swap_total_mb: 8192, swap_free_mb: 0 } };
const fine = { load: [2, 1, 1], memory: { total_mb: 64105, available_mb: 53867, swap_total_mb: 32734, swap_free_mb: 32731 } };
assert.strictEqual(m.machineWarning(low), '1.2 GB memory available, swap nearly full');
assert.strictEqual(m.machineWarning(fine), '');
assert.strictEqual(m.machineWarning({ error: 'not reachable from here' }), '');
assert.strictEqual(m.machineLine(low), '10.4/11.7 GB · swap 8.0/8.0 GB · load 3.3');
assert.strictEqual(m.machineLine({ error: 'not reachable from here' }), 'not reachable from here');
const st = { lanes: [{ lane: 'b', host: 'x', rss_mb: null }, { lane: 'a', host: 'x', rss_mb: 300 }, { lane: 'c', host: 'y', rss_mb: 9 }] };
assert.deepStrictEqual(m.lanesOn(st, 'x').map((l) => l.lane), ['a', 'b']);
assert.deepStrictEqual(m.wrap('aa bb cc', 5), ['aa bb', 'cc']);
assert.deepStrictEqual(m.wrap('averyveryverylongword x', 5), ['averyveryverylongword', 'x'], 'a long word stays whole');
assert.deepStrictEqual(m.wrap(undefined), []);
assert.deepStrictEqual(m.freshDecisions([{ id: 'a' }], [{ id: 'a' }, { id: 'b' }]).map((d) => d.id), ['b']);
const tabs = { qm: { session: 's1', dir: '/c/surfaces/qm' } };
assert.deepStrictEqual(m.sessionOf('cockpit-runtime', s, tabs), { lane: 'cockpit-runtime' });
assert.deepStrictEqual(m.sessionOf('qm', s, tabs), { surface: 'qm' }, 'a surface opens as its tab, kw front knows lanes only');
assert.strictEqual(m.sessionOf('kton-coordination', s, tabs), null);
assert.deepStrictEqual(m.decideArgs({ id: 'x1' }, 'a', ''), ['x1', 'a']);
assert.deepStrictEqual(m.decideArgs({ id: 'x1' }, 'modify', 'push tomorrow'), ['x1', 'modify', 'push tomorrow']);
console.log('model ok');
