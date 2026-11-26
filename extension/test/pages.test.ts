// Every PR 1 webview carries the host-owned CSP: default-src 'none', style and script by nonce,
// connect-src 'none', no 'unsafe-inline' and no style= attributes.
import { test } from "node:test";
import assert from "node:assert/strict";
import { composePage, csp, decisionPage, hashChip, resultsPage, shortenHashes, signDialogPage, threadPage } from "../src/ui/pages.js";

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

test("a 64-hex hash is shown as 8 hex with the full value in the tooltip and for copying; the decision head uses it", () => {
  const full = `sha256:${"ab".repeat(32)}`;
  const c = hashChip(full);
  assert.match(c, /class="hash mono"[^>]*title="sha256:(ab){32}"[^>]*data-copy="sha256:(ab){32}">sha256:abababab…</);
  assert.equal(hashChip("not a hash"), "not a hash");
  const t = shortenHashes(`run-artifact:r/x.png@${full} and https://kton.dev/o/${"cd".repeat(32)}`);
  assert.equal((t.match(/class="hash mono"/g) ?? []).length, 2);
  assert.doesNotMatch(t.replace(/<span class="hide">[^<]*<\/span>/g, "").replace(/title="[^"]*"|data-copy="[^"]*"/g, ""), /[0-9a-f]{64}/, "no 64-hex in the visible text");
  assert.match(decisionPage(row, N), /class="hash mono"[^>]*>sha256:11111111…/);
});
