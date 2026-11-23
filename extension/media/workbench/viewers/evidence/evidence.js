// evidence: what there is to open for one item (an element), one row each: ?kind, ?label, ?note, ?stamp, ?for (the item),
// and either a source at a commit (?repo ?commit ?path ?line ?sha256: open@1 {source}, shown read-only, the bytes
// checked against ?sha256) or a run artifact (?run ?name ?sha256 ?mediaType ?bytes: open@1 {artifact}, checked the
// same way), else only the label and note (nothing to open: "not resolved: …", "no data", a workbook's locator).
// The host decides what an open shows and keeps what was shown in the view session's looked-at list.
// Option ask "var|Placeholder": an input that sets the state `element` (the event element) to a typed id.
// Option emptyText: what to say when there is no row.
// A row with ?diff "true" and ?repo ?commit (?path optional) opens that commit's change (open@1 {source: {diff}}), as a
// reviewer reads it. Rows with ?group are shown under a heading per group, in the query's order. ?approval (the host's:
// which commit a verdict approves, and whether it is stale for the reviewed one) is shown under the item.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const placeholder = o.ask ? (o.ask.includes("|") ? o.ask.slice(o.ask.indexOf("|") + 1) : o.ask) : "";
  const id = String(ctx.use.key ?? "ev").replace(/[^\w-]/g, "_");
  const ask = o.ask
    ? `<form class="picker" data-ask><label for="${id}-el">Element</label><input id="${id}-el" type="text" class="mono" data-testid="evidence-ask" placeholder="${esc(placeholder)}" value="${esc(String(ctx.state?.element ?? ""))}"${o.askHint ? ` aria-describedby="${id}-hint"` : ""}><button type="submit" class="small" data-testid="evidence-show">Show</button>${o.askHint ? `<span id="${id}-hint" class="muted hint">${esc(o.askHint)}</span>` : ""}</form>`
    : "";
  const title = o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : "";
  // a redraw (the block's answer arrives) keeps what the reader typed into the element field meanwhile
  const draw = (html) => {
    const typed = el.querySelector("form[data-ask] input")?.value;
    el.innerHTML = html;
    const input = typed ? el.querySelector("form[data-ask] input") : null;
    if (input && !input.value) input.value = typed;
  };
  el.innerHTML = `${ask}${title}${skeleton("lines")}`;
  const wireAsk = () => {
    const f = el.querySelector("form[data-ask]");
    if (!f) return;
    f.addEventListener("submit", (e) => {
      e.preventDefault();
      const x = f.querySelector("input").value.trim();
      if (x) ctx.setState({ ...(ctx.state ?? {}), element: x });
    });
  };
  wireAsk();
  stamped(el, esc, ctx.request("query@1", {}), ctx.use.key).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const target = (x) => {
        if (v(x.diff) === "true" && v(x.repo) && v(x.commit)) return { source: { repo: v(x.repo), commit: v(x.commit), diff: true, ...(v(x.path) ? { path: v(x.path) } : {}) } };
        if (v(x.repo) && v(x.commit) && v(x.path)) return { source: { repo: v(x.repo), commit: v(x.commit), path: v(x.path), ...(v(x.line) ? { line: Number(v(x.line)) } : {}), ...(v(x.sha256) ? { sha256: v(x.sha256) } : {}) } };
        if (v(x.run) && v(x.name) && v(x.sha256)) return { artifact: { run: v(x.run), name: v(x.name), sha256: v(x.sha256), ...(v(x.mediaType) ? { mediaType: v(x.mediaType) } : {}) } };
        return null;
      };
      const chip = (x) => (v(x.stamp) ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(v(x.stamp))}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`);
      const forWhat = rows.length && v(rows[0].for) ? `<p class="kton-muted" data-evidence-for>for <span class="mono">${esc(v(rows[0].for))}</span></p>` : "";
      let body;
      if (o.runs === "table" && rows.some((x) => v(x.run))) body = runsTable(esc, rows, v, chip, o.collapsePassing === "yes");
      else {
        const item = (x, i) => {
          const t = target(x);
          const shown = itemTitle(x, v);
          const approval = v(x.approval) ? `<div class="kton-muted${v(x.stale) === "true" ? " kton-error" : ""}" data-approval${v(x.stale) === "true" ? " data-stale" : ""}>${esc(v(x.approval))}</div>` : "";
          const flag = mismatch(x, v);
          const size = v(x.bytes) ? `${Number(v(x.bytes)).toLocaleString("en")} bytes` : "";
          const note = [v(x.note), size].filter(Boolean).join(" · ");
          const thumb = v(x.thumb) && /^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/.test(v(x.thumb)) ? `<img class="ev-img" src="${v(x.thumb)}" alt="">` : "";
          return `<li data-evidence="${esc(v(x.kind))}" data-row="${i}" data-label="${esc(v(x.label))}">${icon(v(x.kind))}
  <div><div class="t" data-testid="evidence-${esc(v(x.kind))}-${i}"${shown.title !== v(x.label) ? ` data-tip="${esc(v(x.label))}" tabindex="0"` : ""}>${esc(shown.title)}${shown.sub ? ` <span class="muted">· ${esc(shown.sub)}</span>` : ""}</div>
  ${shown.loc ? `<span class="loc">${esc(shown.loc)}</span>` : ""}${shown.title !== v(x.label) && v(x.label) && !/^Open\b/.test(v(x.label)) ? `<div class="what">${esc(v(x.label))}</div>` : ""}${note ? `<div class="note kton-muted">${esc(note)}</div>` : ""}${thumb}${approval}${flag ? `<div class="flag">${ICON_WARN}<span>${esc(flag)}</span></div>` : ""} ${chip(x)}</div>
  <div class="side"><span class="kton-muted" data-open-out="${i}" role="status"></span>${t ? `<button type="button" class="ghost small" data-open="${i}" aria-label="${esc(v(x.label) || "Open")}">Open</button>` : ""}</div></li>`;
        };
        let lastG = null;
        const items = rows
          .map((x, i) => {
            const g = v(x.group);
            const head = g && g !== lastG ? `</ul><h4 data-group="${esc(g)}">${esc(g)} <span class="muted">(${rows.filter((y) => v(y.group) === g).length})</span></h4><ul class="ev">` : "";
            lastG = g || lastG;
            return head + item(x, i);
          })
          .join("");
        body = `<ul class="ev">${items}</ul>`.replace(/<ul class="ev"><\/ul>/g, "");
      }
      draw(`${ask}${title}${rows.length ? forWhat + body : emptyBox(esc, o.emptyText || "Nothing to open.", o.emptyHow)}`);
      wireAsk();
      for (const b of el.querySelectorAll("button[data-open]")) {
        b.addEventListener("click", () => {
          const i = Number(b.dataset.open);
          const out = el.querySelector(`[data-open-out="${i}"]`);
          if (out) out.textContent = " Opening…";
          b.disabled = true;
          ctx.intent({ kind: "open", ...target(rows[i]) }).then((res) => {
            b.disabled = false;
            if (!out) return;
            out.textContent = ` ${res?.refused ? `Not opened: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Opened."}`;
            out.dataset.opened = res?.refused ? "no" : "yes";
          });
        });
      }
    },
    (e) => {
      draw(`${ask}${title}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`);
      wireAsk();
    },
  );
  return { set() {}, destroy() {} };
}

const dec = (x) => {
  try {
    return decodeURIComponent(x);
  } catch {
    return x;
  }
};

/** What a row is called: the decoded file name and its line, the location (repo · dir/ · at 12 hex); else the query's label. */
export function itemTitle(x, v) {
  const path = v(x.path);
  if (!path || !v(x.repo)) return { title: v(x.label), sub: "", loc: "" };
  const p = dec(path);
  const file = p.split("/").pop() ?? p;
  const dir = p.slice(0, p.length - file.length);
  return { title: file, sub: v(x.line) ? `line ${v(x.line)}` : v(x.diff) === "true" ? "the change" : "", loc: `${v(x.repo)}${dir ? ` · ${dir}` : ""}${v(x.commit) ? ` · at ${v(x.commit).slice(0, 12)}` : ""}` };
}

/** A test row whose label names one file but whose open goes to another (bug 5): say so. */
export function mismatch(x, v) {
  if (v(x.kind) !== "test" || !v(x.path)) return "";
  const named = /:\s*(?:[\w.-]+:)?([^#:]+?)(?:#|$)/.exec(v(x.label))?.[1]?.split("/").pop();
  const opened = dec(v(x.path)).split("/").pop();
  if (!named || !opened || named === opened || !/\.\w+$/.test(named)) return "";
  return `Opens ${opened}${v(x.line) ? ` line ${v(x.line)}` : ""}, not the test in ${named}.`;
}

/**
 * Runs as a table: Run | Test | Result | Artifact | Bytes | Open, grouped by test; an assertion
 * screenshot with a thumbnail when the host sent one. With collapse, passing runs with no artifact are counted, not listed.
 */
export function runsTable(esc, rows, v, chip, collapse) {
  const quiet = collapse ? rows.filter((x) => !v(x.sha256) && v(x.outcome) === "passed") : [];
  const shown = rows.map((x, i) => [x, i]).filter(([x]) => !quiet.includes(x));
  const why = [...new Set(quiet.map((x) => v(x.passingWhy)).filter(Boolean))];
  const tr = ([x, i]) => {
    const thumb = v(x.thumb) && /^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/.test(v(x.thumb)) ? `<img class="ev-img small" src="${v(x.thumb)}" alt="">` : "";
    const res = v(x.outcome);
    return `<tr data-evidence="artifact" data-row="${i}" data-test="${esc(v(x.testName))}"><td class="mono">${esc(v(x.run))}</td><td>${esc(shortName(v(x.testName)))}</td><td class="${res === "passed" ? "st-ok" : res ? "st-warn" : ""}">${esc(res)}</td><td>${thumb}${esc(v(x.artifact) || (v(x.sha256) ? "artifact" : "none"))}${v(x.sha256) ? "" : `<div class="kton-muted">${esc(v(x.note))}</div>`}</td><td class="r">${v(x.bytes) ? esc(Number(v(x.bytes)).toLocaleString("en")) : ""}</td><td>${v(x.sha256) ? `<button type="button" class="ghost small" data-open="${i}" data-label="${esc(v(x.label))}" aria-label="${esc(v(x.label))}">Open</button>` : ""}<span class="kton-muted" data-open-out="${i}" role="status"></span> ${chip(x)}</td></tr>`;
  };
  const table = shown.length
    ? `<div class="tbl-wrap"><table class="t" data-runs data-count="${shown.length}"><thead><tr><th scope="col">Run</th><th scope="col">Test</th><th scope="col">Result</th><th scope="col">Artifact</th><th scope="col" class="r">Bytes</th><th scope="col"><span class="sr-only">Open</span></th></tr></thead><tbody>${shown.map(tr).join("")}</tbody></table></div>`
    : "";
  const sum = quiet.length ? `<p class="kton-muted" data-passing="${quiet.length}">${quiet.length} passing run${quiet.length === 1 ? "" : "s"} without an artifact: ${esc(why.join("; ") || "no artifact")} ${quiet.map(chip).join("")}</p>` : "";
  return table + sum;
}

const shortName = (t) => {
  const m = /^(?:lane:)?[\w.-]+:(.+)$/.exec(String(t));
  if (!m) return String(t);
  const h = m[1].indexOf("#");
  return h < 0 ? m[1].split("/").pop() : `${m[1].slice(0, h).split("/").pop()} · ${m[1].slice(h + 1)}`;
};

const ICONS = {
  spec: `<path d="M4 1.5h5.5L12.5 4.5V14.5H4z"/><path d="M9.5 1.5v3h3"/><path d="M6 8h4.5M6 10.5h4.5"/>`,
  test: `<path d="M6 1.5h4M6.5 1.5v4.5L3 13a1 1 0 0 0 .9 1.5h8.2A1 1 0 0 0 13 13L9.5 6V1.5"/><path d="M4.6 10h6.8"/>`,
  workbook: `<rect x="2" y="2.5" width="12" height="11" rx="1"/><path d="M2 6h12M2 9.5h12M6 2.5v11"/>`,
  code: `<path d="M5.5 4.5 2 8l3.5 3.5M10.5 4.5 14 8l-3.5 3.5"/>`,
  trace: `<circle cx="3.5" cy="8" r="1.8"/><circle cx="12.5" cy="8" r="1.8"/><path d="M5.3 8h5.4" stroke-dasharray="1.5 1.5"/>`,
  run: `<circle cx="8" cy="8" r="6.3"/><path d="M6.5 5.5v5l4-2.5z"/>`,
  artifact: `<rect x="1.5" y="2.5" width="13" height="11" rx="1"/><circle cx="5.5" cy="6" r="1.2"/><path d="m2 12 4-4 3 3 2-2 3.5 3.5"/>`,
};
const icon = (k) => `<svg class="i k" viewBox="0 0 16 16" aria-hidden="true">${ICONS[k] ?? ICONS.spec}</svg>`;
const ICON_WARN = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.8 15 14H1z"/><path d="M8 6.2v3.6M8 11.6v.4"/></svg>`;

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
