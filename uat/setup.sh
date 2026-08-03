#!/usr/bin/env bash
# uat/setup.sh — end-to-end UAT for the Claude-Science-Cockpit tutorial, doubling as guided
# training material: every phase — including the automated ones — prints what it is about to do
# and WHY before doing it, and waits for you to confirm. See uat/README.md for prerequisites.
#
# Three things stay genuinely manual, because they require live Claude reasoning or a UI with no
# documented API (see projectdocs/claude-science-cockpit/progress.md): registering the MCP
# connector, and the two participants actually calling cockpit_publish/cockpit_say. Everything
# else — including mirroring the federation locally and configuring the trust tier, which are pure
# mechanical git/config operations — is automated, explained, and confirmed like every other step.
#
# Follows the tutorial's federation-first sequence: participant 1 publishes and federates BEFORE
# participant 2 does anything, so participant 2 can mirror participant 1's real aggregated foton,
# hit the realistic byte-mismatch, correct it, and record a genuine ↻2 reproduction.
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

# step: explain an AUTOMATED phase (what + why) and require confirmation BEFORE running it.
step() {
  local title="$1" explanation="$2"
  echo
  echo "=== $title ==="
  echo
  echo "$explanation"
  echo
  read -r -p ">>> Press Enter to run this step (Ctrl-C to stop here). " _
}

# manual: explain a MANUAL phase (what + why), give exact instructions, and wait for confirmation
# AFTER you've done it yourself in claude-science/Claude Code.
manual() {
  local title="$1" explanation="$2" instructions="$3"
  echo
  echo "=== $title (MANUAL) ==="
  echo
  echo "$explanation"
  echo
  echo "$instructions"
  echo
  read -r -p ">>> Press Enter once done. " _
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

step "1/10 Create the repos" "\
Creating three repos from the official kton templates: one federation (the aggregator — mirrors
and independently re-verifies every participant's published records) and two participants (each a
standalone, git-based record of one identity's own reproducible work). All public — the viewer at
the end is served over GitHub Pages, which needs a public repo, and the federation template's own
demo convention keeps everything visible so this UAT is easy to inspect afterward.

Repos: $GH_OWNER/$FED_REPO, $GH_OWNER/$P1_REPO, $GH_OWNER/$P2_REPO"
create_repo_if_missing "$FED_REPO" "$TEMPLATE_FED"
create_repo_if_missing "$P1_REPO" "$TEMPLATE_PARTICIPANT"
create_repo_if_missing "$P2_REPO" "$TEMPLATE_PARTICIPANT"

echo "enabling Actions write permissions + Pages on $FED_REPO (mirror.yml needs write access to commit its own aggregation; Pages serves the viewer) — best-effort, verify in Settings if these warn..."
gh api --method PUT "repos/$GH_OWNER/$FED_REPO/actions/permissions/workflow" \
  -f default_workflow_permissions=write -F can_approve_pull_request_reviews=false \
  >/dev/null 2>&1 || echo "  warning: could not set workflow permissions via API — enable 'Read and write permissions' under Settings > Actions > General manually."
gh api --method POST "repos/$GH_OWNER/$FED_REPO/pages" \
  -f "build_type=legacy" -f "source[branch]=main" -f "source[path]=/" \
  >/dev/null 2>&1 || echo "  note: Pages API returned non-zero — this is also what happens if Pages is already enabled from a previous run of this script; verify under Settings > Pages (source: main branch, /) if the viewer doesn't load at the end."

step "2/10 Clone the repos locally" "\
The cockpit only ever operates against a local git working copy, never GitHub's API directly —
every cockpit_publish/cockpit_say call is really just \`git commit && git push\` plus a
plankton/nekton binary call, run in this local clone. This is also what the anti-wrong-folder
guard checks: on every call, the cockpit re-reads THIS clone's actual \`git remote\`, and refuses
if it doesn't match cockpit.config.json — the structural fix for a Claude session ever acting
against the wrong directory."
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

step "3/10 Configure the cockpit for both participants" "\
For each participant: build the cockpit binary, generate TWO signing identities (plankton signs
fotons, nekton signs claims — cockpit_say fails later if trust.tiers.self is missing either half),
publish only the .pub halves (registry/keys/ — the private .key halves stay in the gitignored
keys/ dir, never committed), then scaffold cockpit.config.json (repo.owner/name filled in from the
real git remote — this is the config the anti-wrong-folder guard checks against every call) and
run 'cockpit doctor' to confirm every path/key resolves correctly."
configure_participant "$P1_REPO" "session-1"
configure_participant "$P2_REPO" "session-1"

manual "4/10 Start claude-science and register the MCP connector for BOTH participants" "\
This is the one part of the whole cockpit design with no scriptable path: registering an MCP
'Local command' connector only exists through claude-science's own UI. Once registered, it's the
ONLY thing Claude has for touching git/plankton/nekton in these repos — that narrow surface (three
verbs: publish/say/ask, nothing else) is the entire point of this project." "\
For EACH participant repo below, open claude-science's UI (default instance — no --data-dir/--here;
see the tutorial's step 4 for why) → Connectors → Add connector → Local command, and use:

Environment variables field: enter each line separately (it is one KEY=value PER LINE — pasting
both onto a single line concatenates them into one broken value, a real failure mode hit before).

  --- $P1_REPO ---
  Name:      cockpit-${P1_REPO}
  Command:   $WORKDIR/$P1_REPO/bin/cockpit mcp
  Env vars:  COCKPIT_REPO_DIR=$WORKDIR/$P1_REPO
             GITHUB_TOKEN=$(gh auth token 2>/dev/null || echo '<run: gh auth token>')

  --- $P2_REPO ---
  Name:      cockpit-${P2_REPO}
  Command:   $WORKDIR/$P2_REPO/bin/cockpit mcp
  Env vars:  COCKPIT_REPO_DIR=$WORKDIR/$P2_REPO
             GITHUB_TOKEN=$(gh auth token 2>/dev/null || echo '<run: gh auth token>')

GITHUB_TOKEN is required for cockpit_publish's git push to authenticate — claude-science's sandbox
does not inherit the ambient gh/git credential setup a normal terminal has. Same token for both.

Create a claude-science project/session for each participant (or reuse one), and confirm each
connector's Tools list shows cockpit_ask/cockpit_publish/cockpit_say."

# --- register_participant / trigger_mirror_and_wait are used twice each below: once for
# participant 1 alone (so its foton is aggregated before participant 2 needs to see it), and once
# more for participant 2 (plus a final call after the reproduces claim is recorded). ---

# latest_run_id prints the most recent run id of $1, or nothing if there's never been one.
latest_run_id() {
  gh run list --repo "$GH_OWNER/$FED_REPO" --workflow "$1" --limit 1 --json databaseId --jq '.[0].databaseId // empty'
}

# wait_for_new_workflow_run polls until a run of $1 DIFFERENT from $3 (the id seen immediately
# before triggering it — possibly empty, if the workflow never ran before) appears, then watches
# it to completion. This exclusion matters: register_participant runs twice (once per
# participant) and the mirror retry re-dispatches the same workflow, so "the most recent run"
# alone can match an old, already-completed run instead of the new one just triggered.
wait_for_new_workflow_run() {
  local workflow="$1" description="$2" exclude_id="${3:-}"
  echo "waiting for $description to register..."
  local run_id=""
  for _ in $(seq 1 12); do
    run_id=$(latest_run_id "$workflow")
    if [ -n "$run_id" ] && [ "$run_id" != "$exclude_id" ]; then
      break
    fi
    run_id=""
    sleep 5
  done
  if [ -z "$run_id" ]; then
    echo "  warning: no new $description run appeared after 60s — check manually: gh run list --repo $GH_OWNER/$FED_REPO --workflow $workflow"
    return 1
  fi
  gh run watch "$run_id" --repo "$GH_OWNER/$FED_REPO" --exit-status || { echo "  warning: $description ($run_id) did not report success — check 'gh run view $run_id --repo $GH_OWNER/$FED_REPO --log'"; return 1; }
}

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

  # Baseline captured HERE, not before `gh issue create` above: creating the issue already carries
  # the `registration` label, which itself fires an issues.labeled event and triggers register.yml
  # — its job condition (`if: github.event.label.name == 'approved'`) doesn't match, so it just
  # completes as skipped. Capturing the baseline earlier let that skipped run get mistaken for the
  # real one triggered by the label below (confirmed live: the skipped run's job had zero steps).
  local before_register
  before_register=$(latest_run_id register.yml)
  gh issue edit "$issue_num" --repo "$GH_OWNER/$FED_REPO" --add-label approved

  # Adding the 'approved' label triggers the federation's OWN separate register.yml workflow,
  # which commits participants.json on main — asynchronously, not necessarily by the time this
  # function returns. Waiting for it here (rather than immediately dispatching mirror.yml) is
  # what prevents a real race hit in practice: mirror.yml's own commit gets rejected as
  # non-fast-forward if it tries to push while register.yml's commit is landing, since
  # mirror.yml's own `concurrency: group: mirror` only protects against overlapping mirror runs,
  # not against this separate workflow.
  #
  # Retry the WAIT only (never re-open the issue or re-add a label that's already there — GitHub
  # doesn't even fire a new labeled event for a no-op re-add) — and never let a stall here kill
  # the whole script via `set -e`, since register.yml is otherwise a one-shot gate with no other
  # way back in short of editing participants.json by hand.
  until wait_for_new_workflow_run register.yml "the register workflow" "$before_register"; do
    read -r -p "  Press Enter to check again, type 'skip' to continue anyway, or Ctrl-C to stop and investigate (gh run list --repo $GH_OWNER/$FED_REPO --workflow register.yml). " ans
    [ "$ans" = "skip" ] && break
    before_register=$(latest_run_id register.yml)
  done
}

trigger_mirror_and_wait() {
  local before
  before=$(latest_run_id mirror.yml)
  echo "triggering an immediate mirror (forced, ignoring per-participant intervals)..."
  gh workflow run mirror.yml --repo "$GH_OWNER/$FED_REPO" -f force=true
  # Unlimited interactive retry, same reasoning as register_participant above: never let a stall
  # here kill the whole script via `set -e` — re-dispatch and re-wait until it succeeds, the user
  # says skip, or they Ctrl-C to investigate.
  until wait_for_new_workflow_run mirror.yml "the mirror run" "$before"; do
    read -r -p "  Press Enter to retry (re-dispatches the mirror), type 'skip' to continue anyway, or Ctrl-C to stop and investigate (gh run list --repo $GH_OWNER/$FED_REPO --workflow mirror.yml). " ans
    [ "$ans" = "skip" ] && break
    before=$(latest_run_id mirror.yml)
    gh workflow run mirror.yml --repo "$GH_OWNER/$FED_REPO" -f force=true
  done
}

manual "5/10 Participant 1 publishes" "\
cockpit_publish wraps 'plankton author' plus git commit/push: it stages the input+output files,
builds commit-pinned raw.githubusercontent.com permalinks for each, signs the resulting foton
descriptor, and pushes both the files and the registry entry. A foton is a REPRODUCIBLE record —
verifiable by anyone who re-runs the same command against the same inputs and checks the hash." "\
In the $P1_REPO session, tell Claude exactly:

  \"Work only in $WORKDIR/$P1_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish.\"

Note the fotonId and output hash it returns — both are needed several steps from now."

step "6/10 Register participant 1 with the federation" "\
Opening a 'Register a participant' issue and adding the approved label is the ONLY way into the
federation — it's the admission gate mirror.yml checks for. The mirror run that follows doesn't
just copy participant 1's registry: it independently RE-VERIFIES every signature against the
participant's own published pubkey before aggregating anything (verify, never vouch — a corrupted
or forged record is dropped, not mirrored). This must complete before participant 2 can see it."
register_participant "$P1_REPO"
trigger_mirror_and_wait

manual "7/10 Participant 2 publishes independently" "\
Deliberately the same prose instructions as participant 1, but written into a fresh session with
no knowledge of participant 1's actual script. Two independent implementations of 'the same task'
routinely produce non-identical bytes (row order, float formatting, index handling) even when
correct — that's a realistic starting point, not a mistake, and step 9 corrects it properly rather
than assuming reproduction works on the first try." "\
In the $P2_REPO session — a DIFFERENT directory from participant 1's, this is not copy-paste —
tell Claude exactly:

  \"Work only in $WORKDIR/$P2_REPO — do not use any other directory. Write session-1/clean.py
  that drops rows with any missing value from data/penguins.csv and writes session-1/clean.csv
  (LF line endings, fixed float format). Run it, then call cockpit_publish.\"

Note this identity's own fotonId and output hash too."

step "8/10 Register participant 2 with the federation" "\
Same admission + independent re-verification as step 6, for participant 2. After this mirror run,
both participants' fotons are aggregated in $FED_REPO's mirror/ — the shared graph participant 2
needs for the actual reproduction check."
register_participant "$P2_REPO"
trigger_mirror_and_wait

step "9/10 Mirror the federation locally, and configure the trust tier" "\
Two separate, both-required mechanisms — neither is Claude-specific, both are pure git/config
operations, so this step runs automatically rather than asking Claude to do it via Bash:

(a) DATA visibility: 'plankton mirror'/'nekton mirror' overlay a peer registry purely off the
    filesystem, by hash — no server, no network call. mirror/ mixes fotons and claims in one
    directory, but each command only ingests the record kind it understands and silently skips
    the other (confirmed against the reference implementation's registry.Add(), which rejects
    non-matching payloads without erroring the whole mirror). The federation's mirror/ shards
    objects by a 2-hex-char prefix subdirectory for the viewer's benefit, which isn't the flat
    layout either binary actually scans for, so this step flattens into a scratch dir first.
(b) TRUST configuration: mirroring the data is NOT enough for cockpit_ask to show participant 1's
    record as verified. 'Verified, not declared' is a hard requirement of this design — a record
    is only ever trusted if ITS SIGNING KEY is explicitly configured in cockpit.config.json's
    trust tiers, never because the data merely showed up. Adding participant 1's pubkey to a
    'federation' tier here is what makes cockpit_ask's re-verification actually recognize it,
    rather than correctly excluding an unconfigured signer."
cd "$WORKDIR/$FED_REPO"
git pull -q
cd "$WORKDIR/$P2_REPO"
# plankton/nekton fall back to ./plankton-data / ./nekton-data when PLANKTON_DIR/NEKTON_DIR are
# unset — that bare default does NOT match this repo's actual committed layout (registry/plankton,
# registry/nekton, required by the federation template's mirror_once.py, which hardcodes those
# path prefixes when scanning a participant's tree). The cockpit itself always pins these two vars
# from cockpit.config.json before shelling out; do the same here since these two calls are plain
# git/config operations run directly, not through the cockpit.
PLANKTON_DIR="$WORKDIR/$P2_REPO/$(python3 -c 'import json; print(json.load(open("cockpit.config.json"))["paths"]["plankton_dir"])')"
NEKTON_DIR="$WORKDIR/$P2_REPO/$(python3 -c 'import json; print(json.load(open("cockpit.config.json"))["paths"]["nekton_dir"])')"
export PLANKTON_DIR NEKTON_DIR
# `plankton mirror`/`nekton mirror` expect a PEER's flat registry layout — objects/sha256/<hash>.json
# directly, no subdirectory — confirmed against the binaries' own usage string ("plankton mirror
# ../session-1/plankton-data", i.e. another participant's own registry dir). The federation's
# mirror/ instead SHARDS objects by a 2-hex-char prefix (objects/sha256/<xx>/<hash>.json) — that's
# build_mirror.py's own convention (for the GitHub Pages viewer), unrelated to the peer-mirror
# layout. Passing mirror/ straight to either binary is silently a no-op: 0 new, every time,
# regardless of PLANKTON_DIR/NEKTON_DIR — confirmed live (copying one sharded object into a flat
# scratch dir let it mirror in immediately: "1 new" vs "0 new" from the sharded source). Flatten
# into a scratch dir first so both binaries actually see what's there.
FLATTEN_DIR="$(mktemp -d)"
mkdir -p "$FLATTEN_DIR/objects/sha256"
find "$WORKDIR/$FED_REPO/mirror/objects/sha256" -mindepth 2 -maxdepth 2 -name '*.json' \
  -exec cp {} "$FLATTEN_DIR/objects/sha256/" \;
./bin/plankton mirror "$FLATTEN_DIR"
./bin/nekton mirror "$FLATTEN_DIR"
rm -rf "$FLATTEN_DIR"
python3 - "$WORKDIR/$P2_REPO/cockpit.config.json" "$WORKDIR/$P1_REPO/registry/keys/session-1.pub" <<'PY'
import json, sys
path, p1_pubkey = sys.argv[1], sys.argv[2]
with open(path) as f:
    cfg = json.load(f)
tier = cfg["trust"]["tiers"].setdefault("federation", [])
if p1_pubkey not in tier:
    tier.append(p1_pubkey)
with open(path, "w") as f:
    json.dump(cfg, f, indent=2)
PY
cd "$WORKDIR"
echo "  mirrored federation data into $P2_REPO's local registry, and added participant 1's pubkey to its federation trust tier."

manual "10/10 Check reproduction, correct if needed, and record the claim" "\
This is the one part that genuinely needs Claude's own reasoning: comparing bytes, deciding
whether to correct, and calling cockpit_say — cockpit_say also computes the L0 reproduction level
itself rather than accepting Claude's self-declared claim, so this step exercises that check for
real." "\
Still in the $P2_REPO session, ask Claude to call cockpit_ask (query=reproductions) against
participant 1's output hash (from step 5). This will most likely show ↻1, not ↻2 yet.

To correct it: have Claude fetch participant 1's EXACT clean.py from the commit-pinned permalink
in its foton's descriptor (already returned by participant 1's cockpit_publish; also visible via
cockpit_ask's producer/about output), reuse it verbatim, and call cockpit_publish again with it —
producing a new, now byte-identical foton. Re-run cockpit_ask (reproductions) to confirm ↻2.

Finally, have it call cockpit_say (template=reproduces, subject=participant 1's foton id,
subjectOutputHash=participant 1's output hash, reproducedOutput=session-1/clean.csv,
reproducedFotonId=participant 2's NEW foton id from the correction). This performs the actual git
push; nothing further to push manually."

step "Final mirror and the kton graph" "\
Claims aggregate into the federation exactly the same way fotons do (mirror_once.py scans
registry/nekton/objects/ alongside registry/plankton/objects/) — one more forced mirror run picks
up the new reproduces claim, after which both the viewer and a direct cockpit_ask show the
completed, independently-verified graph: two producers, one reproduces edge, ↻2."
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
