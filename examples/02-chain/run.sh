#!/usr/bin/env bash
# 02 - chain: two steps join because they share a hash, not because anyone said they follow.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"

echo ""
echo "== step one =="
printf 'id,value\n1,42\n2,\n3,17\n' > data/raw.csv
printf 'id,value\n1,42\n3,17\n'     > work/clean.csv
CLEAN=$(cockpit publish '{"cmd":"clean data/raw.csv > work/clean.csv","inputs":["data/raw.csv"],"outputs":["work/clean.csv"]}')
CLEAN_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/clean.csv"])' <<<"$CLEAN")
CLEAN_FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$CLEAN")
echo "  produced work/clean.csv = $CLEAN_HASH"

echo ""
echo "== step two: it names the same PATH as an input, and nothing else =="
printf 'total\n59\n' > work/total.csv
TOTAL=$(cockpit publish '{"cmd":"sum work/clean.csv > work/total.csv","inputs":["work/clean.csv"],"outputs":["work/total.csv"]}')
TOTAL_FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$TOTAL")
echo "  consumed work/clean.csv, produced work/total.csv"
echo ""
echo "  Nothing in either call mentions the other. No pipeline file, no ordering, no dependency"
echo "  declaration. Step two hashed the same bytes step one wrote, so the two records share a"
echo "  hash — and that IS the edge."

echo ""
echo "== the graph agrees =="
TOTAL_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/total.csv"])' <<<"$TOTAL")
LIN=$(cockpit ask "{\"query\":\"lineage\",\"ref\":\"$TOTAL_HASH\"}")
N=$(python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))' <<<"$LIN")
require "lineage walks back through both steps" test "$N" = "2"
require "and reaches step one"                  grep -q "$CLEAN_FOTON" <<<"$LIN"
require "from step two"                         grep -q "$TOTAL_FOTON" <<<"$LIN"

echo ""
echo "== the negative control: an unrelated result joins nothing =="
printf 'unrelated\n' > work/other.csv
OTHER=$(cockpit publish '{"cmd":"echo unrelated > work/other.csv","inputs":[],"outputs":["work/other.csv"]}')
OTHER_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/other.csv"])' <<<"$OTHER")
OTHER_LIN=$(cockpit ask "{\"query\":\"lineage\",\"ref\":\"$OTHER_HASH\"}")
ON=$(python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))' <<<"$OTHER_LIN")
# Without this, "lineage found 2" would prove nothing: a lineage that returned everything in the
# registry would also find 2. This shows the edge is the shared hash and not mere co-residence.
require "a record sharing no hash has a lineage of only itself" test "$ON" = "1"
