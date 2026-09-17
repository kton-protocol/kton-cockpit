# Sourced by the examples that need an image named by a DIGEST.
#
# An image built locally has no RepoDigest. A digest is what a registry assigns when it accepts a
# manifest, so these examples run one rather than pin an image id — an id resolves on the machine
# that built it and nowhere else, which is the opposite of what a pinned environment is for.
#
# Everything below is a host behaviour that reports as something else. They are written down here
# once because each of them cost an afternoon.

# docker_config_without_helpers - a docker config of this example's own, asking for no credentials.
#
# Docker Desktop on Windows writes `credsStore: desktop.exe` into ~/.docker/config.json, and from
# WSL that helper fails for a localhost registry: docker reports "error getting credentials" and
# never falls back to an anonymous pull. A local registry needs no credentials at all. The cockpit
# does not scrub the environment before invoking the engine, so this reaches the run it performs too.
docker_config_without_helpers() {
  export DOCKER_CONFIG="$PWD/.docker"
  mkdir -p "$DOCKER_CONFIG"
  echo '{}' > "$DOCKER_CONFIG/config.json"
}

# local_registry <container-name> - start one and echo the port it answers on.
#
# A low, explicit port, tried in a small range. Two host behaviours meet here and both report as
# something else: Docker Desktop runs its daemon in a VM, so a registry bound to WSL's loopback is
# unreachable from the side that pulls ("connection refused"); and on Windows the high dynamic range
# belongs to WinNAT, where docker reports a port as bound and the listener is then not reachable,
# which arrives as an IPv6 timeout. Low ports on every interface avoid both.
local_registry() {
  local name="$1" port=""
  docker rm -f "$name" >/dev/null 2>&1 || true
  for p in 5000 5001 5002 5003 5004; do
    if docker run -d --rm -p "$p:5000" --name "$name" registry:2 >/dev/null 2>&1; then
      port=$p; break
    fi
  done
  [ -n "$port" ] || { echo "no local registry could be started" >&2; return 1; }
  for _ in $(seq 1 30); do curl -sf "http://127.0.0.1:$port/v2/" >/dev/null 2>&1 && break; sleep 1; done
  curl -sfo /dev/null "http://localhost:$port/v2/" || { echo "the local registry does not answer" >&2; return 1; }
  echo "$port"
}

# push_image <port> <docker-archive-tarball> <repo:tag> - push it and echo the digest the registry
# assigned.
#
# skopeo copies the tarball straight in. `docker load` would first unpack gigabytes onto the daemon
# for no reason — the bytes are already an image. `localhost` rather than the literal address,
# because docker treats that name as an insecure registry by default and so does not insist on TLS
# a local registry has not got, and it resolves on both stacks.
push_image() {
  local port="$1" tarball="$2" ref="$3"
  nix run nixpkgs#skopeo -- --insecure-policy copy --dest-tls-verify=false \
    "docker-archive:$tarball" "docker://localhost:$port/$ref" >/dev/null 2>&1 \
    || { echo "pushing $ref failed" >&2; return 1; }
  nix run nixpkgs#skopeo -- --insecure-policy inspect --tls-verify=false \
    "docker://localhost:$port/$ref" 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["Digest"])'
}

# need_nix - a fresh install puts nix on PATH only for new login shells, so a script started from an
# old one finds nothing. Source the profile if it is there before deciding nix is missing.
need_nix() {
  command -v nix >/dev/null || . /nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh 2>/dev/null || true
  command -v nix >/dev/null || { echo "$EXNAME needs nix — skipping is not an option here, so this is a failure" >&2; exit 1; }
}
