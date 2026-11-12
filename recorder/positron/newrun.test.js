// node recorder/positron/newrun.test.js
'use strict';
const assert = require('assert');
const n = require('./newrun');

assert.strictEqual(n.nextName('run6', ['run1', 'run2', 'run6', 'run7', 'data']), 'run8', 'past the highest, never a sibling');
assert.strictEqual(n.nextName('run06', ['run06']), 'run07');
assert.strictEqual(n.nextName('base', ['base']), null, 'no number, no next');

assert.strictEqual(n.renameRef('model/pk/run6/sdtab6', 'run6', 'run7'), 'model/pk/run7/sdtab7');
assert.strictEqual(n.renameRef('model/pk/run6/patab16', 'run6', 'run7'), 'model/pk/run7/patab16', 'another number stays');
assert.strictEqual(n.renameRef('sdtab6.csv', 'run6', 'run7'), 'sdtab7.csv');
assert.strictEqual(n.renameRef('data/derived/nm_data.csv', 'run6', 'run7'), 'data/derived/nm_data.csv');

const ctl = `; model:       run6
; based_on:    run5
; description: Final Model
; tag:         absorption lag
; note:        In run5 the additive error went to its boundary
; note:        Result: same OFV as run5 (84.5)
; final:       yes
$PROBLEM run6 - run5 with proportional error only
$DATA data/derived/nm_data.csv IGNORE=@
$THETA (0.1, 3) (1, 30)
$TABLE ID TIME DV PRED NOPRINT ONEHEADER FILE=model/pk/run6/sdtab6
`;
const r = n.renameCtl(ctl, 'run6', 'run7');
assert.ok(r.text.startsWith('; model:       run7\n; based_on:    run6\n'), r.text);
assert.ok(r.text.includes('FILE=model/pk/run7/sdtab7'));
assert.ok(r.text.includes('$PROBLEM run6 - run5 with proportional error only'), 'the model content stays byte for byte');
assert.ok(r.text.includes('$DATA data/derived/nm_data.csv'));
assert.ok(r.text.includes('; note:        In run5 the additive error'), 'a note that is no result stays');
assert.ok(!r.text.includes('; final:') && !r.text.includes('Result:'), "run6's own result lines go");
assert.strictEqual(r.changes.length, 5, r.changes.join('\n'));
const noHeader = n.renameCtl('$PROBLEM x\n$TABLE ID FILE=sdtab6\n', 'run6', 'run7');
assert.ok(noHeader.text.startsWith('; based_on:    run6\n$PROBLEM x\n$TABLE ID FILE=sdtab7'), noHeader.text);

const step = (s, t) => ({ session: s, task: t });
const files = [
  { path: 'model/pk/run6/run6.ctl', role: 'source', read_by: [step('a', 'tail'), step('gof', 'tail')] },
  { path: 'model/pk/run6/run6.ext', role: 'intermediate', produced_by: step('a', 'tail'), read_by: [step('gof', 'tail')] },
  { path: 'model/pk/run6/sdtab6', role: 'intermediate', produced_by: step('b', 'tail') }, // a re-run of the same call
  { path: 'data/derived/nm_data.csv', role: 'intermediate', produced_by: step('prep', 'tail'), read_by: [step('a', 'tail')] },
];
const cmd = { 'a/tail': 'nmfe75 model/pk/run6/run6.ctl model/pk/run6/run6.lst', 'b/tail': 'nmfe75 model/pk/run6/run6.ctl model/pk/run6/run6.lst', 'x/tail': 'Rscript script/x.R' };
const cmdOf = (s, t) => cmd[s + '/' + t];
let p = n.plan('model/pk/run6', files, cmdOf, ['run6.ctl', 'run6.ext', 'sdtab6']);
assert.deepStrictEqual([p.ok, p.inputs, p.call], [true, ['model/pk/run6/run6.ctl'], 'nmfe75 model/pk/run6/run6.ctl model/pk/run6/run6.lst']);
p = n.plan('model/pk/run6', files.concat([{ path: 'model/pk/run6/x.csv', produced_by: step('x', 'tail') }]), cmdOf, []);
assert.ok(!p.ok && /2 different recorded calls/.test(p.reason), p.reason);
// run6 done again: the earlier run's versions are no longer on disk and do not count
p = n.plan('model/pk/run6', files.concat([{ path: 'model/pk/run6/run6.lst', now: 'changed', produced_by: step('x', 'tail') }]), cmdOf, []);
assert.ok(p.ok, p.reason);
p = n.plan('model/pk/run9', [], cmdOf, ['run9.ctl']);
assert.deepStrictEqual([p.ok, p.inputs], [true, ['model/pk/run9/run9.ctl']], 'a model never run: its one control stream');
p = n.plan('model/pk/run9', [], cmdOf, ['a.ctl', 'b.ctl']);
assert.ok(!p.ok);
const os = require('os'), fs = require('fs'), path = require('path');
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'nr-'));
n.recordDerivation(root, 'model/pk/run4/run4.ctl', 'model/pk/run2/run2.ctl', fs, new Date('2026-11-12T10:00:00.123Z'));
n.recordDerivation(root, 'model/pk/run5/run5.ctl', 'model/pk/run3/run3.ctl', fs);
const all = n.recordDerivation(root, 'model/pk/run4/run4.ctl', 'model/pk/run3/run3.ctl', fs, new Date('2026-11-12T11:00:00Z'));
assert.deepStrictEqual(all['model/pk/run4/run4.ctl'], { from: 'model/pk/run3/run3.ctl', action: 'new-run', at: '2026-11-12T11:00:00Z' }, 'made again: replaced');
assert.ok(all['model/pk/run5/run5.ctl'], 'merged, not overwritten');
assert.deepStrictEqual(JSON.parse(fs.readFileSync(path.join(root, '.kton/derivations.json'), 'utf8')), all);
console.log('newrun ok');
