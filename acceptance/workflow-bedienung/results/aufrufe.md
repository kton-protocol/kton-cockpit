# Workflow-Bedienung — alle Cockpit-Aufrufe

Erzeugt von `acceptance/workflow-bedienung/lauf.sh`. Jeder Aufruf genau so, wie er lief, im Repo
der genannten Seite, mit der Ausgabe, die ein Mensch sieht. Lange Ausgaben sind gekürzt.


## 0 Vorbedingung: das Workflow-Vokabular

Das Vokabularpaket `kton-workflow` (Templates: installed, reproduces, derived-from; Abfrage: workflows)
hat die Id `sha256:52ec08ba6a86acbe1cf396ac486d9e64196de64479fafb3b12bd160807e4eb2c`. Der Operator installiert es mit `--allow`; das trägt die Id in `claims.allowedPackages` ein.

### A — Vokabular installieren und zulassen

```
$ ./bin/cockpit install ../../kton-workflow --allow
installed kton-workflow, revision 1  →  packages/kton-workflow@1
  package   sha256:52ec08ba6a86acbe1cf396ac486d9e64196de64479fafb3b12bd160807e4eb2c
  profile   templates
  sealed    377944ebff2c (head 8777708d5a1f)
  scope     kton-workflow@1 (d5ce83a4ab69)
  recorded  installed, claim ea47f0690f8f
  allowed   yes, added to claims.allowedPackages — its templates and queries are in use
```


## 1 A: die Raute Schritt für Schritt ausführen, zweimal

### A — Lauf 1, Step aufbereitung

```
$ ./bin/cockpit run --dir work/lauf-1/aufbereitung --in quelle/testdaten/dataset.csv --in quelle/steps/aufbereitung/aufbereiten.R -- Rscript aufbereiten.R
ran  Rscript aufbereiten.R  in work/lauf-1/aufbereitung
  foton    sha256:f4831849971895ff121bc0f7092af2b2e78773e39a1a27bc1219fb0892702a93
  output   work/lauf-1/aufbereitung/daten.txt
```

### A — Lauf 1, Step zweig-c

```
$ ./bin/cockpit run --dir work/lauf-1/zweig-c --in work/lauf-1/aufbereitung/daten.txt --in quelle/steps/zweig-c/zweigC.R -- Rscript zweigC.R
ran  Rscript zweigC.R  in work/lauf-1/zweig-c
  foton    sha256:5ecdf7a4e046d0d7e836f59ee46bd6c0e6da53c9f41df4de5b2b2e2276297642
  output   work/lauf-1/zweig-c/c.txt
```

### A — Lauf 1, Step zweig-b

```
$ ./bin/cockpit run --dir work/lauf-1/zweig-b --in quelle/steps/zweig-b/zweigB.R --in work/lauf-1/aufbereitung/daten.txt -- Rscript zweigB.R 1
ran  Rscript zweigB.R 1  in work/lauf-1/zweig-b
  foton    sha256:3ba45bd420142a1f84540087df3d9273a698a46a0429a753119c3502f1b08be2
  output   work/lauf-1/zweig-b/b.txt
```

### A — Lauf 1, Step zusammenführung

```
$ ./bin/cockpit run --dir 'work/lauf-1/zusammenführung' --in 'quelle/steps/zusammenführung/zusammen.R' --in work/lauf-1/zweig-b/b.txt --in work/lauf-1/zweig-c/c.txt -- Rscript zusammen.R
ran  Rscript zusammen.R  in work/lauf-1/zusammenführung
  foton    sha256:5a91c47260fa01a787a835e16abb16cfe81f770658004e5e669a39ba817d2e3b
  output   work/lauf-1/zusammenführung/ergebnis.txt
```

### A — Lauf 2, Step aufbereitung

```
$ ./bin/cockpit run --dir work/lauf-2/aufbereitung --in dataset.csv=data/eigene-daten.csv --in quelle/steps/aufbereitung/aufbereiten.R -- Rscript aufbereiten.R
ran  Rscript aufbereiten.R  in work/lauf-2/aufbereitung
  foton    sha256:57e0a40531f9c49e5a4ceb228dd7d7fb156eb2b8e28d284d74dbb8bc500b8d5d
  output   work/lauf-2/aufbereitung/daten.txt
```

### A — Lauf 2, Step zweig-c

```
$ ./bin/cockpit run --dir work/lauf-2/zweig-c --in work/lauf-2/aufbereitung/daten.txt --in quelle/steps/zweig-c/zweigC.R -- Rscript zweigC.R
ran  Rscript zweigC.R  in work/lauf-2/zweig-c
  foton    sha256:a71eb29da75c3672fd62348fe5e0dd63c305594bd46aa7747f40e4b536acd5ac
  output   work/lauf-2/zweig-c/c.txt
```

### A — Lauf 2, Step zweig-b

```
$ ./bin/cockpit run --dir work/lauf-2/zweig-b --in quelle/steps/zweig-b/zweigB.R --in work/lauf-2/aufbereitung/daten.txt -- Rscript zweigB.R 3
ran  Rscript zweigB.R 3  in work/lauf-2/zweig-b
  foton    sha256:0273016c3c462a0cab0bdb6165496eee4030cb3ef57034978fc70dae3138a51f
  output   work/lauf-2/zweig-b/b.txt
```

### A — Lauf 2, Step zusammenführung

```
$ ./bin/cockpit run --dir 'work/lauf-2/zusammenführung' --in 'quelle/steps/zusammenführung/zusammen.R' --in work/lauf-2/zweig-b/b.txt --in work/lauf-2/zweig-c/c.txt -- Rscript zusammen.R
ran  Rscript zusammen.R  in work/lauf-2/zusammenführung
  foton    sha256:5aac583ce211280c518140d8c855868d3a9d32765a20a6f335b7bbd985ff1172
  output   work/lauf-2/zusammenführung/ergebnis.txt
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

candidates — choose with --hole NAME=N (files) and --param NAME=N (command-line places)
  [1] file  aufbereitung/dataset.csv     run-1: ab0a3a625389, run-2: e80079e56ed5
  [2] param zweig-b, argument 2          run-1: 3, run-2: 1
```

### A — Herausziehen: Lauf 1 als Referenz, dataset als Loch, anzahl als Parameter

```
$ ./bin/cockpit workflow extract work/lauf-1/zusammenführung/ergebnis.txt work/lauf-2/zusammenführung/ergebnis.txt --name raute --reference work/lauf-1/zusammenführung/ergebnis.txt --hole dataset=1 --param anzahl=2
extracted raute  →  packages/raute
  package     sha256:d92c9eb3ddcd32ec0f64a2c2b3a1509d36d5eadd0c6143b5a92ee2aa10076b5e
  ray         sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  holes       dataset
  parameters  anzahl
  reference   4 run(s) of work/lauf-1/zusammenführung/ergebnis.txt packed with it
  sealed      3ce46066370e
```


## 3 B: installieren

### B — Vokabular installieren und zulassen

```
$ ./bin/cockpit install ../../kton-workflow --allow
installed kton-workflow, revision 1  →  packages/kton-workflow@1
  package   sha256:52ec08ba6a86acbe1cf396ac486d9e64196de64479fafb3b12bd160807e4eb2c
  profile   templates
  sealed    377944ebff2c (head 8777708d5a1f)
  scope     kton-workflow@1 (11f1df2c1004)
  recorded  installed, claim 400411c1d9e7
  allowed   yes, added to claims.allowedPackages — its templates and queries are in use
```

### B — Den Workflow installieren

```
$ ./bin/cockpit install ../../a/repo/packages/raute
installed raute, revision 1  →  packages/raute@1
  package   sha256:d92c9eb3ddcd32ec0f64a2c2b3a1509d36d5eadd0c6143b5a92ee2aa10076b5e
  profile   workflow
  sealed    3ce46066370e (head 66bafb82ad79)
  ray       sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  scope     raute@1 (6ce36b36e34d)
  recorded  installed, claim a11829327de8
```


## 4 B: suchen, auswählen

### B — Welche Workflows gibt es hier?

```
$ ./bin/cockpit workflow list
1 workflow(s) installed  (query from kton-workflow (packages/kton-workflow@1))

  raute            revision 1    ray 764913c16c13  sealed 3ce46066370e
```

### B — Die Raute ansehen

```
$ ./bin/cockpit workflow show raute
raute, revision 1  (packages/raute@1)
  ray     sha256:764913c16c1382aa071c1e1ba424f0b613933e185b6a64295d08e9d4590a2a28
  sealed  3ce46066370e

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
required holes not bound: anzahl, dataset — bind them with --bind NAME=VALUE, or run --check to run on the package's test data
[exit 2]
```

### B — Nachprüfen mit den Testdaten

```
$ ./bin/cockpit workflow run raute --check
ran raute in work/lauf-1  (anzahl=1, dataset=testdaten/dataset/dataset.csv)
  work/lauf-1 is where the reference ran: paths are part of a record's identity, so only there
  is the same work the same record — and your run signs the reference instead of standing beside it
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
$ ./bin/cockpit workflow trace work/eigen/zusammenführung/ergebnis.txt
work/eigen/zusammenführung/ergebnis.txt comes from 4 step(s), each verified against this repo's trust tiers:

  1. aufbereitung     Rscript aufbereiten.R   (foton 9ccba9526d99, signed by self)
     in:  work/eigen/aufbereitung/aufbereiten.R, work/eigen/aufbereitung/dataset.csv
     out: work/eigen/aufbereitung/daten.txt

  2. zweig-b          Rscript zweigB.R 3   (foton afb7134698ca, signed by self)
     in:  work/eigen/zweig-b/daten.txt, work/eigen/zweig-b/zweigB.R
     out: work/eigen/zweig-b/b.txt

  3. zweig-c          Rscript zweigC.R   (foton 41ab74a2bc15, signed by self)
     in:  work/eigen/zweig-c/daten.txt, work/eigen/zweig-c/zweigC.R
     out: work/eigen/zweig-c/c.txt

  4. zusammenführung  Rscript zusammen.R   (foton c05bb202fead, signed by self)
     in:  work/eigen/zusammenführung/b.txt, work/eigen/zusammenführung/c.txt, work/eigen/zusammenführung/zusammen.R
     out: work/eigen/zusammenführung/ergebnis.txt
```

### B — Und das Ergebnis der Nachprüfung?

```
$ ./bin/cockpit workflow trace work/lauf-1/zusammenführung/ergebnis.txt
work/lauf-1/zusammenführung/ergebnis.txt comes from 4 step(s), each verified against this repo's trust tiers:

  1. aufbereitung     Rscript aufbereiten.R   (foton f48318499718, signed by autorin + self)
     in:  work/lauf-1/aufbereitung/aufbereiten.R, work/lauf-1/aufbereitung/dataset.csv
     out: work/lauf-1/aufbereitung/daten.txt
     this is the reference run of raute@1, step aufbereitung

  2. zweig-b          Rscript zweigB.R 1   (foton 3ba45bd42014, signed by autorin + self)
     in:  work/lauf-1/zweig-b/daten.txt, work/lauf-1/zweig-b/zweigB.R
     out: work/lauf-1/zweig-b/b.txt
     this is the reference run of raute@1, step zweig-b

  3. zweig-c          Rscript zweigC.R   (foton 5ecdf7a4e046, signed by autorin + self)
     in:  work/lauf-1/zweig-c/daten.txt, work/lauf-1/zweig-c/zweigC.R
     out: work/lauf-1/zweig-c/c.txt
     this is the reference run of raute@1, step zweig-c

  4. zusammenführung  Rscript zusammen.R   (foton 5a91c47260fa, signed by autorin + self)
     in:  work/lauf-1/zusammenführung/b.txt, work/lauf-1/zusammenführung/c.txt, work/lauf-1/zusammenführung/zusammen.R
     out: work/lauf-1/zusammenführung/ergebnis.txt
     this is the reference run of raute@1, step zusammenführung
```

