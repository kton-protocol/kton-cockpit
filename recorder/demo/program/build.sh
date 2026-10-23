#!/bin/sh
# A programme in context, for the recorder: an indication, projects per compound, studies with
# data, each level its own cockpit repository with its documents and data published, and an
# analysis repository that checks out a study's dataset and works on it.
#
#   recorder/demo/program/build.sh [DIR]      default ~/kton-program-example; DIR must not exist
#
# DIR/program-respiratory/                       programme: strategy, target product profile
#      project-theophylline/                     project: development plan, compound profile
#        study-thp-101/  study-thp-102/          studies: protocol (+ SAP), data/
#      project-compound-x/
#        study-cx-201/
# DIR/analysis-thp-poppk/                        where you work: scripts, federated to the above
#
# Every level is a cockpit repository in local mode. Its claims are chained into a scope whose
# seed names the level above as parent (cockpit scope seed --parent): that is the only structure,
# and it is the context's, not the data's. The analysis knows nothing of it; the recorder finds it
# from where the analysis's inputs come from. Needs: cockpit, Rscript (with nlme), python3.
set -e
HERE=$(cd "$(dirname "$0")" && pwd)
DIR=${1:-$HOME/kton-program-example}
[ -e "$DIR" ] && { echo "$DIR exists: choose another directory or remove it first" >&2; exit 1; }
for t in cockpit Rscript python3; do command -v "$t" >/dev/null || { echo "needs $t" >&2; exit 1; }; done
say() { printf '\n== %s\n' "$*"; }

# repo DIR SIGNER: a cockpit repository in local mode that allows the describes/rationale claims
repo() {
  mkdir -p "$1" && (cd "$1" &&
    cockpit init >/dev/null &&
    cockpit keygen "$2" >/dev/null &&
    for t in "$HERE"/templates/*.json; do [ -e "templates/$(basename "$t")" ] || cp "$t" templates/; done &&
    cockpit config claims.allowedTemplates '["reproduces","working-on","describes","rationale"]' >/dev/null)
}
# seed DIR NAME [PARENT-ID]: a scope for the repository's claims; prints the scope id
seed() {
  (cd "$1" &&
    id=$(cockpit scope seed "$2" ${3:+--parent "$3"} | grep -o 'sha256:[0-9a-f]*' | head -1) &&
    cockpit config claims.scopes "{\"$2\":\"$id\"}" >/dev/null &&
    echo "$id")
}
# put DIR FILE SCOPE TEXT: publish a file of the repository and describe it, in its scope
put() {
  (cd "$1" &&
    foton=$(cockpit publish "{\"outputs\":[\"$2\"],\"cmd\":\"provided by the $(basename "$1") team\"}" | grep -o '"fotonId": *"[^"]*"' | grep -o 'sha256:[0-9a-f]*') &&
    cockpit say "{\"template\":\"describes\",\"subject\":\"$foton\",\"fields\":{\"value\":$(printf '%s' "$4" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')},\"scope\":\"$3\"}" >/dev/null &&
    printf '  %-34s %s\n' "$2" "$foton")
}
doc() { mkdir -p "$1/docs" && cp "$HERE/docs/$2" "$1/docs/$2"; put "$1" "docs/$2" "$3" "$4"; }

PROG=$DIR/program-respiratory
THP=$PROG/project-theophylline
CX=$PROG/project-compound-x
DATA=$DIR/.studydata
Rscript "$HERE/data.R" "$DATA"

say "programme Respiratory"
repo "$PROG" programme-office
P=$(seed "$PROG" program-respiratory)
doc "$PROG" program-strategy.md program-respiratory "Programme Respiratory: strategy for persistent asthma and COPD"
doc "$PROG" target-product-profile.md program-respiratory "Programme Respiratory: target product profile"

say "project Theophylline"
repo "$THP" project-theophylline
J=$(seed "$THP" project-theophylline "$P")
doc "$THP" thp-development-plan.md project-theophylline "Project Theophylline: development plan"
doc "$THP" thp-compound-profile.md project-theophylline "Project Theophylline: compound profile"

say "project Compound X"
repo "$CX" project-compound-x
X=$(seed "$CX" project-compound-x "$P")
doc "$CX" cx-development-plan.md project-compound-x "Project Compound X: development plan"

study() { # study DIR NAME PARENT DATAFILE DOCS... (doc:description pairs follow as "file|text")
  d=$1 name=$2 parent=$3 data=$4; shift 4
  say "study $name"
  repo "$d" "$name"
  s=$(seed "$d" "$name" "$parent")
  for pair in "$@"; do doc "$d" "${pair%%|*}" "$name" "${pair#*|}"; done
  mkdir -p "$d/data" && cp "$DATA/$data" "$d/data/$data"
  put "$d" "data/$data" "$name" "Study $(echo "${name#study-}" | tr a-z A-Z): the PK dataset, one row per sample"
}
study "$THP/study-thp-101" study-thp-101 "$J" thp101_pk.csv \
  "thp-101-protocol.md|Study THP-101: protocol synopsis" "thp-101-sap.md|Study THP-101: statistical analysis plan"
study "$THP/study-thp-102" study-thp-102 "$J" thp102_pk.csv "thp-102-protocol.md|Study THP-102: protocol synopsis"
study "$CX/study-cx-201" study-cx-201 "$X" cx201_pk.csv "cx-201-protocol.md|Study CX-201: protocol synopsis"
rm -r "$DATA"

say "analysis THP-101 population PK"
ANA=$DIR/analysis-thp-poppk
repo "$ANA" analyst
cp -r "$HERE/analysis/." "$ANA/"
mkdir -p "$ANA/data" "$ANA/derived" "$ANA/figures"
(cd "$ANA" && for src in "$PROG" "$THP" "$CX" "$THP/study-thp-101" "$THP/study-thp-102" "$CX/study-cx-201"; do
  n=$(basename "$src")
  cockpit federation add "$n" "$src" >/dev/null
  cockpit trust add "$n" "$src" --yes >/dev/null
  echo "  federated and trusted: $n"
done)

say "done"
echo "$DIR"
echo "next: open $ANA in Positron and follow its README.md"
