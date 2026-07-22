---
title: "Design"
phase: "Design"
project: "claude-science-cockpit"
date: 2026-07-22
status: "active"
tags: []
---

# Design

## Overview

Concrete design of the cockpit, grounded in the actual `plankton`/`nekton` CLI surface (not just
the abstract Bau-Spezifika) and in what actually went wrong during the live federation demo run.
Each entry below covers one design decision or subsystem in detail.

## Goals

- Turn the Bau-Spezifika's three verbs into a concrete tool surface with real inputs/outputs.
- Design the anti-wrong-folder guard as a structural (not conventional) fix.
- Define `cockpit.config.json` precisely enough to implement against.
- Decide the implementation language and enforcement mechanism, with reasoning recorded.

## Deliverables

- [Architecture: Go binary, MCP stdio server](architecture-go-mcp-server.md)
- [The anti-wrong-folder guard](anti-wrong-folder-guard.md)
- [cockpit.config.json schema](cockpit-config-schema.md)
- [The three verbs in detail](three-verbs-publish-say-ask.md)
- [What this cockpit explicitly does not build](what-we-dont-build.md)
- [Rollout and verification plan](rollout-and-verification-plan.md)

## Notes

This phase reflects the design as reviewed and approved on 2026-07-22 (see
[ADR-001](../../decisions/adr-001-go-mcp-server.md) for the central architecture decision, made
after weighing MCP-server enforcement against a CLI + Bash-permission approach).
