# Gemeinsam für git-seite.sh und extraktion.sh: jeden Schritt wörtlich ausführen und protokollieren,
# und einen Plan aus `pkgtool plan` Step für Step mit dem Cockpit laufen lassen.
# Erwartet WORK, LOG und REPO_DIR vom Aufrufer.

# schritt PHASE ID AKTEUR LUECKE TITEL BEFEHL [erwartet-fehler]
#
# Führt BEFEHL genau so aus, wie er dasteht, und schreibt ihn wörtlich ins Protokoll — der Befehl im
# Ergebnis ist der, der lief, nicht eine Beschreibung davon. Mit "erwartet-fehler" ist ein Exit ≠ 0
# der Erfolg des Schritts (eine Ablehnung, die kommen soll).
schritt() {
  local phase="$1" id="$2" actor="$3" gap="$4" title="$5" cmd="$6" expect="${7:-}"
  local out err code
  printf '\n== %-14s %s\n   $ %s\n' "$phase" "$title" "$cmd"
  set +e
  (cd "$REPO_DIR" && eval "$cmd") >"$WORK/stdout" 2>"$WORK/stderr"; code=$?
  set -e
  out=$(cat "$WORK/stdout"); err=$(cat "$WORK/stderr")
  [ -n "$out" ] && printf '%s\n' "$out" | head -12 | sed 's/^/   | /'
  [ -n "$err" ] && printf '%s\n' "$err" | head -6 | sed 's/^/   ! /'
  python3 - "$LOG" "$phase" "$id" "$actor" "$gap" "$title" "$cmd" "$code" "$expect" "$WORK/stdout" "$WORK/stderr" <<'PY'
import json, sys
log, phase, id, actor, gap, title, cmd, code, expect, fout, ferr = sys.argv[1:]
out, err = open(fout).read(), open(ferr).read()
try:
    result = json.loads(out) if out.strip() else {}
except ValueError:
    result = {"text": out.strip()[:4000]}
if err.strip():
    if not isinstance(result, dict): result = {"value": result}
    result["stderr"] = err.strip()[:2000]
ok = (int(code) != 0) if expect else (int(code) == 0)
with open(log, "a") as f:
    f.write(json.dumps({"phase": phase, "id": id, "title": title, "actor": actor, "command": cmd,
                        "gap": gap, "ok": ok, "exit": int(code), "result": result}, ensure_ascii=False) + "\n")
PY
  # Weiter nach einem Fehlschlag, wie die improve-Seite auch: der Vergleich soll zeigen, was auf
  # beiden Seiten geschah, nicht nur bis wohin eine kam.
  if [ -z "$expect" ] && [ "$code" -ne 0 ]; then echo "   (Schritt fehlgeschlagen, Exit $code)"; fi
  if [ -n "$expect" ] && [ "$code" -eq 0 ]; then echo "   (erwartete Ablehnung blieb aus)"; fi
  printf '%s' "$out" > "$WORK/last.out"
}
last() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(eval(sys.argv[2], {"d": d}))' "$WORK/last.out" "$1"; }

# laufen PLAN PHASE — die Steps eines Plans in Reihenfolge: Eingaben bereitlegen, dann
# `cockpit publish` im gepinnten Image.
laufen() {
  local plan="$1" phase="$2" i STEP REQ n
  n=$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["steps"]))' "$plan")
  for ((i = 0; i < n; i++)); do
    STEP=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["steps"][int(sys.argv[2])]["id"])' "$plan" "$i")
    python3 - "$plan" "$i" "$REPO_DIR" <<'PY'
import json, os, shutil, sys
s = json.load(open(sys.argv[1]))["steps"][int(sys.argv[2])]
os.chdir(sys.argv[3])
for st in s["stage"]:
    os.makedirs(os.path.dirname(st["to"]), exist_ok=True)
    shutil.copyfile(st["from"], st["to"])
PY
    REQ=$(python3 -c 'import json,sys; s=json.load(open(sys.argv[1]))["steps"][int(sys.argv[2])]; print(json.dumps({"cmd":s["cmd"],"inputs":s["inputs"],"outputs":s["outputs"]}, ensure_ascii=False))' "$plan" "$i")
    schritt "$phase" "$phase:$STEP" cockpit "S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst" \
      "Step $STEP im gepinnten jam-r-Image ausführen und als Foton veröffentlichen" \
      "./bin/cockpit publish '$REQ'"
  done
}
