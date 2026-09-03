# 01 - publish

One `cockpit_publish` call, and what it does that the caller did not ask for.

## The call

```json
{ "cmd": "sum data/in.csv > work/sum.csv",
  "inputs": ["data/in.csv"],
  "outputs": ["work/sum.csv"] }
```

Paths and a command. That is the entire vocabulary.

## What comes back

A foton id, the output hashes, a commit sha, and one commit-pinned permalink per file. The caller
constructed none of it. It did not choose a signing key, build a URL, decide commit order, or touch
git — those are exactly the steps a session doing this by hand gets subtly wrong, and the reason
this project exists.

## What the example asserts

That the output is committed, that the commit was pushed, that the permalink pins *that* commit,
and — the one that carries weight — that a `producer` query finds the foton and it verifies into a
configured trust tier.

That last assertion is not decoration. A query that found *nothing* satisfies every check that only
looks for the absence of an error, which is how a test suite reports success having verified
nothing. So the example counts what was included, and fails at zero.
