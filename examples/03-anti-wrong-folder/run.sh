#!/usr/bin/env bash
# 03 - the anti-wrong-folder guard: the reason this project exists.
#
# A live demo run once had six separate registries under one parent directory, and a session
# cooperated against the wrong one. Nothing failed. The records were signed, valid, and in the wrong
# project. The guard makes that a refusal instead.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"

printf 'x\n' > work/out.csv
cockpit publish '{"cmd":"echo x > work/out.csv","inputs":[],"outputs":["work/out.csv"]}' >/dev/null
echo "  a record exists in the right repo"

echo ""
echo "== 1. a config bound to a different repo =="
python3 - cockpit.config.json <<'PY'
import json,sys
p=sys.argv[1]; c=json.load(open(p)); c["repo"]["owner"]="someone-else"; json.dump(c,open(p,"w"),indent=2)
PY
expect_fail "acting on this repo with a config that names someone-else/…" \
  cockpit ask '{"query":"producer","ref":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}'
git checkout -- cockpit.config.json

echo ""
echo "== 2. outside any git repository =="
OUTSIDE="$WORK/not-a-repo"; mkdir -p "$OUTSIDE"
expect_fail "acting in a directory that is no repository" \
  env COCKPIT_REPO_DIR="$OUTSIDE" cockpit ask '{"query":"producer","ref":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}'

echo ""
echo "== 3. a config below the repository root =="
mkdir -p nested && cp cockpit.config.json nested/
expect_fail "a config sitting somewhere other than the root it binds" \
  env COCKPIT_REPO_DIR="$PWD/nested" cockpit ask '{"query":"producer","ref":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}'
rm -rf nested

echo ""
echo "== and it still works where it should =="
# The control every guard demonstration needs: a guard that refuses everything is not a guard.
OUT=$(cockpit ask '{"query":"producer","ref":"sha256:0000000000000000000000000000000000000000000000000000000000000000"}')
require "the same call succeeds in the repo the config is bound to" test -n "$OUT"

echo ""
echo "  Note what is NOT checked: a copy of the right repo. It carries the same config and the same"
echo "  remote, so both sources still agree. In local mode (repo.mode=local) the anchor is the"
echo "  config's own declared path, which catches exactly that and misses what this catches."
