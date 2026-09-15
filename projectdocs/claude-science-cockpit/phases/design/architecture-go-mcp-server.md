---
title: "Architecture: Go binary, MCP stdio server"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [architecture, go, mcp]
---

> **Design phase, 2026-07-22.** This records what was designed and why, at that time. The shipped
> contract is [`spec/SPEC.md`](../../../../spec/SPEC.md), whose every normative clause names the test
> that checks it; where the two disagree, the spec is right and this is history. Kept because the
> reasoning behind a decision does not survive in the clause that resulted from it.

# Architecture: Go binary, MCP stdio server

## Summary

The cockpit is a single Go binary (`cockpit`), run as `cockpit mcp` to start an MCP stdio server
exposing exactly three tools: `cockpit_publish`, `cockpit_say`, `cockpit_ask`. See
[ADR-001](../../decisions/adr-001-go-mcp-server.md) for the full reasoning; this entry covers the
resulting shape.

## Details

**Language: Go, not Node/TS.** Participant repos currently ship zero JS tooling — just vendored
Go binaries (`bin/plankton`, `bin/nekton`, committed so participants need no build step) plus
Python pipeline scripts. A `bin/cockpit` Go binary slots into that exact pattern with zero new
install-time dependency — directly relevant given the infra friction already hit during the live
run. `kton`, the protocol's own reference cockpit, is also a Go shell CLI, so this stays idiomatic
to the ecosystem rather than introducing a foreign toolchain.

**MCP library:** the official `modelcontextprotocol/go-sdk` (confirmed available via the Go module
proxy, currently at v1.6.1 stable / v1.7.0-pre.2). Chosen over `mark3labs/mcp-go` (a popular
community alternative, also available) for being the official SDK.

**Transport:** stdio, registered per participant repo via `.mcp.json` pointing at `cockpit mcp`.
This means the MCP tool surface is scoped to whichever repo the session's `.mcp.json` was launched
from — reinforced further by the anti-wrong-folder guard (see that entry), which re-verifies the
binding on every call rather than trusting the launch-time cwd alone.

**Other `cockpit` subcommands** (not exposed to Claude, for the human operator): `cockpit init`
(scaffold a `cockpit.config.json` in a repo), `cockpit doctor` (validate config + connectivity —
git remote, key files, template files all present and consistent).

## Notes

Repo layout (built in this repo):

```
planktonClaudeScienceCockpit/
├── go.mod
├── cmd/cockpit/main.go
├── internal/
│   ├── config/      schema + load + validate + repo/remote-match guard
│   ├── gitops/       commit/push wrappers, permalink base construction
│   ├── binaries/    thin process wrappers around plankton/nekton (parse stdout, never reimplement)
│   ├── verify/      re-verification + trust-tier resolution (calls plankton/nekton verify)
│   └── tools/       publish.go, say.go, ask.go — the 3 MCP tool handlers
├── cockpit.config.schema.json
└── CLAUDE.md         dev-facing: how to build/test the cockpit itself
```
