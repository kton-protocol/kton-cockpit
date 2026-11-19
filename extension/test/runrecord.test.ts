// The e2e writes out/run.json as kton-testrun/1. Here: the builder's record validates against the
// schema, and so does the run.json of the last e2e run if there is one (RUN_JSON=<path> to point at it).
import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const Ajv2020 = require("ajv/dist/2020") as new (o: object) => { compile(s: object): ((d: unknown) => boolean) & { errors?: unknown } };
const addFormats = require("ajv-formats") as (a: unknown) => void;
const schema = JSON.parse(readFileSync(join(process.cwd(), "test", "e2e", "testrun.schema.json"), "utf8")) as object;

function validator() {
  const ajv = new Ajv2020({ strict: false, allErrors: true });
  addFormats(ajv);
  return ajv.compile(schema);
}

test("run record: the builder's kton-testrun/1 record validates against the schema, one unit per flow", async () => {
  const { runRecord, FLOWS } = (await import(join(process.cwd(), "test", "e2e", "runrecord.mjs"))) as {
    runRecord: (a: object) => Record<string, unknown> & { units: Array<{ id: string; result: string; steps: unknown[] }> };
    FLOWS: Record<string, string>;
  };
  const steps = [
    { flow: "S1", n: 1, title: "status bar", ok: true, ms: 10, screenshots: ["s1-status-bar.png", "../home/.kton-keys/x.key"] },
    { flow: "S7", n: 2, title: "ask non-JSON", ok: false, ms: 5, error: "timed out" },
  ];
  const rec = runRecord({ steps, started: "2026-11-19T10:00:00Z", durationMs: 15, native: { file: "smoke-results.json", sha256: "a".repeat(64) } });
  const validate = validator();
  assert.ok(validate(rec), JSON.stringify(validate.errors));
  assert.deepEqual(rec.units.map((u) => u.id), Object.keys(FLOWS).map((f) => `lane:${f}`));
  assert.equal(rec["result"], "failed");
  assert.equal(rec.units[0]?.result, "passed");
  assert.deepEqual((rec.units[0]?.steps[0] as { screenshots: string[] }).screenshots, ["s1-status-bar.png"], "only PNG file names, never a path");
  assert.equal(rec["suite"], "kton-workbench-shell");
  assert.equal(rec["world"], "shell-e2e");
  assert.deepEqual(rec["target"], { name: "throwaway-registry" });
  assert.deepEqual(rec["account"], { user: "qa-decider", admin: false });
});

test("run record: the last e2e run's out/run.json validates (when there is one)", () => {
  const file = process.env["RUN_JSON"] ?? join(process.cwd(), "test", "e2e", "out", "run.json");
  if (!existsSync(file)) {
    // Not a skip of a check: without an e2e run there is no file; the check above covers the builder.
    assert.ok(!process.env["RUN_JSON"], `RUN_JSON names ${file}, which does not exist`);
    return;
  }
  const validate = validator();
  const rec = JSON.parse(readFileSync(file, "utf8")) as unknown;
  assert.ok(validate(rec), JSON.stringify(validate.errors));
});

test("e2e vocabulary package: it brings exactly the queries in vocabulary/package-queries, byte for byte", () => {
  const e2e = join(process.cwd(), "test", "e2e");
  const src = readdirSync(join(e2e, "vocabulary", "package-queries")).filter((f) => f.endsWith(".rq")).sort();
  assert.deepEqual(src, ["answers.rq", "decides.rq"]);
  const blobs = readdirSync(join(e2e, "vocabulary-package", "blobs")).flatMap((d) => readdirSync(join(e2e, "vocabulary-package", "blobs", d))).sort();
  const want = src.map((f) => createHash("sha256").update(readFileSync(join(e2e, "vocabulary", "package-queries", f))).digest("hex")).sort();
  assert.deepEqual(blobs, want, "rebuild the package: test/e2e/build-vocabulary-package.sh");
  // and the checkout's query folder of the e2e carries no decision query
  assert.ok(!readdirSync(join(e2e, "vocabulary", "queries")).some((f) => /^(decides|answers)\.rq$/.test(f)));
  // a package never carries a private key
  assert.deepEqual(readdirSync(join(e2e, "vocabulary-package", "keys")).filter((f) => !f.endsWith(".pub")), []);
});
