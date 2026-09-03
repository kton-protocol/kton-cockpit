# 03 - the anti-wrong-folder guard

The reason this project exists. A live demo run had six separate registries under one parent
directory, and a session cooperated against the wrong one. Nothing failed — the records were signed,
valid, and in the wrong project.

Every tool call re-reads the repository's own `git remote get-url origin` and refuses unless it
names the owner/repo the config claims. Three refusals are shown: a config bound elsewhere, a
directory that is no repository, and a config sitting somewhere other than the root it binds.

## The control

A guard that refuses everything is not a guard. So the example ends by making the same call in the
repo the config *is* bound to, and requires it to succeed. Three refusals without that would be
indistinguishable from a cockpit that had simply stopped working.

## What it does not catch, said plainly

A *copy* of the right repository. The copy carries the same config and the same remote, so the two
sources still agree. `repo.mode: "local"` anchors on the config's own declared absolute path
instead, which catches the copy and misses what this catches — neither is complete, and ADR-004
says so rather than implying otherwise.
