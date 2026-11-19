/**
 * A page viewer as a block in its own frame (a leaf, C1.5 v3): render, feed, graph.
 *
 * The page keeps the v2-shaped calls it already makes (host.state, host.intent({type:'act', action, …}),
 * host.refused, host.embedded) and this turns them into v3: an intent leaves as {kind: action, …}.
 * Its data is never pushed in: every input is a kton:request answer.
 *
 *   inputs (viewer.json "inputs", via "ask")  ->  query@1 {input: <name>}
 *       The host runs the input's query from the installed package, bound as the slot says
 *       (render in review-view: DOC = the subject), and answers SPARQL JSON results.
 *   bytes of a file the graph names by hash   ->  product@1 {hash}
 *       Answered like content@1: {type, hash, size, bytes (base64)} up to 1 MB, else {type, hash, url}.
 *       The bytes are checked against the hash here before anything is shown.
 *
 * A page uses v3 when it sits in a frame and was given no data address (?src=, ?union=); ?bridge=2 keeps
 * the old wire for one release.
 */
import { connect } from "./host.js";

export function wantsV3(dataKeys = ["src"], w = globalThis.window) {
    const p = new URLSearchParams(w.location.search);
    return w.parent !== w && !dataKeys.some((k) => p.has(k)) && p.get("bridge") !== "2";
}

/**
 * start(api, host) runs once, on the first kton:set (it loads the inputs); apply(state) takes every set's
 * view state afterwards, and the first one's once start is done. Both may return {refused, why}.
 */
export function leafBridge({ requires = [], optional = [], accepts = {}, current = () => null, apply, start, win, api: port } = {}) {
    let started = false;
    const answer = (r) => {
        if (r?.refused?.length) b.refused(r.refused, r.why ?? "not accepted by this block");
    };
    const b = connect({
        requires, optional, accepts, current, win, api: port,
        onSet: ({ state, host }) => {
            if (started) return apply ? apply(state) ?? {} : {};
            started = true;
            Promise.resolve(start ? start(api, host) : null)
                .then(() => { answer(apply ? apply(state) : null); b.state(); })
                .catch((e) => { b.refused(requires.length ? requires : ["start"], String(e?.message ?? e)); });
            return {};
        },
    });
    const api = {
        embedded: true,
        v3: true,
        bridge: b,
        provides: (cap) => b.provides(cap),
        state: (s) => b.state(s),
        refused: (what, why) => b.refused(what, why),
        ready: () => b.ready(),
        request: (cap, args) => b.request(cap, args),
        intent: (i) => {
            const { type, action, ...rest } = i ?? {};
            return b.intent(type === "act" ? { kind: action, ...rest } : type ? { kind: type, ...rest } : rest);
        },
    };
    return api;
}

/**
 * The viewer's inputs as query@1 answers, all at once: {name: results}. An optional input the host
 * refuses (or does not provide query@1 for) is null; a required one rejects with the host's reason.
 */
export async function requestInputs(api, inputs) {
    const names = Object.keys(inputs);
    const got = await Promise.all(names.map(async (name) => {
        try {
            return await api.request("query@1", { input: name });
        } catch (e) {
            if (inputs[name]?.optional) return null;
            throw new Error(`${name}: ${e.message}`);
        }
    }));
    return Object.fromEntries(names.map((n, i) => [n, got[i]]));
}

const hex = (buf) => [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");
function fromBase64(s) {
    const bin = atob(s);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
}

/** The bytes of a file by hash (product@1), checked, as {bytes, type}. */
export async function product(api, hash) {
    const h = "sha256:" + String(hash).replace(/^sha256:/, "");
    const r = await api.request("product@1", { hash: h });
    let bytes;
    if (typeof r?.bytes === "string") bytes = fromBase64(r.bytes);
    else if (typeof r?.url === "string") {
        const res = await fetch(r.url);
        if (!res.ok) throw new Error(`${h}: HTTP ${res.status}`);
        bytes = new Uint8Array(await res.arrayBuffer());
    } else throw new Error(`${h}: product@1 answered neither bytes nor url`);
    const got = "sha256:" + hex(await crypto.subtle.digest("SHA-256", bytes));
    if (got !== h) throw new Error(`the bytes for ${h} hash to ${got}`);
    return { bytes, type: r.type ?? "application/octet-stream" };
}

/** product() as an object URL for a preview (img, iframe, embed); null if the host cannot give it. */
export async function productUrl(api, hash) {
    if (!hash || !api.provides("product@1")) return null;
    const { bytes, type } = await product(api, hash);
    return URL.createObjectURL(new Blob([bytes], { type }));
}
