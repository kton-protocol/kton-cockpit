// estimate: a measured rate applied to a base, with its band, against a target. The query's first row:
// ?rate ?low ?high (percent), ?base (count), ?target (percent), ?stamp; optionally ?reportedRate ?reportedLow
// ?reportedHigh ?reportedAt (the values reported earlier), ?wrong ?n (judged wrong by both, of n), ?same ?second and
// ?reportedSame ?reportedSecond (one judge alone), ?note.
// Shows ≈ rate × base, the band in elements, and the rate drawn to scale (DESIGN.md §5 estimate): the band from low to
// high (within the target in the accent colour, above it in orange), the point, the dashed target, the reported value
// as a hollow diamond under the band; two verdict pills; "how it was computed" as a table. Options:
//   title     the block's heading; unit what the base counts (e.g. "covered elements")
//   chart     range (default) | none
//   domain    "lo,hi" in percent (default: 0 to ceil(max(high, target) × 1.3))
//   reported  "yes": the reported values as the secondary marker; repro "yes": the reproduction table
//   emptyText, emptyHow  what to say when there is no row, and how there will be one
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const unit = o.unit ?? "";
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  el.innerHTML = `${o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : ""}${skeleton("lines")}`;
  stamped(el, esc, ctx.request("query@1", {})).then(
    (r) => {
      const x = (r?.results?.bindings ?? [])[0];
      const title = o.title ? `<h3 data-block-title>${esc(o.title)}</h3>` : "";
      if (!x) {
        el.innerHTML = `${title}${emptyBox(esc, o.emptyText || "No measurement.", o.emptyHow)}`;
        return;
      }
      const n = (k) => (v(x[k]) === "" ? NaN : Number(v(x[k])));
      const [rate, low, high, base, target] = ["rate", "low", "high", "base", "target"].map(n);
      const hasBase = Number.isFinite(base) && base > 0;
      const of = (p) => Math.round((p / 100) * base);
      const fmt = (p) => String(p);
      const stamp = v(x.stamp);
      const chip = stamp ? `<span class="kton-stamp" ${stampAttrs(x, v, esc)}>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
      const rep = { rate: n("reportedRate"), low: n("reportedLow"), high: n("reportedHigh"), at: v(x.reportedAt) };
      const hasRep = o.reported === "yes" && Number.isFinite(rep.rate);
      const head = hasBase
        ? `<div class="est-head kton-figure"><b class="kton-number big" data-number>≈ ${esc(of(rate).toLocaleString("en"))}</b> <span>of <b data-number>${esc(base.toLocaleString("en"))}</b> ${esc(unit)}</span> ${chip}</div>
  <div class="est-sub num"><span data-number>${esc(fmt(rate))} %</span> · range <span data-number>${esc(fmt(low))}–${esc(fmt(high))} %</span> = <span data-number>${esc(of(low).toLocaleString("en"))}–${esc(of(high).toLocaleString("en"))}</span> ${esc(unit || "elements")} · target <span data-number>${esc(fmt(target))} %</span></div>`
        : `<div class="est-head kton-figure"><b class="kton-number big" data-number>${esc(fmt(rate))} %</b> <span>range <b data-number>${esc(fmt(low))}–${esc(fmt(high))} %</b> · target <b data-number>${esc(fmt(target))} %</b></span> ${chip}</div><div class="est-sub">no base to count elements</div>`;
      const dom = domainOf(o.domain, high, target);
      const chart = o.chart !== "none" && [rate, low, high, target].every(Number.isFinite) ? rangeBar(esc, { rate, low, high, target }, hasRep ? rep : null, dom) : "";
      const legend = chart
        ? `<div class="legend"><span><i class="sw-pt"></i>recomputed ${esc(fmt(rate))} % (${esc(fmt(low))}–${esc(fmt(high))})</span>${hasRep ? `<span data-reported><i class="sw-rep"></i>reported ${esc(v(x.reportedRate))} %${Number.isFinite(rep.low) ? ` (${esc(v(x.reportedLow))}–${esc(v(x.reportedHigh))})` : ""}${rep.at ? `, ${esc(rep.at)}` : ""}</span>` : ""}<span><i class="sw-tgt"></i>target ${esc(fmt(target))} %</span>${high > target ? `<span><i class="sw-over"></i>range above target</span>` : ""}</div>`
        : "";
      const verdict = verdicts({ rate, high, target });
      const pills = `<div class="verdicts" data-verdict><span class="pill ${verdict.point.ok ? "st-ok" : "st-warn"}">${verdict.point.ok ? ICON_CHECK : ""}${esc(verdict.point.text)}</span> <span class="pill ${verdict.upper.ok ? "st-ok" : "st-warn"}">${verdict.upper.ok ? ICON_CHECK : ""}${esc(verdict.upper.text)}</span></div>`;
      const repro = o.repro === "yes" ? reproTable(esc, x, v, rep) : "";
      el.innerHTML = `${title}<div class="kton-estimate" data-estimate>${head}${chart}${legend}${pills}${repro}</div>`;
      const bar = el.querySelector?.(".rb");
      if (bar && bar.style?.setProperty) for (const [k, val] of Object.entries(positions({ rate, low, high, target }, hasRep ? rep : null, dom))) bar.style.setProperty(`--${k}`, String(val));
      for (const t of el.querySelectorAll?.(".rb [data-x]") ?? []) t.style.setProperty("--x", t.dataset.x);
    },
    (e) => {
      el.innerHTML = `<div class="callout error" data-area-error role="alert"><p>${esc(e?.message ?? String(e))}</p></div>`;
    },
  );
  return { set() {}, destroy() {} };
}

/** The scale: "lo,hi" as given, else [0, ceil(max(high, target) × 1.3)]. */
export function domainOf(opt, high, target) {
  const m = /^\s*(-?[\d.]+)\s*,\s*(-?[\d.]+)\s*$/.exec(opt ?? "");
  if (m && Number(m[2]) > Number(m[1])) return [Number(m[1]), Number(m[2])];
  const top = Math.max(Number.isFinite(high) ? high : 0, Number.isFinite(target) ? target : 0);
  return [0, Math.max(1, Math.ceil(top * 1.3))];
}

/** A value's place on the scale, in percent of the bar, clamped to it. */
export function pos(x, [lo, hi]) {
  return Math.round(Math.min(100, Math.max(0, ((x - lo) / (hi - lo)) * 100)) * 100) / 100;
}

/** The bar's custom properties (percent): band lo/hi, point, target, and the reported marker. */
export function positions(m, rep, dom) {
  const p = { lo: pos(m.low, dom), hi: pos(m.high, dom), pt: pos(m.rate, dom), t: pos(m.target, dom) };
  if (rep && Number.isFinite(rep.rate)) Object.assign(p, { rpt: pos(rep.rate, dom), rlo: pos(Number.isFinite(rep.low) ? rep.low : rep.rate, dom), rhi: pos(Number.isFinite(rep.high) ? rep.high : rep.rate, dom) });
  return p;
}

/** Labels never overlap: the point's label sits left of the point and the target's right of the line; above the target they swap. */
export function labelSides(rate, target) {
  return rate > target ? { point: "start", target: "end" } : { point: "end", target: "start" };
}

/** Axis ticks: about five round steps over the scale. */
export function ticks([lo, hi]) {
  const span = hi - lo;
  const raw = span / 4;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((f) => f * mag).find((s) => s >= raw) ?? raw;
  const out = [];
  for (let t = Math.ceil(lo / step) * step; t <= hi + 1e-9; t += step) out.push(Math.round(t * 1000) / 1000);
  return out;
}

export function verdicts({ rate, high, target }) {
  return {
    point: rate <= target ? { ok: true, text: "point estimate meets the target" } : { ok: false, text: `point estimate ${rate} % does not meet the target` },
    upper: high <= target ? { ok: true, text: `upper bound ${high} % meets it too` } : { ok: false, text: `upper bound ${high} % does not` },
  };
}

function rangeBar(esc, m, rep, dom) {
  const s = labelSides(m.rate, m.target);
  const p = positions(m, rep, dom);
  const tk = ticks(dom);
  const aria = `Rate ${m.rate} percent, range ${m.low} to ${m.high} percent, target ${m.target} percent${rep ? `, reported ${rep.rate} percent` : ""}. Scale ${dom[0]} to ${dom[1]} percent.`;
  return `<div class="rb" role="img" aria-label="${esc(aria)}" data-domain="${dom.join(",")}" data-pt="${p.pt}" data-t="${p.t}" data-lo="${p.lo}" data-hi="${p.hi}">
  <div class="rb-top"><span class="lab ${s.point} pt-lab" data-x="${p.pt}">${esc(String(m.rate))} %</span><span class="lab ${s.target} t-lab" data-x="${p.t}">target ${esc(String(m.target))} %</span></div>
  <div class="rb-track"><span class="band"></span><span class="over"></span><span class="pt${m.rate > m.target ? " above" : ""}"></span>${rep ? `<span class="rep"></span><span class="repm"></span>` : ""}</div>
  <span class="tgt" aria-hidden="true"></span>
  <div class="rb-axis" aria-hidden="true">${tk.map((t, i) => `<span class="tick" data-x="${pos(t, dom)}">${t}${i === tk.length - 1 ? " %" : ""}</span>`).join("")}</div>
</div>`;
}

function reproTable(esc, x, v, rep) {
  const rows = [
    ["Both judges" + (v(x.wrong) && v(x.n) ? ` (${v(x.wrong)} of ${v(x.n)})` : ""), v(x.rate), Number.isFinite(rep.rate) ? `${v(x.reportedRate)}${Number.isFinite(rep.low) ? ` (${v(x.reportedLow)}–${v(x.reportedHigh)})` : ""}` : ""],
    ["One judge alone, same model", v(x.same), v(x.reportedSame)],
    ["One judge alone, second model", v(x.second), v(x.reportedSecond)],
  ].filter(([, a]) => a);
  if (!rows.length) return "";
  const same = (a, b) => b !== "" && Number(a) === Number(String(b).split(" ")[0]);
  return `<details class="more"><summary>How it was computed, and what reproduces</summary><div class="tbl-wrap"><table class="t"><thead><tr><th>Measure</th><th class="r">Recomputed</th><th class="r">Reported${rep.at ? ` ${esc(rep.at)}` : ""}</th><th>Reproduces</th></tr></thead><tbody>${rows
    .map(([m, a, b]) => `<tr><td>${esc(m)}</td><td class="r">${esc(a)}</td><td class="r">${esc(b || "—")}</td><td class="${b === "" ? "muted" : same(a, b) ? "st-ok" : "st-warn"}">${b === "" ? "not reported" : same(a, b) ? "yes" : "no"}</td></tr>`)
    .join("")}</tbody></table></div>${v(x.note) ? `<p>${esc(v(x.note))}</p>` : ""}</details>`;
}

export function stampAttrs(x, v, esc) {
  return `data-stamp="${esc(v(x.stamp))}"${v(x.stampLabel) ? ` data-stamp-label="${esc(v(x.stampLabel))}"` : ""}${v(x.stampFull) ? ` data-stamp-full="${esc(v(x.stampFull))}"` : ""}${v(x.stampScope) ? ` data-stamp-scope="${esc(v(x.stampScope))}"` : ""}`;
}

const ICON_CHECK = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="m3 8.5 3 3 7-7"/></svg>`;

/** Loading: a skeleton of the block's own shape (lines or a tile row), busy for assistive tech (DESIGN.md §5). */
export function skeleton(shape = "lines") {
  return `<div class="wb-skel ${shape}" data-area-loading aria-busy="true" role="status"><span class="sr-only">Loading…</span><i></i><i></i><i></i></div>`;
}

/** Empty: what will appear (bold) and how it will (muted), never a bare "No data". */
export function emptyBox(esc, text, how) {
  return `<div class="empty" data-area-empty><svg class="i" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3"/><path d="M8 7.2v4M8 4.8v.4"/></svg><div><b>${esc(text)}</b>${how ? `<p>${esc(how)}</p>` : ""}</div></div>`;
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
