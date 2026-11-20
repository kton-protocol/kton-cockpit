// figures: numbers with their source. Each row of the query is one figure: ?value ?label ?stamp, and ?note,
// ?scope when given. Every number carries its stamp (G5.4: every number names its source); a figure without
// a stamp says so. Options:
//   guard     a column (e.g. "scope"): when the figures mix values of it, a flag says so and lists them
//   propose   "field=column": the use's acts propose that column's values (all rows, space-separated) as the field (the shell shows it
//             to the person as a default; it never signs it unseen)
//   emptyText what to say when there is no row
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  el.innerHTML = `<div class="kton-muted" data-area-loading>Loading…</div>`;
  ctx.request("query@1", {}).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const figs = rows
        .map((x) => {
          const stamp = v(x.stamp);
          return `<div class="kton-figure" data-figure><b class="kton-number" data-number>${esc(v(x.value))}</b> <span>${esc(v(x.label))}</span> ${stamp ? `<span class="kton-stamp" data-stamp>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`}${v(x.note) ? `<div class="kton-muted">${esc(v(x.note))}</div>` : ""}</div>`;
        })
        .join("");
      let guard = "";
      if (o.guard) {
        const scopes = [...new Set(rows.map((x) => v(x[o.guard])).filter(Boolean))];
        if (scopes.length > 1) {
          const parts = rows.filter((x) => v(x[o.guard])).map((x) => `${v(x.value)} (${v(x[o.guard])})`);
          guard = `<div class="kton-guard" data-guard role="alert"><b>guard</b> This view mixes counts of different ${esc(o.guard)}: ${esc(parts.join(" · "))}. Each number names its source.</div>`;
        }
      }
      const proposals = {};
      for (const p of o.propose ?? []) {
        const [field, col] = p.split("=");
        const all = [...new Set(rows.flatMap((x) => v(x[col]).split(/\s+/)).filter(Boolean))];
        if (field && col && all.length) proposals[field] = all.join(" ");
      }
      const buttons = acts.length
        ? `<div class="kton-act">${acts.map((a, i) => `<button type="button" data-act="${i}" data-testid="act-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`).join("")}<span class="kton-muted" data-act-out></span></div>`
        : "";
      el.innerHTML = `${guard}${rows.length ? figs : `<p class="kton-muted" data-area-empty>${esc(o.emptyText || "No figures.")}</p>`}${buttons}`;
      for (const b of el.querySelectorAll("[data-act]")) {
        b.addEventListener("click", () => {
          const a = acts[Number(b.dataset.act)];
          const out = el.querySelector("[data-act-out]");
          ctx.intent({ kind: a.intent, control: a.control, about: a.about, fields: proposals }).then((res) => {
            if (out) out.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
          });
        });
      }
    },
    (e) => {
      el.innerHTML = `<p class="kton-error" data-area-error>${esc(e?.message ?? String(e))}</p>`;
    },
  );
  return { set() {}, destroy() {} };
}
