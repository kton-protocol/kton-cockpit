// The e2e run as one kton-testrun/1 record (schema: testrun.schema.json beside this file).
// smoke.mjs collects steps per flow; this turns them into units S1…S10 with their steps, checks and
// screenshots. It holds no credentials: only names, results, messages and PNG/JSON file names.

export const SUITE = "kton-workbench-shell";
export const WORLD = "shell-e2e";

/** The flows of the PR 1 gate, in order (S5, S6, S8 belong to later PRs). */
export const FLOWS = {
  S1: "Start, identity and status bar",
  S2: "Home → Inbox → thread → answer",
  S3: "Decide and sign",
  S4: "Person-key signatures only through the dialog",
  S7: "Transport errors",
  S9: "Surfaces and lanes",
  S10: "Recorder handover",
  S5: "Packages: review view, open intent, hostile packages, uninstall",
  C1: "A composition in VS Code: wiring, held acts, refusals shown at the act",
  C2: "Layout in a composition (ktonpkg v0.9.5): grid, tabs, split, restored after a reload, host narrow",
};

/**
 * steps: [{flow: "S1", n: 1, title, ok, ms, error?, screenshots: ["s1-….png"], checks?: [{ok, message}]}]
 * native: {file, sha256} of the native results (smoke-results.json).
 */
export function runRecord({ steps, started, durationMs, native, target = { name: "throwaway-registry" }, account = { user: "qa-decider", admin: false }, sources = [] }) {
  const units = Object.entries(FLOWS).map(([id, title]) => {
    const own = steps.filter((s) => s.flow === id).sort((a, b) => a.n - b.n);
    const result = own.length === 0 ? "not-run" : own.every((s) => s.ok) ? "passed" : "failed";
    return {
      id: `lane:${id}`,
      inventory: false,
      nativeId: id,
      title,
      result,
      durationMs: own.reduce((a, s) => a + (s.ms ?? 0), 0),
      steps: own.map((s) => ({
        n: s.n,
        title: s.title,
        result: s.ok ? "passed" : "failed",
        durationMs: s.ms ?? null,
        screenshots: (s.screenshots ?? []).filter((f) => /^[\w.-]+\.png$/.test(f)),
        checks: s.checks ?? [{ ok: s.ok, message: s.ok ? "passed" : String(s.error ?? "failed") }],
      })),
    };
  });
  const counts = { passed: 0, failed: 0, "not-run": 0 };
  for (const u of units) counts[u.result] = (counts[u.result] ?? 0) + 1;
  return {
    schema: "kton-testrun/1",
    suite: SUITE,
    world: WORLD,
    group: null,
    target,
    account,
    client: "code-server + Playwright",
    sources,
    recorded: null,
    started,
    durationMs,
    result: units.some((u) => u.result === "failed") ? "failed" : units.every((u) => u.result === "passed") ? "passed" : "failed",
    counts,
    native: { format: "kton-testrun", file: native.file, sha256: native.sha256 },
    units,
    notRun: units.filter((u) => u.result === "not-run").map((u) => ({ id: u.id, why: "no step of this flow ran" })),
    deviations: [],
  };
}
