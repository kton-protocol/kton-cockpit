// The story of one file on one page: a line per script or session (what went in, what came out,
// what ran), hand edits as lines of their own, and what kton already has greyed out. Details only
// on request. Data comes from the extension (ktonwatch's /api/story plus what kton holds).
(function () {
  const vscode = acquireVsCodeApi();
  const root = document.getElementById("root");
  let S = null, published = {}, sameResult = {}, notes = {}, cockpitRepo = false, open = {};

  const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"}[c]));
  const clock = (ns) => new Date(ns / 1e6).toLocaleTimeString(undefined, {hour: "2-digit", minute: "2-digit"});
  const day = (ns) => new Date(ns / 1e6).toLocaleDateString(undefined, {weekday: "short", day: "2-digit", month: "2-digit"});
  const skey = (s) => s.session + "/" + s.task;
  const base = (p) => p.split("/").pop();
  const short = (p) => (p.startsWith("/") ? base(p) : p);
  const noteOf = (k) => { const n = notes[k]; return typeof n === "string" ? {description: n, rationale: ""} : {description: (n && n.description) || "", rationale: (n && n.rationale) || ""}; };
  const relp = (p) => (p.startsWith(S.repo + "/") ? p.slice(S.repo.length + 1) : p);
  const targetRel = () => relp(S.target.path);

  // chapters: consecutive steps of the same script (or console) in the same session
  function chapters() {
    const out = [];
    for (const s of S.steps) {
      const where = s.where.replace(/^chunk \S+ of /, "");
      const last = out[out.length - 1];
      if (last && !s.hand && !last.hand && last.session === s.session && last.where === where) last.steps.push(s);
      else out.push({session: s.session, where, hand: !!s.hand, steps: [s]});
    }
    for (const c of out) {
      c.key = c.hand ? skey(c.steps[0]) : c.session + "/" + c.where;
      c.path = c.steps.filter((s) => s.on_path);
      c.when = c.steps[0].when_ns;
    }
    // files: what each chapter took in that it did not make itself, and what it handed on
    const target = targetRel();
    out.forEach((c, i) => {
      const made = new Set(), ins = [], outs = [];
      for (const s of c.path) {
        for (const r of s.reads || []) if (!made.has(r) && !ins.includes(r)) ins.push(r);
        for (const w of s.writes || []) made.add(w);
      }
      const later = new Set(out.slice(i + 1).flatMap((d) => (d.hand ? d.steps[0].reads : d.path.flatMap((s) => s.reads || []))));
      for (const w of made) if ((later.has(w) || w === target) && !outs.includes(w)) outs.push(w);
      c.ins = ins; c.outs = outs;
      c.tools = [...new Set(c.path.flatMap((s) => s.tools || []))].filter((t) => t !== "Rscript");
      c.warn = [...new Set(c.path.flatMap((s) => (s.warnings || []).filter((w) => !/ran but could not/.test(w))))];
      c.failed = c.steps.filter((s) => s.error).length;
      // what the latest reproduction gave for the chapter's steps; a deviation is a warning
      const rs = c.path.map((s) => s.repro).filter(Boolean);
      c.repro = rs.length ? {when: rs[0].when, same: rs.every((r) => r.same),
        different: [...new Set(rs.flatMap((r) => r.different || []))], notProduced: [...new Set(rs.flatMap((r) => r.not_produced || []))]} : null;
      // the deviating outputs of the latest reproduction, with what a diff needs
      const last = (S.reproductions || [])[0];
      const mine = new Set(c.path.map(skey));
      c.devs = last ? (last.steps || []).filter((s) => mine.has(s.session + "/" + s.task))
        .flatMap((s) => (s.outputs || []).filter((o) => !o.same).map((o) => ({...o, dir: last.dir}))) : [];
      if (c.repro) for (const f of c.repro.different) c.warn.push(`${f} came out with other bytes in the reproduction`);
      if (c.repro) for (const f of c.repro.notProduced) c.warn.push(`${f} was not produced in the reproduction`);
      // a file is in kton when the step that wrote it is; the line when all it handed on is
      c.kton = new Set(c.path.filter((s) => published[skey(s)]).flatMap((s) => s.writes || []));
      c.inKton = c.outs.length > 0 && c.outs.every((w) => c.kton.has(w));
      // the same result is in kton, but computed from other inputs: this run of it is not
      c.sameResult = !c.inKton && c.path.some((s) => sameResult[skey(s)]) &&
        c.path.filter((s) => (s.writes || []).length).every((s) => published[skey(s)] || sameResult[skey(s)]);
      c.kton.all = c.inKton;
    });
    return out.filter((c) => c.hand || c.path.length);
  }

  const files = (xs, max, kton) => {
    max = max || 4;
    const shown = xs.slice(0, max).map((x) => `<span class="f" title="${esc(x)}${kton && kton.has(x) ? " – already in kton" : ""}">${esc(short(x))}${kton && kton.has(x) && !kton.all ? " ✓" : ""}</span>`).join("");
    return shown + (xs.length > max ? `<span class="more" title="${esc(xs.slice(max).join("\n"))}">+${xs.length - max}</span>` : "");
  };

  function headline(cs) {
    const v = S.verdict, hands = cs.filter((c) => c.hand).length;
    const scripts = new Set(cs.filter((c) => !c.hand && c.where.startsWith("script ")).map((c) => c.where)).size;
    const consoles = cs.filter((c) => c.where === "console").length;
    const shell = cs.filter((c) => c.where.startsWith("$ ")).length;
    const parts = [];
    if (scripts) parts.push(`${scripts} script${scripts > 1 ? "s" : ""}`);
    if (consoles) parts.push(`${consoles}× console`);
    if (shell) parts.push(`${shell} command${shell > 1 ? "s" : ""} in the shell`);
    if (hands) parts.push(`${hands} change${hands > 1 ? "s" : ""} by hand`);
    const how = {
      "scripts": `Recomputable with <code>${esc(v.command || "")}</code>.`,
      "scripts-with-gaps": hands ? "Recomputable except for the change by hand." : "Recomputable, with gaps.",
      "scripts-need-session": "The script needs what was in the session before.",
      "interactive": "Made in the console, without a script.",
    }[v.kind] || "";
    return `${parts.join(", ")}. ${how}`;
  }

  function line(c) {
    const note = noteOf(c.key);
    const field = (f, ph) => `<div class="note ${note[f] ? "" : "empty"}" contenteditable="true" data-k="${esc(c.key)}" data-f="${f}" data-ph="${ph}">${esc(note[f])}</div>`;
    // the dot: blue, green when in kton, orange for a hand edit, the warning colour when the step
    // has a warning (a file it used was changed or overwritten since), also over green
    const cls = [c.hand ? "hand" : c.inKton ? "done" : "", c.warn.length ? "warned" : ""].filter(Boolean).join(" ");
    const what = c.hand
      ? `<span class="f w">${esc(c.steps[0].reads[0])}</span> changed by hand <span class="muted">· not recorded</span>`
      : `${files(c.ins)} <span class="arrow">→</span>${c.tools.length ? ` <span class="tool">${esc(c.tools.join(", "))}</span> <span class="arrow">→</span>` : ""} ${c.outs.length ? files(c.outs, 4, c.kton) : '<span class="muted" title="this step wrote no file that a later step of this story read: whatever it handed on stayed in the R session (objects), or nothing at all">no files handed on</span>'}`;
    const rp = c.repro;
    const flags = [c.inKton ? '<span class="ok" title="already in kton">✓ in kton</span>' : "",
      c.sameResult ? '<span class="same" title="kton has these output bytes, but from a run with other inputs: the change upstream did not reach this result. This run is not in kton yet.">= same result as in kton</span>' : "",
      rp ? (rp.same ? `<span class="ok" title="the reproduction of ${esc(stamp(rp.when))} gave the same bytes">✓ reproduced</span>`
        : `<span class="warn" title="reproduction of ${esc(stamp(rp.when))}">≠ ${esc([...rp.different.map(short), ...rp.notProduced.map((f) => short(f) + " missing")].join(", "))}</span>`) : "",
      c.warn.length ? `<span class="warn" title="${esc(c.warn.join("\n"))}">⚠ ${c.warn.length}</span>` : "",
      c.failed ? `<span class="muted" title="failed commands, without effect">${c.failed}× failed</span>` : ""].filter(Boolean).join(" ");
    const details = open[c.key] ? `<div class="details">
        ${c.warn.map((w) => `<div class="warn">⚠ ${esc(w)}</div>`).join("")}
        ${c.steps.map((s) => `<div class="cmd ${s.on_path ? "" : "off"}"><span class="when">${clock(s.when_ns)}</span> <code>${esc((s.code || s.sentence).split("\n")[0])}</code>${(s.writes || []).length ? ` → ${files(s.writes, 3)}` : ""}${s.error ? ' <span class="warn">fails</span>' : ""}</div>`).join("")}
      </div>` : "";
    // deviating outputs, each with a diff when both versions exist
    const devs = (c.devs || []).length ? `<div class="devs">${c.devs.map((o, i) => `<div class="dev"><span class="warn">≠</span> <span class="f">${esc(relp(o.path))}</span>
      ${o.reproduced ? `<span class="muted">${esc(o.recorded.slice(0, 8))} → ${esc(o.reproduced.slice(0, 8))}</span> <button class="link diff" data-k="${esc(c.key)}" data-i="${i}">open diff</button>` : '<span class="muted">not produced</span>'}</div>`).join("")}</div>` : "";
    return `<li class="${cls}" data-k="${esc(c.key)}">
      <div class="t">${c.hand ? "✎" : clock(c.when)}</div>
      <div class="body">
        <div class="head">${c.where.startsWith("$ ") ? `<code class="cmdline">${esc(c.where)}</code>` : `<b>${esc(c.hand ? "by hand" : c.where.replace(/^script /, ""))}</b>`} ${what} ${flags}
          ${c.hand ? "" : `<button class="link tog">${open[c.key] ? "less" : `${c.steps.length} commands`}</button>`}</div>
        ${devs}
        ${details}
        <div class="notes ${c.inKton && !note.description && !note.rationale ? "quiet" : ""}">
          <span class="nl">Description</span>${field("description", c.hand ? "What did you change?" : "What was done here?")}
          <span class="nl">Rationale</span>${field("rationale", c.hand ? "Why was the change needed?" : "Why this way?")}
        </div>
      </div></li>`;
  }

  // a reproduction is an event of the story: its own line, at its date, after the steps
  const stamp = (iso) => new Date(iso).toLocaleString(undefined, {day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit"});
  function reproLine(r) {
    const bad = (r.steps || []).filter((s) => (s.outputs || []).some((o) => !o.same)).length;
    const what = r.same ? `the same bytes${bad ? `, though ${bad} step${bad > 1 ? "s" : ""} on the way deviated` : ""}`
      : r.reproduced ? `other bytes (${esc(r.reproduced.slice(0, 12))} instead of ${esc(r.recorded.slice(0, 12))}), ${bad} step${bad === 1 ? "" : "s"} deviated`
      : "not produced";
    return `<li class="${r.same && !bad ? "repro" : "repro warned"}">
      <div class="t">${esc(stamp(r.when))}</div>
      <div class="body"><div class="head"><b>Reproduced</b> in an empty folder: ${what} <span class="muted">· ${r.seconds} s</span></div></div></li>`;
  }

  function markdown(cs) {
    let md = `# How ${targetRel()} came about\n\n${headline(cs).replace(/<\/?code>/g, "`")}\n\n`;
    for (const ps of chains()) md += `In ${ps[0].levels.map((l) => l.name).join(" › ")}, from ${ps.map((p) => short(p.path) + " (" + p.foton + ")").join(", ")}\n\n`;
    for (const c of cs) {
      md += c.hand ? `- ✎ **${c.steps[0].reads[0]} changed by hand** (not recorded)`
        : `- ${clock(c.when)} **${c.where.replace(/^script /, "")}**: ${c.ins.join(", ")} → ${c.tools.length ? c.tools.join(", ") + " → " : ""}${c.outs.join(", ") || "–"}${c.inKton ? " ✓ in kton" : c.sameResult ? " = same result as in kton, from other inputs" : ""}`;
      const n = noteOf(c.key);
      md += "\n";
      if (n.description) md += `  - *Description:* ${n.description.replace(/\n/g, "\n    ")}\n`;
      if (n.rationale) md += `  - *Rationale:* ${n.rationale.replace(/\n/g, "\n    ")}\n`;
    }
    for (const r of (S.reproductions || []).slice(0, 1)) {
      md += `- ${stamp(r.when)} **Reproduced** in an empty folder: ${r.same ? "the same bytes" : r.reproduced ? "other bytes" : "not produced"}\n`;
      for (const s of r.steps || []) for (const o of s.outputs || []) if (!o.same) md += `  - ${short(o.path)}: ${o.reproduced ? "other bytes" : "not produced"}\n`;
    }
    if ((S.gaps || []).length) md += `\n**Gaps:**\n${S.gaps.map((g) => "- " + g).join("\n")}\n`;
    return md;
  }

  // where the story's sources reside: per scope chain the sources whose bytes a federated
  // repository published (innermost first); the rest folded into one line
  function chains() {
    const by = {};
    for (const p of S.context || []) if ((p.levels || []).length) (by[p.levels.map((l) => l.name).join(" › ")] = by[p.levels.map((l) => l.name).join(" › ")] || []).push(p);
    return Object.values(by);
  }
  function contextLine() {
    const ctx = S.context || [], ch = chains(), others = ctx.filter((p) => !(p.levels || []).length);
    if (!ctx.length) return "";
    const rows = ch.map((ps) => `<div class="ctx"><span class="muted">in</span> ${ps[0].levels.map((l) =>
      `<span class="lvl" title="scope ${esc(l.scope)}, seed found in ${esc(l.source)}">${esc(l.name)}</span>`).join(' <span class="muted">›</span> ')}
      <span class="muted">· from ${ps.map((p) => `<span title="published by ${esc(p.published_by)} as ${esc(p.foton)}">${esc(short(p.path))}</span>`).join(", ")}</span></div>`);
    if (others.length) rows.push(`<div class="ctx muted" title="${esc(others.map((p) => short(p.path) + ": " + (p.note || "")).join("\n"))}">${ch.length ? "other sources" : "sources"}: ${others.length} not placed in a context</div>`);
    return rows.join("");
  }

  function render() {
    const cs = chapters();
    const days = new Set(cs.map((c) => day(c.when)));
    const todo = cs.filter((c) => !c.hand && !c.inKton && c.path.some((s) => (s.writes || []).length));
    root.innerHTML = `<main class="one">
      <header>
        <h1>${esc(targetRel())}</h1>
        <div class="sub">${headline(cs)} <span class="muted">${[...days].join(" – ")}</span></div>
        ${contextLine()}
        <div class="actions">
          <button id="repro" title="source files and scripts into an empty folder, run there, compare the hash">Reproduce</button>
          <button id="publish" ${!cockpitRepo || !todo.length ? "disabled" : ""} title="${cockpitRepo ? "cockpit publish for every line without ✓, in the terminal, with description and rationale as comments" : "no cockpit repo (cockpit init)"}">Publish to kton${todo.length ? ` (${todo.length})` : ""}</button>
          <button class="secondary" id="inkton" ${cockpitRepo ? "" : "disabled"} title="${cockpitRepo ? "kton-web beside the story: what was recorded and what is signed, in one graph, centred on this file (kton-web verifies the records)" : "no cockpit repo (cockpit init)"}">In kton</button>
          <button class="secondary" id="save">Markdown</button>
        </div>
      </header>
      <ol class="story">${cs.map(line).join("")}${(S.reproductions || []).slice(0, 1).map(reproLine).join("")}</ol>
      ${(S.gaps || []).length ? `<details class="gaps"><summary>${S.gaps.length} gap${S.gaps.length > 1 ? "s" : ""} in the recording</summary>${S.gaps.map((g) => `<div class="warn">${esc(g)}</div>`).join("")}</details>` : ""}
    </main>`;
    for (const b of root.querySelectorAll("button.diff")) b.onclick = () => {
      const c = cs.find((x) => x.key === b.dataset.k), o = c && c.devs[+b.dataset.i];
      if (o) vscode.postMessage({type: "diff", path: o.path, recorded: o.recorded, reproduced: o.reproduced, dir: o.dir});
    };
    for (const b of root.querySelectorAll("button.tog")) b.onclick = (e) => { const k = e.target.closest("li").dataset.k; open[k] = !open[k]; render(); };
    for (const n of root.querySelectorAll(".note")) {
      n.oninput = () => {
        n.classList.toggle("empty", !n.textContent.trim());
        notes[n.dataset.k] = Object.assign(noteOf(n.dataset.k), {[n.dataset.f]: n.innerText.trim()});
        clearTimeout(n._t); n._t = setTimeout(() => vscode.postMessage({type: "note", key: n.dataset.k, note: notes[n.dataset.k]}), 400);
      };
    }
    document.getElementById("repro").onclick = () => vscode.postMessage({type: "reproduce"});
    document.getElementById("save").onclick = () => vscode.postMessage({type: "save", markdown: markdown(cs)});
    document.getElementById("inkton").onclick = () => vscode.postMessage({type: "inkton"});
    document.getElementById("publish").onclick = () => {
      const steps = [];
      for (const c of cs) {
        if (c.inKton) continue;
        if (c.hand) { steps.push({hand: true, sentence: c.steps[0].sentence, note: noteOf(c.key)}); continue; }
        c.path.filter((s) => (s.writes || []).length && !published[skey(s)]).forEach((s, i) =>
          steps.push({session: s.session, task: s.task, where: s.where, sentence: s.sentence, note: i === 0 ? noteOf(c.key) : null}));
      }
      vscode.postMessage({type: "publish", steps});
    };
  }

  window.addEventListener("message", (e) => {
    const m = e.data;
    if (m.type === "story") { S = m.story; published = m.published || {}; sameResult = m.sameResult || {}; notes = m.notes || {}; cockpitRepo = m.cockpitRepo; render(); }
    if (m.type === "published") { published = m.published || {}; sameResult = m.sameResult || {}; render(); }
  });
  vscode.postMessage({type: "ready"});
})();
