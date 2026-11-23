// rules: candidate rules that settle many items with one signature, one card each, in the query's order. Each card shows
// the rule, how many items it settles and what they collapse to ("settles 240 items = 31 specs"), risk, owner, the proposed answer and why, a few drawn examples, and its members, each of which can be taken
// out before signing (the excluded list is proposed with the act; the shell shows it in the sign dialog). A signed rule
// shows who decided it and when, and that its members are "decided by rule <id>, signed by <who>".
// Options (columns unless said otherwise):
//   id, title       the rule's id and name; target the column naming what the act is about (the rule's claim)
//   size, settles   numbers; collapses what they collapse to ("31 specs"); answer the proposed answer; reason why
//   risk, owner; caveat a warning shown prominently on the card (e.g. a split that one map decides)
//   examples        "; "-separated keys shown as the drawn examples (a click emits `pick` with the key)
//   members         the members' keys, separated by sep (default " ;; "); excludeField the act's field that takes the
//                   excluded keys (joined by sep)
//   decidedBy, decidedAt, decidedVerdict, decidedClaim, excluded (sep-separated): the signed decision, if any
//   choices         "field=value|Label": one button each (e.g. accept, reject); each proposes its value
//   emptyText, emptyHow: what to say when there is no candidate, and where they come from
// Every card shows its row's ?stamp. The block keeps the excluded keys per rule in its state (a reload keeps them).
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
  const excludedOf = (id) => new Set(((st.excluded ??= {})[id] ??= []));
  const setExcluded = (id, set) => {
    (st.excluded ??= {})[id] = [...set];
    ctx.setState(JSON.parse(JSON.stringify(st)));
  };
  const heading = o.heading ? `<h3 data-block-title>${esc(o.heading)}</h3>` : "";
  el.innerHTML = `${heading}${skeleton("card")}`;
  stamped(ctx.request("query@1", {}), ctx.use.key).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const out = new Map();
      const memberList = (x) => col(x, o.members).split(sep).map((s) => s.trim()).filter(Boolean);
      const membersHtml = (x, id, filter, all) => {
        const ex = excludedOf(id);
        const list = memberList(x).filter((k) => !filter || k.toLowerCase().includes(filter.toLowerCase()));
        const shown = all ? list : list.slice(0, SHOW);
        return `${shown.map((k) => `<li><label><input type="checkbox" data-member="${esc(k)}"${ex.has(k) ? "" : " checked"}${col(x, o.decidedBy) ? " disabled" : ""}> <span class="mono">${esc(k)}</span></label></li>`).join("")}${list.length > shown.length ? `<li><button type="button" class="ghost small" data-all>Show all ${list.length}</button></li>` : ""}`;
      };
      const card = (x, i) => {
        const id = col(x, o.id) || String(i);
        const stamp = v(x.stamp);
        const st0 = stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
        const size = col(x, o.size);
        const settles = col(x, o.settles) || size;
        const coll = col(x, o.collapses);
        const ex = excludedOf(id);
        const members = memberList(x);
        const decided = col(x, o.decidedBy);
        const dex = col(x, o.excluded).split(sep).map((s) => s.trim()).filter(Boolean);
        const verdict = col(x, o.decidedVerdict);
        const settledN = verdict === "accepted" ? Math.max(0, (Number(settles) || members.length) - dex.length) : 0;
        const decidedHtml = decided
          ? `<div class="callout info" data-decided role="status"><p><b>Decided by rule ${esc(id)}</b>: ${esc(verdict || "signed")}, signed by ${esc(decided)}${col(x, o.decidedAt) ? ` at ${esc(col(x, o.decidedAt))}` : ""}${col(x, o.decidedClaim) ? ` (claim ${esc(shortClaim(col(x, o.decidedClaim)))})` : ""}.</p><p data-decided-members>${verdict === "accepted" ? `${num(String(settledN))} members: decided by rule ${esc(id)}, signed by ${esc(decided)}.` : "No member is settled by this decision."}${dex.length ? ` Taken out (${dex.length}, back in the queue): ${dex.map((k) => `<span class="mono">${esc(k)}</span>`).join(", ")}.` : ""}</p></div>`
          : "";
        const caveat = col(x, o.caveat) ? `<div class="callout" data-caveat role="status"><p><b>Check the area map.</b> ${esc(col(x, o.caveat))}</p></div>` : "";
        const examples = col(x, o.examples)
          .split(";")
          .map((s) => s.trim())
          .filter(Boolean)
          .map((k) => `<button type="button" class="ghost small mono" data-example="${esc(k)}" data-testid="example-${esc(k)}">${esc(k)}</button>`)
          .join(" ");
        const fields = (o.fields ?? [])
          .map(split)
          .map(([c, label]) => (col(x, c) ? `<dt>${esc(label)}</dt><dd data-col="${esc(c)}">${esc(col(x, c))}</dd>` : ""))
          .join("");
        const buttons = act && col(x, o.target) && !decided
          ? choices.map((c, ci) => `<button type="button" class="${ci === 0 ? "primary" : "secondary"}" data-rule="${i}" data-choice="${ci}" data-testid="rule-${esc(id)}-${esc(c.value)}">${esc(c.label)}</button>`).join("")
          : "";
        return `<article class="kton-card card" data-rule-row="${i}" data-rule-id="${esc(id)}" data-target="${esc(col(x, o.target))}" aria-label="${esc(id)}">
  <div class="card-head"><h4 class="id">${esc(id)}</h4> <b>${esc(col(x, o.title))}</b> ${st0}${col(x, o.risk) ? `<span class="badge" data-risk>risk: ${esc(col(x, o.risk))}</span>` : ""}</div>
  <p class="rc-res num" data-settles>Settles ${esc(num(settles))} item${settles === "1" ? "" : "s"}${coll ? ` = ${esc(coll)}` : ""}${col(x, o.answer) ? `: ${esc(col(x, o.answer))}` : ""}</p>
  ${caveat}${decidedHtml}
  ${fields ? `<dl class="kv">${fields}</dl>` : ""}
  ${examples ? `<p data-examples><span class="muted">Drawn examples (show one on the item screen):</span> ${examples}</p>` : ""}
  ${members.length ? `<details data-members><summary>Members (${num(String(members.length))}) · <span data-excluded-count>${ex.size} taken out</span></summary><p><input type="search" data-filter placeholder="Filter members" aria-label="Filter members"></p><ul class="members">${membersHtml(x, id, "", false)}</ul></details>` : `<p class="empty-line">No members listed.</p>`}
  ${buttons ? `<div class="kton-act actions">${buttons}<span class="kton-muted" data-act-out="${i}" role="status"></span></div>` : ""}
</article>`;
      };
      el.innerHTML = `${heading}${rows.length ? `<div class="cards-list" data-rules-count="${rows.length}">${rows.map(card).join("")}</div>` : emptyBox(esc, o.emptyText || "No rule candidates.", o.emptyHow)}`;
      const wireMembers = (art, x, id) => {
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
          wireMembers(art, x, id);
        });
      };
      for (const art of el.querySelectorAll("article[data-rule-row]")) {
        const i = Number(art.dataset.ruleRow);
        const x = rows[i];
        const id = art.dataset.ruleId;
        wireMembers(art, x, id);
        art.querySelector("[data-filter]")?.addEventListener("input", (e) => {
          art.querySelector("ul.members").innerHTML = membersHtml(x, id, e.target.value, false);
          wireMembers(art, x, id);
        });
      }
      for (const b of el.querySelectorAll("button[data-example]")) b.addEventListener("click", () => ctx.setState({ ...JSON.parse(JSON.stringify(st)), pick: b.dataset.example }));
      for (const b of el.querySelectorAll("button[data-choice]")) {
        b.addEventListener("click", () => {
          const x = rows[Number(b.dataset.rule)];
          const id = col(x, o.id);
          const c = choices[Number(b.dataset.choice)];
          const ex = [...excludedOf(id)].filter((k) => memberList(x).includes(k));
          const outEl = el.querySelector(`[data-act-out="${b.dataset.rule}"]`);
          const fields = { [c.field]: c.value, ...(ex.length && o.excludeField ? { [o.excludeField]: ex.join(sep) } : {}) };
          b.disabled = true;
          if (outEl) outEl.textContent = `${c.label}: ${ex.length} taken out; confirm in the sign dialog…`;
          ctx.intent({ kind: act.intent, control: act.control, about: act.about, fields, target: col(x, o.target) }).then((res) => {
            b.disabled = false;
            if (outEl) outEl.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
            announceAct(res, ctx.use.key);
          });
        });
      }
      out.clear();
    },
    (e) => {
      el.innerHTML = `${heading}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return { set() {}, destroy() {} };
}

function shortClaim(c) {
  const m = /([0-9a-f]{64})$/.exec(c);
  return m ? `${m[1].slice(0, 12)}…` : c;
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
