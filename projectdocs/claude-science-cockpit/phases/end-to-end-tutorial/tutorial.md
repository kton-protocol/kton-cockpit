---
title: "Claude-Science-Cockpit: end-to-end tutorial"
phase: "end-to-end-tutorial"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [tutorial]
---

# Claude-Science-Cockpit: end-to-end tutorial

## Summary

Zero to a verified federation, in eight steps: create the repos, configure the cockpit, start
Claude Science, produce a foton, make a claim, register with a federation, verify.

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
# build the cockpit binary FROM its own source dir (go build resolves the module from cwd, not
# from the package argument — so this must run from the cockpit repo, not from participant-x/).
# Capture this participant repo's path FIRST, then cd into the cockpit source to build:
PARTICIPANT_DIR="$(pwd)"
cd /mnt/c/dev/planktonClaudeScienceCockpit
GOOS=linux GOARCH=amd64 go build -o "$PARTICIPANT_DIR/bin/cockpit" ./cmd/cockpit
cd "$PARTICIPANT_DIR"

# make a signing identity (only if this repo doesn't have one yet — never re-run if it does)
# NOTE: keys/ is in .gitignore (private keys must never be committed) and starts EMPTY in a
# fresh clone, so git never materializes the directory — create it first or keygen fails with
# "no such file or directory":
mkdir -p keys
bin/plankton keygen keys/session-1
bin/nekton   keygen keys/session-1-claims
cp keys/session-1.pub keys/session-1-claims.pub registry/keys/
git add registry/keys && git commit -m "signing identity" && git push

# scaffold + finish the config
bin/cockpit init                       # fills repo.owner/name from your real git remote
# edit cockpit.config.json: trust.tiers.self -> ["registry/keys/session-1.pub"]
bin/cockpit doctor                     # should show [ok] on every line
```

### 3. Register the cockpit as an MCP server (one-time)

**If using `claude-science`** (the sandboxed, browser-UI app — the actual "Claude Science" this
project is named for): open its UI → **Connectors** → **Add connector** → **Local command**. That
dialog only has Name / Command / env vars / Description — **no separate working-directory
field** — and its managed runtime has **no `bash`** either (only node/npx/python3, or an absolute
path to an installed binary), so a shell `cd` wrapper doesn't work here. Instead, tell the cockpit
which repo to bind to via an environment variable it reads for exactly this case
(`COCKPIT_REPO_DIR` — set once by you, at config time, never reachable by Claude at runtime):

| Field | Value |
|---|---|
| Name | `cockpit` |
| Command | `/ABSOLUTE/PATH/TO/participant-x/bin/cockpit mcp` |
| Environment variables | `COCKPIT_REPO_DIR=/ABSOLUTE/PATH/TO/participant-x` |
| Description | optional, e.g. "publish/say/ask for this kton participant repo" |

Both the absolute binary path *and* the env var matter, and they answer different questions: the
path is only "which program to run" (required since the runtime does no shell/PATH resolution);
it does **not** make the process's working directory match where the binary lives. The
anti-wrong-folder guard checks the actual working directory (`git remote get-url origin` from its
own process cwd) — which is what `COCKPIT_REPO_DIR` pins, independent of whatever cwd the runtime
actually launches it from.

**If instead using the Claude Code CLI** (`claude` in a terminal — not claude-science):
```jsonc
// .mcp.json (repo root)
{ "mcpServers": { "cockpit": { "command": "bin/cockpit", "args": ["mcp"] } } }
```

> ✅ Confirmed working (2026-07-22, `participant-christian`): adding the connector with the values
> above made claude-science's Connectors page list all three tools —
> `cockpit_ask`/`cockpit_publish`/`cockpit_say` — with exactly the descriptions from the code.
> "Skip approvals" was left off, so each tool call shows an approval card the first time; that's
> expected, not a problem.

#### Optional: how this is actually wired together, and why

Skip this if step 3 already worked for you. It's here for whoever wants the mechanics, not just
the recipe.

- **`cockpit` is one binary, not a CLI plus a separate server.** It has three subcommands:
  `init`/`doctor` (plain CLI, for a human, print text and exit) and `mcp` (doesn't print text and
  exit — it sits there reading/writing JSON-RPC on stdin/stdout forever). Running `cockpit mcp`
  *is* starting the MCP server; there's no second program in between.
- **The three verbs live one level inside `mcp` mode, not next to it.** There is no `cockpit
  publish` you can type in a terminal — `cockpit_publish`/`cockpit_say`/`cockpit_ask` are MCP
  "tools" that only exist once `cockpit mcp` is running, and only an MCP client (claude-science or
  Claude Code) can call them, by sending a `tools/call` JSON-RPC message. MCP itself is just
  JSON-RPC 2.0, one message per line, over stdin/stdout — no HTTP, no socket.
- **claude-science's Local command connector *is* the MCP client.** When you chat with Claude in
  claude-science, it — not you — launches `bin/cockpit mcp` as a child process and does all the
  JSON-RPC talking to it. You never see or type any JSON; you just say "publish this."
- **Why both the absolute path *and* `COCKPIT_REPO_DIR` are needed, and why they're not
  redundant:** the Command field only answers "which program to run" — claude-science's managed
  runtime has no shell and no PATH lookup, so it needs the exact binary location. That is a
  completely different question from "what directory does the running process consider its
  home" (its working directory). Launching a binary by absolute path does **not** change a
  process's working directory to match — the child inherits whatever cwd its parent happens to be
  in. And the cockpit's anti-wrong-folder guard cares specifically about the working directory: on
  every single tool call it re-runs `git rev-parse --show-toplevel` and `git remote get-url
  origin` from wherever the process actually is, and hard-refuses if that repo's remote doesn't
  match `cockpit.config.json`. `COCKPIT_REPO_DIR` is what pins that, since there's no dialog field
  for it and no shell available to `cd` first.
- **Why this matters at all:** this whole project exists because a Claude session with an
  ambiguous working directory once kept "cooperating" against the wrong local demo folder. The
  guard — and, by extension, getting the connector's directory binding right — is the entire fix
  for that failure mode, not an incidental detail.

Either way, optional but recommended (defense-in-depth): restrict this repo's permissions so
`git push`/`bin/plankton`/`bin/nekton` aren't reachable directly, only through the cockpit tools
— in Claude Code CLI that's `.claude/settings.json`; in claude-science it's the **Permissions**
page under Workspace.

### 4. Start Claude Science
```bash
claude-science serve --data-dir ~/.claude-science-x     # replace x with this participant's name
```
Give each participant identity (christian, wolfi, ...) its own `--data-dir`, both so their
connector lists (and cockpit bindings) don't get shared/confused across identities, and to avoid
a real OS limit: `--here` derives its socket path from the current directory, and Unix domain
sockets have a hard 108-byte path cap (`AF_UNIX sun_path`) — under a long path like
`/mnt/c/dev/planktonReproduce/participant-x/...` this reliably overflows it
(`data_dir path too long for AF_UNIX`). A short path under your home directory avoids this
entirely, and (since `COCKPIT_REPO_DIR` from step 3 already makes the connector's repo binding
independent of claude-science's own cwd) costs nothing.

(Or, on the Claude Code CLI path: just `claude`, run from inside `participant-x`.)

Claude now sees exactly three tools: `cockpit_publish`, `cockpit_say`, `cockpit_ask` — nothing
else touches git/plankton/nekton directly.

### 5. Pick a trivial workflow and produce a foton
Tell Claude: *"Write `session-1/clean.py` that drops rows with any missing value from
`data/penguins.csv` and writes `session-1/clean.csv` (LF line endings, fixed float format). Run
it, then call `cockpit_publish`."*
```jsonc
cockpit_publish({
  inputs:  ["data/penguins.csv", "session-1/clean.py"],
  outputs: ["session-1/clean.csv"],
  cmd:     "python session-1/clean.py"
})
// → { fotonId, outputHashes, commitSha, permalinks }
```
That single call commits the files, builds the permalinks, and signs the foton — the
"veröffentlichen" step, done.

### 6. Check reproduction / make a claim
```jsonc
cockpit_ask({ query: "reproductions", ref: "<output hash from step 5>" })
// → verified records, ↻N so far (1 = just you)

cockpit_say({
  subject: "<some other producer foton id you're reproducing, if any>",
  template: "reproduces",
  subjectOutputHash: "<their output hash>",
  reproducedOutput: "session-1/clean.csv",
  reproducedFotonId: "<your fotonId from step 5>"
})
// cockpit itself runs the L0 check and fills in level — Claude never self-declares it
```
(The first participant in a federation has nothing to reproduce yet — just publish; later
participants will reproduce your step.)

### 7. Federate
On `my-federation`: open a **"Register a participant"** issue naming `<you>/participant-x`, add
the `approved` label. The repo's scheduled `mirror` Action then pulls and **independently
re-verifies** every signature into `mirror/union.json`.

Repeat steps 1–6 for 2–3 participants for a real ↻N > 1.

### 8. Verify everything
- Viewer: `https://<you>.github.io/my-federation/viewer/viewer.html?union=../mirror/union.json`
  — shows each output's ↻N.
- Directly: `cockpit_ask({ query: "producer"|"about", ref: ... })` from any participant repo —
  every returned record has already been re-verified against that repo's own
  `cockpit.config.json` trust tiers, not just trusted on the aggregator's word.

## Notes

Steps 1–3 are one-time human setup per participant repo; steps 4–6 are the actual Claude Science
loop, repeated per pipeline step; steps 7–8 involve the federation aggregator and are mostly
automatic once the participant is `approved`.
