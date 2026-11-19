// The composition frame: the page that holds core/compose.js, in a browser iframe or a VS Code webview.
// It speaks the v3 bridge (core/host.js connect(), C1.5 v3). The host sends kton:set {state, plan, host}
// after every ready; the frame derives requires/optional from the plan (H2), loads the plan's viewer
// modules and renders. Requests and unwired intents go to the host; wiring between blocks stays in here.
//
// Query string: ?host=<origin>[,…] names hosts besides this frame's own origin and vscode-webview://;
// ?base=<url> is what the plan's module paths are relative to (default: the kton-web root).
// The frame also counts CSP violations (data-csp-violations, data-csp): evidence for the host CSP.

import { connect } from "../../core/host.js";
import { loadModules, render } from "../../core/compose.js";

const root = document.getElementById("kton-root");
const violations = new Map();
document.addEventListener("securitypolicyviolation", (e) => {
  violations.set(e.effectiveDirective, (violations.get(e.effectiveDirective) ?? 0) + 1);
  root.dataset.cspViolations = String([...violations.values()].reduce((a, b) => a + b, 0));
  root.dataset.csp = JSON.stringify(Object.fromEntries(violations));
});

const base = new URLSearchParams(location.search).get("base") ?? new URL("../../", import.meta.url).href;
let view = null;
let generation = 0;

const bridge = connect({
  current: () => (view ? view.state() : null),
  onSet: ({ state, acts, plan, host }) => {
    if (!plan?.uses) return { refused: ["plan"], why: "a composition frame needs a plan" };
    const g = ++generation;
    bridge.declare({
      requires: [...new Set(plan.uses.flatMap((u) => u.requires))].sort(),
      optional: [...new Set(plan.uses.flatMap((u) => u.optional))].sort(),
    });
    root.dataset.host = host?.name ?? "";
    root.dataset.load = bridge.load;
    const hostApi = {
      provides: host?.provides ?? [],
      // compose.js puts a var-bound use's vars into args; on the wire they travel beside args (W3)
      request: (cap, { vars, ...args } = {}, use) => bridge.request(cap, args, { use, vars }),
      intent: (intent, use) => bridge.intent(intent, { use }),
    };
    loadModules(plan, base).then((mods) => {
      if (g !== generation) return; // a newer set arrived while the modules loaded
      view = render(root, plan, hostApi, state, (s) => bridge.state(s), mods, acts);
      bridge.state(view.state());
    });
  },
});
bridge.ready();
