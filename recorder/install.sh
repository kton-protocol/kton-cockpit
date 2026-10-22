#!/bin/sh
# Builds the recorder and installs it under $PREFIX (default ~/.local):
#   $PREFIX/lib/kton/libktontrace.so   the shim (LD_PRELOAD)
#   $PREFIX/bin/ktonwatch              the watcher (ktonwatch serve)
#   $PREFIX/bin/cockpit                the cockpit (publish, say, ask) the observations are documented with
#   $PREFIX/share/kton/r/              kton-trace.R, ktonrec/, Rprofile-kton.R
#   $PREFIX/share/kton/python/         ktonrec.py: command boundaries for the Python console
#   ~/.ipython/profile_default/startup/50-kton.py   loads it in IPython kernels that run under
#                                      the shim; nothing else in the profile is touched
#   $PREFIX/share/kton/kton-recorder-*.vsix   the Positron extension
#   $PREFIX/bin/kton-repro             makes a recorded file again in containers and compares
#   $PREFIX/bin/kton-call              the stand-in for a tool call (reuse by key, else the real tool)
#   $PREFIX/share/kton/stubs/          nmfe75, execute, vpc … → kton-call; active only with
#                                      PATH="$PREFIX/share/kton/stubs:$PATH"
# Files are replaced by rename, so a running R session keeps the shim it loaded.
set -e
ROOT=$(cd "$(dirname "$0")" && pwd)
PREFIX=${PREFIX:-$HOME/.local}
sh "$ROOT/build.sh"
python3 -I "$ROOT/positron/pack.py" >/dev/null
put() { mkdir -p "$(dirname "$2")"; cp "$1" "$2.new.$$" && mv -f "$2.new.$$" "$2"; }
put "$ROOT/.build/libktontrace.so" "$PREFIX/lib/kton/libktontrace.so"
put "$ROOT/.build/ktonwatch" "$PREFIX/bin/ktonwatch"; chmod +x "$PREFIX/bin/ktonwatch"
(cd "$ROOT" && CGO_ENABLED=0 go build -o "$ROOT/.build/kton-call" ./cmd/kton-call)
put "$ROOT/.build/kton-call" "$PREFIX/bin/kton-call"; chmod +x "$PREFIX/bin/kton-call"
(cd "$ROOT" && go build -o "$ROOT/.build/kton-repro" ./cmd/kton-repro)
put "$ROOT/.build/kton-repro" "$PREFIX/bin/kton-repro"; chmod +x "$PREFIX/bin/kton-repro"
mkdir -p "$PREFIX/share/kton/stubs"
for t in nmfe74 nmfe75 nmfe76 execute vpc bootstrap scm sse bbi; do ln -sfn "$PREFIX/bin/kton-call" "$PREFIX/share/kton/stubs/$t"; done
# the cockpit itself, so that "publish" from the observations has a verb to call
(cd "$ROOT/.." && go build -o "$ROOT/.build/cockpit" ./cmd/cockpit)
put "$ROOT/.build/cockpit" "$PREFIX/bin/cockpit"; chmod +x "$PREFIX/bin/cockpit"
mkdir -p "$PREFIX/share/kton/r"
rm -rf "$PREFIX/share/kton/r.new.$$" && cp -r "$ROOT/r" "$PREFIX/share/kton/r.new.$$"
rm -rf "$PREFIX/share/kton/r" && mv "$PREFIX/share/kton/r.new.$$" "$PREFIX/share/kton/r"
mkdir -p "$PREFIX/share/kton/python"
put "$ROOT/python/ktonrec.py" "$PREFIX/share/kton/python/ktonrec.py"
STARTUP=${IPYTHONDIR:-$HOME/.ipython}/profile_default/startup
mkdir -p "$STARTUP"
# the path as a Python string literal, whatever characters $PREFIX has
python3 -I -c 'import sys; src = open(sys.argv[1], encoding="utf-8").read(); sys.stdout.buffer.write(src.replace("\"@KTON_PY@\"", repr(sys.argv[2])).encode("utf-8", "surrogateescape"))' \
  "$ROOT/python/50-kton.py" "$PREFIX/share/kton/python" > "$STARTUP/50-kton.py.new.$$" && mv "$STARTUP/50-kton.py.new.$$" "$STARTUP/50-kton.py"
for v in "$ROOT"/.build/*.vsix; do put "$v" "$PREFIX/share/kton/$(basename "$v")"; done
# the flow is kton-web's graph viewer: remember its checkout ($KTON_WEB, or next to kton-cockpit)
WEB=${KTON_WEB:-$(cd "$ROOT/../.." && pwd)/kton-web}
if [ -f "$WEB/viewers/graph/app.js" ]; then echo "$WEB" > "$PREFIX/share/kton/web"
else echo "Hinweis: kein kton-web unter $WEB; ktonwatch serve braucht dann -web" >&2; fi
echo "installiert nach $PREFIX: lib/kton/libktontrace.so, bin/ktonwatch, bin/kton-call, share/kton/"
