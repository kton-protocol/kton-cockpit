#!/usr/bin/env bash
# 10 - the cleaning step is the argument, and it is an input.
#
# Two analyses of the same open data, differing by ONE LINE, both defensible, both honest.
# No row is fabricated, no number is invented, and they disagree by centuries.
#
# What a cockpit adds is not a lie detector — nobody lies here. It records the SCRIPT as an
# input by hash, so "which of these two did you run" is a lookup instead of a question.
#
# Needs R and network.
cd "$(dirname "$0")"
source ../lib/common.sh
command -v Rscript >/dev/null || { echo "10 needs R (Rscript) — skipping is not an option here, so this is a failure" >&2; exit 1; }
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"
mkdir -p R

echo ""
echo "== the data, fetched the way a person fetches it =="
# Required by the licence, and printed rather than buried: the register is Open Government Data
# of the City of Vienna under CC BY 4.0, and attribution is a condition of use, not a courtesy.
echo "  Datenquelle: Stadt Wien – data.wien.gv.at"
echo "  Vienna Tree Register (BAUMKATOGD), CC BY 4.0"
# Only the two columns this is about, which the WFS will project for us: 9 MB instead of 53.
WFS="https://data.wien.gv.at/daten/geo?service=WFS&request=GetFeature&version=1.1.0"
WFS="$WFS&typeName=ogdwien:BAUMKATOGD&srsName=EPSG:4326&outputFormat=csv&propertyName=BEZIRK,PFLANZJAHR"
curl -sL --max-time 300 -o data/trees.csv "$WFS" || { echo "  the download failed; this example needs network" >&2; exit 1; }
require "the register downloaded" test -s data/trees.csv
echo "  $(wc -l < data/trees.csv) rows, $(du -h data/trees.csv | cut -f1)"

# The hash pins the BYTES that were used. data.wien.gv.at is a live service: the register this
# ran against is not the one somebody downloads next month, and nothing but a hash records which.
DATA=$(./bin/plankton hash data/trees.csv)
echo "  pinned as ${DATA:0:24}"

echo ""
echo "== the same question, asked twice =="

cat > R/oldest-districts.R <<'RS'
# Datenquelle: Stadt Wien - data.wien.gv.at | Vienna Tree Register (BAUMKATOGD), CC BY 4.0
#
# Which Vienna districts have the oldest trees?
# PFLANZJAHR is the planting year. Read it, take the mean per district, rank.
d <- read.csv("data/trees.csv", colClasses = "character")
d <- data.frame(district = d$BEZIRK, year = as.numeric(d$PFLANZJAHR))
a <- aggregate(year ~ district, data = d, FUN = function(y) round(2026 - mean(y), 1))
a <- a[order(-a$year), ]
write.csv(head(a, 5), "work/oldest.csv", row.names = FALSE)
RS
CODE_ASIS=$(./bin/plankton hash R/oldest-districts.R)
Rscript R/oldest-districts.R
AS_IS=$(cockpit publish '{"cmd":"Rscript R/oldest-districts.R",
                          "inputs":["data/trees.csv","R/oldest-districts.R"],
                          "outputs":["work/oldest.csv"]}')
RANK_ASIS=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/oldest.csv"])' <<<"$AS_IS")
echo "  as written:"; sed 's/^/    /' work/oldest.csv

# One line. The register encodes "planting year unknown" as 0 — PFLANZJAHR_TXT says
# `nicht definiert` on every one of them — and a mean over a sentinel is a mean over a fiction.
cat > R/oldest-districts-cleaned.R <<'RS'
# Datenquelle: Stadt Wien - data.wien.gv.at | Vienna Tree Register (BAUMKATOGD), CC BY 4.0
#
# Which Vienna districts have the oldest trees?
# PFLANZJAHR is the planting year, and 0 is its "unknown" sentinel. Drop those first.
d <- read.csv("data/trees.csv", colClasses = "character")
d <- data.frame(district = d$BEZIRK, year = as.numeric(d$PFLANZJAHR))
d <- d[d$year > 0, ]
a <- aggregate(year ~ district, data = d, FUN = function(y) round(2026 - mean(y), 1))
a <- a[order(-a$year), ]
write.csv(head(a, 5), "work/oldest.csv", row.names = FALSE)
RS
CODE_CLEAN=$(./bin/plankton hash R/oldest-districts-cleaned.R)
Rscript R/oldest-districts-cleaned.R
CLEANED=$(cockpit publish '{"cmd":"Rscript R/oldest-districts-cleaned.R",
                            "inputs":["data/trees.csv","R/oldest-districts-cleaned.R"],
                            "outputs":["work/oldest.csv"]}')
RANK_CLEAN=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/oldest.csv"])' <<<"$CLEANED")
echo "  with the sentinel dropped:"; sed 's/^/    /' work/oldest.csv

echo ""
echo "== both are true, and they disagree =="
require "the two rankings are different bytes" test "$RANK_ASIS" != "$RANK_CLEAN"
# Nothing was filtered out to make a point, nothing was added, and no figure is impossible on its
# own. A reader who spot-checks one district may well find it in both lists.

echo ""
echo "== so which one produced the number on the poster? =="
used_by() {
  cockpit ask "{\"query\":\"uses\",\"ref\":\"$1\"}" \
    | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))'
}
producer_of() {
  cockpit ask "{\"query\":\"producer\",\"ref\":\"$1\"}" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); print(",".join(d.get("included",[])))'
}

require "both runs consumed the same register"            test "$(used_by "$DATA")" = "2"
require "the as-written script was used by exactly one"   test "$(used_by "$CODE_ASIS")" = "1"
require "the cleaned script was used by exactly one"      test "$(used_by "$CODE_CLEAN")" = "1"

P_ASIS=$(producer_of "$RANK_ASIS"); P_CLEAN=$(producer_of "$RANK_CLEAN")
require "each ranking names one producing run"  test -n "$P_ASIS" -a -n "$P_CLEAN"
require "and they are different runs"           test "$P_ASIS" != "$P_CLEAN"

echo ""
echo "== the negative control =="
# A guard that answers everything is not a guard. A third file nobody recorded must have no
# producer at all — otherwise "found the run" would mean nothing.
printf 'district,year\n1,999\n' > work/invented.csv
INVENTED=$(./bin/plankton hash work/invented.csv)
absent "an unrecorded result has no producer" "$INVENTED" "$(producer_of "$INVENTED")"

echo ""
echo "  Nobody fabricated anything. One line decides whether the 23rd district's trees are"
echo "  900 years old or 40, and that line is an input with a hash. The record does not say"
echo "  which analysis is right — it says which one was run, and lets anyone run it again."
echo ""
# The attribution is not only printed: it is the first line of both scripts, and a script is an
# input recorded by hash. Whoever holds one of these records holds the source statement with it.
echo "  Datenquelle: Stadt Wien – data.wien.gv.at (CC BY 4.0)"
