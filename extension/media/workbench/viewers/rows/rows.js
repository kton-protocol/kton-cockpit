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
//   page      "<param>=<column>": keyset paging over a refine param the use declares; "Next" asks again with the
//             param set to the last row's column (the query keeps the rows after it), "Previous" goes back
//   pageSize  the query's page size (its LIMIT): a full page offers "Next"
// A refine param the use declares with values (a reader's choice, ktonpkg refine) is a select above the table.
// The use's acts (sign / record through a control) are buttons; the host decides everything about them. With
//   target    the column naming what a row is about: each row gets the act buttons, about that row (the host checks it
//             is a row it served this block); without it the acts are one row of buttons below the table
//   choices   "field=value|Button label": one button per choice and act; the value is proposed for the field.
//             "<control>:field=value|Button label" is a choice of that control's act only (an act without a choice of
//             its own is one plain button)
//   propose   "field=column": each row's act proposes that row's column for the field (never signed unseen: the shell
//             asks it, with the value as the default)
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const subjectClaim = ctx.subject?.claim ? `https://kton.dev/o/${String(ctx.subject.claim).replace(/^sha256:/, "")}` : null;
  const value = (b) => (b && typeof b.value === "string" ? b.value : "");
  const shortly = (v) => (/^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(v) ? `sha256:${v.slice(19, 31)}…` : v);
  const refine = ctx.use.refine ?? {};
  const [pageParam, pageCol] = typeof o.page === "string" && o.page.includes("=") ? o.page.split("=", 2) : [undefined, undefined];
  const pageSize = Number(o.pageSize ?? 0) || 0;
  const paging = !!pageParam && !!refine[pageParam] && !!pageCol;
  // the reader's current refine values: a choice per declared values param, the page key; the plan's defaults first
  const cur = {};
  for (const [k, spec] of Object.entries(refine)) if (Array.isArray(spec.values)) cur[k] = value(ctx.use.params?.[k]) || String(spec.values[0] ?? "");
  const back = [];
  el.innerHTML = `<div class="kton-muted" data-area-loading>Loading…</div>`;

  const split = (x) => {
    const i = x.indexOf("|");
    return i < 0 ? [x, x] : [x.slice(0, i), x.slice(i + 1)];
  };
  const choices = (o.choices ?? []).map((c) => {
    const [cfv, label] = split(c);
    const k = cfv.indexOf("=");
    const left = cfv.slice(0, k);
    const colon = left.indexOf(":");
    return { control: colon < 0 ? undefined : left.slice(0, colon), field: colon < 0 ? left : left.slice(colon + 1), value: cfv.slice(k + 1), label };
  });
  const choicesOf = (a) => choices.map((c, ci) => ({ c, ci })).filter(({ c }) => c.control === undefined || c.control === a.control?.name);
  const proposes = (o.propose ?? []).map((p) => [p.slice(0, p.indexOf("=")), p.slice(p.indexOf("=") + 1)]).filter(([f]) => f);
  const perRow = !!o.target && acts.length > 0;
  const rowButtons = (r, i) =>
    value(r[o.target])
      ? acts
          .flatMap((a, ai) => {
            const own = choicesOf(a);
            return own.length ? own.map(({ c, ci }) => ({ a, ai, c, ci })) : [{ a, ai, c: null, ci: -1 }];
          })
          .map(({ a, ai, c, ci }) => `<button type="button" data-row-act="${i}" data-act-i="${ai}" data-choice="${ci}" data-testid="row-${i}-${esc(c ? c.value : a.control?.name ?? a.intent)}">${esc(c ? c.label : a.label ?? a.intent)}</button>`)
          .join(" ") + ` <span class="kton-muted" data-row-out="${i}"></span>`
      : "";
  const wireRowActs = (rows) => {
    for (const b of el.querySelectorAll("button[data-row-act]")) {
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        const row = rows[Number(b.dataset.rowAct)];
        const a = acts[Number(b.dataset.actI)];
        const c = choices[Number(b.dataset.choice)];
        const out = el.querySelector(`[data-row-out="${b.dataset.rowAct}"]`);
        const fields = {};
        for (const [f, col] of proposes) if (value(row[col])) fields[f] = value(row[col]);
        if (c) fields[c.field] = c.value;
        b.disabled = true;
        ctx.intent({ kind: a.intent, control: a.control, about: a.about, fields, target: value(row[o.target]) }).then((res) => {
          b.disabled = false;
          if (out) out.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? (res?.done ? "Done." : "");
          if (out && res?.result?.claim) out.dataset.claim = res.result.claim;
        });
      });
    }
  };
  const actButtons = () =>
    acts.length && !perRow
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
      body = `<table class="kton-table" data-area-table data-count="${rows.length}"><thead><tr>${cols.map((c) => `<th>${esc(c)}</th>`).join("")}${perRow ? "<th></th>" : ""}</tr></thead><tbody>${rows
        .map(
          (r, i) =>
            `<tr data-row="${i}"${click && value(r[click]) ? ` class="kton-clickable" tabindex="0" data-target="${esc(value(r[click]))}"` : ""}>${cols
              .map((c) => (c === "stamp" ? `<td data-col="stamp"><span class="kton-stamp" data-stamp>${esc(value(r[c]))}</span></td>` : `<td data-col="${esc(c)}"${/^-?\d+(\.\d+)?%?$/.test(value(r[c])) ? " data-number" : ""}>${esc(shortly(value(r[c])))}</td>`))
              .join("")}${perRow ? `<td data-acts>${rowButtons(r, i)}</td>` : ""}</tr>`,
        )
        .join("")}</tbody></table>`;
    }
    const choices = Object.entries(refine)
      .filter(([, spec]) => Array.isArray(spec.values))
      .map(([k, spec]) => `<label class="kton-muted">${esc(k)} <select data-refine="${esc(k)}">${spec.values.map((v) => `<option${String(v) === cur[k] ? " selected" : ""}>${esc(String(v))}</option>`).join("")}</select></label>`)
      .join(" ");
    const page = paging
      ? `<div class="kton-pager" data-pager data-page="${back.length + 1}"><span class="kton-muted">Page ${back.length + 1} · rows ${back.length * pageSize + (rows.length ? 1 : 0)}–${back.length * pageSize + rows.length}</span>${back.length ? ` <button type="button" data-page-prev>Previous</button>` : ""}${pageSize && rows.length >= pageSize ? ` <button type="button" data-page-next>Next</button>` : ""}</div>`
      : "";
    el.innerHTML = `${choices ? `<div class="kton-refine" data-refines>${choices}</div>` : ""}${body}${page}${actButtons()}`;
    wireActs();
    wireRowActs(rows);
    for (const sel of el.querySelectorAll("select[data-refine]")) {
      sel.addEventListener("change", () => {
        cur[sel.dataset.refine] = sel.value;
        if (paging) delete cur[pageParam];
        back.length = 0;
        load();
      });
    }
    el.querySelector("[data-page-next]")?.addEventListener("click", () => {
      back.push(cur[pageParam] ?? "");
      cur[pageParam] = value(rows[rows.length - 1]?.[pageCol]);
      load();
    });
    el.querySelector("[data-page-prev]")?.addEventListener("click", () => {
      const prev = back.pop();
      if (prev) cur[pageParam] = prev;
      else delete cur[pageParam];
      load();
    });
    for (const tr of el.querySelectorAll("tr[data-target]")) {
      tr.addEventListener("click", () => {
        const row = rows[Number(tr.dataset.row)];
        if (o.pick) ctx.intent({ kind: "select", value: tr.dataset.target });
        else if (o.open) ctx.intent({ kind: "open", uri: tr.dataset.target, ...(row.claim ? { claim: value(row.claim) } : {}) });
      });
    }
  };

  const load = () => stamped(el, esc, ctx.request("query@1", Object.keys(cur).length ? { refine: { ...cur } } : {})).then(
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
  load();
  return { set() {}, destroy() {} };
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
