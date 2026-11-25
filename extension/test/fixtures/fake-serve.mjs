#!/usr/bin/env node
// A stand-in for `cockpit serve --stdio` (SPEC §12.5) for the StdioTransport's unit tests: Content-Length framing,
// initialize first, refusals as -32001 {code, clause, reason}, a method that never answers until $/cancelRequest.
// FAKE_SERVE_LOG: every message received, one JSON per line.
import { spawn } from "node:child_process";
import { appendFileSync, closeSync } from "node:fs";
if (process.argv[2] !== "serve" || process.argv[3] !== "--stdio") process.exit(9);
const log = (m) => process.env.FAKE_SERVE_LOG && appendFileSync(process.env.FAKE_SERVE_LOG, JSON.stringify(m) + "\n");
let buf = Buffer.alloc(0);
let as = null;
let initialized = false;
const slow = new Map();
// answers still to come (name "later", say text "late"): exit waits for them, as §12.5's exit does
let later = 0;
let exiting = false;
const settle = () => exiting && later === 0 && process.exit(0);
const after = (ms, fn) => {
  later++;
  setTimeout(() => {
    fn();
    later--;
    settle();
  }, ms);
};
const ASK = (v) => ({ query: "sparql", ref: "", raw: "", sparql: { results: { head: { vars: ["n"] }, results: { bindings: [{ n: { type: "literal", value: v } }] } } } });
const send = (m) => {
  const b = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...m }));
  process.stdout.write(`Content-Length: ${b.length}\r\n\r\n`);
  process.stdout.write(b);
};
const refuse = (id, code, reason) => send({ id, error: { code: -32001, message: reason, data: { code, clause: "SPEC §9", reason } } });
function handle(m) {
  log(m);
  const { id, method, params } = m;
  if (method === "$/cancelRequest") {
    const s = slow.get(params.id);
    if (s !== undefined) send({ id: params.id, error: { code: -32800, message: "cancelled" } });
    return;
  }
  if (method === "initialize") {
    if (initialized) return refuse(id, "initialize", "initialized twice");
    initialized = true;
    as = params.as ?? null;
    if (process.env.FAKE_SERVE_REPO_DIR_LOG) appendFileSync(process.env.FAKE_SERVE_REPO_DIR_LOG, `${process.env.COCKPIT_REPO_DIR ?? "(unset)"}\n`);
    // S1: a process that closes its stdin after initialize and stays: every later write to it is EPIPE. Closed
    // before the answer: a request the client writes once it has the answer is never taken in silently.
    if (process.env.FAKE_SERVE_CLOSE_STDIN) {
      process.stdin.destroy(); // not enough: the fd stays open until it is closed
      closeSync(0);
      setInterval(() => {}, 1000);
    }
    return send({ id, result: { repoRoot: params.root, ...(process.env.FAKE_SERVE_METHODS ? { methods: process.env.FAKE_SERVE_METHODS.split(",") } : {}) } });
  }
  if (!initialized) return send({ id, error: { code: -32002, message: "not initialized" } });
  switch (method) {
    case "ask":
      if (params.name === "slow") return slow.set(id, true);
      if (params.name === "garbage") return process.stdout.write("Content-Length: 16\r\n\r\nthis is not json");
      if (params.name === "noheader") return process.stdout.write("this is not json\r\n\r\n");
      if (params.name === "die") return process.exit(137);
      if (params.name === "later") return after(300, () => send({ id, result: ASK("later") }));
      if (params.name === "split") {
        // one answer in pieces: the header split, the body split inside a multi-byte character
        const b = Buffer.from(JSON.stringify({ jsonrpc: "2.0", id, result: ASK("Grüße – 検査 ✓ 𝄞") }));
        const all = Buffer.concat([Buffer.from(`Content-Length: ${b.length}\r\n\r\n`), b]);
        const cut = [7, 20, all.indexOf(Buffer.from("検")) + 1, all.indexOf(Buffer.from("𝄞")) + 2, all.length];
        const pieces = [];
        let at = 0;
        for (const c of cut) {
          pieces.push(all.subarray(at, c));
          at = c;
        }
        pieces.forEach((piece, i) => setTimeout(() => process.stdout.write(piece), 20 * i));
        return;
      }
      if (params.name === "orphan") {
        // Y2, deterministic: this process exits at once; a child that shares its stdout writes the answer 200 ms
        // later. 'exit' comes before the answer; stdout closes ('close') only after it.
        const b = JSON.stringify({ jsonrpc: "2.0", id, result: ASK("after exit") });
        const frame = `Content-Length: ${Buffer.byteLength(b)}\r\n\r\n${b}`;
        spawn(process.execPath, ["-e", `setTimeout(() => process.stdout.write(${JSON.stringify(frame)}), 200)`], { stdio: ["ignore", "inherit", "inherit"] });
        return process.exit(0);
      }
      if (params.name === "bigexit") {
        // a large answer written, then exit at once: 'exit' can come before stdout is drained (Y2)
        const b = Buffer.from(JSON.stringify({ jsonrpc: "2.0", id, result: { ...ASK("big"), raw: "x".repeat(4 << 20) } }));
        // exit as soon as the bytes are handed to the pipe (stdout is asynchronous here), before the client read them
        return process.stdout.write(Buffer.concat([Buffer.from(`Content-Length: ${b.length}\r\n\r\n`), b]), () => process.exit(0));
      }
      if (params.name === "stray") {
        // parsable answers that name no request of the client's: an unknown number id, the same id as a string
        send({ id: 99999, result: { stray: true } });
        send({ id: String(id), error: { code: -32000, message: "not yours", data: { code: "error" } } });
        send({ method: "$/progress", params: { id } });
      }
      if (params.name === "nope") return refuse(id, "query.unknown", "no allowed package brings the query nope");
      return send({ id, result: { query: "sparql", ref: "", raw: "", sparql: { results: { head: { vars: ["n"] }, results: { bindings: [{ n: { type: "literal", value: "7" } }] } } } } });
    case "say":
      if (as && params.as && params.as !== as) return refuse(id, "identity.as", `this connection signs as ${as}`);
      if (params.fields?.text === "late") return after(400, () => send({ id, result: { claimId: `sha256:${"d".repeat(64)}`, confirmation: "signed late" } }));
      return send({ id, result: { claimId: `sha256:${"c".repeat(64)}`, confirmation: `signed as ${params.as ?? as}` } });
    case "say/batch":
      if (params.claims.some((c) => c.fields?.text === "refuse")) return refuse(id, "argument", `claim ${1 + params.claims.findIndex((c) => c.fields?.text === "refuse")}: refused`);
      return send({ id, result: { claims: params.claims.map((_, i) => ({ claimId: `sha256:${String(i + 1).repeat(64)}`, confirmation: "one batch" })) } });
    case "say/templates":
      return send({ id, result: [{ name: "note", about: "either", fields: { text: { type: "string", required: true } }, hash: `sha256:${"1".repeat(64)}` }] });
    case "ui/cat":
      return send({ id, result: { package: params.package, name: "p", dir: ".kton/packages/p@1", members: params.hashes.map((h) => ({ hash: h, bytes: Buffer.from(`bytes of ${h.slice(7, 15)}`).toString("base64") })) } });
    case "shutdown":
      return send({ id, result: null });
    case "exit":
      exiting = true;
      return settle();
    default:
      return send({ id, error: { code: -32601, message: `unknown method ${method}` } });
  }
}
process.stdin.on("data", (d) => {
  buf = Buffer.concat([buf, d]);
  for (;;) {
    const end = buf.indexOf("\r\n\r\n");
    if (end < 0) return;
    const len = Number(/Content-Length: (\d+)/i.exec(buf.subarray(0, end).toString())[1]);
    if (buf.length < end + 4 + len) return;
    const body = buf.subarray(end + 4, end + 4 + len).toString();
    buf = buf.subarray(end + 4 + len);
    handle(JSON.parse(body));
  }
});
