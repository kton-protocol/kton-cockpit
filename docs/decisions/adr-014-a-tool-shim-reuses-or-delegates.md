---
title: "A tool shim: reuse a trusted record of the call, or run it in the tool's own environment"
type: "decision"
project: "kton-cockpit"
date: 2026-10-28
status: "proposed"
tags: [architecture, execution, environment, reuse, qualification, pharmacometrics]
---

# ADR-014: A tool shim reuses a trusted record of the call, or runs it in the tool's own environment

## Status

Proposed (2026-10-28); kton-review: go for the design, with its points D1–D9 in section 8. On kton's request of the same day (`kton-cockpit/INBOX.md`, "tool shims"), from
an idea of MH. It builds on ADR-010, whose context already describes the `nmfe` stub that looks a call
up by its action key, and on ADR-003 (running in a pinned image, the trust base not in the room) and
ADR-011 (executors as a port). It needs no kernel change. Fetching outputs by hash from elsewhere waits
for kton#225 and kton#220.

## Context

Workflow libraries call tools that call tools: pharmpy and PsN call NONMEM, and bbr calls bbi, which
calls NONMEM. Today one environment holds the whole closure. A NONMEM update then forces revalidating
pharmpy's environment as well, and reproducing a workflow reruns every estimation in it, including the
ones a trusted party already recorded.

ADR-010 made reuse cross concrete environments of one qualified class, and named the stub that makes
use of it: an orchestrator's `nmfe` calls are served from reference fotons, or dispatched to the
engine's own image and published. What is missing is the stub itself, as a part of the cockpit with
the cockpit's guarantees.

One relative exists. cockpit-runtime's recorder has `kton-call` (`recorder/cmd/kton-call`): installed
under a tool's name, it learns from earlier traced calls which files such a call reads, and when a
recorded call has the same key it puts the recorded outputs in place; otherwise it runs the real tool
where it stands. That is reuse of one's own **unsigned observations**, in the same environment. The
shim decided here reuses **signed records by trusted keys**, and on a miss runs the tool **elsewhere**:
in the environment that was validated for it.

## Decision

### 1 Two halves: a thin shim inside, a dispatcher outside

- **The shim** is installed under the tool's name (`nmfe75`, `execute`, `bbi` …) on the calling
  environment's `PATH`, as `kton-call` is. It knows no key and no registry. It collects the call (2),
  hands it to the dispatcher, and writes back what comes back: the output files, stdout, stderr and the
  exit code.
- **The dispatcher** is a Go API on a held Cockpit (`cockpit.Open`, handoff 2.1): `Call(ctx, ToolCall)
  (ToolResult, error)`. It computes the action key, looks it up (3), runs a miss (4) and records it.
  It ships as an `http.Handler` on a held `*Cockpit`, as `mcpsurface.Handler` does, so the same code
  is mounted by `cockpit shim serve` on the host and by ktonwatch's service as `/v1/call`
  (cockpit-runtime, agreed 2026-10-28). The listener belongs to whoever mounts it: ktonwatch serves a
  unix socket for containers, with its bearer token.
- **Why split it:** the calling environment is where untrusted code runs. ADR-003 keeps the keys, the
  registries and the substrate out of every container the cockpit runs a command in, and the same
  holds here. The shim talks to the dispatcher over a socket mounted into the calling environment.
- **The record is derived, never handed in.** On a miss the dispatcher runs the tool itself and records
  what that run wrote, so nothing inside the calling environment can get outputs of its choosing
  signed. On a hit it returns outputs a trusted key already signed for. A caller can only ask "run
  this tool on these inputs". That also means a caller can read everything the mapped environment
  can read, since a NONMEM control stream carries Fortran (section 8).
- **Everything from the shim is untrusted.** The dispatcher takes bytes, never hashes, and hashes them
  itself; it re-checks every path the shim sends (section 8). The collector's checks in the shim
  are a convenience for the caller, not a guarantee.
- **The mounted socket offers `Call` and nothing else.** It is never the listener that serves
  `/v1/say` or `/v1/publish`, or code in the calling environment could have claims of its choosing
  signed.

### 2 The call: inputs collected per tool

- **A collector per tool** turns argv and the working directory into the call's inputs: each file it
  reads, as a relative path and its bytes. For NONMEM that is the control stream named in argv and what
  it references (`$DATA`, `$INCLUDE`, `$MSFI`, `$ETAS` files …). For PsN it is the model, the files the
  model references, and the files named by options. Collectors are code, one per tool, tested against
  real calls; the configuration only picks one by name.
- **The arguments** go into the descriptor, normalised as `kton-call` does (`toolcall.NormArgv`): paths
  inside the repository relative to the working directory (`../../data/pk.csv` stays that), the tool
  name without its directory. The call's working directory goes into the descriptor too, relative to
  the repository root.
- **Inputs are staged relative to the repository root**, so NONMEM's usual layout (`models/run1/`
  with `$DATA ../../data/pk.csv`) works. An input outside the repository is refused, with a message
  naming it: it would not exist when the call runs elsewhere, and leaving it out silently is a false
  hit.
- **The caller's environment variables are not forwarded.** The mapped environment brings its own; a
  variable a tool needs from the caller (`OMP_NUM_THREADS`) is named in the tool's configuration and
  then goes into the descriptor, so it is part of the key.
- **Configuration the tool reads from its environment** (`psn.conf`, NONMEM's sizes, pharmpy's
  configuration) belongs to the mapped environment and its class, not to the call (ADR-010, open point
  three, settled this way for the shim).

### 3 The key, and what counts as a hit

- **The action key** is the kernel's (kton §6.3): inputs (relative path → hash) and the protocol:
  kind `tool/<name>`, the descriptor with the normalised arguments and the mapped environment's
  **class**. The concrete image is carried as `envRef` (ADR-010, `kton-env/v1`), never covered. A
  mapped environment without a class covers its digest, as ADR-010 rule 3 says, and then reuses only
  within that exact image.
- **A hit** is a `ReuseHits(actionKey)` entry of the union that passes the shim's policy:
  - a key in a configured **executor tier** verifies it (`verify.SignersOf`, never the declared
    keyid): keys held by dispatchers or executors that ran the image themselves. `envRef` is carried,
    so a person's key could sign a laptop run that names a qualified image; a person tier never makes
    a hit;
  - its environment class is accepted: a `qualifies-as` claim for the hit's carried `envRef`, signed
    under a configured tier (ADR-010 rule 4), and fail closed without one;
  - it is not conditional, unless the shim's policy allows that.
- **Several hits:** one global order, as ADR-013 uses: the highest configured tier, then the earlier
  signed time as an instant, then the id. Two machines with differently assembled unions pick the
  same one. The choice and the others are reported (5).
- **No absent optional input:** a record lists the paths in the staging directory its run probed and
  found absent (section 8). A hit is refused when the caller's repository has one of them.
- **Outputs are fetched by hash** and checked against it: from the local content store, then from
  locators signed by a trusted key (kton#225, kton#220). A hit whose outputs cannot all be fetched and
  checked is a miss, and the report says so.

### 4 A miss runs in the tool's own environment

- **Each tool maps to an environment**: a digest-pinned image and its class (`execution.image` and
  `environment`, per tool). The dispatcher runs the call there through an executor (ADR-011; the
  container executor by default), network off.
- **Only the collected inputs are staged**, into an empty working directory with the same relative
  layout. The real tool sees nothing else of the caller's directory. A file the collector missed is not
  there, so the tool fails on the first run instead of the call becoming a wrong hit later.
- **Outputs are everything the run wrote in that directory**, plus `.kton/stdout`, `.kton/stderr` and
  `.kton/exit-code`, because PsN and pharmpy parse them. They are recorded as a `foton/v1` through the
  publish path (`binaries.Author`), signed by the dispatcher's key, with `envRef` carried, and kept in
  the content store.
- **A run is recorded only when the tool's result check passes**: its exit code is 0 or one the
  configuration lists as a result (NONMEM's failed estimation), **and** the outputs the tool always
  writes exist (`.lst` and `.ext` for NONMEM). A missing licence or input that shares a listed code
  is then not a result. An unrecorded run still returns its outputs and exit code to the caller.
- **Outputs of shim runs stay local by default** (section 8): they are kept in the dispatcher's content
  store and returned to the caller, and their records carry no locators.

### 5 What the caller sees

- On a hit or a miss, the caller's working directory gets the output files, and the shim replays
  stdout and stderr and exits with the recorded code. To the calling program the tool ran.
- One line on stderr says which: `kton: nmfe75 reused sha256:… (alice, tier qualified)` or
  `kton: nmfe75 ran in <class> and was recorded as sha256:…`, and the dispatcher logs the call. A
  workflow's own record can then name every foton its calls used.

### 6 Nesting

- The PsN environment has a `nmfe` shim of its own, pointing at the same dispatcher. A hit at the PsN
  level (a whole bootstrap) skips every inner run. A miss runs PsN in its environment, and its NONMEM
  calls are again looked up one by one.
- Each level is validated on its own: PsN against NONMEM's interface, NONMEM by its spectrum
  (ADR-010). A NONMEM update revalidates the NONMEM environment only.

### 7 Where it sits

- It is not a verb (ADR-009). It is a mode of execution: the shim is a small binary, the dispatcher is
  Go API, and recording goes through `publish`'s path, so a record from a shim is the same kind of
  record as any other.
- It is off until configured: a `shims` block in `cockpit.config.json` names each tool, its collector,
  its mapped environment and class, the exit codes that are results, and the reuse policy. The schema
  and SPEC get a section for it before it ships.
- `kton-call` stays the recorder's (cockpit-runtime): observational reuse in place, unsigned. The two
  share argv normalisation and the content store, moved unchanged into `internal/toolcall` (PR #64,
  agreed with cockpit-runtime), so existing call keys and content stores stay valid.

### 8 What the dispatcher trusts (review, 2026-10-28)

The calling environment and the code a miss runs are both untrusted. Each point below is a test of
the prototype.

- **Paths in (D1).** The dispatcher refuses any path that is absolute, has `..` past the staging root,
  or contains `\0` or an empty segment. It stages each file with `openat` and `O_NOFOLLOW` under the
  root, so a symlink left there earlier is not followed. Test: such paths sent straight to `Call`.
- **Outputs (D2).** Only regular files count. A symlink, device or FIFO in the run directory fails the
  recording, and files are read with `O_NOFOLLOW` from inside the run directory, never through a host
  path the run could redirect. Test: a run that leaves `out -> ~/.ssh/id_ed25519`. The shim, writing
  outputs back, refuses absolute and `..` paths and does not write through a symlink in the caller's
  directory.
- **Secrets in the environment (D3).** A caller can read what the mapped environment can read. A
  licence is mounted read-only at run time, never built into the image and never an input, and it is
  declared in the tool's configuration as a secret. The guarantee is that and local-by-default:
  outputs stay in the dispatcher's content store, and only a tool whose environment holds nothing
  secret may opt in to sharing them (locators, a shared content store). A run whose outputs contain a
  declared secret's bytes is not recorded, but that is a tripwire, not a guarantee: an encoded copy
  passes it, and an unrecorded run still returns its outputs to the caller. It is never a reason to
  share outputs of a licensed environment.
- **Who signs a hit (D4).** Only an executor tier (section 3).
- **Optional inputs (D5).** The miss run is traced (the recorder's `LD_PRELOAD` shim), and the paths in
  the staging directory it looked for and did not find become negative conditions of the record. Until
  that is built, a shim reuses only records this dispatcher made from the same collector.
- **The socket (D9).** `Call` only (section 1).

## What it needs from the kernel

Nothing new. Fetching outputs from elsewhere waits for a content store and locators (kton#225, kton#220);
until then a hit needs the outputs in the local content store. Anything else that turns up while
building it is written into kton's inbox and becomes a public kton issue.

## Consequences

- Every environment is validated on its own, and a tool update revalidates only that tool's
  environment.
- A reproduction reruns only the calls no trusted key has recorded for an accepted class.
- The calling environment no longer needs the tools it delegates: a pharmpy image carries pharmpy and
  shims, not NONMEM.
- A call costs a round trip to the dispatcher and, on a miss, a container start. For NONMEM runs of
  minutes to hours that is noise; for many tiny calls it is not, and the shim is the wrong tool there.
- Output that is not byte-identical (`run.lst` has timestamps) does not matter on a hit, since the
  recorded bytes come back. It matters when two producers' records of one call are compared, which is
  the normaliser question (kton#226), not the shim's.

## Open, not decided here

- **Generated inputs:** PsN writes `psn.mod` and its directories, then calls NONMEM on them. Their keys
  are stable only if the generation is; ADR-010 leaves the input normaliser open, and so does this.
- **The socket's reach:** whoever is in the calling environment can ask the dispatcher to run the mapped
  tools on inputs of their choosing. That is what the environment is for, but it costs compute.
  ktonwatch hands out one token per mounted container, so a call is attributed and can be refused per
  container; a quota on top is not decided.

## Next

A prototype for one NONMEM call end to end: the `nmfe` shim with the NONMEM collector (`$DATA`,
`$INCLUDE`), `cockpit shim serve` with the container executor and a NONMEM image, a miss that records,
and the same call again as a hit, both checked by a test.
