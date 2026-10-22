#!/bin/sh
# Builds the recorder's two binaries into recorder/.build/ (gitignored):
#   libktontrace.so   the LD_PRELOAD shim
#   ktonwatch         the watcher
set -e
ROOT=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$ROOT/.build"
gcc -shared -fPIC -O2 -Wall -Wno-format-truncation -o "$ROOT/.build/libktontrace.so" "$ROOT/shim/ktontrace.c" -ldl
(cd "$ROOT/.." && go build -o "$ROOT/.build/ktonwatch" ./recorder/cmd/ktonwatch)
