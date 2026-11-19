#!/bin/sh
# Build the e2e's vocabulary package: the queries that decide what is a decision and what is closed
# (vocabulary/package-queries/decides.rq, answers.rq), as a sealed package in the scope format
# (template profile). The seed installs it with `cockpit install --allow`, so the shell asks them by
# name (`ask {query: "sparql", name}`) and never runs a query file from the checkout for them.
#
#   test/e2e/build-vocabulary-package.sh <scope CLI of ktonpkg v0.8.0>
#
# The scope CLI is ktonpkg's cmd/scope at v0.8.0, the version this cockpit links. The package is signed
# with a throwaway key made for this build and deleted after it: the committed package holds only its
# public half. Rebuild it after changing a file in vocabulary/package-queries/; the unit test
# "e2e vocabulary package" fails while the package and its sources differ.
set -eu
SCOPE="$1"
HERE=$(cd "$(dirname "$0")" && pwd)
OUT="$HERE/vocabulary-package"
[ ! -e "$OUT" ] || { echo "$OUT exists: move it away first" >&2; exit 1; }
KEYDIR=$(mktemp -d)
trap 'rm -r -f -- "$KEYDIR"' EXIT
"$SCOPE" templates "$OUT" workbench-e2e-queries 1 --queries "$HERE/vocabulary/package-queries" --key "$KEYDIR/package.key"
