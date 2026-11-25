// rules: candidate rules that settle many items with one signature. Two ways to read them, one block:
//   overview  the open rules, one line each (id, name, "settles N items = <what they collapse to>", risk, the caveat flag),
//             a "Start" that opens the card flow at the first open rule, and a collapsed "Decided (n)" with who signed
//             each and when;
//   flow      one rule per card, with where it is ("Rule 3 of 15 open") and the same keys as the item queue: → / j next,
//             ← / k previous, s skip (comes back at the end), Escape back to the overview. The card shows how many items
//             it settles and what they collapse to, the proposed answer, risk, owner, the caveat
//             (e.g. "check the map"), drawn examples (a click emits `pick`), its members with a check box each (take
//             some out before signing) and the act buttons (one signature for all members but the ones taken out).
// A rule is decided when the query names its decision (decidedBy …), or when this view signed it (host-held, before the
// cockpit's answer has it: "signed here at hh:mm, the cockpit's answer follows"). Decided rules leave the open list.
// Options (columns unless said otherwise):
//   id, title       the rule's id and name; target the column naming what the act is about (the rule's claim)
//   size, settles   numbers; collapses what they collapse to ("31 specs"); answer the proposed answer; risk
//   caveat          a warning shown prominently on the card; examples "; "-separated keys (drawn examples)
//   members         the members' keys, separated by sep (default " ;; "); excludeField the act's field that takes the
//                   keys taken out (joined by sep)
//   decidedBy, decidedAt, decidedVerdict, decidedClaim, excluded (sep-separated): the signed decision, if any
//   fields          "column|Label" pairs shown on the card; choices "field=value|Label": one act button each
//   emptyText, emptyHow, heading
// The block keeps, in its state, the mode, the rule on screen, the skipped rules and the keys taken out per rule.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const act = acts[0];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const col = (x, name) => (name ? v(x[name]) : "");
  const sep = o.sep ?? " ;; ";
  const split = (s) => {
    const i = s.indexOf("|");
    return i < 0 ? [s, s] : [s.slice(0, i), s.slice(i + 1)];
  };
  const choices = (o.choices ?? []).map((c) => {
    const [fv, label] = split(c);
    const k = fv.indexOf("=");
    return { field: fv.slice(0, k), value: fv.slice(k + 1), label };
  });
  const num = (s) => (/^\d+$/.test(s) ? Number(s).toLocaleString("en-US") : s);
  const SHOW = 200;
  let st = ctx.state && typeof ctx.state === "object" ? JSON.parse(JSON.stringify(ctx.state)) : {};
  const save = (patch) => {
    st = { ...st, ...patch };
    ctx.setState(JSON.parse(JSON.stringify(st)));
  };
  const excludedOf = (id) => new Set((st.excluded ?? {})[id] ?? []);
  const setExcluded = (id, set) => save({ excluded: { ...(st.excluded ?? {}), [id]: [...set] } });
  const heading = o.heading ? `<h3 data-block-title>${esc(o.heading)}</h3>` : "";
  let rows = [];
  let note = "";

  /** this view's own signatures of a rule (by its target), host-held */
  const signedHere = () => new Map((globalThis.ktonWorkbench?.signedActs?.(ctx.use.key) ?? []).filter((a) => a.target).map((a) => [a.target, a]));
  const decision = (x) => {
    if (col(x, o.decidedBy)) return { by: col(x, o.decidedBy), at: col(x, o.decidedAt), verdict: col(x, o.decidedVerdict), claim: col(x, o.decidedClaim), excluded: col(x, o.excluded), here: false };
    const a = signedHere().get(col(x, o.target));
    if (a) return { by: "you, in this view", at: new Date(a.at).toISOString().replace("T", " ").slice(0, 16), verdict: a.fields?.verdict ?? "", claim: a.claim, excluded: (o.excludeField && a.fields?.[o.excludeField]) || "", here: true };
    return null;
  };
  const idOf = (x, i) => col(x, o.id) || String(i);
  /** " · 18 members decided by this rule; 1 taken out" (accepted), or nothing */
  const decidedCount = (x, d) => {
    if (d.verdict !== "accepted") return "";
    const dex = d.excluded.split(sep).map((s) => s.trim()).filter(Boolean);
    const n = Math.max(0, (Number(col(x, o.settles) || col(x, o.size)) || memberList(x).length) - dex.length);
    return ` · <span data-decided-count>${num(String(n))} members decided by this rule${dex.length ? `; ${dex.length} taken out` : ""}</span>`;
  };
  const open = () => rows.filter((x) => !decision(x));
  const flowOrder = () => {
    const op = open();
    const sk = (st.skipped ?? []).filter((id) => op.some((x, i) => idOf(x, i) === id));
    return [...op.filter((x, i) => !sk.includes(idOf(x, i))), ...sk.map((id) => op.find((x, i) => idOf(x, i) === id)).filter(Boolean)];
  };
  const memberList = (x) => col(x, o.members).split(sep).map((s) => s.trim()).filter(Boolean);
  // the element shown beside the rule card (the event pick → the item screen blocks beside it; v1.2)
  const shownBeside = () => (st.pick && st.pick !== "none" ? st.pick : "");
  const besideBar = (id, d) => {
    const k = shownBeside();
    if (!k) return "";
    const isMember = rows.some((x, i) => idOf(x, i) === id && memberList(x).includes(k));
    const out = excludedOf(id).has(k);
    return `<div class="callout info wb-beside" data-beside="${esc(k)}" role="status"><p><b>Beside this rule:</b> the item screen of <span class="mono">${esc(k)}</span> (spec, judges, tests, code, runs, earlier decisions, stamps).</p><p class="actions">${isMember && !d ? `<button type="button" class="small secondary" data-beside-out data-testid="beside-takeout">${out ? "Put it back into the rule" : "Take this member out of the rule"}</button>` : ""}<button type="button" class="small secondary" data-beside-queue data-testid="beside-queue">Open in item queue</button><button type="button" class="small" data-beside-close data-testid="beside-close">Close (Esc)</button></p></div>`;
  };
  /** the keys opened beside a rule: proposed as what was looked at when it is signed */
  const looked = (id) => (st.looked ?? {})[id] ?? [];

  const membersHtml = (x, id, filter, all) => {
    const ex = excludedOf(id);
    const list = memberList(x).filter((k) => !filter || k.toLowerCase().includes(filter.toLowerCase()));
    const shown = all ? list : list.slice(0, SHOW);
    return `${shown.map((k) => `<li><input type="checkbox" data-member="${esc(k)}" aria-label="Keep ${esc(k)} in the rule"${ex.has(k) ? "" : " checked"}> <button type="button" class="link mono" data-example="${esc(k)}" title="Show ${esc(k)} beside this rule">${esc(k)}</button></li>`).join("")}${list.length > shown.length ? `<li><button type="button" class="ghost small" data-all>Show all ${list.length}</button></li>` : ""}`;
  };
  const stampOf = (x) => {
    const stamp = v(x.stamp);
    return stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
  };
  const settlesLine = (x) => {
    const settles = col(x, o.settles) || col(x, o.size);
    const coll = col(x, o.collapses);
    return `Settles ${esc(num(settles))} item${settles === "1" ? "" : "s"}${coll ? ` = ${esc(coll)}` : ""}${col(x, o.answer) ? `: ${esc(col(x, o.answer))}` : ""}`;
  };
  const decidedText = (x, id, d) => {
    const dex = d.excluded.split(sep).map((s) => s.trim()).filter(Boolean);
    const settles = Number(col(x, o.settles) || col(x, o.size)) || memberList(x).length;
    const n = d.verdict === "accepted" ? Math.max(0, settles - dex.length) : 0;
    return `<p><b>Decided by rule ${esc(id)}</b>: ${esc(d.verdict || "signed")}, signed by ${esc(d.by)}${d.at ? ` at ${esc(d.at)}` : ""}${d.claim ? ` (claim ${esc(shortClaim(d.claim))})` : ""}.${d.here ? " The cockpit's answer follows." : ""}</p><p data-decided-members>${d.verdict === "accepted" ? `${num(String(n))} members: decided by rule ${esc(id)}, signed by ${esc(d.by)}.` : "No member is settled by this decision."}${dex.length ? ` Taken out (${dex.length}, back in the queue): ${dex.map((k) => `<span class="mono">${esc(k)}</span>`).join(", ")}.` : ""}</p>`;
  };

  // one rule, in full (the flow's card; also used for a decided rule's detail)
  const card = (x, i, pos) => {
    const id = idOf(x, i);
    const d = decision(x);
    const ex = excludedOf(id);
    const members = memberList(x);
    const caveat = col(x, o.caveat) ? `<div class="callout" data-caveat role="status"><p><b>Check the area map.</b> ${esc(col(x, o.caveat))}</p></div>` : "";
    const examples = col(x, o.examples).split(";").map((s) => s.trim()).filter(Boolean).map((k) => `<button type="button" class="ghost small mono" data-example="${esc(k)}" data-testid="example-${esc(k)}">${esc(k)}</button>`).join(" ");
    const fields = (o.fields ?? []).map(split).map(([c, label]) => (col(x, c) ? `<dt>${esc(label)}</dt><dd data-col="${esc(c)}">${esc(col(x, c))}</dd>` : "")).join("");
    const buttons = act && col(x, o.target) && !d ? choices.map((c, ci) => `<button type="button" class="${ci === 0 ? "primary" : "secondary"}" data-choice="${ci}" data-testid="rule-${esc(id)}-${esc(c.value)}">${esc(c.label)}</button>`).join("") : "";
    return `<article class="kton-card card" data-rule-row="${i}" data-rule-id="${esc(id)}" data-target="${esc(col(x, o.target))}" aria-label="${esc(id)}">
  ${pos ?? ""}
  <div class="card-head"><h4 class="id">${esc(id)}</h4> <b>${esc(col(x, o.title))}</b> ${stampOf(x)}${col(x, o.risk) ? `<span class="badge" data-risk>risk: ${esc(col(x, o.risk))}</span>` : ""}</div>
  <p class="rc-res num" data-settles>${settlesLine(x)}</p>
  ${caveat}${d ? `<div class="callout info" data-decided role="status">${decidedText(x, id, d)}</div>` : ""}
  ${fields ? `<dl class="kv">${fields}</dl>` : ""}
  ${examples ? `<p data-examples><span class="muted">Drawn examples (click or Enter: its item screen beside this rule):</span> ${examples}</p>` : ""}
  ${besideBar(id, d)}
  ${members.length && !d ? `<details data-members${st.mode === "flow" ? " open" : ""}><summary>Members (${num(String(members.length))}) · <span data-excluded-count>${ex.size} taken out</span></summary><p><input type="search" data-filter placeholder="Filter members" aria-label="Filter members"></p><ul class="members">${membersHtml(x, id, "", false)}</ul></details>` : ""}
  ${buttons ? `<div class="kton-act actions">${buttons}<span class="kton-muted" data-act-out role="status">${esc(note)}</span></div>` : ""}
</article>`;
  };

  const overview = () => {
    const op = open();
    const done = rows.map((x, i) => ({ x, i, d: decision(x) })).filter((r) => r.d);
    const line = (x) => {
      const i = rows.indexOf(x);
      const id = idOf(x, i);
      return `<li data-rule-line="${esc(id)}"><button type="button" class="ghost small mono" data-goto="${esc(id)}" aria-label="Open rule ${esc(id)}">${esc(id)}</button> <b>${esc(col(x, o.title))}</b> <span class="muted" data-settles>${settlesLine(x)}</span>${col(x, o.risk) ? ` <span class="badge">risk: ${esc(col(x, o.risk))}</span>` : ""}${col(x, o.caveat) ? ` <span class="badge" data-caveat-flag title="${esc(col(x, o.caveat))}">check the area map</span>` : ""}</li>`;
    };
    const decided = done.length
      ? `<details data-decided-list><summary>Decided (${done.length})</summary><ul class="rule-lines">${done.map(({ x, i, d }) => `<li data-decided-line="${esc(idOf(x, i))}"><span class="mono">${esc(idOf(x, i))}</span> <b>${esc(col(x, o.title))}</b> · ${esc(d.verdict || "signed")} · signed by ${esc(d.by)}${d.at ? ` at ${esc(d.at)}` : ""}${d.here ? " (the cockpit's answer follows)" : ""}${decidedCount(x, d)}</li>`).join("")}</ul></details>`
      : "";
    return `<div class="pager" data-rules-overview><span class="pos" data-open-count>${op.length} open · ${done.length} decided</span>${op.length ? `<button type="button" class="primary small" data-start data-testid="rules-start">Start: one rule at a time</button>` : ""}<span class="kton-muted" role="status">${esc(note)}</span></div>
${op.length ? `<ul class="rule-lines" data-open-list>${op.map(line).join("")}</ul>` : emptyBox(esc, rows.length ? "Every rule is decided." : o.emptyText || "No rule candidates.", rows.length ? "" : o.emptyHow)}
${decided}`;
  };

  const flow = () => {
    const list = flowOrder();
    if (!list.length) {
      save({ mode: "overview" });
      return overview();
    }
    let i = list.findIndex((x) => idOf(x, rows.indexOf(x)) === st.cur);
    if (i < 0) i = 0;
    const x = list[i];
    const sk = (st.skipped ?? []).length;
    const pos = `<div class="pager" data-rules-flow><button type="button" class="ghost small" data-step="-1" aria-label="Previous rule (←, k)">${ICON_PREV}</button><span class="pos" data-pos aria-live="polite">Rule ${i + 1} of ${list.length} open</span><button type="button" class="ghost small" data-step="1" aria-label="Next rule (→, j)">${ICON_NEXT}</button><button type="button" class="small secondary" data-skip>Skip (s)</button><button type="button" class="ghost small" data-overview>Overview (Esc)</button>${sk ? `<span class="muted">${sk} skipped (they come back at the end)</span>` : ""}</div><p class="muted q-keys">Keys: → or j next · ← or k previous · s skip · Esc overview</p>`;
    if (idOf(x, rows.indexOf(x)) !== st.cur) st = { ...st, cur: idOf(x, rows.indexOf(x)) };
    return card(x, rows.indexOf(x), pos);
  };

  const showBeside = (rid, k) => {
    const l = new Set(looked(rid));
    l.add(k);
    save({ pick: k, looked: { ...(st.looked ?? {}), [rid]: [...l] }, ...(st.mode !== "flow" && rid ? { mode: "flow", cur: rid } : {}) });
    draw();
  };
  const closeBeside = () => {
    save({ pick: "none" });
    draw();
  };
  // redrawn in place: the member list keeps its scroll, the page its position
  const draw = () => {
    const ul = el.querySelector("ul.members");
    const top = ul ? ul.scrollTop : 0;
    const filter = el.querySelector("[data-filter]")?.value ?? "";
    el.innerHTML = `${heading}${st.mode === "flow" ? flow() : overview()}`;
    wire();
    const ul2 = el.querySelector("ul.members");
    if (ul2) ul2.scrollTop = top;
    const f2 = el.querySelector("[data-filter]");
    if (f2 && filter) {
      f2.value = filter;
      f2.dispatchEvent(new Event("input"));
    }
  };

  const move = (d) => {
    const list = flowOrder();
    if (!list.length) return;
    let i = list.findIndex((x) => idOf(x, rows.indexOf(x)) === st.cur);
    if (i < 0) i = 0;
    const j = Math.min(Math.max(i + d, 0), list.length - 1);
    note = "";
    save({ cur: idOf(list[j], rows.indexOf(list[j])) });
    draw();
  };
  const skip = () => {
    const list = flowOrder();
    let i = list.findIndex((x) => idOf(x, rows.indexOf(x)) === st.cur);
    if (i < 0) return;
    const id = st.cur;
    const skipped = [...(st.skipped ?? []).filter((x) => x !== id), id];
    st = { ...st, skipped };
    const next = flowOrder();
    save({ cur: idOf(next[Math.min(i, next.length - 1)], rows.indexOf(next[Math.min(i, next.length - 1)])) });
    note = `Skipped ${id}: it comes back at the end.`;
    draw();
  };

  function wire() {
    el.querySelector("[data-start]")?.addEventListener("click", () => {
      const list = flowOrder();
      save({ mode: "flow", cur: list[0] ? idOf(list[0], rows.indexOf(list[0])) : "" });
      draw();
    });
    for (const b of el.querySelectorAll("[data-goto]")) b.addEventListener("click", () => {
      save({ mode: "flow", cur: b.dataset.goto });
      draw();
    });
    el.querySelector("[data-overview]")?.addEventListener("click", () => {
      save({ mode: "overview" });
      draw();
    });
    for (const b of el.querySelectorAll("[data-step]")) b.addEventListener("click", () => move(Number(b.dataset.step)));
    el.querySelector("[data-skip]")?.addEventListener("click", skip);
    const art0 = el.querySelector("article[data-rule-row]");
    const rid = art0?.dataset.ruleId ?? "";
    for (const b of el.querySelectorAll("button[data-example]")) b.addEventListener("click", () => showBeside(rid, b.dataset.example));
    el.querySelector("[data-beside-close]")?.addEventListener("click", closeBeside);
    el.querySelector("[data-beside-out]")?.addEventListener("click", () => {
      const k = shownBeside();
      const ex = excludedOf(rid);
      if (ex.has(k)) ex.delete(k);
      else ex.add(k);
      setExcluded(rid, ex);
      draw();
    });
    el.querySelector("[data-beside-queue]")?.addEventListener("click", () => {
      const k = shownBeside();
      // the item flow at this element (the queue's group of this rule), with a way back to this rule
      save({ jump: JSON.stringify({ group: rid, item: k, from: rid, n: Date.now() }) });
      showTab(o.queueTab ?? "Items");
    });
    const art = el.querySelector("article[data-rule-row]");
    if (!art) return;
    const x = rows[Number(art.dataset.ruleRow)];
    const id = art.dataset.ruleId;
    const wireMembers = () => {
      for (const cb of art.querySelectorAll("input[data-member]")) {
        cb.addEventListener("change", () => {
          const ex = excludedOf(id);
          if (cb.checked) ex.delete(cb.dataset.member);
          else ex.add(cb.dataset.member);
          setExcluded(id, ex);
          const c = art.querySelector("[data-excluded-count]");
          if (c) c.textContent = `${ex.size} taken out`;
        });
      }
      art.querySelector("[data-all]")?.addEventListener("click", () => {
        art.querySelector("ul.members").innerHTML = membersHtml(x, id, art.querySelector("[data-filter]")?.value ?? "", true);
        wireMembers();
      });
    };
    wireMembers();
    art.querySelector("[data-filter]")?.addEventListener("input", (e) => {
      art.querySelector("ul.members").innerHTML = membersHtml(x, id, e.target.value, false);
      wireMembers();
    });
    for (const b of art.querySelectorAll("button[data-choice]")) {
      b.addEventListener("click", () => {
        const c = choices[Number(b.dataset.choice)];
        const ex = [...excludedOf(id)].filter((k) => memberList(x).includes(k));
        const out = art.querySelector("[data-act-out]");
        const lk = looked(id);
        const fields = { [c.field]: c.value, ...(ex.length && o.excludeField ? { [o.excludeField]: ex.join(sep) } : {}), ...(lk.length && o.lookedField ? { [o.lookedField]: lk.join(" ") } : {}) };
        b.disabled = true;
        if (out) out.textContent = `${c.label}: ${ex.length} taken out; confirm in the sign dialog…`;
        ctx.intent({ kind: act.intent, control: act.control, about: act.about, fields, target: col(x, o.target) }).then((res) => {
          b.disabled = false;
          note = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
          announceAct(res, ctx.use.key);
          // signed: the rule leaves the open list (host-held until the cockpit's answer has it); the flow moves on
          if (res?.result?.signed === true) move(0);
          else draw();
        });
      });
    }
  }

  const onKey = (e) => {
    if (!el.isConnected) return void globalThis.document?.removeEventListener("keydown", onKey);
    if (st.mode !== "flow" && !(e.key === "Escape" && shownBeside())) return;
    if (typeof el.getClientRects === "function" && el.getClientRects().length === 0) return;
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    const t = e.target;
    if (t && /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName ?? "")) return;
    const k = e.key;
    if (k === "ArrowRight" || k === "j" || k === "n") move(1);
    else if (k === "ArrowLeft" || k === "k" || k === "p") move(-1);
    else if (k === "s") skip();
    else if (k === "Escape") {
      if (shownBeside()) closeBeside();
      else {
        save({ mode: "overview" });
        draw();
      }
    } else return;
    e.preventDefault();
  };
  globalThis.document?.addEventListener("keydown", onKey);

  el.innerHTML = `${heading}${skeleton("card")}`;
  stamped(ctx.request("query@1", {}), ctx.use.key).then(
    (r) => {
      rows = r?.results?.bindings ?? [];
      draw();
    },
    (e) => {
      el.innerHTML = `${heading}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return {
    set() {},
    destroy() {
      globalThis.document?.removeEventListener("keydown", onKey);
    },
  };
}

/** Show a tab of the view by its label (the frame's tabs; a block cannot switch them otherwise). */
export function showTab(label) {
  const d = globalThis.document;
  const t = d ? [...d.querySelectorAll('[role="tab"]')].find((x) => (x.textContent ?? "").trim().startsWith(label)) : null;
  if (t) t.click();
  return !!t;
}

const ICON_PREV = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3.5 5.5 8l4.5 4.5"/></svg>`;
const ICON_NEXT = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3.5 10.5 8 6 12.5"/></svg>`;
function shortClaim(c) {
  const m = /([0-9a-f]{64})$/.exec(c);
  return m ? `${m[1].slice(0, 8)}…` : c;
}
function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}
function skeleton(shape = "lines") {
  return `<div class="wb-skel ${shape}" data-area-loading aria-busy="true" role="status"><span class="sr-only">Loading…</span><i></i><i></i><i></i></div>`;
}
function emptyBox(esc, text, how) {
  return `<div class="empty" data-area-empty><svg class="i" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3"/><path d="M8 7.2v4M8 4.8v.4"/></svg><div><b>${esc(text)}</b>${how ? `<p>${esc(how)}</p>` : ""}</div></div>`;
}
function stamped(asked, use) {
  return {
    then(ok, err) {
      return asked.then((r) => {
        const out = ok(r);
        const token = r?.refreshing?.token;
        const wb = globalThis.ktonWorkbench;
        if (token !== undefined && use && typeof wb?.fresh === "function") wb.fresh(use, token).then((x) => x && x.result && ok(x.result), () => {});
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
