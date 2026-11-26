// The StdioTransport (cockpit serve --stdio, SPEC §12.5) against a stand-in server (test/fixtures/fake-serve.mjs).
import { test } from "node:test";
import assert from "node:assert/strict";
import { chmodSync, existsSync, mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { StdioTransport } from "../src/transport/stdio.js";
import { TransportNotSent, TransportTimeout } from "../src/transport/port.js";
import { sparqlResultsOf } from "../src/ui/queries.js";

const FAKE = join(process.cwd(), "test", "fixtures", "fake-serve.mjs");
function transport(timeoutMs = 5000, env: Record<string, string> = {}) {
  chmodSync(FAKE, 0o755);
  const dir = mkdtempSync(join(tmpdir(), "fake-serve-"));
  const log = join(dir, "log.jsonl");
  const t = new StdioTransport({ executable: FAKE, cwd: dir, env: { FAKE_SERVE_LOG: log, ...env }, timeoutMs });
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

test("stdio: an answer that is not JSON fails the waiting request at once, as the command line's non-JSON stdout does; the connection goes on", async () => {
  const { t, sent } = transport(5000);
  try {
    const t0 = Date.now();
    await assert.rejects(t.ask({ query: "sparql", name: "garbage", format: "sparql-results+json" }), (e: Error) => !(e instanceof TransportTimeout) && /^ask: the answer is not JSON$/.test(e.message));
    assert.ok(Date.now() - t0 < 2000, "not left to the timeout");
    const out = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(out.ok, "the same connection answers the next request");
    assert.equal(sent().filter((m) => m.method === "initialize").length, 1);
  } finally {
    t.dispose();
  }
});

test("stdio: output without a Content-Length header loses the framing: the request fails and the next one starts a new process", async () => {
  const { t, sent } = transport(5000);
  try {
    await assert.rejects(t.ask({ query: "sparql", name: "noheader", format: "sparql-results+json" }), (e: Error) => /^ask: the answer is not the protocol/.test(e.message));
    const out = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(out.ok, "a new process answers");
    assert.equal(sent().filter((m) => m.method === "initialize").length, 2);
  } finally {
    t.dispose();
  }
});

test("stdio: the process gone mid-request: the request fails (not a timeout) and the next one starts a new process", async () => {
  const { t, sent } = transport(5000);
  try {
    await assert.rejects(t.ask({ query: "sparql", name: "die", format: "sparql-results+json" }), (e: Error) => !(e instanceof TransportTimeout) && /cockpit serve --stdio ended \(exit 137\)/.test(e.message));
    const out = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(out.ok, "a new process answers");
    assert.equal(sent().filter((m) => m.method === "initialize").length, 2);
  } finally {
    t.dispose();
  }
});

test("stdio: a parsable message with an unknown id is ignored (SPEC §12.5): no failure, the request gets its own answer", async () => {
  const { t, sent } = transport(5000);
  try {
    const out = await t.ask({ query: "sparql", name: "stray", format: "sparql-results+json" });
    assert.ok(out.ok, "the stray answers (id 99999, the id as a string, a notification) fail nothing");
    assert.deepEqual(sparqlResultsOf(out.value).results.bindings.map((b) => b["n"]?.value), ["7"]);
    const next = await t.ask({ query: "sparql", name: "count", format: "sparql-results+json" });
    assert.ok(next.ok, "the connection goes on");
    assert.equal(sent().filter((m) => m.method === "initialize").length, 1, "not restarted");
  } finally {
    t.dispose();
  }
});

test("stdio: a process that closed its stdin (EPIPE): the request fails at once, no uncaught exception, the next request starts a new process (S1)", async () => {
  const uncaught: Error[] = [];
  const onUncaught = (e: Error) => uncaught.push(e);
  process.on("uncaughtException", onUncaught);
  const dir = mkdtempSync(join(tmpdir(), "fake-serve-"));
  chmodSync(FAKE, 0o755);
  const t = new StdioTransport({ executable: FAKE, cwd: dir, env: { FAKE_SERVE_CLOSE_STDIN: "1" }, timeoutMs: 5000 });
  try {
    for (const round of [1, 2]) {
      const t0 = Date.now();
      await assert.rejects(t.ask({ query: "sparql", name: "count", format: "sparql-results+json" }), (e: Error) => !(e instanceof TransportTimeout) && /EPIPE/.test(e.message), `round ${round}`);
      assert.ok(Date.now() - t0 < 2000, `round ${round}: not left to the timeout`);
    }
    await new Promise((r) => setTimeout(r, 200));
    assert.deepEqual(uncaught.map((e) => e.message), [], "no uncaught exception");
  } finally {
    t.dispose();
    process.off("uncaughtException", onUncaught);
  }
});

test("stdio: an answer split across chunks, inside the header and inside multi-byte characters, is read whole", async () => {
  const { t } = transport();
  try {
    const out = await t.ask({ query: "sparql", name: "split", format: "sparql-results+json" });
    assert.ok(out.ok);
    assert.deepEqual(sparqlResultsOf(out.value).results.bindings.map((b) => b["n"]?.value), ["Grüße – 検査 ✓ 𝄞"]);
  } finally {
    t.dispose();
  }
});

test("stdio: an answer written after the process exited, while its stdout is still open, is read (the end is 'close', not 'exit') (Y2)", async () => {
  const { t } = transport();
  try {
    const out = await t.ask({ query: "sparql", name: "orphan", format: "sparql-results+json" });
    assert.ok(out.ok, "the answer that came after 'exit' arrives");
    assert.deepEqual(sparqlResultsOf(out.value).results.bindings.map((b) => b["n"]?.value), ["after exit"]);
  } finally {
    t.dispose();
  }
});

test("stdio: a 4 MB answer written just before exit is read whole (Y2)", async () => {
  const { t } = transport();
  try {
    const out = await t.ask({ query: "sparql", name: "bigexit", format: "sparql-results+json" });
    assert.ok(out.ok, "the 4 MB answer written just before exit arrives");
    assert.equal((out.value as { raw?: string }).raw?.length, 4 << 20);
  } finally {
    t.dispose();
  }
});

test("stdio: dispose sends shutdown and exit; an answer serve still gives is delivered; what has none is unknown (TransportTimeout), never 'not sent' (Y1)", async () => {
  const { t, sent } = transport(5000);
  const later = t.ask({ query: "sparql", name: "later", format: "sparql-results+json" });
  const never = t.ask({ query: "sparql", name: "slow", format: "sparql-results+json" });
  await new Promise((r) => setTimeout(r, 100));
  t.dispose();
  const out = await later;
  assert.ok(out.ok, "the answer that came after dispose is delivered");
  assert.deepEqual(sparqlResultsOf(out.value).results.bindings.map((b) => b["n"]?.value), ["later"]);
  await assert.rejects(never, (e: Error) => e instanceof TransportTimeout && !(e instanceof TransportNotSent) && /the cockpit ended before it answered/.test(e.message));
  const m = sent().map((x) => x.method);
  assert.ok(m.indexOf("shutdown") > m.indexOf("ask") && m.indexOf("exit") > m.indexOf("shutdown"), `shutdown, then exit: ${m.join(", ")}`);
});

test("stdio: a say failed by another request's non-JSON answer, then answered after all, is reported with its claim id (W1)", async () => {
  const { t } = transport(5000);
  const late: Array<{ method: string; result?: unknown }> = [];
  t.onLateAnswer = (a) => late.push(a);
  try {
    const say = t.say({ template: "note", subject: `sha256:${"a".repeat(64)}`, fields: { text: "late" }, as: "qa-decider" });
    await new Promise((r) => setTimeout(r, 100));
    await assert.rejects(t.ask({ query: "sparql", name: "garbage", format: "sparql-results+json" }), /the answer is not JSON/);
    await assert.rejects(say, (e: Error) => /^say: the answer is not JSON$/.test(e.message) && !(e instanceof TransportNotSent));
    await new Promise((r) => setTimeout(r, 600));
    assert.deepEqual(late.map((a) => [a.method, (a.result as { claimId?: string })?.claimId]), [["say", `sha256:${"d".repeat(64)}`]]);
  } finally {
    t.dispose();
  }
});

test("stdio: the command line's local checks run before any request; COCKPIT_REPO_DIR is not passed to serve", async () => {
  const { t, sent, dir } = transport();
  const repoLog = join(dir, "repo-dir.log");
  const before = process.env["COCKPIT_REPO_DIR"];
  process.env["COCKPIT_REPO_DIR"] = "/somewhere/else";
  try {
    const bad = await t.say({ template: "note", subject: "./a/path", fields: { text: "x" }, as: "qa-decider" });
    assert.deepEqual(bad, { ok: false, refusal: { code: "argument", reason: "say takes a sha256 id or an http(s) IRI as its subject, never a path" } });
    const lineage = await t.ask({ query: "lineage", ref: "../x" });
    assert.equal(lineage.ok, false);
    assert.equal(sent().length, 0, "nothing was sent: no process started");
    const t2 = new StdioTransport({ executable: FAKE, cwd: dir, env: { FAKE_SERVE_REPO_DIR_LOG: repoLog }, timeoutMs: 5000 });
    try {
      assert.ok((await t2.ask({ query: "sparql", name: "count", format: "sparql-results+json" })).ok);
      assert.equal(readFileSync(repoLog, "utf8").trim(), "(unset)");
    } finally {
      t2.dispose();
    }
  } finally {
    if (before === undefined) delete process.env["COCKPIT_REPO_DIR"];
    else process.env["COCKPIT_REPO_DIR"] = before;
    t.dispose();
  }
});

test("stdio: a cockpit that cannot be started is 'cannot run' and not sent (Not signed), as on the command line", async () => {
  const t = new StdioTransport({ executable: "/nonexistent/cockpit", cwd: tmpdir(), timeoutMs: 2000 });
  await assert.rejects(t.say({ template: "note", subject: `sha256:${"a".repeat(64)}`, fields: { text: "x" }, as: "qa-decider" }), (e: Error) => e instanceof TransportNotSent && /cannot run \/nonexistent\/cockpit/.test(e.message));
  t.dispose();
});

test("stdio: say/batch is used once initialize lists it; params {claims}; claims answered in order; a refusal is the cockpit's", async () => {
  const { t, sent } = transport(5000, { FAKE_SERVE_METHODS: "say,say/batch" });
  try {
    const reqs = ["a", "b"].map((x) => ({ template: "note", subject: `sha256:${x.repeat(64)}`, fields: { text: x }, as: "k" }));
    const out = await t.sayBatch(reqs);
    assert.ok(out.ok);
    assert.deepEqual(out.value.claims.map((c) => c.claimId), [`sha256:${"1".repeat(64)}`, `sha256:${"2".repeat(64)}`]);
    const m = sent().filter((x) => x.method === "say/batch");
    assert.equal(m.length, 1);
    assert.deepEqual((m[0]?.params as { claims: unknown[] }).claims, reqs);
    const bad = await t.sayBatch([reqs[0] as (typeof reqs)[number], { ...(reqs[1] as (typeof reqs)[number]), fields: { text: "refuse" } }]);
    assert.ok(!bad.ok && bad.refusal.reason === "claim 2: refused");
  } finally {
    t.dispose();
  }
});

test("stdio: without say/batch among the initialized methods it is not offered: BATCH_UNSUPPORTED, nothing sent", async () => {
  const { t, sent } = transport();
  try {
    const out = await t.sayBatch([{ template: "note", subject: `sha256:${"a".repeat(64)}`, fields: { text: "x" } }]);
    assert.ok(!out.ok && out.refusal.code === "batch.unsupported");
    assert.equal(sent().filter((x) => x.method === "say/batch").length, 0);
  } finally {
    t.dispose();
  }
});
