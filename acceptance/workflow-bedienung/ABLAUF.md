# Workflow-Bedienung — der Ablauf, den wir gemeinsam reviewen

Zwei ktons: **A** (Autorin) und **B** (Anwender). Alles läuft über `cockpit …`; wo heute ein
anderes Werkzeug nötig wäre, ist das ein Befund für das Review, kein Umweg im Skript.

Kernel: lokaler Branch `cockpit-kernel-proposals` (K5, K7, K8), Cockpit-Branch `kernel-proposals`.
Suche und Profilregeln: SPARQL in Oxigraph (gepinntes Image).

## 0 Vorbedingung — das Workflow-Template-Paket (beide ktons)

Ein Paket im Scope-Format, Profil `templates`. Es bringt das Vokabular und die Suche mit, statt dass
jedes Repo Templates einzeln anlegt:

- Claim-Templates: `installed`, `derived-from`, `reproduces`, `describes`, `rationale`
- Suchabfragen (SPARQL, `.rq`): `workflows` (installierte Workflows), `workflow` (Details zu einem)

```
cockpit install ../../kton-workflow --allow      # in A und in B
```

Entschieden (Review 1): Der Operator nennt die erlaubten *Pakete*, nicht jedes Template;
`--allow` trägt die Id in `claims.allowedPackages` ein.

## 1 A — die Raute Schritt für Schritt ausführen (zweimal)

Lauf 1 mit den Testdaten, Lauf 2 mit anderen Daten und einer anderen Anzahl — erst zwei Läufe
zeigen, welche Stelle der Befehlszeile ein Parameter ist.

```
cockpit run --dir work/lauf-1/aufbereitung --in quelle/testdaten/dataset.csv --in quelle/steps/aufbereitung/aufbereiten.R -- Rscript aufbereiten.R
… je Step, je Lauf; die Ausgaben erkennt das Cockpit selbst
```

## 2 A — den Workflow herausziehen, die Raute als Referenz mitpacken

```
cockpit workflow propose work/lauf-1/zusammenführung/ergebnis.txt work/lauf-2/zusammenführung/ergebnis.txt
cockpit workflow extract <dieselben Ergebnisse> --name raute --reference <ergebnis von lauf 1> \
        --hole dataset=1 --param anzahl=2          # die Nummern aus propose
```

`extract` schreibt `packages/raute/` im Scope-Format: Ray als Potentiale, Testdaten aus Lauf 1,
**die Läufe von Lauf 1 als Referenzläufe**, versiegelt.

## 3 B — installieren

```
cockpit install <pfad-oder-url-zum-paket>
```

Prüft das Paket, übernimmt die Records, öffnet den Scope `raute@1` im nekton von B, sagt
`installed`. Die lesbare Ansicht liegt unter `packages/raute@1/`.

## 4 B — suchen, auswählen

```
cockpit workflow list                 # SPARQL `workflows` aus dem Vokabularpaket
cockpit workflow show raute           # Steps, Löcher (mit Testdaten), Parameter, Referenz
```

## 5 B — durchführen

```
cockpit workflow run raute --check                               # mit den Testdaten: reproduziert B die Referenz?
cockpit workflow run raute --bind dataset=data/eigene-daten.csv --bind anzahl=3
```

`--check` läuft im Verzeichnis der Referenz, signiert die Referenzläufe mit (dieselbe Arbeit, ein
Record, K5) und sagt `reproduces` im Scope `raute@1`. Ohne `--check` laufen nur gebundene Workflows.

```
cockpit workflow trace work/eigen/zusammenführung/ergebnis.txt   # die Steps hinter einem Ergebnis
```

## 6 Review

Jeder Aufruf steht mit Ausgabe in `results/aufrufe.md`. Wir gehen sie gemeinsam durch: Was muss man
wissen, um den Aufruf zu schreiben? Was sagt die Ausgabe einem Menschen? Was fehlt, was ist zu viel?

## Review 1 (MH, 2026-10-01) — umgesetzt

1. Steps über `cockpit run --dir … --in … -- …` statt `publish` mit JSON; Eingaben werden übergeben.
2. `install --allow` trägt das Paket in die Config ein.
3. `propose` nummeriert die Kandidaten, `extract` nimmt die Nummern.
4. `--check` erklärt, warum es im Verzeichnis der Referenz läuft; die Oberfläche ist englisch.
5. `workflow trace <ergebnis>` zeigt die Steps hinter einem Ergebnis mit Namen, Pfaden, allen
   Unterzeichnern und, wo es einer ist, den Referenzlauf eines installierten Workflows.
