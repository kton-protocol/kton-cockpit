// Run artifacts on demand: read from the read-only root at <run>/<name>, shown only when the bytes hash to the claim's.
import { test } from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { artifactOf, artifactRef, artifactTarget, artifactUri, readArtifact, showAs } from "../src/ui/artifacts.js";

const sha = (b: Buffer | string) => `sha256:${createHash("sha256").update(b).digest("hex")}`;

function root() {
  const base = mkdtempSync(join(tmpdir(), "art-"));
  const r = join(base, "work");
  mkdirSync(join(r, "p2-e2e", "out", "e2e-ärtifacts"), { recursive: true });
  writeFileSync(join(r, "p2-e2e", "out", "e2e-ärtifacts", "test-failed-1.png"), "PNGBYTES");
  writeFileSync(join(base, "secret.txt"), "outside");
  symlinkSync(join(base, "secret.txt"), join(r, "p2-e2e", "out", "link.txt"));
  return r;
}

test("artifacts: the bytes are shown only when they hash to the claim's sha256; a mismatch shows nothing and says why", async () => {
  const r = root();
  const name = "out/e2e-ärtifacts/test-failed-1.png";
  const ok = await readArtifact(r, { run: "p2-e2e", name, sha256: sha("PNGBYTES") });
  assert.ok("bytes" in ok && ok.bytes.toString() === "PNGBYTES");
  const bad = await readArtifact(r, { run: "p2-e2e", name, sha256: sha("other") });
  assert.ok("why" in bad && /does not hash to .*refused, nothing shown/.test(bad.why));
  const none = await readArtifact("", { run: "p2-e2e", name, sha256: sha("PNGBYTES") });
  assert.ok("why" in none && /no artifacts root configured/.test(none.why));
  const missing = await readArtifact(r, { run: "p2-e2e", name: "out/nope.png", sha256: sha("PNGBYTES") });
  assert.ok("why" in missing && /not found/.test(missing.why));
});

test("artifacts: a symlink out of the root is refused, even with the right hash", async () => {
  const r = root();
  const out = await readArtifact(r, { run: "p2-e2e", name: "out/link.txt", sha256: sha("outside") });
  assert.ok("why" in out && /outside the artifacts root/.test(out.why));
});

test("artifacts: a frame's target is checked (run and name plain, sha256 required); the URI round-trips; how it is shown", () => {
  const h = sha("x");
  for (const [v, why] of [
    [{ run: "../p2", name: "a.png", sha256: h }, /run is not a run name/],
    [{ run: "/abs", name: "a.png", sha256: h }, /run is not a run name/],
    [{ run: "p2", name: "../../etc/passwd", sha256: h }, /not a plain path/],
    [{ run: "p2", name: "/etc/passwd", sha256: h }, /not a plain path/],
    [{ run: "p2", name: "a/./b.png", sha256: h }, /not a plain path/],
    [{ run: "p2", name: "a\\b.png", sha256: h }, /not a plain path/],
    [{ run: "p2", name: "C:x.png", sha256: h }, /not a plain path/],
    [{ run: "p2", name: "a.png" }, /sha256 is not/],
    [{ run: "p2", name: "a.png", sha256: h, mediaType: "x y" }, /not a media type/],
    ["p2/a.png", /an artifact target is/],
  ] as const) assert.match(String(artifactTarget(v)), why);
  const t = artifactTarget({ run: "p2-e2e", name: "out/e2e-ärtifacts/test-failed-1.png", sha256: h, mediaType: "image/png" });
  assert.ok(typeof t !== "string");
  assert.deepEqual(artifactOf(artifactUri(t)), t);
  assert.equal(artifactRef(t), `run-artifact:p2-e2e/out/e2e-ärtifacts/test-failed-1.png@${h}`);
  assert.equal(showAs(t), "image");
  assert.equal(showAs({ run: "p", name: "error-context.md", sha256: h, mediaType: "text/markdown" }), "text");
  assert.equal(showAs({ run: "p", name: "trace.zip", sha256: h, mediaType: "application/zip" }), "info");
});
