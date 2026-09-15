---
title: "cockpit.config.json schema"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [config, schema]
---

> **Design phase, 2026-07-22.** This records what was designed and why, at that time. The shipped
> contract is [`spec/SPEC.md`](../../../../spec/SPEC.md), whose every normative clause names the test
> that checks it; where the two disagree, the spec is right and this is history. Kept because the
> reasoning behind a decision does not survive in the clause that resulted from it.

# cockpit.config.json schema

## Summary

Static configuration file at the repo root, validated at cockpit startup, never written or
editable by Claude through any tool. This is the "Config ist Obergrenze" (config is the ceiling)
requirement from the governing spec made concrete.

## Details

```jsonc
{
  "repo": { "owner": "deathbychoco", "name": "participant-alice" },
  "paths": {
    "plankton_dir": "registry/plankton",
    "nekton_dir": "registry/nekton",
    "templates_dir": "templates",
    "keys_dir": "keys",
    "bin_dir": "bin"
  },
  "identity": {
    "session_id": "session-1",
    "plankton_key": "keys/session-1.key",
    "nekton_key": "keys/session-1-claims.key"
  },
  "verbs": { "publish": true, "say": true, "ask": true },
  "claims": { "allowedTemplates": ["reproduces", "working-on"] },
  "trust": {
    "tiers": {
      "self": ["<this repo's own registry/keys/*.pub, resolved at load time>"],
      "federation": ["<pubkeys from the aggregator's registered participants>"]
    }
  },
  "reproduction": {
    "requiredLevel": "L0",
    "normalizer": null
  }
}
```

> **Superseded.** Five more blocks exist now, each optional and each off by default: `environment`
> and `execution` (pinning and running in a container, ADR-003), `git` (committing and pushing are
> configurable, so a repo can record without either), `union` (publishing the aggregate as committed
> files), `anchor` (witnessing records in a transparency log) and `material` (carrying evidence about
> a record, §11). `repo` also gained `mode`, for running with no git repository at all (ADR-004).
> The authoritative shape is `cockpit.config.schema.json` in the repository root.

`cockpit.config.schema.json` documents this shape for editor/tooling support, but the cockpit's
own startup check (`internal/config.validate`) is a hand-written required-field validator, not
schema-driven — it exits with a clear, specific error, never a silent default, if the config is
missing, malformed, or if the repo/remote mismatch check (see the
[anti-wrong-folder guard](anti-wrong-folder-guard.md)) fires. Wiring `validate()` to actually
enforce `cockpit.config.schema.json` at runtime, rather than duplicating a subset of it by hand,
is open follow-up work, not yet done.

## Notes

- `claims.allowedTemplates` is the ceiling for `cockpit_say` — Claude can request one of these
  templates, never invent a new claim shape.
- `trust.tiers` is the ceiling for `cockpit_ask`'s `filter` — a query can narrow within these
  tiers, never surface a signer/tier outside them.
- `reproduction.requiredLevel`/`normalizer` drives the reproduction precondition `cockpit_say` runs
  itself before writing a `reproduces` claim's `level` field — Claude never self-declares that.
