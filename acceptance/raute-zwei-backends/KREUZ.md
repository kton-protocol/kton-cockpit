# Über Kreuz — extrahiert auf der einen Seite, gelaufen auf der anderen

ADR-006, Schritt 3. Erzeugt von `kreuz.sh` aus `results/kreuz-improve.json` (improvego `cmd/kreuz`) und dem git-Lauf hier.

## Auf einen Blick

| Richtung | Paket | lief in | Ausgaben wie auf der Herkunftsseite | Aktionsschlüssel gleich |
|---|---|---|---|---|
| A git → improve | `pakete/raute-aus-git` | improve | ✅ ja | ✅ ja |
| B improve → git | `pakete/raute-aus-improve` | git-Repo, Cockpit | ✅ ja | ✅ ja |

**Dasselbe Paket:** das aus git und das aus improve extrahierte Paket haben die Bundle-Id `sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173` bzw. `sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173` — **gleich**. Dieselben Ausführungen, auf welchem Backend auch immer aufgezeichnet, ergeben byte-für-byte dieselbe Identität; nur die Claims (wer extrahiert hat) und der Zeitstempel zählen nicht dazu.

## Ausgaben

| Datei | git (Referenzlauf) | A: git-Paket in improve | B: improve-Paket in git | improve Lauf 1 |
|---|---|---|---|---|
| `aufbereitung/daten.txt` | `sha256:ab0a3a625389…` | `sha256:ab0a3a625389…` | `sha256:ab0a3a625389…` | `sha256:ab0a3a625389…` |
| `zweig-b/b.txt` | `sha256:e908d8332bb7…` | `sha256:e908d8332bb7…` | `sha256:e908d8332bb7…` | `sha256:e908d8332bb7…` |
| `zweig-c/c.txt` | `sha256:fb76c5fd0781…` | `sha256:fb76c5fd0781…` | `sha256:fb76c5fd0781…` | `sha256:fb76c5fd0781…` |
| `zusammenführung/ergebnis.txt` | `sha256:44d419866f5a…` | `sha256:44d419866f5a…` | `sha256:44d419866f5a…` | `sha256:44d419866f5a…` |

## Das aus git und das aus improve extrahierte Paket

| | links | rechts | |
|---|---|---|---|
| Steps | aufbereitung, zusammenführung, zweig-b, zweig-c | aufbereitung, zusammenführung, zweig-b, zweig-c | ✅ |
| Verdrahtung | zusammenführung/b.txt ← zweig-b:b.txt<br>zusammenführung/c.txt ← zweig-c:c.txt<br>zweig-b/daten.txt ← aufbereitung:daten.txt<br>zweig-c/daten.txt ← aufbereitung:daten.txt | zusammenführung/b.txt ← zweig-b:b.txt<br>zusammenführung/c.txt ← zweig-c:c.txt<br>zweig-b/daten.txt ← aufbereitung:daten.txt<br>zweig-c/daten.txt ← aufbereitung:daten.txt | ✅ |
| Code (Kommandodateien) | aufbereitung: `sha256:7a667219a89e…`<br>zusammenführung: `sha256:86368fd1efca…`<br>zweig-b: `sha256:c8643db6360a…`<br>zweig-c: `sha256:f182205e0308…` | aufbereitung: `sha256:7a667219a89e…`<br>zusammenführung: `sha256:86368fd1efca…`<br>zweig-b: `sha256:c8643db6360a…`<br>zweig-c: `sha256:f182205e0308…` | ✅ |
| Löcher | anzahl: param an zweig-b/anzahl<br>dataset: file an aufbereitung/dataset.csv | anzahl: param an zweig-b/anzahl<br>dataset: file an aufbereitung/dataset.csv | ✅ |
| Parameter am Step | zweig-b: anzahl=hole:anzahl | zweig-b: anzahl=hole:anzahl | ✅ |
| Bedingung | sha256:e0669ff3f8f7… | sha256:e0669ff3f8f7… | ✅ |
| Befehlszeile | aufbereitung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zusammenführung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zweig-b: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file> ⏎ <anzahl>`<br>zweig-c: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>` | aufbereitung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zusammenführung: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>`<br>zweig-b: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file> ⏎ <anzahl>`<br>zweig-c: `scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a ⏎ <command-file>` | ✅ |
| Vorgaben der Löcher | anzahl=3<br>dataset=testdaten/dataset/dataset.csv | anzahl=3<br>dataset=testdaten/dataset/dataset.csv | ✅ |
| Protokoll je Step (ktonpkg) | aufbereitung: `sha256:68aeed08065c…`<br>zusammenführung: `sha256:ece807fc0c78…`<br>zweig-b: `sha256:233f1a7e95b0…`<br>zweig-c: `sha256:a64485e0fd7a…` | aufbereitung: `sha256:68aeed08065c…`<br>zusammenführung: `sha256:ece807fc0c78…`<br>zweig-b: `sha256:233f1a7e95b0…`<br>zweig-c: `sha256:a64485e0fd7a…` | ✅ |
| Aktionsschlüssel je Step, gleich gebunden | aufbereitung: `sha256:4b82ee9d4847…`<br>zusammenführung: `sha256:e4e67faa48e6…`<br>zweig-b: `sha256:1b9e6993bb84…`<br>zweig-c: `sha256:8ea8cb81bdbd…` | aufbereitung: `sha256:4b82ee9d4847…`<br>zusammenführung: `sha256:e4e67faa48e6…`<br>zweig-b: `sha256:1b9e6993bb84…`<br>zweig-c: `sha256:8ea8cb81bdbd…` | ✅ |
| Potential-Id je Step | aufbereitung: `sha256:bae8035a5372…`<br>zusammenführung: `sha256:135c84e5bb44…`<br>zweig-b: `sha256:ecb486d74f98…`<br>zweig-c: `sha256:6743509856e2…` | aufbereitung: `sha256:bae8035a5372…`<br>zusammenführung: `sha256:135c84e5bb44…`<br>zweig-b: `sha256:ecb486d74f98…`<br>zweig-c: `sha256:6743509856e2…` | ✅ |

## Das aus improve extrahierte Paket neben dem Original

| | links | rechts | |
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

## Der Vorschlag auf der improve-Seite

2 Läufe, Steps: aufbereitung, zweig-b, zweig-c, zusammenführung; 5 Dateikandidaten, 1 Parameterkandidat(en), 0 Konflikte.

## Schritte

**improve**

| | Schritt | Akteur | Aufruf | Lücke |
|---|---|---|---|---|
| ✅ | eigenen Testordner und zwei Register anlegen (A und B getrennt) | improvego | `c.CreateFolder(ctx, "/dst", "kreuz-20260930T184355"); c.EnsureRegister(ctx, <ordner>/kton-a); c.EnsureRegister(ctx, <ordner>/kton-b)` | S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego |
| ✅ | die Toolinstanz jam-r finden oder aus dem Paket einrichten | improve-server | `c.ResolveTool(ctx, "", "jam-r 1.0.0"); sonst c.InstallTool(ctx, <runserver "default">, improve/tool-instance.json)` | S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego |
| ✅ | eigene-daten.csv hochladen — Bindung für A und B run 1 | improvego | `c.CreateFile(ctx, "/dst/kreuz-20260930T184355", "/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/eigene-daten.csv")` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | das auf der git-Seite extrahierte Paket öffnen, prüfen und einspielen | ktonpkg | `ktonpkg.Open("/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/pakete/raute-aus-git").Verify(); c.InstallPackage(ctx, b, InstallOptions{Targe…` | S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego |
| ✅ | Step aufbereitung des git-Pakets realisieren und als signiertes Foton ins Register | improve-server | `c.Realize(ctx, tplA, RealizeOptions{Bindings: {dataset: <eigene-daten.csv>, anzahl: "3"}, Run: true, Register: <ordner>/kton-a}) — Step aufbereitung` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | Step zweig-b des git-Pakets realisieren und als signiertes Foton ins Register | improve-server | `c.Realize(ctx, tplA, RealizeOptions{Bindings: {dataset: <eigene-daten.csv>, anzahl: "3"}, Run: true, Register: <ordner>/kton-a}) — Step zweig-b` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | Step zweig-c des git-Pakets realisieren und als signiertes Foton ins Register | improve-server | `c.Realize(ctx, tplA, RealizeOptions{Bindings: {dataset: <eigene-daten.csv>, anzahl: "3"}, Run: true, Register: <ordner>/kton-a}) — Step zweig-c` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | Step zusammenführung des git-Pakets realisieren und als signiertes Foton ins Register | improve-server | `c.Realize(ctx, tplA, RealizeOptions{Bindings: {dataset: <eigene-daten.csv>, anzahl: "3"}, Run: true, Register: <ordner>/kton-a}) — Step zusammenführung` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | Ausgaben und Aktionsschlüssel neben die git-Seite halten | improvego | `c.ActionKey(ctx, tplA, <step>, bindings, <realisiert>); ktonpkg.Open("/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/pakete/raute-aus-git")…` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | die Original-Raute öffnen, prüfen und einspielen | ktonpkg | `ktonpkg.Open("/mnt/c/dev/workstreams/active/improveGo/examples/raute/paket").Verify(); c.InstallPackage(ctx, b, InstallOptions{TargetFolder: <ordner>/b, Tool…` | S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego |
| ✅ | run 1: dataset = eigene-daten.csv, anzahl = 3; jeder Step ein signiertes Foton | improve-server | `c.Realize(ctx, tplB, RealizeOptions{Bindings: {dataset: <eigene-daten.csv>, anzahl: "3"}, Run: true, Register: <ordner>/kton-b})` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | run 2: die Vorgaben des Pakets (mitgelieferte Testdaten, anzahl = 1) | improve-server | `c.Realize(ctx, tplB, RealizeOptions{Bindings: {}, Run: true, Register: <ordner>/kton-b})` | S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ | vom ergebnis.txt jedes Laufs rückwärts: die Linie, nur Fotons, deren Signatur verifiziert | improvego | `reg.VerifiedLineage(ctx, <ergebnis run 1>); reg.VerifiedLineage(ctx, <ergebnis run 2>)` | S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego; im Zielbild `cockpit ask {query: lineage}` gegen das improve-Register |
| ✅ | Ausführungen über den Adapter, dann der Vorschlag — beobachtet, nicht entschieden | ktonpkg | `c.ProposeRay(ctx, <End-Steps beider Läufe>) — Gegenprobe: dieselben Fotons wie im Register verifiziert` | S5/S6: im Zielbild cockpit ask {query: ray} |
| ✅ | ausdrücklich gewählt extrahieren, prov:wasDerivedFrom signieren | ktonpkg | `c.ExtractRay(ctx, <vorschlag>, <choice>, "/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/pakete/raute-aus-improve", improve.Signer())` | S5/S6: im Zielbild cockpit publish {kind: ray, choose} |

**git**

| | Schritt | Akteur | Aufruf | Lücke |
|---|---|---|---|---|
| ✅ | Das in git extrahierte Paket: Identität | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool inspect packages/raute-aus-git` |  |
| ✅ | Das in improve extrahierte Paket öffnen und prüfen | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool inspect packages/raute-aus-improve` | S6: cockpit kennt kton-Pakete noch nicht |
| ✅ | Binden wie Lauf 1 in improve: eigene-daten.csv, anzahl=3 | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool plan packages/raute-aus-improve work/raute-aus-improve --bind dataset=da…` | S5: im Zielbild cockpit publish {from, bindings} |
| ✅ | Step aufbereitung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-aus-improve/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/raute-aus-improve/aufbereitung/aufbereiten…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-b im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-aus-improve/zweig-b && Rscript zweigB.R 3", "inputs": ["work/raute-aus-improve/zweig-b/daten.txt", "work/raute-…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zweig-c im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-aus-improve/zweig-c && Rscript zweigC.R", "inputs": ["work/raute-aus-improve/zweig-c/daten.txt", "work/raute-au…` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Step zusammenführung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen | cockpit | `./bin/cockpit publish '{"cmd": "cd work/raute-aus-improve/zusammenführung && Rscript zusammen.R", "inputs": ["work/raute-aus-improve/zusammenführung/b.txt", …` | S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst |
| ✅ | Je Step Potential, Protokoll und Aktionsschlüssel von raute — gebunden wie Lauf 1 | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool potentials packages/raute --bind dataset=sha256:ab0a3a6253897602cb84d459…` |  |
| ✅ | Je Step Potential, Protokoll und Aktionsschlüssel von raute-aus-git — gebunden wie Lauf 1 | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool potentials packages/raute-aus-git --bind dataset=sha256:ab0a3a6253897602…` |  |
| ✅ | Je Step Potential, Protokoll und Aktionsschlüssel von raute-aus-improve — gebunden wie Lauf 1 | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/kreuz/pkgtool potentials packages/raute-aus-improve --bind dataset=sha256:ab0a3a625389…` |  |
| ✅ | Das aus git und das aus improve extrahierte Paket nebeneinander | script | `python3 /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/paketvergleich.py packages/raute-aus-git packages/raute-aus-improve /mnt/c/dev/git-r…` |  |
| ✅ | Das aus improve extrahierte Paket neben dem Original | script | `python3 /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/paketvergleich.py packages/raute packages/raute-aus-improve /mnt/c/dev/git-repos/kto…` |  |

