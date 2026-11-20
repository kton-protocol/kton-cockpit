---
title: "Derived RDF in the index: one named graph per output hash, claims unchanged"
type: "decision"
project: "kton-cockpit"
date: 2026-11-20
status: "proposed"
tags: [architecture, index, query, oxigraph, sparql, provenance, derived-data]
---

# ADR-017: Derived RDF in the index, one named graph per output hash

## Status

Proposed 2026-11-20 for MH's decision. Amends ADR-012, section 1 ("ingests through the substrate … never the
files"), and adds to ADR-013's query projection. No code before the decision.

## Context

Every fact the index answers from is today a **record**: a foton (who ran what, on which inputs, giving which
outputs, by hash) or a **claim** (a statement a person or agent signed). What a step *produced* is not in the
index. The provenance of a file `https://kton.dev/o/<sha256>` can be queried, its content cannot.

Two kinds of work need that content:

- **Results of analysis steps that are themselves RDF.** One extraction step per published model, each
  writing the model's description as Turtle (a request of 23.10.). Today they live in a separate store, joined
  to the cockpit's provenance on the same hash IRIs. Measured there (ai1, Oxigraph 0.5.13, 4 CPUs): 4,013
  Turtle files, 1.79 million triples, one named graph each, named `https://kton.dev/o/<hash>`. Loaded as one
  N-Quads request in 21.5 s, or graph by graph in about 27 s. Queries across all the graphs take 7–170 ms.
  4,012 of the 4,013 graphs find their producing foton through `prov:wasGeneratedBy` with no added field.
- **Bulk derived data that is not anyone's statement.** A mapping of specification elements to tests: tens of
  thousands of rows, each computed from a source file, none of them a decision. Signing each row as a claim
  costs one `say` per row. That cost grows with the store (about 0.2 s per row at a few thousand rows, about
  23 h for 16,600 rows; kton measured the same, kernel fix #279 in review). It also puts computed rows on the
  same footing as signed decisions.

MH's rule (20.11.): **every fact in the index is either a claim or covered by a hash.**

- **Claims** are what a person or agent states and signs: decisions, rankings, reviews, assignments. They stay
  as they are, in their claim graphs and the claim view (ADR-013).
- **Facts covered by a hash** are everything derived. Each is loaded in bulk from the file it was derived
  into, into **one named graph per file**, and the graph's name is derived from the file's hash. The graph
  name *is* the source; no provenance field is added to the data.

This fits the rest of the system:
- a reader (or a guard) tells the evidence level from the graph: signed claim or content hash;
- plankton has the hash directly, to check, reuse or reproduce;
- the foton that produced the file says who made it and how;
- a new version of the file is a new hash and a new graph, and the old one stays, for comparison and for
  "changed since I last looked".

## Decision

### 1 What is loaded

An output of a foton is loaded when **all** of these hold:

- **The foton is in the view.** It is part of the configuration's union and verified under the configuration's
  trusted keys (ADR-013, section 1). An output of a foton the configuration does not trust is not loaded for
  that configuration. A foton in a federation source counts like any other.
- **The output is RDF.** Its declared `mediaType` is one the index parses (section 7). The media type is a
  carried field, not covered by the foton id (plankton SPEC §6.1). It only chooses the parser. What is loaded
  is decided by the hash, never by the media type: a wrong media type fails to parse, and the failure is
  reported.
- **The bytes are held, and they hash to the output's digest.** The index reads them from, in this order:
  the repository's content store (`.kton/blobs/<hh>/<hex>`, ADR-016; `publish` does not put outputs there
  today, a package or a step that wants its result kept by hash does), then the file at the output's path in
  the working tree, then the same in a federation source's tree. Never from the network. Bytes that are
  missing or hash differently are not loaded and are reported per hash (`derived.missing`,
  `derived.mismatch`). That is ADR-012's "verified once at read-in", applied to content.
- **It is under the limits** (section 6).

### 2 One named graph per hash, named by the hash

- **Graph name: the file's own IRI, `https://kton.dev/o/<hex>`,** in plankton's form (lower-case hex since
  kton v0.4), one graph per distinct content hash. It is the hash of the **exact bytes loaded**, the output
  file, never of a source it was derived from. A step's sources are its foton's inputs; naming by a source would
  put two files derived from one source into one graph. Two fotons that produced the same bytes share one
  graph, and it is loaded once.
- **Why this name:** it is the IRI the foton's record already uses for the file. `GRAPH ?g { … } ?g
  prov:wasGeneratedBy ?foton` joins with no mapping. A foton's *record* graph is in the same namespace
  (`https://kton.dev/o/<foton id>`, kernel `rdf.RecordGraph`), but a query never sees both as named graphs: in
  the query dataset the named graphs are the derived graphs only (section 5). So whether a graph is content or a
  record is read from where it sits, not from its name. An alternative, `urn:kton:derived:sha256:<hex>`, would
  make the kind readable from the name, at the cost of a mapping in every join (open, below).
- **The data is loaded as it is.** No triple is added, none removed. Blank nodes stay local to their graph.
- **What ties a graph to its foton is already in the index:** the foton's record says
  `https://kton.dev/o/<hex> prov:wasGeneratedBy <foton>`. The index adds one catalogue entry per graph,
  per view: `urn:kton:view:<id>:derived` lists the derived graphs the view sees. For each graph it gives the
  file IRI it holds, and the output path(s) and foton(s) it came from. That catalogue is index metadata, like
  the registry graphs of ADR-012. It is not added to the derived data.

### 3 Evidence levels, checked rather than assumed

- **Hash and produced:** the graph's hash is an output of a foton in the view, whose signer the configuration
  trusts. Under section 1, this is every graph the index loads.
- **Hash only:** bytes the index holds whose hash no trusted foton names. The index does not load them. A
  reader of another store, built otherwise, cannot assume every graph has a producer. In the store measured
  above, 1 of 4,013 graphs had none. A guard checks for the producing foton; it does not infer one from the
  name.
- **Claim:** a signed statement, in the claim view (section 4).

### 4 Claims stay apart

- **The claim view never mixes with derived graphs.** The view's default graph (records and claims, ADR-013)
  receives no derived triple. A derived fact is reached only through `GRAPH ?g { … }`, so every answer that
  uses one can return `?g`, which is its hash.
- **A derived fact is never a claim,** and is never counted as one: not by `ask about`, not by trust tiers,
  not by the guard. It is unsigned content, attributed only through the foton that produced its file. A
  viewer labels it as derived, as it labels observations (ADR-012, `urn:kton:observed:`).
- **Signing stays for statements.** Reviewing, deciding, approving a derived fact is a claim, and its subject
  is the derived graph's file hash (or a node inside it).

### 5 How a query sees them

- **Dataset:** the view as the default graph, as today. The **view's derived graphs are the named graphs**.
  A query with no `GRAPH` pattern answers exactly as it does today.
- **How the view's set is enforced, and what it costs:** ADR-013 found that *listing* thousands of graphs per
  request is slow, because the engine evaluates one listed graph at a time. Derived graphs are one per *file*,
  not one per row, but there can still be thousands: 4,013 in the store measured above, where queries over all
  of them with `GRAPH ?g` and no list took 7–170 ms. So the view's derived graphs are not listed per request.
  The query is rewritten to join each `GRAPH ?g` with the view's catalogue (`?g` listed in
  `urn:kton:view:<id>:derived`). Before this is built, that rewrite is measured on the 4,013 graphs against the
  unrestricted query. If it is slow, the fallback is a per-view union graph of derived triples, with the hash
  kept as `nk:inGraph` on each subject. That fallback would be an amendment to this ADR.
- **Ask and poll stay cheap.** A hash is loaded once, at ingest, and never again: the content cannot change
  under a hash. A head check that finds no new fotons does no derived work. A new foton loads only its outputs'
  hashes that are not loaded yet.
- **Answers say what they covered:** `ask sparql` reports how many derived graphs the dataset held, and every
  hash that was missing or mismatched (SPEC §9.9 gains this).

### 6 Limits, and opt-in

- **Off by default** (ADR-009's configuration ceiling): `index.derived.enabled` in `cockpit.config.json`.
- **Per output:** `index.derived.maxBytes`, default 64 MiB. A larger output is reported (`derived.too-large`),
  not loaded.
- **Per index:** `index.derived.maxTriples` per view, default 50 million. Past it, the oldest-ingested hashes
  are left out of the view first, and the answer says so.
- **Per source:** a federation source loads derived graphs only when its entry says `derived: true`.
  Someone else's large result set never swamps this machine by accident.

### 7 Media types

Loaded: `text/turtle`, `application/n-triples`, `application/rdf+xml`. `application/ld+json` is loaded only
with an inline context; a remote `@context` is not fetched, and the output is reported. `application/n-quads`
and `application/trig` are refused (`derived.quads`): their own graph names would compete with the hash as the
graph's name.

**Data that is not RDF** (CSV, JSON rows) is not loaded as such. A converter step projects it to RDF, and that
step is a foton like any other. Its Turtle output is what gets a graph. Every loaded graph then has a producer,
and the conversion itself can be checked and reproduced.

### 8 Older hashes of the same file

**Kept.** A graph stays in the view as long as a foton in the view names its hash as an output. A new version
of a file is a new hash, a new graph, and a new catalogue entry with the same path. "The current version" is a
query over the catalogue: the newest foton that output that path. When a person decides which version holds, that
decision is a claim about the hash. "Changed since" is a comparison of two graphs. Nothing is deleted from the index
except by the size limit (section 6) or by a rebuild.

### 9 Loading

- **Verified at read-in:** the bytes are hashed again right before loading, so a graph's name cannot lie about its
  content.
- **In bulk:** the new graphs of one catch-up go to the store as one N-Quads request, as measured above.
- **Rebuild:** graphs the view no longer names are dropped. The store stays derived: deleting it loses nothing that
  the registries and the bytes held cannot give back.

### 10 What stays

- **ADR-012, section 1** changes in one point: the index now also reads **content bytes**, verified against
  hashes the records name. Everything else holds:
  - the index is derived (rebuildable from the registries and the bytes held), never authoritative, and never
    consulted by the binding guard;
  - it stores no trust tier;
  - it says where an answer came from.
- **The kernel does not change.** The triples live in the index, not in plankton or nekton. Vocabularies such
  as a domain ontology remain a perspective layer, never part of the substrate.
- **Batch claims** remain a separate track, for what really is a claim.

## Consequences

- Bulk derived data is loaded in seconds to tens of seconds for thousands of files (21.5 s for 4,013 files and
  1.79 million triples, measured above), instead of hours at one claim per row, and it can be queried with its
  source visible.
- Content is now part of what the index holds: the cache grows with the derived data, and a rebuild re-reads the
  bytes.
- A step that wants its result queryable must declare an RDF media type on that output, and keep the bytes
  where the index can read them (the content store, or the working tree at the recorded path).
- SPEC: §9.9 (the dataset and the report), §12.4 (what the index reads), and the configuration schema
  (`index.derived`).

## Open, for MH

- **Graph name:** the file's own `https://kton.dev/o/<hex>` (proposed, with ddmanymore: no mapping in any join;
  what they run today) or `urn:kton:derived:sha256:<hex>` (the kind readable from the name alone).
- **Who sees derived graphs:** only through `ask sparql` (proposed), or also the named-query packages
  (`ui list` / `query@1`), so a viewer can show them. The second follows naturally once the first exists.
- **Default limits** (section 6).
