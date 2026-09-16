# uat

Two participants and a federation, driven end to end by the three verbs.

```bash
uat/e2e.sh
```

Needs `git`, `go` and `python3`. No GitHub account, no network, no browser, no pauses — origins
are real github.com URLs so the anti-wrong-folder guard runs unmodified, and only the *push* url
points at a local bare repo.

## What it covers that `examples/` does not

Each example builds **one** participant and demonstrates **one** property. This is the
federation-shaped path, and three things only exist there:

- **Two identities.** Both participants sign with genuinely different keys, so a claim one makes
  about the other's work is a claim between parties rather than a repository talking to itself.
- **An aggregate.** Records are mirrored into a federation repo by hash — a filesystem operation
  over a peer's registry directory, with no server, no admission gate, and no network call.
- **A reproduction that means something.** The ↻2 at the end is two distinct verified signatures
  over the same output bytes.

The sequence is federation-first on purpose: participant 2 must be able to see participant 1's real
foton before it can reproduce it, so participant 1 publishes and is aggregated before participant 2
does anything at all.

## The arc

1. Participant 1 drops incomplete runs from an instrument log, summarises per instrument, and
   publishes.
2. Aggregated into the federation.
3. Participant 2 is given **the task in words, not the solution**, and writes its own script. Both
   are correct. The bytes differ — one sorts by sample count and rounds to two places — which is a
   realistic starting point rather than a mistake.
4. The federation is mirrored back into participant 2, which asks how many parties stand behind
   participant 1's result. One.
5. **The correction, which turns out to be a lookup.** This was the step the old UAT called the one
   that "genuinely needs reasoning". It does not: participant 1's foton names its inputs by hash and
   carries a commit-pinned locator for each, so the exact script that produced that result is
   fetchable. Participant 2 fetches it, re-runs it, and gets identical bytes.
6. Participant 2 says `reproduces`. **It does not get to state the level** — the cockpit runs the
   comparison itself and records what that answers. The same claim over participant 2's own,
   different bytes is refused, because a check that cannot fail is not a check.
7. Participant 1 mirrors the federation back and asks again. Two.

## Why it is a script now

It used to pause four times for a human to drive a browser, because the three verbs existed only
over MCP and nothing could type them. They are subcommands now, so the whole run is a script —
including step 5.

What was lost with the pauses: the old version created three real repositories under your GitHub
account and exercised a real `git push` over the network against `github.com`. This does not. If you
want that, point the participants' push urls at real repositories; everything above is unchanged.
