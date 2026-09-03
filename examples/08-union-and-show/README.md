# 08 - the graph, published and served

Two ways to reach the same records, and neither parses the store.

**Published.** With `union.publish` on, `union.json`, `keys.json` and `names.json` are regenerated
after every record and committed *in the same commit as it*. A union published one commit later
would mean every online view is a record behind the registry it summarises, with no way for a reader
to tell which state they are looking at.

**Served.** `cockpit show` answers the same three files live. It starts `kton serve` for both
substrates and forwards their `/sync` records — a record there is `{seq, fotonId|claimId, envelope}`,
which is exactly what kton-web's reader accepts.

## Why the cockpit does not read the registry itself

kton-web's own reader measures what that costs: against a 2032-record corpus, a whole-file
`JSON.parse` alone found 68 records and skipped 44 files "without a word", and the graph viewer drew
a perfectly convincing lineage-only picture from it. Nothing errored. The cockpit has never known
the store layout, so it cannot make that mistake.

## keys.json is the configured tiers

Not every `.pub` in the registry — kton-web's own union builder takes the directory, which is right
for a corpus someone hands you and wrong here, where the config is the ceiling everywhere else. A
viewer re-verifying against a wider ring than `cockpit_ask` uses would display as trusted exactly
what `ask` excludes. The example asserts the count, and that every signature in the union resolves
to a key in it.
