#!/bin/sh
# e2e of the shell (the PR gate flows S1–S7, S9, S10, C1, C2 and W, the workbench views): seed a throwaway registry, start
# code-server with the packaged VSIX, drive it with Playwright, check the registry. Everything runs in
# containers (--rm), from images pinned by digest.
#
#   test/e2e/run.sh <work dir (absolute, scratch)> <cockpit binary> [<vocabulary folder>]
#   TRANSPORT=stdio …   the window talks to the cockpit over cockpit serve --stdio (default: the command line)
#
# SECURITY: the code-server and sidecar containers get the host's docker socket (the cockpit starts
# its SPARQL engine through docker). Access to the socket is root on the host. Run this only on a
# dedicated, throwaway machine or runner, never on a shared one. The work dir and its test keys are
# discarded at the end (KEEP=1 keeps them for debugging).
#
# The work dir is mounted at the SAME path in every container: the engine's bind mounts are host paths.
set -eu
# Bounds (a test window must not take its host down): every container of the run gets --memory TEST_MEMORY, the
# window's also --cpus CS_CPUS; POLL_SECONDS is the window's poll (the steps' waits grow with it); INDEX_MEMORY is the
# index.memory a seed may give a registry it configures.
TEST_MEMORY="${TEST_MEMORY:-4g}"; CS_CPUS="${CS_CPUS:-2}"; POLL_SECONDS="${POLL_SECONDS:-2}"; INDEX_MEMORY="${INDEX_MEMORY:-4g}"
WORK="$1"; COCKPIT="$2"
HERE=$(cd "$(dirname "$0")" && pwd)
VOCAB="${3:-$HERE/vocabulary}"
EXT=$(cd "$HERE/../.." && pwd)
OUTDIR="$HERE/out"
PORT=${PORT:-18099}
# Container names carry the run so two runs can share a machine (they also need different PORTs).
RUN_ID=$(basename "$WORK" | tr -c 'a-zA-Z0-9-' '-' | cut -c1-40)
UIDGID="$(id -u):$(id -g)"
DOCKER_GID=$(stat -c %g /var/run/docker.sock)
NODE_IMAGE=node:22@sha256:3218f0d1b9e4b63def322e9ae362d581fbeac1ef21b51fc502ef91386667ce92
CS_IMAGE=codercom/code-server:4.96.4@sha256:f93810f7f6636b3af3b1a750faf612349cddb89fbb6d28d69123a214fc75ee9e
PW_IMAGE=mcr.microsoft.com/playwright:v1.49.1-jammy@sha256:019b822658965bf5f6d509472802797554360f2940e677b2afb0f17748d0d0e3
# A static docker CLI from a pinned image, never the host's /usr/bin/docker.
DOCKER_CLI_IMAGE=docker:27-cli@sha256:851f91d241214e7c6db86513b270d58776379aacc5eb9c4a87e5b47115e3065c
VSIX="$EXT/out/kton-workbench.vsix"
[ -f "$VSIX" ] || { echo "no $VSIX: run npm run package first" >&2; exit 1; }

case "$WORK" in /*) ;; *) echo "the work dir must be absolute" >&2; exit 1;; esac
[ ! -e "$WORK" ] || { echo "$WORK exists; pass a fresh work dir" >&2; exit 1; }
mkdir -p "$WORK/tmp" "$WORK/cs/data/User" "$WORK/cs/ext" "$WORK/pw" "$WORK/bin" "$WORK/rpc" "$WORK/faults" "$OUTDIR"
find "$OUTDIR" -maxdepth 1 -type f \( -name '*.png' -o -name '*.json' \) -delete
# from here on the run cleans up after itself, also when the seed or a build fails
cleanup() {
  docker stop kton-wb-e2e-$RUN_ID >/dev/null 2>&1 || true
  docker stop kton-wb-e2e-sidecar-$RUN_ID >/dev/null 2>&1 || true
  # the cockpit's index containers for this run's registries (persistent index, mounts under the work dir)
  for c in $(docker ps -aq --filter name=kton-cockpit-index); do
    docker inspect "$c" --format '{{range .Mounts}}{{.Source}} {{end}}' 2>/dev/null | grep -q "$WORK/" && docker rm -f "$c" >/dev/null 2>&1 || true
  done
  # Discard the throwaway registry and its test keys unless KEEP=1.
  [ "${KEEP:-0}" = 1 ] || rm -r -f -- "$WORK"
}
trap cleanup EXIT

echo "== docker CLI from $DOCKER_CLI_IMAGE"
docker run --memory "$TEST_MEMORY" --rm --entrypoint cat "$DOCKER_CLI_IMAGE" /usr/local/bin/docker > "$WORK/bin/docker"
chmod +x "$WORK/bin/docker"
DOCKER_MOUNT="-v $WORK/bin/docker:/usr/local/bin/docker:ro"

# The cockpit runs its SPARQL engine through docker; pull the pinned image first, so the first asks on a cold
# runner do not wait for the pull (seed.sh pins the same digest in the repo's cockpit.config.json).
ENGINE_IMAGE=oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0
echo "== engine image"
docker pull -q "$ENGINE_IMAGE" >/dev/null

# ktonpkg v0.9.11's scope CLI builds the test packages (entry scopes on review-view entries need v0.9.11 in the
# cockpit too), from a golang image pinned by digest.
GO_IMAGE=golang:1.25@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80
echo "== ktonpkg v0.9.11 scope CLI"
mkdir -p "$WORK/go"
docker run --memory "$TEST_MEMORY" --rm --network host -u "$UIDGID" -e HOME=/tmp -e GOPATH="$WORK/go" -e GOCACHE="$WORK/go/cache" -e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOFLAGS=-modcacherw \
  -v "$WORK:$WORK" "$GO_IMAGE" \
  sh -c "GOBIN=$WORK/bin go install github.com/gitmick/ktonpkg/cmd/scope@v0.9.11"

# ktonpkg v0.9.13's scope CLI for flow W's packages: an act through another package's control (from: a pinned head)
echo "== ktonpkg v0.9.13 scope CLI"
mkdir -p "$WORK/go/bin-0913"
docker run --memory "$TEST_MEMORY" --rm --network host -u "$UIDGID" -e HOME=/tmp -e GOPATH="$WORK/go" -e GOCACHE="$WORK/go/cache" -e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOFLAGS=-modcacherw \
  -v "$WORK:$WORK" "$GO_IMAGE" \
  sh -c "GOBIN=$WORK/go/bin-0913 go install github.com/gitmick/ktonpkg/cmd/scope@v0.9.13"
cp "$WORK/go/bin-0913/scope" "$WORK/bin/scope-0913"

echo "== seed"
# EXTRA_FLOWS (optional): a folder with more flows from outside this repository, run on top of these:
#   hooks.sh   sourced here, before the seed: it may take snapshots (snapshot <name> <git repository>, below) and set
#              EXTRA_SEED_ARGS (docker arguments of the seed container), EXTRA_CS_ARGS (of the sidecar and code-server)
#              and EXTRA_PW_ENV (of the Playwright container), e.g. read-only mounts and their env;
#   seed.sh    sourced at the end of seed.sh (mounted at /extra); it sets EXTRA_SEED_JSON (seed.json's "extra");
#   flows.mjs  its default export runs further steps with smoke.mjs's helpers, after the flows here.
# Every git repository a hook reads should be a SNAPSHOT taken once here: a local clone at the source's current HEAD
# under $WORK/snap, mounted read-only, so nothing moves under a run's steps.
SNAP="$WORK/snap"; mkdir -p "$SNAP"
snapshot() { # snapshot <name> <git repository>: prints the snapshot's path
  git -C "$2" rev-parse --verify -q HEAD >/dev/null || { echo "$2 is no git repository with a HEAD" >&2; exit 1; }
  # the clone keeps the source's own remote (the cockpit reads the registry's owner/repo from origin; never pushed to)
  git clone -q --local "$2" "$SNAP/$1" && git -C "$SNAP/$1" remote set-url origin "$(git -C "$2" remote get-url origin)" && echo "  snapshot $1: $2 at $(git -C "$SNAP/$1" rev-parse --short HEAD)" >&2
  echo "$SNAP/$1"
}
EXTRA_SEED_ARGS=""; EXTRA_CS_ARGS=""; EXTRA_PW_ENV=""; EXTRA_MOUNT=""
if [ -n "${EXTRA_FLOWS:-}" ]; then
  EXTRA_FLOWS=$(cd "$EXTRA_FLOWS" && pwd)
  EXTRA_MOUNT="-v $EXTRA_FLOWS:/extra:ro"
  # shellcheck disable=SC1091
  [ ! -f "$EXTRA_FLOWS/hooks.sh" ] || . "$EXTRA_FLOWS/hooks.sh"
fi
# The seed, the sidecar and code-server share one index daemon (HOME=$WORK/home: the cockpit's index directory and
# daemon.json), published on the host's loopback only. All three run on the host network: a cockpit on a bridge network
# cannot reach that address, takes the daemon for dead and starts it again (docker rm -f, then run), which killed every
# load the window's asks were waiting for (the index restarted every ~35 s while the sidecar asked; the seed's warm-up
# never reached the window's index). The seed gets the docker socket too, so its warm-up asks build that same index.
docker run --memory "$TEST_MEMORY" --rm --network host -u "$UIDGID" --group-add "$DOCKER_GID" -v /var/run/docker.sock:/var/run/docker.sock $DOCKER_MOUNT -v "$WORK:$WORK" -v "$HERE/seed.sh:/seed.sh:ro" $EXTRA_MOUNT $EXTRA_SEED_ARGS -e INDEX_MEMORY="$INDEX_MEMORY" \
  -v "$COCKPIT:/usr/local/bin/cockpit:ro" -v "$VOCAB:/vocab:ro" -v "$HERE/vocabulary-package:/vocab-package:ro" -v "$HERE:/e2e:ro" \
  "$NODE_IMAGE" sh /seed.sh "$WORK" /vocab /vocab-package

# S7: one stand-in that answers through the real cockpit unless $WORK/faults/mode says otherwise.
# Listings (say --json) always pass through: the shell keeps reading its templates.
# TRANSPORT=stdio: the window runs "cockpit-faults serve --stdio", which hands the connection to stdio-faults.mjs, a
# proxy in front of the real "cockpit serve --stdio" that injects the same faults per method (code-server's node runs it).
cp "$HERE/stdio-faults.mjs" "$WORK/bin/stdio-faults.mjs"
cat > "$WORK/bin/cockpit-faults" <<EOF
#!/bin/sh
if [ "\$1" = serve ]; then exec /usr/lib/code-server/lib/node "$WORK/bin/stdio-faults.mjs" "$WORK" "\$@"; fi
MODE=\$(cat "$WORK/faults/mode" 2>/dev/null || echo normal)
if [ "\$1" = say ] && [ "\${2:-}" != --json ]; then
  case "\$MODE" in
    say-refuse) echo '{"code":"template.not-allowed","reason":"the template decides is not allowed here"}'; exit 2;;
    say-garbage) echo "this is not json"; exit 0;;
    say-slow) sleep 120; exec /usr/local/bin/cockpit "\$@";;
    say-locked) if [ -e "$WORK/faults/registry.lock" ]; then echo '{"code":"store.busy","clause":"store","reason":"the registry is busy (git lock held); nothing was signed"}'; exit 2; fi;;
    say-gone) kill -9 \$\$;;
    say-stdin-closed) exec 0<&-; echo "cockpit: its stdin was closed before it answered" >&2; exit 1;;
    say-pending) /usr/local/bin/cockpit "\$@" | sed '0,/{/s//{"pending":{"code":"store.busy","reason":"the registry was busy; the next write commits it"},/'; exit 0;;
  esac
fi
if [ "\$1" = ask ] && [ "\$MODE" = ask-garbage ]; then echo "this is not json"; exit 0; fi
# S5.3 version skew, in this test harness only: a newer package names a slot this shell does not know. The
# listing is the real one, with one entry added to hostile-good (code-server's own node does the JSON).
if [ "\$1" = ui ] && [ "\${2:-}" = list ]; then
  OUT=\$(/usr/local/bin/cockpit "\$@"); RC=\$?
  [ \$RC -eq 0 ] || { printf '%s' "\$OUT"; exit \$RC; }
  printf '%s' "\$OUT" | /usr/lib/code-server/lib/node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const l=JSON.parse(s);const p=l.packages.find(x=>x.name==="hostile-good"&&Array.isArray(x.ui));if(p)p.ui.push({file:"ui/sidebar.json",slot:"sidebar-ad",label:"Sidebar ad",when:{kind:"claim"}});process.stdout.write(JSON.stringify(l));})'
  exit 0
fi
exec /usr/local/bin/cockpit "\$@"
EOF
chmod +x "$WORK/bin/cockpit-faults"
echo normal > "$WORK/faults/mode"

# No person tiers: the window's person is recognised only by its key id (from qa-decider-claims.pub,
# checked against doctor), so every decision step depends on it, never on a tier or a label.
cat > "$WORK/cs/data/User/settings.json" <<EOF
{
  "kton.cockpit.path": "$WORK/bin/cockpit-faults",
  "kton.cockpit.timeoutSeconds": 60,
  "kton.cockpit.transport": "${TRANSPORT:-cli}",
  "kton.queries.dir": "$WORK/queries",
  "kton.identity.personKey": "qa-decider",
  "kton.identity.agentKey": "qa-decider-agent",
  "kton.identity.personTiers": [],
  "kton.poll.seconds": $POLL_SECONDS,
  "kton.surfaces.file": "$WORK/surfaces.json",
  "kton.sources.dir": "$WORK/mirrors",
  "kton.artifacts.dir": "$WORK/artifacts",
  "security.workspace.trust.enabled": false,
  "workbench.startupEditor": "none",
  "telemetry.telemetryLevel": "off",
  "extensions.autoCheckUpdates": false
}
EOF

echo "== sidecar (registry side)"
SIDE=$(docker run --memory "$TEST_MEMORY" -d --rm --network host --name kton-wb-e2e-sidecar-$RUN_ID -u "$UIDGID" --group-add "$DOCKER_GID" -e HOME="$WORK/home" -e TMPDIR="$WORK/tmp" \
  -v "$WORK:$WORK" $EXTRA_CS_ARGS -v /var/run/docker.sock:/var/run/docker.sock $DOCKER_MOUNT -v "$COCKPIT:/usr/local/bin/cockpit:ro" \
  -v "$HERE/sidecar.mjs:/sidecar.mjs:ro" "$NODE_IMAGE" node /sidecar.mjs "$WORK")

echo "== code-server"
CS_COMMON="-u $UIDGID --group-add $DOCKER_GID --network host -e HOME=$WORK/home -e TMPDIR=$WORK/tmp \
  -v $WORK:$WORK $EXTRA_CS_ARGS -v /var/run/docker.sock:/var/run/docker.sock $DOCKER_MOUNT \
  -v $COCKPIT:/usr/local/bin/cockpit:ro -v $EXT/out:/vsix:ro -v $HERE:$HERE:ro --entrypoint code-server"
docker run --memory "$TEST_MEMORY" --rm $CS_COMMON "$CS_IMAGE" --user-data-dir "$WORK/cs/data" --extensions-dir "$WORK/cs/ext" \
  --install-extension /vsix/kton-workbench.vsix
CID=$(docker run --memory "$TEST_MEMORY" -d --rm --name kton-wb-e2e-$RUN_ID --cpus "$CS_CPUS" $CS_COMMON "$CS_IMAGE" --user-data-dir "$WORK/cs/data" --extensions-dir "$WORK/cs/ext" \
  --auth none --bind-addr "127.0.0.1:$PORT" --disable-telemetry --disable-update-check --disable-workspace-trust "$WORK/repo")
i=0; until curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null; do i=$((i+1)); [ $i -lt 60 ] || { docker logs "$CID" | tail -20; exit 1; }; sleep 1; done

echo "== playwright (npm ci from the committed lockfile)"
cp "$HERE/package.json" "$HERE/package-lock.json" "$HERE/smoke.mjs" "$HERE/runrecord.mjs" "$WORK/pw/"
[ -z "${EXTRA_FLOWS:-}" ] || [ ! -f "$EXTRA_FLOWS/flows.mjs" ] || cp "$EXTRA_FLOWS/flows.mjs" "$WORK/pw/extra-flows.mjs"
RC=0
docker run --memory "$TEST_MEMORY" --rm --network host -u "$UIDGID" -e HOME=/tmp -e CS_URL="http://127.0.0.1:$PORT/?folder=$WORK/repo" -e OUT=/out -e FAILFAST="${FAILFAST:-0}" -e TRANSPORT="${TRANSPORT:-cli}" -e ONLY="${ONLY:-}" -e POLL_SECONDS="$POLL_SECONDS" $EXTRA_PW_ENV \
  -e WORK="$WORK" -v "$WORK:$WORK" -v "$OUTDIR:/out" -w "$WORK/pw" "$PW_IMAGE" \
  sh -c "npm ci --no-audit --no-fund >/dev/null && node smoke.mjs" || RC=$?

echo "== out/: only PNG and JSON"
if find "$OUTDIR" -maxdepth 1 -type f ! -name '*.png' ! -name '*.json' | grep -q .; then
  echo "out/ holds something other than PNG and JSON:"; find "$OUTDIR" -maxdepth 1 -type f ! -name '*.png' ! -name '*.json'; RC=1
fi
if grep -l -r -e "PRIVATE KEY" -e "\.key\"" "$OUTDIR" >/dev/null 2>&1; then echo "a key reference reached out/"; RC=1; fi
exit $RC
