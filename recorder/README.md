# recorder — watch, select, document

The recorder sits **beside** the cockpit. It watches work done outside it — an R session, a
Quarto document in Positron, a NONMEM run started from R — and records which command read and
wrote which files, with their content hashes. Nothing it sees enters kton by itself: a person
looks at the observations, selects what matters, documents it, and persists it with the
cockpit's verbs, by hand (`cockpit publish`, `cockpit say`). Registering by hand without the
recorder works exactly as before.

```
 Watch                               Show                           Select & Document
 ─────                               ────                           ─────────────────
 libktontrace.so (LD_PRELOAD)  ─┐                                   "Dokumentieren" in the viewer
 ktonrec (TASK/SOURCE/ENV/VARS) ├─▶ trace/*.tsv ─▶ ktonwatch serve ─▶ proposes `cockpit publish '<json>'`
 Positron extension (origin)   ─┘   positron-exec.jsonl   :8378       you edit, then run it yourself
```

Linux only. Designed from the spikes in the recorder plan (S1–S8): R readers incl. `fread`,
`vroom`, `arrow`, `haven`, `qs2`; `mclapply`, PSOCK, `future`, `callr`; `system()`/`processx`;
NONMEM 7.5 via `nmfe75`, bbi, PsN and pharmpy; Positron's Ark kernel over WSL.

## Parts

| Path | What it is |
|---|---|
| `shim/ktontrace.c` | The `LD_PRELOAD` shim. One log line per event: `open` (with `new`/`trunc`, SHA-256 of what was read), `closew` (SHA-256 after writing), `inherit` (a write fd a program got from its shell), `rename`, `unlink`, `openfail`, `connect` (loopback/unix/remote), `exec`, `spawn`. Hashes files under `$KTON_TRACE_HASH` in-process, with a per-process cache and `$KTON_TRACE_HASH_MAX`. `%p` in `$KTON_TRACE_LOG` gives one log per started session. |
| `watch/` | Go package: turns a log into tasks (`Session`), attaches Positron origins (`AttachOrigins`, Quarto chunk by code), filters (`Filter`, noise rules from the spikes), lineage, drafts for `cockpit publish` (`MakeDraft`), the follow-a-directory service and its read-only HTTP API and viewer. |
| `cmd/ktonwatch/` | `ktonwatch -log FILE` (one log, text report) and `ktonwatch serve` (all logs, viewer at `127.0.0.1:8378`). |
| `r/ktonrec/` | R package: writes `TASK` after each top-level command (status `err` when `geterrmessage()` changed — Ark reports `ok` even after errors), `SOURCE` for `source()`/`sys.source()`, `ENV` (R home, library paths, the working directory as the project), `VARS` (global variables a command created or changed: name, class, shape). |
| `r/kton-trace.R`, `r/Rprofile-kton.R` | Load `ktonrec` without installing it; the profile is what Positron's `R_PROFILE_USER` points to. |
| `r/kton-R` | Starts R or radian under the recorder. |
| `positron/` | Positron extension: writes where each executed piece of code came from (`onDidExecuteCode`: file, lines; Quarto cells only name the file) into `positron-exec.jsonl`; command "kton: Beobachtungen öffnen". `pack.py` builds the `.vsix`. |
| `shimtest/` | Tests of the shim (build with gcc, run scenarios, check the log). |
| `demo/` | `run_demo.sh` (Python stand-in session), `run_r_demo.sh` (real R). |

## Install and use

```sh
sh recorder/install.sh        # builds; installs into ~/.local (lib/kton, bin/ktonwatch, share/kton)
ktonwatch serve &             # follows ~/.cache/kton-cockpit/trace, viewer at http://127.0.0.1:8378/
```

**R in a terminal:** `~/.local/share/kton/r/kton-R` in the project directory.

**Positron (Windows, R in WSL):** connect to the WSL distribution, then put into the remote
settings (`~/.positron-server/data/Machine/settings.json`, the "Remote [WSL: …]" tab):

```json
"positron.r.kernel.env": {
  "LD_PRELOAD": "/home/<you>/.local/lib/kton/libktontrace.so",
  "KTON_TRACE_LOG": "/home/<you>/.cache/kton-cockpit/trace/positron-%p.tsv",
  "KTON_TRACE_HASH": "/mnt/c/dev",
  "R_PROFILE_USER": "/home/<you>/.local/share/kton/r/Rprofile-kton.R"
}
```

and install the extension into the WSL server:
`~/.positron-server/bin/<commit>/bin/positron-server --install-extension ~/.local/share/kton/kton-recorder-0.0.1.vsix`.
A changed `kernel.env` applies after "Developer: Reload Window", not after a console restart.

**A Claude Code session:** the project hooks in `.claude/` (see the workstream) point
`LD_PRELOAD` at the same shim for the Bash tool only and write a `TASK` per command.

## What is tested

```sh
go test ./recorder/...        # shim (needs gcc), watcher, service, API, and a draft accepted by
                              # `cockpit publish` at the command line in a real participant repo
sh recorder/demo/run_demo.sh
sh recorder/demo/run_r_demo.sh   # needs R
```

## Limits

- Linux only. A statically linked program (Go binaries such as bbi) is not entered; its `spawn`
  is logged and shown as "not recorded", what it reads and writes is not. bbi writes its own
  `bbi_config.json`, which is the place to take a declaration from.
- Only processes started under the shim. Attaching to a running process is not possible.
- Data that flows through R objects rather than files is not followed: `VARS` says which
  variables a command left, not which ones it used.
- `mmap` writes are not hashed at close.
- Quarto **Render** runs outside the kernel and is not recorded yet.
- Positron sends no lines for a Quarto cell; the chunk is found by matching the code, which fails
  for two identical chunks.
