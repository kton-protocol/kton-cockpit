#!/bin/sh
# Build the cockpit of this checkout for the e2e, linked against ktonpkg v0.9.5: the test packages' compositions
# carry `layout` (ktonpkg v0.9.5), which an older ktonpkg refuses at install as an unknown field. The checkout
# links an older ktonpkg until kton-cockpit moves to v0.9.5; this build pins it on a copy of HEAD, and nothing in
# the checkout changes (drop the `go get` once go.mod requires v0.9.5 or later).
#
#   test/e2e/build-cockpit.sh <out file (absolute)>
#
# Then: test/e2e/run.sh <work dir> <out file>
set -eu
OUT="$1"
case "$OUT" in /*) ;; *) echo "the out file must be absolute" >&2; exit 1;; esac
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(cd "$HERE/../../.." && pwd)
GO_IMAGE=golang:1.25@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80
SRC=$(mktemp -d)
trap 'rm -r -f -- "$SRC"' EXIT
git -C "$ROOT" archive HEAD | tar -x -C "$SRC"
mkdir -p "$(dirname "$OUT")"
docker run --rm --network host -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOPATH=/tmp/go -e CGO_ENABLED=0 -e GOTOOLCHAIN=local \
  -v "$SRC:/src" -v "$(dirname "$OUT"):/out" -w /src "$GO_IMAGE" \
  sh -c "go get github.com/gitmick/ktonpkg@v0.9.5 && go build -o /out/$(basename "$OUT") ./cmd/cockpit"
echo "built $OUT (this checkout's cockpit, ktonpkg v0.9.5)"
