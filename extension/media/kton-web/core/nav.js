/**
 * Navigation over a kton graph — the primitives, once.
 *
 * These were written inside the graph viewer and worked well; they are here because the compass
 * needs the same walks, and a second traversal would be a second answer to "what does this rest
 * on". The rule in this project is blunt about it: whoever writes a second version has to say why
 * the first one does not do.
 *
 * Everything here is pure: a graph in, a set or a tree out. No DOM, no fetch, no drawing. The
 * viewers decide how to show a cone; none of them decides what a cone IS.
 */

/** Adjacency, both ways, plus the participant list every viewer ends up wanting. */
export function indexGraph(graph) {
    const G = { nodes: graph.nodes || [], edges: graph.edges || [], summary: graph.summary, locators: graph.locators || {} };
    G.byId = new Map(G.nodes.map((n) => [n.id, n]));
    G.out = {}; G.in = {};
    for (const e of G.edges) { (G.out[e.from] ??= []).push(e); (G.in[e.to] ??= []).push(e); }
    G.participants = [...new Set(G.nodes.map((n) => n.participant))].sort();
    return G;
}

/**
 * Pedigree of `id` under `mode`, BFS by hop-distance from the centre, capped at `cap` nodes.
 *
 * The cap is not a rendering convenience: in a densely interconnected registry a full walk pulls
 * the ENTIRE graph (measured in the viewer: 2000 of 2000). BFS means a cap keeps the LOCAL
 * neighbourhood rather than an arbitrary slice, and the full reach is computed anyway - traversal
 * is cheap, drawing is not - so the caller can say "capped 250 of 2000" instead of quietly lying.
 *
 * Returns {set, truncated, total}.
 */
export function pedigree(G, id, mode, cap) {
    cap = cap || Infinity;
    const lineage = mode === "lin" || mode === "imp";      // pure derivation DAG: builds-on only, no claims
    const kf = lineage ? "builds-on" : null;

    const build = (lim) => {
        const set = new Set([id]);
        const dirs = [];
        if (mode === "anc" || mode === "both" || mode === "lin") dirs.push([G.out, "out"]);   // what it builds on
        if (mode === "desc" || mode === "both" || mode === "imp") dirs.push([G.in, "in"]);    // what builds on it
        if (mode === "nbr") {
            for (const e of (G.out[id] || [])) if (set.size < lim) set.add(e.to);
            for (const e of (G.in[id] || [])) if (set.size < lim) set.add(e.from);
        } else if (mode === "par") {
            for (const e of (G.out[id] || [])) if (set.size < lim) set.add(e.to);
        } else if (mode === "chi") {
            for (const e of (G.in[id] || [])) if (set.size < lim) set.add(e.from);
        } else {
            let fr = [id];
            while (fr.length && set.size < lim) {
                const nx = [];
                for (const x of fr) for (const [adj, dir] of dirs) for (const e of (adj[x] || [])) {
                    if (kf && e.kind !== kf) continue;                 // lineage: all parents (fan-in), builds-on only
                    const nb = dir === "out" ? e.to : e.from;
                    if (!set.has(nb) && set.size < lim) { set.add(nb); nx.push(nb); }
                }
                fr = nx;
            }
        }
        // surface claims about anything in view - but not for a pure lineage walk, which is asking
        // a narrower question and would be muddied by them
        if (!lineage) for (const nid of [...set]) for (const e of (G.in[nid] || [])) {
            const f = G.byId.get(e.from);
            if (f && f.type === "claim" && set.size < lim) {
                set.add(e.from);
                for (const oe of (G.out[e.from] || [])) if (set.size < lim) set.add(oe.to);
            }
        }
        return set;
    };

    const full = build(Infinity);
    if (full.size <= cap) return { set: full, truncated: false, total: full.size };
    return { set: build(cap), truncated: true, total: full.size };
}

/** Nodes reachable along `kind` edges. dir 'up' = ancestors / 'down' = descendants, depth-bounded. */
export function reach(G, id, dir, kind, depth) {
    const adj = dir === "up" ? G.out : G.in, pick = (e) => (dir === "up" ? e.to : e.from);
    const seen = new Set([id]);
    let fr = [id], dd = 0;
    depth = depth || Infinity;
    while (fr.length && dd < depth) {
        const nx = [];
        for (const x of fr) for (const e of (adj[x] || [])) {
            if (kind && e.kind !== kind) continue;
            const nb = pick(e);
            if (!seen.has(nb)) { seen.add(nb); nx.push(nb); }
        }
        fr = nx; dd++;
    }
    return seen;
}

/** The claims asserted ABOUT x (incoming `about` edges). */
export function claimsOf(G, id) {
    const out = [];
    for (const e of (G.in[id] || [])) {
        if (e.kind !== "about") continue;
        const c = G.byId.get(e.from);
        if (c && c.type === "claim") out.push(c);
    }
    return out;
}

export function claimMatch(c, pred, value) {
    const okP = !pred || (c.role || "").toLowerCase().includes(pred.toLowerCase());
    const okV = !value || (c.text || "").toLowerCase().includes(value.toLowerCase());
    return okP && okV;
}

/** Existence / absence over fotons - the coverage and worklist primitive. */
export function fotonsWith(G, pred, value) {
    return G.nodes.filter((n) => n.type === "foton" && claimsOf(G, n.id).some((c) => claimMatch(c, pred, value)));
}
export function fotonsLacking(G, pred, value) {
    return G.nodes.filter((n) => n.type === "foton" && !claimsOf(G, n.id).some((c) => claimMatch(c, pred, value)));
}

/**
 * The cone as a TREE, for viewers that list rather than draw.
 *
 * A provenance cone is a DAG, not a tree: a shared input reappears under every consumer. Rendered
 * naively that reads as five files where there is one. So a repeated node is emitted with
 * `repeat: true` and NOT expanded again - the first occurrence carries the subtree - and `distinct`
 * counts what is actually there. "37 rows, 12 distinct things" is the honest sentence.
 *
 * `budget` bounds total rows. Whatever it drops is reported in `truncated`/`total`, never silently.
 *
 * It does NOT assume the walk is acyclic. It should be - a foton's output hash covers its inputs,
 * so A cannot derive from B while B derives from A - but examples/12-submission contains two fotons
 * that sit in each other's builds-on cone (measured: 2 of 61 nodes). Whether that is a data fault is
 * not this file's business; not looping is. The `expanded` guard already handles it, and `cycle`
 * says so out loud rather than letting a reader wonder why the root reappears as a repeat.
 *
 * Returns {rows, distinct, truncated, total, maxDepth, cycle}.
 * Each row: {id, depth, kind, repeat, parent}.
 */
export function cone(G, id, dir, { kind = null, depth = Infinity, budget = 500 } = {}) {
    const adj = dir === "up" ? G.out : G.in, pick = (e) => (dir === "up" ? e.to : e.from);
    // `kind` may be one kind, several, or a predicate. The compass defines a direction as a SET of
    // edge kinds (and, for the claim axis, as everything except one), so a cone that only took a
    // single string would have forced a second traversal over there - the exact duplication that
    // lifting these walks out of the viewer was meant to end.
    const takes = kind == null ? () => true
        : typeof kind === "function" ? kind
        : Array.isArray(kind) ? (e) => kind.includes(e.kind)
        : (e) => e.kind === kind;
    const rows = [], expanded = new Set([id]);
    let total = 0, maxDepth = 0;

    const walk = (at, d, parent) => {
        if (d > depth) return;
        const edges = (adj[at] || []).filter(takes);
        // stable order: by edge kind, then by what the node is called - the caller may relabel, but
        // a cone that reshuffles between two identical renders is a cone nobody trusts
        edges.sort((a, b) => (a.kind === b.kind ? String(pick(a)).localeCompare(String(pick(b))) : a.kind.localeCompare(b.kind)));
        for (const e of edges) {
            const nb = pick(e);
            if (nb === at) continue;                                  // a self-loop is not a step
            total++;
            if (rows.length >= budget) continue;
            const repeat = expanded.has(nb);
            rows.push({ id: nb, depth: d, kind: e.kind, repeat, parent: at });
            if (d > maxDepth) maxDepth = d;
            if (!repeat) { expanded.add(nb); walk(nb, d + 1, nb); }
        }
    };
    walk(id, 1, id);

    // distinct counts what is actually EMITTED, not what got expanded: with a cycle back to the
    // root, the root appears as a row but was pre-marked as expanded and would have been missed.
    const seen = new Set(rows.map((r) => r.id));
    return { rows, distinct: seen.size, truncated: total > rows.length, total, maxDepth, cycle: seen.has(id) };
}
