// Source chips (DESIGN.md §4, MH 20.11.): a part shows where its numbers come from once, as a chip in its head.
// Pure functions, no DOM: the frame decorator (frame-design.js) and the unit tests use them.
//
// A stamp is {stamp, label, full, scope} as a block marks it (data-stamp, data-stamp-label, data-stamp-full,
// data-stamp-scope; the query's ?stamp ?stampLabel ?stampFull ?stampScope). Data sets a mapping round stamps carry its
// label ("mapping round 9 · b610b5c6", verification's covx:sourceStamp); the others (run-class-map, spec-decision,
// test-execution's runs) show their scope.

/** The key two stamps are "the same source" by: the label, else the scope, else the stamp itself. */
export function stampKey(s) {
  return s.label || s.scope || s.stamp || "";
}

/** "sha256:<hex>" from a full hash given as an IRI (https://kton.dev/o/<hex>), a sha256: string or bare hex. */
export function fullHash(x) {
  if (!x) return "";
  const m = /([0-9a-f]{64})$/.exec(String(x));
  return m ? `sha256:${m[1]}` : String(x);
}

/** The chip's text and tooltip for one source. */
export function chipFor(s, what = "Every number in this section comes from this source.") {
  const text = `source: ${s.label || s.scope || s.stamp || "unknown"}`;
  const tip = [what, s.label ? `${s.label}${s.scope ? ` (scope ${s.scope})` : ""}` : s.scope ? `scope ${s.scope}` : "", fullHash(s.full) || (s.stamp && s.stamp !== s.scope && !String(s.stamp).startsWith(`${s.scope} `) ? s.stamp : "")]
    .filter(Boolean)
    .join("\n");
  return { text, tip };
}

/**
 * Split a part's stamps: the section's source (the most frequent key, the first seen on a tie) and, per block, the
 * source of a block whose stamps all differ from it. Stamps equal to the section's are `dup` (the block hides them);
 * a block that mixes sources keeps its own row stamps.
 *   blocks: [{id, stamps: [{stamp,label,full,scope}]}]  →  {section: stamp|null, blocks: {id: stamp|null|"mixed"}}
 */
export function sectionStamps(blocks) {
  const count = new Map();
  const first = new Map();
  for (const b of blocks)
    for (const s of b.stamps) {
      const k = stampKey(s);
      if (!k) continue;
      count.set(k, (count.get(k) ?? 0) + 1);
      if (!first.has(k)) first.set(k, s);
    }
  let best = null;
  for (const [k, n] of count) if (best === null || n > count.get(best)) best = k;
  const section = best === null ? null : first.get(best);
  const out = {};
  for (const b of blocks) {
    const keys = [...new Set(b.stamps.map(stampKey).filter(Boolean))];
    if (!keys.length || (keys.length === 1 && keys[0] === best)) out[b.id] = null;
    else if (keys.length === 1) out[b.id] = b.stamps.find((s) => stampKey(s) === keys[0]);
    else out[b.id] = "mixed";
  }
  return { section, blocks: out };
}
