---
title: "What this cockpit explicitly does not build"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [scope, constraints]
---

# What this cockpit explicitly does not build

## Summary

Directly from the governing spec's "Nicht bauen" section — recorded here as a design constraint,
not just a note in the spec, so it stays visible during implementation.

## Details

- No own canonicalization, signing, registry, or hash-chain logic — every mutation goes through
  `plankton`/`nekton` binaries, never reimplemented.
- No trust logic beyond mechanically applying `cockpit.config.json` — the cockpit resolves and
  applies the config; it never decides trust policy itself.
- No fourth verb, no silently grown surface, no cockpit-owned mutable state beyond the static
  config file it loads at startup.
- Deletable: if `bin/cockpit` were removed entirely, every operation it performs must remain
  runnable by hand exactly as the participant template's `CLAUDE.md` already documents today — the
  same acceptance test the protocol itself applies to `kton` ("delete kton and every operation
  still runs directly via plankton/nekton").

## Notes

This list is a design *constraint*, checked against during implementation review — not just an
aspiration. Any future feature request that would violate one of these bullets should be raised as
a change to the governing spec first, not implemented as a cockpit-side workaround (the spec's own
"Offen" section says the same about protocol gaps: "öffentlicher Change-Request am Protokoll, kein
Cockpit-Hack").
