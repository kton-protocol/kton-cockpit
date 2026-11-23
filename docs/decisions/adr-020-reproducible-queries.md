---
title: "Reproducible queries: a query run pins its graphs, is a record, and is reproduced by re-verifying"
type: "decision"
project: "kton-cockpit"
date: 2026-11-21
status: "accepted"
tags: [architecture, query, sparql, provenance, reproducibility, index]
---

# ADR-020: Reproducible queries

## Status

Accepted by MH (2026-11-21), after kton-review's go. Proposed 2026-11-21. Requested by MH through kton-coordination (`d3c9ee44`), with kton-review's input
(`be4a9a82`, `reviews/2026-11-21-reproducible-queries-input.md`). Builds on ADR-012 and ADR-013 (the index and
its query projection), ADR-017 (loaded files as graphs) and ADR-018 (claims in a graph per scope).

## Context

A query today is transient: it asks the index and leaves nothing behind. For audits, review gates and release
gates (kton-review: "every change between tags has a go", re-runnable over the lanes' scopes pinned at their
heads; the examples' `release.rq`, `completeness.rq`), a query must also be reproducible: anyone can run it
again later, from records alone, and get the same rows, even after the live scopes have moved on.

Since ADR-017 a loaded file is a graph of its own, and since ADR-018 a scope is, so the graphs a query reads
are its inputs. What else decides its rows is the vocabulary (templates, aliases, row templates), the trust
(which keys count), the projection's own version and the engine. A reproducible query pins all of it.

Transient queries stay as they are. This is an additional mode.

## Decision

### 1 A query run, and what it reads

`cockpit ask sparql --reproducible` (MCP and serve: `reproducible: true`) runs a query as a **query run**. Its
dataset is chosen as for any query (claims or nanopub, ADR-013), narrowed by `--graph` to named scope seeds and
loaded hashes when the query needs fewer. The run's inputs are **derived from the dataset it is given, never
from the query text** (kton-review 5): an unbound `GRAPH ?g` reads every listed graph, and the default graph
(the union) and the unscoped graph are inputs too.

### 2 What is pinned

Each input is pinned by what names it and cannot change:

- **a scope** by its signed heads: the id of every tip of its chain the run read (kton-review 2; a scope can
  fork, so there may be more than one), so a later claim in the scope is not in the reproduction. The first
  run's dataset is built by the same rule as the reproduction's: the scope's claims are those on chains ending
  at the pinned tips, so a claim deferred or on another branch is out of both alike (kton-review A2);
- **the unscoped claims and the records** (steps, files) by the sorted list of their ids, as a file, with
  the hash of each registry's list beside it for quick comparison;
- **a federation source** in the same way, its records and its scopes' heads;
- **a loaded file** by its hash, its bytes in the content store (`.kton/blobs`); its local description
  (ADR-018 4a) is not an input, since it is unsigned (kton-review 6);
- **the view**: the trust keys that counted (their public keys), the templates and alias files (bytes), the
  admitted packages' revisions, the row templates, the projection's version (`viewVersion`) and the engine's
  image digest (kton-review 3).

The pins go into one **manifest** (canonical JSON) in the content store, with every byte it refers to. The view is
pinned as one blob (its signers' names, templates, alias files and row templates), and the first run is
evaluated with it, as every reproduction is (manifest version 2, part 2; version 1 pinned the templates only
and is refused by `reproduce`, named).

### 3 The run is a record

A query run is a foton (`protocol.kind` `query/sparql`, descriptor `{dataset, viewVersion, engine}`), signed as
any publish:

- inputs: the query text (with its bound parameters), the manifest;
- output: the result, canonical: a SELECT's rows as a sorted multiset of RDF terms in their N-Triples form, an
  ASK's boolean.

The rows are what the first run computed, evaluated over the **pinned dataset built fresh, in a container of
its own** (ADR-013's one-shot path), never the live index: the first run and every reproduction take the same
path, so the index's state cannot make them differ. A query run is refused:

- when it uses `NOW`, `RAND`, `UUID`, `STRUUID` or `BNODE`, unless the value is bound as a parameter (an
  as-of date);
- when its result is truncated (no partial result passes for a whole one);
- when its answer contains blank nodes (no canonical labelling for them here) (kton-review 4);
- when its answer depends on the order of evaluation: `LIMIT` or `OFFSET` without an `ORDER BY` that orders
  every projected variable totally (ties included), `SAMPLE`, and `GROUP_CONCAT` (kton-review A1). Otherwise
  a reproduction would differ and read as tampering.

The refusals are decided on the parsed query, not its text: `NOW()` in a sub-select is refused, the string
`"NOW()"` is not (kton-review A4). Otherwise the comparison is of the multiset of rows.

Loaded files' descriptions (ADR-018 4a) are local and unsigned, so they are not in the reproducible dataset: a
query that finds a loaded graph by its path or label answers empty there, and finds it by hash instead.

### 4 Reproduction re-verifies

`cockpit reproduce <query-run>` reads the manifest and builds the dataset again **from the registries**, not
from stored rows (kton-review 1):

1. reads every pinned record and claim, and checks its signature under the pinned trust keys. That answers
   whether the run is reproducible. Whether its result is still accepted is a separate answer, under the
   reproducer's current trust tiers: a key revoked since the run does not make the reproduction fail, and the
   reproduction says the run rests on a key the reproducer no longer trusts (kton-review A3);
2. truncates each scope at its pinned head;
3. rebuilds the view with the pinned vocabulary, row templates and projection version, in the pinned engine;
4. runs the query and compares the canonical result's hash with the run's output.

A match is published as a reproduction, a foton with the same output hash, signed by whoever reproduced it: the
query run's `↻N` counts independent signers, declared steps excluded (#220). A mismatch is refused with the
first input that differs. A pinned record the registries no longer hold, a pinned projection version or engine
this cockpit does not have, is refused, named. Whatever differs from the reproducer's own present (its
vocabulary, its trust tiers) is said beside a match, not counted against it.

### 5 Relation to the rest

- **publish/run**: a query run is a publish of a new kind, with the same signing, persistence and reproduction
  count as a step.
- **ADR-012/013**: transient queries keep the index; reproducible ones never read it, so the index stays a
  cache that may be rebuilt at any time.
- **ADR-017/018**: loaded graphs and scope graphs are the units of input; their graph names are what `--graph`
  takes.
- **`ask --dataset`**: both datasets are allowed and recorded.

## Consequences

- A release or review gate is a query run that anyone can reproduce from the records, after the scopes moved on.
- A query run costs a fresh dataset build in a container: seconds to minutes on large unions, by design.
- The manifest of a large union is large (every record id); it lives in the content store, deduplicated.

## Tests (before the code lands)

A second run of the same query is another record, since the first run is now a record its dataset holds, with the same result where nothing it reads changed; a claim added to a scope after the run is not in the reproduction,
nor a claim on another branch of a forked scope; a template changed in the repository since the run leaves
the reproduction matching (it uses the pinned bytes) and says the current vocabulary differs; a key revoked
since the run leaves it matching and says it rests on that key; a pinned byte that cannot be found refuses it,
named; `LIMIT` without a total
order, `SAMPLE` and `GROUP_CONCAT` refused; `NOW()` refused, a bound as-of
accepted; a truncated result refused; a loaded file's bytes from the content store; a reproduction by another
key counts in `↻N`, one by a declared step does not.

## Open and decided in review

- `--graph` takes a scope's name or seed; a name is resolved to its seed when the run starts and recorded as
  the seed (as ADR-018). The engine is pinned by digest and never pulled: absent, a reproduction is refused,
  named.
- Whether a reproduction should also be possible over the nanopub dataset's signed form alone.
