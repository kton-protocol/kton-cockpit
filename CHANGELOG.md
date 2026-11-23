# Changelog

## Unreleased

### Added

- **`cockpit reproduce <query run>`** ([ADR-020](docs/decisions/adr-020-reproducible-queries.md), part 2):
  reads the run's manifest, builds its dataset again from the registries over exactly the pinned ids (a claim
  signed since, or on another branch of a scope, is not in it), evaluates it with the pinned trust, view and
  engine (never pulled), and compares the result. A match signs the run's own record, so its
  `reproductions` count the reproducer, only when the run's signer and every key it rests on are trusted
  here (or with `--sign-anyway`); otherwise it is reproducible and not signed. Keys not trusted here, another vocabulary or
  other packages are said beside it. Refused, named: a missing record or byte, another manifest or projection version, an absent
  engine, a different result (its first differing row).

### Changed

- **A query run's manifest is version 2**: it pins the whole view it was evaluated with (signers' names,
  templates, alias files, row templates) as one blob, and the run is evaluated with it rather than with the
  repository's present, as a reproduction is. Runs recorded by v0.5.10 (version 1) are refused by
  `reproduce`, named.

## v0.5.10 — 2026-11-23

A faster view after a say, the index in the persona windows, and reproducible queries; each reviewed by
kton-review.

### Migrating from v0.5.9

- **Every view is built again once** after the update (the projection's version changed): the first query of
  each repository takes the build time; after that, a say rebuilds only what it changed.

### Added

- **Reproducible queries** ([ADR-020](docs/decisions/adr-020-reproducible-queries.md), part 1, #225):
  `ask sparql --reproducible` (MCP and serve: `reproducible`) pins what the query reads (each scope at its
  tips, every registry's record and claim ids, the trust keys, the vocabulary, the admitted packages, the
  projection's version, the engine), evaluates it over exactly that in a container of its own, and records the
  run as a `query/sparql` step with the query and the manifest in and the canonical result out, all in the
  content store. Refused: values of the moment (`NOW`, `RAND`, …), answers that depend on evaluation order
  (`SAMPLE`, `GROUP_CONCAT`, a `LIMIT` without a total order), blank nodes, results over the limit.
  `cockpit reproduce` follows in part 2.

### Fixed

- **A say no longer rebuilds the whole view** (#226): the view is replaced graph by graph, only the graphs
  whose content changed are uploaded, and the step records are copied only when they changed. One claim in a
  scope of a 11,500-claim view: the next query 36 s -> 9.6 s.
- **The index in the persona windows** (#223): a query container reads its dataset from under the checkout's
  `.kton/local/tmp` (a path the host behind a passed-in engine socket has), a started daemon has 3 minutes to
  answer, and `doctor` warns when the index uses 85% of its memory.

## v0.5.9 — 2026-11-21

A patch release: large federated views fit into the index again, and steps declared afterwards say so; each
reviewed by kton-review.

### Fixed

- **The view of a large federated union goes into the index in chunks** (#221): it was written as one SPARQL
  update, past Oxigraph's 128 MiB body limit, and every query of such a repository failed ("SPARQL body
  payloads are limited…"). The data now goes up through the graph store into staging graphs, and one small
  update puts them in place, so a reader still sees the old view or the new one whole. A build sweeps what a
  killed build left staged; a blank node in the view is refused rather than split.
- **When the index cannot be started and the query container fails too**, the refusal says why the index
  was not used.

### Added

- **Declared steps** (#220, MH 21.11.): `publish --declared hand|heuristic:<name>` for a step written or
  inferred afterwards rather than recorded as it ran. It is signed into the record (and is another record
  than the same step observed), never run, records no image it did not run in, and is no reproduction.
  Absent, its signer did not declare it. kton projects it as `nk:declared` from its next release.

## v0.5.8 — 2026-11-20

Claims in a graph per scope (ADR-018), serve events, and federation that cannot look whole while missing a
record; each reviewed by kton-review (#195, #203, #205-#215). Tracks kton v0.4.2 and ktonpkg v0.9.13.

### Migrating from v0.5.7

- **A federated claim keeps its own field names** where its predicate has no template here (#203): its
  fields read `lab:<field>`, not this repository's alias terms. To read another source's data through
  vocabulary terms, admit a package with its templates and aliases (as `qm.vocabulary` does for the qm
  scopes).
- **`GRAPH ?g`** in a claims query, with nothing else binding `?g`, now also reads the scope graphs, not only
  loaded files: say which you want (`?g a nk:LoadedGraph`, `?g a nk:ClaimGraph`).
- **Every view is built again once** after the update (the view's version changed): the first query of each
  repository takes the build time.
- **A view that acts on another package's control** installs only after that package, at the revision it pins
  (#206).
- **A load into the machine's shared index is bounded** (#215): its files together at most
  `index.derived.maxBytesShared`, 16 MiB by default. A repository loading more uses an index of its own
  (`COCKPIT_INDEX_DIR` and `index.memory`), or raises the limit.

### Added

- **Claims in a graph per scope** ([ADR-018](docs/decisions/adr-018-claims-in-a-graph-per-scope.md), #205,
  #207, #208, #210): each scope's claims in a named graph of their own and the unscoped claims in one more,
  described in the default graph (`nk:ClaimGraph`, `nk:scope`, `rdfs:label`, `nk:parent`, `nk:source`,
  `nk:claims`); the default graph is unchanged. `GRAPH ?g` over one scope is many times faster on large data.
  `ask --json` lists the claim graphs an answer read (`claimGraphs`) and the names more than one carries
  (`repeatedLabels`).
- **Row templates** (`"claims": "rows"`, ktonpkg v0.9.12): their claims say `nk:row true`, so "the latest per
  subject" leaves them out (`docs/sparql.md`, the latest claim per subject and template, #195).
- **Loaded graphs described:** `nk:LoadedGraph` with the output path of the trusted step that produced the
  file, `cockpit load --label` as `nk:loadedAs`, and `nk:loadedAt`.
- **A kton id bound as an IRI** (`--bind-iri X=sha256:<hex>`) is the node `https://kton.dev/o/<hex>`.
- **Acts on another package's control** (ktonpkg v0.9.13, #206): checked against the owner revision in use.
- **The shared index's memory floor** (#215): a `memory` file in the machine's index directory (e.g. `4g`) is
  the least every start of the shared daemon takes; `doctor` says what it runs with and where that came from.
- **serve events** (SPEC §12.5, #213): with `initialize` `events: true` or `{since, intervalMs}`, a frontend
  gets `events/published`, `events/said` (each claim with its scope, template, predicate, subject and
  signers), `events/sourcePulled` and `events/indexMoved` (after the index holds them; the view and the
  scopes that changed). The cursor is the feed's, so a restarted `serve` resumes where it was.

### Fixed

- **A federation source with a record or a store line it cannot load fails the union**, naming the source
  (#200, #214; kton v0.4.2 `Registry.Unreadable`).
- **Aliases rename only the fields of templates this repository holds** (#203).
- **The view build parses each claim once** (#210): about 15% faster than with the scope graphs as first
  merged.
- **A changed file on the load list is not hashed before every query** while its size and modification time
  stay as marked (#211).
- **serve:** a body that is not JSON is answered `-32700` and the connection goes on; lost framing ends it
  after answering what runs (SPEC §12.5, #212).
- Tests that need a later signed second wait for it (#209).

## v0.5.7 — 2026-11-20

Derived RDF in the index, and federation that scales, each reviewed by kton-review. Tracks kton v0.4.1 and
ktonpkg v0.9.11.

### Migrating from v0.5.6

- **ktonpkg v0.9.11:** a package's area `scopes` are labels. A package admitted with an IRI there fails its
  check after the update; rebuild it with the scope's name.

### Added

- **`cockpit load <file|sha256>... [--as TYPE]`** (SPEC §12.6,
  [ADR-017](docs/decisions/adr-017-derived-rdf-in-graphs-per-hash.md)): a file's RDF into the index as a named
  graph named by its hash, `https://kton.dev/o/<hex>`, for this repository's queries. Off unless
  `index.derived.enabled` (with `index.kind oxigraph`); limits `index.derived.maxBytes` (64 MiB per file) and
  `maxTriples` (50 million per repository). Nothing is signed: the checkout keeps an unsigned load list and the
  index loads it again after a rebuild. A query's named graphs are exactly this repository's loaded graphs; a
  load never takes a record's graph name (`derived.clash`). `--list`, `--drop <sha256>`.
- **ktonpkg v0.9.11:** ui entries carry their `scopes` (`records`).

### Fixed

- **A federated repository no longer hangs:** `say`, `federation add` and the first ask after a write rebuild
  the union with one `Registry.AddAll` per source instead of one add per claim (1,500 + 1,500 claims, one say:
  22 s to 1.5 s; `federation add` of 11,500 claims: 7 s, before it did not finish).
- **A record a federation source cannot load fails the union** instead of going missing from it: the union reads
  every record on disk and judges it as the registry does.
- **`index.url` keeps its view:** a query no longer rebuilds the view each time (25–37 s per ask before). One
  machine per store reached by URL.
- **Example 10 runs offline:** it ships an extract of the Vienna tree register instead of downloading it.

### Extension

- The workbench's StdioTransport over `cockpit serve --stdio` (#196); the command line stays the default.

## v0.5.6 — 2026-11-20

The frontend protocol's first part, many claims in one write, and the fixes the dry runs found, each reviewed by
kton-review. Tracks kton v0.4.1 and ktonpkg v0.9.9. Nothing to migrate from v0.5.5.

### Added

- **`cockpit serve --stdio`** (SPEC §12.5, ADR-011 section 6): the frontend protocol, JSON-RPC 2.0 over stdio with
  language-server framing, for an editor extension. `initialize` names the workspace folder (refused when the
  configuration does not bind it, or `COCKPIT_REPO_DIR` would redirect it) and optionally the identity every `say`
  signs as. Methods: `publish`, `say`, `ask`, `say/templates`, `ask/questions`, `ui/list`, `ui/cat`,
  `show/snapshot`, `doctor`, `version`, each answering what the command line's `--json` answers. Refusals are
  error `-32001` carrying `{code, clause, reason}`; `$/cancelRequest`; `shutdown` answers every request it accepted
  first. Events and operator commands follow in later parts.
- **`say --batch <file.jsonl | ->`** and the Go API's `SayBatch` (SPEC §8.9): many claims in one write. Every claim
  is checked before any is signed (a refusal names the claim and nothing is written); claims into one scope chain
  in order; `replace: previous` and `expectHead` work within the batch; one store open (kton's `Registry.AddAll`),
  one commit, one confirmation; each claim anchored where the repository anchors. 16,600 claims in one scope in
  about 16 s (one `say` per claim: about 23 h).

### Changed

- **The template ceiling is by origin** (SPEC §8.1): a repository template is allowed only when
  `claims.allowedTemplates` names it, a package's template when its package is admitted; a package template of a
  repository template's name no longer opens the ceiling for it. The listings (`say --json`, `ask --json`, the
  overview, run status) show exactly the templates `say` signs with, by the name it takes.
- **`bind` and `bindTerms` reach sub-selects** (SPEC §9.9): the `VALUES` clause goes first into every sub-select's
  group too; a brace inside a `SELECT`'s expressions (`EXISTS { }`) is not taken for its group.
- **kton v0.4.1**: nekton, plankton and kton pinned to the tags (`Registry.AddAll`, the Windows `.seq` retry).

### Fixed

- **Concurrent writes no longer fork a scope** (SPEC §8.5): `say`, `install` and `scope seal` hold a claims lock
  in `.kton/local/` from reading the head until the claim is stored, across processes.
- **`scope seed` opens a name once**: a second seed of a name this repository opened is refused (`scope.exists`),
  and a seed whose commit fails names its id.
- **`doctor`** no longer reports an admitted package's templates as missing, and says once when the vocabulary
  cannot be read.
- `say --batch` takes `--json`.

### Extension

- The generic QM area (userinterface's PR 3, #187), named-query rows and the aliases member (#184).

## v0.5.5 — 2026-11-19

Package aliases scoped to their own package, several members per `ui cat`, and `say` held to the
template a person was shown, each reviewed by kton-review. Tracks kton v0.4.0 and ktonpkg v0.9.9.

### Migrating from v0.5.4

- **The repository's alias file is read from `.kton/aliases.json`**, as SPEC has said since v0.5.0.
  Earlier releases read `aliases.json` at the repository root and silently ignored `.kton/aliases.json`.
  A repository that keeps it at the root has to move it: until then `say` refuses (`vocabulary`) and
  `doctor` reports it as blocking.

### Added

- **`say --expect-template`, `--expect-revision`, `--expect-predicate`** (SPEC §8.5): `say` refuses
  (`template.changed`) when the template it would sign under is not the one a person was shown. The
  hash covers the template with its predicate and context resolved, and the alias file. `say --json`
  lists each template's resolved predicate, `context`, `hash` and the package `revision` it comes from.
- **`ui cat <package> <hash>... --json`** (SPEC §6): several members in one call, as
  `{package, name, dir, members: [{hash, bytes}]}`; one hash that is not a member refuses the whole call.

### Changed

- **A package's aliases resolve only its own templates** (SPEC §9.11): its terms and prefixes no longer
  reach the repository's templates or another package's. The view resolves a package's fields through its
  own aliases first, then the repository's; view version 6.
- **One predicate, one template**: when two templates resolve to the same predicate, the repository's
  wins, then packages in `claims.allowedPackages` order; the later one is not offered and `doctor` names
  the clash. A template whose predicate or context is not an IRI is not offered either.
- **`ui list` gives members only for usable packages**; an admitted but unusable package carries
  `members: null`.
- **ktonpkg v0.9.9**: a member name that climbs out of the package (`..`) is refused when reading and
  when writing; such a package is not usable.

## v0.5.4 — 2026-11-19

What the QM Workbench's PR 2 needs to open a composition from a real install, each reviewed by
kton-review. Tracks kton v0.4.0 and ktonpkg v0.9.7. Nothing to migrate from v0.5.3.

### Added

- **`ui list` names every member by hash** (SPEC §6): each package carries `members`
  `[{role, name, hash}]`, its members' content hashes as in their isPartOf claims (`null` when the package
  is not usable); each composition carries its `hash`; a ui entry's `file` is `ui/<name>.json`.
- **`ui cat <package> <hash>`** (SPEC §6): a member's bytes on stdout, so a shell reads no package files
  itself. The package is named by its name, dir or id, among usable packages only (admitted, the latest
  revision, passing its check); the bytes come through ktonpkg's `MemberBytes`. Refused `ui.package` for a
  package that is not usable or a reference that names two, `ui.member` for a hash that is not a member.

### Changed

- **The view aliases a nested object's fields** as it does a top-level field's (SPEC §9.11); view version 5.
- **ktonpkg v0.9.7**: package check reasons are in English.

### Fixed

- **A bound IRI has at most one fragment** (SPEC §9.9): `--bind-iri` and `bindTerms` refuse a second `#`.

## v0.5.3 — 2026-11-19

What the QM Workbench's PR 2 and PR 3 and the C3 areas need, each reviewed by kton-review. Tracks kton
v0.4.0 and ktonpkg v0.9.5. Nothing to migrate from v0.5.2.

### Added

- **Typed bind** (SPEC §9.9): `bindTerms` (JSON, `cockpit_ask`), `--bind-iri VAR=IRI` and
  `--bind-typed VAR=VALUE^^xsd:integer` bind a sparql variable to an IRI or a typed literal. Each is
  one RDF term, checked (an absolute IRI with nothing that ends it, `%HH` only; an xsd lexical form),
  never query text.
- **`show --json`** (SPEC §12.3): the snapshot a viewer fetches, `{union, keys, names, packages}`, on
  stdout.
- **Admitted packages' build keys, labelled** (SPEC §12.3): `/data/packages.json` and `show --json`
  carry `{keyid: {key, packages: [{package, id, dir}]}}` for the keys admitted packages' seals verify
  under. A label, never trust: never a declared keyid, never a key of the trusted ring (doctor names
  such a collision), never a package failing its check.
- **`say` fills `replaces` for `replace: previous`** (SPEC §8, ADR-013 A1/A5): the signing key's own
  newest unreplaced claim of that template about the subject, verified by signature, never another
  key's; the answer names it.

### Changed

- **The vocabulary is per package** (SPEC §8.1): an admitted package that fails its check, or a name two
  sources bring, takes out only itself. A clashing name is offered as `<package>/<name>`; two admitted
  packages of one name are both refused, so a qualified name cannot be spoofed; a template declares the
  name it is filed under; of several installed revisions the latest counts. doctor names what was left
  out.
- **ktonpkg v0.9.5** (composition layout, area approvals/history).
- **`@` is reserved for install scopes** (SPEC §9.6): `cockpit scope seed` refuses a name with `@`.

### Fixed

- **A write commits only its own staged files** (SPEC §12.1): a write whose commit fails unstages its
  files, and every commit names exactly the files staged under its own paths, renames included, so no
  write commits another's leftovers. An extract whose commit fails leaves no folder, so a retry works
  (kton-review R1, S1).

## v0.5.2 — 2026-11-19

What the QM Workbench shell (userinterface) needs for its PR 2, kton-review's findings on it, and
the advisory CI job. Tracks kton v0.4.0 and ktonpkg v0.9.1. Nothing to migrate from v0.5.1.

### Added

- **`cockpit ui list [--json]`** (SPEC §6): the installed packages for a frontend, each with its
  name, package id, revision, label, profile, whether the configuration admits it, and what fails its
  check under `refused` (code `package.check`), listed rather than hidden. A frontend reads what
  packages declare from here, never from package folders: a verticle's `ui` entries, blocks and
  compositions as ktonpkg v0.9.1 reads them, each under its file name: an empty list when it declares
  none, null when the package is not admitted, fails its check or cannot be read, so a frontend uses
  only what is listed (kton-review V1). `ui` is a reserved core command name.
- **Install checks a viewer the package carries** (ktonpkg v0.9.1, `viewers/<viewer>/`): its tree id
  against the pin, its `viewer.json`, and that every block on it emits and takes only what
  `viewer.json` declares. A viewer pinned to a kton-web commit is not fetched and checked yet.
- **Every claim key in `doctor --json`** (SPEC §6): `fields.identity.keys` lists the checkout's own
  claim key and each key of the keyring, with its key id from the seed, whether the `.pub` beside it
  matches, and its trust tier.

### Fixed

- **A locked repository is `store.busy`, not `store`** (SPEC §12.1). A write that still meets git's
  lock after the 30 s wait gets its own code. What exists afterwards decides the answer: a write that
  met the lock before anything was signed (publish's first commit, install, workflow) is refused
  `store.busy` and can be tried again; a write whose record or claim is already signed and stored
  (say, publish's registry commit, a ray's claim) is not refused but answers with the id and
  `pending: {code: store.busy}`, and the next write commits it, so a retry never signs twice
  (kton-review B1).
- **A broken package directory nobody admitted takes nothing away** (kton-review U2): it no longer
  makes every package template refuse. An admitted package that fails its check contributes nothing.
- **The shell form of `say` takes an IRI subject**, as every other form does (SPEC §8).
- **CI against kton main fetches the kton modules from git**, not the module proxy, which kept
  answering `@main` with the commit before the v0.4.0 release.

## v0.5.1 — 2026-11-19

A fix found by userinterface on v0.5.0, and the module door of ADR-015. Tracks kton v0.4.0 and
ktonpkg v0.8.0. Nothing to migrate from v0.5.0.

### Added

- **Modules** (ADR-015, step T1). A module registers itself with `cockpit/module`: its commands,
  run as `cockpit <module> <command>` (on the command line only, never over MCP), the package
  profiles it handles, and the templates and queries it brings. `claims.allowedModules` admits a
  module's words as `claims.allowedPackages` admits a package's (SPEC §8.1). `cockpit install`
  refuses a package of a profile a module handles and names the module's command
  (code `package.module`). The first module is `tool`, a skeleton for tool packages:
  `cockpit tool install <dir>`.

### Fixed

- **`say` resolves its subject in every form** (SPEC §8). With a JSON argument or over MCP,
  `"subject": "data.txt"` was signed as the literal text, so the claim was missing from
  `ask about data.txt` and from the view. Now a `sha256:` id (read in lower case), an IRI, or a file
  in the repository, which stands for its bytes and is reported back (`resolvedFrom`, `subject`).
  Anything else is refused with code `argument`.
- **A file is read where it resolves, and only if it is a regular file** (kton-review S1, S2 on
  #148; #150). `say` takes a file subject only inside the repository: a symbolic link to a file
  outside it is refused, never hashed into a shared claim. `say`, `ask` and the shell's references
  read a file through one resolver that refuses a directory, a FIFO or a device before opening it,
  so a read never hangs. `ask`, which signs nothing, may still read a regular file outside the
  repository.

### Documentation

- **ADR-016 is accepted** (MH, 19.11.), with its addition for the jobs runner: the journal under
  `.kton/local/jobs/`, machine configuration under `~/.config/kton-cockpit/`.

## v0.5.0 — 2026-11-19

Everything kton in a repository is under `.kton/` (ADR-016), the query view is vocabulary 1
(ADR-013 A5), and the kernel is kton v0.4.0. Tracks kton v0.4.0 and ktonpkg v0.8.0; ships with
recorder 0.5.0.

### Upgrading from 0.4

This is a hard break with no migration (ADR-016): a repository is set up again with `cockpit init`,
and a 0.4 store layout is not read.

- **The `.kton/` layout** (SPEC §5.6). `.kton/cockpit.config.json` marks and configures the
  repository; the stores are `.kton/plankton` and `.kton/nekton`, the templates `.kton/templates`,
  installed packages `.kton/packages`, blobs `.kton/blobs`. A trust tier is a folder of public keys,
  `.kton/keys/<tier>/*.pub`, and `trust.tiers` is a list of tier names. The checkout's identity is
  `.kton/local/identity.json` (session, keys, keyring), which is never committed, persisted,
  published or mounted into a container; private keys live outside the repository, in
  `~/.kton-keys` (or `KTON_KEYS`). Run folders are `runs.dir`, default `work/`.
- **kton v0.4.0.** Every nekton store has a root scope: a scope seeded without a named parent hangs
  under it, so `ask scope` names the root as parent and as the last entry of the context. kton 0.2
  stores are refused. Content IRIs are lower case.
- **View vocabulary 1** (SPEC §9.11). `nk:current` and `nk:supersededBy` are gone: nothing is
  inferred from order. A claim names what it replaces: `nk:replaces` for every name in its
  `replaces` field, and `nk:replacedBy` only where every signer of the replaced claim signed the
  replacement, with the same template and a shared subject. A query that asked `nk:current true`
  asks `FILTER NOT EXISTS { ?c nk:replacedBy ?newer }` where replacement is meant, or orders by
  `dct:created`. Every subject is an `nk:about`; a seed has `nk:parent`.
- **Recorder 0.5.0** reads only the new layout: update it together with the cockpit.

### Added

- **One JSON document on stdout for a shell** (SPEC §6). Under `--json`, or with a verb's JSON
  argument, a refusal is `{code, clause, reason}` with exit 2 and any other error the same object
  with code `error` and exit 1. `--json` for `version`, `say` and `ask` without a question (the
  templates with their fields, the questions with a package query's parameters), `trust list`,
  `federation list`, `config`, `index status`, `sync` and `run list`. `doctor --json` names who the
  checkout signs as (`fields.identity`: name, key ids, tier). Asked by userinterface for the QM
  Workbench shell.
- **`iri` fields are IRIs in the view** for `urn:`, `tag:`, `did:`, `mailto:` and a resolved CURIE.

### Fixed

- **`say` with a JSON argument, and over MCP, fills `by-session`** from the identity it signs as,
  as the typed form did (SPEC §8.7).
- **What a file or run folder resolved to goes to stderr**, so `--json` is all of stdout.
- **git reads every path literally** (kton-review L1): pathspec magic such as `:(top).kton/local`
  could stage the checkout's identity once `.kton/.gitignore` was gone. `persist` and `publish` also
  refuse a path starting with `:`.
- **A key in two trust tiers is refused** when the configuration loads (kton-review L2b); it was
  counted in whichever tier a map range met first.

### Also since v0.4.2

Work merged between v0.4.2 and the v0.5.0 changes above.


#### Added

- **The normaliser comes with the environment** (MH 11.11., D5; asked by cockpit-improve-backend).
  `publish` takes the environment class per call (`environment`, `--environment`), as it takes
  `envRef`; the repo's `environment.spectrum` stays the default. `say reproduces` without `via`
  uses the normaliser of the reproduced run's class, from a spectrum in an installed package, and
  only then `reproduction.normalizer`; the answer reports `via` and `viaFrom` (SPEC §7.5, SPEC §8.2).

#### Fixed

- **`cockpit show` marks what it serves for revalidation** (SPEC §12.3): `Cache-Control: no-cache`
  on the viewer's files and the data. A browser kept the old kton-web viewer (ES modules, wasm)
  after the checkout was updated, even through a forced reload. Asked by metworx.
- **`cockpit/` builds for Windows again** (broken since the shim review fixes, PR #65:
  `syscall.O_NOFOLLOW` does not exist there, and the recorder imports `cockpit`). Opening without
  following a link is `internal/nofollow`: `O_NOFOLLOW` on unix, a link found by `Lstat` refused on
  Windows. CI now vets for Windows and builds for macOS.
- **Parallel writers on one working tree no longer fail on git's lock** (SPEC §12.1). Two `say`
  calls at once made one of them report "git commit/push of the claim registry failed"
  (`index.lock`), although its claim had landed; a caller trusting the exit code sent it twice.
  The cockpit's commits, pushes and `sync` merges now take turns under one lock per repository,
  and a lock held by a git outside the cockpit is waited out for up to 30 s. A kernel store's own
  `.objects.lock` or temporary file that another writer removes while `git add` reads the registry
  is tried again too. Found by kton-coordination on `lanes/`.
- **The shim's NONMEM collector reads a repeatedly included control stream once** (kton-review S6).
  A chain where each level includes the next twice was exponential: 16 levels took 19 s, and now
  take milliseconds.
#### Changed

- **Requires ktonpkg v0.3.0** (verticle profile, `Protocol.Image`, `Step.Outputs`).

#### Added

- **`cockpit mcp --runs`: a package's steps as MCP tools, and the tools a verticle declares**
  (ktonpkg v0.3.0, SPEC §6.2). A verticle's `mcp/<name>.json` is served under its own name and
  schema: a query tool with `--vocabulary`, a ray tool only with `--runs`. A ray tool runs the
  package's steps as `workflow run` does, each a recorded foton, in `work/<package>/<source>`.
  `--runs` also gives each installed workflow without a declaration a `run_<name>` tool, its holes
  as arguments. The first verticle served this way is `render` (SOP `.Rmd` → docx → PDF).
- **`workflow run` takes a verticle's ray, and an optional hole's default.** A hole the package
  marks optional (a template) takes the package's default when not bound; required holes must still
  be named. A step whose `Protocol.Image` differs from `execution.image` is refused before anything
  is staged (SPEC §7.5).

- **`cockpit mcp --vocabulary`: an allowed package's queries and templates as MCP tools** (SPEC §6.2).
  Each query is a tool `ask_<name>` with its `$VAR` parameters as arguments, each template a tool
  `say_<name>` with its fields. `--as NAME` signs every claim over the server as NAME (also
  `cockpit_say`; another `as` is refused), `--scope NAME` chains the templates into that scope and
  makes it their default subject, `--bind VAR=VALUE` fixes a parameter and takes it out of the
  schema. For the lanes: `cockpit mcp --vocabulary --as lane-X --scope lane-X --bind LANE=…` in
  `.mcp.json`, with `COCKPIT_REPO_DIR` pointing at `lanes/`, gives `ask_inbox_open`, `ask_lanes`,
  `say_requests`, `say_done`, … without shell wrappers.

## v0.4.2 — 2026-10-30

Fixes found by kton-coordination on `lanes/` with v0.4.1. Tracks kton v0.3.0 and ktonpkg v0.2.0.

### Fixed

- **A reused run directory starts its folder slots empty.** Files of the last run's folder passed for
  this run's: a document after a report saw the report's evidence.
- **An optional folder hole left open stages nothing.** It staged the whole package.
- **`workflow run` stages a folder hole.** Each file in a folder bound to a slot ending in `/` is an
  input under it, recorded by the run; a link in the folder is refused, and an optional folder left
  open stages nothing. A declared ray tool's run is named after its required input, not a folder.
- **The index daemon comes back after a crash or a reboot.** It is started with
  `--restart unless-stopped`, and a cockpit finds a daemon the engine restarted at the port it has
  now, instead of replacing it. Before, it stayed down after a WSL crash, and every inbox with it.
- **`cockpit say` lists the templates an allowed package brings.** It said "no templates allowed
  here" while `say requests …` worked.
- **A flag beside the JSON form is refused with the field that carries it.** `say --as X '{…}'` gave
  a bare usage error, though the JSON form takes `"as"` (and `"scope"`, `"expectHead"`) itself.
- **`install --allow` no longer reads like a failure** when no allowed package brings the
  `installed` template: the package is installed, and only the claim recording it is not written.

## v0.4.1 — 2026-10-30

Tracks kton v0.3.0 and ktonpkg v0.2.0. With ktonpkg published, `-tags unreleased` is gone: one build
has everything.

### Vocabulary packages

- **Packages are in the default build**: `install`, `workflow`, ray extraction, and the vocabulary
  path. An admitted package (`claims.allowedPackages`, `install --allow`) brings its templates and
  its queries (SPEC §8.1).
- **`ask sparql --name <query> [--bind VAR=VALUE]...`** (SPEC §9.9) runs a query an admitted
  package brings, over the same projection as a query written out. A bound value is a literal in an
  appended `VALUES` clause, never query text. MCP: `name` and `bind`.

### Saying and asking

- **`say --as <name>`** (SPEC §8.7): with `identity.keyring` set, a claim is signed with
  `<name>-claims.key` from that directory instead of `nekton_key`. Only a key whose public half is
  in a trust tier can be named, so one registry carries each writer's own signature (one key per lane).
- **`ask scope <name>`** takes a name configured under `claims.scopes`, as `say --scope` does; only the
  id worked before.

### Tool shims (prototype)

- **Tool shims** (ADR-014, SPEC §10.4): `cockpit-shim`, installed under a tool's name (`nmfe75` …),
  hands the call to `cockpit shim serve`. The same call recorded before by this dispatcher is
  answered from that record; otherwise the tool runs in its own pinned image (`shims` in
  `cockpit.config.json`) on exactly the collected inputs, a licence mounted read-only, and the run is
  recorded as a `foton/v1` of kind `tool/<name>`. The dispatcher is `Cockpit.Call` and
  `Cockpit.CallHandler`, for ktonwatch's `/v1/call`.
  - One NONMEM call: 11.3 s run and recorded, 0.39 s the second time.
  - A call is keyed relative to its base directory, so pharmpy's fresh run directories meet their
    records: a pharmpy `fit()` of three models took 28.7 s, and the rerun 1.9 s with no NONMEM run.
  - A run may leave at most `maxOutputBytes` (1 GiB by default).
- **`internal/toolcall`**: argv normalisation, the content store and the collectors, shared by
  `kton-call` and the shim.

### Fixed

- **With `persist.share: false`, nothing is pushed.** A publish whose files were already committed
  still pushed a commit an earlier call had left ahead of its upstream. When a share is refused,
  publish now prints the store's reason, not a guessed cause ("someone shared meanwhile").

## v0.4.0 — 2026-10-28

Tracks kton v0.3.0. What waited for the kernel is in the default build now; `-tags unreleased`
is left for what needs ktonpkg, which is not published (install, workflow, ray, the vocabulary
queries).

### Breaking

- **Records are `foton/v1`** (kton §6.6.1): a record no longer carries its files' locators or a
  concrete environment in its signed bytes. Both are signed separately and attached beside it
  (`kton-locator/v1`, `kton-env/v1`, ADR-010).

### Asking in SPARQL

- **`ask sparql`** (SPEC §9.9): a SELECT or ASK of one's own over the trusted dataset, from MCP
  (`{"query":"sparql","sparql":…}`) or `cockpit ask sparql <query.rq|->`. The vocabulary and tested
  examples are in `docs/sparql.md`. With `format: "sparql-results+json"` (`--results-json`) the
  answer is also in the standard SPARQL 1.1 Query Results JSON, every value with its type, datatype
  and language.
- **It reads the query projection by default** (ADR-013): every trusted claim is one node
  (`nk:about`, `nk:predicate`, its fields, `nk:signer` with the signer's name and tier, `nk:scope`,
  `nk:position`, `nk:current`, `nk:supersededBy`), in the view's default graph, so a query names no
  graphs. Position and current are computed once per change instead of walked by `nk:prev+`;
  `current` is one total order (a claim with a position beats one without, then position, then
  signed time as an instant, then id). Template aliases give a field its term, field types its
  datatype; an alias into the projection's own terms is refused. `dataset: "nanopub"` (or
  `# dataset: nanopub` as the query's first line) reads the kernel's nanopublication graphs. Two tour
  queries on 24 registries: 7–19 s → 60–70 ms.
- **`index.kind: oxigraph`** (ADR-012) is in the default build.
- **A query's default graph is one view graph**, the trusted records copied together once per
  dataset, not a few thousand record graphs listed per query. A join over records and claims went
  from over 50 s to 1.3 s. A view, its claims and its mark change in one update, so a reader never
  sees a view without its claims.
- **A query against registries that did not move is the query alone.** The index keeps the dataset
  it computed under the registries' positions and the trusted key set; a change to either,
  `rebuild` or a new schema computes it again. On 24 registries an `ask sparql` went from 20 s to
  0.14 s.
- **`index.memory` takes effect on a running daemon**: a configuration asking for more than it was
  started with restarts it with that limit on the same store; `index status` shows the limit. A
  limit that cannot be read is refused at load.

### For programs that follow the work

- **`ask feed`** (SPEC §9.10): the union's trusted records after a cursor, each with every key that
  verifies it (`signers`; a co-signed record names each), its tier and name, and the next cursor;
  untrusted ones counted. For a program that follows the work (mignon's handoff 2.2) instead of
  walking the registry's files.
- **`cockpit.Open`**, a Cockpit for a process that answers many calls (a local service, handoff 2.1):
  it keeps the opened union between calls until a registry changes, whatever `index.kind` says, and
  `Close` lets it go. Every call still re-checks the binding.
- **`say` takes `expectHead`** (SPEC §8.5): the claim joins its scope only on the head the caller
  saw, and is refused with the heads there are now if another writer moved it (handoff 2.6).
- **A call reads the material of many records with the own registry opened once**, not once per
  record (mignon: 266 claims 45.7 s → 0.65 s); a foton's signed locators are read from source
  registries kept open per process until one moves (a held `ask producer` on 24 registries: 1 s →
  60 ms).
- **`show` `/data/select`**: a portion of the store for a viewer (kton-web), with envelopes and a
  cursor.
- **The recorder**: kton-web's graph and editor view, MCP tools for an assistant in Positron
  (`kton_ask`, `kton_sparql`, show/select/copy, `~` in paths), notes kept by ktonwatch, a story's
  way along real variable uses, a guided tour through the pmx corpus and an explore repository.

### Fixed

- A record fetched from a federated source carries its signed locators again: `run --from` and
  `ask producer` read `kton-locator/v1` material from the union, and only from a trusted signer.
- **`cockpit <command> --help` prints that command's usage and runs nothing.** `init --help`
  scaffolded the current directory, `mcp --help` started the server.

## v0.3.0 — 2026-10-23

Tracks kton v0.2.1 (`kton.dev/plankton`, `nekton`, `kton` by `require`, no vendoring). What needs
the kernel's unreleased surfaces is built with `-tags unreleased` against kton `main` and stays out
of a default build until kton 0.3.0 is tagged.

### Breaking

- **The `git` configuration block is now `persist`**: `git.commit` → `persist.commit`, `git.push` →
  `persist.share`. The cockpit no longer speaks of git outside a backend (ADR-011), and
  `cockpit_publish` reports `persisted`/`shared` instead of `committed`/`pushed`.
- **`environment.spectrum` must be a canonical content hash** (`sha256:` and 64 lowercase hex), and
  **`query.image` must be pinned by digest**.
- **`cockpit doctor` exits 3** while a step marked `!` is left; it used to exit 0.

### The tool

- **Ports and backends (ADR-011).** `cockpit/backend` (where records persist: git, local; an
  extension registers more, e.g. improve), `cockpit/executor` (what runs a command: the pinned
  container by default) and `cockpit/source` (where federated records come from). Importable, so a
  binary that adds a backend is the same program with one more import.
- **Federation.** `federation.sources` names registries read together with the own one (`dir`,
  `git`). Sources are read read-only, a source with no registry is refused, and trust does not
  widen: the configured tiers stay the ceiling. `ask scope` reads over the sources too, verifies
  each seed against this repository's tiers, and answers with the scope's parent chain.
- **A derived index (ADR-012).** `index.kind`: `none` (default), `memory` (the MCP server keeps the
  opened registries until a file changes) and, with `-tags unreleased`, `oxigraph` — one daemon per
  machine, records stored once, trust chosen per query, `SERVICE` refused. `cockpit index
  status|start|stop|rebuild`.
- **`publish --base`** (SPEC §7.8): paths and the command are relative to a base, so the same call
  in another checkout is the same computation. A path or a command reaching above the base is
  refused.
- **The cache key on the environment class (ADR-010)**, behind `-tags unreleased`: with a spectrum
  set, `envRef` is carried as signed `kton-env/v1` material beside the record.
- **Claim templates `describes` and `rationale`** (SPEC §8.6): `dcterms:description` and EP-Plan's
  `hasRationale`, one field `value` each; `cockpit init` admits both.
- **`cockpit doctor --json`** for a program to read: `ready`, `steps`, `fields`.
- **Operator commands** `trust`, `federation`, `sync`, `config`, `pin`, `keygen`, `install`,
  `workflow`, `run`.
- **The recorder** (`recorder/`): watches a session's consoles and drafts the `cockpit publish` calls
  that record it, with an editor extension.
- **New verbs are allowed (ADR-009)** when they have CLI, MCP and Go implementations, sit under the
  configuration ceiling, are specified and tested. None has been added.

### Fixed

- Concurrent readers of a federated repository tore down each other's union cache. A union now
  lives under the stamp of what it was built from, is built aside and never changed after, and
  building takes turns under a lock.

## v0.2.0 — 2026-09-17

First release under the name **kton-cockpit**, in `kton-protocol`. Tracks kton 0.2.

### The tool

- **The three verbs are subcommands.** `cockpit publish`, `cockpit say` and `cockpit ask` can be
  typed. Until now they existed only over MCP, so the binary a person installed had `mcp`, `init`,
  `doctor` and `show` — and none of what it is for.
  - `--field NAME` prints one value, so a shell need not grow its own JSON parser. A field that is
    not in the answer is an error naming what the answer does have.
  - Two exit codes: **2** the cockpit understood and refused, **1** the call itself was wrong.
  - Unknown fields are refused rather than ignored. A tool schema catches `{"output":…}` for
    `outputs`; at a shell nothing else would, and the record would be published naming no outputs.
  - The MCP surface is unchanged and reaches the same three handlers. A guard belongs to the
    configuration, never to the way a call arrived.
- **`cockpit version`** reports the binary, the commit it was built from, and the kton kernels
  compiled into it.
- **`cockpit scope`** — seed, seal and read a nekton scope (kton §7.4). Sealing records a child's
  head in its parent, repeatably, which is what stops a rewind.
- **Carried evidence** (kton §8.1). Any scheme is attached; a certificate is checked against the key
  that actually signed and the roots the configuration names. Three verdicts — `verified`,
  `carried`, `failed` — and no fourth, because collapsing `carried` in either direction is the
  failure: counting it as verified overstates, and dropping it means a reader cannot tell
  evidence-nobody-read from no evidence at all.

### Under it

- **The kernels are linked, not invoked.** `kton.dev/plankton`, `kton.dev/nekton` and
  `kton.dev/kton` are ordinary Go dependencies, so every registry write, query, verification and the
  Rekor round trip is a function call. One binary is the whole install; a participant repo needs no
  kernel binary. A surface upstream removed is now a build failure rather than a usage error a
  session meets at run time.
  - The kernel version gate is gone with the processes it guarded.
  - `TestAuthor_MatchesTheReferenceCLI` authors the same record both ways and asserts the same foton
    id, because linking is only safe while it agrees with the reference.
- **Vendored.** `vendor/` carries the kernel source, so a clean clone builds with no sibling
  checkout. This comes out when kton.dev serves the modules.

### The name

- `claude-science-cockpit` → **kton-cockpit**; module `github.com/kton-protocol/kton-cockpit`.
- The caller is no longer described as a particular product. It is "the caller" where a value is
  supplied and "a session" where a surface is withheld — which is what the guarantees were always
  about: whoever holds three verbs under a configuration they did not write.
- `CLAUDE.md` → `AGENTS.md`. Apache 2.0, copyright 2026 Michael Hackl and Wolfgang Schwarzenbrunner.

### Shown and checked

- **12 examples**, one property each, 83 assertions of which 6 are negative controls. Seven need
  nothing installed. New: an everyday R cleaning argument on real open data, an environment made
  reconstructible from a Nix expression, and the same thing with the environment declared in R via
  {rix} so nobody writes Nix.
- **`uat/e2e.sh`** — two participants and a federation, end to end, no pauses and no GitHub account.
  It used to stop four times for a person to drive a browser, including at a step it called the one
  that "genuinely needs reasoning". That step is a lookup: the foton names its inputs by hash with a
  commit-pinned locator.
- **A homepage** (`docs/`), generated from the examples and from a transcript captured by running
  the commands. A test fails if it drifts.
