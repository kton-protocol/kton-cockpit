# examples

One property of the cockpit per directory, each a script you can read top to bottom.

```bash
examples/run-all.sh          # all of them
examples/02-chain/run.sh     # or one
```

Each builds its own participant repo from nothing — a real git repo with a real github.com origin,
because the anti-wrong-folder guard reads that remote on every call and an example that bypassed it
would be demonstrating something else. Only the remote's *push* url points at a local bare repo, so
everything completes offline. Binaries are built from a kton checkout (`KTON_SRC`, else `../kton-pinned`, else `../kton`) and the
cockpit from this one; nothing is vendored and nothing comes from `$PATH`.

The examples drive the kernel CLI directly in places, which the cockpit itself no longer does — it
links the kernels as libraries. That is deliberate: what these show is that the work stays
performable by hand, so where a step is something a person would do at a prompt, it is done at a
prompt.

Identities come from `keygen --seed`, so ids are a function of this repository rather than of when
you ran it and two runs print the same hashes. Those are demo keys: the seed is written down, so the
private key is public and the identity is worth nothing — correct for a fixture, catastrophic for
anything real.

| | |
|---|---|
| [01-publish](01-publish/) | one call, and everything the caller did *not* have to do |
| [02-chain](02-chain/) | two steps join by a shared hash, and nothing declares the link |
| [03-anti-wrong-folder](03-anti-wrong-folder/) | the guard, and what it deliberately does not catch |
| [04-claim-ceiling](04-claim-ceiling/) | the claim shapes a session may use, and the level it cannot state |
| [05-trust-tiers](05-trust-tiers/) | verified, not declared — the record does not change, the config does |
| [06-reproduce](06-reproduce/) | reproducing adds a signature, not a record |
| [07-run-in-container](07-run-in-container/) | the environment pin stops being a claim (needs docker) |
| [08-union-and-show](08-union-and-show/) | the graph, published and served, without reading the store |
| [09-carried-evidence](09-carried-evidence/) | whose key signed, and the difference between checked and carried (needs openssl) |
| [10-cleaning-is-an-argument](10-cleaning-is-an-argument/) | one line decides the answer by centuries, and it is an input (needs R + network) |
| [11-environment-reconstructible](11-environment-reconstructible/) | the same numbers twice, and only one record can be re-run by anyone else (needs nix + docker) |
| [12-the-environment-written-in-r](12-the-environment-written-in-r/) | nobody writes Nix: the environment is declared in R, and the digest follows (needs nix + docker) |

## Sources

`12-the-environment-written-in-r` uses **{rix}** (rOpenSci), which generates a Nix expression from
an R function call: <https://docs.ropensci.org/rix/>. The lift from the expression it generates to
an OCI image is that example's own — rix's documented container route runs Nix *inside* a Docker
image, which pins a base image nobody chose as well as the environment somebody did.

`10-cleaning-is-an-argument` uses the **Vienna Tree Register** (`BAUMKATOGD`), Open Government Data
of the City of Vienna under **CC BY 4.0**. Required attribution: *Datenquelle: Stadt Wien –
data.wien.gv.at*. It is downloaded at run time rather than vendored: the register is a live service,
and pinning the bytes that were actually used is half of what the example is about.

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
