---
title: "Signing costs one claim: the view is patched by what arrived, and say reads its scope's head from the index"
type: "decision"
project: "kton-cockpit"
date: 2026-11-25
status: "accepted"
tags: [architecture, index, oxigraph, say, performance]
---

# ADR-022: Signing costs one claim

## Status

Accepted by MH (2026-11-25), with D1.4 narrowed as he asked; proposed the same day. Planned with kton-review before the build. Asked by MH through qm (25.11., b3b1c7b0, 7b886a9e):
"signing one claim must be one claim and an append, the same cost at 50 or 50,000 claims", and "named
graphs already say what to reload". MH put this piece before ADR-021's build (25.11.).

## Context

ui-dryrun measured v0.5.14 on a copy of MH's registry (29,703 claims, 634k triples, 5 sources; answers
9baa0d6e, fc43ee2c):

| step | time | what grows with |
|---|---|---|
| say: prepare | 1.6 s | templates |
| say: **the scope's head** | **18.0 s** (4.2 s when the union is current) | every record of every source |
| say: previous claim | 0.7 s | own registry |
| say: sign + store, persist, confirm | 2.6 s | one claim, git |
| next ask: reading registries | < 3 s | records past each cursor (and a stat per object file) |
| next ask: **projecting the claims** | **18-21 s** | every trusted claim |
| next ask: writing the view | ≤ 1 s | the graphs whose hash changed (#226) |

The two large costs have different roots, and neither is the oxigraph store:

1. **say's scope head** (`cockpit/say.go` `resolveChain` → `Runner.Head` → `Runner.read` → `ReadDirs`). A
   repository with federation sources reads a union *directory*: every source copied into
   `union-<stamp>` (`buildUnion`) and opened, which verifies and settles every record. The stamp names
   the own registry's position, and the previous say moved it, so every say after a say copies and
   verifies the whole federation again (18 s); when the union happens to be current, opening it is still
   4.2 s.
2. **the next ask** (`internal/index/oxigraph/store.go` `prepare`). Reading into the store is already
   incremental (`apply` reads `Records(cursor)`), and writing the view is too (`makeView` uploads only the
   graphs whose hash changed). Between them, any new record changes the dataset key, and `prepare`
   recomputes everything: `dataset()` lists every trusted graph, `claimView` → `quadsOf` reads all their
   quads (~25 MB) back out of the store, `ClaimView` projects every claim, `scopeGraphs` and the hashes
   run over all of it. A one-claim change costs a whole-federation projection, and then the hash
   comparison finds that one scope graph changed.

## Decision

### D1. The view is patched by what arrived, not rebuilt

A view (one per slot) remembers the per-registry cursor it was built at. When `prepare` finds the key
changed, it projects only the **delta**: the records each registry took past the view's cursor, read by
their seq (D1.1), plus the claims those records **touch** (D1.2), and patches the view's graphs (D1.3).
`ClaimView` stays the one definition of the projection: the patch calls it on the delta and its
neighbours, and a test holds the patched view equal to a full build (D1.5).

**D1.1 What arrived.** `apply` numbers what it adds, in the index's own counter per registry: for every
record graph and attribution graph it holds or attests for the first time, `<graph> <urn:kton:arrived:<regid>> n`
in `urn:kton:index`, n one past the registry's last. The kernel's seq is not used for this: records a git pull
brought arrive unnumbered and are read from 0 (`apply`'s existing rule), and only the index can tell which of
them it did not hold yet. The view's mark graph records `<reg> urn:kton:builtAt n` per registry. The delta of
a view is every graph whose arrival number in one of its registries is above that registry's `builtAt`. A
co-sign is a new attribution graph of a record already held, so it is part of the delta too.

**D1.2 What it touches** (the cross-claim parts of `ClaimView`, every one of them):

| part | changes when | neighbour read from the view |
|---|---|---|
| a claim's node (fields, about, signers, created, scope, prev) | its record or a new attribution arrives | none: its own graphs |
| `nk:position` | the claim arrives (= position of its prev + 1) | the prev's position, from the chain graph (D2) |
| `nk:replacedBy` on an older claim | a trusted claim naming it in `replaces` arrives, or either one gains a signer | the replaced claim's `nk:signer`, `nk:predicate`, `nk:about`; and the claims that name the co-signed one in `replaces` |
| signer labels (`rdfs:label`, `nk:tier` of `agent:<kid>`) | a claim brings a signer the view has no label for | the rest graph |
| what a claim asserts about something else (`other`, the shared quads) | the claim arrives | none: it is added to the rest graph |
| scope graph metadata (`nk:claims` count, label, parent, source) | a claim joins the scope; a seed arrives | the metadata in the rest graph |
| the default graph's copy of the plankton records (`viewIRI`) | a plankton record or its attribution arrives | none: the new graphs are added to it, not the whole copy redone |

**D1.3 How the view is patched.** One SPARQL update, as `makeView`'s is, so a reader sees the old view or
the new one: for every claim node the delta projects, the triples it had in its scope graph leave the union of
the claims and that graph (`DELETE { GRAPH c {…} GRAPH g {…} } WHERE { GRAPH g { <o/h> ?p ?o } }`, as
`makeView` removes a graph's triples from the union today; a claim never changes scope graph, its scope
is signed), then `INSERT DATA` its new triples into its scope graph
and the union; the rest graph's changed triples the same way; the mark graph's key, `builtAt` and
per-graph state. A graph's state stops being the hash of its whole content (that would need the whole
content): it becomes `hash(previous state, the triples deleted, the triples inserted)`, which is all
`viewstate.go`'s `Diff` and `indexMoved` need (they compare for equality). Nodes are subjects of their
own triples only; the nested field nodes of a claim (`#o.<field>`) are deleted and inserted with it.

**D1.4 When it is a full build instead.** Registries are append-only, and the kernel's union holds a claim only
once its chain resolves, so nothing that arrives can move a position the union gave. The index is not the
union: it holds claims a registry keeps waiting (deferred, `elsewhere_test.go`), and a seed or prev arriving
later admits them and their successors, transitively; that is a delta row (D1.2), not a rebuild (kton-review
on ecc30f5). Arrival order does not matter otherwise, and a git pull is a delta like any other (MH, 25.11.:
"what merge in an append only format?"). A full build remains only where the view's inputs change, not its
records: the view does not exist or has no `builtAt` (built before this ADR); the view config's fingerprint
or the trusted keys changed (in the slot or key already); and, as a safety net, a registry that lost
records (`forgetDeparted` found something to forget: a registry replaced or reset, which append-only
forbids). A large delta is patched too, with no bound (MH, Q3): a first build of a new view is the only
large one, and that is a full build anyway.

**D1.5 One projection.** `ClaimView(trusted, structure, vc)` is unchanged and stays what a container
(`ClaimDataset`) and a full build use. The patch gives it the delta's quads plus the neighbours' quads
read by their graphs (`quadsOf` on a few dozen graphs, not all of them) and the chain graph's links for
positions, and takes from its output only the nodes of the claims it was asked about. A property test
builds random sequences of says, co-signs, replacements, seeds, and claims from untrusted keys, and holds
after every step: the patched view's graphs equal (as sets of triples) a full build of the same
registries.

### D2. A chain graph per view: every claim's scope, prev and position

Positions count every claim of the union, trusted or not (`structure`), and today they are recomputed
from every claim's pubinfo on each build. The view gets one more graph, `urn:kton:view:<slot>:chain`:
for every nekton record of its registries, `<o/h> nk:scope <o/seed> ; nk:prev <o/p> ; nk:position n`
(trusted or not: this is chain structure, which `ClaimView` already reads for all claims). It is patched
with the view (D1.3), listed in no query's dataset (not the claims dataset, not the nanopub one): it is
the index's own bookkeeping, like `urn:kton:index`.

Its source is the kernel's records, not the projection (kton-review, 25.11.: "the chain graph holds only
records the kernel holds"). `apply` writes each record's chain links from the record itself (the envelope's
scope and prev, `claim.ParseEnvelope` → `ParsePredicate`, as `nregistry.Heads` reads them), for every record
`Records()` returns. So it holds exactly what the kernel holds: settled, chain-resolved records, never bytes
the kernel did not take. The chain graph is built from the envelopes, so it does not depend on the projection: it holds every record
the kernel holds whatever the projection makes of its predicate or subject (a predicate with a space is
percent-encoded, `has%20space`, and still projected). The equivalence test of D3 has such a claim as a
scope's last one.

A registry read alone does not drop a claim whose seed or prev it lacks: the kernel holds it **deferred**
and still returns it from `Records()` (it is in the feed offered to peers), so the index holds claims whose
chain lies in another registry (`elsewhere_test.go`, cases 1 and 2, with a revert check). The same is true of a
claim deferred in *every* registry of the slot (case 3): the union keeps it out of every query and `Heads`
does not count it, yet it reaches the index. So the chain graph holds a claim only when its chain resolves in
the slot's registries: its seed is in the chain graph and its prev is (the seed for the first claim). A claim
that does not resolve is kept aside (not a head, not counted) and enters the chain graph when the record it
waits for arrives: the delta that brings a seed or prev also admits the claims waiting on it. The equivalence
test has a claim deferred everywhere in a scope that also has resolved claims.

**The view follows the same rule (MH, 25.11.).** "We can just import in IRI named graphs; of course I could also
import claims. It just may not show up in a nekton named graph. That's why we are playing this game." A record
graph is data like any imported graph and stays in the store and in the nanopub dataset. A claim node, in a scope
graph and in the union of the claims, exists only for a claim whose chain resolves in the slot's registries.
Today a claim deferred in every registry becomes a Claim node in the graph of a scope whose seed nobody holds
(`elsewhere_test.go` case 3, observed); with PR 2 it does not, case 3 asserts that, and `viewVersion` moves.

### D3. say reads its scope's head from the index

With `index.kind: oxigraph`, `resolveChain` brings the view up to date (D1: the own registry's last
records, usually one) and reads the scope's heads from the chain graph: the scope's claims that no other
claim of the scope names as `nk:prev`, or the seed when it has none; `ChainLength` the count of them;
`Unresolved` those whose prev the chain graph does not hold. That is `nregistry.Heads`' definition
(SPEC §7.4) on the same records. No union directory is copied for it; the claims lock still covers the
read through the store, as it does today. Without the oxigraph index (`memory`, none, or the daemon not
reachable) say reads the union as it does now.

Two things say must not lose, each a test:

- **The same heads as the kernel.** A test runs both on fixtures with a linear scope, a branched one, a
  scope seeded in a federation source with claims in the own registry, a scope with an unresolved claim,
  and a claim from an untrusted key in the scope (it counts for the chain): `Heads` from the chain graph
  equals `nregistry.OpenUnion(dirs...).Heads`.
- **A scope seeded in another registry.** The index reads each registry alone (`OpenNektonSource` of one
  directory), and a registry opened alone settles alone: a claim whose seed lives only in another
  registry is dropped there (`registry.go` openAt, settle), where the union open keeps it. Before D3 is
  built, a test shows whether the index holds such a claim today. If it does not, that is a defect of the
  index already (the view misses the claim too), fixed first by giving `apply` the union's settling for
  nekton (kernel `OpenUnion` of the slot's nekton directories, read past cursors), or by asking kton for
  a read that keeps a record waiting for a seed in another registry.

### D4. Out of scope here

`ReadDirs`' union copy stays for the other readers (publish, show, the union file); a reader-wide change
(the kernel's `OpenUnion(dirs...)` without a copy) is its own piece. "Reading registries" stats every
object file per position read (`core.ReadPosition`); it is under 3 s at 30k claims and grows linearly,
named here, not changed. The window's poll is replaced by #259's events already; with D1 an event follows
one patch.

## Expected times (MH's registry copy, 29.7k claims)

| step | today | after |
|---|---|---|
| say: the scope's head | 18 s (4.2 s) | the own registry's delta into the store + one query: < 1 s |
| say total | ~23 s | ~5-6 s (prepare 1.6, head < 1, previous 0.7, sign/persist/confirm 2.6) |
| next ask after a say | ~26 s | registries < 3 s + patch of one claim < 0.5 s: ~3.5 s, as a steady ask |
| a full build (new key, epoch change) | ~26 s | unchanged |

The remaining say time is prepare (templates) and git; neither grows with claims. Measured again by
ui-dryrun with `COCKPIT_TRACE=1` on the same copy before the PRs are called done.

## For the window (ui-shell)

Nothing to change for D1-D3: say over stdio returns sooner, and the `indexMoved` that follows names one
scope graph and the rest. The window should stop treating a say as "everything moved" if it still does.

## Plan (for kton-review)

Three PRs, each with its own revert checks:

1. **The seed-elsewhere test** (D3's second bullet), and the fix if the index drops such a claim.
2. **D1 + D2**: seqs in `urn:kton:index`, `builtAt`, the chain graph, the patch, the full-build triggers,
   the property test against a full build; `indexMoved` from the patched states.
3. **D3**: say's head from the chain graph, the equivalence test against the kernel, the fallback.

## Questions

- Q1 (kton-review): is D1.2 every cross-claim part of `ClaimView`? Anything missed means a patched view
  that differs from a full build; the property test is meant to find it, the list is meant to make it
  unnecessary.
- Q2 (kton-review): the patch's state replaces the content hash for `indexMoved`. Is equality of a
  chained state enough for ui-shell's "drop an event with the same key" and for #259's diff?
- Q3 (MH): answered 25.11.: no bound, and no merge trigger (D1.4).
