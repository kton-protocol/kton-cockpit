---
title: "Claims in a named graph per scope, beside the view's union of them"
type: "decision"
project: "kton-cockpit"
date: 2026-11-20
status: "proposed"
tags: [architecture, index, query, oxigraph, sparql, scopes, visibility]
---

# ADR-018: Claims in a named graph per scope

## Status

Proposed 2026-11-20. MH chose option B of the evaluation (2026-11-20): a graph per (view, scope), with the
view's union kept as the default graph. This ADR writes that choice down for review by kton-review and the lanes
that query the index. Builds on ADR-012 (the derived index), ADR-013 (the query projection) and ADR-017 (loaded
files in a graph per hash).

## Context

ADR-017 puts each loaded file in a named graph named by its hash. Claims are all in one graph per view,
`urn:kton:view:<slot>:claims`, which a claims-dataset query gets as part of its default graph, with no named
graphs (ADR-013). A scope is found by a pattern: `?c nk:scope ?seed . ?seed rdfs:label "…"`.

The evaluation (input from every lane that queries the index; measured on synthetic data up to 100,000 claims
and on dry run 1's 5,320 real claims) found:

- **A query that names the graphs it needs is much faster.** One scope: 20–40× faster at 11k–100k claims. A
  two-scope join: 58× at 100k; on dry run 1, 2,095 ms to 4 ms. Latest claim in a scope: from more than ten
  minutes to 35 s at 100k.
- **A union of many listed default graphs is slow.** All claims, with 30 scope graphs listed as the default
  graph: 12.7 s against 0.7 s for one claims graph at 100k. So the union cannot be built per request from scope
  graphs; it has to be stored.
- **Storage and load barely move:** +9% disk, the same load time.
- **Every lane's first requirement:** a query that names no graph keeps seeing every claim and every step, or
  the change breaks silently with zero rows.

MH's goal behind it: later, a cockpit used by several people, where a user sees only the graphs they may see,
and **what is not visible does not exist**. That is not built now; this layout must not stand in its way.

## Decision

### 1 A graph per scope, and the union

Each view stores its claims twice, in one atomic build (the view is written in one update, as today):

- **one graph per scope:** `urn:kton:view:<slot>:scope:<hex>`, where `<hex>` is the scope's seed id. A claim is
  in the graph of the scope its signed chain puts it in (`nk:scope`), never by where its store file lies.
  Unscoped claims are in the root scope's graph (kton v0.4 chains them there). A scope's graph does not include
  its subscopes' claims.
- **the view's union:** `urn:kton:view:<slot>:claims`, as today.

A claim's node, its fields, `nk:scope`, `nk:position`, `nk:prev`, `nk:predicate`, `nk:about` and its signer are
in its scope's graph. `nk:replacedBy` is in the graph of the replaced claim.

### 2 What a query gets

- **Default graph:** the view's records graph and its union of claims, as today. Every existing query keeps its
  rows and its speed.
- **Named graphs:** the view's scope graphs and the repository's loaded graphs (ADR-017), listed per request.
  `GRAPH <g> { … }` and `GRAPH ?g { … }` work over exactly these.
- Never the store's own default graph: a request that names no dataset sees every graph in the store, and one
  store holds every configuration's view.

### 3 Finding a scope's graph

A graph's IRI carries the view's slot, so a query does not derive it; it finds it by the metadata in the default
graph:

```turtle
<urn:kton:view:<slot>:scope:<hex>> a nk:ClaimGraph ; nk:scope <seed> ; rdfs:label "dryrun1.mapping" .
```

```sparql
SELECT ?claim WHERE {
  ?g a nk:ClaimGraph ; rdfs:label "dryrun1.mapping" .
  GRAPH ?g { ?claim nk:about ?subject }
}
```

A bound `$SCOPE` (a scope's name) reaches the label through the same binding as every other term.

### 4 Namespaces

`urn:kton:view:` is the cockpit's. `cockpit load` refuses a graph name in it (it already refuses anything but
`https://kton.dev/o/<hex>`), and nothing loaded is ever named like a claim graph. A reader tells the kinds apart
by name: `urn:kton:view:…:scope:` is claims, `https://kton.dev/o/` is a loaded file.

### 5 Latest, and rows

"The latest claim per subject" is written with `GROUP BY` and `MAX(?position)` per subject **and template**
(docs/sparql.md). `FILTER NOT EXISTS` against every later claim is quadratic and does not return where many claims
share a subject.

Some templates' claims are **rows**, not versions: dry run 1's 11,480 coverage claims are each about one foton
and none replaces another. "Latest per subject" over them keeps one row and hides the rest. Such a template says
so (a flag on the template, agreed with the template format's owners), and a query or package that picks
"latest" leaves row templates out. Until the flag exists, a row's subject is its own key (the element's IRI),
not the shared foton.

## Consequences

- No query has to change; a query that wants to be faster names the scope's graph.
- The view's claims are stored twice: about the claims graph's size again, measured at +9% disk in total.
- The rebuild stays atomic: one update writes the union, the scope graphs and their metadata, or none.
- **Tests before the index change:**
  - every query a shipped package carries gives the same rows in both layouts (kton-review R5);
  - `bindTerms` reaches a group inside `GRAPH` (VALUES in the outer group joins into it; to be shown by test);
  - a claim is in its signed scope's graph whatever store file holds it;
  - `cockpit load` refuses a `urn:kton:view:` name.
- **Measured on real data** with `scope_graphs_bench.py` on dry run 1 before release.

## Multi-user later (not built now)

A user's visible set is a set of scope graphs, loaded graphs and registries. The cockpit then builds that set's
union like a view's (a slot per visible set, so no stored graph mixes visibilities: kton-review C4), lists the
set's graphs, and a query sees exactly them, given that:

- every request carries a dataset (C1);
- `SERVICE` stays refused and the store's own endpoint unreachable (C2);
- the list comes from outside, never from the data (C3).

What is not visible does not exist: no answer marks or counts what was withheld, and a refusal does not tell "not
allowed" from "does not exist". Decided then, not now:

- whether steps and fotons are visible per publishing scope or per registry;
- where `nk:replacedBy` lives when a replacement crosses scopes (in qms data it never does);
- who sees a loaded file's graph;
- the paths that do not go through SPARQL (`ask about/producer/lineage/feed/scope`, `show`, `ui`), which need the
  same rule.

The leaks the layout cannot close: text copied into claim fields, hashes that confirm a file's existence to
whoever holds it, and aggregates signed in one scope over data in another.

## Alternatives

- **A: claims stay in one graph.** Nothing breaks, but per-user visibility would need filtering inside queries,
  and no query can name the scope it needs.
- **C: one store per configuration,** so a scope graph is named `https://kton.dev/scope/<hex>` in every
  configuration. Worth it when permissions are built (one rule per scope instead of one per view); it changes
  ADR-012 and costs memory and disk per configuration.
- **D: rewrite `GRAPH <scope>` per request.** Rejected: rewriting arbitrary SPARQL is where the binding bugs came
  from, and a missed rewrite reads another view's graph.
