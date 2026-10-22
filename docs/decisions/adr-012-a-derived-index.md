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
behaviour. Phase 1 (`memory`, 2026-10-20) brings the SPEC §13 amendment with it, and adds SPEC §12.4. Phase 2
(`oxigraph`, 2026-10-20) stored views rather than records, for a reason in the kernel (section "Phase 2 as
built"). Phase 3 (2026-10-22) stores records once, with kton#232 and kton#233 (section "Phase 3 as built").

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
  `urn:kton:rec:<id>`. *(As built in phase 3, the graphs are the kernel's own: a foton's IRI, a claim's four
  nanopublication graphs; see "Phase 3 as built".)*
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
- The first call that needs it starts `oxigraph serve` as a container pinned by digest (`query.image`, the
  image vocabulary queries already ran in), on loopback only, with its store under the index directory. It
  stays up after that call.
- A lockfile records the port, the image digest and the store schema version. A cockpit that finds a
  running daemon with a different digest or schema does not use it: it answers the way it did before the
  daemon existed, a container for that one query, and says so.
- `index.url` means "connect, don't start", which leaves room for a service run elsewhere.
- Operator commands (CLI only): `cockpit index status | start | stop | rebuild`.
- The container runs with a memory limit (`index.memory`, default 2g) and every request with a timeout
  (2 minutes), so one unbounded vocabulary query cannot take the machine down.

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
  them, so it learns nothing about the store's layout (SPEC §12.3). It follows symlinks, and leaves out
  only the bookkeeping that opening a registry rewrites (`.seq`, `.format`, `*.lock`, `.tmp-*`): a store
  reads a dot-named claim file too, so a wider rule would miss a change (kton-review, PR #7). The same stamp keeps the held union; any
  other stamp opens it again from the substrate. A write in this process, a pull, a check-in, another
  cockpit on the same repository: each changes a file, so none of them has to notify the index, and
  write-through needs no `Apply` yet.
- **The stamp is taken before opening**, and the union is held under it. A write landing between the stamp
  and the open costs one more open on the next call, never a stale answer. Opening can itself write (the
  kernel persists its numbering), which costs the same one extra open.
- **No backend or git involved:** the stamp covers uncommitted writes too, which `Syncer.Head` would not.
- **Later:** a kernel call that reports `(epoch, maxSeq)` without replaying would let `oxigraph` follow the
  feed without re-opening. It is a kton issue to file before phase 2, not a precondition of phase 1. It
  landed as kton#233 (`registry.ReadPosition`), which `oxigraph` uses from phase 3 on; `memory` keeps its
  stamp, since it holds opened registries and reads no feed.

## Phase 2 as built

**Views, not records.** The kernel's projections (`rdf.Turtle`, `nanopub.RenderTrig`) decide attribution
by the trusted keys they are given: a record is `prov:wasAttributedTo` a key only if a *trusted* key
verifies it, and a claim nobody trusted is not projected at all. A projection is therefore an answer for
one key set, not a fact about a record, and there is no per-record projection to store once. Writing the
attribution separately would be a second mapping of what a record says in RDF, which SPEC §12.3 and SPEC §13
leave to the kernel. So the daemon stores **views**:

- A view is the projection of one repository's union (own registries and sources) under one set of trusted
  keys, named by a hash of the repository, its sources and the key ids. A tier is still never stored; a
  configuration that trusts other keys simply reads another view.
- Its graphs share the prefix `urn:kton:view:<id>:`. The plankton projection is `…:plankton`. The claims
  are rendered with the base `…:np:<claim id>.` instead of `https://kton.dev/np/<claim id>.`, so two views
  of one claim never write into the same graph.
- The graph `urn:kton:index` holds each view's stamp (the same file stamp `memory` uses) and its named
  graphs. A query whose stamp differs projects the view again first: its stamp is removed, its graphs are
  deleted, the projection is loaded, and the stamp and graph list are written last. A file lock per view
  makes two cockpits take turns.
- A query runs with exactly the view as its dataset (SPARQL protocol `default-graph-uri` = the plankton
  graph, `named-graph-uri` = the view's claims), so it cannot see another view. Results are mapped back to
  `https://kton.dev/np/`, so an answer reads exactly as a container started for that query returns it
  (`TestQuery_TheIndexAnswersAsAContainerForTheQueryDoes`).
- **That dataset is the only wall between views.** A `SERVICE` clause goes around it: pointed at the
  daemon itself it reads every other view, attribution under other keys included (found by kton-review on
  PR #7). Oxigraph cannot switch `SERVICE` off, so the cockpit refuses a query that uses it, on both
  paths, after tokenizing the query as SPARQL does (`index.CheckQuery`). The alternative, a store per
  view, would give up the one daemon per machine. Phase 3 keeps the same rule.
  **Accepted risk:** confinement now rests on this tokenizer agreeing with Oxigraph's parser about where
  `SERVICE` is. Revisit (the engine's own parsed algebra, or a store per view) before the daemon answers
  anything other than queries from allowed vocabulary packages.
- `query.image` must be pinned by digest: the daemon is told from another by the image it was started
  from.
- Reads (`ask`, verification) under `oxigraph` go through `memory`. Only vocabulary queries use the daemon.
- `oxigraph` is a kind of the `-tags unreleased` build only, which links the RDF packages until a kton
  0.3.0 tag exists. `cockpit index` is in every build.

**What it costs, and what lifts it.** A changed registry re-projects its whole view, and a machine with N
repositories of different trust holds N copies of shared records. Both go away with a kernel projection per
record that keeps attribution apart from the record (one graph per record, one per verifying key). That
is a kton proposal (requested via `active/kton`), together with a cheap `(epoch, maxSeq)` (section "Noticing a
change"). Until then, views are correct and the daemon still saves what it was for: a query against an
unchanged registry projects nothing and starts no container.

## Phase 3 as built

With kton#232 (`rdf.Record`/`Attribution`, `nanopub.Record`/`Attribution`) and kton#233
(`registry.ReadPosition`), the store holds what the target in section 2 asked for (store schema 2):

- **Records once.** A record's own quads take no keys; they are loaded once into the kernel's graphs (a
  foton's IRI, a claim's `#Head`, `#assertion`, `#provenance`, `#pubinfo`), whichever registries hold it.
  Its attribution is one graph per key that verifies it (`https://kton.dev/attribution/<hex>/<keyid>`).
- **Per registry directory**, `urn:kton:index` lists the records it holds, the public keys it was
  attributed under, its cursor and its position. Before a query each registry of the union is checked: an
  equal position reads nothing; otherwise `Records(cursor)`, or `Records(0)` when the epoch changed, when
  objects changed with `maxSeq` unchanged (records a git merge brought, numbered by nobody), or when the
  query trusts a key the registry was not attributed under yet. A file lock per registry makes two writers
  take turns; readers do not wait for it. So a read only adds at first: new records and attribution go in,
  what the registry no longer holds is removed after (on a full read), and the position, cursor and keys are
  replaced together last. A registry half read is stale or a superset of what it was, never a subset
  (`TestOxigraph_AReaderNeverSeesARegistryHalfRead`, found by kton-review on PR #8).
- **Attribution is the registry's own.** Each registry lists the attribution graphs of the signatures in its
  own copy of a record (`urn:kton:attests`), and a query selects attribution only through the registries it
  reads. Another registry's copy of the same record may carry other signatures (a co-signature added after
  a pull), and must not attribute this one (`TestOxigraph_AttributionIsTheRegistrysOwn`, also from that
  review). A full read removes only attribution under the keys it read with; a key another configuration
  added meanwhile is left to it.
- **Known limits.** An incremental read never forgets a record that left the registry: stores are append-only,
  so that takes a revert plus a new record before the next query (the next full read corrects it). Each query
  names every graph of its dataset as a request parameter, so the request grows with the union; fine for
  repositories, worth revisiting for aggregations of many thousands of records. Behind `index.url` there is
  no lock: a full read working from an older snapshot can remove a record a concurrent writer just added and
  write its older position. The next query reads the registry again and heals it; one answer in between can
  lack that record.
- **Trust chosen per query.** The dataset is computed for each query from `urn:kton:index`: the default
  graph is every plankton record of the union plus its attribution to the trusted keys; the named graphs are
  the claims a trusted key signed, plus that attribution. A container started for one query loads the same
  dataset (`oxigraph.Dataset`), so the two answer alike by construction
  (`TestQuery_TheIndexAnswersAsAContainerForTheQueryDoes`).
- **What a query sees changed** from phase 2: no `nk:signerVerified false` and no `nk:claimedSigner` (no
  attribution graph means unverified, kton#232), and a claim's attribution is in its own named graph,
  not inside `#provenance`/`#pubinfo`. Nekton's feed includes deferred claims; they are in the store as they
  were in the views.
- A store written under schema 1 (views) is emptied when the cockpit next uses it; one behind `index.url` is
  refused instead, since it is not the cockpit's to empty.

## Phases

| phase | content | needs |
|---|---|---|
| 0 | port + `none`; `ask` reads through it | nothing |
| 1 | `memory`: the opened union kept per process, the file stamp before every answer; `ask` says `index`; SPEC §12.4, SPEC §13 amendment | done (2026-10-20) |
| 2 | `oxigraph` daemon with views; vocabulary queries run on it; `cockpit index status\|start\|stop\|rebuild`; SPEC §12.4 | done (2026-10-20), `-tags unreleased` until kton 0.3.0 |
| 3 | records stored once, attribution per verifying key; incremental by `Records(since)` | done (2026-10-22), kton#232, kton#233 |
