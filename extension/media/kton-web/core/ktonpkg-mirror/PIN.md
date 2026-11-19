# ktonpkg JS mirror

`plan.js`, `sparql.js` and `packages.js` are the JavaScript mirror of ktonpkg's composition library
(`compose` -> query -> fill, `bindVars`, `checkValue`, `varValuesOf`; the package loader stands in for
ktonpkg's member list and install checks). `core/compose.js` uses only `bindVars`, `checkValue` and
`varValuesOf`; the test shell (`tests/shell/`) plans with `plan` and loads packages with `loadPackages`.

**Source:** userinterface's `qm-workbench/composition-proto/core/` (round 5, 19.11.2026): plan.js
ed00b89d…, packages.js bc11e05c…, sparql.js c6238589…. Since 19.11. kton-web carries the mirror, because
the renderer that binds through it lives here and hosts vendor both from one kton-web commit. Changes
since the source, each to match ktonpkg (userinterface's PR 2 findings 3-5, tests/mirror.test.mjs):

- plan.js: the closed set has `subject.kind`, `subject.type`, `subject.path`, `subject.scope` (C2.1);
  `subject.type` binds as an IRI when it is an absolute http(s) IRI, otherwise as a literal (no aliases.json here).
- plan.js: layout as ktonpkg v0.9.5 "Layout": on a composition or on any part that holds parts; stack,
  columns, grid (areas by part id, "." for an empty cell, rectangles, every part placed once), tabs (headings,
  `active`), split (two parts, 0 < ratio < 1, row|column), `narrow` stack|tabs, sizes fr/px/%/auto; refused
  on a report or deck. The plan carries it as `compositions[].layout` and `sections[].layout`, and each
  section has `pid`, the part id before an `every` numbered it.
- packages.js: a block option is checked against the viewer's `accepts` (ktonpkg), not a separate
  `options` list; a viewer.json names its viewer with the key `viewer` (as ktonpkg's pins do) and is
  refused without it.

| file | sha256 |
|---|---|
| plan.js | aef6c768f77c0f4af696bc9afcf731dcd3b72a8bfc5596263e6cf02b5e10ba36 |
| packages.js | 28d0e4f12edb7a5613736ef5e51af8197c2e013e483dbbd577c728dd94f8e2f1 |
| sparql.js | c6238589abc0e92f1d30433a1aaf7f79ea34a99cec0f74ca185cffbc29c9730a |

When ktonpkg v0.10.0 ships `compose`/`Fill`/`ValidateState`, this folder is replaced by that release's
binding.
