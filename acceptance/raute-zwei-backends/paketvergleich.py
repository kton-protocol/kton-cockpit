#!/usr/bin/env python3
"""Hält zwei Ray-Pakete nebeneinander: das Original und das aus Ausführungen extrahierte.

Verglichen wird, was ein Potential ausmacht — Steps, Verdrahtung, Code, Löcher, Bedingung —, nicht,
was sich erwartbar unterscheidet: Testdaten und Spectrum (sie stammen aus einem anderen Lauf) und
die Form der Befehlszeile (improve trägt die Image-Zeile, das Cockpit den Interpreter)."""
import json
import sys


def lade(d):
    ray = json.load(open(f"{d}/kton/ray.json"))
    try:
        req = json.load(open(f"{d}/kton/requires.json"))
    except FileNotFoundError:
        req = []
    return ray, req


def zeile(what, a, b, why=""):
    return {"what": what, "original": a, "extracted": b, "same": a == b, "why": why}


o, oreq = lade(sys.argv[1])
x, xreq = lade(sys.argv[2])
rows = []
steps = lambda r: sorted(s["id"] for s in r["steps"])
rows.append(zeile("Steps", ", ".join(steps(o)), ", ".join(steps(x))))


def wiring(r):
    out = []
    for s in r["steps"]:
        for i in s["inputs"]:
            if i.get("from", "").startswith("upstream:"):
                out.append(f"{s['id']}/{i['slot']} ← {i['from'][9:]}")
    return sorted(out)


rows.append(zeile("Verdrahtung", "<br>".join(wiring(o)), "<br>".join(wiring(x))))


def code(r):
    return sorted(f"{s['id']}: `{i['hash'][:19]}…`" for s in r["steps"] for i in s["inputs"]
                  if i.get("variable") == "command-file")


rows.append(zeile("Code (Kommandodateien)", "<br>".join(code(o)), "<br>".join(code(x))))


def holes(r):
    return sorted(f"{h['name']}: {h['type']} an {h['step']}/{h.get('slot') or h.get('param')}"
                  for h in r.get("holes", []))


rows.append(zeile("Löcher", "<br>".join(holes(o)), "<br>".join(holes(x))))


def params(r):
    return sorted(f"{s['id']}: {k}={v}" for s in r["steps"] for k, v in (s.get("params") or {}).items())


rows.append(zeile("Parameter am Step", "<br>".join(params(o)), "<br>".join(params(x))))
rows.append(zeile("Bedingung", ", ".join(q["spectrum"][:19] + "…" for q in oreq),
                  ", ".join(q["spectrum"][:19] + "…" for q in xreq)))


def args(r):
    return sorted(f"{s['id']}: `{s['protocol']['command']['args'].replace(chr(10), ' ⏎ ')}`" for s in r["steps"])


rows.append(zeile("Befehlszeile", "<br>".join(args(o)), "<br>".join(args(x)),
                  "erwartet: improve trägt das Image, die Cockpit-Ausführung den Interpreter"))
rows.append(zeile("Vorgaben der Löcher",
                  "<br>".join(sorted(f"{h['name']}={h['default']}" for h in o.get("holes", []))),
                  "<br>".join(sorted(f"{h['name']}={h['default']}" for h in x.get("holes", []))),
                  "erwartet: die Vorgaben kommen aus dem gewählten Referenzlauf"))
struktur = all(r["same"] for r in rows[:6])
print(json.dumps({"structureSame": struktur, "rows": rows}, ensure_ascii=False, indent=1))
sys.exit(0 if struktur else 1)
