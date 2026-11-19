/**
 * Steering a viewer from outside it.
 *
 * A viewer is rarely the whole application. It sits in a verticle, an editor, a review tool - and
 * that host knows things the viewer cannot: which file is open, who is logged in, what the reader
 * was doing a moment ago. So the host must be able to say "show this node, under this perspective,
 * with these filters", and the viewer must be able to say back "the reader asked to open this
 * file" - which only the host can actually do.
 *
 * Two rules keep this from becoming a mess.
 *
 * ONE STATE. The URL already carries focus, perspective, facets and source (core/viewstate.js).
 * A message does not introduce a second model: it hands over the same state object, the viewer
 * applies it, and the URL follows. So a host-driven view can still be copied out of the address
 * bar and pasted into a ticket, and a reload lands where the host put it.
 *
 * THE VIEWER PERFORMS NOTHING. It reports that the reader asked for something and names it. What
 * "open in the editor" means is the host's business - the same line the compass element already
 * holds against its page, held once more against the embedding application. A viewer that reached
 * for an editor would be guessing about a world it cannot see.
 *
 *   host -> viewer   kton:set      {state}          apply this view
 *                    kton:ask      {want}           'state' | 'capabilities'
 *   viewer -> host   kton:ready    {capabilities, state}
 *                    kton:state    {state}          the reader moved, or the host's set landed
 *                    kton:intent   {intent}         navigate | act | reveal | mask
 *                    kton:refused  {what, why}      asked for something this viewer cannot do
 *
 * This is the v2 wire (kept for one release). The v3 bridge agreed for compositions (C1.5 v3,
 * VERTICLE-HOSTS.md §11-16) is `connect()` below; new code uses that.
 *
 * `kton:refused` matters more than it looks. A host that sets a facet the sketch does not declare,
 * or a node the corpus does not hold, must be told - otherwise it believes it steered and the
 * reader sees something else, and nobody finds out which of the two was wrong.
 */

import { originAllowed } from "./origins.js";

const NS = "kton:";

/** Origins to trust. Defaults to the embedder; a page may widen it deliberately via ?host=origin. */
function allowedOrigins(explicit) {
    const out = new Set();
    for (const o of explicit || []) out.add(o);
    if (typeof location !== "undefined") {
        out.add(location.origin);
        const p = new URLSearchParams(location.search).get("host");
        if (p) for (const o of p.split(",").map((s) => s.trim()).filter(Boolean)) out.add(o);
    }
    return out;
}

/**
 * The viewer side. Give it what it can do and how to apply a state; it wires up the rest.
 *
 *   const host = hostBridge({
 *     capabilities: { facets: [...], perspectives: [...], sketch: 'default' },
 *     apply: (state) => { ... return {applied, refused} },
 *     current: () => readState(),
 *   });
 *   host.intent({type:'act', action:'open', node});   // bubbles out; also fine if nobody listens
 */
export function hostBridge({ capabilities = {}, apply, current, origins } = {}) {
    const embedded = typeof window !== "undefined" && window.parent && window.parent !== window;
    const allow = allowedOrigins(origins);
    const post = (type, body) => {
        if (!embedded) return;
        // '*' would broadcast a corpus view to any page that happens to hold the frame; name the
        // origins instead, even though it means one message per trusted origin
        for (const o of allow) { try { window.parent.postMessage({ type: NS + type, ...body }, o); } catch { /* not that one */ } }
    };

    const api = {
        embedded,
        state: (state) => post("state", { state }),
        intent: (intent) => post("intent", { intent }),
        refused: (what, why) => post("refused", { what, why }),
        ready: () => post("ready", { capabilities, state: current ? current() : null }),
    };

    if (typeof window !== "undefined") window.addEventListener("message", (ev) => {
        if (!ev.data || typeof ev.data.type !== "string" || !ev.data.type.startsWith(NS)) return;
        if (!allow.has(ev.origin)) { return; }              // silence is the right answer to a stranger
        const kind = ev.data.type.slice(NS.length);
        if (kind === "set" && apply) {
            const r = apply(ev.data.state || {}) || {};
            if (r.refused && r.refused.length) api.refused(r.refused, r.why || "not available in this view");
            api.state(current ? current() : null);
        }
        if (kind === "ask") {
            if (ev.data.want === "capabilities") api.ready();
            else api.state(current ? current() : null);
        }
    });

    return api;
}

/**
 * The host side. Wraps an iframe so a verticle does not have to know the wire format.
 *
 *   const v = embedKton(iframeEl, { onIntent, onState });
 *   v.set({ focus: nodeId, p: 'auditor', facets: { participant: ['qc'] } });
 */
export function embedKton(frame, { onIntent, onState, onReady, onRefused } = {}) {
    const target = () => new URL(frame.src, location.href).origin;
    const send = (type, body) => frame.contentWindow &&
        frame.contentWindow.postMessage({ type: NS + type, ...body }, target());

    window.addEventListener("message", (ev) => {
        if (ev.source !== frame.contentWindow) return;      // another frame's traffic is not ours
        if (!ev.data || typeof ev.data.type !== "string" || !ev.data.type.startsWith(NS)) return;
        const kind = ev.data.type.slice(NS.length);
        if (kind === "ready" && onReady) onReady(ev.data.capabilities, ev.data.state);
        if (kind === "state" && onState) onState(ev.data.state);
        if (kind === "intent" && onIntent) onIntent(ev.data.intent);
        if (kind === "refused" && onRefused) onRefused(ev.data.what, ev.data.why);
    });

    return {
        set: (state) => send("set", { state }),
        ask: (want = "state") => send("ask", { want }),
        frame,
    };
}

/* ============================================================================================== *
 * v3: the bridge for compositions (C1.5 v3, 19.11.2026; VERTICLE-HOSTS.md §11-16)
 *
 *   host -> block   kton:set      {state, plan, host: {name, provides}}   after every ready
 *                   kton:data     {id, result | error}                    a request answered, an intent done
 *                   kton:refused  {what, why}                             what = a request's or intent's id
 *                   kton:ask      {want}                                  v2, kept one release
 *   block -> host   kton:ready    {requires, optional, accepts, state}
 *                   kton:state    {state}
 *                   kton:request  {id, cap, args, vars}
 *                   kton:intent   {intent: {id, kind, …}}
 *                   kton:refused  {what, why}                             a set key this block does not accept
 *
 * Ids are `<use path>#<load>.<n>`: <load> is random per load, so an answer meant for a frame that VS Code
 * discarded and reloaded cannot match a new request with the same number. Requests and intents share the
 * id space. Initial inputs are request answers too: data never arrives unasked.
 *
 * Held acts: a set may carry state.acts (outcomes of acts the host performed while the frame was away).
 * They are handed to onSet separately and never come back in a kton:state: a copied URL must not claim
 * a signature.
 * ============================================================================================== */


// acquireVsCodeApi() may be called only once per webview; keep the handle for the module's lifetime.
let vscodeApi;
function vscode() {
    if (vscodeApi === undefined) vscodeApi = typeof acquireVsCodeApi === "function" ? acquireVsCodeApi() : null; // eslint-disable-line no-undef
    return vscodeApi;
}

/** A random per-load token (12 hex). */
export function loadToken() {
    return [...crypto.getRandomValues(new Uint8Array(6))].map((b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * The block side of the v3 bridge, for a single viewer (a leaf: its plan is its own use) and for the
 * composition frame (its plan is the whole plan) alike.
 *
 *   const bridge = connect({
 *     requires: ["graph@1"], optional: ["open@1"], accepts: {...},  // viewer.json
 *     current: () => state,                                           // what kton:state reports
 *     onSet: ({state, acts, plan, host, data}) => { ... return {refused: [keys], why} },
 *   });
 *   const graph = await bridge.request("graph@1", {});               // rejects with the host's why
 *   const r = await bridge.intent({kind: "open", uri});              // {done, result} | {refused}
 *
 * `win` and `api` exist for tests; a page leaves them out.
 */
export function connect({ requires = [], optional = [], accepts = {}, current = () => null, onSet, origins = [], win, api } = {}) {
    const w = win ?? globalThis.window;
    const port = api !== undefined ? api : vscode();
    const own = w.location.origin;
    const configured = [...origins, ...(new URLSearchParams(w.location.search).get("host") ?? "").split(",").map((o) => o.trim()).filter(Boolean)];
    const load = loadToken();
    const counters = new Map();
    const waiting = new Map(); // id -> {resolve, reject, intent}
    let plan = null;
    let host = null;
    let req = [...requires];
    let opt = [...optional];

    const post = (m) => {
        if (port) return port.postMessage(m);
        if (!w.parent || w.parent === w) return;
        // never '*': one message per origin we would also accept an answer from
        for (const o of new Set([own, ...configured.filter((c) => originAllowed(c, own, configured))])) {
            try { w.parent.postMessage(m, o); } catch { /* not that one */ }
        }
    };
    const accepted = (ev) => originAllowed(ev.origin, own, configured) && (port ? true : ev.source === w.parent);
    const useKey = (use) => use ?? plan?.key ?? "";
    const nextId = (use) => {
        const k = useKey(use);
        const n = (counters.get(k) ?? 0) + 1;
        counters.set(k, n);
        return `${k}#${load}.${n}`;
    };
    const settleAll = (why) => {
        for (const [id, wt] of waiting) wt.intent ? wt.resolve({ refused: { what: id, why } }) : wt.reject(new Error(why));
        waiting.clear();
    };

    const api3 = {
        load,
        get plan() { return plan; },
        get host() { return host; },
        provides: (cap) => (host?.provides ?? []).includes(cap),
        /** Declare what this frame needs (a composition frame derives it from the plan, H2). */
        declare: ({ requires: r, optional: o }) => { if (r) req = [...r]; if (o) opt = [...o]; },
        ready: () => post({ type: NS + "ready", requires: req, optional: opt, accepts, state: current() }),
        state: (state = current()) => post({ type: NS + "state", state }),
        refused: (what, why) => post({ type: NS + "refused", what, why }),
        request: (cap, args = {}, { use, vars } = {}) => new Promise((resolve, reject) => {
            const id = nextId(use);
            waiting.set(id, { resolve, reject });
            post({ type: NS + "request", id, cap, args: { ...args, use: useKey(use) }, ...(vars !== undefined ? { vars } : {}) });
        }),
        intent: (intent, { use } = {}) => new Promise((resolve) => {
            const id = nextId(use);
            waiting.set(id, { resolve, intent: true });
            post({ type: NS + "intent", intent: { ...intent, id } });
        }),
        pending: () => waiting.size,
    };

    w.addEventListener("message", (ev) => {
        const m = ev.data;
        if (!m || typeof m.type !== "string" || !m.type.startsWith(NS)) return;
        if (!accepted(ev)) return; // silence is the right answer to a stranger
        const kind = m.type.slice(NS.length);
        if (kind === "set") {
            // a new set starts a new conversation: what the old one still waits for will not be answered
            if (m.plan !== undefined) settleAll("the host sent a new plan");
            if (m.plan !== undefined) plan = m.plan;
            if (m.host !== undefined) host = m.host;
            const { acts = {}, ...state } = m.state ?? {};
            const r = onSet ? onSet({ state, acts, plan, host, data: m.data }) ?? {} : {};
            if (r.refused?.length) api3.refused(r.refused, r.why ?? "not accepted by this block");
            api3.state();
        } else if (kind === "data" && waiting.has(m.id)) {
            const wt = waiting.get(m.id);
            waiting.delete(m.id);
            if (wt.intent) wt.resolve(m.error ? { refused: { what: m.id, why: m.error } } : { done: true, result: m.result });
            else if (m.error) wt.reject(new Error(m.error));
            else wt.resolve(m.result);
        } else if (kind === "refused" && waiting.has(m.what)) {
            const wt = waiting.get(m.what);
            waiting.delete(m.what);
            if (wt.intent) wt.resolve({ refused: { what: m.what, why: m.why } });
            else wt.reject(Object.assign(new Error(m.why), { refused: true }));
        } else if (kind === "ask") { // v2, one release
            if (m.want === "capabilities") api3.ready();
            else api3.state();
        }
    });
    return api3;
}
