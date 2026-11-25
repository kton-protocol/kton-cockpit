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
- plan.js, sparql.js: optional query params as ktonpkg v0.9.10 (15e444e, `Use.Optional`): a use's
  `optional` lists $VARs of its query that may stay unbound. Checked like ktonpkg's build: each a $VAR of
  the query, named once, not refined, only with a query; any other unbound $VAR is still refused. The plan
  carries the list as `uses[].queryOptional` (`uses[].optional` already holds the viewer's optional
  capabilities). `bindVars` leaves an optional param unbound while its var has no value (unset, or `""`,
  a cleared pick), and the use does not wait for it; `bindQuery(text, params, optional)` leaves an unbound
  optional `$name` as it is, a free SPARQL variable. The area's `optional` is the workbench's: the mirror
  has no area.
- plan.js: `subject.scope` in a query param is the scope's seed, `{type: "uri", value: "https://kton.dev/o/<hex>"}`,
  the node its id names, as kton-cockpit's `--bind-iri` binds a kton id. A composition finds the scope's claim
  graph by it (ADR-018: `?g a nk:ClaimGraph ; nk:scope $scope`). An act field keeps the id itself
  (`sha256:<hex>`), since it goes into a signed claim. A value that is not a scope id is refused.

| file | sha256 |
|---|---|
| plan.js | b7b184e515b0272f8317283519ef0ae8c60db6837c7e854e90a72a3f73a501a8 |
| packages.js | 28d0e4f12edb7a5613736ef5e51af8197c2e013e483dbbd577c728dd94f8e2f1 |
| sparql.js | 16d88eff8eb7ff42dff074ad37a38b2127c2a82235b9216b9ffbb7d6830b4f28 |

When ktonpkg v0.10.0 ships `compose`/`Fill`/`ValidateState`, this folder is replaced by that release's
binding.
