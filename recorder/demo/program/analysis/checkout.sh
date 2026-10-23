#!/bin/sh
# Check out a study's dataset into this analysis: copy it from the study's repository, and ask
# the cockpit whose record these bytes are (the study's, through federation).
#   ./checkout.sh <study-repo> <file in it>      e.g. ../study-thp-101 data/thp101_pk.csv
set -e
src=$1; file=$2
mkdir -p data
cp "$src/$file" "data/$(basename "$file")"
sum=$(sha256sum "data/$(basename "$file")" | cut -d' ' -f1)
echo "data/$(basename "$file")  sha256:$sum"
cockpit ask "{\"query\":\"producer\",\"ref\":\"sha256:$sum\"}" | python3 -c '
import json, sys
recs = json.load(sys.stdin).get("records") or []
for r in recs:
    print("  published by %s  %s%s" % (r.get("tier"), r.get("id"), "" if r.get("verified") else "  (not verified)"))
if not recs:
    print("  not published in any federated, trusted repository")'
