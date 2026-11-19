// Opt-in (npm run test:integration, not part of `npm test`): runs the real cockpit binary against a
// COPY of a scratch repository. Asked for without its inputs, it fails: it never skips.
//   COCKPIT_IT_BIN   path to a cockpit binary (v0.5.0 or later)
//   COCKPIT_IT_REPO  a scratch repo made with `cockpit init` + `keygen`; it is copied, never written
// Keys stay where the repo's identity.json points (outside the repo). No network is used.
import { test } from "node:test";
import assert from "node:assert/strict";
import { cpSync, mkdtempSync, readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { CliTransport } from "../src/transport/cli.js";
import { readIdentityHint } from "../src/identity.js";

const BIN = process.env["COCKPIT_IT_BIN"];
const REPO = process.env["COCKPIT_IT_REPO"];

test("real cockpit: doctor, say (flag form, sha256 subject), ask about", async () => {
  assert.ok(BIN && REPO, "set COCKPIT_IT_BIN and COCKPIT_IT_REPO: this test was asked for, so it does not skip");
  const work = join(mkdtempSync(join(tmpdir(), "cockpit-it-")), "repo");
  cpSync(REPO as string, work, { recursive: true });
  const t = new CliTransport({ executable: BIN as string, cwd: work, timeoutMs: 60_000 });

  const d = await t.doctor();
  assert.equal(d.ready, true, JSON.stringify(d.steps));

  const hint = await readIdentityHint(work);
  assert.ok(hint, "the scratch repo has .kton/local/identity.json");

  const subject = "sha256:" + createHash("sha256").update(readFileSync(join(work, "data.txt"))).digest("hex");
  const said = await t.say({ template: "working-on", subject, fields: { step: "integration" } });
  assert.ok(said.ok, said.ok ? "" : said.refusal.reason);
  assert.match(said.value.claimId, /^sha256:[0-9a-f]{64}$/);

  const about = await t.ask({ query: "about", ref: subject });
  assert.ok(about.ok, about.ok ? "" : about.refusal.reason);
  assert.ok(JSON.stringify(about.value).includes(said.value.claimId.slice(7)), "the new claim is in the answer");

  const refused = await t.say({ template: "nosuch", subject });
  assert.ok(!refused.ok);
  assert.match(refused.refusal.reason, /not in this repo's allowed claim templates/);
});
