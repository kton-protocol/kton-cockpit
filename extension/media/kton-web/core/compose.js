// core/compose.js - the composition renderer (C1.5 v3, VERTICLE-HOSTS.md §11-16). One renderer for every
// host: the browser, the VS Code webview (userinterface vendors this file at a pinned kton-web commit) and
// later the improve host. Taken over from userinterface's composition-proto renderer (round 5, 19.11.2026)
// with the same signature, so the prototype's frame swaps it in unchanged. Plain ES module, no build.
//
// - A viewer module (the plan's `module`, pinned by its folder's tree) exports mount(el, ctx). It imports
//   nothing: ctx.kit carries the helpers (escape, Markdown, term text, sha256, the sandboxed Mermaid frame).
//   ctx.use is its plan entry, ctx.state its state, ctx.setState(s) records a new state,
//   ctx.request(cap, args) -> Promise<result>, ctx.intent(i) -> Promise<{done, result} | {refused}>.
//   mount may return {set(options), destroy()}: set is the way in for options a var feeds.
// - Blocks never fetch or act. ctx.request goes to the host (only declared and provided capabilities);
//   ctx.intent bubbles up through every composition to the outermost host. A composition never acts.
// - Only DATA capabilities hide a block (query@1, product@1, graph@1, content@1): each part shows a
//   counted placeholder ("1 block hidden: needs graph@1"). Acts (open@1, run@1, record@1, sign@1) never hide
//   one: a host that cannot do an act answers refused, and the refusal is shown at the act.
// - State nests by part id: {parts: {<part>: {uses: {<use>: state}, parts: {…}}}}; plain JSON for a URL hash.
// - Vars: the composition's state also holds its vars (only differences from the defaults). A block's events
//   (viewer.json emits: state.<key> or intent.<kind>) write vars through the use's `emits`; a wired intent is
//   consumed (unless `up`), an unwired one goes up. After a change, one settle pass re-binds the plan
//   (bindVars) and re-asks only uses whose key changed; an option fed by a var is pushed to the block's set()
//   (the way in), never back to the block that emitted it. Answers are cached by key.
// - Acts are never wirable: an intent that signs, records, runs or posts always goes up to the host, even if
//   a (bad) plan names it in emits. Their outcomes come host-held (`acts`, from kton:set) and are only shown.
// - `empty: omit`: a query use that answers no rows is hidden (not counted: it is empty, not unmet).
// - An unnamed part (a level jump in the source) renders its children without a heading.
//
// Differences from the prototype renderer: the never-wirable guard above; an act button is disabled while
// its intent is out (a double click is not two signatures); loadModules takes the base the plan's module
// paths are relative to.

import { kit } from "./compose-kit.js";
import { bindVars, checkValue, varValuesOf, NEVER_WIRABLE, LAYOUT_TYPES, SIZE_RE } from "./ktonpkg-mirror/plan.js";

export const DATA_CAPS = ["query@1", "product@1", "graph@1", "content@1"];

/**
 * Load the viewer modules a plan names (once each). A module that does not load leaves its uses hidden.
 * `base`: what the plan's module paths are relative to (default: the directory above core/).
 */
export async function loadModules(plan, base = new URL("../", import.meta.url).href) {
  const mods = {};
  for (const path of new Set(plan.uses.map((u) => u.module))) {
    try {
      mods[path] = await import(new URL(path, base).href);
    } catch (e) {
      console.error(`viewer module ${path}: ${e.message}`);
    }
  }
  return mods;
}

export const MAX_SETTLE_PASSES = 8;

export function render(root, basePlan, host, initialState = {}, onState = () => {}, mods = {}, acts = {}) {
  const doc = root.ownerDocument;
  const state = structuredClone(initialState ?? {});
  const provides = new Set(host.provides ?? []);
  const comps = new Map(basePlan.compositions.map((c) => [c.key, c]));
  const children = new Map();
  for (const s of basePlan.sections) {
    const k = s.parent ?? "";
    if (!children.has(k)) children.set(k, []);
    children.get(k).push(s);
  }
  let pending = 0;
  let hiddenTotal = 0;
  const busy = () => {
    root.dataset.pending = String(pending);
    root.dataset.settled = String(pending === 0 && !scheduled);
  };

  const partState = (sectionKey, create) => {
    let node = state;
    for (const id of sectionKey ? sectionKey.split("/") : []) {
      if (!node.parts?.[id]) {
        if (!create) return undefined;
        (node.parts ??= {})[id] = {};
      }
      node = node.parts[id];
    }
    return node;
  };
  const useState = (u) => partState(u.section, false)?.uses?.[u.id];

  // ---- vars (round 5): values live in the state (root: state.vars; nested: state.parts.<…>.vars), bound by
  // the same pure bindVars the host uses to check a request.
  let plan = bindVars(basePlan, varValuesOf(basePlan, state));
  const writeVars = () => {
    for (const [ck, vals] of Object.entries(plan.varValues ?? {})) {
      const decls = plan.vars[ck];
      const diff = Object.fromEntries(Object.entries(vals).filter(([n, v]) => JSON.stringify(v) !== JSON.stringify(decls[n].default)));
      const node = partState(ck, Object.keys(diff).length > 0);
      if (!node) continue;
      if (Object.keys(diff).length) node.vars = diff; // only differences from the defaults go into the URL state
      else delete node.vars;
    }
  };
  const currentVars = () => structuredClone(plan.varValues ?? {});
  const recs = new Map(); // use key → {u, box, body, handle, section}
  const cache = new Map(); // ask key (+ refine) → Promise<answer>: a re-ask is a new key
  let scheduled = false;
  let turnEmitters = new Set(); // uses that emitted in this turn: not re-triggered by their own change
  let passes = 0;
  const stats = { settles: 0, passes: 0, remounts: 0, pushes: 0, emits: 0, consumed: 0 };
  const publishStats = () => (root.dataset.wiring = JSON.stringify(stats));

  function setVar(compKey, name, value, emitter) {
    const d = plan.vars?.[compKey]?.[name];
    if (!d || !checkValue(d.type, value)) return false;
    const values = currentVars();
    if (JSON.stringify(values[compKey]?.[name]) === JSON.stringify(value)) return false; // an echo: nothing changes
    (values[compKey] ??= {})[name] = value;
    plan = bindVars(basePlan, values);
    stats.emits++;
    if (emitter) turnEmitters.add(emitter);
    // a child composition publishes its declared events to its parent's vars
    for (const pub of plan.publishes ?? []) if (pub.from.comp === compKey && pub.from.var === name) setVar(pub.to.comp, pub.to.var, value, emitter);
    if (!scheduled) {
      scheduled = true;
      queueMicrotask(settle);
    }
    busy();
    return true;
  }

  /** One settle pass per turn (bounded): re-ask only uses whose key changed; push changed accepted options. */
  function settle() {
    passes++;
    stats.passes++;
    if (passes > MAX_SETTLE_PASSES) {
      console.warn(`wiring did not settle in ${MAX_SETTLE_PASSES} passes`);
      scheduled = false;
      return;
    }
    scheduled = false;
    const emitters = turnEmitters;
    turnEmitters = new Set();
    for (const [key, rec] of recs) {
      const next = plan.uses.find((x) => x.key === key);
      const prev = rec.u;
      if (next === prev) continue;
      const askChanged = next.ask !== prev.ask || JSON.stringify(next.waiting ?? []) !== JSON.stringify(prev.waiting ?? []);
      if (askChanged) {
        rec.u = next;
        mountBody(rec);
        stats.remounts++;
        continue;
      }
      const changed = Object.keys(next.bind?.options ?? {}).filter((k) => JSON.stringify(next.options[k]) !== JSON.stringify(prev.options[k]));
      rec.u = next;
      if (!changed.length || emitters.has(key)) continue; // the emitter is not re-triggered by its own change
      if (rec.handle?.set) {
        rec.handle.set(Object.fromEntries(changed.map((k) => [k, next.options[k]]))); // the way in: kton:set {state}
        stats.pushes++;
      } else {
        mountBody(rec);
        stats.remounts++;
      }
    }
    stats.settles++;
    if (!scheduled) passes = 0;
    writeVars();
    onState(structuredClone(state));
    publishStats();
    busy();
  }

  /** An intent goes up through every composition (each adds its name) to the host. */
  const bubble = (compKey, intent, from) => {
    const c = comps.get(compKey);
    const up = { ...intent, via: [...(intent.via ?? []), `${c.package}/${c.name}`] };
    return c.parent === null ? host.intent(up, from) : bubble(c.parent, up, from);
  };

  const el = (tag, attrs = {}, text) => {
    const e = doc.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) e.setAttribute(k, v);
    if (text !== undefined) e.textContent = text;
    return e;
  };

  function mountBody(rec) {
    const { u, box, section } = rec;
    const body = el("div", { class: "kton-block-body" });
    rec.body.replaceWith(body);
    rec.body = body;
    rec.handle = null;
    box.hidden = false;
    delete box.dataset.empty;
    if (u.waiting?.length) {
      box.dataset.waiting = u.waiting.join(" ");
      body.append(el("div", { class: "kton-muted kton-waiting" }, `Waiting for ${u.waiting.map((w) => `var.${w.split(":").pop()}`).join(", ")}.`));
      return;
    }
    delete box.dataset.waiting;
    const mod = mods[u.module];
    const ctx = {
      use: structuredClone(u),
      subject: basePlan.subject,
      kit,
      state: useState(u),
      setState: (s) => {
        const p = partState(u.section, true);
        (p.uses ??= {})[u.id] = s;
        // events from state (viewer.json from "state.<key>") write their vars
        for (const e of Object.values(u.emits ?? {})) if (e.from.startsWith("state.") && e.from.slice(6) in (s ?? {})) setVar(e.comp, e.var, s[e.from.slice(6)], u.key);
        if (!scheduled) onState(structuredClone(state));
      },
      request: async (cap, args = {}) => {
        if (!u.requires.includes(cap) && !u.optional.includes(cap)) throw new Error(`${u.block} does not declare ${cap}`);
        if (!provides.has(cap)) throw new Error(`this host does not provide ${cap}`);
        const full = { ...args, use: u.key, ...(u.bind ? { vars: currentVars() } : {}) };
        const ck = cap === "query@1" && u.ask ? `${u.ask}|${JSON.stringify(args.refine ?? {})}` : null;
        pending++;
        busy();
        try {
          let p = ck ? cache.get(ck) : null;
          if (!p) {
            p = host.request(cap, full, u.key);
            if (ck) cache.set(ck, p);
            p.catch(() => ck && cache.delete(ck));
          }
          const r = await p;
          if (cap === "query@1" && u.empty === "omit" && (r?.results?.bindings?.length ?? 0) === 0) {
            box.hidden = true;
            box.dataset.empty = "omitted";
          }
          return r;
        } finally {
          pending--;
          busy();
        }
      },
      intent: (i) => {
        // a wired intent (viewer.json from "intent.<kind>") writes its var and is consumed, unless the use says up
        const e = NEVER_WIRABLE.includes(i?.kind) ? null : Object.values(u.emits ?? {}).find((x) => x.from === `intent.${i.kind}`);
        if (e) {
          setVar(e.comp, e.var, i.value, u.key);
          if (!u.up) {
            stats.consumed++;
            publishStats();
            return Promise.resolve({ done: true, result: { wired: e.var } });
          }
        }
        return bubble(section.composition, { ...i, from: u.key }, u.key);
      },
    };
    try {
      rec.handle = mod.mount(body, ctx) ?? null;
    } catch (e) {
      body.textContent = `This block failed: ${e.message}`;
    }
  }

  function mountUse(u0, section, into, hidden) {
    const u = plan.uses.find((x) => x.key === u0.key);
    const mod = mods[u.module];
    const missing = u.requires.filter((c) => DATA_CAPS.includes(c) && !provides.has(c));
    if (missing.length) return hidden.push(`needs ${missing.join(", ")}`);
    if (!mod) return hidden.push(`no viewer ${u.viewer}`);
    const box = el("div", { class: "kton-block", "data-use": u.key, "data-block": u.block });
    into.append(box);
    const body = el("div", { class: "kton-block-body" });
    box.append(body);
    const rec = { u, box, body, handle: null, section };
    recs.set(u.key, rec);
    mountBody(rec);
    for (const act of u.acts ?? []) {
      const bar = el("div", { class: "kton-act" });
      const b = el("button", { type: "button", "data-intent": act.intent }, act.label);
      const note = el("span", { class: "kton-act-note", role: "status" });
      // a host-held outcome (the frame may have reloaded meanwhile): shown, never written to the state
      const held = Object.values(acts).filter((a) => a.use === u.key && a.intent === act.intent).pop();
      if (held) {
        note.textContent = held.status === "refused" ? `Refused: ${held.result?.why ?? ""}` : held.result?.message ?? "Done.";
        note.dataset.state = held.status;
        note.dataset.held = "true";
      }
      b.addEventListener("click", async () => {
        if (b.disabled) return;
        b.disabled = true;
        note.textContent = "Sent to the host.";
        note.dataset.state = "sent";
        const r = await bubble(section.composition, { kind: act.intent, about: act.about, fields: act.fields, ...(act.control ? { control: act.control } : {}), ...(act.ray ? { ray: act.ray } : {}), from: u.key }, u.key);
        if (r?.refused) {
          note.textContent = `Refused: ${r.refused.why}`;
          note.dataset.state = "refused";
        } else if (r?.done) {
          note.textContent = r.result?.message ?? "Done.";
          note.dataset.state = "done";
        }
        b.disabled = false;
      });
      bar.append(b, note);
      box.append(bar);
    }
  }

  function renderSection(s, into) {
    if (s.unnamed) {
      const wrap = el("div", { class: "kton-unnamed", "data-part": s.key });
      renderChildren(s.key, s.composition, wrap);
      return into.append(wrap);
    }
    const sec = el("section", { class: "kton-part", "data-part": s.key, "data-level": String(s.level) });
    sec.append(el(`h${Math.min(s.level, 6)}`, {}, s.heading));
    if (s.rules.length) sec.append(el("div", { class: "kton-rules" }, `Rules: ${s.rules.join(", ")}`));
    const hidden = [];
    for (const k of s.uses) mountUse({ key: k }, s, sec, hidden);
    if (hidden.length) {
      hiddenTotal += hidden.length;
      const why = [...new Set(hidden)].join("; ");
      sec.append(el("div", { class: "kton-hidden", "data-hidden": String(hidden.length) }, `${hidden.length} block${hidden.length > 1 ? "s" : ""} hidden: ${why}`));
    }
    renderChildren(s.key, s.composition, sec);
    into.append(sec);
  }

  // ---- layout (VERTICLE-HOSTS §17, kton-web review 19.11.) -------------------------------------------
  // A composition's top parts, or a group's parts, are placed by the plan's layout. Everything is set
  // through the CSSOM (el.style.gridTemplateAreas = …), never as a style attribute or a <style>: the
  // host's CSP governs neither, so it needs no nonce. The plan is untrusted here as well: sizes are
  // re-checked against the closed set and areas against the parts, and anything off draws as stack.
  // Each part goes into a cell that persists, so going narrow (measured on the holder's own box) only
  // moves cells. Tabs build a cell's blocks on first show. The open tab and a split's ratio are view state
  // at <node>.layout = {tab, ratio}, defaults omitted, checked on the way back in.
  const narrowAt = Number.isFinite(host.narrow) && host.narrow > 0 ? host.narrow : 640;
  const reportedKinds = new Set(["report", "deck"]);
  const validLayout = (lay, pids) => {
    if (!lay || lay.type === "stack" || !LAYOUT_TYPES.includes(lay.type)) return null;
    const sizesOk = (v) => Array.isArray(v) && v.length > 0 && v.every((x) => typeof x === "string" && SIZE_RE.test(x));
    if (["columns", "grid"].includes(lay.type) && !sizesOk(lay.columns)) return null;
    if (lay.rows !== undefined && !sizesOk(lay.rows)) return null;
    if (lay.type === "grid") {
      const a = lay.areas;
      if (!Array.isArray(a) || !a.every((r) => Array.isArray(r) && r.length === lay.columns.length && r.every((x) => x === "." || pids.includes(x)))) return null;
      if (!pids.every((id) => a.flat().includes(id))) return null;
    }
    if (lay.type === "split" && pids.length !== 2) return null;
    return lay;
  };
  const layoutNode = (key, create) => partState(key, create);
  function readLayoutState(key, lay, pids) {
    const st = layoutNode(key, false)?.layout ?? {};
    const tab0 = pids.includes(lay.active) ? lay.active : pids[0];
    const ratio0 = typeof lay.ratio === "number" && lay.ratio > 0 && lay.ratio < 1 ? lay.ratio : 0.5;
    return {
      tab: pids.includes(st.tab) ? st.tab : tab0,
      ratio: typeof st.ratio === "number" && st.ratio > 0 && st.ratio < 1 ? st.ratio : ratio0,
    };
  }
  function writeLayoutState(key, lay, pids, cur) {
    const diff = {};
    if (cur.tab !== (pids.includes(lay.active) ? lay.active : pids[0])) diff.tab = cur.tab;
    if (cur.ratio !== (lay.ratio ?? 0.5)) diff.ratio = cur.ratio;
    const node = layoutNode(key, Object.keys(diff).length > 0);
    if (!node) return;
    if (Object.keys(diff).length) node.layout = diff;
    else delete node.layout;
    if (!scheduled) onState(structuredClone(state));
  }

  /** Place sections (in plan order) into `into` by `lay`; `key` is the node its view state lives at. */
  function arrange(into, secs, lay0, key, kind) {
    const items = [];
    for (const s of secs) {
      const last = items[items.length - 1];
      if (last && last.pid === (s.pid ?? s.id)) last.secs.push(s); // the rows of one `every` share a cell
      else items.push({ pid: s.pid ?? s.id, heading: s.heading, secs: [s] });
    }
    const pids = items.map((i) => i.pid);
    const lay = reportedKinds.has(kind) ? null : validLayout(lay0, pids);
    if (!lay) {
      for (const s of secs) renderSection(s, into);
      return;
    }
    const box = el("div", { class: `kton-layout kton-layout-${lay.type}`, "data-layout": lay.type });
    into.append(box);
    const cur = readLayoutState(key, lay, pids);
    const cells = items.map((it, i) => {
      const cell = el("div", { class: "kton-cell", "data-cell": it.pid });
      return { ...it, i, cell, built: false };
    });
    const build = (c) => {
      if (c.built) return;
      c.built = true;
      for (const s of c.secs) renderSection(s, c.cell);
      root.dataset.hidden = String(hiddenTotal);
    };
    let mode = null;
    const clear = () => {
      box.textContent = "";
      for (const p of ["gridTemplateColumns", "gridTemplateRows", "gridTemplateAreas"]) box.style[p] = "";
      for (const c of cells) {
        c.cell.hidden = false;
        c.cell.style.gridArea = "";
        c.cell.removeAttribute("role");
        c.cell.removeAttribute("aria-labelledby");
      }
    };
    const tabsOf = () => {
      const bar = el("div", { class: "kton-tablist", role: "tablist" });
      const show = (pid, focus) => {
        cur.tab = pid;
        for (const c of cells) {
          const on = c.pid === pid;
          c.cell.hidden = !on;
          const b = bar.querySelector(`[data-tab="${CSS.escape(c.pid)}"]`);
          b.setAttribute("aria-selected", String(on));
          b.tabIndex = on ? 0 : -1;
          if (on) {
            build(c);
            if (focus) b.focus();
          }
        }
        box.dataset.tab = pid;
      };
      cells.forEach((c, i) => {
        const id = `kton-tab-${key || "root"}-${i}`.replace(/[^\w-]/g, "_");
        const b = el("button", { type: "button", role: "tab", id, "data-tab": c.pid, "aria-controls": `${id}-panel` }, c.heading || c.pid);
        c.cell.id = `${id}-panel`;
        c.cell.setAttribute("role", "tabpanel");
        c.cell.setAttribute("aria-labelledby", id);
        b.addEventListener("click", () => {
          show(c.pid, false);
          writeLayoutState(key, lay, pids, cur);
        });
        b.addEventListener("keydown", (e) => {
          const d = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 }[e.key] ?? (e.key === "Home" ? -Infinity : e.key === "End" ? Infinity : 0);
          if (!d) return;
          e.preventDefault();
          const j = Math.max(0, Math.min(cells.length - 1, d === Infinity ? cells.length - 1 : d === -Infinity ? 0 : c.i + d));
          show(cells[j].pid, true);
          writeLayoutState(key, lay, pids, cur);
        });
        bar.append(b);
      });
      box.append(bar, ...cells.map((c) => c.cell));
      show(cur.tab, false);
    };
    const draw = (m) => {
      if (m === mode) return;
      mode = m;
      clear();
      box.dataset.mode = m;
      box.className = `kton-layout kton-layout-${m === "wide" ? lay.type : lay.narrow ?? "stack"}${m === "narrow" ? " kton-narrow" : ""}`;
      const t = m === "wide" ? lay.type : lay.narrow ?? "stack";
      if (t === "tabs") return tabsOf();
      for (const c of cells) build(c);
      if (t === "stack") return box.append(...cells.map((c) => c.cell));
      if (t === "columns") {
        box.style.gridTemplateColumns = lay.columns.join(" ");
        return box.append(...cells.map((c) => c.cell));
      }
      if (t === "grid") {
        const name = new Map(cells.map((c, i) => [c.pid, `p${i}`])); // part ids need not be CSS identifiers
        box.style.gridTemplateColumns = lay.columns.join(" ");
        if (lay.rows) box.style.gridTemplateRows = lay.rows.join(" ");
        box.style.gridTemplateAreas = lay.areas.map((r) => `"${r.map((x) => (x === "." ? "." : name.get(x))).join(" ")}"`).join(" ");
        for (const c of cells) c.cell.style.gridArea = name.get(c.pid);
        return box.append(...cells.map((c) => c.cell));
      }
      if (t === "split") {
        const col = lay.direction === "column";
        const bar = el("div", { class: "kton-divider", role: "separator", tabindex: "0", "aria-orientation": col ? "horizontal" : "vertical", "aria-valuemin": "10", "aria-valuemax": "90" });
        // a declared or returned ratio is anything in (0, 1); dragging and keys keep to 0.1-0.9
        const set = (r, free) => {
          cur.ratio = Math.round((free ? r : Math.max(0.1, Math.min(0.9, r))) * 100) / 100;
          box.style[col ? "gridTemplateRows" : "gridTemplateColumns"] = `${cur.ratio}fr 6px ${Math.round((1 - cur.ratio) * 100) / 100}fr`;
          bar.setAttribute("aria-valuenow", String(Math.round(cur.ratio * 100)));
          box.dataset.ratio = String(cur.ratio);
        };
        set(cur.ratio, true);
        bar.addEventListener("pointerdown", (e) => {
          bar.setPointerCapture(e.pointerId);
          const move = (ev) => {
            const r = box.getBoundingClientRect();
            set(col ? (ev.clientY - r.top) / r.height : (ev.clientX - r.left) / r.width);
          };
          const up = () => {
            bar.removeEventListener("pointermove", move);
            bar.removeEventListener("pointerup", up);
            writeLayoutState(key, lay, pids, cur); // reported on release, not on every move
          };
          bar.addEventListener("pointermove", move);
          bar.addEventListener("pointerup", up);
        });
        bar.addEventListener("keydown", (e) => {
          const d = { ArrowLeft: -0.05, ArrowUp: -0.05, ArrowRight: 0.05, ArrowDown: 0.05 }[e.key];
          if (!d) return;
          e.preventDefault();
          set(cur.ratio + d);
          writeLayoutState(key, lay, pids, cur);
        });
        return box.append(cells[0].cell, bar, cells[1].cell);
      }
    };
    draw("wide");
    // only a layout that says what narrow means (layout.narrow) changes below the threshold
    if (lay.narrow) {
      const fit = () => {
        const w = into.getBoundingClientRect().width;
        draw(w > 0 && w < narrowAt ? "narrow" : "wide");
      };
      if (typeof ResizeObserver === "function") new ResizeObserver(fit).observe(into);
      queueMicrotask(fit);
    }
    // print as stack: every cell built and shown
    globalThis.addEventListener?.("beforeprint", () => {
      for (const c of cells) build(c);
    });
  }

  function renderChildren(parentKey, comp, into) {
    const kids = children.get(parentKey) ?? [];
    const own = kids.filter((c) => c.composition === comp);
    const nested = kids.filter((c) => c.composition !== comp);
    const holder = parentKey ? basePlan.sections.find((x) => x.key === parentKey) : null;
    if (own.length) arrange(into, own, parentKey === "" ? comps.get("")?.layout : holder?.layout, parentKey, comps.get(comp)?.kind);
    if (nested.length) {
      const cc = comps.get(nested[0].composition);
      const wrap = el("div", { class: "kton-composition", "data-composition": `${cc.package}/${cc.name}`, "data-kind": cc.kind, "data-key": cc.key });
      into.append(wrap);
      arrange(wrap, nested, cc.layout, cc.key, cc.kind);
    }
  }

  root.textContent = "";
  const top = comps.get("");
  const rootWrap = el("div", { class: "kton-composition", "data-composition": `${top.package}/${top.name}`, "data-kind": top.kind, "data-key": "" });
  root.append(rootWrap);
  renderChildren("", "", rootWrap);
  root.dataset.sections = String(basePlan.sections.filter((s) => !s.unnamed).length);
  root.dataset.hidden = String(hiddenTotal);
  root.dataset.subject = basePlan.subject.identifier ?? basePlan.subject.iri;
  root.dataset.composition = `${basePlan.root.package}/${basePlan.root.composition}`;
  writeVars();
  publishStats();
  busy();
  return { state: () => structuredClone(state) };
}

/** State ↔ URL hash (`#state=<json>`), kton-web's one-state rule applied to the nested state. */
export function stateToHash(state) {
  return `#state=${encodeURIComponent(JSON.stringify(state))}`;
}
/** The URL state budget (kton-web 5): the hash, vars included, stays within it; above it the host holds the state. */
export const URL_STATE_BUDGET = 2048;
export const fitsUrl = (state) => stateToHash(state).length <= URL_STATE_BUDGET;
export function stateFromHash(hash) {
  const m = /^#state=(.*)$/.exec(hash ?? "");
  if (!m) return {};
  try {
    return JSON.parse(decodeURIComponent(m[1]));
  } catch {
    return {};
  }
}
