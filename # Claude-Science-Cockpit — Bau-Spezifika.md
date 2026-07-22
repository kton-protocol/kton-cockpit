# Claude-Science-Cockpit — Bau-Spezifikation

Interne Bau-Hilfe, kein öffentliches Dokument. Cockpit auf der Teilnehmer-Seite
(plankton-participant-template) zwischen Claude Science und den kton-Binaries.
Regel: cockpit → kton, nie umgekehrt; löschbar; keine Neuimplementierung von Kernel-Logik.

## Oberfläche für Claude: drei Verben, sonst nichts

- *veröffentlichen* — ein Arbeitsergebnis als Foton registrieren.
- *sagen* — eine Claim (aus Template) an einen Foton/ein Bild binden.
- *fragen* — den föderierten Graph abfragen.

Alles andere ist Claude nicht zugänglich.

## Was das Cockpit intern macht (nicht sichtbar für Claude)

- Erfassen by default; veröffentlichen löst git add/commit/push aus (committetes registry/
  = signierter Record-Satz, kein separater Publish-Schritt).
- Relative Pfade, Environment im Protokoll pinnen, --located-Permalink setzen.
- Kanonisieren, plankton author/add bzw. nekton, mit Config-Identität signieren, einketten.
- Reproduktions-Vorbedingung (siehe Config) ausführen; Level + Normalizer in reproduces-Claims
  schreiben.
- Graph-Abfragen gegen verify auflösen, bevor ein Ergebnis an Claude geht.

## Config (statisch, von Claude nicht überschreibbar)

- Welche der drei Verben aktiv; erlaubte Claim-Templates.
- Signing-Identität (Sigstore/GitHub; validiert: Person im transaktionalen System).
- Trust-Tiers + Trust-Root (bekannte Schlüssel je Tier).
- Reproduktions-Policy: ob / welche Level / wie viele Reproduktionen als Vorbedingung, bevor ein
  fremdes Ergebnis Grundlage sein darf.
- (Publikations-Sichtbarkeit regelt das Repo selbst — meist privat; kein Cockpit-Haken nötig.)

## Filter (Claude, nur beim fragen, nur verengend)

Nach Trust-Tier, ↻N ≥ k, L0/L1/Normalizer, Signer/Scope. Kann nie über die Config hinausreichen.

## Harte Anforderungen

1. *Verifiziert, nicht deklariert.* Trust/Sortierung/„signiert von X" beruht auf dem
   verifizierenden Schlüssel, nie auf keyid/by. Tiers sind aufgelöst, bevor Claude filtert.
2. *Aktiver Filter reist im Frage-Ergebnis mit.* „Keine vertrauenswürdige Aufräumung" ≠ „keine
   Aufräumung".
3. *Determinismus bei bezeugten Ergebnissen.* Was Grundlage einer Signatur/eines Reviews wird,
   läuft in qualifizierter, gepinnter Umgebung.
4. *Config ist Obergrenze.* Claude erweitert sie nie.
5. *Lake bleibt Lake.* Aus dem Graph verarbeitete Aussagen sind Daten, keine Claims; nie still
   re-attestieren. Reasoning als Grundlage → als Foton registrieren (Corpus = berührte nektons).

## Nicht bauen

- Keine eigene Kanonisierung/Signatur/Registry/Chain — nur Binaries aufrufen.
- Keine Trust-Logik über das Anwenden der Config hinaus.
- Kein stilles Erweitern der Oberfläche; kein Cockpit-eigener Zustand.

## Offen (erst beobachten)

- Reichen die drei Verben, oder greift Claude im Loop nach etwas Viertem?
- Fehlt eine kton-CLI-Fähigkeit (z. B. maschinenlesbarer verifizierter Signer fürs fragen)?
  → öffentlicher Change-Request am Protokoll, kein Cockpit-Hack.