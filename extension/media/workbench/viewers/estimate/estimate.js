// estimate: a measured rate applied to a base, with its band, against a target. The query's first row:
// ?rate ?low ?high (percent), ?base (count), ?target (percent), ?stamp, and ?single (other measurements, text).
// Shows ≈ rate × base [low × base – high × base] and whether the point and the upper bound meet the target.
// Option unit: what the base counts (e.g. "covered elements").
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const unit = ctx.use.options?.unit ?? "";
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  el.innerHTML = `<div class="kton-muted" data-area-loading>Loading…</div>`;
  ctx.request("query@1", {}).then(
    (r) => {
      const x = (r?.results?.bindings ?? [])[0];
      if (!x) {
        el.innerHTML = `<p class="kton-muted" data-area-empty>No measurement.</p>`;
        return;
      }
      const n = (k) => Number(v(x[k]));
      const [rate, low, high, base, target] = ["rate", "low", "high", "base", "target"].map(n);
      const hasBase = Number.isFinite(base) && base > 0;
      const of = (p) => Math.round((p / 100) * base);
      const stamp = v(x.stamp);
      const chip = stamp ? `<span class="kton-stamp" data-stamp>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`;
      const point = rate <= target ? "meets" : "misses";
      const upper = high <= target ? "meets" : "does not meet";
      el.innerHTML = `<div class="kton-estimate" data-estimate>
  ${hasBase
    ? `<div class="kton-figure"><b class="kton-number" data-number>≈ ${esc(of(rate).toLocaleString("en"))}</b> ${esc(unit)} · <b data-number>${esc(String(rate))} %</b> of <b data-number>${esc(base.toLocaleString("en"))}</b> ${chip}</div>
  <div class="kton-figure">range <b data-number>${esc(String(low))}–${esc(String(high))} %</b> = <b data-number>${esc(of(low).toLocaleString("en"))}–${esc(of(high).toLocaleString("en"))}</b> · target <b data-number>${esc(String(target))} %</b> ${chip}</div>`
    : `<div class="kton-figure"><b class="kton-number" data-number>${esc(String(rate))} %</b> · range <b data-number>${esc(String(low))}–${esc(String(high))} %</b> · target <b data-number>${esc(String(target))} %</b> ${chip} <span class="kton-muted">no base to count elements</span></div>`}
  <div class="kton-bar" aria-hidden="true"><span class="band" data-band="${low}-${high}"></span><span class="target" data-target="${target}"></span></div>
  <p data-verdict>The point estimate ${point} the target, the upper bound ${upper}.</p>
  ${v(x.single) ? `<p class="kton-muted" data-single>${esc(v(x.single))} ${chip}</p>` : ""}
</div>`;
    },
    (e) => {
      el.innerHTML = `<p class="kton-error" data-area-error>${esc(e?.message ?? String(e))}</p>`;
    },
  );
  return { set() {}, destroy() {} };
}
