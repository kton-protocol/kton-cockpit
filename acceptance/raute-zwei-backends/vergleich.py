#!/usr/bin/env python3
"""Stellt results/git.json und results/improve.json nebeneinander und schreibt VERGLEICH.md.

Drei Teile, in der Reihenfolge, in der man sie liest:
  1. das Ergebnis — sind die Ausgaben beider Seiten dieselben Bytes, und ist das Werkzeug auf
     beiden Seiten qualifiziert;
  2. die Schritte — je Phase, was auf welcher Seite genau aufgerufen wurde, wer es tat, und was
     herauskam;
  3. die Lücken — wo nicht das Cockpit handelt, obwohl es das im Zielbild (ADR-005) soll.
"""
import json
import os
import sys

HIER = os.path.dirname(os.path.abspath(__file__))
PHASEN = [
    ("setup", "Cockpit einrichten"),
    ("tool-install", "Werkzeug einrichten"),
    ("tool-qualify", "Werkzeug qualifizieren"),
    ("load", "Workflow laden"),
    ("require", "Bedingung des Workflows prüfen"),
    ("refuse-unbound", "Ungebunden starten — muss abgelehnt werden"),
    ("bind", "Löcher füllen"),
    ("run", "Laufen lassen"),
    ("verify", "Nachprüfen"),
]
OUTPUTS = ["aufbereitung/daten.txt", "zweig-b/b.txt", "zweig-c/c.txt", "zusammenführung/ergebnis.txt"]


def lade(seite):
    p = os.path.join(HIER, "results", seite + ".json")
    if not os.path.exists(p):
        return None
    return json.load(open(p))


def kurz(h, n=16):
    if not isinstance(h, str):
        return str(h)
    if h.startswith("sha256:") and len(h) > 7 + n:
        return h[: 7 + n] + "…"
    return h


def zelle(s):
    """Text für eine Tabellenzelle: keine Zeilenumbrüche, keine Pipes."""
    return str(s).replace("|", "\\|").replace("\n", "<br>")


def befund(result):
    """Das Wesentliche aus einem Ergebnis — was ein Leser vergleichen will, nicht alles."""
    if not isinstance(result, dict):
        return zelle(result)[:300]
    teile = []
    for k in ("fotonId", "claimId", "spectrumId", "bundleId", "rayId", "verdict", "level", "reason"):
        if k in result and result[k]:
            teile.append(f"{k}: `{kurz(result[k])}`")
    if "outputHashes" in result:
        for p, h in sorted(result["outputHashes"].items()):
            teile.append(f"`{p.rsplit('/', 1)[-1]}` = `{kurz(h)}`")
    if "missing" in result:
        teile.append("offen: " + ", ".join(result["missing"]))
    if "fulfilled" in result:
        teile.append("erfüllt: " + ("ja" if result["fulfilled"] else "nein"))
    if not teile:
        for k, v in result.items():
            if k in ("stderr",):
                continue
            s = json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v
            teile.append(f"{k}: {s[:160]}")
            if len(teile) >= 3:
                break
    if not teile and result.get("stderr"):
        teile.append(result["stderr"].splitlines()[0][:200])
    return zelle("<br>".join(teile))[:900]


def spalte(s):
    if s is None:
        return "—"
    ok = "✅" if s.get("ok") else "❌"
    teile = [f"{ok} **{zelle(s.get('title', s['id']))}**",
             f"_{s.get('actor', '?')}_",
             f"`{zelle(s.get('command', ''))}`"]
    b = befund(s.get("result", {}))
    if b:
        teile.append("→ " + b)
    if s.get("gap"):
        teile.append(f"⚠ Lücke: {zelle(s['gap'])}")
    return "<br>".join(teile)


def main():
    git, imp = lade("git"), lade("improve")
    if git is None and imp is None:
        sys.exit("keine Ergebnisse in results/ — erst git-seite.sh und rautevergleich laufen lassen")
    leer = {"steps": [], "outputs": {}, "qualification": {}, "bindings": {}, "fotons": {}}
    git, imp = git or dict(leer, side="git"), imp or dict(leer, side="improve")

    z = []
    z.append("# Raute auf zwei Backends — git und improve\n")
    z.append("Werkzeug **jam-r** qualifizieren, Workflow-Paket **Raute** laden, Löcher füllen "
             "(`dataset` = `eigene-daten.csv`, `anzahl` = 3), laufen lassen — einmal in einem "
             "git-Teilnehmerrepo mit dem kton-cockpit, einmal in improve. Erzeugt von `vergleich.py` "
             "aus `results/git.json` und `results/improve.json`.\n")
    z.append(f"| | git | improve |\n|---|---|---|\n"
             f"| gelaufen | {git.get('ranAt', '—')} | {imp.get('ranAt', '—')} |\n"
             f"| Schritte ok | {sum(1 for s in git['steps'] if s.get('ok'))} / {len(git['steps'])} "
             f"| {sum(1 for s in imp['steps'] if s.get('ok'))} / {len(imp['steps'])} |\n"
             f"| `dataset` | `{kurz(git['bindings'].get('dataset'))}` | `{kurz(imp['bindings'].get('dataset'))}` |\n"
             f"| `anzahl` | {git['bindings'].get('anzahl', '—')} | {imp['bindings'].get('anzahl', '—')} |\n")

    # 1 ---------------------------------------------------------------------------------------------
    z.append("## 1 Ergebnis\n")
    z.append("### Ausgaben — dieselben Bytes?\n")
    z.append("| Datei | git | improve | |\n|---|---|---|---|")
    gleich = 0
    for o in OUTPUTS:
        a, b = git["outputs"].get(o), imp["outputs"].get(o)
        same = a is not None and a == b
        gleich += same
        z.append(f"| `{o}` | `{kurz(a, 24) if a else '—'}` | `{kurz(b, 24) if b else '—'}` | "
                 f"{'✅ gleich' if same else ('❌ verschieden' if a and b else '—')} |")
    z.append(f"\n**{gleich} von {len(OUTPUTS)}** Ausgaben sind auf beiden Seiten bitgleich.\n")
    z.append("Die **Foton-Ids** unterscheiden sich erwartungsgemäß: eine Foton-Id deckt das Protokoll "
             "mit ab, und das ist auf der git-Seite die Befehlszeile samt gepinntem Image, in improve "
             "das Tool mit seinem Deskriptor. Gleich sein müssen die Bytes — dass zwei verschiedene "
             "Ausführungen dieselben Bytes liefern, ist genau die Aussage, die ein Vergleich machen kann.\n")
    z.append("| Step | Foton git | Foton improve |\n|---|---|---|")
    for step in ["aufbereitung", "zweig-b", "zweig-c", "zusammenführung"]:
        z.append(f"| {step} | `{kurz(git['fotons'].get(step), 20)}` | `{kurz(imp['fotons'].get(step), 20)}` |")

    z.append("\n### Werkzeugqualifizierung jam-r\n")
    gq, iq = git.get("qualification", {}), imp.get("qualification", {})
    z.append(f"| | git | improve |\n|---|---|---|\n"
             f"| Spectrum | `{kurz(gq.get('spectrum'))}` | `{kurz(iq.get('spectrum'))}` |\n"
             f"| Befund | **{gq.get('verdict', '—')}** | **{iq.get('verdict', '—')}** |")
    names = sorted(set(gq.get("members", {})) | set(iq.get("members", {})))
    if names:
        z.append("\n| Member | Referenz | git | | improve | |\n|---|---|---|---|---|---|")
        for n in names:
            gm, im = gq.get("members", {}).get(n, {}), iq.get("members", {}).get(n, {})
            ref = gm.get("reference") or im.get("reference")
            z.append(f"| {n} | `{kurz(ref)}` | `{kurz(gm.get('output', '—'))}` | {gm.get('match', '—')} "
                     f"| `{kurz(im.get('output', '—'))}` | {im.get('match', '—')} |")

    # 2 ---------------------------------------------------------------------------------------------
    z.append("\n## 2 Die Schritte nebeneinander\n")
    z.append("Je Zelle: Ergebnis, **was** passiert, _wer_ es tut, der exakte Aufruf, was herauskam — "
             "und ⚠ wo im Zielbild das Cockpit handeln soll.\n")
    for phase, titel in PHASEN:
        gs = [s for s in git["steps"] if s.get("phase") == phase]
        ims = [s for s in imp["steps"] if s.get("phase") == phase]
        z.append(f"### {titel}\n")
        z.append("| git | improve |\n|---|---|")
        for i in range(max(len(gs), len(ims), 1)):
            a = gs[i] if i < len(gs) else None
            b = ims[i] if i < len(ims) else None
            z.append(f"| {spalte(a)} | {spalte(b)} |")
        z.append("")

    # 3 ---------------------------------------------------------------------------------------------
    z.append("## 3 Wo das Cockpit noch nicht handelt\n")
    for seite, d in (("git", git), ("improve", imp)):
        gaps = [s for s in d["steps"] if s.get("gap")]
        z.append(f"**{seite}** — {len(gaps)} von {len(d['steps'])} Schritten:\n")
        for s in gaps:
            z.append(f"- `{s['id']}` ({s.get('actor')}): {s['gap']}")
        z.append("")

    open(os.path.join(HIER, "VERGLEICH.md"), "w").write("\n".join(z) + "\n")
    print("VERGLEICH.md geschrieben")


if __name__ == "__main__":
    main()
