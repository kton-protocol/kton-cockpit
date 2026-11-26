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
//   evidence  a column (e.g. "cites": what a text cites, added by the shell's option cite) of packed artifacts (assertion, kind, sha256, run, name, mediaType, bytes, site, text[, thumb] per
//             line): assertion screenshots as thumbnails in assertion order (file:line and the assertion in the tooltip;
//             a click opens it full size, hash-checked), the error-context and the trace as links, "trace removed by
//             the token scan" (traceNote: a column saying so), "none" where nothing exists
//   groupNotes a column whose " | "-separated items repeat across a group's rows (e.g. the lane harness changes of a run): listed
//             once under the group's header; each row shows a small marker with the count and the items in its tooltip
//   cols      the columns on line 1 of a row (about 3 to 5); no tier option at all: the first 4 shown columns
//   line2     "column|Label" (the label is optional): a muted second line under line 1, "label value · label value",
//             wrapping; a row never has a third line
//   details   "column|Label": the columns behind a chevron button in the first cell (aria-expanded) that opens a row
//             holding a definition list; with actsIn "details" the row's acts go there too. No tier option: the
//             shown columns beyond the 4th. A column is in one tier only (cols, then line2, then details).
//             Below 560 px the cols beyond the first 2 move into line2. Nothing scrolls sideways.
//   actsAt    "last" (default) | "first": the column of the row's acts; it fits its buttons, at most min(14rem, 40%),
//             the buttons wrap, and it spans both lines of the row
//   a hex value of 16 or more characters (a hash) is shown as its first 8, the full value in the tooltip; a click or
//   Enter on it copies the full value
//   requireReviewed "[control:]repo|head|reviews[|base[|when]]" (when: a row column; only rows with a value there): the shell finds the commits a rerun at head would contain since
//             base (a query naming the commit the last run ran at; without one, the newest ancestor with an approval)
//             (each needs its own approval from the reviews query); while one has none, that control's act
//             is drawn disabled with the reason, and the commits are listed above the table
//   changesBetween "repo|from|to|path": the row's columns naming a mirrored repository, two commits and a test file; the
//             shell adds `changes` (the commits between them that touched the test or a helper it imports, each opening
//             its diff read-only, with its review from changesReviews "<query>": ?approvedCommit ?by ?when) and
//             `changesNote`; list "changes" among the columns to show them
//   stamp     column (default: a stamp column when the query answers one) | section (no column: each row keeps its
//             stamp, hidden when the part's source chip names it) | hide
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.blockActs ?? ctx.use.acts ?? [];
  const subjectClaim = ctx.subject?.claim ? `https://kton.dev/o/${String(ctx.subject.claim).replace(/^sha256:/, "")}` : null;
  const value = (b) => (b && typeof b.value === "string" ? b.value : "");
  const shortly = (v) => (/^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(v) ? `sha256:${v.slice(19, 27)}…` : v);
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
  // requireReviewed (the shell's option): an act whose rerun contains a commit without a review is drawn disabled, with why
  const [blockedCtl, blockedWhy] = typeof o.blocked === "string" && o.blocked.includes("|") ? [o.blocked.slice(0, o.blocked.indexOf("|")), o.blocked.slice(o.blocked.indexOf("|") + 1)] : [undefined, undefined];
  // with blockedWhen (the rule's `when` column), only a row with a value there is under the rule
  const blockedFor = (a, r) => (blockedWhy && (blockedCtl === "*" || a.control?.name === blockedCtl) && (!o.blockedWhen || !r || value(r[o.blockedWhen])) ? blockedWhy : "");
  const reviewedNote = () =>
    Array.isArray(o.reviewed) && o.reviewed.length
      ? `<details class="reviewed" data-reviewed${blockedWhy ? " open" : ""}><summary>${blockedWhy ? `<b data-blocked-why>${esc(blockedWhy)}</b>` : esc(o.reviewed[0])}</summary><ul>${o.reviewed.map((l) => `<li>${esc(l)}</li>`).join("")}</ul></details>`
      : "";
  const rowButtons = (r, i) =>
    value(r[o.target])
      ? acts
          .flatMap((a, ai) => {
            const own = choicesOf(a);
            return own.length ? own.map(({ c, ci }) => ({ a, ai, c, ci })) : [{ a, ai, c: null, ci: -1 }];
          })
          .map(({ a, ai, c, ci }) => {
            const why = blockedFor(a, r);
            return `<button type="button" class="small" data-row-act="${i}" data-act-i="${ai}" data-choice="${ci}" data-testid="row-${i}-${esc(c ? c.value : a.control?.name ?? a.intent)}"${why ? ` disabled data-blocked data-tip="${esc(why)}"` : ""}>${esc(c ? c.label : a.label ?? a.intent)}</button>`;
          })
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
          .map((a, i) => `<button type="button" class="${actClasses(acts)[i]}" data-act="${i}" data-testid="act-${esc(a.control?.name ?? a.intent)}"${blockedFor(a) ? ` disabled data-blocked data-tip="${esc(blockedFor(a))}"` : ""}>${esc(a.label ?? a.intent)}</button>`)
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
  // narrow: the block is under 560 px wide (a resize observer redraws when that changes); open: the rows whose details are open
  let narrow = false;
  const openRows = new Set();
  let lastDraw = null;
  const measure = () => (el.clientWidth > 0 ? el.clientWidth < NARROW_PX : narrow);
  const draw = (vars, rows) => {
    lastDraw = [vars, rows];
    narrow = measure();
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
      const t = table(rows, cols, { o, esc, value, shortly, click, perRow, rowButtons, vars, narrow, open: openRows });
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
    el.innerHTML = `${o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : ""}${reviewedNote()}${caption()}${ruleStates(rows)}${choices ? `<div class="kton-refine" data-refines>${choices}</div>` : ""}${body}${page}${actButtons()}`;
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
    for (const b of el.querySelectorAll("button[data-src]")) {
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        const [repo, commit] = JSON.parse(b.dataset.src);
        b.disabled = true;
        ctx.intent({ kind: "open", source: { repo, commit, diff: true } }).then((res) => {
          b.disabled = false;
          if (res?.refused) b.title = `Not opened: ${res.refused.why ?? res.refused}`;
        });
      });
    }
    for (const b of el.querySelectorAll("button[data-disc]")) {
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        const n = Number(b.dataset.disc);
        const open = b.getAttribute("aria-expanded") !== "true";
        b.setAttribute("aria-expanded", open ? "true" : "false");
        const det = el.querySelector(`tr[data-det="${n}"]`);
        if (det) det.hidden = !open;
        if (open) openRows.add(n);
        else openRows.delete(n);
      });
    }
    for (const h of el.querySelectorAll(".hash[data-copy]")) {
      const copy = (ev) => {
        ev.stopPropagation();
        try {
          globalThis.navigator?.clipboard?.writeText(h.dataset.copy);
        } catch {}
      };
      h.addEventListener("click", copy);
      h.addEventListener("keydown", (ev) => {
        if (ev.key === "Enter" || ev.key === " ") {
          ev.preventDefault();
          copy(ev);
        }
      });
    }
    for (const th of el.querySelectorAll("th[data-actw]")) th.style.setProperty("width", `min(${th.dataset.actw}rem, 14rem, 40%)`);
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
        const row = rows[Number(tr.dataset.row ?? tr.dataset.l2)];
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
  const ro = typeof globalThis.ResizeObserver === "function" ? new globalThis.ResizeObserver(() => lastDraw && measure() !== narrow && draw(lastDraw[0], lastDraw[1])) : null;
  if (ro) ro.observe(el);
  return { set() {}, destroy() { ro?.disconnect(); } };
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
    if (/^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(s)) return `sha256:${s.slice(19, 27)}…`;
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

/** The commits a row's changes column lists (the shell's option changesBetween): each opens its diff read-only. */
export function changesCell(esc, json) {
  let list = [];
  try {
    list = JSON.parse(json || "[]");
  } catch {
    list = [];
  }
  if (!Array.isArray(list) || !list.length) return "";
  return `<ul class="changes">${list
    .map((x) => {
      const touched = (x.touched ?? []).map((t) => `${String(t.path).split("/").pop()} (${t.via})`).join("; ");
      const reviewed = /^approved by /.test(x.review ?? "");
      return `<li data-commit="${esc(x.sha)}"><button type="button" class="ghost small mono" data-src="${esc(JSON.stringify([x.repo, x.sha]))}" data-tip="${esc(`${x.sha} · open the change read-only`)}">${esc(String(x.sha).slice(0, 7))}</button> ${esc(x.subject ?? "")} <span class="muted">· ${esc(x.author ?? "")} · ${esc(String(x.date ?? "").slice(0, 10))}</span><div class="muted" data-touched>touched: ${esc(touched)}</div><span class="pill ${reviewed ? "ok" : "warn"}" data-review>${esc(x.review ?? "")}</span></li>`;
    })
    .join("")}</ul>`;
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

const NARROW_PX = 560;
const colName = (s) => String(s).split("|")[0];
const colLabel = (s) => (String(s).includes("|") ? String(s).slice(String(s).indexOf("|") + 1) : undefined);
const entry = (s) => (colLabel(s) === undefined ? { col: colName(s) } : { col: colName(s), label: colLabel(s) });

/**
 * The three tiers of a table's columns (DESIGN rows, "Fitting a wide table"): `cols` on line 1, `line2` as a muted second
 * line, `details` behind the disclosure. `shown` are the columns the table would show, `known` says which columns exist.
 * No tier option: the first 4 shown columns go to line 1, the rest to the details. A column is in one tier only (cols,
 * then line2, then details). `narrow`: the line 1 columns beyond the first 2 move to the front of line 2.
 * Returns { line1: [col], line2: [{col, label?}], details: [{col, label?}] }.
 */
export function tiers(shown, o = {}, narrow = false, known = () => true) {
  const list = (x) => (Array.isArray(x) ? x : x === undefined || x === null || x === "" ? [] : [x]).map(entry).filter((e) => e.col && known(e.col));
  const explicit = o.cols !== undefined || o.line2 !== undefined || o.details !== undefined;
  let line1;
  let line2 = list(o.line2);
  let details = list(o.details);
  if (!explicit) {
    line1 = shown.slice(0, 4);
    details = shown.slice(4).map((col) => ({ col }));
  } else {
    const taken = new Set([...line2, ...details].map((e) => e.col));
    line1 = o.cols !== undefined ? list(o.cols).map((e) => e.col) : shown.filter((c) => !taken.has(c));
  }
  const seen = new Set();
  const once = (e) => (seen.has(e.col) ? false : (seen.add(e.col), true));
  line1 = line1.filter((c) => once({ col: c }));
  line2 = line2.filter(once);
  details = details.filter(once);
  if (!line1.length && shown.length) line1 = [shown[0]];
  if (narrow && line1.length > 2) {
    line2 = [...line1.slice(2).map((col) => ({ col })), ...line2];
    line1 = line1.slice(0, 2);
  }
  return { line1, line2, details };
}

/** A hex string of 16 or more characters (a hash or a commit id, with or without "sha256:"). */
export const isLongHex = (v) => /^(sha256:)?[0-9a-f]{16,}$/i.test(String(v));
/** Its short form: the first 8 hex characters and an ellipsis. */
export const shortHex = (v) => `${String(v).replace(/^(?:sha256:|https:\/\/kton\.dev\/o\/)/, "").slice(0, 8)}…`;

let tableSeq = 0;
function table(rows, cols, { o, esc, value, shortly, click, perRow, rowButtons, vars = cols, narrow = false, open = new Set() }) {
  const labels = new Map((o.labels ?? []).map((l) => (l.includes("|") ? [l.slice(0, l.indexOf("|")), l.slice(l.indexOf("|") + 1)] : [l, l])));
  const rg = rangeSpec(o.range, o.scale);
  const numeric = new Set(cols.filter((c) => isNumeric(c, rows, o.numeric ?? [])));
  if (rg) for (const c of [rg.rate, rg.low, rg.n]) numeric.add(c);
  const section = o.stamp === "section" || o.stamp === "hide";
  const label = (c) => labels.get(c) ?? c;
  const known = (c) => vars.includes(c) && c !== o.group && !(c === "stamp" && section) && !(c.endsWith("Label") && vars.includes(c.slice(0, -5)));
  const T = tiers(cols, o, narrow, known);
  const line1 = T.line1;
  const actsFirst = o.actsAt === "first";
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
  // the marker sits in the second shown column (the result)
  const k1 = (c) => line1[1] === c;
  // one column's value: the td's attributes after data-col, and its inner html (the same in a cell, a line 2 item, a detail)
  const part = (r, c, first) => {
    const lead = first ? stampSpan(r) : "";
    if (c === "stamp") return { attrs: "", inner: `<span class="kton-stamp" data-stamp="${esc(value(r[c]))}">${esc(value(r[c]))}</span>` };
    if (rg && c === rg.rate) {
      if (!value(r[rg.rate])) return { attrs: ' class="few"', inner: `not measured${lead}` };
      if (!measurable(r)) return { attrs: ' class="few"', inner: `too few to measure${lead}` };
      const lo = barPos(value(r[rg.low]), rg.lo, rg.hi), hi = barPos(value(r[rg.high]), rg.lo, rg.hi), pt = barPos(value(r[rg.rate]), rg.lo, rg.hi);
      const t = rg.target !== undefined ? barPos(rg.target, rg.lo, rg.hi) : 100;
      const above = rg.target !== undefined && Number(value(r[rg.rate])) > rg.target;
      return { attrs: " data-number", inner: `<div class="ratecell"><span class="rv${above ? " above" : ""}">${esc(pct(value(r[rg.rate])))}</span><span class="mb" aria-hidden="true" data-lo="${lo}" data-hi="${hi}" data-pt="${pt}" data-t="${t}"><i class="band"></i><i class="over"></i>${rg.target !== undefined ? '<i class="tg"></i>' : ""}<i class="pt${above ? " above" : ""}"></i></span></div>${lead}` };
    }
    if (rg && c === rg.low) return { attrs: ' class="r muted"', inner: `${value(r[rg.low]) && measurable(r) ? `${esc(pct(value(r[rg.low])).slice(0, -2))}–${esc(pct(value(r[rg.high])).slice(0, -2))}` : ""}${lead}` };
    if (rg && c === rg.n) return { attrs: ` class="r${measurable(r) ? "" : " muted"}" data-number`, inner: `${esc(value(r[c]))}${lead}` };
    if (o.changesBetween && c === "changes") return { attrs: ' class="ch-cell"', inner: `${changesCell(esc, value(r[c]))}${lead}` };
    if (o.evidence && c === o.evidence) return { attrs: ' class="ev-cell"', inner: `${evidenceCell(esc, value(r[c]), o.traceNote ? value(r[o.traceNote]) : "")}${lead}` };
    const num = numeric.has(c) || NUM_RE.test(value(r[c]));
    const full = value(r[c]);
    const lbl = value(r[`${c}Label`]);
    // a column with a label beside it (<column>Label) shows the label, the IRI in the tooltip
    const shown = lbl || ((o.short ?? []).includes(c) ? shortId(full) : shortly(full));
    const hex = !lbl && (isLongHex(full) || /^https:\/\/kton\.dev\/o\/[0-9a-f]{64}$/.test(full));
    const tip = hex ? "" : shown !== full ? ` data-tip="${esc(full)}"` : "";
    const marker = o.groupNotes && k1(c) ? noteMarker(esc, value(r[o.groupNotes])) : "";
    // a long hash is shown short, the full value in the tooltip; a click or Enter copies it
    const text = hex ? `<span class="hash" tabindex="0" role="button" title="${esc(full)}" data-copy="${esc(full)}">${esc(shortHex(full))}</span>` : `<span${tip}>${esc(shown)}</span>`;
    return { attrs: `${num ? ' class="r" data-number' : ""}${o.group ? " data-ind" : ""}`, inner: `${text}${marker}${lead}` };
  };
  const anyDetail = (r) => T.details.some((d) => value(r[d.col])) || (o.actsIn === "details" && perRow && !!value(r[o.target]));
  const anyDetails = rows.some(anyDetail);
  const detailRow = (r, n, width) => {
    const items = T.details.filter((d) => value(r[d.col])).map((d) => `<dt>${esc(d.label ?? label(d.col))}</dt><dd data-col="${esc(d.col)}" data-detail="${esc(d.col)}">${part(r, d.col, false).inner}</dd>`);
    const acts = o.actsIn === "details" && perRow ? `<div class="actions">${rowButtons(r, n)}</div>` : "";
    return `<tr class="det" id="det-${tableSeq}-${n}" data-det="${n}"${open.has(n) ? "" : " hidden"}><td colspan="${width}"><dl class="kv det-dl">${items.join("")}</dl>${acts}</td></tr>`;
  };
  const line2Of = (r) =>
    T.line2
      .filter((d) => value(r[d.col]))
      .map((d) => `<span class="l2i" data-col="${esc(d.col)}"><span class="l2l">${esc(d.label ?? label(d.col))}</span> ${part(r, d.col, false).inner}</span>`)
      .join("");
  const inlineActs = perRow && o.actsIn !== "details";
  const width = line1.length + (inlineActs ? 1 : 0);
  let i = 0;
  let actChars = 0;
  const tr = (r) => {
    const n = i++;
    const l2 = line2Of(r);
    const det = anyDetail(r);
    const btn = det
      ? `<button type="button" class="disc" data-disc="${n}" aria-expanded="${open.has(n) ? "true" : "false"}" aria-controls="det-${tableSeq}-${n}" aria-label="Details"><svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M5.5 3.5 10 8l-4.5 4.5"/></svg></button>`
      : anyDetails ? '<span class="disc ph" aria-hidden="true"></span>' : "";
    const cells = line1.map((c, k) => {
      const p = part(r, c, k === 0);
      return `<td data-col="${esc(c)}"${p.attrs}>${k === 0 ? btn : ""}${p.inner}</td>`;
    });
    let acts = "";
    if (inlineActs) {
      const b = rowButtons(r, n);
      actChars = Math.max(actChars, b.replace(/<span[^>]*data-row-out[^>]*><\/span>/, "").replace(/<[^>]+>/g, "").length + 3 * (b.match(/<button/g) ?? []).length);
      acts = `<td data-acts class="acts"${l2 ? ' rowspan="2"' : ""}><div class="acts-stack">${b}</div></td>`;
    }
    const target = click && value(r[click]) ? value(r[click]) : "";
    const rowCls = [l2 ? "has2" : "", target ? "kton-clickable" : ""].filter(Boolean).join(" ");
    const main = `<tr data-row="${n}"${rowCls ? ` class="${rowCls}"` : ""}${target ? ` tabindex="0" data-target="${esc(target)}"` : ""}>${actsFirst ? acts : ""}${cells.join("")}${actsFirst ? "" : acts}</tr>`;
    return `${main}${l2 ? `<tr class="l2${target ? " kton-clickable" : ""}" data-l2="${n}"${target ? ` data-target="${esc(target)}"` : ""}><td colspan="${line1.length}"><div class="l2g">${l2}</div></td></tr>` : ""}${det ? detailRow(r, n, width) : ""}`;
  };
  tableSeq++;
  let tbody;
  if (o.group) {
    const glabels = new Map((o.groupLabels ?? []).map((l) => (l.includes("|") ? [l.slice(0, l.indexOf("|")), l.slice(l.indexOf("|") + 1)] : [l, l])));
    const unit = label(cols[0] ?? "").toLowerCase();
    const sumName = o.groupSum ? label(o.groupSum).toLowerCase() : "";
    const ordered = rows.slice();
    const groups = groupRows(ordered, o.group, o.groupSum);
    rows = groups.flatMap((g) => g.rows); // the row index follows the drawn order
    tbody = groups
      .map((g) => {
        const name = glabels.get(g.group) ?? (/^[a-z][a-z -]*$/.test(g.group) ? g.group.charAt(0).toUpperCase() + g.group.slice(1) : g.group || "—");
        const meta = `${g.rows.length} ${unit}${g.rows.length === 1 ? "" : "s"}${o.groupSum ? ` · ${g.sum.toLocaleString("en")} ${sumName}` : ""}`;
        const notes = o.groupNotes ? groupNotes(g.rows.map((x) => value(x[o.groupNotes]))) : [];
        const list = notes.length ? `<div class="grp-notes"><span class="muted">${esc(notesLabel(o.groupNotes))} (${notes.length}), the same for every row below:</span><ol>${notes.map((x) => `<li>${esc(x)}</li>`).join("")}</ol></div>` : "";
        return `<tr class="grp" data-group="${esc(g.group)}"><th colspan="${width}" scope="colgroup">${esc(name)} <span class="muted">${esc(meta)}</span>${list}</th></tr>${g.rows.map(tr).join("")}`;
      })
      .join("");
  } else tbody = rows.map(tr).join("");
  const foot = o.total && line1.includes(o.total)
    ? `<tfoot><tr>${actsFirst && inlineActs ? "<th></th>" : ""}${line1.map((c, k) => (k === 0 ? '<th scope="row">Total</th>' : c === o.total ? `<th class="r" data-total>${esc(rows.reduce((a, r) => a + (Number(value(r[c])) || 0), 0).toLocaleString("en"))}</th>` : "<th></th>")).join("")}${!actsFirst && inlineActs ? "<th></th>" : ""}</tr></tfoot>`
    : "";
  // the acts column fits its buttons (about 0.62 rem a character), at most min(14rem, 40%): the width is set by the block
  const actsTh = inlineActs ? `<th class="acts-h" data-actw="${Math.max(4, Math.ceil(actChars * 0.62))}"><span class="sr-only">Actions</span></th>` : "";
  return { html: `<div class="tbl-wrap${narrow ? " narrow" : ""}"><table class="kton-table t" data-area-table data-count="${rows.length}"><thead><tr>${actsFirst ? actsTh : ""}${line1.map(th).join("")}${actsFirst ? "" : actsTh}</tr></thead><tbody>${tbody}</tbody>${foot}</table></div>`, rows };
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
