// queue: one item at a time out of a long list, by keyboard. A named query answers the open items of one group, in
// its order (risk first), a page at a time; the block shows the current item with all its fields, where it is ("Item 37
// of 412 in this group"), and moves by keys:
//   → j n  next          ← k p  previous          s  skip (no decision: the item comes back at the end)
//   1…9    decide with the first … ninth choice (the use's act; a decision opens the shell's sign dialog)
// Keys are not taken while the focus is in a field, a select or a button's own handler (Enter/Space stay theirs).
// Options:
//   group     the refine param that names the group (the use declares it with its values); a select above the item
//   groupLabel a column with the group's name (shown beside the select)
//   page      "<param>=<column>": keyset paging over a refine param (the next page is asked with the last row's column)
//   pageSize  the query's LIMIT; a full page means there is more, and it is asked at once in the background
//   key       the column that names an item (its key; what the block emits as `item` and keeps as its place)
//   total     a column with the number of open items in the group (the same in every row)
//   title     the column shown as the item's id; kind a column shown as a badge before it (what kind of item it is)
//   quote     a column shown as a quote (the spec statement); fields "column|Label" pairs, in this order; a missing
//             value reads "No <label> recorded."; missing a column that says which detail the item has not (shown
//             as a warning under the fields)
//   target    the column naming what the act is about (the host checks it is a row it served this block)
//   choices   "field=value|Label": the decisions, keys 1…n in this order; each proposes its value for the field
// Its place is kept: the group, the current item and, per group, the skipped and the decided items of this window
// (ctx.setState; the shell keeps a keepsState view's state in the window, never in a registry). Decided items leave the
// queue at once; the query leaves them out once the cockpit has the signature.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const split = (s) => {
    const i = s.indexOf("|");
    return i < 0 ? [s, s] : [s.slice(0, i), s.slice(i + 1)];
  };
  const refine = ctx.use.refine ?? {};
  const gParam = o.group && refine[o.group] ? o.group : undefined;
  const groups = gParam && Array.isArray(refine[gParam].values) ? refine[gParam].values.map(String) : [];
  const [pageParam, pageCol] = typeof o.page === "string" && o.page.includes("=") ? o.page.split("=", 2) : [undefined, undefined];
  const pageSize = Number(o.pageSize ?? 0) || 0;
  const keyCol = o.key ?? "key";
  const choices = (o.choices ?? []).map((c) => {
    const [fv, label] = split(c);
    const k = fv.indexOf("=");
    return { field: fv.slice(0, k), value: fv.slice(k + 1), label };
  });
  const act = acts[0];

  let st = clone(ctx.state);
  const group = () => (gParam ? (groups.includes(st.group) ? st.group : String(v(ctx.use.params?.[gParam]) || groups[0] || "")) : "");
  const at = (g) => ((st.at ??= {})[g] ??= { item: "", skipped: [], done: [] });
  // the state (and with it the emitted item, which the detail blocks ask for) is written once the reader stops moving
  // (QUIET ms after the last key): moving through 50 items asks the detail of one, not of 50
  let quiet = null;
  const save = (patch) => {
    st = { ...st, ...patch };
    if (quiet) clearTimeout(quiet);
    quiet = setTimeout(() => {
      quiet = null;
      ctx.setState(clone(st));
    }, Number(o.quietMs ?? QUIET));
  };

  let rows = [];
  let total = 0;
  let complete = false;
  let loading = 0;
  let failed = "";
  let busy = false;
  let note = "";
  let gen = 0;

  // rows this view signed a decision about (host-held, also after the frame reloaded for the sign dialog): decided
  const signedKeys = () => {
    const t = new Set((globalThis.ktonWorkbench?.signedActs?.(ctx.use.key) ?? []).map((a) => a.target).filter(Boolean));
    return o.target && t.size ? rows.filter((r) => t.has(v(r[o.target]))).map((r) => v(r[keyCol])) : [];
  };
  const order = () => {
    const a = at(group());
    return queueOrder(rows, (r) => v(r[keyCol]), { ...a, done: [...a.done, ...signedKeys()] });
  };
  const index = (list) => {
    const k = at(group()).item;
    const i = list.findIndex((r) => v(r[keyCol]) === k);
    return i < 0 ? 0 : i;
  };
  const count = (list) => {
    if (complete) return list.length;
    const done = new Set([...at(group()).done, ...signedKeys()]);
    return Math.max(list.length, total - rows.filter((r) => done.has(v(r[keyCol]))).length);
  };

  const select = (row) => {
    const k = row ? v(row[keyCol]) : "";
    const a = at(group());
    if (a.item === k && st.item === k) return;
    a.item = k;
    save({ item: k });
  };

  const field = (row, [col, label]) => {
    const val = v(row[col]);
    return `<dt>${esc(label)}</dt>${val ? `<dd class="kton-field" data-col="${esc(col)}">${esc(val)}</dd>` : `<dd class="empty-line" data-col="${esc(col)}">No ${esc(label.toLowerCase())} recorded.</dd>`}`;
  };

  const draw = () => {
    const list = order();
    const i = index(list);
    const row = list[i];
    const n = count(list);
    const a = at(group());
    const skippedN = a.skipped.filter((k) => !a.done.includes(k)).length;
    // where the reader is, in the state too (the shell's support file reads it): only when it changed
    const p = row ? { n: i + 1, of: n, group: group(), item: v(row[keyCol]) } : { n: 0, of: 0, group: group() };
    if (JSON.stringify(p) !== JSON.stringify(st.position)) save({ position: p });
    const groupSel = gParam
      ? `<label class="q-group">Group <select data-group aria-label="Group">${groups.map((g) => `<option value="${esc(g)}"${g === group() ? " selected" : ""}>${esc(g)}</option>`).join("")}</select></label>${rows[0] && o.groupLabel && v(rows[0][o.groupLabel]) ? ` <span class="muted" data-group-label>${esc(v(rows[0][o.groupLabel]))}</span>` : ""}`
      : "";
    const pos = row
      ? `<span class="pos" data-pos aria-live="polite">Item ${i + 1} of ${n} in this group</span>`
      : `<span class="pos" data-pos aria-live="polite">${loading ? "Loading…" : "Nothing open in this group"}</span>`;
    const extra = [skippedN ? `${skippedN} skipped (they come back at the end)` : "", a.done.length ? `${a.done.length} decided in this window` : "", !complete && rows.length ? `${rows.length} loaded` : ""].filter(Boolean).join(" · ");
    const head = `<div class="q-head pager">${groupSel}<button type="button" class="ghost small" data-step="-1" aria-label="Previous item (←, k)" data-testid="queue-prev">${ICON_PREV}</button>${pos}<button type="button" class="ghost small" data-step="1" aria-label="Next item (→, j)" data-testid="queue-next">${ICON_NEXT}</button><button type="button" class="small secondary" data-skip data-testid="queue-skip"${row ? "" : " disabled"}>Skip (s)</button>${extra ? `<span class="muted" data-queue-extra>${esc(extra)}</span>` : ""}${back ? `<button type="button" class="small" data-back data-testid="queue-back">Back to rule ${esc(back)}</button>` : ""}</div>`;
    const keys = `<p class="muted q-keys" data-keys>Keys: → or j next · ← or k previous · s skip (no decision, comes back at the end)${choices.length && act ? ` · ${choices.map((c, ci) => `${ci + 1} ${esc(c.label)}`).join(" · ")} (opens the sign dialog)` : ""}</p>`;
    if (failed) {
      el.innerHTML = `${head}<div class="callout error" data-area-error role="alert"><p>${esc(failed)}</p></div>`;
      wire();
      return;
    }
    if (!row) {
      el.innerHTML = `${head}${loading ? skeleton("card") : emptyBox(esc, o.emptyText || "Nothing open in this group.", o.emptyHow)}${keys}`;
      wire();
      return;
    }
    const stamp = v(row.stamp);
    const st0 = stamp ? `<span class="kton-stamp" ${stampAttrs(row, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
    const kind = o.kind && v(row[o.kind]) ? `<span class="badge" data-kind>${esc(v(row[o.kind]))}</span>` : "";
    const missing = o.missing && v(row[o.missing]) ? `<div class="callout" data-missing role="status"><p>${esc(v(row[o.missing]))}</p></div>` : "";
    const seg = choices.length && act
      ? `<div class="kton-act actions q-decide" data-decide>${choices.map((c, ci) => `<button type="button" class="${ci === 0 ? "primary" : "secondary"}" data-choice="${ci}" data-testid="queue-decide-${esc(c.value)}"${busy ? " disabled" : ""}><kbd>${ci + 1}</kbd> ${esc(c.label)}</button>`).join("")}<span class="kton-muted" data-act-out role="status">${esc(note)}</span></div>`
      : note ? `<p class="kton-muted" data-act-out role="status">${esc(note)}</p>` : "";
    el.innerHTML = `${head}<article class="kton-card card q-item" data-queue-item="${esc(v(row[keyCol]))}" data-target="${esc(o.target ? v(row[o.target]) : "")}" aria-label="${esc(v(row[o.title ?? keyCol]) || "item")}">
  <div class="card-head">${kind}<h4 class="id" data-item-id>${esc(v(row[o.title ?? keyCol]) || "(untitled)")}</h4> ${st0}</div>
  ${o.quote ? (v(row[o.quote]) ? `<blockquote class="quote" data-quote>${esc(v(row[o.quote]))}</blockquote>` : `<p class="empty-line" data-quote-missing>No text recorded for this item.</p>`) : ""}
  ${missing}
  ${(o.fields ?? []).length ? `<dl class="kv">${o.fields.map(split).map((f) => field(row, f)).join("")}</dl>` : ""}
  ${seg}
</article>${keys}`;
    wire();
  };

  const move = (d) => {
    const list = order();
    if (!list.length) return;
    const i = Math.min(Math.max(index(list) + d, 0), list.length - 1);
    note = "";
    select(list[i]);
    draw();
  };
  const skip = () => {
    const list = order();
    const i = index(list);
    const row = list[i];
    if (!row) return;
    const a = at(group());
    const k = v(row[keyCol]);
    a.skipped = [...a.skipped.filter((x) => x !== k), k];
    const next = order();
    note = `Skipped ${k}: it comes back at the end.`;
    // the next item takes the skipped one's place (the skipped one is now last)
    select(next[Math.min(i, next.length - 1)]);
    save({});
    draw();
  };
  const decide = (ci) => {
    const list = order();
    const i = index(list);
    const row = list[i];
    const c = choices[ci];
    if (!row || !c || !act || busy) return;
    busy = true;
    note = `${c.label}: confirm in the sign dialog…`;
    draw();
    const k = v(row[keyCol]);
    ctx.intent({ kind: act.intent, control: act.control, about: act.about, fields: { [c.field]: c.value }, ...(o.target ? { target: v(row[o.target]) } : {}) }).then((res) => {
      busy = false;
      if (res?.result?.signed === true) {
        const a = at(group());
        a.done = [...a.done.filter((x) => x !== k), k];
        a.skipped = a.skipped.filter((x) => x !== k);
        const next = order();
        note = `${res.result.message ?? "Signed."} ${k}: ${c.label}.`;
        select(next[Math.min(i, next.length - 1)]);
        save({});
        announceAct(res, ctx.use.key);
      } else note = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Not signed.";
      draw();
    });
  };

  function wire() {
    el.querySelector("[data-back]")?.addEventListener("click", () => {
      const label = o.backTab ?? "Rules";
      back = "";
      const d = globalThis.document;
      const t = d ? [...d.querySelectorAll('[role="tab"]')].find((x) => (x.textContent ?? "").trim().startsWith(label)) : null;
      if (t) t.click();
      draw();
    });
    for (const b of el.querySelectorAll("[data-step]")) b.addEventListener("click", () => move(Number(b.dataset.step)));
    el.querySelector("[data-skip]")?.addEventListener("click", skip);
    for (const b of el.querySelectorAll("[data-choice]")) b.addEventListener("click", () => decide(Number(b.dataset.choice)));
    el.querySelector("[data-group]")?.addEventListener("change", (e) => {
      save({ group: e.target.value });
      load();
    });
  }

  const onKey = (e) => {
    if (!el.isConnected) return void globalThis.document?.removeEventListener("keydown", onKey);
    // only while the queue is on screen (a hidden tab's queue takes no keys)
    if (el.offsetParent === null && typeof el.getClientRects === "function" && el.getClientRects().length === 0) return;
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    const t = e.target;
    if (t && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName ?? "")) return;
    if (t?.isContentEditable) return;
    const k = e.key;
    if (k === "ArrowRight" || k === "j" || k === "n") move(1);
    else if (k === "ArrowLeft" || k === "k" || k === "p") move(-1);
    else if (k === "s") skip();
    else if (/^[1-9]$/.test(k) && Number(k) <= choices.length) decide(Number(k) - 1);
    else return;
    e.preventDefault();
  };
  globalThis.document?.addEventListener("keydown", onKey);

  const page = (g, after, my) => {
    const ref = { ...(gParam ? { [gParam]: g } : {}), ...(pageParam ? { [pageParam]: after } : {}) };
    loading++;
    return stamped(el, esc, ctx.request("query@1", Object.keys(ref).length ? { refine: ref } : {}), ctx.use.key).then(
      (r) => {
        loading = Math.max(0, loading - 1);
        if (my !== gen) return;
        const got = r?.results?.bindings ?? [];
        // a fresh answer drawn over the one from memory (after a signature) replaces that page
        if (!after) rows = [];
        const seen = new Set(rows.map((x) => v(x[keyCol])));
        rows = [...rows, ...got.filter((x) => !seen.has(v(x[keyCol])))];
        if (got[0] && o.total) total = Number(v(got[0][o.total])) || total;
        const more = pageParam && pageSize && got.length >= pageSize;
        complete = !more;
        failed = "";
        const list = order();
        if (!list.some((x) => v(x[keyCol]) === at(g).item) && list[0] && !after) select(list[0]);
        else if (list.length && st.item !== at(g).item) select(list[index(list)]);
        draw();
        if (more) page(g, v(got[got.length - 1][pageCol]), my);
      },
      (e) => {
        loading = Math.max(0, loading - 1);
        if (my !== gen) return;
        // a page that fails after others drew keeps what is drawn (said in the note); only an empty queue shows the error
        if (rows.length) note = `Not updated: ${e?.message ?? String(e)}`;
        else failed = e?.message ?? String(e);
        draw();
      },
    );
  };
  function load() {
    gen++;
    rows = [];
    total = 0;
    complete = false;
    note = "";
    el.innerHTML = skeleton("card");
    page(group(), "", gen);
  }
  // a jump from another block (option jump, fed by a var: {"group", "item", "from"}): this group at this item, with a way
  // back ("Back to rule C14": the tab named backTab, default "Rules")
  let lastJump = "";
  const jumpTo = (raw) => {
    // once per jump: a frame that mounts again (after the sign dialog) does not jump back
    if (!raw || raw === lastJump || raw === st.jumped) return false;
    lastJump = raw;
    let j;
    try {
      j = JSON.parse(raw);
    } catch {
      return false;
    }
    if (!j || typeof j.item !== "string") return false;
    const g = gParam && groups.includes(String(j.group)) ? String(j.group) : group();
    at(g).item = j.item;
    back = typeof j.from === "string" ? j.from : "";
    save({ group: g, item: j.item, jumped: raw });
    return true;
  };
  let back = "";
  jumpTo(o.jump);
  load();
  return {
    set(next) {
      if (next && typeof next.jump === "string" && jumpTo(next.jump)) load();
    },
    destroy() {
      globalThis.document?.removeEventListener("keydown", onKey);
      if (quiet) {
        clearTimeout(quiet);
        ctx.setState(clone(st));
      }
    },
  };
}

/** The queue's order: the open rows in the query's order without the decided and the skipped ones, then the skipped
 * ones in the order they were skipped (they come back at the end). */
export function queueOrder(rows, keyOf, place) {
  const done = new Set(place?.done ?? []);
  const skipped = (place?.skipped ?? []).filter((k) => !done.has(k));
  const sk = new Set(skipped);
  const byKey = new Map(rows.map((r) => [keyOf(r), r]));
  return [...rows.filter((r) => !done.has(keyOf(r)) && !sk.has(keyOf(r))), ...skipped.map((k) => byKey.get(k)).filter(Boolean)];
}

export const QUIET = 300;

function clone(x) {
  return x && typeof x === "object" ? JSON.parse(JSON.stringify(x)) : {};
}

const ICON_PREV = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3.5 5.5 8l4.5 4.5"/></svg>`;
const ICON_NEXT = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5"/></svg>`;

function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}
function skeleton(shape = "lines") {
  return `<div class="wb-skel ${shape}" data-area-loading aria-busy="true" role="status"><span class="sr-only">Loading…</span><i></i><i></i><i></i></div>`;
}
function emptyBox(esc, text, how) {
  return `<div class="empty" data-area-empty><svg class="i" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3"/><path d="M8 7.2v4M8 4.8v.4"/></svg><div><b>${esc(text)}</b>${how ? `<p>${esc(how)}</p>` : ""}</div></div>`;
}
// as in cards.js: an answer from memory after a signature is drawn at once and the fresh one over it
function stamped(el, esc, asked, use) {
  return {
    then(ok, err) {
      const follow = (r) => {
        const token = r?.refreshing?.token;
        const wb = globalThis.ktonWorkbench;
        if (token === undefined || !use || typeof wb?.fresh !== "function") return;
        wb.fresh(use, token).then(
          (x) => {
            if (x && x.result) ok(x.result);
          },
          () => {},
        );
      };
      return asked.then((r) => {
        const out = ok(r);
        follow(r);
        return out;
      }, err);
    },
  };
}
const ACTED = "kton-workbench:acted";
function announceAct(res, from) {
  if (res?.result?.signed !== true) return false;
  const d = globalThis.document;
  if (!d || typeof d.dispatchEvent !== "function") return false;
  d.dispatchEvent(new CustomEvent(ACTED, { detail: { from: from ?? null, claim: res.result.claim ?? null } }));
  return true;
}
