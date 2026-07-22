---
title: "cockpit.config.json schema"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [config, schema]
---

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

Validated against a JSON Schema (`cockpit.config.schema.json`) at process startup; the cockpit
exits with a clear, specific error — never a silent default — if the config is missing,
malformed, or if the repo/remote mismatch check (see the
[anti-wrong-folder guard](anti-wrong-folder-guard.md)) fires.

## Notes

- `claims.allowedTemplates` is the ceiling for `cockpit_say` — Claude can request one of these
  templates, never invent a new claim shape.
- `trust.tiers` is the ceiling for `cockpit_ask`'s `filter` — a query can narrow within these
  tiers, never surface a signer/tier outside them.
- `reproduction.requiredLevel`/`normalizer` drives the reproduction precondition `cockpit_say` runs
  itself before writing a `reproduces` claim's `level` field — Claude never self-declares that.
