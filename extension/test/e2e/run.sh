#!/bin/sh
# e2e of the shell (the PR gate flows S1–S4, S7, S9, S10): seed a throwaway registry, start
# code-server with the packaged VSIX, drive it with Playwright, check the registry. Everything runs in
# containers (--rm), from images pinned by digest.
#
#   test/e2e/run.sh <work dir (absolute, scratch)> <cockpit binary> [<vocabulary folder>]
#
# SECURITY: the code-server and sidecar containers get the host's docker socket (the cockpit starts
# its SPARQL engine through docker). Access to the socket is root on the host. Run this only on a
# dedicated, throwaway machine or runner, never on a shared one. The work dir and its test keys are
# discarded at the end (KEEP=1 keeps them for debugging).
#
# The work dir is mounted at the SAME path in every container: the engine's bind mounts are host paths.
set -eu
WORK="$1"; COCKPIT="$2"
HERE=$(cd "$(dirname "$0")" && pwd)
VOCAB="${3:-$HERE/vocabulary}"
EXT=$(cd "$HERE/../.." && pwd)
OUTDIR="$HERE/out"
PORT=${PORT:-18099}
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

echo "== docker CLI from $DOCKER_CLI_IMAGE"
docker run --rm --entrypoint cat "$DOCKER_CLI_IMAGE" /usr/local/bin/docker > "$WORK/bin/docker"
chmod +x "$WORK/bin/docker"
DOCKER_MOUNT="-v $WORK/bin/docker:/usr/local/bin/docker:ro"

echo "== seed"
docker run --rm -u "$UIDGID" -v "$WORK:$WORK" -v "$HERE/seed.sh:/seed.sh:ro" \
  -v "$COCKPIT:/usr/local/bin/cockpit:ro" -v "$VOCAB:/vocab:ro" "$NODE_IMAGE" sh /seed.sh "$WORK" /vocab

# S7: one stand-in that answers through the real cockpit unless $WORK/faults/mode says otherwise.
# Listings (say --json) always pass through: the shell keeps reading its templates.
cat > "$WORK/bin/cockpit-faults" <<EOF
#!/bin/sh
MODE=\$(cat "$WORK/faults/mode" 2>/dev/null || echo normal)
if [ "\$1" = say ] && [ "\${2:-}" != --json ]; then
  case "\$MODE" in
    say-refuse) echo '{"code":"template.not-allowed","reason":"the template decides is not allowed here"}'; exit 2;;
    say-garbage) echo "this is not json"; exit 0;;
    say-slow) sleep 120; exec /usr/local/bin/cockpit "\$@";;
    say-locked) if [ -e "$WORK/faults/registry.lock" ]; then echo "the registry is locked by another process (store lock held)" >&2; exit 1; fi;;
  esac
fi
if [ "\$1" = ask ] && [ "\$MODE" = ask-garbage ]; then echo "this is not json"; exit 0; fi
exec /usr/local/bin/cockpit "\$@"
EOF
chmod +x "$WORK/bin/cockpit-faults"
echo normal > "$WORK/faults/mode"

cat > "$WORK/cs/data/User/settings.json" <<EOF
{
  "kton.cockpit.path": "$WORK/bin/cockpit-faults",
  "kton.cockpit.timeoutSeconds": 20,
  "kton.queries.dir": "$WORK/queries",
  "kton.identity.personKey": "qa-decider",
  "kton.identity.agentKey": "qa-decider-agent",
  "kton.identity.personTiers": ["self"],
  "kton.poll.seconds": 2,
  "kton.surfaces.file": "$WORK/surfaces.json",
  "security.workspace.trust.enabled": false,
  "workbench.startupEditor": "none",
  "telemetry.telemetryLevel": "off",
  "extensions.autoCheckUpdates": false
}
EOF

echo "== sidecar (registry side)"
SIDE=$(docker run -d --rm --name kton-wb-e2e-sidecar -u "$UIDGID" --group-add "$DOCKER_GID" -e HOME="$WORK/home" -e TMPDIR="$WORK/tmp" \
  -v "$WORK:$WORK" -v /var/run/docker.sock:/var/run/docker.sock $DOCKER_MOUNT -v "$COCKPIT:/usr/local/bin/cockpit:ro" \
  -v "$HERE/sidecar.mjs:/sidecar.mjs:ro" "$NODE_IMAGE" node /sidecar.mjs "$WORK")

echo "== code-server"
CS_COMMON="-u $UIDGID --group-add $DOCKER_GID --network host -e HOME=$WORK/home -e TMPDIR=$WORK/tmp \
  -v $WORK:$WORK -v /var/run/docker.sock:/var/run/docker.sock $DOCKER_MOUNT \
  -v $COCKPIT:/usr/local/bin/cockpit:ro -v $EXT/out:/vsix:ro --entrypoint code-server"
docker run --rm $CS_COMMON "$CS_IMAGE" --user-data-dir "$WORK/cs/data" --extensions-dir "$WORK/cs/ext" \
  --install-extension /vsix/kton-workbench.vsix
CID=$(docker run -d --rm --name kton-wb-e2e $CS_COMMON "$CS_IMAGE" --user-data-dir "$WORK/cs/data" --extensions-dir "$WORK/cs/ext" \
  --auth none --bind-addr "127.0.0.1:$PORT" --disable-telemetry --disable-update-check --disable-workspace-trust "$WORK/repo")
cleanup() {
  docker stop kton-wb-e2e >/dev/null 2>&1 || true
  docker stop kton-wb-e2e-sidecar >/dev/null 2>&1 || true
  # Discard the throwaway registry and its test keys unless KEEP=1.
  [ "${KEEP:-0}" = 1 ] || rm -r -f -- "$WORK"
}
trap cleanup EXIT
i=0; until curl -sf "http://127.0.0.1:$PORT/healthz" >/dev/null; do i=$((i+1)); [ $i -lt 60 ] || { docker logs "$CID" | tail -20; exit 1; }; sleep 1; done

echo "== playwright (npm ci from the committed lockfile)"
cp "$HERE/package.json" "$HERE/package-lock.json" "$HERE/smoke.mjs" "$HERE/runrecord.mjs" "$WORK/pw/"
RC=0
docker run --rm --network host -u "$UIDGID" -e HOME=/tmp -e CS_URL="http://127.0.0.1:$PORT/?folder=$WORK/repo" -e OUT=/out -e FAILFAST="${FAILFAST:-0}" \
  -e WORK="$WORK" -v "$WORK:$WORK" -v "$OUTDIR:/out" -w "$WORK/pw" "$PW_IMAGE" \
  sh -c "npm ci --no-audit --no-fund >/dev/null && node smoke.mjs" || RC=$?

echo "== out/: only PNG and JSON"
if find "$OUTDIR" -maxdepth 1 -type f ! -name '*.png' ! -name '*.json' | grep -q .; then
  echo "out/ holds something other than PNG and JSON:"; find "$OUTDIR" -maxdepth 1 -type f ! -name '*.png' ! -name '*.json'; RC=1
fi
if grep -l -r -e "PRIVATE KEY" -e "\.key\"" "$OUTDIR" >/dev/null 2>&1; then echo "a key reference reached out/"; RC=1; fi
exit $RC
