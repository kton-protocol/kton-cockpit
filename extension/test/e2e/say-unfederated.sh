#!/bin/sh
# WORKAROUND (20.11., reported to kton-cockpit): cockpit v0.5.5 `say` spins at 100 % CPU and never returns when the
# registry has a federation source (seen with test-execution's dryrun1.runs as a dir source; asks are fine). Until it
# is fixed, a say runs with the federation sources taken out of the checkout's config for its duration (one say at a
# time, under a lock; the config is restored on exit). Every other verb goes to the cockpit unchanged.
#   COCKPIT_REAL=<cockpit> (default: cockpit next to this script)
REAL="${COCKPIT_REAL:-$(dirname "$0")/cockpit}"
[ "${1:-}" = say ] && [ "${2:-}" != --json ] || exec "$REAL" "$@"
CFG=.kton/cockpit.config.json
[ -f "$CFG" ] || exec "$REAL" "$@"
NODE=$(command -v node || { [ -x /usr/lib/code-server/lib/node ] && echo /usr/lib/code-server/lib/node; } || true)
mkdir -p .kton/local
exec 9>.kton/local/say-unfederated.lock
flock 9
cp "$CFG" .kton/local/cockpit.config.json.federated
trap 'cp .kton/local/cockpit.config.json.federated "$CFG"' EXIT INT TERM
if [ -n "$NODE" ]; then
  "$NODE" -e 'const fs=require("fs");const c=JSON.parse(fs.readFileSync(process.argv[1]));if(c.federation)c.federation.sources=[];fs.writeFileSync(process.argv[1],JSON.stringify(c,null,2));' "$CFG"
else
  python3 -c 'import json,sys;p=sys.argv[1];c=json.load(open(p));c.get("federation",{})["sources"]=[];json.dump(c,open(p,"w"),indent=2)' "$CFG"
fi
"$REAL" "$@"
