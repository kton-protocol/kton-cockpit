# Contributing

Start with [`AGENTS.md`](AGENTS.md) for building and testing, and
[`spec/SPEC.md`](spec/SPEC.md) for what the cockpit actually guarantees.

## The three rules that are not style preferences

**A normative clause names the test that checks it.** `spec/SPEC.md` carries a `> **Checked by:**`
line under every clause that states a MUST, and `spec/spec_test.go` fails if a cited test no longer
exists or if a normative section has no such line. If you change behaviour a clause depends on,
change the clause and the test in the same commit. A specification nothing checks drifts from the
code, which is a failure this project has already had one level down and does not intend to repeat
at the level of its contract.

**Nothing in the default suite skips.** If a test cannot run, it fails. This suite once spent
months reporting success while exercising none of the handlers, because it pointed at a hand-made
local clone by absolute path that had been reorganised away and every test degraded to a skip. The
container tests are behind `-tags docker` for that reason rather than skipping: asking for them
without an engine is a failure, because you asked.

**Every check says what it saw.** A test that only asserts the absence of an error passes when a
query returns nothing. Count what came back, name what was expected, and give every guard a
negative control — three refusals prove nothing if the same call also fails when it should succeed.

## The kernel

`bin/plankton` and `bin/nekton` are built from [kton](https://github.com/kton-protocol/kton), never
taken from `$PATH` or a package manager: an older binary reads the current store layout as empty
and exits 0, so the wrong kernel looks like an empty registry rather than an error.

AGENTS.md's `**Verified against:**` line names the kton commit the suite ran against, and
`TestVerifiedAgainst` compares it to the `vcs.revision` Go stamped into the binary. CI reads the
same line. When upstream has moved, rebuild, re-run, and update that line in the same commit.

After rebuilding the kernel, run that check with `-count=1`. Go's test cache does not track
`bin/plankton` or `AGENTS.md` as inputs, so it can serve a stale pass for exactly the claim the
test exists to keep honest. CI runs on a fresh runner with no cache.

## What not to add

[`spec/SPEC.md` §13](spec/SPEC.md) lists it, and each item there is checked by a named test: no own
canonicalisation, signing, registry or chain logic; no trust logic beyond applying
`cockpit.config.json`; no fourth verb; no cockpit-owned mutable state beyond that config file.

If a change would need one of these, it is a request against the kton protocol, not a cockpit-side
workaround. That has happened six times on this project and all six landed upstream; one of them
(`kton serve` being removed) would have been much easier to work around by reading the store
directly, and working around it would have been the wrong answer.

The line is narrower than "no cryptography": verifying a certificate chain with `crypto/x509`
against roots the configuration names is applying the configuration. Re-verifying a stored
transparency-log entry is not — that is transparency-log cryptography and belongs in the kernel.

## Before opening a pull request

```bash
gofmt -l .                  # must print nothing
go vet ./...
go test ./...
go test -tags docker ./...  # if you touched internal/container or publish's execution path
examples/run-all.sh
```

Security reports go to the address in [`SECURITY.md`](SECURITY.md), not to an issue.
