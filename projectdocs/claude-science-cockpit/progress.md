---
title: "claude-science-cockpit — Progress"
project: "claude-science-cockpit"
updated: 2026-07-22
---

# Progress Tracking

## Summary

| Phase | Status | Entries |
|---|---|---|
| Design | done | 6 |
| Build | done | — |
| End-to-End Tutorial | active | 1 |

## Log

- **2026-07-22** — Built the full picture first: read the governing `Bau-Spezifika.md`, the
  kton.dev protocol overview, the `plankton-federation-template` REPRODUCE.md, and the actual
  state of `deathbychoco/my-federation` + `participant-alice/bob/carol` (all healthy — mirror
  converging, all three registered and reproducing). Found six separate local
  `plankton-data`/`nekton-data` folders scattered under `/mnt/c/dev`, which explains the
  wrong-folder incident from the live demo run.
- **2026-07-22** — Read the actual `plankton`/`nekton` reference CLI usage blocks
  (`reference/cmd/plankton/main.go`, `nekton/reference/cmd/nekton/main.go`) and the participant
  template's `CLAUDE.md` (today's manual workflow) to ground the cockpit's three verbs in the
  real command surface rather than the spec's abstractions alone. Confirmed `plankton verify` /
  `nekton verify` (verify against an explicit pubkey, ignoring the declared `keyid`) already exist
  as binary subcommands — so the spec's "verified, not declared" requirement needs no new crypto
  in the cockpit, just calling these commands correctly.
- **2026-07-22** — Decided: Go implementation, MCP stdio server (not a CLI + Bash-permission
  approach) for the three verbs, with tightened Bash permissions as defense-in-depth on top. See
  [ADR-001](decisions/adr-001-go-mcp-server.md).
- **2026-07-22** — Designed the anti-wrong-folder guard (repo-root + git-remote verification on
  every MCP call), the `cockpit.config.json` schema, and the detailed behavior of all three tools.
  See the [design phase](phases/design/index.md) entries.
- **2026-07-22** — Corrected a placement mistake: initially wrote this project's *sibling*
  (`claude-workflow-manager`, the VS Code extension) project docs inside the extension's own
  source repo (`claudeVSDx/projectdocs/`) instead of here. Moved to
  `projectdocs/claude-workflow-manager/` in this repo, where the actual work session lives.
- **2026-07-22** — Corrected a second sequencing mistake: started `go mod init` before writing up
  this project's own design docs. Paused implementation to write this project's docs first
  (this file and its siblings), capturing the design in full before any code exists.
- **2026-07-22** — Built v1 end to end: Go module, `internal/config` (schema + loader + the
  repo/remote-match guard), `internal/gitops`, `internal/binaries` (plankton/nekton process
  wrappers), `internal/verify` (trust-tier resolution), `internal/tools` (publish/say/ask), and
  `cmd/cockpit` wiring an MCP stdio server via the official `modelcontextprotocol/go-sdk` v1.6.1.
  Clean `go build`/`go vet`.
- **2026-07-22** — Smoke-tested against real data rather than fixtures: `cockpit init`/`doctor`
  against the actual `participant-alice-1` clone (real signed fotons/claims from the live demo
  run) — correctly derived `deathbychoco/participant-alice` from its real git remote, validated
  every path/key/template. `cockpit_ask` (producer + reproductions queries) against that same
  real registry returned correctly-verified records. Directly verified the anti-wrong-folder
  guard against the exact failure mode that motivated this project: a `cockpit.config.json` bound
  to `participant-alice` sitting in a repo whose actual origin is `participant-bob` — the cockpit
  refused outright with a clear error, rather than silently acting against the wrong repo.
- **2026-07-22** — Wrote the [end-to-end tutorial](phases/end-to-end-tutorial/tutorial.md):
  create the template repos → configure the cockpit in a participant repo → register it as an MCP
  server → start Claude Science → publish a foton → make a claim → register with a federation →
  verify via the viewer and `cockpit_ask`.
- **2026-07-22** — Fixed two bugs in the tutorial found by actually following it against a real
  new participant repo (`participant-wolfi`): (1) the build command was run from the participant
  repo with an absolute path to the cockpit source as the package argument — `go build` resolves
  the module from cwd, not the argument, so this failed with "go.mod file not found"; fixed by
  building from the cockpit source dir and writing `-o` straight into the participant repo. (2)
  `plankton/nekton keygen` into `keys/` failed on a fresh clone — `keys/` is `.gitignore`d and
  starts empty, so git never materializes the directory on checkout, and keygen doesn't create
  missing parent dirs; fixed by adding `mkdir -p keys` first. Both confirmed fixed by rerunning
  them for real in `participant-wolfi` (keys generated, copied to `registry/keys/`).
- **2026-07-22** — Corrected a wrong assumption in step 4: "Claude Science" isn't a figure of
  speech for the `claude` (Claude Code) CLI I've been running this whole session — it's a
  distinct, already-installed product (`claude-science`, a sandboxed daemon + browser UI, "run
  Claude on your data, locally, in your browser"). Confirmed via a UI screenshot that it has a
  native **Local command** connector type for exactly this use case (alongside Remote URL
  connectors, which is all its cached `directory-cache.json` otherwise showed — those are
  claude.ai-style hosted MCP connectors like Atlassian, a red herring for our purposes). Updated
  the tutorial to cover both paths (claude-science's Connectors UI vs. Claude Code CLI's
  `.mcp.json`), and flagged one still-unverified caveat: whether the Local command connector's
  working directory is guaranteed to be the participant repo (this matters directly for the
  anti-wrong-folder guard, which checks `git remote get-url origin` from its own cwd).
- **2026-07-22** — Resolved that caveat from a screenshot of claude-science's actual "Add
  connector" → Local command dialog: it only has Name / Command / env vars / Description —
  **no working-directory field at all**. So the tutorial no longer just says `bin/cockpit mcp`;
  the Command value must itself pin the directory: `bash -c "cd /ABSOLUTE/PATH && exec bin/cockpit
  mcp"`. Also clarified two points of confusion along the way, worth remembering for future
  explanations: (1) `cockpit` is one binary with `init`/`doctor` (human CLI) and `mcp` (starts the
  MCP server) as subcommands — not a CLI-plus-separate-MCP-server pair; (2) the three verbs
  (publish/say/ask) aren't sibling subcommands next to `init`/`doctor`/`mcp` — they're the three
  MCP "tools" that only exist *inside* `mcp` mode, callable only by an MCP client (claude-science
  or Claude Code), never directly from a terminal.
- **2026-07-22** — The `bash -c "cd ... && exec ..."` connector command turned out not to work at
  all: claude-science's managed MCP runtime has no `bash` (only node/npx/python3, or an absolute
  path to an installed binary). Fixed properly instead of working around it: added a
  `COCKPIT_REPO_DIR` environment variable to `config.Load` — set once, in the connector's own env
  vars field (which the dialog does support), it replaces cwd-based repo-root resolution. This is
  not the kind of ambient env var the guard exists to distrust: it's set by the human operator at
  connector-configuration time, same place as the command itself, never reachable by Claude at
  runtime, and it only ever answers "which repo," not "which registry paths within it" (those are
  still always derived from the resolved repo root, never from the environment). Key clarification
  worth remembering: an absolute path to the binary only answers "which program to run," not
  "what working directory does it run in" — those are independent, and only the latter is what
  the anti-wrong-folder guard actually checks.
- **2026-07-22** — While verifying the fix, found the existing smoke tests had gone stale (they
  pointed at `participant-alice-1`, which no longer exists after the local demo repos got
  reorganized into `participant-christian`/`participant-wolfi`) and, worse, `os.Chdir` into a
  since-deleted directory on this WSL/NTFS setup returns no error while leaving `os.Getwd()`
  broken — so the tests were silently running from a broken cwd instead of skipping cleanly.
  Repointed the fixture at `participant-christian` and hardened the `chdir` test helper to check
  `os.Getwd()` too. Also found and fixed a real bug this surfaced: `plankton reproductions` exits
  non-zero for the legitimate "0 distinct producers" case, which `Ask()` was surfacing as a tool
  error instead of a correct zero-count answer — fixed in `binaries.Reproductions` (same
  never-treat-nonzero-as-failure convention already used for `Reproduces`). All 5 tests pass,
  including one confirming `COCKPIT_REPO_DIR` actually works. Rebuilt `bin/cockpit` in
  `participant-christian` with these fixes.
- **2026-07-23** — Confirmed end to end in the real claude-science UI: the Local command connector
  (absolute binary path + `COCKPIT_REPO_DIR` env var) successfully loaded all three tools in
  `participant-christian`, showing the exact descriptions from the code. First real integration
  point of this project fully working outside of Go tests.
- **2026-07-23** — Step 4 (`claude-science serve --here`) hit a real, unrelated OS limit:
  `--here`'s data-dir socket path under `/mnt/c/dev/planktonReproduce/participant-christian/...`
  exceeded the 108-byte `AF_UNIX sun_path` cap. Fixed by switching the tutorial to
  `claude-science serve --data-dir ~/.claude-science-<name>` (short, per-participant-identity)
  instead of `--here` — costs nothing since `COCKPIT_REPO_DIR` already made the connector's repo
  binding independent of claude-science's own cwd, and additionally keeps each identity's
  connector list from bleeding into the others.
- **2026-07-23** — Chased what looked like a filesystem/sandbox problem through several dead
  ends: first suspected the 9p-mounted `/mnt/c/...` path couldn't be exec'd from claude-science's
  sandbox (moved the binary to `~/.local/bin` — same error); then got an explicit error naming the
  real mechanism ("command not found inside the MCP sandbox: most of the home directory is not
  visible there — install on a system path (/usr/local, /opt)") and was about to relocate the
  binary to `/usr/local/bin` (needs `sudo`, couldn't do it without the user's password). Turned
  out to be neither: the actual variable was which **browser context** was connected to the
  daemon. Opening the login link in a genuine external browser tab instead of an embedded view
  inside VS Code made the exact original command (plain absolute path into the participant repo,
  no relocation) work immediately. Added this as the first thing to check for any
  sandbox/visibility-sounding MCP error in the tutorial, since it's non-obvious and easy to chase
  the wrong lead (as this session did, twice).
- **2026-07-23** — Simplified step 4 back down after confirming the plain default instance
  (`claude-science serve`, no `--data-dir`/`--here`) already has its own project/session concept —
  previous participant sessions (e.g. `participant-christian`) show up in its own sidebar — so the
  whole `--data-dir ~/.claude-science-<name>` recommendation from earlier today was solving a
  non-problem, introduced only to avoid a connector-list collision that claude-science already
  handles itself via sessions. What actually needs to be per-participant is the *connector name*
  (step 3: `cockpit-christian`, `cockpit-wolfi`, ...), since connectors are global to the account,
  not scoped per session. Net effect: step 4 is now just `claude-science serve`.
- **2026-07-23** — Step 5 reproduced the exact original incident that motivated this whole
  project: before ever calling `cockpit_publish`, Claude confidently stated "the repo root is at
  /mnt/c/dev/claudeScience/warfarinTest/" and started creating directories and fetching data
  there — the wrong repo entirely. Traced to the actual root cause via claude-science's own
  sqlite db (`~/.claude-science/orgs/<org>/operon-cli.db`): its cross-project **memories** feature
  stored detailed workspace/path context from an earlier, unrelated `warfarinTest` project
  (`subject_project_id = proj_ff0bdeede90f`) and surfaced it into the `participant-christian`
  conversation (`proj_eadcc516045d`) despite the two being distinct projects in claude-science's
  own data model — a cross-project memory leak, not a cockpit bug. Confirmed the cockpit's own
  guard was never actually at risk: this happened entirely through plain file/Bash operations
  before `cockpit_publish` was ever called, and the guard would have hard-refused there regardless
  (no `cockpit.config.json` in `warfarinTest` binds it to `participant-christian`'s remote) — so
  no registry data was ever at risk, only wasted setup work in the wrong folder. Mitigation added
  to the tutorial: disable claude-science's Memory capability per participant project (or at least
  check it first), and state the absolute repo path explicitly in the first message of every new
  session as a cheap defensive habit on top of that.
- **2026-07-23** — Before the first push, ran an independent cold review (fresh agent, no prior
  context on this project) reading the tutorial top-to-bottom as a brand-new user, then checking
  every technical claim against the actual Go source and rerunning `go build`/`vet`/`test`. Found
  and fixed: (1) a real functional bug — the tutorial's `trust.tiers.self` example only listed the
  plankton pubkey, omitting the nekton one, which would leave every claim the user's own
  `cockpit_say` produces resolving as unverified/excluded; (2) step 7 never had the reader add
  peer participants' pubkeys to any trust tier, so step 8's promise of cross-participant
  verification wouldn't actually hold as written — added that as an explicit sub-step; (3)
  `anti-wrong-folder-guard.md` flatly said "never an environment-variable override," directly
  contradicted by `COCKPIT_REPO_DIR` added earlier the same day — reconciled; (4) `_project.md`
  and `cockpit-config-schema.md` both claimed startup validation is JSON-Schema-driven, when
  `internal/config.validate` is actually a hand-written required-field check — corrected to match
  `CLAUDE.md`'s already-accurate wording. Also fixed several minor/cosmetic items: an
  over-scoped `AskFilter` design description (only `trustTier` shipped, not the other four
  originally-designed fields), a stale `participant-alice-1` reference in the tutorial index, an
  unexplained `data/penguins.csv` in step 5 (it ships with the template — confirmed against both
  the local repo and the real `gitmick/plankton-participant-template` on GitHub), a wording
  tension between "no cockpit-owned mutable state" and the `corpus` manifest file `cockpit_publish`
  can write, and `CLAUDE.md`'s opening line assuming "Claude Code" without acknowledging
  claude-science as an equally valid, architecturally distinct client. `go build`/`vet`/`test` all
  still pass after these doc-only fixes.
- **2026-07-23** — Rewrote the tutorial in a formal, third-person register: removed first-person
  narrative framing ("we hit", "we had Claude", "we initially thought") throughout, replacing each
  with a direct statement of the underlying fact or instruction. Content unchanged — this was a
  register/tone pass only, not a technical revision.
- **2026-07-23** — **Correction to the 2026-07-23 entry above about "browser context":** that
  conclusion was wrong, reached from a single coincidental data point rather than verified
  evidence. Re-checked by reading both claude-science instances' own log files
  (`<data-dir>/logs/server-*.log`) directly and correlating every attempt: **every** "not
  found"/sandbox-visibility failure occurred in the `--data-dir ~/.claude-science-christian`
  instance, across four separate daemon restarts of that instance, regardless of the binary's
  location; **every** success occurred in the plain default instance, on its first attempt. The
  actual variable was which claude-science *instance* was used, not which browser tab viewed it —
  those happened to correlate with which instance was being tested at the time, which is what
  produced the wrong conclusion. Most likely explanation: a sandbox's file-access grants
  accumulate over time via approval cards, and a freshly created `--data-dir` instance starts with
  none of that history. The tutorial's step 4 already recommended the default instance (for the
  separate, correct reason of avoiding connector-list collisions) — its reasoning has been
  corrected to include this stronger, log-verified justification, and the incorrect
  browser-tab-specific warning has been removed rather than left alongside a now-superseded claim.
- **2026-07-23** — While investigating the above, found a second, independent, real bug in the
  same log: three actual `cockpit_publish` calls had failed with `MCP error 0: validating tool
  output: validating root: validating /properties/permalinks: type: <invalid reflect.Value> has
  type "null", want "object"` (and the same for `/properties/outputHashes`). Root cause: `errResult`
  returns each tool's Out struct zero value on any error path; for `PublishOutput`'s
  `OutputHashes`/`Permalinks` map fields, that zero value is `nil`, which `encoding/json` marshals
  as `null`. The MCP Go SDK infers each tool's output JSON schema from the struct via reflection
  and marks every field without `omitempty`/`omitzero` as required, and validates the marshaled
  output against it regardless of `IsError` — so a `null` map fails validation, and the resulting
  confusing schema error replaces whatever the tool's actual, useful error message was. Confirmed
  the exact mechanism by probing `jsonschema-go` directly: validating `{"hashes":null}` against a
  schema for a required, non-omitempty map field reproduces the production error text
  byte-for-byte. Fixed by adding `omitempty` to `PublishOutput.OutputHashes`/`.Permalinks` and
  (defensively, same class of bug) `AskOutput.Records`/`.Included`/`.Excluded`. Added
  `internal/tools/mcp_wire_test.go`: schema-level tests (`TestOutputSchema_*`) that marshal each
  Out struct's zero value and validate it against its own inferred schema — confirmed these fail
  without the fix (reproducing the exact error for `PublishOutput`; `AskOutput`'s zero value
  passed even unfixed, an unexplained map-vs-slice asymmetry in the validator not worth chasing
  further, since `omitempty` is correct either way) — plus wire-level tests via an in-memory MCP
  client/server, which did not reproduce the failure themselves (a gap in the wire tests, not
  evidence the bug was less real; the schema tests are what actually guard against a regression
  here). All 10 tests pass with the fix applied. Rebuilt `bin/cockpit` in `participant-christian`.
- **2026-07-23** — Added a quick manual test to step 3: the official MCP Inspector's `--cli` mode
  calls a tool directly (no browser, no claude-science) — demonstrated live with `cockpit_ask`
  against `participant-christian`, returning a correct structured result. Documented as the way to
  isolate whether a failure is in the cockpit itself versus claude-science's environment.
- **2026-07-23** — Wrote `uat/setup.sh` and `uat/cleanup.sh`: an end-to-end UAT harness that
  creates a federation + 2 participant repos from the official templates, clones them, builds the
  cockpit into each, generates signing identities, configures `cockpit.config.json` (including
  the trust-tier fix from the cold review), registers both participants with the federation
  (issue-form body format and `mirror.yml`'s `workflow_dispatch` support confirmed against the
  actual template source rather than guessed), triggers an immediate mirror, and prints the
  resulting graph (viewer URL + a raw foton/claim count from `mirror/union.json`). Deliberately
  does not automate claude-science's connector registration or the actual publish/reproduce step
  — both are UI-driven or require live Claude reasoning, confirmed to have no documented CLI/API
  (a `custom_mcp_servers` table exists in claude-science's own sqlite db, but writing to it
  directly was rejected as an undocumented, fragile approach) — the script pauses at exactly those
  two points with copy-pasteable instructions. `cleanup.sh` reads a state file the setup script
  writes (rather than accepting typed repo names) and requires explicit confirmation before
  deleting anything. Verified: bash syntax (`bash -n`), and that the generated GitHub issue body
  has no stray whitespace that would break `register.py`'s parsing regex. Not yet verified: a live
  end-to-end run (would require actually creating real GitHub repos, which only the user should
  choose to do by running the script themselves).
- **2026-07-23** — First live run of `uat/setup.sh` hit a real bug in step 3: the build command
  used `-C "$COCKPIT_SRC"` positioned after `-o`, which Go rejects ("-C flag must be first flag on
  command line") — the exact ordering mistake the tutorial had already worked around with a
  `cd`-based approach; the UAT script had reintroduced it independently. Fixed with the same
  `(cd "$COCKPIT_SRC" && go build ...)` subshell pattern, verified from a real unrelated cwd.
  While fixing this mid-run, made repo creation, cloning, keygen, and `cockpit init` all
  idempotent (skip-if-already-done) so a script that dies partway through — as this run just did —
  can be resumed by re-running with the same `UAT_PREFIX` instead of requiring a full restart or
  manual cleanup. Also hardened the mirror-run-id lookup (was a single fixed 10s sleep, a plausible
  race on a brand-new repo) into a 60s poll loop.
- **2026-07-23** — Investigated a report that the cockpit connector "seems to be per session, not
  per project" — the user's suspicion, correctly, since this contradicted the tutorial's own
  unverified claim that connectors are account-global. Searched exhaustively for where Local
  command connector config is actually persisted (every table in claude-science's sqlite db,
  every file under its data directory) and found nothing — no config file for these connectors
  exists anywhere on disk. Asked the user to test directly: the connector had vanished from every
  project/session, not just a new one, and `claude-science status` showed the daemon's pid/uptime
  had genuinely changed (a restart, ~11 minutes prior) since it last worked. Conclusion: Local
  command connectors are apparently kept only in the running daemon's memory, not persisted at
  all — the "per session" appearance was actually "per daemon-process-lifetime," and a foreground
  `claude-science serve` stopping when its terminal closes/gets reused is a plausible way this
  session hit it repeatedly without a deliberate restart. Documented in the tutorial: use
  `--detached`, and expect to re-add connectors after any restart (rebuilding the cockpit binary
  itself does not require one). Corrected the earlier "connectors are account-global, not scoped
  per project/session" claim, which was never actually verified, to reflect only what's confirmed
  (shared across a given daemon's projects/sessions while it's running).
- **2026-07-24** — Fixed a real ambiguity in `uat/setup.sh`'s step 5 instructions: it told the
  user to give the p2 session "the same thing" as p1's instruction, which literally read as
  "Work only in .../p1" — exactly the kind of wrong-directory instruction this whole project
  exists to prevent, and the user correctly caught it before acting on it. The script now spells
  out p2's instruction in full, with its own directory, plus an explicit note that it is not a
  copy-paste of p1's.
- **2026-07-24** — A live `cockpit_publish` run failed at `git commit`: no git identity
  (`user.name`/`user.email`) was configured in claude-science's sandbox, and `gitops.CommitAndPush`
  had never set one — it silently relied on the ambient environment already having git configured,
  which is a real gap the user correctly flagged ("shouldn't the git identity be set already in
  cockpit configs?"). Claude's own recovery attempt (writing identity into the repo's
  `.git/config` directly) hit a further real, environment-specific failure — "could not write
  config file .git/config: Device or resource busy" — something in that sandbox appears to hold
  the file open. Fixed properly rather than documenting the workaround: `git commit` now always
  passes `-c user.name=... -c user.email=...` directly on the command line, derived from
  `identity.session_id` already in config (no new config field needed) — this never writes to any
  config file at all, so it can't hit that busy-file failure regardless of its actual cause, and
  makes `cockpit_publish`/`cockpit_say` fully self-contained rather than depending on the ambient
  environment having git pre-configured. All tests still pass; rebuilt `bin/cockpit` in
  `participant-christian`.
- **2026-07-24** — `uat/setup.sh`'s step 4 instructions predated the `GITHUB_TOKEN` push-auth fix
  and never mentioned it. Added it to both participants' printed env-var instructions (fetched
  live via `gh auth token` so the script prints the actual value, not just a placeholder), plus an
  explicit warning to put each env var on its own line — the exact concatenation bug hit live
  earlier today (`COCKPIT_REPO_DIR`'s value swallowing `GITHUB_TOKEN=...` when both landed on one
  line).
- **2026-07-24** — `uat/setup.sh` step 6 failed: `gh issue create --label registration` requires
  the label to already exist in the repo — `gh`'s API path doesn't auto-create a template's
  declared labels the way GitHub's own issue-form UI does. Already handled this correctly for
  `approved` but missed `registration`. Fixed by creating both labels (idempotently, `|| true`)
  before creating the issue.
- **2026-07-24** — Restructured the tutorial and `uat/setup.sh` around a federation-first
  sequence, worked out collaboratively: participant 1 publishes and is registered/mirrored into
  the federation *before* participant 2 does anything (not both publishing first, then
  registering together, as originally written). This makes a real cross-participant reproduction
  actually mechanically possible: participant 2 clones the federation repo and runs
  `plankton mirror`/`nekton mirror` against its `mirror/` directory — confirmed against the real
  reference implementation that this works even though `mirror/` mixes fotons and claims in one
  directory (`registry.Add()` rejects non-matching payloads without erroring the whole mirror,
  checked directly in `reference/registry/registry.go`) — plus adds the peer's pubkey to a
  `federation` trust tier (mirroring data and configuring trust are two separate, both-required
  steps). The sequence also now honestly includes the realistic failure: participant 2 writes its
  own `clean.py` independently first, checks reproduction (likely ↻1, bytes differing), and only
  then corrects by fetching participant 1's exact script via its foton's permalink and
  republishing — rather than assuming byte-identical reproduction on the first try, which is what
  actually happened in the live UAT run that prompted this whole redesign. `uat/setup.sh` now
  pauses 4 times (was 2) and registers/mirrors each participant separately as it publishes, not
  both together at the end. Added `uat/README.md` with full prerequisites (gh scopes, Go,
  python3, claude-science account, what gets created, override env vars, resuming after failure,
  cleanup) for anyone else who wants to run it, not just the original session.
- **2026-07-24** — Reduced manual interaction and turned `uat/setup.sh` into guided training
  material at the same time. Moved the mirror-federation-locally + trust-tier-configuration part
  out of the manual Claude-conversation instructions and into the script itself, since both are
  pure git/config operations that never actually needed Claude's reasoning — cutting one whole
  manual pause. Added a `step()` helper (paired with the existing manual-step helper, renamed
  `manual()`) that prints what a phase is about to do and *why*, then requires Enter before
  running it — applied to every automated phase, not just the manual ones, so the script reads as
  an explained walkthrough rather than a silent black box. Net: 4 manual steps (was 4, but one of
  those four used to include the now-automated mirror/trust-tier work bundled in), and every other
  phase — including plain repo creation and cloning — now explains itself before running.
- **2026-08-03** — A live run's step 8 (register participant 2) hit a genuine race condition:
  labeling the issue `approved` triggers the federation's own separate `register.yml` workflow
  (commits `participants.json`), and the script immediately dispatched `mirror.yml` right after
  without waiting for that commit to land — `mirror.yml`'s own commit got rejected as
  non-fast-forward (`! [rejected] main -> main (fetch first)`, confirmed from the actual run log)
  since its `concurrency: group: mirror` only guards against overlapping *mirror* runs, not this
  separate workflow. Fixed by having `register_participant` wait for `register.yml` to fully
  complete before returning (so `trigger_mirror_and_wait` never starts concurrently with it), plus
  a retry in `trigger_mirror_and_wait` itself for defense-in-depth. While implementing this, caught
  a second, subtler bug before it shipped: naively polling for "the most recent run" doesn't work
  when a workflow has run before (register.yml runs once per participant, mirror.yml can retry) —
  it can match an old, already-completed run instead of the newly-triggered one. Fixed by
  capturing the latest run id *before* triggering and polling for one different from it
  (`wait_for_new_workflow_run`). Manually recovered the live run's federation repo (re-triggered
  the mirror, confirmed both participants now correctly present in `participants.json`) so it
  could continue from where it was without restarting.
- **2026-08-03 (later)** — Step 9 recovery after a network outage exposed a second bug: running
  `./bin/plankton mirror`/`./bin/nekton mirror` directly (as `uat/setup.sh` does — these are pure
  git/config operations, never routed through the cockpit's MCP tools) without setting
  `PLANKTON_DIR`/`NEKTON_DIR` silently made both binaries fall back to their own bare defaults
  (`./plankton-data`, `./nekton-data`), which don't exist in a participant repo — producing a
  believable-looking but wrong "0 new; registry holds 0 fotons" instead of an error. This surfaced
  a real design question: should `cockpit.config.json`'s `plankton_dir`/`nekton_dir` just *be*
  `plankton-data`/`nekton-data` so raw invocations degrade gracefully instead of silently pointing
  nowhere? Investigated by reading the federation template's actual `scripts/mirror_once.py`
  (`gitmick/plankton-federation-template`, vendored — not ours to change): it hardcodes the literal
  prefixes `registry/plankton/objects/`, `registry/nekton/objects/`, and `registry/keys/*.pub` when
  scanning a participant's committed tree. So `registry/plankton`/`registry/nekton` is not a
  cosmetic choice we get to align with the binaries' bare fallback — it's a hard requirement of the
  external aggregation protocol; renaming would make federation mirroring find nothing for that
  participant. (Initially reasoned the other way — that the non-default name was itself a
  deliberate defense against colliding with legacy `plankton-data`/`nekton-data` folders elsewhere
  under `/mnt/c/dev` — but that doesn't hold up: the guard's actual protection is the git-remote
  check plus deriving `PLANKTON_DIR`/`NEKTON_DIR` as absolute paths from a verified repo root
  (`internal/config/config.go`), which works identically regardless of what the relative subpath
  is named. Retracted that reasoning once challenged.) Fixed the real bug instead, at its actual
  root: `uat/setup.sh`'s step 9 now reads `plankton_dir`/`nekton_dir` out of the repo's own
  `cockpit.config.json` via `python3` and exports `PLANKTON_DIR`/`NEKTON_DIR` explicitly before
  calling the binaries directly — mirroring what `internal/binaries/binaries.go` already does for
  every cockpit-mediated call, so no code path ever relies on the binaries' bare defaults. Also
  annotated `cockpit.config.schema.json`'s `plankton_dir`/`nekton_dir` fields with this constraint
  directly, so the "why can't this just be the bare default" question doesn't need re-deriving from
  `mirror_once.py` again next time.
- **2026-08-03 (later still)** — The PLANKTON_DIR/NEKTON_DIR fix above was necessary but NOT
  sufficient: step 9 still reported "0 new" against a live run even with the env vars correctly
  set. Root cause, found by testing directly against the binaries: `plankton mirror`/`nekton
  mirror` expect a PEER's flat registry layout (`objects/sha256/<hash>.json`, no subdirectory —
  confirmed from the binary's own usage string, `plankton mirror ../session-1/plankton-data`, i.e.
  another participant's own registry dir). The federation's `mirror/` directory instead SHARDS
  objects by a 2-hex-char prefix subdirectory (`objects/sha256/<xx>/<hash>.json`) —
  `build_mirror.py`'s own convention, built for the GitHub Pages viewer, structurally incompatible
  with what either binary's mirror command scans for. Passing the federation's `mirror/` straight
  to `plankton mirror`/`nekton mirror` is silently a no-op regardless of PLANKTON_DIR/NEKTON_DIR —
  confirmed by copying one sharded object into a flat scratch dir and re-mirroring from that:
  "1 new" vs "0 new" from the sharded source, isolating the layout mismatch as the actual cause.
  Fixed `uat/setup.sh`'s step 9 to flatten the federation's sharded objects into a scratch temp dir
  before calling either binary. Separately investigated nekton's "2 unresolved (missing dependency
  - an incomplete chain)" message from the same debugging session and confirmed it's benign: it
  only appears when nekton's mirror scan walks real plankton foton files sitting in the same
  `objects/sha256/` tree (it can't parse a foton as a claim, so it reports "unresolved" rather than
  cleanly skipping it) — proved by mirroring from a genuinely empty source, which produces no such
  warning at all. It doesn't miscount or corrupt anything (`registry holds 0 claim(s)` stayed
  correct throughout) — cosmetically alarming, functionally harmless. Verified the fully corrected
  step 9 end-to-end against the live run: p2's local registry now genuinely holds both
  participants' fotons, and `plankton reproductions` run directly against p2's own registry
  returns the real ↻1 result matching what a `cockpit_ask` call from p2's session should now
  correctly report.
