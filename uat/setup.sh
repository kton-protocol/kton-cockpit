#!/usr/bin/env bash
# uat/setup.sh — end-to-end UAT for the Claude-Science-Cockpit tutorial.
#
# Automates everything that is genuinely scriptable: creating a federation repo and two
# participant repos from the official templates, cloning them, building the cockpit binary into
# each participant, generating signing identities, configuring cockpit.config.json, registering
# each participant with the federation as it publishes, triggering mirrors, and printing the
# resulting graph. See uat/README.md for prerequisites before running this.
#
# Three things are NOT automated, by design (see projectdocs/claude-science-cockpit/progress.md):
# starting claude-science / creating a project / adding the MCP connector; actually performing the
# publish/correct/say steps in a live Claude conversation; and the local-mirror + trust-tier setup
# that happens inside that same conversation. All three are driven through claude-science's own UI
# and Claude's own reasoning — there is no documented CLI/API for connector registration, and a
# script cannot stand in for Claude deciding what to do. The script pauses at each such point with
# exact, copy-pasteable instructions and waits for you to confirm before continuing.
#
# This follows the tutorial's federation-first sequence (see
# projectdocs/claude-science-cockpit/phases/end-to-end-tutorial/tutorial.md): participant 1
# publishes and federates BEFORE participant 2 does anything, so participant 2 can actually mirror
# participant 1's real, aggregated foton back down and attempt a genuine reproduction — including
# the realistic correction step (participant 2 first tries independently, discovers a byte
# mismatch, then reuses participant 1's exact script) rather than assuming reproduction "just
# works" on the first try.
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

banner "1/10 Creating repos: $GH_OWNER/$FED_REPO, $GH_OWNER/$P1_REPO, $GH_OWNER/$P2_REPO"
create_repo_if_missing "$FED_REPO" "$TEMPLATE_FED"
create_repo_if_missing "$P1_REPO" "$TEMPLATE_PARTICIPANT"
create_repo_if_missing "$P2_REPO" "$TEMPLATE_PARTICIPANT"

echo "enabling Actions write permissions + Pages on $FED_REPO (best-effort; verify in Settings if these warn)..."
gh api --method PUT "repos/$GH_OWNER/$FED_REPO/actions/permissions/workflow" \
  -f default_workflow_permissions=write -F can_approve_pull_request_reviews=false \
  >/dev/null 2>&1 || echo "  warning: could not set workflow permissions via API — enable 'Read and write permissions' under Settings > Actions > General manually."
gh api --method POST "repos/$GH_OWNER/$FED_REPO/pages" \
  -f "build_type=legacy" -f "source[branch]=main" -f "source[path]=/" \
  >/dev/null 2>&1 || echo "  note: Pages API returned non-zero — this is also what happens if Pages is already enabled from a previous run of this script; verify under Settings > Pages (source: main branch, /) if the viewer doesn't load at the end."

banner "2/10 Cloning repos into $WORKDIR"
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
  banner "3/10 Configuring cockpit in $repo (identity: $session)"
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

banner "4/10 Manual step: start claude-science and register the MCP connector for BOTH participants"
GH_TOKEN_FOR_CONNECTOR="$(gh auth token 2>/dev/null || true)"
cat <<EOF
For EACH participant repo below, open claude-science's UI (default instance — no --data-dir/--here;
see the tutorial's step 4) → Connectors → Add connector → Local command, and use:

Environment variables field: enter each line separately (it is one KEY=value PER LINE — pasting
both onto a single line concatenates them into one broken value, a real failure mode hit before).

  --- $P1_REPO ---
  Name:      cockpit-${P1_REPO}
  Command:   $WORKDIR/$P1_REPO/bin/cockpit mcp
  Env vars:  COCKPIT_REPO_DIR=$WORKDIR/$P1_REPO
             GITHUB_TOKEN=${GH_TOKEN_FOR_CONNECTOR:-<run: gh auth token>}

  --- $P2_REPO ---
  Name:      cockpit-${P2_REPO}
  Command:   $WORKDIR/$P2_REPO/bin/cockpit mcp
  Env vars:  COCKPIT_REPO_DIR=$WORKDIR/$P2_REPO
             GITHUB_TOKEN=${GH_TOKEN_FOR_CONNECTOR:-<run: gh auth token>}

GITHUB_TOKEN is required for cockpit_publish's git push to authenticate — claude-science's sandbox
does not inherit the ambient gh/git credential setup a normal terminal has. Same token for both.

Create a claude-science project/session for each participant (or reuse one), and confirm each
connector's Tools list shows cockpit_ask/cockpit_publish/cockpit_say before continuing.
EOF
pause "Both connectors registered and showing their tools"

# --- register_participant / trigger_mirror_and_wait are used twice each below: once for
# participant 1 alone (so its foton is aggregated before participant 2 needs to see it), and once
# more for participant 2 (plus a final call after the reproduces claim is recorded). ---

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
  # gh issue create --label requires the label to already exist (unlike GitHub's own issue-form
  # UI, which auto-creates a template's declared labels) — create both labels first if missing.
  gh label create registration --repo "$GH_OWNER/$FED_REPO" --color fbca04 \
    --description "a participant registration request" >/dev/null 2>&1 || true
  gh label create approved --repo "$GH_OWNER/$FED_REPO" --color 0e8a16 \
    --description "admits a registered participant" >/dev/null 2>&1 || true

  local issue_url
  issue_url=$(gh issue create --repo "$GH_OWNER/$FED_REPO" \
    --title "register: $repo" --label registration --body "$body")
  echo "opened: $issue_url"
  local issue_num="${issue_url##*/}"
  gh issue edit "$issue_num" --repo "$GH_OWNER/$FED_REPO" --add-label approved
}

trigger_mirror_and_wait() {
  echo "triggering an immediate mirror (forced, ignoring per-participant intervals)..."
  gh workflow run mirror.yml --repo "$GH_OWNER/$FED_REPO" -f force=true

  echo "waiting for the mirror run to register..."
  local run_id=""
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
}

banner "5/10 Manual step: participant 1 publishes"
cat <<EOF
In the $P1_REPO session, tell Claude exactly:

  "Work only in $WORKDIR/$P1_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish."

Note the fotonId and output hash it returns — both are needed several steps from now.
EOF
pause "Participant 1 has published"

banner "6/10 Registering participant 1 with the federation"
register_participant "$P1_REPO"
trigger_mirror_and_wait

banner "7/10 Manual step: participant 2 publishes independently"
cat <<EOF
In the $P2_REPO session — a DIFFERENT directory from participant 1's, this is not copy-paste —
tell Claude exactly:

  "Work only in $WORKDIR/$P2_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish."

Deliberately do NOT give Claude participant 1's script here — writing it independently, from the
same prose instructions, is what surfaces the byte-mismatch correction in the next manual step.
Note this identity's own fotonId and output hash too.
EOF
pause "Participant 2 has published (independently)"

banner "8/10 Registering participant 2 with the federation"
register_participant "$P2_REPO"
trigger_mirror_and_wait

banner "9/10 Manual step: mirror the federation locally, check reproduction, correct, and claim"
cat <<EOF
Still in the $P2_REPO session, tell Claude to run (it already has git/plankton/nekton available
via Bash for this — cockpit's own tools only cover the publish/say/ask verbs, not local mirroring):

  git clone https://github.com/$GH_OWNER/$FED_REPO /tmp/federation-clone
  bin/plankton mirror /tmp/federation-clone/mirror
  bin/nekton   mirror /tmp/federation-clone/mirror

Then have it edit $WORKDIR/$P2_REPO/cockpit.config.json, adding participant 1's pubkey to a
"federation" trust tier (mirroring the data alone is not enough — cockpit_ask's verification step
separately needs the signer's pubkey configured, or it correctly excludes the record):

  "trust": { "tiers": { "self": [...], "federation": ["<path to participant 1's registry/keys/session-1.pub, from the clone above>"] } }

Then ask it to call cockpit_ask (query=reproductions) against participant 1's output hash. This
will most likely show ↻1, not ↻2 — participant 2's independently-written script probably produced
different bytes even from the same instructions. That is expected, not a failure.

To correct it: have Claude fetch participant 1's EXACT clean.py from the commit-pinned permalink
in its foton's descriptor (already returned by participant 1's cockpit_publish; also visible via
cockpit_ask's producer/about output), reuse it verbatim, and call cockpit_publish again with it —
producing a new, now byte-identical foton. Re-run cockpit_ask (reproductions) to confirm ↻2.

Finally, have it call cockpit_say (template=reproduces, subject=participant 1's foton id,
subjectOutputHash=participant 1's output hash, reproducedOutput=session-1/clean.csv,
reproducedFotonId=participant 2's NEW foton id from the correction). This performs the actual git
push; nothing further to push manually.
EOF
pause "Participant 2 has mirrored, corrected, confirmed ↻2, and recorded the reproduces claim"

banner "10/10 Final mirror and the kton graph"
trigger_mirror_and_wait

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
