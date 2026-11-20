// Two answers recorded from a real cockpit v0.5.4 (test/fixtures/cockpit-v054), which PR 2's shell misread:
// a named query's rows (the JSON-argument form answers under sparql.results), and a package with an alias file
// (ui list names its aliases member ""). Fix on #174.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { PackagesArea, type ActPath, type CompositionUi } from "../src/composition/area.js";
import { loadPackages } from "../src/packages/model.js";
import type { UiPackage } from "../src/packages/listing.js";
import { assertUiListing } from "../src/packages/listing.js";
import { fakeCockpit } from "./helpers.js";
import { loadVendored } from "../src/packages/vendored.js";

const FIX = join(process.cwd(), "test", "fixtures", "cockpit-v054");

test("a named query's rows from a real cockpit answer (sparql.results) reach the shell: one row, n = 20", async () => {
  const stdout = readFileSync(join(FIX, "ask-named.json"), "utf8");
  const { transport, argvLog } = fakeCockpit([{ argv0: "ask", stdout }]);
  const area = new PackagesArea(transport, {} as CompositionUi, {} as ActPath);
  const named = (area as unknown as { named: (p: string, n: string, t: object) => Promise<{ results: { bindings: Array<Record<string, { value: string }>> } }> }).named.bind(area);
  const r = await named("workbench-graph", "claim-count", {});
  assert.deepEqual(r.results.bindings.map((b) => b["n"]?.value), ["20"]);
  assert.equal(JSON.parse(argvLog()[0]?.[1] ?? "{}").name, "claim-count");
  // an answer that holds no SPARQL results is an error, never "no rows"
  const bad = fakeCockpit([{ argv0: "ask", stdout: JSON.stringify({ query: "sparql", ref: "", raw: "", sparql: { vars: [], rows: [] } }) }]);
  const named2 = (new PackagesArea(bad.transport, {} as CompositionUi, {} as ActPath) as unknown as { named: (p: string, n: string, t: object) => Promise<unknown> }).named;
  await assert.rejects(named2.call(new PackagesArea(bad.transport, {} as CompositionUi, {} as ActPath), "x", "y", {}), /holds no SPARQL results/);
});

test("a real ui list package with an alias file (member {role: aliases, name: \"\"}) loads; its tile is offered", async () => {
  const fx = JSON.parse(readFileSync(join(FIX, "uilist-aliases-member.json"), "utf8")) as { package: UiPackage; blobs: Record<string, string> };
  assert.ok(fx.package.members.some((m) => m.role === "aliases" && m.name === ""), "the fixture has the aliases member");
  const listing = assertUiListing({ packages: [fx.package] });
  const l = await loadPackages(listing, async (_p, h) => new Uint8Array(Buffer.from(fx.blobs[h] ?? "", "base64")), loadVendored(join(process.cwd(), "media", "kton-web")));
  assert.deepEqual(l.refusals, []);
  assert.deepEqual(Object.keys(l.packages), ["workbench-graph"]);
  assert.ok(l.entries.some((e) => e.slot === "home-tile" && e.query === "claim-count"));
});
