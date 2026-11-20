// notice: one line of text the shell writes into a plan (an option), e.g. "the findings package is not installed".
export function mount(el, ctx) {
  el.innerHTML = `<p class="kton-notice" data-area-notice>${ctx.kit.esc(ctx.use.options?.text ?? "")}</p>`;
  return { set() {}, destroy() {} };
}
