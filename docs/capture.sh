#!/usr/bin/env bash
# Capture the transcript the homepage shows, by really running the commands.
#
# Nothing on that page is typed by hand: every command below is run against a participant repo
# built from nothing, and every answer is what came back. Re-run this when a verb's answer changes,
# then `docs/build.py`. `TestDocs_TheHomepageIsWhatBuildPyProduces` fails if the page and the
# builder disagree; nothing can check that the transcript is fresh, which is why it is captured
# rather than written.
set -uo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE/../examples/01-publish"
source ../lib/common.sh
set +e   # common.sh turns on -e, and a refusal is exactly what this is here to capture
OUT="$HERE/_data/transcript.json"
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
EXNAME=cockpit-tour
participant "$WORK/repo" >/dev/null 2>&1
cd "$WORK/repo"

echo "[" > "$OUT"
first=1
run() {
  local cmd="$1" out code
  out=$(eval "$cmd" 2>&1); code=$?
  python3 - "$OUT" "$cmd" "$out" "$code" "$first" <<'PY'
import json, sys
path, cmd, out, code, first = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4]), sys.argv[5]
with open(path, "a") as f:
    if first != "1":
        f.write(",\n")
    f.write(json.dumps({"cmd": cmd, "out": out, "code": code}, indent=1))
PY
  first=0
  echo "  ($code) $cmd"
}

printf 'ts,reading\n1,9.8\n2,10.4\n3,9.9\n' > data/sensor.csv
printf 'mean,n\n10.03,3\n' > work/summary.csv

run "cockpit doctor"
run "cockpit publish '{\"cmd\":\"mean data/sensor.csv > work/summary.csv\",\"inputs\":[\"data/sensor.csv\"],\"outputs\":[\"work/summary.csv\"]}'"
HASH=$(./bin/plankton hash work/summary.csv)
FOTON=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$HASH\"}" --field included 2>/dev/null \
        | python3 -c 'import json,sys; print(json.load(sys.stdin)[0])')
run "cockpit ask '{\"query\":\"producer\",\"ref\":\"$HASH\"}'"
run "cockpit say '{\"subject\":\"$FOTON\",\"template\":\"working-on\",\"fields\":{\"step\":\"calibration check\",\"by-session\":\"session-1\"}}'"
run "cockpit ask '{\"query\":\"about\",\"ref\":\"$FOTON\"}'"
run "cockpit publish '{\"cmd\":\"mean data/sensor.csv > work/summary.csv\",\"inputs\":[\"data/sensor.csv\"],\"output\":[\"work/summary.csv\"]}'"
python3 - cockpit.config.json <<'PY'
import json, sys
c = json.load(open(sys.argv[1])); c["repo"]["owner"] = "someone-else"
json.dump(c, open(sys.argv[1], "w"), indent=2)
PY
run "cockpit publish '{\"cmd\":\"mean data/sensor.csv > work/summary.csv\",\"inputs\":[\"data/sensor.csv\"],\"outputs\":[\"work/summary.csv\"]}'"

echo "]" >> "$OUT"
python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))), "steps captured into", sys.argv[1])' "$OUT"
