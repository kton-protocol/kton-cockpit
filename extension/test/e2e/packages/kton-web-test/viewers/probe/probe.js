// probe: the test viewer for the shell flows S4.2-4/4.6 and S5 (kton-web tests/). It shows its label
// option, lets the reader pick a value (state.pick: the event `pick`), and has buttons that try what a
// block must not get away with. Each outcome lands in [data-probe-out] for the e2e run to read.
//   - "Record with the decision control": an intent that names a decision control under the kind record
//     (S4.4, the intent cannot change the kind) - the host refuses it.
//   - "Say": an intent of a kind no shell performs (S4.6, no say in the shim) - refused.
//   - "Request sign@1": a capability this viewer does not declare - refused before it leaves the frame.
//   - "Open": an act the host performs (open@1), unwired, goes up.
//   - "Focus": a wirable intent; wired, it is consumed in the composition and the host never sees it.

export function mount(el, ctx) {
  const esc = ctx.kit.esc;
  let label = ctx.use.options?.label ?? "";
  let pick = ctx.state?.pick ?? null;
  const out = (k, v) => {
    const o = el.querySelector(`[data-probe-out="${k}"]`);
    if (o) o.textContent = typeof v === "string" ? v : JSON.stringify(v);
  };
  const draw = () => {
    el.innerHTML = `<div class="kton-probe">
      <div>label: <b data-probe="label">${esc(label || "(none)")}</b> · pick: <b data-probe="pick">${esc(pick ?? "(none)")}</b></div>
      <div class="kton-act">
        <button type="button" data-probe-do="pick-a">Pick A</button>
        <button type="button" data-probe-do="pick-b">Pick B</button>
        <button type="button" data-probe-do="kind">Record with the decision control</button>
        <button type="button" data-probe-do="say">Say</button>
        <button type="button" data-probe-do="cap">Request sign@1</button>
        <button type="button" data-probe-do="open">Open</button>
        <button type="button" data-probe-do="focus">Focus</button>
      </div>
      <div class="kton-muted">kind: <span data-probe-out="kind"></span> · say: <span data-probe-out="say"></span> · cap: <span data-probe-out="cap"></span> · open: <span data-probe-out="open"></span> · focus: <span data-probe-out="focus"></span></div>
    </div>`;
    const on = (k, f) => el.querySelector(`[data-probe-do="${k}"]`).addEventListener("click", f);
    const show = (k) => (r) => out(k, r?.refused ? `refused: ${r.refused.why}` : r?.done ? `done: ${JSON.stringify(r.result)}` : JSON.stringify(r));
    on("pick-a", () => { pick = "A"; ctx.setState({ ...(ctx.state ?? {}), pick }); draw(); });
    on("pick-b", () => { pick = "B"; ctx.setState({ ...(ctx.state ?? {}), pick }); draw(); });
    on("kind", () => ctx.intent({ kind: "record", control: { package: "kton-web-test", name: "test-decision" }, about: ctx.subject.claim, fields: {} }).then(show("kind")));
    on("say", () => ctx.intent({ kind: "say", text: "hello" }).then(show("say")));
    on("cap", () => ctx.request("sign@1", {}).then((r) => out("cap", r), (e) => out("cap", `refused: ${e.message}`)));
    on("open", () => ctx.intent({ kind: "open", uri: "https://kton.dev/" }).then(show("open")));
    on("focus", () => ctx.intent({ kind: "focus", value: "F1" }).then(show("focus")));
  };
  draw();
  return {
    set(state) {
      if (typeof state.label === "string") label = state.label;
      draw();
    },
  };
}
