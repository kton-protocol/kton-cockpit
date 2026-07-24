---
title: "End-to-End Tutorial"
phase: "End-to-End Tutorial"
project: "claude-science-cockpit"
date: 2026-07-22
status: "active"
tags: [tutorial, onboarding]
---

# End-to-End Tutorial

## Overview

A brief, copy-pasteable walkthrough from zero to a verified federation: create the template
repos, configure the cockpit in a participant repo, start Claude Science, produce and publish a
foton, make a claim, register with a federation, and verify the result. Written after the v1
cockpit build passed its smoke tests against a real, already-populated participant registry from
the live demo run (`participant-alice-1` at the time; the local demo repos were later reorganized
into `participant-christian`/`participant-wolfi` — see `internal/tools/smoke_test.go`'s fixture
comment for whichever currently exists).

## Goals

- Give a new participant repo operator a single document that takes them from "nothing exists
  yet" to "my work is federated and independently verified," without re-deriving it from the
  design docs each time.

## Deliverables

- [The tutorial](tutorial.md)

## Notes

This intentionally stays terse — it's an operational runbook, not a design rationale. See
[phases/design/](../design/index.md) for *why* each step works the way it does (in particular
[three-verbs-publish-say-ask.md](../design/three-verbs-publish-say-ask.md) for what each MCP call
actually does under the hood).
