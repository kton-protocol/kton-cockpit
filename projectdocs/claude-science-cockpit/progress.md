---
title: "claude-science-cockpit — Progress"
project: "claude-science-cockpit"
updated: 2026-07-22
---

# Progress Tracking

## Summary

| Phase | Status | Entries |
|---|---|---|
| Design | done | 6 |
| Build | done | — |
| End-to-End Tutorial | active | 1 |

## Log

- **2026-07-22** — Built the full picture first: read the governing `Bau-Spezifika.md`, the
  kton.dev protocol overview, the `plankton-federation-template` REPRODUCE.md, and the actual
  state of `deathbychoco/my-federation` + `participant-alice/bob/carol` (all healthy — mirror
  converging, all three registered and reproducing). Found six separate local
  `plankton-data`/`nekton-data` folders scattered under `/mnt/c/dev`, which explains the
  wrong-folder incident from the live demo run.
- **2026-07-22** — Read the actual `plankton`/`nekton` reference CLI usage blocks
  (`reference/cmd/plankton/main.go`, `nekton/reference/cmd/nekton/main.go`) and the participant
  template's `CLAUDE.md` (today's manual workflow) to ground the cockpit's three verbs in the
  real command surface rather than the spec's abstractions alone. Confirmed `plankton verify` /
  `nekton verify` (verify against an explicit pubkey, ignoring the declared `keyid`) already exist
  as binary subcommands — so the spec's "verified, not declared" requirement needs no new crypto
  in the cockpit, just calling these commands correctly.
- **2026-07-22** — Decided: Go implementation, MCP stdio server (not a CLI + Bash-permission
  approach) for the three verbs, with tightened Bash permissions as defense-in-depth on top. See
  [ADR-001](decisions/adr-001-go-mcp-server.md).
- **2026-07-22** — Designed the anti-wrong-folder guard (repo-root + git-remote verification on
  every MCP call), the `cockpit.config.json` schema, and the detailed behavior of all three tools.
  See the [design phase](phases/design/index.md) entries.
- **2026-07-22** — Corrected a placement mistake: initially wrote this project's *sibling*
  (`claude-workflow-manager`, the VS Code extension) project docs inside the extension's own
  source repo (`claudeVSDx/projectdocs/`) instead of here. Moved to
  `projectdocs/claude-workflow-manager/` in this repo, where the actual work session lives.
- **2026-07-22** — Corrected a second sequencing mistake: started `go mod init` before writing up
  this project's own design docs. Paused implementation to write this project's docs first
  (this file and its siblings), capturing the design in full before any code exists.
- **2026-07-22** — Built v1 end to end: Go module, `internal/config` (schema + loader + the
  repo/remote-match guard), `internal/gitops`, `internal/binaries` (plankton/nekton process
  wrappers), `internal/verify` (trust-tier resolution), `internal/tools` (publish/say/ask), and
  `cmd/cockpit` wiring an MCP stdio server via the official `modelcontextprotocol/go-sdk` v1.6.1.
  Clean `go build`/`go vet`.
- **2026-07-22** — Smoke-tested against real data rather than fixtures: `cockpit init`/`doctor`
  against the actual `participant-alice-1` clone (real signed fotons/claims from the live demo
  run) — correctly derived `deathbychoco/participant-alice` from its real git remote, validated
  every path/key/template. `cockpit_ask` (producer + reproductions queries) against that same
  real registry returned correctly-verified records. Directly verified the anti-wrong-folder
  guard against the exact failure mode that motivated this project: a `cockpit.config.json` bound
  to `participant-alice` sitting in a repo whose actual origin is `participant-bob` — the cockpit
  refused outright with a clear error, rather than silently acting against the wrong repo.
- **2026-07-22** — Wrote the [end-to-end tutorial](phases/end-to-end-tutorial/tutorial.md):
  create the template repos → configure the cockpit in a participant repo → register it as an MCP
  server → start Claude Science → publish a foton → make a claim → register with a federation →
  verify via the viewer and `cockpit_ask`.
- **2026-07-22** — Fixed two bugs in the tutorial found by actually following it against a real
  new participant repo (`participant-wolfi`): (1) the build command was run from the participant
  repo with an absolute path to the cockpit source as the package argument — `go build` resolves
  the module from cwd, not the argument, so this failed with "go.mod file not found"; fixed by
  building from the cockpit source dir and writing `-o` straight into the participant repo. (2)
  `plankton/nekton keygen` into `keys/` failed on a fresh clone — `keys/` is `.gitignore`d and
  starts empty, so git never materializes the directory on checkout, and keygen doesn't create
  missing parent dirs; fixed by adding `mkdir -p keys` first. Both confirmed fixed by rerunning
  them for real in `participant-wolfi` (keys generated, copied to `registry/keys/`).
