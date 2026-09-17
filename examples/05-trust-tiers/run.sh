#!/usr/bin/env bash
# 05 - verified, not declared: a record is trusted because a CONFIGURED key verifies it, never
# because it turned up in the registry.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"

export PLANKTON_DIR="$PWD/registry/plankton"

echo ""
echo "== our own record =="
printf 'ours\n' > work/ours.csv
PUB=$(cockpit publish '{"cmd":"produce work/ours.csv","inputs":[],"outputs":["work/ours.csv"]}')
OURS=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/ours.csv"])' <<<"$PUB")

echo ""
echo "== a stranger's record, put into the SAME registry =="
# Signed with a key this repo's config knows nothing about. It is a perfectly valid record — not
# forged, not corrupt — by someone outside this repo's trust configuration. That distinction is the
# whole subject: "valid" and "trusted here" are different questions.
./bin/plankton keygen keys/stranger --seed "$(demoseed stranger)" >/dev/null
printf 'theirs\n' > work/theirs.csv
./bin/plankton author --in work/theirs.csv --out work/theirs.csv \
  --cmd "a stranger's computation" --sign keys/stranger.key --add >/dev/null 2>&1
THEIRS=$(./bin/plankton hash work/theirs.csv)
echo "  ingested, signed by a key that is in no configured tier"

echo ""
echo "== what ask does with each =="
A=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$OURS\"}")
B=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$THEIRS\"}")
inc() { python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))' <<<"$1"; }
exc() { python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("excluded",[])))' <<<"$1"; }

require "our record is included"                     test "$(inc "$A")" = "1"
require "the stranger's record is NOT included"      test "$(inc "$B")" = "0"
# Accounted for, not hidden: the answer says something was found and left out, which is a different
# statement from "nothing is there" and the reader needs to be able to tell them apart.
require "but it IS reported as found and excluded"   test "$(exc "$B")" = "1"
# Asserted on the foton's ID in the structured answer, not on its command text. The lineage
# queries report id, kind and counts — never the command — so searching for "a stranger" would
# hold whether the record was included or not, which is a check that cannot fail.
# The id comes from `summary`, which is where kton §12 puts it: `records` is an array of bare
# ENVELOPES, so there is no id to read off one. Taken by name rather than by position — a JSON
# object has no order, and this query has exactly one producer.
THEIRS_FOTON=$(./bin/plankton producer --json "$THEIRS" | python3 -c 'import json, sys
d = json.load(sys.stdin)
ids = list(d.get("summary", {}))
if len(ids) != 1:
    sys.exit("expected exactly one producer, got %d" % len(ids))
print(ids[0])')
absent  "and the record itself is not in the answer's included set" "$THEIRS_FOTON" \
  "$(python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin).get("fotons",[])))' <<<"$B")"
require "though the answer does account for having found it" grep -q "$THEIRS_FOTON" <<<"$B"

echo ""
echo "== now configure that key into a tier =="
python3 - cockpit.config.json <<'PY'
import json,sys
p=sys.argv[1]; c=json.load(open(p))
c["trust"]["tiers"]["federation"] = ["keys/stranger.pub"]
json.dump(c, open(p,"w"), indent=2)
PY
C=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$THEIRS\"}")
require "the same record is now included" test "$(inc "$C")" = "1"
echo "  the record did not change. The configuration did."

echo ""
echo "== and a filter narrows within the tiers, it does not widen =="
D=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$THEIRS\",\"filter\":{\"trustTier\":\"self\"}}")
require "asking only for tier 'self' excludes the federation record" test "$(inc "$D")" = "0"
