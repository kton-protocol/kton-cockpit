# 04 - the claim ceiling, and the one thing a session cannot state

`cockpit_say` binds a claim from a fixed set of templates named in `cockpit.config.json`. The caller
picks one and fills its fields. It cannot invent a claim shape, and a template outside the ceiling
is refused.

The sharper half is `reproduces`. the caller does not supply the level:

```json
{ "subject": "sha256:…", "template": "reproduces",
  "subjectOutputHash": "sha256:…", "reproducedOutput": "work/rerun.csv",
  "reproducedFotonId": "sha256:…" }
```

No `level` anywhere. The cockpit runs `plankton reproduces` over the two output hashes itself and
records what that answers — `L0` here, because the bytes are identical. Hand it bytes that do not
match and no claim is written at all, which the example checks by counting the claims afterwards
rather than trusting the refusal message.

That count is the assertion that matters. "The call failed" and "the call failed and left nothing
behind" are different statements, and only the second is what a precondition is for.
