# examples

One property of the cockpit per directory, each a script you can read top to bottom.

```bash
examples/run-all.sh          # all of them
examples/02-chain/run.sh     # or one
```

Each builds its own participant repo from nothing — a real git repo with a real github.com origin,
because the anti-wrong-folder guard reads that remote on every call and an example that bypassed it
would be demonstrating something else. Only the remote's *push* url points at a local bare repo, so
everything completes offline. Binaries are built from a kton checkout (`KTON_SRC`, else `../kton`)
and the cockpit from this one; nothing is vendored and nothing comes from `$PATH`.

Identities come from `keygen --seed`, so ids are a function of this repository rather than of when
you ran it and two runs print the same hashes. Those are demo keys: the seed is written down, so the
private key is public and the identity is worth nothing — correct for a fixture, catastrophic for
anything real.

| | |
|---|---|
| [01-publish](01-publish/) | one call, and everything the caller did *not* have to do |
| [02-chain](02-chain/) | two steps join by a shared hash, and nothing declares the link |
| [03-anti-wrong-folder](03-anti-wrong-folder/) | the guard, and what it deliberately does not catch |
| [04-claim-ceiling](04-claim-ceiling/) | the claim shapes Claude may use, and the level it cannot state |
| [05-trust-tiers](05-trust-tiers/) | verified, not declared — the record does not change, the config does |
| [06-reproduce](06-reproduce/) | reproducing adds a signature, not a record |
| [07-run-in-container](07-run-in-container/) | the environment pin stops being a claim (needs docker) |
| [08-union-and-show](08-union-and-show/) | the graph, published and served, without reading the store |

## Why they live here and not in their own repository

kton-examples is separate from kton, and it went stale in exactly the way that predicts: its CI
checked out a repository that had been archived, every step silently skipped, and the run reported
success for six weeks. Examples that demonstrate *this binary* have to move with it, so they sit
beside it — the same reason `uat/` does.

## Every check says what it saw

This is the one convention worth stating. A check that only looks for the absence of an error passes
when a query returns nothing, which is how a suite reports success having verified nothing. So the
examples count: how many records were included, how many claims exist afterwards, whether the
registry grew.

And every guard has a **negative control**. Three refusals prove nothing if the same call also fails
when it should succeed — a guard that refuses everything is not a guard, and a lineage that returns
everything would also "find" the two records you expected. `expect_fail` fails the example when a
command it expected to be refused is accepted.
