// e2e of the PR gates in code-server, driven by Playwright: flows S1–S7, S9, S10 (shell flows,
// test-flow-design §4 v3), each step numbered as in the flow. The registry is checked from outside
// the shell after every signature, through the sidecar (sidecar.mjs). Writes out/smoke-results.json
// (native) and out/run.json (kton-testrun/1). Only PNG and JSON go to out/.
// Env: CS_URL (code-server with the folder open), OUT (screenshots, results), WORK (the work dir).
import { chromium } from "playwright";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, readdirSync, renameSync, writeFileSync } from "node:fs";
import { runRecord } from "./runrecord.mjs";

const { CS_URL: URL, OUT, WORK } = process.env;
const seed = JSON.parse(readFileSync(`${WORK}/seed.json`, "utf8"));
const P = seed.proposals;
const iriOf = (id) => `https://kton.dev/o/${id.slice(7)}`;
const results = [];
let current = null;
const started = new Date();

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1600, height: 4500 } });
// S5.3 viewer-calls-network: every request the page or its frames make to example.org (there must be none)
const leaked = [];
page.on("request", (r) => {
  if (/example\.org/.test(r.url())) leaked.push(r.url());
});
async function shot(name) {
  const file = `${name}.png`;
  await page.screenshot({ path: `${OUT}/${file}` });
  current?.screenshots.push(file);
}
async function step(flow, n, title, fn) {
  // FAILFAST=1 (debugging): after the first failure the remaining steps do not run (and count as not run).
  if (process.env.FAILFAST === "1" && results.some((r) => !r.ok)) return;
  const t0 = Date.now();
  current = { flow, n, title, ok: false, ms: 0, screenshots: [], checks: [] };
  try {
    await fn();
    current.ok = true;
    console.log(`ok   ${flow}.${n} ${title}`);
  } catch (e) {
    current.error = String(e?.message ?? e).split("\n")[0];
    current.checks.push({ ok: false, message: current.error });
    console.log(`FAIL ${flow}.${n} ${title}: ${current.error}`);
    if (process.env.FAILFAST === "1") console.log(String(e?.message ?? e));
    await shot(`fail-${flow}-${n}`).catch(() => {});
  }
  current.ms = Date.now() - t0;
  if (current.ok && current.checks.length === 0) current.checks.push({ ok: true, message: "passed" });
  results.push(current);
}
/** A named check inside a step: recorded, and a failure fails the step. */
function check(ok, message, expected, received) {
  current.checks.push({ ok, message, ...(expected !== undefined ? { expected } : {}), ...(received !== undefined ? { received } : {}) });
  if (!ok) throw new Error(`${message}${expected !== undefined ? `: expected ${JSON.stringify(expected)}, got ${JSON.stringify(received)}` : ""}`);
}

// ---- the registry, from outside the shell (sidecar)
let rpcN = 0;
async function cockpit(argv, stdin, extra = {}) {
  const id = `${process.pid}-${++rpcN}`;
  const req = `${WORK}/rpc/${id}.req.json`;
  writeFileSync(`${req}.tmp`, JSON.stringify({ argv, ...(stdin !== undefined ? { stdin } : {}), ...extra }));
  renameSync(`${req}.tmp`, req);
  const end = Date.now() + 180_000;
  while (Date.now() < end) {
    const res = `${WORK}/rpc/${id}.res.json`;
    if (existsSync(res)) return JSON.parse(readFileSync(res, "utf8"));
    await page.waitForTimeout(200);
  }
  throw new Error(`the sidecar did not answer ${argv.join(" ")}`);
}
const PREFIXES = `PREFIX nk: <https://kton.dev/v/>
PREFIX t: <https://example.org/workbench-e2e/>
PREFIX lab: <https://kton.dev/v/lab/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
`;
async function sparql(q) {
  const r = await cockpit(["ask", "sparql", "-", "--results-json"], PREFIXES + q);
  if (r.code !== 0) throw new Error(`registry check failed: ${r.stderr.split("\n")[0]}`);
  return JSON.parse(r.stdout).results.bindings.map((b) => Object.fromEntries(Object.entries(b).map(([k, v]) => [k, v.value])));
}
/** The decides claims about a proposal, with their signer and choice. */
// Signers are compared by key id (the signer IRI), never by label: the look-alike key carries the
// person key's label in another tier (review check 7, Q2).
const PERSON = `https://kton.dev/agent/${seed.personKeyId}`;
const LOOKALIKE = `https://kton.dev/agent/${seed.lookalikeKeyId}`;
const AGENT = `https://kton.dev/agent/${seed.agentKeyId}`;
const decidesAbout = (pid) => sparql(`SELECT ?d ?s ?by ?choice WHERE { ?d nk:predicate t:decides ; lab:answers <${iriOf(pid)}> ; nk:signer ?s . ?s rdfs:label ?by . OPTIONAL { ?d lab:choice ?choice } }`);
const claimsWithTitle = (template, title) => sparql(`SELECT ?c ?s ?by ?area WHERE { ?c nk:predicate t:${template} ; lab:title ${JSON.stringify(title)} ; nk:signer ?s . ?s rdfs:label ?by . OPTIONAL { ?c lab:area ?area } }`);

// ---- the window
/** The frame (webview) that contains `text`. VS Code nests webviews two iframes deep. */
async function frameWith(text, timeout = 20000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    for (const f of page.frames()) {
      if (f === page.mainFrame()) continue; // its tab titles are text too
      try {
        // visible: VS Code keeps a hidden, pending frame per webview while it swaps them
        if (await f.getByText(text, { exact: false }).first().isVisible()) return f;
      } catch {}
    }
    await page.waitForTimeout(300);
  }
  throw new Error(`no webview shows "${text}"`);
}
const status = () => page.locator(".statusbar");
const toast = (re) => page.locator(".notifications-toasts, .notification-toast-container").getByText(re).first();
async function runCommand(title) {
  await page.keyboard.press("F1");
  await page.locator(".quick-input-box input").first().fill(`>${title}`);
  await page.waitForTimeout(500);
  await page.keyboard.press("Enter");
}
async function pickQuick(label) {
  const row = page.locator(".quick-input-list .monaco-list-row", { hasText: label }).first();
  await row.waitFor({ timeout: 10000 });
  await row.click();
}
async function inputQuick(value) {
  const box = page.locator(".quick-input-box input").first();
  await box.waitFor({ timeout: 10000 });
  await box.fill(value);
  await page.keyboard.press("Enter");
  await page.waitForTimeout(300);
}
async function clearToasts() {
  await page.keyboard.press("Escape");
  for (const b of await page.locator(".notification-toast .codicon-notifications-clear, .notification-toast [aria-label^='Clear Notification']").all()) await b.click().catch(() => {});
}
/** Show the workbench's views; a second click on the activity bar icon would close them. */
async function views() {
  const item = page.locator(".activitybar .action-item", { has: page.locator('[aria-label^="kton workbench"]') }).first();
  const active = async () => /\bchecked\b/.test((await item.getAttribute("class")) ?? "") && (await page.locator(".part.sidebar").isVisible());
  if (!(await active())) await item.click();
  const end = Date.now() + 10000;
  while (!(await active())) {
    if (Date.now() > end) throw new Error("the workbench's views did not open");
    await page.waitForTimeout(200);
  }
}
const row = (text) => page.locator(".monaco-list-row", { hasText: text }).first();
const rowCount = (text) => page.locator(".monaco-list-row", { hasText: text }).count();
let ws = {};
/** Settings for this window through the WORKSPACE settings file (watched by the file service). */
function setWorkspace(patch) {
  ws = { ...ws, ...patch };
  writeFileSync(`${WORK}/repo/.vscode/settings.json`, JSON.stringify(ws, null, 2));
}
let rebuilds = 0;
/** A changed kton.* setting rebuilds the workbench in place (the identity file is read again). */
const rebuild = () => setWorkspace({ "kton.cockpit.timeoutSeconds": 20 + (++rebuilds % 2) });
const faults = (mode) => writeFileSync(`${WORK}/faults/mode`, mode);
/** Run fn again when the webview it used was rendered anew under it (VS Code swaps the frame). */
async function retryDetached(fn, attempts = 4) {
  for (let i = 1; ; i++) {
    try {
      return await fn();
    } catch (e) {
      if (i >= attempts || !/detached/i.test(String(e?.message ?? e))) throw e;
      await page.waitForTimeout(500);
    }
  }
}
/** Close every editor: a signed dialog stays open with its outcome, and a new one replaces it. */
async function closeEditors() {
  await runCommand("View: Close All Editors");
  await page.waitForTimeout(500);
}
/** The open sign dialog's frame. */
const dialog = () => frameWith("I know I sign this now");
async function signInDialog(d) {
  await d.locator("#iknow").check();
  await d.locator("#sign").click();
}
/** Open a decision by its title on Home and press Choose <id> in its panel; returns the dialog frame. */
async function chooseOnPanel(title, choice) {
  await closeEditors();
  await row(title).click();
  const f = await frameWith(title);
  await f.locator(`[data-testid="choose-${choice}"]`).click();
  return dialog();
}

mkdirSync(`${WORK}/repo/.vscode`, { recursive: true });
setWorkspace({});
await page.goto(URL);
await page.locator(".monaco-workbench").waitFor({ timeout: 90000 });
await views();

await step("S1", 1, "status bar: both keys, unverified, no name from a claim; test mode; one poller", async () => {
  await status().getByText("person key qa-decider · agent key qa-decider-agent · unverified", { exact: true }).waitFor({ timeout: 60000 });
  await status().getByText("TEST MODE").waitFor();
  await status().getByText(/polling: inbox every 2s · 2 open · 4 decisions/).waitFor({ timeout: 60000 });
  const id = await status().getByText(/person key /).first().textContent();
  check(!/qa-requestor/.test(id), "the identity shows no name from a claim's signer", false, /qa-requestor/.test(id));
  await shot("s1-status-bar");
});

await step("S5", 6, "a home tile shows its named query's number as the real cockpit answers it (rows read from sparql.results)", async () => {
  // workbench-graph's tile counts the claims in the window's view; the registry answers the same query from outside.
  // workbench-graph also carries an alias file (cockpit lists it as the member {role: aliases, name: ""}): it loads.
  await views();
  const tile = page.locator(".monaco-list-row", { hasText: "Claims in view" }).first();
  await tile.waitFor({ timeout: 30000 });
  const end = Date.now() + 30000;
  let shown = "";
  let want = "";
  while (Date.now() < end) {
    want = (await sparql("SELECT (COUNT(?c) AS ?n) WHERE { ?c a nk:Claim }"))[0]?.n ?? "";
    shown = ((await tile.textContent()) ?? "").replace("Claims in view", "").trim();
    if (shown === want) break;
    await page.waitForTimeout(1000);
  }
  check(/^\d+$/.test(shown) && Number(shown) > 0, "the tile shows a number above 0, not an error", "a number > 0", shown);
  check(shown === want, "the tile's number is the registry's", want, shown);
  const admitted = await page.locator(".monaco-list-row", { hasText: "workbench-graph" }).filter({ hasText: "admitted" }).count(); // the row reads "workbench-graph" + "admitted"
  check(admitted >= 1, "the package with an alias file is admitted, not refused", ">= 1", admitted);
  await shot("s5-tile");
});

await step("S9", 1, "surfaces and lanes: the switch filters the same window; Lanes view with open counts", async () => {
  await views();
  await row("qa-lane").waitFor({ timeout: 15000 });
  await row("ops-lane").getByText("1 open").waitFor();
  await row("Open in terminal").getByText("comes with the workgroup extension").waitFor();
  await status().getByText("surface: all").click();
  await pickQuick("ops");
  await status().getByText("surface: ops").waitFor();
  await row("Rotate the test certificate").waitFor();
  await page.waitForTimeout(500);
  check((await rowCount("Check the release notes")) === 0, "the qa request is not on the ops surface");
  check(page.context().pages().length === 1, "the switch opened no window");
  await shot("s9-surface-ops");
  await status().getByText("surface: ops").click();
  await pickQuick("all");
  await status().getByText("surface: all").waitFor();
});

await step("S10", 1, "recorder handover: not installed here, says so, opens nothing", async () => {
  await runCommand("kton workbench: open recorded runs in the recorder");
  await toast(/recorder extension is not installed/).waitFor({ timeout: 10000 });
  await clearToasts();
});

await step("S2", 1, "Home and Inbox: the seeded open requests, without the answered one", async () => {
  await views();
  await row("Decisions waiting").waitFor({ timeout: 15000 });
  await row("Inbox").getByText("2 open").waitFor();
  await row("Check the release notes").waitFor();
  await row("Rotate the test certificate").waitFor();
  check((await rowCount("Already answered")) === 0, "the answered announce is not in the inbox");
  await shot("s2-home-inbox");
});

await step("S2", 2, "a request opens its thread with its text and claim id", async () => {
  await row("Check the release notes").click();
  const f = await frameWith("Please check the notes of the next release.");
  check((await f.locator('[data-testid="claim-id"]').textContent()) === seed.requests[0], "the thread shows the claim id", seed.requests[0], await f.locator('[data-testid="claim-id"]').textContent());
  await shot("s2-thread");
});

await step("S2", 3, "answer with done through the sign dialog; the registry has the done, by the person key", async () => {
  const f = await frameWith("Please check the notes of the next release.");
  await f.locator('[data-testid="answer-outcome"]').fill("Checked, the notes are fine.");
  await f.locator('[data-testid="answer-send"]').click();
  const d = await dialog();
  for (const t of ["Template: done", `answers = ${seed.requests[0]}`, 'person key "qa-decider"']) await d.getByText(t, { exact: false }).first().waitFor();
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  await shot("s2-answered");
  const done = await sparql(`SELECT ?d ?s ?outcome WHERE { ?d nk:predicate t:done ; lab:answers <${iriOf(seed.requests[0])}> ; lab:outcome ?outcome ; nk:signer ?s }`);
  check(done.length === 1 && done[0].s === PERSON && done[0].outcome === "Checked, the notes are fine.", "one done about the request, signed by qa-decider's person key (by key id)", [{ s: PERSON, outcome: "Checked, the notes are fine." }], done.map(({ s, outcome }) => ({ s, outcome })));
  await clearToasts();
});

await step("S2", 4, "after the next poll the request is closed: Inbox 1 open", async () => {
  await row("Inbox").getByText("1 open").waitFor({ timeout: 15000 });
  check((await rowCount("Check the release notes")) === 0, "the answered request left the inbox");
  await shot("s2-counts");
});

await step("S2", 5, "compose: a required field left empty blocks Send and is named; no dialog", async () => {
  await runCommand("kton workbench: compose");
  await pickQuick("requests");
  const f = await frameWith("Compose requests");
  await f.locator('[data-testid="field-about"]').fill(seed.scope);
  await f.locator('[data-testid="field-area"]').fill("qa");
  await f.locator('[data-testid="field-text"]').fill("Composed in the e2e.");
  await f.locator('[data-testid="compose-send"]').click();
  const g = await frameWith("Not sent: title is required"); // the page is rendered again
  await g.locator('[data-testid="missing-title"]').waitFor({ timeout: 10000 });
  check((await page.frames().filter((x) => x.url().includes("ktonWorkbench.sign")).length) === 0, "no sign dialog opened");
  await shot("s2-compose-missing");
});

await step("S2", 6, "compose: Cancel in the dialog keeps the draft and makes no claim; then Sign sends it", async () => {
  const f = await frameWith("Compose requests");
  await f.locator('[data-testid="field-title"]').fill("Composed request");
  await f.locator('[data-testid="compose-send"]').click();
  const d = await dialog();
  await d.locator("#cancel").click();
  await toast(/^Not signed\.$/).waitFor({ timeout: 10000 });
  const g = await frameWith("The draft is kept");
  check((await g.locator('[data-testid="field-title"]').inputValue()) === "Composed request", "the draft is kept", "Composed request", await g.locator('[data-testid="field-title"]').inputValue());
  check((await claimsWithTitle("requests", "Composed request")).length === 0, "Cancel made no claim", 0, (await claimsWithTitle("requests", "Composed request")).length);
  await shot("s2-compose-cancelled");
  await clearToasts();
  await g.locator('[data-testid="compose-send"]').click();
  await signInDialog(await dialog());
  await toast(/Signed sha256:[0-9a-f]{64}/).waitFor({ timeout: 30000 });
  const c = await claimsWithTitle("requests", "Composed request");
  check(c.length === 1 && c[0].area === "qa" && c[0].s === PERSON, "the composed request exists with its area and title, by the person key", [{ area: "qa", s: PERSON }], c.map(({ area, s }) => ({ area, s })));
  await clearToasts();
});


// ---- PR 2: packages and compositions (C2, C1.5 v3), on P1 while it is still open
/** The claims of a package template about a proposal, with their signer. */
const templateClaims = (tpl, pid) => sparql(`SELECT ?c ?s WHERE { ?c nk:template ?t ; nk:about <${iriOf(pid)}> ; nk:signer ?s . FILTER(CONTAINS(STR(?t), ${JSON.stringify(tpl)})) }`);
/** The frame (webview) whose document has this selector, visible. */
async function frameWithSel(sel, timeout = 30000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) {
    for (const f of page.frames()) {
      if (f === page.mainFrame()) continue;
      try {
        if (await f.locator(sel).first().isVisible()) return f;
      } catch {}
    }
    await page.waitForTimeout(300);
  }
  throw new Error(`no webview shows ${sel}`);
}
/** Click the slot entry row `label` (with `pkg` in its description) under the proposal titled `title`. */
async function openReview(title, label, pkg) {
  await views();
  const rows = page.locator(".monaco-list-row");
  const texts = await rows.allTextContents();
  const at = texts.findIndex((t) => t.includes(title));
  if (at < 0) throw new Error(`no row ${title}`);
  const i = texts.findIndex((t, j) => j > at && t.includes(label) && t.includes(pkg));
  if (i < 0) throw new Error(`no row ${label} (${pkg}) under ${title}`);
  await rows.nth(i).click();
}
const P1_TITLE = "Which build goes to qualification?";
const shell = () => frameWithSel('[data-composition="kton-web-test/test-shell"]');
const probeOut = async (k) => (await (await shell()).locator(`[data-use="acts#probe"] [data-probe-out="${k}"]`).textContent()) ?? "";

await step("S5", 1, "a package's review-view entry is offered on a proposal and opens its composition from the checked package", async () => {
  await row("Review · Test shell").first().waitFor({ timeout: 30000 });
  await openReview(P1_TITLE, "Review · Test shell", "kton-web-test");
  const f = await shell();
  await f.locator('[data-use="acts#probe"] [data-probe="label"]', { hasText: "the acting block" }).waitFor({ timeout: 30000 });
  const v = await f.locator("#kton-root").getAttribute("data-csp-violations");
  check(!v || v === "0", "the composition frame ran without a CSP violation", "0", v ?? "0");
  await shot("s5-review-view");
});

await step("S4", 2, "a decision act goes through the one sign dialog and signs with the person key", async () => {
  await (await shell()).locator('[data-use="acts#probe"] button[data-intent="sign"]').click();
  const d = await dialog();
  await d.getByText("Template: test-note", { exact: false }).first().waitFor();
  await d.getByText('person key "qa-decider"', { exact: false }).first().waitFor();
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  await shot("s4-act-sign-dialog");
  const c = await templateClaims("test-note", P.P1);
  check(c.length === 1 && c[0].s === PERSON, "one test-note about P1, by the person key id", [{ s: PERSON }], c.map(({ s }) => ({ s })));
  await clearToasts();
});

await step("C1", 1, "the act's outcome is shown at the act after the dialog took its column (held by the host if the frame was reloaded)", async () => {
  await page.locator(".tab", { hasText: "Test shell" }).first().click();
  // VS Code may or may not have discarded the hidden webview: then the outcome comes back held (data-held, from
  // the next set), else it arrived live. Either way it must be shown at the act (held replay: unit-tested).
  const note = (await shell()).locator('[data-use="acts#probe"] .kton-act-note[data-state="done"]');
  await note.waitFor({ timeout: 30000 });
  check(/Signed with the person key/.test((await note.textContent()) ?? ""), "the act's outcome is shown at the act", "Signed with the person key…", await note.textContent());
  current.checks.push({ ok: true, message: `the outcome came ${(await note.getAttribute("data-held")) === "true" ? "held, after the frame reloaded" : "live (the frame was kept)"}` });
  await shot("c1-held-act");
});

await step("S4", 3, "a record act is written with the agent key, with no dialog", async () => {
  const dialogsBefore = page.frames().filter((f) => f.url().includes("ktonWorkbench.sign")).length;
  await (await shell()).locator('[data-use="acts#probe"] button[data-intent="record"]').click();
  await (await shell()).locator('[data-use="acts#probe"] .kton-act-note[data-state="done"]', { hasText: "Recorded with the agent key" }).waitFor({ timeout: 30000 });
  check(page.frames().filter((f) => f.url().includes("ktonWorkbench.sign")).length <= dialogsBefore, "no sign dialog opened");
  const c = (await templateClaims("test-note", P.P1)).filter((x) => x.s === AGENT);
  check(c.length === 1, "one test-note about P1 by the agent key id", 1, c.length);
  await shot("s4-act-record");
});

await step("S4", 4, "an intent that names a decision control under record is refused: the intent cannot change the kind", async () => {
  const before = (await templateClaims("test-note", P.P1)).length;
  await (await shell()).locator('[data-use="acts#probe"] button[data-probe-do="kind"]').click();
  await (await shell()).locator('[data-use="acts#probe"] [data-probe-out="kind"]', { hasText: "refused" }).waitFor({ timeout: 15000 });
  check(/cannot change the kind/.test(await probeOut("kind")), "refused with the reason", "…cannot change the kind", await probeOut("kind"));
  check((await templateClaims("test-note", P.P1)).length === before, "nothing was signed", before, (await templateClaims("test-note", P.P1)).length);
});

await step("S4", 6, "say is no act of the shell; a capability the viewer does not declare is refused in the frame", async () => {
  const f = await shell();
  await f.locator('[data-use="acts#probe"] button[data-probe-do="say"]').click();
  await f.locator('[data-use="acts#probe"] [data-probe-out="say"]', { hasText: "refused" }).waitFor({ timeout: 15000 });
  check(/say is not an act this shell performs/.test(await probeOut("say")), "say refused by the host", "say is not an act…", await probeOut("say"));
  await f.locator('[data-use="acts#probe"] button[data-probe-do="cap"]').click();
  await f.locator('[data-use="acts#probe"] [data-probe-out="cap"]', { hasText: "refused" }).waitFor({ timeout: 15000 });
  check(/does not declare sign@1/.test(await probeOut("cap")), "sign@1 refused before it left the frame", "…does not declare sign@1", await probeOut("cap"));
  await shot("s4-refused-intents");
});

await step("C1", 2, "wiring: a pick in one block reaches its sibling through a var; a wired intent is consumed in the frame", async () => {
  const f = await shell();
  await f.locator('[data-use="acts#probe"] button[data-probe-do="pick-a"]').click();
  await f.locator('[data-use="wired#echo"] [data-probe="label"]', { hasText: "A" }).waitFor({ timeout: 15000 });
  await f.locator('[data-use="acts#probe"] button[data-probe-do="focus"]').click();
  await f.locator('[data-use="acts#probe"] [data-probe-out="focus"]', { hasText: "wired" }).waitFor({ timeout: 15000 });
  const stats = JSON.parse((await f.locator("#kton-root").getAttribute("data-wiring")) ?? "{}");
  check(stats.consumed >= 1 && stats.emits >= 2, "the focus intent was consumed in the frame (never sent to the host); the pick wrote its var", ">= 1 consumed, >= 2 emits", stats);
  await shot("c1-wiring");
});

await step("C1", 3, "a diagram in the composition: drawn in the sandboxed Mermaid frame (127.0.0.1, H13), no foreignObject, labels intact", async () => {
  const f = await shell();
  const d = f.locator(".kton-diagram");
  await f.locator('.kton-diagram[data-drawn]').waitFor({ state: "attached", timeout: 60000 });
  const named = await f.locator('meta[name="kton-mermaid-frame"]').getAttribute("content").catch(() => null);
  const status = named ? await page.request.get(named).then((r) => r.status(), (e) => String(e.message ?? e)) : "no meta";
  current.checks.push({ ok: true, message: `the Mermaid frame the page names: ${named} (${status})` });
  const drawn = await d.getAttribute("data-drawn");
  check(drawn === "true", "the diagram was drawn", "true", drawn === "true" ? "true" : await d.innerText());
  check((await d.getAttribute("data-foreign-objects")) === "0", "no foreignObject (htmlLabels false)", "0", await d.getAttribute("data-foreign-objects"));
  const labels = JSON.parse((await d.getAttribute("data-labels")) ?? "[]");
  for (const l of ["Bug Reporting & Triage", "Fix in a branch", "Review passed?", "Merge"]) check(labels.includes(l), `label "${l}" intact`, l, labels);
  const src = (await d.locator("img").getAttribute("src")) ?? "";
  check(src.startsWith("data:image/svg+xml"), "shown as an <img> from a data: URL", "data:image/svg+xml…", src.slice(0, 40));
  const v = await f.locator("#kton-root").getAttribute("data-csp-violations");
  check(!v || v === "0", "the composition frame stayed within its CSP", "0", v ?? "0");
  await shot("c1-diagram");
});

// ---- layout (ktonpkg v0.9.5, VERTICLE-HOSTS §17): kton-web-test's test-layout, opened from its review-view entry
const layout = () => frameWithSel('[data-composition="kton-web-test/test-layout"]');
const boxIn = (f, sel) => f.locator(sel).first().evaluate((e) => { const r = e.getBoundingClientRect(); return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) }; });
const selectedTab = async (f) => (await f.locator('[data-layout="tabs"] [role="tab"][aria-selected="true"]').first().innerText()).trim();

await step("C2", 1, "grid: filter | record | side side by side, 2fr wider than 1fr, side's second row an empty cell", async () => {
  await openReview(P1_TITLE, "Review · Test layout", "kton-web-test");
  const f = await layout();
  await f.locator('#kton-root[data-settled="true"]').waitFor({ timeout: 30000 });
  const grid = f.locator('.kton-composition > [data-layout="grid"]');
  check((await grid.getAttribute("data-mode")) === "wide", "wide in this editor", "wide", await grid.getAttribute("data-mode"));
  const [a, b, c] = [await boxIn(f, '[data-cell="filter"]'), await boxIn(f, '[data-cell="main"]'), await boxIn(f, '[data-cell="side"]')];
  check(a.x < b.x && b.x < c.x && Math.abs(a.y - b.y) < 3 && Math.abs(b.y - c.y) < 3, "three cells side by side", "filter < main < side", { a, b, c });
  check(b.w > a.w * 1.5, "2fr is wider than 1fr", `> ${a.w * 1.5}`, b.w);
  const root = await boxIn(f, "#kton-root");
  current.checks.push({ ok: true, message: `the composition's width in this editor: ${root.w}px (host.narrow 640)` });
  await shot("c2-grid");
});

await step("C2", 2, "tabs: History open first; Details mounts on first show; arrow keys and End move", async () => {
  const f = await layout();
  check((await selectedTab(f)) === "History", "History is the active tab", "History", await selectedTab(f));
  check((await f.locator('[data-use="main/details#d"]').count()) === 0, "Details not mounted before it is shown", 0, await f.locator('[data-use="main/details#d"]').count());
  await f.locator('[role="tab"]', { hasText: "Details" }).click();
  await f.locator('[data-use="main/details#d"]').waitFor({ timeout: 10000 });
  await f.locator('[role="tab"]', { hasText: "Details" }).press("ArrowRight");
  check((await selectedTab(f)) === "History", "ArrowRight moves to History", "History", await selectedTab(f));
  await f.locator('[role="tab"]', { hasText: "History" }).press("End");
  check((await selectedTab(f)) === "Approvals", "End moves to Approvals", "Approvals", await selectedTab(f));
  await shot("c2-tabs");
});

await step("C2", 3, "split: 0.3 on the left; the divider moves by keys (ArrowRight: 0.35)", async () => {
  const f = await layout();
  const [l, r] = [await boxIn(f, '[data-cell="left"]'), await boxIn(f, '[data-cell="right"]')];
  const share = l.w / (l.w + r.w);
  check(Math.abs(share - 0.3) <= 0.05, "the left part has 0.3 of the width", 0.3, Number(share.toFixed(3)));
  await f.locator(".kton-divider").press("ArrowRight");
  const ratio = await f.locator('[data-layout="split"]').getAttribute("data-ratio");
  check(ratio === "0.35", "ArrowRight moves the ratio to 0.35", "0.35", ratio);
  await shot("c2-split");
});

await step("C2", 4, "a webview reload restores the open tab and the ratio (host-held view state)", async () => {
  let f = await layout();
  const load = await f.locator("#kton-root").getAttribute("data-load");
  await page.waitForTimeout(1500); // the frame reports its state
  await runCommand("Developer: Reload Webviews");
  const end = Date.now() + 60000;
  for (;;) {
    f = await layout();
    const now = await f.locator("#kton-root").getAttribute("data-load").catch(() => null);
    if (now && now !== load && (await f.locator('#kton-root[data-settled="true"]').count())) break;
    if (Date.now() > end) throw new Error(`the composition frame did not reload (load ${load} -> ${now})`);
    await page.waitForTimeout(500);
  }
  current.checks.push({ ok: true, message: `the frame reloaded (load ${load} -> ${await f.locator("#kton-root").getAttribute("data-load")})` });
  check((await selectedTab(f)) === "Approvals", "the open tab came back", "Approvals", await selectedTab(f));
  const ratio = await f.locator('[data-layout="split"]').getAttribute("data-ratio");
  check(ratio === "0.35", "the ratio came back", "0.35", ratio);
  await shot("c2-restored");
});

await step("C2", 5, "narrow: an editor narrower than host.narrow (640) turns the grid into tabs; wide again after", async () => {
  await page.setViewportSize({ width: 900, height: 4500 });
  let f = await layout();
  await f.locator('.kton-composition > .kton-layout[data-mode="narrow"]').waitFor({ timeout: 15000 });
  const w = (await boxIn(f, "#kton-root")).w;
  const top = await f.locator('.kton-composition > .kton-layout > [role="tablist"] [role="tab"]').allInnerTexts();
  check(JSON.stringify(top.map((t) => t.trim())) === JSON.stringify(["Filter", "Record", "Side"]), "the top parts became tabs", ["Filter", "Record", "Side"], top);
  current.checks.push({ ok: true, message: `narrow at a composition width of ${w}px (window 900px)` });
  await shot("c2-narrow");
  await page.setViewportSize({ width: 1600, height: 4500 });
  f = await layout();
  await f.locator('.kton-composition > .kton-layout[data-mode="wide"]').waitFor({ timeout: 15000 });
});

await step("C2", 6, "the layouts ran under the composition frame's strict CSP: no violation, no style attribute needed", async () => {
  const f = await layout();
  const v = await f.locator("#kton-root").getAttribute("data-csp-violations");
  check(!v || v === "0", "no CSP violation", "0", v ?? "0");
  check(leaked.length === 0, "nothing left the page", 0, leaked);
});

await step("S5", 2, "an unwired open intent goes up to the shell, which performs it (and fetches nothing)", async () => {
  await page.locator(".tab", { hasText: "Test shell" }).first().click(); // the layout panel took the column
  await (await shell()).locator('[data-use="acts#probe"] button[data-probe-do="open"]').click();
  await toast(/Open: https:\/\/kton\.dev\/ \(not a claim in this window; nothing was fetched\)/).waitFor({ timeout: 15000 });
  await (await shell()).locator('[data-use="acts#probe"] [data-probe-out="open"]', { hasText: "done" }).waitFor({ timeout: 15000 });
  await clearToasts();
});

await step("S5", 3, "hostile packages: refusals reach the Packages view with their reasons; good renders; a network call is blocked", async () => {
  await views();
  const texts = (await page.locator(".monaco-list-row").allTextContents()).join("\n");
  for (const [what, re] of [
    ["tampered after admission (the cockpit's own check: only that package goes)", /hostile-tamperedrefused: package\.check/],
    ["viewer-pin-mismatch, with its reason", /hostile-pinrefused: viewer-pin-mismatch/],
    ["unknown slot (version skew): listed, not rendered", /hostile-good: Sidebar adnot rendered/],
  ]) check(re.test(texts), what, String(re), "not in the Home tree");
  const tampered = JSON.parse(readFileSync(`${WORK}/tampered.json`, "utf8")).hash;
  const why = new RegExp(`the bytes for ${tampered.slice(0, 20)}[0-9a-f]* have the hash sha256:[0-9a-f]`);
  check(why.test(texts), "the reason is in English and names the tampered member (ktonpkg v0.9.7)", `the bytes for ${tampered} have the hash …`, texts.split("\n").find((l) => l.includes(tampered.slice(7, 19))) ?? "not in the Home tree");
  check(/pins viewer kton-web\/table at kton-web 39242b9f0bfc, which this shell does not carry/.test(texts), "the pin's reason is shown", "…which this shell does not carry", "");
  check(!/Sidebar ad\b(?!not)/.test(texts.replace(/Sidebar adnot rendered/g, "")), "the unknown slot has no place of its own");
  await shot("s5-packages-refused");
  // the positive control renders
  await openReview(P1_TITLE, "Review", "hostile-good");
  const g = await frameWithSel('[data-block="hostile-good/table"]');
  await g.locator('[data-block="hostile-good/table"]', { hasText: "table" }).waitFor({ timeout: 30000 });
  // viewer-calls-network: renders, but the host CSP blocks the fetch: 0 requests leave, one violation recorded
  await openReview(P1_TITLE, "Review", "hostile-network");
  const n = await frameWithSel('[data-block="hostile-network/table"]');
  await n.locator("#kton-root[data-csp-violations]").waitFor({ state: "attached", timeout: 15000 });
  const csp = JSON.parse((await n.locator("#kton-root").getAttribute("data-csp")) ?? "{}");
  check((csp["connect-src"] ?? 0) >= 1, "the fetch was a connect-src violation in the composition frame", ">= 1", csp);
  check(leaked.length === 0, "no request to example.org left the page", 0, leaked);
  await shot("s5-network-blocked");
});

await step("S5", 5, "record view: kton-web's graph as a leaf; graph@1 answered with records the block verifies in its wasm", async () => {
  await openReview(P1_TITLE, "Record · Provenance", "workbench-graph");
  const g = await frameWithSel("#stats", 60000);
  await g.locator("#stats").filter({ hasText: /\d/ }).waitFor({ timeout: 60000 });
  const err = (await g.locator("#err").isVisible().catch(() => false)) ? await g.locator("#err").innerText() : "";
  check(err === "", "the graph drew without an error", "", err);
  const stats = (await g.locator("#stats").innerText()).replace(/\s+/g, " ").trim();
  current.checks.push({ ok: true, message: `the graph's stats: ${stats}` });
  // what graph@1 was answered with (`cockpit show --json`, v0.5.3): every record's signers, against the trusted
  // ring (keys, the configured tiers) and the admitted packages' build keys (packages: a label, never trust).
  //   in keys                 -> verified
  //   in packages             -> "signed by an admitted package" (checked against the package's key), never verified
  //   neither                 -> "not in the trusted keys" (the tampered package: v0.5.3 labels no package failing its check)
  //   in keys, does not check -> "unverified": there must be none
  const shown = await cockpit(["show", "--json"]);
  check(shown.code === 0, "cockpit show --json answered", 0, shown.code);
  const m = JSON.parse(shown.stdout);
  const kidOf = (pubHex) => createHash("sha256").update(Buffer.from(pubHex.trim(), "hex")).digest("hex").slice(0, 16);
  const who = new Map(Object.entries(m.names));
  for (const p of readdirSync(`${WORK}/pkgs`)) for (const f of readdirSync(`${WORK}/pkgs/${p}/keys`)) who.set(f.replace(/\.pub$/, ""), `package ${p}`);
  for (const f of readdirSync(`${WORK}/vocab-package/keys`)) who.set(f.replace(/\.pub$/, ""), "package workbench-e2e-queries");
  for (const f of (existsSync(`${WORK}/outside`) ? readdirSync(`${WORK}/outside`) : []).filter((x) => x.endsWith("-claims.pub"))) who.set(kidOf(readFileSync(`${WORK}/outside/${f}`, "utf8")), `${f.replace(/\.pub$/, "")} (moved out of the trust config, S1.3)`);
  const count = { ring: 0, pkg: new Map(), outside: new Map() };
  for (const r of m.union) {
    const kids = (r.envelope?.signatures ?? []).map((x) => x.keyid);
    if (kids.some((k) => k in m.keys)) count.ring++;
    else if (kids.some((k) => k in m.packages)) for (const k of kids.filter((x) => x in m.packages)) count.pkg.set(k, (count.pkg.get(k) ?? 0) + 1);
    else for (const k of kids) count.outside.set(k, (count.outside.get(k) ?? 0) + 1);
  }
  const sum = (mp) => [...mp.values()].reduce((a, b) => a + b, 0);
  const list = (mp) => [...mp.entries()].map(([k, n]) => `${k} ${who.get(k) ?? "?"}${m.packages[k] ? ` [labelled: ${m.packages[k].packages.map((p) => p.package).join(", ")}]` : ""}: ${n}`).sort().join("; ");
  current.checks.push({ ok: true, message: `records ${m.union.length}: ${count.ring} in the trusted ring; ${sum(count.pkg)} by admitted packages' build keys: ${list(count.pkg)}; ${sum(count.outside)} by keys in neither: ${list(count.outside)}` });
  current.checks.push({ ok: true, message: `keys.json (trusted): ${Object.keys(m.keys).map((k) => `${k} ${who.get(k) ?? "?"}`).sort().join("; ")}` });
  const num = (re) => Number(re.exec(stats)?.[1] ?? 0);
  check(num(/(\d+) signed by an admitted package/) === sum(count.pkg), "records by admitted packages' build keys show as 'signed by an admitted package' (not a trust tier)", sum(count.pkg), num(/(\d+) signed by an admitted package/));
  check(sum(count.pkg) > 0, "the seed has package-signed records", "> 0", sum(count.pkg));
  check(num(/(\d+)\/\d+ verified/) === count.ring, "verified counts only the trusted ring, never a package's key", count.ring, num(/(\d+)\/\d+ verified/));
  check(num(/(\d+) signer not in the trusted keys/) === sum(count.outside), "'not in the trusted keys' are the records signed by keys in neither", sum(count.outside), num(/(\d+) signer not in the trusted keys/));
  check(!/\d+ unverified/.test(stats), "0 unverified: every signature by a key the ring holds checks", "no 'unverified'", stats);
  check([...count.outside.keys(), ...count.pkg.keys()].every((k) => who.has(k)), "every signer outside the ring is accounted for", "all known", list(count.outside));
  check(leaked.length === 0, "nothing left the page", 0, leaked);
  await shot("s5-graph-leaf");
});

await step("S5", 4, "a package that is no longer admitted: its open panel closes at the next poll", async () => {
  // the operator takes the package out of claims.allowedPackages: the cockpit's ui list says allowed: false
  const id = JSON.parse((await cockpit(["ui", "list", "--json"])).stdout).packages.find((p) => p.name === "hostile-good").package;
  const cf = `${WORK}/repo/.kton/cockpit.config.json`;
  const cfg = JSON.parse(readFileSync(cf, "utf8"));
  cfg.claims.allowedPackages = cfg.claims.allowedPackages.filter((x) => x !== id);
  writeFileSync(`${cf}.tmp`, JSON.stringify(cfg, null, 2));
  renameSync(`${cf}.tmp`, cf);
  await toast(/Closed hostile-good\/ui\/review\.json: package hostile-good is no longer admitted or loaded/).waitFor({ timeout: 30000 });
  await page.waitForTimeout(1000);
  let stale = 0;
  for (const fr of page.frames()) if (fr !== page.mainFrame()) stale += await fr.locator('[data-block="hostile-good/table"]').count().catch(() => 0);
  check(stale === 0, "no frame shows the package's block any more", 0, stale);
  await clearToasts();
  await shot("s5-uninstalled");
});

await step("S3", 1, "Home lists the proposals with their options; the recommendation is marked", async () => {
  await row("Which build goes to qualification?").waitFor({ timeout: 15000 });
  await row("a · Build 39").getByText("recommended").waitFor();
  await row("b · Build 41").waitFor();
  await shot("s3-home");
});

await step("S3", 2, "the Decision panel shows the text, the options with consequences, the recommendation", async () => {
  await row("Which build goes to qualification?").click();
  const f = await frameWith("Two candidates are ready.");
  for (const [id, t] of [["consequence-a", "the build the test system runs"], ["consequence-b", "newer, not yet on a test system"]]) {
    check((await f.locator(`[data-testid="${id}"]`).textContent()) === t, `${id} shown`, t, await f.locator(`[data-testid="${id}"]`).textContent());
  }
  await f.locator('[data-testid="recommended"]').waitFor();
  await shot("s3-decision");
});

await step("S3", 3, "the rules are labelled as information, not enforced", async () => {
  const f = await frameWith("Two candidates are ready.");
  const g = await f.locator('[data-testid="guard"]').textContent();
  check(/not enforced/.test(g), "the guard panel says the rules are not enforced", "…not enforced…", g);
});

await step("S3", 4, "choose a, then Cancel: no decides", async () => {
  const f = await frameWith("Two candidates are ready.");
  await f.locator('[data-testid="choose-a"]').click();
  const d = await dialog();
  await d.getByText("choice = a").first().waitFor();
  await d.locator("#cancel").click();
  await toast(/^Not signed\.$/).waitFor({ timeout: 10000 });
  check((await decidesAbout(P.P1)).length === 0, "no decides about the proposal", 0, (await decidesAbout(P.P1)).length);
  await clearToasts();
});

await step("S4", 8, "the dialog's footer says a kton signature is not yet a QMS e-signature", async () => {
  const f = await frameWith("Two candidates are ready.");
  await f.locator('[data-testid="choose-b"]').click();
  const d = await dialog();
  await d.getByText("not yet a QMS e-signature", { exact: false }).first().waitFor();
  await shot("s4-sign-dialog");
});

await step("S4", 9, "Sign stays disabled until the confirmation; with Sign focused, Enter does nothing", async () => {
  const d = await dialog();
  check(await d.locator("#sign").isDisabled(), "Sign is disabled before the confirmation");
  await d.locator("#iknow").check();
  check(!(await d.locator("#sign").isDisabled()), "Sign is enabled after the confirmation");
  await d.locator("#sign").focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Enter");
  await page.waitForTimeout(4000);
  await d.locator("#sign:not([disabled])").waitFor();
  check((await decidesAbout(P.P1)).length === 0, "Enter signed nothing", 0, (await decidesAbout(P.P1)).length);
  await shot("s4-enter-did-not-sign");
});

await step("S3", 5, "choose b and sign: the dialog showed the plan; the registry has one decides, choice b, by the person key", async () => {
  const d = await dialog();
  for (const t of ["Template: decides", `About: ${P.P1}`, "choice = b", 'person key "qa-decider"']) await d.getByText(t, { exact: false }).first().waitFor();
  await d.locator("#sign").click();
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  const dec = await decidesAbout(P.P1);
  check(dec.length === 1 && dec[0].s === PERSON && dec[0].choice === "b", "one decides about P1, by the person key id", [{ s: PERSON, choice: "b" }], dec.map(({ s, choice }) => ({ s, choice })));
  await shot("s3-signed");
  await clearToasts();
});

await step("S3", 6, "the decided proposal leaves Home", async () => {
  await row("Decisions waiting").getByText("3").waitFor({ timeout: 15000 });
  check((await rowCount("Which build goes to qualification?")) === 0, "P1 is no longer listed");
});

await step("S3", 7, "a/b on the Home card open the same dialog (never one click); signing b records b", async () => {
  await closeEditors();
  await row("b · System B").click();
  const d = await dialog();
  await d.getByText(`About: ${P.P2}`).first().waitFor();
  check((await decidesAbout(P.P2)).length === 0, "the click alone signed nothing");
  await shot("s3-home-card-dialog");
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  await clearToasts();
});

await step("S4", 1, "every decision path (Decision panel, Home card, palette say) opens the one dialog and signs with the person key", async () => {
  // Decision panel: S3.5 (P1); Home card: S3.7 (P2); here the command palette's say with decides (P3).
  await closeEditors();
  await runCommand("kton workbench: say");
  await pickQuick("decides");
  await inputQuick(P.P3); // about
  // the listing's fields in its order: answers, choice, outcome, text
  for (const v of [P.P3, "a", "https://schema.org/AcceptAction", ""]) await inputQuick(v);
  const d = await dialog();
  await d.getByText(`About: ${P.P3}`).first().waitFor();
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  const by = {};
  for (const [k, pid] of [["panel", P.P1], ["home card", P.P2], ["palette", P.P3]]) by[k] = (await decidesAbout(pid)).map((x) => x.s);
  const want = { panel: [PERSON], "home card": [PERSON], palette: [PERSON] };
  check(JSON.stringify(by) === JSON.stringify(want), "each path signed one decides with the person key (by key id)", want, by);
  await clearToasts();
});

await step("S4", 5, "decides by agent keys do not count, nor does one by a key with the person's label in tier agent; a tampered decides.rq in the checkout changes nothing", async () => {
  const dec = await decidesAbout(P.P4);
  const seen = dec.map(({ s, by }) => ({ s, by })).sort((a, b) => a.by.localeCompare(b.by));
  check(dec.length === 2 && dec.every((d) => d.s !== PERSON) && dec.some((d) => d.s === LOOKALIKE && d.by === "qa-decider-claims"),
    "the seed: an agent key's decides, and the look-alike's (label qa-decider-claims, another key id)", "2, neither by the person key id", seen);
  await row("Decided by an agent key only").waitFor({ timeout: 15000 });
  await row("Decided by an agent key only").click();
  const f = await frameWith("An agent signed a decides here.");
  const recs = (await f.locator('[data-testid="agent-record"]').allTextContents()).sort();
  check(recs.length === 2 && recs.every((r) => /\(agent\)/.test(r) && /Not a decision/.test(r)) && recs.some((r) => /Recorded by qa-decider-claims \(agent\)/.test(r)),
    "both shown as records, the look-alike too (Q2)", "2 records, one 'Recorded by qa-decider-claims (agent)'", recs);
  await shot("s4-agent-record");
  // Q1: the checkout's query folder gets a decides.rq and answers.rq that would close P4 and every request
  const inboxBefore = await row("Inbox").textContent();
  writeFileSync(`${WORK}/queries/decides.rq`, `PREFIX nk: <https://kton.dev/v/>\nPREFIX lab: <https://kton.dev/v/lab/>\nSELECT ?d ?answers ?outcome ?s ?by ?byTier WHERE { ?d lab:answers ?answers ; lab:outcome ?outcome ; nk:signer ?s . BIND("self" AS ?byTier) BIND("qa-decider-claims" AS ?by) }\n`);
  writeFileSync(`${WORK}/queries/answers.rq`, `PREFIX nk: <https://kton.dev/v/>\nSELECT ?d ?answers ?outcome WHERE { ?answers nk:predicate ?p . BIND(?answers AS ?d) BIND("x" AS ?outcome) }\n`);
  rebuild(); // the folder is read when the window is built
  await page.waitForTimeout(8000); // several polls of the rebuilt window
  await row("Decided by an agent key only").waitFor({ timeout: 5000 });
  check((await row("Inbox").textContent()) === inboxBefore, "the inbox is unchanged", inboxBefore, await row("Inbox").textContent());
  await shot("s4-tampered-queries-ignored");
});

await step("S1", 3, "a claim by a key outside the trust config is never shown by name", async () => {
  const kg = await cockpit(["keygen", "qa-outsider"]);
  check(kg.code === 0, "an outsider key was made", 0, kg.code);
  const said = await cockpit(["say", "requests", seed.scope, "area=qa", "title=From an outsider", "text=t", "why=w", "--as", "qa-outsider", "--scope", "lanes", "--json"]);
  check(said.code === 0, "the outsider said a request", 0, said.code);
  mkdirSync(`${WORK}/outside`, { recursive: true });
  for (const f of readdirSync(`${WORK}/repo/.kton/keys/self`).filter((x) => x.startsWith("qa-outsider"))) renameSync(`${WORK}/repo/.kton/keys/self/${f}`, `${WORK}/outside/${f}`);
  await page.waitForTimeout(8000); // several polls
  check((await rowCount("From an outsider")) === 0, "the outsider's claim is not shown (v0.5.0 leaves untrusted claims out of the view)");
  check((await rowCount("qa-outsider")) === 0, "the outsider's key name is shown nowhere");
});

await step("S1", 4, "there is no way to switch identity inside the window", async () => {
  await page.keyboard.press("F1");
  await page.locator(".quick-input-box input").first().fill(">kton workbench");
  await page.waitForTimeout(800);
  const titles = await page.locator(".quick-input-list .monaco-list-row").allTextContents();
  await page.keyboard.press("Escape");
  check(titles.length > 3, "the palette lists the workbench's commands", ">3", titles.length);
  check(!titles.some((t) => /identity|sign in as|switch (user|person|key)/i.test(t)), "no command switches the identity", [], titles.filter((t) => /identity/i.test(t)));
});

await step("S1", 6, "a claim seeded meanwhile appears within 2 poll intervals", async () => {
  const said = await cockpit(["say", "requests", seed.scope, "area=qa", "title=Seeded during the run", "text=t", "why=w", "--as", "qa-requestor", "--scope", "lanes", "--json"]);
  check(said.code === 0, "seeded", 0, said.code);
  const t0 = Date.now();
  await row("Seeded during the run").waitFor({ timeout: 30000 });
  const ms = Date.now() - t0;
  current.checks.push({ ok: ms <= 2 * 2000 + 2 * 3000, message: `appeared after ${ms} ms (interval 2 s; two intervals including two polls of at most 3 s each)`, expected: "<= 10000", received: ms });
  check(ms <= 10000, "appeared within two intervals", "<= 10000 ms", ms);
});

await step("S1", 5, "no identity file: reading works, signing is disabled", async () => {
  renameSync(`${WORK}/repo/.kton/local/identity.json`, `${WORK}/repo/.kton/local/identity.json.away`);
  rebuild();
  await status().getByText("no identity file: reading only").waitFor({ timeout: 30000 });
  await row("Seeded during the run").waitFor({ timeout: 30000 });
  await row("a · Yes").getByText("signing disabled").waitFor({ timeout: 15000 });
  await row("Decided by an agent key only").click();
  // the open panel is rendered again for the rebuilt window (and again on the click): wait for its new text
  await retryDetached(async () => {
    const f = await frameWith("This checkout has no identity file");
    await f.locator('[data-testid="cannot-sign"]').waitFor({ timeout: 5000 });
    check(await f.locator('[data-testid="choose-a"]').isDisabled(), "Choose is disabled");
  });
  await shot("s1-no-identity");
  renameSync(`${WORK}/repo/.kton/local/identity.json.away`, `${WORK}/repo/.kton/local/identity.json`);
  rebuild();
  await status().getByText(/person key qa-decider · agent key qa-decider-agent · unverified/).waitFor({ timeout: 30000 });
});

await step("S1", 7, "a missing person or agent key is named; neither stands in for the other", async () => {
  setWorkspace({ "kton.identity.personKey": "" });
  await status().getByText(/person key missing · agent key qa-decider-agent/).waitFor({ timeout: 30000 });
  await row("a · Yes").getByText("signing disabled").waitFor({ timeout: 15000 });
  await shot("s1-no-person-key");
  setWorkspace({ "kton.identity.personKey": "qa-decider", "kton.identity.agentKey": "" });
  await status().getByText(/person key qa-decider · agent key missing/).waitFor({ timeout: 30000 });
  await runCommand("kton workbench: say");
  await pickQuick("announces");
  await toast(/no agent key.*person key never stands in/).waitFor({ timeout: 10000 });
  await shot("s1-no-agent-key");
  await clearToasts();
  setWorkspace({ "kton.identity.agentKey": "qa-decider-agent" });
  await status().getByText(/agent key qa-decider-agent · unverified/).waitFor({ timeout: 30000 });
});

await step("S7", 1, "cockpit missing: one clear error with the setting to check; the views say no data", async () => {
  setWorkspace({ "kton.cockpit.path": "/nonexistent/cockpit" });
  await toast(/cannot read the cockpit: cannot run \/nonexistent\/cockpit.*kton\.cockpit\.path/).waitFor({ timeout: 30000 });
  await status().getByText(/polling: error · no data · cockpit/).waitFor({ timeout: 15000 });
  await row("No data: the cockpit cannot be read").waitFor();
  await shot("s7-missing");
  await clearToasts();
  setWorkspace({ "kton.cockpit.path": `${WORK}/bin/cockpit-faults` });
  await status().getByText(/polling: inbox every 2s .* cockpit-faults$/).waitFor({ timeout: 30000 });
  // the rebuilt window has data again (a fresh workbench has none until its first good poll)
  await row("Seeded during the run").waitFor({ timeout: 30000 });
});

await step("S7", 2, "ask answers non-JSON: an error, and the last good data stays, marked stale", async () => {
  // The switch to cockpit-faults in S7.1 rebuilds the window, and the rows seen there may still be the old
  // window's. Wait until the rebuilt window has good data of its own: its status line shows the faults wrapper
  // polling without error, and it stays so for more than two poll intervals (2 s each).
  const goodPoll = /polling: inbox every 2s .* cockpit-faults$/;
  await status().getByText(goodPoll).waitFor({ timeout: 30000 });
  await page.waitForTimeout(4500);
  await status().getByText(goodPoll).waitFor({ timeout: 30000 });
  await row("Seeded during the run").waitFor({ timeout: 30000 });
  faults("ask-garbage");
  await status().getByText(/polling: error · stale since \d\d:\d\d:\d\d/).waitFor({ timeout: 30000 });
  await row("Stale: the last poll failed").waitFor();
  await row("Seeded during the run").getByText("stale").waitFor();
  await toast(/the answer is not JSON/).waitFor({ timeout: 10000 });
  await shot("s7-stale");
  await clearToasts();
  faults("normal");
  await status().getByText(/polling: inbox every 2s/).waitFor({ timeout: 30000 });
  check((await rowCount("Stale: the last poll failed")) === 0, "fresh again after a good poll");
});

const p4Person = async () => (await decidesAbout(P.P4)).filter((d) => d.s === PERSON);

await step("S7", 5, "say refused (exit 2, JSON refusal): Not signed with the reason; no claim", async () => {
  faults("say-refuse");
  const d = await chooseOnPanel("Decided by an agent key only", "b");
  await signInDialog(d);
  await toast(/Not signed: the cockpit refused: the template decides is not allowed here/).waitFor({ timeout: 15000 });
  await d.locator('[data-testid="sign-outcome"][data-state="failed"]').waitFor();
  check((await p4Person()).length === 0, "no claim", 0, (await p4Person()).length);
  await shot("s7-refusal");
  await clearToasts();
});

await step("S7", 6, "say prints non-JSON with exit 0: unknown whether signed (check the registry); no claim", async () => {
  faults("say-garbage");
  const d = await chooseOnPanel("Decided by an agent key only", "b");
  await signInDialog(d);
  await toast(/Unknown whether signed: say: the answer is not JSON\. Check the registry before signing again/).waitFor({ timeout: 15000 });
  await d.locator('[data-testid="sign-outcome"][data-state="failed"]').waitFor();
  check((await p4Person()).length === 0, "no claim", 0, (await p4Person()).length);
  await shot("s7-malformed");
  await clearToasts();
});

await step("S7", 3, "say times out: the dialog says signing failed; the registry decides (no claim)", async () => {
  faults("say-slow");
  const d = await chooseOnPanel("Decided by an agent key only", "b");
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="failed"]', { hasText: "timed out" }).waitFor({ timeout: 60000 });
  const text = await d.locator('[data-testid="sign-outcome"]').textContent();
  check(/registry/.test(text), "the dialog sends the reader to the registry", "…the registry…", text);
  check((await p4Person()).length === 0, "the registry has no claim from the timed-out say", 0, (await p4Person()).length);
  if (process.env.TRANSPORT === "stdio") {
    // over cockpit serve --stdio the window cancels the request (SPEC §12.5): stdio-faults.mjs saw the $/cancelRequest
    const seen = existsSync(`${WORK}/faults/stdio.log`) ? readFileSync(`${WORK}/faults/stdio.log`, "utf8") : "";
    check(/"fault":"say-slow","cancelled":/.test(seen), "the timed-out say was cancelled with $/cancelRequest", "cancelled", seen.split("\n").filter((l) => l.includes("say-slow")));
  }
  await shot("s7-timeout");
  await clearToasts();
});

await step("S7", 4, "the registry is locked: the retry is shown in the status bar; the say completes after release", async () => {
  faults("say-locked");
  writeFileSync(`${WORK}/faults/registry.lock`, "held by the e2e");
  const d = await chooseOnPanel("Decided by an agent key only", "b");
  await signInDialog(d);
  await status().getByText(/registry locked: retrying say/).waitFor({ timeout: 15000 });
  await shot("s7-locked");
  renameSync(`${WORK}/faults/registry.lock`, `${WORK}/faults/registry.lock.released`);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  const dec = await p4Person();
  check(dec.length === 1 && dec[0].choice === "b", "the say completed: one decides by the person key", [{ choice: "b" }], dec.map(({ choice }) => ({ choice })));
  faults("normal");
  await clearToasts();
});

await step("S7", 8, "say answers pending store.busy: shown as signed, not yet committed; never signed twice", async () => {
  // A proposal of its own, made now (the seeded ones are decided by this point).
  const opts = JSON.stringify([{ id: "a", label: "Yes" }, { id: "b", label: "No" }]);
  const made = await cockpit(["say", "proposes", seed.scope, "area=qa", "title=Signed while the registry was busy", "text=t", "why=e2e S7.8", `options=${opts}`, "--as", "qa-requestor", "--scope", "lanes", "--json"]);
  check(made.code === 0, "the proposal is made", 0, made.code);
  const P5 = JSON.parse(made.stdout).claimId;
  await row("Signed while the registry was busy").waitFor({ timeout: 30000 });
  faults("say-pending");
  const d = await chooseOnPanel("Signed while the registry was busy", "a");
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 30000 });
  const text = await d.locator('[data-testid="sign-outcome"]').innerText();
  check(/not yet committed/.test(text), "the outcome says not yet committed", "not yet committed", text);
  await shot("s7-pending");
  faults("normal");
  const dec = (await decidesAbout(P5)).filter((x) => x.s === PERSON);
  check(dec.length === 1 && dec[0].choice === "a", "exactly one decides by the person key: the pending claim was not signed again", 1, dec.length);
  await clearToasts();
});

await step("S7", 9, "the cockpit process is gone during a say: unknown whether signed (check the registry); no claim; the next poll recovers", async () => {
  const opts = JSON.stringify([{ id: "a", label: "Yes" }, { id: "b", label: "No" }]);
  const made = await cockpit(["say", "proposes", seed.scope, "area=qa", "title=Signed while the cockpit died", "text=t", "why=e2e S7.9", `options=${opts}`, "--as", "qa-requestor", "--scope", "lanes", "--json"]);
  check(made.code === 0, "the proposal is made", 0, made.code);
  const P6 = JSON.parse(made.stdout).claimId;
  await row("Signed while the cockpit died").waitFor({ timeout: 30000 });
  faults("say-gone");
  const d = await chooseOnPanel("Signed while the cockpit died", "a");
  await signInDialog(d);
  await toast(/Unknown whether signed: .*Check the registry before signing again/).waitFor({ timeout: 15000 });
  await d.locator('[data-testid="sign-outcome"][data-state="failed"]').waitFor();
  await shot("s7-gone");
  faults("normal");
  check((await decidesAbout(P6)).filter((x) => x.s === PERSON).length === 0, "no claim", 0, (await decidesAbout(P6)).length);
  // the next poll starts the cockpit again (over stdio: a new serve process) and the window is fresh
  await page.waitForTimeout(4500);
  await status().getByText(/polling: inbox every 2s .* cockpit-faults$/).waitFor({ timeout: 30000 });
  await clearToasts();
});

await step("S7", 10, "the cockpit closes its stdin during a say: unknown whether signed, no uncaught error; the next poll recovers", async () => {
  // kton-cockpit #196 S1: over stdio the window's next write is EPIPE, which fails the waiting say and drops the
  // process (before the fix: an uncaught exception in the extension host, the say waiting for its timeout)
  const opts = JSON.stringify([{ id: "a", label: "Yes" }, { id: "b", label: "No" }]);
  const made = await cockpit(["say", "proposes", seed.scope, "area=qa", "title=Signed while the cockpit closed its input", "text=t", "why=e2e S7.10", `options=${opts}`, "--as", "qa-requestor", "--scope", "lanes", "--json"]);
  check(made.code === 0, "the proposal is made", 0, made.code);
  const P7 = JSON.parse(made.stdout).claimId;
  await row("Signed while the cockpit closed its input").waitFor({ timeout: 30000 });
  faults("say-stdin-closed");
  const d = await chooseOnPanel("Signed while the cockpit closed its input", "a");
  await signInDialog(d);
  await toast(/Unknown whether signed: .*Check the registry before signing again/).waitFor({ timeout: 15000 });
  const text = await d.locator('[data-testid="sign-outcome"][data-state="failed"]').innerText();
  if (process.env.TRANSPORT === "stdio") check(/EPIPE/.test(text), "over stdio the say failed on the closed input (EPIPE), not on its timeout", "…EPIPE…", text);
  await shot("s7-stdin-closed");
  faults("normal");
  check((await decidesAbout(P7)).filter((x) => x.s === PERSON).length === 0, "no claim", 0, (await decidesAbout(P7)).length);
  await page.waitForTimeout(4500);
  await status().getByText(/polling: inbox every 2s .* cockpit-faults$/).waitFor({ timeout: 30000 });
  await clearToasts();
});

// ---- S6: the generic QM area (PR 3; SHELL-FLOWS §S6 and §6, steps 1-16), over the S6 packages and seed
// The areas are in their own registry, repo-s6 (seed.sh says why): the window opens that folder now.
faults("normal");
async function sparqlS6(q) {
  const r = await cockpit(["ask", "sparql", "-", "--results-json"], `PREFIX nk: <https://kton.dev/v/>\nPREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>\n${q}`, { repo: "repo-s6" });
  if (r.code !== 0) throw new Error(`registry check failed: ${r.stderr.split("\n")[0]}`);
  return JSON.parse(r.stdout).results.bindings.map((b) => Object.fromEntries(Object.entries(b).map(([k, v]) => [k, v.value])));
}
await page.goto(URL.replace(/folder=[^&]*/, `folder=${encodeURIComponent(`${WORK}/repo-s6`)}`));
await page.locator(".monaco-workbench").waitFor({ timeout: 90000 });
await views();
const D = seed.s6.deviations;
const QA = "https://example.org/qa";
const areaFrame = (pkg) => frameWithSel(`[data-composition^="@workbench/area:${pkg}/"]`);
const recordFrame = (pkg) => frameWithSel(`[data-composition^="@workbench/area-record:${pkg}/"]`);
const box = (f, id) => f.locator(`[data-use$="#${id}"]`).first();
const boxText = async (f, id) => ((await box(f, id).textContent()) ?? "").replace(/\s+/g, " ");
/** The rows a block shows (its table's data-count), once it has drawn. */
async function countIn(f, id) {
  const t = box(f, id).locator("[data-area-table], [data-area-empty], [data-area-card], [data-area-notice], [data-area-error]").first();
  await t.waitFor({ timeout: 30000 });
  return Number((await box(f, id).locator("[data-area-table]").first().getAttribute("data-count").catch(() => null)) ?? 0);
}
async function openArea(pkg) {
  await views();
  await closeEditors();
  await page.locator(".monaco-list-row", { hasText: pkg }).filter({ hasText: pkg === "qa.area-one" ? "Deviations" : "Supplier evaluations" }).first().click();
  return areaFrame(pkg);
}
const usesOf = async (f) => (await f.locator("[data-use]").evaluateAll((els) => els.map((e) => e.getAttribute("data-use").split("#").pop()))).filter((x) => !/^findings/.test(x));
const perScope = async () => Object.fromEntries((await sparqlS6(`SELECT ?scope (COUNT(?c) AS ?n) WHERE { ?seed nk:genesis true ; rdfs:label ?scope . FILTER(STRSTARTS(STR(?scope), "qa.")) OPTIONAL { ?c nk:scope ?seed . FILTER(?c != ?seed) } } GROUP BY ?scope`)).map((r) => [r.scope, Number(r.n)]));
let scopesBefore = {};

await step("S6", 1, "both QM areas appear on Home with their packages' labels", async () => {
  await views();
  await row("QM areas").waitFor({ timeout: 60000 });
  await page.locator(".monaco-list-row", { hasText: "Deviations" }).filter({ hasText: "qa.area-one" }).first().waitFor();
  await page.locator(".monaco-list-row", { hasText: "Supplier evaluations" }).filter({ hasText: "qa.area-two" }).first().waitFor();
  await shot("s6-areas");
});

await step("S6", 16, "two areas with the same when are listed by order, then package name", async () => {
  const texts = await page.locator(".monaco-list-row").allTextContents();
  const two = texts.findIndex((t) => t.includes("Supplier evaluations"));
  const one = texts.findIndex((t) => t.includes("Deviations") && t.includes("qa.area-one"));
  check(two >= 0 && one > two, "area-two (order 10) before area-one (order 20)", "two < one", { two, one });
});

await step("S6", 2, "area-one: the list is the package's list query; a row opens the record with the record query's fields", async () => {
  const f = await openArea("qa.area-one");
  check((await countIn(f, "records")) === 4, "the list has the list query's 4 rows", 4, await countIn(f, "records"));
  await box(f, "records").locator("tr", { hasText: "D-001" }).first().click();
  const r = await recordFrame("qa.area-one");
  await box(r, "record").locator('[data-area-card] [data-col="identifier"]', { hasText: "D-001" }).waitFor({ timeout: 30000 });
  const t = await boxText(r, "record");
  check(/Label printed with the wrong batch number/.test(t) && /closed/.test(t), "the record shows the record query's fields", "title, status", t.slice(0, 200));
  await shot("s6-record");
});

await step("S6", 8, "a replaced record: only the latest version is listed and shown; both are in its history", async () => {
  const f = await openArea("qa.area-one");
  await countIn(f, "records");
  const list = await boxText(f, "records");
  const d3 = await box(f, "records").locator("tbody tr", { hasText: "D-003" }).count();
  check(d3 === 1, "D-003 is listed once (one row)", 1, d3);
  check(/store room 2/.test(list), "the listed D-003 is the latest version", "store room 2", list.slice(0, 300));
  await box(f, "records").locator("tr", { hasText: "D-003" }).first().click();
  const r = await recordFrame("qa.area-one");
  await box(r, "record").locator('[data-col="title"]', { hasText: "store room 2" }).waitFor({ timeout: 30000 });
  check((await countIn(r, "history")) === 2, "the history shows both versions", 2, await countIn(r, "history"));
  check(/replacedBy/.test(await boxText(r, "history")), "with replaces / replacedBy");
  await shot("s6-history");
});

await step("S6", 3, "findings: the seeded violation with its rule id; the compliant rule shows none", async () => {
  const f = await openArea("qa.area-one");
  check((await countIn(f, "findings-1")) === 1, "QA-R-MAJOR-OWNER fires once", 1, await countIn(f, "findings-1"));
  const t = await boxText(f, "findings-1");
  check(/QA-R-MAJOR-OWNER/.test(t) && /D-004 is critical and names no owner/.test(t), "rule id, focus and message", "QA-R-MAJOR-OWNER … D-004", t.slice(0, 300));
  await countIn(f, "findings-2");
  check(/no findings/.test(await boxText(f, "findings-2")), "QA-R-CLOSED-DATE: no findings");
  await shot("s6-findings");
});

await step("S6", 4, "RACI equals the package's query; the area's documents come from the register, shown as links", async () => {
  const f = await openArea("qa.area-one");
  check((await countIn(f, "raci")) === 2, "RACI: 2 rows", 2, await countIn(f, "raci"));
  check((await countIn(f, "documents")) === 2, "documents: the area's 2 keys", 2, await countIn(f, "documents"));
  const d = await boxText(f, "documents");
  check(/QA-SOP-10/.test(d) && /QA-FOR-10/.test(d) && !/QA-SOP-20/.test(d), "QA-SOP-10 and QA-FOR-10 only");
  check((await box(f, "documents").locator("tr[data-target]").count()) === 2, "each document is a link", 2, await box(f, "documents").locator("tr[data-target]").count());
});

await step("S6", 9, "a read scope's data is shown (RACI from qa.deviations.raci)", async () => {
  const f = await openArea("qa.area-one");
  await countIn(f, "raci");
  const t = await boxText(f, "raci");
  check(/Deviation owner/.test(t) && /QA lead/.test(t), "the read scope's entries", "Deviation owner, QA lead", t.slice(0, 200));
  scopesBefore = await perScope();
});

await step("S6", 7, "training: the package's training query result, computed nowhere else", async () => {
  const f = await openArea("qa.area-one");
  // unfiltered until a person is picked (ktonpkg v0.9.10 optional params): the package's query, all 4 obligations
  check((await countIn(f, "training")) === 4, "the training query's 4 rows, unfiltered", 4, await countIn(f, "training"));
  const t = await boxText(f, "training");
  check(/person\/p1/.test(t) && /person\/p2/.test(t), "both RACI people's obligations");
});

await step("S6", 10, "a param: the record query is bound to the record (subject.iri), not to all records", async () => {
  const f = await openArea("qa.area-one");
  await countIn(f, "records");
  await box(f, "records").locator("tr", { hasText: "D-002" }).first().click();
  const r = await recordFrame("qa.area-one");
  await box(r, "record").locator('[data-col="identifier"]', { hasText: "D-002" }).waitFor({ timeout: 30000 });
  const t = await boxText(r, "record");
  check(!/D-001|D-003|D-004/.test(t), "only D-002's fields", "D-002 only", t.slice(0, 200));
  await shot("s6-record-bound");
});

await step("S6", 11, "approvals: a rejected outcome is shown as rejected, never as approved", async () => {
  const f = await openArea("qa.area-one");
  check((await countIn(f, "approvals")) === 3, "3 approvals", 3, await countIn(f, "approvals"));
  const rej = await box(f, "approvals").locator("tr", { hasText: "rejected" }).first().textContent();
  check(/no: rejected/.test(rej ?? ""), "the rejected one does not count", "no: rejected", rej);
});

await step("S6", 12, "an approval signed by the agent key does not count; the person key's does", async () => {
  const f = await openArea("qa.area-one");
  await countIn(f, "approvals");
  const t = await boxText(f, "approvals");
  check((t.match(/yes: person key/g) ?? []).length === 1, "one approval counts (D-001, person key)", 1, (t.match(/yes: person key/g) ?? []).length);
  check(/no: tier agent is no person tier/.test(t), "the agent key's approval is not counted (its tier, as the cockpit answers it, is agent)");
  await shot("s6-approvals");
});

await step("S6", 15, "the governing SOP's state is shown: released for area-one", async () => {
  const f = await openArea("qa.area-one");
  await box(f, "governing").locator('[data-col="state"]', { hasText: "released" }).waitFor({ timeout: 30000 });
});

await step("S6", 5, "actions: approves through the dialog with the person key; a note with the agent key and no dialog; both into the records scope", async () => {
  const f = await openArea("qa.area-one");
  await countIn(f, "records");
  await box(f, "records").locator("tr", { hasText: "D-002" }).first().click();
  const r = await recordFrame("qa.area-one");
  await box(r, "record").locator('[data-col="identifier"]', { hasText: "D-002" }).waitFor({ timeout: 30000 });
  // the note: a record control, its text asked by the shell, no dialog
  await box(r, "record").locator('[data-testid="act-note"]').click();
  await inputQuick("e2e note on D-002");
  await box(r, "record").locator("[data-act-out]", { hasText: "Recorded with the agent key" }).waitFor({ timeout: 60000 });
  // approves: a decision control, the outcome picked, then the one sign dialog with the meaning and the scope
  await box(r, "record").locator('[data-testid="act-approves"]').click();
  // the control's meaning (approved) fixes the outcome: nothing is asked, and the dialog shows what is signed
  const d = await dialog();
  for (const s of ["Meaning: approved (signed as outcome = approved)", "Into scope: qa.deviations", "outcome = approved", 'person key "qa-decider"']) await d.getByText(s, { exact: false }).first().waitFor({ timeout: 15000 });
  await signInDialog(d);
  await d.locator('[data-testid="sign-outcome"][data-state="signed"]').waitFor({ timeout: 60000 });
  await shot("s6-approve-dialog");
  const about = iriOf(D["D-002"]);
  const notes = await sparqlS6(`SELECT ?c ?s ?sc WHERE { ?c nk:predicate <${QA}/note> ; nk:about <${about}> ; nk:signer ?s ; nk:scope ?scope . ?scope rdfs:label ?sc . ?c <${QA}/text> "e2e note on D-002" }`);
  check(notes.length === 1 && notes[0].s === AGENT && notes[0].sc === "qa.deviations", "the note: agent key, in qa.deviations", { s: AGENT, sc: "qa.deviations" }, notes);
  const appr = await sparqlS6(`SELECT ?c ?s ?sc ?o WHERE { ?c nk:predicate <${QA}/approves> ; nk:about <${about}> ; nk:signer ?s ; nk:scope ?scope ; <${QA}/outcome> ?o . ?scope rdfs:label ?sc . FILTER(STR(?o) = "approved") }`);
  check(appr.length === 1 && appr[0].s === PERSON && appr[0].sc === "qa.deviations", "approves: person key, in qa.deviations", { s: PERSON, sc: "qa.deviations" }, appr);
  const after = await perScope();
  check(after["qa.deviations"] === scopesBefore["qa.deviations"] + 2, "two claims more in the records scope", scopesBefore["qa.deviations"] + 2, after["qa.deviations"]);
  check(after["qa.deviations.raci"] === scopesBefore["qa.deviations.raci"] && after["qa.documents"] === scopesBefore["qa.documents"], "S6.9: nothing written into the read scopes", { raci: scopesBefore["qa.deviations.raci"], documents: scopesBefore["qa.documents"] }, { raci: after["qa.deviations.raci"], documents: after["qa.documents"] });
});

let areaOneUses = [];
await step("S6", 6, "area-two renders with the same structure (the same built-in composition; no code per package)", async () => {
  areaOneUses = await usesOf(await openArea("qa.area-one"));
  const f = await openArea("qa.area-two");
  await countIn(f, "raci");
  const two = await usesOf(f);
  check(JSON.stringify(two) === JSON.stringify(areaOneUses), "the same blocks in the same order (findings aside)", areaOneUses, two);
  check((await countIn(f, "raci")) === 2, "its own RACI (2 rows)", 2, await countIn(f, "raci"));
  await shot("s6-area-two");
});

await step("S6", 13, "area-two names a findings package that is not installed: the view says so, never 'no findings'", async () => {
  const f = await areaFrame("qa.area-two");
  const t = await boxText(f, "findings");
  check(/qa\.rules-suppliers is not installed/.test(t) && !/no findings/.test(t), "not installed, not 'no findings'", "…is not installed…", t);
});

await step("S6", 14, "area-two's records scope is empty: planned, 0 records", async () => {
  const f = await areaFrame("qa.area-two");
  await countIn(f, "records");
  check(/Planned, 0 records \(scope qa\.evaluations\)/.test(await boxText(f, "records")), "planned, 0 records", "Planned, 0 records (scope qa.evaluations).", await boxText(f, "records"));
});

await step("S6", "15b", "area-two's governing SOP QA-SOP-20 is shown as a draft", async () => {
  const f = await areaFrame("qa.area-two");
  await box(f, "governing").locator('[data-col="state"]', { hasText: "draft" }).waitFor({ timeout: 30000 });
  await shot("s6-governing-draft");
  await closeEditors();
});

await browser.close();
const native = { file: "smoke-results.json", results };
const nativeText = JSON.stringify(native, null, 2);
writeFileSync(`${OUT}/smoke-results.json`, nativeText);
const rec = runRecord({
  steps: results,
  started: started.toISOString(),
  durationMs: Date.now() - started.getTime(),
  native: { file: "smoke-results.json", sha256: createHash("sha256").update(nativeText).digest("hex") },
});
writeFileSync(`${OUT}/run.json`, JSON.stringify(rec, null, 2));
await cockpit(["version"]).catch(() => {});
writeFileSync(`${WORK}/rpc/zz-stop.req.json`, JSON.stringify({ stop: true }));
process.exit(results.every((r) => r.ok) ? 0 : 1);
