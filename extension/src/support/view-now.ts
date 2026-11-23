// The current view for a support session: a small JSON file a support session (e.g. an assistant) in the window's
// terminal reads to see what the active panel shows. The shell writes it on every view change (debounced, ~500 ms) to the
// path set in kton.support.viewFile; an empty setting writes nothing. Values as the view got them, at most MAX_ROWS rows
// per block (the rest counted), and never key material: a column or value that names a key, secret or password is left
// out, and nothing from the identity but its shown name. The file is the window user's only (mode 0600): what the
// view shows may be more than others on the machine may read.
import { chmodSync, mkdirSync, renameSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

export const MAX_ROWS = 200;
export const DEBOUNCE_MS = 500;

/** One block of the view as the host answered it. */
export interface SupportBlock {
  readonly use: string;
  readonly block: string;
  readonly query?: string;
  readonly params?: Readonly<Record<string, string>>;
  readonly answer?: unknown;
}
export interface SupportInput {
  readonly at: Date;
  readonly identity: string;
  readonly view: { readonly package: string; readonly composition: string; readonly entry: string };
  readonly run?: string;
  readonly state: unknown;
  readonly blocks: readonly SupportBlock[];
  readonly opened: readonly string[];
}

const SECRET = /(private|secret|password|passwd|token|\.key\b|-----BEGIN)/i;
const KEYISH = /^(key(file|path)?|privateKey|secret|password|token)$/i;

/** The view as a small, secret-free object (see the file's head). */
export function supportView(i: SupportInput): Record<string, unknown> {
  const blocks = i.blocks.map((b) => {
    const a = b.answer as { head?: { vars?: string[] }; results?: { bindings?: Array<Record<string, { value?: unknown }>> } } | undefined;
    const all = a?.results?.bindings ?? [];
    const cols = (a?.head?.vars ?? Object.keys(all[0] ?? {})).filter((c) => !KEYISH.test(c));
    const rows = all.slice(0, MAX_ROWS).map((r) => Object.fromEntries(cols.filter((c) => typeof r[c]?.value === "string" && !SECRET.test(String(r[c]?.value))).map((c) => [c, String(r[c]?.value)])));
    const stamps = [...new Set(all.map((r) => [r["stamp"]?.value, r["stampLabel"]?.value].filter((x) => typeof x === "string").join(" · ")).filter(Boolean))].slice(0, 10);
    return {
      use: b.use,
      block: b.block,
      ...(b.query ? { query: b.query } : {}),
      ...(b.params && Object.keys(b.params).length ? { params: b.params } : {}),
      ...(a ? { rows, rowCount: all.length, ...(all.length > MAX_ROWS ? { more: all.length - MAX_ROWS } : {}), stamps } : { rows: [], note: "not answered yet" }),
    };
  });
  const { selected, queue } = picked(i.state);
  return {
    at: i.at.toISOString(),
    window: { identity: i.identity },
    view: i.view,
    ...(i.run ? { run: i.run } : {}),
    ...(selected ? { selected } : {}),
    ...(queue ? { queue } : {}),
    blocks,
    opened: [...i.opened],
  };
}

/** What is selected (a var's value: item, element, pick, rule) and a queue's position, from the frame's state. */
export function picked(state: unknown): { selected?: Record<string, string>; queue?: Record<string, unknown> } {
  const selected: Record<string, string> = {};
  let queue: Record<string, unknown> | undefined;
  const walk = (x: unknown, depth: number): void => {
    if (!x || typeof x !== "object" || depth > 8) return;
    const o = x as Record<string, unknown>;
    const pos = o["position"] as Record<string, unknown> | undefined;
    if (pos && typeof pos === "object" && typeof pos["n"] === "number") queue = { ...pos };
    for (const [k, v] of Object.entries(o)) {
      if (typeof v === "string" && v && v !== "none" && /^(item|element|pick|rule|selected|unit|spec)$/.test(k) && !SECRET.test(v)) selected[k] = v;
      else if (typeof v === "object") walk(v, depth + 1);
    }
  };
  walk(state, 0);
  return { ...(Object.keys(selected).length ? { selected } : {}), ...(queue ? { queue } : {}) };
}

/** Writes the active view's file, debounced; nothing when the path is empty. Atomic (a temp file renamed). */
export class SupportWriter {
  private timer: ReturnType<typeof setTimeout> | undefined;
  constructor(
    private readonly path: () => string,
    private readonly write: (path: string, text: string) => void = atomicWrite,
    private readonly delay = DEBOUNCE_MS,
  ) {}
  /** a view changed: write `build()` once things settle (the last call wins) */
  changed(build: () => Record<string, unknown> | undefined): void {
    if (!this.path()) return;
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => {
      this.timer = undefined;
      const p = this.path();
      const v = p ? build() : undefined;
      if (p && v) {
        try {
          this.write(p, `${JSON.stringify(v, null, 1)}\n`);
        } catch {
          /* the support file is a convenience: never fail the view over it */
        }
      }
    }, this.delay);
  }
}

/** Write `text` to `path` through a temp file renamed over it, readable and writable by the owner only (0600). */
export function atomicWrite(path: string, text: string): void {
  mkdirSync(dirname(path), { recursive: true });
  const tmp = `${path}.tmp-${process.pid}`;
  writeFileSync(tmp, text, { mode: 0o600 });
  // a temp file left from before keeps its mode on writeFileSync: set it either way
  chmodSync(tmp, 0o600);
  renameSync(tmp, path);
}
