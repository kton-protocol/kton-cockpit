# CLAUDE.md

This is the source repository for **claude-science-cockpit**: a Go binary that gives a Claude
session — whether that's the Claude Code CLI or claude-science (a separate, sandboxed,
browser-UI product; see the tutorial's step 3-4 for the distinction and why it matters) — exactly
three verbs: `cockpit_publish` (veröffentlichen), `cockpit_say` (sagen), `cockpit_ask` (fragen) —
over MCP, for cooperating in a [kton](https://kton.dev) federation. It reimplements no
plankton/nekton kernel logic; every mutation and query shells out to the vendored
`bin/plankton`/`bin/nekton` binaries in whichever participant repo it's configured for.

The full design — why this exists, the anti-wrong-folder guard, the config schema, and what this
project deliberately does not build — is written up in
[`projectdocs/claude-science-cockpit/`](projectdocs/claude-science-cockpit/_project.md). Read that
before making architectural changes; this file only covers building/testing the cockpit itself.

The governing spec is [`# Claude-Science-Cockpit — Bau-Spezifika.md`](./%23%20Claude-Science-Cockpit%20%E2%80%94%20Bau-Spezifika.md)
(German; the internal build spec this project implements).

## Build

```bash
go build -o bin/cockpit ./cmd/cockpit
```

Requires Go ≥ 1.25 (the module's `go.mod` pins this via the `github.com/modelcontextprotocol/go-sdk`
dependency; `go build`/`go run` auto-fetch the matching toolchain if the ambient `go` is older).

## Subcommands

```
cockpit mcp      start the MCP stdio server (cockpit_publish/cockpit_say/cockpit_ask)
cockpit init     scaffold a cockpit.config.json in the current git repo (human operator only)
cockpit doctor   validate cockpit.config.json + the repo/remote binding (human operator only)
```

Only `mcp` is ever registered as Claude's tool surface (via a participant repo's `.mcp.json`).
`init`/`doctor` are for whoever is setting up or debugging a participant repo — Claude never calls
them.

## Test

```bash
go vet ./...
go test ./...
```

### Manual smoke test against a real, already-populated registry

Before wiring the cockpit into any live GitHub-connected repo, point `cockpit doctor` at one of the
existing local participant clones from the live demo run — real signed data, no fixtures to
fabricate (the exact repo name has changed as local demo repos were reorganized; check
`/mnt/c/dev/planktonReproduce/` for whichever `participant-*` currently exists):

```bash
cd /mnt/c/dev/planktonReproduce/participant-christian   # or whichever currently exists
# (if cockpit.config.json doesn't exist yet — `cockpit init` scaffolds one, using
#  that repo's own git remote to fill in repo.owner/repo.name; fill in trust.tiers by hand
#  before running `doctor` — the schema comment on that field explains the shape)
/mnt/c/dev/planktonClaudeScienceCockpit/bin/cockpit init
/mnt/c/dev/planktonClaudeScienceCockpit/bin/cockpit doctor
```

### Automated end-to-end UAT

See [`uat/README.md`](uat/README.md) for prerequisites (gh CLI scopes, Go, python3, claude-science
account). Follows the tutorial's federation-first sequence: participant 1 publishes and federates
before participant 2 does anything, so participant 2 can mirror participant 1's real aggregated
foton, hit the realistic byte-mismatch, correct it, and record a genuine ↻2 reproduction.

```bash
uat/setup.sh      # creates a federation + 2 participant repos, configures the cockpit in both,
                   # pauses 4 times for claude-science steps (connector setup; p1 publish; p2
                   # publish independently; p2 mirror+correct+claim) — not scriptable, see the
                   # script's header comment for why — then shows the resulting graph
uat/cleanup.sh <workdir printed by setup.sh>   # deletes everything setup.sh created, with confirmation
```

## Repo layout

```
cmd/cockpit/main.go     entry point: mcp / init / doctor subcommands
internal/
├── config/              cockpit.config.json schema, loader, and the repo/remote-match guard
├── gitops/               commit/push wrappers, commit-pinned permalink construction
├── binaries/             thin process wrappers around bin/plankton, bin/nekton
├── verify/               trust-tier resolution from the actual verifying key, never a declared keyid
└── tools/                the three MCP tool handlers (publish.go, say.go, ask.go)
cockpit.config.schema.json   JSON Schema for cockpit.config.json (documentation + tooling)
```

## What not to add here

Per the governing spec's "Nicht bauen" section (also recorded in
[`projectdocs/claude-science-cockpit/phases/design/what-we-dont-build.md`](projectdocs/claude-science-cockpit/phases/design/what-we-dont-build.md)):
no own canonicalization/signing/registry/chain logic, no trust logic beyond applying
`cockpit.config.json`, no fourth verb, no cockpit-owned mutable state beyond that static config
file. If a change would require one of these, raise it against the protocol spec first, not as a
cockpit-side workaround.
