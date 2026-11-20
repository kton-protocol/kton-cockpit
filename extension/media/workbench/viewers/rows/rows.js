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
// The look (DESIGN.md §5 rows):
//   title     a heading above the table; labels "column|Header": header text
//   numeric   columns that are numbers (also: typed xsd numbers, and values that read as one): right-aligned, tabular
//   group     a column: rows grouped under a header row per value (query order kept within a group), the column itself
//             not shown; groupLabels "value|Label"; groupSum a column summed in the header ("4 areas · 84 unsettled")
//   range     "rate,low,high,n": the rate cell is the value with a mini range bar on the scale "lo,hi,target" (scale),
//             the low column shows "low–high", n right-aligned; measurable a column: a row whose value is not "true"
//             reads "too few to measure" (muted, no bar)
//   total     a column summed in a footer row
//   stamp     column (default: a stamp column when the query answers one) | section (no column: each row keeps its
//             stamp, hidden when the part's source chip names it) | hide
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
          .map(({ a, ai, c, ci }) => `<button type="button" class="small" data-row-act="${i}" data-act-i="${ai}" data-choice="${ci}" data-testid="row-${i}-${esc(c ? c.value : a.control?.name ?? a.intent)}">${esc(c ? c.label : a.label ?? a.intent)}</button>`)
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
          .map((a, i) => `<button type="button" class="${actClasses(acts)[i]}" data-act="${i}" data-testid="act-${esc(a.control?.name ?? a.intent)}">${esc(a.label ?? a.intent)}</button>`)
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
    const stampMode = o.stamp === "section" || o.stamp === "hide" ? o.stamp : "column";
    const cols = (o.columns && o.columns.length ? o.columns : vars).filter((c) => vars.includes(c)).filter((c) => !(stampMode !== "column" && c === "stamp") && c !== o.group);
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
      const t = table(rows, cols, { o, esc, value, shortly, click, perRow, rowButtons });
      body = t.html;
      rows = t.rows; // grouped: the drawn order, which data-row indexes
    }
    const choices = Object.entries(refine)
      .filter(([, spec]) => Array.isArray(spec.values))
      .map(([k, spec]) => `<label class="kton-muted">${esc(k)} <select data-refine="${esc(k)}">${spec.values.map((v) => `<option${String(v) === cur[k] ? " selected" : ""}>${esc(String(v))}</option>`).join("")}</select></label>`)
      .join(" ");
    const page = paging
      ? `<div class="kton-pager" data-pager data-page="${back.length + 1}"><span class="kton-muted">Page ${back.length + 1} · rows ${back.length * pageSize + (rows.length ? 1 : 0)}–${back.length * pageSize + rows.length}</span>${back.length ? ` <button type="button" class="small" data-page-prev>Previous</button>` : ""}${pageSize && rows.length >= pageSize ? ` <button type="button" class="small" data-page-next>Next</button>` : ""}</div>`
      : "";
    el.innerHTML = `${o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : ""}${choices ? `<div class="kton-refine" data-refines>${choices}</div>` : ""}${body}${page}${actButtons()}`;
    for (const mb of el.querySelectorAll(".mb[data-lo]")) for (const k of ["lo", "hi", "pt", "t"]) mb.style.setProperty(`--${k}`, mb.dataset[k]);
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

/** One primary per object (DESIGN.md §5 acts): the first sign act is primary, every other act secondary (record included). */
export function actClasses(acts) {
  const first = acts.findIndex((a) => a.intent === "sign");
  return acts.map((_, i) => (i === first ? "primary" : "secondary"));
}

const NUM_RE = /^-?\d+(\.\d+)?%?$/;
const XSD_NUM = /XMLSchema#(integer|decimal|double|float|int|long|short|nonNegativeInteger|positiveInteger)$/;

/** Is a column numeric: named in `numeric`, typed as an xsd number in the rows, or every value reads as one. */
export function isNumeric(col, rows, numeric = []) {
  if (numeric.includes(col)) return true;
  const vals = rows.map((r) => r[col]).filter((b) => b && b.value !== "");
  return vals.length > 0 && vals.every((b) => XSD_NUM.test(b.datatype ?? "") || NUM_RE.test(b.value));
}

/** Rows grouped by a column's value, in first-seen order; query order kept within a group; sum of a column per group. */
export function groupRows(rows, col, sumCol) {
  const out = [];
  const at = new Map();
  for (const r of rows) {
    const g = r[col]?.value ?? "";
    if (!at.has(g)) {
      at.set(g, out.length);
      out.push({ group: g, rows: [], sum: 0 });
    }
    const e = out[at.get(g)];
    e.rows.push(r);
    if (sumCol) e.sum += Number(r[sumCol]?.value ?? 0) || 0;
  }
  return out;
}

/** "rate,low,high,n" and "lo,hi,target" → the range spec, or undefined. */
export function rangeSpec(range, scale) {
  if (typeof range !== "string") return undefined;
  const [rate, low, high, n] = range.split(",").map((x) => x.trim());
  const sc = String(scale ?? "0,20,5").split(",").map(Number);
  if (!rate || !low || !high || !(sc[1] > sc[0])) return undefined;
  return { rate, low, high, n, lo: sc[0], hi: sc[1], target: Number.isFinite(sc[2]) ? sc[2] : undefined };
}

/** A value's place on the mini bar, in percent, clamped. */
export function barPos(x, lo, hi) {
  return Math.round(Math.min(100, Math.max(0, ((Number(x) - lo) / (hi - lo)) * 100)) * 100) / 100;
}

const pct = (x) => `${Number(x).toFixed(1)} %`;

function table(rows, cols, { o, esc, value, shortly, click, perRow, rowButtons }) {
  const labels = new Map((o.labels ?? []).map((l) => (l.includes("|") ? [l.slice(0, l.indexOf("|")), l.slice(l.indexOf("|") + 1)] : [l, l])));
  const rg = rangeSpec(o.range, o.scale);
  const numeric = new Set(cols.filter((c) => isNumeric(c, rows, o.numeric ?? [])));
  if (rg) for (const c of [rg.rate, rg.low, rg.n]) numeric.add(c);
  const section = o.stamp === "section" || o.stamp === "hide";
  const label = (c) => labels.get(c) ?? c;
  const th = (c) => {
    if (rg && c === rg.rate) return `<th scope="col" data-head="${esc(c)}">${esc(labels.get(c) ?? "Rate")} <span class="muted">(scale ${rg.lo}–${rg.hi} %${rg.target !== undefined ? `, dashed = target ${rg.target} %` : ""})</span></th>`;
    if (rg && c === rg.low) return `<th scope="col" class="r" data-head="${esc(c)}">${esc(labels.get(c) ?? "Range %")}</th>`;
    return `<th scope="col"${numeric.has(c) ? ' class="r"' : ""} data-head="${esc(c)}">${esc(label(c))}</th>`;
  };
  const stampSpan = (r) => {
    const st = value(r.stamp);
    if (!section || o.stamp === "hide" || !("stamp" in r)) return "";
    return st ? `<span class="kton-stamp wb-rowstamp" data-stamp="${esc(st)}"${value(r.stampLabel) ? ` data-stamp-label="${esc(value(r.stampLabel))}"` : ""}${value(r.stampFull) ? ` data-stamp-full="${esc(value(r.stampFull))}"` : ""}${value(r.stampScope) ? ` data-stamp-scope="${esc(value(r.stampScope))}"` : ""}>${esc(st)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
  };
  const measurable = (r) => !o.measurable || value(r[o.measurable]) === "true";
  const cell = (r, c, first) => {
    const lead = first ? stampSpan(r) : "";
    if (c === "stamp") return `<td data-col="stamp"><span class="kton-stamp" data-stamp="${esc(value(r[c]))}">${esc(value(r[c]))}</span></td>`;
    if (rg && c === rg.rate) {
      if (!value(r[rg.rate])) return `<td data-col="${esc(c)}" class="few">not measured${lead}</td>`;
      if (!measurable(r)) return `<td data-col="${esc(c)}" class="few">too few to measure${lead}</td>`;
      const lo = barPos(value(r[rg.low]), rg.lo, rg.hi), hi = barPos(value(r[rg.high]), rg.lo, rg.hi), pt = barPos(value(r[rg.rate]), rg.lo, rg.hi);
      const t = rg.target !== undefined ? barPos(rg.target, rg.lo, rg.hi) : 100;
      const above = rg.target !== undefined && Number(value(r[rg.rate])) > rg.target;
      return `<td data-col="${esc(c)}" data-number><div class="ratecell"><span class="rv${above ? " above" : ""}">${esc(pct(value(r[rg.rate])))}</span><span class="mb" aria-hidden="true" data-lo="${lo}" data-hi="${hi}" data-pt="${pt}" data-t="${t}"><i class="band"></i><i class="over"></i>${rg.target !== undefined ? '<i class="tg"></i>' : ""}<i class="pt${above ? " above" : ""}"></i></span></div>${lead}</td>`;
    }
    if (rg && c === rg.low) return `<td data-col="${esc(c)}" class="r muted">${value(r[rg.low]) && measurable(r) ? `${esc(pct(value(r[rg.low])).slice(0, -2))}–${esc(pct(value(r[rg.high])).slice(0, -2))}` : ""}${lead}</td>`;
    if (rg && c === rg.n) return `<td data-col="${esc(c)}" class="r${measurable(r) ? "" : " muted"}" data-number>${esc(value(r[c]))}${lead}</td>`;
    const num = numeric.has(c) || NUM_RE.test(value(r[c]));
    return `<td data-col="${esc(c)}"${num ? ' class="r" data-number' : ""}${o.group ? ' data-ind' : ""}>${esc(shortly(value(r[c])))}${lead}</td>`;
  };
  let i = 0;
  const tr = (r) => {
    const n = i++;
    return `<tr data-row="${n}"${click && value(r[click]) ? ` class="kton-clickable" tabindex="0" data-target="${esc(value(r[click]))}"` : ""}>${cols.map((c, k) => cell(r, c, k === 0)).join("")}${perRow ? `<td data-acts>${rowButtons(r, n)}</td>` : ""}</tr>`;
  };
  const width = cols.length + (perRow ? 1 : 0);
  let tbody;
  if (o.group) {
    const glabels = new Map((o.groupLabels ?? []).map((l) => (l.includes("|") ? [l.slice(0, l.indexOf("|")), l.slice(l.indexOf("|") + 1)] : [l, l])));
    const unit = label(cols[0] ?? "").toLowerCase();
    const sumName = o.groupSum ? label(o.groupSum).toLowerCase() : "";
    const ordered = rows.slice();
    const groups = groupRows(ordered, o.group, o.groupSum);
    tbody = groups
      .map((g) => {
        const name = glabels.get(g.group) ?? (g.group ? g.group.charAt(0).toUpperCase() + g.group.slice(1) : "—");
        const meta = `${g.rows.length} ${unit}${g.rows.length === 1 ? "" : "s"}${o.groupSum ? ` · ${g.sum.toLocaleString("en")} ${sumName}` : ""}`;
        return `<tr class="grp" data-group="${esc(g.group)}"><th colspan="${width}" scope="colgroup">${esc(name)} <span class="muted">${esc(meta)}</span></th></tr>${g.rows.map(tr).join("")}`;
      })
      .join("");
    rows = groups.flatMap((g) => g.rows); // the row index follows the drawn order
  } else tbody = rows.map(tr).join("");
  const foot = o.total && cols.includes(o.total)
    ? `<tfoot><tr>${cols.map((c, k) => (k === 0 ? '<th scope="row">Total</th>' : c === o.total ? `<th class="r" data-total>${esc(rows.reduce((a, r) => a + (Number(value(r[c])) || 0), 0).toLocaleString("en"))}</th>` : "<th></th>")).join("")}${perRow ? "<th></th>" : ""}</tr></tfoot>`
    : "";
  return { html: `<div class="tbl-wrap"><table class="kton-table t" data-area-table data-count="${rows.length}"><thead><tr>${cols.map(th).join("")}${perRow ? '<th><span class="sr-only">Actions</span></th>' : ""}</tr></thead><tbody>${tbody}</tbody>${foot}</table></div>`, rows };
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
