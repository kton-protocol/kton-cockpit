# Workflow-Bedienung — alle Cockpit-Aufrufe

Erzeugt von `acceptance/workflow-bedienung/lauf.sh`. Jeder Aufruf genau so, wie er lief, im Repo
der genannten Seite, mit der Ausgabe, die ein Mensch sieht. Lange Ausgaben sind gekürzt.


## 0 Vorbedingung: das Workflow-Vokabular

Das Vokabularpaket `kton-workflow` (Templates: installed, reproduces, derived-from; Abfrage: workflows)
hat die Id `sha256:08ca7978225776cfca25be5d82d522e07a54a905ede4c99a28adb3e84b344861`. Der Operator trägt sie in beiden Repos unter `claims.allowedPackages` ein.

### A — Vokabular installieren

```
$ ./bin/cockpit install ../../kton-workflow
installed kton-workflow, revision 1  →  packages/kton-workflow@1
  package   sha256:08ca7978225776cfca25be5d82d522e07a54a905ede4c99a28adb3e84b344861
  profile   templates
  sealed    a0d38c5539de (head 0713ca0c8a9a)
  scope     kton-workflow@1 (db54ac10562a)
  recorded  installed, claim 2110488cbb2b
  allowed   yes — its templates and queries are in use
```


## 1 A: die Raute Schritt für Schritt ausführen, zweimal

### A — Lauf 1, Step aufbereitung

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-1/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/lauf-1/aufbereitung/aufbereiten.R", "work/lauf-1/aufbereitung/dataset.csv"], "outputs": ["work/lauf-1/aufbereitung/daten.txt"]}' --field fotonId
sha256:f4831849971895ff121bc0f7092af2b2e78773e39a1a27bc1219fb0892702a93
```

### A — Lauf 1, Step zweig-c

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-1/zweig-c && Rscript zweigC.R", "inputs": ["work/lauf-1/zweig-c/daten.txt", "work/lauf-1/zweig-c/zweigC.R"], "outputs": ["work/lauf-1/zweig-c/c.txt"]}' --field fotonId
sha256:5ecdf7a4e046d0d7e836f59ee46bd6c0e6da53c9f41df4de5b2b2e2276297642
```

### A — Lauf 1, Step zweig-b

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-1/zweig-b && Rscript zweigB.R 1", "inputs": ["work/lauf-1/zweig-b/daten.txt", "work/lauf-1/zweig-b/zweigB.R"], "outputs": ["work/lauf-1/zweig-b/b.txt"]}' --field fotonId
sha256:3ba45bd420142a1f84540087df3d9273a698a46a0429a753119c3502f1b08be2
```

### A — Lauf 1, Step zusammenführung

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-1/zusammenführung && Rscript zusammen.R", "inputs": ["work/lauf-1/zusammenführung/b.txt", "work/lauf-1/zusammenführung/c.txt", "work/lauf-1/zusammenführung/zusammen.R"], "outputs": ["work/lauf-1/zusammenführung/ergebnis.txt"]}' --field fotonId
sha256:5a91c47260fa01a787a835e16abb16cfe81f770658004e5e669a39ba817d2e3b
```

### A — Lauf 2, Step aufbereitung

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-2/aufbereitung && Rscript aufbereiten.R", "inputs": ["work/lauf-2/aufbereitung/aufbereiten.R", "work/lauf-2/aufbereitung/dataset.csv"], "outputs": ["work/lauf-2/aufbereitung/daten.txt"]}' --field fotonId
sha256:57e0a40531f9c49e5a4ceb228dd7d7fb156eb2b8e28d284d74dbb8bc500b8d5d
```

### A — Lauf 2, Step zweig-c

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-2/zweig-c && Rscript zweigC.R", "inputs": ["work/lauf-2/zweig-c/daten.txt", "work/lauf-2/zweig-c/zweigC.R"], "outputs": ["work/lauf-2/zweig-c/c.txt"]}' --field fotonId
sha256:a71eb29da75c3672fd62348fe5e0dd63c305594bd46aa7747f40e4b536acd5ac
```

### A — Lauf 2, Step zweig-b

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-2/zweig-b && Rscript zweigB.R 3", "inputs": ["work/lauf-2/zweig-b/daten.txt", "work/lauf-2/zweig-b/zweigB.R"], "outputs": ["work/lauf-2/zweig-b/b.txt"]}' --field fotonId
sha256:0273016c3c462a0cab0bdb6165496eee4030cb3ef57034978fc70dae3138a51f
```

### A — Lauf 2, Step zusammenführung

```
$ ./bin/cockpit publish '{"cmd": "cd work/lauf-2/zusammenführung && Rscript zusammen.R", "inputs": ["work/lauf-2/zusammenführung/b.txt", "work/lauf-2/zusammenführung/c.txt", "work/lauf-2/zusammenführung/zusammen.R"], "outputs": ["work/lauf-2/zusammenführung/ergebnis.txt"]}' --field fotonId
sha256:5aac583ce211280c518140d8c855868d3a9d32765a20a6f335b7bbd985ff1172
```


## 2 A: den Workflow herausziehen, die Raute als Referenz mitpacken

### A — Was bieten die Läufe an?

```
$ ./bin/cockpit workflow propose work/lauf-1/zusammenführung/ergebnis.txt work/lauf-2/zusammenführung/ergebnis.txt
2 run(s) behind work/lauf-1/zusammenführung/ergebnis.txt, work/lauf-2/zusammenführung/ergebnis.txt
  run-1  ends in work/lauf-2/zusammenführung/ergebnis.txt  (4 executions)
  run-2  ends in work/lauf-1/zusammenführung/ergebnis.txt  (4 executions)
  (--reference takes a run or the result it ended in)

steps
  aufbereitung       aufbereiten.R
  zweig-b            zweigB.R
  zweig-c            zweigC.R
  zusammenführung    zusammen.R

could be holes (--hole NAME=STEP/SLOT)
  aufbereitung/dataset.csv  — differs between runs

could be parameters (--param NAME=STEP/POSITION)
  zweig-b/2  — run-1: 3, run-2: 1
```

### A — Herausziehen: Lauf 1 als Referenz, dataset als Loch, anzahl als Parameter

```
$ ./bin/cockpit workflow extract work/lauf-1/zusammenführung/ergebnis.txt work/lauf-2/zusammenführung/ergebnis.txt --name raute --reference work/lauf-1/zusammenführung/ergebnis.txt --hole dataset=aufbereitung/dataset.csv --param anzahl=zweig-b/2
extracted raute  →  packages/raute
  package     sha256:f1cced11d13c7240d62563c84ba58ae447f4c150301589d8e8c94f14aabeec75
  ray         sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  holes       dataset
  parameters  anzahl
  reference   4 run(s) of work/lauf-1/zusammenführung/ergebnis.txt packed with it
  sealed      f43ee7f857c6
```


## 3 B: installieren

### B — Vokabular installieren

```
$ ./bin/cockpit install ../../kton-workflow
installed kton-workflow, revision 1  →  packages/kton-workflow@1
  package   sha256:08ca7978225776cfca25be5d82d522e07a54a905ede4c99a28adb3e84b344861
  profile   templates
  sealed    a0d38c5539de (head 0713ca0c8a9a)
  scope     kton-workflow@1 (30fb3b3ea8c5)
  recorded  installed, claim c51bbb049f47
  allowed   yes — its templates and queries are in use
```

### B — Den Workflow installieren

```
$ ./bin/cockpit install ../../a/repo/packages/raute
installed raute, revision 1  →  packages/raute@1
  package   sha256:f1cced11d13c7240d62563c84ba58ae447f4c150301589d8e8c94f14aabeec75
  profile   workflow
  sealed    f43ee7f857c6 (head a62b5420b2c7)
  ray       sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  scope     raute@1 (39ce68833ed9)
  recorded  installed, claim 59916c8f2fbe
```


## 4 B: suchen, auswählen

### B — Welche Workflows gibt es hier?

```
$ ./bin/cockpit workflow list
1 workflow(s) installed  (query from kton-workflow (packages/kton-workflow@1))

  raute            revision 1    ray 764913c16c13  sealed f43ee7f857c6
```

### B — Die Raute ansehen

```
$ ./bin/cockpit workflow show raute
raute, revision 1  (packages/raute@1)
  ray     sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  sealed  f43ee7f857c6

steps
  1. aufbereitung     Rscript aufbereiten.R
     in:  aufbereiten.R ← fixed, dataset.csv ← hole:dataset
     out: daten.txt  (reference packed)
  2. zweig-b          Rscript zweigB.R <anzahl>
     in:  daten.txt ← upstream:aufbereitung:daten.txt, zweigB.R ← fixed
     out: b.txt  (reference packed)
  3. zweig-c          Rscript zweigC.R
     in:  daten.txt ← upstream:aufbereitung:daten.txt, zweigC.R ← fixed
     out: c.txt  (reference packed)
  4. zusammenführung  Rscript zusammen.R
     in:  b.txt ← upstream:zweig-b:b.txt, c.txt ← upstream:zweig-c:c.txt, zusammen.R ← fixed
     out: ergebnis.txt  (reference packed)

holes (--bind NAME=VALUE)
  anzahl       param  required  test data: 1
  dataset      file   required  test data: testdaten/dataset/dataset.csv
```


## 5 B: durchführen

### B — Ohne Bindung (soll ablehnen)

```
$ ./bin/cockpit workflow run raute
offene Pflichtlöcher: anzahl, dataset — bind them with --bind NAME=VALUE, or run --check to run on the package's test data
[exit 2]
```

### B — Nachprüfen mit den Testdaten

```
$ ./bin/cockpit workflow run raute --check
ran raute in work/lauf-1  (anzahl=1, dataset=testdaten/dataset/dataset.csv)
  aufbereitung     foton f48318499718  reproduces the reference (L0, same record, co-signed)
  zweig-b          foton 3ba45bd42014  reproduces the reference (L0, same record, co-signed)
  zweig-c          foton 5ecdf7a4e046  reproduces the reference (L0, same record, co-signed)
  zusammenführung  foton 5a91c47260fa  reproduces the reference (L0, same record, co-signed)
```

### B — Mit eigenen Daten

```
$ ./bin/cockpit workflow run raute --bind dataset=data/eigene-daten.csv --bind anzahl=3 --dir work/eigen
ran raute in work/eigen  (anzahl=3, dataset=data/eigene-daten.csv)
  aufbereitung     foton 9ccba9526d99
  zweig-b          foton afb7134698ca
  zweig-c          foton 41ab74a2bc15
  zusammenführung  foton c05bb202fead
```

### B — Woher kommt mein Ergebnis?

```
$ ./bin/cockpit ask '{"query":"lineage","ref":"work/eigen/zusammenführung/ergebnis.txt"}' --field raw
sha256:c05bb202fead574a6683feacb1a43ca182eaa35223b12f8d2e8e1137a0aed573  kind=script  in=3 out=1
sha256:afb7134698ca15b89ef86159ffa280c40fe2df2ffb2f1db1bd147a615d75db09  kind=script  in=2 out=1
sha256:9ccba9526d990e9ee7c323ae0020dbf0c65939bd91d996b3a865766646e5c87b  kind=script  in=2 out=1
sha256:41ab74a2bc158767581ee27f74b13d0ee84261ac55603458af5edbd813d1a3d1  kind=script  in=2 out=1
```

