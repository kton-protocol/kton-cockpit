# Sourced by every example.
#
# Two things it deliberately does NOT do. It does not wrap the three verbs in helpers that hide
# their arguments — what an example shows is the call a session actually makes, and a wrapper would
# show a wrapper. And it does not scaffold quietly: `participant` prints what it builds, because a
# reader who cannot see where the registry, the keys and the config came from cannot tell which
# part of the demonstration is the cockpit's doing.
set -euo pipefail

EXROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"   # examples/
REPO="$(cd "$EXROOT/.." && pwd)"                            # the cockpit checkout
# A sibling `kton-pinned` before `kton`, for the reason internal/testrepo prefers it: the second is
# somebody's WORKING TREE, so every intermediate state of their work becomes a build input here —
# and an example then runs against a kernel the pin in CLAUDE.md does not name. That is how three
# examples came to fail on a wire form nothing in this repository had been tested against.
_kton_src() {
  local root; root="$(dirname "$REPO")"
  for c in "${KTON_SRC:-}" "$root/kton-pinned" "$root/kton"; do
    [ -n "$c" ] && [ -d "$c/reference/cmd/plankton" ] && { echo "$c"; return; }
  done
  echo "${KTON_SRC:-$root/kton}"
}
KTON_SRC="$(_kton_src)"
EXNAME="$(basename "$PWD")"

# cockpit - the binary in the participant repo, called the way anyone would call it.
#
# It is a function only so the examples do not repeat the path. Every argument after it is a real
# argument to a real command: what an example prints is what you type. Every guard in the cockpit
# arrives as a refusal and leaves by exit 2, so `set -e` stops the example rather than carrying on.
#
# The same three verbs are also the MCP tool surface, and `examples/lib/mcp.py` drives them that
# way. Both reach the same handler — nothing here is a shortcut past a check a session would meet.
cockpit() { "${COCKPIT_REPO_DIR:-$PWD}/bin/cockpit" "$@"; }

# expect_fail <what> <cmd...> - a NEGATIVE CONTROL. It fails the example when the command SUCCEEDS.
# Without these, an example that demonstrates a guard proves nothing: a guard that never fires and a
# guard that cannot fire look identical from the outside.
expect_fail() {
  local what="$1"; shift
  if "$@" >/dev/null 2>&1; then
    echo "  FAIL: $what was accepted, and it must not be" >&2
    exit 1
  fi
  echo "  refused, as it must be: $what"
}

# require <description> <condition...> - assert, and say what was asserted. Every check here states
# what it SAW, because the failure this repo has actually had is a check that passed having read
# nothing at all.
require() {
  local what="$1"; shift
  if ! "$@"; then
    echo "  FAIL: $what" >&2
    exit 1
  fi
  echo "  ok: $what"
}

# absent <description> <needle> <text> - assert that <text> does NOT contain <needle>. Its own
# helper rather than a negated grep inside `require`, because the shell quoting needed to nest one
# inside the other is the kind that silently word-splits the description into filenames.
absent() {
  local what="$1" needle="$2" text="$3"
  if grep -qF -- "$needle" <<<"$text"; then
    echo "  FAIL: $what (found \"$needle\")" >&2
    exit 1
  fi
  echo "  ok: $what"
}

# demoseed <label> - a deterministic 64-hex seed, so an example's identity is a function of the
# repository rather than of when it ran, and two runs produce the same ids.
#
# These are DEMO keys. The seed is written down here, so the private key is public and the identity
# is worth nothing — correct for a fixture, catastrophic for anything real. Real use: plain keygen.
demoseed() { printf 'cockpit-examples/%s/%s' "$EXNAME" "$1" | sha256sum | cut -d" " -f1; }

# build_binaries <dir> - the kernel and the cockpit, built from source into <dir>.
# Never vendored and never taken from PATH: which kernel wrote a store decides whether that store
# reads as populated or as empty-with-exit-0, so a binary of unknown provenance is the one thing
# not to demonstrate against.
build_binaries() {
  local bin="$1"
  mkdir -p "$bin"
  [ -d "$KTON_SRC/reference/cmd/plankton" ] || { echo "no kton checkout at $KTON_SRC (set KTON_SRC)" >&2; exit 1; }
  (cd "$KTON_SRC" && go build -o "$bin/plankton" ./reference/cmd/plankton)
  (cd "$KTON_SRC" && go build -o "$bin/nekton"   ./nekton/reference/cmd/nekton)
  (cd "$KTON_SRC" && go build -o "$bin/kton"     ./kton/reference/cmd/kton)
  (cd "$REPO"     && go build -o "$bin/cockpit"  ./cmd/cockpit)
}

# participant <dir> [config-json] [name] - a working participant repo, from nothing.
#
# It is a real git repo with a real github.com origin, because the anti-wrong-folder guard reads
# that remote on every call and an example that bypassed it would be demonstrating something else.
# Only the remote's PUSH url points at a local bare repo, so commits and pushes complete offline.
#
# `name` gives a SECOND participant its own repository identity, and with it its own keys: the demo
# seeds are derived from it, so two participants in one example sign with genuinely different keys
# rather than the same one twice. Without that, every federation example would be one party talking
# to itself, and the trust questions it exists to ask would all answer trivially.
participant() {
  local dir="$1" cfg="${2:-}" name="${3:-$EXNAME}"
  local base; base="$(dirname "$dir")"
  rm -rf "$dir" "$base/origin.git"
  mkdir -p "$dir"

  git init --bare --quiet -b main "$base/origin.git"
  git -C "$dir" init --quiet -b main
  git -C "$dir" remote add origin "git@github.com:cockpit-examples/$name.git"
  git -C "$dir" remote set-url --push origin "$base/origin.git"

  mkdir -p "$dir"/{registry/plankton,registry/nekton,registry/keys,templates,keys,data,work}
  cp "$REPO/uat/participant-skeleton/gitignore" "$dir/.gitignore"
  cp "$REPO/uat/participant-skeleton/templates/"*.json "$dir/templates/"
  build_binaries "$dir/bin"

  (cd "$dir" && ./bin/plankton keygen keys/session-1 --seed "$(demoseed "plankton/$name")" >/dev/null
                ./bin/nekton  keygen keys/session-1-claims --seed "$(demoseed "nekton/$name")" >/dev/null
                cp keys/session-1.pub keys/session-1-claims.pub registry/keys/)

  if [ -n "$cfg" ]; then printf '%s\n' "$cfg" > "$dir/cockpit.config.json"
  else cp "$EXROOT/lib/cockpit.config.json" "$dir/cockpit.config.json"; fi
  # The origin url the guard checks is written above; keep the config's repo binding in step with it.
  python3 - "$dir/cockpit.config.json" "$name" <<'PY'
import json, sys
path, name = sys.argv[1], sys.argv[2]
cfg = json.load(open(path))
cfg.setdefault("repo", {}).update({"owner": "cockpit-examples", "name": name})
json.dump(cfg, open(path, "w"), indent=2)
PY

  git -C "$dir" add -A
  git -C "$dir" -c user.name=example -c user.email=example@cockpit.local commit --quiet -m "scaffold"
  git -C "$dir" push --quiet -u origin main
  export COCKPIT_REPO_DIR="$dir"
  echo "  participant at $dir (git origin cockpit-examples/$name, pushes to a local bare repo)"
}
