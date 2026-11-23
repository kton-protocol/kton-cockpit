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

Proposed 2026-11-23. Requested by this lane of kton (19.11., "Gap for v0.6.x: passphrase-protected
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

1. **The frontend, over serve.** serve asks its client with a request of the frontend protocol,
   `identity/passphrase {key: <name>, keyid, purpose}` (SPEC §12.5), and the workbench asks the person in
   its sign dialog, which already asks for consent per signature (#196). The answer goes back over the
   connection and is never logged.
2. **A terminal.** The CLI on a terminal prompts on the terminal (no echo), naming the key and its keyid.
3. **`KTON_KEY_PASSPHRASE`**, for what runs without a person (CI, tests), as the kernel's reference CLIs
   take it (open for MH, Q2).
4. **Nothing else.** The MCP server and a CLI without a terminal have no one to ask: an encrypted key is
   refused with `identity.locked`, naming the key and the ways to unlock it, before anything is signed.

### 3 Never stored

A passphrase is never written: not to a file, a log, the jobs journal or a trace. A decrypted key lives in
the process's memory only, for as long as section 4 says, and is zeroed when it is dropped where Go allows.

### 4 How long a key stays unlocked (open for MH, Q1)

Proposed: per signature in serve for the person's keys (the dialog already asks per signature, so the
passphrase is asked with it), and per process for the CLI (one prompt per `cockpit say`, which signs once
or a batch). An agent key (records, #196) is not encrypted by default (Q3).

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
signature; `keygen --encrypt` writes a key `ssh-keygen -y` reads.
