// The review checks for the shell (contract C1.2–C1.4, "PR review checks"), one named test each:
// 1 verified signer and tier only from the cockpit's answer; 2 intents are untrusted, meaning and
// templates from the cockpit's listing; 3 the name from the trust config, not identity.json; 4 argv,
// never a shell, no flag value or positional starting with "-"; 5 a shape check for every parsed answer;
// 6 the person key only with the dialog's consent, gated on the key; 7 a decision is recognised by its
// signer (controller.test.ts, "review check 7").
import { test } from "node:test";
import assert from "node:assert/strict";
import { fakeCockpit, sparqlAnswer, CLAIM, SUBJECT, PERSON_KEY, AGENT_KEY } from "./helpers.js";
import { mkdirSync, mkdtempSync, readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { claimKeyId, keyIdOfPublicKey, signerKeyId } from "../src/identity.js";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readIdentityHint, showSigner, statusText, type WindowIdentity } from "../src/identity.js";
import { plan, sign, DialogConsent, type ControlInfo } from "../src/sign.js";
import { OperatorConfirmation, TransportError, type TemplateInfo } from "../src/transport/port.js";

const identity: WindowIdentity = {
  hintName: "Name From identity.json",
  person: { role: "person", keyName: "person-a", keyId: PERSON_KEY },
  agent: { role: "agent", keyName: "person-a-agent", keyId: AGENT_KEY },
};

test("check 1+3: name and tier come from the cockpit's answer, never from identity.json", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "ask", stdout: sparqlAnswer([{ name: "Trust Config Name", tier: "person" }]) }]);
  const s = await showSigner(transport, PERSON_KEY, identity.hintName, "view");
  assert.ok(s.verified);
  assert.equal(s.name, "Trust Config Name");
  assert.equal(s.tier, "person");
  const req = JSON.parse(argvLog()[0]?.[1] ?? "{}") as { bind: Record<string, string>; sparql: string };
  assert.deepEqual(req.bind, { KEYID: PERSON_KEY });
  assert.ok(!req.sparql.includes(PERSON_KEY), "the key id is bound, not spliced into the query");
});

test("check 1: an unknown key is shown as unverified with the hint, never as verified", async () => {
  const { transport } = fakeCockpit([{ argv0: "ask", stdout: sparqlAnswer([]) }]);
  const s = await showSigner(transport, AGENT_KEY, identity.hintName, "view");
  assert.ok(!s.verified);
  assert.match(statusText(s, s), /unverified/);
});

test("check 1: no cache: every render asks again", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "ask", stdout: sparqlAnswer([{ name: "A", tier: "person" }]) }]);
  await showSigner(transport, PERSON_KEY, "x", "view");
  await showSigner(transport, PERSON_KEY, "x", "view");
  assert.equal(argvLog().length, 2);
});

test("until kton-cockpit has an identity verb, name and tier are shown unverified without asking", async () => {
  const { transport, argvLog } = fakeCockpit([]);
  const s = await showSigner(transport, PERSON_KEY, "probe");
  assert.ok(!s.verified);
  assert.equal(argvLog().length, 0);
  assert.match(statusText(s, s), /probe \(unverified: this cockpit has no identity verb yet\)/);
});

test("the identity file is read as a hint (ADR-016 .kton/local/identity.json)", async () => {
  const dir = mkdtempSync(join(tmpdir(), "idhint-"));
  mkdirSync(join(dir, ".kton", "local"), { recursive: true });
  writeFileSync(join(dir, ".kton", "local", "identity.json"), JSON.stringify({ session_id: "probe", nekton_key: "/k/probe-claims.key" }));
  assert.deepEqual(await readIdentityHint(dir), { sessionId: "probe", nektonKey: "/k/probe-claims.key" });
  assert.equal(await readIdentityHint(join(dir, "nowhere")), undefined);
});

const templates: TemplateInfo[] = [
  { name: "approves", package: "render", fields: { meaning: { type: "string", required: true, values: ["approved", "reviewed"] }, note: { type: "string" } } },
  { name: "edits", package: "mignon", fields: { text: { type: "string", required: true } } },
];
const approve: ControlInfo = { package: "render", name: "approve", template: "approves", meaning: "approved", kind: "decision" };
const edit: ControlInfo = { package: "mignon", name: "edit", template: "edits", kind: "record" };

test("check 2: the meaning comes from the control; an intent cannot set it or the key", () => {
  const p = plan({ kind: "sign", about: SUBJECT, fields: { note: "looks fine" } }, approve, templates, identity);
  assert.ok(p.ok);
  assert.equal(p.value.meaning, "approved");
  const m = plan({ kind: "sign", about: SUBJECT, fields: { meaning: "reviewed" } }, approve, templates, identity);
  assert.ok(!m.ok);
  const a = plan({ kind: "sign", about: SUBJECT, fields: { as: "someone-else" } }, approve, templates, identity);
  assert.ok(!a.ok);
});

test("check 2: unknown fields and values outside the template are refused", () => {
  assert.ok(!plan({ kind: "sign", about: SUBJECT, fields: { extra: "x" } }, approve, templates, identity).ok);
  const loose: ControlInfo = { ...approve, meaning: undefined } as unknown as ControlInfo;
  assert.ok(!plan({ kind: "sign", about: SUBJECT, fields: { meaning: "released" } }, loose, templates, identity).ok);
});

test("O1: a decision needs the dialog's consent for exactly this plan; a record does not", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "" }) }]);
  const p = plan({ kind: "sign", about: SUBJECT, fields: {} }, approve, templates, identity);
  assert.ok(p.ok);
  assert.equal(p.value.as, "person-a");
  assert.equal(p.value.needsDialog, true);
  const noDialog = await sign(transport, p.value, undefined);
  assert.ok(!noDialog.ok);
  assert.equal(noDialog.refusal.code, "dialog.required");
  const other = plan({ kind: "sign", about: SUBJECT, fields: { note: "x" } }, approve, templates, identity);
  assert.ok(other.ok);
  const wrong = await sign(transport, p.value, DialogConsent.fromDialog(other.value));
  assert.ok(!wrong.ok);
  assert.equal(argvLog().length, 0, "nothing reached the cockpit");
  const ok = await sign(transport, p.value, DialogConsent.fromDialog(p.value));
  assert.ok(ok.ok);
  assert.equal(ok.value.id, CLAIM);

  const r = plan({ kind: "sign", about: SUBJECT, fields: { text: "line 3 changed" } }, edit, templates, identity);
  assert.ok(r.ok);
  assert.equal(r.value.as, "person-a-agent");
  assert.equal(r.value.needsDialog, false);
  assert.ok((await sign(transport, r.value, undefined)).ok);
});

test("check 4: hostile field values reach the cockpit verbatim as one argv element", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "" }) }]);
  const evil = `$(touch /tmp/pwned); rm -rf / ' " \` --as other`;
  await transport.say({ template: "edits", subject: SUBJECT, fields: { text: evil } });
  assert.deepEqual(argvLog()[0], ["say", "edits", SUBJECT, `text=${evil}`, "--json"]);
});

test("check 5: a wrong answer shape is a TransportError, never half read", async () => {
  const cases = [
    { argv0: "say", stdout: JSON.stringify({ claimId: "not-an-id" }) },
    { argv0: "ask", stdout: JSON.stringify({ ref: "x" }) },
    { argv0: "publish", stdout: "[]" },
  ];
  for (const c of cases) {
    const { transport } = fakeCockpit([c]);
    const call =
      c.argv0 === "say"
        ? transport.say({ template: "t", subject: SUBJECT })
        : c.argv0 === "ask"
          ? transport.ask({ query: "about", ref: SUBJECT })
          : transport.publish({ inputs: [], cmd: "true" });
    await assert.rejects(call, TransportError, c.argv0);
  }
  const { transport } = fakeCockpit([{ argv0: "ask", stdout: JSON.stringify({ query: "sparql", results: { head: {}, results: {} } }) }]);
  await assert.rejects(transport.ask({ query: "sparql", sparql: "ASK {}" }), TransportError);
});

test("check 2: a template that exists only in the checkout is not signable: templates come from the cockpit's listing", async () => {
  // A template file in the checkout (where the old stand-in read them) that the cockpit does not list.
  const dir = mkdtempSync(join(tmpdir(), "checkout-"));
  mkdirSync(join(dir, ".kton", "templates"), { recursive: true });
  writeFileSync(join(dir, ".kton", "templates", "approves.json"), JSON.stringify({ name: "approves", fields: { meaning: { type: "string" } } }));
  const { transport, argvLog } = fakeCockpit(
    [
      { argv0: "say", argv1: "--json", stdout: JSON.stringify([{ name: "edits", about: "either", fields: { text: { type: "string", required: true } } }]) },
      { argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "" }) },
    ],
    { cwd: dir },
  );
  const listed = await transport.templates();
  assert.ok(listed.ok);
  const p = plan({ kind: "sign", about: SUBJECT, fields: { meaning: "approved" } }, approve, listed.value, identity);
  assert.ok(!p.ok);
  assert.equal(p.refusal.code, "template.not-allowed");
  assert.deepEqual(argvLog().map((a) => a.slice(0, 2)), [["say", "--json"]], "nothing but the listing reached the cockpit");
  // and the shell has no code path that reads a template file
  for (const f of sources()) assert.ok(!/templates\/\*?\.json|readdir\([^)]*templates/.test(readFileSync(f, "utf8")), `${f} reads template files`);
});

test("check 4: no flag value or positional starts with '-': each position is refused or reaches the cockpit as key=value", async () => {
  const { transport, argvLog } = fakeCockpit([
    { argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "" }) },
    { argv0: "workflow", argv1: "run", stdout: "{}" },
  ]);
  const refusedSay: Array<[string, Parameters<typeof transport.say>[0]]> = [
    ["template", { template: "-x", subject: SUBJECT }],
    ["subject", { template: "edits", subject: "-x" }],
    ["field name", { template: "edits", subject: SUBJECT, fields: { "-x": "v" } }],
    ["as", { template: "edits", subject: SUBJECT, as: "-x" }],
    ["scope", { template: "edits", subject: SUBJECT, scope: "-x" }],
    ["scope", { template: "edits", subject: SUBJECT, scope: "--as=other" }],
    ["expectHead", { template: "edits", subject: SUBJECT, expectHead: "-x" }],
  ];
  for (const [pos, req] of refusedSay) {
    const out = await transport.say(req);
    assert.ok(!out.ok, `say ${pos} "-x" must be refused`);
  }
  assert.ok(!(await transport.workflowRun({ workflow: "-x", bind: {} })).ok, "workflow name");
  assert.ok(!(await transport.workflowRun({ workflow: "render", bind: { "-x": "v" } })).ok, "hole name");
  assert.ok(!(await transport.install({ packageDir: "-x", allow: false }, OperatorConfirmation.fromPerson("x"))).ok, "package folder");
  assert.ok(!(await transport.ask({ query: "about", ref: "-x" })).ok, "a lineage ref");
  assert.equal(argvLog().length, 0, "none of them reached the cockpit");
  // values: they travel as key=value, which starts with the key, never as a flag
  await transport.say({ template: "edits", subject: SUBJECT, fields: { text: "-rf --as other" }, scope: "lanes" });
  assert.deepEqual(argvLog()[0], ["say", "edits", SUBJECT, "text=-rf --as other", "--scope", "lanes", "--json"]);
  await transport.workflowRun({ workflow: "render", bind: { source: "-x" } });
  assert.deepEqual(argvLog()[1], ["workflow", "run", "render", "--bind", "source=-x", "--json"]);
  for (const argv of argvLog()) for (const [i, a] of argv.entries()) if (a.startsWith("-")) assert.ok(/^--[a-z-]+$/.test(a) && !argv[i - 1]?.startsWith("--b"), `${a} is a flag the shell wrote, not a value`);
});

test("check 5: workflow run and install answers are shape-checked, and the shell reads no field of them", async () => {
  for (const [argv0, call] of [
    ["workflow", (t: ReturnType<typeof fakeCockpit>["transport"]) => t.workflowRun({ workflow: "render", bind: {} })],
    ["install", (t: ReturnType<typeof fakeCockpit>["transport"]) => t.install({ packageDir: "pkg", allow: false }, OperatorConfirmation.fromPerson("x"))],
  ] as const) {
    const { transport } = fakeCockpit([{ argv0, stdout: "[1, 2]" }]);
    await assert.rejects(call(transport), TransportError, `${argv0}: a list is not an answer`);
  }
  // nothing outside the transport calls them, so nothing reads a field of their answers
  for (const f of sources().filter((f) => !f.includes(`${join("src", "transport")}`))) {
    assert.ok(!/\.(workflowRun|install)\(/.test(readFileSync(f, "utf8")), `${f} calls workflowRun or install`);
  }
});

test("check 6: a person-key plan marked needsDialog: false is refused before the transport", async () => {
  const { transport, argvLog } = fakeCockpit([{ argv0: "say", stdout: JSON.stringify({ claimId: CLAIM, confirmation: "" }) }]);
  const p = plan({ kind: "sign", about: SUBJECT, fields: {} }, approve, templates, identity);
  assert.ok(p.ok);
  assert.equal(p.value.keyRole, "person");
  const forged = { ...p.value, needsDialog: false };
  const out = await sign(transport, forged, undefined);
  assert.ok(!out.ok);
  assert.equal(out.refusal.code, "dialog.required");
  assert.equal(argvLog().length, 0, "nothing reached the cockpit");
});

test("check 6: DialogConsent.fromDialog has exactly one call site in src/, right after the sign dialog said yes", () => {
  const calls = sources().flatMap((f) => {
    const lines = readFileSync(f, "utf8").split("\n");
    return lines.map((l, i) => ({ f, i, l, before: lines.slice(Math.max(0, i - 8), i).join("\n") })).filter((x) => /DialogConsent\.fromDialog\(/.test(x.l));
  });
  assert.equal(calls.length, 1, calls.map((c) => `${c.f}:${c.i + 1}`).join(", "));
  const c = calls[0]!;
  assert.match(c.f, /controller\.ts$/);
  assert.match(c.before, /const yes = await this\.host\.signDialog\(/);
  assert.match(c.before, /if \(!yes\) \{/);
});

/** Every TypeScript file under src/. */
function sources(dir = join(process.cwd(), "src")): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    return statSync(p).isDirectory() ? sources(p) : p.endsWith(".ts") ? [p] : [];
  });
}

test("check 7 (Q2): the person key id comes from the window's public key, as the kernels make it; doctor must agree when it speaks for that key", async () => {
  // a v0.5.1 `doctor --json` gave nektonKeyId 819f1c7595e4b466 for a claims key with this public half
  const pub = "1816edf7de7ef2334957a05873d28013fa6bdf869e80d6a9c084695efd464086";
  assert.equal(keyIdOfPublicKey(pub), "819f1c7595e4b466");
  assert.equal(keyIdOfPublicKey("not hex"), undefined);
  assert.equal(signerKeyId("https://kton.dev/agent/819f1c7595e4b466"), "819f1c7595e4b466");
  assert.equal(signerKeyId("qa-decider-claims"), undefined, "a label is never a key id");
  assert.equal(signerKeyId("https://kton.dev/agent/819f1c7595e4b466x"), undefined);

  const ring = mkdtempSync(join(tmpdir(), "keyring-"));
  writeFileSync(join(ring, "qa-decider-claims.pub"), pub + "\n");
  const hint = { sessionId: "qa-decider", keyring: ring, nektonKey: join(ring, "qa-decider-claims.key") };
  const doctor = (nektonKeyId: string) => ({ ready: true, steps: [], fields: { identity: { name: "anything", nektonKeyId, tier: "self" } } });
  assert.deepEqual(await claimKeyId(hint, "qa-decider", doctor("819f1c7595e4b466")), { ok: true, keyId: "819f1c7595e4b466", checkedByDoctor: true });
  const wrong = await claimKeyId(hint, "qa-decider", doctor("0000000000000000"));
  assert.ok(!wrong.ok && /does not belong/.test(wrong.why), "a .pub that is not the signing key's gives no key id");
  // doctor speaks for another (default) key: the .pub alone; doctor's name is never used
  assert.deepEqual(await claimKeyId({ ...hint, nektonKey: join(ring, "other-claims.key") }, "qa-decider", doctor("0000000000000000")), { ok: true, keyId: "819f1c7595e4b466", checkedByDoctor: false });
  assert.ok(!(await claimKeyId(hint, "qa-missing")).ok, "no .pub, no key id");
  assert.ok(!(await claimKeyId({ sessionId: "x" }, "qa-decider")).ok, "no keyring, no key id");
  assert.ok(!(await claimKeyId(hint, "")).ok);
});
