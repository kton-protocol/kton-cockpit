#!/usr/bin/env bash
# Die Raute als Paket im Scope-Format, von beiden Seiten: die Autorin baut und versiegelt es, der
# User installiert es, prüft es nach und rechnet mit eigenen Daten. Zwei Teilnehmerrepos, zwei
# nektons — das Paket ist das der Autorin, alles Weitere steht im nekton des Users.
#
# Am Ende liegen zwei unions für kton-web bereit: nur das Paket, und das Repo des Users nach der
# Installation (KTON_WEB, Standard ../../../kton-web, unter data/raute-paket und data/raute-anwender).
#
# Braucht docker, go, python3, das alte Raute-Paket aus dem improveGo-Workstream und ktonpkg.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
EXNAME=raute-autorin
source ../../examples/lib/common.sh

IMPROVEGO_WS="${IMPROVEGO_WS:-/mnt/c/dev/workstreams/active/improveGo}"
KTONPKG="${KTONPKG:-$(cd "$REPO/.." && pwd)/ktonpkg}"
KTON_WEB="${KTON_WEB:-$(cd "$REPO/.." && pwd)/kton-web}"
RAUTE="$IMPROVEGO_WS/examples/raute/paket"
IMAGE="scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a"
INSTALLED="https://kton.dev/v/installed"

WORK="$HIER/.work"
LOG="$WORK/schritte.jsonl"
rm -rf "$WORK"; mkdir -p "$WORK"; : > "$LOG"
( cd ../raute-zwei-backends/pkgtool && go build -o "$WORK/pkgtool" . )
( cd "$KTONPKG" && go build -o "$WORK/scope" ./cmd/scope )
PAKET="$WORK/paket"
source ../raute-zwei-backends/schritt.sh

config() { # name [extra-python]
  python3 - "$REPO/examples/lib/cockpit.config.json" "docker.io/$IMAGE" "${2:-}" <<'PY'
import json, sys
c = json.load(open(sys.argv[1]))
c["execution"] = {"image": "oci://" + sys.argv[2], "network": False, "entrypoint": "Rscript"}
c["claims"]["allowedTemplates"] = ["reproduces", "installed"]
exec(sys.argv[3])
print(json.dumps(c, indent=2))
PY
}
installed_template() {
  cat > "$1/templates/installed.json" <<EOF2
{
  "name": "installed",
  "target": "either",
  "predicate": "$INSTALLED",
  "fields": {
    "package": {"type": "ref",    "required": true},
    "seal":    {"type": "ref",    "required": true},
    "head":    {"type": "ref",    "required": true},
    "ray":     {"type": "ref",    "required": true}
  }
}
EOF2
}
commit() { git -C "$REPO_DIR" add -A && git -C "$REPO_DIR" -c user.name=example -c user.email=example@cockpit.local commit -qm "$1" && git -C "$REPO_DIR" push -q; }

# ===================================================================================================
echo "=== AUTORIN"
participant "$WORK/autorin/repo" "$(config)" raute-autorin >/dev/null
REPO_DIR="$WORK/autorin/repo"
AUTORIN="$REPO_DIR"
cp -r "$RAUTE" "$REPO_DIR/packages/raute" 2>/dev/null || { mkdir -p "$REPO_DIR/packages"; cp -r "$RAUTE" "$REPO_DIR/packages/raute"; }
commit "das Raute-Paket im alten Format, als Quelle des Rays"

echo; echo "=== Referenzlauf: der Ray mit den Testdaten"
schritt referenz referenz:plan ktonpkg "" \
  "Der Ray mit den mitgelieferten Testdaten als Bindung — dieser Lauf erzeugt die Referenz (FS-11)" \
  "$WORK/pkgtool plan packages/raute work/raute"
cp "$WORK/last.out" "$WORK/plan-referenz.json"
laufen "$WORK/plan-referenz.json" referenz

echo; echo "=== Renderlauf: die Dokumentation"
mkdir -p "$REPO_DIR/work/doku"
cp -r "$RAUTE/requirements" "$RAUTE/specification" "$REPO_DIR/work/doku/"
cp "$HIER/dokumentation.R" "$REPO_DIR/work/doku/"
DOKU_IN=$(cd "$REPO_DIR" && python3 -c 'import json,glob; print(json.dumps(sorted(glob.glob("work/doku/requirements/*.md")+glob.glob("work/doku/specification/*.md"))+["work/doku/dokumentation.R"], ensure_ascii=False))')
schritt render render:doku cockpit "" \
  "URS und FS im gepinnten Image zu einem Dokument rendern und als Lauf veröffentlichen" \
  "./bin/cockpit publish '{\"cmd\":\"cd work/doku && Rscript dokumentation.R dokumentation.md\",\"inputs\":$DOKU_IN,\"outputs\":[\"work/doku/dokumentation.md\"]}'"
RENDER=$(last 'd["fotonId"]')

echo; echo "=== Das Paket bauen"
schritt paket paket:ray ktonpkg "" \
  "Paket-Seed raute, Revision 1: der Ray als Potentiale, Testdaten, Spectrum, Bedingungen — ohne unbelegte Referenzen" \
  "$WORK/scope from-bundle packages/raute $PAKET 1 --only-measured --key keys/session-1-claims.key"
for step in $(python3 -c 'import json,sys; print(" ".join(s["id"] for s in json.load(open(sys.argv[1]))["steps"]))' "$WORK/plan-referenz.json"); do
  RUN=$(python3 -c 'import json,sys
for l in open(sys.argv[1]):
    s = json.loads(l)
    if s["id"] == "referenz:" + sys.argv[2]: print(s["result"]["fotonId"])' "$LOG" "$step")
  schritt paket "paket:referenz:$step" ktonpkg "" \
    "Den Referenzlauf von $step samt Ausgaben ins Paket" \
    "$WORK/scope add-run $PAKET 1 $RUN --registry registry/plankton --root . --pub registry/keys/session-1.pub --kind reference --step $step --key keys/session-1-claims.key"
done
schritt paket paket:render ktonpkg "" \
  "Den Renderlauf samt Dokument ins Paket" \
  "$WORK/scope add-run $PAKET 1 $RENDER --registry registry/plankton --root . --pub registry/keys/session-1.pub --kind render --key keys/session-1-claims.key"
schritt paket paket:seal ktonpkg "" \
  "Revision 1 versiegeln: ihr Kopf in die Kette des Pakets" \
  "$WORK/scope seal $PAKET 1 --key keys/session-1-claims.key"
schritt paket paket:inspect ktonpkg "" \
  "Das fertige Paket: Revisionen, Ray-Id, Siegel, Strukturbefund" \
  "$WORK/scope inspect $PAKET"
python3 - "$WORK/last.out" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
assert not d["problems"], d["problems"]
print("   Paket", d["package"][:23], "Ray", d["revisions"][0]["rayId"][:23], "ohne Befund")
PY

# ===================================================================================================
echo; echo "=== USER"
AUTOR_KEYS=$(cd "$PAKET/keys" && ls *.pub)
participant "$WORK/anwender/repo" "$(config anwender "c['trust']['tiers']['autorin'] = ['registry/keys/autorin-' + k for k in '''$AUTOR_KEYS'''.split()]")" raute-anwender >/dev/null
REPO_DIR="$WORK/anwender/repo"
for k in $AUTOR_KEYS; do cp "$PAKET/keys/$k" "$REPO_DIR/registry/keys/autorin-$k"; done
installed_template "$REPO_DIR"
commit "der Autorin vertrauen (ihre Schlüssel als Stufe autorin); Template installed"

schritt install install:records ktonpkg "S6: einspielen soll ein Cockpit-Verb sein" \
  "Das Paket prüfen und seine Records in die eigenen Register übernehmen; die Ansicht nach packages/raute@1" \
  "$WORK/scope install $PAKET 1 registry/plankton registry/nekton packages/raute@1"
INST="$WORK/install.json"; cp "$WORK/last.out" "$INST"
commit "Paket raute@1 eingespielt"

schritt install install:seed cockpit "" \
  "Den eigenen Scope für dieses Paket öffnen — der Seed trägt nur den Namen" \
  "./bin/cockpit scope seed raute@1"
SEED=$(grep -o 'sha256:[0-9a-f]*' "$WORK/last.out" | head -1)
python3 - "$REPO_DIR/cockpit.config.json" "$SEED" <<'PY'
import json, sys
c = json.load(open(sys.argv[1])); c["claims"]["scopes"] = {"raute": sys.argv[2]}
json.dump(c, open(sys.argv[1], "w"), indent=2)
PY
commit "Scope raute konfiguriert"

FIELDS=$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(json.dumps({k: d[k] for k in ("package","seal","head","ray")}))' "$INST")
REV=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["revision"])' "$INST")
schritt install install:claim cockpit "" \
  "Festhalten, was installiert ist: die Revision, und das Siegel, auf das sich alles Weitere bezieht" \
  "./bin/cockpit say '{\"subject\":\"$REV\",\"template\":\"installed\",\"fields\":$FIELDS,\"scope\":\"raute\"}'"

echo; echo "=== Nachprüfen: der Ray mit den Testdaten des Pakets, beim User"
schritt pruefen pruefen:plan ktonpkg "" \
  "Derselbe Plan wie bei der Autorin, aus der installierten Ansicht" \
  "$WORK/pkgtool plan packages/raute@1 work/raute"
cp "$WORK/last.out" "$WORK/plan-pruefen.json"
laufen "$WORK/plan-pruefen.json" pruefen
python3 - "$LOG" <<'PY'
import json, sys
steps = [json.loads(l) for l in open(sys.argv[1])]
ref = {s["id"].split(":", 1)[1]: s["result"]["fotonId"] for s in steps if s["id"].startswith("referenz:") and s["id"] != "referenz:plan"}
for s in steps:
    if s["id"].startswith("pruefen:") and s["id"] != "pruefen:plan":
        step = s["id"].split(":", 1)[1]
        assert s["result"]["fotonId"] == ref[step], f"{step}: ein zweiter Record statt des Referenzlaufs"
        assert s["result"].get("coSigned"), f"{step}: der Lauf des Users steht nicht am Referenzlauf"
        print(f"   {step}: derselbe Record wie bei der Autorin, vom User mitsigniert")
PY
python3 - "$LOG" > "$WORK/reproduktionen.tsv" <<'PY'
import json, sys
steps = [json.loads(l) for l in open(sys.argv[1])]
ref = {s["id"].split(":", 1)[1]: s["result"] for s in steps if s["id"].startswith("referenz:") and s["id"] != "referenz:plan"}
for s in steps:
    if s["id"].startswith("pruefen:") and s["id"] != "pruefen:plan":
        step = s["id"].split(":", 1)[1]
        for path, h in s["result"]["outputHashes"].items():
            print("\t".join([step, ref[step]["fotonId"], ref[step]["outputHashes"][path], path, s["result"]["fotonId"]]))
PY
while IFS=$'\t' read -r step aid ahash path uid; do
  schritt pruefen "pruefen:reproduces:$step" cockpit "" \
    "Reproduziert der eigene Lauf von $step den Referenzlauf der Autorin? Das Cockpit rechnet die Stufe selbst" \
    "./bin/cockpit say '{\"subject\":\"$aid\",\"template\":\"reproduces\",\"subjectOutputHash\":\"$ahash\",\"reproducedOutput\":\"$path\",\"reproducedFotonId\":\"$uid\",\"scope\":\"raute\"}'"
done < "$WORK/reproduktionen.tsv"

echo; echo "=== Eigene Daten"
cp "$HIER/../raute-zwei-backends/eigene-daten.csv" "$REPO_DIR/data/eigene-daten.csv"
commit "eigener Datensatz"
schritt eigen eigen:plan ktonpkg "" \
  "Löcher binden: dataset = eigener Datensatz, anzahl = 3" \
  "$WORK/pkgtool plan packages/raute@1 work/eigen --bind dataset=data/eigene-daten.csv --bind anzahl=3"
cp "$WORK/last.out" "$WORK/plan-eigen.json"
laufen "$WORK/plan-eigen.json" eigen
schritt eigen eigen:lineage cockpit "" \
  "Vom eigenen Ergebnis zurück zum Datensatz, gegen die Vertrauensstufen dieses Repos verifiziert" \
  "./bin/cockpit ask '{\"query\":\"lineage\",\"ref\":\"work/eigen/zusammenführung/ergebnis.txt\"}'"

# ===================================================================================================
echo; echo "=== Für kton-web"
python3 - "$AUTORIN" "$REPO_DIR" "$PAKET" > "$WORK/names.json" <<'PY'
import hashlib, json, sys, pathlib
autorin, anwender, paket = map(pathlib.Path, sys.argv[1:])
def kid(p): return hashlib.sha256(bytes.fromhex(p.read_text().strip())).hexdigest()[:16]
names = {}
for who, repo in (("Autorin", autorin), ("User", anwender)):
    names[kid(repo / "keys/session-1.pub")] = who + " (Läufe)"
    names[kid(repo / "keys/session-1-claims.pub")] = who + " (Aussagen)"
print(json.dumps(names, ensure_ascii=False, indent=1))
PY
schritt web web:paket ktonpkg "" "Nur das Paket" \
  "$WORK/scope union $KTON_WEB/data/raute-paket/data $PAKET --names $WORK/names.json"
schritt web web:anwender ktonpkg "" "Das Repo des Users nach Installation, Nachprüfung und eigenem Lauf" \
  "$WORK/scope union $KTON_WEB/data/raute-anwender/data . --names $WORK/names.json"

python3 - "$LOG" <<'PY'
import json, sys
steps = [json.loads(l) for l in open(sys.argv[1])]
bad = [s["id"] for s in steps if not s["ok"]]
print(f"\n{len(steps)} Schritte, {len(bad)} fehlgeschlagen" + (": " + ", ".join(bad) if bad else ""))
sys.exit(1 if bad else 0)
PY
