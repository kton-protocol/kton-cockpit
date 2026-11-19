/**
 * A portion of a store — the viewer's side of the cockpit's /data/select.
 *
 * A large kton does not fit in one union.json, so the viewer asks the store for the part it is
 * drawing (the pedigree of a hash, what was built on it, a scope, records by id) and grows it as
 * the reader expands. What comes back is union.json-shaped: records with their signed envelopes
 * and the keys to verify them. The store CHOSE the records; it did not vouch for them. They go
 * through the same wasm verification as a union, so a selection can only ever show less.
 *
 * This file only gathers: it fetches pages, follows a cut page's cursor when asked, and merges
 * everything into one record set, each record once. No DOM, no drawing, no verdicts.
 */

const strip = (h) => String(h || "").replace(/^sha256:/, "").toLowerCase();
const recId = (r) => r && (r.fotonId || r.claimId);

/** The URL of one selection page: base is the endpoint (e.g. "/data/select"). */
export function selectUrl(base, query, cursor) {
    const u = new URL(base, typeof location !== "undefined" ? location.href : "http://localhost/");
    for (const [k, v] of Object.entries(query)) {
        if (v == null || v === "") continue;
        for (const x of Array.isArray(v) ? v : [v]) u.searchParams.append(k, String(x));
    }
    if (cursor) u.searchParams.set("cursor", cursor);
    return u.toString();
}

/** The full output hashes a foton record declares (its statement's subjects). */
export function outputsOf(rec) { return digests(payloadOf(rec), "subject"); }
/** The full input hashes a foton record declares. */
export function inputsOf(rec) { const p = payloadOf(rec); return digests(p && p.predicate, "inputs"); }

function payloadOf(rec) {
    try { return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(rec.envelope.payload), (c) => c.charCodeAt(0)))); } catch { return null; }   // UTF-8, not Latin-1
}
function digests(obj, field) {
    const out = [];
    for (const s of (obj && obj[field]) || []) { const d = s && s.digest && (s.digest.sha256 || s.digest["sha-256"]); if (d) out.push(strip(d)); }
    return out;
}

/** A growing portion of one store. `fetchFn` is injectable for the checks. */
export class Portion {
    constructor(base, fetchFn) {
        this.base = base;
        this.fetch = fetchFn || ((u) => fetch(u));
        this.byId = new Map();     // record id -> record, in arrival order
        this.keys = {};
        this.names = {};
        this.pending = new Map();  // query key -> {query, cursor}: pages the store cut
        this.asked = new Set();    // query keys already fetched (first page)
    }

    /** Records gathered so far, each once. */
    records() { return [...this.byId.values()]; }
    /** How many selections were cut and have more to give. */
    get more() { return this.pending.size; }

    /** Ask one selection (its first page). Asking the same query twice is free. Returns records added. */
    async ask(query) {
        const key = JSON.stringify(query);
        if (this.asked.has(key)) return 0;
        this.asked.add(key);
        return this.page(key, query, null);
    }

    /** Follow every cut selection one page further. Returns records added. */
    async loadMore() {
        let added = 0;
        for (const [key, { query, cursor }] of [...this.pending]) added += await this.page(key, query, cursor);
        return added;
    }

    async page(key, query, cursor) {
        const r = await this.fetch(selectUrl(this.base, query, cursor));
        if (!r.ok) throw new Error(`select ${query.q}: HTTP ${r.status}${r.text ? " - " + (await r.text()).trim() : ""}`);
        const sel = await r.json();
        let added = 0;
        for (const rec of sel.records || []) { const id = recId(rec); if (id && !this.byId.has(id)) { this.byId.set(id, rec); added++; } }
        Object.assign(this.keys, sel.keys || {});
        Object.assign(this.names, sel.names || {});
        if (sel.more) this.pending.set(key, { query, cursor: sel.more }); else this.pending.delete(key);
        return added;
    }

    /** The record behind a graph node: node ids are a prefix of the record id's hex. */
    recordFor(nodeId) {
        const n = strip(nodeId);
        if (n.length < 6) return null;
        for (const [id, rec] of this.byId) if (strip(id).startsWith(n)) return rec;
        return null;
    }

    /** The full content hash behind a (possibly shortened) file hash, from what the records declare. */
    fullHash(prefix) {
        const p = strip(prefix);
        if (p.length === 64) return p;
        if (p.length < 6) return null;
        for (const rec of this.byId.values()) for (const h of [...outputsOf(rec), ...inputsOf(rec)]) if (h.startsWith(p)) return h;
        return null;
    }
}
