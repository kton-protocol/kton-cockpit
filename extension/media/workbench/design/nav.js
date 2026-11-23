// The section nav's highlight: the link of the section on screen is on (it used to follow only clicks, so it could name
// another section than the one on screen). Pure: the frame decorator (frame-design.js) measures, this decides.
//
// parts:   the nav's sections in document order, {id, top, bottom} relative to the viewport (getBoundingClientRect)
// line:    where the sticky bar ends (its height plus a few pixels): a section whose head has passed it is the one read
// viewH:   the viewport's height
// atBottom the page cannot scroll further: the last sections may never reach the line, so the one clicked (if it is on
//          screen) or else the last one on screen is shown
// clicked: the id of the link last clicked (kept while its section is on screen at the bottom of the page)
/** The id of the section the reader is in, or "" when there is none. */
export function shownSection(parts, { line = 0, viewH = Infinity, atBottom = false, clicked = "" } = {}) {
  const ps = (parts ?? []).filter((p) => p && typeof p.id === "string" && Number.isFinite(p.top) && Number.isFinite(p.bottom) && p.bottom > p.top);
  if (!ps.length) return "";
  const onScreen = ps.filter((p) => p.bottom > line && p.top < viewH);
  if (atBottom && onScreen.length) {
    if (clicked && onScreen.some((p) => p.id === clicked)) return clicked;
    return onScreen[onScreen.length - 1].id;
  }
  // the section whose head passed the line last: the greatest top at or above it; sections side by side (equal tops,
  // e.g. summary | spot check) keep the first
  let best = null;
  for (const p of ps) if (p.top <= line && (!best || p.top > best.top)) best = p;
  return (best ?? ps[0]).id;
}
