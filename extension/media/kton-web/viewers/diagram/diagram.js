// diagram: a block for a Mermaid source (a text content file: the plan carries its text in use.content).
// The kit draws it in the sandboxed Mermaid frame and hands back SVG text; this block shows it as an
// <img> from a data: URL, so nothing in the SVG runs or reaches the composition frame's DOM. No links.
export function mount(el, ctx) {
  el.classList.add("kton-diagram");
  const u = ctx.use;
  const esc = ctx.kit.esc;
  const source = u.content?.text;
  if (typeof source !== "string") {
    el.innerHTML = '<div class="kton-error">This diagram has no Mermaid source (a text content file).</div>';
    el.dataset.drawn = "false";
    return;
  }
  el.innerHTML = '<span class="kton-muted">drawing…</span>';
  ctx.kit.mermaid(source).then(
    (r) => {
      const size = r.width ? ` width="${r.width}" height="${r.height}"` : "";
      el.innerHTML = `<img${size} alt="${esc(u.caption ?? (r.labels ?? []).join(", ") ?? "diagram")}" src="data:image/svg+xml;charset=utf-8,${encodeURIComponent(r.svg)}">${u.caption ? `<div class="kton-caption">${esc(u.caption)}</div>` : ""}`;
      el.dataset.labels = JSON.stringify(r.labels ?? []);
      el.dataset.foreignObjects = String(r.foreignObjects ?? 0);
      el.dataset.drawn = "true";
    },
    (e) => {
      el.innerHTML = `<div class="kton-error">Could not draw the diagram: ${esc(String(e?.message ?? e))}</div>`;
      el.dataset.drawn = "false";
    },
  );
}
