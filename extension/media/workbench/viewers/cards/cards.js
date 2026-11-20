// cards: one card per row (a spot check, a rule case). Options:
//   title    the column shown as the card's title; fields: "column|Label" shown below it, in this order
//   target   the column naming what the card is about: the use's acts are about that row (the host checks it
//            is a row it served this block)
//   choices  "field=value|Button label": one button per choice and act; the value is proposed for the field
//            (the shell shows it in the sign dialog)
//   draws    "kind|Button label": buttons that set the state `draw` (the event draw) to "<kind>:<n>"
//   pick     "column|Button label": a button per card that sets the state `pick` (the event pick) to the row's column,
//            e.g. the element whose evidence another block shows
// Every card shows its row's ?stamp; a row with ?fixture "true" is marked as fixture data.
export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  const o = ctx.use.options ?? {};
  const acts = ctx.use.acts ?? [];
  const v = (b) => (b && typeof b.value === "string" ? b.value : "");
  const split = (s) => {
    const i = s.indexOf("|");
    return i < 0 ? [s, s] : [s.slice(0, i), s.slice(i + 1)];
  };
  let n = Number(String(ctx.state?.draw ?? "").split(":")[1] ?? 0) || 0;
  el.innerHTML = `<div class="kton-muted" data-area-loading>Loading…</div>`;
  ctx.request("query@1", {}).then(
    (r) => {
      const rows = r?.results?.bindings ?? [];
      const choices = (o.choices ?? []).map((c) => {
        const [fv, label] = split(c);
        const [field, value] = fv.split("=");
        return { field, value, label };
      });
      const card = (x, i) => {
        const stamp = v(x.stamp);
        const fields = (o.fields ?? [])
          .map(split)
          .filter(([col]) => v(x[col]))
          .map(([col, label]) => `<div class="kton-field" data-col="${esc(col)}"><span class="kton-muted">${esc(label)}</span> ${esc(v(x[col]))}</div>`)
          .join("");
        const buttons = acts
          .flatMap((a, ai) => (choices.length ? choices.map((c, ci) => ({ a, ai, c, ci })) : [{ a, ai, c: null, ci: -1 }]))
          .map(({ a, ai, c, ci }) => `<button type="button" data-card="${i}" data-act="${ai}" data-choice="${ci}" data-testid="card-${i}-${esc(c ? c.value : a.control?.name ?? a.intent)}">${esc(c ? c.label : a.label ?? a.intent)}</button>`)
          .join("");
        const pick = pickCol && v(x[pickCol]) ? `<button type="button" data-pick="${i}" data-testid="pick-${i}">${esc(pickLabel)}</button>` : "";
        return `<article class="kton-card" data-card-row="${i}" data-target="${esc(o.target ? v(x[o.target]) : "")}">
  <h4>${esc(v(x[o.title]) || "(untitled)")} ${stamp ? `<span class="kton-stamp" data-stamp>${esc(stamp)}</span>` : `<span class="kton-stamp kton-error" data-stamp-missing>no source</span>`}${v(x.fixture) === "true" ? ` <span class="kton-stamp" data-fixture>fixture</span>` : ""}</h4>
  ${fields}
  ${buttons || pick ? `<div class="kton-act">${buttons}${pick}<span class="kton-muted" data-act-out="${i}"></span></div>` : ""}
</article>`;
      };
      const [pickCol, pickLabel] = o.pick ? split(o.pick) : [null, null];
      const draws = (o.draws ?? []).map(split).map(([kind, label]) => `<button type="button" data-draw="${esc(kind)}" data-testid="draw-${esc(kind)}">${esc(label)}</button>`).join("");
      el.innerHTML = `${rows.length ? rows.map(card).join("") : `<p class="kton-muted" data-area-empty>${esc(o.emptyText || "Nothing to show.")}</p>`}${draws ? `<div class="kton-act" data-draws>${draws}</div>` : ""}`;
      for (const b of el.querySelectorAll("button[data-act]")) {
        b.addEventListener("click", () => {
          const row = rows[Number(b.dataset.card)];
          const a = acts[Number(b.dataset.act)];
          const c = choices[Number(b.dataset.choice)];
          const out = el.querySelector(`[data-act-out="${b.dataset.card}"]`);
          const intent = { kind: a.intent, control: a.control, about: a.about, fields: c ? { [c.field]: c.value } : {}, ...(o.target ? { target: v(row[o.target]) } : {}) };
          ctx.intent(intent).then((res) => {
            if (out) out.textContent = res?.refused ? `Not done: ${res.refused.why ?? res.refused}` : res?.result?.message ?? "Done.";
          });
        });
      }
      for (const b of el.querySelectorAll("button[data-pick]")) {
        b.addEventListener("click", () => {
          const row = rows[Number(b.dataset.pick)];
          ctx.setState({ ...(ctx.state ?? {}), pick: v(row[pickCol]) });
          const out = el.querySelector(`[data-act-out="${b.dataset.pick}"]`);
          if (out) out.textContent = `${pickLabel}: ${v(row[pickCol])}`;
        });
      }
      for (const b of el.querySelectorAll("button[data-draw]")) {
        b.addEventListener("click", () => {
          n += 1;
          ctx.setState({ ...(ctx.state ?? {}), draw: `${b.dataset.draw}:${n}` });
        });
      }
    },
    (e) => {
      el.innerHTML = `<p class="kton-error" data-area-error>${esc(e?.message ?? String(e))}</p>`;
    },
  );
  return { set() {}, destroy() {} };
}
