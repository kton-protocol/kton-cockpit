// notice: one line of text the shell writes into a plan (an option), e.g. "the findings package is not installed".
// Option stamp: a stamp shown beside it (e.g. the run a view reads and that it signs with test keys).
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  el.innerHTML = `<p class="kton-notice" data-area-notice>${esc(o.text ?? "")}${o.stamp ? ` <span class="kton-stamp" data-stamp>${esc(o.stamp)}</span>` : ""}</p>`;
  return { set() {}, destroy() {} };
}
