#!/usr/bin/env bash
# uat/setup.sh — end-to-end UAT for the Claude-Science-Cockpit tutorial, doubling as guided
# training material: every phase — including the automated ones — prints what it is about to do
# and WHY before doing it, and waits for you to confirm. See uat/README.md for prerequisites.
#
# Three things stay genuinely manual, because they require live Claude reasoning or a UI with no
# documented API (see projectdocs/claude-science-cockpit/progress.md): registering the MCP
# connector, and the two participants actually calling cockpit_publish/cockpit_say. Everything
# else is automated, explained, and confirmed like every other step.
#
# Follows the tutorial's federation-first sequence: participant 1 publishes and federates BEFORE
# participant 2 does anything, so participant 2 can mirror participant 1's real aggregated foton,
# hit the realistic byte-mismatch, correct it, and record a genuine reproduction.
#
# NOTHING here is scaffolded from a public GitHub template. The repos are created empty and their
# contents come from uat/participant-skeleton/ in this repository, with the kernel binaries built
# from a kton checkout in the same run. The templates this used to clone vendor kton 0.1 binaries,
# which reject `nekton about/by --json` and `plankton reproductions --trust-keys` — all three of
# which this cockpit requires — so a run from them would have driven the cockpit against a kernel
# missing every flag it depends on, and nobody would have been told.
#
# Usage:
#   uat/setup.sh                       # uses a fresh, timestamped prefix
#   UAT_PREFIX=myrun uat/setup.sh       # override the prefix
#   UAT_WORKDIR=/path uat/setup.sh      # override where repos are created (default below)
#   KTON_SRC=/path uat/setup.sh         # override the kton checkout to build binaries from
#
# Writes $WORKDIR/.uat-state.json, which uat/cleanup.sh reads to know exactly what to remove.

set -euo pipefail

# Everything locates itself from this script, so no absolute path can go stale the way the previous
# hardcoded /mnt/c/dev/... defaults did — both of those directories no longer exist.
COCKPIT_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SKELETON="$COCKPIT_SRC/uat/participant-skeleton"
KTON_SRC="${KTON_SRC:-$(dirname "$COCKPIT_SRC")/kton}"

PREFIX="${UAT_PREFIX:-uat-$(date +%Y%m%d-%H%M%S)}"
WORKDIR="${UAT_WORKDIR:-$(dirname "$COCKPIT_SRC")/uat-runs/$PREFIX}"

GH_OWNER="$(gh api user --jq .login)"
FED_REPO="${PREFIX}-federation"
P1_REPO="${PREFIX}-p1"
P2_REPO="${PREFIX}-p2"

if [ ! -d "$KTON_SRC/reference/cmd/plankton" ]; then
  echo "error: no kton checkout at $KTON_SRC (set KTON_SRC=/path/to/kton)." >&2
  echo "The kernel binaries are built here, never vendored — see CLAUDE.md, 'The kernel binaries'." >&2
  exit 1
fi

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
  local repo="$1"
  if gh repo view "$GH_OWNER/$repo" >/dev/null 2>&1; then
    echo "  $GH_OWNER/$repo already exists, skipping creation"
  else
    gh repo create "$GH_OWNER/$repo" --public
  fi
}

step "1/10 Create three empty repos" "\
One federation (the aggregate every participant's records are mirrored into) and two participants
(each a standalone, git-based record of one identity's own reproducible work).

Empty, not from a template. Their contents come from uat/participant-skeleton/ in this repository
and from binaries built out of $KTON_SRC in this same run — so this software's test material is
something we ship and control, and the kernel under test is the one we mean rather than whatever a
public repo happened to vendor.

Repos: $GH_OWNER/$FED_REPO, $GH_OWNER/$P1_REPO, $GH_OWNER/$P2_REPO"
create_repo_if_missing "$FED_REPO"
create_repo_if_missing "$P1_REPO"
create_repo_if_missing "$P2_REPO"

cat > "$WORKDIR/.uat-state.json" <<EOF
{
  "owner": "$GH_OWNER",
  "workdir": "$WORKDIR",
  "federation_repo": "$FED_REPO",
  "participant_repos": ["$P1_REPO", "$P2_REPO"]
}
EOF
echo "state written to $WORKDIR/.uat-state.json"

# build_kernel puts freshly built plankton/nekton/kton into $1/bin.
build_kernel() {
  local bindir="$1"
  mkdir -p "$bindir"
  (cd "$KTON_SRC" && go build -o "$bindir/plankton" ./reference/cmd/plankton)
  (cd "$KTON_SRC" && go build -o "$bindir/nekton" ./nekton/reference/cmd/nekton)
  (cd "$KTON_SRC" && go build -o "$bindir/kton" ./kton/reference/cmd/kton)
}

# scaffold_repo lays out one repo from the skeleton and pushes it. $2 is "participant" or
# "federation": both hold a plankton and a nekton registry in the same flat layout, which is what
# `kton mirror` reads and write — the aggregate is not a special shape.
scaffold_repo() {
  local repo="$1" kind="$2"
  local dir="$WORKDIR/$repo"

  if [ -d "$dir/.git" ]; then
    echo "  $dir already scaffolded, skipping"
    return
  fi

  mkdir -p "$dir"
  cd "$dir"
  git init -q -b main
  git remote add origin "https://github.com/$GH_OWNER/$repo.git"

  mkdir -p registry/plankton registry/nekton registry/keys bin
  cp "$SKELETON/gitignore" .gitignore

  if [ "$kind" = "participant" ]; then
    mkdir -p templates keys data session-1
    cp "$SKELETON"/templates/*.json templates/
    cp "$SKELETON"/data/*.csv data/
  fi

  build_kernel "$dir/bin"
  "$dir/bin/plankton" version
  "$dir/bin/nekton" version

  # Empty directories do not exist in git, and a missing registry directory is not an empty
  # registry: `kton mirror` refuses a peer path that is not there ("nothing to mirror"), which is
  # the right behaviour and exactly why the placeholders matter.
  for d in registry/plankton registry/nekton registry/keys; do
    printf '' > "$d/.gitkeep"
  done

  cat > README.md <<EOF
# $repo

Created by \`uat/setup.sh\` from the Claude-Science-Cockpit. A $kind repo in a kton federation.
Scaffolded from \`uat/participant-skeleton/\`; the \`bin/\` binaries were built from a kton
checkout during the same run.
EOF

  git add -A
  git -c user.name="uat" -c user.email="uat@cockpit.local" commit -q -m "scaffold $kind repo"
  git push -q -u origin main
  cd "$WORKDIR"
}

step "2/10 Scaffold and push all three repos" "\
Each repo gets the same flat registry layout (registry/plankton, registry/nekton, registry/keys),
so a participant registry and the federation aggregate are the same shape — which is what lets
\`kton mirror\` read one directly into the other with no conversion.

The participants additionally get the claim templates, the exercise input (data/penguins.csv), and
a keys/ directory that .gitignore excludes. Binaries are built from $KTON_SRC now, and each repo's
plankton/nekton print their version below — read them. Which kernel wrote a store decides whether
that store reads as populated or as EMPTY WITH EXIT 0, so a mismatch here does not announce itself."
scaffold_repo "$FED_REPO" "federation"
scaffold_repo "$P1_REPO" "participant"
scaffold_repo "$P2_REPO" "participant"

configure_participant() {
  local repo="$1" session="$2"
  local dir="$WORKDIR/$repo"
  cd "$dir"

  # go build resolves the module from cwd, not from a -C flag placed after other flags (-C must
  # be first on the command line) — build from the cockpit source dir and write -o back out.
  (cd "$COCKPIT_SRC" && go build -o "$dir/bin/cockpit" ./cmd/cockpit)

  if [ -f "keys/$session.key" ]; then
    echo "  keys/$session.key already exists, skipping keygen (never regenerate — it would orphan any already-signed records)"
  else
    bin/plankton keygen "keys/$session"
    bin/nekton keygen "keys/${session}-claims"
    # The two halves go to different places on purpose: the private .key stays in the gitignored
    # keys/, the .pub is published under registry/keys/ because no peer can verify this
    # participant's signatures without it.
    cp "keys/$session.pub" "keys/${session}-claims.pub" registry/keys/
    git add registry/keys
    git -c user.name="uat" -c user.email="uat@cockpit.local" commit -q -m "signing identity"
    git push -q
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
publish only the .pub halves, then scaffold cockpit.config.json (repo.owner/name filled in from the
real git remote — this is the config the anti-wrong-folder guard checks against every call) and run
'cockpit doctor'.

Doctor also reports each binary's own version and refuses a kernel older than the cockpit needs.
That check exists because this exact setup used to hand the cockpit 0.1 binaries, whose only
symptom was a usage error mid-call that read like a syntax mistake."
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

# federate mirrors each named participant's registries into the federation repo and pushes the
# result. It replaces what a GitHub Actions workflow in the old federation template did.
#
# Read this before believing the aggregate means more than it does: `kton mirror` COPIES signed
# envelopes, it does not check them. The kernel says so itself, at mirrorNekton: "Claims keep their
# original signatures - mirroring is not confirming (SPEC §6)." The template's own mirror_once.py
# additionally re-verified every signature before aggregating; that property is NOT reproduced here
# and this script does not claim it. It is also not what the cockpit's trust rests on: cockpit_ask
# re-verifies every record against the reading repo's configured trust tiers before returning it,
# so trust is established where it is consumed, not where it is transported.
federate() {
  local dir="$WORKDIR/$FED_REPO"
  (cd "$dir" && git pull -q --rebase 2>/dev/null || true)

  local repo
  for repo in "$@"; do
    echo "  mirroring $repo ..."
    PLANKTON_DIR="$dir/registry/plankton" NEKTON_DIR="$dir/registry/nekton" \
      "$dir/bin/kton" mirror plankton "$WORKDIR/$repo/registry/plankton"
    PLANKTON_DIR="$dir/registry/plankton" NEKTON_DIR="$dir/registry/nekton" \
      "$dir/bin/kton" mirror nekton "$WORKDIR/$repo/registry/nekton"
    # Carry the participant's published pubkeys along: without them nobody reading the aggregate
    # can verify anything in it.
    cp "$WORKDIR/$repo"/registry/keys/*.pub "$dir/registry/keys/" 2>/dev/null || true
  done

  cd "$dir"
  git add -A
  if git diff --cached --quiet; then
    echo "  nothing new to aggregate"
  else
    git -c user.name="uat" -c user.email="uat@cockpit.local" commit -q -m "federate: $*"
    git push -q
    echo "  aggregated and pushed"
  fi
  cd "$WORKDIR"
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

step "6/10 Aggregate participant 1 into the federation" "\
Mirroring is a filesystem operation over a peer's registry directory, by hash — no server, no
network call, and no admission gate: this aggregate is ours, so a participant is included because
we point at it, not because a label was added to an issue.

This must complete before participant 2 can see participant 1's foton."
federate "$P1_REPO"

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

step "8/10 Aggregate participant 2, then mirror the federation into participant 2" "\
Two separate, both-required mechanisms — neither is Claude-specific, both are pure filesystem and
config operations, so this runs automatically rather than asking Claude to do it via Bash:

(a) DATA visibility: participant 2's own registry gets the federation's aggregate mirrored into it,
    which is how participant 1's foton becomes visible to a cockpit_ask run in participant 2's
    session at all.
(b) TRUST configuration: mirroring the data is NOT enough for cockpit_ask to show participant 1's
    record as verified — and this is the half that carries the guarantee, because mirroring itself
    checks nothing. A record is trusted only if ITS SIGNING KEY is configured in
    cockpit.config.json's trust tiers, never because the data merely showed up. Adding participant
    1's pubkey to a 'federation' tier is what makes cockpit_ask's own re-verification recognize it
    instead of correctly excluding an unconfigured signer."
federate "$P2_REPO"
cd "$WORKDIR/$P2_REPO"
PLANKTON_DIR="$WORKDIR/$P2_REPO/registry/plankton" NEKTON_DIR="$WORKDIR/$P2_REPO/registry/nekton" \
  ./bin/kton mirror plankton "$WORKDIR/$FED_REPO/registry/plankton"
PLANKTON_DIR="$WORKDIR/$P2_REPO/registry/plankton" NEKTON_DIR="$WORKDIR/$P2_REPO/registry/nekton" \
  ./bin/kton mirror nekton "$WORKDIR/$FED_REPO/registry/nekton"
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
echo "  mirrored the federation into $P2_REPO's registry, and added participant 1's pubkey to its federation trust tier."

manual "9/10 Check reproduction, correct if needed, and record the claim" "\
This is the one part that genuinely needs Claude's own reasoning: comparing bytes, deciding
whether to correct, and calling cockpit_say — cockpit_say also computes the reproduction level
itself rather than accepting Claude's self-declared claim, so this step exercises that check for
real." "\
Still in the $P2_REPO session, ask Claude to call cockpit_ask (query=reproductions) against
participant 1's output hash (from step 5). This will most likely show 1 verified producer, not 2.

To correct it: have Claude fetch participant 1's EXACT clean.py from the commit-pinned permalink
in its foton's descriptor (already returned by participant 1's cockpit_publish; also visible via
cockpit_ask's producer/about output), reuse it verbatim, and call cockpit_publish again with it —
producing a new, now byte-identical foton. Re-run cockpit_ask (reproductions) to confirm 2.

Finally, have it call cockpit_say (template=reproduces, subject=participant 1's foton id,
subjectOutputHash=participant 1's output hash, reproducedOutput=session-1/clean.csv,
reproducedFotonId=participant 2's NEW foton id from the correction). This performs the actual git
push; nothing further to push manually."

step "10/10 Final aggregation and summary" "\
Claims aggregate exactly the way fotons do — the same mirror over the same flat layout — so one
more pass picks up the new reproduces claim, after which the federation holds the completed graph:
two producers, one reproduces edge."
federate "$P1_REPO" "$P2_REPO"

echo
echo "Federation aggregate at $WORKDIR/$FED_REPO:"
FED_DIR="$WORKDIR/$FED_REPO"
echo "  fotons: $(find "$FED_DIR/registry/plankton" -name '*.json' -o -name '*.jsonl' 2>/dev/null | wc -l) file(s)"
echo "  claims: $(find "$FED_DIR/registry/nekton" -name '*.json' -o -name '*.jsonl' 2>/dev/null | wc -l) file(s)"
echo "  published pubkeys: $(find "$FED_DIR/registry/keys" -name '*.pub' 2>/dev/null | wc -l)"
echo
echo "To read the graph, ask a participant session — cockpit_ask re-verifies against that repo's"
echo "configured trust tiers, which is the only place trust is actually established. There is no"
echo "hosted viewer in this run: rendering the aggregate is a separate, still-open piece of work."
echo
echo "Done. State saved at $WORKDIR/.uat-state.json — pass that directory to uat/cleanup.sh when finished."
