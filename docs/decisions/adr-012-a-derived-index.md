---
title: "A derived index: one per machine, fed by the sync feed, never authoritative"
type: "decision"
project: "kton-cockpit"
date: 2026-10-19
status: "proposed"
tags: [architecture, index, query, oxigraph, sparql, federation, performance]
---

# ADR-012: A derived index

## Status

Proposed (2026-10-19). Builds on ADR-011 (the index is a port). It needs a SPEC §13 amendment before phase 1
ships. Phase 0 (`none`) holds no state and changes no behaviour.

## Context

Every `ask` re-opens the registries, which replays their logs. With `federation.sources` configured, it
deletes and rebuilds the whole union in the user cache, once per call and again for every claim it verifies.
SPARQL exists only in the unreleased build, as a fresh Oxigraph container and a fresh projection per call.
That is fine for a few hundred records in one repository. It does not hold for queries across aggregations,
for vocabularies (ktonpkg profiles) that run SPARQL rule sets, or for several clients (CLI, MCP, ktonwatch,
kton-web viewers) asking the same machine.

The kernel already defines how a consumer follows a registry incrementally. The kton §12 sync feed is `Epoch()`,
`MaxSeq()` and `Records(since)` on both the plankton and nekton registries. With 0.3.0, the kernel also
ships the RDF projections as libraries (`kton.dev/plankton/rdf`, `kton.dev/nekton/nanopub`).

## Decision

### 1 The index is derived, never authoritative

The signed registries stay the only source of truth. The index:

- is **rebuildable from the registries alone**: deleting it loses nothing, and the next call re-ingests;
- is **never consulted for the binding guard** (SPEC §5 stays: re-checked on every call, never cached);
- **stores no trust tier**. It keeps each record's signer key ids and whether the substrate verified the
  signature. The tier comes from the *calling* repository's `cockpit.config.json` at answer time. The same
  record can be trusted in one repository and unknown in another, and a configuration change needs no
  rebuild;
- **says where an answer came from**: index or direct read, and at which heads per registry. A registry the
  index could not catch up on is reported, so a partial union never passes for a whole one (SPEC §9.8);
- **ingests through the substrate**: the kernel's registry API and its RDF projections, never the files
  (SPEC §12.3).

### 2 One index per machine, across aggregations

- **Location:** `<UserCacheDir>/kton-cockpit/index`.
- **Records:** each record is stored once, by its content-addressed id, in its own named graph
  `urn:kton:rec:<id>`.
- **Registries:** each registry has a graph `urn:kton:reg:<identity>` listing the records it holds, together
  with its `(epoch, maxSeq)`. A repository, an aggregation and a federation source are sets of registry
  graphs.
- **Scope of an answer:** by default, `ask` answers over exactly the union SPEC §9.8 defines for the calling
  repository (own + configured sources). A query across aggregations names its wider scope explicitly, and
  the answer lists the registries it covered.
- **Observations:** unsigned ktonwatch observations live under `urn:kton:observed:`. `ask` never reads that
  namespace. Viewers may, and they label it as unsigned.

### 3 The index follows the sync feed

The index is one more sync peer. For every registry it keeps `(epoch, maxSeq)` and applies
`Records(maxSeq)` as a delta. Updates arrive from three places, all through one `Apply`:

1. **The cockpit**, right after `publish` or `say` has persisted (write-through).
2. **Sources and backends**, when a pull or check-in moves their head.
3. **A head check before an answer**, which catches up on anything written elsewhere.

When an epoch changes, the index re-ingests that registry from zero. A co-signature can renumber a record in
place, so `Apply` replaces a record's graph by id. nekton's feed includes deferred claims: they are ingested
marked as deferred and excluded from answers until their dependency resolves, as the registry does itself.

### 4 Three implementations, chosen in configuration

| `index.kind` | lives | for |
|---|---|---|
| `none` (default) | per call | today's behaviour. The reference, the fallback, and the proof that every operation stays performable with the substrate alone. |
| `memory` | as long as the process | the MCP server, which is already long-running |
| `oxigraph` | a daemon | SPARQL vocabularies, aggregations, several clients |

**Who runs Oxigraph:** a cockpit starts it.
- The first call that needs it starts `oxigraph serve` as a container pinned by digest (`index.image`), on
  loopback only, with its store under the index directory. It stays up after that call.
- A lockfile records the port, the image digest and the store schema version. A cockpit that finds a
  running daemon with a different digest or schema does not use it: it answers through `none` and says so.
- `index.url` means "connect, don't start", which leaves room for a service run elsewhere.
- Operator commands (CLI only): `cockpit index status | start | stop | rebuild`.
- The container runs with a memory limit and a per-query timeout, so one unbounded vocabulary query cannot
  take the machine down.

### 5 The port

```go
type Reader interface { /* the reads ask, verify, material and scope need today */ }
type Index interface {
    Reader
    Heads(ctx) (map[RegistryID]Head, error)   // what has been digested
    Apply(ctx, Delta) error                   // digest records, or drop a registry
}
```

`none` implements `Reader` by reading the registries, as today. Its `Heads` is empty and its `Apply` is a
no-op, because it holds nothing. A conformance suite (`indextest`) runs the same questions against every
implementation and requires identical answers to `none`.

## SPEC amendment (before phase 1)

SPEC §13 currently reads: "no state of its own between calls beyond that static configuration". It becomes:

> No **authoritative** state of its own beyond that static configuration. Derived caches (the index, source
> clones) are allowed when they are rebuildable from the substrate alone, are never consulted for the binding
> verdict or the trust tier, and deleting them changes no answer. An answer states whether it came from such
> a cache and at which heads.

The amendment lands together with the first stateful implementation (`memory`), not before, and after
`cockpit/adr-009` has been merged, because that branch edits the same section.

## Open

- **Noticing a change cheaply.** `MaxSeq` is only known after `Open`, and `Open` replays the log. The options
  are to re-open on every call (the replay stays, the union and projection costs go), to watch the registry
  directories and git `HEAD` in long-running processes, or to ask the kernel for a call that reports
  `(epoch, maxSeq)` without replaying. Undecided.

## Phases

| phase | content | needs |
|---|---|---|
| 0 | port + `none`; `ask` reads through it | nothing |
| 1 | `memory` in the MCP process; write-through; head check; SPEC §13 amendment | the open point above |
| 2 | `oxigraph` daemon; `query.go` runs on it; `cockpit index …` | kernel 0.3.0 (RDF projections) |
