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
cockpit install packages/kton-workflow-vokabular      # in A und in B
```

Offen fürs Review: Bestimmt das Paket, welche Templates erlaubt sind, oder bleibt die Obergrenze
`claims.allowedTemplates` in `cockpit.config.json` beim Operator? (Vorschlag: Operator nennt die
erlaubten *Pakete*, nicht jedes Template.)

## 1 A — die Raute Schritt für Schritt ausführen (zweimal)

Lauf 1 mit den Testdaten, Lauf 2 mit anderen Daten und einer anderen Anzahl — erst zwei Läufe
zeigen, welche Stelle der Befehlszeile ein Parameter ist.

```
cockpit publish '{"cmd":"cd work/lauf-1/aufbereitung && Rscript aufbereiten.R", "inputs":[…], "outputs":[…]}'
… je Step, je Lauf
```

## 2 A — den Workflow herausziehen, die Raute als Referenz mitpacken

```
cockpit workflow propose work/lauf-1/zusammenführung/ergebnis.txt work/lauf-2/zusammenführung/ergebnis.txt
cockpit workflow extract <dieselben Ergebnisse> --name raute --reference <lauf-1> \
        --hole dataset=aufbereitung/dataset.csv --param anzahl=zweig-b/2
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

`--check` signiert die Referenzläufe mit (dieselbe Arbeit, ein Record, K5) und sagt `reproduces`
im Scope `raute@1`.

## 6 Review

Jeder Aufruf steht mit Ausgabe in `results/aufrufe.md`. Wir gehen sie gemeinsam durch: Was muss man
wissen, um den Aufruf zu schreiben? Was sagt die Ausgabe einem Menschen? Was fehlt, was ist zu viel?
