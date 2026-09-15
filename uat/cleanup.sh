#!/usr/bin/env bash
# uat/cleanup.sh — tear down everything uat/setup.sh created.
#
# Usage:
#   uat/cleanup.sh ../uat-runs/uat-20260723-171500
#
# Reads .uat-state.json from the given directory (the same WORKDIR setup.sh printed at the end),
# shows exactly what it is about to delete, and requires explicit confirmation before deleting
# anything. Deletes the 3 GitHub repos (irreversible) and the local clone directory.
#
# Does NOT attempt to remove the claude-science project/connector entries created during the
# manual steps — those live in claude-science's own data and were never scripted by setup.sh;
# remove them yourself via its UI if desired.

set -euo pipefail

WORKDIR="${1:?usage: uat/cleanup.sh <workdir printed by uat/setup.sh>}"
STATE_FILE="$WORKDIR/.uat-state.json"

if [ ! -f "$STATE_FILE" ]; then
  echo "no .uat-state.json found at $STATE_FILE — refusing to guess what to delete. Pass the exact WORKDIR uat/setup.sh printed." >&2
  exit 1
fi

OWNER=$(python3 -c "import json; print(json.load(open('$STATE_FILE'))['owner'])")
FED_REPO=$(python3 -c "import json; print(json.load(open('$STATE_FILE'))['federation_repo'])")
mapfile -t PARTICIPANT_REPOS < <(python3 -c "import json; print('\n'.join(json.load(open('$STATE_FILE'))['participant_repos']))")

echo "This will permanently delete:"
echo "  - GitHub repo: $OWNER/$FED_REPO"
for r in "${PARTICIPANT_REPOS[@]}"; do
  echo "  - GitHub repo: $OWNER/$r"
done
echo "  - local directory: $WORKDIR"
echo
read -r -p "Type 'yes' to confirm permanent deletion: " confirm
if [ "$confirm" != "yes" ]; then
  echo "aborted — nothing was deleted."
  exit 1
fi

for r in "$FED_REPO" "${PARTICIPANT_REPOS[@]}"; do
  echo "deleting $OWNER/$r ..."
  gh repo delete "$OWNER/$r" --yes || echo "  warning: could not delete $OWNER/$r (already gone, or missing delete_repo token scope?)"
done

echo "removing local directory $WORKDIR ..."
rm -rf "$WORKDIR"

echo "done."
