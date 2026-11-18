---
title: "Tool packages as the first module: install a tool for a program, qualify it, gate reuse on it"
type: "decision"
project: "kton-cockpit"
date: 2026-11-18
status: "accepted"
tags: [architecture, module, package, tool, environment, qualification, reuse]
---

# ADR-015: Tool packages as the first module

## Status

Accepted (MH, 2026-11-18, after the four changes of his review on PR #132; ADR-011 accepted with it).
Requested by kton-coordination on MH's decision of 18.11. (plan
`kton-coordination/proposals/2026-11-18-tq-packages.md`, phase P2). The decision covers three things.
It specifies the module door that ADR-011 section 5 left open "until the first module moves". It makes ktonpkg's
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

### 1 The module door (ADR-011 section 5, specified)

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
as ADR-011 section 5 says. It never opens a registry and never signs anything itself. Operator commands are
CLI only. An agent cannot install or qualify a tool, because both decide what later counts as evidence.
A verb would need ADR-009's route: SPEC, CLI, MCP and Go together, under the configuration ceiling.
That is not taken here.

The public binary links the `tool` module. A build without it behaves exactly as today.

### 2 `cockpit tool install <package>`: one class per program

`tool install` installs a `profile/tool` package as `install` does: it verifies the package
(`Revision.ToolProblems` must be empty), installs it, and says so in a signed installation claim. The
package names the programs it serves (`tool.json` `programs`, for example `Rscript` or `nmfe75`).

**The binding is derived, not stored.** The binding is the program, package, revision, `class` and
`envRef`. `class` is the id of the package's spectrum (`Revision.Spectrum().ID()`) and `envRef` is its
`image`, by digest. All of it is read from the installation claim and the sealed revision it names.
Nothing is written to the config and no operator types anything in, so nothing can drift from the
package. One Go function in kton-cockpit, `tool.Binding(cfg, program)`, answers the lookup for both
backends. It replaces `repo.improve.tools.<prog>`.

**Upgrading and removing are claims too.** Claims are never removed, and nothing is superseded
implicitly (MH, 18.11.). Installing a new revision says a new installation claim that names the one it
replaces (`replaces sha256:…`); `cockpit tool uninstall` says an uninstall claim about the installation.
The binding is read only from installations that are neither replaced nor uninstalled. Two such
installations for one program are an error until one is replaced or uninstalled; the cockpit does not
pick between them.

- A publish resolves its class in this order:
  1. the call's `environment` (PR #98);
  2. the binding of the program the command starts with (`argv[0]`, or the shim's program);
  3. `environment.spectrum`.
  The cockpit says which one it took. With `execution` enabled, a call that names a class different from
  the binding's is refused, as PR #98 refuses one against the repo's class. `argv[0]` serves shims and the
  recorder; a plain publish through a wrapper (`bash run.sh`) names its class with `--environment`.
- The improve defaults a package carries (run-server name, instance) are ignored by every site except
  improve's (MH, D2).
- **Site facts are split by what they describe.**
  - Facts about a run's **environment** (the digest an image tag resolved to) travel as `kton-env/v1` env
    material on the foton: signed by the producer (`foton.SignEnv`), read with `foton.ReadEnv`, outside
    every identity (ADR-010 rule 1). `qualifies-as` (section 4) is about that carried `envRef`.
  - Facts about **improve** (run server, instance, step, tree) are nekton claims, with two built-in
    templates: `runs-on` about a tool installation (where improve runs it), and `located-at` about a
    published record (the improve step and tree it came from). Carried material does not travel on a
    mirror yet (kton #225), so what must survive a mirror is a claim.

### 3 The `ToolSite` port: where a tool lives

```go
type ToolSite interface {
    // Install makes the tool available here. Adopt (the default) uses a matching environment that is
    // already there; Create makes one, and a site may refuse it (improve: it needs an administrator).
    Install(ctx, pkg ToolPackage, mode InstallMode) (Installed, error)
    // Locate says where one run goes: the envRef as the site will run it, and whether that still is
    // the package's digest. Hidden configuration is passed through by name, opaque to the port.
    Locate(ctx, b Binding) (Location, error)
}

type Location struct {
    EnvRef  string   // what the site will run, resolved to a digest where it can be
    Drifted string   // non-empty: why EnvRef is not the package's image (e.g. a tag now on another digest)
    Hidden  []string // the names of hidden configuration this class needs (e.g. "licence")
}
```

A drifted location is refused for `qualify` and for a run in the class, with the reason. improve holds
images by tag, so its site reports drift where the git/local site, which pulls by digest, cannot drift.

**Paths** follow the core round's layout (`kton-coordination/proposals/2026-11-18-core-change.md`): on
git/local, everything kton, installed packages included, is under `.kton/`, and private things (a licence,
a key) are outside the repository. This ADR does not fix the paths.

**Hidden configuration** is named by the package and supplied by the site, in the site's own way. The
port sees only names. On the **git/local site** it is a **read-only mount** from the operator's
configuration directory (outside the repository), at the target the package declares.
On **improve** it is a setting of the call frame. Either way it is not an input, not hashed, not material,
not committed, and never printed. A site refuses to run if it cannot supply it. The **improve site** is
implemented in kton-cockpit-improve (plan phase P3).

### 4 `cockpit tool qualify <program|package>`: evidence, then a signed claim

1. For each member of the package's spectrum: run the member's inputs in the bound environment, through
   the executor (ADR-011's seam, so a site needs nothing extra for qualification), with the package's command frame, and publish the run as a foton of the class. The
   package's reference runs are added to the registry first, so they can be compared against.
2. Check the candidates against the spectrum: `Revision.SpectrumCheck` (ktonpkg ≥ v0.6.0), which is
   `kton.dev/plankton/spectrum.Check` under the spectrum's normaliser. Counter-cases are run and
   reported, but are not members. `Revision.Coverage` gives each requirement its EARL outcome, as input
   for the report.
3. If the outcome is `fulfilled`, say **`qualifies-as`** about the `envRef`. The fields are the `class`, the
   package `revision`, the `level` reached (`L1` exact, `L2` within tolerance), and `evidence`, the
   member fotons. The claim is signed with
   `--as <person>`: qualification is a person's signature with a meaning (Part 11), not the repo's.
   If a member fails, nothing is said, and the report lists the failure.

**Tolerances (L2)** live in the package's spectrum, as its normaliser's tolerances (provisional values
until the business review corrects them). A change of tolerance is a new package revision, not a cockpit
change. NONMEM needs this from the start.

**IQ checks** (version, licence present, health) are members of the package's spectrum, run like any
other member: one mechanism.

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
- SPEC gains a section for the `tool` operator commands, the derived binding, the class resolution order (SPEC §7.5), the built-in templates `qualifies-as`, `runs-on` and `located-at` (SPEC §8), installation, replacement and uninstall claims, and the reuse
  gate. Every statement names its test.
- The licence rule becomes a tested property: no hidden file appears in a record, material, a commit or
  any output.

## Steps

| step | content | needs |
|---|---|---|
| T0 | this ADR accepted | MH |
| T1 | `cockpit/module` registry; `install` hands profiles to modules; `tool` module skeleton | — |
| T2 | the derived binding (`tool.Binding`) over unreplaced installations + class resolution by program; `tool install` (with `replaces`) and `tool uninstall` on the git/local site; `runs-on`/`located-at` templates | ktonpkg v0.6.0 (`BuildTool`, `ToolProblems`), PR #98 |
| T3 | hidden configuration as a read-only mount; the licence tests | T2 |
| T4 | `qualifies-as` template; `tool qualify` (members → fotons → `SpectrumCheck` → claim) | T2 |
| T5 | reuse gate in the shim and `ask` | T4 |
| T6 | jam-r through T2–T4 on git, compared with 30.09. (same spectrum, L1) | T4, the jam-r package (P4) |

## Decided in review (MH, 18.11.)

- IQ checks are members of the package (section 4).
- An L2 result is the `level` field of `qualifies-as`, with the tolerances in the package's spectrum
  (section 4).
