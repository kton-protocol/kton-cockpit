#!/usr/bin/env bash
# 12 - nobody writes Nix. The environment is declared in R, by the person doing the analysis, and
# the digest the record pins follows from that R file.
#
# 11 makes the environment reconstructible, and pays for it with a hand-written Nix expression.
# That is a real cost: an R user who has to learn a second language to pin their environment will
# not pin their environment. So here the only files a human writes are R, and one of them is
# `R/env.R` — a call to {rix}, which GENERATES the Nix expression. The generated file and the
# image it builds are both recorded, but neither is authored.
#
# The chain, all the way down, and every step an input to the record:
#
#     R/env.R  --rix-->  nix/default.nix  --lift-->  nix/image.nix  --nix-->  image  --push-->  digest
#
# Needs nix, docker and network. R's Nix closure is about 2 GB and dplyr's dependencies add to it;
# the first run downloads them. That is a true fact about the stack, stated here rather than
# discovered halfway through.
#
# Source: {rix} is https://docs.ropensci.org/rix/ (rOpenSci). The lift in nix/image.nix is this
# example's own, not rix's recommendation — rix's documented container route runs Nix INSIDE a
# Docker image, which pins a base image nobody chose as well as the environment somebody did.
cd "$(dirname "$0")"
source ../lib/common.sh
source ../lib/registry.sh
need_nix
docker version >/dev/null 2>&1 || { echo "12 needs a running container engine" >&2; exit 1; }
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"
mkdir -p nix R

cleanup() { docker rm -f cockpit-ex12-registry >/dev/null 2>&1 || true; }
trap cleanup EXIT

# The nixpkgs that provides {rix} itself, pinned by commit. Pinning the thing that generates the
# pin is not belt-and-braces: a different rix writes a different expression, and then the record
# says the environment came from an R file that no longer produces it.
RIXPKGS="https://github.com/NixOS/nixpkgs/archive/c7def046b9a883d46974757852106483d741586f.tar.gz"

echo ""
echo "== the environment, declared in R =="
cat > R/env.R <<'RE'
# The environment, as an R user would say it. No Nix in this file.
#
# `date` is the pin: rix resolves it against the rstats-on-nix fork of nixpkgs, which keeps a
# dated snapshot for every day, so "R and the packages as they were on 2025-09-01" is a thing you
# can ask for and get back. A version number alone would not do it — dplyr 1.1.4 built against two
# different R releases is two different environments.
library(rix)
rix(
  date       = "2025-09-01",
  r_pkgs     = c("dplyr"),
  ide        = "none",
  project_path = "nix",
  overwrite  = TRUE
)
RE
cat > nix/image.nix <<'NIX'
# The lift: the environment rix generated, as an OCI image. One file, written once, and the
# only Nix in this example that a person typed.
#
# `import ./default.nix` returns { pkgs, shell } — the pinned nixpkgs and an mkShell derivation.
# Its buildInputs is a list of LISTS (rix emits the R packages and the system packages as two
# groups), which nix-shell flattens on its own and buildEnv does not, hence lib.flatten. R finds
# its packages through R_LIBS_SITE because nixpkgs installs them under /library, which has to be
# linked in explicitly — with the default pathsToLink the image has R and no dplyr, and says so
# only when the analysis runs.
let
  env  = import ./default.nix;
  pkgs = env.pkgs;
in
pkgs.dockerTools.buildImage {
  name = "r-analysis";
  tag  = "rix";
  copyToRoot = pkgs.buildEnv {
    name = "root";
    paths = pkgs.lib.flatten env.shell.buildInputs ++ [ pkgs.coreutils pkgs.bash ];
    pathsToLink = [ "/bin" "/lib" "/library" ];
  };
  # A Nix-built image contains exactly its closure and nothing else — including no /tmp, because
  # nothing in the closure asked for one. R needs one on its first line (R_TempDir).
  extraCommands = "mkdir -p tmp work && chmod 1777 tmp";
  config = {
    Cmd = [ "/bin/bash" ];
    WorkingDir = "/work";
    Env = [ "R_LIBS_SITE=/library" ];
  };
}
NIX

# rix is an R package, so running it needs an R that has it. nix provides that R; the wrapper is
# scaffolding for this example and is not part of the record.
cat > nix/rix-runner.nix <<NIX
let pkgs = import (fetchTarball { url = "$RIXPKGS"; }) {};
in pkgs.rWrapper.override { packages = [ pkgs.rPackages.rix ]; }
NIX
echo "  running R/env.R (this is the only environment file a person wrote)"
nix-shell nix/rix-runner.nix --run "Rscript R/env.R" >/dev/null 2>&1 \
  || { echo "  rix could not generate the expression" >&2; exit 1; }
require "rix generated nix/default.nix" test -s nix/default.nix
# rix writes an .Rprofile beside it, to keep a system R's library off the path. Not an input to
# anything here, and not pretended to be.
rm -f nix/.Rprofile
echo "  nix/default.nix, generated:"
grep -E "fetchTarball|r_ver" nix/default.nix | head -3 | sed 's/^/    /'

echo ""
echo "== the image, built from what R wrote =="
TARBALL=$(nix-build nix/image.nix --no-out-link) || { echo "  the image build failed" >&2; exit 1; }
require "nix produced an OCI image" test -s "$TARBALL"

# The claim worth checking, and the reason a GENERATED file is safe to record as an input: run the
# R file again and you get the same environment. Not "a similar one" — the same store path, which
# is nix's name for the same bytes.
nix-shell nix/rix-runner.nix --run "Rscript R/env.R" >/dev/null 2>&1
rm -f nix/.Rprofile
AGAIN=$(nix-build nix/image.nix --no-out-link)
require "regenerating from R/env.R rebuilds the same image" test "$TARBALL" = "$AGAIN"

echo ""
echo "== a digest, which means a registry =="
docker_config_without_helpers
PORT=$(local_registry cockpit-ex12-registry)
require "a local registry answers" test -n "$PORT"
DIGEST=$(push_image "$PORT" "$TARBALL" "r-analysis:rix")
require "the registry assigned a digest" test -n "$DIGEST"
REF="localhost:$PORT/r-analysis@$DIGEST"
echo "  $REF"

echo ""
echo "== the analysis, which needs a package =="
cat > R/summarise.R <<'RS'
# dplyr, not base R — on purpose. 11's analysis used base R, so the only thing that could differ
# between two machines was R itself. Here "which dplyr" is a real question, and R/env.R answers it.
library(dplyr)
mtcars |>
  group_by(cyl) |>
  summarise(mpg = round(mean(mpg), 4), n = n()) |>
  arrange(cyl) |>
  write.csv("work/mpg-by-cyl.csv", row.names = FALSE)
RS

python3 - cockpit.config.json "$REF" <<'PY'
import json, sys
p, ref = sys.argv[1], sys.argv[2]
c = json.load(open(p))
c["execution"] = {"image": "oci://" + ref, "network": False}
json.dump(c, open(p, "w"), indent=2)
PY

# Every file in the chain is an input. R/env.R is the one a person wrote; nix/default.nix is what
# it produced; nix/image.nix is the lift. Recording the generated file as well as its source is
# not redundant — it is what lets a reader check that the two agree, instead of taking it on trust.
PUB=$(cockpit publish '{"cmd":"Rscript R/summarise.R",
                        "inputs":["R/env.R","nix/default.nix","nix/image.nix","R/summarise.R"],
                        "outputs":["work/mpg-by-cyl.csv"]}')
FOTON=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["fotonId"])' <<<"$PUB")
ENVREF=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("envRef",""))' <<<"$PUB")
echo "  ran in the image, produced work/mpg-by-cyl.csv"
sed 's/^/    /' work/mpg-by-cyl.csv

echo ""
echo "== what the record carries =="
require "the record pins the image by digest"  test "$ENVREF" = "oci://$REF"
absent  "and not by tag"                       ":rix\"" "$ENVREF"

# The point of the whole example, asked rather than asserted: start from the file that declares the
# environment and find the work that ran in it. R/env.R is an input like any other, so `uses` —
# "which records consumed these bytes" — answers it, and answers it for every future record too.
ENVHASH=$(./bin/plankton hash R/env.R)
USES=$(cockpit ask "{\"query\":\"uses\",\"ref\":\"$ENVHASH\"}")
require "the environment declaration leads back to the work that ran in it" \
  grep -q "$FOTON" <<<"$USES"
# And so does what rix made of it, which is what lets a reader check that the two agree rather than
# take it on trust: the generated expression is in the record next to its source.
GENHASH=$(./bin/plankton hash nix/default.nix)
GEN=$(cockpit ask "{\"query\":\"uses\",\"ref\":\"$GENHASH\"}")
require "so does the expression rix generated from it" grep -q "$FOTON" <<<"$GEN"

echo ""
echo "  The record names an image by digest, and the digest is reachable from an R file:"
echo "  run R/env.R, build nix/image.nix, push, and you are at that digest again. The only Nix"
echo "  anybody typed is nix/image.nix, once — and whoever has to reproduce the work does not"
echo "  read even that. They read R/env.R, which is R."
