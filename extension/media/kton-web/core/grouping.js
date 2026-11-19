/**
 * Grouping, declared rather than written.
 *
 * Two different things go by that name, and conflating them is how a grouping control ends up
 * meaning something different in each viewer:
 *
 *   CONTRACTION    the drawn graph gets smaller: a run of adjacent nodes becomes one node carrying
 *                  its members. This is what a canvas needs at two thousand nodes, and the graph
 *                  viewer has done it since before this kit existed.
 *   LIST GROUPING  what a compass needs: the rows in a direction get headers and counts, and the
 *                  reader collapses the ones they do not care about. Nothing is contracted.
 *
 * Both need the same one thing - a KEY: node to which group, called what. So the key is declared in
 * a sketch and the two mechanisms share it. That is also what makes grouping describable from
 * outside: a host, or an agent over MCP, can ask which groupings exist and get an answer, instead
 * of each viewer knowing seven of them by heart.
 */

import { reach } from "./nav.js";
import { parsePath, formatPath, evalPath, closureSteps, canAdvance } from "./path.js";

/**
 * Build a grouping key from a declaration.
 *
 *   {by: 'attr', name: 'participant'}    a field on the node
 *   {by: 'type'}                         foton | claim | source | entity | ref
 *   {by: 'term'}                         the exact predicate a claim carries
 *   {by: 'root',                         which origin it descends from - "which analysis is this".
 *    path: 'about?/builds-on*'}          A property path (core/path.js). The older
 *                                        {edges, enter} form is translated to one.
 *   {by: 'none'}                         one group; the honest way to say "do not group"
 *
 * Returns node to {id, label}. An empty id means ungrouped.
 */
/** {edges, enter} was a path spelled as two fields. Spell it as a path. */
function legacyPath(s) {
    const or = (xs) => (xs.length === 1 ? xs[0] : `(${xs.join("|")})`);
    const climb = or(s.edges && s.edges.length ? s.edges : ["builds-on"]);
    const door = s.enter && s.enter.length ? or(s.enter) + "?/" : "";
    return `${door}${climb}*`;
}

export function makeKey(spec, ctx = {}) {
    const { G, terms } = ctx;
    const s = spec || { by: "none" };

    if (s.by === "type") return (n) => ({ id: n.type || "?", label: n.type || "?" });

    if (s.by === "attr") {
        const name = s.name || "participant";
        return (n) => {
            const v = n[name];
            const empty = v == null || v === "";
            return { id: empty ? " " : String(v), label: empty ? (s.emptyLabel || "ohne") : String(v) };
        };
    }

    if (s.by === "term") {
        return (n) => {
            const t = terms && terms.get(n.id);
            return t ? { id: t.term, label: t.short, title: t.term } : { id: " ", label: "ohne Term" };
        };
    }

    if (s.by === "root") {
        // Which origin does this descend from - in a pipeline corpus, "which analysis is this part
        // of". The way there is a PATH (core/path.js), because it was never only "climb these
        // edges": a claim hangs off the tree by `about` and has to enter before it can climb, and
        // the case after that will want a condition mid-way. `edges`/`enter` still parse - they are
        // translated to a path here, so there is one evaluator and not two that drift.
        const src = s.path || legacyPath(s);
        let ast;
        try {
            ast = parsePath(src);
        } catch (err) {
            // A sketch is data, and data can be wrong. Say so once, in the key, rather than
            // throwing inside a viewer's render loop where it reads as a broken viewer.
            return () => ({ id: " ", label: `ungültiger Pfad: ${err.message}` });
        }
        const steps = closureSteps(ast);            // what the path can still take - derived, not declared

        // Cached: walking to the root for every row would be O(rows x depth), and the rows are
        // exactly the case where grouping was needed in the first place.
        const cache = new Map();

        // Does this node sit in the tree the path climbs, in either direction? A node that neither
        // climbs nor is climbed into is not in it at all, and belongs in the unnamed bucket - the
        // invented singleton it used to get looked like an answer, which is worse than "ohne".
        const inTree = (x) => steps.some((st) =>
            ((st.inv ? G.in : G.out)[x] || []).some((e) => e.kind === st.kind) ||
            ((st.inv ? G.out : G.in)[x] || []).some((e) => e.kind === st.kind));

        const rootOf = (id) => {
            if (cache.has(id)) return cache.get(id);
            cache.set(id, null);                     // guard: a path can lead back around
            const { ends, truncated } = evalPath(G, id, ast, { budget: 20000 });

            // A truncated climb may simply not have reached the root yet. Reporting the furthest
            // node found would be a plausible wrong answer, which is the one thing this must not do.
            let best = null;
            if (!truncated) {
                // `a*` reaches every node on the way, so the root is an end that cannot advance.
                // The start only counts when the path went nowhere at all.
                let bestFanout = -1;
                for (const x of ends) {
                    if (x === id && ends.size > 1) continue;
                    if (canAdvance(G, x, steps)) continue;
                    const fanout = (G.in[x] || []).length;   // several roots: the one most is built on
                    if (fanout > bestFanout) { bestFanout = fanout; best = x; }
                }
                if (best === id && !inTree(id)) best = null;
            }
            cache.set(id, best);
            return best;
        };

        const key = (n) => {
            const r = rootOf(n.id);
            if (!r) return { id: " ", label: s.emptyLabel || "ohne" };
            const rn = G && G.byId.get(r);
            return { id: r, label: rn ? (rn.label || rn.role || r.slice(0, 10)) : String(r).slice(0, 10) };
        };
        key.path = formatPath(ast);                 // so a host, or an agent, can be told HOW it groups
        return key;
    }

    return () => ({ id: "", label: "" });
}

/**
 * Group a list. Returns [{id, label, items}], largest first, with the unnamed bucket last - "ohne"
 * at the top would push what the reader came for below the fold.
 */
export function groupList(items, key) {
    const by = new Map();
    for (const it of items) {
        const k = key(it) || { id: "", label: "" };
        const g = by.get(k.id) || { id: k.id, label: k.label, title: k.title, items: [] };
        g.items.push(it);
        by.set(k.id, g);
    }
    const out = [...by.values()];
    out.sort((a, b) => (a.id === " ") - (b.id === " ")
        || b.items.length - a.items.length
        || String(a.label).localeCompare(String(b.label)));
    return out;
}

/**
 * Contract a drawn graph: ADJACENT nodes sharing a key become one node carrying its members.
 *
 * Generalised from the graph viewer's groupRuns, which hard-coded `participant`. The adjacency is
 * not an implementation detail: grouping ALL of a participant's work into one blob asserts
 * something false, while contracting a RUN of their consecutive steps says exactly what happened.
 * Sources are never contracted - a file is a landmark, and merging two of them hides the thing a
 * reader is usually looking for.
 */
export function contract(nodes, edges, key, { skipTypes = ["source"] } = {}) {
    const uf = {}, find = (x) => { while (uf[x] !== x) x = uf[x] = uf[uf[x]]; return x; };
    nodes.forEach((n) => (uf[n.id] = n.id));
    const byId = new Map(nodes.map((n) => [n.id, n]));
    const keyOf = (n) => (key(n) || { id: "" }).id;

    for (const e of edges) {
        const a = byId.get(e.from), b = byId.get(e.to);
        if (!a || !b) continue;
        if (skipTypes.includes(a.type) || skipTypes.includes(b.type)) continue;
        const ka = keyOf(a);
        if (!ka || ka !== keyOf(b)) continue;
        uf[find(a.id)] = find(b.id);
    }

    const groups = new Map();
    for (const n of nodes) {
        const r = skipTypes.includes(n.type) ? n.id : find(n.id);
        (groups.get(r) || groups.set(r, []).get(r)).push(n);
    }

    const gnodes = [], gid = {};
    for (const [r, m] of groups) {
        if (m.length === 1) { gnodes.push(m[0]); gid[m[0].id] = m[0].id; continue; }
        const id = "grp:" + r;
        m.forEach((x) => (gid[x.id] = id));
        gnodes.push({ id, type: "group", participant: m[0].participant, role: m.length + " records",
                      count: m.length, verified: m.every((x) => x.verified), members: m });
    }

    const seen = new Set(), gedges = [];
    for (const e of edges) {
        const f = gid[e.from], t = gid[e.to];
        if (f === t) continue;
        const k = f + ">" + t + ">" + e.kind;
        if (!seen.has(k)) { seen.add(k); gedges.push({ from: f, to: t, kind: e.kind }); }
    }
    return { nodes: gnodes, edges: gedges };
}

/** The groupings a sketch declares, plus the ones every corpus can answer. */
/**
 * The path a root grouping walks, as text. `describe` hands this out: naming a grouping `analyse`
 * says nothing about HOW it reaches an analysis, and an agent choosing between `analyse` and
 * `modellzweig` was picking by label alone. Legacy {edges, enter} render as the path they mean.
 */
export function pathOf(spec) {
    if (!spec || spec.by !== "root") return null;
    try { return formatPath(parsePath(spec.path || legacyPath(spec))); } catch { return spec.path || null; }
}

export function catalogue(spec) {
    const base = [
        { id: "none", label: "keine", by: "none" },
        { id: "type", label: "Art", by: "type" },
        { id: "participant", label: "Beteiligter", by: "attr", name: "participant" },
        { id: "signer", label: "Signierer", by: "attr", name: "signer" },
    ];
    const byId = new Map(base.map((g) => [g.id, g]));
    for (const g of (spec && spec.groupings) || []) byId.set(g.id, { ...byId.get(g.id), ...g });
    return [...byId.values()];
}
