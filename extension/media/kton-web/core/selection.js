/**
 * A set of selected steps, settable and readable from outside.
 *
 * A host (the editor, a guide) works with what IT knows: a foton's full id, or a file it has open.
 * The graph works with shortened node ids. This file translates both ways - what a host sends
 * becomes node ids, and what the reader selected goes back as full ids with each step's command
 * and output files - so neither side has to know the other's naming.
 *
 * Only fotons (steps) are selectable. Pure: no DOM, no drawing.
 */

const strip = (h) => String(h || "").replace(/^sha256:/, "").toLowerCase();

/** Full foton ids by graph node id (a node id is a prefix of the record id's hex). */
export function fotonIds(records) {
    const m = new Map();
    for (const r of records || []) if (r && r.fotonId) { const h = strip(r.fotonId); m.set(h.slice(0, 32), "sha256:" + h); }
    return m;
}

/**
 * What a host sent -> node ids of steps in G.
 * Each entry may be a node id, a full or prefixed foton id, or a file hash (meaning the step that
 * produced it - `byFile` resolves that, e.g. the viewer's focus lookup). Unknown entries are
 * reported, not dropped silently: a host that believes it selected three steps must hear that it
 * selected two.
 */
export function resolveSelection(G, entries, byFile) {
    const ids = [], missed = [];
    const isStep = (id) => { const n = id && G.byId.get(id); return n && n.type === "foton" ? id : null; };
    for (const e of Array.isArray(entries) ? entries : [entries]) {
        const s = strip(e);
        let id = isStep(s) || (s.length > 32 ? isStep(s.slice(0, 32)) : null);
        if (!id && byFile) id = isStep(byFile(String(e)));
        if (id) { if (!ids.includes(id)) ids.push(id); } else missed.push(String(e));
    }
    return { ids, missed };
}

/** Add or remove one step. Returns a new set; the old one is not changed. */
export function toggle(set, id) {
    const out = new Set(set);
    if (out.has(id)) out.delete(id); else out.add(id);
    return out;
}

/**
 * What goes back to the host: per selected step, its full id, node id, command and outputs.
 * Steps no longer in the graph (a rebuilt portion) are left out.
 */
export function describe(G, ids, full, info) {
    const out = [];
    for (const id of ids) {
        const n = G.byId.get(id);
        if (!n || n.type !== "foton") continue;
        out.push({ id: (full && full.get(id)) || id, node: id, command: info ? info(n).command : n.role,
                   outputs: (n.outputs || []).map((o) => ({ name: o.name, hash: o.hash })) });
    }
    return out;
}
