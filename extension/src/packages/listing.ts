// `cockpit ui list --json` (C2.4): what the installed packages declare for a frontend. The shell reads
// declarations from here, never from package folders, and every package byte it loads it checks by hash.
//
// cockpit v0.5.4 (#170): {packages: [{name, package, revision, label, dir, allowed, ui[], blocks[], compositions[],
// members[], refused[]}]}. Declarations and members are listed only for a package a frontend may use (admitted,
// passing its check); the others come with refused or allowed: false, not hidden. ui entries are named by their
// path (ui/<name>.json), compositions carry their hash, members {role, name, hash} are fetched with `ui cat`.
import { TransportError } from "../transport/port.js";

export interface UiWhen {
  readonly kind: string;
  readonly type?: string;
}
export interface UiEntry {
  readonly file: string;
  readonly slot: string;
  readonly label: string;
  readonly when?: UiWhen;
  readonly query?: string;
  readonly viewer?: string;
  readonly control?: string;
  readonly ray?: string;
  readonly composition?: string;
  readonly inputs?: Readonly<Record<string, string>>;
  readonly order?: number;
}
export interface Member {
  readonly role: string;
  /** the member's name within its role: a file name without extension, or <viewer>/<path> for a viewer file */
  readonly name: string;
  readonly hash: string;
}
export interface Refusal {
  readonly file: string;
  readonly code: string;
  readonly reason: string;
}
export interface UiPackage {
  readonly name: string;
  readonly package: string;
  readonly revision: string;
  readonly label: string;
  readonly dir: string;
  readonly allowed: boolean;
  readonly ui: readonly UiEntry[];
  readonly blocks: readonly Readonly<Record<string, unknown>>[];
  readonly compositions: readonly Readonly<Record<string, unknown>>[];
  readonly refused: readonly Refusal[];
  readonly members: readonly Member[];
}
export interface UiListing {
  readonly packages: readonly UiPackage[];
}

const HASH = /^sha256:[0-9a-f]{64}$/;
const bad = (what: string): never => {
  throw new TransportError(`ui list: ${what}`, 0, "");
};
const str = (o: Record<string, unknown>, k: string, what: string, optional = false): string | undefined => {
  const v = o[k];
  if (v === undefined && optional) return undefined;
  if (typeof v !== "string") return bad(`${what}.${k} must be a string`);
  return v;
};
// v0.5.2 writes null for declarations it could not read (the reason is under refused): nothing to load
const list = (v: unknown, what: string): unknown[] => (v === undefined || v === null ? [] : Array.isArray(v) ? v : bad(`${what} must be a list`));
const obj = (v: unknown, what: string): Record<string, unknown> => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Record<string, unknown>) : bad(`${what} must be an object`));

/** The shape check for every field the shell reads (review check 5). Unknown fields are kept, not read. */
export function assertUiListing(v: unknown): UiListing {
  const top = obj(v, "the answer");
  const packages = list(top["packages"], "packages").map((raw, i) => {
    const p = obj(raw, `packages[${i}]`);
    const what = `packages[${i}]`;
    const refused = list(p["refused"], `${what}.refused`).map((r, j) => {
      const o = obj(r, `${what}.refused[${j}]`);
      return { file: str(o, "file", `${what}.refused[${j}]`) ?? "", code: str(o, "code", `${what}.refused[${j}]`) ?? "", reason: str(o, "reason", `${what}.refused[${j}]`) ?? "" };
    });
    // a package that fails its check may have no name or revision: it is shown by its dir and refusals
    const failed = refused.some((r) => r.file === "");
    const ui = list(p["ui"], `${what}.ui`).map((r, j) => {
      const o = obj(r, `${what}.ui[${j}]`);
      const w = `${what}.ui[${j}]`;
      const when = o["when"] === undefined ? undefined : obj(o["when"], `${w}.when`);
      const inputs = o["inputs"] === undefined ? undefined : obj(o["inputs"], `${w}.inputs`);
      if (inputs) for (const [k, x] of Object.entries(inputs)) if (typeof x !== "string") bad(`${w}.inputs.${k} must be a string`);
      if (o["order"] !== undefined && typeof o["order"] !== "number") bad(`${w}.order must be a number`);
      const e: UiEntry = {
        file: str(o, "file", w) ?? "",
        slot: str(o, "slot", w) ?? "",
        label: str(o, "label", w) ?? "",
        ...(when ? { when: { kind: str(when, "kind", `${w}.when`) ?? "", ...(when["type"] !== undefined ? { type: str(when, "type", `${w}.when`) ?? "" } : {}) } } : {}),
        ...Object.fromEntries((["query", "viewer", "control", "ray", "composition"] as const).filter((k) => o[k] !== undefined).map((k) => [k, str(o, k, w)])),
        ...(inputs ? { inputs: inputs as Record<string, string> } : {}),
        ...(typeof o["order"] === "number" ? { order: o["order"] } : {}),
      };
      return e;
    });
    const members = list(p["members"], `${what}.members`).map((r, j) => {
      const o = obj(r, `${what}.members[${j}]`);
      const m = { role: str(o, "role", `${what}.members[${j}]`) ?? "", name: str(o, "name", `${what}.members[${j}]`) ?? "", hash: str(o, "hash", `${what}.members[${j}]`) ?? "" };
      if (!HASH.test(m.hash)) bad(`${what}.members[${j}].hash is not sha256:<hex>`);
      // a member name that is not a plain path refuses its package at load (model.ts), not the whole listing
      return m;
    });
    return {
      name: str(p, "name", what, failed) ?? "",
      package: str(p, "package", what, failed) ?? "",
      revision: str(p, "revision", what, failed) ?? "",
      label: str(p, "label", what, failed) ?? "",
      dir: str(p, "dir", what) ?? "",
      allowed: typeof p["allowed"] === "boolean" ? p["allowed"] : bad(`${what}.allowed must be a boolean`),
      ui,
      blocks: list(p["blocks"], `${what}.blocks`).map((b, j) => obj(b, `${what}.blocks[${j}]`)),
      compositions: list(p["compositions"], `${what}.compositions`).map((c, j) => obj(c, `${what}.compositions[${j}]`)),
      refused,
      members,
    } satisfies UiPackage;
  });
  return { packages };
}
