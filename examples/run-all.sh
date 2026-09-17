#!/usr/bin/env bash
# Every example, in order, and a summary that counts.
#
# It does not stop at the first failure — a run that dies on 03 tells you nothing about 04 to 08,
# and the question after a change is usually "what else moved". Exit status is non-zero if any
# example failed.
cd "$(dirname "$0")"
PASS=(); FAIL=()
for dir in [0-9][0-9]-*/; do
  name="${dir%/}"
  printf '\n\033[1m=== %s ===\033[0m\n' "$name"
  if bash "$name/run.sh"; then PASS+=("$name"); else FAIL+=("$name"); fi
done

printf '\n\033[1m=== summary ===\033[0m\n'
printf '  %d passed: %s\n' "${#PASS[@]}" "${PASS[*]:-—}"
if [ "${#FAIL[@]}" -gt 0 ]; then
  printf '  %d FAILED: %s\n' "${#FAIL[@]}" "${FAIL[*]}"
  exit 1
fi
# Said out loud, because "8 passed" out of an unknown total says nothing.
printf '  all %d examples passed\n' "${#PASS[@]}"
