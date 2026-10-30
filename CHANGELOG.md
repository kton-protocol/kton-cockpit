# Changelog

## Unreleased

### The tool

- **`say --as <name>`** (SPEC §8.7): with `identity.keyring` set, a claim is signed with
  `<name>-claims.key` from that directory instead of `nekton_key`. Only a key whose public half is
  in a trust tier can be named, so one registry carries each writer's own signature (one key per lane).
- **`ask scope <name>`** takes a name configured under `claims.scopes`, as `say --scope` does; only the
  id worked before.
- **Tool shims, a prototype** (ADR-014, SPEC §10.4): `cockpit-shim`, installed under a tool's name
  (`nmfe75` …), hands the call to `cockpit shim serve`. The same call recorded before by this
  dispatcher is answered from that record; otherwise the tool runs in its own pinned image
  (`shims` in `cockpit.config.json`) on exactly the collected inputs, a licence mounted read-only,
  and the run is recorded as a `foton/v1` of kind `tool/<name>`. One NONMEM call: 11.3 s run and
  recorded, 0.39 s the second time. The dispatcher is `Cockpit.Call` and `Cockpit.CallHandler`, for
  ktonwatch's `/v1/call`. A call is keyed relative to its base directory, so pharmpy's fresh run
  directories meet their records: a pharmpy `fit()` of three models took 28.7 s, and the rerun 1.9 s
  with no NONMEM run.
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
