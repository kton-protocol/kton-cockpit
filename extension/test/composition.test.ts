// The composition host's checks (C1.5 v3), slots (C2.1), the composition page's CSP and the vendored kton-web pin.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { answerBytes, CompositionHost, type Backend, type Plan } from "../src/composition/host.js";
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
      query: async (pkg, name, terms) => {
        calls.push(["query", { pkg, name, terms }]);
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
  assert.deepEqual(calls, [["query", { pkg: "hostile-good", name: "items", terms: {} }]]);
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

test("vendored kton-web: every file matches its pin (PIN.json, commit 9909597), the licences included", async () => {
  const mod = (await import(pathToFileURL(join(process.cwd(), "scripts", "vendor-kton-web.mjs")).href)) as { check: () => string[]; COMMIT: string };
  assert.deepEqual(mod.check(), []);
  assert.equal(mod.COMMIT, "9909597");
});

// ---- graph@1: the graph viewer as a leaf (its own frame) in a record-view slot
const KIT = join(process.cwd(), "media", "kton-web");
const PROPOSAL = { ...SUBJECT, type: "https://example.org/workbench-e2e/proposes" };

test("vendored viewers: kton-web/graph is a leaf, with the tree id the e2e package pins", async () => {
  const { loadVendored } = await import("../src/packages/vendored.js");
  const v = loadVendored(KIT).get("kton-web/graph");
  assert.equal(v?.decl["frame"], "leaf");
  const pin = JSON.parse(readFileSync(join(PKGS, "workbench-graph", "viewers", "graph.json"), "utf8")) as { tree: string };
  assert.equal(v?.tree, pin.tree);
});

test("a record-view entry on a kton-web leaf viewer resolves to the vendored tree; a pin to another tree is refused with its reason", async () => {
  const { loadVendored } = await import("../src/packages/vendored.js");
  const vendored = loadVendored(KIT);
  const f = listed(join(PKGS, "workbench-graph"), "workbench-graph");
  const l = await loadPackages({ packages: [f.pkg] }, reader(f), vendored);
  assert.deepEqual(l.refusals, []);
  assert.equal(l.packages["workbench-graph"]?.leaves["graph"]?.viewer, "kton-web/graph");
  const e = l.entries[0] as SlotEntry;
  assert.ok(applies(e, PROPOSAL));
  assert.ok(!applies(e, SUBJECT), "a claim of another type is not offered the entry");
  const bad = listed(join(PKGS, "workbench-graph"), "workbench-graph", "1", (p, b) => (p === "viewers/graph.json" ? Buffer.from(JSON.stringify({ ...JSON.parse(b.toString()), tree: `sha256:${"1".repeat(64)}` })) : b));
  const r = await loadPackages({ packages: [bad.pkg] }, reader(bad), vendored);
  assert.equal(r.refusals[0]?.code, "viewer-pin-mismatch");
  assert.match(r.refusals[0]?.reason ?? "", /viewers\/graph\.json pins viewer kton-web\/graph at tree sha256:1{64}; the tree here is sha256:/);
});

test("graph@1 as a leaf: set binds focus = the subject; graph@1 is answered {records, keys, names}; what the viewer does not declare is refused", async () => {
  const { LeafHost } = await import("../src/composition/host.js");
  const { loadVendored } = await import("../src/packages/vendored.js");
  const decl = loadVendored(KIT).get("kton-web/graph")?.decl as Record<string, unknown>;
  const posted: Array<Record<string, unknown>> = [];
  const { backend } = backendLog();
  const graph = { records: [{ id: "r1" }], keys: { k: "self" }, names: { k: "qa" }, packages: { b1: { package: "p", id: "sha256:1", dir: ".kton/packages/p@1", key: "00" } } };
  const l = await goodPackage();
  const h = new LeafHost({ decl, slot: "record-view" }, { label: "Provenance" }, { id: SUBJECT.id, claim: SUBJECT.claim, closed: {} }, l, { ...backend, graph: async () => graph }, { post: (m) => posted.push(m as Record<string, unknown>), log: () => undefined });
  await h.onMessage({ type: "kton:ready", requires: ["graph@1"] });
  assert.deepEqual(posted[0], { type: "kton:set", state: { focus: SUBJECT.id }, host: { name: "vscode", provides: ["graph@1", "query@1", "product@1", "open@1", "sign@1"] } });
  await h.onMessage({ type: "kton:request", id: "graph#1#x.1", cap: "graph@1", args: {} });
  assert.deepEqual(posted[1], { type: "kton:data", id: "graph#1#x.1", result: graph });
  await h.onMessage({ type: "kton:request", id: "graph#1#x.2", cap: "query@1", args: { input: "x" } });
  assert.match(JSON.stringify(posted[2]), /this viewer does not declare query@1/);
  await h.onMessage({ type: "kton:intent", intent: { id: "graph#1#x.3", kind: "say", text: "hi" } });
  assert.match(JSON.stringify(posted[3]), /say is not an act this shell performs/);
});

test("the leaf page: the viewer's page with the host's head; no 'unsafe-inline'; every script and style by nonce; connect-src only the webview origin", async () => {
  const { leafCsp, leafPage } = await import("../src/composition/page.js");
  const csp = leafCsp("N0nce", "https://webview.example");
  assert.ok(!csp.includes("unsafe-inline"));
  assert.match(csp, /(^|; )connect-src https:\/\/webview\.example(;|$)/, "the kernel wasm, from the vendored kit, and nothing else");
  const html = leafPage(readFileSync(join(KIT, "viewers", "graph", "index.html"), "utf8"), "N0nce", "https://webview.example", "https://webview.example/kit/viewers/graph/");
  assert.equal([...html.matchAll(/http-equiv="Content-Security-Policy"/g)].length, 1);
  assert.match(html, /<base href="https:\/\/webview\.example\/kit\/viewers\/graph\/">/);
  for (const t of html.matchAll(/<(script|style)\b([^>]*)>/g)) assert.match(t[2] ?? "", /nonce="N0nce"/, `<${t[1]}> without the nonce`);
});

test("CSP (C1.5 v3): a leaf may fetch its pinned files (wasm) from its own resource origin; a composition frame connects nowhere, Mermaid frame or not", async () => {
  const { compositionCsp, leafCsp } = await import("../src/composition/page.js");
  const src = "https://webview.example";
  assert.match(leafCsp("n", src), /(^|; )connect-src https:\/\/webview\.example(;|$)/);
  for (const c of [compositionCsp("n", src), compositionCsp("n", src, "http://127.0.0.1:4242")]) {
    assert.match(c, /(^|; )connect-src 'none'(;|$)/);
    assert.ok(!c.includes("unsafe-inline"));
  }
  assert.match(compositionCsp("n", src, "http://127.0.0.1:4242"), /(^|; )frame-src blob: http:\/\/127\.0\.0\.1:4242(;|$)/, "the Mermaid frame is named in frame-src");
});

test("the Mermaid frame server (H13): 127.0.0.1 only, exactly the frame's three pinned files, 404 for anything else", async () => {
  const { startMermaidServer } = await import("../src/composition/mermaid-server.js");
  const m = await startMermaidServer(KIT);
  try {
    assert.match(m.url, /^http:\/\/127\.0\.0\.1:\d+\/mermaid\/frame\.html$/);
    const ok = await fetch(m.url);
    assert.equal(ok.status, 200);
    assert.match(await ok.text(), /sandboxed Mermaid frame/);
    assert.equal((await fetch(`http://127.0.0.1:${m.port}/vendor/mermaid/mermaid.min.js`)).status, 200);
    for (const p of ["/PIN.json", "/core/compose.js", "/vendor/mermaid/LICENSE", "/mermaid/../PIN.json", "/runtime/wasm/kton-full.wasm"]) assert.equal((await fetch(`http://127.0.0.1:${m.port}${p}`)).status, 404, p);
  } finally {
    m.close();
  }
});

test("typed bind (cockpit v0.5.3): a planned IRI param goes as an IRI, a typed literal keeps its datatype, a plain literal stays plain", async () => {
  const { termsOf } = await import("../src/composition/host.js");
  assert.deepEqual(termsOf({ S: { type: "uri", value: "https://kton.dev/o/ab" }, N: { type: "literal", value: "12", datatype: "http://www.w3.org/2001/XMLSchema#integer" }, L: { type: "literal", value: "x" } }), {
    S: { type: "iri", value: "https://kton.dev/o/ab" },
    N: { type: "literal", value: "12", datatype: "http://www.w3.org/2001/XMLSchema#integer" },
    L: { type: "literal", value: "x" },
  });
});

test("review on #174 (blocking): a leaf's approve with a RECORD control is refused, nothing is signed; with a decision control it goes through the act path", async () => {
  const { LeafHost } = await import("../src/composition/host.js");
  const l = await goodPackage();
  for (const kind of ["record", "decision"] as const) {
    const { backend, calls } = backendLog();
    const posted: Array<Record<string, unknown>> = [];
    const control = { package: "p", name: "c", template: "t", kind };
    const h = new LeafHost({ decl: { requires: [], optional: [] }, slot: "review-view" }, { label: "L", control, package: "p" }, { id: SUBJECT.id, claim: SUBJECT.claim, closed: {} }, l, backend, { post: (m) => posted.push(m as Record<string, unknown>), log: () => undefined });
    await h.onMessage({ type: "kton:intent", intent: { id: "a#x.1", kind: "approve" } });
    if (kind === "record") {
      assert.deepEqual(calls, [], "no act, so no say");
      assert.match(JSON.stringify(posted.at(-1)), /kton:refused.*is a record control: an approval is a decision/);
    } else assert.deepEqual(calls.map((c) => [c[0], (c[1] as { kind: string }).kind]), [["act", "decision"]]);
  }
});

test("review on #174 (minor): a leaf query without the entry's package is refused; product@1 without a planned product serves nothing", async () => {
  const { LeafHost } = await import("../src/composition/host.js");
  const l = await goodPackage();
  const { backend, calls } = backendLog();
  const posted: Array<Record<string, unknown>> = [];
  const decl = { requires: ["query@1"], optional: [], inputs: { doc: { query: "q", bind: {} } } };
  const h = new LeafHost({ decl, slot: "review-view" }, { label: "L" }, { id: SUBJECT.id, closed: {} }, l, backend, { post: (m) => posted.push(m as Record<string, unknown>), log: () => undefined });
  await h.onMessage({ type: "kton:request", id: "q#x.1", cap: "query@1", args: { input: "doc" } });
  assert.match(JSON.stringify(posted.at(-1)), /names no package/);
  assert.deepEqual(calls, []);
  // the composition: a use that may ask product@1 but whose plan names no product
  const plan = await planEntry(l.entries[0] as SlotEntry, SUBJECT, l, "qa-decider", async () => ({ results: { bindings: [] } }));
  const key = plan.uses[0]?.key as string;
  const loose = { ...plan, uses: plan.uses.map((u) => ({ ...u, optional: [...u.optional, "product@1"] })) } as Plan;
  const out: unknown[] = [];
  const c = new CompositionHost(loose, l, backend, { post: (m) => out.push(m), log: () => undefined });
  const any = [...l.byHash.keys()][0] ?? `sha256:${"1".repeat(64)}`;
  await c.onMessage({ type: "kton:request", id: "p#x.1", cap: "product@1", args: { use: key, hash: any } });
  assert.match(JSON.stringify(out.at(-1)), /the plan names no product for this use/);
});

test("T1 on #174: answerBytes re-hashes what ui cat serves after load: other bytes for an admitted hash are refused", async () => {
  const admitted = Buffer.from("the admitted bytes");
  const hash = `sha256:${createHash("sha256").update(admitted).digest("hex")}`;
  const l = { byHash: new Map([[hash, new Map([["p", { pkg: "p", hash, type: "text/plain" }]])]]) } as unknown as Loaded;
  const { backend } = backendLog();
  // the admitted bytes are served
  assert.equal((await answerBytes({ ...backend, bytes: async () => new Uint8Array(admitted) }, l, hash, undefined, "content@1"))["bytes"], admitted.toString("base64"));
  const swapped = { ...backend, bytes: async () => new Uint8Array(Buffer.from("not the admitted bytes")) };
  await assert.rejects(answerBytes(swapped, l, hash, undefined, "content@1"), /content@1: the bytes hash to sha256:[0-9a-f]{64}, not /);
});

test("T2 on #174: a plan act sign through a control whose kind is record is refused; nothing is signed", async () => {
  const l = await goodPackage();
  const plan = await planEntry(l.entries[0] as SlotEntry, SUBJECT, l, "qa-decider", async () => ({ results: { bindings: [] } }));
  const approve = l.controls.get("hostile-good/approve");
  assert.equal(approve?.kind, "decision");
  (l.controls as Map<string, NonNullable<typeof approve>>).set("hostile-good/approve", { ...(approve as NonNullable<typeof approve>), kind: "record" });
  const { backend, calls } = backendLog();
  const posted: unknown[] = [];
  const h = new CompositionHost(plan, l, backend, { post: (m) => posted.push(m), log: () => undefined });
  const key = plan.uses[0]?.key as string;
  await h.onMessage({ type: "kton:intent", intent: { id: "s#t.1", kind: "sign", from: key, about: SUBJECT.claim, control: { package: "hostile-good", name: "approve" } } });
  assert.deepEqual(calls, [], "no act");
  assert.match(JSON.stringify(posted), /control approve is a record control, not decision/);
});
