/**
 * Property paths - how a declaration says "go this way", instead of naming a field per nuance.
 *
 * Grouping used to declare `edges: ['builds-on']`, and a claim (whose only way up is `about`) could
 * not be grouped by the analysis it plainly belongs to. The fix was a second field, `enter`. The
 * one after it - "group by the analysis, but only through a review that was signed" - would have
 * been a third. That is a query language accreting one field at a time, written by whoever hits the
 * next case. So the declaration takes a PATH, and the fields go away.
 *
 * The syntax is a strict subset of SPARQL 1.1 property paths, deliberately: this is the seam where
 * a real graph language attaches later, and a bespoke notation would have to be translated away
 * first. What is here:
 *
 *   a          one step along edges of kind `a`
 *   ^a         one step against them
 *   a/b        a, then b
 *   a|b        a or b
 *   a*         zero or more a          (closure - budgeted, see below)
 *   a+         one or more a           (closure)
 *   a?         zero or one a
 *   (a|b)/c    grouping
 *
 * What is NOT here, and why: negated sets (`!a`), arbitrary nesting of closures inside closures,
 * and anything that binds a variable. The first two make cost hard to predict by reading, and the
 * third is a join - which core/query.js refuses on purpose, for the same reason. A path that cannot
 * be written here is a reason to widen this on purpose, not to smuggle a traversal in beside it.
 *
 * COST. `*` and `+` are precisely the unbounded part. Every evaluation carries a budget of node
 * visits and returns `truncated` when it runs out. A truncated answer is reported as truncated and
 * never quietly returned as if it were complete - a short list that looks whole is the failure mode
 * this kit keeps finding in itself.
 */

const NAME = /[A-Za-z0-9_:.\-]/;

/** Tokenise. Kind names carry `:` and `-` (dcat:downloadURL, builds-on), so they are name chars. */
function lex(src) {
    const ts = [];
    for (let i = 0; i < src.length;) {
        const c = src[i];
        if (c === " " || c === "\t") { i++; continue; }
        if ("/|*+?()^".includes(c)) { ts.push({ t: c }); i++; continue; }
        if (NAME.test(c)) {
            let j = i;
            while (j < src.length && NAME.test(src[j])) j++;
            ts.push({ t: "name", v: src.slice(i, j) });
            i = j;
            continue;
        }
        throw new SyntaxError(`unexpected character ${JSON.stringify(c)} at ${i} in path ${JSON.stringify(src)}`);
    }
    return ts;
}

/** path := alt ; alt := seq ('|' seq)* ; seq := unary ('/' unary)* ; unary := atom [*+?] */
export function parsePath(src) {
    if (typeof src !== "string" || !src.trim()) throw new SyntaxError("empty path");
    const ts = lex(src);
    let p = 0;
    const peek = () => ts[p] && ts[p].t;
    const eat = (t) => { if (peek() !== t) throw new SyntaxError(`expected ${t} in path ${JSON.stringify(src)}`); return ts[p++]; };

    function atom() {
        let inv = false;
        while (peek() === "^") { p++; inv = !inv; }
        if (peek() === "(") { p++; const x = alt(); eat(")"); return inv ? { t: "inv", x } : x; }
        if (peek() !== "name") throw new SyntaxError(`expected an edge kind in path ${JSON.stringify(src)}`);
        return { t: "step", kind: ts[p++].v, inv };
    }
    function unary() {
        const x = atom();
        const q = peek();
        if (q === "*") { p++; return { t: "rep", x, min: 0, max: Infinity }; }
        if (q === "+") { p++; return { t: "rep", x, min: 1, max: Infinity }; }
        if (q === "?") { p++; return { t: "rep", x, min: 0, max: 1 }; }
        return x;
    }
    function seq() {
        const xs = [unary()];
        while (peek() === "/") { p++; xs.push(unary()); }
        return xs.length === 1 ? xs[0] : { t: "seq", xs };
    }
    function alt() {
        const xs = [seq()];
        while (peek() === "|") { p++; xs.push(seq()); }
        return xs.length === 1 ? xs[0] : { t: "alt", xs };
    }
    const ast = alt();
    if (p !== ts.length) throw new SyntaxError(`trailing input in path ${JSON.stringify(src)}`);
    return ast;
}

/** Render an AST back to source - so what a viewer shows and what a sketch wrote are the same text. */
export function formatPath(ast) {
    const wrap = (x, s) => (x.t === "alt" || x.t === "seq" ? `(${s})` : s);
    switch (ast.t) {
        case "step": return (ast.inv ? "^" : "") + ast.kind;
        case "inv": return "^" + wrap(ast.x, formatPath(ast.x));
        case "seq": return ast.xs.map((x) => wrap(x, formatPath(x))).join("/");
        case "alt": return ast.xs.map(formatPath).join("|");
        case "rep": {
            const s = wrap(ast.x, formatPath(ast.x));
            return s + (ast.max === 1 ? "?" : ast.min === 0 ? "*" : "+");
        }
        default: return "";
    }
}

/** One step: the nodes reachable from `ids` across edges of `kind`, in `inv ? in : out` direction. */
function step(G, ids, kind, inv) {
    const out = new Set();
    for (const id of ids) {
        for (const e of ((inv ? G.in : G.out)[id] || [])) {
            if (e.kind !== kind) continue;
            out.add(inv ? e.from : e.to);
        }
    }
    return out;
}

/**
 * Evaluate `ast` from `start`. Returns {ends, truncated}.
 *
 * `ends` is the SET the path reaches, in no particular order and without duplicates - a path is not
 * a tree walk and a node reached two ways is one node. `budget` counts node visits across the whole
 * evaluation, closures included.
 */
export function evalPath(G, start, ast, { budget = 20000 } = {}) {
    const state = { spent: 0, truncated: false };
    const charge = (n) => { state.spent += n; if (state.spent > budget) state.truncated = true; return !state.truncated; };

    const go = (ids, node) => {
        if (state.truncated || !ids.size) return ids;
        switch (node.t) {
            case "step": {
                if (!charge(ids.size)) return new Set();
                return step(G, ids, node.kind, node.inv);
            }
            case "inv": {
                // ^( ... ) - invert every step inside, which is what SPARQL means by it
                return go(ids, invert(node.x));
            }
            case "seq": {
                let cur = ids;
                for (const x of node.xs) { cur = go(cur, x); if (state.truncated || !cur.size) break; }
                return cur;
            }
            case "alt": {
                const out = new Set();
                for (const x of node.xs) { for (const v of go(ids, x)) out.add(v); if (state.truncated) break; }
                return out;
            }
            case "rep": {
                const out = new Set();
                if (node.min === 0) for (const v of ids) out.add(v);
                let frontier = ids, n = 0;
                while (frontier.size && n < node.max) {
                    const next = new Set();
                    for (const v of go(frontier, node.x)) if (!out.has(v)) next.add(v);
                    if (state.truncated) break;
                    for (const v of next) out.add(v);
                    if (node.min > 0 && n === 0) for (const v of next) out.add(v);
                    frontier = next;
                    n++;
                }
                if (node.min > 0 && n === 0) return new Set();
                return out;
            }
            default: return ids;
        }
    };

    const ends = go(new Set([start]), ast);
    return { ends, truncated: state.truncated };
}

/** Flip every step in a subtree - used by `^( ... )`. */
function invert(ast) {
    switch (ast.t) {
        case "step": return { t: "step", kind: ast.kind, inv: !ast.inv };
        case "seq": return { t: "seq", xs: [...ast.xs].reverse().map(invert) };
        case "alt": return { t: "alt", xs: ast.xs.map(invert) };
        case "rep": return { t: "rep", x: invert(ast.x), min: ast.min, max: ast.max };
        case "inv": return ast.x;
        default: return ast;
    }
}

/**
 * The steps a path can still take - the kinds that appear under an unbounded closure.
 *
 * "Which analysis is this" wants the FURTHEST node the path reaches, not every node along the way,
 * and `a*` reaches every one of them. So a root is an end that cannot advance along the part of the
 * path that repeats. Derived from the AST rather than declared again, so the two cannot disagree.
 */
export function closureSteps(ast, acc = []) {
    switch (ast.t) {
        case "rep":
            if (ast.max === Infinity) collectSteps(ast.x, acc);
            closureSteps(ast.x, acc);
            break;
        case "seq": case "alt": for (const x of ast.xs) closureSteps(x, acc); break;
        case "inv": closureSteps(invert(ast.x), acc); break;
    }
    return acc;
}

function collectSteps(ast, acc) {
    switch (ast.t) {
        case "step": acc.push({ kind: ast.kind, inv: ast.inv }); break;
        case "seq": case "alt": for (const x of ast.xs) collectSteps(x, acc); break;
        case "rep": collectSteps(ast.x, acc); break;
        case "inv": collectSteps(invert(ast.x), acc); break;
    }
}

/** Can `id` take any of `steps`? A node that cannot is where a climb ends. */
export function canAdvance(G, id, steps) {
    for (const s of steps) {
        for (const e of ((s.inv ? G.in : G.out)[id] || [])) if (e.kind === s.kind) return true;
    }
    return false;
}
