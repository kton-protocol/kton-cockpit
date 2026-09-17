#!/usr/bin/env bash
# 07 - the environment pin stops being a claim. Needs docker.
cd "$(dirname "$0")"
source ../lib/common.sh

docker version >/dev/null 2>&1 || { echo "07 needs a running container engine — skipping is not an option here, so this is a failure" >&2; exit 1; }
IMAGE_TAG=python:3.12-slim
docker pull -q "$IMAGE_TAG" >/dev/null
# Selected by name rather than taken at index 0. An image has a RepoDigest per repository it has
# been pulled from or pushed to, and one that was built locally and then pushed carries TWO — the
# second of which is the only registry-resolvable one. Index 0 happens to be right for a pulled
# image and is an assumption either way, of the same shape as "a record's id is the first hash on
# its line": true today, guaranteed nowhere.
DIGEST=$(docker image inspect "$IMAGE_TAG" --format '{{json .RepoDigests}}' \
  | python3 -c 'import json, sys

def repo(ref):
    # The tag is whatever follows the LAST colon, and only when that colon comes after the last
    # slash: a registry host carries its own colon, so localhost:5000/tiny:probe splits at the
    # wrong one if you take the first. Same mistake this comment block exists to fix, one level in.
    slash, colon = ref.rfind("/"), ref.rfind(":")
    return ref[:colon] if colon > slash else ref

want = repo(sys.argv[1])
for d in json.load(sys.stdin):
    if d.split("@")[0] == want:
        print(d); break
else:
    sys.exit("no RepoDigest for " + want + " - the image was never pulled from or pushed to a registry")' "$IMAGE_TAG")
require "the image has a registry-resolvable digest" test -n "$DIGEST"
echo "  pinned $IMAGE_TAG at $DIGEST"

WORK="$PWD/.work"
CFG=$(python3 - "$REPO/examples/lib/cockpit.config.json" "$DIGEST" <<'PY'
import json,sys
c=json.load(open(sys.argv[1]))
c["execution"]={"image":"oci://"+sys.argv[2],"network":False}
print(json.dumps(c,indent=2))
PY
)
participant "$WORK/repo" "$CFG"; cd "$WORK/repo"

echo ""
echo "== the cockpit runs the command; nothing here does =="
printf 'id,value\n1,42\n2,17\n' > data/in.csv
OUT=$(cockpit publish '{
  "cmd": "awk -F, '\''NR>1 {s+=$2} END {print \"total\"; print s}'\'' data/in.csv > work/total.csv",
  "inputs": ["data/in.csv"],
  "outputs": ["work/total.csv"]
}')
require "the container produced the output" test -f work/total.csv
echo "  work/total.csv = $(tr '\n' ' ' < work/total.csv)"

EXEC=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("executedIn",""))' <<<"$OUT")
ENVREF=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("envRef",""))' <<<"$OUT")
require "it reports which image it ran in"        test "$EXEC" = "oci://$DIGEST"
# The property the whole thing is for: one string went to docker and into the foton, so what ran
# and what is recorded cannot be different things.
require "and the foton pins that same string"     test "$ENVREF" = "$EXEC"

echo ""
echo "== an undeclared output is reported, not adopted =="
UND=$(cockpit publish '{
  "cmd": "cp data/in.csv work/declared.csv; cp data/in.csv work/forgotten.csv",
  "inputs": ["data/in.csv"], "outputs": ["work/declared.csv"]
}')
require "the file nobody declared is named" \
  grep -q 'work/forgotten.csv' <<<"$UND"
absent "and it did NOT become an output of the foton" '"work/forgotten.csv": "sha256' "$(python3 -c 'import json,sys; print(json.dumps(json.load(sys.stdin).get("outputHashes",{})))' <<<"$UND")"
echo "  taking it would put an incidental file into the foton's IDENTITY, and two identical runs"
echo "  would stop producing the same record"

echo ""
echo "== a failed run publishes nothing =="
# git is addressed with -C rather than through the shell's cwd: a container that has just had this
# directory bind-mounted can leave the cwd handle stale on WSL/NTFS, and `git rev-parse` then fails
# with "Unable to read current working directory" — which looks like a broken example rather than
# what it is.
REPO_DIR="$WORK/repo"
BEFORE=$(git -C "$REPO_DIR" rev-parse HEAD)
expect_fail "a command that exits non-zero" \
  cockpit publish '{"cmd":"exit 3","inputs":[],"outputs":["work/never.csv"]}'
require "and it left no commit behind" test "$(git -C "$REPO_DIR" rev-parse HEAD)" = "$BEFORE"
