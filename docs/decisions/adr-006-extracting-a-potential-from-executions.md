---
title: "Extracting a potential from executions that already ran"
type: "decision"
project: "kton-cockpit"
date: 2026-09-30
status: "proposed"
tags: [potentials, rays, packages, extraction, federation]
---

# ADR-006: Extracting a potential from executions

## Status

Proposed (2026-09-30), on `jam-beta`. Steps 1 and 2 below are being built; the cockpit surface
(step 5) waits for ADR-005's S5 and S6.

## Context

The usual direction is potential → realisation: a ray with holes is bound and run. The opposite
direction is at least as common in practice. Somebody ran an analysis step by step, and now it
should become something another person can run on their own data: a `kton-package/1` ray.

improvego already does this for improve workflows by hand. `LoadWorkflow` reads a workflow back,
`ParameterizeStep` turns a slot or a command-line value into a hole, and `ExportPackage` writes the
package (`cmd/diamondtest`). On the git side there is only what the registry holds: signed fotons,
connected by content hashes.

## Decision

### The idea

From a set of **realisations**, derive their **potential**. Inputs that flowed between the
executions become wiring. Inputs that came from outside become either fixed payload or a hole.
Values that differ between runs of the same code become either literals or parameters.

The executions themselves become the package's evidence. Their inputs are the **test data** of the
holes, and their outputs are the **reference members of its spectrum**. A package extracted this
way can therefore check itself immediately: realise it on its defaults, and its outputs must meet
the spectrum. Those members are outputs of recorded fotons, so the kernel does not call them
fabricated (kton §10).

### Nothing becomes a hole unless somebody names it

The extraction **proposes** and a person **chooses**. No input becomes a hole, and no command-line
value becomes a parameter, because a rule guessed it.

- **File candidates** are every input whose hash no selected execution produced. The proposal says
  which of them is the command file. It knows this as a fact of the execution: improve's
  `command-file` variable, or a token of the command line that is exactly an input's slot. The
  proposal does not judge whether something "looks like data".
- **Parameter candidates** exist only where several runs of the same step disagree at the same
  position of the command line. With a single run there are none. A number in a command line is
  not a parameter until somebody says so.
- Whatever is not chosen stays fixed: a file travels in the payload, and a value stays a literal.

### Which executions

- The selection starts at an endpoint, an output hash or a foton id, and walks the lineage
  backwards.
- **Only executions that verify against the configured trust tiers are taken.** Extraction adopts
  another party's code as fixed payload, so an unverified record must not get in.
- **Federated executions are included** when they verify. A foton from another participant counts
  as much as one's own, and its bytes must be obtainable: from the local tree, or by a locator
  whose fetch is checked against the hash.
- Several producers of the same hash are reproductions, not separate steps.

### Runs and steps

- A **run** is a connected component: executions linked by the hashes that flowed between them.
- A **step** is a group of executions across runs that share their code and their shape: the same
  command-file hash, and the same input and output slots.
- One run is chosen as the **reference**. Its values become the defaults of the holes, and its
  outputs become the spectrum.

### Where it lives

- **`ktonpkg`: `Propose` and `Extract`.** They work over backend-neutral executions (slots,
  hashes, command line, command file) plus a function that fetches bytes by hash. The rule exists
  once, like the ray identity does.
- **Adapters** turn a backend's records into those executions:
  - the cockpit's fotons: working directory, command line, repo-relative paths;
  - improvego's steps: descriptor, variables, references.
- **Cockpit, without a fourth verb:**
  - `ask {query: "ray", ref: <endpoint>}` returns the proposal. It only reads, and it contains
    verified executions only.
  - `publish {kind: "ray", from: <proposal>, choose: {…}}` writes the package, records the
    potential, and signs a `prov:wasDerivedFrom` claim from the package to the source fotons.

### Round trip, and where it depends on ADR-005

The acceptance criterion is behavioural: realise the extracted package on its defaults, and the
outputs meet its spectrum.

A stronger criterion would be that a realisation of the extracted step has the **same action key**
as the original execution, because reuse would then find the original. That holds only when both
sides compute identity the same way. improvego and `ktonpkg` already do. The cockpit does not yet:
`publish` records its command line and image in the protocol, which the Raute acceptance test
already showed for the normaliser. When S5 realises through `ktonpkg`'s step identity, extraction
becomes lossless on the git side too.

## Steps

| step | content |
|---|---|
| 1 | `ktonpkg.Propose` / `ktonpkg.Extract`, unit tests on synthetic executions shaped like the Raute |
| 2 | prototype in `acceptance/raute-zwei-backends`: run the Raute twice on the git side, extract with explicit choices, compare with the original package, realise the extracted package and hold it against its spectrum |
| 3 | across backends: extract on one side, install and run on the other |
| 4 | improvego on `Propose`/`Extract` instead of hand-written `ParameterizeStep` |
| 5 | cockpit `ask ray` and `publish kind:ray`, with ADR-005 S5/S6 |
