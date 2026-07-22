---
title: "Rollout and verification plan"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [rollout, testing]
---

# Rollout and verification plan

## Summary

How the cockpit moves from "builds and passes tests here" to "actually protects a live
participant repo," plus how it gets verified end-to-end along the way.

## Details

### Rollout

1. Build and test the cockpit standalone in this repo first, against a throwaway registry — no
   need to touch the live `deathbychoco/*` repos while iterating.
2. Vendor the cross-compiled `bin/cockpit` into a fork/derivative of `plankton-participant-template`,
   alongside the existing `bin/plankton`, `bin/nekton`.
3. Add one `cockpit.config.json` per participant repo, plus `.mcp.json` registering `cockpit mcp`.
4. Add the recommended `.claude/settings.json` Bash denials as defense-in-depth (see
   [ADR-001](../../decisions/adr-001-go-mcp-server.md)).
5. Rewrite participant `CLAUDE.md` to describe the three verbs instead of the raw CLI recipe — the
   existing `CLAUDE.md` becomes this project's own internal reference for *what* the cockpit
   automates, not instructions Claude follows by hand anymore.

### Verification

- **Unit-level**: `internal/config` tests for the repo/remote mismatch guard (must hard-fail on
  any mismatch), and `internal/verify` tests using the existing recorded fixtures under
  `/mnt/c/dev/planktonReproduce/**/registry/` as real signed-envelope test data (no need to
  fabricate test envelopes — genuine ones already exist from the live demo run).
- **End-to-end**: stand up a scratch git repo with a `cockpit.config.json`, drive
  `cockpit_publish` → `cockpit_say` → `cockpit_ask` through an MCP test client (or the Go SDK's
  in-process test harness), and confirm: (a) a mismatched `origin` remote causes every tool call
  to refuse; (b) an end-to-end publish produces a foton whose `--located` permalinks resolve to
  the actual pushed commit; (c) `cockpit_ask` output never includes a record whose signature
  didn't independently verify against the configured trust tier.
- **Manual smoke test**: point `cockpit mcp` at one of the existing local clones under
  `/mnt/c/dev/planktonReproduce/alice/participant-alice-1` (a real, already-populated registry)
  and run `cockpit_ask` queries against it before ever wiring the cockpit into a live
  GitHub-connected repo.

## Notes

Open questions, flagged rather than blocking (per the spec's own "Offen" section):

- Exact MCP Go SDK version to pin (`modelcontextprotocol/go-sdk` was confirmed available at
  v1.6.1/v1.7.0-pre.2 as of 2026-07-22 — re-check maturity at implementation time).
- Whether `cockpit_ask` should also shell to `kton mirror <url>`/`kton serve` for live federation
  queries in v1, or stay local-registry-only initially. The spec's own "Offen" section already
  names this class of question ("fehlt eine kton-CLI-Fähigkeit?") as something to raise as a
  protocol change-request if a gap is found, not to solve with a cockpit-side hack.
