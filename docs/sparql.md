# Asking in SPARQL

`ask sparql` (SPEC §9.9) answers a SELECT or an ASK you wrote, over the dataset the vocabulary
queries read:

```sh
cockpit ask sparql query.rq            # or - for stdin
cockpit ask '{"query":"sparql","sparql":"SELECT …","limit":100}'
```

Over MCP, `cockpit_ask` takes the same `{"query":"sparql","sparql":…}`, and `"dataset":"nanopub"`
for the kernel's projection. The answer is
`{"form","dataset","vars","rows","boolean","truncated","limit","via"}`. Each row binds the variables by name, and
each value is in its lexical form.

For the types, ask for the standard format: `"format":"sparql-results+json"` adds `results`, the same
answer as a SPARQL 1.1 Query Results JSON document, where every value keeps its type (`uri`, `literal`,
`bnode`), datatype and language. `cockpit ask sparql query.rq --results-json` prints that document
alone, for a tool that reads it.

It needs a SPARQL engine: `query.image`, or `index.kind: oxigraph`. The projection below is kton's
K8, released in kton 0.3.0.

## What is in the dataset

There are two datasets, both chosen for your trust tiers and kept by the index until a registry or
your trust moves (ADR-013):

- **`claims`, the default: the query projection.** The default graph holds every step (foton) of
  the union with its attribution to the keys your tiers trust, and **every claim a key you trust
  signed, as one node**, with its subjects, its fields, its signer, its scope, its position in the
  scope's chain and what it replaces. A query that names no graph reads all of it. Each scope's
  claims are also in a named graph of their own, and the claims with no scope in one more
  (ADR-018); the default graph describes each (`?g a nk:ClaimGraph ; nk:scope <seed> ;
  rdfs:label "name" ; nk:source "own" ; nk:parent <seed> ; nk:claims 12`, or `nk:unscoped true`).
  Naming the scope's graph (`GRAPH ?g`) reads only it, which on large data is many times faster.
  The files `cockpit load` loaded for this repository are named graphs too, each described as
  `?g a nk:LoadedGraph ; nk:path "out/x.ttl" ; nk:loadedAs "label" ; nk:loadedAt …` (a path
  only from a step a key you trust signed). A
  `GRAPH ?g` whose `?g` nothing else binds reads both kinds: say which (`?g a nk:ClaimGraph`).
  `ask --json` lists the claim graphs an answer read (`claimGraphs`) and the scope names more
  than one of them carries (`repeatedLabels`).
- **`nanopub`: the kernel's projection.** The same steps, and each trusted claim as a
  nanopublication of four named graphs (`"dataset":"nanopub"`, or `# dataset: nanopub` as the
  query's first line). For queries about the signed form.

A step **nobody you trust signed is there too, but unattributed**: a query that wants trusted steps
asks for `prov:wasAttributedTo`. A claim nobody you trust signed is in neither dataset.

A query names no dataset of its own (`FROM` is refused) and calls no endpoint (`SERVICE` is refused).
Only SELECT and ASK are answered.

| Prefix | IRI |
|---|---|
| `prov:` | `http://www.w3.org/ns/prov#` |
| `rdfs:` | `http://www.w3.org/2000/01/rdf-schema#` |
| `dct:` | `http://purl.org/dc/terms/` |
| `ep:` | `https://w3id.org/ep-plan#` |
| `np:` | `http://www.nanopub.org/nschema#` |
| `nk:` | `https://kton.dev/v/` |
| `xsd:` | `http://www.w3.org/2001/XMLSchema#` |

**Content.** A file or a record with the id `sha256:<hex>` is the node `<https://kton.dev/o/<hex>>`.
A signer is `<https://kton.dev/agent/<keyid>>`, where the keyid is the 16 hex characters that
`cockpit doctor` and `ask` print.

**A step** (default graph):

```
?step  a prov:Activity ;
       prov:used ?input ;                  # each input file
       nk:protocolKind "script" ;
       nk:protocolRef ?protocol ;          # the content hash of what ran, not its text
       nk:declared "hand" ;                # only a step declared afterwards: "hand" or "heuristic:<name>"
       prov:wasAttributedTo ?signer .      # only where a key you trust verified it
?output prov:wasGeneratedBy ?step ;
        prov:wasDerivedFrom ?input .
?file  a prov:Entity ; rdfs:label "data/in.csv" .   # the path the step named it by
```

A step carries no time and no command text. A step without `nk:declared` was recorded as it ran
(`FILTER NOT EXISTS { ?step nk:declared ?d }` keeps only those).

**A claim** with the claim id `sha256:<hex>` is the node `<https://kton.dev/o/<hex>>` (dataset
`claims`):

```
?claim a nk:Claim ;
       nk:about ?subject ;                 # what it is about: a step, a file, any IRI; one or more
       nk:predicate ?predicate ;           # the template's predicate
       nk:template "describes" ;           # where your templates name it
       <https://kton.dev/v/lab/value> "…" ;  # each field: nk:lab/<field>, or the term your aliases give it
       dct:created ?when ;                 # xsd:dateTime, signed
       nk:signer ?agent ;                  # every key you trust that verifies it
       nk:scope ?seed ; nk:prev ?previous ;  # when it is chained in a scope
       nk:position 3 ;                     # its place in the scope's chain; the seed is 0
       nk:replaces ?earlier ;              # each claim its replaces field names
       nk:replacedBy ?later .              # a later claim by the same signer that retires it (below)
?agent rdfs:label "alice" ; nk:tier "peers" .   # from your trust configuration
```

`describes` is `dct:description`, `rationale` is `ep:hasRationale`, and both have the field
`nk:lab/value`. Other templates are under their own predicate (`cockpit ask` lists them). A field
your aliases name (`aliases.json`, kton's alias format) is under that term; a field a template types
`date` is an `xsd:date` or `xsd:dateTime`, and one it types `integer` is an `xsd:integer`. An `enum`
value written as an IRI, or as a CURIE your aliases resolve (`qms:Effective`), is that IRI; a bare
value such as `approve` stays a literal. A field typed `iri` is an IRI when it is one (`https://…`,
`urn:`, `tag:`, `did:`, `mailto:`, or a CURIE your aliases resolve). A value that does not read as its type stays as signed. A field that names a file or a step (a `ref`, such as
`parent-step`) points at its node `<https://kton.dev/o/<hex>>`, so it joins the steps directly.

**Positions are kept, not walked.** The index computes them once per change, so a query that wants
order asks `nk:position`, without `nk:prev+`. Positions count every claim of the chain, including
ones you do not trust. Nothing else is inferred from order (view vocabulary 1, SPEC §9.11): a later
claim does not make an earlier one anything unless it says so.

**A claim says what it replaces.** A template with `"replace": "previous"` has the signing tool fill
the reserved field `replaces` with the claim it replaces. Every name in it is `nk:replaces`, whoever
signed either claim. `nk:replacedBy` is only where every key that verifies the replaced claim also
verifies the replacing one, with the same template and a subject in common (ADR-013 A5): one of two
co-signers cannot withdraw the other's signature. What a name by another
signer means is your rule's decision, so a query that wants what still stands asks
`FILTER NOT EXISTS { ?claim nk:replacedBy ?any }`, and a rule that also accepts another key's
replacement asks `nk:replaces` with that key. An alias may not name a term the projection writes
itself (`nk:signer`, `nk:replacedBy`, …): the query is refused.

**A scope**'s seed is a claim node with `nk:genesis true`, `rdfs:label "<name>"` and `nk:scope` its own
node. `nk:parent` is the parent scope's seed. Since kton v0.4.0 every store has a root scope, and a scope
seeded without a named parent has the root as its parent.

**In the `nanopub` dataset** a claim is the nanopublication `<https://kton.dev/np/<hex>.>`:

```
GRAPH ?head      { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
GRAPH ?assertion { ?subject ?predicate ?o . ?o ?field ?value }
GRAPH ?pubinfo   { ?np dct:created ?when ; prov:wasDerivedFrom ?claim ; nk:scope ?scope ; nk:prev ?previous }
GRAPH ?attribution { ?np dct:creator ?signer . ?assertion prov:wasAttributedTo ?signer }
```

## A query from a vocabulary package

A query can come from an installed package instead of a file: `cockpit install <package-dir> --allow`
admits a package's templates and queries, and `cockpit ask sparql --name <query>` runs one of them
over the same dataset as a query written out (MCP: `name`). The answer names the package it came from.

`--bind VAR=VALUE` (MCP: `bind`) gives a variable a value. It binds `?VAR` and `$VAR` alike, as a plain
literal in a `VALUES` clause put first into the query's `WHERE` group, so a value is never read as
SPARQL, and every pattern and `FILTER` of that group sees it:

```sh
cockpit ask sparql --name inbox-open --bind LANE=kton-cockpit
cockpit ask sparql --name overdue --bind asOf=2026-11-18    # FILTER (?due < xsd:date(?asOf))
```

A bound value is a plain literal: cast it where the query compares it with a typed one, as above, and make
an IRI of it where the query compares it with one (`--bind STEP=https://kton.dev/o/<hex>`):

```text
SELECT ?claim WHERE {
  BIND(IRI(?STEP) AS ?step)
  ?claim nk:about ?step .
}
```

Or bind an IRI directly: `--bind-iri STEP=https://kton.dev/o/<hex>`. A kton id is taken as the node it
names, so `--bind-iri SCOPE=sha256:<hex>` binds a scope's seed `https://kton.dev/o/<hex>`; a `sha256:` value
that is not 64 hex digits is refused rather than bound as an IRI that matches nothing.

A rule with an as-of date takes it as a parameter rather than `NOW()`, so a recorded run answers the same
again. The binding is put first into the `WHERE` group and into every sub-select's group; a `FILTER` inside a
nested `{ … }` group that is not a sub-select does not see it.

## Examples

Each example runs in the test `TestSparql_TheExamplesAnswerOverAFixture` against a fixture with
two signers, `describes`/`rationale` claims and a scope. Replace `FILE`, `STEP` and `KEYID` with
your own.

### Every step that used a file, and who signed it

```sparql
PREFIX prov: <http://www.w3.org/ns/prov#>
SELECT ?step ?signer WHERE {
  ?step prov:used <https://kton.dev/o/FILE> .
  OPTIONAL { ?step prov:wasAttributedTo ?signer }
}
```

A step with no `?signer` is in a registry you read, but no key you trust signed it.

### The descriptions and rationales of a step

```sparql
PREFIX dct: <http://purl.org/dc/terms/>
PREFIX ep:  <https://w3id.org/ep-plan#>
PREFIX nk:  <https://kton.dev/v/>
SELECT ?kind ?text ?signer ?when WHERE {
  VALUES (?predicate ?kind) { (dct:description "description") (ep:hasRationale "rationale") }
  ?claim nk:about <https://kton.dev/o/STEP> ; nk:predicate ?predicate ;
         <https://kton.dev/v/lab/value> ?text ; nk:signer ?signer ; dct:created ?when .
}
ORDER BY ?when
```

A second description does not replace the first (SPEC §8.6): both are answered.

### The latest description of a step

```sparql
PREFIX dct:  <http://purl.org/dc/terms/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX nk:   <https://kton.dev/v/>
SELECT ?text ?by WHERE {
  ?claim nk:about <https://kton.dev/o/STEP> ; nk:predicate dct:description ; dct:created ?when ;
         <https://kton.dev/v/lab/value> ?text ; nk:signer ?signer .
  OPTIONAL { ?signer rdfs:label ?by }
}
ORDER BY DESC(?when)
LIMIT 1
```

### What one signer said in a time window

```sparql
PREFIX dct: <http://purl.org/dc/terms/>
PREFIX nk:  <https://kton.dev/v/>
PREFIX xsd: <http://www.w3.org/2001/XMLSchema#>
SELECT ?claim ?when WHERE {
  ?claim nk:signer <https://kton.dev/agent/KEYID> ; dct:created ?when .
  FILTER (?when >= "2026-01-01T00:00:00Z"^^xsd:dateTime && ?when < "2100-01-01T00:00:00Z"^^xsd:dateTime)
}
ORDER BY ?when
```

Steps carry no time, so this covers claims. The steps a signer signed are
`?step prov:wasAttributedTo <https://kton.dev/agent/KEYID>`. A signer's name and tier are on the
agent (`rdfs:label`, `nk:tier`).

### The claims in a scope, by its name

```sparql
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX nk:   <https://kton.dev/v/>
SELECT ?claim ?subject ?predicate ?position WHERE {
  ?seed nk:genesis true ; rdfs:label "study-af-2026" .
  ?claim nk:scope ?seed ; nk:about ?subject ; nk:predicate ?predicate ; nk:position ?position .
}
ORDER BY ?position
```

One scope at a time: a study's project and programme are not in the projection, and `ask scope`
names them (`context`).

### The claims of one scope, read from its graph

```sparql
PREFIX nk:   <https://kton.dev/v/>
SELECT ?claim ?subject ?predicate WHERE {
  ?g a nk:ClaimGraph ; nk:scope <https://kton.dev/o/SCOPE> .
  GRAPH ?g { ?claim nk:about ?subject ; nk:predicate ?predicate }
}
```

The scope's graph holds its claims and its seed, nothing of other scopes. Found by the seed, exactly
(`--bind-iri SCOPE=sha256:<hex>` binds it too); by the name (`rdfs:label`) it reads every scope of that
name, which `repeatedLabels` says. The version of a loaded data set loaded last is found the same way:

```text
SELECT ?path (MAX(?when) AS ?latest) WHERE { ?g a nk:LoadedGraph ; nk:path ?path ; nk:loadedAt ?when }
GROUP BY ?path
```

That is the version loaded last, not the one produced last: reloading an old file makes it the latest
loaded.

### The latest claim per subject in a scope

```sparql
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX nk:   <https://kton.dev/v/>
SELECT ?subject ?pred ?claim WHERE {
  { SELECT ?seed ?subject ?pred (MAX(?position) AS ?latest) WHERE {
      ?seed nk:genesis true ; rdfs:label "study-af-2026" .
      ?c nk:scope ?seed ; nk:about ?subject ; nk:predicate ?pred ; nk:position ?position .
    } GROUP BY ?seed ?subject ?pred }
  ?claim nk:scope ?seed ; nk:about ?subject ; nk:predicate ?pred ; nk:position ?latest .
}
```

The latest claim per subject **and template**: ask for the highest position per subject and predicate with
`GROUP BY` and `MAX`, then join back. Leave the predicate out of the group and a later claim of another
template about the same subject (a comment after a decision) hides the one you meant. The scope's name is
bound inside the sub-select, so it groups over that scope's claims only. Positions order the claims of one
scope's chain; a claim outside a scope has none. Where the chain forked, two claims share a position and both
come back. The form
`FILTER NOT EXISTS { ?later … nk:position ?p2 FILTER(?p2 > ?p) }` compares every claim of a subject with
every other, and where many claims share a subject it does not return: on dry run 1, 2,556 claims about one
foton took longer than ten minutes that way and 0.04 s with `GROUP BY`. Mind what "latest" means for a
template: claims that are rows (each about the same foton, none a version of another) are not versions, and
"latest per subject" would keep one of them.

### The descriptions of a step, in the nanopublication graphs

```sparql
# dataset: nanopub
PREFIX dct: <http://purl.org/dc/terms/>
PREFIX np:  <http://www.nanopub.org/nschema#>
SELECT ?text ?signer ?when WHERE {
  GRAPH ?assertion { <https://kton.dev/o/STEP> dct:description ?o . ?o <https://kton.dev/v/lab/value> ?text }
  GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
  GRAPH ?pubinfo { ?np dct:created ?when }
  GRAPH ?attribution { ?np dct:creator ?signer }
}
```

The same claims as the signed form shows them. A claim is four graphs here, and a query over many of
them is slower than over the claim nodes.

### Files that two independent signers produced

```sparql
PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT DISTINCT ?file ?path WHERE {
  ?file prov:wasGeneratedBy ?one, ?other .
  ?one prov:wasAttributedTo ?a .
  ?other prov:wasAttributedTo ?b .
  FILTER (?one != ?other && ?a != ?b)
  OPTIONAL { ?file rdfs:label ?path }
}
```

This is a count of signers, not of reproductions. `ask reproductions` applies the protocol's rule
for independence.

### Steps that ran a script, by its path

```sparql
PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT DISTINCT ?step ?path WHERE {
  ?step prov:used ?script ; prov:wasAttributedTo ?signer .
  ?script rdfs:label ?path .
  FILTER regex(?path, "analyse", "i")
}
```

The command line itself is not in the projection, only `nk:protocolRef`, its content hash. So this
matches the paths of a step's inputs. `ask record` shows the command of a step.

### Everything a file was derived from

```sparql
PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT DISTINCT ?source ?path WHERE {
  <https://kton.dev/o/FILE> prov:wasDerivedFrom+ ?source .
  OPTIONAL { ?source rdfs:label ?path }
}
```

### Did a key you trust produce this file?

```sparql
PREFIX prov: <http://www.w3.org/ns/prov#>
ASK { <https://kton.dev/o/FILE> prov:wasGeneratedBy ?step . ?step prov:wasAttributedTo ?signer }
```
