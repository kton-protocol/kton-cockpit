// Never a blank view after a signature (the blocks were grey skeletons for as long as the cockpit rebuilt a federated
// index after the say). Pure: the frame decorator (frame-design.js) draws, the
// unit tests check. The host answers a re-ask at once with the last answer, marked `refreshing`, and posts the fresh one
// (kton-workbench:answer) when it has it; a block draws the fresh one over the last (its stamped(), via fresh()).

const two = (n) => String(n).padStart(2, "0");
/** "14:02" in the viewer's local time. */
export function clock(ms) {
  const d = new Date(ms);
  return `${two(d.getHours())}:${two(d.getMinutes())}`;
}

/** "about 40 s" / "about 2 min": how long it has been (never "0 s"). */
export function about(ms) {
  const s = Math.max(1, Math.round(ms / 1000));
  return s < 90 ? `about ${s} s` : `about ${Math.round(s / 60)} min`;
}

/** The line on a block showing its last answer while the fresh one is asked for. */
export function updatingText(since, now, afterSignature = true) {
  return `${afterSignature ? "Updating after your signature" : "Updating"} (since ${clock(since)}, ${about(now - since)}). Showing the last answer until then.`;
}

/** The line on a block whose refresh failed: the last answer stays, with the reason. */
export function staleText(why, since) {
  return `Not updated: ${String(why || "the refresh failed").replace(/\s+/g, " ").trim()}. Showing the answer from before ${clock(since)}.`;
}

/** The confirmation of a signature: "Signed: <what> (claim 1234abcd) at 14:02". */
export function signedText(s) {
  const claim = String(s?.claim ?? "").replace(/^sha256:/, "").slice(0, 8);
  return `Signed: ${s?.what || "your answer"}${claim ? ` (claim ${claim})` : ""}${Number.isFinite(s?.at) ? ` at ${clock(s.at)}` : ""}`;
}

/**
 * The fresh answers the host posted, by use: fresh(use, token) resolves with {result} or {error, since} for that token
 * (or a later one), at once if it is here already. put() takes the host's message.
 */
export function answerStore() {
  const got = new Map(); // use → {token, msg}
  const waiting = new Map(); // use → [{token, resolve}]
  return {
    put(msg) {
      if (!msg || typeof msg.use !== "string" || !Number.isFinite(msg.token)) return false;
      const had = got.get(msg.use);
      if (had && had.token > msg.token) return false;
      got.set(msg.use, { token: msg.token, msg });
      const rest = [];
      for (const w of waiting.get(msg.use) ?? []) (msg.token >= w.token ? w.resolve(msg) : rest.push(w));
      waiting.set(msg.use, rest);
      return true;
    },
    fresh(use, token) {
      const had = got.get(use);
      if (had && had.token >= token) return Promise.resolve(had.msg);
      return new Promise((resolve) => {
        const list = waiting.get(use) ?? [];
        list.push({ token, resolve });
        waiting.set(use, list);
      });
    },
    has(use, token) {
      const had = got.get(use);
      return !!had && had.token >= token;
    },
  };
}

/** Attributes a copy of a block's last content must not carry (ids, form names, test ids, counts the frame reads). */
export const PREV_STRIP = ["id", "name", "for", "data-testid", "data-cards-count", "data-count", "aria-controls", "aria-labelledby", "aria-describedby"];

/**
 * The banner at the top of a view while the window waits for the cockpit, from the window's ask queue state
 * ({cold, buildingSince, queued, inFlight, lastError}): building, waiting, or failed (with the reason; the frame offers
 * Retry). None when nothing waits. The last good answers stay under it; skeletons only where nothing came yet.
 */
export function indexBannerText(s, now) {
  if (!s || typeof s !== "object") return null;
  if (typeof s.buildingSince === "number")
    return { kind: "building", text: `Loading data: the index is being built after a change (first load takes about 1–2 min; started ${clock(s.buildingSince)}, ${Math.max(0, Math.round((now - s.buildingSince) / 1000))} s so far).${s.queued ? ` ${s.queued} asks wait for it.` : ""}` };
  if (s.cold && s.lastError && s.lastError.unreachable && typeof s.lastError.message === "string")
    return { kind: "failed", text: `Index not reachable: ${s.lastError.message.replace(/\s+/g, " ").trim()} (at ${clock(s.lastError.at)}). Run \`cockpit doctor\`. The last answers stay shown.` };
  if (s.cold && s.lastError && typeof s.lastError.message === "string")
    return { kind: "failed", text: `Not loaded: ${s.lastError.message.replace(/\s+/g, " ").trim()} (at ${clock(s.lastError.at)}). The last answers stay shown.` };
  if (s.queued > 0) return { kind: "waiting", text: `Loading data: ${s.queued} asks waiting (at most 2 at a time).` };
  return null;
}
