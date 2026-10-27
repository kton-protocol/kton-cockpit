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
  signed, as one node**, with its subject, its fields, its signer, its scope, its position in the
  scope's chain and whether it is current. There are no named graphs: a query names nothing, and
  stays fast as the work grows.
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
       prov:wasAttributedTo ?signer .      # only where a key you trust verified it
?output prov:wasGeneratedBy ?step ;
        prov:wasDerivedFrom ?input .
?file  a prov:Entity ; rdfs:label "data/in.csv" .   # the path the step named it by
```

A step carries no time and no command text.

**A claim** with the claim id `sha256:<hex>` is the node `<https://kton.dev/o/<hex>>` (dataset
`claims`):

```
?claim a nk:Claim ;
       nk:about ?subject ;                 # what it is about: a step, a file, any IRI
       nk:predicate ?predicate ;           # the template's predicate
       nk:template "describes" ;           # where your templates name it
       <https://kton.dev/v/lab/value> "…" ;  # each field: nk:lab/<field>, or the term your aliases give it
       dct:created ?when ;                 # xsd:dateTime, signed
       nk:signer ?agent ;                  # every key you trust that verifies it
       nk:scope ?seed ; nk:prev ?previous ;  # when it is chained in a scope
       nk:position 3 ;                     # its place in the scope's chain; the seed is 0
       nk:current true .                   # no later claim you trust, same scope, subject, predicate
?agent rdfs:label "alice" ; nk:tier "peers" .   # from your trust configuration
```

`describes` is `dct:description`, `rationale` is `ep:hasRationale`, and both have the field
`nk:lab/value`. Other templates are under their own predicate (`cockpit ask` lists them). A field
your aliases name (`aliases.json`, kton's alias format) is under that term; a field a template types
`date` is an `xsd:date` or `xsd:dateTime`. A field that names a file or a step (a `ref`, such as
`parent-step`) points at its node `<https://kton.dev/o/<hex>>`, so it joins the steps directly.

**Position and current are kept, not walked.** The index computes them once per change, so a query
that wants the latest of something asks `nk:current true` and one that wants order asks
`nk:position`, without `nk:prev+`. Positions count every claim of the chain, including ones you do not
trust; only claims you trust supersede one another.

**A scope**'s seed is a claim node with `nk:genesis true`, `rdfs:label "<name>"` and `nk:scope` its own
node. Its parent is not in the projection: `ask scope` reports it.

**In the `nanopub` dataset** a claim is the nanopublication `<https://kton.dev/np/<hex>.>`:

```
GRAPH ?head      { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
GRAPH ?assertion { ?subject ?predicate ?o . ?o ?field ?value }
GRAPH ?pubinfo   { ?np dct:created ?when ; prov:wasDerivedFrom ?claim ; nk:scope ?scope ; nk:prev ?previous }
GRAPH ?attribution { ?np dct:creator ?signer . ?assertion prov:wasAttributedTo ?signer }
```

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

A second description does not replace the first (SPEC §8.6): both are answered, and `nk:current`
says which is the latest.

### The current description of a step

```sparql
PREFIX dct:  <http://purl.org/dc/terms/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX nk:   <https://kton.dev/v/>
SELECT ?text ?by WHERE {
  ?claim nk:about <https://kton.dev/o/STEP> ; nk:predicate dct:description ; nk:current true ;
         <https://kton.dev/v/lab/value> ?text ; nk:signer ?signer .
  OPTIONAL { ?signer rdfs:label ?by }
}
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
