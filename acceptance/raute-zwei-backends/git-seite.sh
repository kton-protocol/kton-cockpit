#!/usr/bin/env bash
# Die git-Seite des Vergleichs: jam-r qualifizieren, die Raute laden, ihre Löcher füllen, laufen
# lassen — in einem git-Teilnehmerrepo, mit dem Cockpit, wo es das heute schon kann.
#
# Jeder Schritt wird mit seinem EXAKTEN Aufruf, seinem Akteur und seinem Ergebnis nach
# results/git.json geschrieben (Format: results/SCHEMA.md). Wo nicht das Cockpit handelt, obwohl es
# das im Zielbild soll, steht die Lücke daneben (ADR-005: S4 improve-Modus, S5 Potentials und
# Realisierung, S6 Spectra und Pakete, K3 Spectrum-Prüfung als Kernel-Bibliothek).
#
# Braucht docker, go, python3 und die beiden Pakete aus dem improveGo-Workstream.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
EXNAME=raute-zwei-backends
source ../../examples/lib/common.sh

IMPROVEGO_WS="${IMPROVEGO_WS:-/mnt/c/dev/workstreams/active/improveGo}"
JAMR="$IMPROVEGO_WS/tools/jam-r"
RAUTE="$IMPROVEGO_WS/examples/raute/paket"
IMAGE="scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a"
OQ="https://scinteco.com/ns/qualification/v1#operationalQualification"

WORK="$HIER/.work"
LOG="$WORK/schritte.jsonl"
rm -rf "$WORK"; mkdir -p "$WORK"; : > "$LOG"
( cd pkgtool && go build -o "$WORK/pkgtool" . )

source "$HIER/schritt.sh"


# ---------------------------------------------------------------------------------------------------
echo "=== setup"
CFG=$(python3 - "$REPO/examples/lib/cockpit.config.json" "docker.io/$IMAGE" <<'PY'
import json, sys
c = json.load(open(sys.argv[1]))
c["execution"] = {"image": "oci://" + sys.argv[2], "network": False, "entrypoint": "Rscript"}
c["claims"]["allowedTemplates"] = ["reproduces", "working-on", "oq-executed", "derived-from", "reviewed"]
c["reproduction"] = {"requiredLevel": "L1"}
print(json.dumps(c, indent=2))
PY
)
participant "$WORK/repo" "$CFG" "$EXNAME" >/dev/null
REPO_DIR="$WORK/repo"
cat > "$REPO_DIR/templates/oq-executed.json" <<EOF
{
  "name": "oq-executed",
  "target": "either",
  "predicate": "$OQ",
  "fields": {
    "tool":       {"type": "string", "required": true},
    "verdict":    {"type": "string", "required": true},
    "candidates": {"type": "string", "required": true}
  }
}
EOF
git -C "$REPO_DIR" add templates && git -C "$REPO_DIR" -c user.name=example -c user.email=example@cockpit.local commit -qm "template oq-executed" && git -C "$REPO_DIR" push -q

schritt setup setup:version cockpit "" \
  "Welches Cockpit, welcher Kernel" \
  "./bin/cockpit version"
schritt setup setup:doctor cockpit "" \
  "Teilnehmerrepo: Konfiguration und Bindung an den origin-Remote prüfen" \
  "./bin/cockpit doctor"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Werkzeug einrichten"
schritt tool-install tool-install:pin script "" \
  "Das Werkzeug ist das per Digest gepinnte Image in execution.image — Einrichten heißt: es liegt lokal vor" \
  "docker pull -q $IMAGE && python3 -c 'import json; print(json.dumps(json.load(open(\"cockpit.config.json\"))[\"execution\"]))'"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Werkzeug qualifizieren"
mkdir -p "$REPO_DIR/packages" && cp -r "$JAMR/paket" "$REPO_DIR/packages/jam-r"
schritt tool-qualify tool-qualify:inspect ktonpkg "S6: cockpit kennt kton-Pakete noch nicht" \
  "Das Werkzeugpaket öffnen: Identität, Prüfbefund, angebotenes Spectrum" \
  "$WORK/pkgtool inspect packages/jam-r"
SPECTRUM=$(last 'd["spectrumId"]')
MEMBERS=$(last '" ".join(m["name"] for m in d["spectrum"]["members"])')

schritt tool-qualify tool-qualify:referenzen kernel-cli "K3/S6: Referenz-Fotons des Spectrums übernimmt heute die Kernel-CLI" \
  "Die Referenz-Fotons des Herstellers ins eigene Register übernehmen — ohne sie ist das Spectrum eine Liste unbelegter Hashes" \
  "./bin/plankton add $JAMR/spectrum/registry/objects/sha256/*.json --registry registry/plankton"

CANDIDATES=()
for name in $MEMBERS; do
  file=$(python3 -c 'import json,sys; print(next(m["file"] for m in json.load(open(sys.argv[1])) if m["name"]==sys.argv[2]))' \
         "$JAMR/spectrum/members/members.json" "$name")
  out=$(python3 -c 'import json,sys; print(next(m["out"] for m in json.load(open(sys.argv[1])) if m["name"]==sys.argv[2]))' \
         "$JAMR/spectrum/members/members.json" "$name")
  mkdir -p "$REPO_DIR/work/jam-r/$name" && cp "$REPO_DIR/packages/jam-r/members/$file" "$REPO_DIR/work/jam-r/$name/"
  schritt tool-qualify "tool-qualify:member:$name" cockpit "" \
    "Spectrum-Member $name im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen" \
    "./bin/cockpit publish '{\"cmd\":\"cd work/jam-r/$name && Rscript $file\",\"inputs\":[\"work/jam-r/$name/$file\"],\"outputs\":[\"work/jam-r/$name/$out\"]}'"
  CANDIDATES+=("--candidate $name=$(last "d['outputHashes']['work/jam-r/$name/$out']")")
done

spectrum_check() { # id titel
  schritt tool-qualify "$1" kernel-cli "K3: die Spectrum-Prüfung gibt es nur als plankton-Kommando, nicht als Bibliothek — das Cockpit kann sie nicht aufrufen" \
    "$2" \
    "PLANKTON_DIR=registry/plankton ./bin/plankton spectrum check packages/jam-r/kton/spectrum.json ${CANDIDATES[*]}"
  VERDICT_TEXT=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readlines()[-1])["result"].get("text",""))' "$LOG")
  UNFULFILLED=$(python3 -c 'import sys; print(" ".join(l.split()[0] for l in sys.argv[1].splitlines() if "not fulfilled" in l))' "$VERDICT_TEXT")
}
spectrum_check tool-qualify:check "Die eigenen Ausgaben gegen das Spectrum halten (identisch oder über den Normalisierer)"

# Ein Member, der nicht bitgleich ist, zählt nur über den Normalisierer des Spectrums: dessen
# Potential muss auf die Referenz UND auf die eigene Ausgabe angewandt und beides aufgezeichnet sein,
# mit derselben Protokoll-Ref wie das Potential (kton §10). `cockpit publish` kann das nicht — es
# schreibt sein Image ins Protokoll, und die Ref wäre eine andere. Also: ktonpkg rechnet die
# Identität, die Kernel-CLI zeichnet auf, das Skript führt aus.
if [ -n "$UNFULFILLED" ]; then
  schritt tool-qualify tool-qualify:normalizer-potential ktonpkg "S5: ein Potential aufzeichnen soll cockpit publish {kind: potential} sein" \
    "Das Normalisierer-Potential des Spectrums (Ray-Step normalisieren, Loch eingabe) aufzeichnen" \
    "$WORK/pkgtool foton packages/jam-r normalisieren > $WORK/potential.json && ./bin/plankton author $WORK/potential.json keys/session-1.key $WORK/potential.dsse && ./bin/plankton add $WORK/potential.dsse --registry registry/plankton --print-id"
  for name in $UNFULFILLED; do
    out=$(python3 -c 'import json,sys; print(next(m["out"] for m in json.load(open(sys.argv[1])) if m["name"]==sys.argv[2]))' \
           "$JAMR/spectrum/members/members.json" "$name")
    for seite in referenz eigen; do
      d="work/jam-r/normalisiert/$name/$seite"
      mkdir -p "$REPO_DIR/$d"
      cp "$REPO_DIR/packages/jam-r/normalizer/fences.R" "$REPO_DIR/$d/"
      if [ "$seite" = referenz ]; then cp "$REPO_DIR/packages/jam-r/reference/$out" "$REPO_DIR/$d/eingabe"
      else cp "$REPO_DIR/work/jam-r/$name/$out" "$REPO_DIR/$d/eingabe"; fi
      schritt tool-qualify "tool-qualify:normalize:$name:$seite" script "S5: das Potential realisieren (Loch binden, im gepinnten Image laufen lassen) soll das Cockpit" \
        "Normalisierer auf die $seite-Ausgabe von $name anwenden, im gepinnten jam-r-Image" \
        "docker run --rm --network none --user \$(id -u):\$(id -g) -v \$PWD/$d:/w -w /w $IMAGE sh -c 'Rscript fences.R eingabe normalisiert.out'"
      schritt tool-qualify "tool-qualify:record:$name:$seite" kernel-cli "S5: die Realisierung aufzeichnen soll cockpit publish {from, bindings} sein" \
        "Die Realisierung als Foton aufzeichnen — Protokoll aus dem Potential, Loch eingabe an die Bytes gebunden" \
        "$WORK/pkgtool foton packages/jam-r normalisieren --bind eingabe=\$(./bin/plankton hash $d/eingabe) --output normalisiert.out=$d/normalisiert.out > $WORK/r.json && ./bin/plankton author $WORK/r.json keys/session-1.key $WORK/r.dsse && ./bin/plankton add $WORK/r.dsse --registry registry/plankton --print-id"
    done
  done
  spectrum_check tool-qualify:check-normalized "Erneut gegen das Spectrum halten, jetzt mit aufgezeichneter Normalisierung"
fi
VERDICT=$(python3 - "$VERDICT_TEXT" <<'PY'
import sys
t = sys.argv[1]
lines = [l for l in t.splitlines() if "fulfilled" in l]
if not lines or any("not fulfilled" in l for l in lines): print("failed")
elif any("via" in l for l in lines): print("L1")
else: print("L0")
PY
)
echo "   Befund: $VERDICT"

schritt tool-qualify tool-qualify:claim cockpit "" \
  "Den Befund als signierten Claim festhalten (dasselbe OQ-Prädikat wie auf der improve-Seite)" \
  "./bin/cockpit say '{\"subject\":\"$SPECTRUM\",\"template\":\"oq-executed\",\"fields\":{\"tool\":\"oci://docker.io/$IMAGE\",\"verdict\":\"$VERDICT\",\"candidates\":\"${CANDIDATES[*]}\"}}'"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Workflow laden"
cp -r "$RAUTE" "$REPO_DIR/packages/raute"
schritt load load:inspect ktonpkg "S6: cockpit kennt kton-Pakete noch nicht" \
  "Das Workflow-Paket öffnen und prüfen: Identität, Ray, Löcher, Bedingungen" \
  "$WORK/pkgtool inspect packages/raute"
REQUIRED=$(last 'd["requires"][0]["spectrum"]')

schritt require require:spectrum script "S6: die Bedingung eines Pakets gegen qualifizierte Werkzeuge halten gehört ins Cockpit" \
  "Verlangt das Paket ein Werkzeug, das hier qualifiziert ist?" \
  "python3 -c 'import json,sys; ok=sys.argv[1]==sys.argv[2] and sys.argv[3]!=\"failed\"; print(json.dumps({\"required\":sys.argv[1],\"qualified\":sys.argv[2],\"verdict\":sys.argv[3],\"fulfilled\":ok})); sys.exit(0 if ok else 1)' $REQUIRED $SPECTRUM $VERDICT"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Löcher füllen"
schritt refuse-unbound refuse-unbound:plan ktonpkg "S5: im Zielbild lehnt cockpit publish {from, bindings} das selbst ab" \
  "Ohne Bindungen (und ohne Testdatensatz als Vorgabe) darf der Workflow nicht anlaufen" \
  "$WORK/pkgtool plan packages/raute work/raute --no-defaults" expect-fail

cp "$HIER/eigene-daten.csv" "$REPO_DIR/data/eigene-daten.csv"
schritt bind bind:plan ktonpkg "S5: Löcher binden und realisieren soll cockpit publish {from, bindings} sein" \
  "Löcher binden: dataset = eigener Datensatz, anzahl = 3; daraus die Steps in Reihenfolge" \
  "$WORK/pkgtool plan packages/raute work/raute --bind dataset=data/eigene-daten.csv --bind anzahl=3"
cp "$WORK/last.out" "$WORK/plan.json"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Laufen lassen"
laufen "$WORK/plan.json" run

# ---------------------------------------------------------------------------------------------------
echo; echo "=== Nachprüfen"
schritt verify verify:lineage cockpit "" \
  "Vom Ergebnis zurück zum Datensatz: die Linie, gegen die Vertrauensstufen dieses Repos verifiziert" \
  "./bin/cockpit ask '{\"query\":\"lineage\",\"ref\":\"work/raute/zusammenführung/ergebnis.txt\"}'"

# ---------------------------------------------------------------------------------------------------
python3 - "$LOG" "$HIER/results/git.json" "$SPECTRUM" "$VERDICT" "$VERDICT_TEXT" "$HIER/eigene-daten.csv" <<'PY'
import datetime, hashlib, json, sys
log, dest, spectrum, verdict, verdict_text, csv = sys.argv[1:]
steps = [json.loads(l) for l in open(log)]
outputs, fotons, members = {}, {}, {}
for s in steps:
    if s["id"].startswith("run:") and s["ok"]:
        step = s["id"][4:]
        for path, h in s["result"].get("outputHashes", {}).items():
            outputs[step + "/" + path.rsplit("/", 1)[1]] = h
        fotons[step] = s["result"].get("fotonId")
    if s["id"].startswith("tool-qualify:member:") and s["ok"]:
        name = s["id"].rsplit(":", 1)[1]
        members[name] = {"output": next(iter(s["result"].get("outputHashes", {}).values()), "")}
spec = next(s for s in steps if s["id"] == "tool-qualify:inspect")["result"]["spectrum"]
for m in spec["members"]:
    e = members.setdefault(m["name"], {})
    e["reference"] = m["output"]
    line = next((l for l in verdict_text.splitlines() if l.strip().startswith(m["name"] + " ")), "")
    e["match"] = "identical" if "identical" in line else "via-normalizer" if "via" in line else "differs"
json.dump({
    "side": "git",
    "ranAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "bindings": {"dataset": "sha256:" + hashlib.sha256(open(csv, "rb").read()).hexdigest(), "anzahl": "3"},
    "steps": steps,
    "qualification": {"tool": "jam-r", "spectrum": spectrum, "verdict": verdict, "members": members},
    "outputs": outputs,
    "fotons": fotons,
}, open(dest, "w"), indent=2, ensure_ascii=False)
print(f"\nresults/git.json: {len(steps)} Schritte, {sum(1 for s in steps if s['ok'])} ok")
PY
