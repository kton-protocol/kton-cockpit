import { chmodSync, mkdtempSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { CliTransport } from "../src/transport/cli.js";

export interface FakeResponse {
  readonly argv0: string;
  readonly argv1?: string;
  readonly exit?: number;
  readonly stdout?: string;
  readonly stderr?: string;
}

export const FAKE = join(process.cwd(), "test", "fixtures", "fake-cockpit.mjs");

export function fakeCockpit(responses: readonly FakeResponse[], extra: Partial<ConstructorParameters<typeof CliTransport>[0]> = {}) {
  chmodSync(FAKE, 0o755);
  const dir = mkdtempSync(join(tmpdir(), "fake-cockpit-"));
  const resp = join(dir, "responses.json");
  const log = join(dir, "argv.log");
  writeFileSync(resp, JSON.stringify(responses));
  const transport = new CliTransport({
    executable: FAKE,
    cwd: dir,
    env: { FAKE_COCKPIT_RESPONSES: resp, FAKE_COCKPIT_LOG: log },
    ...extra,
  });
  const argvLog = (): string[][] =>
    existsSync(log) ? readFileSync(log, "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l) as string[]) : [];
  return { transport, argvLog, dir };
}

/** A real output recorded from a cockpit pre-release build (local paths replaced by /work/probe). */
export function probe(name: string): string {
  return readFileSync(join(process.cwd(), "test", "fixtures", "probe", name), "utf8");
}

export const CLAIM = "sha256:" + "a".repeat(64);
export const SUBJECT = "sha256:" + "b".repeat(64);
export const SCOPE = "sha256:" + "c".repeat(64);
export const PERSON_KEY = "1f".repeat(16);
export const AGENT_KEY = "2e".repeat(16);

export function sparqlAnswer(rows: Array<Record<string, string>>): string {
  return JSON.stringify({
    query: "sparql",
    ref: "",
    raw: "",
    results: {
      head: { vars: rows[0] ? Object.keys(rows[0]) : [] },
      results: { bindings: rows.map((r) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, { type: "literal", value: v }]))) },
    },
  });
}
