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

cd "$E2E/repo"

# The decision view (DM): a copy of verification's dryrun1 data registry (read only, /dm-data), with this run's test keys
# added, the lanes vocabulary (a stand-in proposal for 2c50ddd6), and the view package dryrun1.triage.
# seed_dm <dir> <run>: a copy of verification's dryrun1 data with the view built for <run> (dryrun1: its records scope
# seeded; dryrun2: only the package, so the registry has dryrun1's data and no dryrun2.* scope). Prints the proposal id.
seed_dm() {
    mkdir -p "$E2E/$1" && cp -R /dm-data/. "$E2E/$1/" && cd "$E2E/$1"
    rm -rf .kton/local && mkdir -p .kton/local && cp "$E2E/repo/.kton/local/identity.json" .kton/local/identity.json
    mkdir -p .kton/keys/self .kton/keys/agent
    cp "$E2E/repo/.kton/keys/self/"qa-*.pub .kton/keys/self/ 2>/dev/null || true
    cp "$E2E/repo/.kton/keys/agent/qa-decider-agent"*.pub .kton/keys/agent/
    rm -f .kton/keys/self/qa-lookalike*
    node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.trust=Object.assign(c.trust||{},{tiers:[...new Set([...(c.trust&&c.trust.tiers||[]),"self","agent"])]});
c.query={image:"oci://oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0"};
c.index={kind:"oxigraph"};c.persist=Object.assign(c.persist||{},{share:false});fs.writeFileSync(p,JSON.stringify(c,null,2));'
    git config user.email qa@example.org; git config user.name qa
    cp "$VOCAB"/templates/*.json .kton/templates/
    VOCAB="$VOCAB" node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.claims=c.claims||{};
const names=fs.readdirSync(process.env.VOCAB+"/templates").filter(f=>f.endsWith(".json")).map(f=>f.slice(0,-5));
c.claims.allowedTemplates=[...new Set([...(c.claims.allowedTemplates||[]),...names])];fs.writeFileSync(p,JSON.stringify(c,null,2));'
    cockpit install "$E2E/vocab-package" --allow --json >/dev/null
    cockpit install "$E2E/pkgs/$2.triage" --allow --json >/dev/null
    seedScope() {
      id=$(cockpit scope seed "$1" 2>&1 | sed -n "s/.*\"$1\": \"\(sha256:[0-9a-f]*\)\".*/\1/p")
      node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.claims.scopes[process.argv[1]]=process.argv[2];fs.writeFileSync(p,JSON.stringify(c,null,2));' "$1" "$id"
      echo "$id"
    }
    LS=$(seedScope lanes)
    # the view's records scope (its entry's scopes.records): the triage claims land here, never in verification's scope
    [ "$2" = dryrun1 ] && seedScope dryrun1.triage >/dev/null
    # test-execution's dryrun1.runs (a copy, /runs-data), federated read-only; its dev key's public halves in a tier
    cockpit say proposes "$LS" area=qm title="Can the 5.0.0 mapping be the basis for G1 triage?" text="Stand-in for verification's 2c50ddd6 (dry run 1). Option b names 237 elements." why="e2e seed" \
      options='[{"id":"a","label":"Take round 9"},{"id":"b","label":"Wolfgang reviews the 237"},{"id":"c","label":"Settle rules 6, 9, 10"}]' --scope lanes --as qa-requestor --json | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).claimId))'
    # federation after the seed's own says: a say with a federation source was slow before kton-cockpit #198 (main 99fe43e)
    if [ -d /runs-data/.kton ]; then
      [ -d "$E2E/runs-dm" ] || { mkdir -p "$E2E/runs-dm" && cp -R /runs-data/. "$E2E/runs-dm/"; }
      mkdir -p .kton/keys/lane && cp /runs-data/.kton/keys/self/lane-test-execution-dryrun1-dev*.pub .kton/keys/lane/
      node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.trust.tiers=[...new Set([...c.trust.tiers,"lane"])];fs.writeFileSync(p,JSON.stringify(c,null,2));'
      cockpit federation add test-execution-runs "$E2E/runs-dm" >/dev/null 2>&1
    fi
    cd "$E2E/repo"
}
DM_PROPOSAL=""; DM2_PROPOSAL=""
if [ -d /dm-data/.kton ]; then
  DM_PROPOSAL=$(seed_dm repo-dm dryrun1 | tail -1)
  # the same view built for dryrun2 (build.sh RUN=dryrun2), in its own registry copy (one package per name set)
  DM2_PROPOSAL=$(seed_dm repo-dm2 dryrun2 | tail -1)
fi

# R (role views, dry run 1; userinterface imvprototypes workbench/dryrun1-role-views): only with ROLES_RUNS, ROLES_PKG
# and ROLES_TRIAGE_PKG (run.sh mounts them read only). repo-roles is a COPY of test-execution's dryrun1.runs registry
# (its claims and its dev key's public halves, as they are) with this run's test keys added, the triage package and
# the role views' package installed, and a stand-in dryrun1.triage: the guard rules' good and bad cases (qm-packages
# areas/dryrun1-triage testdata/scenario.py), signed with this run's throwaway keys. ROLES_COVERAGE and ROLES_MAPPING
# (optional): verification's registries, federated LIVE and read-only (below). An Oxigraph index (the first ask after
# the seed builds it; then asks take about a second).
ROLES=""
if [ -d /roles-runs/.kton ] && [ -d /roles-pkg ] && [ -d /roles-triage-pkg ]; then
  mkdir -p "$E2E/repo-roles" && cp -R /roles-runs/. "$E2E/repo-roles/" && cd "$E2E/repo-roles"
  # cockpit v0.5.5 reads the alias file only at .kton/aliases.json
  [ ! -f aliases.json ] || [ -f .kton/aliases.json ] || mv aliases.json .kton/aliases.json
  rm -rf .kton/local && mkdir -p .kton/local && cp "$E2E/repo/.kton/local/identity.json" .kton/local/identity.json
  mkdir -p .kton/keys/self .kton/keys/agent
  cp "$E2E/repo/.kton/keys/self/"qa-decider*.pub .kton/keys/self/
  cp "$E2E/repo/.kton/keys/agent/qa-decider-agent"*.pub .kton/keys/agent/
  node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.trust=Object.assign(c.trust||{},{tiers:[...new Set([...((c.trust||{}).tiers||[]),"self","agent"])]});
c.query={image:"oci://oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0"};
c.persist=Object.assign(c.persist||{},{share:false});c.index={kind:"oxigraph"};fs.writeFileSync(p,JSON.stringify(c,null,2));'
  git config user.email qa@example.org; git config user.name qa
  cockpit install "$E2E/vocab-package" --allow --json >/dev/null
  cp -R /roles-triage-pkg "$E2E/pkg-roles-triage" && cockpit install "$E2E/pkg-roles-triage" --allow --json >/dev/null
  cp -R /roles-pkg "$E2E/pkg-roles" && cockpit install "$E2E/pkg-roles" --allow --json >/dev/null
  TS=$(cockpit scope seed dryrun1.triage 2>&1 | sed -n 's/.*"dryrun1.triage": "\(sha256:[0-9a-f]*\)".*/\1/p')
  [ -n "$TS" ] || { echo "seed of dryrun1.triage failed" >&2; exit 1; }
  node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.claims.scopes["dryrun1.triage"]=process.argv[1];fs.writeFileSync(p,JSON.stringify(c,null,2));' "$TS"
  E=https://scinteco.com/ns/dryrun1/element/; RD=https://scinteco.com/ns/dryrun1/round/; PP=https://scinteco.com/ns/agents#
  SRC='"sourceProduct":"sha256:0000000000000000000000000000000000000000000000000000000000000001","sourceBuilt":"2026-11-16","certainty":"medium"'
  T=dryrun1.triage; node_of() { echo "https://kton.dev/o/${1#sha256:}"; }
  RANK='"class":"G5","position":"1","gxpImpact":"data-integrity","likelihood":"high","detectability":"low"'
  E1=$(sayq triage-classifies ${E}e1 $T $AG "{$SRC,\"class\":\"G5\",\"evidence\":\"static\",\"reach\":\"customer\"}")
  E2S=$(sayq triage-classifies ${E}e2 $T $AG "{$SRC,\"class\":\"G5\",\"evidence\":\"static\"}")
  sayq triage-classifies ${E}e2 $T $AG "{$SRC,\"class\":\"G5\",\"evidence\":\"run\",\"examples\":\"S6 unit-12\"}" >/dev/null
  E3=$(sayq triage-classifies ${E}e3 $T $AG "{$SRC,\"class\":\"G3\",\"evidence\":\"run\"}")
  sayq triage-classifies ${E}e4 $T $AG "{$SRC,\"class\":\"G1.1\",\"evidence\":\"static\"}" >/dev/null
  sayq triage-classifies ${E}e5 $T $AG "{$SRC,\"class\":\"G2\",\"evidence\":\"mapping\",\"reach\":\"customer\"}" >/dev/null
  sayq triage-reviews "$(node_of "$E1")" $T $AG '{"outcome":"changes"}' >/dev/null
  sayq triage-reviews "$(node_of "$E3")" $T $PE '{"outcome":"approved"}' >/dev/null
  sayq triage-ranks ${RD}r1/G5 $T $PE "{\"round\":\"${RD}r1\",$RANK}" >/dev/null
  sayq triage-assigns ${RD}r1/G5 $T $PE "{\"round\":\"${RD}r1\",\"assignedTo\":\"${PP}christof-winkler\",\"assignedRole\":\"test\",\"due\":\"2026-12-01\"}" >/dev/null
  sayq triage-assigns ${RD}r2/G3 $T $PE "{\"round\":\"${RD}r2\",\"assignedTo\":\"${PP}gregor-metzenbauer\",\"assignedRole\":\"spec\"}" >/dev/null
  sayq triage-assigns ${RD}r3/G5 $T $PE "{\"round\":\"${RD}r3\",\"assignedTo\":\"${PP}wolfgang-schwarzenbrunner\",\"assignedRole\":\"dev\"}" >/dev/null
  sleep 2
  sayq triage-ranks ${RD}r3/G5 $T $PE "{\"round\":\"${RD}r3\",$RANK}" >/dev/null
  # fa4908f: sourceBuilt is optional beside sourceSeenSince; one classification with neither (guard rule source-stamped,
  # "blocked: no source date" in Christof's view), one with only sourceSeenSince
  NODATE='"sourceProduct":"sha256:0000000000000000000000000000000000000000000000000000000000000001","certainty":"medium"'
  E6=$(sayq triage-classifies ${E}e6 $T $AG "{$NODATE,\"class\":\"G2.1\",\"evidence\":\"run\"}")
  sayq triage-classifies ${E}e7 $T $AG "{$NODATE,\"sourceSeenSince\":\"2026-11-20\",\"class\":\"G2.4\",\"evidence\":\"run\"}" >/dev/null
  # a review item naming the commit it reviews, in the persona lane's clone (mounted read-only as christof-ext-core)
  CC="${CHRISTOF_COMMIT:-b2b4f50}"
  E8=$(sayq triage-classifies ${E}e8 $T $AG "{$SRC,\"class\":\"G2.1\",\"evidence\":\"run\",\"examples\":\"fix in christof-ext-core@$CC (stand-in review item, e2e)\"}")
  # its verdict names the approved commit only in its free text (persona finding a5f326ae: an approval of 453f574
  # claimed to hold for b2b4f50); the review screen reads it out and checks it against the clone
  AC="${APPROVED_COMMIT:-453f574}"
  sayq triage-reviews "$(node_of "$E8")" $T $PE "{\"outcome\":\"approved\",\"why\":\"approved on $AC (group 1); holds for $CC (stand-in, e2e)\"}" >/dev/null
  # STAND-IN for test-execution's answers (rerun-compared, capture-result): a throwaway key of this run, never theirs;
  # the steps sign the answers with it into dryrun1.runs and say so
  cockpit keygen standin-test-execution >/dev/null
  ROLES_COV=false; ROLES_MAP=false; ROLES_SR=false
  # STAND-IN for Wolfgang's spec-review clone (path not known yet): a throwaway COPY of verification's dryrun1-triage
  # (scope dryrun1.spec-review, template spec-decision c2f6ae3) with one open row signed by a throwaway key of this run
  if [ -d /roles-spec-review/.kton ]; then
    SR="$E2E/standin-spec-review"; mkdir -p "$SR" && cp -R /roles-spec-review/. "$SR/" && cd "$SR"
    rm -rf .kton/local && mkdir -p .kton/local && cp "$E2E/repo-roles/.kton/local/identity.json" .kton/local/identity.json
    git config user.email qa@example.org; git config user.name qa
    cockpit keygen standin-wolfgang >/dev/null
    SPEC=ics2133; SPATH="specifications/vscode-extension/Explorer/ics2133 improve Audit Trail Webview.md"
    cockpit say "$(node -e 'console.log(JSON.stringify({template:"spec-decision",subject:"https://scinteco.com/ns/improve/spec/ics2133",scope:"dryrun1.spec-review",as:"standin-wolfgang",fields:{spec:"ics2133",element:"ics2133#*",decision:"open",owner:"product-development",question:"STAND-IN (e2e): are Actor and IP Address meant to be hidden by default?",evidence:"stand-in",specCommit:"49966c23eceb754264ea6c36296a6ca2ff6214b8",specPath:process.argv[1],specLine:"12"}}))' "$SPATH")" >/dev/null
    cd "$E2E/repo-roles"
    cp "$SR/.kton/keys/self/"standin-wolfgang*.pub .kton/keys/self/
    # verification's own key (the scope's seed) in tier lane, as for its other repos
    mkdir -p .kton/keys/lane && for k in "$SR/.kton/keys/self/"*.pub; do case "$k" in *standin-wolfgang*) ;; *) cp "$k" .kton/keys/lane/;; esac; done
    node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.trust.tiers=[...new Set([...c.trust.tiers,"lane"])];fs.writeFileSync(p,JSON.stringify(c,null,2));'
    cockpit federation add standin-spec-review "$SR" >/dev/null
    ROLES_SR=true
  fi
  # verification's dryrun1-coverage (~11.5k claims) and dryrun1-mapping (dryrun1.classes: the run-class map), LIVE:
  # federated read-only from their own paths (mounted :ro; federation only reads, the union and the index live under
  # this run's home), after the says (cockpit main 99fe43e or later: #198 fixed the federation add of a large source)
  for pair in "verification-coverage=${ROLES_COVERAGE:-}" "verification-mapping=${ROLES_MAPPING:-}"; do
    n=${pair%%=*}; src=${pair#*=}
    [ -n "$src" ] && [ -d "$src/.kton" ] || continue
    mkdir -p .kton/keys/lane && cp "$src/.kton/keys/self/"*.pub .kton/keys/lane/
    node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.trust.tiers=[...new Set([...c.trust.tiers,"lane"])];fs.writeFileSync(p,JSON.stringify(c,null,2));'
    cockpit federation add "$n" "$src" >/dev/null
    case "$n" in verification-coverage) ROLES_COV=true;; verification-mapping) ROLES_MAP=true;; esac
  done
  # warm the index once (the first ask after the seed builds the view with the federated sources, ADR-018 scope graphs:
  # ~40 s alone on ai1); the window's first asks then answer from it instead of running into their timeout together
  for q in ch-figures ch-test-gaps-by-part wf-figures gr-review-items; do cockpit ask sparql --name "$q" --results-json --timeout 120 >/dev/null 2>&1 || true; done
  ROLES="{\"e1\": \"$E1\", \"e2static\": \"$E2S\", \"e6\": \"$E6\", \"e8\": \"$E8\", \"christofCommit\": \"$CC\", \"approvedCommit\": \"$AC\", \"coverage\": $ROLES_COV, \"mapping\": $ROLES_MAP, \"specReview\": $ROLES_SR}"
  cd "$E2E/repo"
fi

cat > "$E2E/seed.json" <<EOF
{"scope": "$SEED", "requests": ["$R1", "$R2"], "announce": "$ANN", "proposals": {"P1": "$P1", "P2": "$P2", "P3": "$P3", "P4": "$P4"},
 "personKeyId": "$PKID", "agentKeyId": "$AKID", "lookalikeKeyId": "$LKID",
 "dm": {"proposal": "$DM_PROPOSAL", "proposal2": "$DM2_PROPOSAL"},
 "roles": ${ROLES:-null},
 "s6": {"deviations": {"D-001": "$D1", "D-002": "$D2", "D-003-v1": "$D3A", "D-003": "$D3", "D-004": "$D4"}}}
EOF
echo "seeded: scope $SEED"
