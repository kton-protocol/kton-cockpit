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
# The permalink pins the commit holding the DATA, which is not HEAD: publish commits the files
# first and the registry second, so HEAD is one commit further on. A locator pinned to HEAD would
# point at a commit that does contain the file — here — and would not, the moment anything else
# lands between the two. So it pins the sha publish reported.
DATA_SHA=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["commitSha"])' <<<"$OUT")
require "the output is committed"        test -n "$(git log --oneline -- work/sum.csv)"
require "everything was pushed"          test "$(git rev-parse HEAD)" = "$(git -C "$WORK/origin.git" rev-parse main)"
require "the permalink pins the data commit, not HEAD" grep -q "$DATA_SHA/work/sum.csv" <<<"$OUT"
require "and HEAD really is further on"  test "$(git rev-parse HEAD)" != "$DATA_SHA"

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
