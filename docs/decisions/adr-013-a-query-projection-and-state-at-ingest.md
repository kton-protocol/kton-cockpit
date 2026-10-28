---
title: "A query projection per configuration, with state kept at ingest"
type: "decision"
project: "kton-cockpit"
date: 2026-10-27
status: "proposed"
tags: [architecture, index, query, oxigraph, sparql, performance, rules, identity]
---

# ADR-013: A query projection per configuration, with state kept at ingest

## Status

Proposed (2026-10-27). Amends ADR-012 (the derived index). It follows the design kton-coordination agreed
with MH on 2026-10-27 (`kton-coordination/proposals/2026-10-27-index-state-and-rules.md`). That design
answers mignon's handoff rows 2.3, 2.4, 2.5, 2.8 and 2.12 (`kton-mignon/docs/cockpit-handoff.md`).
Review: `longrunning/kton-review`.

## Context

ADR-012 phase 3 stores each record once, in the kernel's own graphs: a foton's IRI, and a claim's four
nanopublication graphs plus one attribution graph per key that verifies it. Trust is chosen per query by
listing graphs in the request. That shape is right for verification and export. As a shape to query it
breaks, and it breaks where the work is meant to grow:

- **Listing graphs per request.** The engine evaluates a listed graph one at a time, for every row that
  joins into it. On explore (24 registries, 1,638 fotons, 2,938 claims) cockpit-runtime's two tour queries
  take 0.05–0.08 s over the whole store and 7–19 s over the same 14,690 claim graphs listed. Merging the
  3,276 record graphs into one view graph (PR #48) took a records-and-claims join from over 50 s to 1.3 s.
  The claim graphs cannot be merged the same way, because their names are data. mignon measured the same
  cost at 100k claims: an unchanged `COUNT` takes 5–49 s.
- **State computed by walking chains.** "Position in a scope's chain" and "current" (not superseded) are
  computed in SPARQL with `(nk:prev/^prov:wasDerivedFrom)+`. That costs about L² on a chain of length L.
  Oxigraph crashes at about 10k claims (a 5.6k chain), and `backfill.rq` takes 245 s at 100k. The QMS views
  (`qm-packages`, `views/_current.md`) work only because their data is small.
- **Fields without the template's vocabulary.** Fields land as `nk:lab/<field>` on a helper node. Whether a
  value is an IRI, a claim, a time or text is guessed. The kernel can apply aliases (`kton-rdf --aliases`),
  but the cockpit does not pass them.
- **Signers as key ids.** An answer says `https://kton.dev/agent/<keyid>`, never who that is.

## Decision

### 1 Two projections

- **The kernel's projection stays as stored** (ADR-012 phase 3): records once, attribution per key, the
  nanopublication graphs. It remains the shape for verification, export, and anyone who wants the signed
  form.
- **A query projection is derived from it, per configuration.** A configuration is the registries of a
  union plus a set of trusted keys (the "view" of PR #48). Only what that key set may see goes in. A
  configuration that trusts other keys reads another view. A tier is still never stored as a fact about a
  record (ADR-012, section 1).
- **The kernel does not change.** Nanopub stays the signed and published form. Aliases and types come from
  the template, which is content with a hash that every claim names.

### 2 The dataset is the index's, never the request's (2.12)

- A query against the query projection names **one view**: its graphs (`urn:kton:view:<id>` with the
  records, `…:claims` with the claim nodes), always as the default graph, and no named graphs. It never
  names graphs one by one. Confinement rests on the view holding only what its key set may see, not on
  the request listing the right graphs.
- The view's graph IRIs carry the view's id, so two views never write into the same graph.
- `SERVICE` and `FROM` stay refused (ADR-012, `index.CheckQuery`).

### 3 The query projection's shape (2.3, 2.8)

**Records.** These are the kernel's projection of the fotons the key set attributes, in the view's
default graph, as PR #48 builds it.

**Claims: the claim is a node.** Each claim is `<https://kton.dev/o/<hex>>`, the same IRI as its id
anywhere else, in the view's default graph. A scope is a triple (`nk:scope <seed>`), not a graph. The
proposal had one graph per scope. Those graph names would be the view's own (`urn:kton:view:<id>/…`),
so an answer binding `?g` would differ between the daemon and a container started for one query, and
between two views. One default graph names nothing, and a scope restricts through an indexed join. On
the node:

| triple | from |
|---|---|
| `a nk:Claim`, `nk:template "<name>"` | the claim |
| `nk:about <subject>` | the claim's subject, an IRI |
| one triple per field | the template's term and type, or `nk:lab/<field>` with a literal when the template declares none |
| `nk:signer <https://kton.dev/agent/<keyid>>` | the trusted key that verifies it |
| `dct:created` | the signed time |
| `nk:scope`, `nk:prev`, `nk:position`, `nk:current`, `nk:supersededBy` | kept at ingest (section 4) |

**The template declares its vocabulary (2.3).**
- A template MAY give each field a term (an IRI) and a type: `text`, `iri`, `claim`, `claims`,
  `dateTime`, `number`, `json`.
- It uses the kernel's alias format, so the same map serves `kton-rdf --aliases`.
- Without a declaration, a field stays a literal under `nk:lab/<field>`. Nothing is guessed.

**Signers have names (2.8).**
- `<https://kton.dev/agent/<keyid>> rdfs:label "<name>"; nk:tier "<tier>"`.
- The name and tier come from the trust configuration (the key file's name and its tier). Once key claims
  exist (section 5), the name a trusted key claim gives takes precedence.

**Answers.** SPARQL 1.1 Query Results JSON is available on request (PR #53).

### 4 Position and "current" are kept at ingest, never walked (2.4)

- **Position:** a claim's position is its predecessor's (`nk:prev`) plus one, within its scope's chain. A
  scope's seed is at 0.
- **Current:** a claim is current unless a later claim by a trusted key, in the same scope, has the same
  subject and predicate. Any trusted key supersedes any other, whatever its tier, and a superseded
  claim names the claim that is current (`nk:supersededBy`), so a rule sees whose claim hid whose.
- **"Later" is one total order**, so the same quads give the same answer however they arrive: a claim
  with a position beats one without (its predecessor is outside the union), then the higher
  position, then the later signed time compared as an instant, then the id. Where a scope branched
  (two claims after the same one) and outside any scope, the time the signer stated decides: a
  trusted key that states a far-future time stays current until a rule says otherwise.
- **Both are computed in Go when the view is built**, linear in the number of claims, and written as
  triples. A view and every rule then use plain joins. No query walks `prev+`, and "sealed up to here"
  becomes a comparison of two positions.
- **`current` depends on the key set; position does not.** A position counts every claim of the
  union's chain, trusted or not. A claim by an untrusted key is not in the view and supersedes
  nothing. So `current` belongs to the view, not to the record.
- **The view is rebuilt whenever its dataset key changes** (a registry moved, or the keys changed), as
  the default graph is today. Rebuilding only the changed scopes comes later. A full rebuild remains the
  fallback, and the check for an incremental one.

### 5 Keys, identities and mandates are signed claims (2.5)

- **Who a key is, and on whose behalf it acts, are claims.** The QMS does this with `qm.schluessel`. They
  are not local configuration. Readers can check them, and a later claim dates or revokes them.
- **Template constraints from kton** (2026-10-27): the subject is `https://kton.dev/agent/<keyid>`, so it
  joins the attribution directly. The principal is an `http(s)` IRI, mapped to `prov:actedOnBehalfOf` in
  the template's terms. Permissions and revocation are rule checks.
- **No kernel change.** The template comes from kton.
- **In the query projection:** a trusted key claim names its agent (section 3), and a mandate becomes
  `prov:actedOnBehalfOf`. An answer can then say "agent for Michael".
- **Who may name whom.** A key claim signed by a key proves possession of that key, nothing more. A
  name is taken only from a claim about the key that signed it, or from one a registrar rule
  accepts, and it is applied when the view is built. Otherwise key B could label key A "Michael".
- **Later:** a personal keyring outside the repository, a request naming who it acts as, and rules per key
  or tier.

### 6 Rules are rule files over the query projection (2.4, 2.5)

- **Views are SPARQL `CONSTRUCT`, and rules are SHACL with `sh:sparql`**, as in the QMS: one rule file
  each, with fixtures and expected findings. ktonpkg is how rule packages travel, not a second format.
- **One engine, two moments:** before signing, a rule gates the claim `say` is about to write; after
  signing, findings are computed over the state.
- **Rules read the query projection, never the chain.** A derived graph names its rule by hash. A rule
  change rebuilds it.

### 7 What does not change

- The registries stay the only truth, and the index is never authoritative (ADR-012, section 1).
- The binding is re-checked on every call (SPEC §5).
- Ingest goes through the substrate (SPEC §12.3).
- The kernel's projection, attribution and nanopublication graphs are kept as they are.

## `ask sparql` and the two projections

- **The query projection becomes the default dataset of `ask sparql`.** That answer was introduced in
  v0.4.0, which is not released yet, so nothing published breaks.
- **`dataset: "nanopub"` asks the kernel's projection**, exactly as before: for queries about the signed
  form, and for comparison.
- `docs/sparql.md` documents both. Each example is still run by a test on the container path and the index
  path.

## Consequences

- **Each configuration holds its own copy of its claims**, as phase 2 views did. That is a duplication
  bounded by the number of configurations on one machine (one person's machine: a handful). It buys
  datasets that do not grow with the work. ADR-012's "one record once" stays true for the kernel's
  projection.
- A view build is linear in the claims of its union. At explore's size that takes about seconds, once per
  change. mignon's 100k case needs the incremental rebuild of section 4 to be interactive after each
  write.
- Queries written against the nanopublication shape keep working under `dataset: "nanopub"`. Queries for
  the tour, mignon and the QMS views move to the query projection to become fast.

## As built (2026-10-27)

- `oxigraph.ClaimView` is one function of the kernel's quads, used by the daemon (it fetches the
  trusted claims' graphs by name: 14,690 graphs, 55,556 quads on explore in 1.5 s) and by a container
  started for one query (from the kernel's projection in Go). So the two answer alike
  (`TestSparql_TheExamplesAnswerOverAFixture`, every example on both paths).
- The view is built when its dataset key changes (positions, trusted keys, or the configuration's
  signer names and templates), under a lock per view. The records, the claims and the mark change
  in one update, so a reader sees the old view or the new one, never a view without its claims
  (`TestOxigraph_AReaderNeverSeesAViewWithoutItsClaims`). A builder that waited for the lock finds
  the view already made under its key. When the incremental rebuild comes, it writes into graphs
  named by key and then switches the mark, rather than dropping graphs in place.
- An alias that resolves into the projection's own terms (`nk:signer`, `nk:current`, …) is refused.
- On explore, cockpit-runtime's two tour queries ("final/base models with their signer", "the review
  cycle with dates") take 60–70 ms against 7–19 s over the nanopublication graphs.

## What goes into v0.4.0

MH decided on 2026-10-27 that everything from the handoff that can be done goes into v0.4.0.

| part | v0.4.0 | later |
|---|---|---|
| dataset per view (2.12) | records (PR #48) and claim nodes in the view's default graph | incremental per scope |
| claim as node, signer, scope, prev | yes | |
| position, current (2.4) | yes, at view build | incremental |
| template terms and types (2.3) | yes, where a template declares them | |
| signer names (2.8) | from the trust configuration | from key claims |
| standard results (2.8) | yes (PR #53) | |
| key and mandate claims (2.5) | | template from kton, then names and mandates |
| rule runner (2.4/2.5) | | gate and findings |
