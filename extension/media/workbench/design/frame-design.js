// The workbench frame's look (DESIGN.md §3-4): what compose.js (vendored at its kton-web pin, unchanged) draws gets
// the view's frame here, after it draws it. Nothing in here fetches, acts or changes what a block shows:
//   - the dry-run banner (a notice with tone banner) and the section nav, sticky above the view;
//   - each part's head: its heading, the one-line subtitle (a notice with tone sub, moved up) and its source chip;
//   - source chips once per part (stamps.js): stamps equal to the part's are hidden in the blocks (still in the DOM:
//     every number keeps its stamp), a block with another source gets its own chip, a missing stamp stays red;
//   - tab labels count their rows; tooltips (data-tip) open on hover and on keyboard focus.
// It reads the plan from the host's kton:set (the same message the frame renders) and re-runs after each change.
import { chipFor, sectionStamps, fullHash } from "./stamps.js";

const root = document.getElementById("kton-root");
let plan = null;
let queued = false;
let busy = false;

window.addEventListener("message", (e) => {
  const m = e.data;
  if (m && m.type === "kton:set" && m.plan) {
    plan = m.plan;
    document.getElementById("wb-top")?.remove();
  }
});

const el = (tag, attrs = {}, text) => {
  const x = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) x.setAttribute(k, v);
  if (text !== undefined) x.textContent = text;
  return x;
};

const stampOf = (x) => ({ stamp: x.dataset.stamp ?? "", label: x.dataset.stampLabel ?? "", full: x.dataset.stampFull ?? "", scope: x.dataset.stampScope ?? "" });
const chip = (s, what) => {
  const c = chipFor(s, what);
  const b = el("button", { type: "button", class: "chip wb-source", "data-tip": c.tip, "data-source": s.label || s.scope || s.stamp }, c.text);
  return b;
};

function decorate() {
  if (!root || !plan) return;
  busy = true;
  try {
    root.classList.add("wb-design");
    const parts = [...root.querySelectorAll("section.kton-part")];
    const top = parts.filter((p) => !p.parentElement?.closest("section.kton-part"));
    // ---- banner + nav
    const banner = root.querySelector(".wb-banner");
    let bar = document.getElementById("wb-top");
    if (!bar) {
      bar = el("div", { id: "wb-top", class: "wb-sticky" });
      document.body.insertBefore(bar, document.body.firstChild);
    }
    if (banner && !bar.querySelector(".wb-banner")) bar.prepend(banner.cloneNode(true));
    if (banner) banner.closest("section.kton-part")?.classList.add("wb-banner-part");
    const navParts = top.flatMap((p) => {
      if (p.classList.contains("wb-banner-part")) return [];
      const group = !p.querySelector(":scope > .kton-block") && p.querySelector(":scope > .kton-layout:not(.kton-layout-tabs)");
      return group ? [...p.querySelector(":scope > .kton-layout").querySelectorAll(":scope > .kton-cell > section.kton-part")] : [p];
    });
    if (navParts.length >= 4) {
      let nav = bar.querySelector("nav.wb-nav");
      if (!nav) {
        nav = el("nav", { class: "wb-nav", "aria-label": "Sections" });
        bar.append(nav);
      }
      const want = navParts.map((p) => p.dataset.part).join("|");
      if (nav.dataset.parts !== want) {
        nav.textContent = "";
        nav.dataset.parts = want;
        for (const p of navParts) {
          const a = el("a", { href: `#wb-s-${p.dataset.part}`, "data-nav": p.dataset.part }, headingOf(p));
          a.addEventListener("click", (e) => {
            e.preventDefault();
            for (const x of nav.querySelectorAll("a")) x.classList.toggle("on", x === a);
            root.querySelector(`section.kton-part[data-part="${CSS.escape(p.dataset.part)}"]`)?.scrollIntoView({ block: "start" });
          });
          nav.append(a);
        }
      }
      for (const a of nav.querySelectorAll("a")) {
        const p = root.querySelector(`section.kton-part[data-part="${CSS.escape(a.dataset.nav)}"]`);
        const n = p?.querySelector("[data-cards-count]")?.dataset.cardsCount;
        let badge = a.querySelector(".badge");
        if (n && Number(n) > 0) {
          if (!badge) a.append((badge = el("span", { class: "badge count" })));
          badge.textContent = n;
        } else badge?.remove();
      }
    }
    document.documentElement.style.setProperty("--wb-top-h", `${bar.offsetHeight}px`);
    // ---- eyebrow: the package and the subject
    const eb = root.querySelector(".wb-head [data-eyebrow]");
    if (eb && !eb.dataset.done) {
      eb.dataset.done = "1";
      const comp = plan.compositions?.find((c) => c.key === "") ?? {};
      if (comp.package) eb.append(el("span", { "aria-hidden": "true" }, "·"), el("span", { class: "mono" }, comp.package));
      // a proposal chip only for a real claim (a view of a scope carries the zero claim)
      const claim = plan.subject?.claim ?? (/^https:\/\/kton\.dev\/o\/([0-9a-f]{64})$/.exec(plan.subject?.iri ?? "")?.[1] ? `sha256:${plan.subject.iri.slice(19)}` : "");
      if (claim && /^sha256:[0-9a-f]{64}$/.test(claim) && !/^sha256:0+$/.test(claim)) {
        eb.append(el("span", { "aria-hidden": "true" }, "·"));
        const c = el("button", { type: "button", class: "chip", "data-tip": `Proposal\n${fullHash(claim)}` }, "proposal ");
        c.append(el("span", { class: "mono" }, claim.replace(/^sha256:/, "").slice(0, 8)));
        eb.append(c);
      }
    }
    // ---- part heads
    for (const p of parts) {
      p.id ||= `wb-s-${p.dataset.part}`;
      const inTab = !!p.parentElement?.closest(".kton-layout-tabs");
      const h = p.querySelector(":scope > :is(h1,h2,h3,h4,h5,h6)");
      const isGroup = !p.querySelector(":scope > .kton-block") && !!p.querySelector(":scope > .kton-layout");
      if (isGroup) p.classList.add("wb-group");
      if (inTab || isGroup || p.classList.contains("wb-banner-part")) {
        if (inTab) p.classList.add("wb-tabpart");
        continue;
      }
      let head = p.querySelector(":scope > .wb-sec-head");
      if (!head && h) {
        head = el("div", { class: "wb-sec-head" });
        const left = el("div", { class: "wb-sec-title" });
        h.replaceWith(head);
        h.classList.add("wb-h");
        left.append(h);
        head.append(left);
      }
      if (!head) continue;
      const sub = p.querySelector(":scope > .kton-block .wb-sub");
      if (sub && !head.querySelector(".wb-sub")) {
        head.firstChild.append(sub);
        p.querySelector(":scope > .kton-block[data-use$='#sub']")?.classList.add("wb-moved");
      }
      // ---- source chips: every stamp in this part, its nested parts (tabs) included
      const blocks = [...p.querySelectorAll(".kton-block")].filter((b) => !b.classList.contains("wb-moved"));
      const list = blocks.map((b) => ({ id: b.dataset.use, b, stamps: [...b.querySelectorAll("[data-stamp]:not([data-stamp=''])")].filter((x) => !x.closest(".wb-banner")).map(stampOf) }));
      const r = sectionStamps(list);
      let sc = head.querySelector(":scope > .wb-source");
      const key = r.section ? r.section.label || r.section.scope || r.section.stamp : "";
      if (sc && sc.dataset.source !== key) (sc.remove(), (sc = null));
      if (r.section && !sc) head.append(chip(r.section));
      for (const x of list) {
        const own = r.blocks[x.id];
        for (const s of x.b.querySelectorAll("[data-stamp]")) {
          const k = s.dataset.stampLabel || s.dataset.stampScope || s.dataset.stamp;
          s.classList.toggle("wb-dup", !!key && k === key || (own && own !== "mixed" && k === (own.label || own.scope || own.stamp)));
        }
        let bc = x.b.querySelector(".wb-block-source");
        const bkey = own && own !== "mixed" ? own.label || own.scope || own.stamp : "";
        if (bc && bc.dataset.source !== bkey) (bc.remove(), (bc = null));
        if (bkey && !bc) {
          const c = chip(own, "Differs from the section: this block's rows come from here.");
          c.classList.add("wb-block-source");
          const t = x.b.querySelector("[data-block-title]");
          if (t) t.append(" ", c);
          else x.b.prepend(c);
        }
      }
    }
    // ---- tabs count their rows
    for (const t of root.querySelectorAll(".kton-tablist [role='tab']")) {
      const panel = document.getElementById(t.getAttribute("aria-controls"));
      const n = panel?.querySelector("[data-count]")?.dataset.count;
      let s = t.querySelector(".wb-count");
      if (n !== undefined) {
        if (!s) t.append(" ", (s = el("span", { class: "wb-count muted" })));
        s.textContent = n;
      }
    }
  } finally {
    busy = false;
  }
}

function headingOf(p) {
  const short = p.querySelector(":scope .wb-sub[data-nav-label]")?.dataset.navLabel;
  if (short) return short;
  return (p.querySelector(":scope > :is(h1,h2,h3,h4,h5,h6), :scope > .wb-sec-head .wb-h")?.textContent ?? p.dataset.part).trim();
}

const schedule = () => {
  if (busy || queued) return;
  queued = true;
  requestAnimationFrame(() => {
    queued = false;
    decorate();
  });
};
if (root) new MutationObserver(schedule).observe(root, { childList: true, subtree: true, attributes: true, attributeFilter: ["data-settled", "hidden"] });

// ---- tooltips: hover or keyboard focus; Escape hides
const tip = el("div", { id: "wb-tip", role: "tooltip" });
tip.hidden = true;
document.body.append(tip);
const place = (t) => {
  tip.textContent = t.getAttribute("data-tip");
  tip.hidden = false;
  const r = t.getBoundingClientRect();
  const w = tip.offsetWidth;
  const h = tip.offsetHeight;
  tip.style.left = `${Math.min(Math.max(8, r.left), innerWidth - w - 8)}px`;
  let y = r.bottom + 6;
  if (y + h > innerHeight - 8) y = r.top - h - 6;
  tip.style.top = `${Math.max(8, y)}px`;
};
const hide = () => (tip.hidden = true);
for (const [ev, fn] of [
  ["mouseover", (e) => e.target.closest?.("[data-tip]") && place(e.target.closest("[data-tip]"))],
  ["mouseout", (e) => e.target.closest?.("[data-tip]") && hide()],
  ["focusin", (e) => e.target.closest?.("[data-tip]") && place(e.target.closest("[data-tip]"))],
  ["focusout", hide],
]) document.addEventListener(ev, fn);
document.addEventListener("keydown", (e) => e.key === "Escape" && hide());
addEventListener("scroll", hide, { passive: true });
