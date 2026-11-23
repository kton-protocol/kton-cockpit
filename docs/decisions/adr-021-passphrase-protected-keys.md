---
title: "Passphrase-protected signing keys: the kernel's format, one loader, the passphrase asked where a person is"
type: "decision"
project: "kton-cockpit"
date: 2026-11-23
status: "proposed"
tags: [architecture, keys, identity, signing, serve, mcp]
---

# ADR-021: Passphrase-protected signing keys

## Status

Proposed 2026-11-23; kton-review's go for the direction with P1-P4, written in below. Requested by this lane of kton (19.11., "Gap for v0.6.x: passphrase-protected
signing keys", kton #277); kton prepared the format and the loader in kton PR #299 (not merged: MH
decides whether the kernel takes `golang.org/x/crypto`). This ADR is drafted against that API; it changes
only if MH keeps the KDF out of the kernel (kton's option c). Three questions are open for MH (below).

## Context

A signing key today is a file of hex next to its public half, readable by anyone who can read the file:
`identity.json` names the plankton and nekton keys, `identity.keyring` a folder of further claim keys
(`say --as`). A person's key signs decisions (#196), so a copied file is a forged signature with no trace.
The kernel now reads the OpenSSH private-key form, ed25519 encrypted with bcrypt-pbkdf and AES-256-CTR, as
`ssh-keygen -t ed25519` writes it (`ssh-keygen -p` changes its passphrase), beside the plain hex form, with
the same seed and keyid in both. One loader: `kton.dev/plankton/keys.Load(path, pass)`, where `pass` is a
`Passphrase func(keyPath string) ([]byte, error)` called only for an encrypted key; `keys.PublicKey` gives
the public half without a passphrase; `keys.MarshalEncrypted` writes one.

SPEC §13 keeps key logic out of the cockpit: it reads keys through the kernel, and decides only where a
passphrase comes from.

## Decision

### 1 One loader, both forms

Every place the cockpit reads a private key (the claim signer, `say --as`, publish's foton key, the
workflow runner, doctor) reads it through `keys.Load`, so an encrypted key works wherever a plain one
does and nothing parses a key on its own. `identity.json` and the keyring are unchanged: they name files,
and a file is either form. The public half is read without a passphrase, so `doctor`, `say --json` listings
and trust checks never ask for one.

### 2 Where the passphrase comes from

Asked only when a signature needs the key, never to read, in this order:

1. **The frontend, over serve, bound to one signature** (kton-review P2). serve asks its client only
   during a say, for that say's key: `identity/passphrase {say: <request id>, key: <name>, keyid, digest}`,
   where `digest` is the one the sign dialog shows (SPEC §12.5), and the workbench asks the person in that
   dialog, which already asks for consent per signature (#196). An answer for another say, a second answer,
   or one nobody asked for is refused, and nothing is signed. After `keys.Load`, the public half derived from
   the key must have the keyid asked about, or the say is refused. The key signs that say and is dropped.
2. **A terminal.** The CLI prompts on the controlling terminal (`keys.CLIPassphrase`: `/dev/tty`,
   `CONIN$` on Windows, never stdin), no echo, naming the key and its keyid.
3. **`KTON_KEY_PASSPHRASE`**, for what runs without a person (CI, tests), documented as weaker (Q2), and
   only as kton-review's P1 has it: `keys.TakeEnvPassphrase()` is the first thing `main` does, before
   anything starts a child, so the variable is gone from the process's environment and no child the
   cockpit starts (git, the container CLI, a shimmed tool, a workflow step) inherits it. On Linux,
   `/proc/<pid>/environ` still shows the start value, to the same user and root: nothing in the process
   can change that, which is one reason this source is the weaker one.
   `keys.ForgetEnvPassphrase()` clears the held value once the keys are loaded.
4. **Nothing else.** The MCP server and a CLI without a terminal have no one to ask: an encrypted key is
   refused with `identity.locked`, naming the key and the ways to unlock it, before anything is signed.

### 3 Never stored, never logged

A passphrase is never written: not to a file, a log, the jobs journal, `COCKPIT_TRACE`, serve's stdio
transport log or an error message that echoes a request (kton-review P3). SPEC §12.5 marks the field
secret, so a frontend logs it nowhere either. serve decodes it as raw JSON into a byte slice, never a Go
string, and clears it after `keys.Load`; the decrypted key is cleared after the signature. Go cannot
promise that no copy remains in memory; the cockpit makes none of its own.

### 4 How long a key stays unlocked (open for MH, Q1)

Proposed: per signature in serve for the person's keys (the dialog already asks per signature, so the
passphrase is asked with it), and per process for the CLI (one prompt per `cockpit say`, which signs once
or a batch). An unlock that holds for minutes would make serve a signing oracle for whatever reaches the
connection (kton-review). An agent key (records, #196) is not encrypted by default (Q3).

A batch (`say --batch`) asks once and signs all its claims or none. A run in the background (a workflow
run over serve, #218) asks before its first step, while the person is at the dialog, or is refused before
any step runs: a step never stops halfway for a passphrase (kton-review P4).

### 5 Making and checking

`cockpit keygen --encrypt` writes the encrypted form (`keys.MarshalEncrypted`), asking twice on the
terminal. `doctor` says per key whether it is encrypted, from the file alone, without asking. `ssh-agent`
holding the key is a later step, as in the kernel (it needs a newer x/crypto).

## Open for MH

- **Q1, unlock lifetime:** per signature for person keys in serve, per process on the CLI (proposed), or
  an unlock that holds for N minutes in serve.
- **Q2, `KTON_KEY_PASSPHRASE`:** allowed (proposed, for CI and tests, documented as weaker: the
  environment is readable by the process's user), or refused outside tests.
- **Q3, agent keys:** stay unencrypted (proposed: an agent has no one to ask, and its records are
  attributed to the agent key, not a person), or encrypted with the passphrase in its environment.

## Consequences

- A copied key file no longer signs without its passphrase; a person's signature needs the person.
- The MCP server cannot sign with an encrypted key; a lane that signs through MCP keeps a plain agent key
  (Q3) or an `ssh-agent` later.
- One more frontend request (`identity/passphrase`); a frontend that does not answer it gets
  `identity.locked`, as the MCP server does.
- Depends on kton PR #299 and MH's x/crypto decision.

## Tests (before the code lands)

An encrypted claim key signs a say with the right passphrase and is refused with a wrong one, nothing
written; the plain and encrypted forms of one seed sign under one keyid; `doctor` and `say --json` read an
encrypted key's public half without asking; serve asks its client `identity/passphrase` and signs with the
answer, and a client that does not answer gets `identity.locked`; the MCP server refuses an encrypted key
with `identity.locked` before signing; the passphrase appears in no file, log, journal line or trace after a
signature; `keygen --encrypt` writes a key `ssh-keygen -y` reads; a child the cockpit starts after the
key loads does not see `KTON_KEY_PASSPHRASE` (P1); an `identity/passphrase` answer nobody asked for, or for
another say, is refused and nothing signed, and a key whose derived keyid is not the one asked about is
refused (P2); a batch with an encrypted key asks once and signs all or none, and a background run asks
before its first step or is refused before any (P4).
