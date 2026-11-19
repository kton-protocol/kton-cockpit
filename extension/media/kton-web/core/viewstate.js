/**
 * One address for one question, whichever viewer answers it.
 *
 * The compass and the graph ask different things about the same data - the compass says where you
 * can go and why, the graph shows how much of it there is - so alternating between them is a real
 * move, not indecision. But it is only worth offering if the STATE survives it. A switch that
 * lands the reader in an unfiltered two-thousand-node canvas has thrown away ten clicks of work,
 * and is worse than no switch at all.
 *
 * So the state lives in the URL. Not in a store, not in a message: the URL already crosses page
 * boundaries, survives a reload, can be bookmarked, pasted into a ticket, and handed to a
 * colleague who then sees exactly what was being looked at. For a thing whose whole point is
 * showing someone else what you found, that is not a shortcut - it is the right place.
 *
 *   union, keys, names   where the records come from (absolute, so depth never matters)
 *   focus                the node being stood on - the name lens.js already writes, kept
 *   sketch               which configuration resolves meaning
 *   p                    the perspective
 *   f                    facet selections, "participant:analyst,qc;verified:ja"
 *   d                    how far each direction is followed, "origin:3,effect:2"
 *   q                    the named question being asked (core/questions.js)
 *   g                    how the rows are grouped (core/grouping.js)
 *   mode                 the graph's pedigree mode, carried through the compass untouched
 *
 * A viewer carries keys it does not understand rather than dropping them, so that going compass ->
 * graph -> compass returns what you left, instead of quietly eroding it one hop at a time.
 */

const KEYS = ["union", "keys", "names", "attested", "focus", "sketch", "p", "f", "d", "q", "g", "mode"];

const encodeFacets = (f) => Object.entries(f || {})
    .filter(([, v]) => v && v.length)
    .map(([k, v]) => `${k}:${[...v].join(",")}`).join(";");

const decodeFacets = (s) => Object.fromEntries((s || "").split(";").filter(Boolean).map((part) => {
    const i = part.indexOf(":");
    return i < 0 ? [part, []] : [part.slice(0, i), part.slice(i + 1).split(",").filter(Boolean)];
}));

/** Read the state out of a URL (default: the current page). */
export function readState(href) {
    const u = new URL(href || (typeof location !== "undefined" ? location.href : "http://x/"));
    const out = { extra: {} };
    for (const [k, v] of u.searchParams) {
        if (KEYS.includes(k)) out[k] = v; else out.extra[k] = v;   // carry what we do not understand
    }
    out.facets = decodeFacets(out.f);
    // the compass used to keep its address in the fragment; still honour it, the URL wins
    if (!out.focus && u.hash.length > 1) out.focus = decodeURIComponent(u.hash.slice(1));
    return out;
}

/** Turn a state back into search params, dropping what is empty. */
export function toParams(state) {
    const p = new URLSearchParams();
    for (const k of KEYS) {
        if (k === "f") continue;
        const v = state[k];
        if (v) p.set(k, v);
    }
    const f = encodeFacets(state.facets);
    if (f) p.set("f", f);
    for (const [k, v] of Object.entries(state.extra || {})) if (v) p.set(k, v);
    return p;
}

/** Update the current URL in place - no reload, no history entry per keystroke. */
export function writeState(patch, { push = false } = {}) {
    if (typeof location === "undefined") return null;
    const next = { ...readState(), ...patch };
    if (patch.facets) next.facets = patch.facets;
    const url = new URL(location.href);
    url.search = toParams(next).toString();
    url.hash = "";                                   // the address lives in `focus` now, not the fragment
    const s = url.toString();
    if (push) history.pushState(null, "", s); else history.replaceState(null, "", s);
    return next;
}

/**
 * A link to the other viewer, carrying everything.
 * `viewerHref` is relative to the current page ("../graph/index.html"); the source URLs are made
 * absolute first, because the two viewers sit at different depths and a relative union path that
 * resolved in one would silently point somewhere else in the other.
 */
export function linkTo(viewerHref, state, base) {
    const b = base || (typeof document !== "undefined" ? document.baseURI : "http://x/");
    const abs = (u) => { try { return new URL(u, b).href; } catch { return u; } };
    const s = { ...state, extra: state.extra };
    for (const k of ["union", "keys", "names", "attested"]) if (s[k]) s[k] = abs(s[k]);
    const url = new URL(viewerHref, b);
    url.search = toParams(s).toString();
    return url.toString();
}

/**
 * The same pedigree question, in each viewer's words.
 *
 * The graph names it `anc` / `desc` / `both`; the compass follows a direction to a depth. They are
 * the same intent - how far back, how far forward - so the switch should carry it. Without this a
 * reader who opened a three-deep origin cone lands in the graph on a one-hop neighbourhood and has
 * to ask the question again, which is the erosion this whole file exists to prevent.
 *
 * The compass keeps a depth per direction and the graph keeps one mode, so the mapping loses
 * precision on the way over. That is the honest place to lose it: a mode is coarser, and pretending
 * otherwise would mean inventing a depth the graph never had.
 */
export function modeFromDepth(d) {
    const deep = (k) => (d && d[k] || 1) > 1;
    if (deep("origin") && deep("effect")) return "both";
    if (deep("origin")) return "anc";
    if (deep("effect")) return "desc";
    return "";
}

export function depthFromMode(mode, depth = 3) {
    if (mode === "anc" || mode === "lin") return { origin: depth };
    if (mode === "desc" || mode === "imp") return { effect: depth };
    if (mode === "both") return { origin: depth, effect: depth };
    return {};
}

/** The sibling registry files a union path implies - the same rule the graph viewer uses. */
export function regUrls(unionUrl) {
    const b = String(unionUrl).replace(/[^/]*$/, "");
    return { union: unionUrl, keys: b + "keys.json", names: b + "names.json", attested: b + "attested.json" };
}
