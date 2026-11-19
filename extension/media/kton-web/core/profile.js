/**
 * What a viewer shows — one object, so it can be set from anywhere.
 *
 * The full view is the right default for someone exploring a corpus; inside an editor it is too
 * much: there the reader wants the graph, what each step did and why, and its files. Rather than a
 * second viewer, the view is a PROFILE: a plain object that switches the pieces on and off. It
 * arrives the same three ways as every other piece of state - the URL (`?view=extension`, or
 * inline JSON), the embedding host (`kton:set {view}`), and anything that speaks to the host, a
 * guide or an LLM included. Nothing here draws; the viewer reads the resolved profile.
 *
 * A profile is a preset name plus overrides: {preset:"extension", panel:{hashes:true}}.
 */

/** Everything on: the viewer as it has always been. */
const FULL = {
    chrome: { header: true, stats: true, controls: true, legend: true, hint: true, overview: true },
    node:   { label: "name",      // "name" (participant · step) | "command" (the step alone) | "description" (what its describes claim says, else the command)
              tooltip: false,     // hover a step: its command, description and rationale
              font: 11,           // a step's label size on the canvas, in px at scale 1 (a talk wants ~14)
              wrap: 0 },          // >0: wrap a step's label at this many characters, up to three lines
    panel:  { title: "full",      // "full" (participant + kind) | "command" (the step's command)
              annotations: true,  // description and rationale, from verified claims
              identity: true,     // id, signer, verdict badges
              navigate: true, marks: true, file: true, related: true, text: true,
              inputs: true, outputs: true,
              hashes: true },     // content hashes next to file names
    // not drawn: a node type ("claim") or a type:kind ("source:code" - the scripts a step ran; they
    // are listed with the step's inputs and named on hover, and as dots they crowd the left edge)
    graph:  { hide: ["source:code"],
              // "auto": top-down (parent above child) when the view holds a model development
              // (parent-step relations), the declared default otherwise; or a layout id
              layout: "auto",
              // "endpoints": the outputs of the flow's last steps as nodes (the kernel draws none)
              // "auto": so when a model tree is in view; false: never
              outputs: "auto",
              // "rationale": a step's rationale as a note beside it; "auto" / false as above
              notes: "auto",
              // steps only a recorder saw (?graph= beside a union): "show" | "hide" (a switch brings them back)
              recorded: "show",
              // without ?focus=: "overview" | "lineage" (the ancestry of the step most of the work leads to)
              start: "overview" },
    // several steps at once: ctrl/⌘- or shift-click adds or removes one; read and set by the host
    select: { multi: false },
    actions: [],                  // host-provided actions on files: [{id, label, on:"input"|"output"|"both"}]
};

/** The graph and what each step did: for the editor. */
const EXTENSION = {
    chrome: { header: false, stats: false, controls: false, legend: false, hint: false, overview: false },
    node:   { label: "command", tooltip: true },
    panel:  { title: "command", annotations: true, identity: false, navigate: false, marks: false,
              file: false, related: false, text: false, inputs: true, outputs: true, hashes: false },
    graph:  { hide: ["claim", "source:code"] },  // a step's description and rationale are at the step now
};

/**
 * A model development (pmx): what the Metworx demo worked out, MH 11.11. Steps named by what the
 * modeler wrote, statements folded into their steps (marks as badges, rationale as notes), only the
 * files several steps share; the layout puts the parent-step tree inside the data flow.
 */
const PMX = {
    node:   { label: "description", tooltip: true },
    graph:  { hide: ["claim", "source:code", "source:single-use"], layout: "auto", outputs: "auto", notes: "auto",
              recorded: "hide", start: "lineage" },
};

export const PRESETS = { full: FULL, extension: EXTENSION, pmx: PMX };

const isObj = (x) => x && typeof x === "object" && !Array.isArray(x);

/** Deep merge: b's keys win, arrays are replaced (an action list is a whole, not a patch). */
function merge(a, b) {
    const out = { ...a };
    for (const [k, v] of Object.entries(b || {})) out[k] = isObj(v) && isObj(a[k]) ? merge(a[k], v) : v;
    return out;
}

/**
 * Resolve a profile spec into a complete profile.
 *   undefined | ""            -> full
 *   "extension"               -> that preset
 *   '{"preset":"extension"…}' -> JSON, from a URL
 *   {preset, ...overrides}    -> preset (default full) with overrides on top
 * Unknown preset names and unparsable JSON fall back to full and say so in `.problems`, rather
 * than throw: a host with a typo still gets a working viewer, and is told.
 */
export function resolveProfile(spec) {
    const problems = [];
    let s = spec;
    if (typeof s === "string") {
        const t = s.trim();
        if (t.startsWith("{")) { try { s = JSON.parse(t); } catch (e) { problems.push(`view is not JSON: ${e.message}`); s = {}; } }
        else s = t ? { preset: t } : {};
    }
    if (!isObj(s)) s = {};
    const name = s.preset || "full";
    let base = PRESETS[name];
    if (!base) { problems.push(`no preset "${name}" (have: ${Object.keys(PRESETS).join(", ")})`); base = {}; }
    const { preset, ...over } = s;
    // every preset is a patch over FULL, so a profile always has every key
    const p = merge(merge(FULL, base), over);
    p.preset = PRESETS[name] ? name : "full";
    p.actions = (Array.isArray(p.actions) ? p.actions : []).filter((a) => a && a.id)
        .map((a) => ({ id: String(a.id), label: String(a.label || a.id), on: a.on || "both" }));
    p.problems = problems;
    return p;
}

/** Apply a change on top of a resolved profile (a host or a guide adjusting one thing). */
export function adjustProfile(current, change) {
    if (typeof change === "string" || (isObj(change) && change.preset)) return resolveProfile(change);
    const { preset, problems, ...cur } = current;
    return resolveProfile({ ...merge(cur, change || {}), preset });
}
