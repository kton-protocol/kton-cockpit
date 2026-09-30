---
title: "An API first, backends behind an interface, and improve as the third one"
type: "decision"
project: "kton-cockpit"
date: 2026-09-30
status: "proposed"
tags: [architecture, api, backend, improve, guard, potentials, packages]
---

# ADR-005: API first, three backends, potentials in the cockpit

## Status

Proposed (2026-09-30), on `jam-beta`. Nothing here is implemented yet.

## Context

Three things are wanted that the current shape cannot carry.

1. **A third backend.** Next to `git` and `local` (ADR-004), a cockpit working against an
   **improve** repository — where execution already happens inside the repository, and a version of
   an entity has a long, stable id.
2. **Potentials, rays and spectra** as first-class material, for both git and improve.
3. **Use from R, Go and TypeScript**, the way improvego is used — not only as a binary.

### What the code looks like today

- **There is no transport-neutral API.** `tools.Publish`, `tools.Say` and `tools.Ask` have the MCP
  SDK's handler signature, and a refusal is an `mcp.CallToolResult{IsError: true}`
  (`internal/tools/publish.go`, `errResult`). The command line calls those MCP handlers
  (`cmd/cockpit/main.go`, `callVerb`), and `jam-beta`'s `cockpit run` is a third caller of the same
  MCP-shaped function. "One implementation, two transports" holds only because the command line
  imports the MCP handler.
- **git is spread, not abstracted.** The binding check (`internal/config`), commit and push and the
  `raw.githubusercontent.com` permalink (`internal/gitops`), the two commits in `publish`, the
  registry commit in `say` and `scope`, and `init`/`doctor` each assume git. The only mode
  abstraction is `CommitEnabled`/`PushEnabled` switching off in local mode. `jam-beta` adds
  `gitops.PushFailed`, a git error in the verb's own semantics.
- **Locators ride only on fotons**, in `FileSpec.URI`: carried in the signed record, not covered by
  the foton id (kton §6.1). Claims carry none.
- **The kernel is vendored at `52b49f0`**, thirteen commits behind kton `main` (v0.2.1). Since
  0.2.1 the three modules are tagged separately and no longer need `replace`.

## Decision

### 1 The order: API, then command line, then MCP

A package `cockpit`, importable, with exactly the three verbs:

```go
package cockpit

// A Cockpit knows only where it starts — never the configuration. Every call re-resolves the
// configuration and re-checks the binding (SPEC §5); nothing is cached across calls.
type Cockpit struct{ start Start }

type Start struct {
    Dir string // the starting directory; COCKPIT_REPO_DIR replaces it
}

func New(s Start) *Cockpit

func (c *Cockpit) Publish(ctx context.Context, r PublishRequest) (*PublishResult, error)
func (c *Cockpit) Say(ctx context.Context, r SayRequest) (*SayResult, error)
func (c *Cockpit) Ask(ctx context.Context, r AskRequest) (*AskResult, error)

// A refusal is an answer, not a crash. The command line maps it to exit 2, anything else to exit 1.
type Refusal struct {
    Code   string // stable and machine-readable: "binding.remote-mismatch", "template.not-allowed", …
    Clause string // "SPEC §5.2"
    Reason string
}
```

- Request and result types end up carrying **no** `jsonschema` tags and no `omitempty` added only
  to satisfy the SDK's output-schema check (S7). They are plain, JSON-serialisable Go values,
  because every other binding (below) crosses a boundary as JSON.
- Operator commands (`init`, `doctor`, `scope`, `run`, `keygen`, `show`) stay outside the three
  verbs, in their own package, and are never reachable over MCP (SPEC §6).
- **MCP comes last.** No MCP-specific work happens before the API and the command line are
  settled. MCP is not detached in the meantime, though: keeping it is a thirty-line adapter
  (`internal/mcpsurface`) that turns each cockpit method into a tool handler, which costs less than
  detaching it would, and keeps SPEC §6 ("every transport reaches the same implementation") true
  throughout. Its tool schemas are still inferred from the request and result types, so those keep
  their `jsonschema` tags until S7 moves the schema into the adapter.

#### Use from R, Go and TypeScript

The same pattern improvego already follows for `kton.dev/plankton/core` (libkton): one Go
implementation, exposed through a thin C ABI with a `{value | error}` JSON envelope.

| consumer | route |
|---|---|
| Go | import `github.com/kton-protocol/kton-cockpit/cockpit` |
| R | `libcockpit` built with `-buildmode=c-shared`, called via `.Call` |
| TypeScript (Node, VS Code) | the same `libcockpit` through a Node FFI or an N-API addon |
| any language, no FFI | the command line with `--json`, same request/result types |

A browser WASM build, as improvego ships for the pure kernel, does **not** fit the cockpit as a
whole: the verbs touch a filesystem, git, a container runtime or an improve server. Only the
read-and-verify half of `ask` could run there. That is left out until someone needs it.

A `Refusal` crosses every boundary as `{"error": {"code", "clause", "reason"}}`, so an R or
TypeScript caller can tell a refusal from a failure exactly as the command line does.

### 2 A backend interface instead of a mode flag

```go
package backend

type Backend interface {
    Binder   // the anti-wrong-folder guard: two independent sources must agree
    Store    // replaces gitops.CommitAndPush
    Locator  // replaces PermalinkBase / LocatedFlags
    Executor // realises a potential; the kernel never does (kton §6.4)
}

type Binder interface {
    Bind(ctx context.Context, cfg *config.Config) (Binding, error) // *cockpit.Refusal on mismatch
}
type Store interface {
    Persist(ctx context.Context, b Binding, files []string, msg string) (Revision, error)
}
type Locator interface {
    Locate(rev Revision, path string) (uri string, ok bool)
}
type Executor interface {
    Realize(ctx context.Context, p Potential, b Bindings) (Realization, error)
}

type Revision struct {
    Backend string            // "git" | "local" | "improve"
    ID      string            // a commit sha, or ""
    Per     map[string]string // improve: path → entity version id
    Stored  bool              // false: written locally, not yet durable elsewhere (jam-beta's PushFailed)
}
```

| | git | local | improve |
|---|---|---|---|
| **Bind** | config owner/name ↔ `git remote get-url origin` | config `root` ↔ where it actually is | see "The guard in improve mode" below |
| **Persist** | commit + push → sha | nothing | the repository's own check-in → entity version ids |
| **Locate** | `https://raw.githubusercontent.com/<o>/<n>/<sha>/<path>` | none | `improve://<host>/<entityVersionId>` |
| **Realize** | pinned container (ADR-003) | pinned container | the improve run server, via improvego |

`IsLocal()` disappears from config, `init` and `doctor` in favour of `backend.For(cfg.Repo.Mode)`.

### 3 The guard in improve mode

#### What the guard needs, stated as requirements

SPEC §5 and ADR-004 together ask five things of any binding, whatever the backend:

1. **One place for the configuration.** It is found where the backend says, never by an open
   search upwards (SPEC §5.1).
2. **The configuration says what it belongs to.**
3. **A second source that the configuration did not write agrees with it.** For git that is the
   remote, written by `git clone`. For local mode it is the file system's answer to "where is this
   file", which the configuration cannot fake.
4. **Every call checks again.** No cached verdict (SPEC §5).
5. **A mismatch refuses, and the limits are named.** Neither mode catches everything, and
   SPEC §5.4 says which case each one misses.

#### Applied to improve

In improve mode the thing that can be wrong is no longer a directory, but **which improve server and
which folder in it** the cockpit writes to. improvego works without a working copy, so there may be
no local directory at all. The shape that fits is local mode's, moved to the server:

```json
"repo": { "mode": "improve", "server": "https://demo1.example.org", "root": "server:FO-1234" }
```

- The configuration lives **in the improve root folder**, as `cockpit.config.json`.
- It declares its own `server` and `root` (the root folder's entity id).
- On every call the cockpit asks the server — the second source — for the folder that actually
  contains that file, and refuses unless that folder's entity id equals the declared `root` and
  the server answering is the declared `server`.
- A local copy of the configuration, when there is one (running from a directory), must be
  byte-identical to the server's copy, compared by hash. That keeps sibling directories pointing at
  different improve folders from being confused, which is the original incident.
- An unreachable server refuses. The guard fails closed.

What it catches: a folder that was copied inside improve (the copy's config still names the old
root), a wrong server, a stale local copy. **What it does not catch:** a config edited by hand to
declare the new location after a copy. That is the same deliberate re-confirmation local mode
already accepts. SPEC §5.4 gains an improve row.

This needs nothing from improvego that is not already there: `ResolveID` and reading a file's
content. It costs two REST calls per verb.

Once improve V5 ships check-out, the check-out's own metadata is a third source, the exact analogue
of a git remote, and should be added then.

### 4 Locators and "commit and push" in improve mode

- **The entity version id replaces the permalink.** `FileSpec.URI = ["improve://<host>/<entityVersionId>"]`,
  for example `improve://demo1.example.org/server:ST-63657-1`. Identity stays the content hash. The
  version id is where the bytes are, not what they are. Nothing has to trust that a version id never
  changes, because the hash is checked on every fetch. The resolver for `improve:` is on the
  cockpit side, since `kton fetch` knows only http(s) and file.
- **There is no separate commit and push.** In improve, a run is `INITIAL → RUNNING → CHECKIN →
  FINISHED`. After check-in its outputs *are* versioned in the repository. `publish` in improve mode
  runs a step (or takes one that already ran), builds the foton from it (improvego `FotonOfStep`),
  signs and registers it, and writes the changed registry files back. The registry already lives as
  files in improve (improvego `store.go`).
- Writing the registry back is the one place a conflict can happen, like a rejected push. Until
  V5's check-in/check-out transaction exists, an open revision transaction is detected and refused
  (`store.transaction-open`), never overwritten.
- Gap in improvego: `PutContent` does not return the new version id, so a write is followed by a
  read. Worth fixing in improvego rather than working around.

### 5 Potentials, rays, spectra — without a fourth verb

SPEC §13 stays: three verbs. Everything maps onto them.

| concept | where its identity comes from | in the cockpit |
|---|---|---|
| **potential** | the kernel: a foton with holes and/or virtual outputs, with an id but no action key (kton §6.4) | `publish {kind: "potential"}` registers the definition |
| **realisation** | the kernel computes the realised foton; the backend's `Executor` runs it | `publish {from: <potential id>, bindings}` |
| **ray** | **the package format** (`kton-package/1`, kind `ray`), not the cockpit | `publish` a ray package; `ask {query: "ray"}` reads the subgraph |
| **spectrum** | the kernel's shape (kton §10) | `publish {kind: "spectrum"}`, `say qualifies-as`, `ask {query: "fulfils"}` |
| **reuse** | the kernel: action key plus `Registry.Reuse` | `ask {query: "reuse"}`; hits compete, they are not trusted |

The same potential has the same id in both backends, because only the kernel computes it. In git it
realises in the pinned container; in improve through `improvego.Realize`/`MakePlan` (run,
rerun-in-place or reuse, each with a stated reason).

**Rays go through the package format.** The kernel defines a ray only in its glossary and gives it
no identity. The cockpit does not invent one. A ray is a `kton-package/1` package of kind `ray`, and
its identity is the package's. That rule must exist exactly once: in `ktonpkg`, split out of
improvego into its own repository, which the cockpit and improvego both link.

### 6 One package format: `kton-package/1`

Four verticle formats exist today:

| | manifest | identity | role |
|---|---|---|---|
| R `improVerticles` 4.4.0 | `descriptor.json` (schema 1.0.0) | sha256 of the zip, name@semver | the running system |
| TypeScript `improve-verticles` | `descriptor.json` with `profile`, `bundle`, `accepts` | manifest root hash | prototype |
| Go `improvego` | `verticle.json` inside `kton-package/1` | foton id / `Bundle.ID()` | the proposed successor |
| Go `toolsrc` | `tool.json` + `members.json` | `Bundle.ID()` | build side of a tool with a spectrum |

The cockpit speaks **`kton-package/1` only**: `kton/package.json`, `kton/<entry>.json`,
`kton/claims/`, `kton/spectrum.json`. Other formats come in through **importers** (R
`descriptor.json` → a potential), never as a second identity rule. A verticle's `accepts` are the
holes of its potential.

The format questions themselves are settled in the `verticleformat` work, not here. Those questions
are the two Go paths to the same package, the disjoint `contextType` vocabularies, `:` versus `.`
in op names, R's typed `dependencies` with version ranges, and V-model documents inside
`Bundle.ID()`.

### 7 Templates: description, rationale, review

Templates are nekton JSON files in `templates_dir`, admitted by `claims.allowedTemplates`. No Go code
is needed, except where the cockpit determines a field itself, as it already does for `reproduces`.

| template | predicate | subject | bound in improve mode to |
|---|---|---|---|
| `describes` | `http://purl.org/dc/terms/description` | foton or potential | `Step.Description` / `Resource.Description` |
| `rationale` | `https://w3id.org/ep-plan#hasRationale` | foton or potential | `Step.Rationale` |
| `reviewed` | `https://scinteco.com/ns/qualification/v1#reviewed`, object accepted or rejected | the execution's claim id | the improve review (improvego `ReviewClaim`), with the review entry's hash as evidence |

"Bound" means that in improve mode the cockpit **reads the value from improve** and does not take the
caller's word for it, as with `reproduces`. In git mode the caller supplies the text.

**A review is signed twice.** The system key attests "improve recorded this decision". The
reviewer's own key co-signs, attesting "I decided this". nekton already unions co-signatures on the
same claim. A trust tier can then require either one or both.

### 8 Kernel

- **Move to kton `main` (v0.2.1)** by `require`, without `replace`, and re-vendor. The API change is
  one addition (`template.LoadAliases`). The rest are fixes. `reuse --json` renamed `verified` to
  `verification`.
- **Extensions to raise against the kernel**, as public proposals, not cockpit workarounds:
  - **K3.** Spectrum fulfilment as a library function (today only `cmd/plankton spectrum`, with
    unexported `fulfils`). Without it the cockpit would hold a second opinion on fulfilment, which
    SPEC §13 forbids.
  - **K4.** Un-reserve `FileRef.id` / `foton.FileSpec.ID`, the natural slot for a persistent id
    such as `server:ST-63657`.
  - **K6.** Move reuse-hit assembly (`declaredSigner`, `verification`) out of `cmd/` into the
    library.

## Steps

| step | content | behaviour changes? |
|---|---|---|
| S0 | kernel v0.2.1 by `require`, re-vendor, CI green | no |
| S1 | extract the `cockpit` API and `Refusal`; the command line and MCP on top; fix the citation in `main.go` of a test that does not exist | no |
| S2 | `backend` interface; git and local behind it; `PushFailed` becomes `Revision.Stored` | no |
| S3 | `ktonpkg` as its own repository; improvego on Go 1.25 with a real module path, no `replace`, same kernel | — |
| S4 | `backend/improve`: bind, persist, locate, realise; SPEC §5 gets an improve clause | new |
| S5 | potentials and realisation; templates `describes`, `rationale`, `reviewed` | new |
| S6 | rays and spectra on `ktonpkg` (spectra after K3); R importer | new |
| S7 | `libcockpit` (C ABI) for R and TypeScript; the MCP schema moves into the adapter | — |

## Settled on the way

1. **`rationale` uses EP-Plan's `hasRationale`.** Neither DCMI nor PROV-O has a term for "why this was
   done", and folding it into `dcterms:description` would make the two indistinguishable to a query
   by predicate. EP-Plan is a published PROV extension (`https://w3id.org/ep-plan`) whose
   `hasRationale` links a plan element to "some information on why such element was included in a
   plan" — which is what a step's rationale is.
2. **`ktonpkg` becomes its own repository**, depended on by both the cockpit and improvego, so the
   package and ray identity rule exists once and the cockpit does not depend on an improve client
   in git mode.
3. **improvego moves to Go 1.25**, the cockpit's version.

## Consequences

- Git and local repos behave exactly as before through S0 to S2. The refactor is visible only as a
  new importable package.
- The cockpit gains a dependency on improvego (S3). That is acceptable only if improvego builds
  against the same kernel version, which is checked in CI.
- Every refusal the cockpit had before S1 is still a refusal (exit 2), including failures underneath
  it — a kernel call, a commit — which carry codes such as `kernel` and `store` and no clause.
  Whether those should become plain errors (exit 1) is a behaviour change, left for when a caller
  needs the distinction.
