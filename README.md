# kton-cockpit

[![CI](https://github.com/kton-protocol/kton-cockpit/actions/workflows/ci.yml/badge.svg)](https://github.com/kton-protocol/kton-cockpit/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

A command-line tool with exactly three verbs, for doing work in a [kton](https://kton.dev)
federation and leaving a record of it that somebody else can check.

📖 **[A tour, with real output →](https://kton-protocol.github.io/kton-cockpit/)**

| Verb | German | What it does |
|---|---|---|
| `cockpit publish` | veröffentlichen | Record a work result as a signed, reproducible **foton**: runs the command in a pinned container (when one is pinned), commits and pushes the files, builds commit-pinned permalinks, signs, registers. |
| `cockpit say` | sagen | Make a claim — from an allowed template — about a run, a file or a record. For `reproduces`, the cockpit compares the bytes and measures the level itself; it never takes a declared one. |
| `cockpit ask` | fragen | Ask the graph: who made these bytes, what used them, the lineage behind them, who reproduced them, what was claimed — every answer re-verified against the configured trust tiers. |

```console
$ cockpit run new means --in data/sensor.csv          # a run folder: inputs/, run.py, out/
$ cockpit run means                                   # runs in the pinned image, becomes a record
$ cockpit say working-on runs/means step="checking outliers"
$ cockpit ask producer runs/means/out/means.csv
(runs/means/out/means.csv → its bytes sha256:499d4608bf571222…)
sha256:1780e61af81aa9a9…  self     cd runs/means && python3 run.py
```

A reference is whatever you have in hand: a **file** (its bytes), a **run folder** (`runs/<slug>`,
the record its run made) or a `sha256:…` id. Every verb also takes its full argument as one JSON
object (`cockpit ask '{"query":"producer","ref":"…"}'`), which is what scripts and the MCP transport
use; `--json` prints the full answer of the word form.

An agent can call the same three verbs over MCP, as `cockpit_publish`/`cockpit_say`/`cockpit_ask`.
That is a second transport onto the same three handlers, not a second tool: a guard belongs to the
configuration, never to how the call arrived.

What the cockpit guarantees — and, as importantly, what it does not — is written up clause by
clause in [`spec/SPEC.md`](spec/SPEC.md). Every normative statement there names the test that
exercises it, so the document is checkable rather than merely asserted.

There is no fourth verb. Everything else on the command line (`init`, `keygen`, `pin`, `config`,
`trust`, `federation`, `run`, `doctor`, `show`) is operator work around the three. The cockpit
reimplements no plankton/nekton logic of its own — it **links** the kernels as Go libraries
(`kton.dev/plankton`, `kton.dev/nekton`, `kton.dev/kton`) and calls them, so one binary is the whole
install and no participant repo needs a kernel binary. See
[`AGENTS.md`](AGENTS.md#what-not-to-add-here) for the full list of what this project deliberately
does not build.

## Install

```sh
go install github.com/kton-protocol/kton-cockpit/cmd/cockpit@main
cockpit version            # the binary, its commit, and the kton kernels compiled into it
```

Needs Go and git; Docker (or another engine, `execution.engine`) to run commands in a pinned image.
The default build uses only published modules (kton v0.2.1). Work that needs kernel API not yet
released — package installation, workflows, ray extraction, SPARQL queries, foton/v1 locators — is
behind `-tags unreleased` and needs those modules locally.

## Set up a repository

```sh
mkdir study && cd study
git init -b main
git remote add origin https://github.com/alice/study.git
cockpit init                       # configuration, claim templates, .gitignore
cockpit keygen alice               # signing identity; binds itself into the configuration
cockpit pin python:3.12-slim       # the image every run executes in, pinned by digest
cockpit doctor                     # ends with "ready: publish · say · ask" or the next steps
git add -A && git commit -m "cockpit set up" && git push -u origin main
```

What that creates, all inside the repository:

| Path | What | In git |
|---|---|---|
| `cockpit.config.json` | binding to `alice/study` (must match `origin`), identity, trust tier `self`, pinned image | yes |
| `.gitignore` | keeps `/keys/` out | yes |
| `templates/working-on.json`, `templates/reproduces.json` | the claims this repository allows | yes |
| `registry/keys/alice.pub`, `alice-claims.pub` | the public keys — a peer verifies with them | yes |
| `keys/alice.key`, `alice-claims.key` | the **private** signing keys | **no** |

Work adds `registry/plankton/` (records), `registry/nekton/` (claims) and `runs/<slug>/`, all
committed and pushed by the verbs themselves. Outside the repository the cockpit writes nothing,
except a cache under `~/.cache/kton-cockpit/` when it reads federation sources.

`init` and `keygen` work in either order. Without git, `init` sets up local mode (no commits, no
permalinks; [ADR-004](docs/decisions/adr-004-running-without-git.md)).

## Everyday work

**Run something.** A run folder holds its inputs, its script and its outputs:

```sh
cockpit run new means --in data/sensor.csv     # runs/means/{inputs/, run.py, out/}; --script run.R|run.sh
$EDITOR runs/means/run.py                      # reads inputs/, writes out/
cockpit run means                              # runs in the pinned image → a record, committed and pushed
cockpit run new means-a --from runs/means      # a variant: same inputs and script, out/ empty
cockpit run list                               # runs, their record, the claims about them
```

**Report a result** that did not come from a run folder:

```sh
cockpit publish --in data/sensor.csv --in report/count.py --out report/missing.txt -- python3 report/count.py
```

With an image pinned the cockpit runs the command; without one it records the command you ran.

**Say something** about a run, a file — also one outside the repository, by its bytes — or a record:

```sh
cockpit say                                                  # templates, their fields, steps used so far
cockpit say working-on runs/means step="checking outliers"
cockpit say working-on data/sensor.csv step="P4 has no value, ask the lab"
```

Who speaks is the signature; `by-session` is filled from your identity. A missing or misspelled
field is named before anything is signed.

**Ask:**

```sh
cockpit ask                                    # the questions there are
cockpit ask producer runs/means/out/means.csv  # the runs that made these bytes
cockpit ask uses data/sensor.csv               # the runs that used them
cockpit ask lineage runs/means-a/out/means.csv # the chain behind them
cockpit ask record runs/means-a                # command, inputs, outputs, image, signer
cockpit ask about data/sensor.csv              # the claims about it
cockpit ask by signer me                       # what I have claimed
cockpit ask by predicate working-on            # every claim of one kind
cockpit ask reproductions <file> --min 2       # how many independent parties produced these bytes
```

Only records a key in your trust tiers verifies are answered; the rest are counted, never shown.

**See it as a graph:**

```sh
cockpit show --web <kton-web checkout>         # http://127.0.0.1:8377/viewers/graph/?union=/data/union.json
```

Serves the union of your records and your federation sources, with your trust tiers as the keys the
viewer re-verifies against. Without a kton-web checkout (`--web` or `KTON_WEB`) it serves the data
only.

**Change a setting:**

```sh
cockpit config                                 # every setting, and the command that changes it
cockpit config git.push off                    # one value; a change that does not load is put back
```

## Working with others

```sh
cockpit federation add hub https://github.com/org/aggregate.git   # read an aggregation repo (or a peer's)
cockpit trust add bob https://github.com/bob/study.git            # shows bob's own keys and the commit
cockpit trust add bob https://github.com/bob/study.git --yes      # records bob signed now count, in tier bob
cockpit federation list                                           # what each source holds, how much verifies
cockpit federation pull                                           # read newer records

cockpit run new check --from sha256:<bob's record>   # bob's inputs and script, every hash checked
cockpit run check                                    # "same bytes as …"
cockpit say reproduces runs/check                    # the cockpit measures the level
cockpit ask reproductions runs/check/out/<file>      # ↻2: two keys, one result
```

A source adds records, never signers. `trust add` takes only the peer's own keys (its tier `self`),
not the ones it trusts in turn, and writes nothing without `--yes`. Sources are read from a clone
in the cache; asking does not go to the network.

## Quick start

The whole of the above, two participants on GitHub-shaped origins and Docker, nothing but `cockpit`
and `git`:

```bash
uat/cli.sh
```

Two participants and a federation, driven by the three verbs, showing the protocol rather than the
tool — participant 2 solves the same task its own way, gets different bytes, fetches the exact script
participant 1's record names, reproduces it and claims it:

```bash
uat/e2e.sh
```

Neither needs a GitHub account or the network beyond pulling the image. See
[`uat/README.md`](uat/README.md).

## The ecosystem this fits into

This repo is only the cockpit. It sits on the **participant side** of a separate ecosystem:

- **[plankton](https://kton.dev) / nekton** — the underlying content-addressed record types this
  cockpit drives: a *foton* is a signed, reproducible record of "this command, these inputs,
  produced these outputs"; a *nekton claim* is a signed attestation about a foton (e.g. "I
  reproduced this"). The cockpit never reimplements their canonicalization, signing, or
  verification — it links them and calls them.
- **[`gitmick/plankton-participant-template`](https://github.com/gitmick/plankton-participant-template)**
  — a GitHub template a participant repo can be created from, with kernel binaries vendored in
  `bin/`. `cockpit init` sets up a repository without it.
- **[`gitmick/plankton-federation-template`](https://github.com/gitmick/plankton-federation-template)**
  — the template for the *aggregator* repo. A GitHub Actions workflow independently re-verifies and
  mirrors every registered participant's signed records into one aggregate, which a GitHub Pages
  viewer renders as a graph. Registration happens by opening a "register a participant" issue and
  a maintainer adding the `approved` label — that label **is** the admission gate. A participant
  reads an aggregate with `cockpit federation add`.
- **kton-web** — the viewer `cockpit show` serves (the graph, a pedigree per output, trust cards).
- **[`kton-protocol/kton`](https://github.com/kton-protocol/kton)** — the kernel itself: the
  `plankton` and `nekton` reference implementations this cockpit links, plus the protocol's own
  reference cockpit / CLI (`kton mirror`, `kton anchor`). This project is a different, narrower
  cockpit, not a replacement for it. The kernel used to live in `gitmick/plankton`, which is
  now archived and private — anything still pointing there is stale.

None of those are this repo's code — they're independent tools this cockpit is built to sit
alongside.

## Why this exists

It replaces a fragile manual workflow — hand-running `plankton`/`nekton` CLI commands, exporting
`PLANKTON_DIR`/`NEKTON_DIR` by hand, constructing permalinks yourself — which is fragile in exactly
the way you'd expect: an ambiguous working directory makes you "cooperate" against the wrong
project's registry, and hand-run infra steps (commit order, key hygiene, template field names) are
easy to get subtly wrong. The cockpit's **anti-wrong-folder guard** re-verifies,
on every single tool call, that the repo it's running in actually matches its own
`cockpit.config.json` and that config's declared `git remote` — and hard-refuses on any mismatch.
See [`spec/SPEC.md` §5](spec/SPEC.md) for both anchors the guard uses and what each one misses.

## Building and developing the cockpit itself

See [`AGENTS.md`](AGENTS.md) — build command, subcommands, tests, repo layout, and the manual
smoke-test recipe against a real populated registry.
[`CONTRIBUTING.md`](CONTRIBUTING.md) has the three rules that are not style preferences: a
normative clause names the test that checks it, nothing in the default suite skips, and every
check says what it saw.

CI runs the suite against the kernel version `go.mod` requires, the container path
(`go test -tags docker`), the command-line walkthrough (`uat/cli.sh`), and — allowed to fail —
the suite against kton `main` HEAD, because a moving kernel breaking this repository is news rather
than a defect in it, and it has arrived four times as a symptom instead.

## Security

Report anything that would let a record be trusted when it should not be to the address in
[`SECURITY.md`](SECURITY.md), not as a public issue. That file also lists the limits that are
documented rather than defects — what a guard deliberately does not catch, what is attested rather
than proven, and why a verdict about carried evidence is a property of the reading rather than of
the record.

Licensed under [Apache 2.0](LICENSE).

## Where the reasoning lives

[`spec/SPEC.md`](spec/SPEC.md) is the contract: every normative clause names the test that checks
it, and states its limit beside the guarantee. [`docs/decisions/`](docs/decisions/) holds the
decisions whose reasoning outlives the clause they produced — among them running in a pinned
container (ADR-003), running with no git repository (ADR-004), and the cockpit as a core with
backends and extensions (ADR-008).
