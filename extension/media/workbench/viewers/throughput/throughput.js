// throughput: items settled per hour, by rule and one by one, computed from the times of the signed decisions a query
// answers (one row per signed decision): its time (option when, default ?when, an xsd:dateTime), its kind (option kind,
// default ?kind; byRule / oneByOne name the two values, default "rule" / "item") and how many items it settled (option
// count, default ?count: 1 for an item, the members less the ones taken out for a rule). The hour is the time from the
// first to the last signature of the answer (at least one minute); with fewer than two signatures there is no rate yet.
// Nothing is kept: every number is the query's answer as of now; option scope names where the decisions are read.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const heading = o.heading ? `<h3 data-block-title>${esc(o.heading)}</h3>` : "";
  el.innerHTML = `${heading}<div class="wb-skel tiles" data-area-loading aria-busy="true" role="status"><span class="sr-only">Loading…</span><i></i><i></i><i></i></div>`;
  const draw = (r) => {
    const rows = r?.results?.bindings ?? [];
    const t = throughput(rows, o);
    if (!t.signatures) {
      el.innerHTML = `${heading}<div class="empty" data-area-empty><div><b>${esc(o.emptyText || "Nothing settled yet.")}</b>${o.emptyHow ? `<p>${esc(o.emptyHow)}</p>` : ""}</div></div>`;
      return;
    }
    const rate = (n) => (t.hours === null ? "no rate yet (one signature)" : `${fmt(n / t.hours)} per hour`);
    const tile = (k, label, x) => `<div class="kton-card card" data-throughput="${k}"><div class="muted">${esc(label)}</div><div class="num big" data-rate>${esc(rate(x.items))}</div><div class="muted" data-settled>${x.items.toLocaleString("en-US")} item${x.items === 1 ? "" : "s"} in ${x.signatures} signature${x.signatures === 1 ? "" : "s"}</div></div>`;
    el.innerHTML = `${heading}<div class="rc-grid" data-throughput-tiles>${tile("rule", "By rule", t.rule)}${tile("item", "One by one", t.item)}${tile("all", "All", { items: t.rule.items + t.item.items, signatures: t.signatures })}</div>
<p class="kton-stamp" data-stamp="${esc(o.scope ?? "")}" data-throughput-span>Computed from ${t.signatures} signed decision${t.signatures === 1 ? "" : "s"}${o.scope ? ` in ${esc(o.scope)}` : ""}, ${esc(t.first)}${t.last !== t.first ? ` to ${esc(t.last)}` : ""}${t.hours === null ? "" : ` (${fmt(t.hours * 60)} min)`}.</p>`;
  };
  ctx.request("query@1", {}).then(
    (r) => {
      draw(r);
      const token = r?.refreshing?.token;
      const wb = globalThis.ktonWorkbench;
      if (token !== undefined && typeof wb?.fresh === "function") wb.fresh(ctx.use.key, token).then((x) => x && x.result && draw(x.result), () => {});
    },
    (e) => {
      el.innerHTML = `${heading}<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return { set() {}, destroy() {} };
}

/** The rates' inputs: items and signatures per kind, the first and last time, the hours between (null under two). */
export function throughput(rows, o = {}) {
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const byRule = o.byRule ?? "rule";
  const oneByOne = o.oneByOne ?? "item";
  const out = { rule: { items: 0, signatures: 0 }, item: { items: 0, signatures: 0 }, signatures: 0, first: "", last: "", hours: null };
  const times = [];
  for (const x of rows) {
    const kind = v(x[o.kind ?? "kind"]);
    const k = kind === byRule ? "rule" : kind === oneByOne ? "item" : null;
    const ms = Date.parse(v(x[o.when ?? "when"]));
    if (!k || Number.isNaN(ms)) continue;
    const n = Math.max(0, Number(v(x[o.count ?? "count"])) || 0);
    out[k].items += n;
    out[k].signatures += 1;
    out.signatures += 1;
    times.push(ms);
  }
  if (!times.length) return out;
  const lo = Math.min(...times), hi = Math.max(...times);
  out.first = new Date(lo).toISOString().slice(0, 16).replace("T", " ");
  out.last = new Date(hi).toISOString().slice(0, 16).replace("T", " ");
  out.hours = times.length < 2 ? null : Math.max(hi - lo, 60_000) / 3_600_000;
  return out;
}

function fmt(x) {
  return x >= 100 ? Math.round(x).toLocaleString("en-US") : x >= 10 ? x.toFixed(0) : x.toFixed(1);
}
