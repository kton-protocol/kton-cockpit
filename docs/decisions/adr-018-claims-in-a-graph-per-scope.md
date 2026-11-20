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
that query the index. Revised the same day after the reviews of qm-dev-provenance, userinterface and kton-web:
a scope's graph is found by its seed, a label is a convenience that may match several graphs, each graph names
its source, loaded graphs get the same kind of metadata, and row claims keep the foton as subject (MH's decision
`06e4084d`, point 3). Builds on ADR-012 (the derived index), ADR-013 (the query projection) and ADR-017 (loaded
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
  A scope's graph does not include its subscopes' claims.
- **one graph for the unscoped claims:** `urn:kton:view:<slot>:unscoped`. `say` without a scope signs a claim
  with no scope (nothing chains it under a root), and the core's `located-at`, `describes`, `rationale` and
  `based-on` are such claims. Their graph is decided by the claim having no signed scope, never by the registry
  or store file holding it, and it is listed with the scope graphs every time: its own unit of visibility later.
- **federated sources alike:** a federation source's scopes get graphs in the view that reads them, as the
  own registry's do. The view is the union; a scope is a scope wherever its seed was signed.
- **the view's union:** `urn:kton:view:<slot>:claims`, as today.

A claim's node, its fields, `nk:scope`, `nk:position`, `nk:prev`, `nk:predicate`, `nk:about` and its signer are
in its scope's graph. A scope's graph holds every claim a trusted key chained into that scope, whoever signed it.
`nk:replacedBy` is in the graph of the replaced claim, also where the replacing claim is in another scope (the
projection does not require one scope for a replacement); in one view that is what today's union shows. Where the
two scopes later have different readers is decided with multi-user (below).

The nanopub dataset is unchanged: four graphs per claim, apart from the views.

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
<urn:kton:view:<slot>:scope:<hex>> a nk:ClaimGraph ;
    nk:scope  <https://kton.dev/o/<hex>> ;   # the seed: exact
    rdfs:label "dryrun1.mapping" ;           # the seed's name, as signed: not unique
    nk:source "own" ;                        # the federation source the seed came from, or "own"
    nk:parent <https://kton.dev/o/<hex>> ;   # the parent scope's seed, when the seed names one
    nk:claims 3967 .                         # how many claims the graph holds

<urn:kton:view:<slot>:unscoped> a nk:ClaimGraph ; nk:unscoped true ; nk:claims 412 .
```

`nk:parent` lets a view over a scope and its subscopes find their graphs in the metadata alone.

**By the seed, exactly.** A seed is `https://kton.dev/o/<hex>`; a scope id `sha256:<hex>` names it. This is the
documented form, and the one a package or a composition uses; a package's scope parameter is bound to the seed
the configuration names (`claims.scopes`), never to a name:

```sparql
SELECT ?claim WHERE {
  ?g a nk:ClaimGraph ; nk:scope $SCOPE .
  GRAPH ?g { ?claim nk:about ?subject }
}
```

**By the name, as a convenience.** A name is not unique: two registries in one union can each have a scope
called `review`, and a versioned scope's seeds carry their own names (`dryrun1.runs`, `dryrun1.runs@v1`, …). A
graph's label is its seed's name exactly as signed, never shortened. `?g rdfs:label "review"` matches every graph
of that name, and `GRAPH ?g` then reads all of them; each row still carries its `?g`, so a query that needs one
binds the seed or adds `nk:source`. The view build counts the names that more than one graph shares; `doctor`
and `ask --json` name them (a warning, not a refusal: two scopes may share a name).

**What a query read.** `ask --json` lists the claim graphs of the dataset it answered over (label, seed, source,
claims), so an answer can be stamped with exactly the graphs it was computed over.

A bound `$SCOPE` is a plain literal (docs/sparql.md); a seed is compared as an IRI, so a query binds it with
`BIND(IRI(?SCOPE) AS ?seed)` or the cockpit binds an IRI where the parameter is declared one. A scope id written
`sha256:<hex>` is converted to its seed `https://kton.dev/o/<hex>` where a parameter is declared a scope;
anything else that is not an IRI there is refused, so a mistyped seed never answers zero rows silently.

### 4 Namespaces

`urn:kton:view:` is the cockpit's. `cockpit load` refuses a graph name in it (it already refuses anything but
`https://kton.dev/o/<hex>`), and nothing loaded is ever named like a claim graph. A reader tells the kinds apart
by name: `urn:kton:view:…:scope:` is claims, `https://kton.dev/o/` is a loaded file.

### 4a Loaded graphs get the same metadata

A new version of a file is a new hash, so a loaded graph found only by its content breaks once two versions are
loaded. Each loaded graph is described in the repository's catalogue. The view is per slot and the catalogue per
repository, so the catalogue is never copied into a view: a claims-dataset query of this repository gets it as
one more default graph, listed per request, and a repository sharing the slot does not see it. This amends
ADR-017 (the catalogue was the cockpit's bookkeeping only):

```turtle
<https://kton.dev/o/<hex>> a nk:LoadedGraph ;
    nk:path "out/sidefx.ttl" ;          # the output path the producing foton records, when one does
    rdfs:label "sidefx" ;               # cockpit load --label, when given
    nk:loadedAt "2026-11-20T12:45:00Z"^^xsd:dateTime .
```

"The version of a data set loaded last" is then a `GROUP BY` over `nk:path` (or the label) and `MAX(nk:loadedAt)`,
as in section 5. That is the version loaded last, not the one produced last: reloading an old file makes it the
latest loaded. "Produced last" orders by the producing step (`prov:wasGeneratedBy` and the step's time) instead.
One file may have several paths (the same bytes written twice); each is a triple. The load list keeps the label
and the time; nothing about a load is signed (ADR-017).

### 5 Latest, and rows

"The latest claim per subject" is written with `GROUP BY` and `MAX(?position)` per subject **and template**
(docs/sparql.md). The key is the predicate: one predicate, one template (SPEC §9.11), so grouping by
`nk:predicate` is grouping by template. `FILTER NOT EXISTS` against every later claim is quadratic and does not
return where many claims share a subject.

Some templates' claims are **rows**, not versions: dry run 1's 11,480 coverage claims are each about one foton
and none replaces another. "Latest per subject" over them keeps one row and hides the rest. A template says so:
`"claims": "rows"`, or `"versions"`, the default (ktonpkg v0.9.12, `scope.TemplateClaims`). The projection marks
each claim of a row template with `nk:row true` on its node, so a "latest" query or package leaves rows out with
one pattern:

```sparql
?c nk:about ?subject ; nk:position ?position .
FILTER NOT EXISTS { ?c nk:row true }
```

**Rows keep the foton as their subject** (MH, 20.11., `06e4084d` point 3): nothing is re-signed. Until a row
template carries the flag, a "latest per subject" query leaves it out by name. In dry run 1 they are
`coverage-of`, `gap-element`, `evidence-*`, `mapping-item`, `mapping-rate`, `rule-case-effect`, `covered-draw`,
`element-key`, `mapping-base`, `run-class-map`.

## Consequences

- No query has to change; a query that wants to be faster names the scope's graph.
- The view's claims are stored twice: about the claims graph's size again, measured at +9% disk in total.
- The rebuild stays atomic: one update writes the union, the scope graphs and their metadata, or none.
- **Tests before the index change:**
  - every query a shipped package carries gives the same rows in both layouts (kton-review R5); outside the
    public repository, the same is run for qm-dev-provenance's 19 named queries of dry run 1 (including the two over
    loaded graphs) before release;
  - a query with `GRAPH ?g` and nothing else binding `?g` now also reads the claim graphs, where before it read
    only loaded graphs: such a query says which kind it wants (`?g a nk:LoadedGraph`); the release notes say so;
  - `bindTerms` reaches a group inside `GRAPH` (VALUES in the outer group joins into it), and `BIND(IRI(?X))` in
    the outer group does too (to be shown by test);
  - two graphs of one label are both read, and `doctor` names the label;
  - two repositories on one slot each see only their own `nk:LoadedGraph` descriptions;
  - a scope parameter given as `sha256:<hex>` reads the seed's graph;
  - a claim of a template with `"claims": "rows"` carries `nk:row true`, one of a template without it does not;
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
