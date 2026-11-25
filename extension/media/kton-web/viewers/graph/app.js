// Surface any uncaught error visibly - a webview has no console the user reads, and a throw before
// load() otherwise leaves only the empty header. Added at the top so it catches the rest of this file.
(function(){ function show(msg){ try{ var d=document.getElementById("err")||document.body;
  if(d.style) d.style.display="grid"; d.textContent="JS-Fehler: "+msg; }catch(_){}}
  addEventListener("error", function(e){ show((e.message||e)+" @ "+(e.filename||"")+":"+(e.lineno||"")); });
  addEventListener("unhandledrejection", function(e){ show("rejected: "+((e.reason&&e.reason.message)||e.reason)); }); })();
const $=s=>document.querySelector(s), $$=s=>[...document.querySelectorAll(s)];
const PAL=["--a","--b","--c","--d","--e","--f"];
const css=k=>getComputedStyle(document.documentElement).getPropertyValue(k).trim();
let G=null, partColor={}, ex=null, filt=null, LOC={}, autoPreview=!!localStorage.getItem("kton-autopreview")||new URLSearchParams(location.search).get("preview")==="1";
const PED_CAP=250; let LOD=false;   // explorer scale guards: cap a pedigree to PED_CAP nodes; drop labels/cull above ~220 rendered
/**
 * Is this view running inside the verticle host, or on the open web?
 *
 * Read lazily: the host injects improveHost into the webview, and the answer must not be frozen
 * before it lands. Everything downstream of it is a fold, never a fork - there is ONE app.js, and
 * the web viewer keeps the panels that are its whole interface.
 */
const EMBEDDED=()=>typeof window!=="undefined" && !!window.improveHost;

/**
 * Show what stands at a node. On the web that is the record panel. Embedded, the compass IS the
 * inspector - a whole verticle whose job is this - so re-aim it instead of drawing a second one on
 * top of the graph. A grouped node has no counterpart out there, so it ungroups in place.
 */
function inspect(id){
  // In the contracted view the claims live ON the node, so a click has something to show here and
  // jumping straight to the compass would throw the picture away to read three lines.
  if(EMBEDDED() && VIEW==="contracted" && marksOf(id).length){ openNode(id); return; }
  if(!EMBEDDED()){ openNode(id); return; }
  const gn=ex&&ex._byId&&ex._byId.get(id);
  if(gn&&gn.type==="group"){ ex.grouped=false; drawControls(); layoutAndRun(); return; }
  if(window.improveHost) window.improveHost.callOp("ui:openVerticle",
    {verticle:"kton-compass",context:{focus:id, mode:ex&&ex.mode}}).catch(()=>{});
}

const RECORD_CAP=20000;             // load-time safety valve: bound the whole-graph BUILD so a huge union.json (100k records, ~100MB) cannot hang the tab. Above this, the overview is capped and the reader is pointed at ?focus=<hash> for a single pedigree.
const pcol=p=>css(partColor[p]||"--dim");
const clip=(s,n)=>s&&s.length>n?s.slice(0,n)+"…":s;

let _wasmReady=false, NAV=null, REG=null, VS=null, HOST=null, BRIDGE=null, QS=null, GRP=null, LAY=null, JOIN=null, SEL=null, PRF=null, ANNO=null;
// What this viewer shows (core/profile.js): ?view=, or set by a host. ANN: per step, its command,
// description and rationale (core/annotate.js); FULLH: short file hash -> full one, for actions.
let PROF=null, ANN=new Map(), FULLH=new Map();
// the multi-selection (prof().select.multi): node ids of steps, and full foton ids for the host
let SELSET=new Set(), FOTONID=new Map(), SELM=null;
// step-to-step relations from verified claims (parent-step, based-on) - core/annotate.js
let RELS=[], NAMES_ALL={}, PANEL_AT_START=new URLSearchParams(location.search).get("panel")!=="closed";
// closed only until the reader first touches the page: loading may enter the explorer more than once
if(!PANEL_AT_START) document.addEventListener("pointerdown",()=>{ PANEL_AT_START=true; },{once:true,capture:true});
function prof(){ return PROF || (PROF = PRF.resolveProfile(new URLSearchParams(location.search).get("view"))); }
// what a step did and why; a step only the recorder saw has its code as text and no claims
function stepInfo(n){ const a=ANN.get(n.id)||{};
  return { command: a.command || (n.origin==="recorded"&&n.text) || n.role || "", description: a.description||"", rationale: a.rationale||"" }; }
// Switch the page's pieces on or off by the profile - CSS does the hiding, the layout measures what is left.
// the node types drawn: all this corpus has, less what the profile hides (prof().graph.hide)
function typesShown(){ const h=new Set(prof().graph.hide||[]); return new Set((G&&G._types||[]).filter(t=>!h.has(t))); }
// hidden "type:kind" pairs, e.g. "source:code" - the scripts a step ran (still in its inputs and on hover)
// "source:single-use" in graph.hide: a file only one step reads is part of that step (still in its
// inputs); the files several steps share - the data set - stay as nodes
function singleUse(n){ return n.type==="source"&&filt&&filt.kinds&&filt.kinds.has("source:single-use")&&((G.in&&G.in[n.id])||[]).filter(e=>e.kind==="builds-on").length<=1; }
function kindsHidden(){ const h=new Set((prof().graph.hide||[]).filter(x=>String(x).includes(":")));
  if(!(prof().graph.show||[]).includes("scope")){ h.add("claim:scope"); h.add("entity:scope"); }   // scope seeds: structure, hidden unless shown
  return h; }
function applyChrome(){ const c=prof().chrome; for(const k of Object.keys(c)) document.body.classList.toggle("no-"+k, !c[k]);
  document.body.dataset.view=prof().preset; }
// ?select=: the records come from a store, a portion at a time (core/select.js), not as a whole
// union.json. RECORDED is a recorder's graph to join with whatever is signed (core/join.js).
let STORE=null, RECORDED=null;
// The records the drawn graph was built from (a union's JSON text). Focus-by-file-hash reads the
// statements in here; a graph a host built arrives without them, so it stays "[]" there.
let CUR_UNION="[]";
let SKETCH=null, LAYOUT_ID=null;   // the sketch that travels with graph.json, and the layout picked from it
// The DERIVED layer, if the builder shipped one. It rides NEXT to the graph, never inside it:
// `G.edges` stays what somebody signed. Reading it is a separate act, which is why it is a separate
// variable and every use of it is guarded.
let DERIVED=null;
// The contracted view: claims that relate became named edges, claims that assert became marks, and
// a file is a node only where it joins two. Off by default - a reader who opened the graph asked
// for what was signed, and this is a reading of it.
let CONTRACTED=null, VIEW="attested", GRAPH_PATH=null;
// The kit's loader owns the wasm: it reads runtime/wasm/manifest.json, brings its own wasm_exec.js
// and instantiates once, however many viewers on the page ask for it. The module still registers
// the same globals this file calls further down (plktBuildGraph, plktSha256*), which is why nothing
// below this line had to change when the viewer moved out of the protocol repo.
// the shared modules, without the wasm: enough to draw a graph someone else already built
async function loadModules(){
  NAV = await import("../../core/nav.js");        // the shared walks; load() awaits this before any render
  REG = await import("../../core/registry.js");   // reads BOTH store shapes - see the note there
  VS  = await import("../../core/viewstate.js");  // the URL is the state both viewers share
  HOST= await import("../../core/host.js");       // an embedding verticle can steer this view
  LAY = await import("../../core/layout.js");     // placement is declared, not hard-coded
  QS  = await import("../../core/questions.js"); // the named questions, shared with the compass
  GRP = await import("../../core/grouping.js");  // one grouping key, shared with the compass
  JOIN= await import("../../core/join.js");      // recorded + signed in one graph, by file hash
  SEL = await import("../../core/select.js");    // a portion of a store instead of the whole union
  PRF = await import("../../core/profile.js");   // what to show: a preset plus overrides, settable by a host
  ANNO= await import("../../core/annotate.js");  // a step's command, description and rationale
  SELM= await import("../../core/selection.js"); // several steps selected: set, read and translated
}
async function initWasm(){ if(_wasmReady) return;
  const { loadKton } = await import("../../runtime/kton.js");
  await loadModules();
  await loadKton();                 // profile "full" - see runtime/kton.js and wasm/profiles.json
  _wasmReady=true; }
function showErr(e){ $("#err").style.display="grid"; $("#err").textContent="Could not build the graph: "+(e&&e.message||e); }

/* ===== file preview: a file node is a window onto its bytes ===== */
// esc: HTML-escape any record-derived string before it goes into innerHTML. Record fields (by, participant,
// role, name, signer, text, value, …) are attacker-controlled - a `by` of `<img src=x onerror=…>` would
// otherwise execute. Escapes the 5 significant chars (element-text AND quoted-attribute contexts).
function esc(s){ return String(s==null?"":s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
const clipe=(s,n)=>esc(clip(s,n));                                    // clip THEN escape - for record-derived text
// ATTESTED_NAMES: the participant labels backed by a trusted-authority sec:controller binding. A name
// NOT in this set is a mere .pub key label, NOT an authenticated identity - so the green signature ring
// (which verifies the KEY) must not be read as vouching for the human NAME (screenshot-deception). idlabel
// renders an unattested identity with a visible "key label" marker so a screenshot cannot pass a
// key label off as "senior person approved". idmark carries the same marker as a compact suffix.
let ATTESTED_NAMES=new Set();
function isAttestedName(name){ return ATTESTED_NAMES.has(String(name==null?"":name)); }
function idlabel(name){ const n=esc(String(name==null?"":name).replace('pfed-',''));
  if(isAttestedName(name)) return `<span class="idok" title="identity attested by a trusted authority (signed sec:controller)">${n}</span>`;
  // "(self)": the holder vouches for its own key (cockpit's tier self) - not attested, but not a failure either
  if(/\s\(self\)$/.test(String(name||""))) return `<span class="idself" title="self-signed: the key holder vouches for this name; no authority has attested it">${esc(String(name).replace(/\s\(self\)$/,""))}<span class="idmark">self</span></span>`;
  return isAttestedName(name) ? `<span class="idok" title="identity attested by a trusted authority (signed sec:controller)">${n}</span>`
    : `<span class="idun" title="unattested: this is a key label, not an authenticated identity - the signature verifies the KEY, not this name">${n}<span class="idmark">key&nbsp;label</span></span>`; }
const safeUri=u=>/^(https?|ipfs):/i.test(String(u||""))?String(u):"";  // only http(s)/ipfs into an href; else inert text
// hash(12) -> [uri], read from FileRef.uri carried on inputs/outputs (plankton --located); merged with claim-based G.locators
// a relative locator names a place next to the registry that published it, so it resolves against that
// registry's URL (?union=...), not against this viewer: then it is an ordinary address, linked and previewed
const RECSRC={};
function locAbs(u,id){ const s=String(u||""); if(/^[a-z][a-z0-9+.-]*:/i.test(s)||!RECSRC[id]) return s; try{ return new URL(s,RECSRC[id]).href; }catch{ return s; } }
function buildLoc(unionStr){ LOC={};
  try{ for(const r of JSON.parse(unionStr)){ if(!r.envelope) continue;
    let p; try{ p=JSON.parse(new TextDecoder().decode(b64ToBytes(r.envelope.payload))); }catch{ continue; }
    const files=(p.subject||[]).concat((p.predicate&&p.predicate.inputs)||[]);
    for(const s of files){ const d=s.digest&&(s.digest.sha256||s.digest["sha-256"]);
      if(d&&s.uri){ const k=String(d).toLowerCase().slice(0,32); LOC[k]=[...new Set([...(LOC[k]||[]),...[].concat(s.uri).map(u=>locAbs(u,r.fotonId||r.claimId))])]; } }
  } }catch{}
}
function renderPreview(uri,bytes,partial){ const ext=(uri.split(/[?#]/)[0].split(".").pop()||"").toLowerCase();
  if(["svg","png","jpg","jpeg","gif","webp","avif"].includes(ext)){
    if(partial) return `<div class="kv">image file - a partial slice can't be rendered; use ↗ to open the whole file.</div>`;
    const type=ext==="svg"?"image/svg+xml":ext==="jpg"?"image/jpeg":"image/"+ext;
    return `<img class="previmg" src="${URL.createObjectURL(new Blob([bytes],{type}))}">`; }
  const text=new TextDecoder().decode(bytes);
  if(ext==="csv"||ext==="tsv"){ const sep=ext==="tsv"?"\t":",";
    const rows=text.replace(/\s+$/,"").split(/\r?\n/).slice(0,25).map(r=>r.split(sep));
    return `<div class="prevscroll"><table class="prevt">${rows.map((r,i)=>"<tr>"+r.map(c=>`<${i?"td":"th"}>${esc(c)}</${i?"td":"th"}>`).join("")+"</tr>").join("")}</table></div>`; }
  return `<pre class="prevpre">${esc(text.slice(0,6000))}${text.length>6000?"\n… (truncated)":""}</pre>`;
}
const PREVIEW_CAP=512*1024;        // bytes we fetch/show for a big file's partial preview
const VERIFY_CAP =12*1024*1024;    // fetch-and-hash the WHOLE file (verifiable) only up to this size
function humanSize(n){ if(n==null||!isFinite(n)) return "unknown size"; const u=["B","KB","MB","GB","TB"]; let i=0; while(n>=1024&&i<u.length-1){n/=1024;i++;} return (i?n.toFixed(1):n)+" "+u[i]; }
async function headSize(uri){ try{ const r=await fetch(uri,{method:"HEAD"}); if(!r.ok) return null; const n=+r.headers.get("content-length"); return isFinite(n)&&n>0?n:null; }catch{ return null; } }
// read at most `cap` bytes off the response, then abort the stream - so a 10 GB file never lands in memory.
async function fetchCapped(uri,cap){ const ac=new AbortController(); const r=await fetch(uri,{signal:ac.signal}); if(!r.ok) throw new Error("HTTP "+r.status);
  if(!r.body){ const all=new Uint8Array(await r.arrayBuffer()); return {bytes:all.length>cap?all.slice(0,cap):all, truncated:all.length>cap}; }
  const rd=r.body.getReader(); const chunks=[]; let total=0, truncated=false;
  for(;;){ const {done,value}=await rd.read(); if(done) break; chunks.push(value); total+=value.length; if(total>=cap){ truncated=true; try{ac.abort();}catch{} break; } }
  const out=new Uint8Array(Math.min(total,cap)); let off=0; for(const c of chunks){ if(off>=out.length) break; const take=Math.min(c.length,out.length-off); out.set(c.subarray(0,take),off); off+=take; }
  return {bytes:out, truncated};
}
// streamHashAndPreview: verify a file of ANY size by streaming its bytes through the WASM's incremental
// sha256 (O(1) memory) while capturing only the first `previewCap` bytes to show. One pass, one download.
// Returns null if the WASM streaming hash isn't available (falls back to the capped path). This is what
// turns a big file's "partial preview, unhashable" into a real ✓ over the WHOLE file.
async function streamHashAndPreview(uri,previewCap){
  if(typeof plktSha256Init!=="function") return null;
  plktSha256Init();
  const r=await fetch(uri); if(!r.ok) throw new Error("HTTP "+r.status);
  if(!r.body){ const all=new Uint8Array(await r.arrayBuffer()); plktSha256Update(all); return {hash:plktSha256Final(), preview:all.slice(0,previewCap)}; }
  const rd=r.body.getReader(); const pv=[]; let pvLen=0;
  for(;;){ const {done,value}=await rd.read(); if(done) break;
    plktSha256Update(value);
    if(pvLen<previewCap){ const take=Math.min(value.length,previewCap-pvLen); pv.push(value.subarray(0,take)); pvLen+=take; } }
  const preview=new Uint8Array(pvLen); let off=0; for(const p of pv){ preview.set(p,off); off+=p.length; }
  return {hash:plktSha256Final(), preview};
}
async function previewFile(nodeId,uri,box){ box=box||$("#preview"); if(!box) return;
  if(box.dataset.open){ box.innerHTML=""; delete box.dataset.open; return; }   // click again = close
  box.dataset.open="1"; box.innerHTML='<div class="kv">fetching…</div>';
  const X='<span class="pvx" title="close preview">×</span>';
  const partialHead=(cap,size)=>`<div class="pvhead"><span class="badge vpart">◐ partial preview · first ${humanSize(cap)}${size!=null?" of "+humanSize(size):""}</span>${X}</div>`
    +`<div class="kv">too large to hash in the browser - the recorded hash still covers the whole file. verify with <code>plankton verify</code> or re-download the bytes.</div>`;
  try{
    const size=await headSize(uri);
    if(size!=null && size>VERIFY_CAP){                                   // known-large: STREAM-verify the whole file (O(1) memory)
      const sv=await streamHashAndPreview(uri,PREVIEW_CAP);
      if(sv){ const match=sv.hash.startsWith(String(nodeId).toLowerCase());
        box.innerHTML=`<div class="pvhead"><span class="badge ${match?"vok":"vno"}">${match?"✓ whole file streamed - matches the recorded hash":"✗ streamed bytes do not match the hash"}</span>${X}</div>`
          +`<div class="kv">preview shows the first ${humanSize(PREVIEW_CAP)}; the ✓ covers the whole ${humanSize(size)}, streamed without loading it into memory.</div>`+renderPreview(uri,sv.preview,true);
      }else{ const {bytes}=await fetchCapped(uri,PREVIEW_CAP);            // no WASM streaming: fall back to partial preview
        box.innerHTML=partialHead(PREVIEW_CAP,size)+renderPreview(uri,bytes,true); }
    }else{                                                               // small / unknown: fetch up to the verify cap
      const {bytes,truncated}=await fetchCapped(uri,VERIFY_CAP);
      if(truncated){ box.innerHTML=partialHead(VERIFY_CAP,size)+renderPreview(uri,bytes,true); }   // no Content-Length but still huge
      else{ const match=(await sha256hex(bytes)).startsWith(String(nodeId).toLowerCase());          // whole file present -> real content-verify
        box.innerHTML=`<div class="pvhead"><span class="badge ${match?"vok":"vno"}">${match?"✓ content matches the recorded hash":"✗ bytes do not match the hash"}</span>${X}</div>`+renderPreview(uri,bytes); }
    }
    box.querySelector(".pvx").onclick=ev=>{ ev.stopPropagation(); box.innerHTML=""; delete box.dataset.open; };
  }catch(e){ delete box.dataset.open; box.innerHTML='<div class="kv" style="color:var(--bad)">could not fetch the bytes: '+esc(e.message||String(e))+'</div>'; }
}
// the graph WASM drops a claim's object - the actual assertion ("final", "L0", the review sentence).
// Recover it from the raw payloads and fold it into the node so it shows on the label and in the panel.
let CLAIMS=[];   // structured claim facets for the overview claims-browser: {id,pred,value,part,by,subj}
// The claim's human text/label now comes from the graph WASM itself (graph.go renders predicate.object
// into node.text), so this no longer mutates nodes - it only reads the union for clean, STRUCTURED facet
// values (a scalar value/level per claim), which the rendered text can't give as reliably.
function buildClaimFacets(unionStr){ CLAIMS=[]; try{ const recs=JSON.parse(unionStr);
  for(const r of recs){ const id=r&&r.claimId; if(!id||!r.envelope||!r.envelope.payload) continue;
    let p; try{ p=JSON.parse(new TextDecoder().decode(b64ToBytes(r.envelope.payload))); }catch{ continue; }
    const pd=p.predicate||{}, obj=pd.object;
    const nid=String(id).replace(/^sha256:/,"").toLowerCase().slice(0,32); const n=G.byId.get(nid); if(!n) continue;
    const pick=(obj&&typeof obj==="object")?(obj.value!=null?obj.value:(obj.level!=null?obj.level:null)):obj;
    const s0=(p.subject||[])[0]||{}; const sh=(s0.digest&&s0.digest.sha256)||(typeof s0.hash==="string"?s0.hash.replace(/^sha256:/,""):"");
    CLAIMS.push({ id:nid, pred:n.role, value:(pick!=null&&typeof pick!=="object")?String(pick):"", part:n.participant||"", by:pd.by||"", subj:String(sh||"").toLowerCase().slice(0,32) });
  } }catch{} }
function renderGraph(union,keys,names,recorded){ try{ NAMES_ALL=JSON.parse(names||"{}"); }catch{ NAMES_ALL={}; }
  buildG(union,keys,names,recorded);
  afterGraph();
}
function buildG(union,keys,names,recorded){
  LAST_BUILD=[union,keys,names,recorded];
  G=JSON.parse(plktBuildGraph(union,ringWithPackages(keys),names)); if(G.error) throw new Error(G.error);
  markUntrusted(G, keys);
  if(G.locators){ const base=Object.values(RECSRC)[0]; for(const k in G.locators) G.locators[k]=[...new Set([].concat(G.locators[k]).map(u=>/^[a-z][a-z0-9+.-]*:/i.test(u)||!base?u:(()=>{ try{ return new URL(u,base).href; }catch{ return u; } })()))]; }
  // a recorder's graph next to the union: join by output hash. Only labels move across; the
  // verdicts are the wasm's, and a step only the recorder saw stays unverified (core/join.js).
  if(recorded) G=JOIN.joinGraphs(G, recorded);
  if(recorded && !recordedShown()) G=dropRecorded(G);
  buildLoc(union); buildClaimFacets(union); CUR_UNION=union;
}
// Steps only the recorder saw: graph.recorded "hide" leaves them out (the pmx view - a project's
// lineage, not every shell command), with a switch in the stats line to bring them back. A file only
// such steps touched goes with them: ~/.bashrc is not part of the analysis.
let SHOW_RECORDED=null, LAST_BUILD=null;
function recordedShown(){ return SHOW_RECORDED!=null ? SHOW_RECORDED : prof().graph.recorded!=="hide"; }
function dropRecorded(g){
  const gone=new Set(g.nodes.filter(n=>n.origin==="recorded").map(n=>n.id)); if(!gone.size) return g;
  const edges=g.edges.filter(e=>!gone.has(e.from)&&!gone.has(e.to));
  const touched=new Set(), linked=new Set(edges.flatMap(e=>[e.from,e.to]));
  for(const e of g.edges) if(gone.has(e.from)||gone.has(e.to)){ touched.add(e.from); touched.add(e.to); }
  const nodes=g.nodes.filter(n=>!gone.has(n.id)&&!(touched.has(n.id)&&!linked.has(n.id)&&n.type!=="foton"&&n.type!=="claim"));
  const summary=JOIN.summarize(nodes,edges); summary.origins.hidden=gone.size;
  return {...g, nodes, edges, summary};
}
function toggleRecorded(){ SHOW_RECORDED=!recordedShown(); if(!LAST_BUILD) return;
  const c=ex&&ex.center; buildG(...LAST_BUILD); index(); showStats();
  if(c&&G.byId.has(c)) enterExplorer(c,(ex&&ex.mode)||"both",false); else startView(); }
// Where a view without ?focus= begins. graph.start "lineage": on the step most of the project leads
// to (the most ancestors - a report, the deliverables), its ancestry drawn; otherwise the overview.
function startView(){
  if(prof().graph.start!=="lineage") return showOverview(false);
  const up={}; for(const e of G.edges) if(e.kind==="builds-on") (up[e.from]??=[]).push(e.to);
  let best=null, most=-1;
  for(const n of G.nodes){ if(n.type!=="foton") continue; const seen=new Set([n.id]), st=[n.id];
    while(st.length){ for(const t of up[st.pop()]||[]) if(!seen.has(t)){ seen.add(t); st.push(t); } }
    const k=seen.size+(n.verified?0.5:0); if(k>most){ most=k; best=n.id; } }
  if(best) enterExplorer(best,"both",false); else showOverview(false);
}
// A recorder's graph.json (?graph=), or null. An unreachable recorder costs the labels, not the
// signed graph.
async function fetchRecorded(url){ if(!url) return null;
  try{ const r=await fetch(url); if(!r.ok) throw new Error("HTTP "+r.status); return (await r.json()).graph||null; }
  catch(e){ console.warn("recorded graph not joined:", e); return null; } }

/* ===== a portion of a store (?select=): grow it, rebuild, stay where the reader was ===== */
// The rebuild goes through the same wasm as a union - every record the store chose is verified here.
function storeRebuild(center, note=""){
  const union=JSON.stringify(STORE.records());
  buildG(union, JSON.stringify(STORE.keys), JSON.stringify(STORE.names), RECORDED);
  index(); showStats();
  if(center && G.byId.has(center)) enterExplorer(center, (ex&&ex.mode)||"both", false, note); else showOverview(false);
}
function storeFail(e){ notice("store: "+(e&&e.message||e)); }
// Say what went wrong where the reader looks: in the stats bar, or - when the profile hides the
// header (an editor's view) - in a line over the graph. A focus that did not land must not just
// leave an empty overview behind.
function notice(msg){ const st=$("#stats");
  if(st && prof().chrome.header && prof().chrome.stats){ st.insertAdjacentHTML("beforeend", `<span class="bad">${esc(msg)}</span>`); return; }
  let el=$("#notice"); if(!el){ el=document.createElement("div"); el.id="notice"; document.body.appendChild(el); }
  el.innerHTML=`<span>${esc(msg)}</span><span class="nx" title="dismiss">×</span>`; el.style.display="flex";
  el.querySelector(".nx").onclick=()=>{ el.style.display="none"; }; }
// Expand a node: what it was built from and what was built on it, one hop each way, by the FULL
// hash its record declares (graph ids are shortened; the store answers to the real hash).
async function storeExpand(id){
  try{ const rec=STORE.recordFor(id);
    const hashes=(rec&&rec.fotonId)?SEL.outputsOf(rec):[STORE.fullHash(id)].filter(Boolean);
    if(!hashes.length) return storeFail(new Error("no content hash known for "+id));
    let added=0; for(const h of hashes){ added+=await STORE.ask({q:"pedigree",hash:h,hops:1}); added+=await STORE.ask({q:"uses",hash:h,hops:1}); }
    storeRebuild(id, added?`expanded from the store: +${added} record${added===1?"":"s"}`:"the store has nothing more around this one");
  }catch(e){ storeFail(e); } }
// A host re-aims the viewer at a file the portion does not hold yet (kton:set {focus}): fetch its
// pedigree and uses, then centre on it - no reload, and the host hears where it landed.
async function storeFocus(hash, mode){
  try{ const h=String(hash).replace(/^sha256:/,"");
    await STORE.ask({q:"pedigree", hash:h, hops:3}); await STORE.ask({q:"uses", hash:h, hops:1});
    const union=JSON.stringify(STORE.records());
    buildG(union, JSON.stringify(STORE.keys), JSON.stringify(STORE.names), RECORDED); index(); showStats();
    const id=resolveFocus(union, hash);
    if(id) enterExplorer(id, mode||"both");
    else { storeFail(new Error(`nothing in the store produced or used ${hash}`)); if(BRIDGE) BRIDGE.refused([`focus=${hash}`], "the store holds nothing for this file"); }
    if(BRIDGE) BRIDGE.state(viewState());
  }catch(e){ storeFail(e); if(BRIDGE) BRIDGE.refused([`focus=${hash}`], String(e&&e.message||e)); } }
async function storeMore(){
  try{ const added=await STORE.loadMore(); storeRebuild(ex&&ex.center, `+${added} record${added===1?"":"s"} from the cut pages`); }
  catch(e){ storeFail(e); } }
// A graph the HOST already built (the verticle path): no wasm here, and no raw union, so locators
// come from the graph itself (plktBuildGraph puts them there) and the claim-filter panel stays
// empty - it needs the union to read predicate values, which the webview never receives. A named
// question over the claim axis answers the same thing; the panel is a convenience, not the axis.
function renderPrebuilt(graph){
  G=graph; if(G.error) throw new Error(G.error); CUR_UNION="[]";
  LOC=G.locators||{}; CLAIMS=[];
  afterGraph();
}
function afterGraph(){
  $("#err").style.display="none";
  applyChrome();
  if(prof().problems.length) console.warn("view:", prof().problems.join("; "));
  document.body.classList.toggle("embedded", EMBEDDED());
  // <html data-theme="dark"> is a decision this page makes for the open web. Inside the editor the
  // reader has already made it, and the host says which way - so drop the lock and follow.
  if(EMBEDDED()) document.documentElement.removeAttribute("data-theme");
  index(); showStats(); startView(); history.replaceState({view:'overview'},"");
  { const sel=new URLSearchParams(location.search).get("sel");          // a selection a reload or a link carries
    if(sel && prof().select.multi){ SELSET=new Set(SELM.resolveSelection(G, sel.split(","), null).ids); renderSelBar(); } }
  const st0=VS?VS.readState():{};
  if(st0.facets && Object.keys(st0.facets).length) applyFacets(st0.facets);   // a view handed over by the compass
  // The embedding bridge (postMessage) is for the BROWSER embed case; a verticle steers via
  // improveHost.onLaunch instead and never loads HOST. So this only runs when HOST is present.
  if(V3){ const st=V3.pendingState; V3.pendingState=null; if(st) V3.answerSet(applyHostState(st)); else BRIDGE.state(viewState()); }
  else if(HOST){
  BRIDGE = HOST.hostBridge({
    capabilities:{ viewer:"graph", state:["union","keys","names","focus","f","mode","layout","view"],
                   facets:["participant","type"], modes:["nbr","anc","desc","both","lin","imp"],
                   views:Object.keys(PRF.PRESETS) },
    current: ()=>viewState(),
    apply: (st)=>applyHostState(st),
  });
  BRIDGE.ready();
  }
  window.onpopstate=e=>{ const s=e.state||{view:'overview'}; if(s.view==='explorer'&&G.byId.has(s.center)) enterExplorer(s.center,s.mode,false); else showOverview(false); };
  document.addEventListener('keydown',e=>{ if(e.key==='Escape'&&$("#explorer").style.display!=='none') showOverview(); });
  document.addEventListener('click',e=>{ if(e.target&&e.target.id==="togrec"){ e.preventDefault(); toggleRecorded(); } });
}
// A host's view state (v2 kton:set {state}, v3 kton:set {state}): applied, and what could not be is named.
function applyHostState(st){
      const refused=[];
      // a view: a preset name, or a change to the current one ({panel:{hashes:true}}, actions, …)
      if(st.view){ PROF=PRF.adjustProfile(prof(), st.view); refused.push(...PROF.problems.map(p=>"view: "+p)); applyChrome();
        if(filt){ filt.types=typesShown(); filt.kinds=kindsHidden(); }
        if(ex&&ex.started){ drawControls(); layoutAndRun(); fitView(); redraw(); if(ex.sel && $("#panel").classList.contains("open")) openNode(ex.sel); } }
      // the multi-selection, replaced as a whole; [] clears. Only with select.multi on - a host
      // must turn it on deliberately, so a view that never asked for it is not changed by one
      if(st.selection!==undefined){
        if(!prof().select.multi) refused.push("selection: multi-select is off (view.select.multi)");
        else { const r=SELM.resolveSelection(G, st.selection, h=>resolveFocus(CUR_UNION,h)); setSelection(new Set(r.ids), false);
          refused.push(...r.missed.map(m=>"selection: "+m)); } }
      if(st.facets) refused.push(...applyFacets(st.facets));
      if(st.layout) LAYOUT_ID=st.layout;
      const mode = st.mode || (ex&&ex.mode) || "both";
      if(st.focus){ const id=resolveFocus(CUR_UNION, st.focus);
        if(id) enterExplorer(id, mode);
        else if(STORE) storeFocus(st.focus, mode);          // not drawn yet: ask the store, it reports when it lands
        else refused.push(`focus=${st.focus}`); }
      else if(ex){ ex.mode=mode; drawControls(); layoutAndRun(); }
      return { refused, why:"this corpus does not carry it" };
}
// the reader's own connected planktons - the SAME list the lens consults (localStorage "kton-planktons",
// a JSON array or comma list of union.json URLs); unioned onto the delivered registry.
// Only URL strings count: an entry another page stored as an object, or null, used to be listed and
// fetched as "undefined". A relative path is taken relative to the page the reader typed it on.
function extraSources(){ let a=[]; try{ const v=localStorage.getItem("kton-planktons"); if(!v) return [];
  try{ a=JSON.parse(v); }catch{ a=v.split(",").map(s=>s.trim()); } }catch{ return []; }
  const base=document.referrer||location.href;
  return (Array.isArray(a)?a:[a]).map(x=>typeof x==="string"?x:(x&&(x.union||x.url))||"").map(s=>String(s).trim()).filter(Boolean)
    .map(s=>{ try{ return new URL(s, base).href; }catch{ return s; } }); }
const SRCSTAT={};   // url -> "n records" | "not reachable" | "switched off", shown in settings
// every source of the last load, in order: {url, delivered} - delivered = named by the page (?union=),
// otherwise added by the reader (extraSources). Settings lists them all with a switch each.
let SOURCES=[];
// sources the reader switched off in settings: left out of the next splice. Kept for this page only
// (not stored), so a reload - or the next slide - shows everything that was delivered again.
const SRC_OFF=new Set();
function regUrls(u){ const b=u.replace(/[^/]*$/,""); return {union:u, keys:b+"keys.json", names:b+"names.json", attested:b+"attested.json"}; }
async function fetchSrc(reg){ const recs=JSON.parse(await (await fetch(reg.union)).text());
  let keys={},names={},attested=[]; try{ keys=JSON.parse(await (await fetch(reg.keys)).text()); }catch{}
  try{ names=JSON.parse(await (await fetch(reg.names)).text()); }catch{}
  try{ attested=JSON.parse(await (await fetch(reg.attested)).text()); }catch{}  // absent -> nothing attested (safe)
  // ?packages=<url>: cockpit v0.5.3's admitted packages' build keys {keyid: {package, dir}}; named, never guessed
  return {recs,keys,names,attested}; }
// Read a graph.json the host built and draw it. One function for both the first load and a
// re-aim at another corpus - two paths that read the same file differently is how a viewer ends up
// showing one corpus and reporting another.
async function loadGraphInto(path){
  const gt=await window.improveHost.callOp("resource:read",{path});
  const doc=JSON.parse(typeof gt==="string"?gt:(gt&&gt.content)||"{}");
  SKETCH = doc.sketch || null;          // it declares the layouts, same as it declares groupings
  DERIVED = doc.derived || null;
  CONTRACTED = doc.contracted || null;
  if(!CONTRACTED) VIEW="attested";      // a corpus without the reading cannot be shown in it
  GRAPH_PATH = path;
  ex = null;                            // another corpus: the old explorer state means nothing here
  renderPrebuilt(doc.graph);
}
async function load(){
  try{
    // Inside a verticle: the host built the graph; the webview draws it. No fetch, no wasm here.
    if(window.improveHost){
      // the shared modules the render path uses (NAV for the walks, QS for countFor on a node
      // panel, GRP for grouping, VS for state). The browser loads them in initWasm; a verticle skips
      // initWasm, so load them here before drawing - otherwise the first node panel throws on QS.
      NAV = await import("./nav.js");
      QS  = await import("./questions.js");
      GRP = await import("./grouping.js");
      LAY = await import("./layout.js");
      VS  = await import("./viewstate.js");
      // the graph is a file the host built (resource:read), not kton:graph - so this viewer shows
      // data whether or not the kton-ops mcp-tool is mounted
      const gp=(window.improveHost.launch&&window.improveHost.launch.graph)||"/pmx/graph.json";
      await loadGraphInto(gp);
      if(window.improveHost.onLaunch) window.improveHost.onLaunch(async (ctx)=>{
        if(!ctx) return;
        // A handover may name ANOTHER corpus. This used to be read only at first load, so an open
        // panel could be re-aimed at a node but never at a different graph: kton:show reported
        // success, the host delivered the context, and nothing on screen changed. Re-read it.
        // Re-read when the path changed - OR when the focus names a node this graph does not have.
        // A corpus is rebuilt under the SAME path all the time, and content-addressed ids change
        // with it; guarding on the path alone left the panel showing yesterday's graph while every
        // handover reported success.
        const wantsUnknown = ctx.focus && G && G.byId && !G.byId.has(String(ctx.focus));
        if(ctx.graph && (ctx.graph!==GRAPH_PATH || wantsUnknown)) await loadGraphInto(ctx.graph);
        if(ctx.layout) LAYOUT_ID=ctx.layout;
        // facets arrive in the compass's string form ("type:foton,entity;view:contracted")
        if(ctx.f && filt) applyFacets(typeof ctx.f==="string" ? Object.fromEntries((ctx.f||"").split(";").filter(Boolean).map(p=>{const i=p.indexOf(":");return i<0?[p,[]]:[p.slice(0,i),p.slice(i+1).split(",").filter(Boolean)];})) : ctx.f);
        const id=ctx.focus && resolveFocus(JSON.stringify([]), ctx.focus);
        if(id) enterExplorer(id, ctx.mode||"both"); else if(ex&&(ctx.layout||ctx.f)){ layoutAndRun(); }
      });
      return;
    }
    const qs=new URLSearchParams(location.search);
    // In a frame with no data address: a block (C1.5 v3). It says ready before anything else and loads
    // the wasm only for a graph@1 answer that needs it (records), so a host's CSP problem is reported.
    if(window.parent!==window && !qs.get("union") && !qs.get("graph") && !qs.get("select")){ await bootV3(); return; }
    // ?graph=<url>: a graph.json some host already built (the same document a verticle reads), drawn
    // in the browser. No wasm and no union: whoever built it vouches for it, as in the verticle path.
    // Used by recorders that show observations in kton's own graph form before anything is signed.
    // With ?union= as well, the two are joined instead (below): recorded and signed in one graph.
    // (not with ?select=: there the recorder's graph is joined to the store's portion, below)
    if(qs.get("graph") && !qs.get("union") && !qs.get("select")){
      await loadModules();
      const r=await fetch(qs.get("graph")); if(!r.ok) throw new Error("graph: HTTP "+r.status);
      const doc=await r.json();
      SKETCH=doc.sketch||null; DERIVED=doc.derived||null; CONTRACTED=doc.contracted||null;
      if(!CONTRACTED) VIEW="attested";
      GRAPH_PATH=qs.get("graph");
      renderPrebuilt(doc.graph);
      const focus=qs.get("focus"), id=focus&&resolveFocus("[]",focus);
      if(id) enterExplorer(id, qs.get("mode")||"both");
      else if(focus) notice(`no step in this graph has the file ${focus}`);
      return;
    }
    await initWasm();
    // ?select=<endpoint>&focus=<hash>: a portion of a store - the pedigree of one file and what was
    // built on it - grown by expanding nodes. `from` keeps the starting hash: the explorer rewrites
    // `focus` to the centre node, and a reload must ask the store the same question again.
    if(qs.get("select")){
      const start=qs.get("from")||qs.get("focus");
      if(!start) throw new Error("?select= needs ?focus=<content hash> - the file to start from");
      if(!qs.get("from")){ const u=new URL(location.href); u.searchParams.set("from",start); history.replaceState(history.state,"",u); }
      STORE=new SEL.Portion(qs.get("select"));
      RECORDED=await fetchRecorded(qs.get("graph"));
      const h=start.replace(/^sha256:/,""), hops=Math.min(+qs.get("hops")||3, 32);
      await STORE.ask({q:"pedigree", hash:h, hops}); await STORE.ask({q:"uses", hash:h, hops:1});
      const union=JSON.stringify(STORE.records());
      renderGraph(union, JSON.stringify(STORE.keys), JSON.stringify(STORE.names), RECORDED);
      const id=resolveFocus(union, qs.get("focus")||start);
      if(id) enterExplorer(id, qs.get("mode")||"both");
      else storeFail(new Error(`nothing in the store produced or used ${start}`));
      return;
    }
    if(!qs.get("union")){ showDropUI(); return; }          // no registry named -> let anyone drop their own records
    // ?union= may repeat (or hold a comma list): several registries, spliced by hash. keys/names
    // override the first one's siblings only.
    const unions=qs.getAll("union").flatMap(u=>u.split(",")).map(u=>u.trim()).filter(Boolean);
    // the admitted packages' build keys: named by ?packages=, or beside cockpit show's own /data/union.json
    { const pu=qs.get("packages") || (/\/data\/union\.json$/.test(new URL(unions[0]||"",location.href).pathname) ? unions[0].replace(/union\.json$/,"packages.json") : null);
      if(pu){ try{ const r=await fetch(pu); if(r.ok) await usePackages(await r.json()); }catch{} } }
    const prim=regUrls(unions[0]);                          // sibling keys.json/names.json, unless overridden
    if(qs.get("keys")) prim.keys=qs.get("keys"); if(qs.get("names")) prim.names=qs.get("names");
    const regs=[prim, ...unions.slice(1).map(regUrls), ...extraSources().filter(u=>!unions.includes(u)).map(regUrls)];   // delivered + the reader's own connected planktons
    const seen=new Set(), recs=[], keys={}, names={}; const attKids=new Set(); let total=0;
    for(const k in SRCSTAT) delete SRCSTAT[k]; for(const k in RECSRC) delete RECSRC[k];   // a re-splice starts clean
    SOURCES=regs.map((r,i)=>({url:r.union, delivered:i<unions.length}));
    for(const reg of regs){ if(SRC_OFF.has(reg.union)){ SRCSTAT[reg.union]="switched off"; continue; } try{ const s=await fetchSrc(reg); SRCSTAT[reg.union]=s.recs.length+" records";   // union by content hash; dedup on foton/claim id
      for(const r of s.recs){ const id=r&&(r.fotonId||r.claimId); if(id&&!seen.has(id)){ seen.add(id); total++; RECSRC[id]=reg.union; if(recs.length<RECORD_CAP) recs.push(r); } }
      Object.assign(keys,s.keys); Object.assign(names,s.names); for(const k of (s.attested||[])) attKids.add(k);
    }catch(e){ SRCSTAT[reg.union]="not reachable"; /* skipped, not fatal - but said in settings */ } }
    // an identity label is authenticated only if its keyid is in some source's attested.json (a
    // trusted-authority sec:controller binding); everything else is a bare key label.
    ATTESTED_NAMES=new Set([...attKids].map(k=>names[k]).filter(Boolean));
    const truncated=total>recs.length;
    const union=JSON.stringify(recs);
    // ?graph= next to ?union=: the recorder's view of the same work, joined by file hash. An
    // unreachable recorder costs the labels, not the signed graph.
    RECORDED=await fetchRecorded(qs.get("graph"));
    renderGraph(union, JSON.stringify(keys), JSON.stringify(names), RECORDED);
    // Bound the graph BUILD, not just the render: above RECORD_CAP records we cap the overview and tell
    // the reader to open ONE pedigree (the whole-registry force graph is unreadable at that size anyway).
    // (The fetch still downloads the whole static union.json; true streaming needs server-side paging.)
    if(truncated){ const st=$("#stats"); if(st) st.insertAdjacentHTML("beforeend",
      `<span class="bad">large registry: ${total.toLocaleString()} records, showing ${RECORD_CAP.toLocaleString()} - open one output's pedigree with <code>?focus=&lt;hash&gt;</code></span>`); }
    // ?focus=sha256:<hex> - the "lens" entry point: open straight onto one output's provenance
    const focus=qs.get("focus");
    if(focus){ const id=resolveFocus(union,focus);
      if(id) enterExplorer(id,"both");                       // lineage (inputs) + attestations on it
      else showErr("that output hash is not in the connected registries"+(truncated?" (or beyond the "+RECORD_CAP.toLocaleString()+"-record cap - narrow the source)":"")+": "+focus); }
  }catch(e){ showErr(e); }
}
// settings: auto-preview toggle + every kton source of this picture: the registries the page
// delivered (?union=) and the reader's own, each with a switch (off = left out of the splice)
function srcName(u){ const m=String(u).match(/([^/]+)\/[^/]*$/); return m?m[1]:String(u); }
function openSettings(){ const el=$("#settings"); const srcs=extraSources();
  const all=SOURCES.length?SOURCES:srcs.map(u=>({url:u,delivered:false}));
  const dlv=all.filter(s=>s.delivered).length, on=all.filter(s=>!SRC_OFF.has(s.url)).length;
  const row=s=>{ const off=SRC_OFF.has(s.url), i=srcs.indexOf(s.url);
    return `<div class="srcrow${off?" srcoff":""}"><label class="srcsw" title="${off?"switch on: splice it back in":"switch off: leave it out of the graph"}"><input type="checkbox" data-src="${esc(s.url)}" ${off?"":"checked"} ${!off&&on<=1?"disabled":""}></label>`
      +`<span class="srcmain"><b>${esc(srcName(s.url))}</b> <span class="srctag ${s.delivered?"dlv":"own"}">${s.delivered?"delivered":"added by you"}</span><br><code title="${esc(s.url)}">${esc(s.url.length>46?"…"+s.url.slice(-45):s.url)}</code></span>`
      +`<span class="stmuted srcst">${esc(SRCSTAT[s.url]||"")}</span>`+(i>=0&&!s.delivered?`<span class="srcx" data-i="${i}">remove</span>`:``)+`</div>`; };
  el.innerHTML=`<div class="sthead">settings <span class="stx" title="close">×</span></div>
    <label class="strow"><input type="checkbox" id="autoprev" ${autoPreview?"checked":""}> auto-preview a file's bytes when you open its node</label>
    <div class="stlbl">kton sources${all.length>1?` <span class="stmuted">· ${on} of ${all.length} on</span>`:``}</div>
    <div id="srclist">${all.length?all.map(row).join(""):'<div class="stmuted">none</div>'}</div>
    <div class="stmuted">${dlv>1?`${dlv} registries delivered with this page`:dlv?`one registry delivered with this page`:``}${srcs.length?` + ${srcs.length} added by you`:``}. Switch one off to see the graph without it; a reload switches everything back on.</div>
    <div class="stlbl">add a source</div>
    <div class="srcadd"><input id="srcurl" placeholder="https://…/union.json"><button id="srcaddbtn">add</button></div>
    <div class="stmuted">Sources union in by content hash, so you may see more (or less) than the delivered registries. Added sources are stored in this browser only.</div>`;
  el.style.display="block";
  $("#autoprev").onchange=e=>{ autoPreview=e.target.checked; localStorage.setItem("kton-autopreview", autoPreview?"1":""); };
  el.querySelector(".stx").onclick=()=>el.style.display="none";
  el.querySelectorAll("input[data-src]").forEach(x=>x.onchange=()=>{ const u=x.dataset.src; if(x.checked) SRC_OFF.delete(u); else SRC_OFF.add(u); load().then(openSettings); });
  el.querySelectorAll(".srcx").forEach(x=>x.onclick=()=>{ const a=extraSources(); a.splice(+x.dataset.i,1); localStorage.setItem("kton-planktons",JSON.stringify(a)); load().then(openSettings); });
  $("#srcaddbtn").onclick=()=>{ let u=($("#srcurl").value||"").trim(); if(!u) return; try{ u=new URL(u, document.referrer||location.href).href; }catch{} const a=extraSources(); if(!a.includes(u)) a.push(u); localStorage.setItem("kton-planktons",JSON.stringify(a)); load().then(openSettings); };
}
// map an output hash (or a raw node id) to the graph node to centre on.
// graph node ids are RAW hex (no "sha256:" prefix), so normalise both sides before comparing.
function resolveFocus(union, focus){
  const strip=s=>String(s||"").replace(/^sha256:/,"").toLowerCase();
  const hx=strip(focus);
  // graph node ids are 32 hex, so match a full hash by prefix
  const nodeIdFor=full=>{ const h=strip(full); if(G.byId.has(h)) return h;
    for(const n of G.nodes){ const nid=strip(n.id); if(nid.length>=6 && (h.startsWith(nid)||nid.startsWith(h))) return n.id; }
    return null; };
  try{
    for(const r of JSON.parse(union)){                       // 1. the foton whose OUTPUT is this hash
      // only STEPS produce files: a claim's subject is the step it is about, so scanning claims here
      // turned a focus on a step id into a focus on some claim about that step
      const id=r.fotonId; if(!id||!r.envelope) continue;
      let p; try{ p=JSON.parse(new TextDecoder().decode(b64ToBytes(r.envelope.payload))); }catch{ continue; }
      for(const s of (p.subject||[]))
        if(strip(s.digest&&(s.digest.sha256||s.digest["sha-256"]))===hx){ const nid=nodeIdFor(id); if(nid) return nid; }
    }
  }catch{}
  for(const n of G.nodes) if(n.type==="foton")              // 1b. a step by its output - a recorder's step has a
    for(const o of (n.outputs||[])){ const oh=strip(o.hash); //     synthetic id and no record, its output is the file
      if(oh.length>=6 && (hx.startsWith(oh)||oh.startsWith(hx))) return n.id; }
  return nodeIdFor(focus);                                   // 2. a node addressed directly by this hash
}

/* ===== load your own records: build the union in the browser, exactly like a multi-source read ===== */
function hexToBytes(h){ const a=new Uint8Array(h.length/2); for(let i=0;i<a.length;i++) a[i]=parseInt(h.substr(i*2,2),16); return a; }
async function sha256hex(bytes){ const d=await crypto.subtle.digest("SHA-256",bytes); return [...new Uint8Array(d)].map(b=>b.toString(16).padStart(2,"0")).join(""); }
const b64ToBytes=s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0));
// verifyDsse: does env's Ed25519 signature verify under the raw-hex Ed25519 pubkey? Reconstructs the
// DSSE PAE ("DSSEv1 <lenType> <type> <lenPayload> <payload>", byte lengths) and checks it with WebCrypto.
// Used to AUTHENTICATE a sec:controller binding: an attacker can relabel a binding's claimed keyid to a
// trusted authority, but not forge that authority's SIGNATURE - so a relabelled "attested senior
// identity" fails here. Returns false on any error / unsupported engine (fail-safe: no false attest).
async function verifyDsse(env, pubHex){
  try{
    if(!(env&&env.payload&&env.signatures&&env.signatures[0]&&env.signatures[0].sig)) return false;
    const payload=b64ToBytes(env.payload), type=new TextEncoder().encode(env.payloadType||"");
    const head=new TextEncoder().encode("DSSEv1 "+type.length+" "), mid=new TextEncoder().encode(" "+payload.length+" ");
    const pae=new Uint8Array(head.length+type.length+mid.length+payload.length); let o=0;
    for(const part of [head,type,mid,payload]){ pae.set(part,o); o+=part.length; }
    const key=await crypto.subtle.importKey("raw",hexToBytes(pubHex),{name:"Ed25519"},false,["verify"]);
    return await crypto.subtle.verify({name:"Ed25519"},key,b64ToBytes(env.signatures[0].sig),pae);
  }catch(e){ return false; }
}
const shortName=p=>p.replace(/\/+$/,"").split("/").pop().split(":").pop()||p;

// Read every file dropped OR picked - recursing into dropped folders via the entries API.
async function filesFromDrop(dt){
  const items=dt.items?[...dt.items]:[]; const entries=items.map(i=>i.webkitGetAsEntry&&i.webkitGetAsEntry()).filter(Boolean);
  if(!entries.length) return [...dt.files];
  const out=[];
  const walk=e=>new Promise(res=>{
    if(e.isFile){ e.file(f=>{ out.push(f); res(); }, ()=>res()); }
    else if(e.isDirectory){ const rd=e.createReader(); const read=()=>rd.readEntries(async es=>{ if(!es.length) return res(); await Promise.all(es.map(walk)); read(); }, ()=>res()); read(); }
    else res();
  });
  await Promise.all(entries.map(walk));
  return out;
}

async function buildFromFiles(files){
  const canHash=!!(window.crypto&&crypto.subtle&&crypto.subtle.digest);   // needs https or localhost
  const keys={}, pubName={};
  // core/registry.js knows both store shapes: plankton keeps one object per file, a nekton scope is
  // one JSONL subnekton. Parsing every file as a single JSON object - which is what stood here -
  // silently skipped every subnekton: measured 68 of 2032 records on a current corpus, 1964 claims
  // gone, and the page still drew a convincing lineage-only graph.
  const { records, pubs } = await REG.unionFromFiles(files, (f)=>f.text());
  for(const p of pubs){                                                   // *.pub - raw ed25519 pubkey hex
    if(!canHash) continue;
    const kid=(await sha256hex(hexToBytes(p.hex))).slice(0,16);
    keys[kid]=p.hex; pubName[kid]=(p.file.name||"").replace(/\.pub$/i,"").split(/[\\/]/).pop();
  }
  // attested names: a sec:controller binding relabels a key ONLY if its signer is a trusted authority
  // (same rule as build_union.py; leave the trust box empty to trust none). Else the .pub filename.
  const trust=new Set(($("#trust")?.value||"").split(/[,\s]+/).map(s=>s.trim()).filter(Boolean));
  const bindings={};
  if(canHash) for(const rec of records){
    let p; try{ p=JSON.parse(atob(rec.envelope.payload)); }catch{ continue; }
    const signer=rec.envelope.signatures&&rec.envelope.signatures[0]&&rec.envelope.signatures[0].keyid;
    const body=p.predicate||{};                        // the claim body: {by, object, predicate(term), when}
    const pred=body.predicate;
    if(pred&&pred.uri==="https://w3id.org/security#controller"){
      // A binding relabels a key to a principal ONLY if a TRUSTED authority signed it. Two checks, both
      // required: (1) the claimed signer keyid is in the trust set (empty box = trust nobody, so a
      // SELF-SIGNED binding never attests - run-4); (2) the binding's DSSE signature actually VERIFIES
      // under THAT authority's real public key. Without (2), an attacker just RELABELS a self-signed
      // binding's keyid to a trusted authority and gets an "attested senior identity" green ring
      // (run-5): the claimed keyid is checked, the signature is not. verifyDsse closes that - the
      // authority's signature can't be forged. (If crypto.subtle lacks Ed25519, verifyDsse is false =
      // no attestation, the safe direction.)
      if(!trust.has(signer)) continue;
      const authPub=keys[signer];
      if(!authPub || !(await verifyDsse(rec.envelope, authPub))) continue;
      const principal=(body.object||{}).id||(body.object||{}).value;   // object is in the body, not top-level
      for(const s of (p.subject||[])) if(s.uri&&principal) bindings[s.uri]=principal;   // subject IS top-level
    }
  }
  const names={}, attNames=new Set();
  if(canHash) for(const kid in keys){ const iri="https://kton.dev/o/"+await sha256hex(hexToBytes(keys[kid]));
    if(bindings[iri]){ names[kid]=shortName(bindings[iri]); attNames.add(names[kid]); }   // attested identity
    else names[kid]=pubName[kid]; }                                                       // bare key label
  ATTESTED_NAMES=attNames;   // same rule as the URL flow: only trusted-authority-vouched labels are authenticated
  return { union:JSON.stringify(records), keys:JSON.stringify(keys), names:JSON.stringify(names), n:records.length, k:Object.keys(keys).length, noHash:!canHash };
}

function showDropUI(){
  $("#stats").innerHTML=""; $("#explorer").style.display="none"; $("#overview").style.display="block";
  $("#overview").innerHTML=`<div id="drop" class="drop">
    <h2>View your own records</h2>
    <p>Drop the object files from one or more <b>plankton</b> and <b>nekton</b> registries here
      (<code>objects/&hellip;/*.json</code>), plus any <code>*.pub</code> keys to draw the verified ring.
      No build step and nothing is uploaded - the union is assembled in your browser, exactly like a
      multi-source read: several registries converge on one graph by content hash.</p>
    <div class="picks">
      <label class="btn">choose a folder&hellip;<input id="pick"  type="file" webkitdirectory multiple hidden></label>
      <label class="btn">or files&hellip;<input id="pickf" type="file" multiple hidden></label>
    </div>
    <input id="trust" placeholder="optional: trusted authority keyids for attested names (comma-separated)">
    <div id="dropmsg"></div>
  </div>`;
  const drop=$("#drop"), msg=$("#dropmsg");
  const go=async files=>{ msg.textContent="reading "+files.length+" file"+(files.length===1?"":"s")+"…";
    try{ const b=await buildFromFiles(files);
      if(!b.n){ msg.textContent="No records found - drop registry object files (JSON with a fotonId or claimId)."; return; }
      renderGraph(b.union,b.keys,b.names);
      if(b.noHash) $("#stats").insertAdjacentHTML("beforeend",'<span class="bad">served over http: the verified ring needs https or localhost</span>');
    }catch(e){ msg.textContent="Could not read: "+(e&&e.message||e); } };
  $("#pick").onchange =e=>go([...e.target.files]);
  $("#pickf").onchange=e=>go([...e.target.files]);
  drop.ondragover=e=>{ e.preventDefault(); drop.classList.add("over"); };
  drop.ondragleave=()=>drop.classList.remove("over");
  drop.ondrop=async e=>{ e.preventDefault(); drop.classList.remove("over"); go(await filesFromDrop(e.dataTransfer)); };
}
function personOf(p){ const m=/^(.*?)-claims(\s\(self\))?$/.exec(String(p)); return m?m[1]+(m[2]||""):p; }
function index(){
  Object.assign(G, NAV.indexGraph(G));      // adjacency both ways - one copy, shared with the compass
  { let recs=[]; try{ recs=JSON.parse(CUR_UNION); }catch{}     // steps' command/description/rationale, from
    ANN=ANNO.annotations(G, recs); FULLH=ANNO.fullHashes(recs); FOTONID=SELM.fotonIds(recs); RELS=ANNO.relationEdges(G, recs); } // verified claims only (core/annotate.js)
  // one PERSON per identity: cockpit signs claims with "<id>-claims", runs with "<id>" - same holder
  for(const n of G.nodes) if(n.participant) n.participant=personOf(n.participant);
  // a file belongs to nobody: a recorder join can hand it the console session that read it, which
  // then shows up as a participant of its own
  for(const n of G.nodes) if(n.type==="source") n.participant="";
  // a scope's seed (kton 0.4: every nekton store opens a root scope) is a claim about urn:nekton:scope:<name>.
  // It is structure, not something said about a record: kind "scope", hidden unless the reader shows it
  // (filter chip "scope seeds"). It still counts in the totals - it is a signed record and is verified.
  for(const n of G.nodes) if(n.type==="entity" && /^urn:nekton:scope:/.test(n.uri||"")){ n.kind="scope";
    for(const e of (G.in[n.id]||[])) if(e.kind==="about"){ const c=G.byId.get(e.from); if(c&&c.type==="claim") c.kind="scope"; } }
  G.participants=[...new Set(G.nodes.map(n=>n.participant))].sort();
  G._parts=G.participants;
  // one colour per participant for good: the order is the one the published names give (every stage of a
  // page publishes the same names.json), so a party keeps its colour when other registries come and go;
  // only parties nobody named fall back to the order they appear in
  { const named=[...new Set(Object.values(NAMES_ALL||{}))].sort(), order=[...named, ...G._parts.filter(p=>!named.includes(p))];
    G._parts.forEach(p=>partColor[p]=PAL[order.indexOf(p)%PAL.length]); }
  // The types this corpus actually has. It used to be the literal ["foton","claim","source","ref"],
  // which is the very sin core/config.js warns about: right for one corpus and wrong for every
  // other. In /qm two thirds of the named things are `entity` - SOPs, findings, CAPAs - and the
  // literal filtered every one of them out of the drawing, silently.
  G._types=[...new Set(G.nodes.map(n=>n.type))].sort();
  filt={parts:new Set(G._parts), types:typesShown(), kinds:kindsHidden()};
}
// "unverified" said two different things. keys.json is the ring a host trusts (cockpit show serves only
// the CONFIGURED trust tiers), so a record whose signer is not in it cannot be checked here at all - that
// is "not in the trusted keys", not a signature that failed. Only a signer the ring holds and whose
// signature does not check is a failure. The wasm's verdict stays as it is; this only names the reason.
// A third state (kton-cockpit v0.5.3, /data/packages.json): {keyid: {key, packages: [{package, id, dir}]}}
// - one author's key may sign several admitted packages. A signer that is such a key is named by its
// packages - admitted by the cockpit, not a trust tier. With `key` (hex) the signature is re-checked here:
// the wasm verifies against the ring plus these keys, and such a node then reads "signed by admitted
// package X" (checked) or counts as unverified (the signature fails). It never counts as verified:
// verified means signed by a trusted key. A key whose sha256 does not derive its keyid is dropped.
let PACKAGES={}, PKG_KEYS={};
async function usePackages(map){ PACKAGES={}; PKG_KEYS={};
  if(!map||typeof map!=="object") return;
  for(const [kid,e] of Object.entries(map)){
    const names=(e&&Array.isArray(e.packages)?e.packages:[]).map(p=>p&&p.package).filter(x=>typeof x==="string"&&x);
    if(!names.length) continue;
    PACKAGES[kid]={ package:[...new Set(names)].sort().join(", ") };
    if(typeof e.key==="string" && /^[0-9a-f]{64}$/i.test(e.key)){
      const raw=new Uint8Array(e.key.match(/../g).map(h=>parseInt(h,16)));
      const d=[...new Uint8Array(await crypto.subtle.digest("SHA-256", raw))].map(b=>b.toString(16).padStart(2,"0")).join("").slice(0,16);
      if(d===kid) PKG_KEYS[kid]=e.key.toLowerCase(); } } }
/** the ring the wasm checks against: keys.json plus the admitted packages' build keys */
function ringWithPackages(keysText){ if(!Object.keys(PKG_KEYS).length) return keysText;
  try{ return JSON.stringify({ ...PKG_KEYS, ...JSON.parse(keysText||"{}") }); }catch{ return keysText; } }
function markUntrusted(g, keysText){ let ring; try{ ring=new Set(Object.keys(JSON.parse(keysText||"{}"))); }catch{ return; }
  let n=0, p=0, moved=0;
  for(const x of g.nodes){ if(!(x.type==="foton"||x.type==="claim") || !x.signer || ring.has(x.signer)) continue;
    const pk=PACKAGES[x.signer];
    if(pk&&pk.package){
      if(x.verified){ x.verified=false; moved++; x.pkgChecked=true; }          // checked against the package key, not trusted
      if(x.pkgChecked || !PKG_KEYS[x.signer]){ x.pkgSigned=String(pk.package); p++; } // no key: named, not checked
      // with a key and a failing signature it stays plain unverified
    } else if(!x.verified){ x.untrusted=true; n++; } }
  if(moved && g.summary&&g.summary.totals){ g.summary.totals.verified-=moved; g.summary.totals.unverified=(g.summary.totals.unverified||0)+moved; }
  g._untrusted=n; g._pkgSigned=p; g._pkgChecked=moved; }
function showStats(){ const t=G.summary.totals;
  const untrusted=G._untrusted||0, pkg=G._pkgSigned||0, failed=Math.max(0,(t.unverified||0)-untrusted-pkg);
  // the registries this picture is spliced from, each its own source (by hash, nothing merged beforehand)
  const srcs=Object.entries(SRCSTAT).filter(([,v])=>/records/.test(v));
  const regName=u=>{ const m=String(u).match(/([^/]+)\/[^/]*$/); return m?m[1]:u; };
  $("#stats").innerHTML=(srcs.length>1?`<span title="${esc(srcs.map(([u,v])=>regName(u)+": "+v).join("\n"))}"><b>${srcs.length}</b> registries <span style="opacity:.75">(${esc(srcs.map(([u])=>regName(u)).join(" + "))})</span>, spliced by hash</span>`:``)+`<span><b>${(G._parts||[]).filter(Boolean).length||t.participants}</b> participants</span><span><b>${t.fotons}</b> fotons</span><span><b>${t.claims}</b> claims</span><span class="ok"><b>${t.verified}</b>/${t.fotons+t.claims} verified</span>`+(pkg?`<span class="warn" title="signed by the build key of a package this cockpit admitted: not a trust tier. ${G._pkgChecked||0} of them checked here against the package's key (packages.json); the rest named only"><b>${pkg}</b> signed by an admitted package${G._pkgChecked?` (${G._pkgChecked===pkg?"all":G._pkgChecked} checked)`:``}</span>`:``)+(untrusted?`<span class="warn" title="signed by a key outside the trust tiers this host gave the viewer (keys.json): not checked here, not a failed signature"><b>${untrusted}</b> signer not in the trusted keys</span>`:``)+(failed?`<span class="bad" title="the signer's key is in keys.json and the signature does not check"><b>${failed}</b> unverified</span>`:``)
    +(G.summary.origins?`<span title="steps joined by file hash: seen by the recorder and signed / signed only / recorded only (not signed)"><b>${G.summary.origins.both}</b> recorded+signed · <b>${G.summary.origins.signed}</b> signed only · <b>${G.summary.origins.recorded+(G.summary.origins.hidden||0)}</b> recorded only</span>`+(G.summary.origins.hidden||recordedShown()&&prof().graph.recorded==="hide"?` <a href="#" id="togrec" title="steps the recorder saw that no signed record has">${recordedShown()?"hide them":"show them"}</a>`:``):``)
    +(STORE?`<span title="a portion of the store, chosen by the store and verified here - expand a node to grow it">portion: <b>${STORE.byId.size}</b> records${STORE.more?` · <span class="rc" data-storemore="1" title="some selections were cut at the page size">load more (${STORE.more} cut)</span>`:``}</span>`:``);
}

/* ===== OVERVIEW ===== */
function showOverview(push=true){
  // a view without the overview (an editor's) stays on the graph once it has one to show
  if(!prof().chrome.overview && ex && ex.center && G.byId.has(ex.center)) return;
  if(push) history.pushState({view:'overview'},"");
  $("#explorer").style.display="none"; $("#overview").style.display="block";
  const S=G.summary, rel={};
  // one card per PERSON (personOf): the kernel's summary counts each key on its own
  const people=new Map();
  for(const p of S.participants||[]){ const k=personOf(p.name), q=people.get(k)||{name:k,fotons:0,claims:0,verified:0,total:0};
    q.fotons+=p.fotons||0; q.claims+=p.claims||0; q.verified+=p.verified||0; q.total+=p.total||0; people.set(k,q); }
  for(const e of (S.edges||[])){ const f=personOf(e.from), t=personOf(e.to); if(f===t) continue;
    (rel[f]??=[]).push(`${e.kind==="about"?"reviews":"builds on"} ${esc(t.replace('pfed-',''))}×${esc(e.count)}`); }
  const cards=[...people.values()].map(p=>{ const pct=p.total?Math.round(100*p.verified/p.total):0;
    return `<div class="card" data-p="${esc(p.name)}"><div class="nm"><span class="d" style="background:${pcol(p.name)}"></span>${idlabel(p.name)}</div>
      <div class="mt">${p.fotons} fotons · ${p.claims} claims</div><div class="bar"><i style="width:${pct}%"></i></div>
      <div class="mt" style="margin-top:5px">${p.verified}/${p.total} verified (${pct}%)</div>${rel[p.name]?`<div class="rel">${rel[p.name].join(" · ")}</div>`:``}</div>`;}).join("");
  const datasets=G.nodes.filter(n=>n.type==="source"&&n.kind==="dataset");
  const dsCards=datasets.map(d=>{ const c=(G.in[d.id]||[]).length;
    return `<div class="card" data-src="${esc(d.id)}"><div class="nm"><span class="d" style="background:#cfd8e6"></span>${esc(d.role)}</div>
      <div class="mt">original dataset · external input</div><div class="rel">${c} step${c===1?"":"s"} build on it → start here</div></div>`;}).join("");
  $("#overview").innerHTML=`<div class="searchrow"><div style="position:relative"><input id="q" placeholder="search a record - participant, role, hash, or text…" autocomplete="off"><div id="results"></div></div><span style="color:var(--dim);font-size:12.5px">or start from a dataset / participant below</span></div>
    ${datasets.length?`<div class="h2">datasets - start a pedigree from the original data</div><div class="cards">${dsCards}</div>`:``}
    <div class="h2" style="margin-top:18px">participants (grouped view - scales with the federation, not the record count)</div><div class="cards">${cards}</div>
    <div class="h2" style="margin-top:18px">views - answer a question across the whole graph</div>
    <div class="cfacets"><span class="fchip" id="vReview">⚠ fotons needing sign-off</span><span class="fchip" id="vRepro">↻ reproduction coverage</span>
      <span class="cmt">or open any foton → <b style="color:var(--accent)">trace to roots · blast radius · trust card</b></span></div>
    <div id="viewout" class="claimlist"></div>
    ${CLAIMS.length?`<div class="h2" style="margin-top:18px">claims (${CLAIMS.length}) - filter by predicate, value, or signer</div>
      <div class="cfacets">
        <select id="fPred"><option value="">any predicate</option>${[...new Set(CLAIMS.map(c=>c.pred))].sort().map(v=>`<option>${esc(v)}</option>`).join("")}</select>
        <select id="fVal"><option value="">any value</option>${[...new Set(CLAIMS.map(c=>c.value).filter(Boolean))].sort().map(v=>`<option>${esc(v)}</option>`).join("")}</select>
        <select id="fSigner"><option value="">any signer</option>${[...new Set(CLAIMS.map(c=>c.part).filter(Boolean))].sort().map(v=>`<option>${esc(v.replace('pfed-',''))}</option>`).join("")}</select>
        <span id="cCount" class="cmt"></span>
      </div>
      <div id="claimlist" class="claimlist"></div>`:``}`;
  $$("#overview .card[data-src]").forEach(el=>el.onclick=()=>enterExplorer(el.dataset.src,"desc"));
  $$("#overview .card[data-p]").forEach(el=>el.onclick=()=>enterExplorer(firstNodeOf(el.dataset.p),"both"));
  if(CLAIMS.length){ ["fPred","fVal","fSigner"].forEach(f=>$("#"+f).onchange=renderClaims); renderClaims(); }
  $("#vReview").onclick=()=>renderView("review"); $("#vRepro").onclick=()=>renderView("repro");   // stories 3,4
  const q=$("#q"),res=$("#results");
  q.oninput=()=>{ const s=q.value.trim().toLowerCase(); if(!s){res.style.display="none";return;}
    const hits=G.nodes.filter(n=>`${n.id} ${n.role} ${n.text||""} ${n.participant} ${n.by||""}`.toLowerCase().includes(s)).slice(0,40);
    res.innerHTML=hits.map(n=>`<div class="r" data-id="${esc(n.id)}"><span style="color:${pcol(n.participant)}">${idlabel(n.participant)}</span> · ${esc(n.type)} · ${esc(n.role)} <span class="m">${clipe(n.headline||n.id,44)}</span></div>`).join("")||`<div class="r m">no match</div>`;
    res.style.display="block"; res.querySelectorAll(".r[data-id]").forEach(r=>r.onclick=()=>enterExplorer(r.dataset.id,"both")); };
  document.addEventListener("click",e=>{ if(e.target!==q) res.style.display="none"; },true);
}
// the claims-browser: facet CLAIMS by predicate / value / signer, list the matches, click to open one in its subject's graph
function renderClaims(){ const P=$("#fPred").value, V=$("#fVal").value, S=$("#fSigner").value;
  const rows=CLAIMS.filter(c=>(!P||c.pred===P)&&(!V||c.value===V)&&(!S||c.part.replace('pfed-','')===S));
  $("#cCount").textContent=rows.length+" / "+CLAIMS.length+" claims";
  $("#claimlist").innerHTML=rows.map(c=>{ const subj=G.byId.get(c.subj); const sr=subj?subj.role:c.subj;
    return `<div class="crow" data-id="${esc(c.id)}" data-subj="${esc(c.subj)}"><span class="cd" style="background:${pcol(c.part)}"></span><b>${esc(c.part.replace('pfed-',''))}</b> <span class="cpred">${esc(c.pred)}${c.value?": "+esc(c.value):""}</span> <span class="cm">&rarr; ${clipe(sr,28)}</span></div>`;
  }).join("")||`<div class="crow cm">no claims match</div>`;
  $$("#claimlist .crow[data-id]").forEach(el=>el.onclick=()=>{ const subj=el.dataset.subj, center=G.byId.has(subj)?subj:el.dataset.id;
    enterExplorer(center,"both"); ex.sel=el.dataset.id; redraw(); inspect(el.dataset.id); });
}
// stories 3,4 - a whole-graph question answered as a list (absence / coverage over fotons)
function renderView(kind){ const out=$("#viewout"); if(!out) return; if(out.dataset.k===kind){ out.innerHTML=""; out.dataset.k=""; return; } out.dataset.k=kind;
  // the worklists live in core/questions.js; a sketch can add its own by naming a predicate
  const q=QS.BUILTIN.find(x=>x.id===(kind==="review"?"signoff":"repro"));
  const r=QS.ask(G,q); const list=r.list; let head;
  if(kind==="review") head=`<b>${r.head.missing}</b> of ${r.head.of} fotons have <b>no approval</b> — the sign-off worklist`;
  else head=`<b>${r.head.confirmed}</b> reproduced · <b>${r.head.missing}</b> not — showing the <b>unconfirmed</b>`;
  out.innerHTML=`<div class="cmt" style="margin:4px 0 8px;display:flex;justify-content:space-between;gap:8px;align-items:center"><span>🔍 ${head}</span><span class="fchip" style="flex:none" data-clear="${kind}">✕ clear</span></div>`
    +list.slice(0,200).map(n=>`<div class="crow" data-id="${esc(n.id)}"><span class="cd" style="background:${pcol(n.participant)}"></span><b>${idlabel(n.participant)}</b> <span class="cpred">${esc(n.role)}</span> <span class="cm">${esc(n.id)}</span></div>`).join("")
    +(list.length>200?`<div class="crow cm">…and ${list.length-200} more</div>`:"");
  $$("#viewout .crow[data-id]").forEach(el=>el.onclick=()=>enterExplorer(el.dataset.id,"nbr"));   // land in a small local view
}
function firstNodeOf(part){ const ns=G.nodes.filter(n=>n.participant===part&&n.type!=="source");
  ns.sort((a,b)=>((G.in[b.id]||[]).length)-((G.in[a.id]||[]).length)); return (ns[0]||G.nodes.find(n=>n.participant===part)).id; }

/* ===== EXPLORER (pedigree, layered DAG) ===== */
// The pedigree / reach / claims primitives moved to ../../core/nav.js: the compass walks the same
// cone, and a second traversal would be a second answer to "what does this rest on". What stays here
// is the shape the call sites below already expect - including pedigree's .trunc/.total side channel,
// which the controls read to say "capped 250 of 2000" instead of quietly showing a slice.
// The attested walk, then the READING extended over it.
//
// A derived edge is not decoration on the attested picture - it is what CONNECTS nodes the attested
// walk keeps apart. In this QMS corpus the records are leaves hanging off separate claims, so a
// pedigree that only follows signed edges brings in one end of every derived edge and never the
// other: the layer ships 898 edges and the canvas draws zero. So the walk has to be allowed to
// cross them - but only the ones a LINE is the right shape for, and the nodes that arrive that way
// are counted separately, because they are here on the strength of a reading, not a signature.
function pedigree(id, mode, cap){
  const r=NAV.pedigree(G,id,mode,cap); pedigree.trunc=r.truncated; pedigree.total=r.total;
  pedigree.viaDerived=0;
  if(DERIVED && DERIVED.edges){
    const lines=DERIVED.edges.filter(e=>e.as==="edge");
    const limit=cap===Infinity?Infinity:cap;
    for(let grew=true; grew && r.set.size<limit; ){
      grew=false;
      for(const e of lines){
        const a=r.set.has(e.from), b=r.set.has(e.to);
        if(a===b) continue;                       // both in, or both out
        const add=a?e.to:e.from;
        if(!G.byId.get(add)) continue;            // never invent a node
        r.set.add(add); pedigree.viaDerived++; grew=true;
        if(r.set.size>=limit) break;
      }
    }
  }
  return r.set;
}
function reach(id, dir, kind, depth){ return NAV.reach(G,id,dir,kind,depth); }
function claimsOf(id){ return NAV.claimsOf(G,id); }
function claimMatch(c, pred, value){ return NAV.claimMatch(c,pred,value); }
function fotonsWith(pred, value){ return NAV.fotonsWith(G,pred,value); }
function fotonsLacking(pred, value){ return NAV.fotonsLacking(G,pred,value); }

// story 5 - aggregate the claims ABOUT a node into a trust card (approve/reject tally, reproduced?, roles).
// story 5 - the aggregate now comes from core/questions.js so the compass shows the same tally.
// Only the LOOK stays here: a card is a rendering decision, and the graph and the compass are
// allowed to disagree about it while agreeing about the numbers.
function trustCard(id){
  const c=QS.ask(G, QS.BUILTIN.find(q=>q.id==="trust"), {node:id});
  if(!c.claims) return `<div class="kv">no claims about this record yet.</div>`;
  const repro=c.reproducedBy>0;
  let rows=""; for(const pred in c.byPredicate){
    const parts=Object.entries(c.byPredicate[pred]).map(([v,n])=>`${v||"—"}${n>1?" ×"+n:""}`).join(", ");
    rows+=`<div class="tcrow"><b>${esc(pred)}</b> · ${esc(parts)}</div>`; }
  return `<div class="tcbanner"><span class="badge ${c.reject?"vno":c.approve?"vok":"vpart"}">${c.approve} approve · ${c.reject} reject</span> <span class="badge ${repro?"vok":"vpart"}" title="distinct signers of reproduces claims about this foton">${repro?(c.reproducedBy>1?"↻"+c.reproducedBy+" reproduced":"✓ reproduced"):"not reproduced"}</span></div>${rows}`;
}

function enterExplorer(centerId, mode, push=true, note=""){ $("#overview").style.display="none"; $("#explorer").style.display="block";
  ex={center:centerId, sel:centerId, mode, grouped:false, cap:PED_CAP, note, expand:new Set(), view:{scale:1,tx:0,ty:0}, started:false}; drawControls(); layoutAndRun();
  // ?panel=closed: the first picture gets the whole width (a talk, a projector); a click opens the panel
  if(PANEL_AT_START) inspect(centerId); fitView(); redraw();
  // the centre goes into the URL, not just into history state: that is what the compass reads when
  // the reader switches back, and what makes a pasted link land on the same node
  if(VS) VS.writeState({focus:centerId, mode}, {push});
  if(BRIDGE){ BRIDGE.intent({type:"navigate", node:centerId, mode}); BRIDGE.state(viewState()); }
  if(push) history.pushState({view:'explorer',center:centerId,mode},""); }
// The compass answers a different question about the same node - where can I go from here, and
// why - so the link carries the whole state rather than starting the reader over.
function compassLink(center){
  if(!VS) return "../compass/index.html";
  return VS.linkTo("../compass/index.html", {...VS.readState(), focus:center});
}
// The graph already had these filters; they become facets so the compass can carry them across and
// a host can set them. An empty list means "no restriction", never "nothing" - a facet that filtered
// everything away when it was left unset would make a shared link land on an empty canvas.
function graphFacets(){
  const f={};
  if(filt.parts.size && filt.parts.size!==G._parts.length) f.participant=[...filt.parts];
  const allT=G._types||[];
  if(filt.types.size && filt.types.size!==allT.length) f.type=[...filt.types];
  return f;
}
function applyFacets(f){
  const refused=[];
  if(f.participant){ const known=new Set(G._parts); const bad=f.participant.filter(x=>!known.has(x));
    bad.forEach(x=>refused.push(`participant=${x}`));
    filt.parts = f.participant.length ? new Set(f.participant.filter(x=>known.has(x))) : new Set(G._parts); }
  if(f.type){ const allT=G._types||[];
    const bad=f.type.filter(x=>!allT.includes(x)); bad.forEach(x=>refused.push(`type=${x}`));
    filt.types = f.type.length ? new Set(f.type.filter(x=>allT.includes(x))) : new Set(allT); }
  // `view` is not a filter, it is which READING to draw - but it rides in the facet bag because
  // that is the only field kton:show forwards to this viewer today. Until `view` gets its own
  // handover field it lives here, named honestly rather than smuggled into `layout`.
  if(f.view){ const want=[].concat(f.view)[0];
    if(want==="contracted"||want==="attested") VIEW = (want==="contracted"&&!CONTRACTED) ? "attested" : want;
    else refused.push(`view=${want}`); }
  for(const k of Object.keys(f)) if(k!=="participant"&&k!=="type"&&k!=="view") refused.push(`facet=${k}`);
  return refused;
}
function drawControls(){ const modes=[["nbr","neighbours"],["anc","ancestors"],["desc","descendants"],["both","both"]]; const c=G.byId.get(ex.center);
  $("#controls").innerHTML=`<span class="ctoggle" id="ctoggle">☰ filters ▾</span><span class="back" data-overview="1">← overview&nbsp;&nbsp;<span style="opacity:.55;font-weight:400">Esc</span></span>
    <a class="back" id="tocompass" style="display:block;margin-top:3px" href="${esc(compassLink(ex.center))}" title="same place, other question: where does it lead from here, and why">↔ compass&nbsp;&nbsp;<span style="opacity:.55;font-weight:400">directions</span></a>
    <div class="qbar"><span class="qq">🔍 ${ex.note?esc(ex.note):("pedigree · "+({nbr:"neighbours",anc:"ancestors",desc:"descendants",both:"both",lin:"trace inputs",imp:"blast radius"}[ex.mode]||ex.mode))}</span><span class="qx" id="qreset" title="reset to this node's pedigree">reset ✕</span></div>
    <div class="lbl">centre</div><div style="font-size:12.5px;color:${c.type==='source'?'#cfd8e6':'var(--accent)'}">${c.type==='source'?esc(c.role):idlabel(c.participant)+" · "+esc(c.role)}</div>
    ${ex.note?`<div class="lbl" style="color:var(--b);margin-top:4px;text-transform:none;letter-spacing:0">▸ ${ex.note}</div>`:``}
    <div class="lbl">pedigree (path explorer)</div><div class="arrows">${modes.map(m=>`<button data-m="${m[0]}" class="${ex.mode===m[0]?'on':''}">${m[1]}</button>`).join("")}</div>
    <div class="lbl">grouping</div><div class="fchips"><span class="fchip ${ex.grouped?'':'off'}" id="grpbtn">▣ group same-participant runs</span></div>
    ${(ex.mode==="lin"||ex.mode==="imp")?`<div class="lbl">lineage</div><div class="fchips"><span class="fchip ${ex.collapse===false?'off':''}" id="collbtn">⋯ collapse linear runs</span></div>`:``}
    <div class="lbl">filter · participant</div><div class="fchips">${G._parts.filter(Boolean).map(p=>`<span class="fchip ${filt.parts.has(p)?'':'off'}" data-fp="${esc(p)}"><span class="d" style="background:${pcol(p)}"></span>${esc(p.replace('pfed-',''))}</span>`).join("")}</div>
    ${CONTRACTED?`<div class="lbl">Sicht</div><div class="fchips"><span class="fchip ${VIEW==="contracted"?"":"off"}" id="vcontract" title="Aussagen als Kante oder Mark, Dateien nur als Bruecke">kontrahiert</span></div>`:``}
    <div class="lbl">filter · type</div><div class="fchips">${(G._types||[]).map(t=>`<span class="fchip ${filt.types.has(t)?'':'off'}" data-ft="${esc(t)}">${esc(t)}</span>`).join("")}${G.nodes.some(n=>n.type==="source"&&n.kind==="code")?`<span class="fchip ${(filt.kinds&&filt.kinds.has("source:code"))?'off':''}" data-fk="source:code" title="the scripts each step ran - also listed with the step's inputs">scripts</span>`:``}${G.nodes.some(n=>n.kind==="scope")?`<span class="fchip ${(filt.kinds&&filt.kinds.has("claim:scope"))?'off':''}" data-fk="claim:scope entity:scope" title="the seeds that open each nekton store's scopes (kton 0.4: a root scope per store) - signed records, counted in the totals">scope seeds</span>`:``}</div>
    <div class="lbl" id="cnt"></div>`;
  $$("#controls .arrows button").forEach(b=>b.onclick=()=>{ex.mode=b.dataset.m;ex.cap=PED_CAP;ex.note="";ex.expand=new Set();drawControls();layoutAndRun();});   // re-cap + leave any story view
  $("#grpbtn").onclick=()=>{ex.grouped=!ex.grouped;drawControls();layoutAndRun();};
  // "im Kompass": in a verticle, OPEN the compass verticle (the host re-aims an open one); the <a
  // href> is browser-only. Following the href in a webview navigates the panel to a broken relative
  // URL (host-cache/kton-graph/compass/index.html) - exactly the symptom the user saw.
  { const tc=$("#tocompass"); if(tc) tc.onclick=(e)=>{ if(window.improveHost){ e.preventDefault();
      window.improveHost.callOp("ui:openVerticle",{verticle:"kton-compass",context:{focus:ex.center, mode:ex.mode}}).catch(()=>{}); } }; }
  { const cb=$("#collbtn"); if(cb) cb.onclick=()=>{ex.collapse=(ex.collapse===false);drawControls();layoutAndRun();}; }
  { const qr=$("#qreset"); if(qr) qr.onclick=()=>enterExplorer(ex.center,"both"); }   // reset the query -> plain pedigree of this node
  const afterFilter=()=>{ drawControls(); layoutAndRun();
    if(VS) VS.writeState({facets:graphFacets()});
    if(BRIDGE) BRIDGE.state(viewState()); };
  $$("#controls [data-fp]").forEach(el=>el.onclick=()=>{const p=el.dataset.fp;filt.parts.has(p)?filt.parts.delete(p):filt.parts.add(p);afterFilter();});
  { const b=$("#vcontract"); if(b) b.onclick=()=>{ VIEW = VIEW==="contracted"?"attested":"contracted"; drawControls(); layoutAndRun(); }; }
  $$("#controls [data-ft]").forEach(el=>el.onclick=()=>{const t=el.dataset.ft;filt.types.has(t)?filt.types.delete(t):filt.types.add(t);afterFilter();});
  $$("#controls [data-fk]").forEach(el=>el.onclick=()=>{const ks=el.dataset.fk.split(" ");filt.kinds=filt.kinds||new Set();const off=filt.kinds.has(ks[0]);for(const k of ks){ if(off) filt.kinds.delete(k); else filt.kinds.add(k); }afterFilter();});
  { const ct=$("#ctoggle"); if(ct) ct.onclick=()=>{ const c=$("#controls"); c.classList.toggle("min"); ex.ctrlsOpen=!c.classList.contains("min"); }; }
  if(((window.matchMedia&&matchMedia("(max-width:640px)").matches)||EMBEDDED()) && !ex.ctrlsOpen) $("#controls").classList.add("min");  // phones: start collapsed, tap ☰ to expand
}
function layoutAndRun(){
  const set=pedigree(ex.center, ex.mode, ex.cap||PED_CAP);
  let nodes=[...set].map(id=>G.byId.get(id)).filter(n=>n.id===ex.center||(filt.parts.has(n.participant)&&filt.types.has(n.type)&&!(filt.kinds&&filt.kinds.has(n.type+":"+n.kind))&&!singleUse(n)));
  const keep=new Set(nodes.map(n=>n.id));
  let edges=G.edges.filter(e=>keep.has(e.from)&&keep.has(e.to));
  if(ex.grouped){ const g=groupRuns(nodes,edges); nodes=g.nodes; edges=g.edges; }
  if((ex.mode==="lin"||ex.mode==="imp") && ex.collapse!==false){        // lineage skeleton: keep the centre + any ATTESTED node (a landmark), collapse the rest
    const keepIds=new Set([ex.center,ex.sel]); for(const nn of nodes) if(claimsOf(nn.id).length) keepIds.add(nn.id);
    const c=collapseRuns(nodes,edges,keepIds,ex.expand); nodes=c.nodes; edges=c.edges; }
  // The contracted view. The walk still runs over what was signed - then the boxes that were only
  // bookkeeping are dropped and the edges that replace them put back, so nothing becomes
  // unreachable: a claim between two steps IS the edge between them.
  if(VIEW==="contracted" && CONTRACTED){
    const keep=new Set(CONTRACTED.nodes), inCone=new Set(nodes.map(n=>n.id));
    nodes=nodes.filter(n=>keep.has(n.id));
    const still=new Set(nodes.map(n=>n.id));
    edges=CONTRACTED.edges.filter(e=>still.has(e.from)&&still.has(e.to));
    ex.contractedAway=inCone.size-still.size;
  } else ex.contractedAway=0;

  // The derived layer, and only the part of it that a LINE is the right shape for. `as` came out of
  // how the references spread (core/projection.js): a field that puts 250 strokes onto two people is
  // an attribute, not a picture, and drawing it is how the canvas became a texture in the first
  // place. What is not drawn is COUNTED - a filter that hides silently is why "is this everything?"
  // had no answer.
  let held=0, heldBy={};
  if(DERIVED && DERIVED.edges){
    const inView=new Set(nodes.map(n=>n.id));
    for(const d of DERIVED.edges){
      // A LINE needs both ends. What is deliberately not drawn is counted as soon as it TOUCHES
      // something on screen - the question it answers is "what is there about what I am looking at
      // that you did not draw", and half of that hangs on nodes outside the cone by construction.
      if(d.as==="edge"){ if(inView.has(d.from)&&inView.has(d.to)) edges=edges.concat([d]); continue; }
      if(inView.has(d.from)||inView.has(d.to)){ held++; heldBy[d.as]=(heldBy[d.as]||0)+1; }
    }
  }
  // A step developed from another: an edge between the two steps, from the verified claim. The kernel
  // draws the claim as a box between them; with claim boxes hidden (or contracted) the relation would
  // otherwise vanish. Added once per pair and kind.
  { const inV=new Set(nodes.map(n=>n.id)), have=new Set(edges.map(e=>e.from+"|"+e.to+"|"+shortRel(e.kind)));
    for(const r of RELS) if(inV.has(r.from)&&inV.has(r.to)){
      // a reproduction read from its claim says more than the generic line for the same pair (its level): it wins
      if(r.kind==="reproduces") edges=edges.filter(e=>!(e.from===r.from&&e.to===r.to&&shortRel(e.kind)==="reproduces"&&!e.step));
      else if(have.has(r.from+"|"+r.to+"|"+r.kind)) continue;
      edges=edges.concat([r]); } }
  // "auto" (the default): on when a model tree is in view - the picture it is made for
  const treeInView=edges.some(e=>e.step&&e.kind==="parent-step");
  const on=(v,word)=>v===word||(v==="auto"&&treeInView);
  if(on(prof().graph.outputs,"endpoints")){ const o=endpointOutputs(nodes,edges); nodes=nodes.concat(o.nodes); edges=edges.concat(o.edges); }
  ex.nodes=nodes; ex.edges=edges; ex._byId=new Map(nodes.map(n=>[n.id,n]));
  ex.held=held; ex.heldBy=heldBy;
  // `based-on` is a lighter relation ("adapted from"): drawn, but it does not decide the layering
  layerLayout(ex.nodes, ex.edges.filter(e=>!(e.step&&(e.kind==="based-on"||e.kind==="reproduces"))), ex._byId);
  // a reproduction is the same thing again: directly BELOW the run it reproduces, in its column; what
  // stood there moves down to make room
  for(const e of ex.edges) if(e.step&&e.kind==="reproduces"){ const r=ex._byId.get(e.from), t=ex._byId.get(e.to); if(!r||!t) continue;
    const gap=Math.max(60,(+prof().node.font||11)*4.4), y=t.y+gap;
    for(const n of ex.nodes) if(n!==r&&n!==t&&Math.abs(n.x-t.x)<1&&n.y>t.y-1&&n.y<y+gap) n.y+=gap;
    r.x=t.x; r.y=y; r._d=t._d; }
  basedOnBelow();
  // notes stand in for the claim boxes: with claims drawn, the rationale is already on screen
  if(prof().graph.notes==="rationale"||(prof().graph.notes==="auto"&&treeInView&&!filt.types.has("claim"))){ const ns=rationaleNotes(ex.nodes); ex.nodes=ex.nodes.concat(ns); ns.forEach(n=>ex._byId.set(n.id,n)); roomForNotes(ns); }
  const derivedShown=edges.filter(e=>e.derived).length;
  const AS_DE={facet:"facets",region:"regions",group:"groups"};
  $("#cnt").innerHTML=`${ex.nodes.length} nodes · ${ex.edges.length} links`
    +(ex.contractedAway?` · <span style="opacity:.7">${ex.contractedAway} nodes contracted</span>`:``)
    +(derivedShown?` · <span style="opacity:.7">${derivedShown} derived`
        +(pedigree.viaDerived?`, ${pedigree.viaDerived} nodes reached only through them`:``)+`</span>`:``)
    +(held?` · <span title="not drawn: a line is the wrong shape for them" style="opacity:.7">`
        +Object.entries(heldBy).map(([k,n])=>`${n} ${AS_DE[k]||k}`).join(", ")+` not drawn</span>`:``)
    +(pedigree.trunc?` · <b style="color:var(--b)">capped ${ex.cap||PED_CAP} of ${pedigree.total}</b>`
        +`<div class="fchips" style="margin-top:6px"><span class="fchip" id="expand">expand +400 ▸</span><span class="fchip" id="expandall">show all ${pedigree.total}</span></div>`:``);
  if(pedigree.trunc){ $("#expand").onclick=()=>{ ex.cap=(ex.cap||PED_CAP)+400; layoutAndRun(); };
    $("#expandall").onclick=()=>{ ex.cap=Infinity; layoutAndRun(); }; }
  fitView(); if(!ex.started) startRender(); redraw();
}
// A step based on another (a verified parent-step claim) sits directly BELOW its parent, whatever the
// layout made of the data flow; siblings side by side, centred under the parent, grandchildren under
// them. Whatever the subtree now covers moves down out of its way. The data-flow edges are untouched.
function basedOnBelow(){
  const kids=new Map();
  for(const e of ex.edges) if(e.step&&e.kind==="parent-step"){ const c=ex._byId.get(e.from), p=ex._byId.get(e.to);
    if(!c||!p||c===p) continue; if(!kids.has(p.id)) kids.set(p.id,[]); if(!kids.get(p.id).includes(c)) kids.get(p.id).push(c); }
  if(!kids.size) return;
  const f=+prof().node.font||11, wrap=+prof().node.wrap||22;
  const sp=Math.max(240, wrap*f*0.6+70), gap=labelsBelow()?Math.max(170, 3*1.18*f+120):Math.max(130, 96+2.3*f);   // a wrapped label and its note fit beside each
  const isKid=new Set([...kids.values()].flat().map(n=>n.id)), placed=new Set();
  const width=(n,seen)=>{ if(seen.has(n.id)) return sp; seen.add(n.id); const k=kids.get(n.id)||[];
    return k.length?Math.max(sp, k.reduce((a,c)=>a+width(c,seen),0)):sp; };
  const place=(p)=>{ placed.add(p.id);
    const k=(kids.get(p.id)||[]).filter(c=>!placed.has(c.id)).sort((a,b)=>(a.x-b.x)||(a.y-b.y)); if(!k.length) return;
    // labels under the nodes (an orthogonal left-to-right picture): the children stack in the parent's
    // own column, an indented tree, so the flow lines leaving them sideways never meet a sibling
    if(labelsBelow()){ let y=p.y; for(const c of k){ y+=gap; c.x=p.x; c.y=y; placed.add(c.id); const before=y; place(c);
        y=Math.max(before, ...[...placed].map(id=>ex._byId.get(id)).filter(n=>n.x===p.x).map(n=>n.y)); } return; }
    const ws=k.map(c=>width(c,new Set([p.id]))); let x=p.x-ws.reduce((a,b)=>a+b,0)/2;
    k.forEach((c,i)=>{ c.x=x+ws[i]/2; c.y=p.y+gap; placed.add(c.id); x+=ws[i]; });
    k.forEach(place); };
  const roots=[...kids.keys()].filter(id=>!isKid.has(id)).map(id=>ex._byId.get(id));
  for(const r of roots){ const before=new Set(placed); place(r);
    const T=[...placed].filter(id=>!before.has(id)).map(id=>ex._byId.get(id)), sub=T.filter(n=>n!==r);
    if(!sub.length) continue;
    const x0=Math.min(...sub.map(n=>n.x))-sp/2, x1=Math.max(...sub.map(n=>n.x))+sp/2, y1=Math.max(...sub.map(n=>n.y));
    const inT=new Set(T.map(n=>n.id));
    for(const n of ex.nodes) if(!inT.has(n.id)&&n.type!=="note"&&n.x>x0&&n.x<x1&&n.y>r.y+RW&&n.y<y1+gap*0.8) n.y+=y1+gap-r.y; }
}
// based on: orthogonal - up from the child, across, up into the parent - with the arrow into the child as
// the data flow draws it; the label beside the child's own drop, so two siblings never stack theirs
function manhattan(ctx,p,c,color,lbl,off=0){
  const f=+prof().node.font||11, top=c.y-RW-4, below=labelsBelow();
  const bar=(below?c.y-Math.max(30,f*2.4):p.y+Math.min(80,(c.y-p.y)*0.6))+off;
  const px=p.x+off, cx=c.x+(c.x<p.x?-off:off);
  ctx.strokeStyle=color; ctx.lineWidth=off?1.2:1.6; ctx.setLineDash([]); ctx.beginPath();
  // with the label under the parent, the trunk leaves sideways and drops outside the label's width
  if(below&&Math.abs(c.x-p.x)<1){   // the indented tree: out of the parent's left, down the side, into the child from the left
    const side=p.x-Math.max(RW+14,(+prof().node.wrap||22)*f*0.3+12)+off, ey=c.y+off, ex2=c.x-RW-4;
    ctx.moveTo(p.x-RW,p.y+off); ctx.lineTo(side,p.y+off); ctx.lineTo(side,ey); ctx.lineTo(ex2,ey); ctx.stroke();
    const h=7; ctx.fillStyle=color; ctx.beginPath(); ctx.moveTo(ex2,ey); ctx.lineTo(ex2-h,ey-h*0.55); ctx.lineTo(ex2-h,ey+h*0.55); ctx.fill();
    if(lbl){ ctx.font=`italic ${Math.max(10,Math.round(f*0.86))}px ui-sans-serif`; ctx.textAlign="center"; ctx.textBaseline="bottom"; ctx.fillText(lbl,(side+ex2)/2,ey-4); }
    return; }
  if(below){ const side=p.x-Math.max(RW+14,(+prof().node.wrap||22)*f*0.3+12)+off; ctx.moveTo(p.x-RW,p.y+off); ctx.lineTo(side,p.y+off); ctx.lineTo(side,bar); }
  else { ctx.moveTo(px,p.y+RW); ctx.lineTo(px,bar); }
  ctx.lineTo(cx,bar); ctx.lineTo(cx,top); ctx.stroke();
  const h=7; ctx.fillStyle=color; ctx.beginPath(); ctx.moveTo(cx,top); ctx.lineTo(cx-h*0.55,top-h); ctx.lineTo(cx+h*0.55,top-h); ctx.fill();
  if(lbl){ ctx.font=`italic ${Math.max(10,Math.round(f*0.86))}px ui-sans-serif`; ctx.textAlign="right"; ctx.textBaseline="middle"; ctx.fillText(lbl,c.x-(c.x<p.x?14:7),(bar+top)/2+4); } }
// The deliverables: what the flow hands out at its end. The kernel draws a file only where no step
// made it, so an output is never a node; here the outputs of the LAST steps become nodes - steps no
// step in view reads from, and outside the model tree (a fit is the modeling, not the hand-over).
// Real file hashes as ids, so a click on one is a click on that file.
function endpointOutputs(nodes, edges){
  const inV=new Set(nodes.map(n=>n.id)), tree=new Set(), read=new Set(), out={nodes:[],edges:[]};
  for(const r of RELS) if(r.kind==="parent-step"&&inV.has(r.from)&&inV.has(r.to)){ tree.add(r.from); tree.add(r.to); }
  for(const e of edges) if(e.kind==="builds-on"&&inV.has(e.from)) read.add(e.to);
  for(const n of nodes){ if(n.type!=="foton"||tree.has(n.id)||read.has(n.id)) continue;
    for(const o of n.outputs||[]){ if(!o||!o.hash||inV.has(o.hash)) continue;
      const f=G.byId.get(o.hash)||{id:o.hash, type:"source", kind:"deliverable", role:String(o.name||o.hash).split("/").pop(), name:o.name||"", participant:n.participant, verified:false, endpoint:true};
      if(!G.byId.has(f.id)) G.byId.set(f.id,f);
      inV.add(f.id); out.nodes.push(f); out.edges.push({from:f.id, to:n.id, kind:"builds-on", made:true}); } }
  return out; }
// A step's rationale as a note beside it: what the modeler wrote about WHY, readable on the canvas
// rather than one click away. Placed after the layout, so it never moves a step.
function rationaleNotes(nodes){ const out=[];
  for(const n of nodes){ if(n.type!=="foton") continue; const r=stepInfo(n).rationale; if(!r) continue;
    out.push({id:"why:"+n.id, type:"note", step:n.id, text:r, participant:n.participant, x:n.x+RW*1.4, y:n.y+RW*3.2}); }
  return out; }
// Where a step's note sits and how tall it is: under the step's label, in both label modes. One
// function for the layout (roomForNotes) and the drawing (noteDraw), so the room made is the room used.
function noteBox(s,text){ const lines=wrapText(text,34,3).length, h=lines*12+6, f=+prof().node.font||11, lh=f*1.18, r=focusR(s)||RW;
  const top=labelsBelow()? labelTop(s,r)+labelLines(s)*lh+4 : s.y+Math.max(r+4, labelLines(s)*lh/2+5);
  return {top, h, x:labelsBelow()?null:s.x+r+5}; }
// The notes are placed after the layout, which spaced a column for nodes and labels only: a step
// stacked under another (a rerun under its run, two figures from one input) sat closer than the note
// between them is tall. Walk each column top to bottom and push what follows below the note above it -
// steps only move down, within their column, so the left-to-right flow and its edges stay as laid out.
function roomForNotes(notes){ if(!notes.length) return;
  const f=+prof().node.font||11, lh=f*1.18, noteOf=new Map(notes.map(n=>[n.step,n]));
  const cols=new Map(); for(const n of ex.nodes){ if(n.type==="note") continue; const k=Math.round(n.x); if(!cols.has(k)) cols.set(k,[]); cols.get(k).push(n); }
  for(const col of cols.values()){ if(col.length<2) continue; col.sort((a,b)=>a.y-b.y);
    for(let i=1;i<col.length;i++){ const a=col[i-1], b=col[i], nt=noteOf.get(a.id);
      const bottomA = nt ? (()=>{ const g=noteBox(a,nt.text); return g.top+g.h; })() : labelsBelow()? labelTop(a,focusR(a)||RW)+labelLines(a)*lh : a.y+Math.max(RW,labelLines(a)*lh/2);
      const topB = labelsBelow()? b.y-(focusR(b)||RW) : b.y-Math.max(focusR(b)||RW, labelLines(b)*lh/2);
      const need=bottomA+12-topB; if(need>0) for(let j=i;j<col.length;j++) col[j].y+=need; } }
  for(const n of notes){ const st=ex._byId.get(n.step); if(!st) continue; const g=noteBox(st,n.text); n.y=g.top; n.x=g.x==null?st.x-90:g.x; } }
// collapseRuns: contract maximal chains of PASS-THROUGH fotons (exactly one parent + one child within the
// view) into a single "×N steps" edge, so a long lineage shows its SKELETON - sources, merges, forks, and
// the node you're on - not every intermediate step. A node with >1 input (a merge/fan-in) is always kept.
function collapseRuns(nodes, edges, keep, expand){
  expand = expand || new Set();
  const byId=new Map(nodes.map(n=>[n.id,n])), outE={}, inE={};
  for(const e of edges){ (outE[e.from]??=[]).push(e); (inE[e.to]??=[]).push(e); }
  const isPass=id=>{ const n=byId.get(id); return !!n && n.type==="foton" && !keep.has(id) && !expand.has(id)
    && (outE[id]||[]).length===1 && (inE[id]||[]).length===1 && outE[id][0].kind==="builds-on" && inE[id][0].kind==="builds-on"; };
  const pass=new Set(nodes.filter(n=>isPass(n.id)).map(n=>n.id));
  if(!pass.size) return {nodes, edges};
  const uf={}, find=x=>{ while(uf[x]!==x) x=uf[x]=uf[uf[x]]; return x; };
  for(const id of pass) uf[id]=id;
  for(const e of edges){ if(pass.has(e.from)&&pass.has(e.to)) uf[find(e.from)]=find(e.to); }   // one run per connected chain of pass-through nodes
  const groups={}; for(const id of pass) (groups[find(id)]??=[]).push(id);
  const runNode={}, resNodes=nodes.filter(n=>!pass.has(n.id));
  for(const r in groups){ const mem=groups[r], rid="run:"+mem[0]; runNode[r]=rid;
    resNodes.push({id:rid, type:"run", count:mem.length, members:mem, participant:byId.get(mem[0]).participant, role:mem.length+" steps", verified:true}); }
  const rep=id=> pass.has(id) ? runNode[find(id)] : id;
  const seen=new Set(), resEdges=[];
  for(const e of edges){ const f=rep(e.from), t=rep(e.to); if(f===t) continue; const k=f+">"+t+">"+e.kind; if(!seen.has(k)){ seen.add(k); resEdges.push({from:f,to:t,kind:e.kind}); } }
  return { nodes:resNodes, edges:resEdges };
}
// groupRuns: contract each CONNECTED RUN of same-participant nodes into one group node, preserving
// lineage. a-a-b-a-a-b-b -> a-b-a-b (two 'a' runs stay separate because a 'b' sits between them).
// Sources (datasets/code) never group. Union-find over edges between same-participant non-source nodes.
// The contraction moved to core/grouping.js, generalised past `participant`: the compass needs the
// same KEY for grouping lists, and two answers to "which group is this in" would drift. The
// adjacency rule went with it and is the part that matters - contracting a RUN of someone's
// consecutive steps says what happened, while grouping all their work into one blob asserts
// something false.
function groupRuns(nodes, edges){
  return GRP.contract(nodes, edges, GRP.makeKey({by:"attr", name:"participant"}));
}

/** Which placement is in force: the agent's or reader's pick, else a portrait one on a phone, else
 *  whatever the sketch declares as its default. */
function layoutSpec(){
  const cat=LAY.catalogue(SKETCH||{}), want=prof().graph.layout;
  if(!LAYOUT_ID && want && want!=="auto"){ const l=cat.find(l=>l.id===want); if(l) return l; }
  // a model development reads top-down: parent above child
  // ... inside the flow it belongs to: the tree top-down, data in on the left, deliverables out on the right
  if(!LAYOUT_ID && want==="auto" && ex && ex.edges && ex.edges.some(e=>e.step&&e.kind==="parent-step")) return cat.find(l=>l.id==="fluss-baum")||cat.find(l=>l.id==="entwicklung");
  const phone=window.matchMedia&&matchMedia("(max-width:640px)").matches;
  return cat.find(l=>l.id===(LAYOUT_ID||(phone?"hoch":null))) || LAY.defaultLayout(SKETCH||{});
}
function layerLayout(nodes, edges, byId){ const spec=layoutSpec(); if(ex){ ex.ortho=spec.route==="orthogonal"; ex.tb=spec.orient==="tb"; } LAY.makeLayout(spec)(nodes, edges, byId); }
/**
 * What of the canvas the reader can actually SEE.
 *
 * #controls and #panel are absolute overlays ON the canvas, not columns beside it, so the drawable
 * area is the strip between them - and in `anc` the centre sits at layer 0, the far left of the
 * bounding box, which is precisely where the controls sit. Fitting to the full canvas therefore put
 * the one node the reader was sent to look at underneath a panel. Measured with offsetWidth and not
 * getBoundingClientRect: the panel slides in over .18s and a rect read mid-transition is the
 * animating position, not where it lands.
 */
function overlayInsets(cv){
  const ins={l:0,r:0,t:0,b:0}, W=cv.clientWidth, H=cv.clientHeight, gap=10;
  for(const sel of ["#controls","#panel"]){
    const el=$(sel); if(!el) continue;
    if(sel==="#panel" && !el.classList.contains("open")) continue;
    if(el.offsetParent===null) continue;                       // display:none - not covering anything
    const w=el.offsetWidth, h=el.offsetHeight, x=el.offsetLeft, y=el.offsetTop;
    if(!w||!h) continue;
    if(w>=W*0.8){                                              // a full-width band: phones put both this way
      if(y<=H-(y+h)) ins.t=Math.max(ins.t,y+h+gap); else ins.b=Math.max(ins.b,H-y+gap);
    } else {
      if(x<=W-(x+w)) ins.l=Math.max(ins.l,x+w+gap); else ins.r=Math.max(ins.r,W-x+gap);
    }
  }
  return ins;
}
// The rectangles the overlays actually cover. `overlayInsets` gives away a FULL-HEIGHT column for a
// box that sits in one corner - on a wide picture that is most of the canvas surrendered for a
// control panel a hundred pixels tall.
function overlayRects(cv){
  const out=[], W=cv.clientWidth, gap=10;
  for(const sel of ["#controls","#panel"]){
    const el=$(sel); if(!el) continue;
    if(sel==="#panel" && !el.classList.contains("open")) continue;
    if(el.offsetParent===null) continue;
    const w=el.offsetWidth,h=el.offsetHeight; if(!w||!h) continue;
    if(w>=W*0.8) continue;                       // a full-width band: the inset path handles it
    out.push({x0:el.offsetLeft-gap,y0:el.offsetTop-gap,x1:el.offsetLeft+w+gap,y1:el.offsetTop+h+gap});
  }
  return out;
}
function fitView(){ const cv=$("#cv"),DPR=devicePixelRatio||1; cv.width=cv.clientWidth*DPR; cv.height=cv.clientHeight*DPR;
  const ins=overlayInsets(cv);
  // two panels on a narrow window can leave no strip at all; keep a floor rather than a scale of ~0
  const floor=(lo,hi,span)=>{ if(hi-lo>=span*0.34) return [lo,hi]; const m=(lo+hi)/2,r=span*0.17;
    return [Math.max(0,m-r),Math.min(span,m+r)]; };
  if(!ex.nodes.length){ex.view={scale:DPR,tx:cv.width/2,ty:cv.height/2};return;}
  // a node's label runs to its right: the picture is as wide as its longest label reaches, not its last dot
  const lw=n=>{ if(n.type!=="foton") return RW*2; const t=String(stepLabel(n)||""), m=prof().node.label==="description"?40:22,
    px=+prof().node.font||11, w=+prof().node.wrap||0;
    return RW+5+px*0.58*(w?Math.min(t.length,w+2):Math.min(t.length,m+1)); };
  let a=1e9,b=1e9,c=-1e9,d=-1e9;
  if(labelsBelow()) for(const n of ex.nodes){ const h=n.type==="note"?0:(n.type==="foton"?labelLines(n)*(+prof().node.font||11)*1.18+70:24), half=Math.max(RW,(lw(n)-RW)/2);
    a=Math.min(a,n.x-half);c=Math.max(c,n.x+half);b=Math.min(b,n.y-RW);d=Math.max(d,n.y+RW+h); }
  else for(const n of ex.nodes){a=Math.min(a,n.x-RW);c=Math.max(c,n.x+lw(n));b=Math.min(b,n.y-RW);d=Math.max(d,n.y+RW);}
  const gw=(c-a)||1, gh=(d-b)||1;
  // A sparse view may zoom IN: a flat cap of 1.7 is right for hundreds of nodes and turns eleven
  // into a thumbnail in the middle of an empty panel.
  const cap=(ex.nodes.length<=40?9:ex.nodes.length<=120?4:1.7)*DPR;
  // Centre the FOCUS, not the bounding box. The node you asked about belongs in the middle, and it
  // belongs in the SAME middle after a re-aim - otherwise every step costs you your place.
  const f=ex.center && ex._byId && ex._byId.get(ex.center);
  const place=(x0,x1,y0,y1)=>{ const pad=Math.min(90*DPR,(x1-x0)*0.12,(y1-y0)*0.12);
    const sc=Math.min(((x1-x0)-2*pad)/gw,((y1-y0)-2*pad)/gh,cap);
    // the focus in the middle - unless that pushes part of the picture out of the strip (a focus at
    // one end of a flow, or the deepest node of a tree): the scale was chosen for the whole picture,
    // so then the whole picture wins
    const fitsX=f&&Math.abs(f.x-(a+c)/2)*sc+gw*sc/2<=((x1-x0)-2*pad)/2+1;
    const fitsY=f&&Math.abs(f.y-(b+d)/2)*sc+gh*sc/2<=((y1-y0)-2*pad)/2+1;
    const cx=fitsX?f.x:(a+c)/2, cy=fitsY?f.y:(b+d)/2;
    return {scale:sc,tx:(x0+x1)/2-sc*cx,ty:(y0+y1)/2-sc*cy}; };
  const hits=(v)=>{ const bx0=(v.tx+v.scale*a)/DPR-14,bx1=(v.tx+v.scale*c)/DPR+14,
                          by0=(v.ty+v.scale*b)/DPR-14,by1=(v.ty+v.scale*d)/DPR+14;
    return overlayRects(cv).some(r=>bx0<r.x1&&bx1>r.x0&&by0<r.y1&&by1>r.y0); };
  // Take the WHOLE canvas unless the picture would actually run under an overlay. Measured, not
  // assumed: a band of nodes across the middle never touches a box in the corner, and used to be
  // squeezed into two thirds of the width for nothing.
  const full=place(0,cv.width,0,cv.height);
  if(!hits(full)){ ex.view=full; return; }
  let x0=ins.l*DPR,x1=cv.width-ins.r*DPR,y0=ins.t*DPR,y1=cv.height-ins.b*DPR;
  [x0,x1]=floor(x0,x1,cv.width); [y0,y1]=floor(y0,y1,cv.height);
  ex.view=place(x0,x1,y0,y1);
}
const RW=8;
// The focus is drawn at a fixed size ON SCREEN, not in world units. Everything that hangs on it -
// the mark counter, the label, the action menu next - then always has the same size and the same
// place to reach for, whatever the zoom is. A control that changes size with the zoom is a control
// you have to aim at twice.
const FOCUS_PX=26;
// FOCUS_PX on screen, but held between 1.5x and 2.2x a normal node in world units: divided by the
// zoom alone, a fit view of a large graph made the centre dwarf everything around it
const focusR=(n)=>(ex && n.id===ex.center) ? Math.min(Math.max(FOCUS_PX/(ex.view?ex.view.scale:1), RW*1.5), RW*2.2) : null;
function rrect(c,x,y,w,h,r){c.beginPath();c.moveTo(x+r,y);c.arcTo(x+w,y,x+w,y+h,r);c.arcTo(x+w,y+h,x,y+h,r);c.arcTo(x,y+h,x,y,r);c.arcTo(x,y,x+w,y,r);c.closePath();}
function redraw(){ const cv=$("#cv"),ctx=cv.getContext("2d"),v=ex.view;
  ctx.setTransform(1,0,0,1,0,0); ctx.clearRect(0,0,cv.width,cv.height); ctx.setTransform(v.scale,0,0,v.scale,v.tx,v.ty);
  LOD = ex.nodes.length>220;                                       // level-of-detail: above this, drop labels + cull off-screen
  const M=70, wx0=(0-v.tx)/v.scale-M, wy0=(0-v.ty)/v.scale-M, wx1=(cv.width-v.tx)/v.scale+M, wy1=(cv.height-v.ty)/v.scale+M;
  const vis=n=>n&&n.x>=wx0&&n.x<=wx1&&n.y>=wy0&&n.y<=wy1;
  // two kinds of line, told apart by their shape: a relation between steps (based on, developed
  // from) runs Manhattan style, the lineage (data flow, and what is read from it) runs as a curve
  for(const e of ex.edges){ const f=ex._byId.get(e.from),t=ex._byId.get(e.to); if(!f||!t)continue;
    if(LOD&&!vis(f)&&!vis(t)) continue;                            // skip edges wholly off-screen
    const [L,R]= f._d>=t._d?[t,f]:[f,t]; const rel=e.kind!=="builds-on";
    // a derived edge is a READING, and must never look like an attestation: dotted, dimmer, named
    if(e.derived){ edgeArrow(ctx,L,R,css("--dim"),"dot",LOD?"":shortRel(e.kind),"curve"); continue; }
    // developed from (strong) / adapted from (light): a relation between two steps, from a verified claim
    if(e.step&&e.kind==="parent-step"&&f.y>t.y+RW*3){ manhattan(ctx,t,f,css("--b"),LOD?"":e.label); continue; }
    if(e.step){ edgeArrow(ctx,L,R,e.kind==="parent-step"?css("--b"):(e.kind==="reproduces"||e.kind==="review-valid")?css("--ok"):e.kind==="review-invalid"?css("--bad"):css("--dim"),e.kind==="based-on",LOD?"":e.label,(e.kind==="based-on"||e.kind==="parent-step")?"ortho":undefined); continue; }
    if(e.contracted){ edgeArrow(ctx,L,R,css("--about"),true,LOD?"":shortRel(e.kind),"curve"); continue; }
    edgeArrow(ctx,L,R,rel?css("--about"):css("--edge"),rel,(!LOD&&e.kind!=="builds-on"&&e.kind!=="about")?e.kind:"","curve"); }
  for(const n of ex.nodes){ if(LOD&&!vis(n)) continue; nodeDraw(ctx,n); }
}
const shortRel=(t)=>{ const i=Math.max(String(t).lastIndexOf(":"),String(t).lastIndexOf("/"),String(t).lastIndexOf("#")); return i<0?t:String(t).slice(i+1); };
// route: "ortho" or "curve" for this edge; without it the layout's route
function edgeArrow(ctx,a,b,color,dash,lbl,route){
  const orth=route==="ortho"?true:route==="curve"?false:!!(ex&&ex.ortho);
  // a top-down layout stacks the pair: leave the bottom of the upper node, enter the top of the lower
  if((orth&&ex&&ex.tb&&Math.abs(b.y-a.y)>RW*3)||Math.abs(b.y-a.y)>Math.abs(b.x-a.x)){ const s=b.y>a.y?1:-1, x1=a.x,y1=a.y+s*RW,x2=b.x,y2=b.y-s*(RW+4),my=(y1+y2)/2;
    ctx.strokeStyle=color;ctx.lineWidth=dash==="dot"?1:1.3;ctx.setLineDash(dash==="dot"?[2,3]:dash?[5,4]:[]);ctx.beginPath();ctx.moveTo(x1,y1);
    // an orthogonal layout: down, across, down - the across in the gap between the two rows
    if(orth){ ctx.lineTo(x1,my);ctx.lineTo(x2,my);ctx.lineTo(x2,y2); } else ctx.bezierCurveTo(x1,my,x2,my,x2,y2);
    ctx.stroke();ctx.setLineDash([]);
    const ang=Math.atan2(y2-my,x2-x2)||s*Math.PI/2,h=7;ctx.fillStyle=color;ctx.beginPath();ctx.moveTo(x2,y2);ctx.lineTo(x2-h*Math.cos(ang-.4),y2-h*Math.sin(ang-.4));ctx.lineTo(x2-h*Math.cos(ang+.4),y2-h*Math.sin(ang+.4));ctx.fill();
    if(lbl){ ctx.fillStyle=color; ctx.font="italic 9px ui-sans-serif"; ctx.textBaseline="middle";
      // one column (a rerun under its run): the right side holds the labels and notes, so the left
      if(Math.abs(x2-x1)<1){ ctx.textAlign="right"; ctx.fillText(lbl,x1-6,my); } else { ctx.textAlign="left"; ctx.fillText(lbl,(x1+x2)/2+6,my); } } return; }
  const x1=a.x+RW,y1=a.y,x2=b.x-RW-4,y2=b.y,mx=(x1+x2)/2;
  ctx.strokeStyle=color;ctx.lineWidth=dash==="dot"?1:1.3;ctx.setLineDash(dash==="dot"?[2,3]:dash?[5,4]:[]);ctx.beginPath();ctx.moveTo(x1,y1);
  // an orthogonal layout: across, up or down, across - the turn halfway between the two columns
  let tx=mx;
  if(orth){ tx=orthoTurn(a,b,x1,y1,x2,y2); ctx.lineTo(tx,y1);ctx.lineTo(tx,y2);ctx.lineTo(x2,y2); } else ctx.bezierCurveTo(mx,y1,mx,y2,x2,y2);
  ctx.stroke();ctx.setLineDash([]);
  const ang=Math.atan2(y2-y1,x2-mx),h=7;ctx.fillStyle=color;ctx.beginPath();ctx.moveTo(x2,y2);ctx.lineTo(x2-h*Math.cos(ang-.4),y2-h*Math.sin(ang-.4));ctx.lineTo(x2-h*Math.cos(ang+.4),y2-h*Math.sin(ang+.4));ctx.fill();
  // the label sits below the line, two thirds along: the source node's own label runs along the line's start
  if(lbl){ ctx.fillStyle=color; ctx.font="italic 9px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="top"; if(orth) ctx.fillText(lbl,(tx+x2)/2,y2+7); else ctx.fillText(lbl,x1+(x2-x1)*0.68,y1+(y2-y1)*0.68+7); } }
// where an across-up-across edge turns: just after the source, halfway, or just before the target -
// whichever runs through the fewest other nodes and their labels (a label hangs under its node here)
function orthoTurn(a,b,x1,y1,x2,y2){
  const boxes=[]; const f=+prof().node.font||11, below=labelsBelow();
  const halfOf=n=>n.type==="foton"?Math.max(RW+4,(+prof().node.wrap||22)*f*0.29):RW+4;
  for(const n of ex.nodes){ if(n.type==="note") continue; const half=halfOf(n), lab=below&&n.type==="foton";
    // the two ends' own dots are where the line starts and ends; their labels still count
    if(n===a||n===b){ if(lab) boxes.push([n.x-half,labelTop(n,focusR(n)||RW)-2,n.x+half,labelTop(n,focusR(n)||RW)+labelLines(n)*f*1.18+50]); continue; }
    boxes.push([n.x-half,n.y-RW-4,n.x+half,n.y+RW+(lab?labelLines(n)*f*1.18+60:4)]); }
  const hits=(X0,Y0,X1,Y1)=>{ const [l,r]=X0<X1?[X0,X1]:[X1,X0], [t,u]=Y0<Y1?[Y0,Y1]:[Y1,Y0]; let k=0;
    for(const q of boxes) if(r>=q[0]&&l<=q[2]&&u>=q[1]&&t<=q[3]) k++; return k; };
  let best=(x1+x2)/2, bk=1e9;
  const hb=below&&b.type==="foton"?halfOf(b):0, ha=below&&a.type==="foton"?halfOf(a):0;
  for(const t of [x2-22,(x1+x2)/2,x1+22,b.x-hb-14,a.x+ha+14].filter(t=>t>x1&&t<x2)){ const k=hits(x1,y1,t,y1)+hits(t,y1,t,y2)+hits(t,y2,x2,y2); if(k<bk){ bk=k; best=t; } }
  return best; }
// A mark strip: one dot per claim standing about this node, drawn at a constant place so a column
// of nodes stays scannable. The count, not the content - the content is one click away.
// What to call a node. A foton's own label is its command - right for a machine, useless for a
// reader: four refinements that all run model.R are four identical boxes. The claims name their
// subject, and a `base`/`final`/`key` says which of them to look at first. Both come out of the
// corpus; neither is invented here.
function nodeName(n){
  const c=(VIEW==="contracted"&&CONTRACTED)||null;
  const nm=c&&c.names&&c.names[n.id];
  const role=c&&c.roles&&c.roles[n.id];
  if(!nm) return `${(n.participant||"").replace('pfed-','')} · ${n.role}`;
  const short=String(nm).split("/").pop();
  return role ? `${short} · ${role}` : short;
}
function isLandmark(n){ const c=(VIEW==="contracted"&&CONTRACTED)||null; return !!(c&&c.roles&&c.roles[n.id]); }
function marksOf(id){ return (VIEW==="contracted" && CONTRACTED && CONTRACTED.marks && CONTRACTED.marks[id]) || []; }
// key / base / final as small chips under a step, and a checkmark when it passed QC - read from
// verified claims (core/annotate.js), in every view
function stepBadges(ctx,n,r){ const a=ANN.get(n.id); if(!a) return;
  if(a.qc&&a.qc.length){ const bx=n.x-r*0.78,by=n.y-r*0.78; ctx.fillStyle=css("--ok"); ctx.beginPath(); ctx.arc(bx,by,7.5,0,7); ctx.fill();
    ctx.strokeStyle=css("--panel"); ctx.lineWidth=1.5; ctx.stroke(); ctx.fillStyle="#0b0f18"; ctx.font="bold 10px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle"; ctx.fillText("✓",bx,by+0.5); }
  const kinds=[...new Set((a.marks||[]).map(m=>m.kind))]; if(!kinds.length||LOD) return;
  ctx.font="bold 8px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle";
  const ws=kinds.map(k=>ctx.measureText(k.toUpperCase()).width+8), tot=ws.reduce((s,w)=>s+w+3,-3); let x=n.x-tot/2; const y=n.y+r+9;
  kinds.forEach((k,i)=>{ const w=ws[i]; ctx.fillStyle=k==="final"?css("--ok"):k==="base"?css("--a"):k==="not followed"?"#8a93a6":css("--b"); rrect(ctx,x,y-6,w,12,6); ctx.fill();
    ctx.fillStyle="#0b0f18"; ctx.fillText(k.toUpperCase(),x+w/2,y+0.5); x+=w+3; }); }
// the same, as text: panel and hover
function stepMarksText(n){ const a=ANN.get(n.id)||{}; const out=[];
  for(const m of (a.marks||[])) out.push(`<span class="mk mk-${esc(m.kind)}">${esc(m.kind)}</span>${m.value&&m.value!==m.kind?` ${esc(m.value)}`:``}`);
  for(const q of (a.qc||[])) out.push(`<span class="mk mk-qc">✓ QC</span> ${esc(q.by)}${q.value?`: ${esc(q.value)}`:``}`);
  return out.length?`<div class="marks">${out.join("<br>")}</div>`:``; }
function markStrip(ctx,n,r){
  const m=marksOf(n.id); if(!m.length) return;
  ctx.fillStyle=css("--ok"); ctx.font="bold 8.5px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle";
  const bx=n.x-r-7, by=n.y-r-2;
  ctx.beginPath(); ctx.arc(bx,by,7,0,7); ctx.fill();
  ctx.fillStyle="#0b0f18"; ctx.fillText(String(m.length),bx,by);
}
// an orthogonal left-to-right layout runs its lines through the nodes' rows: the label goes UNDER the
// node there, so no line crosses it (labelsBelow), and a step's note goes under the label
const labelsBelow=()=>!!(ex&&ex.ortho&&!ex.tb);
function labelLines(n){ const step=n.type==="foton", w=step?(+prof().node.wrap||0):0; return w?Math.min(3,wrapText(stepLabel(n),w,3).length):1; }
function labelTop(n,r){ const a=ANN.get(n.id); return n.y+r+5+(a&&a.marks&&a.marks.length?16:0); }
function label(ctx,n,txt,r,max=22){ ctx.fillStyle=css("--ink");ctx.textAlign="left";ctx.textBaseline="middle";
  const step=n.type==="foton", px=step?(+prof().node.font||11):11, w=step?(+prof().node.wrap||0):0;
  ctx.font=px+"px ui-sans-serif,system-ui";
  if(labelsBelow()){ const lines=w?wrapText(txt,w,3):[clip(txt,max)], lh=px*1.18, y0=labelTop(n,r); ctx.textAlign="center"; ctx.textBaseline="top";
    lines.forEach((l,i)=>ctx.fillText(l,n.x,y0+i*lh)); return; }
  if(!w){ ctx.fillText(clip(txt,max),n.x+r+5,n.y); return; }
  const lines=wrapText(txt,w,3), lh=px*1.18; lines.forEach((l,i)=>ctx.fillText(l,n.x+r+5,n.y+(i-(lines.length-1)/2)*lh)); }
// a step's name on the canvas, by node.label: "description" is what the modeler wrote about it (a
// verified dc:description), falling back to the command when nobody described it
function stepLabel(n){ const m=prof().node.label;
  if(m==="description"){ const s=stepInfo(n); return s.description||s.command; }
  return m==="command"?stepInfo(n).command:nodeName(n); }
function noteDraw(ctx,n){ const s=ex._byId.get(n.step);
  const lines=wrapText(n.text,34,3); ctx.font="italic 10px ui-sans-serif,system-ui"; const w=Math.max(...lines.map(l=>ctx.measureText(l).width))+10, h=lines.length*12+6;
  if(s){ const g=noteBox(s,n.text); n.y=g.top; n.x=g.x==null?s.x-w/2:g.x; }
  ctx.fillStyle="rgba(245,182,66,.10)"; ctx.strokeStyle="rgba(245,182,66,.55)"; ctx.lineWidth=1; rrect(ctx,n.x,n.y,w,h,4); ctx.fill(); ctx.stroke();
  ctx.fillStyle=css("--b"); ctx.textAlign="left"; ctx.textBaseline="top"; lines.forEach((l,i)=>ctx.fillText(l,n.x+5,n.y+4+i*12)); }
function wrapText(t,w,max){ const words=String(t||"").split(/\s+/), out=[]; let cur="";
  for(const x of words){ if((cur+" "+x).trim().length>w&&cur){ out.push(cur); cur=x; } else cur=(cur+" "+x).trim(); }
  if(cur) out.push(cur); if(out.length>max){ out.length=max; out[max-1]=clip(out[max-1],w-1)+"…"; } return out; }
function nodeDraw(ctx,n){ if(n.type==="note") return noteDraw(ctx,n); const sel=n.id===ex.center;
  if(SELSET.has(n.id)){ ctx.strokeStyle=css("--b"); ctx.lineWidth=3.2/(ex.view?ex.view.scale:1); ctx.setLineDash([]);   // in the multi-selection
    ctx.beginPath(); ctx.arc(n.x,n.y,(focusR(n)||RW)*1.75,0,7); ctx.stroke(); }
  if(n.id===ex.sel){ const rr=focusR(n); ctx.strokeStyle=css("--accent"); ctx.lineWidth=2/(ex.view?ex.view.scale:1); ctx.beginPath(); ctx.arc(n.x,n.y,(rr||RW)*(rr?1.5:2.1),0,7); ctx.stroke(); }
  if(n.type==="group"){ ctx.fillStyle=pcol(n.participant); ctx.strokeStyle=n.verified?css("--ok"):css("--bad"); ctx.lineWidth=sel?3:2.2;
    const w=RW*2.7,h=RW*1.9; rrect(ctx,n.x-w/2-3,n.y-h/2-3,w,h,5); ctx.stroke(); rrect(ctx,n.x-w/2,n.y-h/2,w,h,5); ctx.fill(); ctx.stroke();
    ctx.fillStyle="#0b0f18"; ctx.font="700 11px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle"; ctx.fillText(n.count,n.x,n.y);
    label(ctx,n,`${n.participant.replace('pfed-','')} · ${n.role}`,w/2+3); return; }
  if(n.type==="ref"){ ctx.strokeStyle="#8b97ad"; ctx.lineWidth=sel?3:1.4; ctx.setLineDash([3,3]);
    ctx.beginPath();ctx.arc(n.x,n.y,RW*0.9,0,7);ctx.stroke();ctx.setLineDash([]);
    label(ctx,n,n.role,RW*0.9); return; }
  if(n.type==="run"){ const w=RW*3.0,h=RW*1.5; ctx.fillStyle="#161d2b"; ctx.strokeStyle=css("--dim"); ctx.lineWidth=sel?2.4:1.3; ctx.setLineDash([3,3]);   // collapsed linear run - click to expand
    rrect(ctx,n.x-w/2,n.y-h/2,w,h,7); ctx.fill(); ctx.stroke(); ctx.setLineDash([]);
    ctx.fillStyle=css("--dim"); ctx.font="10px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle"; ctx.fillText("⋯ "+n.count+" steps",n.x,n.y);
    if(!LOD) label(ctx,n,"click to expand",w/2+2); return; }
  if(n.type==="source"){ const data=n.kind==="dataset"||n.kind==="deliverable"; ctx.fillStyle=data?"#cfd8e6":"#455066";ctx.strokeStyle="#6b7688";ctx.lineWidth=sel?3:1.4;
    const r=data?RW*1.15:RW*0.7;ctx.beginPath();ctx.arc(n.x,n.y,r,0,7);ctx.fill();ctx.stroke(); if(data||sel)label(ctx,n,n.role,r); return; }
  ctx.fillStyle=pcol(n.participant);ctx.strokeStyle=n.verified?css("--ok"):css("--bad");ctx.lineWidth=sel?3:2;
  const rFix=focusR(n);
  // recorded only: dashed rim - the recorder saw it, nobody signed it
  // a branch the modeler abandoned (pmx not-followed) stays in the picture, but faded
  const dropped=n.type==="foton"&&((ANN.get(n.id)||{}).marks||[]).some(m=>m.kind==="not followed"); if(dropped) ctx.globalAlpha=0.38;
  if(n.type==="foton"){const r=rFix||(isLandmark(n)?RW*1.45:RW);ctx.beginPath();ctx.arc(n.x,n.y,r,0,7);ctx.fill();if(n.origin==="recorded")ctx.setLineDash([4,3]);ctx.stroke();ctx.setLineDash([]);}else{const w=RW*1.7;rrect(ctx,n.x-w/2,n.y-w/2,w,w,3);ctx.fill();ctx.stroke();}
  if(dropped) ctx.globalAlpha=1;
  markStrip(ctx,n,rFix||RW);
  if(n.type==="foton") stepBadges(ctx,n,rFix||RW);
  if(n.type==="foton"&&n.repro>1){ const bx=n.x+RW*0.78,by=n.y-RW*0.78; ctx.fillStyle=css("--ok"); ctx.beginPath(); ctx.arc(bx,by,7.5,0,7); ctx.fill(); ctx.strokeStyle=css("--panel"); ctx.lineWidth=1.5; ctx.stroke(); ctx.fillStyle="#0b0f18"; ctx.font="bold 8.5px ui-sans-serif"; ctx.textAlign="center"; ctx.textBaseline="middle"; ctx.fillText("↻"+n.repro,bx,by); }  // ↻N = distinct signers that produced this output (native from graph.wasm)
  // claims: append the assertion (from the WASM's rendered object text) so verdicts/roles read off the graph
  const val=(n.type==="claim"&&n.text)?": "+clip(n.text,16):"";
  // a landmark keeps its name even when the picture is too dense for labels: that is the whole
  // point of one - it outranks distance and density both
  if(!LOD||n.id===ex.center||n.id===ex.sel||isLandmark(n))
    label(ctx,n,n.type==="foton"?stepLabel(n):`${(n.participant||"").replace('pfed-','')} · ${n.role}${val}`,rFix||RW,
          n.type==="foton"&&prof().node.label==="description"?40:22);
}
const CLICK_SLOP=4;   // screen px a mouse may travel between down and up and still be a click
// The canvas follows its box. Its backing store is set by fitView; when the box changes size later
// (an editor tab dragged wider, a split resized) the picture would stretch and every click would map
// to the wrong place. Resize the store, keep the reader's zoom, keep the same point in the middle.
let CANVAS_RO=null;
function followCanvasSize(cv){
  if(CANVAS_RO || typeof ResizeObserver==="undefined") return;
  CANVAS_RO=new ResizeObserver(()=>{ const DPR=devicePixelRatio||1, w=Math.round(cv.clientWidth*DPR), h=Math.round(cv.clientHeight*DPR);
    if(!w || !h || (w===cv.width && h===cv.height)) return;
    if(ex && ex.view){ ex.view.tx+=(w-cv.width)/2; ex.view.ty+=(h-cv.height)/2; }
    cv.width=w; cv.height=h; if(ex && ex.nodes) redraw(); });
  CANVAS_RO.observe(cv);
}
function startRender(){ ex.started=true; const cv=$("#cv"),DPR=devicePixelRatio||1; followCanvasSize(cv);
  const s2w=(sx,sy)=>[(sx*DPR-ex.view.tx)/ex.view.scale,(sy*DPR-ex.view.ty)/ex.view.scale];
  const hit=(sx,sy)=>{const [wx,wy]=s2w(sx,sy);const n=ex.nodes.find(n=>n.type!=="note"&&Math.hypot(n.x-wx,n.y-wy)<RW*1.7);return n;};
  let drag=null,pan=null;
  cv.onwheel=e=>{ e.preventDefault(); $("#hint")?.classList.add("gone"); const f=e.deltaY<0?1.12:1/1.12,mx=e.offsetX*DPR,my=e.offsetY*DPR;
    const wx=(mx-ex.view.tx)/ex.view.scale,wy=(my-ex.view.ty)/ex.view.scale; ex.view.scale*=f; ex.view.tx=mx-wx*ex.view.scale; ex.view.ty=my-wy*ex.view.scale; redraw(); };
  cv.onmousedown=e=>{ $("#hint")?.classList.add("gone"); const n=hit(e.offsetX,e.offsetY); if(n){drag={n,moved:false,sx:e.offsetX,sy:e.offsetY};} else {pan={x:e.offsetX*DPR,y:e.offsetY*DPR,tx:ex.view.tx,ty:ex.view.ty};cv.style.cursor="grabbing";} };
  // a hand or a touchpad moves a pixel or two between down and up: below CLICK_SLOP it is still a
  // click that selects, not a drag (the touch path below does the same with a wider margin)
  cv.onmousemove=e=>{ if(drag){ if(!drag.moved && Math.hypot(e.offsetX-drag.sx,e.offsetY-drag.sy)<=CLICK_SLOP) return;
      const [wx,wy]=s2w(e.offsetX,e.offsetY);drag.n.x=wx;drag.n.y=wy;drag.moved=true;redraw();}
    else if(pan){ex.view.tx=pan.tx+(e.offsetX*DPR-pan.x);ex.view.ty=pan.ty+(e.offsetY*DPR-pan.y);redraw();}
    else{ const hn=hit(e.offsetX,e.offsetY); cv.style.cursor=hn?"pointer":"grab"; showTip(hn,e.offsetX,e.offsetY); } };
  cv.onmouseleave=()=>showTip(null);
  window.onmouseup=(e)=>{ if(drag&&!drag.moved){ const nd=drag.n;
      // multi-select on: ctrl/⌘- or shift-click adds or removes a step and leaves the panel alone
      if(prof().select.multi && nd.type==="foton" && e && (e.ctrlKey||e.metaKey||e.shiftKey)){ setSelection(SELM.toggle(SELSET, nd.id)); }
      else if(nd.type==="run"){ (ex.expand||(ex.expand=new Set())); nd.members.forEach(m=>ex.expand.add(m)); layoutAndRun(); }   // expand this collapsed run in place
      else { ex.sel=nd.id; redraw(); inspect(nd.id); } }
    if(pan)cv.style.cursor="grab"; drag=null;pan=null; };
  cv.ondblclick=e=>{ const n=hit(e.offsetX,e.offsetY); if(n){recentre(n.id);} };
  // --- touch: one finger pans (or drags a node); two fingers pinch-zoom. Mirrors the mouse logic. ---
  const toff=t=>{ const r=cv.getBoundingClientRect(); return [t.clientX-r.left, t.clientY-r.top]; };
  let pinch=null;
  cv.addEventListener("touchstart",e=>{ e.preventDefault(); $("#hint")?.classList.add("gone");
    if(e.touches.length===1){ const [ox,oy]=toff(e.touches[0]); const n=hit(ox,oy);
      if(n){drag={n,moved:false,sx:ox,sy:oy};} else {pan={x:ox*DPR,y:oy*DPR,tx:ex.view.tx,ty:ex.view.ty};} pinch=null; }
    else if(e.touches.length>=2){ const [ax,ay]=toff(e.touches[0]),[bx,by]=toff(e.touches[1]);
      pinch={d:Math.hypot(ax-bx,ay-by)||1, scale:ex.view.scale, tx:ex.view.tx, ty:ex.view.ty}; drag=null; pan=null; } },{passive:false});
  cv.addEventListener("touchmove",e=>{ e.preventDefault();
    if(pinch && e.touches.length>=2){ const [ax,ay]=toff(e.touches[0]),[bx,by]=toff(e.touches[1]);
      const nd=Math.hypot(ax-bx,ay-by), mx=(ax+bx)/2*DPR, my=(ay+by)/2*DPR;
      const wx=(mx-pinch.tx)/pinch.scale, wy=(my-pinch.ty)/pinch.scale;   // world point under the pinch centre
      ex.view.scale=pinch.scale*(nd/pinch.d); ex.view.tx=mx-wx*ex.view.scale; ex.view.ty=my-wy*ex.view.scale; redraw(); }
    else if(e.touches.length===1){ const [ox,oy]=toff(e.touches[0]);
      // only DRAG a node once the finger has actually travelled (>10px); below that it stays a TAP so
      // touchend can select the node (mobile taps jitter, which otherwise flips every tap into a drag).
      if(drag){ if(drag.moved||Math.hypot(ox-drag.sx,oy-drag.sy)>10){ const [wx,wy]=s2w(ox,oy);drag.n.x=wx;drag.n.y=wy;drag.moved=true;redraw(); } }
      else if(pan){ex.view.tx=pan.tx+(ox*DPR-pan.x);ex.view.ty=pan.ty+(oy*DPR-pan.y);redraw();} } },{passive:false});
  cv.addEventListener("touchend",e=>{ if(drag&&!drag.moved){ const nd=drag.n;
      if(nd.type==="run"){ (ex.expand||(ex.expand=new Set())); nd.members.forEach(m=>ex.expand.add(m)); layoutAndRun(); }
      else { ex.sel=nd.id; redraw(); inspect(nd.id); } }
    if(e.touches.length<2) pinch=null; if(e.touches.length===0){ drag=null; pan=null; } });
}
function recentre(id, push=true){ ex.center=id; ex.sel=id; ex.cap=PED_CAP; ex.note=""; ex.expand=new Set(); drawControls(); layoutAndRun(); inspect(id); fitView(); redraw();
  if(push) history.pushState({view:'explorer',center:id,mode:ex.mode},""); }
function fileRow(f,isOut,nodeId){ const PP=prof().panel, uris=PP.file?[...new Set([...(LOC[f.hash]||[]), ...((G.locators||{})[f.hash]||[])])]:[], isNode=G.byId.has(f.hash);
  // host-provided actions (prof().actions): the viewer offers them, the host performs them
  const acts=prof().actions.filter(a=>a.on==="both"||a.on===(isOut?"output":"input"))
    .map(a=>`<span class="rc fact" data-fileact="${esc(a.id)}" data-h="${esc(f.hash)}" data-n="${esc(f.name||"")}" data-io="${isOut?"output":"input"}" data-node="${esc(nodeId||"")}">${esc(a.label)}</span>`).join("");
  return `<div class="frow ${isNode?'nav':''}" ${isNode?`data-id="${esc(f.hash)}"`:``}>${esc(f.name||"(unnamed)")}${PP.hashes?` <span class="m">${esc(f.hash)}</span>`:``}${acts?`<div class="facts">${acts}</div>`:``}`+
    (uris.length?`<span class="rc pvrow${isOut?" pvout":""}" data-h="${esc(f.hash)}" data-u="${encodeURIComponent(uris[0])}">⤢ preview</span>`:``)+
    uris.map(u=>safeUri(u)?`<a class="uri" href="${esc(safeUri(u))}" target="_blank" rel="noopener">↗ ${clipe(u,46)}</a>`:`<span class="uri m">↗ ${clipe(u,46)} (unsafe uri)</span>`).join("")+`</div>`+
    (uris.length?`<div class="frow-prev" id="pv-${esc(f.hash)}"></div>`:``); }
// The claims that stand ABOUT a node, as cards. In the contracted view they are not boxes on the
// canvas any more - which is right, they are not steps - but they must still be readable, or
// contracting them is just hiding them. Term, who, when, why, and what was actually said.
function marksCard(id){
  const ms=marksOf(id);
  if(!ms.length) return "";
  const val=(m)=>{
    const b=m.body||m.object;
    if(b==null) return "";
    if(typeof b!=="object") return esc(String(b));
    const parts=Object.entries(b).filter(([k])=>k!=="@context")
      .map(([k,v])=>`${esc(k)}: ${esc(typeof v==="object"?JSON.stringify(v).slice(0,60):String(v))}`);
    return parts.join(" · ");
  };
  return `<h4>Aussagen (${ms.length})</h4>`+ms.map((m)=>`
    <div class="mcard">
      <div class="mterm">${esc(shortRel(m.term||""))}</div>
      ${val(m)?`<div class="mval">${val(m)}</div>`:``}
      ${m.why?`<div class="mwhy">„${esc(clip(String(m.why),160))}"</div>`:``}
      <div class="mby">${esc(m.by||"")}${m.when?` · ${esc(String(m.when).slice(0,10))}`:``}</div>
    </div>`).join("");
}
function openNode(id){ const p=$("#panel"); const gn=ex&&ex._byId&&ex._byId.get(id);
  if(gn&&gn.type==="group"){
    p.innerHTML=`<span class="x" data-closepanel="1">×</span>
      <div class="who" style="color:${pcol(gn.participant)}">${esc(gn.participant)}</div>
      <div class="role">group · ${esc(gn.count)} same-participant records in a run</div>
      <span class="rc" data-ungroup="1">▣ ungroup</span>
      <h4>records (${esc(gn.count)})</h4>${gn.members.map(m=>`<div class="nav" data-id="${esc(m.id)}"><span class="d" style="background:${pcol(m.participant)}"></span>${esc(m.type)} · ${esc(m.role)} <span style="color:var(--dim)">${m.verified?'✓':''}</span></div>`).join("")}`;
    p.querySelectorAll(".nav[data-id]").forEach(el=>el.onclick=()=>{ex.grouped=false;ex.sel=el.dataset.id;drawControls();layoutAndRun();openNode(el.dataset.id);});
    p.classList.add("open"); return;
  }
  const n=G.byId.get(id),ins=(G.in[id]||[]),outs=(G.out[id]||[]),isSrc=n.type==="source"||n.type==="ref";
  const lk=(o,kind)=>{const c=o.type==="source"?"#cfd8e6":pcol(o.participant);
    return `<div class="nav" data-id="${esc(o.id)}"><span class="d" style="background:${c}"></span>${o.type==="source"?esc(o.role):idlabel(o.participant)+" · "+esc(o.role)} <span style="color:var(--dim)">(${esc(kind)})</span></div>`;};
  const link=(e)=>lk(G.byId.get(e.from),e.kind);
  const uris=[...new Set([...(LOC[n.id]||[]), ...((G.locators||{})[n.id]||[])])];
  const origin=n.origin==="both"?` <span class="badge vok" title="a recorded step produced the same file">● recorded too</span>`
    :n.origin==="signed"?` <span class="badge" title="no recorded step produced this file">signed only</span>`:``;
  const meta=isSrc?`<div class="kv">external input - referenced by content hash; not a signed record.</div>`
    :n.origin==="recorded"?`<div class="kv"><span class="badge vno" title="the recorder saw this step; no signed record has its outputs">recorded only - not signed</span></div>`
    :`<div class="kv">${origin?origin.trim()+"<br>":""}signer <code>${esc(n.signer||"?")}</code> <span class="badge ${n.verified?'vok':'vno'}">${n.verified?"✓ verified":n.pkgSigned?`signed by admitted package ${esc(n.pkgSigned)}${n.pkgChecked?", signature checked":""} (not a trust tier)`:n.untrusted?"signer not in the trusted keys":"unverified"}</span>${n.repro>1?` <span class="badge vok" title="distinct independent signers that produced this exact output (↻N)">↻${n.repro} reproduced</span>`:``}${n.signers?`<br>co-signed by ${n.signers.map(k=>`<code>${esc((G.nodes.find(x=>x.signer===k&&x.participant)||{}).participant||k)}</code>`).join(", ")} - the same work, re-run and signed on this one record`:``}</div>`;
  const qOf=id=>QS.BUILTIN.find(q=>q.id===id);   // counts on the buttons - same questions the compass asks
  const linN=n.type==="foton"?QS.countFor(G,qOf("inputs"),n.id):0, impN=n.type==="foton"?QS.countFor(G,qOf("impact"),n.id):0;
  const PP=prof().panel, an=stepInfo(n);
  const title=(PP.title==="command"&&n.type==="foton")
    ?`<div class="cmd" title="the step's command">${esc(an.command)}</div>`
    :`<div class="who" style="color:${isSrc?'#cfd8e6':pcol(n.participant)}">${isSrc?esc(n.role):idlabel(n.participant)}</div>
    <div class="role">${isSrc?("source · "+esc(n.kind)):(esc(n.type)+" · "+esc(n.role))}</div>`;
  const rels=RELS.filter(r=>r.from===n.id).map(r=>{ const t=G.byId.get(r.to); return `<div class="nav rel" data-id="${esc(r.to)}">${esc(r.label)} <b>${esc(t?stepInfo(t).command:r.to)}</b></div>`; }).join("");
  const why=(PP.annotations&&(an.description||an.rationale||ANN.get(n.id)||rels))
    ?`<div class="why">${an.description?`<div class="desc">${esc(an.description)}</div>`:``}${an.rationale?`<div class="rat"><b>why</b> ${esc(an.rationale)}</div>`:``}${n.type==="foton"?stepMarksText(n):``}${rels}</div>`:``;
  p.innerHTML=`<span class="x" data-closepanel="1">×</span>
    ${title}${why}
    ${(PP.navigate&&n.id!==ex.center)?`<span class="rc" data-recentre="${esc(n.id)}">⌖ re-centre pedigree here</span>`:``}
    ${(STORE&&(n.type==="foton"||n.type==="source")&&n.origin!=="recorded")?`<span class="rc" data-storeexpand="${esc(n.id)}" title="ask the store what this was built from and what was built on it, one hop each way">⊕ expand from store</span>`:``}
    ${PP.identity?`<div class="kv">id <code>${esc(n.id)}</code></div>${(n.by&&!isSrc)?`<div class="kv">by <code>${esc(n.by)}</code></div>`:``}${meta}`:``}
    ${(PP.navigate&&n.type==="foton")?`<h4>navigate</h4><div class="navrow"><span class="rc" id="nvSpine" title="every foton this was built from, back to raw inputs - all parents, not one path">⤴ trace inputs · ${linN}</span><span class="rc" id="nvImpact" title="every foton downstream that depends on this">⤵ blast radius · ${impN}</span><span class="rc" id="nvCard" title="who attested to this: reviews, reproductions, roles">🛡 trust card</span></div><div id="navout"></div>`:``}
    ${PP.marks?marksCard(n.id):``}
    ${(PP.file&&uris.length)?`<h4>file</h4><span class="rc" id="pvbtn">⤢ preview</span>${uris.map(u=>safeUri(u)?`<a class="uri" href="${esc(safeUri(u))}" target="_blank" rel="noopener">↗ ${clipe(u,46)}</a>`:`<span class="uri m">↗ ${clipe(u,46)} (unsafe uri)</span>`).join("")}<div id="preview"></div>`:``}
    ${(PP.inputs&&n.inputs&&n.inputs.length)?`<h4>inputs (${n.inputs.length})</h4>${n.inputs.map(f=>fileRow(f,false,n.id)).join("")}`:``}
    ${(PP.outputs&&n.outputs&&n.outputs.length)?`<h4>outputs (${n.outputs.length})</h4>${n.outputs.map(f=>fileRow(f,true,n.id)).join("")}`:``}
    ${(PP.related&&ins.length)?`<h4>${isSrc?"used by":"built on / reviewed by"} (${ins.length})</h4>${ins.map(link).join("")}`:``}
    ${(PP.text&&n.text)?`<div class="txt">${esc(n.text)}</div>`:``}`;
  p.querySelectorAll(".nav[data-id],.frow.nav[data-id]").forEach(el=>el.onclick=()=>{ ex.sel=el.dataset.id; openNode(el.dataset.id); redraw(); });
  const pvb=p.querySelector("#pvbtn"); if(pvb) pvb.onclick=e=>{ e.stopPropagation(); previewFile(n.id, uris[0]); };
  p.querySelectorAll(".pvrow").forEach(b=>b.onclick=e=>{ e.stopPropagation(); previewFile(b.dataset.h, decodeURIComponent(b.dataset.u), $("#pv-"+b.dataset.h)); });
  const nvS=$("#nvSpine"), nvI=$("#nvImpact"), nvC=$("#nvCard");                                    // stories 1,2,5
  if(nvS) nvS.onclick=()=>enterExplorer(n.id,"lin",true,`inputs — ${linN} foton${linN===1?"":"s"} this was built from (every parent, fan-in included)`);
  if(nvI) nvI.onclick=()=>enterExplorer(n.id,"imp",true,`blast radius — ${impN} foton${impN===1?"":"s"} build on this`);
  if(nvC) nvC.onclick=()=>{ const o=$("#navout"); o.innerHTML=o.innerHTML?"":trustCard(n.id); };
  if(autoPreview){ const b=p.querySelector("#pvbtn")||p.querySelector(".pvrow.pvout")||p.querySelector(".pvrow"); if(b) b.click(); }  // prefer the OUTPUT - usually the interesting artifact
  p.classList.add("open");
}
$("#gear").onclick=()=>{ const el=$("#settings"); if(el.style.display==="block") el.style.display="none"; else openSettings(); };
{ const st=$("#story"), ab=$("#about"); const show=localStorage.getItem("kton-about")==="1"; st.style.display=show?"block":"none"; ab.classList.toggle("on",show);   // intro hidden by default; ⓘ toggles it, remembered
  ab.onclick=()=>{ const on=st.style.display!=="block"; st.style.display=on?"block":"none"; ab.classList.toggle("on",on); localStorage.setItem("kton-about",on?"1":""); }; }
{ const lt=$("#legtoggle"); if(lt) lt.onclick=()=>$("#legend").classList.toggle("open"); }   // legend collapsed by default
// Hover a step: what it ran, what it did and why - when the profile asks for it (node.tooltip).
/* ===== multi-selection (prof().select.multi) ===== */
// What the host hears: the usual view state plus the selected steps, by full id, with command and outputs.
function viewState(){ const st=VS?VS.readState():{};
  if(prof().select.multi) st.selection=SELM.describe(G, [...SELSET], FOTONID, stepInfo);
  return st; }
// One place that changes the selection: redraw, the bar, the URL (sel=, so a reload keeps it), the host.
function setSelection(set, report=true){
  SELSET=new Set([...set].filter(id=>{ const n=G.byId.get(id); return n && n.type==="foton"; }));
  try{ const u=new URL(location.href); if(SELSET.size) u.searchParams.set("sel",[...SELSET].join(",")); else u.searchParams.delete("sel");
       history.replaceState(history.state,"",u); }catch{}
  renderSelBar(); if(ex && ex.nodes) redraw();
  if(report && BRIDGE) BRIDGE.state(viewState()); }
function renderSelBar(){ let b=$("#selbar");
  if(!prof().select.multi || !SELSET.size){ if(b) b.style.display="none"; return; }
  if(!b){ b=document.createElement("div"); b.id="selbar"; $("#explorer").appendChild(b); }
  b.innerHTML=`<b>${SELSET.size}</b> step${SELSET.size===1?"":"s"} selected <span class="rc" data-selclear="1">clear</span>`; b.style.display="flex"; }
document.addEventListener("click", function(e){ if(e.target.closest && e.target.closest("[data-selclear]")) setSelection(new Set()); });
// Hover a file: its name and what it is. Code files are drawn small and unlabelled (the graph would
// drown in script names), so without this a reader sees dots and lines and cannot tell what they are.
function showTip(n,x,y){ let t=$("#tip");
  if(!n || (n.type!=="foton" && n.type!=="source") || !prof().node.tooltip){ if(t) t.style.display="none"; return; }
  if(!t){ t=document.createElement("div"); t.id="tip"; $("#explorer").appendChild(t); }
  if(n.type==="source"){ const users=(G.in[n.id]||[]).length;
    t.innerHTML=`<div class="cmd">${esc(n.role)}</div><div class="desc">${n.kind==="code"?"script / code":"dataset"} · an input no step here produced${users?` · used by ${users} step${users===1?"":"s"}`:``}</div>`; }
  else { const a=stepInfo(n);
    t.innerHTML=`<div class="cmd">${esc(a.command)}</div>${a.description?`<div class="desc">${esc(a.description)}</div>`:``}${a.rationale?`<div class="rat"><b>why</b> ${esc(a.rationale)}</div>`:``}${stepMarksText(n)}`; }
  t.style.display="block"; t.style.left=(x+14)+"px"; t.style.top=(y+14)+"px"; }
// A host-provided file action (prof().actions): the viewer names the file, the host does the work.
// Capture phase, so the click does not also navigate the row it sits in.
document.addEventListener("click", function(e){
  var b=e.target.closest && e.target.closest("[data-fileact]"); if(!b) return;
  e.preventDefault(); e.stopPropagation();
  const short=b.dataset.h, full=FULLH.get(short)||short;
  const intent={type:"act", action:b.dataset.fileact, file:{name:b.dataset.n, hash:(full.length===64?"sha256:":"")+full, role:b.dataset.io}, node:b.dataset.node};
  if(BRIDGE) BRIDGE.intent(intent); else console.info("file action (no host listening):", intent);
}, true);
// Embedded in an editor, a link out is the host's call, not ours: report it as an intent and let
// the editor open it (or not). Only when somebody is listening - a plain tab keeps its links.
document.addEventListener("click", function(e){
  var a=e.target.closest && e.target.closest("a.uri[href]");
  if(!a || !BRIDGE || !BRIDGE.embedded) return;
  e.preventDefault(); BRIDGE.intent({type:"act", action:"open", uri:a.getAttribute("href")});
}, true);
// inline onclick= is blocked by the webview CSP, so these controls are wired by delegation instead.
document.addEventListener("click", function(e){
  var t=e.target.closest && e.target.closest("[data-clear],[data-overview],[data-closepanel],[data-ungroup],[data-recentre],[data-storeexpand],[data-storemore]");
  if(!t) return;
  if(t.hasAttribute("data-storeexpand")) return storeExpand(t.getAttribute("data-storeexpand"));
  if(t.hasAttribute("data-storemore")) return storeMore();
  if(t.hasAttribute("data-overview")) return showOverview();
  if(t.hasAttribute("data-clear")) return renderView(t.getAttribute("data-clear"));
  if(t.hasAttribute("data-closepanel")){ var pn=document.querySelector("#panel"); if(pn) pn.classList.remove("open"); if(ex&&ex.nodes){fitView();redraw();} return; }
  if(t.hasAttribute("data-ungroup")){ ex.grouped=false; drawControls(); layoutAndRun(); return; }
  if(t.hasAttribute("data-recentre")) return recentre(t.getAttribute("data-recentre"));
});
/* ===== a block in a composition (C1.5 v3): the host answers graph@1 ===== *
 * viewer.json says what this block needs (graph@1; product@1 and open@1 optional) and accepts. The first
 * kton:set brings its use and the host; the graph comes as the answer to kton:request graph@1, in one of
 * two forms: {records, keys, names, attested?} - verified here by the same wasm as a union - or {graph,
 * sketch?, derived?, contracted?} - a graph the host built and vouches for (the verticle path). Every
 * later set is a view state, applied as v2's was. Intents leave in the v3 form: {kind, …}. */
let V3=null;
const V3_ACCEPTS={ focus:{type:"hash"}, mode:{type:"literal"}, facets:{type:"object"}, layout:{type:"literal"}, view:{type:"literal|object"}, selection:{type:{set:"hash"}} };
async function bootV3(){
  await loadModules();
  let started=false;
  const b=HOST.connect({ requires:["graph@1"], optional:["product@1","open@1"], accepts:V3_ACCEPTS,
    current:()=>G?viewState():null,
    onSet:({state, host})=>{
      if(started){ if(!G){ V3.pendingState=state; return {}; } return applyHostState(state); }
      started=true; V3.pendingState=state; v3Load(host); return {}; } });
  // the rest of this file talks v2 (BRIDGE.intent({type:"act", action, …}) / {type:"navigate", …});
  // this is the one place that turns it into v3
  V3={ bridge:b, pendingState:null,
    answerSet:(r)=>{ if(r&&r.refused&&r.refused.length) b.refused(r.refused, r.why||"not accepted"); b.state(viewState()); } };
  BRIDGE={ embedded:true, state:(st)=>b.state(st), refused:(what,why)=>b.refused(what,why), ready:()=>b.ready(),
    intent:(i)=>{ const {type, action, ...rest}=i||{};
      const v3=type==="act" ? {kind:action, ...rest} : type==="navigate" ? {kind:"navigate", value:rest.node, ...rest} : {kind:type, ...rest};
      return b.intent(v3).then(r=>{ if(r&&r.refused) notice(`${v3.kind}: ${r.refused.why}`); return r; }); } };
  b.ready();
}
async function v3Load(host){
  try{
    if(!(host&&host.provides||[]).includes("graph@1")) throw new Error("this host does not provide graph@1");
    const r=await V3.bridge.request("graph@1", {});
    if(r&&r.graph){
      SKETCH=r.sketch||null; DERIVED=r.derived||null; CONTRACTED=r.contracted||null; if(!CONTRACTED) VIEW="attested";
      GRAPH_PATH=null; renderPrebuilt(r.graph); return;
    }
    if(!r||!Array.isArray(r.records)) throw new Error("graph@1 answered neither {records} nor {graph}");
    await initWasm();
    const names=r.names||{}; ATTESTED_NAMES=new Set((r.attested||[]).map(k=>names[k]).filter(Boolean));
    await usePackages(r.packages);
    renderGraph(JSON.stringify(r.records.slice(0,RECORD_CAP)), JSON.stringify(r.keys||{}), JSON.stringify(names), null);
  }catch(e){ showErr(e); V3.bridge.refused(["graph@1"], String(e&&e.message||e)); }
}
load();
