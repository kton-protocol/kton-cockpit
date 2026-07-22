---
title: "Implement the cockpit as a Go binary running an MCP stdio server"
type: "decision"
project: "claude-science-cockpit"
date: 2026-07-22
status: "accepted"
tags: [architecture, enforcement, mcp, go]
---

# ADR-001: Implement the cockpit as a Go binary running an MCP stdio server

## Status

Accepted

## Context

The governing Bau-Spezifika requires that Claude's entire surface for cockpit-related actions be
exactly three verbs — "Alles andere ist Claude nicht zugänglich" (everything else is inaccessible
to Claude). Two ways to build toward that were considered:

1. **MCP server**: the cockpit exposes `cockpit_publish`/`cockpit_say`/`cockpit_ask` as MCP tools.
2. **CLI + restricted Bash permissions**: a `cockpit` CLI invoked via Bash, with
   `.claude/settings.json` denying direct `git`/`plankton`/`nekton` invocation.

Separately, participant repos today ship zero JS/Node tooling — only vendored Go binaries
(`bin/plankton`, `bin/nekton`) and Python pipeline scripts — and `kton`, the protocol's own
reference cockpit, is itself a Go shell CLI.

## Decision

Build the cockpit as a single Go binary, run as `cockpit mcp` (stdio transport, via the official
`modelcontextprotocol/go-sdk`), exposing exactly the three tools. Pair this with recommended
`.claude/settings.json` Bash denials (`git push`, direct `plankton`/`nekton` invocation) as
**defense-in-depth**, not as the primary enforcement mechanism — Bash itself stays available in
participant repos for the legitimate Python pipeline work.

## Consequences

### Positive

- Structural enforcement: in a session with only the cockpit's `.mcp.json` registered, there is no
  raw git/plankton/nekton tool for Claude to fall back to by mistake — matching "Alles andere ist
  Claude nicht zugänglich" at the mechanism level, not just by convention.
- MCP tool schemas let the cockpit validate/constrain inputs at the boundary (e.g. only allowed
  claim templates) — a natural fit for `claims.allowedTemplates` in config.
- Go keeps the cockpit idiomatic to this ecosystem (same language as `plankton`/`nekton`/`kton`)
  and installable with zero new dependency footprint in participant repos (a single vendored
  binary, same pattern as the existing `bin/plankton`, `bin/nekton`).

### Negative

- More moving parts than a plain CLI: an MCP server process, `.mcp.json` registration per
  participant repo, onboarding friction across multiple repos.
- MCP alone doesn't remove Claude's ambient Bash access, which participants still need for running
  their actual pipeline scripts — hence the Bash-permission layer is still required as
  defense-in-depth, not optional.
- Harder to invoke ad hoc from a terminal for manual testing than a plain CLI (mitigated by the
  non-MCP `cockpit init`/`cockpit doctor` subcommands, and by testing the tool handlers directly
  in Go tests without going through the MCP transport).

## Alternatives Considered

- **CLI + restricted Bash permissions** — simpler to build and test, and reuses Claude Code's
  existing permission system rather than introducing a new integration surface. Rejected as the
  *primary* mechanism because enforcement would live entirely in per-repo permission config, which
  is exactly the class of thing (misconfigured/drifted settings) that contributed to the infra
  issues already hit during the live demo run — a structural guarantee was judged worth the extra
  setup cost.
- **Node/TypeScript MCP server** — considered by analogy to other tooling in this environment
  (the `claude-workflow-manager` VS Code extension), but rejected once it became clear the
  participant-repo ecosystem is 100% Go + Python with no existing JS footprint; Go avoids
  introducing a new toolchain dependency into repos that currently need none.
