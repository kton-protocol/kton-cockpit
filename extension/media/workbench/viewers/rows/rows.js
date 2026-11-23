// rows: the workbench's one data block for the generic QM area. It shows a named query's answer as a
// table (or as one record's fields, option card), and knows nothing of any package: what to show comes from
// the plan's options, which the shell writes from the area declaration.
//   columns   the columns to show, in this order (default: every column the query answers)
//   pick      a row click emits `select` with this column's IRI (wired by the composition to a var)
//   open      a row click asks the host to open this column's IRI (with the row's claim): list → record
//   rules     keep only the rows whose ?rule is one of these (an area's findings.rules)
//   keys      keep only the rows whose ?key is one of these (an area's documents)
//   match     keep only the rows where one of these columns equals the subject's claim (a record's approvals:
//             ?record, the record claim the approval ?claim approves)
//   card      "yes": the first row as fields, not a table; a long-form answer (?field ?value, one row per field)
//             as all its rows' fields
//   caption   a line above the rows (a finding source's label)
//   ruleState rule ids this query checks: each is shown as passed, findings (n) or not run (the query failed)
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
// The look:
//   title     a heading above the table; labels "column|Header": header text
//   numeric   columns that are numbers (also: typed xsd numbers, and values that read as one): right-aligned, tabular
//   group     a column: rows grouped under a header row per value (query order kept within a group), the column itself
//             not shown; groupLabels "value|Label"; groupSum a column summed in the header ("4 areas · 84 unsettled")
//   range     "rate,low,high,n": the rate cell is the value with a mini range bar on the scale "lo,hi,target" (scale),
//             the low column shows "low–high", n right-aligned; measurable a column: a row whose value is not "true"
//             reads "too few to measure" (muted, no bar)
//   total     a column summed in a footer row
//   short     columns shown short: a test id "repo:dir/File.kt#name" as "File.kt · name", an IRI with a label column
//             (<column>Label) as its label; the full value stays in the tooltip
//   evidence  a column (or a list of columns, e.g. "cites": what a text cites, added by the shell's option cite) of packed artifacts (assertion, kind, sha256, run, name, mediaType, bytes, site, text[, thumb] per
//             line): assertion screenshots as thumbnails in assertion order (file:line and the assertion in the tooltip;
//             a click opens it full size, hash-checked), the error-context and the trace as links, "trace removed by
//             the token scan" (traceNote: a column saying so), "none" where nothing exists
//   groupNotes a column whose " | "-separated items repeat across a group's rows (e.g. the lane harness changes of a run): listed
//             once under the group's header; each row shows a small marker with the count and the items in its tooltip
//   details   "column|Label": an expandable row detail (in the first cell) with these fields; with actsIn "details" the
//             row's acts go there too
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
  el.innerHTML = `${skeleton("lines")}`;

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
          announceAct(res, ctx.use.key);
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
          announceAct(r, ctx.use.key);
        });
      });
    }
  };

  const caption = () => (o.caption ? `<p class="kton-muted" data-area-caption>${esc(o.caption)}</p>` : "");
  // one line per rule this query checks: passed, findings (n), or not run (why)
  const ruleStates = (rows, why) =>
    o.ruleState && o.ruleState.length
      ? `<ul data-rule-states>${o.ruleState
          .map((id) => {
            const n = rows ? rows.filter((x) => value(x.rule) === id).length : 0;
            const state = !rows ? "not-run" : n ? "findings" : "passed";
            const text = !rows ? `not run (${why})` : n ? `findings (${n})` : "passed";
            return `<li data-rule-state="${esc(id)}" data-state="${state}" data-n="${n}"><strong>${esc(id)}</strong>: ${esc(text)}</li>`;
          })
          .join("")}</ul>`
      : "";
  const short = (v) => v.replace(/^.*[#/]/, "") || v;
  const draw = (vars, rows) => {
    const stampMode = o.stamp === "section" || o.stamp === "hide" ? o.stamp : "column";
    const cols = (o.columns && o.columns.length ? o.columns : vars.filter((c) => !(c.endsWith("Label") && vars.includes(c.slice(0, -5))))).filter((c) => vars.includes(c)).filter((c) => !(stampMode !== "column" && c === "stamp") && c !== o.group);
    let body;
    if (!rows.length) body = `${emptyBox(esc, o.emptyText || "No rows.", o.emptyHow)}`;
    else if (o.card === "yes" && vars.includes("field") && vars.includes("value")) {
      // long form: one row per field (a field may repeat: a list of values)
      body = `<dl class="kton-card" data-area-card data-count="${rows.length}">${rows
        .map((r) => `<dt>${esc(short(value(r.field)))}</dt><dd data-col="${esc(short(value(r.field)))}">${esc(shortly(value(r.value)))}</dd>`)
        .join("")}</dl>`;
    } else if (o.card === "yes") {
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
    el.innerHTML = `${o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : ""}${caption()}${ruleStates(rows)}${choices ? `<div class="kton-refine" data-refines>${choices}</div>` : ""}${body}${page}${actButtons()}`;
    for (const mb of el.querySelectorAll(".mb[data-lo]")) for (const k of ["lo", "hi", "pt", "t"]) mb.style.setProperty(`--${k}`, mb.dataset[k]);
    for (const b of el.querySelectorAll("button[data-art]")) {
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        const [run, name, sha256, mediaType] = JSON.parse(b.dataset.art);
        b.disabled = true;
        ctx.intent({ kind: "open", artifact: { run, name, sha256, ...(mediaType ? { mediaType } : {}) } }).then((res) => {
          b.disabled = false;
          if (res?.refused) b.title = `Not opened: ${res.refused.why ?? res.refused}`;
        });
      });
    }
    for (const d of el.querySelectorAll("details.rowmore")) d.addEventListener("click", (ev) => ev.stopPropagation());
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

  const load = () => stamped(el, esc, ctx.request("query@1", Object.keys(cur).length ? { refine: { ...cur } } : {}), ctx.use.key).then(
    (r) => {
      const vars = r?.head?.vars ?? [];
      let rows = r?.results?.bindings ?? [];
      if (o.rules) rows = rows.filter((x) => o.rules.includes(value(x.rule)));
      if (o.keys) rows = rows.filter((x) => o.keys.includes(value(x.key)));
      if (o.match) rows = subjectClaim ? rows.filter((x) => [].concat(o.match).some((c) => value(x[c]) === subjectClaim)) : [];
      if (o.atLeast) {
        const [col, min] = o.atLeast.split(":");
        rows = rows.filter((x) => Number(value(x[col])) >= Number(min));
      }
      draw(vars, rows);
    },
    (e) => {
      el.innerHTML = `${caption()}${ruleStates(null, "the query failed")}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  load();
  return { set() {}, destroy() {} };
}

/** One primary per object: the first sign act is primary, every other act secondary (record included). */
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

/** A test id "repo:dir/File.kt#name|ids" → "File.kt · name"; an IRI → its last segment; else as it is (decoded). */
export function shortId(v) {
  const s = String(v ?? "");
  if (/^https?:\/\//.test(s)) {
    if (/^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(s)) return `sha256:${s.slice(19, 31)}…`;
    const tail = s.replace(/[#/]+$/, "").split(/[#/]/).pop() ?? s;
    return dec(tail) || s;
  }
  const m = /^(?:lane:)?[\w.-]+:(.+)$/.exec(s);
  if (!m) return s;
  const [body] = m[1].split("|");
  const h = body.indexOf("#");
  const file = dec((h < 0 ? body : body.slice(0, h)).split("/").pop() ?? body);
  return h < 0 ? file : `${file} · ${body.slice(h + 1)}`;
}
const dec = (x) => {
  try {
    return decodeURIComponent(x);
  } catch {
    return x;
  }
};

/** The items that repeat in a group (" | "-separated), in first-seen order, each once. */
export function groupNotes(values) {
  const out = [];
  for (const v of values) for (const x of String(v ?? "").split(" | ").map((y) => y.trim()).filter(Boolean)) if (!out.includes(x)) out.push(x);
  return out;
}
const notesLabel = (col) => (col === "harness" ? "Lane harness changes" : col);
function noteMarker(esc, v) {
  const items = groupNotes([v]);
  return items.length ? ` <span class="badge note-marker" tabindex="0" data-tip="${esc(items.join("\n"))}" data-notes="${items.length}">${items.length === 1 ? "1 harness change" : `${items.length} harness changes`}</span>` : "";
}

/** The packed evidence column → entries sorted by assertion: {assertion, kind, sha256, run, name, mediaType, bytes, site, text, thumb}. */
export function parseEvidence(packed) {
  return String(packed ?? "")
    .split("\n")
    .filter((l) => l.trim() !== "")
    .map((l) => {
      const [assertion, kind, sha256, run, name, mediaType, bytes, site, text, thumb] = l.split("\t");
      return { assertion, kind, sha256, run, name, mediaType, bytes, site, text, thumb };
    })
    .filter((e) => e.kind && /^sha256:[0-9a-f]{64}$/.test(e.sha256 ?? ""))
    .sort((a, b) => (Number(a.assertion) || 1e9) - (Number(b.assertion) || 1e9));
}

function evidenceCell(esc, packed, traceNote) {
  const es = parseEvidence(packed);
  const art = (e) => esc(JSON.stringify([e.run, e.name, e.sha256, e.mediaType || ""]));
  const shots = es.filter((e) => e.kind === "assertion-screenshot");
  const thumbs = shots
    .map((e, i) => {
      const tip = `${e.site || "assertion"}${e.text ? ` — ${e.text}` : ""}`;
      const img = e.thumb && /^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/.test(e.thumb) ? `<img src="${e.thumb}" alt="">` : `<span class="n">${esc(e.assertion || String(i + 1))}</span>`;
      return `<button type="button" class="thumb" data-art="${art(e)}" data-tip="${esc(tip)}" aria-label="Assertion ${esc(e.assertion || String(i + 1))}: ${esc(tip)}">${img}${e.thumb ? `<span class="sr-only">assertion ${esc(e.assertion || String(i + 1))}</span>` : ""}</button>`;
    })
    .join("");
  const link = (e, label) => `<button type="button" class="ghost small" data-art="${art(e)}" data-tip="${esc(`${e.run}/${e.name}`)}">${esc(label)}</button>`;
  const ctx = es.filter((e) => e.kind === "error-context").map((e) => link(e, "error-context")).join("");
  const shot = es.filter((e) => e.kind === "screenshot").map((e) => link(e, "screenshot")).join("");
  const trace = es.filter((e) => e.kind === "trace").map((e) => link(e, "trace")).join("") || (traceNote ? `<span class="muted">${esc(traceNote)}</span>` : "");
  const other = es.filter((e) => !["assertion-screenshot", "error-context", "screenshot", "trace", "assertions"].includes(e.kind)).map((e) => link(e, e.kind === "cited" ? String(e.name ?? "").split("/").pop() || "cited" : e.kind)).join("");
  const any = thumbs || ctx || shot || trace || other;
  return any ? `<div class="ev-thumbs">${thumbs}</div><div class="ev-links">${shot}${ctx}${trace}${other}</div>` : `<span class="muted">none</span>`;
}

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
  // the marker sits in the second shown column (the result), the details in the first
  const k1 = (c) => cols[1] === c;
  let curRow = 0; // the row being drawn (its acts are about it)
  const detailsOf = (r) => {
    const f = (o.details ?? []).map((d) => (d.includes("|") ? [d.slice(0, d.indexOf("|")), d.slice(d.indexOf("|") + 1)] : [d, d])).filter(([col]) => value(r[col]));
    const acts = o.actsIn === "details" && perRow ? `<div class="actions">${rowButtons(r, curRow)}</div>` : "";
    return `<details class="rowmore"><summary>Details</summary><dl class="kv">${f.map(([col, l]) => `<dt>${esc(l)}</dt><dd data-detail="${esc(col)}">${esc(value(r[col]))}</dd>`).join("")}</dl>${acts}</details>`;
  };
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
    if (o.evidence && (Array.isArray(o.evidence) ? o.evidence.includes(c) : c === o.evidence)) return `<td data-col="${esc(c)}" class="ev-cell">${evidenceCell(esc, value(r[c]), o.traceNote ? value(r[o.traceNote]) : "")}${lead}</td>`;
    const num = numeric.has(c) || NUM_RE.test(value(r[c]));
    const full = value(r[c]);
    const label = value(r[`${c}Label`]);
    // a column with a label beside it (<column>Label) shows the label, the IRI in the tooltip
    const shown = label || ((o.short ?? []).includes(c) ? shortId(full) : shortly(full));
    const tip = shown !== full ? ` data-tip="${esc(full)}"` : "";
    const marker = o.groupNotes && k1(c) ? noteMarker(esc, value(r[o.groupNotes])) : "";
    const more = first && (o.details ?? []).length ? detailsOf(r) : "";
    return `<td data-col="${esc(c)}"${num ? ' class="r" data-number' : ""}${o.group ? " data-ind" : ""}><span${tip}>${esc(shown)}</span>${marker}${more}${lead}</td>`;
  };
  let i = 0;
  const tr = (r) => {
    const n = i++;
    curRow = n;
    const inline = perRow && o.actsIn !== "details";
    return `<tr data-row="${n}"${click && value(r[click]) ? ` class="kton-clickable" tabindex="0" data-target="${esc(value(r[click]))}"` : ""}>${cols.map((c, k) => cell(r, c, k === 0)).join("")}${inline ? `<td data-acts>${rowButtons(r, n)}</td>` : ""}</tr>`;
  };
  const inlineActs = perRow && o.actsIn !== "details";
  const width = cols.length + (inlineActs ? 1 : 0);
  let tbody;
  if (o.group) {
    const glabels = new Map((o.groupLabels ?? []).map((l) => (l.includes("|") ? [l.slice(0, l.indexOf("|")), l.slice(l.indexOf("|") + 1)] : [l, l])));
    const unit = label(cols[0] ?? "").toLowerCase();
    const sumName = o.groupSum ? label(o.groupSum).toLowerCase() : "";
    const ordered = rows.slice();
    const groups = groupRows(ordered, o.group, o.groupSum);
    tbody = groups
      .map((g) => {
        const name = glabels.get(g.group) ?? (/^[a-z][a-z -]*$/.test(g.group) ? g.group.charAt(0).toUpperCase() + g.group.slice(1) : g.group || "—");
        const meta = `${g.rows.length} ${unit}${g.rows.length === 1 ? "" : "s"}${o.groupSum ? ` · ${g.sum.toLocaleString("en")} ${sumName}` : ""}`;
        const notes = o.groupNotes ? groupNotes(g.rows.map((x) => value(x[o.groupNotes]))) : [];
        const list = notes.length ? `<div class="grp-notes"><span class="muted">${esc(notesLabel(o.groupNotes))} (${notes.length}), the same for every row below:</span><ol>${notes.map((x) => `<li>${esc(x)}</li>`).join("")}</ol></div>` : "";
        return `<tr class="grp" data-group="${esc(g.group)}"><th colspan="${width}" scope="colgroup">${esc(name)} <span class="muted">${esc(meta)}</span>${list}</th></tr>${g.rows.map(tr).join("")}`;
      })
      .join("");
    rows = groups.flatMap((g) => g.rows); // the row index follows the drawn order
  } else tbody = rows.map(tr).join("");
  const foot = o.total && cols.includes(o.total)
    ? `<tfoot><tr>${cols.map((c, k) => (k === 0 ? '<th scope="row">Total</th>' : c === o.total ? `<th class="r" data-total>${esc(rows.reduce((a, r) => a + (Number(value(r[c])) || 0), 0).toLocaleString("en"))}</th>` : "<th></th>")).join("")}${perRow ? "<th></th>" : ""}</tr></tfoot>`
    : "";
  return { html: `<div class="tbl-wrap"><table class="kton-table t" data-area-table data-count="${rows.length}"><thead><tr>${cols.map(th).join("")}${inlineActs ? '<th><span class="sr-only">Actions</span></th>' : ""}</tr></thead><tbody>${tbody}</tbody>${foot}</table></div>`, rows };
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
