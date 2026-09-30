# Extraktion — aus Ausführungen das Potential

ADR-006, Schritt 2. Zwei Läufe der Raute im git-Teilnehmerrepo (eigene Daten mit anzahl=3, Testdaten mit anzahl=1) → Vorschlag → ausdrückliche Wahl → Ray-Paket → laufen lassen. Erzeugt von `extraktion.sh`.

## Vorschlag

2 Läufe, Steps: aufbereitung, zweig-c, zweig-b, zusammenführung. Konflikte: 0. Als Reproduktion zusammengefasst: 12 Ausführungen (gleiche Berechnung, gleiche Bytes — kein eigener Step).

| Kandidat | Art | Werte je Lauf | gewählt |
|---|---|---|---|
| `aufbereitung/aufbereiten.R` | Code (Kommandodatei) | run-1: `sha256:7a667219a89e…`, run-2: `sha256:7a667219a89e…` | fest |
| `aufbereitung/dataset.csv` | Datei | run-1: `sha256:ab0a3a625389…`, run-2: `sha256:e80079e56ed5…` | Loch `dataset` |
| `zweig-c/zweigC.R` | Code (Kommandodatei) | run-1: `sha256:f182205e0308…`, run-2: `sha256:f182205e0308…` | fest |
| `zweig-b/zweigB.R` | Code (Kommandodatei) | run-1: `sha256:c8643db6360a…`, run-2: `sha256:c8643db6360a…` | fest |
| `zusammenführung/zusammen.R` | Code (Kommandodatei) | run-1: `sha256:86368fd1efca…`, run-2: `sha256:86368fd1efca…` | fest |
| `zweig-b` Stelle 2 | Befehlszeile | run-1: `3`, run-2: `1` | Parameter `anzahl` |

Nichts davon ist geraten: Dateikandidaten sind Eingaben, die keine ausgewählte Ausführung erzeugt hat; der einzige Parameterkandidat ist die Stelle, an der die beiden Läufe verschieden waren.

## Neben dem Original

| | Original | extrahiert | |
|---|---|---|---|
| Steps | aufbereitung, zusammenführung, zweig-b, zweig-c | aufbereitung, zusammenführung, zweig-b, zweig-c | ✅ |
| Verdrahtung | zusammenführung/b.txt ← zweig-b:b.txt<br>zusammenführung/c.txt ← zweig-c:c.txt<br>zweig-b/daten.txt ← aufbereitung:daten.txt<br>zweig-c/daten.txt ← aufbereitung:daten.txt | zusammenführung/b.txt ← zweig-b:b.txt<br>zusammenführung/c.txt ← zweig-c:c.txt<br>zweig-b/daten.txt ← aufbereitung:daten.txt<br>zweig-c/daten.txt ← aufbereitung:daten.txt | ✅ |
| Code (Kommandodateien) | aufbereitung: `sha256:7a667219a89e…`<br>zusammenführung: `sha256:86368fd1efca…`<br>zweig-b: `sha256:c8643db6360a…`<br>zweig-c: `sha256:f182205e0308…` | aufbereitung: `sha256:7a667219a89e…`<br>zusammenführung: `sha256:86368fd1efca…`<br>zweig-b: `sha256:c8643db6360a…`<br>zweig-c: `sha256:f182205e0308…` | ✅ |
| Löcher | anzahl: param an zweig-b/anzahl<br>dataset: file an aufbereitung/dataset.csv | anzahl: param an zweig-b/anzahl<br>dataset: file an aufbereitung/dataset.csv | ✅ |
| Parameter am Step | zweig-b: anzahl=hole:anzahl | zweig-b: anzahl=hole:anzahl | ✅ |
| Bedingung | sha256:e0669ff3f8f7… | sha256:e0669ff3f8f7… | ✅ |
| Befehlszeile | aufbereitung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zusammenführung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zweig-b: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file> ⏎ <anzahl>`<br>zweig-c: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>` | aufbereitung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zusammenführung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zweig-b: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file> ⏎ <anzahl>`<br>zweig-c: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>` | ✅ |
| Vorgaben der Löcher | anzahl=1<br>dataset=testdaten/dataset.csv | anzahl=3<br>dataset=testdaten/dataset/dataset.csv | ≠ erwartet: die Vorgaben kommen aus dem gewählten Referenzlauf |
| Protokoll je Step (ktonpkg) | aufbereitung: `sha256:68aeed08065c…`<br>zusammenführung: `sha256:ece807fc0c78…`<br>zweig-b: `sha256:233f1a7e95b0…`<br>zweig-c: `sha256:a64485e0fd7a…` | aufbereitung: `sha256:68aeed08065c…`<br>zusammenführung: `sha256:ece807fc0c78…`<br>zweig-b: `sha256:233f1a7e95b0…`<br>zweig-c: `sha256:a64485e0fd7a…` | ✅ |
| Aktionsschlüssel je Step, gleich gebunden | aufbereitung: `sha256:4b82ee9d4847…`<br>zusammenführung: `sha256:e4e67faa48e6…`<br>zweig-b: `sha256:1b9e6993bb84…`<br>zweig-c: `sha256:8ea8cb81bdbd…` | aufbereitung: `sha256:4b82ee9d4847…`<br>zusammenführung: `sha256:e4e67faa48e6…`<br>zweig-b: `sha256:1b9e6993bb84…`<br>zweig-c: `sha256:8ea8cb81bdbd…` | ✅ |
| Potential-Id je Step | aufbereitung: `sha256:5ba5b8ae206d…`<br>zusammenführung: `sha256:fe7e2d66e456…`<br>zweig-b: `sha256:55482fd8afb6…`<br>zweig-c: `sha256:c29fee12d8be…` | aufbereitung: `sha256:bae8035a5372…`<br>zusammenführung: `sha256:135c84e5bb44…`<br>zweig-b: `sha256:ecb486d74f98…`<br>zweig-c: `sha256:6743509856e2…` | ≠ erwartet: das extrahierte Paket erklärt seine Ausgaben (virtuelle Ausgaben, kton §6.4), das Original nicht — der Aktionsschlüssel rechnet ohne Ausgaben |

## Laufen lassen

- Auf den Vorgaben (Daten des Referenzlaufs): Ausgaben **gleich** wie im Referenzlauf; Spectrum-Prüfung bestanden.
- Anders gebunden (Testdaten des Originals, anzahl=1): Ausgaben **gleich** wie im zweiten Lauf.

## Schritte

| | Schritt | Akteur | Aufruf | Lücke |
|---|---|---|---|---|
| ✅ | Die Raute mit ihren Vorgaben binden: Testdatensatz des Pakets, anzahl=1 | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool plan packages/raute work/raute-2` |  |
| ✅ | Step aufbereitung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-2/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/raute-2/aufbereitung/dataset.csv", "work/raute-2/auf…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-c im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-2/zweig-c && Rscript zweigC.R", "inputs": ["work/raute-2/zweig-c/daten.txt", "work/raute-2/zweig-c/zweigC.R"], …` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-b im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-2/zweig-b && Rscript zweigB.R 1", "inputs": ["work/raute-2/zweig-b/zweigB.R", "work/raute-2/zweig-b/daten.txt"]…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zusammenführung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-2/zusammenführung && Rscript zusammen.R", "inputs": ["work/raute-2/zusammenführung/zusammen.R", "work/raute-2/z…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Vom Ergebnis des Laufs raute rückwärts: die Linie, nur gegen die Vertrauensstufen verifizierte Fotons | cockpit | `./bin/cockpit ask '{"query":"lineage","ref":"work/raute/zusammenführung/ergebnis.txt"}' --field included` |  |
| ✅ | Vom Ergebnis des Laufs raute-2 rückwärts: die Linie, nur gegen die Vertrauensstufen verifizierte Fotons | cockpit | `./bin/cockpit ask '{"query":"lineage","ref":"work/raute-2/zusammenführung/ergebnis.txt"}' --field included` |  |
| ✅ | Jede ausgewählte Ausführung im Einzelnen: Befehl, Umgebung, Ein- und Ausgaben (je ein cockpit ask record) | cockpit | `python3 /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/records.py /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/ex…` |  |
| ✅ | Vorschlag: Läufe, Steps, Verdrahtung und die Kandidaten — beobachtet, nicht entschieden | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool propose /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-bac…` | S5/S6: im Zielbild cockpit ask {query: ray} |
| ✅ | Ausdrücklich gewählt: dataset als Loch, anzahl als Parameter, Referenz der Lauf mit eigene-daten.csv | script | `cat /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/choice.json` | S5/S6: im Zielbild die Auswahl in cockpit publish {kind: ray, choose} |
| ✅ | Extrahieren: Ray-Paket mit Testdaten und Spectrum aus dem Referenzlauf, prov:wasDerivedFrom signiert | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool extract /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-bac…` | S5/S6: im Zielbild cockpit publish {kind: ray} |
| ✅ | Das extrahierte Paket öffnen und prüfen | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool inspect packages/raute-extrahiert` |  |
| ✅ | Je Step Potential, Protokoll und Aktionsschlüssel des extrahierten Pakets — gebunden wie der Referenzlauf | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool potentials packages/raute-extrahiert --bind dataset=sha256:ab0a3a62…` |  |
| ✅ | Neben das Original-Paket halten: Steps, Verdrahtung, Code, Löcher, Befehlszeile, Bedingung, Potential je Step | script | `python3 /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/paketvergleich.py packages/raute packages/raute-extrahiert /mnt/c/dev/git-repos/kton…` |  |
| ✅ | Auf seinen Vorgaben binden — das sind die Daten des Referenzlaufs | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool plan packages/raute-extrahiert work/raute-x` |  |
| ✅ | Step aufbereitung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-x/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/raute-x/aufbereitung/aufbereiten.R", "work/raute-x/a…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-c im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-x/zweig-c && Rscript zweigC.R", "inputs": ["work/raute-x/zweig-c/daten.txt", "work/raute-x/zweig-c/zweigC.R"], …` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-b im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-x/zweig-b && Rscript zweigB.R 3", "inputs": ["work/raute-x/zweig-b/daten.txt", "work/raute-x/zweig-b/zweigB.R"]…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zusammenführung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-x/zusammenführung && Rscript zusammen.R", "inputs": ["work/raute-x/zusammenführung/b.txt", "work/raute-x/zusamm…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Die Ausgaben gegen das Spectrum des extrahierten Pakets halten | kernel-cli | `PLANKTON_DIR=registry/plankton ./bin/plankton spectrum check packages/raute-extrahiert/kton/spectrum.json --candidate aufbereitung=sha256:ab0a3a6253897602cb8…` | K3: Spectrum-Prüfung nur als Kernel-CLI |
| ✅ | Anders binden: die Daten des zweiten Laufs (Testdatensatz des Originals, anzahl=1) | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/extraktion/pkgtool plan packages/raute-extrahiert work/raute-y --bind dataset=packages…` |  |
| ✅ | Step aufbereitung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-y/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/raute-y/aufbereitung/aufbereiten.R", "work/raute-y/a…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-c im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-y/zweig-c && Rscript zweigC.R", "inputs": ["work/raute-y/zweig-c/daten.txt", "work/raute-y/zweig-c/zweigC.R"], …` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-b im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-y/zweig-b && Rscript zweigB.R 1", "inputs": ["work/raute-y/zweig-b/daten.txt", "work/raute-y/zweig-b/zweigB.R"]…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zusammenführung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-y/zusammenführung && Rscript zusammen.R", "inputs": ["work/raute-y/zusammenführung/b.txt", "work/raute-y/zusamm…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
