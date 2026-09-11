---
title: "claude-science-cockpit"
created: 2026-07-22
tags: [plankton, kton, mcp, cockpit, claude-code]
---

# claude-science-cockpit

## Vision

A cockpit that sits on the **participant side** of a kton federation (see
[kton.dev](https://kton.dev)), between a Claude Code session doing "cooperative Claude science"
and the `plankton`/`nekton` binaries. It gives Claude exactly **three verbs** —
*veröffentlichen* (publish), *sagen* (say), *fragen* (ask) — and nothing else. It reimplements no
kernel logic: every mutation is a call into the existing `plankton`/`nekton` binaries; the cockpit
only owns git plumbing, config, and trust-tier resolution around them.

This exists because a live run of the `gitmick/plankton-federation-template` demo
(`deathbychoco/my-federation` + `participant-alice/bob/carol`) worked, but the *manual* workflow
behind it is fragile: Claude Code sessions manually run `plankton`/`nekton`, set
`PLANKTON_DIR`/`NEKTON_DIR` env vars by hand, and construct commit-pinned permalinks by hand.
Given `/mnt/c/dev` hosts a dozen unrelated projects — several of which (`MCP/`,
`claudeScience/warfarinTest/`, `plankton_fed_demo/`) *also* contain `plankton-data`/`nekton-data`
folders from earlier experiments — a Claude session with an ambiguous working directory can latch
onto the wrong project's registry and keep "cooperating" there. The cockpit's job is to make that
structurally impossible, not just discouraged by convention.

The governing spec is `# Claude-Science-Cockpit — Bau-Spezifika.md` (this repo's root) — a short,
intentionally minimal internal build spec. This project's docs exist to carry that spec forward
into a concrete, buildable design, and to record the design decisions made along the way.

## Goals

- Give Claude exactly three verbs (publish/say/ask); nothing else is reachable through the cockpit.
- Make it structurally impossible for the cockpit to act against the wrong repo, even if the
  Claude session's own working-directory context is confused.
- Wrap the existing `plankton`/`nekton` CLI (already vendored as `bin/plankton`, `bin/nekton` in
  the participant template) — never reimplement canonicalization, signing, registry, or trust logic.
- Resolve trust (signature verification, tier membership) *before* any result reaches Claude —
  never let Claude interpret an unverified `by`/`keyid` field itself.
- Stay deletable: every operation the cockpit performs must remain runnable by hand exactly as
  today's `CLAUDE.md` participant brief already documents (same acceptance test as `kton` itself).

## Tech Stack

| Technology | Purpose |
|---|---|
| Go | Cockpit implementation language — matches `plankton`/`nekton`/`kton`, which are all Go; participant repos ship zero JS tooling today |
| MCP (Model Context Protocol), Go SDK (`modelcontextprotocol/go-sdk`, official) | Exposes exactly 3 tools (`cockpit_publish`/`cockpit_say`/`cockpit_ask`) over stdio — a hard capability boundary, not a policy one |
| `plankton`, `nekton` binaries (vendored per participant repo) | All actual canonicalization, hashing, signing, registry, and verification — the cockpit only shells out to these |
| git / `gh` | Commit-pinned permalinks, publish = commit+push |
| JSON Schema (`cockpit.config.schema.json`) | Documents the `cockpit.config.json` shape for tooling/editors; the cockpit's own startup check is a hand-written required-field validator (`internal/config`), not schema-driven |

## In Scope

- The three MCP tools and their behavior (see [design phase](phases/design/index.md)).
- `cockpit.config.json` schema: repo binding, paths, signing identity, allowed claim templates,
  trust tiers, reproduction policy.
- The anti-wrong-folder guard (repo/remote verification on every call).
- Verify-before-filter trust resolution for `ask`.
- Rollout plan into a forked/derivative `plankton-participant-template`.

## Out of Scope

- Any reimplementation of plankton/nekton kernel logic (canonicalization, signing, registry,
  hash-chains) — explicitly forbidden by the governing spec's "Nicht bauen" section.
- A fourth verb, or any cockpit-owned mutable state beyond the static config file.
- Live network federation features beyond what `plankton`/`nekton`/`kton` already provide
  (`kton mirror`/`anchor` stay as-is; the cockpit only decides *whether* to shell to them).
  `kton serve` was named here too and no longer exists: kton removed it in #83 — "a protocol
  reference is not a place to ship a service" — and #85 answered the same query over stdout, which
  is what `cockpit show` reads now.
- Governance/trust-root policy decisions themselves — the cockpit applies a config; who goes in
  that config is a decision made by whoever operates a given participant repo, not by this project.

## Team

- Wolfgang Schwarzenbrunner — author/operator

## Progress

See [progress.md](./progress.md)
