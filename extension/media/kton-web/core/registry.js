/**
 * Reading a registry off disk. The two substrates store differently, and both shapes are current.
 *
 *     plankton/objects/sha256/<hash>.json          one object per file, pretty-printed
 *     nekton/objects/scope/<scope_id>.nekton.jsonl a subnekton: its seed and its chain, one file
 *
 * (kton v0.4 has no unscoped nekton any more: the store's first write opens its root scope, and
 * every seed and claim hangs off it, so every claim file is a scope/ file. An older registry's
 * nekton/objects/unscoped.nekton.jsonl still reads, as JSONL, by the rule below.)
 *
 * This is not legacy versus current, and not an accident of history: THE UNIT OF THE FILE IS THE
 * UNIT OF VALIDITY. A foton is valid on its own, so plankton keeps one object per file and handing
 * over a subset is a real partial share - a substrate may be shared selectively. A subnekton is
 * seed-chain bound and only valid with its whole context, so it is one file: the thing you can
 * hand over is the whole thing, and there is no half of it to give. See core/chain.js.
 *
 * A reader that assumes only one of the two loses data, and loses it SILENTLY. Measured against a
 * current corpus of 2032 records:
 *
 *     whole-file JSON.parse only  ->   68 records, 44 files skipped without a word
 *     line-by-line only           -> 1965 records, every pretty-printed foton lost
 *
 * The first is what the graph viewer did: 1964 of 1965 claims gone, and the page still drew a
 * perfectly convincing lineage-only graph. Nothing errored. That is the worst kind of wrong.
 *
 * So: try the file as one object; if that yields no record, read it as JSONL. By content, not by
 * extension - a file named `.json` that holds JSONL still reads, and so does the reverse.
 */

/** A registry object is anything carrying an id and a signed envelope. */
const isRecord = (r) => !!(r && r.envelope && (r.fotonId || r.claimId));

/**
 * Records in one file, whatever its shape.
 * Returns {records, shape} - shape is 'object' | 'jsonl' | 'none', so a caller can report what it
 * actually found instead of guessing.
 */
export function recordsFromText(text) {
    const t = String(text || "").trim();
    if (!t) return { records: [], shape: "none" };

    try {                                        // one object per file (plankton, old nekton)
        const one = JSON.parse(t);
        if (isRecord(one)) return { records: [one], shape: "object" };
        if (Array.isArray(one)) {                // a union.json handed in by mistake still reads
            const recs = one.filter(isRecord);
            if (recs.length) return { records: recs, shape: "array" };
        }
    } catch { /* fall through - a multi-record file is not valid JSON as a whole */ }

    const records = [];
    for (const line of t.split("\n")) {          // a subnekton: seed + chain, one per line
        const s = line.trim();
        if (!s) continue;
        try { const r = JSON.parse(s); if (isRecord(r)) records.push(r); } catch { /* skip the line, not the file */ }
    }
    return { records, shape: records.length ? "jsonl" : "none" };
}

/** A raw ed25519 public key, as a *.pub holds it. */
export const isPubKey = (text) => /^[0-9a-f]{64}$/i.test(String(text || "").trim());

/**
 * Assemble a union from a set of files. Deduplicates on record id, because two registries
 * converging on the same content is the normal case, not a conflict (SPEC §12).
 *
 * `read` is `(file) => Promise<string>`, so this works over a browser FileList and over the
 * filesystem without knowing which it is.
 */
export async function unionFromFiles(files, read) {
    const records = [], seen = new Set(), pubs = [];
    const shapes = { object: 0, jsonl: 0, array: 0, none: 0 };

    for (const f of files) {
        let text; try { text = await read(f); } catch { continue; }
        if (isPubKey(text)) { pubs.push({ file: f, hex: text.trim().toLowerCase() }); continue; }
        const { records: recs, shape } = recordsFromText(text);
        shapes[shape] = (shapes[shape] || 0) + 1;
        for (const r of recs) {
            const id = r.fotonId || r.claimId;
            if (id && !seen.has(id)) { seen.add(id); records.push(r); }
        }
    }
    const claims = records.filter((r) => r.claimId).length;
    return { records, pubs, shapes, claims, fotons: records.length - claims };
}
