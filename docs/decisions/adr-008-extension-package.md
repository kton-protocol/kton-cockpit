---
title: "An extension API, a GitHub/Docker reference implementation, and improve as an extension"
type: "decision"
project: "kton-cockpit"
date: 2026-09-30
status: "proposed"
tags: [architecture, api, extension, backend, github, docker, improve]
---

# ADR-008: The cockpit as core, reference implementation and extensions

## Status

Proposed (2026-09-30), on `jam-beta`. This is ADR-005's backend interface (S2) turned into the
cockpit's public shape.

## Context

The cockpit has three verbs as a library (ADR-005 S1). What happens *behind* a verb is still wired
in, though: the binding check, commit and push, permalinks and the container all assume git on
GitHub and Docker. improve does the same jobs differently. It binds to a server folder, persists
through its own check-in, locates by entity version id, and executes on its run server. Today improve
can only be reached through improvego commands next to the cockpit; the acceptance scenario shows
every such step as a gap.

improvego is not public, and neither is ktonpkg. The public cockpit must not depend on them.

## Decision

Three layers, in three places.

### 1 The core: the extension API (public, in kton-cockpit)

- **`cockpit`**: the three verbs, unchanged for callers (ADR-005).
- **`cockpit/backend`**: the interfaces a backend implements, as ADR-005 sketched them ("A backend interface instead of a mode flag").

```go
type Backend interface {
    Binder   // the anti-wrong-folder guard: two independent sources agree (SPEC §5)
    Store    // makes written records durable; returns a Revision
    Locator  // where bytes can be fetched: a URI per path and revision
    Executor // runs a command, or realises a potential, in a pinned environment
}

func Register(mode string, f Factory) // called from an extension's init()
```

- Which backend a repository uses is `repo.mode` in its configuration. An unknown mode refuses:
  a binary that lacks the extension says so, instead of acting as another backend.
- A backend adds no fourth verb and holds no trust logic. It answers *where* and *how*, never
  *whether to believe* (SPEC §13). Verification against trust tiers stays in the core.
- The core carries its own conformance test suite (`backend/backendtest`). Every backend, including
  one outside this repository, runs it against itself: the guard refuses a wrong folder, a written
  record is durable, a locator's bytes match their hash.

### 2 The reference implementation: GitHub and Docker (public, in kton-cockpit)

- **`backend/github`**: git mode as it is today. The binding is the `origin` remote, persisting is
  commit and push, locating is the commit-pinned `raw.githubusercontent.com` permalink.
- **`backend/local`**: local mode (ADR-004).
- **`executor/docker`**: the pinned container (ADR-003).
- The `cockpit` binary links these. Its behaviour does not change; the tests stay as they are and
  also run the conformance suite.

### 3 improve as an extension (private, next to improvego)

- A separate module implements `cockpit/backend` with improvego:
  - **Binder**: the server-side configuration in the improve root folder (ADR-005, "The guard in improve mode").
  - **Store**: improve's check-in, and the register as files in improve.
  - **Locator**: `improve://<host>/<entityVersionId>`, content-hash checked on fetch.
  - **Executor**: `Realize` on the run server.
- It also carries improve's review. `say reviewed` in improve mode opens the improve review instead
  of writing a claim, and the claim appears only as the projection of a decided entry (ADR-007).
- **A binary per composition.** Go links statically, so an extension is a blank import in its own
  `main`: `cockpit-improve` is the core, the reference implementation and the improve extension,
  built together. The public `cockpit` stays free of improve.
- The same applies to `libcockpit` (ADR-005 S7): the C-ABI build for R and TypeScript is built from
  whichever composition a site uses.

## Steps

| step | content | behaviour changes? |
|---|---|---|
| E1 | `cockpit/backend`: interfaces, registry, conformance suite | no |
| E2 | today's git, local and container code moves behind them as `backend/github`, `backend/local`, `executor/docker` | no |
| E3 | the core selects by `repo.mode`; an unknown mode refuses | no, except a clearer refusal |
| E4 | improve extension module and `cockpit-improve` binary; SPEC §5 gains an improve clause | new |
| E5 | the acceptance scenario runs its improve side through `cockpit-improve` instead of improvego commands, which closes the S4 gaps there | — |

## Open questions

1. Where does the improve extension live: in improvego (which has no remote yet), or in a private
   repository of its own that depends on the public cockpit and on improvego?
2. Should improvego get a remote (private, under gitmick), so the extension can require it like
   ktonpkg, without `replace`?
