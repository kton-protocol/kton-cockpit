# UAT: end-to-end federation test

`setup.sh` and `cleanup.sh` build a real federation across real GitHub repositories, end to end.
This file covers what you need to *run* them; `setup.sh`'s own header comment explains what happens
at each step and why four of them cannot be scripted.

## What this creates

Three **real, public** GitHub repositories under your own authenticated GitHub account:
`<prefix>-federation`, `<prefix>-p1`, `<prefix>-p2` (prefix defaults to a timestamp, so repeated
runs never collide). The script performs real `git push`es. Nothing is simulated. Run `cleanup.sh`
when done — see below.

The repos are created **empty**. Their contents come from [`participant-skeleton/`](participant-skeleton/)
in this repository, and the kernel binaries are built from a kton checkout during the run. Nothing
is cloned from a public GitHub template: this software's test material has to be something we ship
and control. The templates this used to scaffold from vendor kton **0.1** binaries, which reject
every flag the cockpit now depends on — a run from them drove the cockpit against the wrong kernel
and said nothing about it.

## Prerequisites

- **`gh` (GitHub CLI), authenticated**, with these token scopes:
  - `repo` — create repos and push
  - `delete_repo` — only needed for `cleanup.sh`, not `setup.sh`
  Check with `gh auth status`; the scopes are listed there. Re-auth with
  `gh auth refresh -s repo,delete_repo` if any are missing. (`workflow` is no longer needed: the
  aggregation is a local `kton mirror` now, not a GitHub Actions run.)
- **Go ≥ 1.25** on `PATH` (or just internet access — `go build`/`go run` auto-fetch the toolchain
  version pinned in this repo's `go.mod` if the ambient `go` is older).
- **A kton checkout**, for building `plankton`/`nekton`/`kton`. Defaults to a sibling `../kton`
  directory next to this repo; override with `KTON_SRC=/path/to/kton`. The script refuses to start
  without one rather than falling back to a binary of unknown provenance.
- **`python3`** on `PATH` — used to patch `cockpit.config.json`. No packages beyond the standard
  library.
- **`bash`**, not a POSIX-only `sh` — the scripts use bash-specific syntax.
- **`git`** on `PATH`, obviously.
- Write access to create **public** repositories under your authenticated GitHub account (the
  script always creates under the token's own user, via `gh api user --jq .login` — not an org;
  edit `GH_OWNER` in `setup.sh` if you need org-owned repos instead).
- **claude-science installed and signed in** (or the Claude Code CLI, `claude`, as the alternative
  path — see the tutorial's step 3 for both). The script itself never launches either; it pauses
  and gives you exact values to paste into whichever one you're using.
- Internet access (GitHub API/push, Go module fetching, and `npx` if you also want to try the
  tutorial's MCP Inspector manual-test aside).
- A few hundred MB of free disk for three clones plus Go build artifacts.

## Running it

```bash
uat/setup.sh
```

This doubles as guided training material: **every phase, including the automated ones, prints
what it's about to do and *why* before doing it**, and waits for you to press Enter — not just
the genuinely manual steps. There are only **4 steps you actually do yourself** in claude-science
(or Claude Code), because they need live Claude reasoning or a UI with no scriptable API:

1. Register the MCP connector for both participants.
2. Have participant 1 publish a foton.
3. Have participant 2 publish independently (deliberately not reusing participant 1's script).
4. Have participant 2 check reproduction, correct it by fetching participant 1's exact script,
   confirm 2 verified producers, and record the reproduction claim.

Everything else — repo creation, scaffolding, cockpit configuration, aggregating each participant
into the federation, and (since it's pure filesystem/config, not Claude-specific) mirroring the
federation back into participant 2 and configuring its trust tier — runs automatically once you
confirm each explained step.

**What the aggregate does and does not mean.** `kton mirror` copies signed envelopes between
registries; it does not check them. The kernel states this itself: *"Claims keep their original
signatures - mirroring is not confirming (SPEC §6)."* The old federation template additionally
re-verified every signature before aggregating, and that property is not reproduced here. It is
also not where this design's guarantee lives: `cockpit_ask` re-verifies every record against the
reading repo's configured trust tiers before returning it, so trust is established where a record
is consumed, not where it is transported.

There is no hosted viewer at the end. To look at the aggregate, point `cockpit show` at it — or a
[kton-web](https://github.com/gitmick/kton-web) checkout, which is where the browser side lives since
it was taken out of the protocol repo.

**Overrides:**
```bash
UAT_PREFIX=myrun uat/setup.sh          # fixed prefix instead of a timestamp
UAT_WORKDIR=/path/to/dir uat/setup.sh  # where repos are created (default: ../uat-runs/<prefix>)
KTON_SRC=/path/to/kton uat/setup.sh    # kton checkout to build the kernel from (default: ../kton)
```

**Resuming after a failure:** every repo-creation, scaffolding, keygen, and config-init step is
idempotent (skips if already done) — if the script dies partway through, just re-run it with the
*same* `UAT_PREFIX` rather than starting over or hand-cleaning anything.

## Cleaning up

```bash
uat/cleanup.sh <workdir printed at the end of setup.sh>
```

Shows exactly what it's about to delete (the 3 GitHub repos + the local clone directory) and
requires typing `yes` to confirm before deleting anything. It does **not** remove the
claude-science project/connector entries you created manually — remove those yourself via its UI
if desired.
