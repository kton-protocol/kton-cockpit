// evidence: what there is to open for one item (an element), one row each: ?kind, ?label, ?note, ?stamp, ?for (the item),
// and either a source at a commit (?repo ?commit ?path ?line ?sha256: open@1 {source}, shown read-only, the bytes
// checked against ?sha256) or a run artifact (?run ?name ?sha256 ?mediaType ?bytes: open@1 {artifact}, checked the
// same way), else only the label and note (nothing to open: "not resolved: …", "no data", a workbook's locator).
// The host decides what an open shows and keeps what was shown in the view session's looked-at list.
// Option ask "var|Placeholder": an input that sets the state `element` (the event element) to a typed id.
// Option emptyText: what to say when there is no row.
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
  ctx.request("query@1", {}).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const target = (x) => {
        if (v(x.repo) && v(x.commit) && v(x.path)) return { source: { repo: v(x.repo), commit: v(x.commit), path: v(x.path), ...(v(x.line) ? { line: Number(v(x.line)) } : {}), ...(v(x.sha256) ? { sha256: v(x.sha256) } : {}) } };
        if (v(x.run) && v(x.name) && v(x.sha256)) return { artifact: { run: v(x.run), name: v(x.name), sha256: v(x.sha256), ...(v(x.mediaType) ? { mediaType: v(x.mediaType) } : {}) } };
        return null;
      };
      const item = (x, i) => {
        const stamp = v(x.stamp);
        const chip = stamp ? `<span class="kton-stamp" data-stamp>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
        const t = target(x);
        const head = t
          ? `<button type="button" data-open="${i}" data-testid="evidence-${esc(v(x.kind))}-${i}">${esc(v(x.label))}</button>`
          : `<span data-testid="evidence-${esc(v(x.kind))}-${i}">${esc(v(x.label))}</span>`;
        const size = v(x.bytes) ? ` · ${esc(v(x.bytes))} bytes` : "";
        return `<div class="kton-field" data-evidence="${esc(v(x.kind))}" data-row="${i}">${head} ${chip}${v(x.note) || size ? `<div class="kton-muted">${esc(v(x.note))}${size}</div>` : ""}<span class="kton-muted" data-open-out="${i}"></span></div>`;
      };
      const forWhat = rows.length && v(rows[0].for) ? `<p class="kton-muted" data-evidence-for>for ${esc(v(rows[0].for))}</p>` : "";
      el.innerHTML = `${ask}${rows.length ? forWhat + rows.map(item).join("") : `<p class="kton-muted" data-area-empty>${esc(o.emptyText || "Nothing to open.")}</p>`}`;
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
