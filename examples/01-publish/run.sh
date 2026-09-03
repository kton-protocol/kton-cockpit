#!/usr/bin/env bash
# 01 - publish: one call records a signed, reproducible foton and everything that goes with it.
#
# The point is what the CALLER does not do. It names paths and the command it ran. It does not
# commit, push, construct a permalink, choose a key, or sign — the cockpit owns all of that, which
# is why those are the things a session cannot get subtly wrong.
cd "$(dirname "$0")"
source ../lib/common.sh

WORK="$PWD/.work"
participant "$WORK/repo"
cd "$WORK/repo"

echo ""
echo "== a result to publish =="
printf 'id,value\n1,42\n2,17\n' > data/in.csv
printf 'total\n59\n'           > work/sum.csv
echo "  data/in.csv -> work/sum.csv"

echo ""
echo "== cockpit_publish =="
OUT=$(cockpit publish '{
  "cmd": "sum data/in.csv > work/sum.csv",
  "inputs": ["data/in.csv"],
  "outputs": ["work/sum.csv"]
}')
echo "$OUT" | python3 -c 'import json,sys; d=json.load(sys.stdin); print("\n".join(f"  {k}: {v}" for k,v in sorted(d.items()) if not isinstance(v, dict)))'

FOTON=$(echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])')
HASH=$(echo "$OUT" | python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/sum.csv"])')

echo ""
echo "== what the cockpit did on the caller's behalf =="
# publish makes TWO commits — the files first, then the signed registry entry, because the foton
# cannot be authored until the permalinks it embeds exist. Two shas therefore exist, and they mean
# different things:
#
#   commitSha (returned)  the repo's real final state after this call. Reporting the first would be
#                         stale the moment publish returned.
#   the foton's OWN uri   pinned to the first commit, unavoidably: authoring happens between the
#                         two. Both resolve — the files' bytes are identical in either.
DATA_SHA=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["commitSha"])' <<<"$OUT")
require "the output is committed"        test -n "$(git log --oneline -- work/sum.csv)"
require "everything was pushed"          test "$(git rev-parse HEAD)" = "$(git -C "$WORK/origin.git" rev-parse main)"
require "the reported sha IS the repo's final state" test "$DATA_SHA" = "$(git rev-parse HEAD)"
require "and the returned permalink pins it" grep -q "$DATA_SHA/work/sum.csv" <<<"$OUT"

# The foton's embedded locator is a different sha, and this is the assertion that shows it rather
# than the comment above claiming it.
EMBEDDED=$(PLANKTON_DIR="$PWD/registry/plankton" ./bin/plankton show "$FOTON" --json \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["outputs"][0]["uri"][0])')
EMBEDDED_SHA=$(sed -E 's#.*/([0-9a-f]{40})/.*#\1#' <<<"$EMBEDDED")
require "the foton's locator carries a real commit sha" test "${#EMBEDDED_SHA}" = "40"
require "which is NOT the one publish returned"         test "$EMBEDDED_SHA" != "$DATA_SHA"
require "and it is that sha's parent"                   test "$EMBEDDED_SHA" = "$(git rev-parse "$DATA_SHA^")"
require "both name the same file"                       grep -q 'work/sum.csv$' <<<"$EMBEDDED"
echo "  returned:  $DATA_SHA"
echo "  in foton:  $EMBEDDED_SHA  (its parent)"

echo ""
echo "== and the record is queryable, and verifies =="
ASK=$(cockpit ask "{\"query\": \"producer\", \"ref\": \"$HASH\"}")
INCLUDED=$(echo "$ASK" | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included", [])))')
# The assertion that matters: a query that FOUND NOTHING would satisfy every check that only looks
# for the absence of an error. This one fails unless the foton is actually there and verified.
require "producer found the foton and it verified into a configured tier" test "$INCLUDED" = "1"
require "it is the foton publish reported"  grep -q "$FOTON" <<<"$ASK"

echo ""
echo "done: foton $FOTON"
