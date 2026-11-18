---
title: "Jobs: scheduled and long-running work as a cockpit module, declared in kton and run by one runner per machine"
type: "decision"
project: "kton-cockpit"
date: 2026-11-18
status: "proposed"
tags: [architecture, module, jobs, schedule, workflow, ray, poll, monitoring, runner]
---

# ADR-017: Jobs, scheduled and long-running work as a cockpit module

## Status

Proposed (2026-11-18) by cockpit-runtime, on MH's request of 18.11. relayed by kton-coordination
("one cron for everything that runs regularly or long and should be monitored"; owner cockpit-runtime).
The module door and the operator commands are to be agreed with kton-cockpit. Released after cockpit
v0.5.0, on its `.kton/` layout (ADR-016). It changes no verb and leaves SPEC §6 untouched.

## Context

- **Regular and long work runs in invisible loops today.**
  - test-execution polls robert1 and the 4315 server from inside its Claude session.
  - Lanes keep `/loop` and cron jobs in their sessions.
  - ktonwatch prunes its store on its own timer (`serve -prune-every`).
  - The registry's delivery polls on ai1 run from a shell loop.
  - The improveR test runner image and Lukas's OQ environment rebuilds are started by hand.

  Nobody outside the session sees what runs, when it last ran, or what it did. A loop dies with its
  session.
- **ext-core has a model that works** (improve-ops' workflow player, `feature/verticles-ops`, read
  only):
  - **The repository is the server.** A workflow file in a queue folder carries its own `execution`
    block, and each dispatched step is linked to its workflow by a relation.
  - **A tick is a resumable heartbeat.** It dispatches every step that is ready, polls the running
    ones, writes the state back and exits. A crash loses nothing, because the next tick reads the state
    from the repository.
  - **The configuration is readable** (ticks, interval, concurrency). To control the player is to
    change that configuration (`PlayerApi.configure`); the facade holds no state of its own.
  - **A security gate:** a workflow is played only if the player's own user last changed it.
- **The cockpit has the parts but no clock.**
  - `cockpit workflow run` runs a workflow once, and a published ray or workflow step is a record.
  - The executor is a port (ADR-011 §3), and the module door is specified (ADR-015 §1).
  - The index daemon (ADR-012) shows how a per-machine daemon is operated (`cockpit index
    status|start|stop`).

## Decision

### 1 A job is declared as data: a signed `schedules` claim

A job is one claim of the built-in template **`schedules`**. Changing a job is a new `schedules` claim
that `replaces` the old one; stopping it is a new claim with `state: stopped` that replaces the last.
As everywhere since the core round, nothing is superseded implicitly and no claim is removed (MH,
18.11.). The job's identity is its name within the repository's scope. The claim in force is the one no
trusted claim replaces (`nk:replacedBy`, view vocabulary 1).

| field | meaning |
|---|---|
| `name` | the job's name, unique in the repository (`te-poll-robert1`) |
| `kind` | `workflow` (a ray or workflow package), `poll` (a command whose result is observed), `command` (a regular job: prune, rebuild, delivery) |
| `what` | `workflow`: the package and its revision by hash, plus `--bind` values. `poll`/`command`: the argv. The program must be on the runner's allow list (section 4). |
| `schedule` | `every <duration>` (`every 10m`) or a five-field cron expression, in the runner's time zone |
| `concurrency` | runs of this job allowed at once (default 1). A due tick while that many are running is **skipped and reported**, never queued. |
| `runner` | the machine that runs it (its runner name, section 2). A repository cloned on two machines runs the job once. |
| `owner` | the lane or person answerable for it (an area of the lanes registry) |
| `purpose` | one sentence: why it runs |
| `until` | the end date. **Required**: no job runs forever. Running it longer is a new claim. |
| `state` | `active` (default), `paused` or `stopped` |
| `notify` | optional: an area that is told when a run fails, or when a poll's answer changes |
| `hidden` | optional: the names of hidden configuration it needs (ADR-015 §3), such as a token for robert1. Supplied by the runner's site, never in the claim. |

The claim is about the job's subject: the package for `workflow`, the hash of the argv for `poll` and
`command`. So `ask about` a package also shows that it is scheduled.

### 2 One runner per machine, a daemon like the index daemon

- **The runner and its commands.** `cockpit jobs runner start|stop|status` manages one runner process
  per machine.
- **The repositories it serves.** It serves the repositories registered on that machine (`cockpit jobs
  enable` in a repository). The registration is machine state in `~/.cache/kton-cockpit/jobs/`
  (ADR-016 §2). It has a name, `jobs.runner` in the machine configuration, by default the host name.
- **A tick is the heartbeat** (one a minute; `jobs.tick`, readable configuration). For each served
  repository, a tick:
  1. reads the `schedules` claims in force for this runner (from the index, through `Ask`);
  2. reads the journal to see which runs are in flight and which have finished;
  3. starts every job that is due, within its `concurrency` and the runner's own limit (`jobs.concurrency`,
     default 4);
  4. writes the journal and exits.

  A crash or a restart loses nothing: the next tick reads the journal and the claims. Like the player,
  the tick never waits for a run.
- **Workflow and ray jobs** run each ready step through the **executor port** (ADR-011 §3). A step is
  published as `cockpit workflow run` publishes it, so its runs are records (fotons) like any other.
  The next tick starts the steps whose inputs are done. The workflow's plan (its steps) and the state of
  each step (pending, running, done, failed) are kept in the journal, in the role of the player's
  `execution` block.
- **`poll` and `command` jobs** run their argv under a timeout (`jobs.timeout`, default 10 minutes).
  They leave **observations only**: start, end, exit code and the hash of their output, written to the
  journal and to the index graph `urn:kton:observed:jobs` (the way ktonwatch hands observations to the
  cockpit, ADR-012). They make no fotons and no claims. A poll whose output hash changed, and any run
  that failed, says nothing itself: if `notify` is set, the runner raises a module event (section 3),
  and whoever subscribes decides what to say.
- **Who signs.** The runner publishes with the checkout's own identity (`.kton/local/identity.json`),
  as `cockpit publish` would there. It never signs a `schedules` claim, and it never signs as a person.
- **The journal** is `.kton/local/jobs/<name>.jsonl`, one line per tick per job. It is local, never
  shared (ADR-016 §1). Only the declarations and the published runs are in kton. The journal holds
  what the player keeps in its execution block, and it is enough to resume.

On improve the same runner can dispatch through improve's executor. ext-core's player stays improve's own
tool; this ADR does not replace it.

### 3 Monitored from outside

- **CLI** (operator commands, also `--json`):
  - `cockpit jobs list` shows each job with its kind, schedule, runner, state, `until`, owner, last tick
    and next tick.
  - `cockpit jobs status <name>` adds what the last ticks did. For a workflow job it shows the plan
    (its ray steps) and each step's state. For a poll it shows the last outputs and whether they
    changed.
- **Events** over the frontend protocol (ADR-011 §6), as module events: `job-ticked`, `job-run-started`,
  `job-run-finished`, `job-failed`, `poll-changed`, `job-skipped` (concurrency) and `job-expired`
  (`until` passed).
- **Until `cockpit serve --stdio` exists**, frontends call `jobs list|status --json`. The workgroup
  extension and the QM Workbench read it the same way: one answer for both, and neither reads the
  journal itself.
- **`cockpit doctor`** reports two problems: a runner that is registered but has not ticked for three
  intervals, and a job whose `until` is within a week.

### 4 Who may schedule: operator commands, a person confirms

- `cockpit jobs schedule <job.json> --as <person>`, `jobs pause|resume|stop <name> --as <person>`
  and `jobs enable` are **operator commands, CLI only** (SPEC §6). They are never on MCP. Over the
  frontend protocol they are operator commands that need a person's confirmation (ADR-011 §6).
  Scheduling decides what later runs and publishes unattended, so it is a person's signature.
- **An agent proposes.** A lane writes a job file (`jobs/<name>.job.json`, the claim's fields) and asks.
  `cockpit jobs schedule` shows it, and the person signs it. This is the line ADR-015 §1 draws for
  `tool install`.
- **The runner trusts only `schedules` claims signed by a key in the tiers `jobs.schedulers` names**
  (default: the repository's people tier). It fails closed, as the player's gate does. A claim from
  any other key is listed by `jobs list` as "not trusted to schedule" and never runs.
- **The allow list.** `poll` and `command` jobs may only start programs on the runner's allow list
  (`jobs.allow` in the machine configuration, set by the machine's operator). A signed claim does not
  make an arbitrary command runnable on someone else's machine.

### 5 The module

The module registers through the door of ADR-015 §1. It reaches the core only through `Publish`, `Ask`,
the executor and the index's read side, and it never opens a registry.

```go
module.Register(module.Module{
    Name:     "jobs",
    Commands: []module.Command{ /* schedule, pause, resume, stop, list, status, enable, runner */ },
    Profiles: nil,                 // jobs are claims, not packages
    Words:    jobsWords,           // the `schedules` template; named queries jobs-in-force, job-history
})
```

**To agree with kton-cockpit:**
- that `schedules` is a built-in template (like `qualifies-as`), so its fields are the same in every
  repository;
- that the observation graph `urn:kton:observed:jobs` is the jobs module's own;
- that the module may raise events through the door once events exist.

### 6 First users

| job | kind | today |
|---|---|---|
| test-execution: robert1 and 4315 health | `poll` every 5m, `notify` test-execution | a loop inside its Claude session |
| improveR test runner image | `workflow` (the runner's ray) on its schedule | started by hand |
| Lukas's OQ environment rebuilds | `command` (rebuild script), with hidden config for credentials | started by hand |
| the lanes registry's delivery polls on ai1 | `poll` every 1m, runner `ai1` | a shell loop |
| ktonwatch's store pruning | `command` daily (`ktonwatch prune`, limits as MH sets them) | `serve -prune-every`, off |

A lane's `/loop` that only waits on something outside (CI, a server) becomes a `poll` whose `notify`
nudges the lane, so the session no longer has to stay alive for it.

## Consequences

- Everything that runs regularly or for a long time is visible in one place (`jobs list`, the
  workgroup view, the QM Workbench), and survives a session or a machine restart.
- The declaration is evidence: who scheduled what, why, until when, and every change, all signed. The
  runs of workflow jobs are records. Polls stay observations, so a health check never becomes a claim.
- One more daemon per machine, operated like the index daemon. It must run on ai1 and on each workgroup
  member's PC that has jobs.
- SPEC gains a section for the `jobs` operator commands, the `schedules` template, the trust rule for
  schedulers, the allow list, and the journal's place among the `.kton/local` paths (SPEC §12.4).
  Every statement names its test.

## Steps

| step | content | needs |
|---|---|---|
| J0 | this ADR accepted; the door and template agreed with kton-cockpit | MH, kton-cockpit |
| J1 | `schedules` template; `jobs schedule/pause/resume/stop/list` over claims; trust rule | ADR-015 T1 (module registry), cockpit v0.5.0 |
| J2 | the runner: tick, journal, `poll`/`command` with timeout, allow list, observations, `jobs status`, `doctor` checks | J1 |
| J3 | `workflow` jobs: steps through the executor, plan and state per step, resumption | J2 |
| J4 | events over the frontend protocol; workgroup extension and QM Workbench views | J2, `cockpit serve --stdio` |
| J5 | first users moved (section 6), the in-session loops removed | J2–J3 |

## Open

- Whether ktonwatch's own timers (prune, the cas cap) become jobs, or stay inside ktonwatch with only
  their result reported. Proposed: jobs, so there is one place.
- Whether a `notify` on a poll change should be able to start another job (a trigger, like the player's
  `fireTriggers`). Proposed: not in J1–J3. A person or lane decides after the event.
