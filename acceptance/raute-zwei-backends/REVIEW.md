# Review — dasselbe Paket, in improve und in git geprüft

ADR-007. Gegenstand ist das extrahierte Raute-Paket, benannt über seine Bundle-Id `sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173` — auf beiden Backends dieselben Bytes (ADR-006). Erzeugt von `review.sh` aus `results/review-improve.json` (improvego `cmd/review`) und dem git-Lauf hier.

## Wo entschieden wird

| | improve | git |
|---|---|---|
| entschieden in | dem improve-Review: Einträge, eingeladener Prüfer, Vier-Augen vom Server, Audit-Trail | nekton — der Claim **ist** der Review |
| der nekton-Claim ist | eine **Projektion** eines entschiedenen Eintrags | das Urteil selbst |
| unterschrieben von | improves Systemschlüssel (bezeugt den Eintrag) | dem Schlüssel des Prüfers |
| Vier Augen durch | den Server: Selbstprüfung wird abgelehnt | Schlüssel und Stufe: ein eigenes Urteil verifiziert nur in die eigene Stufe |

## improve

- Review `<improve-server>:RV-182199`: angefragt von `<anfragender>`, geprüft von `<pruefer-konto>`.
- Selbstprüfung abgelehnt: ✅ — „HTTP 400: You haven't been invited to this review.“
- Einträge: `kton/package.json` Approved; `steps/zweig-c/zweigC.R` Approved; `kton/ray.json` Approved; `reference/zweig-b-b.txt` Approved; `steps/zweig-b/zweigB.R` Approved; `reference/zweig-c-c.txt` Approved; `kton/requires.json` Approved; `testdaten/dataset/dataset.csv` Approved; `steps/aufbereitung/aufbereiten.R` Approved; `steps/zusammenführung/zusammen.R` Approved; `reference/aufbereitung-daten.txt` Approved; `kton/spectrum.json` Approved; `reference/zusammenführung-ergebnis.txt` Approved
- Projektion: `sha256:5e1aad2ee36fb7e6…` (accepted), `sha256:e433ffea93a85269…` (accepted), `sha256:8b8a73f5b978681d…` (accepted), `sha256:9a1116ac9f1b5632…` (accepted), `sha256:096dec8f52b7ddfb…` (accepted), `sha256:e0ea086ca3d27405…` (accepted), `sha256:6052647208a3272d…` (accepted), `sha256:951d9d9ea9ca3a0d…` (accepted), `sha256:b372d44cfa4d404a…` (accepted), `sha256:6eef53b47a24f152…` (accepted), `sha256:386216da38d3da1f…` (accepted), `sha256:a90c3b970a283f0c…` (accepted), `sha256:cbc9e8b4821f778b…` (accepted)

## Was die Autorin sieht — `cockpit ask about <bundle>`

| Claim | verifiziert in Stufe | Urteil |
|---|---|---|
| `sha256:983e11ba78e372c9…` | **pruefer** | accepted |
| `sha256:096dec8f52b7ddfb…` | **improve** | accepted — `steps/zweig-b/zweigB.R` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:386216da38d3da1f…` | **improve** | accepted — `reference/aufbereitung-daten.txt` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:5e1aad2ee36fb7e6…` | **improve** | accepted — `kton/package.json` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:6052647208a3272d…` | **improve** | accepted — `kton/requires.json` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:6eef53b47a24f152…` | **improve** | accepted — `steps/zusammenführung/zusammen.R` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:8b8a73f5b978681d…` | **improve** | accepted — `kton/ray.json` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:951d9d9ea9ca3a0d…` | **improve** | accepted — `testdaten/dataset/dataset.csv` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:9a1116ac9f1b5632…` | **improve** | accepted — `reference/zweig-b-b.txt` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:a90c3b970a283f0c…` | **improve** | accepted — `kton/spectrum.json` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:b372d44cfa4d404a…` | **improve** | accepted — `steps/aufbereitung/aufbereiten.R` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:cbc9e8b4821f778b…` | **improve** | accepted — `reference/zusammenführung-ergebnis.txt` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:e0ea086ca3d27405…` | **improve** | accepted — `reference/zweig-c-c.txt` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:e433ffea93a85269…` | **improve** | accepted — `steps/zweig-c/zweigC.R` (improve-Eintrag, Prüfer <pruefer-konto>) |
| `sha256:bda24cae2c18e692…` | **self** | accepted |

Mit Filter `trustTier: pruefer`: 1 Urteil(e) — nur das des Prüfers, nicht das der Autorin über ihr eigenes Paket.
Mit Filter `trustTier: improve`: 13 Urteil(e) — die Projektion aus improve.

## Schritte

| | Schritt | Akteur | Aufruf | Lücke |
|---|---|---|---|---|
| ✅ | eigenen Testordner und ein Register darin anlegen; Systemschlüssel laden | improvego | `c.CreateFolder(ctx, "/dst", "review-20260930T184844"); c.EnsureRegister(ctx, <ordner>/kton); improve.Signer()` | S4: das Cockpit kennt den improve-Modus noch nicht — die Schritte laufen über improvego |
| ✅ | das Paket kopieren, prüfen, gegen die erwartete Bundle-Id halten und einspielen | improvego | `ktonpkg.Open(<Kopie von /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/pakete/raute-aus-improve>).Verify(); c.InstallPackage(ctx, b, Instal…` | S6: Pakete einspielen gehört ins Cockpit (backend/improve) — heute improvego |
| ✅ | ein vorhandenes zweites Konto als Prüfer: nachschlagen und sich als es anmelden | improve-server | `GET /v1/users/username/<pruefer-konto>; Basic, sonst Passwort-Grant am Identitätsdienst (client improve-test-client, Geheimnis aus IMPROVE_TEST_CLIENT_SECRET); GET /v1…` | keine Cockpit-Lücke: Prüfer sind Personen mit eigenem Konto — der Test leiht sich eines aus dem Testaufbau |
| ✅ | als <anfragender> den Vorgang über die Dateien des Pakets beantragen, Prüfer einladen, auf Reviewing stellen | improvego | `c.CreateReview(ctx, NewReview{Name, ParentPath: <ordner>, ResourceIDs: <Paketdateien>, ReviewerIDs: [<Prüfer>]}); c.SetReviewStatus(ctx, id, "Reviewing")` | S4: im Zielbild `cockpit say {template: reviewed}` im improve-Modus — dort legt es den Vorgang an, statt den Claim zu schreiben (ADR-007) |
| ✅ | als <anfragender> selbst entscheiden und sich selbst einladen — improve muss beides ablehnen | improve-server | `<anfragender>: c.ApproveEntries(ctx, id, {reviewEntryIds: [<erster Eintrag>]}); POST /v1/reviews/{id}/reviewers {userId: <<anfragender>>, username: <anfragender>}` |  |
| ✅ | als Prüfer die Einladung annehmen und jeden Eintrag mit Befund annehmen | improve-server | `Prüfer: c.AcceptReview(ctx, id); POST /v1/reviews/{id}/entries/{eid}/comments "Struktur, Verdrahtung und Löcher geprüft; Spectrum und Testdaten vorhanden"; c…` | S4: im Zielbild `cockpit say {template: reviewed}` im improve-Modus — dort legt es den Vorgang an, statt den Claim zu schreiben (ADR-007) |
| ✅ | erst jetzt projizieren: je entschiedenem Eintrag ein Claim über die Bundle-Id, mit improves Systemschlüssel | improvego | `improve.ReviewClaim(review, eintrag, "sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173") je Eintrag; reg.Sign(ctx, improve.Signer(), c…` | S4: im Zielbild projiziert das Cockpit im improve-Modus selbst, aus dem entschiedenen Eintrag (ADR-007) — heute improvego ReviewClaim |
| ✅ | die Projektion aus dem Register zurücklesen und prüfen; ein offener Eintrag ergibt keine | improvego | `reg.About(ctx, "sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173"); ktonpkg.ClaimFromEnvelope(<Datei>, [improve-system.pub]); zweiter …` | S4: im Zielbild `cockpit ask {query: about}` gegen das improve-Register |
| ✅ | den Testordner entfernen; das Prüferkonto bleibt, wie es war | improve-server | `c.Delete(ctx, <ordner>)` |  |
| ✅ | Das Repo des Prüfers: eigene Konfiguration, eigene Schlüssel, eigene Bindung | cockpit | `./bin/cockpit doctor` |  |
| ✅ | Der Prüfer öffnet das Paket und stellt fest, worüber er urteilt: seine Bundle-Id | ktonpkg | `/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/review/pkgtool inspect packages/raute` | S6: cockpit kennt kton-Pakete noch nicht |
| ✅ | Der Prüfer sagt reviewed über die Bundle-Id — mit seinem Cockpit, seinem Schlüssel | cockpit | `./bin/cockpit say '{"subject":"sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173","template":"reviewed","fields":{"outcome":"accepted",…` |  |
| ✅ | Den signierten Umschlag seines Claims herausnehmen, um ihn weiterzugeben | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton records --json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Der Umschlag des Prüfers kommt ins Register der Autorin — die Unterschrift reist mit, sonst nichts | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.work/review/pruefer-claim.dsse.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/096dec8f52b7ddfb.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/386216da38d3da1f.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/5e1aad2ee36fb7e6.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/6052647208a3272d.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/6eef53b47a24f152.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/8b8a73f5b978681d.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/951d9d9ea9ca3a0d.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/9a1116ac9f1b5632.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/a90c3b970a283f0c.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/b372d44cfa4d404a.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/cbc9e8b4821f778b.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/e0ea086ca3d27405.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Projektion des improve-Reviews kommt ins Register der Autorin | kernel-cli | `NEKTON_DIR=registry/nekton ./bin/nekton add /mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/results/review-improve-claims/e433ffea93a85269.json` | Föderation: den Umschlag zu tragen ist nicht Sache des Cockpits (kton-web, fetch) |
| ✅ | Die Konfiguration der Autorin nimmt die Schlüssel auf: Prüfer in „pruefer“, improves Systemschlüssel in „improve“ | script | `python3 -c 'import json,shutil,os; c=json.load(open("cockpit.config.json")); shutil.copy("/mnt/c/dev/git-repos/kton-cockpit/acceptance/raute-zwei-backends/.w…` |  |
| ✅ | Die Autorin sagt selbst reviewed über ihr Paket — möglich, aber es verifiziert nur in ihre eigene Stufe | cockpit | `./bin/cockpit say '{"subject":"sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173","template":"reviewed","fields":{"outcome":"accepted",…` |  |
| ✅ | Alle Urteile über dieselben Bytes (Bundle-Id), jedes mit der Stufe, in die es verifiziert | cockpit | `./bin/cockpit ask '{"query":"about","ref":"sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173"}'` |  |
| ✅ | Nur die Urteile der Prüfer — der Filter engt ein, er weitet nichts | cockpit | `./bin/cockpit ask '{"query":"about","ref":"sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173","filter":{"trustTier":"pruefer"}}'` |  |
| ✅ | Nur die Projektion aus improve — verifiziert gegen improves Systemschlüssel | cockpit | `./bin/cockpit ask '{"query":"about","ref":"sha256:135701e7d917ae4aedbef8f973e22d59038840d5605b360a208217aabb3c9173","filter":{"trustTier":"improve"}}'` |  |
