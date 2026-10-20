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

Proposed (2026-10-19). Builds on ADR-011 (the index is a port). Phase 0 (`none`) holds no state and changes no
behaviour. Phase 1 (`memory`, 2026-10-20) brings the SPEC §13 amendment with it, and adds SPEC §12.4.

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

This is how an index that digests records (`oxigraph`) keeps up. `memory` digests nothing: it holds the
registries as the kernel opened them and opens them again when they change (section "Noticing a change").

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
| `memory` | as long as the process | the MCP server, which is already long-running. It keeps the opened registries and checks them before every answer (section "Noticing a change") |
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

As built in phase 1, the port is still `Reader` alone: `memory` needs no `Heads` or `Apply`, so they arrive
with `oxigraph`. `memory` is a `binaries.Runner` with an `Opener` that keeps the opened union, so it answers
through the same code as `none` by construction; its tests compare it with `none` after every kind of change.

## SPEC amendment (before phase 1)

SPEC §13 currently reads: "no state of its own between calls beyond that static configuration". It becomes:

> No **authoritative** state of its own beyond that static configuration. Derived caches (the index, source
> clones) are allowed when they are rebuildable from the substrate alone, are never consulted for the binding
> verdict or the trust tier, and deleting them changes no answer. An answer states whether it came from such
> a cache and at which heads.

The amendment landed together with the first stateful implementation (`memory`), in SPEC §13 and the new SPEC §12.4.
`cockpit/adr-009` edits the same line of SPEC §13 (it removes "no fourth verb"); whichever merges second keeps
both changes.

## Noticing a change

`MaxSeq` is only known after `Open`, and `Open` replays the log, so the kernel cannot say cheaply whether
anything changed. Decided for phase 1 (2026-10-20):

- **`memory` stamps the files.** Before every answer it takes the path, size and modification time of every
  file under the registries the union is built from (own and every source), with `stat`. It never reads
  them, so it learns nothing about the store's layout (SPEC §12.3). The same stamp keeps the held union; any
  other stamp opens it again from the substrate. A write in this process, a pull, a check-in, another
  cockpit on the same repository: each changes a file, so none of them has to notify the index, and
  write-through needs no `Apply` yet.
- **The stamp is taken before opening**, and the union is held under it. A write landing between the stamp
  and the open costs one more open on the next call, never a stale answer. Opening can itself write (the
  kernel persists its numbering), which costs the same one extra open.
- **No backend or git involved:** the stamp covers uncommitted writes too, which `Syncer.Head` would not.
- **Later:** a kernel call that reports `(epoch, maxSeq)` without replaying would let `oxigraph` follow the
  feed without re-opening. It is a kton issue to file before phase 2, not a precondition of phase 1.

## Phases

| phase | content | needs |
|---|---|---|
| 0 | port + `none`; `ask` reads through it | nothing |
| 1 | `memory`: the opened union kept per process, the file stamp before every answer; `ask` says `index`; SPEC §12.4, SPEC §13 amendment | done (2026-10-20) |
| 2 | `oxigraph` daemon; `query.go` runs on it; `cockpit index …` | kernel 0.3.0 (RDF projections) |
