---
title: "The anti-wrong-folder guard"
phase: "design"
project: "claude-science-cockpit"
date: 2026-07-22
tags: [safety, git]
---

# The anti-wrong-folder guard

## Summary

The structural fix for the exact incident that motivated this project: a Claude Code session
with an ambiguous working directory found a legacy demo project folder (one of several under
`/mnt/c/dev` containing `plankton-data`/`nekton-data` artifacts from earlier, unrelated
experiments — `MCP/`, `claudeScience/warfarinTest/`, `plankton_fed_demo/`) and kept working
against it instead of the intended participant repo.

## Details

On **every** MCP tool call, before doing anything else, the cockpit:

1. Resolves its own repo root via `git rev-parse --show-toplevel` from its actual current working
   directory (the cwd it was launched in by the MCP client) — or, if the `COCKPIT_REPO_DIR`
   environment variable is set, from that directory instead (see Notes: this exists for MCP client
   runtimes that give no way to pin a spawned local command's cwd, e.g. claude-science's managed
   connector sandbox; it's set once by the human operator at connector-configuration time, not
   something Claude can reach or change at runtime).
2. Loads `cockpit.config.json` from *that* root only — never an ancestor directory. Refuses the
   call outright if the file is missing.
3. Compares `git remote get-url origin` against the config's `repo.owner`/`repo.name` — **hard
   refuses**, with a clear error surfaced back to Claude, on any mismatch.
4. Derives `PLANKTON_DIR` / `NEKTON_DIR` / `NEKTON_TEMPLATES` / key paths as **absolute paths**
   computed from that verified repo root + config — never inherited from ambient shell
   environment variables Claude might have set (or left stale) in a prior session or a different
   terminal.

## Notes

This is the key difference from the manual `CLAUDE.md` workflow, which has Claude `export
PLANKTON_DIR=...` by hand at the start of every session: an env var is exactly the kind of
ambient, easy-to-leave-stale state that let a confused session keep operating in the wrong place.
Binding by verified git remote instead means the cockpit simply cannot act against a folder other
than the one it's actually configured for, regardless of what Claude's own context believes about
where it is.

`COCKPIT_REPO_DIR` (point 1) is not an exception to this — it only ever answers "which directory
do I resolve the repo from," and the remote-match check in point 3 still runs unconditionally
against whatever that resolves to. It is not the kind of ambient, Claude-reachable env var this
guard exists to distrust: `PLANKTON_DIR`/`NEKTON_DIR`/etc. (point 4) are still always derived from
the verified repo root, never from the environment, and Claude has no tool call that can set or
change `COCKPIT_REPO_DIR` — only the human operator configuring the MCP connector can. See
[three-verbs-publish-say-ask.md](three-verbs-publish-say-ask.md) and `internal/config/config.go`
for where it was added and why (claude-science's managed connector runtime has no shell to `cd`
first and no dialog field for a working directory).
