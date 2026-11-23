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

# PR 2: the verticle packages (ktonpkg v0.9.7), built with throwaway keys and admitted (claims.allowedPackages)
node /e2e/build-packages.mjs "$E2E/bin/scope" "$E2E/pkgs" >/dev/null
for p in kton-web-test workbench-graph hostile-good hostile-tampered hostile-network hostile-pin; do cockpit install "$E2E/pkgs/$p" --allow --json >/dev/null; done
# S5.3 tampered after admission (the harness, not the shell, touches the package folder): the carried viewer
# module changes on disk under the same pin. Its member hash is the original file's sha256; the seed records it.
# The package stays admitted: since v0.5.3 a package that fails its check takes out only itself.
node -e '
const fs=require("fs"),c=require("crypto");
const orig=fs.readFileSync("/e2e/packages/hostile/good/viewers/table/index.js");
const hex=c.createHash("sha256").update(orig).digest("hex");
const b=process.argv[1]+"/.kton/packages/hostile-tampered@1/blobs/"+hex.slice(0,2)+"/"+hex;
fs.chmodSync(b,0o644);fs.writeFileSync(b,"export function mount(el) { el.textContent = \"tampered\"; }\n");
fs.writeFileSync(process.argv[2],JSON.stringify({member:"viewers/table/index.js",hash:"sha256:"+hex}));' "$E2E/repo" "$E2E/tampered.json"

# PR 3 (S6): the QM areas live in a registry of their own, repo-s6, with the same keys. Their packages bring field
# aliases (title, text, outcome → qa:…), and the view reads one alias file per registry (the repo root's; cockpit
# ignores a package's): in the lanes registry they would rename the lanes vocabulary's fields.
mkdir -p "$E2E/repo-s6"
cd "$E2E/repo-s6"
git init -q
git config user.email qa@example.org
git config user.name qa
git commit -q --allow-empty -m "throwaway registry for the S6 areas"
git remote add origin https://github.com/qa/workbench-e2e-s6.git
cockpit init >/dev/null
cp -R "$E2E/repo/.kton/keys/." .kton/keys/
cp "$E2E/repo/.kton/local/identity.json" .kton/local/identity.json
node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.repo={owner:"qa",name:"workbench-e2e-s6"};c.persist=Object.assign(c.persist||{},{share:false});
c.query={image:"oci://oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0"};
c.trust=Object.assign(c.trust||{},{tiers:["self","agent"]});fs.writeFileSync(p,JSON.stringify(c,null,2));'
rm -f .kton/keys/agent/qa-decider-claims.pub .kton/keys/agent/qa-decider.pub   # no look-alike here
# the lanes vocabulary too (its named queries; no lanes claims): the window's views poll them in every registry
cockpit install "$E2E/vocab-package" --allow --json >/dev/null
# the S6 packages, their scopes and seeded claims (qm-workbench/s6-packages/seed.sh, README "Seeded data")
for p in qa.register qa.area-one qa.area-two qa.rules; do cockpit install "$E2E/pkgs/$p" --allow --json >/dev/null; done
# the view reads one alias file, .kton/aliases.json (cockpit v0.5.5; a package's own is not applied): the packages'
# alias files are merged into it
node -e '
const fs=require("fs");const m={schema:"https://kton.dev/aliases/v0",prefixes:{},terms:{}};
for(const n of ["qa.register","qa.area-one","qa.area-two"]){const a=JSON.parse(fs.readFileSync("/e2e/packages/s6/"+n+"/templates/aliases.json"));
 for(const k of ["prefixes","terms"])for(const[x,v]of Object.entries(a[k]||{})){m[k]=m[k]||{};if(m[k][x]!==undefined&&JSON.stringify(m[k][x])!==JSON.stringify(v))throw new Error("alias "+x+" differs");m[k][x]=v;}}
fs.writeFileSync(".kton/aliases.json",JSON.stringify(m,null,1));'
S6SCOPES="qa.deviations qa.deviations.raci qa.evaluations qa.evaluations.raci qa.documents"
for s in $S6SCOPES; do
  id=$(cockpit scope seed "$s" 2>&1 | sed -n "s/.*\"$s\": \"\(sha256:[0-9a-f]*\)\".*/\1/p")
  [ -n "$id" ] || { echo "seed of $s failed" >&2; exit 1; }
  node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.claims=c.claims||{};c.claims.scopes=Object.assign(c.claims.scopes||{},{[process.argv[1]]:process.argv[2]});fs.writeFileSync(p,JSON.stringify(c,null,2));' "$s" "$id"
done
# sayq TEMPLATE SUBJECT SCOPE AS FIELDS-JSON -> the claim id (the JSON form: it takes an IRI subject)
sayq() { cockpit say "$(node -e 'console.log(JSON.stringify({template:process.argv[1],subject:process.argv[2],scope:process.argv[3],as:process.argv[4],fields:JSON.parse(process.argv[5])}))' "$@")" | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).claimId))'; }
Q=https://example.org/qa; AG=qa-decider-agent; PE=qa-decider
sayq document-state $Q/document/QA-SOP-10 qa.documents $AG '{"key":"QA-SOP-10","title":"Handling deviations","revision":"2","state":"released","effectiveDate":"2026-10-01"}' >/dev/null
sayq document-state $Q/document/QA-FOR-10 qa.documents $AG '{"key":"QA-FOR-10","title":"Deviation report form","revision":"1","state":"released","effectiveDate":"2026-10-15"}' >/dev/null
sayq document-state $Q/document/QA-SOP-20 qa.documents $AG '{"key":"QA-SOP-20","title":"Evaluating suppliers","revision":"1","state":"draft"}' >/dev/null
sayq document-state $Q/document/QA-FOR-20 qa.documents $AG '{"key":"QA-FOR-20","title":"Supplier evaluation form","revision":"1","state":"released","effectiveDate":"2026-11-01"}' >/dev/null
sayq raci-entry $Q/raci/deviations/1 qa.deviations.raci $AG "{\"activity\":\"Handle a deviation\",\"role\":\"Deviation owner\",\"person\":\"$Q/person/p1\",\"raci\":\"R\"}" >/dev/null
sayq raci-entry $Q/raci/deviations/2 qa.deviations.raci $AG "{\"activity\":\"Handle a deviation\",\"role\":\"QA lead\",\"person\":\"$Q/person/p2\",\"raci\":\"A\"}" >/dev/null
sayq raci-entry $Q/raci/evaluations/1 qa.evaluations.raci $AG "{\"activity\":\"Evaluate a supplier\",\"role\":\"Buyer\",\"person\":\"$Q/person/p3\",\"raci\":\"R\"}" >/dev/null
sayq raci-entry $Q/raci/evaluations/2 qa.evaluations.raci $AG "{\"activity\":\"Evaluate a supplier\",\"role\":\"QA lead\",\"person\":\"$Q/person/p2\",\"raci\":\"A\"}" >/dev/null
D1=$(sayq deviation $Q/deviation/D-001 qa.deviations $AG "{\"identifier\":\"D-001\",\"title\":\"Label printed with the wrong batch number\",\"severity\":\"minor\",\"status\":\"closed\",\"raisedBy\":\"$Q/person/p1\",\"owner\":\"$Q/person/p1\",\"closedOn\":\"2026-11-05\"}")
D2=$(sayq deviation $Q/deviation/D-002 qa.deviations $AG "{\"identifier\":\"D-002\",\"title\":\"Cleaning log entry missing\",\"severity\":\"major\",\"status\":\"open\",\"raisedBy\":\"$Q/person/p2\",\"owner\":\"$Q/person/p1\"}")
D3A=$(sayq deviation $Q/deviation/D-003 qa.deviations $AG "{\"identifier\":\"D-003\",\"title\":\"Temperature excursion in store\",\"severity\":\"major\",\"status\":\"open\",\"raisedBy\":\"$Q/person/p1\",\"owner\":\"$Q/person/p2\"}")
D3=$(sayq deviation $Q/deviation/D-003 qa.deviations $AG "{\"identifier\":\"D-003\",\"title\":\"Temperature excursion in store room 2\",\"severity\":\"major\",\"status\":\"open\",\"raisedBy\":\"$Q/person/p1\",\"owner\":\"$Q/person/p2\",\"replaces\":\"$D3A\"}")
D4=$(sayq deviation $Q/deviation/D-004 qa.deviations $AG "{\"identifier\":\"D-004\",\"title\":\"Calibration overdue on scale 4\",\"severity\":\"critical\",\"status\":\"open\",\"raisedBy\":\"$Q/person/p2\"}")
sayq deviation-approves "$D1" qa.deviations $PE '{"outcome":"approved"}' >/dev/null
sayq deviation-approves "$D2" qa.deviations $PE '{"outcome":"rejected"}' >/dev/null
sayq deviation-approves "$D3" qa.deviations $AG '{"outcome":"approved"}' >/dev/null
sayq deviation-note "$D2" qa.deviations $AG '{"text":"Waiting for the cleaning supervisor."}' >/dev/null

# W: the workbench's own views over invented data (seed-workbench.sh): a decision view with figures, an estimate,
# rows, cards, evidence, requests and a foreign control, guard rules, and relayed inbox messages, in repo-w
. /e2e/seed-workbench.sh
cd "$E2E/repo"

# More flows from outside this repository (run.sh EXTRA_FLOWS): their seed runs here, in this shell (it may use say,
# sayq and the keys above) and sets EXTRA_SEED_JSON, which goes into seed.json as "extra"
EXTRA_SEED_JSON=null
if [ -f /extra/seed.sh ]; then . /extra/seed.sh; cd "$E2E/repo"; fi

cat > "$E2E/seed.json" <<EOF
{"scope": "$SEED", "requests": ["$R1", "$R2"], "announce": "$ANN", "proposals": {"P1": "$P1", "P2": "$P2", "P3": "$P3", "P4": "$P4"},
 "personKeyId": "$PKID", "agentKeyId": "$AKID", "lookalikeKeyId": "$LKID",
 "w": $W_JSON,
 "extra": $EXTRA_SEED_JSON,
 "s6": {"deviations": {"D-001": "$D1", "D-002": "$D2", "D-003-v1": "$D3A", "D-003": "$D3", "D-004": "$D4"}}}
EOF
echo "seeded: scope $SEED"
