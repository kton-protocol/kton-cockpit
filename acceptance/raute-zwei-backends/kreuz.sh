#!/usr/bin/env bash
# ADR-006, Schritt 3: über Kreuz. Die improve-Seite (improvego cmd/kreuz) hat
#   A  das aus git extrahierte Paket (pakete/raute-aus-git) in improve eingespielt und laufen lassen,
#   B  die Raute in improve zweimal laufen lassen und daraus pakete/raute-aus-improve extrahiert.
# Hier die git-Hälfte von B: das Paket aus improve im git-Teilnehmerrepo laufen lassen — und beide
# extrahierten Pakete nebeneinander halten, bis hinunter zum Aktionsschlüssel.
#
# Braucht results/kreuz-improve.json und das Repo von git-seite.sh. Schreibt results/kreuz-git.json
# und KREUZ.md.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
REPO_DIR="$HIER/.work/repo"
[ -x "$REPO_DIR/bin/cockpit" ] || { echo "erst ./git-seite.sh laufen lassen" >&2; exit 1; }
[ -f results/kreuz-improve.json ] || { echo "erst die improve-Seite: improvego cmd/kreuz" >&2; exit 1; }
WORK="$HIER/.work/kreuz"
LOG="$WORK/schritte.jsonl"
rm -rf "$WORK"; mkdir -p "$WORK"; : > "$LOG"
( cd pkgtool && go build -o "$WORK/pkgtool" . )
source "$HIER/schritt.sh"
PKG="$WORK/pkgtool"

rm -rf "$REPO_DIR/packages/raute-aus-improve" "$REPO_DIR/packages/raute-aus-git"
cp -r pakete/raute-aus-improve "$REPO_DIR/packages/raute-aus-improve"
cp -r pakete/raute-aus-git "$REPO_DIR/packages/raute-aus-git"

echo "=== beide extrahierten Pakete: dieselbe Identität?"
schritt compare compare:inspect:git ktonpkg "" \
  "Das in git extrahierte Paket: Identität" \
  "$PKG inspect packages/raute-aus-git"

echo "=== B: das Paket aus improve im git-Repo"
schritt b:load b:load:inspect ktonpkg "S6: cockpit kennt kton-Pakete noch nicht" \
  "Das in improve extrahierte Paket öffnen und prüfen" \
  "$PKG inspect packages/raute-aus-improve"
schritt b:bind b:bind:plan ktonpkg "S5: im Zielbild cockpit publish {from, bindings}" \
  "Binden wie Lauf 1 in improve: eigene-daten.csv, anzahl=3" \
  "$PKG plan packages/raute-aus-improve work/raute-aus-improve --bind dataset=data/eigene-daten.csv --bind anzahl=3"
cp "$WORK/last.out" "$WORK/plan.json"
laufen "$WORK/plan.json" b:run

echo; echo "=== beide extrahierten Pakete nebeneinander"
python3 -c 'import json,sys; json.dump(json.load(open(sys.argv[1]))["outputs"], open(sys.argv[2],"w"))' results/git.json "$WORK/upstream.json"
BIND="--bind dataset=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["bindings"]["dataset"])' results/git.json) --bind anzahl=3 --upstream $WORK/upstream.json"
for p in raute raute-aus-git raute-aus-improve; do
  schritt compare "compare:potentials:$p" ktonpkg "" \
    "Je Step Potential, Protokoll und Aktionsschlüssel von $p — gebunden wie Lauf 1" \
    "$PKG potentials packages/$p $BIND | tee $WORK/pot-$p.json"
done
schritt compare compare:git-improve script "" \
  "Das aus git und das aus improve extrahierte Paket nebeneinander" \
  "python3 $HIER/paketvergleich.py packages/raute-aus-git packages/raute-aus-improve $WORK/pot-raute-aus-git.json $WORK/pot-raute-aus-improve.json"
schritt compare compare:original-improve script "" \
  "Das aus improve extrahierte Paket neben dem Original" \
  "python3 $HIER/paketvergleich.py packages/raute packages/raute-aus-improve $WORK/pot-raute.json $WORK/pot-raute-aus-improve.json"

python3 - "$LOG" "$HIER" <<'PY'
import datetime, json, sys
log, hier = sys.argv[1], sys.argv[2]
steps = [json.loads(l) for l in open(log)]
git = json.load(open(f"{hier}/results/git.json"))
imp = json.load(open(f"{hier}/results/kreuz-improve.json"))
b_out = {}
for s in steps:
    if s["phase"] == "b:run":
        step = s["id"].split(":", 2)[2]
        for p, h in s["result"].get("outputHashes", {}).items():
            b_out[step + "/" + p.rsplit("/", 1)[1]] = h
cmp_gi = next(s for s in steps if s["id"] == "compare:git-improve")["result"]
cmp_oi = next(s for s in steps if s["id"] == "compare:original-improve")["result"]
bid_git = next(s for s in steps if s["id"] == "compare:inspect:git")["result"].get("bundleId")
bid_imp = next(s for s in steps if s["id"] == "b:load:inspect")["result"].get("bundleId")
res = {
    "ranAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "steps": steps,
    "b": {"outputs": b_out, "sameAsImproveRun1": b_out == imp["b"]["runs"].get("run1Outputs"),
          "sameAsGit": b_out == git["outputs"]},
    "compareGitImprove": cmp_gi, "compareOriginalImprove": cmp_oi,
    "bundleIds": {"git": bid_git, "improve": bid_imp, "same": bid_git == bid_imp},
}
json.dump(res, open(f"{hier}/results/kreuz-git.json", "w"), indent=2, ensure_ascii=False)

A = imp["a"]
ok = lambda b: "✅ ja" if b else "❌ nein"
z = ["# Über Kreuz — extrahiert auf der einen Seite, gelaufen auf der anderen\n",
     "ADR-006, Schritt 3. Erzeugt von `kreuz.sh` aus `results/kreuz-improve.json` (improvego `cmd/kreuz`) "
     "und dem git-Lauf hier.\n",
     "## Auf einen Blick\n",
     "| Richtung | Paket | lief in | Ausgaben wie auf der Herkunftsseite | Aktionsschlüssel gleich |",
     "|---|---|---|---|---|",
     f"| A git → improve | `pakete/raute-aus-git` | improve | {ok(A.get('sameOutputsAsGit'))} | {ok(A.get('sameActionKeysAsGit'))} |",
     f"| B improve → git | `pakete/raute-aus-improve` | git-Repo, Cockpit | {ok(res['b']['sameAsImproveRun1'])} | "
     f"{ok(all(r['same'] for r in cmp_gi['rows'] if r['what'].startswith('Aktionsschlüssel')))} |",
     "",
     f"**Dasselbe Paket:** das aus git und das aus improve extrahierte Paket haben die Bundle-Id "
     f"`{bid_git}` bzw. `{bid_imp}` — {'**gleich**' if bid_git == bid_imp else '**verschieden**'}. "
     "Dieselben Ausführungen, auf welchem Backend auch immer aufgezeichnet, ergeben byte-für-byte "
     "dieselbe Identität; nur die Claims (wer extrahiert hat) und der Zeitstempel zählen nicht dazu.\n",
     "## Ausgaben\n",
     "| Datei | git (Referenzlauf) | A: git-Paket in improve | B: improve-Paket in git | improve Lauf 1 |",
     "|---|---|---|---|---|"]
for f in ["aufbereitung/daten.txt", "zweig-b/b.txt", "zweig-c/c.txt", "zusammenführung/ergebnis.txt"]:
    k = lambda h: f"`{h[:19]}…`" if h else "—"
    z.append(f"| `{f}` | {k(git['outputs'].get(f))} | {k(A['outputs'].get(f))} | {k(b_out.get(f))} | "
             f"{k(imp['b']['runs'].get('run1Outputs', {}).get(f))} |")
for titel, c in (("Das aus git und das aus improve extrahierte Paket", cmp_gi),
                 ("Das aus improve extrahierte Paket neben dem Original", cmp_oi)):
    z.append(f"\n## {titel}\n")
    z.append("| | links | rechts | |\n|---|---|---|---|")
    for r in c["rows"]:
        z.append(f"| {r['what']} | {r['original']} | {r['extracted']} | {'✅' if r['same'] else '≠ ' + r.get('why', '')} |")
prop = imp["b"]["proposal"]
z.append("\n## Der Vorschlag auf der improve-Seite\n")
z.append(f"{len(prop.get('runs', []))} Läufe, Steps: {', '.join(s if isinstance(s, str) else s.get('id') for s in prop.get('steps', []))}; "
         f"{len(prop.get('files', []))} Dateikandidaten, {len(prop.get('params', []))} Parameterkandidat(en), "
         f"{len(prop.get('conflicts') or [])} Konflikte.\n")
z.append("## Schritte\n")
for seite, st in (("improve", imp["steps"]), ("git", steps)):
    z.append(f"**{seite}**\n")
    z.append("| | Schritt | Akteur | Aufruf | Lücke |\n|---|---|---|---|---|")
    for s in st:
        cmd = s["command"].replace("|", "\\|").replace("\n", " ")
        cmd = cmd if len(cmd) <= 160 else cmd[:157] + "…"
        z.append(f"| {'✅' if s['ok'] else '❌'} | {s['title']} | {s['actor']} | `{cmd}` | {s.get('gap', '')} |")
    z.append("")
open(f"{hier}/KREUZ.md", "w").write("\n".join(z) + "\n")
print(f"\nresults/kreuz-git.json, KREUZ.md: {sum(s['ok'] for s in steps)}/{len(steps)} Schritte ok; "
      f"B gleich wie improve Lauf 1: {res['b']['sameAsImproveRun1']}")
PY
