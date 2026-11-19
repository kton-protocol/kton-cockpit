// The composition host's checks (C1.5 v3), slots (C2.1), the composition page's CSP and the vendored kton-web pin.
import { test } from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { CompositionHost, type Backend, type Plan } from "../src/composition/host.js";
import { compositionCsp, compositionPage } from "../src/composition/page.js";
import { applies, oneNumber, partition, planEntry } from "../src/composition/slots.js";
import { loadPackages, type Loaded, type SlotEntry } from "../src/packages/model.js";
import { listed, PKGS, reader } from "./pkg-helpers.js";

const SUBJECT = { kind: "claim" as const, iri: `https://kton.dev/o/${"1".repeat(64)}`, id: `sha256:${"1".repeat(64)}`, claim: `sha256:${"1".repeat(64)}` };

async function goodPackage(): Promise<Loaded> {
  const f = listed(join(PKGS, "hostile", "good"), "hostile-good");
  return loadPackages({ packages: [f.pkg] }, reader(f));
}

function backendLog(): { backend: Backend; calls: Array<[string, unknown]> } {
  const calls: Array<[string, unknown]> = [];
  return {
    calls,
    backend: {
      query: async (name, bind) => {
        calls.push(["query", { name, bind }]);
        return { head: { vars: ["item"] }, results: { bindings: [] } };
      },
      bytes: async () => new Uint8Array(),
      open: async (t) => {
        calls.push(["open", t]);
        return "opened";
      },
      act: async (c, about) => {
        calls.push(["act", { control: c.name, kind: c.kind, about }]);
        return "signed";
      },
      run: async (ray) => `ran ${ray}`,
    },
  };
}

test("a viewer-only review-view entry is planned as one use of the package's block, its query and its control", async () => {
  const l = await goodPackage();
  const e = l.entries[0] as SlotEntry;
  assert.ok(applies(e, SUBJECT));
  assert.ok(!applies(e, { ...SUBJECT, kind: "foton" }));
  const plan = await planEntry(e, SUBJECT, l, "qa-decider", async () => ({ results: { bindings: [] } }));
  const u = plan.uses[0] as Plan["uses"][number] & { module: string };
  assert.equal(u.query?.name, "items");
  assert.deepEqual(u.acts?.map((a) => [a.intent, a.control?.name, a.about]), [["sign", "approve", SUBJECT.claim]]);
  assert.equal(u.module, "packages/hostile-good@1/viewers/table/index.js");
});

test("query@1 runs the package's named query (never query text) with its params bound from the closed set", async () => {
  const l = await goodPackage();
  const plan = await planEntry(l.entries[0] as SlotEntry, SUBJECT, l, "qa-decider", async () => ({ results: { bindings: [] } }));
  const { backend, calls } = backendLog();
  const posted: unknown[] = [];
  const h = new CompositionHost(plan, l, backend, { post: (m) => posted.push(m), log: () => undefined });
  const key = plan.uses[0]?.key as string;
  await h.onMessage({ type: "kton:request", id: `${key}#t.1`, cap: "query@1", args: { use: key } });
  assert.deepEqual(calls, [["query", { name: "items", bind: {} }]]);
  assert.equal((posted.at(-1) as { type: string }).type, "kton:data");
  // a frame cannot swap the query, ask for an undeclared capability, or name a use that is not in the plan
  await h.onMessage({ type: "kton:request", id: "x#t.2", cap: "query@1", args: { use: key, query: { package: "hostile-good", name: "item-card", hash: "sha256:x" } } });
  assert.match(JSON.stringify(posted.at(-1)), /the query differs from the plan's/);
  await h.onMessage({ type: "kton:request", id: "x#t.3", cap: "graph@1", args: { use: key } });
  assert.match(JSON.stringify(posted.at(-1)), /graph@1 is not provided by this host/);
  await h.onMessage({ type: "kton:request", id: "x#t.4", cap: "query@1", args: { use: "main#nope" } });
  assert.match(JSON.stringify(posted.at(-1)), /no use .{0,2}main#nope.{0,2} in this plan/);
  await h.onMessage({ type: "kton:request", id: "x#t.5", cap: "query@1", args: { use: key, refine: { item: "x" } } });
  assert.match(JSON.stringify(posted.at(-1)), /does not declare refine item/);
  assert.equal(calls.length, 1, "nothing refused reached the cockpit");
  // the host saw requests only (the e2e checks it sees no sibling messages)
  assert.deepEqual([...new Set(h.seen.map((s) => s.type))], ["kton:request"]);
});

test("an open intent is performed by the shell; a sign act through its control; the act's kind comes from the control", async () => {
  const l = await goodPackage();
  const plan = await planEntry(l.entries[0] as SlotEntry, SUBJECT, l, "qa-decider", async () => ({ results: { bindings: [] } }));
  const { backend, calls } = backendLog();
  const posted: unknown[] = [];
  const h = new CompositionHost(plan, l, backend, { post: (m) => posted.push(m), log: () => undefined });
  const key = plan.uses[0]?.key as string;
  await h.onMessage({ type: "kton:intent", intent: { id: "o#t.1", kind: "open", from: key, uri: SUBJECT.iri } });
  await h.onMessage({ type: "kton:intent", intent: { id: "s#t.1", kind: "sign", from: key, about: SUBJECT.claim, control: { package: "hostile-good", name: "approve" } } });
  assert.deepEqual(calls.map((c) => c[0]), ["open", "act"]);
  assert.deepEqual(calls[1]?.[1], { control: "approve", kind: "decision", about: SUBJECT.claim });
  // (b) an intent without {id, kind} is not acted on
  await h.onMessage({ type: "kton:intent", kind: "sign", from: key });
  assert.equal(calls.length, 2);
});

test("slots: unknown slot names are delivered and listed, never rendered; tiles need exactly one number", () => {
  const e = (slot: string): SlotEntry => ({ file: `ui/${slot}.json`, slot, label: slot, package: "p", revision: "r" });
  const { rendered, listed: only } = partition([e("review-view"), e("sidebar-ad"), e("material-editor")]);
  assert.deepEqual(rendered.map((x) => x.slot), ["review-view"]);
  assert.deepEqual(only.map((x) => x.why), ["unknown slot sidebar-ad: delivered, not rendered", "slot material-editor is not shown by this shell yet"]);
  assert.deepEqual(oneNumber({ results: { bindings: [{ n: { value: "7" } }] } }), { n: 7 });
  assert.match((oneNumber({ results: { bindings: [] } }) as { why: string }).why, /0 row/);
  assert.match((oneNumber({ results: { bindings: [{ n: { value: "x" } }] } }) as { why: string }).why, /not a number/);
});

test("the composition page: a host-owned CSP with no 'unsafe-inline' and connect-src 'none'; scripts by nonce and the webview origin", () => {
  const csp = compositionCsp("N0nce", "https://webview.example");
  assert.ok(!csp.includes("unsafe-inline"));
  assert.match(csp, /(^|; )connect-src 'none'(;|$)/);
  assert.match(csp, /(^|; )default-src 'none'(;|$)/);
  assert.match(csp, /script-src 'nonce-N0nce' https:\/\/webview\.example 'wasm-unsafe-eval'/);
  const html = compositionPage("T", "N0nce", { cspSource: "https://webview.example", frame: "https://webview.example/frame.js", css: "https://webview.example/compose.css" });
  assert.equal([...html.matchAll(/Content-Security-Policy/g)].length, 1);
  assert.ok(!/\sstyle=/.test(html));
  assert.match(html, /<script type="module" nonce="N0nce" src="https:\/\/webview\.example\/frame\.js">/);
});

test("vendored kton-web: every file matches its pin (PIN.json, commit 39242b9)", async () => {
  const mod = (await import(pathToFileURL(join(process.cwd(), "scripts", "vendor-kton-web.mjs")).href)) as { check: () => string[]; COMMIT: string };
  assert.deepEqual(mod.check(), []);
  assert.equal(mod.COMMIT, "39242b9f0bfce4f78d35f25d291c79877aaeeed5");
});
