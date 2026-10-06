#!/usr/bin/env python3
"""Holt zu jeder Foton-Id die Einzelheiten über `cockpit ask {query: record}` und schreibt sie als
Liste. Jede Antwort ist schon gegen die Vertrauensstufen des Repos verifiziert; was nicht
verifiziert, kommt gar nicht erst als record zurück."""
import json
import subprocess
import sys

ids = [l.strip() for l in open(sys.argv[1]) if l.strip()]
out = []
for i in ids:
    arg = json.dumps({"query": "record", "ref": i})
    print(f"./bin/cockpit ask '{arg}'", file=sys.stderr)
    out.append(json.loads(subprocess.check_output(["./bin/cockpit", "ask", arg])))
json.dump(out, open(sys.argv[2], "w"), ensure_ascii=False, indent=1)
print(json.dumps({"records": len(out), "ids": ids}))
