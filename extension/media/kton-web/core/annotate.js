/**
 * What a step did and why, read off the records next to a graph.
 *
 * A step's description and rationale are claims ABOUT its foton (dc:description,
 * ep-plan:hasRationale - ADR-005's `describes` template), and its command is in the foton's own
 * statement. The graph carries the claims as nodes but not their values in a form a label can use,
 * so this reads them from the records - and takes a claim only if the GRAPH says it verified. The
 * records are what the store or union handed over; the graph is what the wasm checked. A value from
 * an unverified claim must not end up under a step as if it were the step's own.
 *
 * Pure: graph + records in, a map out.
 */

const DESCRIPTION = /(^|[/#])description$/;
const RATIONALE = /(^|[/#])hasRationale$/i;
// model development (the pmx vocabulary, and the cockpit templates that follow it): which step is
// a key / the base / the final model, which passed QC, and which step a step was developed from
const lastSeg = (uri) => String(uri || "").split(/[/#]/).pop();
const MARK = { "key-step": "key", "base-model": "base", "final-model": "final", "not-followed": "not followed" };
const MARK_VALUE = { key: "key", base: "base", final: "final", "not-followed": "not followed" };
const RELATION = { "parent-step": "based on", "based-on": "adapted from", "review-valid": "reviewed · valid", "review-invalid": "reviewed · not valid" };
const strip = (h) => String(h || "").replace(/^sha256:/, "").toLowerCase();

function payloadOf(rec) {
    // the payload is UTF-8; atob alone yields Latin-1 and turns "·" into "Â·"
    try { return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(rec.envelope.payload), (c) => c.charCodeAt(0)))); } catch { return null; }
}

/**
 * annotations(G, records) -> Map(fotonNodeId -> {command, description, rationale, marks, qc})
 *   marks: [{kind:"key"|"base"|"final", value, by}]   qc: [{by, value}]
 * G is an indexed graph (byId, in). Latest claim wins when a step was described twice.
 */
export function annotations(G, records) {
    const out = new Map();
    const at = (id) => out.get(id) || out.set(id, {}).get(id);
    // graph node ids are a prefix of the record's hex id
    const nodeFor = (recId) => {
        const h = strip(recId);
        for (const len of [32, 16, h.length]) { const n = G.byId.get(h.slice(0, len)); if (n) return n; }
        return null;
    };
    const when = new Map();
    for (const rec of records || []) {
        const p = payloadOf(rec);
        if (!p) continue;
        if (rec.fotonId) {
            const n = nodeFor(rec.fotonId);
            const cmd = p.predicate && p.predicate.protocol && p.predicate.protocol.descriptor && p.predicate.protocol.descriptor.cmd;
            if (n && cmd) at(n.id).command = String(cmd);
            continue;
        }
        if (!rec.claimId) continue;
        const c = nodeFor(rec.claimId);
        if (!c || !c.verified) continue;                         // the graph's verdict, not the record's say-so
        const pr = p.predicate || {}, uri = String((pr.predicate && pr.predicate.uri) || "");
        const value = pr.object && pr.object.value, seg = lastSeg(uri), by = String(pr.by || "").replace(/^CN=/, "");
        const steps = (p.subject || []).map((s) => nodeFor(s.digest && s.digest.sha256)).filter((t) => t && t.type === "foton");
        // the same marks as a value of pmx/model-role (base | final | key | not-followed)
        if (seg === "model-role" && MARK_VALUE[String(value)]) { for (const t of steps) (at(t.id).marks ??= []).push({ kind: MARK_VALUE[String(value)], value: "", by }); continue; }
        if (MARK[seg]) { for (const t of steps) (at(t.id).marks ??= []).push({ kind: MARK[seg], value: value == null ? "" : String(value), by }); continue; }
        if (seg === "qc-passed") { for (const t of steps) (at(t.id).qc ??= []).push({ by, value: value == null ? "" : String(value) }); continue; }
        const field = DESCRIPTION.test(uri) ? "description" : RATIONALE.test(uri) ? "rationale" : null;
        if (!field || value == null) continue;
        for (const t of steps) {
            const k = t.id + "\0" + field, w = String(pr.when || "");
            if (when.has(k) && when.get(k) > w) continue;
            when.set(k, w); at(t.id)[field] = String(value);
        }
    }
    return out;
}

/**
 * Step-to-step relations from verified claims: a claim about step A whose object names step B
 * (`parent-step`, `based-on`) becomes an edge A -> B. The kernel draws the same claim as a box
 * between the two; a view that hides claim boxes would lose the relation without this.
 * Both reference forms: `{"parent-step": {"hash": "sha256:…"}}` and `{"parent-step": "sha256:…"}`.
 */
export function relationEdges(G, records) {
    const out = [], seen = new Set();
    const nodeFor = (h) => { h = strip(h); for (const len of [32, 16, h.length]) { const n = G.byId.get(h.slice(0, len)); if (n) return n; } return null; };
    for (const rec of records || []) {
        if (!rec.claimId) continue;
        const c = nodeFor(rec.claimId);
        if (!c || !c.verified) continue;
        const p = payloadOf(rec), pr = (p && p.predicate) || {}, kind = lastSeg(pr.predicate && pr.predicate.uri);
        // a reproduction names its level as the object's key: {"L0": {"hash": <the run it reproduces>}}
        const level = kind === "reproduces" && pr.object && ["L0", "L1", "L2"].find((l) => pr.object[l]);
        if (!RELATION[kind] && !level) continue;
        const o = pr.object && pr.object[level || kind], ref = typeof o === "string" ? o : o && o.hash;
        const to = ref && nodeFor(ref);
        if (!to || to.type !== "foton") continue;
        for (const s of p.subject || []) {
            const from = nodeFor(s.digest && s.digest.sha256);
            if (!from || from.type !== "foton" || from.id === to.id) continue;
            const k = from.id + "\0" + to.id + "\0" + kind;
            if (seen.has(k)) continue; seen.add(k);
            out.push({ from: from.id, to: to.id, kind, label: level ? "reproduces · " + level : RELATION[kind], claim: c.id, by: String(pr.by || "").replace(/^CN=/, ""), why: String(pr.why || ""), step: true });
        }
    }
    return out;
}

/** Full content hashes for the (shortened) file hashes a graph shows, from what records declare. */
export function fullHashes(records) {
    const m = new Map();
    for (const rec of records || []) {
        const p = payloadOf(rec);
        if (!p) continue;
        const refs = [...(p.subject || []), ...((p.predicate && p.predicate.inputs) || [])];
        for (const r of refs) { const h = strip(r && r.digest && r.digest.sha256); if (h.length === 64) m.set(h.slice(0, 32), h); }
    }
    return m;
}
