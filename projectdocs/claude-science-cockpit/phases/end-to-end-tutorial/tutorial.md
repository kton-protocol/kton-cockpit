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
```jsonc
// .mcp.json (repo root)
{ "mcpServers": { "cockpit": { "command": "bin/cockpit", "args": ["mcp"] } } }
```
Optional but recommended (defense-in-depth): in `.claude/settings.json`, deny `Bash(git push:*)`,
`Bash(bin/plankton *)`, `Bash(bin/nekton *)` so those are only reachable through the cockpit tools.

### 4. Start Claude Science
```bash
claude
```
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
