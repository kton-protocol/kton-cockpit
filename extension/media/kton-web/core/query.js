/**
 * The small algebra the named questions are written in.
 *
 * Before this, `blast radius` was built in and a domain question was a parameter slot - which meant
 * the interesting half was privileged and the extensible half was a template. That is the failure
 * this project measures itself against: an abstraction that is claimed rather than built. So the
 * five built-in questions are now written in the SAME form a sketch writes, and if that form cannot
 * express one of them, the form is wrong and not the question.
 *
 * Four clauses, deliberately few:
 *
 *   from    where to start      'node' | 'fotons' | 'claims' | 'nodes'
 *   walk    how to travel       {dir: 'up'|'down', edges: [...] | null, depth: n | null}
 *   where   what to keep        [{has|lacks: {pred, value?}} | {attr: {name, in: [...]}}]
 *   as      what shape          'cone' | 'list' | 'count' | 'card'
 *
 * `walk` follows the same adjacency the compass and the graph walk (core/nav.js); `where` reads the
 * claim axis. Nothing here renders, and nothing here is special-cased by id.
 *
 * What it deliberately does NOT have: joins, arbitrary nesting, negation of a whole clause,
 * user-supplied code. Every one of those would let a sketch express something no reader could
 * predict the cost of, on a corpus of two thousand records in a browser tab. A question that cannot
 * be written here is a reason to widen the algebra on purpose, not to smuggle a function in.
 */

import { reach, cone, claimsOf, claimMatch } from "./nav.js";

const startSet = (G, from, node) => {
    if (from === "node") return node ? [G.byId.get(node)].filter(Boolean) : [];
    if (from === "fotons") return G.nodes.filter((n) => n.type === "foton");
    if (from === "claims") return G.nodes.filter((n) => n.type === "claim");
    return G.nodes;
};

/** A walk with nowhere to stand, in the shape that was asked for. */
const emptyWalk = (shape) =>
    shape === "list" ? { as: "list", list: [], of: 0, n: 0 }
    : shape === "count" ? { as: "count", n: 0, of: 0 }
    : { as: "cone", rows: [], distinct: 0, total: 0, truncated: false, count: 0 };

/** One `where` clause against one node. */
function keeps(G, n, clause) {
    if (clause.has) {
        const { pred, value } = clause.has;
        return claimsOf(G, n.id).some((c) => claimMatch(c, pred, value));
    }
    if (clause.lacks) {
        const { pred, value } = clause.lacks;
        return !claimsOf(G, n.id).some((c) => claimMatch(c, pred, value));
    }
    if (clause.attr) {
        const { name, in: allowed } = clause.attr;
        const v = n[name];
        return !allowed || allowed.includes(v);
    }
    return true;
}

/**
 * Run a query.
 * `ctx.node` is where the reader stands; `ctx.depth`/`ctx.budget` bound a cone at render time,
 * because how far to draw is the viewer's call and not the question's.
 */
export function run(G, q, { node = null, depth = null, budget = 400 } = {}) {
    const from = q.from || (q.walk ? "node" : "nodes");
    const at = from === "node" ? node : null;

    if (q.as === "card") {
        const cs = at ? claimsOf(G, at) : [];
        // The buckets are DATA. They have to be: "approve" and "reproduces" are this domain's words,
        // and a card that hard-codes them is a card only this domain can have. A sketch declares its
        // own tally the same way, which is the whole point of writing the built-ins in this form.
        const counts = {};
        for (const t of q.tally || []) {
            const hits = cs.filter((c) => {
                const vals = t.value == null ? [null] : [].concat(t.value);
                return vals.some((v) => claimMatch(c, t.pred, v));
            });
            // `distinct` is not decoration: corroboration accrues as SIGNERS, never as producer
            // multiplicity - a foton is immutable, so an identical rerun dedups to one producer and
            // counting producers would make a rerun look like independent confirmation.
            counts[t.id] = t.distinct
                ? new Set(hits.map((c) => c[t.distinct]).filter(Boolean)).size
                : hits.length;
        }
        const byPredicate = {};
        for (const c of cs) {
            const pred = (c.role || "").split(":").pop();
            const v = (c.text || "").split(",")[0] || "";
            (byPredicate[pred] ??= {});
            byPredicate[pred][v] = (byPredicate[pred][v] || 0) + 1;
        }
        return { as: "card", claims: cs, node: at, counts, byPredicate };
    }

    if (q.walk) {
        const w = q.walk;
        const edges = w.edges && w.edges.length
            ? (e) => w.edges.includes(e.kind)
            : null;
        const d = depth ?? w.depth ?? Infinity;
        // A walk answers in whichever shape was asked for. It used to build a cone and then stamp it
        // with `as`, so `as:'list'` handed back a cone wearing a list's label and the reader tripped
        // over the missing `list`. The shape is part of the question, not decoration on the answer.
        const shape = q.as || "cone";
        if (!at) return emptyWalk(shape);
        const c = cone(G, at, w.dir, { kind: edges, depth: d, budget });
        const full = reach(G, at, w.dir, w.edges && w.edges.length === 1 ? w.edges[0] : null, Infinity);
        // the count is the whole reach, not what a depth happened to draw: a button that says 7 and
        // then shows 3 because the depth was 2 is a button nobody believes twice
        if (shape === "cone") return { as: "cone", ...c, count: full.size - 1 };

        // Flattened, a walk is the SET it reached, in first-encounter order. The rows are a tree - a
        // node can hang under several parents and appear more than once - so the list dedups them,
        // and drops the start: where you stand is not something the walk found.
        const seen = new Set(), list = [];
        for (const r of c.rows) {
            if (r.id === at || seen.has(r.id)) continue;
            seen.add(r.id);
            const n = G.byId.get(r.id);
            if (n) list.push(n);
        }
        // `of` is the whole reach for the same reason the cone's count is: what a depth drew is `n`.
        const of = Math.max(full.size - 1, list.length);
        if (shape === "count") return { as: "count", n: list.length, of };
        return { as: "list", list, of, n: list.length };
    }

    let list = startSet(G, from, at);
    for (const clause of q.where || []) list = list.filter((n) => keeps(G, n, clause));
    const total = startSet(G, from, at).length;

    if (q.as === "count") return { as: "count", n: list.length, of: total };
    return { as: "list", list, of: total, n: list.length };
}
