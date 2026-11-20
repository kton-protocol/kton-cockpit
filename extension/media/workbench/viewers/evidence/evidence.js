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
  const ask = o.ask
    ? `<form class="kton-act" data-ask><input type="text" data-testid="evidence-ask" placeholder="${esc(placeholder)}" value="${esc(String(ctx.state?.element ?? ""))}"><button type="submit" data-testid="evidence-show">Show evidence</button></form>`
    : "";
  el.innerHTML = `${ask}<div class="kton-muted" data-area-loading>Loading…</div>`;
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
  stamped(el, esc, ctx.request("query@1", {})).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const target = (x) => {
        if (v(x.diff) === "true" && v(x.repo) && v(x.commit)) return { source: { repo: v(x.repo), commit: v(x.commit), diff: true, ...(v(x.path) ? { path: v(x.path) } : {}) } };
        if (v(x.repo) && v(x.commit) && v(x.path)) return { source: { repo: v(x.repo), commit: v(x.commit), path: v(x.path), ...(v(x.line) ? { line: Number(v(x.line)) } : {}), ...(v(x.sha256) ? { sha256: v(x.sha256) } : {}) } };
        if (v(x.run) && v(x.name) && v(x.sha256)) return { artifact: { run: v(x.run), name: v(x.name), sha256: v(x.sha256), ...(v(x.mediaType) ? { mediaType: v(x.mediaType) } : {}) } };
        return null;
      };
      const item = (x, i) => {
        const stamp = v(x.stamp);
        const chip = stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
        const t = target(x);
        const head = t
          ? `<button type="button" data-open="${i}" data-testid="evidence-${esc(v(x.kind))}-${i}">${esc(v(x.label))}</button>`
          : `<span data-testid="evidence-${esc(v(x.kind))}-${i}">${esc(v(x.label))}</span>`;
        const size = v(x.bytes) ? ` · ${esc(v(x.bytes))} bytes` : "";
        const approval = v(x.approval) ? `<div class="kton-muted${v(x.stale) === "true" ? " kton-error" : ""}" data-approval${v(x.stale) === "true" ? " data-stale" : ""}>${esc(v(x.approval))}</div>` : "";
        return `<div class="kton-field" data-evidence="${esc(v(x.kind))}" data-row="${i}">${head} ${chip}${approval}${v(x.note) || size ? `<div class="kton-muted">${esc(v(x.note))}${size}</div>` : ""}<span class="kton-muted" data-open-out="${i}"></span></div>`;
      };
      const forWhat = rows.length && v(rows[0].for) ? `<p class="kton-muted" data-evidence-for>for ${esc(v(rows[0].for))}</p>` : "";
      const items = rows
        .map((x, i) => {
          const g = v(x.group);
          const head = g && (i === 0 || v(rows[i - 1].group) !== g) ? `<h4 data-group="${esc(g)}">${esc(g)} (${rows.filter((y) => v(y.group) === g).length})</h4>` : "";
          return head + item(x, i);
        })
        .join("");
      el.innerHTML = `${ask}${rows.length ? forWhat + items : `<p class="kton-muted" data-area-empty>${esc(o.emptyText || "Nothing to open.")}</p>`}`;
      wireAsk();
      for (const b of el.querySelectorAll("button[data-open]")) {
        b.addEventListener("click", () => {
          const i = Number(b.dataset.open);
          const out = el.querySelector(`[data-open-out="${i}"]`);
          if (out) out.textContent = " Opening…";
          ctx.intent({ kind: "open", ...target(rows[i]) }).then((res) => {
            if (out) out.textContent = ` ${res?.refused ? `Not opened: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Opened."}`;
          });
        });
      }
    },
    (e) => {
      el.innerHTML = `${ask}<p class="kton-error" data-area-error>${esc(e?.message ?? String(e))}</p>`;
      wireAsk();
    },
  );
  return { set() {}, destroy() {} };
}

/** The stamp's data attributes: the stamp and, when the query gives them, its source label, full hash and scope (the part's source chip). */
export function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
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
