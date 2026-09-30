#!/usr/bin/env bash
# ADR-006, Schritt 2: aus Ausführungen, die schon liefen, das Potential zurückgewinnen.
#
# Baut auf dem Teilnehmerrepo auf, das git-seite.sh hinterlassen hat (dort lief die Raute einmal,
# mit eigene-daten.csv und anzahl=3). Hier:
#
#   1. ein zweiter Lauf, mit den Vorgaben des Pakets (dessen Testdaten, anzahl=1) — erst zwei Läufe
#      zeigen, welche Stelle der Befehlszeile ein Parameter sein kann;
#   2. die Ausführungen auswählen: vom Ergebnis jedes Laufs rückwärts, nur verifizierte;
#   3. vorschlagen lassen, ausdrücklich wählen, extrahieren;
#   4. das Extrahierte mit dem Original-Paket vergleichen;
#   5. das Extrahierte laufen lassen — auf seinen Vorgaben muss es sein Spectrum treffen, und mit
#      den Daten des zweiten Laufs gebunden dessen Ergebnis.
#
# Schreibt results/extraktion.json und EXTRAKTION.md.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
REPO_DIR="$HIER/.work/repo"
[ -x "$REPO_DIR/bin/cockpit" ] || { echo "erst ./git-seite.sh laufen lassen — hier wird auf seinem Repo aufgebaut" >&2; exit 1; }
WORK="$HIER/.work/extraktion"
LOG="$WORK/schritte.jsonl"
rm -rf "$WORK"; mkdir -p "$WORK"; : > "$LOG"
( cd pkgtool && go build -o "$WORK/pkgtool" . )
source "$HIER/schritt.sh"
PKG="$WORK/pkgtool"
JAMR_SPECTRUM=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["qualification"]["spectrum"])' results/git.json)

# ---------------------------------------------------------------------------------------------------
echo "=== zweiter Lauf, auf den Vorgaben des Pakets"
schritt second-run second-run:plan ktonpkg "" \
  "Die Raute mit ihren Vorgaben binden: Testdatensatz des Pakets, anzahl=1" \
  "$PKG plan packages/raute work/raute-2"
cp "$WORK/last.out" "$WORK/plan-2.json"
laufen "$WORK/plan-2.json" second-run

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Ausführungen auswählen"
for lauf in raute raute-2; do
  schritt select "select:$lauf" cockpit "" \
    "Vom Ergebnis des Laufs $lauf rückwärts: die Linie, nur gegen die Vertrauensstufen verifizierte Fotons" \
    "./bin/cockpit ask '{\"query\":\"lineage\",\"ref\":\"work/$lauf/zusammenführung/ergebnis.txt\"}' --field included"
  cp "$WORK/last.out" "$WORK/ids-$lauf.json"
done
python3 - "$WORK" <<'PY'
import json, sys
w = sys.argv[1]
ids = sorted(set(json.load(open(f"{w}/ids-raute.json")) + json.load(open(f"{w}/ids-raute-2.json"))))
open(f"{w}/ids.txt", "w").write("\n".join(ids) + "\n")
PY
schritt select select:records cockpit "" \
  "Jede ausgewählte Ausführung im Einzelnen: Befehl, Umgebung, Ein- und Ausgaben (je ein cockpit ask record)" \
  "python3 $HIER/records.py $WORK/ids.txt $WORK/records.json"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== vorschlagen, wählen, extrahieren"
schritt propose propose ktonpkg "S5/S6: im Zielbild cockpit ask {query: ray}" \
  "Vorschlag: Läufe, Steps, Verdrahtung und die Kandidaten — beobachtet, nicht entschieden" \
  "$PKG propose $WORK/records.json"
cp "$WORK/last.out" "$WORK/proposal.json"

# Die Auswahl ist ausdrücklich und steht hier im Skript, wo man sie lesen kann. Referenz ist der
# Lauf mit eigene-daten.csv: sein Ergebnis ist der Endpunkt, von dem aus gewählt wird.
REF_END=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["outputs"]["zusammenführung/ergebnis.txt"])' results/git.json)
python3 - "$WORK/proposal.json" "$REF_END" "$JAMR_SPECTRUM" "$WORK/choice.json" <<'PY'
import json, sys
p, ref_end, spectrum, dest = json.load(open(sys.argv[1])), sys.argv[2], sys.argv[3], sys.argv[4]
ref = next(r["id"] for r in p["runs"] if ref_end in r["endpoints"])
choice = {
    "name": "raute-extrahiert",
    "reference": ref,
    "holes":  [{"name": "dataset", "step": "aufbereitung", "slot": "dataset.csv"}],
    "params": [{"name": "anzahl", "step": "zweig-b", "position": 2}],
    "requires": [{"kind": "tool", "spectrum": spectrum, "name": "jam-r"}],
}
json.dump(choice, open(dest, "w"), indent=2, ensure_ascii=False)
print(json.dumps(choice, ensure_ascii=False))
PY
schritt choose choose script "S5/S6: im Zielbild die Auswahl in cockpit publish {kind: ray, choose}" \
  "Ausdrücklich gewählt: dataset als Loch, anzahl als Parameter, Referenz der Lauf mit eigene-daten.csv" \
  "cat $WORK/choice.json"

schritt extract extract ktonpkg "S5/S6: im Zielbild cockpit publish {kind: ray}" \
  "Extrahieren: Ray-Paket mit Testdaten und Spectrum aus dem Referenzlauf, prov:wasDerivedFrom signiert" \
  "$PKG extract $WORK/records.json $WORK/choice.json packages/raute-extrahiert --sign $WORK/extraktion.key"

schritt extract extract:inspect ktonpkg "" \
  "Das extrahierte Paket öffnen und prüfen" \
  "$PKG inspect packages/raute-extrahiert"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== mit dem Original vergleichen"
schritt compare compare script "" \
  "Struktur neben das Original-Paket halten: Steps, Verdrahtung, Code, Löcher, Bedingung" \
  "python3 $HIER/paketvergleich.py packages/raute packages/raute-extrahiert"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== das Extrahierte laufen lassen"
schritt roundtrip roundtrip:plan ktonpkg "" \
  "Auf seinen Vorgaben binden — das sind die Daten des Referenzlaufs" \
  "$PKG plan packages/raute-extrahiert work/raute-x"
cp "$WORK/last.out" "$WORK/plan-x.json"
laufen "$WORK/plan-x.json" roundtrip
CANDS=$(python3 - "$LOG" <<'PY'
import json, sys
for l in open(sys.argv[1]):
    s = json.loads(l)
    if s["phase"] == "roundtrip" and s["id"] != "roundtrip:plan":
        step = s["id"].split(":", 1)[1]
        for h in s["result"].get("outputHashes", {}).values():
            print(f"--candidate {step}={h}", end=" ")
PY
)
schritt roundtrip roundtrip:check kernel-cli "K3: Spectrum-Prüfung nur als Kernel-CLI" \
  "Die Ausgaben gegen das Spectrum des extrahierten Pakets halten" \
  "PLANKTON_DIR=registry/plankton ./bin/plankton spectrum check packages/raute-extrahiert/kton/spectrum.json $CANDS"

schritt rebind rebind:plan ktonpkg "" \
  "Anders binden: die Daten des zweiten Laufs (Testdatensatz des Originals, anzahl=1)" \
  "$PKG plan packages/raute-extrahiert work/raute-y --bind dataset=packages/raute/testdaten/dataset.csv --bind anzahl=1"
cp "$WORK/last.out" "$WORK/plan-y.json"
laufen "$WORK/plan-y.json" rebind

# ---------------------------------------------------------------------------------------------------
python3 - "$LOG" "$HIER" <<'PY'
import datetime, json, sys
log, hier = sys.argv[1], sys.argv[2]
steps = [json.loads(l) for l in open(log)]
def outs(phase):
    o = {}
    for s in steps:
        if s["phase"] == phase and s["result"].get("outputHashes"):
            step = s["id"].split(":", 1)[1]
            for p, h in s["result"]["outputHashes"].items():
                o[step + "/" + p.rsplit("/", 1)[1]] = h
    return o
git = json.load(open(f"{hier}/results/git.json"))
second, x, y = outs("second-run"), outs("roundtrip"), outs("rebind")
proposal = next(s for s in steps if s["id"] == "propose")["result"]
compare = next(s for s in steps if s["id"] == "compare")["result"]
check = next(s for s in steps if s["id"] == "roundtrip:check")
res = {
    "ranAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "steps": steps,
    "proposal": {"runs": len(proposal.get("runs", [])), "steps": [s["id"] for s in proposal.get("steps", [])],
                 "files": proposal.get("files", []), "params": proposal.get("params", []),
                 "conflicts": proposal.get("conflicts", [])},
    "compare": compare,
    "roundtrip": {"outputs": x, "sameAsReference": x == git["outputs"], "spectrumCheck": check["ok"]},
    "rebind": {"outputs": y, "sameAsSecondRun": y == second},
}
json.dump(res, open(f"{hier}/results/extraktion.json", "w"), indent=2, ensure_ascii=False)

z = ["# Extraktion — aus Ausführungen das Potential\n",
     "ADR-006, Schritt 2. Zwei Läufe der Raute im git-Teilnehmerrepo (eigene Daten mit anzahl=3, "
     "Testdaten mit anzahl=1) → Vorschlag → ausdrückliche Wahl → Ray-Paket → laufen lassen. "
     "Erzeugt von `extraktion.sh`.\n",
     "## Vorschlag\n",
     f"{res['proposal']['runs']} Läufe, Steps: {', '.join(res['proposal']['steps'])}. "
     f"Konflikte: {len(res['proposal']['conflicts'])}.\n",
     "| Kandidat | Art | Werte je Lauf | gewählt |", "|---|---|---|---|"]
for f in res["proposal"]["files"]:
    chosen = "Loch `dataset`" if (f["step"], f["slot"]) == ("aufbereitung", "dataset.csv") else "fest"
    kind = "Code (Kommandodatei)" if f.get("commandFile") else "Datei"
    vals = ", ".join(f"{r}: `{h[:19]}…`" for r, h in sorted(f["hashes"].items()))
    z.append(f"| `{f['step']}/{f['slot']}` | {kind} | {vals} | {chosen} |")
for p in res["proposal"]["params"]:
    vals = ", ".join(f"{r}: `{v}`" for r, v in sorted(p["values"].items()))
    z.append(f"| `{p['step']}` Stelle {p['position']} | Befehlszeile | {vals} | Parameter `anzahl` |")
z.append("\nNichts davon ist geraten: Dateikandidaten sind Eingaben, die keine ausgewählte Ausführung "
         "erzeugt hat; der einzige Parameterkandidat ist die Stelle, an der die beiden Läufe verschieden waren.\n")
z.append("## Neben dem Original\n")
z.append("| | Original | extrahiert | |\n|---|---|---|---|")
for row in compare.get("rows", []):
    z.append(f"| {row['what']} | {row['original']} | {row['extracted']} | {'✅' if row['same'] else '≠ ' + row.get('why', '')} |")
z.append("\n## Laufen lassen\n")
z.append(f"- Auf den Vorgaben (Daten des Referenzlaufs): Ausgaben "
         f"{'**gleich**' if res['roundtrip']['sameAsReference'] else '**verschieden**'} wie im Referenzlauf; "
         f"Spectrum-Prüfung {'bestanden' if res['roundtrip']['spectrumCheck'] else 'nicht bestanden'}.")
z.append(f"- Anders gebunden (Testdaten des Originals, anzahl=1): Ausgaben "
         f"{'**gleich**' if res['rebind']['sameAsSecondRun'] else '**verschieden**'} wie im zweiten Lauf.\n")
z.append("## Schritte\n")
z.append("| | Schritt | Akteur | Aufruf | Lücke |\n|---|---|---|---|---|")
for s in steps:
    cmd = s["command"].replace("|", "\\|").replace("\n", " ")
    if len(cmd) > 160:
        cmd = cmd[:157] + "…"
    z.append(f"| {'✅' if s['ok'] else '❌'} | {s['title']} | {s['actor']} | `{cmd}` | {s['gap']} |")
open(f"{hier}/EXTRAKTION.md", "w").write("\n".join(z) + "\n")
print(f"\nresults/extraktion.json, EXTRAKTION.md: {sum(s['ok'] for s in steps)}/{len(steps)} Schritte ok; "
      f"Rundlauf gleich: {res['roundtrip']['sameAsReference']}; anders gebunden gleich: {res['rebind']['sameAsSecondRun']}")
PY
