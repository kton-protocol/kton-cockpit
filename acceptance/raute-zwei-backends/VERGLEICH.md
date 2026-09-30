# Raute auf zwei Backends — git und improve

Werkzeug **jam-r** qualifizieren, Workflow-Paket **Raute** laden, Löcher füllen (`dataset` = `eigene-daten.csv`, `anzahl` = 3), laufen lassen — einmal in einem git-Teilnehmerrepo mit dem kton-cockpit, einmal in improve. Erzeugt von `vergleich.py` aus `results/git.json` und `results/improve.json`.

| | git | improve |
|---|---|---|
| gelaufen | 2026-09-30T15:20:25Z | 2026-09-30T14:45:45Z |
| Schritte ok | 26 / 27 | 12 / 12 |
| `dataset` | `sha256:ab0a3a6253897602…` | `sha256:ab0a3a6253897602…` |
| `anzahl` | 3 | 3 |

## 1 Ergebnis

### Ausgaben — dieselben Bytes?

| Datei | git | improve | |
|---|---|---|---|
| `aufbereitung/daten.txt` | `sha256:ab0a3a6253897602cb84d459…` | `sha256:ab0a3a6253897602cb84d459…` | ✅ gleich |
| `zweig-b/b.txt` | `sha256:e908d8332bb7d78ee94deadb…` | `sha256:e908d8332bb7d78ee94deadb…` | ✅ gleich |
| `zweig-c/c.txt` | `sha256:fb76c5fd07819ff96996d189…` | `sha256:fb76c5fd07819ff96996d189…` | ✅ gleich |
| `zusammenführung/ergebnis.txt` | `sha256:44d419866f5a847c591f1c4a…` | `sha256:44d419866f5a847c591f1c4a…` | ✅ gleich |

**4 von 4** Ausgaben sind auf beiden Seiten bitgleich.

Die **Foton-Ids** unterscheiden sich erwartungsgemäß: eine Foton-Id deckt das Protokoll mit ab, und das ist auf der git-Seite die Befehlszeile samt gepinntem Image, in improve das Tool mit seinem Deskriptor. Gleich sein müssen die Bytes — dass zwei verschiedene Ausführungen dieselben Bytes liefern, ist genau die Aussage, die ein Vergleich machen kann.

| Step | Foton git | Foton improve |
|---|---|---|
| aufbereitung | `sha256:4afc54a1d6f2b61bd58c…` | `sha256:5e72c51daef042009762…` |
| zweig-b | `sha256:8aade9fc2f7c7913a30e…` | `sha256:142298e492946eed7d57…` |
| zweig-c | `sha256:bc72f4dbdaa329516bcb…` | `sha256:fa5cd0add6216a9c5f5a…` |
| zusammenführung | `sha256:b45494f6cbcbcb405c74…` | `sha256:172d20fbeff48789b7d3…` |

### Werkzeugqualifizierung jam-r

| | git | improve |
|---|---|---|
| Spectrum | `sha256:e0669ff3f8f75686…` | `sha256:e0669ff3f8f75686…` |
| Befund | **L1** | **L1** |

| Member | Referenz | git | | improve | |
|---|---|---|---|---|---|
| bericht | `sha256:7c05410d8c68ca61…` | `sha256:defc1bafe2f9152c…` | via-normalizer | `sha256:defc1bafe2f9152c…` | via-normalizer |
| grafik-daten | `sha256:f75dd1bc9d8112d8…` | `sha256:f75dd1bc9d8112d8…` | identical | `sha256:f75dd1bc9d8112d8…` | identical |
| json | `sha256:28f2841e50343381…` | `sha256:28f2841e50343381…` | identical | `sha256:28f2841e50343381…` | identical |
| modell | `sha256:9e8a620b446144a6…` | `sha256:9e8a620b446144a6…` | identical | `sha256:9e8a620b446144a6…` | identical |
| tabelle | `sha256:0cce87e13fe345df…` | `sha256:0cce87e13fe345df…` | identical | `sha256:0cce87e13fe345df…` | identical |

## 2 Die Schritte nebeneinander

Je Zelle: Ergebnis, **was** passiert, _wer_ es tut, der exakte Aufruf, was herauskam — und ⚠ wo im Zielbild das Cockpit handeln soll.

### Cockpit einrichten

| git | improve |
|---|---|
| ✅ **Welches Cockpit, welcher Kernel**<br>_cockpit_<br>`./bin/cockpit version`<br>→ text: cockpit (devel)<br>  built from d5de05cb38875adb1466f2b135ed1b0134984147 (uncommitted changes)<br>  go1.25.0<br>  kton.dev/kton        v0.2.1<br>  kton.dev/nekton      v0.2 | ✅ **eigenen Testordner und das kton-Register darin anlegen**<br>_improvego_<br>`c.CreateFolder(ctx, "/dst", "raute-vergleich-20260930T164545"); c.EnsureRegister(ctx, "/dst/raute-vergleich-20260930T164545/kton")`<br>→ cockpitModus: keiner — cockpit kennt nur git und local (SPEC §5.2 und SPEC §5.3)<br>ordner: /dst/raute-vergleich-20260930T164545<br>register: /dst/raute-vergleich-20260930T164545/kton<br>⚠ Lücke: S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego |
| ✅ **Teilnehmerrepo: Konfiguration und Bindung an den origin-Remote prüfen**<br>_cockpit_<br>`./bin/cockpit doctor`<br>→ text: repo root:      /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/repo<br>bound to:       cockpit-examples/raute-zwei-backends (verified again | — |

### Werkzeug einrichten

| git | improve |
|---|---|
| ✅ **Das Werkzeug ist das per Digest gepinnte Image in execution.image — Einrichten heißt: es liegt lokal vor**<br>_script_<br>`docker pull -q scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a && python3 -c 'import json; print(json.dumps(json.load(open("cockpit.config.json"))["execution"]))'`<br>→ text: docker.io/scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a<br>{"image": "oci://docker.io/scinteco/jam-r@sha256:dee2a8cfb95adf | ✅ **die Toolinstanz jam-r finden oder aus dem Paket einrichten**<br>_improve-server_<br>`c.ResolveTool(ctx, "", "jam-r 1.0.0"); sonst c.InstallTool(ctx, <runserver "default">, improve/tool-instance.json)`<br>→ args: scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a<br><command-file><br>instanz: jam-r 1.0.0<br>runserver: default<br>⚠ Lücke: S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego; Werkzeuginstallation hat im Cockpit kein Verb — sie gehört zu S6 (Werkzeugpaket über ktonpkg) |

### Werkzeug qualifizieren

| git | improve |
|---|---|
| ✅ **Das Werkzeugpaket öffnen: Identität, Prüfbefund, angebotenes Spectrum**<br>_ktonpkg_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool inspect packages/jam-r`<br>→ spectrumId: `sha256:e0669ff3f8f75686…`<br>bundleId: `sha256:2a5a9cfb05278c57…`<br>rayId: `sha256:cda4c48206833580…`<br>⚠ Lücke: S6: cockpit kennt kton-Pakete noch nicht | ✅ **die Member des jam-r-Spectrums auf dieser Instanz laufen lassen und gegen das Spectrum halten**<br>_improvego_<br>`c.QualifySpectrum(ctx, ktonpkg.Open("/mnt/c/dev/workstreams/active/improveGo/tools/jam-r/paket"), QualifyOptions{TargetFolder: "/dst/raute-vergleich-20260930T164545/werkzeug", Tool: <jam-r>, Register: "/dst/raute-vergleich-20260930T164545/kton"}); reg.Sign(OQ)`<br>→ spectrumId: `sha256:e0669ff3f8f75686…`<br>verdict: `L1`<br>⚠ Lücke: S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego |
| ✅ **Die Referenz-Fotons des Herstellers ins eigene Register übernehmen — ohne sie ist das Spectrum eine Liste unbelegter Hashes**<br>_kernel-cli_<br>`./bin/plankton add /mnt/c/dev/workstreams/active/improveGo/tools/jam-r/spectrum/registry/objects/sha256/*.json --registry registry/plankton`<br>→ text: indexed 14 fotons, 0 already present, 0 refused  (registry now holds 14)<br>⚠ Lücke: K3/S6: Referenz-Fotons des Spectrums übernimmt heute die Kernel-CLI | — |
| ✅ **Spectrum-Member json im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd":"cd work/jam-r/json && Rscript 01-json.R","inputs":["work/jam-r/json/01-json.R"],"outputs":["work/jam-r/json/json.out"]}'`<br>→ fotonId: `sha256:73439b1843aec541…`<br>`json.out` = `sha256:28f2841e50343381…` | — |
| ✅ **Spectrum-Member tabelle im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd":"cd work/jam-r/tabelle && Rscript 02-tabelle.R","inputs":["work/jam-r/tabelle/02-tabelle.R"],"outputs":["work/jam-r/tabelle/tabelle.out"]}'`<br>→ fotonId: `sha256:d536927e8b417d5a…`<br>`tabelle.out` = `sha256:0cce87e13fe345df…` | — |
| ✅ **Spectrum-Member modell im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd":"cd work/jam-r/modell && Rscript 03-modell.R","inputs":["work/jam-r/modell/03-modell.R"],"outputs":["work/jam-r/modell/modell.out"]}'`<br>→ fotonId: `sha256:32bd97b3f3d7d410…`<br>`modell.out` = `sha256:9e8a620b446144a6…` | — |
| ✅ **Spectrum-Member bericht im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd":"cd work/jam-r/bericht && Rscript 05-bericht.R","inputs":["work/jam-r/bericht/05-bericht.R"],"outputs":["work/jam-r/bericht/bericht.out"]}'`<br>→ fotonId: `sha256:eedf5bf09e5b2541…`<br>`bericht.out` = `sha256:defc1bafe2f9152c…` | — |
| ✅ **Spectrum-Member grafik-daten im gepinnten jam-r-Image laufen lassen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd":"cd work/jam-r/grafik-daten && Rscript 06-grafik-daten.R","inputs":["work/jam-r/grafik-daten/06-grafik-daten.R"],"outputs":["work/jam-r/grafik-daten/grafik-daten.out"]}'`<br>→ fotonId: `sha256:641cd59d842106f1…`<br>`grafik-daten.out` = `sha256:f75dd1bc9d8112d8…` | — |
| ❌ **Die eigenen Ausgaben gegen das Spectrum halten (identisch oder über den Normalisierer)**<br>_kernel-cli_<br>`PLANKTON_DIR=registry/plankton ./bin/plankton spectrum check packages/jam-r/kton/spectrum.json --candidate json=sha256:28f2841e50343381e09f001facb3f9b87c047d879a4c25cdfb0a24f87ce86db1 --candidate tabelle=sha256:0cce87e13fe345df0da8946de1fcf6689769e50895fea0b8efad6a4e06aa010b --candidate modell=sha256:9e8a620b446144a6f34cfb0f5b2c605ee2db5b192686700d59eefe49be7e99b4 --candidate bericht=sha256:defc1bafe2f9152c8d5407f7985193b52bace7084cf80067d9d8a4f5f815893f --candidate grafik-daten=sha256:f75dd1bc9d8112d8d6d18614bd543af749e47e2516d010fc87eabc8996a0aa43`<br>→ text: json                     fulfilled (identical)<br>  tabelle                  fulfilled (identical)<br>  modell                   fulfilled (identical)<br>  bericht      <br>⚠ Lücke: K3: die Spectrum-Prüfung gibt es nur als plankton-Kommando, nicht als Bibliothek — das Cockpit kann sie nicht aufrufen | — |
| ✅ **Das Normalisierer-Potential des Spectrums (Ray-Step normalisieren, Loch eingabe) aufzeichnen**<br>_ktonpkg_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool foton packages/jam-r normalisieren > /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/potential.json && ./bin/plankton author /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/potential.json keys/session-1.key /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/potential.dsse && ./bin/plankton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/potential.dsse --registry registry/plankton --print-id`<br>→ text: sha256:3486ee0a054438c03208dcfab306eb3f41fb26768e2f0e5a48c8087b05951b27<br>⚠ Lücke: S5: ein Potential aufzeichnen soll cockpit publish {kind: potential} sein | — |
| ✅ **Normalisierer auf die referenz-Ausgabe von bericht anwenden, im gepinnten jam-r-Image**<br>_script_<br>`docker run --rm --network none --user $(id -u):$(id -g) -v $PWD/work/jam-r/normalisiert/bericht/referenz:/w -w /w scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a sh -c 'Rscript fences.R eingabe normalisiert.out'`<br>⚠ Lücke: S5: das Potential realisieren (Loch binden, im gepinnten Image laufen lassen) soll das Cockpit | — |
| ✅ **Die Realisierung als Foton aufzeichnen — Protokoll aus dem Potential, Loch eingabe an die Bytes gebunden**<br>_kernel-cli_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool foton packages/jam-r normalisieren --bind eingabe=$(./bin/plankton hash work/jam-r/normalisiert/bericht/referenz/eingabe) --output normalisiert.out=work/jam-r/normalisiert/bericht/referenz/normalisiert.out > /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.json && ./bin/plankton author /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.json keys/session-1.key /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.dsse && ./bin/plankton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.dsse --registry registry/plankton --print-id`<br>→ text: sha256:a50ab0d9e9f3c6322f635c008bb3dfc555ee0cc8603d0bf8b98d4a84e068d36c<br>⚠ Lücke: S5: die Realisierung aufzeichnen soll cockpit publish {from, bindings} sein | — |
| ✅ **Normalisierer auf die eigen-Ausgabe von bericht anwenden, im gepinnten jam-r-Image**<br>_script_<br>`docker run --rm --network none --user $(id -u):$(id -g) -v $PWD/work/jam-r/normalisiert/bericht/eigen:/w -w /w scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a sh -c 'Rscript fences.R eingabe normalisiert.out'`<br>⚠ Lücke: S5: das Potential realisieren (Loch binden, im gepinnten Image laufen lassen) soll das Cockpit | — |
| ✅ **Die Realisierung als Foton aufzeichnen — Protokoll aus dem Potential, Loch eingabe an die Bytes gebunden**<br>_kernel-cli_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool foton packages/jam-r normalisieren --bind eingabe=$(./bin/plankton hash work/jam-r/normalisiert/bericht/eigen/eingabe) --output normalisiert.out=work/jam-r/normalisiert/bericht/eigen/normalisiert.out > /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.json && ./bin/plankton author /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.json keys/session-1.key /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.dsse && ./bin/plankton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/r.dsse --registry registry/plankton --print-id`<br>→ text: sha256:2dbacb4fd5e8e39975ecf82e0fdbd5b652e003685070d54f164432acfecd8a3a<br>⚠ Lücke: S5: die Realisierung aufzeichnen soll cockpit publish {from, bindings} sein | — |
| ✅ **Erneut gegen das Spectrum halten, jetzt mit aufgezeichneter Normalisierung**<br>_kernel-cli_<br>`PLANKTON_DIR=registry/plankton ./bin/plankton spectrum check packages/jam-r/kton/spectrum.json --candidate json=sha256:28f2841e50343381e09f001facb3f9b87c047d879a4c25cdfb0a24f87ce86db1 --candidate tabelle=sha256:0cce87e13fe345df0da8946de1fcf6689769e50895fea0b8efad6a4e06aa010b --candidate modell=sha256:9e8a620b446144a6f34cfb0f5b2c605ee2db5b192686700d59eefe49be7e99b4 --candidate bericht=sha256:defc1bafe2f9152c8d5407f7985193b52bace7084cf80067d9d8a4f5f815893f --candidate grafik-daten=sha256:f75dd1bc9d8112d8d6d18614bd543af749e47e2516d010fc87eabc8996a0aa43`<br>→ text: json                     fulfilled (identical)<br>  tabelle                  fulfilled (identical)<br>  modell                   fulfilled (identical)<br>  bericht      <br>⚠ Lücke: K3: die Spectrum-Prüfung gibt es nur als plankton-Kommando, nicht als Bibliothek — das Cockpit kann sie nicht aufrufen | — |
| ✅ **Den Befund als signierten Claim festhalten (dasselbe OQ-Prädikat wie auf der improve-Seite)**<br>_cockpit_<br>`./bin/cockpit say '{"subject":"sha256:e0669ff3f8f756868721fe603e72ee1243e93805b3cf422affd261dc311a8bee","template":"oq-executed","fields":{"tool":"oci://docker.io/scinteco/jam-r@sha256:dee2a8cfb95adf7eb63213da617a1ac538e0e328477ece0f07920bd9e513294a","verdict":"L1","candidates":"--candidate json=sha256:28f2841e50343381e09f001facb3f9b87c047d879a4c25cdfb0a24f87ce86db1 --candidate tabelle=sha256:0cce87e13fe345df0da8946de1fcf6689769e50895fea0b8efad6a4e06aa010b --candidate modell=sha256:9e8a620b446144a6f34cfb0f5b2c605ee2db5b192686700d59eefe49be7e99b4 --candidate bericht=sha256:defc1bafe2f9152c8d5407f7985193b52bace7084cf80067d9d8a4f5f815893f --candidate grafik-daten=sha256:f75dd1bc9d8112d8d6d18614bd543af749e47e2516d010fc87eabc8996a0aa43"}}'`<br>→ claimId: `sha256:648c6bbfb4f69a31…` | — |

### Workflow laden

| git | improve |
|---|---|
| ✅ **Das Workflow-Paket öffnen und prüfen: Identität, Ray, Löcher, Bedingungen**<br>_ktonpkg_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool inspect packages/raute`<br>→ spectrumId: `sha256:af4e183638c7ebe1…`<br>bundleId: `sha256:0cb0d874284a939f…`<br>rayId: `sha256:0d6a28044aa6d22d…`<br>⚠ Lücke: S6: cockpit kennt kton-Pakete noch nicht | ✅ **das Raute-Paket öffnen, prüfen und in den Testordner einspielen**<br>_ktonpkg_<br>`ktonpkg.Open("/mnt/c/dev/workstreams/active/improveGo/examples/raute/paket").Verify(); c.InstallPackage(ctx, b, InstallOptions{TargetFolder: "/dst/raute-vergleich-20260930T164545/raute", Tool: <jam-r>})`<br>→ loecher: ["anzahl (param, Vorgabe \"1\")", "dataset (file, Vorgabe \"testdaten/dataset.csv\")"]<br>paket: sha256:0cb0d874284a939f9ef9c9d425232f8a613bb36c1cd62cb6bbeb67540940c06a<br>potential: sha256:0d6a28044aa6d22dbec51efdbc70e42289e7397f88439561e6c84d8bdeb5d036<br>⚠ Lücke: S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego |

### Bedingung des Workflows prüfen

| git | improve |
|---|---|
| ✅ **Verlangt das Paket ein Werkzeug, das hier qualifiziert ist?**<br>_script_<br>`python3 -c 'import json,sys; ok=sys.argv[1]==sys.argv[2] and sys.argv[3]!="failed"; print(json.dumps({"required":sys.argv[1],"qualified":sys.argv[2],"verdict":sys.argv[3],"fulfilled":ok})); sys.exit(0 if ok else 1)' sha256:e0669ff3f8f756868721fe603e72ee1243e93805b3cf422affd261dc311a8bee sha256:e0669ff3f8f756868721fe603e72ee1243e93805b3cf422affd261dc311a8bee L1`<br>→ verdict: `L1`<br>erfüllt: ja<br>⚠ Lücke: S6: die Bedingung eines Pakets gegen qualifizierte Werkzeuge halten gehört ins Cockpit | ✅ **verlangt das Paket genau das Spectrum, das die qualifizierte Instanz erfüllt?**<br>_improvego_<br>`b.Requires[i].Spectrum == toolBundle.Spectrum.ID(); c.Resolve(ctx, b, OpenRegister("/dst/raute-vergleich-20260930T164545/kton"))`<br>→ erfuelltVonInstanz: sha256:e0669ff3f8f756868721fe603e72ee1243e93805b3cf422affd261dc311a8bee<br>passt: true<br>resolveErfuellt: false<br>⚠ Lücke: S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego |

### Ungebunden starten — muss abgelehnt werden

| git | improve |
|---|---|
| ✅ **Ohne Bindungen (und ohne Testdatensatz als Vorgabe) darf der Workflow nicht anlaufen**<br>_ktonpkg_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool plan packages/raute work/raute --no-defaults`<br>→ reason: `offene Pflichtlöcher: anzahl, dataset`<br>offen: anzahl, dataset<br>⚠ Lücke: S5: im Zielbild lehnt cockpit publish {from, bindings} das selbst ab | ✅ **ohne Bindung und ohne Paketvorgaben realisieren — muss abgelehnt werden (URS-6)**<br>_improvego_<br>`c.Realize(ctx, tpl /* Vorgaben von dataset und anzahl entfernt */, RealizeOptions{TreeID: <ungebunden>, Bindings: {}, Run: true})`<br>→ ablehnung: Required parameters not set: anzahl, dataset<br>stepsAngelegt: 0<br>vorgabenEntfernt: ["anzahl", "dataset"]<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |

### Löcher füllen

| git | improve |
|---|---|
| ✅ **Löcher binden: dataset = eigener Datensatz, anzahl = 3; daraus die Steps in Reihenfolge**<br>_ktonpkg_<br>`/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/pkgtool plan packages/raute work/raute --bind dataset=data/eigene-daten.csv --bind anzahl=3`<br>→ bindings: {"anzahl": "3", "dataset": "data/eigene-daten.csv"}<br>steps: [{"id": "aufbereitung", "dir": "work/raute/aufbereitung", "stage": [{"from": "data/eigene-daten.csv", "to": "work/raute/aufbereitung/dataset.csv"}, {"from": "pa<br>⚠ Lücke: S5: Löcher binden und realisieren soll cockpit publish {from, bindings} sein | ✅ **eigene Daten hochladen und die Löcher binden: dataset = eigene-daten.csv, anzahl = 3**<br>_improvego_<br>`c.CreateFile(ctx, "/dst/raute-vergleich-20260930T164545", "/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/eigene-daten.csv"); bindings = {dataset: <resourceId>, anzahl: "3"}`<br>→ bindings: {"anzahl": "3", "dataset": "<improve-id>"}<br>datei: eigene-daten.csv<br>hash: sha256:ab0a3a6253897602cb84d4592293e905714a017ec4333254ca80b836780df12b<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |

### Laufen lassen

| git | improve |
|---|---|
| ✅ **Step aufbereitung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd": "cd work/raute/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/raute/aufbereitung/dataset.csv", "work/raute/aufbereitung/aufbereiten.R"], "outputs": ["work/raute/aufbereitung/daten.txt"]}'`<br>→ fotonId: `sha256:4afc54a1d6f2b61b…`<br>`daten.txt` = `sha256:ab0a3a6253897602…`<br>⚠ Lücke: S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst | ✅ **Step aufbereitung realisieren, laufen lassen und als signiertes Foton ins Register**<br>_improve-server_<br>`c.Realize(ctx, tpl, RealizeOptions{TreeID: <Raute>, Bindings: {dataset: "<improve-id>", anzahl: "3"}, Run: true, Register: "/dst/raute-vergleich-20260930T164545/kton"}) — Step aufbereitung; danach c.ExportFoton(ctx, <stepId>, reg)`<br>→ ausgabe: daten.txt<br>foton: sha256:5e72c51daef04200976243ea979b528591af7a738289705e9c49da1a61bc0bc6<br>hash: sha256:ab0a3a6253897602cb84d4592293e905714a017ec4333254ca80b836780df12b<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ **Step zweig-c im gepinnten jam-r-Image ausführen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd": "cd work/raute/zweig-c && Rscript zweigC.R", "inputs": ["work/raute/zweig-c/daten.txt", "work/raute/zweig-c/zweigC.R"], "outputs": ["work/raute/zweig-c/c.txt"]}'`<br>→ fotonId: `sha256:bc72f4dbdaa32951…`<br>`c.txt` = `sha256:fb76c5fd07819ff9…`<br>⚠ Lücke: S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst | ✅ **Step zweig-c realisieren, laufen lassen und als signiertes Foton ins Register**<br>_improve-server_<br>`c.Realize(ctx, tpl, RealizeOptions{TreeID: <Raute>, Bindings: {dataset: "<improve-id>", anzahl: "3"}, Run: true, Register: "/dst/raute-vergleich-20260930T164545/kton"}) — Step zweig-c; danach c.ExportFoton(ctx, <stepId>, reg)`<br>→ ausgabe: c.txt<br>foton: sha256:fa5cd0add6216a9c5f5af602656cbde7c6bfe6370b86e752a6cd5cba29f1cc21<br>hash: sha256:fb76c5fd07819ff96996d189cd08fe52c64894e55d3dba89eebbebb495f60739<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ **Step zweig-b im gepinnten jam-r-Image ausführen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd": "cd work/raute/zweig-b && Rscript zweigB.R 3", "inputs": ["work/raute/zweig-b/zweigB.R", "work/raute/zweig-b/daten.txt"], "outputs": ["work/raute/zweig-b/b.txt"]}'`<br>→ fotonId: `sha256:8aade9fc2f7c7913…`<br>`b.txt` = `sha256:e908d8332bb7d78e…`<br>⚠ Lücke: S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst | ✅ **Step zweig-b realisieren, laufen lassen und als signiertes Foton ins Register**<br>_improve-server_<br>`c.Realize(ctx, tpl, RealizeOptions{TreeID: <Raute>, Bindings: {dataset: "<improve-id>", anzahl: "3"}, Run: true, Register: "/dst/raute-vergleich-20260930T164545/kton"}) — Step zweig-b; danach c.ExportFoton(ctx, <stepId>, reg)`<br>→ ausgabe: b.txt<br>foton: sha256:142298e492946eed7d57eeca5295933f0df60f1e1438c77297caa2806f96219b<br>hash: sha256:e908d8332bb7d78ee94deadbcda35d4525de0e85d1fe4c508a5668d5520e7a31<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |
| ✅ **Step zusammenführung im gepinnten jam-r-Image ausführen und als Foton veröffentlichen**<br>_cockpit_<br>`./bin/cockpit publish '{"cmd": "cd work/raute/zusammenführung && Rscript zusammen.R", "inputs": ["work/raute/zusammenführung/zusammen.R", "work/raute/zusammenführung/b.txt", "work/raute/zusammenführung/c.txt"], "outputs": ["work/raute/zusammenführung/ergebnis.txt"]}'`<br>→ fotonId: `sha256:b45494f6cbcbcb40…`<br>`ergebnis.txt` = `sha256:44d419866f5a847c…`<br>⚠ Lücke: S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst | ✅ **Step zusammenführung realisieren, laufen lassen und als signiertes Foton ins Register**<br>_improve-server_<br>`c.Realize(ctx, tpl, RealizeOptions{TreeID: <Raute>, Bindings: {dataset: "<improve-id>", anzahl: "3"}, Run: true, Register: "/dst/raute-vergleich-20260930T164545/kton"}) — Step zusammenführung; danach c.ExportFoton(ctx, <stepId>, reg)`<br>→ ausgabe: ergebnis.txt<br>foton: sha256:172d20fbeff48789b7d345d9b5ae7088680fea5033ea6a318730a3f5f626cbf8<br>hash: sha256:44d419866f5a847c591f1c4a3f9e46ff6416985c1b57840413d986ba60ba01e1<br>⚠ Lücke: S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego |

### Nachprüfen

| git | improve |
|---|---|
| ✅ **Vom Ergebnis zurück zum Datensatz: die Linie, gegen die Vertrauensstufen dieses Repos verifiziert**<br>_cockpit_<br>`./bin/cockpit ask '{"query":"lineage","ref":"work/raute/zusammenführung/ergebnis.txt"}'`<br>→ query: lineage<br>ref: sha256:44d419866f5a847c591f1c4a3f9e46ff6416985c1b57840413d986ba60ba01e1<br>raw: sha256:b45494f6cbcbcb405c7417f1e9b9c589b6af14dfca262a1db1850774b6e3d262  kind=script  in=3 out=1<br>sha256:8aade9fc2f7c7913a30e075c9df99713fe03f76f6a3c15e5b2262790 | ✅ **aus dem Register zurück: Herkunft von ergebnis.txt bis zum Datensatz**<br>_improvego_<br>`improve.OpenRegister(ctx, "/dst/raute-vergleich-20260930T164545/kton").Lineage(ctx, <hash ergebnis.txt>)`<br>→ erreichtDatensatz: true<br>kette: [{"foton": "sha256:172d20fbeff48789b7d345d9b5ae7088680fea5033ea6a318730a3f5f626cbf8", "protokoll": "improve/step", "eingaben": ["sha256:e908d8332bb7d78ee94deadb<br>steps: ["zusammenführung", "zweig-b", "aufbereitung", "zweig-c"]<br>⚠ Lücke: S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego; `cockpit ask lineage` sollte das gegen das improve-Register beantworten |

## 3 Wo das Cockpit noch nicht handelt

**git** — 17 von 27 Schritten:

- `tool-qualify:inspect` (ktonpkg): S6: cockpit kennt kton-Pakete noch nicht
- `tool-qualify:referenzen` (kernel-cli): K3/S6: Referenz-Fotons des Spectrums übernimmt heute die Kernel-CLI
- `tool-qualify:check` (kernel-cli): K3: die Spectrum-Prüfung gibt es nur als plankton-Kommando, nicht als Bibliothek — das Cockpit kann sie nicht aufrufen
- `tool-qualify:normalizer-potential` (ktonpkg): S5: ein Potential aufzeichnen soll cockpit publish {kind: potential} sein
- `tool-qualify:normalize:bericht:referenz` (script): S5: das Potential realisieren (Loch binden, im gepinnten Image laufen lassen) soll das Cockpit
- `tool-qualify:record:bericht:referenz` (kernel-cli): S5: die Realisierung aufzeichnen soll cockpit publish {from, bindings} sein
- `tool-qualify:normalize:bericht:eigen` (script): S5: das Potential realisieren (Loch binden, im gepinnten Image laufen lassen) soll das Cockpit
- `tool-qualify:record:bericht:eigen` (kernel-cli): S5: die Realisierung aufzeichnen soll cockpit publish {from, bindings} sein
- `tool-qualify:check-normalized` (kernel-cli): K3: die Spectrum-Prüfung gibt es nur als plankton-Kommando, nicht als Bibliothek — das Cockpit kann sie nicht aufrufen
- `load:inspect` (ktonpkg): S6: cockpit kennt kton-Pakete noch nicht
- `require:spectrum` (script): S6: die Bedingung eines Pakets gegen qualifizierte Werkzeuge halten gehört ins Cockpit
- `refuse-unbound:plan` (ktonpkg): S5: im Zielbild lehnt cockpit publish {from, bindings} das selbst ab
- `bind:plan` (ktonpkg): S5: Löcher binden und realisieren soll cockpit publish {from, bindings} sein
- `run:aufbereitung` (cockpit): S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst
- `run:zweig-c` (cockpit): S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst
- `run:zweig-b` (cockpit): S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst
- `run:zusammenführung` (cockpit): S5: heute ein publish je Step mit ausgeschriebenem Befehl; im Zielbild realisiert das Cockpit das Potential selbst

**improve** — 12 von 12 Schritten:

- `setup` (improvego): S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego
- `tool-install` (improve-server): S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego; Werkzeuginstallation hat im Cockpit kein Verb — sie gehört zu S6 (Werkzeugpaket über ktonpkg)
- `tool-qualify` (improvego): S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego
- `load` (ktonpkg): S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego
- `require` (improvego): S6: Rays und Spectren über ktonpkg im Cockpit (Spectrum-Erfüllung als Kernel-Funktion erst mit K3) — heute improvego
- `refuse-unbound` (improvego): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `bind` (improvego): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `run:aufbereitung` (improve-server): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `run:zweig-c` (improve-server): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `run:zweig-b` (improve-server): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `run:zusammenführung` (improve-server): S5: Potentiale und Realisierung gehören ins Cockpit (backend/improve: bind, realise) — heute improvego
- `verify` (improvego): S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego; `cockpit ask lineage` sollte das gegen das improve-Register beantworten

