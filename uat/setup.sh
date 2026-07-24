#!/usr/bin/env bash
# uat/setup.sh — end-to-end UAT for the Claude-Science-Cockpit tutorial.
#
# Automates everything that is genuinely scriptable: creating a federation repo and two
# participant repos from the official templates, cloning them, building the cockpit binary into
# each participant, generating signing identities, configuring cockpit.config.json, registering
# both participants with the federation, triggering a mirror, and printing the resulting graph.
#
# Two things are NOT automated, by design (see projectdocs/claude-science-cockpit/progress.md,
# 2026-07-23): starting claude-science / creating a project / adding the MCP connector, and
# actually performing the tutorial's step 5 pipeline. Both are driven through claude-science's own
# UI and a live Claude conversation — there is no documented CLI/API for either, and a script
# cannot stand in for Claude's own reasoning. The script pauses at both points with exact,
# copy-pasteable instructions and waits for you to confirm before continuing.
#
# Usage:
#   uat/setup.sh                       # uses a fresh, timestamped prefix
#   UAT_PREFIX=myrun uat/setup.sh       # override the prefix
#   UAT_WORKDIR=/path uat/setup.sh      # override where repos are cloned (default below)
#
# Writes $WORKDIR/.uat-state.json, which uat/cleanup.sh reads to know exactly what to remove.

set -euo pipefail

COCKPIT_SRC="/mnt/c/dev/planktonClaudeScienceCockpit"
TEMPLATE_FED="gitmick/plankton-federation-template"
TEMPLATE_PARTICIPANT="gitmick/plankton-participant-template"

PREFIX="${UAT_PREFIX:-uat-$(date +%Y%m%d-%H%M%S)}"
WORKDIR="${UAT_WORKDIR:-/mnt/c/dev/planktonReproduce/$PREFIX}"

GH_OWNER="$(gh api user --jq .login)"
FED_REPO="${PREFIX}-federation"
P1_REPO="${PREFIX}-p1"
P2_REPO="${PREFIX}-p2"

banner() { echo; echo "=== $1 ==="; echo; }
pause() {
  echo
  read -r -p ">>> $1 — press Enter once done. " _
}

mkdir -p "$WORKDIR"
cd "$WORKDIR"

create_repo_if_missing() {
  local repo="$1" template="$2"
  if gh repo view "$GH_OWNER/$repo" >/dev/null 2>&1; then
    echo "  $GH_OWNER/$repo already exists, skipping creation"
  else
    gh repo create "$GH_OWNER/$repo" --template "$template" --public
  fi
}

clone_if_missing() {
  local repo="$1"
  if [ -d "$WORKDIR/$repo/.git" ]; then
    echo "  $WORKDIR/$repo already cloned, skipping"
  else
    git clone "https://github.com/$GH_OWNER/$repo.git"
  fi
}

banner "1/7 Creating repos: $GH_OWNER/$FED_REPO, $GH_OWNER/$P1_REPO, $GH_OWNER/$P2_REPO"
create_repo_if_missing "$FED_REPO" "$TEMPLATE_FED"
create_repo_if_missing "$P1_REPO" "$TEMPLATE_PARTICIPANT"
create_repo_if_missing "$P2_REPO" "$TEMPLATE_PARTICIPANT"

echo "enabling Actions write permissions + Pages on $FED_REPO (best-effort; verify in Settings if these warn)..."
gh api --method PUT "repos/$GH_OWNER/$FED_REPO/actions/permissions/workflow" \
  -f default_workflow_permissions=write -F can_approve_pull_request_reviews=false \
  >/dev/null 2>&1 || echo "  warning: could not set workflow permissions via API — enable 'Read and write permissions' under Settings > Actions > General manually."
gh api --method POST "repos/$GH_OWNER/$FED_REPO/pages" \
  -f "build_type=legacy" -f "source[branch]=main" -f "source[path]=/" \
  >/dev/null 2>&1 || echo "  note: Pages API returned non-zero — this is also what happens if Pages is already enabled from a previous run of this script; verify under Settings > Pages (source: main branch, /) if the viewer doesn't load in step 7."

banner "2/7 Cloning repos into $WORKDIR"
clone_if_missing "$FED_REPO"
clone_if_missing "$P1_REPO"
clone_if_missing "$P2_REPO"

# Save state immediately after repo creation — cleanup must be possible even if a later step fails.
cat > "$WORKDIR/.uat-state.json" <<EOF
{
  "owner": "$GH_OWNER",
  "workdir": "$WORKDIR",
  "federation_repo": "$FED_REPO",
  "participant_repos": ["$P1_REPO", "$P2_REPO"]
}
EOF
echo "state written to $WORKDIR/.uat-state.json"

configure_participant() {
  local repo="$1" session="$2"
  banner "3/7 Configuring cockpit in $repo (identity: $session)"
  local dir="$WORKDIR/$repo"
  cd "$dir"

  # go build resolves the module from cwd, not from a -C flag placed after other flags (-C must
  # be first on the command line) — build from the cockpit source dir and write -o back out.
  (cd "$COCKPIT_SRC" && GOOS=linux GOARCH=amd64 go build -o "$dir/bin/cockpit" ./cmd/cockpit)

  mkdir -p keys
  if [ -f "keys/$session.key" ]; then
    echo "  keys/$session.key already exists, skipping keygen (never regenerate — it would orphan any already-signed records)"
  else
    bin/plankton keygen "keys/$session"
    bin/nekton keygen "keys/${session}-claims"
    cp "keys/$session.pub" "keys/${session}-claims.pub" registry/keys/
    git add registry/keys
    git commit -m "signing identity" >/dev/null
    git push
  fi

  if [ -f cockpit.config.json ]; then
    echo "  cockpit.config.json already exists, skipping init (re-checking trust tiers below)"
  else
    bin/cockpit init
  fi
  python3 - "$dir/cockpit.config.json" "$session" <<'PY'
import json, sys
path, session = sys.argv[1], sys.argv[2]
with open(path) as f:
    cfg = json.load(f)
cfg["trust"]["tiers"]["self"] = [f"registry/keys/{session}.pub", f"registry/keys/{session}-claims.pub"]
with open(path, "w") as f:
    json.dump(cfg, f, indent=2)
PY
  bin/cockpit doctor

  cd "$WORKDIR"
}

configure_participant "$P1_REPO" "session-1"
configure_participant "$P2_REPO" "session-1"

banner "4/7 Manual step: start claude-science and register the MCP connector for BOTH participants"
cat <<EOF
For EACH participant repo below, open claude-science's UI (default instance — no --data-dir/--here;
see the tutorial's step 4) → Connectors → Add connector → Local command, and use:

  --- $P1_REPO ---
  Name:      cockpit-${P1_REPO}
  Command:   $WORKDIR/$P1_REPO/bin/cockpit mcp
  Env var:   COCKPIT_REPO_DIR=$WORKDIR/$P1_REPO

  --- $P2_REPO ---
  Name:      cockpit-${P2_REPO}
  Command:   $WORKDIR/$P2_REPO/bin/cockpit mcp
  Env var:   COCKPIT_REPO_DIR=$WORKDIR/$P2_REPO

Create a claude-science project/session for each participant (or reuse one), and confirm each
connector's Tools list shows cockpit_ask/cockpit_publish/cockpit_say before continuing.
EOF
pause "Both connectors registered and showing their tools"

banner "5/7 Manual step: perform the minimal example in BOTH participants"
cat <<EOF
In the $P1_REPO session, tell Claude exactly:

  "Work only in $WORKDIR/$P1_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish."

Note the fotonId and output hash it returns.

In the $P2_REPO session — NOTE THE DIRECTORY IS DIFFERENT, this is not copy-paste of the above —
tell Claude exactly:

  "Work only in $WORKDIR/$P2_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish."

Then, still in the $P2_REPO session, ask Claude to call cockpit_ask (query=reproductions) against
$P1_REPO's output hash (from above), and — if the bytes match — call cockpit_say with
template=reproduces to register a real cross-participant reproduction (↻2).
This step performs the actual git pushes to both repos; nothing further to push manually.
EOF
pause "Both participants have published, and p2 has reproduced p1 (or you're OK proceeding with ↻1 only)"

banner "6/7 Registering both participants with the federation"
register_participant() {
  local repo="$1"
  local body
  body=$(cat <<EOF
### Participant name

$repo

### Repository (owner/repo)

$GH_OWNER/$repo

### Mirror interval

Every 15 minutes
EOF
)
  local issue_url
  issue_url=$(gh issue create --repo "$GH_OWNER/$FED_REPO" \
    --title "register: $repo" --label registration --body "$body")
  echo "opened: $issue_url"
  local issue_num="${issue_url##*/}"
  gh label create approved --repo "$GH_OWNER/$FED_REPO" --color 0e8a16 \
    --description "admits a registered participant" >/dev/null 2>&1 || true
  gh issue edit "$issue_num" --repo "$GH_OWNER/$FED_REPO" --add-label approved
}
register_participant "$P1_REPO"
register_participant "$P2_REPO"

echo "triggering an immediate mirror (forced, ignoring per-participant intervals)..."
gh workflow run mirror.yml --repo "$GH_OWNER/$FED_REPO" -f force=true

echo "waiting for the mirror run to register..."
run_id=""
for _ in $(seq 1 12); do
  run_id=$(gh run list --repo "$GH_OWNER/$FED_REPO" --workflow mirror.yml --limit 1 --json databaseId --jq '.[0].databaseId // empty')
  [ -n "$run_id" ] && break
  sleep 5
done
if [ -z "$run_id" ]; then
  echo "  warning: no mirror run appeared after 60s — check manually: gh run list --repo $GH_OWNER/$FED_REPO --workflow mirror.yml"
else
  gh run watch "$run_id" --repo "$GH_OWNER/$FED_REPO" --exit-status || echo "  warning: mirror run did not report success — check 'gh run view $run_id --repo $GH_OWNER/$FED_REPO --log'"
fi

banner "7/7 The kton graph"
VIEWER_URL="https://$GH_OWNER.github.io/$FED_REPO/viewer/viewer.html?union=../mirror/union.json"
echo "Viewer (may take a minute or two to become reachable after Pages was just enabled):"
echo "  $VIEWER_URL"
echo
echo "Raw summary from mirror/union.json:"
cd "$WORKDIR/$FED_REPO"
git pull -q
python3 - <<'PY'
import json
try:
    with open("mirror/union.json") as f:
        data = json.load(f)
    fotons = sum(1 for r in data if "fotonId" in r)
    claims = sum(1 for r in data if "claimId" in r)
    print(f"  {len(data)} records total: {fotons} fotons, {claims} claims")
except FileNotFoundError:
    print("  mirror/union.json not found yet — the mirror run may still be in progress.")
PY

echo
echo "Done. State saved at $WORKDIR/.uat-state.json — pass that directory to uat/cleanup.sh when finished."
