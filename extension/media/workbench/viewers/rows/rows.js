// rows: the workbench's one data block for the generic QM area (PR 3). It shows a named query's answer as a
// table (or as one record's fields, option card), and knows nothing of any package: what to show comes from
// the plan's options, which the shell writes from the area declaration.
//   columns   the columns to show, in this order (default: every column the query answers)
//   pick      a row click emits `select` with this column's IRI (wired by the composition to a var)
//   open      a row click asks the host to open this column's IRI (with the row's claim): list → record
//   rules     keep only the rows whose ?rule is one of these (an area's findings.rules)
//   keys      keep only the rows whose ?key is one of these (an area's documents)
//   match     keep only the rows whose column equals the subject's claim (a record's approvals)
//   card      "yes": the first row as fields, not a table
//   emptyText what to say when no row is left (default: "No rows.")
//   atLeast   "column:n": keep only the rows whose column is at least n (e.g. agreement after 10 verdicts)
// A column `stamp` is shown as each row's source chip, and the row's numbers are marked as numbers.
// The use's acts (sign / record through a control) are buttons; the host decides everything about them.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.acts ?? [];
  const subjectClaim = ctx.subject?.claim ? `https://kton.dev/o/${String(ctx.subject.claim).replace(/^sha256:/, "")}` : null;
  const value = (b) => (b && typeof b.value === "string" ? b.value : "");
  const shortly = (v) => (/^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(v) ? `sha256:${v.slice(19, 31)}…` : v);
  el.innerHTML = `<div class="kton-muted" data-area-loading>Loading…</div>`;

  const actButtons = () =>
    acts.length
      ? `<div class="kton-act" data-area-acts>${acts
          .map((a, i) => `<button type="button" data-act="${i}" data-testid="act-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`)
          .join("")}<span class="kton-muted" data-act-out></span></div>`
      : "";
  const wireActs = () => {
    for (const b of el.querySelectorAll("[data-act]")) {
      b.addEventListener("click", () => {
        const a = acts[Number(b.dataset.act)];
        const out = el.querySelector("[data-act-out]");
        b.disabled = true;
        ctx.intent({ kind: a.intent, control: a.control, about: a.about, fields: {} }).then((r) => {
          b.disabled = false;
          if (out) out.textContent = r?.refused ? `Not done: ${r.refused.why ?? r.refused}` : r?.result?.message ?? (r?.done ? "Done." : "");
        });
      });
    }
  };

  const draw = (vars, rows) => {
    const cols = (o.columns && o.columns.length ? o.columns : vars).filter((c) => vars.includes(c));
    let body;
    if (!rows.length) body = `<p class="kton-muted" data-area-empty>${esc(o.emptyText || "No rows.")}</p>`;
    else if (o.card === "yes") {
      const r = rows[0];
      body = `<dl class="kton-card" data-area-card>${cols
        .filter((c) => r[c])
        .map((c) => `<dt>${esc(c)}</dt><dd data-col="${esc(c)}">${esc(shortly(value(r[c])))}</dd>`)
        .join("")}</dl>`;
    } else {
      const click = o.pick || o.open;
      body = `<table class="kton-table" data-area-table data-count="${rows.length}"><thead><tr>${cols.map((c) => `<th>${esc(c)}</th>`).join("")}</tr></thead><tbody>${rows
        .map(
          (r, i) =>
            `<tr data-row="${i}"${click && value(r[click]) ? ` class="kton-clickable" tabindex="0" data-target="${esc(value(r[click]))}"` : ""}>${cols
              .map((c) => (c === "stamp" ? `<td data-col="stamp"><span class="kton-stamp" data-stamp>${esc(value(r[c]))}</span></td>` : `<td data-col="${esc(c)}"${/^-?\d+(\.\d+)?%?$/.test(value(r[c])) ? " data-number" : ""}>${esc(shortly(value(r[c])))}</td>`))
              .join("")}</tr>`,
        )
        .join("")}</tbody></table>`;
    }
    el.innerHTML = `${body}${actButtons()}`;
    wireActs();
    for (const tr of el.querySelectorAll("tr[data-target]")) {
      tr.addEventListener("click", () => {
        const row = rows[Number(tr.dataset.row)];
        if (o.pick) ctx.intent({ kind: "select", value: tr.dataset.target });
        else if (o.open) ctx.intent({ kind: "open", uri: tr.dataset.target, ...(row.claim ? { claim: value(row.claim) } : {}) });
      });
    }
  };

  ctx.request("query@1", {}).then(
    (r) => {
      const vars = r?.head?.vars ?? [];
      let rows = r?.results?.bindings ?? [];
      if (o.rules) rows = rows.filter((x) => o.rules.includes(value(x.rule)));
      if (o.keys) rows = rows.filter((x) => o.keys.includes(value(x.key)));
      if (o.match) rows = subjectClaim ? rows.filter((x) => value(x[o.match]) === subjectClaim) : [];
      if (o.atLeast) {
        const [col, min] = o.atLeast.split(":");
        rows = rows.filter((x) => Number(value(x[col])) >= Number(min));
      }
      draw(vars, rows);
    },
    (e) => {
      el.innerHTML = `<p class="kton-error" data-area-error>${esc(e?.message ?? String(e))}</p>`;
    },
  );
  return { set() {}, destroy() {} };
}
