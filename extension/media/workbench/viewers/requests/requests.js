// requests: items that can be asked for, each with the answer it got (a "capture now" per test). One card per value
// of the `title` column; its first row gives the card's fields, every row with ?run ?name ?sha256 is an artifact to
// open (open@1 {artifact}: the host checks the bytes against ?sha256), with ?label as its button. Options:
//   title     the column naming the item (e.g. test)
//   fields    "column|Label": shown under the title when the row has a value, in this order
//   offer     a column: the use's acts are offered on a card only when its value is "true" (no request open)
//   waiting   a column: while a card's value is "true" (asked, no answer yet) the block asks again
//   poll      seconds between those asks (default 10). It needs the use's refine `poll` (type integer): each ask binds
//             a new value, so it is a new ask (the frame caches answers by params). Without it the block never asks again.
//   propose   "field=column": the act proposes the card's value of that column for the field; "field=" proposes an
//             empty value (the shell asks it, and leaves it out when left empty). Proposals are never signed unseen.
//   done      what the card says once its act is signed (the claim id follows)
//   nudge     read by the host, not here: the line it prints in its output channel after the act signed
//   emptyText what to say when there is no row
// Every card shows its row's ?stamp. After any act of the same view signed or recorded a claim (this block's or another
// block's, announced in the frame: ACTED), the block asks again and keeps asking for a while, until the request shows.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const split = (s, sep) => {
    const i = s.indexOf(sep);
    return i < 0 ? [s, s] : [s.slice(0, i), s.slice(i + 1)];
  };
  const canPoll = !!ctx.use.refine?.poll;
  const every = Math.max(2, Number(o.poll) || 10) * 1000;
  const outs = new Map(); // card title → what its last act said
  // each ask binds a new poll value: a frame reloaded after the sign dialog asks afresh, never the cached answer
  let n = Math.floor(Date.now() / 1000);
  let timer = null;
  let gone = false;
  let pollUntil = 0; // after an act: ask again for a while, until the request shows

  const target = (x) => (v(x.run) && v(x.name) && v(x.sha256) ? { artifact: { run: v(x.run), name: v(x.name), sha256: v(x.sha256), ...(v(x.mediaType) ? { mediaType: v(x.mediaType) } : {}) } } : null);

  const draw = (r) => {
    const rows = r?.results?.bindings ?? [];
    const groups = new Map();
    for (const x of rows) {
      const k = v(x[o.title]);
      if (!groups.has(k)) groups.set(k, []);
      groups.get(k).push(x);
    }
    const cards = [...groups.entries()].map(([title, rs], gi) => {
      const x = rs[0];
      const stamp = v(x.stamp);
      const fields = (o.fields ?? [])
        .map((f) => split(f, "|"))
        .map(([col, label]) => [col, label, v(rs.find((y) => v(y[col]))?.[col])])
        .filter(([, , val]) => val)
        .map(([col, label, val]) => `<div class="kton-field" data-col="${esc(col)}"><span class="kton-muted">${esc(label)}</span> ${esc(val)}</div>`)
        .join("");
      const arts = rs
        .map((a, ai) => ({ a, ai, t: target(a) }))
        .filter(({ t }) => t)
        .map(({ a, ai }) => `<div class="kton-field" data-artifact data-row="${ai}"><button type="button" class="ghost small" data-open="${gi}:${ai}" data-testid="request-${gi}-open-${ai}">${esc(v(a.label) || `Open ${v(a.name)}`)}</button>${v(a.bytes) ? ` <span class="kton-muted">${esc(v(a.bytes))} bytes</span>` : ""}<span class="kton-muted" data-open-out="${gi}:${ai}"></span></div>`)
        .join("");
      const offered = !o.offer || v(x[o.offer]) === "true";
      const buttons = offered ? acts.map((a, ai) => `<button type="button" class="${actClasses(acts)[ai]}" data-act="${ai}" data-card="${gi}" data-testid="request-${gi}-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`).join("") : "";
      const waiting = o.waiting && v(x[o.waiting]) === "true";
      return `<article class="kton-card" data-request-card="${gi}" data-title="${esc(title)}"${waiting ? ' data-waiting="true"' : ""}>
  <h4>${esc(title || "(untitled)")} ${stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`}</h4>
  ${fields}${arts}
  <div class="kton-act">${buttons}<span class="kton-muted" data-act-out="${gi}">${esc(outs.get(title) ?? "")}</span></div>
</article>`;
    });
    el.innerHTML = rows.length ? cards.join("") : `${emptyBox(esc, o.emptyText || "Nothing to request.", o.emptyHow)}`;
    const list = [...groups.entries()];
    for (const b of el.querySelectorAll("button[data-act]")) {
      b.addEventListener("click", () => {
        const [title, rs] = list[Number(b.dataset.card)];
        const a = acts[Number(b.dataset.act)];
        const fields = {};
        for (const p of o.propose ?? []) {
          const [field, col] = split(p, "=");
          if (field) fields[field] = col ? v(rs[0][col]) : "";
        }
        const out = el.querySelector(`[data-act-out="${b.dataset.card}"]`);
        b.disabled = true;
        if (out) out.textContent = "Sent to the host.";
        ctx.intent({ kind: a.intent, control: a.control, about: a.about, fields }).then((res) => {
          b.disabled = false;
          const said = res?.refused
            ? `Not done: ${res.refused.why ?? res.refused}`
            : res?.result?.signed && o.done
              ? `${o.done}${res.result.claim ? ` (${res.result.claim})` : ""}`
              : res?.result?.message ?? "Done.";
          outs.set(title, said);
          if (out) out.textContent = said;
          // its own act too: announced in the frame, so this block and every other requests block of the view ask again
          if (!announceAct(res, ctx.use.key) && res?.result?.signed) acted();
        });
      });
    }
    for (const b of el.querySelectorAll("button[data-open]")) {
      b.addEventListener("click", () => {
        const [gi, ai] = b.dataset.open.split(":").map(Number);
        const out = el.querySelector(`[data-open-out="${b.dataset.open}"]`);
        if (out) out.textContent = " Opening…";
        ctx.intent({ kind: "open", ...target(list[gi][1][ai]) }).then((res) => {
          if (out) out.textContent = ` ${res?.refused ? `Not opened: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Opened."}`;
        });
      });
    }
    if (el.querySelector('[data-waiting="true"]') || Date.now() < pollUntil) schedule(every);
  };

  const ask = () =>
    stamped(el, esc, ctx.request("query@1", canPoll ? { refine: { poll: String(n) } } : {})).then(draw, (e) => {
      el.innerHTML = `<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    });
  // an act of this view (any block) signed or recorded: ask again soon, then for a while (the index may still be building)
  function acted() {
    if (gone) return;
    pollUntil = Date.now() + 180_000;
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    schedule(1000);
  }
  const doc = globalThis.document;
  if (doc && typeof doc.addEventListener === "function") doc.addEventListener(ACTED, acted);
  function schedule(ms) {
    if (!canPoll || gone || timer) return;
    timer = setTimeout(() => {
      timer = null;
      if (gone) return;
      n += 1;
      ask();
    }, ms);
  }
  el.innerHTML = `${skeleton("lines")}`;
  ask();
  return {
    set() {},
    destroy() {
      gone = true;
      if (timer) clearTimeout(timer);
      if (doc && typeof doc.removeEventListener === "function") doc.removeEventListener(ACTED, acted);
    },
  };
}

/** One primary per object (DESIGN.md §5 acts): the first sign act is primary, every other act secondary (record included). */
export function actClasses(acts) {
  const first = acts.findIndex((a) => a.intent === "sign");
  return acts.map((_, i) => (i === first ? "primary" : "secondary"));
}

/** The stamp's data attributes: the stamp and, when the query gives them, its source label, full hash and scope (the part's source chip). */
export function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}

/** Loading: a skeleton of the block's own shape (lines or a tile row), busy for assistive tech (DESIGN.md §5). */
export function skeleton(shape = "lines") {
  return `<div class="wb-skel ${shape}" data-area-loading aria-busy="true" role="status"><span class="sr-only">Loading…</span><i></i><i></i><i></i></div>`;
}

/** Empty: what will appear (bold) and how it will (muted), never a bare "No data". */
export function emptyBox(esc, text, how) {
  return `<div class="empty" data-area-empty><svg class="i" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3"/><path d="M8 7.2v4M8 4.8v.4"/></svg><div><b>${esc(text)}</b>${how ? `<p>${esc(how)}</p>` : ""}</div></div>`;
}

// An answer the host ran with a declared optional param unbound says so (`unfiltered`: the params): its FILTER did not
// apply, so the block is stamped "unfiltered: <param> not set" above what it drew (the unbound-parameter guard).
function stamped(el, esc, asked) {
  return {
    then(ok, err) {
      const drawn = asked.then(ok, err);
      asked.then(
        (r) => {
          const u = Array.isArray(r?.unfiltered) ? r.unfiltered.filter((x) => typeof x === "string") : [];
          if (u.length) el.insertAdjacentHTML("afterbegin", `<p class="kton-stamp kton-unfiltered" data-unfiltered>unfiltered: ${esc(u.join(", "))} not set</p>`);
        },
        () => {},
      );
      return drawn;
    },
  };
}

// One frame holds all blocks of a view. After an act of the view signed or recorded a claim, the block announces it in
// the frame, and every requests block of the same view asks again: what it lists may have changed (an act in another
// block, e.g. a row act in a table, records the request it shows). Nothing leaves the frame.
export const ACTED = "kton-workbench:acted";
export function announceAct(res, from) {
  if (res?.result?.signed !== true) return false;
  const d = globalThis.document;
  if (!d || typeof d.dispatchEvent !== "function") return false;
  d.dispatchEvent(new CustomEvent(ACTED, { detail: { from: from ?? null, claim: res.result.claim ?? null } }));
  return true;
}
