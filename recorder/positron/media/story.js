// The story of one file on one page: a line per script or session (what went in, what came out,
// what ran), hand edits as lines of their own, and what kton already has greyed out. Details only
// on request. Data comes from the extension (ktonwatch's /api/story plus what kton holds).
(function () {
  const vscode = acquireVsCodeApi();
  const root = document.getElementById("root");
  let S = null, published = {}, notes = {}, cockpitRepo = false, open = {};

  const esc = (s) => String(s ?? "").replace(/[&<>"]/g, (c) => ({"&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;"}[c]));
  const clock = (ns) => new Date(ns / 1e6).toLocaleTimeString("de-AT", {hour: "2-digit", minute: "2-digit"});
  const day = (ns) => new Date(ns / 1e6).toLocaleDateString("de-AT", {weekday: "short", day: "2-digit", month: "2-digit"});
  const skey = (s) => s.session + "/" + s.task;
  const base = (p) => p.split("/").pop();
  const short = (p) => (p.startsWith("/") ? base(p) : p);
  const noteOf = (k) => { const n = notes[k]; return typeof n === "string" ? {description: n, rationale: ""} : {description: (n && n.description) || "", rationale: (n && n.rationale) || ""}; };
  const targetRel = () => (S.target.path.startsWith(S.repo + "/") ? S.target.path.slice(S.repo.length + 1) : S.target.path);

  // chapters: consecutive steps of the same script (or console) in the same session
  function chapters() {
    const out = [];
    for (const s of S.steps) {
      const where = s.where.replace(/^Chunk \S+ von /, "");
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
      c.warn = [...new Set(c.path.flatMap((s) => (s.warnings || []).filter((w) => !/lief, konnte aber nicht/.test(w))))];
      c.failed = c.steps.filter((s) => s.error).length;
      // a file is in kton when the step that wrote it is; the line when all it handed on is
      c.kton = new Set(c.path.filter((s) => published[skey(s)]).flatMap((s) => s.writes || []));
      c.inKton = c.outs.length > 0 && c.outs.every((w) => c.kton.has(w));
      c.kton.all = c.inKton;
    });
    return out.filter((c) => c.hand || c.path.length);
  }

  const files = (xs, max, kton) => {
    max = max || 4;
    const shown = xs.slice(0, max).map((x) => `<span class="f" title="${esc(x)}${kton && kton.has(x) ? " – schon in kton" : ""}">${esc(short(x))}${kton && kton.has(x) && !kton.all ? " ✓" : ""}</span>`).join("");
    return shown + (xs.length > max ? `<span class="more" title="${esc(xs.slice(max).join("\n"))}">+${xs.length - max}</span>` : "");
  };

  function headline(cs) {
    const v = S.verdict, hands = cs.filter((c) => c.hand).length;
    const scripts = new Set(cs.filter((c) => !c.hand && c.where.startsWith("Skript ")).map((c) => c.where)).size;
    const consoles = cs.filter((c) => c.where === "Konsole").length;
    const shell = cs.filter((c) => c.where.startsWith("$ ")).length;
    const parts = [];
    if (scripts) parts.push(`${scripts} Skript${scripts > 1 ? "e" : ""}`);
    if (consoles) parts.push(`${consoles}× Konsole`);
    if (shell) parts.push(`${shell} Befehl${shell > 1 ? "e" : ""} in der Shell`);
    if (hands) parts.push(`${hands} Handänderung${hands > 1 ? "en" : ""}`);
    const how = {
      "scripts": `Nachrechenbar mit <code>${esc(v.command || "")}</code>.`,
      "scripts-with-gaps": hands ? "Nachrechenbar bis auf die Handänderung." : "Nachrechenbar, mit Lücken.",
      "scripts-need-session": "Das Skript braucht, was vorher in der Sitzung lag.",
      "interactive": "In der Konsole entstanden, ohne Skript.",
    }[v.kind] || "";
    return `${parts.join(", ")}. ${how}`;
  }

  function line(c) {
    const note = noteOf(c.key);
    const field = (f, ph) => `<div class="note ${note[f] ? "" : "empty"}" contenteditable="true" data-k="${esc(c.key)}" data-f="${f}" data-ph="${ph}">${esc(note[f])}</div>`;
    const cls = c.hand ? "hand" : c.inKton ? "done" : "";
    const what = c.hand
      ? `<span class="f w">${esc(c.steps[0].reads[0])}</span> von Hand geändert <span class="muted">· nicht aufgezeichnet</span>`
      : `${files(c.ins)} <span class="arrow">→</span>${c.tools.length ? ` <span class="tool">${esc(c.tools.join(", "))}</span> <span class="arrow">→</span>` : ""} ${c.outs.length ? files(c.outs, 4, c.kton) : '<span class="muted">nur Objekte in der Sitzung</span>'}`;
    const flags = [c.inKton ? '<span class="ok" title="schon in kton">✓ in kton</span>' : "",
      c.warn.length ? `<span class="warn" title="${esc(c.warn.join("\n"))}">⚠ ${c.warn.length}</span>` : "",
      c.failed ? `<span class="muted" title="Fehlgeschlagene Befehle, ohne Folgen">${c.failed}× gescheitert</span>` : ""].filter(Boolean).join(" ");
    const details = open[c.key] ? `<div class="details">
        ${c.warn.map((w) => `<div class="warn">⚠ ${esc(w)}</div>`).join("")}
        ${c.steps.map((s) => `<div class="cmd ${s.on_path ? "" : "off"}"><span class="when">${clock(s.when_ns)}</span> <code>${esc((s.code || s.sentence).split("\n")[0])}</code>${(s.writes || []).length ? ` → ${files(s.writes, 3)}` : ""}${s.error ? ' <span class="warn">scheitert</span>' : ""}</div>`).join("")}
      </div>` : "";
    return `<li class="${cls}" data-k="${esc(c.key)}">
      <div class="t">${c.hand ? "✎" : clock(c.when)}</div>
      <div class="body">
        <div class="head">${c.where.startsWith("$ ") ? `<code class="cmdline">${esc(c.where)}</code>` : `<b>${esc(c.hand ? "von Hand" : c.where.replace(/^Skript /, ""))}</b>`} ${what} ${flags}
          ${c.hand ? "" : `<button class="link tog">${open[c.key] ? "weniger" : `${c.steps.length} Befehle`}</button>`}</div>
        ${details}
        <div class="notes ${c.inKton && !note.description && !note.rationale ? "quiet" : ""}">
          <span class="nl">Beschreibung</span>${field("description", c.hand ? "Was hast du geändert?" : "Was wurde hier gemacht?")}
          <span class="nl">Begründung</span>${field("rationale", c.hand ? "Warum war die Änderung nötig?" : "Warum so?")}
        </div>
      </div></li>`;
  }

  function markdown(cs) {
    let md = `# Wie ${targetRel()} entstanden ist\n\n${headline(cs).replace(/<\/?code>/g, "`")}\n\n`;
    for (const c of cs) {
      md += c.hand ? `- ✎ **${c.steps[0].reads[0]} von Hand geändert** (nicht aufgezeichnet)`
        : `- ${clock(c.when)} **${c.where.replace(/^Skript /, "")}**: ${c.ins.join(", ")} → ${c.tools.length ? c.tools.join(", ") + " → " : ""}${c.outs.join(", ") || "–"}${c.inKton ? " ✓ in kton" : ""}`;
      const n = noteOf(c.key);
      md += "\n";
      if (n.description) md += `  - *Beschreibung:* ${n.description.replace(/\n/g, "\n    ")}\n`;
      if (n.rationale) md += `  - *Begründung:* ${n.rationale.replace(/\n/g, "\n    ")}\n`;
    }
    if ((S.gaps || []).length) md += `\n**Lücken:**\n${S.gaps.map((g) => "- " + g).join("\n")}\n`;
    return md;
  }

  function render() {
    const cs = chapters();
    const days = new Set(cs.map((c) => day(c.when)));
    const todo = cs.filter((c) => !c.hand && !c.inKton && c.path.some((s) => (s.writes || []).length));
    root.innerHTML = `<main class="one">
      <header>
        <h1>${esc(targetRel())}</h1>
        <div class="sub">${headline(cs)} <span class="muted">${[...days].join(" – ")}</span></div>
        <div class="actions">
          <button id="repro" title="Ausgangsdateien und Skripte in einen leeren Ordner, dort laufen lassen, Hash vergleichen">Reproduzieren</button>
          <button id="publish" ${!cockpitRepo || !todo.length ? "disabled" : ""} title="${cockpitRepo ? "cockpit publish für jede Zeile ohne ✓, im Terminal, mit Beschreibung und Begründung als Kommentar" : "kein Cockpit-Repo (cockpit init)"}">In kton veröffentlichen${todo.length ? ` (${todo.length})` : ""}</button>
          <button class="secondary" id="save">Markdown</button>
        </div>
      </header>
      <ol class="story">${cs.map(line).join("")}</ol>
      ${(S.gaps || []).length ? `<details class="gaps"><summary>${S.gaps.length} Lücke${S.gaps.length > 1 ? "n" : ""} in der Aufzeichnung</summary>${S.gaps.map((g) => `<div class="warn">${esc(g)}</div>`).join("")}</details>` : ""}
    </main>`;
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
    if (m.type === "story") { S = m.story; published = m.published || {}; notes = m.notes || {}; cockpitRepo = m.cockpitRepo; render(); }
    if (m.type === "published") { published = m.published || {}; render(); }
  });
  vscode.postMessage({type: "ready"});
})();
