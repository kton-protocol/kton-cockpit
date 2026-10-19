---
title: "Ports, bundles and modules: a small core with everything else pluggable"
type: "decision"
project: "kton-cockpit"
date: 2026-10-19
status: "proposed"
tags: [architecture, api, extension, backend, executor, index, module, frontend, vscode]
---

# ADR-011: Ports, bundles and modules

## Status

Proposed (2026-10-19). This builds on ADR-008 (an extension API, a GitHub/Docker reference implementation,
improve as an extension) and on ADR-009 (the verb count is not fixed). It finishes ADR-008's E4 for the
executor, and it names where ktonpkg and ktonwatch plug in. The read side, the index, is its own decision
(ADR-012).

## Context

ADR-008 put the binding, persisting and locating behind `cockpit/backend`. Everything else that differs
between "a GitHub repository with Docker" and "improve" is still wired in:

- **Execution:** `internal/container` runs `docker`/`podman` via `exec`. The improve run server cannot
  stand in.
- **Federation reading:** this is behind a registry (`cockpit/source`), but nothing ties a source kind to
  the backend that brings it.
- **Reading records:** every read goes through `internal/binaries` directly, re-opening the registries on
  every call. With federation configured, it rebuilds the union for every claim it verifies.
- **Packages and the recorder:** ktonpkg is a private import behind `-tags unreleased` (`internal/packages`).
  ktonwatch (on `recorder`) talks to the cockpit only by printing a `cockpit publish` line for a human to run.

"GitHub+Docker" and "improve" are each a **bundle** of these capabilities, but nothing names the bundle,
and an extension that adds verbs (ADR-009) has no defined door into the core.

## Decision

### 1 The core owns verbs, the guard and trust. Everything else is a port.

The core is `publish`, `say` and `ask` plus whatever verbs a module adds (ADR-009). It also owns the
binding check on every call (SPEC §5) and the trust tiers, which it applies from the configuration at the
moment it answers (SPEC §13). Everything that answers *where* or *how* is a **port**: an interface with a
registry, filled from `init` functions and chosen by name in `cockpit.config.json`. Nothing is on until it
is enabled.

| port | package | answers | built-in implementations |
|---|---|---|---|
| Binder · Persister · Locator | `cockpit/backend` | is this the repository; how does a write become durable; where can its bytes be fetched | `github`, `local` |
| Executor | `cockpit/executor` | where and how a command runs | `container` (docker/podman) |
| Source | `cockpit/source` | where federated records come from | `dir`, `git` |
| Index | `internal/index` (ADR-012) | how records are read and queried | `none` (today's behaviour) |

### 2 A backend is three small interfaces

`Backend` stays, but as the composition of `Binder` (`Validate`, `Bind`), `Persister` (`Persist`) and
`Locator` (`Locate`). Existing implementations compile unchanged. A conformance suite or a caller that
needs only one part takes only that part.

### 3 The executor is a port

`executor.Executor.Run(ctx, Step) (Result, error)`. A `Step` is self-contained: the repository root, the
image, the network policy, the paths to hide from the command, and the command itself. It carries no
cockpit configuration type, so an implementation outside this repository can provide one. `publish` calls
the executor named in `execution.executor` (default `container`) and no longer calls `exec` itself. The
snapshot diff that finds a run's outputs stays in the core, because it is the core's evidence, not the
executor's claim.

### 4 Bundles are a composition, not a type

A bundle is the set of registrations one module makes from its `init`: improve registers a backend mode,
an executor, a source kind and later an index feeder. The binary that links the module *is* the
composition (ADR-008 §3), so there is no `Bundle` struct to keep in sync with the ports.

### 5 Modules add verbs through a narrow door

A module (ktonpkg, ktonwatch, improve) may register:

- **verbs** (ADR-009): one definition, exposed on CLI, MCP and Go, under the configuration ceiling;
- **operator commands**: CLI only (SPEC §6);
- **a vocabulary**: named queries and templates, run on the index (ADR-012);
- **event subscriptions**: `published`, `said`, `source-pulled`.

A module reaches the core only through `Publish`, `Say`, `Ask`, the index's read side and the executor.
It never opens a registry. In particular, ktonwatch writes only through `Publish`, after a human accepts a
draft, so "nothing enters kton automatically" stays true.

The module registry is specified when the first module moves (ktonpkg), not before. This ADR fixes only
the shape above.

### 6 Frontends are clients, never a second core

A frontend (a VS Code or Positron extension, a viewer, kton-web) presents the cockpit to a person. It talks
to the core over a **transport**. It does not reimplement the verbs, read the registries itself, or apply
trust. There are four transports, all serving the same handlers (SPEC §6: one implementation per verb):

| transport | for | reaches |
|---|---|---|
| CLI | a person in a shell, scripts | verbs + operator commands |
| MCP (stdio) | an agent | **verbs only** (SPEC §6) |
| Go API | modules, extension binaries, tests | verbs + ports' read sides |
| **frontend protocol** (new) | an editor extension or viewer | verbs, the operator commands a person may confirm, index queries, events |

The **frontend protocol** is JSON-RPC 2.0 over stdio. The extension starts `cockpit serve --stdio` (the
same pattern as a language server), so one installed binary is still the whole install, and the protocol
needs no port and no authentication. It carries:

- **requests:** the verbs, `ask` with index scopes (ADR-012), and the vocabulary queries of enabled modules;
- **operator commands**, marked as needing a person. The frontend shows a confirmation, and the core refuses
  them unless the request carries that confirmation. This is the line SPEC §6 draws for MCP: an agent never
  reaches operator commands, and a person in an editor does, explicitly;
- **notifications:** `published`, `said`, `source-pulled`, index heads moved, and module events (ktonwatch:
  session recorded, draft ready). A frontend refreshes on these instead of polling.

The binding guard still runs on every request, against the workspace folder the extension opened.

**Consequences for the existing frontends:**
- **kton-mignon** (`@kton/kern`) has its own TypeScript implementation of configuration, keys, nekton
  reading and writing, and an in-process Oxigraph. Under this ADR, that core becomes a client of the
  frontend protocol. Distributions without a cockpit binary would need the core built for the browser (Go to
  wasm, as `cockpit/ARCHITEKTUR-BILD.md` §2.5 plans). That is the owning lane's decision, not taken here.
- **The Positron extension** (`recorder/positron`) stops pasting `cockpit publish` into a terminal. It sends
  the accepted draft as a `publish` request, and it receives ktonwatch's events over the same connection.

## Consequences

- The public `cockpit` binary links `github`, `local`, `container`, `dir`, `git` and `none`. `cockpit-improve`
  additionally links improve and ktonpkg. Behaviour of the public binary is unchanged by this ADR.
- `internal/packages`, `cockpit/install.go` and `cockpit/workflow*.go` move into the ktonpkg module once the
  module registry exists. The `-tags unreleased` split for packages then goes away.
- SPEC §13's "no own canonicalisation, signing, registry or chain logic" and "no trust logic beyond the
  configuration" are untouched. "No state between calls" is ADR-012's to amend.

## Steps

| step | content | behaviour changes? |
|---|---|---|
| P0.1 | the union is built once per call, not once per verified claim | no (faster) |
| P0.2 | `Backend` = `Binder` + `Persister` + `Locator` | no |
| P0.3 | `cockpit/executor` with `container`; `publish` runs through it | no |
| P0.4 | `internal/index` with `none`; `ask` reads through it (ADR-012) | no |
| P3 | module registry; ktonpkg and ktonwatch as modules | new verbs, under the ceiling |
| P4 | improve bundle: backend, executor, source, index feeder | new |
| P5 | frontend protocol: `cockpit serve --stdio`, events; Positron extension on it, then kton-mignon | new transport |
