# Raute auf zwei Backends

Ein Abnahmeszenario für ADR-005: derselbe Ablauf einmal in einem **git**-Teilnehmerrepo und einmal
in **improve**, Schritt für Schritt nebeneinander.

1. **Cockpit einrichten**
2. **Werkzeug einrichten und qualifizieren** — jam-r (R 4.4, `scinteco/jam-r@sha256:dee2a8…`) gegen
   sein Spectrum: fünf Member, identisch oder über den Normalisierer
3. **Workflow laden** — das kton-Paket „Raute" (vier Steps), prüfen, seine Bedingung gegen das
   qualifizierte Werkzeug halten
4. **Löcher füllen** — erst ungebunden (muss abgelehnt werden), dann `dataset` = `eigene-daten.csv`,
   `anzahl` = 3
5. **Laufen lassen und nachprüfen** — vier Fotons, die Linie vom Ergebnis zurück zum Datensatz

Es ist ein **Soll-Test**. Heute handelt das Cockpit nur dort, wo es das schon kann; alles andere
erledigen improvego, ktonpkg, die Kernel-CLI oder ein Skript — und jeder solche Schritt nennt die
Lücke, die ihn schließt (S4 improve-Modus, S5 Potentials und Realisierung, S6 Pakete und Spectra,
K3 Spectrum-Prüfung als Kernel-Bibliothek). Mit jedem dieser Schritte wird ein Eintrag hier zu einem
`cockpit …`-Aufruf, und der Vergleich bleibt derselbe.

## Extraktion (ADR-006)

`extraktion.sh` baut auf dem Repo der git-Seite auf und geht die andere Richtung: aus Ausführungen,
die schon liefen, das Potential zurückgewinnen. Ein zweiter Lauf auf den Vorgaben des Pakets, dann
Vorschlag (`cockpit ask {query: ray}`) → ausdrückliche Wahl → Ray-Paket (`cockpit publish {kind:
ray}`), neben das Original gehalten und selbst laufen gelassen. Ergebnis in `EXTRAKTION.md`.

## Über Kreuz (ADR-006, Schritt 3)

Auf einer Seite extrahieren, auf der anderen laufen lassen. `pakete/raute-aus-git` spielt improvego
`cmd/kreuz` in improve ein. Außerdem lässt es die Raute dort zweimal laufen und extrahiert daraus
`pakete/raute-aus-improve`. `kreuz.sh` lässt dieses Paket im git-Repo laufen und hält beide Pakete
nebeneinander, bis zum Aktionsschlüssel. Das Ergebnis steht in `KREUZ.md`.

## Review (ADR-007)

Dasselbe Paket, in beiden Backends geprüft. In improve entscheidet der improve-Review mit
Vier-Augen-Prinzip und Audit-Trail, der nekton-Claim ist nur seine Projektion (improvego
`cmd/review`). In git **ist** der Review ein nekton-Claim: `review.sh` legt einen zweiten
Teilnehmer an, den Prüfer, der mit eigenem Schlüssel urteilt. Sein Claim und die Projektion aus
improve kommen über die Föderation ins Register der Autorin. `ask about <bundle>` zeigt jedes
Urteil mit der Stufe, in die es verifiziert. Ergebnis in `REVIEW.md`.

## Ausführen

```bash
./git-seite.sh                                   # git: docker, go, python3
(cd /home/hacklm/improvego && IMPROVE_URL=http://<improve-server>/repository/api \
  IMPROVE_TEST_PATH=/dst go run ./cmd/rautevergleich)   # improve: <improve-server>, ~5 min
python3 vergleich.py                             # → VERGLEICH.md
./extraktion.sh                                  # nach git-seite.sh → EXTRAKTION.md, pakete/raute-aus-git
(cd /home/hacklm/improvego && IMPROVE_URL=http://<improve-server>/repository/api \
  IMPROVE_TEST_PATH=/dst go run ./cmd/kreuz)       # improve: A und B, ~6 min
./kreuz.sh                                       # → KREUZ.md
(cd /home/hacklm/improvego && IMPROVE_URL=http://<improve-server>/repository/api \
  IMPROVE_TEST_PATH=/dst go run ./cmd/review)      # improve: Review mit zweitem Benutzer, Projektion
./review.sh                                      # → REVIEW.md
```

Beide Seiten brauchen die Pakete aus dem improveGo-Workstream (`tools/jam-r/paket`,
`examples/raute/paket`); `IMPROVEGO_WS` zeigt auf ihn.

## Was liegt hier

| | |
|---|---|
| `git-seite.sh` | die git-Seite; schreibt `results/git.json` |
| `extraktion.sh` | aus den Ausführungen der git-Seite das Potential; schreibt `results/extraktion.json` und `EXTRAKTION.md` |
| `review.sh` | der Review im git-Backend, dazu die Projektion aus improve; schreibt `results/review-git.json` und `REVIEW.md` |
| `kreuz.sh` | die git-Hälfte von Schritt 3; schreibt `results/kreuz-git.json` und `KREUZ.md` |
| `pakete/` | die extrahierten Pakete, die zwischen den Seiten wandern |
| `schritt.sh` | gemeinsam: jeden Schritt wörtlich ausführen und protokollieren, einen Plan mit dem Cockpit laufen lassen |
| `records.py`, `paketvergleich.py` | Ausführungen über `cockpit ask record` holen; zwei Ray-Pakete nebeneinander |
| `pkgtool/` | der Teil, den das Cockpit noch nicht kann: Pakete öffnen, Löcher binden, Steps als Fotons, Vorschlag und Extraktion — alles über ktonpkg |
| `eigene-daten.csv` | der Datensatz für das Loch `dataset`, auf beiden Seiten derselbe |
| `anonymisieren.py` | nimmt Host, Konten und interne Ids aus Ergebnissen und Berichten — vor jedem Commit |
| `results/SCHEMA.md` | das gemeinsame Ergebnisformat |
| `results/*.json` | die Ergebnisse des letzten Laufs je Seite |
| `vergleich.py` → `VERGLEICH.md` | beide nebeneinander: Ausgaben, Qualifizierung, jeder Schritt mit exaktem Aufruf, die Lücken |

**Vor jedem Commit `IMPROVE_URL=… python3 anonymisieren.py`.** Das Repository ist öffentlich, und die Ergebnisse
nennen sonst den improve-Host, Konten und interne Ids. Die Skripte lesen den Host aus `IMPROVE_URL`,
die Beispielaufrufe oben verwenden `<improve-server>` als Platzhalter. Konten und weitere Wörter stehen je Zeile als `wort=ersatz` in `.anonymisieren.lokal`, die nicht versioniert wird — das Skript selbst nennt nichts.

Nicht Teil von `examples/run-all.sh` und nicht in CI: die improve-Seite braucht einen erreichbaren
improve-Server.
