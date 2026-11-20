// Packages from `ui list` (C2.4): the shape check, loading by hash with re-verify at load (C1.5), viewer
// pins against the trees (ktonpkg v0.9.1 carried viewers), and refusals with their reasons (S5.3).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { createHash } from "node:crypto";
import { assertUiListing } from "../src/packages/listing.js";
import { loadPackages, nameProblem, treeId } from "../src/packages/model.js";
import { writeTree } from "../src/packages/store.js";
import { answerBytes, type Backend } from "../src/composition/host.js";
import { existsSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { TransportError } from "../src/transport/port.js";
import { listed, PKGS, reader } from "./pkg-helpers.js";

const kwt = () => listed(join(PKGS, "kton-web-test"), "kton-web-test", "test");
const good = () => listed(join(PKGS, "hostile", "good"), "hostile-good");

test("TreeID: our tree id of kton-web-test's probe folder is the pin kton-web wrote with ktonpkg's rule", () => {
  const dir = join(PKGS, "kton-web-test", "viewers", "probe");
  const t = treeId(new Map(["probe.js", "viewer.json"].map((f) => [f, readFileSync(join(dir, f))])));
  const pin = (JSON.parse(readFileSync(join(PKGS, "kton-web-test", "ui", "blocks", "probe.json"), "utf8")) as { viewer: { tree: string } }).viewer.tree;
  assert.equal(t, pin);
});

test("ui list: the shape is checked; a member hash or name that is not plain is refused", () => {
  const ok = assertUiListing({ packages: [kwt().pkg] });
  assert.equal(ok.packages[0]?.members.length, 17);
  assert.throws(() => assertUiListing({ packages: [{ ...kwt().pkg, allowed: "yes" }] }), TransportError);
  assert.throws(() => assertUiListing({ packages: [{ ...kwt().pkg, members: [{ role: "block", name: "x", hash: "md5:1" }] }] }), /not sha256/);
  // a member name that is not a plain path passes the shape check: it refuses its own package at load (F1), not the listing
  assert.equal(assertUiListing({ packages: [{ ...kwt().pkg, members: [{ role: "viewer-file", name: "viewers/../../x", hash: `sha256:${"a".repeat(64)}` }] }] }).packages.length, 1);
  // a package that fails its check may come with no name: it is listed by dir with its reason
  const failed = assertUiListing({ packages: [{ dir: ".kton/packages/x@1", allowed: false, refused: [{ file: "", code: "package.check", reason: "seal broken" }] }] });
  assert.equal(failed.packages[0]?.refused[0]?.reason, "seal broken");
});

test("load: kton-web-test's controls keep their kind (decision/record), its viewers their trees and modules", async () => {
  const f = kwt();
  const l = await loadPackages({ packages: [f.pkg] }, reader(f));
  assert.deepEqual(l.refusals, []);
  assert.equal(l.controls.get("kton-web-test/test-decision")?.kind, "decision");
  assert.equal(l.controls.get("kton-web-test/test-record")?.kind, "record");
  assert.equal(l.controls.get("kton-web-test/test-record")?.template, "test-note");
  const probe = l.packages["kton-web-test"]?.viewers["probe"];
  assert.equal(probe?.module, "packages/kton-web-test@test/viewers/probe/probe.js");
  assert.deepEqual(l.entries.map((e) => [e.slot, e.composition]).sort(), [["review-view", "test-layout"], ["review-view", "test-shell"]]);
});

test("load: a control that says no kind is a decision (ktonpkg EffectiveKind); a malformed control is refused", async () => {
  const f = listed(join(PKGS, "kton-web-test"), "kton-web-test", "test", (p, b) => (p === "controls/test-decision.json" ? Buffer.from('{"say": {"template": "test-note"}}') : p === "controls/test-record.json" ? Buffer.from('{"kind": "record"}') : b));
  const l = await loadPackages({ packages: [f.pkg] }, reader(f));
  assert.equal(l.refusals[0]?.code, "control.format");
  const g = listed(join(PKGS, "kton-web-test"), "kton-web-test", "test", (p, b) => (p === "controls/test-decision.json" ? Buffer.from('{"say": {"template": "test-note"}}') : b));
  assert.equal((await loadPackages({ packages: [g.pkg] }, reader(g))).controls.get("kton-web-test/test-decision")?.kind, "decision");
});

test("S5.3 tampered after admission: a member whose bytes changed is refused at load, naming the file", async () => {
  const f = good();
  const js = f.pkg.members.find((m) => m.name === "viewers/table/index.js");
  f.blobs.set(js?.hash as string, Buffer.from("export function mount(el) { el.textContent = 'tampered'; }"));
  const l = await loadPackages({ packages: [f.pkg, kwt().pkg] }, reader(f, kwt()));
  assert.deepEqual(Object.keys(l.packages), ["kton-web-test"], "the other package is unaffected");
  assert.equal(l.refusals[0]?.code, "package.tampered");
  assert.match(l.refusals[0]?.reason ?? "", /viewers\/table\/index\.js changed after admission/);
});

test("S5.3 viewer-pin-mismatch: a pin no tree here has is refused with its reason; a kton-web pin this shell does not carry too", async () => {
  const bad = listed(join(PKGS, "hostile", "good"), "hostile-pin", "1", (p, b) => {
    if (p !== "ui/blocks/table.json") return b;
    const d = JSON.parse(b.toString()) as Record<string, unknown>;
    return Buffer.from(JSON.stringify({ ...d, viewer: { viewer: "table", at: "hostile-pin@1", tree: `sha256:${"0".repeat(64)}` } }));
  });
  const l = await loadPackages({ packages: [bad.pkg] }, reader(bad));
  assert.equal(l.refusals[0]?.code, "viewer-pin-mismatch");
  assert.match(l.refusals[0]?.reason ?? "", /pins viewer table at tree sha256:0{64}; the tree here is sha256:/);
  const kw = listed(join(PKGS, "hostile", "good"), "hostile-pin", "1", (p, b) => (p === "ui/blocks/table.json" ? Buffer.from(JSON.stringify({ ...JSON.parse(b.toString()), viewer: { viewer: "kton-web/table", at: "39242b9f0bfce4f78d35f25d291c79877aaeeed5", tree: `sha256:${"0".repeat(64)}` } })) : b));
  const l2 = await loadPackages({ packages: [kw.pkg] }, reader(kw));
  assert.match(l2.refusals[0]?.reason ?? "", /pins viewer kton-web\/table at kton-web 39242b9f0bfc, which this shell does not carry/);
});

test("load: a block's events and options must be what its viewer declares, with equal types", async () => {
  const f = listed(join(PKGS, "kton-web-test"), "kton-web-test", "test", (p, b) => (p === "ui/blocks/probe.json" ? Buffer.from(JSON.stringify({ ...JSON.parse(b.toString()), emits: { pick: "iri" } })) : b));
  const l = await loadPackages({ packages: [f.pkg] }, reader(f));
  assert.equal(l.refusals[0]?.code, "viewer.emits");
  assert.match(l.refusals[0]?.reason ?? "", /event pick is "iri", viewer probe says "literal"/);
});

test("load: refused and not admitted packages are listed, never loaded", async () => {
  const f = kwt();
  const l = await loadPackages({ packages: [{ ...f.pkg, allowed: false }, { ...good().pkg, refused: [{ file: "", code: "package.check", reason: "seal does not verify" }] }] }, reader(f));
  assert.deepEqual(Object.keys(l.packages), []);
  assert.deepEqual(l.notAdmitted, ["kton-web-test"]);
  assert.deepEqual(l.refusals.map((r) => [r.package, r.reason]), [["hostile-good", "seal does not verify"]]);
});

test("a package with allowed: false, or a non-empty refused, contributes nothing to slots or compositions (v0.5.2 lists their declarations anyway)", async () => {
  const f = kwt();
  const g = good();
  // as cockpit v0.5.2's ui list: declarations filled for every installed package, admitted or not, checked or not
  const listing = { packages: [{ ...f.pkg, allowed: false }, { ...g.pkg, refused: [{ file: "ui/review.json", code: "ui.check", reason: "viewer table: pin mismatch" }] }] };
  assert.ok(listing.packages.every((p) => p.ui.length > 0 && p.blocks.length > 0 && p.compositions.length > 0));
  const l = await loadPackages(listing, reader(f, g));
  assert.deepEqual(l.entries, [], "no slot entry");
  assert.deepEqual(Object.keys(l.packages), [], "no package for compositions, blocks or viewers");
  assert.deepEqual([...l.controls.keys()], [], "no control");
  assert.deepEqual([...l.byHash.keys()], [], "no content");
  assert.deepEqual(l.notAdmitted, ["kton-web-test"]);
  assert.deepEqual(l.refusals.map((r) => [r.package, r.file, r.reason]), [["hostile-good", "ui/review.json", "viewer table: pin mismatch"]], "shown with its reason, never rendered");
});

test("ui list as cockpit v0.5.2 answers it (null for unreadable declarations, no members) is read; without members nothing loads", async () => {
  const real = {
    packages: [
      { name: "kton-web-test", package: "sha256:1", revision: "sha256:2", label: "test", profile: "https://kton.dev/profile/verticle", dir: ".kton/packages/kton-web-test@test", allowed: true, ui: [{ slot: "review-view", label: "Test shell", composition: "test-shell", file: "test-shell" }], blocks: [], compositions: [{ name: "test-shell", kind: "view", parts: [] }], refused: [] },
      { dir: ".kton/packages/broken@1", allowed: false, ui: null, blocks: null, compositions: null, refused: [{ file: "", code: "package.check", reason: "no seal" }] },
    ],
  };
  const l = assertUiListing(real);
  assert.equal(l.packages[1]?.ui.length, 0);
  const loaded = await loadPackages(l, async () => new Uint8Array());
  assert.equal(loaded.refusals[0]?.code, "package.members", "the shell asks kton-cockpit for members: without them nothing can be loaded by hash");
  assert.equal(loaded.refusals[1]?.reason, "no seal");
});

test("a composition whose hash in ui list is not its member's is refused (the body comes by that hash through ui cat)", async () => {
  const f = kwt();
  const pkg = { ...f.pkg, compositions: f.pkg.compositions.map((c) => (c["name"] === "test-shell" ? { ...c, hash: `sha256:${"0".repeat(64)}` } : c)) };
  const l = await loadPackages({ packages: [pkg] }, reader(f));
  assert.equal(l.refusals[0]?.code, "composition.hash");
});

test("shipped code reads no stand-in and no package folder: package bytes come only through `cockpit ui cat`", () => {
  const files: string[] = [];
  const walk = (d: string) => {
    for (const x of readdirSync(d)) {
      const p = join(d, x);
      if (statSync(p).isDirectory()) walk(p);
      else if (p.endsWith(".ts")) files.push(p);
    }
  };
  walk(join(process.cwd(), "src"));
  for (const f of files) {
    const t = readFileSync(f, "utf8");
    assert.ok(!/listFixture|uiListFixture|fixture/i.test(t), `${f} mentions a fixture`);
    assert.ok(!/blobs\/|["'`]blobs["'`]|\.kton\/packages|"packages", |join\([^)]*"packages"/.test(t), `${f} reads a package folder`);
  }
  // package bytes: only the transport's uiCat runs anything, and it runs `ui cat`
  const cli = readFileSync(join(process.cwd(), "src", "transport", "cli.ts"), "utf8");
  assert.match(cli, /this\.run\(\["ui", "cat", pkg, hash\], true\)/);
  // store.ts only writes checked trees to the viewer store (F1); it reads nothing
  for (const f of files.filter((x) => /src[\\/](packages|composition)[\\/]/.test(x))) {
    const t = readFileSync(f, "utf8");
    assert.ok(!/node:fs/.test(t) || f.endsWith("vendored.ts") || f.endsWith("mermaid-server.ts") || (f.endsWith("store.ts") && !/read(File|dir)|createReadStream/.test(t)), `${f} touches the file system`);
  }
});

test("F1 on #174: a package carrying a climbing viewer file is refused at load, nothing is read or written, the other package is unaffected", async () => {
  const climb = "viewers/table/../../../ZZ-escape.txt";
  const f = good();
  const bytes = Buffer.from("escaped");
  const h = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  f.blobs.set(h, bytes);
  const hostile = { ...f.pkg, members: [...f.pkg.members, { role: "viewer-file", name: climb, hash: h }] };
  const reads: string[] = [];
  const read = reader(f, kwt());
  const l = await loadPackages({ packages: [hostile, kwt().pkg] }, async (p, x) => (reads.push(x), read(p, x)));
  assert.deepEqual(Object.keys(l.packages), ["kton-web-test"], "the other package is unaffected");
  assert.equal(l.refusals[0]?.code, "viewer.file");
  assert.match(l.refusals[0]?.reason ?? "", /ZZ-escape\.txt.*has a \.\. segment/);
  assert.ok(!reads.includes(h), "the climbing member was never read");
  for (const n of ["/etc/x", "viewers/t/a\\b", "viewers/t/c:x", "viewers/t//x", "viewers/./x", "content/../x"]) assert.ok(nameProblem(n), n);
  for (const n of ["viewers/table/index.js", "content/SOP-02.md"]) assert.equal(nameProblem(n), undefined, n);
  // the store write (defence in depth): one escaping path, and nothing at all is written
  const root = mkdtempSync(join(tmpdir(), "wb-store-"));
  try {
    const dir = join(root, "globalStorage", "viewers", "tree");
    assert.throws(() => writeTree(dir, new Map([["index.js", bytes], ["../../../ZZ-escape.txt", bytes]])), /not inside the viewer store: nothing is written/);
    assert.ok(!existsSync(join(root, "ZZ-escape.txt")) && !existsSync(join(dir, "index.js")), "nothing written, inside or out");
    assert.throws(() => writeTree(dir, new Map([["/tmp/abs", bytes]])), /not inside/);
    writeTree(dir, new Map([["a/b.js", bytes]]));
    assert.ok(existsSync(join(dir, "a", "b.js")));
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("member names: the one rule of ktonpkg v0.9.9 (memberNameProblem), case by case", () => {
  const cases: Array<[string, string | undefined]> = [
    ["viewers/table/index.html", undefined],
    ["viewers/table/index.js", undefined],
    ["content/SOP-02.md", undefined],
    ["a", undefined],
    ["review 10.30.md", undefined],
    ["a/", "has an empty segment or a leading or trailing /"],
    ["/a", "has an empty segment or a leading or trailing /"],
    ["a//b", "has an empty segment or a leading or trailing /"],
    ["", "has an empty segment or a leading or trailing /"],
    ["a:b", "has a backslash or a colon"],
    ["review 10:30.md", "has a backslash or a colon"],
    ["C:/x", "has a backslash or a colon"],
    ["a\\b", "has a backslash or a colon"],
    ["./a", "has a . segment"],
    ["a/..", "has a .. segment"],
    ["viewers/table/../../../ZZ-escape.txt", "has a .. segment"],
  ];
  for (const [name, why] of cases) assert.equal(nameProblem(name), why, JSON.stringify(name));
});

test("byHash by (package, hash): two packages carrying the same bytes each serve their own file to their own leaf", async () => {
  const bytes = Buffer.from("# the same note in two packages");
  const h = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  const withNote = (name: string) => {
    const f = listed(join(PKGS, "hostile", "good"), name);
    f.blobs.set(h, bytes);
    return { ...f, pkg: { ...f.pkg, members: [...f.pkg.members, { role: "content", name: "content/note.md", hash: h }] } };
  };
  const a = withNote("pkg-a");
  const b = withNote("pkg-b");
  const l = await loadPackages({ packages: [a.pkg, b.pkg] }, reader(a, b));
  assert.deepEqual(l.refusals, []);
  assert.deepEqual([...(l.byHash.get(h)?.keys() ?? [])].sort(), [a.pkg.package, b.pkg.package].sort());
  for (const name of ["pkg-a", "pkg-b"]) {
    const own = l.packages[name];
    const asked: string[] = [];
    const backend = { bytes: async (pkg: string) => (asked.push(pkg), new Uint8Array(bytes)) } as unknown as Backend;
    const out = await answerBytes(backend, l, h, undefined, "product@1", { id: own?.id as string, name });
    assert.equal(out["bytes"], bytes.toString("base64"), name);
    assert.deepEqual(asked, [own?.id], `${name} is served from its own package`);
  }
});

test("member names on #184: only the aliases member listed as \"\" is exempt; a query, template or other aliases name that climbs refuses the package", async () => {
  const bytes = Buffer.from("{}");
  const h = `sha256:${createHash("sha256").update(bytes).digest("hex")}`;
  const withMember = (role: string, name: string) => {
    const f = good();
    f.blobs.set(h, bytes);
    return { ...f, pkg: { ...f.pkg, members: [...f.pkg.members, { role, name, hash: h }] } };
  };
  const load = async (role: string, name: string) => {
    const f = withMember(role, name);
    return loadPackages({ packages: [f.pkg] }, reader(f));
  };
  for (const [role, name] of [["query", "../x"], ["template", "../x"], ["aliases", "../x"], ["aliases", "/x"], ["control", "a:b"], ["content", "content/../../x.md"]] as const) {
    const l = await load(role, name);
    assert.deepEqual(Object.keys(l.packages), [], `${role} ${name} refuses the package`);
    assert.equal(l.refusals[0]?.code, "member.name", `${role} ${name}`);
  }
  // the one exemption: cockpit lists a package's alias file as {role: aliases, name: ""}
  const ok = await load("aliases", "");
  assert.deepEqual(ok.refusals, []);
  assert.deepEqual(Object.keys(ok.packages), ["hostile-good"]);
  // and an empty name in any other role is still refused
  assert.equal((await load("query", "")).refusals[0]?.code, "member.name");
});
