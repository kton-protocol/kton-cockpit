// figures: numbers with their source. Each row of the query is one figure: ?value ?label ?stamp, and ?note,
// ?scope, ?total when given. Every number carries its stamp (G5.4: every number names its source); a figure without
// a stamp says so. Options:
//   display     tiles (default: number, label, note in a bordered tile; one column under 440 px) | list
//   guard       a column (e.g. "scope"): when the figures mix values of it, a callout says so ("Mixed scopes.")
//   relation    "i+j=k" (1-based rows): the callout states it ("20 in scope + 5 outside = 25 all judged") when the
//               numbers hold it; otherwise it lists the figures with their scopes
//   guardText   a sentence the package adds to the callout
//   scopeLabels "value|short": the tile's tag for a guard value (e.g. "in scope (as agreed)|in scope")
//   notes       "label|note": a note for the figure with that label when its row has none
//   propose     "field=column": the use's acts propose that column's values (all rows, space-separated) as the field (the shell shows it
//               to the person as a default; it never signs it unseen)
//   emptyText, emptyHow  what to say when there is no row, and how there will be one
// A row's ?total shows as "n of total".
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const tiles = o.display !== "list";
  el.innerHTML = `${skeleton("tiles")}`;
  stamped(el, esc, ctx.request("query@1", {})).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const short = pairs(o.scopeLabels);
      const notes = pairs(o.notes);
      const figs = rows
        .map((x) => {
          const stamp = v(x.stamp);
          const st = stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
          const total = v(x.total) ? ` <span class="of">of ${esc(v(x.total))}</span>` : "";
          const note = v(x.note) || notes.get(v(x.label)) || "";
          if (!tiles) return `<div class="kton-figure" data-figure><b class="kton-number" data-number>${esc(v(x.value))}</b>${total} <span>${esc(v(x.label))}</span> ${st}${note ? `<div class="kton-muted">${esc(note)}</div>` : ""}</div>`;
          const tag = o.guard && v(x[o.guard]) ? `<div class="scope">${esc(short.get(v(x[o.guard])) ?? v(x[o.guard]))}</div>` : "";
          return `<div class="kton-figure tile" data-figure>${tag}<div class="n"><b class="kton-number" data-number>${esc(v(x.value))}</b>${total}</div> <div class="l">${esc(v(x.label))} ${st}</div>${note ? `<div class="s">${esc(note)}</div>` : ""}</div>`;
        })
        .join("");
      let guard = "";
      if (o.guard) {
        const scopes = [...new Set(rows.map((x) => v(x[o.guard])).filter(Boolean))];
        if (scopes.length > 1) {
          const figures = rows.map((x) => ({ value: v(x.value), scope: v(x[o.guard]) }));
          const said = relationText(figures, o.relation) ?? `${figures.filter((f) => f.scope).map((f) => `${f.value} (${f.scope})`).join(" · ")}.`;
          guard = `<div class="kton-guard callout" data-guard role="alert">${ICON_WARN}<p><b>Mixed scopes.</b> These counts cover different sets: ${esc(said)} Read each number against its own scope.${o.guardText ? ` ${esc(o.guardText)}` : ""}</p></div>`;
        }
      }
      const proposals = {};
      for (const p of o.propose ?? []) {
        const [field, col] = p.split("=");
        const all = [...new Set(rows.flatMap((x) => v(x[col]).split(/\s+/)).filter(Boolean))];
        if (field && col && all.length) proposals[field] = all.join(" ");
      }
      const buttons = acts.length
        ? `<div class="kton-act actions">${acts.map((a, i) => `<button type="button" class="${actClasses(acts)[i]}" data-act="${i}" data-testid="act-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`).join("")}<span class="kton-muted" data-act-out></span></div>`
        : "";
      const body = rows.length ? (tiles ? `<div class="tiles" data-count="${rows.length}">${figs}</div>` : figs) : `${emptyBox(esc, o.emptyText || "No figures.", o.emptyHow)}`;
      el.innerHTML = `${body}${guard}${buttons}`;
      for (const b of el.querySelectorAll("[data-act]")) {
        b.addEventListener("click", () => {
          const a = acts[Number(b.dataset.act)];
          const out = el.querySelector("[data-act-out]");
          ctx.intent({ kind: a.intent, control: a.control, about: a.about, fields: proposals }).then((res) => {
            if (out) out.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
            announceAct(res, ctx.use.key);
          });
        });
      }
    },
    (e) => {
      el.innerHTML = `<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return { set() {}, destroy() {} };
}

/** One primary per object: the first sign act is primary, every other act secondary (record included). */
export function actClasses(acts) {
  const first = acts.findIndex((a) => a.intent === "sign");
  return acts.map((_, i) => (i === first ? "primary" : "secondary"));
}

/** "value|label" pairs → Map. */
export function pairs(list) {
  return new Map((list ?? []).map((s) => (s.includes("|") ? [s.slice(0, s.indexOf("|")), s.slice(s.indexOf("|") + 1)] : [s, s])));
}

/**
 * The relation between figures the guard states: "i+j=k" (1-based) over [{value, scope}] → "20 in scope + 5 outside
 * = 25 all judged.", only when the numbers hold it (else undefined: the guard lists the figures instead).
 */
export function relationText(figures, relation) {
  const m = /^\s*(\d+)\s*\+\s*(\d+)\s*=\s*(\d+)\s*$/.exec(relation ?? "");
  if (!m) return undefined;
  const [a, b, c] = [m[1], m[2], m[3]].map((i) => figures[Number(i) - 1]);
  if (!a || !b || !c) return undefined;
  const n = (f) => Number(String(f.value).replace(/,/g, ""));
  if (![a, b, c].every((f) => Number.isFinite(n(f))) || n(a) + n(b) !== n(c)) return undefined;
  const say = (f) => `${f.value}${f.scope ? ` ${f.scope}` : ""}`;
  return `${say(a)} + ${say(b)} = ${say(c)}.`;
}

/** The stamp's data attributes: the stamp and, when the query gives them, its source label, full hash and scope. */
export function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}

const ICON_WARN = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.8 15 14H1z"/><path d="M8 6.2v3.6M8 11.6v.4"/></svg>`;

/** Loading: a skeleton of the block's own shape (lines or a tile row), busy for assistive tech. */
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
