#!/usr/bin/env bash
# 08 - the graph, published and served, without the cockpit ever reading the store.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"

CFG=$(python3 - "$REPO/examples/lib/cockpit.config.json" <<'PY'
import json,sys
c=json.load(open(sys.argv[1])); c["union"]={"publish":True}; print(json.dumps(c,indent=2))
PY
)
participant "$WORK/repo" "$CFG"; cd "$WORK/repo"

echo ""
echo "== a foton and a claim =="
printf 'result\n59\n' > work/out.csv
PUB=$(cockpit publish '{"cmd":"compute > work/out.csv","inputs":[],"outputs":["work/out.csv"]}')
FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$PUB")
SAY=$(cockpit say "{\"subject\":\"$FOTON\",\"template\":\"working-on\",\"fields\":{\"step\":\"analysis\",\"by-session\":\"session-1\"}}")
CLAIM=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["claimId"])' <<<"$SAY")

echo ""
echo "== the union is committed WITH the record, not after it =="
require "docs/data/union.json is tracked" test -n "$(git -C "$WORK/repo" ls-files docs/data/union.json)"
count() { python3 -c 'import json,sys; d=json.load(open("docs/data/union.json")); print(sum(1 for r in d if sys.argv[1] in r))' "$1"; }
require "it holds the foton"  test "$(count fotonId)" = "1"
require "and the claim"       test "$(count claimId)" = "1"
require "the foton that was just published" grep -q "$FOTON" docs/data/union.json
require "and the claim that was just made"  grep -q "$CLAIM" docs/data/union.json
echo "  a union published one commit later would mean every online view is a record behind"

echo ""
echo "== cockpit show serves the same three files, live =="
PORT=8391
./bin/cockpit show "127.0.0.1:$PORT" >/dev/null 2>&1 &
SHOW=$!; trap 'kill $SHOW 2>/dev/null || true' EXIT
for _ in $(seq 1 40); do curl -sf "http://127.0.0.1:$PORT/data/union.json" >/dev/null 2>&1 && break; sleep 0.25; done

SERVED=$(curl -s "http://127.0.0.1:$PORT/data/union.json")
N=$(python3 -c 'import json,sys; print(len(json.load(sys.stdin)))' <<<"$SERVED")
require "the served union carries both records" test "$N" = "2"
require "in the shape a viewer reads" \
  python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if all(("envelope" in r) and ("fotonId" in r or "claimId" in r) for r in d) else 1)' <<<"$SERVED"

KEYS=$(curl -s "http://127.0.0.1:$PORT/data/keys.json")
require "keys.json holds the two CONFIGURED keys, not whatever is in the registry" \
  python3 -c 'import json,sys; sys.exit(0 if len(json.load(sys.stdin))==2 else 1)' <<<"$KEYS"
SIGNER=$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d[0]["envelope"]["signatures"][0]["keyid"])' <<<"$SERVED")
require "and every signature resolves in it" \
  python3 -c 'import json,sys; sys.exit(0 if sys.argv[1] in json.load(sys.stdin) else 1)' "$SIGNER" <<<"$KEYS"
echo "  so a viewer re-verifies against exactly what cockpit_ask does"
