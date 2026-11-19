/**
 * The named questions.
 *
 * A depth control is general and useless: nobody walks up to a corpus and asks for depth three.
 * They ask "what was this built from", "what breaks if this is wrong", "what still needs signing
 * off". The graph viewer already had five such questions - it called them stories - expressed over
 * the same walks in core/nav.js. They live here so the compass can ask them too, so a sketch can
 * add its own, and so the switch between viewers can carry the QUESTION rather than a number.
 *
 * Each question says where it stands (`at`) and is WRITTEN IN THE SAME ALGEBRA a sketch writes
 * (core/query.js): from / walk / where / as. Nothing here is special-cased by id, which is the only
 * way "a domain brings its own questions" is a built thing rather than a claimed one. If the algebra
 * cannot express one of these five, the algebra is wrong - not the question.
 *
 *   at 'node'   needs somewhere to stand      at 'graph'  a whole-corpus question
 *
 * Nothing here renders. A question returns what it found; how that looks is the viewer's business,
 * which is why the same `impact` reads as a tree in the compass and a layered DAG in the graph
 * without either of them owning the answer.
 */

import { run } from "./query.js";

/** Lineage only: these two questions are about derivation, and a claim would muddy them. */
const LINEAGE = "builds-on";

export const BUILTIN = [
    {
        id: "inputs", label: "Herkunft", gloss: "woraus das gebaut wurde - alle Eltern, nicht ein Pfad",
        at: "node", axis: "l", mode: "lin",
        from: "node", walk: { dir: "up", edges: [LINEAGE] }, as: "cone",
    },
    {
        id: "impact", label: "Auswirkung", gloss: "was davon abhängt - was bricht, wenn das falsch ist",
        at: "node", axis: "l", mode: "imp",
        from: "node", walk: { dir: "down", edges: [LINEAGE] }, as: "cone",
    },
    {
        id: "trust", label: "Bilanz", gloss: "wer was dazu gesagt hat: Prüfungen, Reproduktionen, Rollen",
        at: "node", from: "node", as: "card",
        tally: [
            { id: "approve", pred: "reviewedBy", value: ["approve", "accept"] },
            { id: "reject", pred: "reviewedBy", value: "reject" },
            { id: "reproducedBy", pred: "reproduces", distinct: "signer" },
        ],
    },
    {
        id: "signoff", label: "braucht Freigabe", gloss: "Läufe ohne Zustimmung - die Arbeitsliste",
        at: "graph", from: "fotons", where: [{ lacks: { pred: "reviewedBy", value: "approve" } }], as: "list",
    },
    {
        id: "repro", label: "Reproduktionsabdeckung", gloss: "bestätigt gegen unbestätigt",
        at: "graph", from: "fotons", where: [{ lacks: { pred: "reproduces" } }], as: "list",
    },
];

/** How many nodes a cone question reaches - cheap, and what a button needs to show a count. */
export function countFor(G, q, node) {
    if (q.as !== "cone" || !node) return 0;
    return run(G, q, { node, depth: 1 }).count;
}

/**
 * Ask a question.
 *
 * This is now a thin layer over core/query.js: the five built-ins are written in the same algebra a
 * sketch writes, so nothing here branches on an id. What is left is the shape the viewers already
 * expect - `head` for a worklist, the named tallies for a card - which is presentation vocabulary,
 * not a second way to compute an answer.
 */
export function ask(G, q, { node = null, depth = Infinity, budget = 400 } = {}) {
    const r = run(G, q, { node, depth, budget });

    if (r.as === "card") {
        const c = r.counts || {};
        return { id: q.id, kind: "card", claims: r.claims.length,
                 approve: c.approve || 0, reject: c.reject || 0, reproducedBy: c.reproducedBy || 0,
                 counts: c, byPredicate: r.byPredicate };
    }
    if (r.as === "cone") return { id: q.id, kind: "cone", ...r };
    // a tally is its own shape. Falling through to the list branch handed back `list: undefined`
    // under a list's label, and the reader tripped over it - the same mistake as stamping a cone
    // with `as`, one layer up.
    if (r.as === "count") return { id: q.id, kind: "count", n: r.n, of: r.of };
    return { id: q.id, kind: "list", list: r.list || [], total: r.of,
             head: { missing: r.n, of: r.of, confirmed: r.of - r.n, found: r.n } };
}

/**
 * The catalogue for a sketch: the built-ins plus whatever the domain adds.
 *
 * A sketch adds a worklist by naming a predicate, not by writing code - "welche SOPs sind nicht
 * freigegeben" is the same shape as "which fotons lack sign-off", and the difference between two
 * domains should stay data. See sketches/*.json.
 */
export function catalogue(spec) {
    const extra = (spec && spec.questions) || [];
    const byId = new Map(BUILTIN.map((q) => [q.id, q]));
    for (const q of extra) byId.set(q.id, { at: "graph", from: "fotons", as: "list", ...byId.get(q.id), ...q });
    return [...byId.values()];
}
