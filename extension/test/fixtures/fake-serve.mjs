#!/usr/bin/env node
// A stand-in for `cockpit serve --stdio` (SPEC §12.5) for the StdioTransport's unit tests: Content-Length framing,
// initialize first, refusals as -32001 {code, clause, reason}, a method that never answers until $/cancelRequest.
// FAKE_SERVE_LOG: every message received, one JSON per line.
import { appendFileSync } from "node:fs";
if (process.argv[2] !== "serve" || process.argv[3] !== "--stdio") process.exit(9);
const log = (m) => process.env.FAKE_SERVE_LOG && appendFileSync(process.env.FAKE_SERVE_LOG, JSON.stringify(m) + "\n");
let buf = Buffer.alloc(0);
let as = null;
let initialized = false;
const slow = new Map();
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
    return send({ id, result: { repoRoot: params.root } });
  }
  if (!initialized) return send({ id, error: { code: -32002, message: "not initialized" } });
  switch (method) {
    case "ask":
      if (params.name === "slow") return slow.set(id, true);
      if (params.name === "garbage") return process.stdout.write("Content-Length: 16\r\n\r\nthis is not json");
      if (params.name === "noheader") return process.stdout.write("this is not json\r\n\r\n");
      if (params.name === "die") return process.exit(137);
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
      return send({ id, result: { claimId: `sha256:${"c".repeat(64)}`, confirmation: `signed as ${params.as ?? as}` } });
    case "say/templates":
      return send({ id, result: [{ name: "note", about: "either", fields: { text: { type: "string", required: true } }, hash: `sha256:${"1".repeat(64)}` }] });
    case "ui/cat":
      return send({ id, result: { package: params.package, name: "p", dir: ".kton/packages/p@1", members: params.hashes.map((h) => ({ hash: h, bytes: Buffer.from(`bytes of ${h.slice(7, 15)}`).toString("base64") })) } });
    case "shutdown":
      return send({ id, result: null });
    case "exit":
      return process.exit(0);
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
