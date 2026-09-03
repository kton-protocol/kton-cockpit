# 05 - verified, not declared

A record is trusted because a key named in `cockpit.config.json` verifies its signature — never
because it is sitting in the registry, and never because of the `keyid` the envelope declares about
itself.

The example puts two records in the same registry: one this repo produced, and one signed by a key
the config knows nothing about. The second is a perfectly valid record — not forged, not corrupt —
by someone outside this repo's trust configuration. "Valid" and "trusted here" are different
questions, and the whole point is that the cockpit answers the second.

`ask` includes the first and excludes the second. Then the example adds the stranger's key to a
`federation` tier and asks again: now it is included. **The record did not change. The
configuration did.**

## Two details worth the extra assertions

The excluded record is *reported* as found-and-excluded rather than silently dropped. "Nothing is
there" and "something is there that you do not trust" are different answers, and a reader has to be
able to tell them apart. But its *content* never reaches the answer — the example greps for it and
requires its absence.

And a filter narrows within the configured tiers; it cannot widen past them. Asking for tier `self`
excludes the federation record, which is the ceiling doing its job in the other direction.
