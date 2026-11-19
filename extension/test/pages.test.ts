// Every PR 1 webview carries the host-owned CSP: default-src 'none', style and script by nonce,
// connect-src 'none', no 'unsafe-inline' and no style= attributes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { composePage, csp, decisionPage, resultsPage, signDialogPage, threadPage } from "../src/ui/pages.js";

const N = "n0nce";
const row = { msg: `https://kton.dev/o/${"1".repeat(64)}`, title: "T", text: "x", options: JSON.stringify([{ id: "a", label: "A" }]) };
const pages: Record<string, string> = {
  decision: decisionPage(row, N, { records: [{ by: "agent-claims", byTier: "agent", choice: "a" }] }),
  decisionDisabled: decisionPage(row, N, { cannotSign: "no person key" }),
  thread: threadPage(row, [], N, { answerable: true, draft: "d", error: "e" }),
  threadAnswered: threadPage(row, [{ by: "a", byTier: "self", outcome: "ok" }], N),
  compose: composePage({ name: "requests", fields: { title: { type: "string", required: true } } }, N, { values: { title: "t" }, missing: ["about"], error: "x" }),
  results: resultsPage("ask x", [{ a: "1" }], N),
  signDialog: signDialogPage("Sign", "detail", "person", N),
};

test("every PR 1 page: host-owned CSP, no 'unsafe-inline', connect-src 'none', no style attributes", () => {
  for (const [name, html] of Object.entries(pages)) {
    const metas = [...html.matchAll(/<meta http-equiv="Content-Security-Policy" content="([^"]*)">/g)].map((m) => m[1]);
    assert.equal(metas.length, 1, `${name}: one CSP`);
    const policy = metas[0] ?? "";
    assert.equal(policy, csp(N), name);
    assert.ok(!policy.includes("unsafe-inline"), `${name}: no 'unsafe-inline'`);
    assert.match(policy, /(^|; )default-src 'none'(;|$)/, name);
    assert.match(policy, /(^|; )connect-src 'none'(;|$)/, name);
    assert.match(policy, /(^|; )style-src 'nonce-n0nce'(;|$)/, name);
    assert.match(policy, /(^|; )script-src 'nonce-n0nce'(;|$)/, name);
    assert.ok(!/\sstyle=/.test(html), `${name}: no style= attribute`);
    for (const tag of html.matchAll(/<(style|script)\b([^>]*)>/g)) assert.match(tag[2] ?? "", /nonce="n0nce"/, `${name}: <${tag[1]}> carries the nonce`);
  }
});
