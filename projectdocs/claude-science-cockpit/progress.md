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
- **2026-07-22** — Corrected a wrong assumption in step 4: "Claude Science" isn't a figure of
  speech for the `claude` (Claude Code) CLI I've been running this whole session — it's a
  distinct, already-installed product (`claude-science`, a sandboxed daemon + browser UI, "run
  Claude on your data, locally, in your browser"). Confirmed via a UI screenshot that it has a
  native **Local command** connector type for exactly this use case (alongside Remote URL
  connectors, which is all its cached `directory-cache.json` otherwise showed — those are
  claude.ai-style hosted MCP connectors like Atlassian, a red herring for our purposes). Updated
  the tutorial to cover both paths (claude-science's Connectors UI vs. Claude Code CLI's
  `.mcp.json`), and flagged one still-unverified caveat: whether the Local command connector's
  working directory is guaranteed to be the participant repo (this matters directly for the
  anti-wrong-folder guard, which checks `git remote get-url origin` from its own cwd).
- **2026-07-22** — Resolved that caveat from a screenshot of claude-science's actual "Add
  connector" → Local command dialog: it only has Name / Command / env vars / Description —
  **no working-directory field at all**. So the tutorial no longer just says `bin/cockpit mcp`;
  the Command value must itself pin the directory: `bash -c "cd /ABSOLUTE/PATH && exec bin/cockpit
  mcp"`. Also clarified two points of confusion along the way, worth remembering for future
  explanations: (1) `cockpit` is one binary with `init`/`doctor` (human CLI) and `mcp` (starts the
  MCP server) as subcommands — not a CLI-plus-separate-MCP-server pair; (2) the three verbs
  (publish/say/ask) aren't sibling subcommands next to `init`/`doctor`/`mcp` — they're the three
  MCP "tools" that only exist *inside* `mcp` mode, callable only by an MCP client (claude-science
  or Claude Code), never directly from a terminal.
- **2026-07-22** — The `bash -c "cd ... && exec ..."` connector command turned out not to work at
  all: claude-science's managed MCP runtime has no `bash` (only node/npx/python3, or an absolute
  path to an installed binary). Fixed properly instead of working around it: added a
  `COCKPIT_REPO_DIR` environment variable to `config.Load` — set once, in the connector's own env
  vars field (which the dialog does support), it replaces cwd-based repo-root resolution. This is
  not the kind of ambient env var the guard exists to distrust: it's set by the human operator at
  connector-configuration time, same place as the command itself, never reachable by Claude at
  runtime, and it only ever answers "which repo," not "which registry paths within it" (those are
  still always derived from the resolved repo root, never from the environment). Key clarification
  worth remembering: an absolute path to the binary only answers "which program to run," not
  "what working directory does it run in" — those are independent, and only the latter is what
  the anti-wrong-folder guard actually checks.
- **2026-07-22** — While verifying the fix, found the existing smoke tests had gone stale (they
  pointed at `participant-alice-1`, which no longer exists after the local demo repos got
  reorganized into `participant-christian`/`participant-wolfi`) and, worse, `os.Chdir` into a
  since-deleted directory on this WSL/NTFS setup returns no error while leaving `os.Getwd()`
  broken — so the tests were silently running from a broken cwd instead of skipping cleanly.
  Repointed the fixture at `participant-christian` and hardened the `chdir` test helper to check
  `os.Getwd()` too. Also found and fixed a real bug this surfaced: `plankton reproductions` exits
  non-zero for the legitimate "0 distinct producers" case, which `Ask()` was surfacing as a tool
  error instead of a correct zero-count answer — fixed in `binaries.Reproductions` (same
  never-treat-nonzero-as-failure convention already used for `Reproduces`). All 5 tests pass,
  including one confirming `COCKPIT_REPO_DIR` actually works. Rebuilt `bin/cockpit` in
  `participant-christian` with these fixes.
- **2026-07-23** — Confirmed end to end in the real claude-science UI: the Local command connector
  (absolute binary path + `COCKPIT_REPO_DIR` env var) successfully loaded all three tools in
  `participant-christian`, showing the exact descriptions from the code. First real integration
  point of this project fully working outside of Go tests.
- **2026-07-23** — Step 4 (`claude-science serve --here`) hit a real, unrelated OS limit:
  `--here`'s data-dir socket path under `/mnt/c/dev/planktonReproduce/participant-christian/...`
  exceeded the 108-byte `AF_UNIX sun_path` cap. Fixed by switching the tutorial to
  `claude-science serve --data-dir ~/.claude-science-<name>` (short, per-participant-identity)
  instead of `--here` — costs nothing since `COCKPIT_REPO_DIR` already made the connector's repo
  binding independent of claude-science's own cwd, and additionally keeps each identity's
  connector list from bleeding into the others.
