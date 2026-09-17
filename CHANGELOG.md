# Changelog

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
