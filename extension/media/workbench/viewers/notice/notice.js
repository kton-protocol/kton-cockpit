// notice: text the shell or a package writes into a plan (an option), e.g. "the findings package is not installed".
// Options:
//   text     the line
//   stamp    a stamp shown beside it (e.g. the run a view reads and that it signs with test keys); an 8-hex hash in it
//            becomes a chip
//   tone     plain (default) | banner (the sticky dry-run bar: the frame lifts it above the view and drops its part's
//            heading) | head (the view's head: eyebrow, text as the title, lede) | sub (the part's one-line subtitle:
//            the frame moves it under the part's heading) | empty (a dashed box: text bold, how, chips, after) | info |
//            warning (a callout)
//   eyebrow, lede  (head); nav  (sub: the part's short label in the section nav); how, chips, after  (empty)
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const tone = ["banner", "head", "sub", "empty", "info", "warning"].includes(o.tone) ? o.tone : "plain";
  const stamp = o.stamp ? `<span class="kton-stamp" data-stamp="${esc(o.stamp)}">${hashChips(esc(o.stamp))}</span>` : "";
  if (tone === "banner") {
    // "Dry run: dryrun1" reads as DRY RUN · dryrun1 (the label is upper-cased by CSS; the text stays as written)
    const m = /^([^:]+):\s*(.*)$/.exec(o.text ?? "");
    const lead = m ? `<b class="wb-banner-lead">${esc(m[1])}<span class="wb-colon">:</span></b> <span class="mono">${esc(m[2])}</span>` : `<b class="wb-banner-lead">${esc(o.text ?? "")}</b>`;
    el.innerHTML = `<p class="kton-notice wb-banner" data-area-notice data-tone="banner">${lead}${o.stamp ? ` <span class="wb-sep" aria-hidden="true">·</span> <span class="wb-banner-stamp" data-stamp="${esc(o.stamp)}">${hashChips(esc(o.stamp), true)}</span>` : ""}</p>`;
  } else if (tone === "head") {
    el.innerHTML = `<header class="kton-notice wb-head" data-area-notice data-tone="head">${o.eyebrow ? `<div class="wb-eyebrow" data-eyebrow><span>${esc(o.eyebrow)}</span></div>` : ""}<h1 class="wb-title">${esc(o.text ?? "")}</h1>${o.lede ? `<p class="wb-lede">${esc(o.lede)}</p>` : ""}</header>`;
  } else if (tone === "sub") {
    el.innerHTML = `<p class="kton-notice wb-sub" data-area-notice data-tone="sub"${o.nav ? ` data-nav-label="${esc(o.nav)}"` : ""}>${esc(o.text ?? "")}</p>`;
  } else if (tone === "empty") {
    const chips = (o.chips ?? []).length ? `<div class="wb-dash-chips">${o.chips.map((c) => `<span>${esc(c)}</span>`).join("")}</div>` : "";
    el.innerHTML = `<div class="kton-notice empty" data-area-notice data-tone="empty">${ICON_INFO}<div><b>${esc(o.text ?? "")}</b>${o.how ? `<p>${esc(o.how)}</p>` : ""}${chips}${o.after ? `<p>${esc(o.after)}</p>` : ""}</div></div>`;
  } else if (tone === "info" || tone === "warning") {
    el.innerHTML = `<div class="kton-notice callout${tone === "info" ? " info" : ""}" data-area-notice data-tone="${tone}">${tone === "info" ? ICON_INFO : ICON_WARN}<p>${esc(o.text ?? "")}${stamp ? ` ${stamp}` : ""}</p></div>`;
  } else {
    el.innerHTML = `<p class="kton-notice" data-area-notice>${esc(o.text ?? "")}${stamp ? ` ${stamp}` : ""}</p>`;
  }
  return { set() {}, destroy() {} };
}

/** An 8-hex (or longer) hash in already escaped text becomes a chip (a button: its tooltip opens on focus too). */
export function hashChips(escaped, chip = false) {
  return chip ? escaped.replace(/\b([0-9a-f]{8})([0-9a-f]*)\b/g, (_m, h) => `<button type="button" class="chip mono" data-tip="sha256:${h}… · the real decision; signatures in this dry run go into dry-run scopes and do not settle it">${h}</button>`) : escaped;
}

const ICON_INFO = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.3"/><path d="M8 7.2v4M8 4.8v.4"/></svg>`;
const ICON_WARN = `<svg class="i" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.8 15 14H1z"/><path d="M8 6.2v3.6M8 11.6v.4"/></svg>`;
