#!/bin/sh
# Build a throwaway registry for the shell's e2e flows. Runs INSIDE a container. Arguments: the work
# dir (absolute; mounted at the same path everywhere) and the vocabulary folder (read only). The cockpit
# binary is /usr/local/bin/cockpit (mounted read only).
#
# Test identities are generated here, under $E2E/home, and discarded with the dir: never a person's keys,
# never a real registry. qa-decider is the window's person key, qa-decider-agent its agent key (in a
# trust tier of its own, "agent"), qa-requestor a second person.
set -eu
E2E="$1"; VOCAB="$2"
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
# the agent key in a tier of its own: a decides it signs is a record, never a decision (review check 7)
mkdir -p .kton/keys/agent
for f in .kton/keys/self/qa-decider-agent*.pub; do mv "$f" .kton/keys/agent/; done
node -e '
const fs=require("fs");
const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.trust=Object.assign(c.trust||{},{tiers:["self","agent"]});fs.writeFileSync(p,JSON.stringify(c,null,2));
const i=".kton/local/identity.json";const id=JSON.parse(fs.readFileSync(i));
id.keyring=process.env.HOME+"/.kton-keys";fs.writeFileSync(i,JSON.stringify(id,null,2));'
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
cat > "$E2E/surfaces.json" <<EOF
{"_doc": "throwaway surfaces for the e2e (workgroup format)",
 "core": {"lanes": {"qa-lane":  {"lane": "active/qa-lane"}},  "folder": "$E2E/surfaces/core"},
 "ops":  {"lanes": {"ops-lane": {"lane": "active/ops-lane"}}}}
EOF
cat > "$E2E/seed.json" <<EOF
{"scope": "$SEED", "requests": ["$R1", "$R2"], "announce": "$ANN", "proposals": {"P1": "$P1", "P2": "$P2", "P3": "$P3", "P4": "$P4"}}
EOF
echo "seeded: scope $SEED"
