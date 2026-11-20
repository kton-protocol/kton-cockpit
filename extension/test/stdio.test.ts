// The StdioTransport (cockpit serve --stdio, SPEC §12.5) against a stand-in server (test/fixtures/fake-serve.mjs).
import { test } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { StdioTransport } from "../src/transport/stdio.js";
import { TransportTimeout } from "../src/transport/port.js";
import { sparqlResultsOf } from "../src/ui/queries.js";

const FAKE = join(process.cwd(), "test", "fixtures", "fake-serve.mjs");
function transport(timeoutMs = 5000) {
  chmodSync(FAKE, 0o755);
  const dir = mkdtempSync(join(tmpdir(), "fake-serve-"));
  const log = join(dir, "log.jsonl");
  const t = new StdioTransport({ executable: FAKE, cwd: dir, env: { FAKE_SERVE_LOG: log }, timeoutMs });
  const sent = () => (existsSync(log) ? readFileSync(log, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l) as { method: string; params?: Record<string, unknown>; id?: number }) : []);
  return { t, sent, dir };
}

test("stdio: initialize first, with the folder and the timeout; answers are the command line's JSON (ask: sparql.results)", async () => {
  const { t, sent, dir } = transport();
  try {
    const out = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(out.ok);
    assert.deepEqual(sparqlResultsOf(out.value).results.bindings.map((b) => b["n"]?.value), ["7"]);
    const m = sent();
    assert.equal(m[0]?.method, "initialize");
    assert.deepEqual(m[0]?.params, { root: dir, requestTimeoutMs: 5000 });
    assert.equal(m[1]?.method, "ask");
    const tpl = await t.templates();
    assert.ok(tpl.ok && tpl.value[0]?.name === "note");
  } finally {
    t.dispose();
  }
});

test("stdio: a refusal (-32001 {code, clause, reason}) is the same refusal the command line gives", async () => {
  const { t } = transport();
  try {
    const out = await t.ask({ query: "sparql", name: "nope", format: "sparql-results+json" });
    assert.deepEqual(out, { ok: false, refusal: { code: "query.unknown", clause: "SPEC §9", reason: "no allowed package brings the query nope" } });
  } finally {
    t.dispose();
  }
});

test("stdio: one connection per window; each say names its own identity (initialize names none)", async () => {
  const { t, sent } = transport();
  try {
    const a = await t.say({ template: "note", subject: `sha256:${"a".repeat(64)}`, fields: { text: "x" }, as: "qa-decider-agent" });
    const p = await t.say({ template: "note", subject: `sha256:${"a".repeat(64)}`, fields: { text: "x" }, as: "qa-decider" });
    assert.ok(a.ok && p.ok);
    const m = sent();
    assert.deepEqual(m.filter((x) => x.method === "initialize").map((x) => x.params?.["as"]), [undefined], "one initialize, no identity");
    assert.deepEqual(m.filter((x) => x.method === "say").map((x) => x.params?.["as"]), ["qa-decider-agent", "qa-decider"]);
  } finally {
    t.dispose();
  }
});

test("stdio: ui/cat answers the member's bytes (base64 in the JSON)", async () => {
  const { t } = transport();
  try {
    const h = `sha256:${"d".repeat(64)}`;
    const b = await t.uiCat(`sha256:${"9".repeat(64)}`, h);
    assert.equal(Buffer.from(b).toString(), `bytes of ${"d".repeat(8)}`);
  } finally {
    t.dispose();
  }
});

test("stdio: a request past the timeout is cancelled ($/cancelRequest) and reported as a timeout; the connection goes on", async () => {
  const { t, sent } = transport(400);
  try {
    await assert.rejects(t.ask({ query: "sparql", name: "slow", format: "sparql-results+json" }), (e: Error) => e instanceof TransportTimeout && /timed out .*\(cancelled\)/.test(e.message));
    await new Promise((r) => setTimeout(r, 200));
    const cancel = sent().find((m) => m.method === "$/cancelRequest");
    const slow = sent().find((m) => m.method === "ask" && (m.params as { name?: string })?.["name"] === "slow");
    assert.equal(cancel?.params?.["id"], slow?.id, "the cancel names the request");
    const out = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(out.ok, "the same connection answers the next request");
  } finally {
    t.dispose();
  }
});

test("stdio: a cockpit that cannot be started is 'cannot run', as on the command line", async () => {
  const t = new StdioTransport({ executable: "/nonexistent/cockpit", cwd: tmpdir(), timeoutMs: 2000 });
  await assert.rejects(t.ask({ query: "sparql", name: "x" }), /cannot run \/nonexistent\/cockpit/);
  t.dispose();
});
