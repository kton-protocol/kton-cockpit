# Ergebnisformat — `results/<seite>.json`

Beide Seiten schreiben dasselbe Format, damit `vergleich.py` sie Schritt für Schritt nebeneinander
stellen kann.

```json
{
  "side": "git" | "improve",
  "ranAt": "2026-09-30T18:00:00Z",
  "bindings": { "dataset": "sha256:<hash von eigene-daten.csv>", "anzahl": "3" },
  "steps": [
    {
      "phase":   "setup | tool-install | tool-qualify | load | require | bind | refuse-unbound | run | verify",
      "id":      "kurz, eindeutig je Seite, z.B. run:zweig-b",
      "title":   "was hier passiert, ein Satz",
      "actor":   "cockpit | kernel-cli | ktonpkg | improvego | improve-server | script",
      "command": "der exakte Aufruf, so wie er lief (Kommandozeile oder Go-API-Aufruf mit Argumenten)",
      "gap":     "leer, oder z.B. \"S4: cockpit kennt improve-Modus noch nicht\" — wenn eigentlich das Cockpit hier handeln sollte",
      "ok":      true,
      "result":  { "frei": "was herauskam: ids, hashes, verdicts, Fehlermeldung bei erwarteter Ablehnung" }
    }
  ],
  "qualification": {
    "tool": "jam-r",
    "spectrum": "sha256:<spectrum id>",
    "verdict": "L0 | L1 | failed",
    "members": { "<member>": { "output": "sha256:…", "reference": "sha256:…", "match": "identical | via-normalizer | differs" } }
  },
  "outputs": {
    "aufbereitung/daten.txt":        "sha256:…",
    "zweig-b/b.txt":                 "sha256:…",
    "zweig-c/c.txt":                 "sha256:…",
    "zusammenführung/ergebnis.txt":  "sha256:…"
  },
  "fotons": { "aufbereitung": "sha256:<foton id>", "zweig-b": "…", "zweig-c": "…", "zusammenführung": "…" }
}
```

Hashes sind kton-Inhaltshashes (`sha256:<64 hex>` über die Bytes). Die Phasen und ihre Reihenfolge
sind auf beiden Seiten dieselben; eine Seite, die eine Phase nicht hat, schreibt den Schritt trotzdem
mit `"ok": false` und einer `gap`-Begründung.
