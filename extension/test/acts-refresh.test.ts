// The shell gap "request blocks don't refresh after an act in another block": one frame holds a view's blocks; after an
// act of the view signed or recorded a claim, every requests block of the view asks again (no reopen of the view).
import { test, mock } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

type Any = any; // eslint-disable-line @typescript-eslint/no-explicit-any
const viewer = (name: string): Promise<Any> => import(pathToFileURL(join(process.cwd(), "media", "workbench", "viewers", name, `${name}.js`)).href);
const esc = (x: unknown) => String(x).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

/** A stand-in element: what the viewer drew, and the buttons it wired (by selector, as the viewers query them). */
function element() {
  const handlers = new Map<string, () => void>();
  return {
    innerHTML: "",
    handlers,
    insertAdjacentHTML() {},
    querySelector: () => null,
    querySelectorAll: (sel: string) => {
      if (sel !== "button[data-row-act]") return [];
      // one row act button of row 0, act 0, no choice (the rows viewer reads these data attributes)
      const b = { dataset: { rowAct: "0", actI: "0", choice: "-1" }, disabled: false, addEventListener: (_k: string, f: (ev: unknown) => void) => handlers.set("row-act", () => f({ stopPropagation() {} })) };
      return [b];
    },
  };
}

test("a requests block asks again after an act of another block of the same view signed or recorded a claim", async (t) => {
  const prev = (globalThis as Any).document;
  (globalThis as Any).document = new EventTarget();
  t.after(() => ((globalThis as Any).document = prev));
  mock.timers.enable({ apis: ["setTimeout"] });
  t.after(() => mock.timers.reset());

  const { mount: mountRequests, ACTED } = await viewer("requests");
  const asked: Array<Record<string, unknown>> = [];
  const reqEl = element();
  const handle = mountRequests(reqEl, {
    kit: { esc },
    use: { key: "v#rerunrequests", options: { title: "test", poll: "10" }, refine: { poll: { type: "integer" } } },
    request: async (_cap: string, args: Record<string, unknown>) => (asked.push(args), { head: { vars: ["test"] }, results: { bindings: [] } }),
    intent: async () => ({}),
  });
  await flush();
  assert.equal(asked.length, 1, "the first ask");
  mock.timers.tick(60_000);
  await flush();
  assert.equal(asked.length, 1, "no act, nothing waiting: it does not ask again by itself");

  // a row act in another block (the failures table) records a claim
  const { mount: mountRows, announceAct } = await viewer("rows");
  const rowEl = element();
  mountRows(rowEl, {
    kit: { esc },
    use: { key: "v#failures", options: { target: "claim" }, blockActs: [{ intent: "record", label: "Request rerun", control: { package: "dryrun1.runs", name: "rerun-request" } }] },
    request: async () => ({ head: { vars: ["claim"] }, results: { bindings: [{ claim: { type: "uri", value: `https://kton.dev/o/${"a".repeat(64)}` } }] } }),
    intent: async () => ({ done: true, result: { signed: true, key: "agent", message: "Recorded with the agent key.", claim: `sha256:${"b".repeat(64)}` } }),
  });
  await flush();
  const heard: unknown[] = [];
  (globalThis as Any).document.addEventListener(ACTED, (e: Any) => heard.push(e.detail));
  rowEl.handlers.get("row-act")?.();
  await flush();
  assert.deepEqual(heard, [{ from: "v#failures", claim: `sha256:${"b".repeat(64)}` }], "the row act is announced in the frame");
  mock.timers.tick(1000);
  await flush();
  assert.equal(asked.length, 2, "the requests block asked again after the act");
  assert.notDeepEqual(asked[1], asked[0], "with a new poll value (a new ask, never the frame's cached answer)");
  // and keeps asking for a while (the index may still be building): every 10 s
  mock.timers.tick(10_000);
  await flush();
  assert.equal(asked.length, 3);

  // refused, cancelled and failed acts are not announced
  assert.equal(announceAct({ refused: { why: "no" } }, "x"), false);
  assert.equal(announceAct({ done: true, result: { signed: false, message: "Not signed: the dialog was cancelled." } }, "x"), false);
  assert.equal(announceAct(undefined, "x"), false);

  // a destroyed block no longer listens
  handle.destroy();
  const before = asked.length;
  (globalThis as Any).document.dispatchEvent(new CustomEvent(ACTED, { detail: {} }));
  mock.timers.tick(60_000);
  await flush();
  assert.equal(asked.length, before);
});

test("every viewer that offers acts announces a signed act the same way", async () => {
  for (const name of ["rows", "figures", "cards", "requests"]) {
    const m = await viewer(name);
    assert.equal(m.ACTED, "kton-workbench:acted", name);
    assert.equal(typeof m.announceAct, "function", name);
  }
});

test("the evidence block keeps an element typed while its first answer was on its way", async () => {
  const { mount } = await viewer("evidence");
  // a stand-in element whose form input is made anew on every innerHTML, with the value the markup says
  let input: { value: string } | null = null;
  let html = "";
  const el = {
    get innerHTML() {
      return html;
    },
    set innerHTML(h: string) {
      html = h;
      const m = /data-testid="evidence-ask"[^>]*value="([^"]*)"/.exec(h);
      input = m ? { value: m[1] ?? "" } : null;
    },
    insertAdjacentHTML() {},
    querySelector: (sel: string) => (sel === "form[data-ask] input" ? input : sel === "form[data-ask]" && input ? { addEventListener() {}, querySelector: () => input } : null),
    querySelectorAll: () => [],
  };
  let answer!: (r: unknown) => void;
  mount(el, { kit: { esc }, use: { key: "v#evidence", options: { ask: "element|e.g. ics1000#e2" } }, state: {}, request: () => new Promise((r) => (answer = r)), intent: async () => ({}), setState() {} });
  (input as unknown as { value: string }).value = "ics1742#e23"; // typed while the block still loads
  answer({ head: { vars: [] }, results: { bindings: [] } });
  await flush();
  assert.equal((input as unknown as { value: string } | null)?.value, "ics1742#e23", "the redraw kept the typed element");
});
