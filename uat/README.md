# UAT: end-to-end federation test

`setup.sh` and `cleanup.sh` automate the
[end-to-end tutorial](../projectdocs/claude-science-cockpit/phases/end-to-end-tutorial/tutorial.md)
— read that first for what's actually happening at each step; this file only covers what you need
to *run* the scripts.

## What this creates

Three **real, public** GitHub repositories under your own authenticated GitHub account:
`<prefix>-federation`, `<prefix>-p1`, `<prefix>-p2` (prefix defaults to a timestamp, so repeated
runs never collide). The script performs real `git push`es, opens a real GitHub issue, and
triggers a real GitHub Actions workflow run. Nothing is simulated. Run `cleanup.sh` when done —
see below.

## Prerequisites

- **`gh` (GitHub CLI), authenticated**, with these token scopes:
  - `repo` — create repos, push, open issues
  - `workflow` — trigger the federation's `mirror.yml` via `gh workflow run`
  - `delete_repo` — only needed for `cleanup.sh`, not `setup.sh`
  Check with `gh auth status`; the scopes are listed there. Re-auth with
  `gh auth refresh -s repo,workflow,delete_repo` if any are missing.
- **Go ≥ 1.25** on `PATH` (or just internet access — `go build`/`go run` auto-fetch the toolchain
  version pinned in this repo's `go.mod` if the ambient `go` is older).
- **`python3`** on `PATH` — used to patch `cockpit.config.json` and to summarize
  `mirror/union.json` at the end. No packages beyond the standard library.
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

It runs unattended through repo creation, cloning, and cockpit configuration for both
participants, then **pauses four times** with exact copy-pasteable instructions, waiting for you
to press Enter after completing each manual part in claude-science (or Claude Code):

1. Register the MCP connector for both participants.
2. Have participant 1 publish a foton.
3. Have participant 2 publish independently (deliberately not reusing participant 1's script).
4. Have participant 2 mirror the federation locally, discover the byte mismatch, correct it by
   reusing participant 1's exact script, confirm ↻2, and record the reproduction claim.

Between and after these, it registers each participant with the federation, triggers and waits on
the `mirror` GitHub Action, and finally prints the viewer URL plus a raw record count.

**Overrides:**
```bash
UAT_PREFIX=myrun uat/setup.sh          # fixed prefix instead of a timestamp
UAT_WORKDIR=/path/to/dir uat/setup.sh  # clone location (default: /mnt/c/dev/planktonReproduce/<prefix>)
```

**Resuming after a failure:** every repo-creation, cloning, keygen, and config-init step is
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
