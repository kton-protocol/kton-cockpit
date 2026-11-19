import { test } from "node:test";
import assert from "node:assert/strict";
import { fakeCockpit, probe, CLAIM, SUBJECT } from "./helpers.js";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { CliTransport } from "../src/transport/cli.js";
import { OperatorConfirmation, TransportError, TransportTimeout } from "../src/transport/port.js";

test("say uses the flag form with a sha256 subject and returns the claim id", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "ok" }) }]);
  const out = await transport.say({ template: "approves", subject: SUBJECT, fields: { meaning: "approved", note: "a b" }, as: "person-a" });
  assert.ok(out.ok);
  assert.equal(out.value.claimId, CLAIM);
  assert.deepEqual(argvLog()[0], ["say", "approves", SUBJECT, "meaning=approved", "note=a b", "--as", "person-a", "--json"]);
});

test("say and lineage asks refuse paths before calling the cockpit (the resolution line problem)", async () => {
  const { transport, argvLog } = fakeCockpit([]);
  const s = await transport.say({ template: "working-on", subject: "data.txt" });
  assert.ok(!s.ok);
  const a = await transport.ask({ query: "about", ref: "data.txt" });
  assert.ok(!a.ok);
  const f = await transport.say({ template: "working-on", subject: SUBJECT, fields: { "--as": "x" } });
  assert.ok(!f.ok);
  assert.equal(argvLog().length, 0);
});

test("recorded: say with a file path prints a line before the JSON, so it is not parsed", async () => {
  const { transport } = fakeCockpit([{ argv0: "say", stdout: probe("say-sign-json.stdout") }]);
  await assert.rejects(transport.say({ template: "working-on", subject: SUBJECT, fields: { step: "probe" } }), TransportError);
});

test("recorded: say's JSON without the resolution line is read", async () => {
  const raw = probe("say-sign-json.stdout");
  const { transport } = fakeCockpit([{ argv0: "say", stdout: raw.slice(raw.indexOf("{")) }]);
  const out = await transport.say({ template: "working-on", subject: SUBJECT, fields: { step: "probe" } });
  assert.ok(out.ok);
  assert.match(out.value.claimId, /^sha256:7d8958cb/);
});

test("recorded refusals: text on stderr with exit 2", async () => {
  for (const [file, argv0] of [["say-unknown-template-json.stderr", "say"], ["ask-refuse-json.stderr", "ask"], ["sparql-service-refused.stderr", "ask"]] as const) {
    const { transport } = fakeCockpit([{ argv0, exit: 2, stderr: probe(file) }]);
    const out = argv0 === "say" ? await transport.say({ template: "nosuch", subject: SUBJECT }) : await transport.ask({ query: "sparql", sparql: "SELECT * {}" });
    assert.ok(!out.ok, file);
    assert.equal(out.refusal.code, "refused");
    assert.equal(out.refusal.reason, probe(file).trim());
  }
});

test("recorded: doctor --json, ask producer and SPARQL results parse", async () => {
  const { transport } = fakeCockpit([
    { argv0: "doctor", stdout: probe("doctor-json.stdout") },
    { argv0: "ask", stdout: probe("ask-producer-json.stdout") },
  ]);
  const d = await transport.doctor();
  assert.equal(d.ready, true);
  assert.equal(d.fields["mode"], "git");
  const a = await transport.ask({ query: "producer", ref: SUBJECT });
  assert.ok(a.ok);
  assert.equal(a.value.query, "producer");
  const sp = JSON.parse(probe("sparql-results-json.stdout")) as { head: unknown; results: unknown };
  const { transport: t2 } = fakeCockpit([{ argv0: "ask", stdout: JSON.stringify({ query: "sparql", ref: "", raw: "", results: sp }) }]);
  const r = await t2.ask({ query: "sparql", sparql: "SELECT * {}", format: "sparql-results+json" });
  assert.ok(r.ok);
  assert.equal(r.value.results?.results.bindings.length, (sp as { results: { bindings: unknown[] } }).results.bindings.length);
});

test("exit 2 is a refusal: v0.5.0 JSON on stdout", async () => {
  const { transport } = fakeCockpit([
    { argv0: "say", exit: 2, stdout: JSON.stringify({ code: "template.not-allowed", clause: "SPEC §8.1", reason: "not allowed" }) },
  ]);
  const out = await transport.say({ template: "x", subject: SUBJECT });
  assert.ok(!out.ok);
  assert.deepEqual(out.refusal, { code: "template.not-allowed", clause: "SPEC §8.1", reason: "not allowed" });
});

test("exit 2 is a refusal: reason on stderr before v0.5.0", async () => {
  const { transport } = fakeCockpit([{ argv0: "ask", exit: 2, stderr: "filter names an unknown tier\n" }]);
  const out = await transport.ask({ query: "about", ref: SUBJECT });
  assert.ok(!out.ok);
  assert.equal(out.refusal.code, "refused");
  assert.equal(out.refusal.reason, "filter names an unknown tier");
});

test("exit 1 is a failure, not a refusal", async () => {
  const { transport } = fakeCockpit([{ argv0: "say", exit: 1, stderr: "cockpit: git commit failed\n" }]);
  await assert.rejects(transport.say({ template: "x", subject: SUBJECT }), (e: unknown) => e instanceof TransportError && e.exitCode === 1);
});

test("a missing executable is a TransportError", async () => {
  const { transport } = fakeCockpit([], { executable: "/nonexistent/cockpit" });
  await assert.rejects(transport.doctor(), TransportError);
});

test("doctor: exit 3 is a report with ready=false, not an error", async () => {
  const { transport } = fakeCockpit([
    { argv0: "doctor", exit: 3, stdout: JSON.stringify({ ready: false, steps: [{ must: true, text: "make a key" }], fields: {} }) },
  ]);
  const r = await transport.doctor();
  assert.equal(r.ready, false);
  assert.equal(r.steps[0]?.text, "make a key");
});

test("install without a person's confirmation never reaches the cockpit", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "install", stdout: "{}" }]);
  const out = await transport.install({ packageDir: "pkg", allow: true }, undefined);
  assert.ok(!out.ok);
  assert.equal(out.refusal.code, "operator.unconfirmed");
  assert.equal(argvLog().length, 0);
  const ok = await transport.install({ packageDir: "pkg", allow: true }, OperatorConfirmation.fromPerson("install pkg and allow it"));
  assert.ok(ok.ok);
  assert.deepEqual(argvLog()[0], ["install", "pkg", "--allow", "--json"]);
});

test("a confirmation cannot be forged outside the UI path", () => {
  const Ctor = OperatorConfirmation as unknown as new (s: string, d: Date, t: symbol) => unknown;
  assert.throws(() => new Ctor("x", new Date(), Symbol("confirmation")));
});

test("workflow run binds holes as argv pairs and refuses odd hole names", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "workflow", argv1: "run", stdout: "{}" }]);
  const out = await transport.workflowRun({ workflow: "render", bind: { source: "docs/report 01.Rmd" } });
  assert.ok(out.ok);
  assert.deepEqual(argvLog()[0], ["workflow", "run", "render", "--bind", "source=docs/report 01.Rmd", "--json"]);
  const bad = await transport.workflowRun({ workflow: "render", bind: { "--dir": "/" } });
  assert.ok(!bad.ok);
  assert.equal(argvLog().length, 1);
});

test("templates come from the cockpit's listing (say --json, v0.5.0), shape-checked", async () => {
  const listing = [
    { name: "decides", about: "either", predicate: "https://example.org/v/decides", fields: { answers: { type: "ref", required: true }, choice: { type: "string" } } },
  ];
  const { transport, argvLog } = fakeCockpit([{ argv0: "say", argv1: "--json", stdout: JSON.stringify(listing) }]);
  const out = await transport.templates();
  assert.ok(out.ok);
  assert.deepEqual(out.value, [{ name: "decides", fields: listing[0]!.fields, target: "either", predicate: "https://example.org/v/decides" }]);
  assert.deepEqual(argvLog()[0], ["say", "--json"]);
  const { transport: bad } = fakeCockpit([{ argv0: "say", argv1: "--json", stdout: JSON.stringify([{ name: "x", fields: { a: {} } }]) }]);
  await assert.rejects(bad.templates(), TransportError);
});

test("list: templates (say --json), queries (ask --json) and workflows", async () => {
  const { transport } = fakeCockpit([
    { argv0: "say", argv1: "--json", stdout: JSON.stringify([{ name: "approves", fields: { meaning: { type: "string", values: ["approved"] } } }]) },
    { argv0: "ask", argv1: "--json", stdout: JSON.stringify([{ name: "record", arg: "<run|file|sha256:id>", what: "x" }, { name: "inbox-open", params: ["LANE"], package: "lanes" }]) },
    { argv0: "workflow", argv1: "list", stdout: JSON.stringify([{ name: "render" }]) },
  ]);
  const out = await transport.list();
  assert.ok(out.ok);
  assert.deepEqual(out.value.workflows, ["render"]);
  assert.equal(out.value.templates[0]?.name, "approves");
  assert.deepEqual(out.value.queries[1], { name: "inbox-open", parameters: ["LANE"], package: "lanes" });
});

test("version reads version --json (cockpit, revision); non-JSON is a TransportError", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "version", argv1: "--json", stdout: JSON.stringify({ cockpit: "v0.5.1", revision: "e88adfba36a24a93", modified: false, go: "go1.25", kernels: [] }) }]);
  const out = await transport.version();
  assert.ok(out.ok);
  assert.equal(out.value.text, "cockpit v0.5.1 e88adfba36a2");
  assert.deepEqual(argvLog()[0], ["version", "--json"]);
  const { transport: bad } = fakeCockpit([{ argv0: "version", argv1: "--json", stdout: "cockpit (devel)" }]);
  await assert.rejects(bad.version(), TransportError);
});

test("a call that takes longer than its timeout is a TransportTimeout: nothing is claimed about the registry", async () => {
  const d = (await import("node:fs")).mkdtempSync((await import("node:path")).join((await import("node:os")).tmpdir(), "slow-"));
  const f = (await import("node:path")).join(d, "cockpit");
  (await import("node:fs")).writeFileSync(f, "#!/bin/sh\nsleep 5\n");
  (await import("node:fs")).chmodSync(f, 0o755);
  const { CliTransport } = await import("../src/transport/cli.js");
  const t = new CliTransport({ executable: f, cwd: d, timeoutMs: 300 });
  await assert.rejects(t.say({ template: "x", subject: SUBJECT }), (e: unknown) => e instanceof TransportTimeout && /say timed out after 0 s/.test((e as Error).message));
});

test("ui cat: one argv per call, the member's bytes from stdout unchanged; a bad hash or a flag-like package never runs", async () => {
  const dir = mkdtempSync(join(tmpdir(), "uicat-"));
  const bin = join(dir, "cockpit");
  writeFileSync(bin, "#!/bin/sh\nprintf 'a\\000b' ; echo \"$@\" > \"$0.argv\"\n");
  chmodSync(bin, 0o755);
  const t = new CliTransport({ executable: bin, cwd: dir });
  const b = await t.uiCat("pkg-a", `sha256:${"a".repeat(64)}`);
  assert.deepEqual([...b], [97, 0, 98], "binary bytes, a NUL included");
  assert.equal(readFileSync(`${bin}.argv`, "utf8").trim(), `ui cat pkg-a sha256:${"a".repeat(64)}`);
  await assert.rejects(t.uiCat("pkg-a", "sha256:x"), /not sha256/);
  await assert.rejects(t.uiCat("--as", `sha256:${"a".repeat(64)}`), /not a package/);
});

test("ui cat refusals (cockpit v0.5.4): ui.package and ui.member each say what is wrong", async () => {
  for (const [code, re] of [["ui.package", /package pkg-a is not usable here/], ["ui.member", /package pkg-a has no member sha256:a{64}/]] as const) {
    const dir = mkdtempSync(join(tmpdir(), "uicatr-"));
    const bin = join(dir, "cockpit");
    writeFileSync(bin, `#!/bin/sh\necho '{"code":"${code}","reason":"x"}'\nexit 2\n`);
    chmodSync(bin, 0o755);
    const t = new CliTransport({ executable: bin, cwd: dir });
    await assert.rejects(t.uiCat("pkg-a", `sha256:${"a".repeat(64)}`), re);
  }
});

test("an answer larger than the transport reads is reported as too large, not as cannot run", async () => {
  const dir = mkdtempSync(join(tmpdir(), "wb-big-"));
  const exe = join(dir, "cockpit");
  writeFileSync(exe, "#!/bin/sh\nhead -c 5000 /dev/zero\n");
  chmodSync(exe, 0o755);
  const t = new CliTransport({ executable: exe, cwd: dir, maxBufferBytes: 1000 });
  await assert.rejects(t.uiList(), (e: Error) => /cockpit ui list: the answer is larger than 1000 bytes/.test(e.message) && !/cannot run/.test(e.message));
});
