---
title: "Claude-Science-Cockpit: end-to-end tutorial"
phase: "end-to-end-tutorial"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [tutorial]
---

# Claude-Science-Cockpit: end-to-end tutorial

## Summary

Zero to a verified federation with a real cross-participant reproduction: create the repos,
configure the cockpit for two participants, have the first publish and federate, have the second
publish independently, mirror the federation's aggregate back down locally, discover and correct
a byte mismatch, record the reproduction claim, and verify.

## Details

### 1. Create the repos (one-time, human, via `gh`)
```bash
gh repo create <you>/my-federation --template gitmick/plankton-federation-template --public
# in the new repo's GitHub settings: enable Actions (read/write) + Pages

gh repo create <you>/participant-x --template gitmick/plankton-participant-template --public
git clone https://github.com/<you>/participant-x && cd participant-x
```

### 2. Configure the cockpit in the participant repo (one-time, human)
```bash
# The cockpit binary must be built from its own source directory: `go build` resolves the module
# from the current working directory, not from the package argument, so this cannot run from
# participant-x/. Capture the participant repo's path first, then switch to the cockpit source:
PARTICIPANT_DIR="$(pwd)"
cd /mnt/c/dev/planktonClaudeScienceCockpit
GOOS=linux GOARCH=amd64 go build -o "$PARTICIPANT_DIR/bin/cockpit" ./cmd/cockpit
cd "$PARTICIPANT_DIR"

# Create a signing identity (only if this repo does not already have one — do not re-run if it
# does; regenerating orphans any claims already signed under the old key).
# Note: keys/ is listed in .gitignore (private keys must never be committed) and starts empty in
# a fresh clone, so git never materializes the directory on checkout. Create it first, or keygen
# fails with "no such file or directory":
mkdir -p keys
bin/plankton keygen keys/session-1
bin/nekton   keygen keys/session-1-claims
cp keys/session-1.pub keys/session-1-claims.pub registry/keys/
git add registry/keys && git commit -m "signing identity" && git push

# Scaffold and finish the config
bin/cockpit init                       # fills repo.owner/name from the repo's actual git remote
# Edit cockpit.config.json: trust.tiers.self must list both pubkeys, not only the plankton one —
# fotons verify against the plankton key, claims (what cockpit_say produces) verify against the
# nekton key; omitting either leaves that half of this identity's own records unverified/excluded:
# ["registry/keys/session-1.pub", "registry/keys/session-1-claims.pub"]
bin/cockpit doctor                     # every line should read [ok]
```

### 3. Register the cockpit as an MCP server (one-time)

**When using `claude-science`** (the sandboxed, browser-UI application — the "Claude Science"
this project is named for): open its UI → **Connectors** → **Add connector** → **Local command**.
That dialog has only Name / Command / environment variables / Description — no separate
working-directory field — and its managed runtime provides no `bash` (only node/npx/python3, or
an absolute path to an installed binary), so a shell `cd` wrapper is not usable here. Instead, the
repository to bind to is passed via an environment variable read for exactly this case
(`COCKPIT_REPO_DIR` — set once, at configuration time, by the operator; not reachable by Claude at
runtime):

| Field | Value |
|---|---|
| Name | `cockpit-x` — name per participant identity (`cockpit-christian`, `cockpit-wolfi`, ...), not simply `cockpit`. A running daemon's connectors appear to be shared across its projects/sessions, so distinct names are required to avoid collisions across identities. |
| Command | `/ABSOLUTE/PATH/TO/participant-x/bin/cockpit mcp` |
| Environment variables | `COCKPIT_REPO_DIR=/ABSOLUTE/PATH/TO/participant-x` |
| Description | optional, e.g. "publish/say/ask for this kton participant repo" |

Both the absolute binary path and the environment variable are required, and they answer
different questions. The path answers only "which program to run" (required since the runtime
performs no shell or `PATH` resolution); it does not make the process's working directory match
the binary's location. The anti-wrong-folder guard checks the actual working directory
(`git remote get-url origin` from its own process cwd) — `COCKPIT_REPO_DIR` is what pins that,
independent of whatever cwd the runtime actually launches the process from.

**When instead using the Claude Code CLI** (`claude` in a terminal — not claude-science):
```jsonc
// .mcp.json (repo root)
{ "mcpServers": { "cockpit": { "command": "bin/cockpit", "args": ["mcp"] } } }
```

> Validated (2026-07-22, `participant-christian`): registering the connector with the values above
> made claude-science's Connectors page list all three tools —
> `cockpit_ask`/`cockpit_publish`/`cockpit_say` — with the descriptions defined in the code.
> "Skip approvals" left off is expected: each tool call then shows an approval card the first time.

> ⚠️ **Local command connectors do not appear to survive a claude-science daemon restart.**
> Confirmed (2026-07-23): after registering successfully, the connector was later found gone from
> every project — traced to the daemon having actually restarted (a new pid, ~11 minutes of
> uptime) in the meantime, not to any per-project/per-session scoping. No config file for these
> connectors could be found anywhere under claude-science's data directory (database included) —
> they appear to exist only in the running daemon's memory. Practical consequences: run
> `claude-science serve --detached` rather than foreground `serve`, since a foreground daemon
> stops the moment its terminal closes or gets reused, which is an easy way to trigger this
> accidentally; and expect to re-add every Local command connector after any restart, deliberate
> or not. Rebuilding the cockpit *binary* itself does not require restarting the daemon — just
> click **Reconnect** on the existing connector.

#### Quick manual test, independent of claude-science

To confirm the cockpit itself works — isolated from any claude-science instance, sandbox, or
connector configuration — call a tool directly via the official MCP Inspector's CLI mode (no
browser required):
```bash
COCKPIT_REPO_DIR=/ABSOLUTE/PATH/TO/participant-x \
  npx --yes @modelcontextprotocol/inspector --cli /ABSOLUTE/PATH/TO/participant-x/bin/cockpit mcp \
  --method tools/call \
  --tool-name cockpit_ask \
  --tool-arg query=producer \
  --tool-arg ref=sha256:0000000000000000000000000000000000000000000000000000000000000000
```
`cockpit_ask` is read-only, so this is safe to run at any time. A working cockpit returns a JSON
result (an empty/not-found record for this placeholder hash is expected — the point is that a
result comes back at all, not that this specific hash resolves to anything). If this fails but
the claude-science connector also fails, the problem is in the cockpit itself; if this succeeds
but the claude-science connector still fails, the problem is specific to claude-science's
environment (its sandbox, its instance/data-dir, or the connector configuration) — see step 4.

#### Optional: how this is wired together, and why

This section may be skipped if step 3 has already succeeded. It documents the underlying
mechanics for reference, not additional required steps.

- **`cockpit` is a single binary, not a CLI plus a separate server.** It has three subcommands:
  `init`/`doctor` (plain CLI, print text and exit) and `mcp` (does not print text and exit — it
  reads/writes JSON-RPC on stdin/stdout indefinitely). Running `cockpit mcp` is starting the MCP
  server; there is no second program involved.
- **The three verbs exist only inside `mcp` mode, not alongside it.** There is no `cockpit
  publish` command in a terminal — `cockpit_publish`/`cockpit_say`/`cockpit_ask` are MCP "tools"
  that exist only while `cockpit mcp` is running, callable only by an MCP client (claude-science
  or Claude Code) via a `tools/call` JSON-RPC message. MCP itself is JSON-RPC 2.0, one message per
  line, over stdin/stdout — no HTTP, no socket.
- **claude-science's Local command connector is the MCP client.** During a claude-science
  conversation, claude-science — not the user — launches `bin/cockpit mcp` as a child process and
  performs the JSON-RPC exchange with it. No JSON is seen or typed directly by the user.
- **The absolute path and `COCKPIT_REPO_DIR` are not redundant.** The Command field answers only
  "which program to run" — the managed runtime has no shell and no `PATH` lookup, so it requires
  the exact binary location. That is a different question from "what directory does the running
  process consider its working directory." Launching a binary by absolute path does not change
  the process's working directory to match; the child inherits whatever cwd its parent happens to
  use. The anti-wrong-folder guard specifically checks the working directory: on every tool call
  it re-runs `git rev-parse --show-toplevel` and `git remote get-url origin` from wherever the
  process actually is, and refuses if that repo's remote does not match `cockpit.config.json`.
  `COCKPIT_REPO_DIR` pins that, since no dialog field and no shell are available to set it
  otherwise.
- **Why this matters:** this project exists specifically because an earlier Claude session,
  operating with an ambiguous working directory, continued operating against an incorrect local
  demo folder. The guard — and, by extension, correctly configuring the connector's directory
  binding — is the fix for that failure mode, not an incidental detail.

Recommended in addition (defense-in-depth, optional): restrict this repo's permissions so that
`git push`/`bin/plankton`/`bin/nekton` are not reachable directly, only through the cockpit tools
— in the Claude Code CLI this is `.claude/settings.json`; in claude-science it is the
**Permissions** page under Workspace.

### 4. Start Claude Science
```bash
claude-science serve
```
**Use the default instance — do not use `--data-dir` or `--here`.** claude-science already
provides its own project/session concept within the default instance — previously created
participant sessions appear in its sidebar — so switching identity is a matter of switching
sessions there; per-participant connector *names* (step 3) are what keep cockpit bindings distinct
across identities, not separate instances. `--here` additionally derives its socket path from the
current directory, which under a long repository path can exceed the 108-byte `AF_UNIX`
socket-path limit.

There is a second, more important reason to avoid a separate `--data-dir`/`--here` instance:
logs from a `--data-dir`-created instance (`<data-dir>/logs/server-*.log`) showed every attempt to
load the cockpit connector failing with a sandbox/visibility error ("command not found inside the
MCP sandbox: most of the home directory is not visible there"), regardless of the binary's
location (tried under the participant repo, under `~/.local/bin`, and under `/usr/local/bin`) and
across four separate restarts of that instance. The default instance loaded the identical
connector configuration successfully on its first attempt. The most likely explanation is that a
sandbox's file-access grants accumulate over time through approval cards, and a freshly created
`--data-dir` instance starts with none of that history — not the binary's location or command
syntax. Using the default instance avoids this class of error entirely.

(On the Claude Code CLI path: `claude`, run from inside `participant-x`.)

Claude then has access to exactly three tools: `cockpit_publish`, `cockpit_say`, `cockpit_ask` —
nothing else touches git/plankton/nekton directly.

Repeat steps 1–4 for a second participant repo before continuing — the rest of this tutorial
produces a real cross-participant reproduction, which needs both.

### 5. First participant: produce and publish a foton

`data/penguins.csv` already exists at this point — it ships with the participant template from
step 1, along with `data/DATA.md` describing it. Nothing further needs to be fetched or created.

> Before the first message in a new session, check claude-science's Memory capability for this
> project. Its cross-project Memory feature can surface stored workspace context from an
> unrelated, pre-existing project into a new session, causing Claude to operate against an
> incorrect directory before any cockpit tool is called. This is not a cockpit defect: the
> anti-wrong-folder guard is unaffected, since this occurs via plain file/Bash operations prior to
> any `cockpit_publish` call, which would refuse regardless (no `cockpit.config.json` in the
> unrelated project binds it to the current participant's remote) — but it wastes real setup work.
> Mitigation: disable Memory for this project if it is not needed, and state the absolute
> repository path explicitly in the first message of every session regardless — an explicit
> instruction takes precedence over memory-filled context, but should not be the only safeguard.

In participant 1's session, instruct Claude: *"Work only in `/ABSOLUTE/PATH/TO/participant-1` —
do not use any other directory. Write `session-1/clean.py` that drops rows with any missing value
from `data/penguins.csv` and writes `session-1/clean.csv` (LF line endings, fixed float format).
Run it, then call `cockpit_publish`."*
```jsonc
cockpit_publish({
  inputs:  ["data/penguins.csv", "session-1/clean.py"],
  outputs: ["session-1/clean.csv"],
  cmd:     "python session-1/clean.py"
})
// → { fotonId, outputHashes, commitSha, permalinks }
```
That single call commits the files, builds the permalinks, and signs the foton — the
"veröffentlichen" step, complete. Note the `fotonId` and output hash; both are needed below.

### 6. Register participant 1 with the federation

On `my-federation`: open a **"Register a participant"** issue naming `<you>/participant-1`, add
the `approved` label. The repo's scheduled `mirror` Action then pulls and independently
re-verifies every signature into `mirror/`. Wait for that run to complete (or trigger it directly:
`gh workflow run mirror.yml --repo <you>/my-federation -f force=true`) before continuing — the
next steps depend on participant 1's foton actually being aggregated.

### 7. Second participant: produce and publish independently

In participant 2's session, instruct Claude the same way as step 5, but with participant 2's own
directory: *"Work only in `/ABSOLUTE/PATH/TO/participant-2` — do not use any other directory. Write
`session-1/clean.py` that drops rows with any missing value from `data/penguins.csv` and writes
`session-1/clean.csv` (LF line endings, fixed float format). Run it, then call `cockpit_publish`."*

Deliberately do **not** hand it participant 1's script yet — writing it independently, from the
same prose instructions, is what surfaces the byte-mismatch correction in step 9. Note this
identity's own `fotonId` and output hash too.

### 8. Register participant 2 with the federation

Same as step 6, naming `<you>/participant-2`. After this mirror run, both participants' fotons are
aggregated in `my-federation`'s `mirror/`.

### 9. Mirror the federation locally, then check reproduction

Back in participant 2's repo, pull the federation's aggregate down and overlay it into this
identity's own local registry — `plankton mirror`/`nekton mirror` read any peer registry
directory on the filesystem, no server or network call involved, so a local clone of the
federation repo is enough:
```bash
git clone https://github.com/<you>/my-federation /tmp/federation-clone
bin/plankton mirror /tmp/federation-clone/mirror
bin/nekton   mirror /tmp/federation-clone/mirror
```
`mirror/` mixes both fotons and claims in one directory, but each command only ingests the record
kind it understands and silently skips the other — confirmed against the reference
implementation's `Add()`, which rejects non-matching payloads without erroring the whole mirror.

**This alone is not enough for `cockpit_ask` to show participant 1's record as verified** —
mirroring only makes the *data* locally visible; `cockpit_ask`'s verification step separately
needs participant 1's *pubkey* in this repo's own trust config. Add it:
```jsonc
"trust": {
  "tiers": {
    "self": ["registry/keys/session-1.pub", "registry/keys/session-1-claims.pub"],
    "federation": ["<path to participant-1's registry/keys/session-1.pub, e.g. from the clone above>"]
  }
}
```
Now check:
```jsonc
cockpit_ask({ query: "reproductions", ref: "<participant 1's output hash from step 5>" })
```
This will most likely show ↻1, not ↻2 — participant 2's independently-written script produced
different bytes even though it followed the same instructions (different row order, float
formatting, or index handling are common causes). This is the expected, honest outcome of writing
the script independently, not a tool failure.

### 10. Correct: reuse participant 1's exact script, republish

Fetch participant 1's *exact* `clean.py` — not a re-written copy — from the commit-pinned
permalink recorded in its foton's descriptor (`cockpit_publish`'s step 5 result already has this;
`cockpit_ask`'s `about`/`producer` output on the foton also carries it), and reuse it verbatim:
```jsonc
cockpit_publish({
  inputs:  ["data/penguins.csv", "session-1/clean.py"],   // now byte-identical to participant 1's
  outputs: ["session-1/clean.csv"],
  cmd:     "python session-1/clean.py"
})
```
This produces a new foton whose output should now be byte-identical to participant 1's. Re-run
the same `cockpit_ask` query from step 9 — it should now show ↻2.

### 11. Record the reproduction claim
```jsonc
cockpit_say({
  subject: "<participant 1's foton id, from step 5>",
  template: "reproduces",
  subjectOutputHash: "<participant 1's output hash, from step 5>",
  reproducedOutput: "session-1/clean.csv",
  reproducedFotonId: "<participant 2's NEW fotonId, from step 10>"
})
// The cockpit itself runs the L0 check and fills in level — it is never self-declared by Claude.
```

### 12. Final mirror and verify everything
Trigger the federation's mirror once more so it picks up the new claim (claims aggregate the same
way fotons do — confirmed against `mirror_once.py`, which scans `registry/nekton/objects/`
alongside `registry/plankton/objects/`):
```bash
gh workflow run mirror.yml --repo <you>/my-federation -f force=true
```
Then verify:
- Viewer: `https://<you>.github.io/my-federation/viewer/viewer.html?union=../mirror/union.json`
  — shows the reproduces edge and ↻2.
- Directly: `cockpit_ask({ query: "producer"|"about", ref: ... })` from either participant repo —
  every record from a signer configured in that repo's own `trust.tiers` (step 9) has already
  been re-verified, not merely trusted on the aggregator's word; signers outside the configured
  tiers are correctly excluded, not silently trusted.

## Notes

Steps 1–4 are one-time human setup per participant repo, repeated for both participants before
continuing; steps 5–11 are the actual sequential Claude Science + federation loop; step 12 is
final verification. Steps 6, 8, and 12 involve the federation aggregator and are largely automatic
once a participant is `approved` — the only manual part is triggering (or waiting for) the mirror
run and giving it time to complete before the next step depends on its result.

`uat/setup.sh` and `uat/cleanup.sh` (repo root) automate everything above that is genuinely
scriptable — steps 1–2, and the register/mirror parts of 6, 8, and 12 — pausing at exactly the
points that require claude-science's own UI and a live Claude conversation (steps 3–4's connector
registration, and the actual publish/correct/say steps). See `uat/README.md` and the scripts'
header comments for prerequisites and what they do and do not cover, and why.
