// cards: one card per row (a spot check, a rule case). Options:
//   title    the column shown as the card's id; group a column shown muted before it; badge "column|prefix": a badge on
//            the right (e.g. "earlier|earlier: "); quote a column shown as a quote (the spec statement)
//   fields   "column|Label" shown as label/value pairs, in this order (a missing value: "No <label> recorded.");
//            locs "column=locColumn": the location under that field's value (a decoded path, muted)
//   compare  "prefix|Label": judges side by side, from <prefix>Who <prefix>Verdict <prefix>Why (name,
//            model and round, a verdict pill, the reason; "No verdict recorded." when there is none)
//   target   the column naming what the card is about: the use's acts are about that row (the host checks it
//            is a row it served this block)
//   choices  "field=value|Label": the answers, as a segmented control (legend choiceLegend); each act is one primary
//            button that stays disabled until an answer is chosen, and proposes it for the field (the shell shows it in
//            the sign dialog). Without choices an act is a plain button.
//   draws    "kind|Label": small buttons after drawsLabel that set the state `draw` (the event draw) to "<kind>:<n>"
//   pick     "column|Label": a ghost button per card that sets the state `pick` (the event pick) to the row's column,
//            e.g. the element whose evidence another block shows
//   effects  a column "answer|covered|exercisedOnly|gap|resolved;…" (a rule case's effect per answer) and touched a column
//            (the elements it touches): "Resolves 78 of 78 elements, whatever the answer" and a table If answered |
//            Covered | Ex. only | Gap with a stacked bar per answer (covered blue, exercised purple, gap orange)
//   layout   list (default) | pager (one card at a time, ‹ n of m › and dots) | grid (auto columns)
//   heading  a heading above the cards; emptyText, emptyHow what to say when there is no row, and how there will be one
// Every card shows its row's ?stamp; a row with ?fixture "true" is marked as fixture data.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const split = (s) => {
    const i = s.indexOf("|");
    return i < 0 ? [s, s] : [s.slice(0, i), s.slice(i + 1)];
  };
  const uid = String(ctx.use.key ?? ctx.use.id ?? "cards").replace(/[^\w-]/g, "_");
  let n = Number(String(ctx.state?.draw ?? "").split(":")[1] ?? 0) || 0;
  const heading = o.heading ? `<h3 data-block-title>${esc(o.heading)}</h3>` : "";
  el.innerHTML = `${heading}${skeleton("card")}`;
  stamped(el, esc, ctx.request("query@1", {}), ctx.use.key).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const choices = (o.choices ?? []).map((c) => {
        const [fv, label] = split(c);
        const [field, value] = fv.split("=");
        return { field, value, label };
      });
      const [pickCol, pickLabel] = o.pick ? split(o.pick) : [null, null];
      const [badgeCol, badgePre] = o.badge ? (o.badge.includes("|") ? split(o.badge) : [o.badge, ""]) : [null, ""];
      const locs = new Map((o.locs ?? []).map((l) => l.split("=")));
      const card = (x, i) => {
        const stamp = v(x.stamp);
        const st = stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
        const fields = (o.fields ?? [])
          .map(split)
          .map(([col, label]) => {
            const val = v(x[col]);
            const loc = locs.has(col) && v(x[locs.get(col)]) ? `<span class="loc">${esc(locLabel(v(x[locs.get(col)])))}</span>` : "";
            return `<dt>${esc(label)}</dt>${val ? `<dd class="kton-field" data-col="${esc(col)}"${val !== shortTest(val) ? ` data-tip="${esc(val)}" tabindex="0"` : ""}>${esc(shortTest(val))}${loc}</dd>` : `<dd class="empty-line" data-col="${esc(col)}">No ${esc(label.toLowerCase())} recorded.</dd>`}`;
          })
          .join("");
        const judges = (o.compare ?? []).length
          ? `<div class="judges">${o.compare
              .map(split)
              .map(([p, label]) => {
                const verdict = v(x[`${p}Verdict`]);
                const who = v(x[`${p}Who`]);
                const why = v(x[`${p}Why`]);
                if (!verdict && !why) return `<div class="judge" data-judge="${esc(p)}"><div class="jh">${esc(label)}</div><p class="empty-line">No verdict recorded.</p></div>`;
                return `<div class="judge" data-judge="${esc(p)}"><div class="jh">${esc(label)} ${verdict ? `<span class="pill ${verdictClass(verdict)}">${esc(verdict)}</span>` : ""}</div>${who ? `<div class="jm">${esc(who)}</div>` : ""}${why ? `<p>${esc(why)}</p>` : ""}</div>`;
              })
              .join("")}</div>`
          : "";
        const seg = choices.length && acts.length && v(x[o.target])
          ? `<fieldset class="seg" data-choices="${i}"><legend>${esc(o.choiceLegend ?? "Your answer")}</legend><span class="opts">${choices
              .map((c, ci) => `<label data-testid="card-${i}-${esc(c.value)}"><input type="radio" name="${uid}-${i}" value="${ci}"><span>${esc(c.label)}</span></label>`)
              .join("")}</span></fieldset>`
          : "";
        const buttons = acts
          .map((a, ai) => `<button type="button" class="primary" data-card="${i}" data-act="${ai}"${seg ? " disabled data-needs-choice" : ""} data-testid="card-${i}-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`)
          .join("");
        const pick = pickCol && v(x[pickCol]) ? `<button type="button" class="ghost" data-pick="${i}" data-testid="pick-${i}">${esc(pickLabel)}</button>` : "";
        const badge = badgeCol && v(x[badgeCol]) ? `<span class="badge" data-badge>${esc(badgePre)}${esc(v(x[badgeCol]))}</span>` : "";
        const group = o.group && v(x[o.group]) ? `<span class="muted">${esc(v(x[o.group]))}</span>` : "";
        return `<article class="kton-card card" data-card-row="${i}" data-target="${esc(o.target ? v(x[o.target]) : "")}" aria-label="${esc(v(x[o.title]) || "card")}">
  <div class="card-head">${group}<h4 class="id">${esc(v(x[o.title]) || "(untitled)")}</h4> ${st}${v(x.fixture) === "true" ? ` <span class="kton-stamp" data-fixture>fixture</span>` : ""}${badge}</div>
  ${o.quote && v(x[o.quote]) ? `<blockquote class="quote">${esc(v(x[o.quote]))}</blockquote>` : ""}
  ${o.effects ? effectsTable(esc, parseEffects(v(x[o.effects])), Number(v(x[o.touched ?? ""])) || undefined) : ""}
  ${fields ? `<dl class="kv">${fields}</dl>` : ""}${judges}${seg}
  ${buttons || pick ? `<div class="kton-act actions">${buttons}${pick}<span class="kton-muted" data-act-out="${i}" role="status"></span></div>` : ""}
</article>`;
      };
      const draws = (o.draws ?? []).map(split).map(([kind, label]) => `<button type="button" class="small" data-draw="${esc(kind)}" data-testid="draw-${esc(kind)}">${esc(label)}</button>`).join("");
      const pager = o.layout === "pager" && rows.length > 1;
      const nav = pager
        ? `<div class="pager" aria-label="Cards"><button type="button" class="ghost small" data-step="-1" aria-label="Previous card">${ICON_PREV}</button><span class="pos" aria-live="polite" data-pos>1 of ${rows.length}</span><button type="button" class="ghost small" data-step="1" aria-label="Next card">${ICON_NEXT}</button><span class="dots">${rows.map((_, k) => `<button type="button" data-dot="${k}" aria-label="Card ${k + 1}" aria-current="${k === 0}"></button>`).join("")}</span></div>`
        : "";
      const list = rows.map(card).join("");
      const wrap = o.layout === "grid" ? "rc-grid" : "cards-list";
      el.innerHTML = `${heading}${rows.length ? `<div data-cards-count="${rows.length}">${nav}<div class="${wrap}">${list}</div></div>` : emptyBox(esc, o.emptyText || "Nothing to show.", o.emptyHow)}${draws ? `<div class="kton-act draws" data-draws><span class="muted">${esc(o.drawsLabel ?? "Draw more:")}</span>${draws}</div>` : ""}`;
      for (const i of el.querySelectorAll(".stack i[data-w]")) i.style.setProperty("--w", i.dataset.w);
      const cards = [...el.querySelectorAll("article[data-card-row]")];
      let cur = 0;
      const show = (k) => {
        cur = (k + cards.length) % cards.length;
        cards.forEach((c, j) => (c.hidden = j !== cur));
        for (const d of el.querySelectorAll("[data-dot]")) d.setAttribute("aria-current", String(Number(d.dataset.dot) === cur));
        const pos = el.querySelector("[data-pos]");
        if (pos) pos.textContent = `${cur + 1} of ${cards.length}`;
      };
      if (pager) {
        show(0);
        for (const b of el.querySelectorAll("[data-step]")) b.addEventListener("click", () => show(cur + Number(b.dataset.step)));
        for (const d of el.querySelectorAll("[data-dot]")) d.addEventListener("click", () => show(Number(d.dataset.dot)));
      }
      const chosen = (i) => {
        const r0 = el.querySelector(`[data-choices="${i}"] input:checked`);
        return r0 ? choices[Number(r0.value)] : undefined;
      };
      for (const f of el.querySelectorAll("[data-choices]")) {
        f.addEventListener("change", () => {
          for (const b of el.querySelectorAll(`button[data-card="${f.dataset.choices}"][data-needs-choice]`)) b.disabled = false;
        });
      }
      for (const b of el.querySelectorAll("button[data-act]")) {
        b.addEventListener("click", () => {
          const row = rows[Number(b.dataset.card)];
          const a = acts[Number(b.dataset.act)];
          const c = chosen(b.dataset.card);
          if (b.hasAttribute("data-needs-choice") && !c) return;
          const out = el.querySelector(`[data-act-out="${b.dataset.card}"]`);
          const intent = { kind: a.intent, control: a.control, about: a.about, fields: c ? { [c.field]: c.value } : {}, ...(o.target ? { target: v(row[o.target]) } : {}) };
          b.disabled = true;
          ctx.intent(intent).then((res) => {
            b.disabled = false;
            if (out) out.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
            announceAct(res, ctx.use.key);
          });
        });
      }
      for (const b of el.querySelectorAll("button[data-pick]")) {
        b.addEventListener("click", () => {
          const row = rows[Number(b.dataset.pick)];
          ctx.setState({ ...(ctx.state ?? {}), pick: v(row[pickCol]) });
          const out = el.querySelector(`[data-act-out="${b.dataset.pick}"]`);
          if (out) out.textContent = `${pickLabel}: ${v(row[pickCol])}`;
        });
      }
      for (const b of el.querySelectorAll("button[data-draw]")) {
        b.addEventListener("click", () => {
          n += 1;
          ctx.setState({ ...(ctx.state ?? {}), draw: `${b.dataset.draw}:${n}` });
        });
      }
    },
    (e) => {
      el.innerHTML = `${heading}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return { set() {}, destroy() {} };
}

/** "answer|covered|exercisedOnly|gap|resolved;…" → [{answer, covered, exercisedOnly, gap, resolved}] (bad entries dropped). */
export function parseEffects(s) {
  return String(s ?? "")
    .split(";")
    .map((e) => e.split("|"))
    .filter((p) => p.length === 5 && p.slice(1).every((n) => /^\d+$/.test(n)))
    .map(([answer, c, x, g, r]) => ({ answer, covered: Number(c), exercisedOnly: Number(x), gap: Number(g), resolved: Number(r) }));
}

/** "Resolves 78 of 78 elements, whatever the answer" (or "28–29 of 29" when the answers resolve different counts). */
export function resolvesLine(effects, touched) {
  if (!effects.length) return "";
  const r = effects.map((e) => e.resolved);
  const lo = Math.min(...r), hi = Math.max(...r);
  const of = touched ?? Math.max(...effects.map((e) => e.covered + e.exercisedOnly + e.gap));
  return `Resolves ${lo === hi ? lo : `${lo}–${hi}`} of ${of} element${of === 1 ? "" : "s"}${lo === hi ? ", whatever the answer" : ", depending on the answer"}`;
}

function effectsTable(esc, effects, touched) {
  if (!effects.length) return "";
  const of = touched ?? Math.max(...effects.map((e) => e.covered + e.exercisedOnly + e.gap));
  const w = (n) => (of ? Math.round((n / of) * 1000) / 10 : 0);
  const bar = (e) => `<div class="stack" aria-hidden="true">${e.covered ? `<i class="s-cov" data-w="${w(e.covered)}"></i>` : ""}${e.exercisedOnly ? `<i class="s-ex" data-w="${w(e.exercisedOnly)}"></i>` : ""}${e.gap ? `<i class="s-gap" data-w="${w(e.gap)}"></i>` : ""}</div>`;
  return `<div class="rc-res num" data-resolves>${esc(resolvesLine(effects, touched))}</div><table class="eff" data-effects><thead><tr><th>If answered</th><th>Covered</th><th>Ex. only</th><th>Gap</th></tr></thead><tbody>${effects
    .map((e) => `<tr><td>${esc(e.answer.replace(/-/g, " "))}${bar(e)}</td><td>${e.covered}</td><td>${e.exercisedOnly}</td><td>${e.gap}</td></tr>`)
    .join("")}</tbody></table>`;
}

/** A verdict's pill colour: asserted blue, exercised purple, no-relation muted, unclear (or anything else) warning. */
export function verdictClass(v) {
  const x = String(v).toLowerCase();
  if (/^(asserted|covered)/.test(x)) return "v-asserted";
  if (/^exercised/.test(x)) return "v-exercised";
  if (/^no[- ]relation/.test(x)) return "v-norel";
  return "v-unclear";
}

/** A test's short name: "repo:dir/File.kt#name|ids" → "File.kt · name" (the full name stays in the tooltip). */
export function shortTest(t) {
  const s = String(t);
  const m = /^[\w.-]+:(.+)$/.exec(s);
  if (!m) return s;
  const [body] = m[1].split("|");
  const hash = body.indexOf("#");
  const path = hash < 0 ? body : body.slice(0, hash);
  const name = hash < 0 ? "" : body.slice(hash + 1);
  const file = safeDecode(path.split("/").pop() ?? path);
  return name ? `${file} · ${name}` : file;
}

/** A location "repo/dir/…/File.kt:749" → "repo · …/dir/File.kt:749" (decoded; never %20). */
export function locLabel(p) {
  const s = safeDecode(String(p));
  const parts = s.split("/");
  if (parts.length < 3) return s;
  const tail = parts.slice(-2).join("/");
  return `${parts[0]} · ${parts.length > 3 ? "…/" : ""}${tail}`;
}

function safeDecode(s) {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
}

const ICON_PREV = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3.5 5.5 8l4.5 4.5"/></svg>`;
const ICON_NEXT = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5"/></svg>`;

/** The stamp's data attributes: the stamp and, when the query gives them, its source label, full hash and scope (the part's source chip). */
export function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}

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
// An answer the host gave from memory while it asks again after a signature (`refreshing`: {since, token})
// is drawn at once, and the fresh answer is drawn over it when the frame has it (ktonWorkbench.fresh, frame-design.js).
// A failed refresh leaves the drawn answer (the frame marks it stale, with the reason): never a skeleton after an answer.
function stamped(el, esc, asked, use) {
  return {
    then(ok, err) {
      const mark = (r) => {
        const u = Array.isArray(r?.unfiltered) ? r.unfiltered.filter((x) => typeof x === "string") : [];
        if (u.length) el.insertAdjacentHTML("afterbegin", `<p class="kton-stamp kton-unfiltered" data-unfiltered>unfiltered: ${esc(u.join(", "))} not set</p>`);
      };
      const follow = (r) => {
        const token = r?.refreshing?.token;
        const wb = globalThis.ktonWorkbench;
        if (token === undefined || !use || typeof wb?.fresh !== "function") return;
        wb.fresh(use, token).then(
          (x) => {
            if (x && x.result) {
              ok(x.result);
              mark(x.result);
            }
          },
          () => {},
        );
      };
      return asked.then((r) => {
        const out = ok(r);
        mark(r);
        follow(r);
        return out;
      }, err);
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
