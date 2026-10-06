#!/usr/bin/env bash
# ADR-007: der Review des extrahierten Raute-Pakets — hier die git-Seite, wo der Review nur ein
# nekton-Claim ist.
#
#   1. Ein zweiter Teilnehmer, der Prüfer, mit eigenem Repo und eigenem Schlüssel.
#   2. Er öffnet das Paket, prüft, dass es das ist, worüber er urteilt (Bundle-Id), und sagt
#      `reviewed` darüber — mit seinem Cockpit, seinem Schlüssel.
#   3. Sein Claim wandert ins Register der Autorin (Föderation: der signierte Umschlag, sonst nichts).
#      Ihre Konfiguration nimmt seinen Schlüssel in eine Stufe „pruefer" auf.
#   4. Die Autorin sagt selbst `reviewed` über ihr eigenes Paket — das geht, aber es verifiziert nur
#      in ihre eigene Stufe. Vier Augen sind eine Frage von Schlüssel und Stufe, keine Cockpit-Regel.
#   5. Die Projektion des improve-Reviews (results/review-improve-claims, vom improvego cmd/review)
#      kommt ebenso herein, gegen improves Systemschlüssel in einer Stufe „improve".
#   6. `ask about <bundle>`: alle Urteile über dieselben Bytes, jedes mit der Stufe, in die es
#      verifiziert — und mit Filter nur die der Prüfer.
#
# Braucht das Repo von git-seite.sh/extraktion.sh. Schreibt results/review-git.json und REVIEW.md.
set -euo pipefail
HIER="$(cd "$(dirname "$0")" && pwd)"
cd "$HIER"
EXNAME=raute-zwei-backends
source ../../examples/lib/common.sh
AUTORIN="$HIER/.work/repo"
[ -x "$AUTORIN/bin/cockpit" ] || { echo "erst ./git-seite.sh und ./extraktion.sh laufen lassen" >&2; exit 1; }
WORK="$HIER/.work/review"
LOG="$WORK/schritte.jsonl"
rm -rf "$WORK"; mkdir -p "$WORK"; : > "$LOG"
( cd pkgtool && go build -o "$WORK/pkgtool" . )
source "$HIER/schritt.sh"
PKG="$WORK/pkgtool"
BUNDLE=$("$PKG" inspect pakete/raute-aus-git | python3 -c 'import json,sys; print(json.load(sys.stdin)["bundleId"])')
BEFUND="Struktur, Verdrahtung, Code und Löcher gegen das Original geprüft; Spectrum und Testdaten vorhanden"

# ---------------------------------------------------------------------------------------------------
echo "=== der Prüfer: ein eigener Teilnehmer"
CFG=$(python3 - "$REPO/examples/lib/cockpit.config.json" <<'PY'
import json, sys
c = json.load(open(sys.argv[1]))
c["claims"]["allowedTemplates"] = ["reviewed"]
print(json.dumps(c, indent=2))
PY
)
participant "$WORK/pruefer/repo" "$CFG" "raute-pruefer" >/dev/null
unset COCKPIT_REPO_DIR
PRUEFER="$WORK/pruefer/repo"
mkdir -p "$PRUEFER/packages" && cp -r pakete/raute-aus-git "$PRUEFER/packages/raute"

REPO_DIR="$PRUEFER"
schritt pruefer pruefer:doctor cockpit "" \
  "Das Repo des Prüfers: eigene Konfiguration, eigene Schlüssel, eigene Bindung" \
  "./bin/cockpit doctor"
schritt pruefer pruefer:inspect ktonpkg "S6: cockpit kennt kton-Pakete noch nicht" \
  "Der Prüfer öffnet das Paket und stellt fest, worüber er urteilt: seine Bundle-Id" \
  "$PKG inspect packages/raute"
schritt pruefer pruefer:say cockpit "" \
  "Der Prüfer sagt reviewed über die Bundle-Id — mit seinem Cockpit, seinem Schlüssel" \
  "./bin/cockpit say '{\"subject\":\"$BUNDLE\",\"template\":\"reviewed\",\"fields\":{\"outcome\":\"accepted\",\"befund\":\"$BEFUND\"}}'"
PRUEFER_CLAIM=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["claimId"])' "$WORK/last.out")
schritt pruefer pruefer:export kernel-cli "Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch)" \
  "Den signierten Umschlag seines Claims herausnehmen, um ihn weiterzugeben" \
  "NEKTON_DIR=registry/nekton ./bin/nekton records --json"
python3 - "$WORK/last.out" "$PRUEFER_CLAIM" "$WORK/pruefer-claim.dsse.json" <<'PY'
import json, sys
recs = json.load(open(sys.argv[1]))
recs = recs.get("records", recs) if isinstance(recs, dict) else recs
for r in recs:
    if r.get("id") == sys.argv[2] or r.get("claimId") == sys.argv[2]:
        env = r.get("envelope") or r.get("dsse")
        json.dump(env, open(sys.argv[3], "w"))
        break
else:
    sys.exit("der Claim des Prüfers ist nicht unter seinen records")
PY
cp "$PRUEFER/keys/session-1-claims.pub" "$WORK/pruefer.pub"

# ---------------------------------------------------------------------------------------------------
echo; echo "=== bei der Autorin"
REPO_DIR="$AUTORIN"
schritt federation federation:add kernel-cli "Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch)" \
  "Der Umschlag des Prüfers kommt ins Register der Autorin — die Unterschrift reist mit, sonst nichts" \
  "NEKTON_DIR=registry/nekton ./bin/nekton add $WORK/pruefer-claim.dsse.json"
IMPROVE_CLAIMS=( "$HIER"/results/review-improve-claims/*.json )
if [ -e "${IMPROVE_CLAIMS[0]}" ]; then
  for f in "${IMPROVE_CLAIMS[@]}"; do
    schritt federation "federation:add-improve:$(basename "$f" .json)" kernel-cli "Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch)" \
      "Die Projektion des improve-Reviews kommt ins Register der Autorin" \
      "NEKTON_DIR=registry/nekton ./bin/nekton add $f"
  done
fi
schritt federation federation:tiers script "" \
  "Die Konfiguration der Autorin nimmt die Schlüssel auf: Prüfer in „pruefer“, improves Systemschlüssel in „improve“" \
  "python3 -c 'import json,shutil,os; c=json.load(open(\"cockpit.config.json\")); shutil.copy(\"$WORK/pruefer.pub\",\"registry/keys/pruefer-claims.pub\"); c[\"trust\"][\"tiers\"][\"pruefer\"]=[\"registry/keys/pruefer-claims.pub\"]; p=\"$HIER/results/review-improve-claims/improve-system.pub\"
if os.path.exists(p): shutil.copy(p,\"registry/keys/improve-system.pub\"); c[\"trust\"][\"tiers\"][\"improve\"]=[\"registry/keys/improve-system.pub\"]
json.dump(c,open(\"cockpit.config.json\",\"w\"),indent=2); print(json.dumps(c[\"trust\"][\"tiers\"]))'"

schritt review review:self cockpit "" \
  "Die Autorin sagt selbst reviewed über ihr Paket — möglich, aber es verifiziert nur in ihre eigene Stufe" \
  "./bin/cockpit say '{\"subject\":\"$BUNDLE\",\"template\":\"reviewed\",\"fields\":{\"outcome\":\"accepted\",\"befund\":\"selbst angesehen\"}}'"

schritt review review:about cockpit "" \
  "Alle Urteile über dieselben Bytes (Bundle-Id), jedes mit der Stufe, in die es verifiziert" \
  "./bin/cockpit ask '{\"query\":\"about\",\"ref\":\"$BUNDLE\"}'"
schritt review review:about-pruefer cockpit "" \
  "Nur die Urteile der Prüfer — der Filter engt ein, er weitet nichts" \
  "./bin/cockpit ask '{\"query\":\"about\",\"ref\":\"$BUNDLE\",\"filter\":{\"trustTier\":\"pruefer\"}}'"
if [ -e "${IMPROVE_CLAIMS[0]}" ]; then
  schritt review review:about-improve cockpit "" \
    "Nur die Projektion aus improve — verifiziert gegen improves Systemschlüssel" \
    "./bin/cockpit ask '{\"query\":\"about\",\"ref\":\"$BUNDLE\",\"filter\":{\"trustTier\":\"improve\"}}'"
fi

# ---------------------------------------------------------------------------------------------------
python3 - "$LOG" "$HIER" "$BUNDLE" <<'PY'
import datetime, json, os, sys
log, hier, bundle = sys.argv[1], sys.argv[2], sys.argv[3]
steps = [json.loads(l) for l in open(log)]
res = lambda i: next((s["result"] for s in steps if s["id"] == i), {})
def tiers(r):
    by = {x["id"]: x for x in r.get("records", [])}
    return [{"id": c.get("id"), "tier": by.get(c.get("id"), {}).get("tier"), "fields": c.get("fields") or c.get("object"),
             "predicate": c.get("predicate")} for c in r.get("claims", [])]
alle, pruefer, improve = tiers(res("review:about")), tiers(res("review:about-pruefer")), tiers(res("review:about-improve"))
imp = json.load(open(f"{hier}/results/review-improve.json")) if os.path.exists(f"{hier}/results/review-improve.json") else {}
out = {"ranAt": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
       "subject": bundle, "steps": steps, "claims": alle, "pruefer": pruefer, "improve": improve}
json.dump(out, open(f"{hier}/results/review-git.json", "w"), indent=2, ensure_ascii=False)

ok = lambda b: "✅" if b else "❌"
z = ["# Review — dasselbe Paket, in improve und in git geprüft\n",
     "ADR-007. Gegenstand ist das extrahierte Raute-Paket, benannt über seine Bundle-Id "
     f"`{bundle}` — auf beiden Backends dieselben Bytes (ADR-006). Erzeugt von `review.sh` aus "
     "`results/review-improve.json` (improvego `cmd/review`) und dem git-Lauf hier.\n",
     "## Wo entschieden wird\n",
     "| | improve | git |", "|---|---|---|",
     "| entschieden in | dem improve-Review: Einträge, eingeladener Prüfer, Vier-Augen vom Server, Audit-Trail | nekton — der Claim **ist** der Review |",
     "| der nekton-Claim ist | eine **Projektion** eines entschiedenen Eintrags | das Urteil selbst |",
     "| unterschrieben von | improves Systemschlüssel (bezeugt den Eintrag) | dem Schlüssel des Prüfers |",
     "| Vier Augen durch | den Server: Selbstprüfung wird abgelehnt | Schlüssel und Stufe: ein eigenes Urteil verifiziert nur in die eigene Stufe |",
     ""]
if imp:
    fe = imp.get("fourEyes") or {}
    rv = imp.get("review") or {}
    z += ["## improve\n",
          f"- Review `{rv.get('entityId') or rv.get('id')}`: angefragt von `{rv.get('requestor')}`, geprüft von `{rv.get('reviewer')}`.",
          f"- Selbstprüfung abgelehnt: {ok(fe.get('selfApprovalRefused'))} — „{fe.get('message', '')}“",
          "- Einträge: " + "; ".join(f"`{e.get('resource')}` {e.get('status')}" for e in (rv.get("entries") or [])),
          "- Projektion: " + ", ".join(f"`{c.get('id', '')[:23]}…` ({c.get('objectId', '').rsplit('#', 1)[-1]})"
                                        for c in ((imp.get("projection") or {}).get("claims") or [])),
          ""]
z += ["## Was die Autorin sieht — `cockpit ask about <bundle>`\n",
      "| Claim | verifiziert in Stufe | Urteil |", "|---|---|---|"]
for c in alle:
    f = c.get("fields") or {}
    # Ein Template-Claim trägt das Urteil als Feld; eine improve-Projektion als Objekt-Id (ein IRI),
    # mit dem geprüften Eintrag daneben.
    if isinstance(f.get("id"), str) and "#" in f["id"]:
        v = f.get("value") or {}
        urteil = f["id"].rsplit("#", 1)[-1] + (f" — `{v.get('resource')}` (improve-Eintrag, Prüfer {v.get('reviewer')})" if v.get("resource") else "")
    else:
        urteil = f.get("outcome") or json.dumps(f, ensure_ascii=False)[:80]
    z.append(f"| `{(c.get('id') or '')[:23]}…` | **{c.get('tier')}** | {urteil} |")
z += ["", f"Mit Filter `trustTier: pruefer`: {len(pruefer)} Urteil(e) — nur das des Prüfers, nicht das der Autorin über ihr eigenes Paket.",
      f"Mit Filter `trustTier: improve`: {len(improve)} Urteil(e) — die Projektion aus improve.\n",
      "## Schritte\n", "| | Schritt | Akteur | Aufruf | Lücke |", "|---|---|---|---|---|"]
for s in (imp.get("steps") or []) + steps:
    cmd = s["command"].replace("|", "\\|").replace("\n", " ")
    cmd = cmd if len(cmd) <= 160 else cmd[:157] + "…"
    z.append(f"| {ok(s['ok'])} | {s['title']} | {s['actor']} | `{cmd}` | {s.get('gap', '')} |")
open(f"{hier}/REVIEW.md", "w").write("\n".join(z) + "\n")
print(f"\nresults/review-git.json, REVIEW.md: {sum(s['ok'] for s in steps)}/{len(steps)} Schritte ok; "
      f"Urteile: {len(alle)} gesamt, {len(pruefer)} Prüfer, {len(improve)} improve")
PY
