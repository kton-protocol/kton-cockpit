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

## Two shas, and why they differ

publish makes two commits — the files first, then the signed registry entry, because the foton
cannot be authored until the permalinks it embeds already exist. So two shas exist and they mean
different things:

| | |
|---|---|
| `commitSha` returned | the repo's real final state after the call — the registry commit |
| the foton's own `uri` | the *first* commit, unavoidably: authoring happens between the two |

Both resolve, because the files' bytes are identical in either. The example reads the embedded
locator back out of the foton with `plankton show --json` and asserts that its sha is the returned
one's **parent** — not merely that the two differ, which a comparison against an undefined variable
would also have "proved".

## What else it asserts

That the output is committed, that everything was pushed, and — the one that carries weight — that a
`producer` query finds the foton and it verifies into a configured trust tier.

That last assertion is not decoration. A query that found *nothing* satisfies every check that only
looks for the absence of an error, which is how a test suite reports success having verified
nothing. So the example counts what was included, and fails at zero.
