# participant-skeleton

The layout a cockpit participant repo needs, as files rather than as a GitHub template.

It used to come from `gitmick/plankton-participant-template`. Scaffolding from a public repo made
this project's own test material something we neither ship nor control, and it went stale in the
way that predicts: that template vendors kton **0.1** binaries, which reject
`nekton about/by --json` and `plankton reproductions --trust-keys` — all three of which this
cockpit now requires. A UAT run from it would have driven the cockpit against a kernel missing
every flag it depends on.

Both `uat/setup.sh` and the Go test fixture (`internal/testrepo`) build participants from these
files, so the claim templates and the key hygiene below have one definition, not two.

## What is here, and what is not

- `gitignore` — copied in as `.gitignore`; kept dotless here so it is an inert file in THIS repo
  rather than a live ignore rule over its own directory. It carries the key hygiene, and the only subtle part: `/keys/` (private `.key` halves) is
  anchored to the repo root so it does NOT match `registry/keys/`, where the `.pub` halves are
  committed on purpose. A peer cannot verify your signatures without them.
- `data/penguins.csv` — the input the UAT's publish exercise cleans. Deliberately small, fixed, and
  carrying two all-missing rows, so "drop rows with any missing value" has something to do and two
  independent implementations can be compared byte for byte.
- `templates/` — the two claim templates the default `claims.allowedTemplates` names. Templates are
  federated example data, not protocol: the cockpit only ever passes a name through to nekton.

Not here, and deliberately: `bin/`. The binaries are built from a kton checkout at scaffold time,
never vendored — see CLAUDE.md, "The kernel binaries".
