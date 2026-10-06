#!/usr/bin/env bash
# uat/cli.sh — two participants, GitHub and Docker, and nothing but the cockpit's own command line.
#
# e2e.sh shows the protocol; this shows the tool. Where e2e.sh reaches for Python, the kernel
# binaries and `git show` — writing configs, copying keys under safe names, following a locator —
# this types `cockpit`. Each step is a user story from setting up to a claimed reproduction; a step
# that still needs hand work says so and is the next thing to build.
#
# Needs git, go and docker (the image is pulled once). No GitHub account: origins are real
# github.com URLs so the binding guard runs unmodified, pushes go to local bare repos, and fetches of
# github.com are redirected to them by url.insteadOf.
set -euo pipefail
cd "$(dirname "$0")/.."
REPO="$PWD"
WORK="$REPO/uat/.work/cli"
IMAGE="${COCKPIT_UAT_IMAGE:-python:3.12-slim}"
rm -rf "$WORK"; mkdir -p "$WORK"

require() { local what="$1"; shift; if ! "$@"; then echo "  FAIL: $what" >&2; exit 1; fi; echo "  ok: $what"; }
refused() { local what="$1"; shift; if "$@" >/dev/null 2>&1; then echo "  FAIL: $what was accepted" >&2; exit 1; fi; echo "  refused, as it must be: $what"; }
step()    { printf '\n== %s ==\n' "$*"; }
show()    { printf '$ %s\n' "$*"; "$@"; }

go build -o "$WORK/bin/cockpit" ./cmd/cockpit
export PATH="$WORK/bin:$PATH"

# participant <name> — a repo whose origin is github.com/kton-uat/<name>; its pushes go to a local
# bare repo.
participant() {
  local name="$1"
  git init -q --bare -b main "$WORK/$name.git"
  git init -q -b main "$WORK/$name"
  git -C "$WORK/$name" remote add origin "https://github.com/kton-uat/$name.git"
  git -C "$WORK/$name" config remote.origin.pushurl "$WORK/$name.git"
  git -C "$WORK/$name" config user.name "$name"
  git -C "$WORK/$name" config user.email "$name@uat.local"
}

# from_github <cmd...> — run a command whose fetches of github.com/kton-uat/alice reach her local
# bare repo. Only around fetches: insteadOf also rewrites what `git remote get-url` reports, which
# would bind a repository to the wrong place.
from_github() {
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.$WORK/alice.git.insteadOf" \
    GIT_CONFIG_VALUE_0="https://github.com/kton-uat/alice.git" "$@"
}

# ------------------------------------------------------------------------------------------------
step "A — alice sets up: init, keygen, pin, doctor"
participant alice
cd "$WORK/alice"
before=$(git status --porcelain | wc -l)
show cockpit init
show cockpit keygen alice
show cockpit pin "$IMAGE"
require "doctor says ready"                   bash -c 'cockpit doctor | grep -q "^ready:"'
require "and asks for the commit it needs"     bash -c 'cockpit doctor | grep -q "not committed yet"'
git add -A && git commit -qm "cockpit init" && git push -q origin main
require "nothing is left to do"                bash -c '! cockpit doctor | grep -qE "^  . [0-9]+\. (not committed|no signing)"'
require "no private key is committed"          test -z "$(git ls-files keys)"
require "the public keys are"                  test -n "$(git ls-files registry/keys)"

step "B — alice works in a run folder, in the pinned image"
mkdir -p data
printf 'instrument,value\nA,1.5\nB,2.0\nA,2.5\n' > data/runs.csv
show cockpit run new means --in data/runs.csv
cat > runs/means/run.py <<'PY'
import csv
by = {}
for r in csv.DictReader(open("inputs/runs.csv")):
    by.setdefault(r["instrument"], []).append(float(r["value"]))
with open("out/means.csv", "w") as f:
    f.write("instrument,mean\n")
    for k in sorted(by):
        f.write("%s,%.4f\n" % (k, sum(by[k]) / len(by[k])))
PY
cockpit run means | tee "$WORK/alice-run.txt"
cd "$WORK/alice"   # on a WSL /mnt drive a container bind mount leaves the shell's cwd stale
ALICE_FOTON=$(awk '/^foton /{print $2}' "$WORK/alice-run.txt")
require "the run is a record"                  test -n "$ALICE_FOTON"
require "made in the pinned image"             grep -q "^ran in  oci://.*@sha256:" "$WORK/alice-run.txt"
require "and pushed"                           test "$(git rev-parse HEAD)" = "$(git -C "$WORK/alice.git" rev-parse main)"

# ------------------------------------------------------------------------------------------------
step "C — bob sets up the same way"
participant bob
cd "$WORK/bob"
cockpit init >/dev/null && cockpit keygen bob >/dev/null && cockpit pin "$IMAGE" >/dev/null
git add -A && git commit -qm "cockpit init" && git push -q origin main

# Hand work, C2 not built yet: bob reads alice's registry from a clone of her repository, named in
# federation.sources. `cockpit federation add` is the next step on the list.
from_github git clone -q "https://github.com/kton-uat/alice.git" "$WORK/alice-mirror"
sed -i 's#"federation": {}#"federation": {"sources": [{"name": "alice", "kind": "dir", "path": "'"$WORK/alice-mirror"'"}]}#' cockpit.config.json
require "(hand work) alice's registry is a source" grep -q '"kind": "dir"' cockpit.config.json

refused "fetching a record nobody bob trusts signed" from_github cockpit run new early --from "$ALICE_FOTON"
require "and the refusal leaves no folder"     test ! -e runs/early

step "C1 — bob takes alice's keys in, seeing them first"
cfg_before=$(cat cockpit.config.json)
show from_github cockpit trust add alice "https://github.com/kton-uat/alice.git"
require "showing changed nothing"              test "$cfg_before" = "$(cat cockpit.config.json)"
show from_github cockpit trust add alice "https://github.com/kton-uat/alice.git" --yes
show cockpit trust list

step "C3 — bob fetches alice's run by its record, every input checked against its hash"
show from_github cockpit run new check --from "$ALICE_FOTON"
require "her script is there, byte for byte"   cmp -s runs/check/run.py "$WORK/alice/runs/means/run.py"

step "C4 — bob reruns it and is handed the claim"
cockpit run check | tee "$WORK/bob-run.txt"
cd "$WORK/bob"
require "same bytes"                           grep -q "^same bytes as" "$WORK/bob-run.txt"
show cockpit say reproduces runs/check
OUT_HASH="sha256:$(sha256sum "$WORK/alice/runs/means/out/means.csv" | awk '{print $1}')"
N=$(cockpit ask '{"query":"reproductions","ref":"'"$OUT_HASH"'"}' --field verifiedSigners)
require "two parties, two keys, one result: ↻$N" test "$N" = "2"

step "E — bob works with words: claims about a run and a file, questions, his runs"
show cockpit say working-on runs/check step="checking alice's means"
show cockpit say working-on runs/check/out/means.csv step="reading the means"
show cockpit ask about runs/check
show cockpit ask reproductions runs/check/out/means.csv
show cockpit ask by signer me
show cockpit run list
require "his claims are his"                   bash -c 'cockpit ask by signer me | grep -c "self" | grep -qx 3'
refused "a claim missing a field"              cockpit say working-on runs/check
printf 'a,b\n1,2\n' > notes.csv
show cockpit publish --in runs/check/out/means.csv --out notes.csv -- cp runs/check/out/means.csv notes.csv
require "a reported result answers producer"   bash -c 'cockpit ask producer notes.csv | grep -q "cp runs/check"'

printf '\nalice set up, worked and published; bob took her keys in, fetched her run by its record,\n'
printf 'reran it in the same image and claimed it — all with cockpit, except naming her registry as a source.\n'
