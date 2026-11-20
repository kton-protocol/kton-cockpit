---
title: "Derived RDF in the index: one named graph per file hash, loaded by a cockpit action, claims unchanged"
type: "decision"
project: "kton-cockpit"
date: 2026-11-20
status: "accepted"
tags: [architecture, index, query, oxigraph, sparql, provenance, derived-data]
---

# ADR-017: Derived RDF in the index, one named graph per file hash

## Status

Accepted by MH (2026-11-20, his initiative; decision `7f62db0e55`): graph name the file's own IRI (1a),
derived graphs also through the named-query packages from the start (2b), the proposed limits (3a). Amended the
same day by MH: **loading is a cockpit action, not something a foton declares**, and nothing about a load is
signed; marking a file, if anyone wants to, is a claim (section 1). Proposed 2026-11-20. Amends ADR-012, section 1 ("ingests through the substrate … never the files"), and adds to
ADR-013's query projection.

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

### 1 What is loaded: what a person or a script loads

**Loading is a cockpit action.** `cockpit load <file|sha256> [--as <media type>]` loads one file's RDF into
the index, as a graph named by the file's hash. A person runs it, or a script does, in whatever way it is
automated; nothing has to declare beforehand that a file is RDF. The foton format is not touched: it has no
place for an output's media type (foton/v1 carries path, hash and, beside the record, locators), and the
cockpit signs no statement of its own (SPEC §13).

- **The bytes are verified.** A path is read and hashed; a `sha256:` is read from the repository's content
  store (`.kton/blobs/<hh>/<hex>`, ADR-016), and refused when it is not there. The bytes are hashed again right
  before loading, so a graph's name cannot lie about its content.
- **The media type chooses the parser.** `--as` names it; without it, the file extension does (`.ttl`,
  `.nt`, `.rdf` / `.owl`, `.jsonld`), and a file with none of them is refused. A wrong media type fails to parse
  and loads nothing.
- **Nothing is signed.** A load is not a record and not a claim. The checkout keeps a local list of what it
  loaded (section 9), never shared and never signed, from which the index loads it again after a rebuild. Who
  loaded what is not attested; where the content came from is what its hash says (section 3).
- **RDF/XML with entity declarations is refused.** A DTD's nested entities expand without bound while the
  engine parses; a file with `<!ENTITY` is not read.
- **Marking a file is a claim.** Anyone who wants to say that a file is to be read as RDF, or that a graph is
  the one that counts, says a claim about its hash. A load does not need one, and a claim does not load.
- **Under the limits** (section 6).

### 2 One named graph per hash, named by the hash

- **Graph name: the file's own IRI, `https://kton.dev/o/<hex>`,** in plankton's form (lower-case hex since
  kton v0.4), one graph per distinct content hash. It is the hash of the **exact bytes loaded**, the output
  file, never of a source it was derived from. A step's sources are its foton's inputs; naming by a source would
  put two files derived from one source into one graph. Two fotons that produced the same bytes share one
  graph, and it is loaded once; loading it again changes nothing. Whether it is loaded already is decided
  from the catalogues, never from a graph of that name existing: a foton's record graph is in the same
  namespace (kton-review D4.3).
- **Why this name:** it is the IRI the foton's record already uses for the file. `GRAPH ?g { … } ?g
  prov:wasGeneratedBy ?foton` joins with no mapping. A foton's *record* graph is in the same namespace
  (`https://kton.dev/o/<foton id>`, kernel `rdf.RecordGraph`), but a query never sees both as named graphs: in
  the query dataset the named graphs are the derived graphs only (section 5). So whether a graph is content or a
  record is read from where it sits, not from its name. An alternative, `urn:kton:derived:sha256:<hex>`, would
  make the kind readable from the name, at the cost of a mapping in every join (decided: the file's own IRI).
- **The data is loaded as it is.** No triple is added, none removed. Blank nodes stay local to their graph.
- **What ties a graph to a foton is already in the index:** a foton's record that produced the file says
  `https://kton.dev/o/<hex> prov:wasGeneratedBy <foton>`. The index keeps a catalogue per repository,
  `urn:kton:loaded:<repository>`, listing the graphs loaded for it, each with the path or hash it was loaded
  from, its media type, its triple count and when. `<repository>` is a digest of the repository's resolved
  root on this machine: the index is per machine, and so is the catalogue. That catalogue is index metadata, like the registry graphs of ADR-012. It is
  not added to the derived data.

### 3 Evidence levels, checked rather than assumed

- **Hash and produced:** the graph's hash is an output of a foton in the view, whose signer the configuration
  trusts. Then who made the file, how, and from what is on record.
- **Hash only:** the graph's hash is what the bytes are, and nothing more is known. A loaded file need not have
  a producer; in the store measured above, 1 of 4,013 graphs had none.
- **Claim:** a signed statement, in the claim view (section 4).

A reader or a guard checks for the producing foton (`?g prov:wasGeneratedBy ?f` with `?f` in the view); it
never infers one from the graph being loaded. A file that asserts its own producer (`<its own IRI>
prov:wasGeneratedBy …` among its triples) stays hash only: those triples are in its own graph, never in the
view the check reads.

### 4 Claims stay apart

- **The claim view never mixes with derived graphs.** The view's default graph (records and claims, ADR-013)
  receives no derived triple. A derived fact is reached only through `GRAPH ?g { … }`, so every answer that
  uses one can return `?g`, which is its hash.
- **A derived fact is never a claim,** and is never counted as one: not by `ask about`, not by trust tiers,
  not by the guard. It is unsigned content, attributed only through a foton that produced its file, where there is one. A
  viewer labels it as derived, as it labels observations (ADR-012, `urn:kton:observed:`).
- **Signing stays for statements.** Reviewing, deciding, approving a derived fact is a claim, and its subject
  is the derived graph's file hash (or a node inside it).

### 5 How a query sees them

- **Dataset:** the view as the default graph, as today. The **graphs loaded for the repository are the named
  graphs**.
  A query with no `GRAPH` pattern answers exactly as it does today.
- **How the repository's set is enforced, and what it costs:** the loaded graphs are **listed per request**
  as the dataset's named graphs (`named-graph-uri`). A rewrite that joins `GRAPH ?g` with the catalogue
  instead would need the request to leave the named graphs open, and on Oxigraph 0.5.13 a request that names no
  dataset sees every graph of the store, every other view's included (kton-review D1): confinement would then
  rest on the rewrite alone. Listing costs more at large counts. Measured on 4,013 graphs (ai1, Oxigraph
  0.5.13, median of 5 runs), listing is 2.5–4x slower than the unrestricted query at large views (35.5–43.8 ms
  against 10.5–13.5 ms), and the request grows by about 80 bytes per graph (about 330 KB for 4,013). That is
  milliseconds, and the dry runs load tens of files. If it is ever slow, the fallback is a per-repository union
  graph of loaded triples with the hash kept as `nk:inGraph` on each subject, as an amendment to this ADR.
- **Ask and poll stay cheap.** A hash is loaded once, when someone loads it, and never again: the content cannot
  change under a hash. A query does no derived work beyond reading the catalogue, and the load list when the
  index was rebuilt (section 9).
- **Answers say what they covered:** `ask sparql` reports how many loaded graphs the dataset held, and how many
  on the load list could not be loaded again (SPEC §9.9).
- **Only the caller's catalogue counts,** also when another repository on this machine is the caller's
  federation source: a source's loads are its own.

### 6 Limits, and opt-in

- **Off by default** (ADR-009's configuration ceiling): `index.derived.enabled` in `cockpit.config.json`.
- **Per file:** `index.derived.maxBytes`, default 64 MiB. A larger file is refused (`derived.too-large`).
- **Per repository:** `index.derived.maxTriples`, default 50 million. A load past it is refused, and says so.
- A federation source's loads are its own machine's; nothing loaded elsewhere arrives here.

### 7 Media types

Loaded: `text/turtle`, `application/n-triples`, `application/rdf+xml`. `application/ld+json` is loaded only
with an inline context; a remote `@context` is not fetched, and the load is refused. `application/n-quads`
and `application/trig` are refused (`derived.quads`): their own graph names would compete with the hash as the
graph's name.

**Data that is not RDF** (CSV, JSON rows) is not loaded as such. A converter step projects it to RDF. Published
as a foton, its Turtle output is a graph that has a producer, and the conversion can be checked and reproduced.

### 8 Older hashes of the same file

**Kept until dropped.** A new version of a file is a new hash and a new graph, loaded beside the old one, for
comparison and for "changed since I last looked". "The current version" is a query: the catalogue says when
each was loaded and from which path, and a foton, where there is one, says which run produced which. When a
person decides which version holds, that decision is a claim about the hash. `cockpit load --drop <sha256>`
removes a graph from the repository's catalogue, and from the store only when no other repository's catalogue
lists it (kton-review D4.3).

### 9 Loading, and the load list

- **One request per file:** each file is a graph store `PUT` of its own. Several files in one N-Quads request
  would merge their blank nodes, which are labelled per file (kton-review D2).
- **The checkout keeps a load list** (`.kton/local/loaded.json`: each hash, the path or hash it was loaded from,
  and its media type), local and never shared, like the identity beside it. It is not a record and is not
  signed. *(This replaces "a rebuild drops what was loaded", kton-review D3: rebuilds also happen without anyone
  choosing them, on an image or schema change or a cleared cache, and an index whose deletion changes an answer
  breaks SPEC §13.)*
- **After a rebuild the index loads again from the list:** when a query finds a hash of the list missing from
  the catalogue, it reads the file again, checks it against the hash and loads it. A file that is gone or
  changed is not loaded and is counted in the answer (section 5); `cockpit load --list` names it.
- `--drop` takes a hash off the list and out of the catalogue.

### 10 What stays

- **ADR-012, section 1** changes in one point: the index now also holds **content a person loaded**, verified
  against its hash. Everything else holds:
  - the index is derived (rebuildable from the registries and, for what was loaded, from the checkout's load list
    and the files it names), never authoritative, and never consulted by the binding guard;
  - it stores no trust tier;
  - it says where an answer came from.
- **SPEC §13** holds: deleting the index changes no answer, as long as the files on the load list are where it
  says; a file that is not is reported, not silently missing.
- **`index.kind` none and memory** have no store to load into: `cockpit load` is refused there, and a query
  there has no named graphs.
- **The kernel does not change.** The triples live in the index, not in plankton or nekton. Vocabularies such
  as a domain ontology remain a perspective layer, never part of the substrate.
- **Batch claims** remain a separate track, for what really is a claim.

## Consequences

- Bulk derived data is loaded in seconds to tens of seconds for thousands of files (21.5 s for 4,013 files and
  1.79 million triples, measured above), instead of hours at one claim per row, and it can be queried with its
  source visible.
- Content is now part of what the index holds: the cache grows with the derived data, and a rebuild re-reads the
  bytes.
- Loading is a step someone takes: a script that wants its result queryable runs `cockpit load` on it once; the
  checkout remembers it, and the index loads it again after a rebuild as long as the file is where it was.
- SPEC: §9.9 (the dataset), §12.4 (what the index holds), the `load` command, and the configuration schema
  (`index.derived`).

## Decided (MH, 2026-11-20)

- **Graph name:** the file's own IRI, `https://kton.dev/o/<hex>`.
- **Who sees derived graphs:** `ask sparql` and the named-query packages (`query@1`, `ui list`) alike, from
  the first release: a package's named query may use `GRAPH ?g { … }` and is answered over the repository's loaded
  graphs as an inline query is.
- **Limits:** 64 MiB per file, 50 million triples per repository, off by default (`index.derived.enabled`).
- **Loading is a cockpit action, nothing is signed** (amended the same day): `cockpit load`, automated however
  one likes; marking a file, if wanted, is a claim. After kton-review's reading, the checkout keeps an unsigned
  local load list the index reloads from after a rebuild, rather than losing the loads (section 9, D3).
