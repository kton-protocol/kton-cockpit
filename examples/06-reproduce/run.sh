#!/usr/bin/env bash
# 06 - reproducing does not add a record. It adds a signature to the one that is already there.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"
export PLANKTON_DIR="$PWD/registry/plankton"

count_records() { find registry/plankton/objects -name '*.json' 2>/dev/null | wc -l; }

echo ""
echo "== publish once =="
printf 'id,value\n1,42\n' > data/in.csv
printf 'total\n42\n'      > work/out.csv
A=$(cockpit publish '{"cmd":"sum data/in.csv > work/out.csv","inputs":["data/in.csv"],"outputs":["work/out.csv"]}')
A_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$A")
N1=$(count_records)
echo "  foton $A_ID"
echo "  registry holds $N1 record(s)"

echo ""
echo "== publish the SAME work again =="
B=$(cockpit publish '{"cmd":"sum data/in.csv > work/out.csv","inputs":["data/in.csv"],"outputs":["work/out.csv"]}')
B_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$B")
N2=$(count_records)

require "the same work is the SAME foton, not a second one" test "$A_ID" = "$B_ID"
require "and the registry did not grow"                     test "$N1" = "$N2"
echo ""
echo "  The foton id is the hash of the DESCRIPTOR — the input and output names and hashes and the"
echo "  command. Identical work therefore has an identical id, and a second producer's signature is"
echo "  unioned onto the record that exists. Reproducibility here is not a report saying 'matched';"
echo "  it is the absence of a new record."

echo ""
echo "== change one byte of the output =="
printf 'total\n43\n' > work/out.csv
C=$(cockpit publish '{"cmd":"sum data/in.csv > work/out.csv","inputs":["data/in.csv"],"outputs":["work/out.csv"]}')
C_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$C")
N3=$(count_records)
# The control: if identical work produced one record for any reason other than being identical,
# different work would too. This shows the id really does follow the bytes.
require "different bytes are a different foton" test "$A_ID" != "$C_ID"
require "and that one IS a new record"          test "$N3" -gt "$N2"

echo ""
echo "== who produced these bytes, verified =="
HASH=$(./bin/plankton hash work/out.csv)
REP=$(cockpit ask "{\"query\":\"reproductions\",\"ref\":\"$HASH\"}")
SIGNERS=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("verifiedSigners",0))' <<<"$REP")
require "counted by verified signature, not by self-declared keyid" test "$SIGNERS" -ge 1
echo "  the count comes from plankton over exactly the keys this repo's config names"
