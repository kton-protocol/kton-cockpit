#!/usr/bin/env bash
# uat/e2e.sh — two participants and a federation, driven end to end by the three verbs.
#
# What this covers that the examples do not: the examples each build ONE participant and demonstrate
# ONE property. This is the federation-shaped path — two identities with genuinely different keys,
# an aggregate that mirrors both, and a reproduction claim that only means anything because the
# party making it is not the party that made the record.
#
# The sequence is federation-first on purpose. Participant 2 must be able to see participant 1's
# real foton before it can reproduce it, so participant 1 publishes and is aggregated before
# participant 2 does anything at all.
#
# It used to pause four times for a human to drive a browser, because the three verbs existed only
# over MCP and nothing could type them. They are subcommands now, so the whole run is a script —
# including the one step that was said to need judgement, which turns out to need a query.
#
# Local by default: origins are real github.com URLs so the anti-wrong-folder guard runs unmodified,
# and only the PUSH url points at a local bare repo, so everything completes offline and in CI.
cd "$(dirname "$0")"
source ../examples/lib/common.sh

EXNAME=uat                                   # the demo seeds derive from this; see common.sh
WORK="${UAT_WORKDIR:-$PWD/.work}"
rm -rf "$WORK"; mkdir -p "$WORK"

echo "== three repos =="
# Both participants start from the same instrument log and nothing else. They are given the task in
# words, not the solution: what each one writes is its own.
# Each gets its own parent directory, so each gets its own bare origin: `participant` derives one
# beside the repo it builds, and three sharing a parent would be three repos pushing one ref.
R1="$WORK/p1/repo"; R2="$WORK/p2/repo"; FED="$WORK/fed/repo"
for r in p1 p2 fed; do
  participant "$WORK/$r/repo" "" "uat-$r"
  cp participant-skeleton/data/runs.csv "$WORK/$r/repo/data/runs.csv"
  git -C "$WORK/$r/repo" add -A
  git -C "$WORK/$r/repo" -c user.name=uat -c user.email=uat@cockpit.local commit -q -m "the instrument log"
  git -C "$WORK/$r/repo" push -q
done

# federate <repo dir>... — mirror each participant's registries into the aggregate.
#
# A filesystem operation over a peer's registry directory, by hash. No server, no network call, and
# no admission gate: this aggregate is ours, so a participant is in it because we pointed at it.
# The pubkeys ride along, because without them nobody reading the aggregate can verify what is in it.
federate() {
  local fed="$FED" repo
  for repo in "$@"; do
    PLANKTON_DIR="$fed/registry/plankton" NEKTON_DIR="$fed/registry/nekton" \
      "$fed/bin/kton" mirror plankton "$repo/registry/plankton" >/dev/null
    PLANKTON_DIR="$fed/registry/plankton" NEKTON_DIR="$fed/registry/nekton" \
      "$fed/bin/kton" mirror nekton "$repo/registry/nekton" >/dev/null
    # Named after the participant, not copied under its own filename. Every repo built from the
    # skeleton calls its key `session-1.pub`, so a plain copy puts one party's public key exactly
    # where another party's used to be — and the aggregate then verifies the second against the
    # first. Nothing errors; the records simply stop resolving.
    local who; who="$(basename "$(dirname "$repo")")"
    local k
    for k in "$repo"/registry/keys/*.pub; do
      [ -e "$k" ] && cp "$k" "$fed/registry/keys/$who-$(basename "$k")"
    done
  done
  git -C "$fed" add -A
  git -C "$fed" diff --cached --quiet || \
    git -C "$fed" -c user.name=uat -c user.email=uat@cockpit.local commit -q -m "federate $*"
}

# trust <repo dir> <label> <pubkey...> — put another party's keys into a tier of this repo's config.
# Nothing is trusted by being mirrored in; a record is included because a CONFIGURED key verifies it.
trust() {
  local dir="$1" tier="$2"; shift 2
  python3 - "$dir/cockpit.config.json" "$tier" "$@" <<'PY'
import json, sys, os, shutil
cfg_path, tier, keys = sys.argv[1], sys.argv[2], sys.argv[3:]
root = os.path.dirname(cfg_path)
cfg = json.load(open(cfg_path))
rel = []
for k in keys:
    # Prefixed with whose key it is. Both participants call theirs session-1.pub, so copying one in
    # under its own name overwrites this repo's own public half — and then the repo cannot verify
    # its own records, while reporting nothing wrong.
    who = os.path.basename(os.path.dirname(os.path.dirname(os.path.dirname(k))))
    dest = os.path.join(root, "registry", "keys", who + "-" + os.path.basename(k))
    shutil.copyfile(k, dest)
    rel.append(os.path.relpath(dest, root))
cfg["trust"]["tiers"][tier] = sorted(set(cfg["trust"]["tiers"].get(tier, []) + rel))
json.dump(cfg, open(cfg_path, "w"), indent=2)
PY
}

# ---------------------------------------------------------------------------------------------
echo ""
echo "== participant 1 publishes =="
cd "$R1"; export COCKPIT_REPO_DIR="$PWD"
mkdir -p session-1
cat > session-1/clean.py <<'PY'
# Drop every run with a missing value, and report what is left per instrument.
import csv, sys
rows = list(csv.DictReader(open("data/runs.csv", newline="")))
kept = [r for r in rows if all(v.strip() for v in r.values())]
by = {}
for r in kept:
    by.setdefault(r["instrument"], []).append(float(r["concentration_mg_l"]))
out = csv.writer(open("session-1/clean.csv", "w", newline="\n"))
out.writerow(["instrument", "n", "mean_mg_l"])
for inst in sorted(by):
    v = by[inst]
    out.writerow([inst, len(v), "%.4f" % (sum(v) / len(v))])
PY
python3 session-1/clean.py
P1=$(cockpit publish '{"cmd":"python3 session-1/clean.py",
                       "inputs":["data/runs.csv","session-1/clean.py"],
                       "outputs":["session-1/clean.csv"]}')
P1_FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$P1")
P1_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["session-1/clean.csv"])' <<<"$P1")
require "participant 1 has a foton"            test -n "$P1_FOTON"
require "and it committed and pushed"          grep -q '"pushed": true' <<<"$P1"
echo "  $P1_FOTON"

echo ""
echo "== aggregated into the federation, before participant 2 exists to it =="
federate "$R1"
FED_COUNT=$(PLANKTON_DIR="$FED/registry/plankton" "$FED/bin/plankton" records --json \
            | python3 -c 'import json,sys; print(len(json.load(sys.stdin)))')
require "the aggregate holds participant 1's record" test "$FED_COUNT" -ge 1

# ---------------------------------------------------------------------------------------------
echo ""
echo "== participant 2 does the same task, independently =="
# The same task, written by someone who has not seen the other script. Both are correct. They do
# not agree byte for byte — this one sorts by sample count and prints two decimals — and that is a
# realistic starting point rather than a mistake.
cd "$R2"; export COCKPIT_REPO_DIR="$PWD"
mkdir -p session-1
cat > session-1/clean.py <<'PY'
# Same task: drop incomplete runs, summarise per instrument.
import csv
rows = [r for r in csv.DictReader(open("data/runs.csv", newline=""))
        if all(f.strip() for f in r.values())]
groups = {}
for r in rows:
    groups.setdefault(r["instrument"], []).append(float(r["concentration_mg_l"]))
w = csv.writer(open("session-1/clean.csv", "w", newline="\n"))
w.writerow(["instrument", "n", "mean_mg_l"])
for inst, vals in sorted(groups.items(), key=lambda kv: (-len(kv[1]), kv[0])):
    w.writerow([inst, len(vals), round(sum(vals) / len(vals), 2)])
PY
python3 session-1/clean.py
P2=$(cockpit publish '{"cmd":"python3 session-1/clean.py",
                       "inputs":["data/runs.csv","session-1/clean.py"],
                       "outputs":["session-1/clean.csv"]}')
P2_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["session-1/clean.csv"])' <<<"$P2")
require "participant 2's bytes differ from participant 1's" test "$P1_HASH" != "$P2_HASH"
echo "  p1 $P1_HASH"
echo "  p2 $P2_HASH"

echo ""
echo "== the federation mirrored back into participant 2 =="
federate "$R2"
PLANKTON_DIR="$PWD/registry/plankton" NEKTON_DIR="$PWD/registry/nekton" \
  ./bin/kton mirror plankton "$FED/registry/plankton" >/dev/null
trust "$PWD" federation "$R1/registry/keys/session-1.pub" "$R1/registry/keys/session-1-claims.pub"
git add -A && git -c user.name=uat -c user.email=uat@cockpit.local commit -q -m "mirror the federation"

REPRO=$(cockpit ask "{\"query\":\"reproductions\",\"ref\":\"$P1_HASH\",\"filter\":{\"trustTier\":\"federation\"}}")
N=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["verifiedSigners"])' <<<"$REPRO")
require "participant 1's result stands on one signature so far" test "$N" = "1"

# ---------------------------------------------------------------------------------------------
echo ""
echo "== the correction, which is a lookup and not a judgement call =="
# Participant 2 does not guess why the bytes differ. The foton names its own inputs by hash and
# carries a commit-pinned locator for each, so the exact script that produced participant 1's
# result is fetchable. Here the locator's host is github.com and these repos are local, so the
# bytes come from the commit the locator names in participant 1's own origin — the same operation.
LOCATOR=$(PLANKTON_DIR="$FED/registry/plankton" "$FED/bin/plankton" show "$P1_FOTON" --json \
  | python3 -c 'import json,sys
d=json.load(sys.stdin)
print(next(i["uri"][0] for i in d["inputs"] if i["path"].endswith("clean.py")))')
SHA=$(sed -E 's#.*/([0-9a-f]{40})/.*#\1#' <<<"$LOCATOR")
require "participant 1's script is named by a commit-pinned locator" test "${#SHA}" = "40"
git -C "$WORK/p1/origin.git" show "$SHA:session-1/clean.py" > session-1/theirs.py
require "and those bytes are what participant 1 recorded" \
  test "$(./bin/plankton hash session-1/theirs.py)" = "$(./bin/plankton hash "$R1/session-1/clean.py")"

python3 session-1/theirs.py
RERUN=$(cockpit publish '{"cmd":"python3 session-1/theirs.py",
                          "inputs":["data/runs.csv","session-1/theirs.py"],
                          "outputs":["session-1/clean.csv"]}')
R_FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$RERUN")
R_HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["session-1/clean.csv"])' <<<"$RERUN")
require "re-running participant 1's script reproduces their bytes exactly" test "$R_HASH" = "$P1_HASH"

echo ""
echo "== the claim, whose level participant 2 does not get to state =="
SAY=$(cockpit say "{\"subject\":\"$P1_FOTON\",\"template\":\"reproduces\",
                    \"subjectOutputHash\":\"$P1_HASH\",
                    \"reproducedOutput\":\"session-1/clean.csv\",
                    \"reproducedFotonId\":\"$R_FOTON\"}")
require "the cockpit measured L0 itself" grep -q '"L0"' <<<"$SAY"
echo "  $(python3 -c 'import json,sys; print(json.load(sys.stdin).get("confirmation",""))' <<<"$SAY" | head -c 160)"

# A reproduction claim over bytes that do NOT match must be refused, or the claim above proves
# nothing: a check that cannot fail is not a check.
expect_fail "a reproduction claim over participant 2's own, different bytes" \
  cockpit say "{\"subject\":\"$P1_FOTON\",\"template\":\"reproduces\",
                \"subjectOutputHash\":\"$P2_HASH\",
                \"reproducedOutput\":\"session-1/clean.csv\",
                \"reproducedFotonId\":\"$R_FOTON\"}"

echo ""
echo "== and the count, by verified signature =="
federate "$R2"
cd "$R1"; export COCKPIT_REPO_DIR="$PWD"
PLANKTON_DIR="$PWD/registry/plankton" NEKTON_DIR="$PWD/registry/nekton" \
  ./bin/kton mirror plankton "$FED/registry/plankton" >/dev/null
trust "$PWD" federation "$R2/registry/keys/session-1.pub" "$R2/registry/keys/session-1-claims.pub"

FINAL=$(cockpit ask "{\"query\":\"reproductions\",\"ref\":\"$P1_HASH\"}")
N2=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["verifiedSigners"])' <<<"$FINAL")
require "two distinct parties now stand behind the same bytes" test "$N2" = "2"
# The number is what it is because two DIFFERENT keys verified, not because two records said so.
require "counted by verified signature, in a configured tier" grep -q '"federation"' <<<"$FINAL"

echo ""
echo "  Participant 1 recorded a result. Participant 2 wrote its own solution, got different bytes,"
echo "  fetched the exact script participant 1's record names, re-ran it, and reproduced them. The"
echo "  ↻2 is two keys, and neither party had to be believed for it."
