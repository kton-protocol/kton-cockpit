#!/bin/sh
# Build a throwaway registry for the shell's e2e flows. Runs INSIDE a container. Arguments: the work
# dir (absolute; mounted at the same path everywhere), the vocabulary folder and the vocabulary package
# (both read only). The cockpit binary is /usr/local/bin/cockpit (mounted read only).
#
# Test identities are generated here, under $E2E/home, and discarded with the dir: never a person's keys,
# never a real registry. qa-decider is the window's person key, qa-decider-agent its agent key (in a
# trust tier of its own, "agent"), qa-requestor a second person. qa-lookalike is a different key whose
# public files sit in tier "agent" under the person key's names (qa-decider-claims.pub): its label is
# the person's, its key id is not (review check 7, Q2).
set -eu
E2E="$1"; VOCAB="$2"; PACKAGE="$3"
export HOME="$E2E/home"
mkdir -p "$HOME" "$E2E/repo" "$E2E/queries"
cp "$VOCAB"/queries/*.rq "$E2E/queries/"
cd "$E2E/repo"
git init -q
git config user.email qa@example.org
git config user.name qa
git commit -q --allow-empty -m "throwaway registry for the workbench e2e"
git remote add origin https://github.com/qa/workbench-e2e.git   # the binding check needs one; nothing is pushed
cockpit init >/dev/null
cp "$VOCAB"/templates/*.json .kton/templates/
VOCAB="$VOCAB" node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.repo={owner:"qa",name:"workbench-e2e"};c.persist=Object.assign(c.persist||{},{share:false});
c.query={image:"oci://oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0"};
c.claims=c.claims||{};
const names=fs.readdirSync(process.env.VOCAB+"/templates").filter(f=>f.endsWith(".json")).map(f=>f.slice(0,-5));
c.claims.allowedTemplates=[...new Set([...(c.claims.allowedTemplates||[]),...names])];
fs.writeFileSync(p,JSON.stringify(c,null,2));'
cockpit keygen qa-decider >/dev/null
cockpit keygen qa-decider-agent >/dev/null
cockpit keygen qa-requestor >/dev/null
cockpit keygen qa-lookalike >/dev/null
# the agent key in a tier of its own: a decides it signs is a record, never a decision (review check 7)
mkdir -p .kton/keys/agent
for f in .kton/keys/self/qa-decider-agent*.pub; do mv "$f" .kton/keys/agent/; done
node -e '
const fs=require("fs");
const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.trust=Object.assign(c.trust||{},{tiers:["self","agent"]});fs.writeFileSync(p,JSON.stringify(c,null,2));
const i=".kton/local/identity.json";const id=JSON.parse(fs.readFileSync(i));
id.keyring=process.env.HOME+"/.kton-keys";fs.writeFileSync(i,JSON.stringify(id,null,2));'
# decides and answers come by name from an admitted package, never from the checkout (review check 7, Q1).
# The package is mounted read only and git keeps no empty directories, so install works on a writable copy.
cp -R "$PACKAGE" "$E2E/vocab-package"
cockpit install "$E2E/vocab-package" --allow --json >/dev/null
SEED=$(cockpit scope seed lanes 2>&1 | sed -n 's/.*"lanes": "\(sha256:[0-9a-f]*\)".*/\1/p')
[ -n "$SEED" ] || { echo "seed failed" >&2; exit 1; }
node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.claims.scopes={lanes:process.argv[1]};fs.writeFileSync(p,JSON.stringify(c,null,2));' "$SEED"

# say … → the claim id
say() { cockpit say "$@" --scope lanes --json | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).claimId))'; }
OPTS='[{"id":"a","label":"Build 39","consequence":"the build the test system runs"},{"id":"b","label":"Build 41","consequence":"newer, not yet on a test system"}]'
R1=$(say requests "$SEED" area=qa title="Check the release notes" text="Please check the notes of the next release." why="e2e seed" --as qa-requestor)
R2=$(say requests "$SEED" area=ops title="Rotate the test certificate" text="The test certificate expires next month." why="e2e seed" --as qa-requestor)
ANN=$(say announces "$SEED" area=qa title="Already answered" text="This one has a done." why="e2e seed" --as qa-requestor)
say done "$ANN" answers="$ANN" outcome="Read and closed." --as qa-decider >/dev/null
say responsible "$SEED" lane=active/qa-lane areas=qa >/dev/null
say responsible "$SEED" lane=active/ops-lane areas=ops >/dev/null
P1=$(say proposes "$SEED" area=qa title="Which build goes to qualification?" text="Two candidates are ready." why="e2e seed" options="$OPTS" recommended=a --as qa-requestor)
P2=$(say proposes "$SEED" area=qa title="Which test system first?" text="Both are free this week." why="e2e seed" \
  options='[{"id":"a","label":"System A","consequence":"older, stable"},{"id":"b","label":"System B","consequence":"newer"}]' recommended=b --as qa-requestor)
P3=$(say proposes "$SEED" area=qa title="Which release name?" text="Two names are proposed." why="e2e seed" \
  options='[{"id":"a","label":"Name A"},{"id":"b","label":"Name B"}]' --as qa-requestor)
P4=$(say proposes "$SEED" area=qa title="Decided by an agent key only" text="An agent signed a decides here." why="e2e seed" \
  options='[{"id":"a","label":"Yes"},{"id":"b","label":"No"}]' --as qa-requestor)
say decides "$P4" answers="$P4" outcome=https://schema.org/AcceptAction choice=a --as qa-decider-agent >/dev/null
say decides "$P4" answers="$P4" outcome=https://schema.org/RejectAction choice=b --as qa-lookalike >/dev/null
# the look-alike's public keys go to tier agent under the person key's file names
mv .kton/keys/self/qa-lookalike-claims.pub .kton/keys/agent/qa-decider-claims.pub
mv .kton/keys/self/qa-lookalike.pub .kton/keys/agent/qa-decider.pub
# key ids, for the registry checks: the person's from doctor (derived from the private seed), checked
# against its public key; the look-alike's from its public key
kid() { node -e 'const c=require("crypto");const h=require("fs").readFileSync(process.argv[1],"utf8").trim();console.log(c.createHash("sha256").update(Buffer.from(h,"hex")).digest("hex").slice(0,16))' "$1"; }
PKID=$(cockpit doctor --json | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).fields.identity.nektonKeyId))')
[ "$PKID" = "$(kid "$HOME/.kton-keys/qa-decider-claims.pub")" ] || { echo "doctor's key id is not qa-decider-claims.pub's" >&2; exit 1; }
LKID=$(kid "$HOME/.kton-keys/qa-lookalike-claims.pub")
cat > "$E2E/surfaces.json" <<EOF
{"_doc": "throwaway surfaces for the e2e (workgroup format)",
 "core": {"lanes": {"qa-lane":  {"lane": "active/qa-lane"}},  "folder": "$E2E/surfaces/core"},
 "ops":  {"lanes": {"ops-lane": {"lane": "active/ops-lane"}}}}
EOF
AKID=$(kid "$HOME/.kton-keys/qa-decider-agent-claims.pub")

# PR 2: the verticle packages (ktonpkg v0.9.1), built with throwaway keys and admitted (claims.allowedPackages)
node /e2e/build-packages.mjs "$E2E/bin/scope" "$E2E/pkgs" >/dev/null
for p in kton-web-test workbench-graph hostile-good hostile-tampered hostile-network hostile-pin; do cockpit install "$E2E/pkgs/$p" --allow --json >/dev/null; done
# what `cockpit ui list --json` will answer (v0.5.3), written before the tampering so the shell's load check is what catches it
"$E2E/bin/uilist-fixture" "$E2E/repo" > "$E2E/ui-list.json"
node -e '
const fs=require("fs");const f=process.argv[1];const l=JSON.parse(fs.readFileSync(f));
// S5.3 tampered after admission: the carried viewer module changes on disk under the same pin
const t=l.packages.find(p=>p.name==="hostile-tampered");const m=t.members.find(x=>x.name==="viewers/table/index.js");
const hex=m.hash.slice(7);const b=process.argv[2]+"/"+t.dir+"/blobs/"+hex.slice(0,2)+"/"+hex;
fs.chmodSync(b,0o644);fs.writeFileSync(b,"export function mount(el) { el.textContent = \"tampered\"; }\n");
// S5.3 unknown slot (version skew): a newer package names a slot this shell does not know
l.packages.find(p=>p.name==="hostile-good").ui.push({file:"ui/sidebar.json",slot:"sidebar-ad",label:"Sidebar ad",when:{kind:"claim"}});
fs.writeFileSync(f,JSON.stringify(l,null,1));
// The cockpit drops its whole vocabulary (every named query, the shell'"'"'s decides and answers too) when one
// admitted package fails its check. So the tampered package stays admitted in the ui list (the shell must catch
// it at load) but leaves claims.allowedPackages, as if it was tampered after the cockpit'"'"'s last read.
const c=process.argv[2]+"/.kton/cockpit.config.json";const cfg=JSON.parse(fs.readFileSync(c));
cfg.claims.allowedPackages=cfg.claims.allowedPackages.filter(id=>id!==t.package);fs.writeFileSync(c,JSON.stringify(cfg,null,2));' "$E2E/ui-list.json" "$E2E/repo"

cat > "$E2E/seed.json" <<EOF
{"scope": "$SEED", "requests": ["$R1", "$R2"], "announce": "$ANN", "proposals": {"P1": "$P1", "P2": "$P2", "P3": "$P3", "P4": "$P4"},
 "personKeyId": "$PKID", "agentKeyId": "$AKID", "lookalikeKeyId": "$LKID"}
EOF
echo "seeded: scope $SEED"
