#!/usr/bin/env python3
"""Nimmt interne Angaben aus den Ergebnissen und Berichten, bevor sie in das öffentliche Repository
gehen: den improve-Host, die Konten und die internen Ids von improve. Die Skripte lesen den Host
weiterhin aus IMPROVE_URL; hier geht es nur um das, was committet wird.

    anonymisieren.py [<ordner>]      (Vorgabe: der Ordner dieses Skripts)
"""
import os
import re
import sys

def regeln():
    """Die Muster stehen nicht hier: ein öffentliches Skript, das den Host zum Suchen enthält, hätte
    ihn selbst veröffentlicht. Der Host kommt aus IMPROVE_URL, weitere Wörter (eines je Zeile) aus
    der nicht versionierten Datei .anonymisieren.lokal neben diesem Skript."""
    out = []
    url = os.environ.get("IMPROVE_URL", "")
    host = re.sub(r"^https?://", "", url).split("/")[0]
    if host:
        name, _, port = host.partition(":")
        out.append((re.compile(r"https?://" + re.escape(host)), "http://<improve-server>"))
        if port:
            out.append((re.compile(re.escape(name) + "-" + re.escape(port)), "<improve-server>"))
        out.append((re.compile(re.escape(name) + r"(:\d+)?"), "<improve-server>"))
        kurz = name.split(".")[0]
        out.append((re.compile(r"improve \(" + re.escape(kurz) + r"\)"), "improve"))
        out.append((re.compile(r"\b" + re.escape(kurz) + r"\b"), "<improve-server>"))
    lokal = os.path.join(os.path.dirname(os.path.abspath(__file__)), ".anonymisieren.lokal")
    if os.path.exists(lokal):
        for zeile in open(lokal, encoding="utf-8"):
            wort, _, ersatz = zeile.strip().partition("=")
            if wort:
                out.append((re.compile(r"\b" + re.escape(wort) + r"\b"), ersatz or "<anonymisiert>"))
    # Allgemein, ohne etwas zu verraten: improves interne Ids.
    out.append((re.compile(r"\b[0-9A-F]{32}\b"), "<improve-id>"))
    return out

ENDUNGEN = (".md", ".json", ".sh")


def main():
    wurzel = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))
    global REGEL
    REGEL = regeln()
    if not os.environ.get("IMPROVE_URL"):
        print("IMPROVE_URL ist nicht gesetzt — der Host wird nicht ersetzt", file=sys.stderr)
    geaendert = 0
    for d, dirs, files in os.walk(wurzel):
        dirs[:] = [x for x in dirs if x not in (".work", "pkgtool", "pakete")]
        for f in files:
            if not f.endswith(ENDUNGEN) or f == "anonymisieren.py":
                continue
            p = os.path.join(d, f)
            alt = open(p, encoding="utf-8").read()
            neu = alt
            for muster, ersatz in REGEL:
                neu = muster.sub(ersatz, neu)
            if neu != alt:
                open(p, "w", encoding="utf-8").write(neu)
                geaendert += 1
    print(f"{geaendert} Datei(en) anonymisiert")


if __name__ == "__main__":
    main()
