# Asking in SPARQL

`ask sparql` (SPEC §9.9) answers a SELECT or an ASK you wrote, over the dataset the vocabulary
queries read:

```sh
cockpit ask sparql query.rq            # or - for stdin
cockpit ask '{"query":"sparql","sparql":"SELECT …","limit":100}'
```

Over MCP, `cockpit_ask` takes the same `{"query":"sparql","sparql":…}`. The answer is
`{"form","vars","rows","boolean","truncated","limit","via"}`. Each row binds the variables by name, and
each value is in its lexical form.

It needs a SPARQL engine: `query.image`, or `index.kind: oxigraph`. The projection below is kton's
K8, released in kton 0.3.0.

## What is in the dataset

The dataset is the kernel's RDF projection of every registry in your union, sources included. It is
chosen for your trust tiers each time a query runs:

- **The default graph** holds every step (foton) of the union, and that step's attribution to the
  keys your tiers trust. A step **nobody you trust signed is there too, but unattributed**. A query
  that wants trusted steps only asks for `prov:wasAttributedTo`.
- **The named graphs** hold every claim a key you trust signed, as a nanopublication of four
  graphs. A claim nobody you trust signed is not in the dataset at all.

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

**A claim** with the claim id `sha256:<hex>` is the nanopublication `<https://kton.dev/np/<hex>.>`.
It has these graphs:

```
GRAPH ?head      { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
GRAPH ?assertion { ?subject ?predicate ?o . ?o ?field ?value }  # the template's predicate; fields are nk:lab/<field>
GRAPH ?pubinfo   { ?np dct:created ?when ;                       # xsd:dateTime
                       prov:wasDerivedFrom ?claim ;              # the claim's own content node
                       nk:scope ?scope ; nk:prev ?previous }     # when it is chained in a scope
GRAPH ?attribution { ?np dct:creator ?signer . ?assertion prov:wasAttributedTo ?signer }
```

`describes` is `dct:description`, `rationale` is `ep:hasRationale`, and both have the field
`nk:lab/value`. Other templates are under their own predicate (`cockpit ask` lists them).

**A scope**'s seed is a claim with `nk:genesis true` and `nk:scope` its own id. Its assertion is
`<urn:nekton:scope:<name>> a nk:Scope ; rdfs:label "<name>"`. A scope's parent is not in the
projection: `ask scope` reports it.

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
PREFIX np:  <http://www.nanopub.org/nschema#>
PREFIX nk:  <https://kton.dev/v/>
SELECT ?kind ?text ?signer ?when WHERE {
  VALUES (?predicate ?kind) { (dct:description "description") (ep:hasRationale "rationale") }
  GRAPH ?assertion { <https://kton.dev/o/STEP> ?predicate ?o . ?o <https://kton.dev/v/lab/value> ?text }
  GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
  GRAPH ?pubinfo { ?np dct:created ?when }
  GRAPH ?attribution { ?np dct:creator ?signer }
}
ORDER BY ?when
```

A second description does not replace the first (SPEC §8.6). Ordered by time, the last one is the
newest.

### What one signer said in a time window

```sparql
PREFIX dct:  <http://purl.org/dc/terms/>
PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX xsd:  <http://www.w3.org/2001/XMLSchema#>
SELECT ?claim ?when WHERE {
  GRAPH ?attribution { ?np dct:creator <https://kton.dev/agent/KEYID> }
  GRAPH ?pubinfo { ?np dct:created ?when ; prov:wasDerivedFrom ?claim }
  FILTER (?when >= "2026-01-01T00:00:00Z"^^xsd:dateTime && ?when < "2100-01-01T00:00:00Z"^^xsd:dateTime)
}
ORDER BY ?when
```

Steps carry no time, so this covers claims. The steps a signer signed are
`?step prov:wasAttributedTo <https://kton.dev/agent/KEYID>`. A trust tier is not in the dataset, so
name the tier's keys.

### The claims in a scope, by its name

```sparql
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX prov: <http://www.w3.org/ns/prov#>
PREFIX np:   <http://www.nanopub.org/nschema#>
PREFIX nk:   <https://kton.dev/v/>
SELECT ?claim ?subject ?predicate WHERE {
  GRAPH ?seedAssertion { ?s a nk:Scope ; rdfs:label "study-af-2026" }
  GRAPH ?seedHead { ?seed np:hasAssertion ?seedAssertion ; np:hasPublicationInfo ?seedInfo }
  GRAPH ?seedInfo { ?seed nk:genesis true ; nk:scope ?scope }
  GRAPH ?pubinfo { ?np nk:scope ?scope ; prov:wasDerivedFrom ?claim
                   FILTER NOT EXISTS { ?np nk:genesis true } }
  GRAPH ?head { ?np np:hasAssertion ?assertion ; np:hasPublicationInfo ?pubinfo }
  GRAPH ?assertion { ?subject ?predicate ?o FILTER NOT EXISTS { ?any ?points ?subject } }
}
```

The subject of a claim is the node in its assertion that nothing else there points to. Its fields
hang off `?o`. One scope at a time: a study's project and programme are not in the projection, and `ask scope`
names them (`context`).

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
