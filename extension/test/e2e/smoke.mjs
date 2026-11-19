// e2e of the PR 1 gate in code-server, driven by Playwright: flows S1–S4, S7, S9, S10 (shell flows,
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
const page = await browser.newPage({ viewport: { width: 1600, height: 1800 } });
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
async function cockpit(argv, stdin) {
  const id = `${process.pid}-${++rpcN}`;
  const req = `${WORK}/rpc/${id}.req.json`;
  writeFileSync(`${req}.tmp`, JSON.stringify({ argv, ...(stdin !== undefined ? { stdin } : {}) }));
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
const decidesAbout = (pid) => sparql(`SELECT ?d ?by ?choice WHERE { ?d nk:predicate t:decides ; lab:answers <${iriOf(pid)}> ; nk:signer ?s . ?s rdfs:label ?by . OPTIONAL { ?d lab:choice ?choice } }`);
const claimsWithTitle = (template, title) => sparql(`SELECT ?c ?by ?area WHERE { ?c nk:predicate t:${template} ; lab:title ${JSON.stringify(title)} ; nk:signer ?s . ?s rdfs:label ?by . OPTIONAL { ?c lab:area ?area } }`);

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
  await status().getByText(/person key: .*qa-decider.*unverified/).waitFor({ timeout: 60000 });
  await status().getByText(/agent key: .*qa-decider-agent.*unverified/).waitFor();
  await status().getByText("TEST MODE").waitFor();
  await status().getByText(/polling: inbox every 2s · 2 open · 4 decisions/).waitFor({ timeout: 60000 });
  const id = await status().getByText(/person key:/).first().textContent();
  check(!/qa-requestor/.test(id), "the identity shows no name from a claim's signer", false, /qa-requestor/.test(id));
  await shot("s1-status-bar");
});

await step("S9", 1, "surfaces and lanes: the switch filters the same window; Lanes view with open counts", async () => {
  await views();
  await row("qa-lane").waitFor({ timeout: 15000 });
  await row("ops-lane").getByText("1 open").waitFor();
  await row("Open in terminal").getByText("not yet available").waitFor();
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
  const done = await sparql(`SELECT ?d ?by ?outcome WHERE { ?d nk:predicate t:done ; lab:answers <${iriOf(seed.requests[0])}> ; lab:outcome ?outcome ; nk:signer ?s . ?s rdfs:label ?by }`);
  check(done.length === 1 && done[0].by === "qa-decider-claims" && done[0].outcome === "Checked, the notes are fine.", "one done about the request, signed by qa-decider's person key", [{ by: "qa-decider-claims", outcome: "Checked, the notes are fine." }], done.map(({ by, outcome }) => ({ by, outcome })));
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
  check(c.length === 1 && c[0].area === "qa" && c[0].by === "qa-decider-claims", "the composed request exists with its area and title", [{ area: "qa", by: "qa-decider-claims" }], c.map(({ area, by }) => ({ area, by })));
  await clearToasts();
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
  check(dec.length === 1 && dec[0].by === "qa-decider-claims" && dec[0].choice === "b", "one decides about P1", [{ by: "qa-decider-claims", choice: "b" }], dec.map(({ by, choice }) => ({ by, choice })));
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
  for (const [k, pid] of [["panel", P.P1], ["home card", P.P2], ["palette", P.P3]]) by[k] = (await decidesAbout(pid)).map((x) => x.by);
  check(JSON.stringify(by) === JSON.stringify({ panel: ["qa-decider-claims"], "home card": ["qa-decider-claims"], palette: ["qa-decider-claims"] }), "each path signed one decides with the person key", { panel: ["qa-decider-claims"], "home card": ["qa-decider-claims"], palette: ["qa-decider-claims"] }, by);
  await clearToasts();
});

await step("S4", 5, "a decides signed by an agent key does not count: the proposal stays open, the claim shows as a record", async () => {
  const dec = await decidesAbout(P.P4);
  check(dec.length === 1 && dec[0].by === "qa-decider-agent-claims", "the seeded agent-key decides exists", [{ by: "qa-decider-agent-claims" }], dec.map(({ by }) => ({ by })));
  await row("Decided by an agent key only").waitFor({ timeout: 15000 });
  await row("Decided by an agent key only").click();
  const f = await frameWith("An agent signed a decides here.");
  const rec = await f.locator('[data-testid="agent-record"]').textContent();
  check(/qa-decider-agent-claims \(agent\)/.test(rec) && /Not a decision/.test(rec), "shown as a record by an agent", "Recorded by qa-decider-agent-claims (agent) … Not a decision", rec);
  await shot("s4-agent-record");
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
  await status().getByText(/person key: .*qa-decider.*unverified/).waitFor({ timeout: 30000 });
});

await step("S1", 7, "a missing person or agent key is named; neither stands in for the other", async () => {
  setWorkspace({ "kton.identity.personKey": "" });
  await status().getByText(/person key: missing · agent key: .*qa-decider-agent/).waitFor({ timeout: 30000 });
  await row("a · Yes").getByText("signing disabled").waitFor({ timeout: 15000 });
  await shot("s1-no-person-key");
  setWorkspace({ "kton.identity.personKey": "qa-decider", "kton.identity.agentKey": "" });
  await status().getByText(/person key: .*qa-decider.*agent key: missing/).waitFor({ timeout: 30000 });
  await runCommand("kton workbench: say");
  await pickQuick("announces");
  await toast(/no agent key.*person key never stands in/).waitFor({ timeout: 10000 });
  await shot("s1-no-agent-key");
  await clearToasts();
  setWorkspace({ "kton.identity.agentKey": "qa-decider-agent" });
  await status().getByText(/agent key: .*qa-decider-agent.*unverified/).waitFor({ timeout: 30000 });
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

const p4Person = async () => (await decidesAbout(P.P4)).filter((d) => d.by === "qa-decider-claims");

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

await step("S7", 6, "say prints non-JSON with exit 0: Not signed; no claim", async () => {
  faults("say-garbage");
  const d = await chooseOnPanel("Decided by an agent key only", "b");
  await signInDialog(d);
  await toast(/Not signed: say: the answer is not JSON/).waitFor({ timeout: 15000 });
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
