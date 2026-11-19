# ktonpkg JS mirror (pinned copy)

`plan.js`, `sparql.js` and `packages.js` (the package loader, a stand-in for ktonpkg's member list and
install checks) are the JavaScript mirror of ktonpkg's composition library (`compose` →
query → fill, `bindVars`, `checkValue`, `varValuesOf`), copied unchanged from userinterface's
`qm-workbench/composition-proto/core/` (round 5, 19.11.2026). `core/compose.js` uses only `bindVars`,
`checkValue` and `varValuesOf`; the test shell (`tests/shell/`) plans with `plan` and loads packages
with `loadPackages`.

| file | sha256 |
|---|---|
| plan.js | ed00b89d505e3bbafd241c59e75b9f21f3c2a91385163867d19da6356ac84b97 |
| packages.js | bc11e05c6e66fc3cdc603a4415a9203cc5b5295af51049edbfc4b356868d5db5 |
| sparql.js | c6238589abc0e92f1d30433a1aaf7f79ea34a99cec0f74ca185cffbc29c9730a |

Do not edit here. When ktonpkg v0.10.0 ships `compose`/`Fill`/`ValidateState`, this folder is replaced by
that release's binding and the hashes above go.
