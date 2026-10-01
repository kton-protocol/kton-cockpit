#!/usr/bin/env bash
# Workflow-Bedienung: die Raute von der Ausführung bis zur Durchführung in einem anderen kton, nur
# mit `cockpit …`. Jeder Aufruf steht mit seiner Ausgabe in results/aufrufe.md — dem Material für
# das gemeinsame Review der Bedienbarkeit (ABLAUF.md).
#
# Läuft gegen den lokalen Kernel-Branch (K5, K7, K8): GOWORK zeigt auf kernel.go.work im Workstream.
# Braucht docker, go, python3, das alte Raute-Paket aus dem improveGo-Workstream und ktonpkg.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
EXNAME=workflow-bedienung
export GOWORK="${GOWORK:-/mnt/c/dev/workstreams/active/cockpit/kernel.go.work}"
source ../../examples/lib/common.sh

IMPROVEGO_WS="${IMPROVEGO_WS:-/mnt/c/dev/workstreams/active/improveGo}"
KTONPKG="${KTONPKG:-$(cd "$REPO/.." && pwd)/ktonpkg}"
RAUTE="$IMPROVEGO_WS/examples/raute/paket"
IMAGE="scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a"
OXIGRAPH="oci://ghcr.io/oxigraph/oxigraph@sha256:7532c1f9aa5f28c0dc6a4243198f7f6144017fab7429bf6c164a89fec9411767"

WORK="$HIER/.work"
LOG="$WORK/schritte.jsonl"
AUFRUFE="$HIER/results/aufrufe.md"
rm -rf "$WORK"; mkdir -p "$WORK" "$HIER/results"; : > "$LOG"
( cd ../raute-zwei-backends/pkgtool && go build -o "$WORK/pkgtool" . )
( cd "$KTONPKG" && go build -o "$WORK/scope" ./cmd/scope )
source ../raute-zwei-backends/schritt.sh

cat > "$AUFRUFE" <<EOF
# Workflow-Bedienung — alle Cockpit-Aufrufe

Erzeugt von \`acceptance/workflow-bedienung/lauf.sh\`. Jeder Aufruf genau so, wie er lief, im Repo
der genannten Seite, mit der Ausgabe, die ein Mensch sieht. Lange Ausgaben sind gekürzt.

EOF

# aufruf SEITE TITEL BEFEHL [erwartet-fehler] — einen Cockpit-Aufruf ausführen und mitschreiben.
aufruf() {
  local seite="$1" titel="$2" cmd="$3" expect="${4:-}" code
  printf '\n== [%s] %s\n   $ %s\n' "$seite" "$titel" "$cmd"
  set +e
  (cd "$REPO_DIR" && eval "$cmd") >"$WORK/out" 2>&1; code=$?
  set -e
  sed 's/^/   | /' "$WORK/out" | head -40
  {
    printf '### %s — %s\n\n```\n$ %s\n' "$seite" "$titel" "$cmd"
    head -60 "$WORK/out"
    [ "$(wc -l < "$WORK/out")" -gt 60 ] && printf '… (%s Zeilen)\n' "$(wc -l < "$WORK/out")"
    [ "$code" -ne 0 ] && printf '[exit %s]\n' "$code"
    printf '```\n\n'
  } >> "$AUFRUFE"
  if [ -z "$expect" ] && [ "$code" -ne 0 ]; then echo "   (fehlgeschlagen, exit $code)"; FEHLER=$((FEHLER+1)); fi
  if [ -n "$expect" ] && [ "$code" -eq 0 ]; then echo "   (erwartete Ablehnung blieb aus)"; FEHLER=$((FEHLER+1)); fi
  return 0
}
FEHLER=0
abschnitt() { printf '\n## %s\n\n' "$1" >> "$AUFRUFE"; echo; echo "=== $1"; }

config() { # extra-python
  python3 - "$REPO/examples/lib/cockpit.config.json" "docker.io/$IMAGE" "$OXIGRAPH" "$1" <<'PY'
import json, sys
c = json.load(open(sys.argv[1]))
c["execution"] = {"image": "oci://" + sys.argv[2], "network": False, "entrypoint": "Rscript"}
c["claims"]["allowedTemplates"] = []
c["query"] = {"image": sys.argv[3]}
exec(sys.argv[4])
print(json.dumps(c, indent=2))
PY
}
commit() { git -C "$REPO_DIR" add -A && git -C "$REPO_DIR" -c user.name=example -c user.email=example@cockpit.local commit -qm "$1" && git -C "$REPO_DIR" push -q; }
# Die Templates des Grundgerüsts weichen dem Vokabularpaket: dieselben Namen aus zwei Quellen
# lehnt das Cockpit ab, statt nach Lesereihenfolge zu entscheiden.
nur_vokabular() { rm -f "$REPO_DIR"/templates/*.json; touch "$REPO_DIR/templates/.keep"; }

# ===================================================================================================
abschnitt "0 Vorbedingung: das Workflow-Vokabular"
"$WORK/scope" templates "$WORK/kton-workflow" kton-workflow 1 \
  --templates "$HIER/vokabular/templates" --queries "$HIER/vokabular/queries" --key "$WORK/kton-projekt.key" > "$WORK/vokabular.json"
VOK=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["package"])' "$WORK/vokabular.json")
echo "   Vokabularpaket kton-workflow: $VOK"
printf 'Das Vokabularpaket `kton-workflow` (Templates: installed, reproduces, derived-from; Abfrage: workflows)\nhat die Id `%s`. Der Operator installiert es mit `--allow`; das trägt die Id in `claims.allowedPackages` ein.\n\n' "$VOK" >> "$AUFRUFE"

participant "$WORK/a/repo" "$(config "")" raute-autorin >/dev/null
REPO_DIR="$WORK/a/repo"; A="$REPO_DIR"; nur_vokabular; commit "nur das Vokabularpaket"
aufruf A "Vokabular installieren und zulassen" "./bin/cockpit install ../../kton-workflow --allow"

# ===================================================================================================
abschnitt "1 A: die Raute Schritt für Schritt ausführen, zweimal"
cp -r "$RAUTE" "$REPO_DIR/quelle"
cp "$HIER/../raute-zwei-backends/eigene-daten.csv" "$REPO_DIR/data/eigene-daten.csv"
commit "die Skripte der Raute und ein zweiter Datensatz"
(cd "$REPO_DIR" && "$WORK/pkgtool" plan quelle work/lauf-1 > "$WORK/plan-1.json")
(cd "$REPO_DIR" && "$WORK/pkgtool" plan quelle work/lauf-2 --bind dataset=data/eigene-daten.csv --bind anzahl=3 > "$WORK/plan-2.json")
for lauf in 1 2; do
  n=$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["steps"]))' "$WORK/plan-$lauf.json")
  for ((i = 0; i < n; i++)); do
    STEP=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["steps"][int(sys.argv[2])]["id"])' "$WORK/plan-$lauf.json" "$i")
    ARGS=$(python3 - "$WORK/plan-$lauf.json" "$i" <<'PY'
import json, os, shlex, sys
s = json.load(open(sys.argv[1]))["steps"][int(sys.argv[2])]
ins = []
for st in s["stage"]:
    name = os.path.basename(st["to"])
    src = st["from"]
    ins.append("--in " + shlex.quote(src if os.path.basename(src) == name else name + "=" + src))
cmd = s["cmd"].split(" && ", 1)[1]
print("--dir " + shlex.quote(s["dir"]) + " " + " ".join(ins) + " -- " + cmd)
PY
)
    aufruf A "Lauf $lauf, Step $STEP" "./bin/cockpit run $ARGS"
  done
done

# ===================================================================================================
abschnitt "2 A: den Workflow herausziehen, die Raute als Referenz mitpacken"
E1=work/lauf-1/zusammenführung/ergebnis.txt; E2=work/lauf-2/zusammenführung/ergebnis.txt
aufruf A "Was bieten die Läufe an?" "./bin/cockpit workflow propose $E1 $E2"
aufruf A "Herausziehen: Lauf 1 als Referenz, dataset als Loch, anzahl als Parameter" \
  "./bin/cockpit workflow extract $E1 $E2 --name raute --reference $E1 --hole dataset=1 --param anzahl=2"

# ===================================================================================================
abschnitt "3 B: installieren"
AUTOR_PUBS="registry/keys/autorin-plankton.pub registry/keys/autorin-nekton.pub"
participant "$WORK/b/repo" "$(config "c['trust']['tiers']['autorin'] = '$AUTOR_PUBS'.split()")" raute-anwender >/dev/null
REPO_DIR="$WORK/b/repo"; B="$REPO_DIR"; nur_vokabular
cp "$A/keys/session-1.pub" "$B/registry/keys/autorin-plankton.pub"
cp "$A/keys/session-1-claims.pub" "$B/registry/keys/autorin-nekton.pub"
commit "der Autorin vertrauen; nur das Vokabularpaket"
aufruf B "Vokabular installieren und zulassen" "./bin/cockpit install ../../kton-workflow --allow"
aufruf B "Den Workflow installieren" "./bin/cockpit install ../../a/repo/packages/raute"

# ===================================================================================================
abschnitt "4 B: suchen, auswählen"
aufruf B "Welche Workflows gibt es hier?" "./bin/cockpit workflow list"
aufruf B "Die Raute ansehen" "./bin/cockpit workflow show raute"

# ===================================================================================================
abschnitt "5 B: durchführen"
aufruf B "Ohne Bindung (soll ablehnen)" "./bin/cockpit workflow run raute" expect-fail
aufruf B "Nachprüfen mit den Testdaten" "./bin/cockpit workflow run raute --check"
cp "$HIER/../raute-zwei-backends/eigene-daten.csv" "$B/data/eigene-daten.csv"; commit "eigener Datensatz"
aufruf B "Mit eigenen Daten" "./bin/cockpit workflow run raute --bind dataset=data/eigene-daten.csv --bind anzahl=3 --dir work/eigen"
aufruf B "Woher kommt mein Ergebnis?" "./bin/cockpit workflow trace work/eigen/zusammenführung/ergebnis.txt"
aufruf B "Und das Ergebnis der Nachprüfung?" "./bin/cockpit workflow trace work/lauf-1/zusammenführung/ergebnis.txt"

echo
echo "$FEHLER Aufruf(e) anders als erwartet — Protokoll: $AUFRUFE"
exit $((FEHLER > 0))
