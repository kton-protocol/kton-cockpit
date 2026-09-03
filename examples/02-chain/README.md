# 02 - chain

Two publishes. Neither mentions the other.

```json
{ "cmd": "clean …", "inputs": ["data/raw.csv"],   "outputs": ["work/clean.csv"] }
{ "cmd": "sum …",   "inputs": ["work/clean.csv"], "outputs": ["work/total.csv"] }
```

There is no pipeline file, no ordering, no dependency declaration. Step two hashed the bytes step
one had written, so the two records carry the same hash — and that is the edge. `lineage` walks it
back without having been told anything.

This is why a chain here is a fact rather than a note. A pipeline definition says what *should*
follow what; this says what *did*, because the hash of the bytes that actually moved between the
steps is in both records.

## The negative control

`lineage` finding two records proves nothing on its own — a lineage that returned everything in the
registry would also find two. So the example publishes a third, unrelated result and asserts its
lineage contains only itself. Without that, the demonstration would be indistinguishable from a
query that ignores its argument.
