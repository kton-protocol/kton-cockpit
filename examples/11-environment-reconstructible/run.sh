#!/usr/bin/env bash
# 11 - the same numbers, twice, and only one of the two records can be re-run by someone else.
#
# 07 pins an environment by digest, and that says WHICH image. This says how to get it back: the
# image is built from a Nix expression, the expression is an input to the record, and the digest
# follows from it. A pin identifies; a derivation reconstructs.
#
# Needs nix, docker and network. The first run downloads R's Nix closure — about 2 GB, because
# nixpkgs' R carries the toolchain it compiles packages with. That is a true fact about the stack
# and it is stated here rather than discovered halfway through.
cd "$(dirname "$0")"
source ../lib/common.sh
source ../lib/registry.sh
need_nix
docker version >/dev/null 2>&1 || { echo "11 needs a running container engine" >&2; exit 1; }
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"
mkdir -p nix R

cleanup() { docker rm -f cockpit-ex11-registry >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo ""
echo "== the environment, written down =="
cat > nix/env.nix <<'NIX'
# The execution environment as an expression. nixpkgs is pinned by tag, so this names one exact
# set of packages rather than "whatever R was current".
{ pkgs ? import (fetchTarball {
    url = "https://github.com/NixOS/nixpkgs/archive/refs/tags/25.05.tar.gz";
  }) {} }:
pkgs.dockerTools.buildImage {
  name = "r-analysis";
  tag = "pinned";
  copyToRoot = pkgs.buildEnv {
    name = "root";
    paths = [ pkgs.R pkgs.coreutils pkgs.bash ];
    pathsToLink = [ "/bin" "/lib" ];
  };
  # A Nix-built image contains exactly its closure and nothing else — including no /tmp, because
  # nothing in the closure asked for one. R needs one on its first line (R_TempDir), so it is
  # created here rather than discovered by a failing analysis.
  extraCommands = "mkdir -p tmp work && chmod 1777 tmp";
  config = { Cmd = [ "/bin/bash" ]; WorkingDir = "/work"; };
}
NIX
echo "  nix/env.nix written; building (cached after the first time)"
TARBALL=$(nix-build nix/env.nix --no-out-link) || { echo "  the image build failed" >&2; exit 1; }
require "nix produced an OCI image" test -s "$TARBALL"

echo ""
echo "== a digest, which means a registry =="
# An image that was built locally has no RepoDigest: a digest is what a registry assigns when it
# accepts a manifest. So the example runs one. Accepting an image ID instead would pin something
# nobody else can resolve. Everything this takes — a credential-free docker config, a port the
# engine can actually bind, skopeo rather than `docker load` — is in ../lib/registry.sh.
docker_config_without_helpers
PORT=$(local_registry cockpit-ex11-registry)
require "a local registry answers" test -n "$PORT"
DIGEST=$(push_image "$PORT" "$TARBALL" "r-analysis:pinned")
require "the registry assigned a digest" test -n "$DIGEST"
REF="localhost:$PORT/r-analysis@$DIGEST"
echo "  $REF"

echo ""
echo "== the analysis, run twice =="
cat > R/summarise.R <<'RS'
# Everyday: summarise mtcars by cylinder count. Base R, no packages, so the only thing that
# could differ between two runs is R itself.
d <- aggregate(mpg ~ cyl, data = mtcars, FUN = function(x) round(mean(x), 4))
write.csv(d, "work/mpg-by-cyl.csv", row.names = FALSE)
RS

# 1. On this machine, the way anyone would.
Rscript R/summarise.R
UNPINNED=$(cockpit publish '{"cmd":"Rscript R/summarise.R",
                             "inputs":["R/summarise.R"],
                             "outputs":["work/mpg-by-cyl.csv"]}')
U_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$UNPINNED")
U_OUT=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/mpg-by-cyl.csv"])' <<<"$UNPINNED")
U_ENV=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("envRef",""))' <<<"$UNPINNED")
echo "  on this machine   -> ${U_OUT:0:24}"

# 2. In the pinned image. The cockpit RUNS it this time, so the string handed to the engine and
#    the string recorded are one string — and nix/env.nix rides along as an input, which is what
#    makes the environment reconstructible rather than merely named.
python3 - cockpit.config.json "$REF" <<'PY'
import json, sys
p, ref = sys.argv[1], sys.argv[2]
c = json.load(open(p))
c["execution"] = {"image": "oci://" + ref, "network": False}
json.dump(c, open(p, "w"), indent=2)
PY
PINNED=$(cockpit publish '{"cmd":"Rscript R/summarise.R",
                           "inputs":["R/summarise.R","nix/env.nix"],
                           "outputs":["work/mpg-by-cyl.csv"]}')
P_ID=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$PINNED")
P_OUT=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/mpg-by-cyl.csv"])' <<<"$PINNED")
P_ENV=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("envRef",""))' <<<"$PINNED")
echo "  in the image      -> ${P_OUT:0:24}"

echo ""
echo "== same numbers, different records =="
require "both runs produced the same bytes"        test "$U_OUT" = "$P_OUT"
require "and they are two different fotons"        test "$U_ID" != "$P_ID"
absent  "the unpinned run names no environment" "oci://" "$U_ENV"
require "the pinned run names the exact image"     test "$P_ENV" = "oci://$REF"

echo ""
echo "== which is why the bytes cannot tell you =="
# Both fotons produced this hash, so asking about the RESULT finds both. The difference is not in
# the numbers — it is in what each record says about where they came from.
BOTH=$(cockpit ask "{\"query\":\"producer\",\"ref\":\"$U_OUT\"}" \
  | python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("included",[])))')
require "the result names both runs that produced it" test "$BOTH" = "2"

echo ""
echo "  Both are true and the numbers are identical. One of them can be re-run by somebody else:"
echo "  the image is named by digest, and nix/env.nix — an input, recorded by hash — is the"
echo "  expression that produces that digest. The other says only that it ran somewhere."
