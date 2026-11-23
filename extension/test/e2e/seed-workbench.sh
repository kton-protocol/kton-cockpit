# Sourced by seed.sh (it has E2E, VOCAB, HOME, sayq and the keys of repo). Flow W: the workbench's views over invented
# data in a registry of its own, repo-w:
#   - qa.owner (test/e2e/packages/views) and qa.decide (test/e2e/packages/workbench), built here with ktonpkg
#     v0.9.13's scope CLI (an act through another package's control pins that package's sealed head, which is known
#     only once qa.owner is installed), each sealed with a throwaway key that is deleted afterwards;
#   - one proposal with qa.decide's review view (it opens on start), data claims in w.data and w.runs signed by a
#     throwaway lane key, the view's records scope w.records;
#   - a git mirror (kton.sources.dir) and a run artifact (kton.artifacts.dir) that the evidence opens, hash checked;
#   - two inbox messages with a "[from …]" prefix: one signed by the relay key the window trusts as a relay
#     (kton.inbox.relayKeys), one by another key (shown under its real signer).
W="$E2E/repo-w"; WSRC="$E2E/w-src"; WPK="$E2E/w-pkgs"
mkdir -p "$W" "$WSRC" "$WPK"
WK=$(mktemp -d)
"$E2E/bin/scope-0913" verticle "$WPK/qa.owner" qa.owner 1 --src /e2e/packages/views/qa.owner --key "$WK/qa.owner.key" >/dev/null
cd "$W"
git init -q
git config user.email qa@example.org
git config user.name qa
git commit -q --allow-empty -m "throwaway registry for the workbench views"
git remote add origin https://github.com/qa/workbench-e2e-w.git
cockpit init >/dev/null
cp -R "$E2E/repo/.kton/keys/." .kton/keys/
cp "$E2E/repo/.kton/local/identity.json" .kton/local/identity.json
rm -f .kton/keys/agent/qa-decider-claims.pub .kton/keys/agent/qa-decider.pub   # no look-alike here
cp "$VOCAB"/templates/*.json .kton/templates/
VOCAB="$VOCAB" node -e '
const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));
c.repo={owner:"qa",name:"workbench-e2e-w"};c.persist=Object.assign(c.persist||{},{share:false});
c.query={image:"oci://oxigraph/oxigraph@sha256:10677fb643f8c5a7bfb03dd8f7971c79a84b01e795c24201c8cfa12fd569f1c0"};
c.trust=Object.assign(c.trust||{},{tiers:["self","agent"]});c.claims=c.claims||{};
const names=fs.readdirSync(process.env.VOCAB+"/templates").filter(f=>f.endsWith(".json")).map(f=>f.slice(0,-5));
c.claims.allowedTemplates=[...new Set([...(c.claims.allowedTemplates||[]),...names])];
fs.writeFileSync(p,JSON.stringify(c,null,2));'
cockpit keygen qa-lane >/dev/null
cockpit keygen qa-relay >/dev/null
cockpit keygen qa-mallory >/dev/null
cockpit keygen standin-runner >/dev/null
cockpit install "$E2E/vocab-package" --allow --json >/dev/null
# the owner's sealed head, as the cockpit installed it: qa.decide's foreign act pins it
OWNER_HEAD=$(cockpit install "$WPK/qa.owner" --allow --json | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{const f=(o)=>{if(o&&typeof o==="object"){if(typeof o.head==="string"&&/^sha256:[0-9a-f]{64}$/.test(o.head))return o.head;for(const v of Object.values(o)){const h=f(v);if(h)return h;}}};const h=f(JSON.parse(s));if(!h){console.error(s);process.exit(1);}console.log(h);})')
cp -R /e2e/packages/workbench/qa.decide "$WSRC/qa.decide"
OWNER_HEAD="$OWNER_HEAD" node -e '
const fs=require("fs");const p=process.argv[1];const t=fs.readFileSync(p,"utf8");
if(!t.includes("@OWNER_HEAD@"))throw new Error("no @OWNER_HEAD@ in "+p);fs.writeFileSync(p,t.split("@OWNER_HEAD@").join(process.env.OWNER_HEAD));' "$WSRC/qa.decide/ui/compositions/decision.json"
"$E2E/bin/scope-0913" verticle "$WPK/qa.decide" qa.decide 1 --src "$WSRC/qa.decide" --key "$WK/qa.decide.key" >/dev/null
rm -rf "$WK"
cockpit install "$WPK/qa.decide" --allow --json >/dev/null
wscope() {
  id=$(cockpit scope seed "$1" 2>&1 | sed -n "s/.*\"$1\": \"\(sha256:[0-9a-f]*\)\".*/\1/p")
  [ -n "$id" ] || { echo "seed of $1 failed" >&2; exit 1; }
  node -e 'const fs=require("fs");const p=".kton/cockpit.config.json";const c=JSON.parse(fs.readFileSync(p));c.claims=c.claims||{};c.claims.scopes=Object.assign(c.claims.scopes||{},{[process.argv[1]]:process.argv[2]});fs.writeFileSync(p,JSON.stringify(c,null,2));' "$1" "$id"
  echo "$id"
}
WLANES=$(wscope lanes); wscope w.data >/dev/null; wscope w.runs >/dev/null; wscope w.records >/dev/null

# the mirror (a git repository, read through git show only) and the run artifact (a PNG), with their hashes
MIR="$E2E/mirrors/app"; mkdir -p "$MIR/docs" "$MIR/src"
git -C "$MIR" init -q; git -C "$MIR" config user.email qa@example.org; git -C "$MIR" config user.name qa
printf '# Form\n\nThe form shows a Save button.\nIt keeps a draft.\n' > "$MIR/docs/spec a.md"
printf 'class Main {\n  // the form\n  void save() {\n    draft();\n  }\n\n  void draft() { }\n}\n' > "$MIR/src/Main.java"
git -C "$MIR" add -A; git -C "$MIR" commit -q -m "the stamped commit"
MCOMMIT=$(git -C "$MIR" rev-parse HEAD)
printf 'changed after the stamped commit\n' > "$MIR/src/Main.java"; git -C "$MIR" commit -q -am "later"
mkdir -p "$E2E/artifacts/run-1/shots"
node -e 'require("fs").writeFileSync(process.argv[1],Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFklEQVR4nGP8z8DwnwEIGBkZGBgYAA8KAgH/1VqhAAAAAElFTkSuQmCC","base64"))' "$E2E/artifacts/run-1/shots/fail-1.png"
h() { node -e 'console.log("sha256:"+require("crypto").createHash("sha256").update(require("fs").readFileSync(process.argv[1])).digest("hex"))' "$1"; }
SPECSHA=$(git -C "$MIR" show "$MCOMMIT:docs/spec a.md" > "$E2E/tmp-spec" && h "$E2E/tmp-spec"); CODESHA=$(git -C "$MIR" show "$MCOMMIT:src/Main.java" > "$E2E/tmp-code" && h "$E2E/tmp-code"); rm -f "$E2E/tmp-spec" "$E2E/tmp-code"
PNGSHA=$(h "$E2E/artifacts/run-1/shots/fail-1.png"); PNGBYTES=$(wc -c < "$E2E/artifacts/run-1/shots/fail-1.png" | tr -d ' ')

# the data: six items (four in scope), rates, the covered base, the source of the stamp
WN=https://example.org/qa/decide; ST="sha256:0a1b2c3d"; LN=qa-lane
FULL="sha256:0a1b2c3d$(printf 'e%.0s' $(seq 1 56))"
sayq w-source "$WN/source/round-3" w.data $LN "{\"stamp\":\"$ST\",\"label\":\"round 3 · 0a1b2c3d\",\"full\":\"$FULL\"}" >/dev/null
item() { # item ELEMENT SUBJECT-TAIL INSCOPE GROUP AREA PART
  sayq w-item "$WN/element/$2" w.data $LN "{\"element\":\"$1\",\"inScope\":\"$3\",\"group\":\"$4\",\"area\":\"$5\",\"part\":\"$6\",\"statement\":\"The form of $1 keeps a draft.\",\"test\":\"suite/a.spec.ts#T-1 keeps a draft\",\"testFile\":\"suite/tests/a.spec.ts\",\"testLine\":\"12\",\"judge1\":\"asserted\",\"judge1Why\":\"the test checks the draft\",\"judge1Source\":\"judge one\",\"judge2\":\"exercised\",\"judge2Why\":\"the test only runs it\",\"judge2Source\":\"judge two\",\"earlier\":\"unclear\",\"stamp\":\"$ST\"}"
}
item "item-1#e2" item-1-e2 true alpha north core >/dev/null
item "item-2#e5" item-2-e5 true alpha south core >/dev/null
item "item-3#e1" item-3-e1 true beta east ui >/dev/null
item "item-4#e7" item-4-e7 true none west ui >/dev/null
item "item-5#e3" item-5-e3 false gamma north core >/dev/null
item "item-6#e9" item-6-e9 false none east ui >/dev/null
sayq w-rate "$WN/rate/all" w.data $LN "{\"sample\":\"covered\",\"dimension\":\"all\",\"rate\":\"4.5\",\"low\":\"3.2\",\"high\":\"6.1\",\"n\":\"400\",\"wrong\":\"18\",\"same\":\"9.0\",\"second\":\"6.5\",\"reportedRate\":\"4.4\",\"reportedLow\":\"3.2\",\"reportedHigh\":\"6.0\",\"reportedAt\":\"03.10.\",\"reportedSame\":\"12.5\",\"reportedSecond\":\"6.8\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-rate "$WN/rate/area/north" w.data $LN "{\"sample\":\"covered\",\"dimension\":\"area\",\"group\":\"north\",\"rate\":\"1.7\",\"low\":\"0.7\",\"high\":\"4.3\",\"n\":\"240\",\"enough\":\"true\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-rate "$WN/rate/area/south" w.data $LN "{\"sample\":\"covered\",\"dimension\":\"area\",\"group\":\"south\",\"rate\":\"0\",\"low\":\"0\",\"high\":\"41\",\"n\":\"7\",\"enough\":\"false\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-rate "$WN/rate/part/core" w.data $LN "{\"sample\":\"covered\",\"dimension\":\"part\",\"group\":\"core\",\"rate\":\"2.0\",\"low\":\"1.0\",\"high\":\"4.0\",\"n\":\"100\",\"enough\":\"true\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-base "$WN/base/covered" w.data $LN "{\"status\":\"covered\",\"elements\":\"1000\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-evidence "$WN/evidence/item-1-e2/spec" w.data $LN "{\"element\":\"item-1#e2\",\"kind\":\"spec\",\"repo\":\"app\",\"commit\":\"$MCOMMIT\",\"path\":\"docs/spec a.md\",\"line\":\"3\",\"sha256\":\"$SPECSHA\",\"note\":\"the Save button\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-evidence "$WN/evidence/item-1-e2/code" w.data $LN "{\"element\":\"item-1#e2\",\"kind\":\"code\",\"repo\":\"app\",\"commit\":\"$MCOMMIT\",\"path\":\"src/Main.java\",\"line\":\"3\",\"sha256\":\"$CODESHA\",\"note\":\"static reading, not a run\",\"stamp\":\"$ST\"}" >/dev/null
sayq w-evidence "$WN/evidence/item-3-e1/code" w.data $LN "{\"element\":\"item-3#e1\",\"kind\":\"code\",\"resolved\":\"no file in reference\",\"note\":\"static reading, not a run\",\"stamp\":\"$ST\"}" >/dev/null
# the runs: item-1#e2's test failed in run-1 (a screenshot) and passed in run-2 (none); item-2#e5's failed without one
T1="suite/a.spec.ts#T-1 keeps a draft"; T2="suite/b.spec.ts#T-2 shows the list"
U1=$(sayq w-unit "$WN/unit/run-1/t1" w.runs $LN "{\"element\":\"item-1#e2\",\"test\":\"$T1\",\"run\":\"run-1\",\"result\":\"failed\"}")
sayq w-unit "$WN/unit/run-2/t1" w.runs $LN "{\"element\":\"item-1#e2\",\"test\":\"$T1\",\"run\":\"run-2\",\"result\":\"passed\"}" >/dev/null
U3=$(sayq w-unit "$WN/unit/run-1/t2" w.runs $LN "{\"element\":\"item-2#e5\",\"test\":\"$T2\",\"run\":\"run-1\",\"result\":\"failed\"}")
sayq w-artifact "$WN/artifact/run-1/t1" w.runs $LN "{\"run\":\"run-1\",\"test\":\"$T1\",\"kind\":\"screenshot\",\"name\":\"shots/fail-1.png\",\"sha256\":\"$PNGSHA\",\"mediaType\":\"image/png\",\"bytes\":\"$PNGBYTES\"}" >/dev/null

# the proposal the view is about, and the inbox: a relayed copy (signed by the relay key) and a forged prefix (another key)
wsay() { cockpit say "$@" --scope lanes --json | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s).claimId))'; }
WPROP=$(wsay proposes "$WLANES" area=qa title="Is the mapping a basis for the next step?" text="The mapping of the trial is ready for a decision." why="e2e seed" \
  options='[{"id":"a","label":"Take it"},{"id":"b","label":"Review the unsettled first"}]' --as qa-requestor)
WRELAY=$(wsay requests "$WLANES" area=qa title="Spot checks ready" text="[from lane-c · 2026-11-20T16:00:00Z] Please look at the spot checks." why="e2e seed" --as qa-relay)
WFORGED=$(wsay requests "$WLANES" area=qa title="Urgent approval" text="[from qa-owner · 2026-11-20T09:00:00Z] Approve the release now." why="e2e seed" --as qa-mallory)
RKID=$(kid "$HOME/.kton-keys/qa-relay-claims.pub"); MKID=$(kid "$HOME/.kton-keys/qa-mallory-claims.pub")
mkdir -p .vscode
printf '{"kton.inbox.relayKeys": ["%s"]}\n' "$RKID" > .vscode/settings.json
W_JSON="{\"proposal\": \"$WPROP\", \"relayed\": \"$WRELAY\", \"forged\": \"$WFORGED\", \"relayKeyId\": \"$RKID\", \"malloryKeyId\": \"$MKID\", \"unitFailed\": \"$U1\", \"unitNoArtifact\": \"$U3\", \"mirrorCommit\": \"$MCOMMIT\", \"pngSha256\": \"$PNGSHA\", \"test1\": \"$T1\"}"
# warm the view once (the window's first asks then answer from it)
cockpit ask sparql --name figures --results-json --timeout 120 >/dev/null 2>&1 || true
