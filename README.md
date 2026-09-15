# claude-science-cockpit

A Go binary that gives a Claude session — the Claude Code CLI, or
[claude-science](https://kton.dev) (a separate, sandboxed, browser-UI product) — exactly three
verbs for cooperating in a [kton](https://kton.dev) federation:

| Verb | German | What it does |
|---|---|---|
| `cockpit_publish` | veröffentlichen | Register a work result as a signed, reproducible **foton** (commits + pushes the files, builds commit-pinned permalinks, signs, adds to the local registry). |
| `cockpit_say` | sagen | Bind a claim — from a fixed template — to a foton or output. For the `reproduces` template, the cockpit itself computes the L0 reproduction level; it never trusts a self-declared claim. |
| `cockpit_ask` | fragen | Query the federated graph (producer, uses, lineage, reproductions, ...) — every result is independently re-verified against the configured trust tiers before being returned. |

What the cockpit guarantees — and, as importantly, what it does not — is written up clause by
clause in [`spec/SPEC.md`](spec/SPEC.md). Every normative statement there names the test that
exercises it, so the document is checkable rather than merely asserted.

Nothing else is exposed to Claude. The cockpit reimplements no plankton/nekton logic of its own —
every mutation and query shells out to the vendored `bin/plankton`/`bin/nekton` binaries in
whichever participant repo it's configured for. See
[`CLAUDE.md`](CLAUDE.md#what-not-to-add-here) for the full list of what this project deliberately
does not build.

## Why this exists

It replaces a fragile manual workflow — a Claude session hand-running `plankton`/`nekton` CLI
commands, exporting `PLANKTON_DIR`/`NEKTON_DIR` by hand, constructing permalinks itself — which is
fragile in exactly the way you'd expect: an ambiguous working directory can make a session "cooperate"
against the wrong project's registry, and hand-run infra steps (commit order, key hygiene, template
field names) are easy to get subtly wrong. The cockpit's **anti-wrong-folder guard** re-verifies,
on every single tool call, that the repo it's running in actually matches its own
`cockpit.config.json` and that config's declared `git remote` — and hard-refuses on any mismatch.
See [`spec/SPEC.md` §5](spec/SPEC.md) for both anchors the guard uses and what each one misses.

## The ecosystem this fits into

This repo is only the cockpit. It sits on the **participant side** of a separate ecosystem:

- **[plankton](https://kton.dev) / nekton** — the underlying content-addressed record types this
  cockpit drives: a *foton* is a signed, reproducible record of "this command, these inputs,
  produced these outputs"; a *nekton claim* is a signed attestation about a foton (e.g. "I
  reproduced this"). The cockpit never reimplements their canonicalization, signing, or
  verification — it only shells out to the vendored `bin/plankton`/`bin/nekton` binaries.
- **[`gitmick/plankton-participant-template`](https://github.com/gitmick/plankton-participant-template)**
  — the GitHub template a participant repo is created from. It vendors `bin/plankton`/`bin/nekton`
  and expects `cockpit.config.json` + a `.mcp.json` registering `cockpit mcp`.
- **[`gitmick/plankton-federation-template`](https://github.com/gitmick/plankton-federation-template)**
  — the template for the *aggregator* repo. A GitHub Actions workflow independently re-verifies and
  mirrors every registered participant's signed records into one aggregate (`mirror/`), which a
  GitHub Pages viewer renders as a graph. Registration itself happens by opening a "register a
  participant" issue and a maintainer adding the `approved` label — that label **is** the admission
  gate.
- **[`kton-protocol/kton`](https://github.com/kton-protocol/kton)** — the kernel itself: the
  `plankton` and `nekton` reference implementations this cockpit shells out to, plus the protocol's
  own reference cockpit / CLI (`kton mirror`, `kton anchor`). This project is a different, narrower cockpit purpose-built for a
  Claude session, not a replacement for it. The kernel used to live in `gitmick/plankton`, which is
  now archived and private — anything still pointing there is stale. The two *template* repos above
  did not move and are still live.

None of those three are this repo's code — they're independent, external templates/tools this
cockpit is built to sit alongside.

## Quick start

The fastest way to see the whole thing work, end to end, without touching your own real repos:

```bash
uat/setup.sh
```

This creates two throwaway participant repos plus a federation repo under your own GitHub account,
walks through every step (with an explanation before each one — it doubles as guided training
material), and finishes with a link to the resulting kton graph. See
[`uat/README.md`](uat/README.md) for prerequisites (`gh` CLI scopes, Go ≥ 1.25, `python3`,
claude-science or Claude Code) and `uat/cleanup.sh` to tear it all down afterwards.

## Building and developing the cockpit itself

See [`CLAUDE.md`](CLAUDE.md) — build command, subcommands (`mcp`/`init`/`doctor`), tests, repo
layout, and the manual smoke-test recipe against a real populated registry.
[`CONTRIBUTING.md`](CONTRIBUTING.md) has the three rules that are not style preferences: a
normative clause names the test that checks it, nothing in the default suite skips, and every
check says what it saw.

CI runs the suite against the kernel commit `CLAUDE.md` pins, and separately against kton `dev`
HEAD on a schedule — the second one is allowed to fail, because a moving `dev` breaking this
repository is news rather than a defect in it, and it has arrived four times as a symptom instead.

## Security

Report anything that would let a record be trusted when it should not be to the address in
[`SECURITY.md`](SECURITY.md), not as a public issue. That file also lists the limits that are
documented rather than defects — what a guard deliberately does not catch, what is attested rather
than proven, and why a verdict about carried evidence is a property of the reading rather than of
the record.

Licensed under [Apache 2.0](LICENSE).

## Where the reasoning lives

[`spec/SPEC.md`](spec/SPEC.md) is the contract: every normative clause names the test that checks
it, and states its limit beside the guarantee. [`docs/decisions/`](docs/decisions/) holds the two
decisions whose reasoning outlives the clause they produced — running in a pinned container
(ADR-003) and running with no git repository (ADR-004).
