---
title: "Everything kton under .kton/: the git and local layout, private things outside the repository"
type: "decision"
project: "kton-cockpit"
date: 2026-11-18
status: "proposed"
tags: [layout, backend, git, local, keys, identity, packages, federation]
---

# ADR-016: Everything kton under `.kton/`

## Status

Proposed (2026-11-18), for the core round (`kton-coordination/proposals/2026-11-18-core-change.md`, section 5.2a,
MH's direction of 18.11.). To be agreed with ktonpkg (package layout) and cockpit-improve (its private
folder) before it is accepted. Released with cockpit v0.5.0 on kton v0.4.0, together with the ADR-013
amendment. **Hard break, no migration** (MH): the only installations are MH's and Lukas's.

## Context

- **The protocol prescribes no folder format.** In improve, kton's information lives in the repository's
  databases. A layout is a backend's business: this ADR is the git and local backends' (ADR-011).
- **Today kton is spread over the repository root:** `cockpit.config.json`, `registry/{plankton,nekton,keys}`,
  `templates/`, `keys/` (private keys, gitignored), `packages/`, `runs/`, `bin/`, `.kton/` (shim streams).
  `registry/` and `templates/` are names other things use too.
- **`keys/` means two things.** In the cockpit it holds private keys; in a ktonpkg package it holds public
  keys. A package inside a cockpit repository, or `scope union` over one, reads private-key folders as
  public keys.
- **One shared config holds a local identity.** `identity.*` names this checkout's signing key, so every
  participant of a shared repository carries an uncommitted edit to `cockpit.config.json`, and `sync`
  refuses until they take the shared version back (found in example 18).
- **`.kton/` already means two things**: the shim's call streams and the recorder's `derivations.json`.

## Decision

### 1 The tree

```
<repository>/
  .kton/                              everything kton, and only kton
    cockpit.config.json               the shared configuration (committed); its presence marks the root
    plankton/                         the kernel's store: objects/sha256/<hex>.json, material.jsonl, .seq, peers.json
    nekton/                           objects/scope/<id>.nekton.jsonl (the root scope too, kton #213), .format
    keys/<tier>/<name>[-claims].pub   PUBLIC keys only, the trust tiers by folder
    blobs/<hh>/<hex>                  bytes by hash, the kernel's blobstore shape
    templates/*.json, aliases.json    the repository's own vocabulary (packages bring theirs)
    packages/<name>@<rev>/            installed packages, each in the package's own layout (section 3)
    local/                            NEVER shared (.kton/.gitignore lists it): this checkout's state
      identity.json                   which key this checkout signs with, and its keyring
      calls/<id>/                     tool-shim streams (SPEC §10.4)
      derivations.json                the recorder's
  work/                               runs, by default: `runs.dir` in the configuration (section 4)
  (the work itself, under its usual names)
```

- **`cockpit.config.json` moves in.** It is kton's, so it belongs under `.kton/`. The binding check
  (SPEC §5) looks for `.kton/cockpit.config.json` in the working directory, never upward, as today.
- **Identity leaves the shared config.** `.kton/local/identity.json` names this checkout's keys
  (`plankton_key`, `nekton_key`, `keyring`, `session_id`). `keygen <name>` writes it. The shared config
  holds only what every participant agrees on, so `sync` no longer conflicts with who is signing.
- **Public keys by tier folder** (`keys/people/ana.pub`) instead of the tier as a file-name prefix: the
  tier configuration then lists folders, and `trust add` copies into one.
- **`bin/`** (binaries the cockpit fetched for a run) moves to the machine cache; it never belonged to the
  repository.
- **`docs/data/`** (`show`'s output for kton-web) stays where the configuration says: it is a publication,
  not kton state.

### 2 Private things never in a repository

- **Private keys:** `~/.kton-keys/<name>.key` (and `<name>-claims.key`), the place the files-and-backup
  proposal of 02.11. named; `identity.json` points there. `keygen` refuses a key path inside a repository.
- **Licences and other hidden configuration:** `~/.config/kton-cockpit/licences/<name>` (ADR-015).
- **Machine state:** `~/.cache/kton-cockpit/{index,cas,fetch,sources,watch,repro,bin}`, as today plus `bin`.

### 3 Packages

An installed package keeps the package's own layout under `.kton/packages/<name>@<rev>/`. With this tree its
`keys/` holds public keys, the same meaning as the repository's, so the clash is gone. **Asked of ktonpkg:**
a package's record stores use the same names as above (`plankton/`, `nekton/`, `keys/`, `blobs/<hh>/<hex>`, written with plankton's blobstore),
so a package and a repository read alike, and a workflow's extracted files go to `work/`, not `packages/`.

### 4 Runs

Where runs happen is the git backend's configuration (`runs.dir`, default `work/`): `cockpit run`, a
workflow run and `reproduce` all go there, one folder per run. In improve, runs happen on the run server
and this key does not exist.

### 5 What reads the old paths

Nothing (hard break). The federation's `mirror_once.py` scans `registry/{plankton,nekton}/objects/` today;
it moves to `.kton/{plankton,nekton}/objects/` in the same release, which is why `plankton_dir` and
`nekton_dir` stop being configurable: the layout is fixed per backend.

## Consequences

- One folder to ignore, back up, or hand to another tool; `registry/` and `templates/` are free again.
- Sharing a repository no longer shares an identity edit; example 18's `catchup` step disappears.
- Every example, test fixture (`internal/testrepo`), kton-examples participant and the federation template
  change paths in one release.
- SPEC §5.1 (config location), SPEC §9.8 (federation paths), SPEC §10.4 (streams) and SPEC §12.4 (stamp
  exclusions) change.

## Open (to agree)

- **ktonpkg:** built on branch `adr-016-layout` (1601d90): a package's stores are `plankton/`, `nekton/`,
  `keys/` (public, by key id; readers walk `keys/**/*.pub`, so a repository's tier folders read too) and
  `blobs/<hh>/<hex>`; `scope union` takes package or repository roots. Blob path confirmed 18.11. To be
  tagged v0.8.0 once this ADR is accepted.
- **cockpit-improve:** agreed (18.11.). Its private `.cockpit-improve/` becomes `.kton/local/improve/` on a
  local checkout, and its config lookup stops walking upward.

Both lanes have agreed; what remains is MH's accept.
