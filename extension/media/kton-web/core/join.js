/**
 * Recorded and signed in one graph — joined by file content hash.
 *
 * Two graphs arrive in the same shape: the SIGNED one the kernel wasm builds from a union (every
 * foton verified against the keys, here in the browser), and the RECORDED one a recorder vouches
 * for (`?graph=`: what it watched happen, nothing signed, step ids synthetic). They describe the
 * same work from two sides, and the only thing they share is the bytes: a recorded step and a
 * signed foton are the same step when they produced the same file.
 *
 * So the join is by output hash, and it only ever adds a label, never trust:
 *   - a recorded step with an output some signed foton also has  -> that foton is "both"; the
 *     recorded node folds into it and its edges point at the foton
 *   - a recorded step no signed foton matches                    -> stays, "recorded", unverified
 *   - a signed foton no recorded step matches                     -> "signed"
 * A recorded node is never shown as verified: the viewer did not verify it, the recorder said so.
 *
 * Pure: two graphs in, one graph out. No DOM, no fetch.
 */

const FOTON = "foton";

/**
 * Join a signed graph and a recorded graph into one.
 * Either may be null; with only one, its fotons are labelled and it is otherwise unchanged.
 * Returns a new graph object (nodes, edges, locators, summary, stats); the inputs are not mutated.
 */
export function joinGraphs(signed, recorded) {
    const S = signed || { nodes: [], edges: [] }, R = recorded || { nodes: [], edges: [] };
    const nodes = [], byId = new Map();
    const put = (n) => { byId.set(n.id, n); nodes.push(n); };

    // signed first: it is the side that decides what a node IS
    for (const n of S.nodes || []) put(n.type === FOTON ? { ...n, origin: "signed" } : { ...n });

    // which signed fotons produced a file - one file can have several (a reproduction)
    const producers = new Map();
    for (const n of nodes) if (n.type === FOTON)
        for (const o of n.outputs || []) if (o && o.hash) (producers.get(o.hash) || producers.set(o.hash, []).get(o.hash)).push(n.id);

    const alias = new Map();   // recorded step id -> signed foton id it folded into
    for (const r of R.nodes || []) {
        if (r.type !== FOTON) continue;
        const hits = new Set();
        for (const o of r.outputs || []) for (const id of producers.get(o && o.hash) || []) hits.add(id);
        if (hits.size) {
            const ids = [...hits].sort();          // deterministic: the same join every time
            alias.set(r.id, ids[0]);
            for (const id of ids) { const s = byId.get(id); s.origin = "both"; (s.recordedAs ??= []).push(r.id); }
        } else if (!byId.has(r.id)) {
            put({ ...r, origin: "recorded", verified: false });
        }
    }
    // the recorder's sources: a file is a file, so a hash already in the signed graph is that node
    for (const r of R.nodes || []) if (r.type !== FOTON && !byId.has(r.id)) put({ ...r });

    const edges = [], seen = new Set();
    const addEdge = (e) => {
        const from = alias.get(e.from) || e.from, to = alias.get(e.to) || e.to;
        if (from === to || !byId.has(from) || !byId.has(to)) return;   // folded onto itself, or dangling
        const k = from + "\0" + to + "\0" + e.kind;
        if (!seen.has(k)) { seen.add(k); edges.push({ ...e, from, to }); }
    };
    for (const e of S.edges || []) addEdge(e);
    for (const e of R.edges || []) addEdge(e);

    const locators = {};
    for (const L of [S.locators, R.locators]) for (const [h, us] of Object.entries(L || {}))
        locators[h] = [...new Set([...(locators[h] || []), ...(us || [])])];

    const g = { nodes, edges, locators, summary: summarize(nodes, edges) };
    g.stats = { fotons: g.summary.totals.fotons, claims: g.summary.totals.claims,
                verified: g.summary.totals.verified, unverified: g.summary.totals.unverified };
    return g;
}

/**
 * The summary a graph carries, recomputed from its nodes and edges - after a join neither input's
 * summary is true any more. Same shape as the kernel's (totals, participants, participant edges),
 * plus `origins`: how many fotons are signed only, recorded only, both.
 */
export function summarize(nodes, edges) {
    const parts = new Map(), partOf = new Map();
    const totals = { participants: 0, fotons: 0, claims: 0, verified: 0, unverified: 0 };
    const origins = { both: 0, signed: 0, recorded: 0 };
    for (const n of nodes) {
        if (n.type !== FOTON && n.type !== "claim") continue;
        partOf.set(n.id, n.participant);
        const p = parts.get(n.participant) || parts.set(n.participant, { name: n.participant, fotons: 0, claims: 0, verified: 0, total: 0 }).get(n.participant);
        if (n.type === FOTON) { p.fotons++; totals.fotons++; if (n.origin) origins[n.origin]++; }
        else { p.claims++; totals.claims++; }
        p.total++;
        if (n.verified) { p.verified++; totals.verified++; } else totals.unverified++;
    }
    totals.participants = parts.size;
    const pe = new Map();
    for (const e of edges) {
        const a = partOf.get(e.from), b = partOf.get(e.to);
        if (!a || !b || a === b) continue;
        const k = a + "\0" + b + "\0" + e.kind;
        const x = pe.get(k) || pe.set(k, { from: a, to: b, kind: e.kind, count: 0 }).get(k);
        x.count++;
    }
    return { totals, origins, participants: [...parts.values()].sort((x, y) => x.name < y.name ? -1 : 1), edges: [...pe.values()] };
}
