#!/usr/bin/env bash
# 04 - what Claude may say, and what it may not decide.
#
# cockpit_say binds a claim from a FIXED set of templates. Claude picks one and fills its fields; it
# cannot invent a claim shape. And for `reproduces` it cannot state the level either — the cockpit
# runs the check itself and records what that answers.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"

printf 'result\n59\n' > work/out.csv
PUB=$(cockpit publish '{"cmd":"compute > work/out.csv","inputs":[],"outputs":["work/out.csv"]}')
FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$PUB")
HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/out.csv"])' <<<"$PUB")

echo ""
echo "== a template inside the ceiling =="
SAY=$(cockpit say "{\"subject\":\"$FOTON\",\"template\":\"working-on\",\"fields\":{\"step\":\"analysis\",\"by-session\":\"session-1\"}}")
CLAIM=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["claimId"])' <<<"$SAY")
require "the claim was recorded" grep -q '^sha256:' <<<"$CLAIM"

ABOUT=$(cockpit ask "{\"query\":\"about\",\"ref\":\"$FOTON\"}")
require "and reading it back gives what it SAYS, not just that it exists" \
  grep -q '"step": "analysis"' <<<"$ABOUT"

echo ""
echo "== a template outside it =="
expect_fail "a claim shape this repo does not allow" \
  cockpit say "{\"subject\":\"$FOTON\",\"template\":\"gxp/review\",\"fields\":{\"outcome\":\"pass\"}}"

echo ""
echo "== reproduces: the cockpit computes the level, it does not accept one =="
cp work/out.csv work/rerun.csv                      # byte-identical
R=$(cockpit say "{\"subject\":\"$FOTON\",\"template\":\"reproduces\",\"subjectOutputHash\":\"$HASH\",\"reproducedOutput\":\"work/rerun.csv\",\"reproducedFotonId\":\"$FOTON\"}")
LEVEL=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("level",""))' <<<"$R")
require "identical bytes were assessed as L0" test "$LEVEL" = "L0"
echo "  no level was passed in — the cockpit ran plankton reproduces and recorded its answer"

echo ""
printf 'result\n999\n' > work/wrong.csv
expect_fail "a reproduction claim over bytes that do not match" \
  cockpit say "{\"subject\":\"$FOTON\",\"template\":\"reproduces\",\"subjectOutputHash\":\"$HASH\",\"reproducedOutput\":\"work/wrong.csv\",\"reproducedFotonId\":\"$FOTON\"}"
echo "  and nothing was written: a refused precondition records no claim at all"
AFTER=$(cockpit ask "{\"query\":\"about\",\"ref\":\"$FOTON\"}")
N=$(python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))' <<<"$AFTER")
require "still exactly the two claims that were legitimately made" test "$N" = "2"
