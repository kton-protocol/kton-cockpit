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
Vorschlag → ausdrückliche Wahl → Ray-Paket, neben das Original gehalten und selbst laufen gelassen.
Ergebnis in `EXTRAKTION.md`.

## Ausführen

```bash
./git-seite.sh                                   # git: docker, go, python3
(cd /home/hacklm/improvego && IMPROVE_URL=http://<improve-server>/repository/api \
  IMPROVE_TEST_PATH=/dst go run ./cmd/rautevergleich)   # improve: <improve-server>, ~5 min
python3 vergleich.py                             # → VERGLEICH.md
./extraktion.sh                                  # nach git-seite.sh → EXTRAKTION.md
```

Beide Seiten brauchen die Pakete aus dem improveGo-Workstream (`tools/jam-r/paket`,
`examples/raute/paket`); `IMPROVEGO_WS` zeigt auf ihn.

## Was liegt hier

| | |
|---|---|
| `git-seite.sh` | die git-Seite; schreibt `results/git.json` |
| `extraktion.sh` | aus den Ausführungen der git-Seite das Potential; schreibt `results/extraktion.json` und `EXTRAKTION.md` |
| `schritt.sh` | gemeinsam: jeden Schritt wörtlich ausführen und protokollieren, einen Plan mit dem Cockpit laufen lassen |
| `records.py`, `paketvergleich.py` | Ausführungen über `cockpit ask record` holen; zwei Ray-Pakete nebeneinander |
| `pkgtool/` | der Teil, den das Cockpit noch nicht kann: Pakete öffnen, Löcher binden, Steps als Fotons, Vorschlag und Extraktion — alles über ktonpkg |
| `eigene-daten.csv` | der Datensatz für das Loch `dataset`, auf beiden Seiten derselbe |
| `results/SCHEMA.md` | das gemeinsame Ergebnisformat |
| `results/*.json` | die Ergebnisse des letzten Laufs je Seite |
| `vergleich.py` → `VERGLEICH.md` | beide nebeneinander: Ausgaben, Qualifizierung, jeder Schritt mit exaktem Aufruf, die Lücken |

Nicht Teil von `examples/run-all.sh` und nicht in CI: die improve-Seite braucht einen erreichbaren
improve-Server.
