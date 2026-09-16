# AGENTS.md

This is the source repository for **kton-cockpit**: a command-line tool with exactly
three verbs — `publish` (veröffentlichen), `say` (sagen), `ask` (fragen) — for doing work in a
[kton](https://kton.dev) federation and leaving a record of it that somebody else can check.

You type them. An agent can also call them over MCP, as `cockpit_publish`/`cockpit_say`/
`cockpit_ask`; that is a second transport onto the same three handlers, not a second tool. It reimplements no
plankton/nekton kernel logic: it LINKS the kernels as Go libraries and calls them. kton's own
`kton.dev/kton` module describes that role in the same words — "the cockpit that CONDUCTS both
kernels: it imports plankton AND nekton and reimplements nothing" — and the dependency direction
is the same. Nothing spawns a kernel process any more — a participant repo needs no kernel binaries
at all, and one binary is the whole install.

Two decisions with consequences beyond their clause are written up in
[`docs/decisions/`](docs/decisions/): running a command in a pinned container (ADR-003) and running
with no git repository at all (ADR-004). Everything else that governs is in the spec below.

What the cockpit guarantees is specified in [`spec/SPEC.md`](spec/SPEC.md) — clause by clause, each
naming the test that exercises it, with the limits stated beside the guarantees. Read that before
changing behaviour a clause depends on; `spec/spec_test.go` fails if a clause cites a test that is
no longer there, or states a MUST without saying how it is checked.

The German design brief this was commissioned from is not in the repository. It said of itself that
it was an internal document, and it described three things that were never built — a Sigstore-backed
signing identity, a reproduction-count precondition, and four of the five filter dimensions. Two of
those are now built and specified; the third is [an open request](spec/SPEC.md#open-questions).
Keeping the brief would have published commitments the code does not keep, which is the failure this
project spends its effort preventing one level down.

Everything in it that still governs — the three verbs, verified-not-declared, the configuration as a
ceiling, the "do not build" list — is in `spec/SPEC.md`, where each clause names the test that
checks it.

## Build

```bash
go build -o bin/cockpit ./cmd/cockpit
```

Requires Go ≥ 1.25 (the module's `go.mod` pins this via the `github.com/modelcontextprotocol/go-sdk`
dependency; `go build`/`go run` auto-fetch the matching toolchain if the ambient `go` is older).

### The kernel

The kernels are **linked, not invoked**. `kton.dev/plankton`, `kton.dev/nekton` and `kton.dev/kton`
are ordinary Go dependencies — all three declare zero third-party dependencies of their own — so
everything the cockpit does to a registry is a function call: authoring, signing, ingest,
verification, the lineage and claim queries, verification material, scopes, and the Rekor round
trip. Nothing spawns a process, nothing parses another program's prose, and a surface the cockpit
depends on that upstream removed is a build failure rather than a usage error a session meets at
run time.

Until kton.dev serves the modules, `go.mod` resolves them through `replace` directives to a local
checkout:

```
replace kton.dev/plankton => ../kton-pinned/reference
replace kton.dev/nekton   => ../kton-pinned/nekton/reference
replace kton.dev/kton     => ../kton-pinned/kton/reference
```

`kton-pinned` is a clone of [`kton-protocol/kton`](https://github.com/kton-protocol/kton) that this
repo controls, checked out at the commit below. It is deliberately not a sibling working tree
someone is editing: building against one mid-refactor is how three examples once broke on a wire
form nothing had been tested against.

**Verified against:** kton `dev` at `52b49f0` (0.2).

`dev` moves, and this line is checked rather than remembered:
`TestVerifiedAgainst_NamesTheKernelTheBinariesWereBuiltFrom` compares it against the `vcs.revision`
Go stamped into the `bin/plankton` the fixture builds. When upstream has
moved, the fixture rebuilds to the newer kernel — so the suite really did run against a different
one than this claims, and the test says so. Re-run and update the line in the same commit.

One limit worth knowing, because it looks like a pass: **Go can serve that test from its cache.**
The inputs it actually reads — `AGENTS.md` and `bin/plankton` — are not inputs Go tracks, and on a
clean tree `bin/plankton` does not exist when the cache decision is made, so a plain `go test ./...`
right after upstream moved reports `(cached) ok` for a claim that is no longer true. Use
`go test -count=1 ./internal/testrepo/` when you have just rebuilt the kernel. CI is unaffected: a
fresh runner has no cache, and the workflow runs `-count=1` besides.

### The binaries that are still binaries

`bin/plankton` and `bin/nekton` are still built (`bin/` is gitignored), and two things use them —
neither of them the cockpit at run time:

- the **examples**, which drive the kernel CLI directly on purpose, to show that everything the
  cockpit does stays performable by hand (SPEC §13, "deletable");
- `TestAuthor_MatchesTheReferenceCLI`, which authors the same record both ways and asserts the
  **same foton id** comes out. Linking is only safe while it agrees with the reference, so that
  agreement is a test rather than an assumption.

```bash
cd /path/to/kton-pinned
go build -o /path/to/cockpit/bin/plankton ./reference/cmd/plankton
go build -o /path/to/cockpit/bin/nekton   ./nekton/reference/cmd/nekton
```

**Which kernel a store was written with decides whether it reads as populated or as empty with exit
0.** An older binary against the current single-file subnekton layout finds nothing and still exits
successfully; kton 0.2 added `objects/.format` so a store can say what wrote it, but an old binary
does not know to look. So: never use a `plankton`/`nekton` from `$PATH`, a package manager, or
another checkout — only one built from the kton tree you mean.

## Subcommands

```
cockpit publish '<json>' [--field NAME]   record a result as a signed foton
cockpit say     '<json>' [--field NAME]   bind a claim from an allowed template
cockpit ask     '<json>' [--field NAME]   query the graph, re-verified against configured trust

cockpit mcp      start the MCP stdio server (cockpit_publish/cockpit_say/cockpit_ask)
cockpit init     scaffold a cockpit.config.json in the current git repo (human operator only)
cockpit doctor   validate cockpit.config.json + the repo binding (human operator only)
cockpit show     serve this repo's records to a kton-web viewer (human operator only)
cockpit scope    seed, seal or read a nekton scope (human operator only)
```

The three verbs have **two transports and one implementation**: `publish`/`say`/`ask` at a shell and
`cockpit_publish`/`cockpit_say`/`cockpit_ask` over MCP dispatch to the same three handlers, so a
guard is the configuration's and never the transport's. `TestVerbs_TheGuardsApplyAtTheCommandLineToo`
holds that for the anti-wrong-folder guard specifically.

Reaching them from a shell grants nothing that was withheld: whoever can type them already has the
registry, the keys and the config on their filesystem. What the tool surface withholds from a
*session* it still withholds, because the ceiling is `cockpit.config.json`, not the transport.

Two exit codes, and the difference matters to a script: **2** means the cockpit understood and
**refused**; **1** means the call itself was wrong. At a shell nothing validates the argument the
way an MCP tool schema does, so unknown fields are refused rather than ignored — `{"output":[…]}`
for `outputs` would otherwise publish a record naming no outputs at all.

`show` is an operator subcommand, not a fourth verb: the MCP surface stays at three and cannot
reach it. It renders nothing and parses no registry files of its own — it asks both kernels for
their records and forwards them as the union a viewer fetches, which is the cockpit's side of the kernel's own division ("RENDERING … is a cockpit's job,
not the kernel's", see `plankton export`). `keys.json` is built from the configured trust tiers rather than from whatever
`.pub` files sit in the registry, so the viewer re-verifies against exactly what `cockpit_ask` does.
Point it at a [kton-web](https://github.com/gitmick/kton-web) checkout with `--web`/`$KTON_WEB`, or
omit that and serve only the data endpoints for a viewer running elsewhere.

Only `mcp` is ever registered as the tool surface (via a participant repo's `.mcp.json`).
`init`/`doctor` are for whoever is setting up or debugging a participant repo — a session never
calls them.

## Test

```bash
go vet ./...
go test ./...
```

`go test ./...` needs no fixtures and no pre-existing repo. `internal/testrepo` builds a complete
participant repo from nothing for each test — git init, a real github.com `origin` (so the
anti-wrong-folder guard runs unmodified; only the remote's *push* url points at a local bare repo,
so commits and pushes complete offline), the registry/keys/templates/bin layout, signing identities
from the real `keygen`, the claim templates, and a `cockpit.config.json`. Records are genuinely
signed; assertions are about what actually happened. `testrepo.NewLocal` builds the same thing with
no git repository at all, for the local-mode guard (ADR-004).

If `bin/` is empty the tests build those binaries themselves from `$KTON_SRC`, else a sibling
`../kton-pinned`, else `../kton` — and they rebuild when what is in `bin/` was built from a
different commit than that checkout now holds. Go stamps `vcs.revision` into a binary, so "were these built from what is
checked out now" has an answer rather than depending on someone remembering. That check exists
because the alternative kept happening: a moving `dev`, binaries left behind, and a symptom that
looks like a missing subcommand rather than a stale build.

`COCKPIT_TEST_PLANKTON`/`COCKPIT_TEST_NEKTON` override both and are never second-guessed. Nothing
ever falls back to `$PATH` — see the warning under "The binaries that are still binaries" for why.

One area is behind a build tag, and deliberately so:

```bash
go test -tags docker ./...     # adds the tests that drive a real container runtime
```

Those cover `internal/container` and publish's execution path (ADR-003). They are excluded from the
default run so `go test ./...` keeps the property below; asking for them without an engine is a
failure, not a skip, because you asked. Run them before merging anything that touches either.

Anchoring is covered differently, and the limit is worth stating rather than discovering: the one
function that reaches Rekor is swapped out (`testrepo.StubAnchor`), because it writes to a public,
permanent log and a suite that anchored on every run would leave entries nobody can withdraw — the
kernel gates its own live Rekor test behind a `live` tag for the same reason. What the stub leaves
covered is everything on this side of the network: the envelope is found by id, the verifier is
derived from the key that actually signed, and the entry is attached to the record as kton §8.1
material and committed with it.

The other side has its own test, gated the same way:

```bash
go test -tags live -run TestAnchorLive ./internal/tools/
```

It anchors a throwaway record in the public log and then asks Rekor — not the cockpit — whether the
entry it reported is really there. Every run leaves a permanent public entry, so run it when the
anchoring path changes, not as a habit. Verified once on 2026-09-03: logIndex 2698571462.

Nothing in the default suite skips. If a test cannot run it fails, because a suite that quietly degrades to
skips is how this one previously spent months reporting success while exercising none of the
handlers: it pointed at a hand-made local clone by absolute path (`/mnt/c/dev/planktonReproduce/…`)
that had been reorganized away.

### Examples

```bash
examples/run-all.sh          # all of them
examples/02-chain/run.sh     # or one
```

One property per directory, each a readable script that builds its own participant repo from
nothing. Three need something the others do not: `07-run-in-container` a container engine,
`09-carried-evidence` `openssl` (it issues a certificate for the key plankton already holds — the
step an operator actually has to perform, so it is performed rather than assumed), and
`10-cleaning-is-an-argument` R and network, because it works on real open data and pinning the
bytes that were actually downloaded is half of what it is about. `11-environment-reconstructible`
and `12-the-environment-written-in-r` need nix and docker, and their first run pulls R's Nix
closure — about 2 GB, because nixpkgs' R carries the toolchain it compiles packages with. They demonstrate what the cockpit adds over the kernel — the guard, the claim ceiling, the
verified-not-declared rule, the environment pin — over the MCP surface a session actually gets,
rather than by driving plankton and git directly.

They live here rather than in their own repository on purpose: kton-examples is separate from kton
and went stale exactly as that predicts, its CI checking out an archived repo and reporting success
for six weeks with every step silently skipped. See [`examples/README.md`](examples/README.md).

### Automated end-to-end UAT

See [`uat/README.md`](uat/README.md) for prerequisites (gh CLI scopes, Go, python3, claude-science
account). Follows the tutorial's federation-first sequence: participant 1 publishes and federates
before participant 2 does anything, so participant 2 can mirror participant 1's real aggregated
foton, hit the realistic byte-mismatch, correct it, and record a genuine ↻2 reproduction.

```bash
uat/setup.sh      # creates a federation + 2 participant repos, configures the cockpit in both,
                   # pauses 4 times for claude-science steps (connector setup; p1 publish; p2
                   # publish independently; p2 mirror+correct+claim) — not scriptable, see the
                   # script's header comment for why — then shows the resulting graph
uat/cleanup.sh <workdir printed by setup.sh>   # deletes everything setup.sh created, with confirmation
```

## Repo layout

```
cmd/cockpit/main.go     entry point: mcp / init / doctor subcommands
internal/
├── config/              cockpit.config.json schema, loader, and the anti-wrong-folder guard
│                          (git mode: the origin remote must match; local mode: the config must be
│                           where it declares itself to be — ADR-004)
├── container/            runs a published command in a pinned image, when a repo opts in (ADR-003)
├── material/             carries external evidence about a record (SPEC §8.1) and evaluates what
│                          it can: any scheme is attached, a certificate is checked against the key
│                          that actually signed and the configured roots, the rest is reported as
│                          carried rather than silently counted or dropped
├── anchor/               witnesses a record in Rekor via `kton.dev/kton/sigstore` and attaches
│                          the verified entry as kton §8.1 material
├── gitops/               commit/push wrappers, commit-pinned permalink construction
├── show/                 serves the union/keys/names a kton-web viewer fetches
├── binaries/             the kernels, called as libraries: one place where every plankton and
│                          nekton call lives, so the rest of the cockpit sees one surface
├── verify/               trust-tier resolution from the actual verifying key, never a declared keyid
├── tools/                the three MCP tool handlers (publish.go, say.go, ask.go)
cockpit.config.schema.json   JSON Schema for cockpit.config.json (documentation + tooling)
```

## What not to add here

Per [`spec/SPEC.md` §13](spec/SPEC.md), where each item is checked by a named test: no own
canonicalization/signing/registry/chain logic, no trust logic beyond applying
`cockpit.config.json`, no fourth verb, no cockpit-owned mutable state beyond that static config
file. If a change would require one of these, raise it against the protocol spec first, not as a
cockpit-side workaround.

Applying the config is the line, and it is not the same as doing nothing: SPEC §11.2's certificate check
is `crypto/x509` against roots the config names, which is applying configured trust. Re-verifying a
stored Rekor entry would not be — that is transparency-log cryptography, and it belongs in the
kernel, which is where it is raised.
