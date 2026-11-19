/**
 * Layout, declared rather than written.
 *
 * Placement was a constant in the drawing code: a layer is a column, DX=210, DY=60, and that was
 * the whole vocabulary. It held until a real question produced a real shape - the ancestry of one
 * rendered report is 250 nodes whose widest layer holds 97, which as a single column is 5820 units
 * against 2730 of width. The view fits to the taller side, so the picture collapses into a bar. The
 * numbers are not pathological; they are what a report that aggregates fifty figures looks like.
 *
 * There is no single right placement for that, which is exactly why it does not belong in the code.
 * A reader auditing one lineage wants depth left-to-right; a reader comparing branches wants the
 * wide layer opened out; a phone wants the whole thing turned ninety degrees. So layouts are
 * DECLARED beside groupings and perspectives, `describe` hands them out, and whoever is steering -
 * a person with a dropdown or an agent with kton_show - picks one by id.
 *
 *   {by:'layers', dx, dy, wrap, subdx, orient}
 *
 *     dx      distance between layers          dy     distance between nodes within a layer
 *     wrap    max nodes before a layer breaks into a second sub-column (0 = never break)
 *     subdx   distance between those sub-columns (default: 45% of dx)
 *     orient  'lr' layers run left-to-right | 'tb' top-to-bottom (portrait)
 *
 * `wrap` is the whole answer to the bar: the same 97 nodes as a block of 14 rows is 840 units tall
 * and 7 sub-columns wide, which fits a screen. Nothing is hidden and no layer changes meaning - a
 * layer is still one depth, just folded.
 */

const BUILTIN = [
    { id: "lineage", label: "Abstammung", by: "layers", dx: 210, dy: 60, wrap: 0, orient: "lr",
      note: "layers left to right, one layer per column - the shape for a narrow, deep chain" },
    { id: "breit", label: "breite Ebenen", by: "layers", dx: 210, dy: 60, wrap: 14, orient: "lr",
      note: "wie lineage, aber eine Ebene mit vielen Knoten klappt in einen Block statt in ein Band" },
    { id: "hoch", label: "hochkant", by: "layers", dx: 76, dy: 96, wrap: 10, orient: "tb",
      note: "layers top to bottom - for a narrow, tall window" },
    { id: "entwicklung", label: "Modellentwicklung", by: "layers", dx: 96, dy: 300, wrap: 6, orient: "tb",
      note: "parents above children: a model development reads top to bottom (run001 → … → run064)" },
    { id: "baum", label: "top to bottom", by: "layers", dx: 170, dy: 280, orient: "tb", route: "orthogonal",
      note: "everything top to bottom - data, the steps, a step based on another directly below it, the deliverables - every edge down, across, down" },
    { id: "fluss-baum", label: "Fluss mit Modellbaum", by: "mixed", dx: 300, dy: 150, tdx: 330, tdy: 170, route: "orthogonal",
      note: "the data flow left to right (data → runs → deliverables), only the model tree (parent-step) top to bottom" },
];

/** Declared layouts: the built-ins, with a sketch's own merged over them by id. */
export function catalogue(spec) {
    const byId = new Map(BUILTIN.map((l) => [l.id, l]));
    for (const l of (spec && spec.layouts) || []) byId.set(l.id, { ...byId.get(l.id), ...l });
    return [...byId.values()];
}

/** Which one to start with: the sketch says, or the first declared, or `breit`. */
export function defaultLayout(spec) {
    const cat = catalogue(spec);
    const named = spec && spec.defaultLayout;
    return cat.find((l) => l.id === named) || cat.find((l) => l.default) || cat.find((l) => l.id === "breit") || cat[0];
}

/**
 * Build a placement function from a declaration. Returns (nodes, edges, byId) => void, setting
 * n.x / n.y. Unknown `by` falls back to layers rather than throwing: a sketch is data, and a viewer
 * that renders nothing because one field was misspelt is worse than one that renders plainly.
 */
export function makeLayout(spec) {
    const s = spec || {};
    if (s.by === "mixed") return mixed(s);
    const dx = s.dx || 210, dy = s.dy || 60, wrap = s.wrap || 0;
    // `fold: "auto"` decides from the SHAPE OF THE WINDOW, not from a constant. Every fixed number
    // in this file is right for one picture: wrap 14 does nothing to eleven nodes in eight layers,
    // and a chain stays a ribbon however wide the canvas is. `aspect` is width/height of the strip
    // the picture actually gets; the viewer knows it, this does not.
    const foldSpec = s.fold || 0, aspect = s.aspect || 1.6;
    const subdx = s.subdx || Math.round(dx * 0.45), tb = s.orient === "tb";

    return function place(nodes, edges, byId) {
        const has = new Set(nodes.map((n) => n.id)), anc = {};
        for (const e of edges) if (has.has(e.from) && has.has(e.to)) (anc[e.from] ??= []).push(e.to);

        // depth = longest way back within the view; a cycle resolves to 0 rather than recurring
        const depth = {}, seen = {};
        const d = (id) => {
            if (id in depth) return depth[id];
            if (seen[id]) return (depth[id] = 0);
            seen[id] = 1;
            let m = 0;
            for (const t of (anc[id] || [])) if (has.has(t)) m = Math.max(m, d(t) + 1);
            return (depth[id] = m);
        };
        nodes.forEach((n) => (n._d = d(n.id)));

        const layers = {}, maxD = Math.max(0, ...nodes.map((n) => n._d));
        nodes.forEach((n) => (layers[n._d] ??= []).push(n));
        nodes.forEach((n) => (n._y = 0));
        // barycentre passes: pull each node towards the mean of what it descends from, so the
        // edges between layers stop crossing each other more than they have to
        for (let pass = 0; pass < 5; pass++) for (let L = 0; L <= maxD; L++) {
            const lay = layers[L] || [];
            lay.forEach((n) => {
                const ps = (anc[n.id] || []).map((id) => byId.get(id)).filter((p) => p && has.has(p.id) && p._d < L);
                n._b = ps.length ? ps.reduce((sum, p) => sum + p._y, 0) / ps.length : n._y;
            });
            lay.sort((a, b) => (a._b - b._b) || String(a.participant).localeCompare(String(b.participant)));
            lay.forEach((n, i) => (n._y = i));
        }

        // `wrap` folds a layer that is too WIDE. `fold` folds the sequence of layers when it is too
        // LONG - a different bar with the same cause. Eleven steps in eight layers is a thin ribbon
        // however wide the window is, and no within-layer wrap can help: there is nothing to wrap.
        // With fold=4 the same eight layers become two bands of four, and the picture matches the
        // shape of the canvas instead of one edge of it.
        // solve fold*dx / (ceil(L/fold) * bandHeight) ~= aspect for fold
        const fold = foldSpec === "auto"
            ? Math.max(2, Math.min(maxD + 1, Math.round(Math.sqrt((maxD + 1) * aspect * 2.6 * dy / dx))))
            : (foldSpec || 0);
        const bandOf = (L) => (fold > 0 ? Math.floor(L / fold) : 0);
        const posInBand = (L) => (fold > 0 ? L % fold : L);

        // how deep each band runs, so the next band starts clear of it
        const bandDepth = {};
        for (let L = 0; L <= maxD; L++) {
            const lay = layers[L] || [];
            const cols = wrap > 0 ? Math.max(1, Math.ceil(lay.length / wrap)) : 1;
            const rows = Math.ceil(lay.length / cols) || 1;
            const b = bandOf(L);
            bandDepth[b] = Math.max(bandDepth[b] || 0, rows);
        }
        const bandStart = {}; let acc = 0;
        for (const b of Object.keys(bandDepth).map(Number).sort((a, z) => a - z)) {
            bandStart[b] = acc; acc += (bandDepth[b] + 1.6) * dy;
        }

        const alongOf = {}; let along = 0, band = 0;     // distance travelled across layers so far
        for (let L = 0; L <= maxD; L++) {
            if (bandOf(L) !== band) { band = bandOf(L); along = 0; }
            alongOf[L] = along;
            const lay = layers[L] || [];
            const cols = wrap > 0 ? Math.max(1, Math.ceil(lay.length / wrap)) : 1;
            along += (cols - 1) * subdx + dx;
        }

        for (let L = 0; L <= maxD; L++) {
            const lay = layers[L] || [];
            const cols = wrap > 0 ? Math.max(1, Math.ceil(lay.length / wrap)) : 1;
            const rows = Math.ceil(lay.length / cols) || 1;
            const off = (rows - 1) / 2;
            const shift = fold > 0 ? bandStart[bandOf(L)] : 0;
            lay.forEach((n, i) => {
                const c = Math.floor(i / rows), r = i % rows;
                const across = (r - off) * dy + shift, into = alongOf[L] + c * subdx;
                if (tb) { n.x = across; n.y = into; } else { n.x = into; n.y = across; }
            });
        }
        void posInBand;
    };
}

/**
 * Two directions in one picture. A model development is a tree (parent above child, `parent-step`
 * between steps), and it sits inside a flow (data in, deliverables out) that reads left to right.
 * Either layout alone draws one of them wrong: all left-to-right flattens the tree into a fan, all
 * top-down stands the data flow on its head.
 *
 * So the steps the tree joins are placed as a block, top-down, and the block is ONE layer of the
 * flow: what feeds it stands left of it, what it feeds stands right of it. Without a tree in view
 * this is the plain left-to-right layering.
 *
 *   {by:'mixed', dx, dy, tdx, tdy}   dx/dy: flow layers and nodes in a layer · tdx/tdy: siblings and generations in the tree
 */
function mixed(s) {
    const dx = s.dx || 280, dy = s.dy || 70, tdx = s.tdx || 250, tdy = s.tdy || 120;
    const plain = makeLayout({ by: "layers", dx, dy, orient: "lr" });
    return function place(nodes, edges, byId) {
        const has = new Set(nodes.map((n) => n.id));
        const treeE = edges.filter((e) => e.step && e.kind === "parent-step" && has.has(e.from) && has.has(e.to) && e.from !== e.to);
        if (!treeE.length) return plain(nodes, edges, byId);
        const T = new Set(treeE.flatMap((e) => [e.from, e.to]));

        // the tree: generation = longest way up to a root; siblings in order of first visit, a parent
        // over the middle of its children (a tidy tree, enough for tens of runs)
        const up = {}, kids = {};
        for (const e of treeE) { (up[e.from] ??= []).push(e.to); (kids[e.to] ??= []).push(e.from); }
        const gen = {}, busy = {};
        const g = (id) => { if (id in gen) return gen[id]; if (busy[id]) return (gen[id] = 0); busy[id] = 1;
            let m = 0; for (const p of up[id] || []) m = Math.max(m, g(p) + 1); return (gen[id] = m); };
        T.forEach(g);
        const label = (id) => String((byId.get(id) || {}).role || id);
        for (const k in kids) kids[k].sort((a, b) => label(a).localeCompare(label(b), undefined, { numeric: true }));
        const slot = {}, done = new Set(); let next = 0;
        const put = (id) => { if (done.has(id)) return; done.add(id);
            const ks = (kids[id] || []).filter((k) => !done.has(k) && gen[k] > gen[id]);
            ks.forEach(put);
            slot[id] = ks.length ? ks.reduce((a, k) => a + slot[k], 0) / ks.length : next++; };
        [...T].filter((id) => !(up[id] || []).length).sort((a, b) => label(a).localeCompare(label(b), undefined, { numeric: true })).forEach(put);
        T.forEach(put);                                          // anything a cycle left out
        const width = Math.max(0, ...Object.values(slot)) * tdx, height = Math.max(0, ...[...T].map((id) => gen[id])) * tdy;

        // the flow: the tree is one node of it
        const rep = (id) => (T.has(id) ? "@tree" : id);
        const anc = {};
        for (const e of edges) {
            if (!has.has(e.from) || !has.has(e.to) || (e.step && e.kind === "parent-step")) continue;
            const f = rep(e.from), t = rep(e.to);
            if (f !== t) (anc[f] ??= new Set()).add(t);
        }
        const depth = {}, seen = {};
        const d = (id) => { if (id in depth) return depth[id]; if (seen[id]) return (depth[id] = 0); seen[id] = 1;
            let m = 0; for (const t of anc[id] || []) m = Math.max(m, d(t) + 1); return (depth[id] = m); };
        // satellites: what only points INTO the tree and nothing builds on - the statements about a
        // step (describes, marks, rationale, the parent-step box itself). As a flow layer they were
        // one tall column beside the tree; they belong next to the step they are about.
        const pointedAt = new Set(edges.filter((e) => has.has(e.from) && has.has(e.to)).map((e) => e.to));
        const intoTree = (n) => { const out = edges.filter((e) => e.from === n.id && has.has(e.to));
            return out.length && out.every((e) => T.has(e.to)) ? out.find((e) => e.kind === "about") || out[0] : null; };
        const sat = new Map();
        for (const n of nodes) { if (T.has(n.id) || pointedAt.has(n.id)) continue; const e = intoTree(n); if (e) sat.set(n.id, e.to); }
        const rest = nodes.filter((n) => !T.has(n.id) && !sat.has(n.id));
        const LT = d("@tree");
        const xOf = (L) => L * dx + (L > LT ? width : 0);

        for (const n of nodes) if (T.has(n.id)) { n._d = LT; n.x = xOf(LT) + slot[n.id] * tdx; n.y = gen[n.id] * tdy - height / 2; }
        const per = {};
        for (const n of nodes) { const at = sat.get(n.id); if (!at) continue; const s = byId.get(at), i = (per[at] = (per[at] || 0) + 1);
            n._d = LT; n.x = s.x + 34; n.y = s.y + 8 + i * 15; }
        const layers = {};
        for (const n of rest) (layers[n._d = d(n.id)] ??= []).push(n);
        // a node beside the tree sits level with what it is joined to, as far as its layer allows
        const yOf = (id) => { const n = byId.get(id); return n && T.has(id) ? n.y : n && typeof n.y === "number" ? n.y : 0; };
        for (const L of Object.keys(layers).map(Number).sort((a, b) => a - b)) {
            const lay = layers[L];
            for (const n of lay) {
                const near = [...(anc[n.id] || [])].flatMap((t) => (t === "@tree" ? [...T] : [t]))
                    .concat(edges.filter((e) => e.to === n.id && has.has(e.from)).map((e) => e.from));
                const ys = near.map(yOf).filter((y) => Number.isFinite(y));
                n._b = ys.length ? ys.reduce((a, y) => a + y, 0) / ys.length : 0;
            }
            lay.sort((a, b) => a._b - b._b || label(a.id).localeCompare(label(b.id)));
            const off = (lay.length - 1) / 2, mid = lay.reduce((a, n) => a + n._b, 0) / lay.length;
            lay.forEach((n, i) => { n.x = xOf(L); n.y = mid + (i - off) * dy; });
        }
    };
}
