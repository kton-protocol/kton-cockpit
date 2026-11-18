---
title: "Tool packages as the first module: install a tool for a program, qualify it, gate reuse on it"
type: "decision"
project: "kton-cockpit"
date: 2026-11-18
status: "proposed"
tags: [architecture, module, package, tool, environment, qualification, reuse]
---

# ADR-015: Tool packages as the first module

## Status

Proposed (2026-11-18). Requested by kton-coordination on MH's decision of 18.11. (plan
`kton-coordination/proposals/2026-11-18-tq-packages.md`, phase P2). The decision covers three things.
It specifies the module door that ADR-011 §5 left open "until the first module moves". It makes ktonpkg's
tool profile that first module. It implements ADR-010 rule 4, the reuse gate, which has been decided but
not built. It needs ktonpkg's tool builder and its tool-profile verification (plan phase P1) before its
code can land. It changes no verb and leaves SPEC §6 untouched.

## Context

- **Tool packages exist, but the cockpit ignores them.** ktonpkg's scope format has `profile/tool`, with
  the roles `instance`, `build`, `spectrum`, `reference` and `document` and the claims `image` and
  `invocation`. `cockpit install` acts only on the workflow, verticle and templates profiles. A tool
  package installs, and nothing reads it.
- **There is one environment class per repo.** `environment.spectrum` names one class for every
  publish. A repo that runs R and NONMEM needs two. PR #98 lets a caller name the class per publish
  (`--environment`), but nothing yet ties a program to its class. The open request of 11.11.
  (`repo.improve.tools.nmfe75.environment`) asks exactly for that.
- **Qualification happens outside the cockpit.** jam-r 1.0.0 was qualified L1 on git and improve on
  30.09. (spectrum `e0669ff3…`), with improvego and the plankton CLI. Nothing in the cockpit signs
  `qualifies-as`, so ADR-010 rule 4 cannot be applied: no reuse crosses environments, or else it
  would cross unqualified (the tool shim, ADR-014, defers it as "not yet").
- **Licences are hidden configuration.** NONMEM needs a licence file in the environment. It must
  never be an input of a record, be hashed into an identity, be committed or be printed. It belongs to
  the class's site, like the image's digest, not to the computation.

## Decision

### 1 The module door (ADR-011 §5, specified)

A module is a Go package that registers itself from `init` with `cockpit/module.Register`:

```go
type Module struct {
    Name     string     // the operator command group: `cockpit <name> …`
    Commands []Command  // operator commands, CLI only (SPEC §6): never on MCP
    Profiles []string   // the package profiles `cockpit install` hands to this module
    Words    fs.FS      // templates and named queries it brings, admitted like a package's
}
```

A module reaches the core only through `Publish`, `Say`, `Ask`, the executor and the index's read side,
as ADR-011 §5 says. It never opens a registry and never signs anything itself. Operator commands are
CLI only. An agent cannot install or qualify a tool, because both decide what later counts as evidence.
A verb would need ADR-009's route: SPEC, CLI, MCP and Go together, under the configuration ceiling.
That is not taken here.

The public binary links the `tool` module. A build without it behaves exactly as today.

### 2 `cockpit tool install <package>`: one class per program

`tool install` installs a `profile/tool` package as `install` does: it verifies the package and records the
installation. It also **binds the tool to the programs it serves**, in the config:

```json
"tools": {
  "Rscript": { "package": "jam-r", "revision": "sha256:…", "class": "sha256:e0669ff3…", "envRef": "oci://scinteco/jam-r@sha256:dee2a8cf…" },
  "nmfe75":  { "package": "nonmem-7.5", "revision": "sha256:…", "class": "sha256:…", "envRef": "oci://…@sha256:…",
               "hidden": { "licence": "nonmem.lic" } }
}
```

- The programs a tool serves are named by the package (`tool.json` `programs`). `class` is the id of
  its spectrum (`Revision.Spectrum().ID()`); `envRef` is its `image`, by digest. All of them come from
  the sealed revision, which `Revision.ToolProblems` has passed, and none is typed in by the operator.
- A publish resolves its class in this order:
  1. the call's `environment` (PR #98);
  2. the binding of the program the command starts with (`argv[0]`, or the shim's program);
  3. `environment.spectrum`.
  The cockpit says which one it took. With `execution` enabled, a call that names a class different from
  the binding's is refused, as PR #98 refuses one against the repo's class.
- The improve defaults a package carries (run-server name, instance) are ignored by every site except
  improve's (MH, D2). Facts that exist only after an install there (an instance id, step ids) are said as
  claims at install or run time. They never go into a package or foton identity.

### 3 The `ToolSite` port: where a tool lives

```go
type ToolSite interface {
    Install(ctx, pkg ToolPackage) (Binding, error) // make the tool available here; what was done
    Locate(ctx, b Binding) (Environment, error)    // envRef + hidden mounts for one run
}
```

The **git/local site** runs the image by digest through the container executor (ADR-003). Hidden
configuration is a **read-only mount** from the operator's configuration directory
(`~/.config/kton-cockpit/licences/<name>`), at the path the package declares. It is not an input, not
hashed, not material, and not committed. The executor refuses to run if the file is missing, and it
never prints the file. The **improve site** is implemented in kton-cockpit-improve (plan phase P3).

### 4 `cockpit tool qualify <program|package>`: evidence, then a signed claim

1. For each member of the package's spectrum: run the member's inputs in the bound environment, through
   the executor, with the package's command frame, and publish the run as a foton of the class. The
   package's reference runs are added to the registry first, so they can be compared against.
2. Check the candidates against the spectrum: `Revision.SpectrumCheck` (ktonpkg ≥ v0.6.0), which is
   `kton.dev/plankton/spectrum.Check` under the spectrum's normaliser. Counter-cases are run and
   reported, but are not members. `Revision.Coverage` gives each requirement its EARL outcome, as input
   for the report.
3. If the outcome is `fulfilled`, say **`qualifies-as`** about the `envRef`. The fields are the `class`, the
   package `revision`, the `level` reached, and `evidence`, the member fotons. The claim is signed with
   `--as <person>`: qualification is a person's signature with a meaning (Part 11), not the repo's.
   If a member fails, nothing is said, and the report lists the failure.

`qualifies-as` is a built-in template, like `reproduces`, so its fields are the same in every repo. The
release that follows (four-eyes, FOR-QE-04 report) is the `tool-qualification` workflow's (plan phase
P6). `qualify` produces its input and does not replace it.

### 5 The reuse gate (ADR-010 rule 4)

A reuse hit across concrete environments (the tool shim, ADR-014; `ask` by action key) counts only if the
hit's carried `envRef` has a `qualifies-as` claim for the record's class, signed by a key in a trust tier
this repo configures. Without such a claim the hit is unqualified and is not reused (fail closed). Within
one exact environment nothing changes. `Registry.Reuse` keeps returning every candidate; the gate is the
consumer's, as ADR-010 says.

## Consequences

- R and NONMEM can live in one repo with their own classes, and a recorder publishing for both no longer
  has to name the class itself (it still may).
- Qualification on the git side goes through the cockpit with the same package and the same commands as
  on improve. Only the site differs.
- `cockpit install` hands `profile/tool` to the module; `internal/packages` and `cockpit/install.go` begin
  to move behind the door, as ADR-011's consequences foresee. The workflow and verticle profiles follow
  later.
- The cockpit moves to ktonpkg v0.6.0, which has the tool builder (draft API of 18.11.). It links v0.3.0 today;
  `plankton/spectrum` is already in the plankton v0.3.0 it links.
- SPEC gains a section for the `tool` operator commands, the `tools` config block (under the ceiling,
  off when absent), the class resolution order (§7.5), the `qualifies-as` template (§8) and the reuse
  gate. Every statement names its test.
- The licence rule becomes a tested property: no hidden file appears in a record, material, a commit or
  any output.

## Steps

| step | content | needs |
|---|---|---|
| T0 | this ADR accepted | MH |
| T1 | `cockpit/module` registry; `install` hands profiles to modules; `tool` module skeleton | — |
| T2 | `tools` config + class resolution by program; `tool install` on the git/local site | ktonpkg v0.6.0 (`BuildTool`, `ToolProblems`), PR #98 |
| T3 | hidden configuration as a read-only mount; the licence tests | T2 |
| T4 | `qualifies-as` template; `tool qualify` (members → fotons → `SpectrumCheck` → claim) | T2 |
| T5 | reuse gate in the shim and `ask` | T4 |
| T6 | jam-r through T2–T4 on git, compared with 30.09. (same spectrum, L1) | T4, the jam-r package (P4) |

## Open

- Whether `qualify` should also run the member's IQ checks (version, licence present, health) as members
  of their own, or the package models them as members. The latter is preferred: one mechanism.
- How an L2 tolerance result is recorded, once `plankton/spectrum` reports one: as the `level` field, or
  as its own claim.
