---
title: "Running without git, and what replaces the anti-wrong-folder guard there"
type: "decision"
project: "claude-science-cockpit"
date: 2026-09-02
status: "proposed"
tags: [architecture, guard, git, config]
---

# ADR-004: Running without git

## Status

Proposed.

## Context

Stage 1 made committing and pushing configurable. Stage 2 is running with no git repository at all —
a plain directory holding a registry, keys and a config.

The obstacle is not the git calls. Those are three, and stage 1 already routes around them. It is
that **the anti-wrong-folder guard is the git check**. `config.Load` resolves the repository root
with `git rev-parse --show-toplevel`, reads `cockpit.config.json` from exactly that root, and then
requires the repo's own `git remote get-url origin` to name the owner/repo the config claims. Remove
git and all three steps go with it: there is no root, no config location, and no second opinion.

That guard is why this project exists. A live demo run had six separate `plankton-data`/`nekton-data`
directories scattered under one parent, and a session cooperated against the wrong one.

### What the guard actually buys, stated precisely

It is a **cross-check between two independent sources**: the config file says "I belong to
owner/name", and git's own metadata agrees or does not. What it catches is the cockpit acting on
repository B while configured for repository A.

What it does not catch is a *copy* of A: a duplicated clone carries both the config and the same
remote, so both sources still agree. That limit is worth naming, because the replacement below has
the opposite shape — and neither shape catches everything.

## Decision

*(Proposed.)*

`repo` gains a mode.

```json
"repo": { "mode": "git", "owner": "…", "name": "…" }     // today's behaviour, the default
"repo": { "mode": "local", "root": "/abs/path/to/dir" }  // no git
```

**In `local` mode the guard becomes: the directory this config is in must be the directory it says
it is in.** The root is found by walking up from the starting directory to the nearest
`cockpit.config.json`, and that location must equal the absolute `root` the config declares. On any
mismatch every tool call refuses, exactly as today.

The two checks fail on opposite things, and both are worth having in their own context:

| | catches | misses |
|---|---|---|
| `git` mode | acting on a different repository than configured | a duplicated clone of the right one |
| `local` mode | a copied or moved directory — the config names where it was written | two directories at paths that both look right |

`local` mode is a stricter shape for the incident that motivated the guard, which was several
sibling directories under one parent rather than a different remote.

Walking up to find the config is safe here precisely because of the declared root: a config found in
some ancestor only passes if that ancestor is the path it names.

### Consequences for the rest

- **No permalinks, no locators.** They are built from `owner/name` and a commit sha, none of which
  exist. Fotons are recorded without locators, as stage 1 already does when commits are off. A
  foton never implied its bytes were published anywhere.
- **Committing is off, structurally.** `git.commit: true` alongside `mode: local` is refused rather
  than silently ignored — it states something that cannot happen, which is the one case where
  silence is worse than an error.
- **Everything else is unchanged.** Signing, the registry, verification against trust tiers, the
  three verbs, `say`'s reproduction precondition, the environment pin. None of them ever involved
  git.

### Why the guard is not simply dropped in local mode

Because the directory is the only thing left that can be wrong. With git gone there is no remote to
disagree with, so a config with no anchor at all would let the cockpit act on whatever directory it
was pointed at — which is the original incident, reproduced exactly.

## Consequences

- Repos that use git are entirely unaffected; `mode` defaults to `git` and an absent `repo.mode`
  behaves as before.
- A local-mode directory becomes non-portable on purpose: moving it requires updating `root`, which
  is a deliberate re-confirmation rather than an obstacle.
- `doctor` reports which mode is in force and what the guard checked.
