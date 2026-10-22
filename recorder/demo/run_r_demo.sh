#!/bin/sh
# Like run_demo.sh, with real R: shim + ktonrec boundaries (TASK, SOURCE, ENV, VARS, err).
set -e
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-$(mktemp -d)}
command -v R >/dev/null || { echo "R is not installed" >&2; exit 1; }
sh "$ROOT/build.sh"
P="$WORK/project"; mkdir -p "$P/data"
printf 'id,time,conc\n1,0,0\n1,1,4.2\n2,1,5.0\n' > "$P/data/pk.csv"
printf 'id,dose\n1,100\n2,200\n' > "$P/data/dose.csv"
cat > "$P/step2.R" <<'R'
dose <- read.csv("data/dose.csv")
pkd <- merge(pk, dose)
write.csv(pkd, "data/pk_dose.csv", row.names = FALSE)
R
cat > "$WORK/session.R" <<'R'
source(Sys.getenv("KTON_TRACE_R"))
pk <- read.csv("data/pk.csv")
source("step2.R")
x <- try(read.csv("data/does_not_exist.csv"), silent = TRUE)
pkd2 <- read.csv("data/pk_dose.csv"); m <- matrix(1:6, 2)
R
LOG="$WORK/trace.tsv"; : > "$LOG"
cd "$P"
KTON_TRACE_LOG="$LOG" KTON_TRACE_HASH="$P" KTON_TRACE_R="$ROOT/r/kton-trace.R" LD_PRELOAD="$ROOT/.build/libktontrace.so" \
  R --no-save --no-restore -q -f "$WORK/session.R" > "$WORK/r.out" 2>&1
"$ROOT/.build/ktonwatch" -log "$LOG" -once -noise
