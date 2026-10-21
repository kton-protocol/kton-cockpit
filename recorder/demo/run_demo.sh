#!/bin/sh
# Builds shim + watcher, creates a small test project, runs the session
# with the shim preloaded while ktonwatch follows the log live.
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-$(mktemp -d)}

missing=""
for t in gcc go python3; do command -v "$t" >/dev/null 2>&1 || missing="$missing $t"; done
if [ -n "$missing" ]; then
  echo "Fehlt:$missing  (Ubuntu: sudo apt install build-essential; Go: https://go.dev/dl/)" >&2
  exit 1
fi

sh "$ROOT/build.sh"
BIN="$ROOT/.build"

P="$WORK/project"
OUTSIDE="$WORK/outside"
mkdir -p "$P/data" "$P/derived" "$OUTSIDE"
printf 'id,time,conc\n1,0,0\n1,1,4.2\n1,2,3.1\n2,0,0\n2,1,5.0\n2,2,0.8\n' > "$P/data/raw.csv"
printf 'id,dose\n1,100\n2,200\n' > "$P/data/dose.csv"
for i in 1 2 3; do printf 'id,x\n%d,%d\n' $i $((i*10)) > "$P/data/sub_$i.csv"; done
printf 'id,wt\n1,70\n2,82\n' > "$OUTSIDE/covariates.csv"

LOG="$WORK/trace.tsv"; : > "$LOG"
PYENV=$(python3 -I -c 'import sys,sysconfig; print(",".join(sorted({sysconfig.get_paths()["stdlib"], sysconfig.get_paths()["purelib"], sysconfig.get_paths()["platlib"], "/usr/local/lib/python3", "/usr/lib/python3"} | {__import__("site").getusersitepackages()})))')

"$BIN/ktonwatch" -log "$LOG" -project "$P" \
  -env "$PYENV,/usr/lib/x86_64-linux-gnu,/lib/,/usr/bin/,/usr/local/bin/" \
  -json "$WORK/tasks.json" &
WATCH=$!
# ktonwatch only stops at the END line; if the session dies first, don't leave it running
trap 'kill $WATCH 2>/dev/null || true' EXIT

cd "$P"
KTON_DEMO_OUTSIDE="$OUTSIDE" KTON_TRACE_LOG="$LOG" KTON_TRACE_HASH="$P" LD_PRELOAD="$BIN/libktontrace.so" \
  python3 "$ROOT/demo/session.py"

wait $WATCH
echo
echo "Rohlog:  $LOG  ($(wc -l < "$LOG") Zeilen)"
echo "JSON:    $WORK/tasks.json"
