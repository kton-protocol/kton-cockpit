// Intents and slot inputs (contract C2.2, C1.5).
//
// A slot (a viewer or a package view) gives back intents only; the shell does every verb. An intent
// is untrusted input (kton-review check 2): it is validated against the `ui` entry that placed the
// slot, before any handler runs, and refusals keep their reasons apart.

export const SHELL_VALUES = [
  "subject.id",
  "subject.kind",
  "subject.type",
  "subject.path",
  "subject.scope",
  "identity.name",
] as const;
export type ShellValue = (typeof SHELL_VALUES)[number];

export type SubjectKind = "claim" | "foton" | "scope";

/** What a slot gets (C2.2): subject ids, the window identity's NAME (never its key), theme, size. */
export interface SlotContext {
  readonly subject: {
    readonly id: string;
    readonly kind: SubjectKind;
    readonly type?: string;
    readonly path?: string;
    readonly scope?: string;
  };
  readonly identityName: string;
}

/** A `ui/<name>.json` entry as cockpit-verticleformat agreed it (C2.1), the parts the shell uses. */
export interface UiEntry {
  readonly package: string;
  readonly name: string;
  readonly slot: string;
  readonly label?: string;
  readonly when?: { readonly kind: SubjectKind; readonly type?: string };
  readonly ray?: string;
  readonly viewer?: string;
  readonly query?: string;
  readonly control?: string;
  readonly inputs?: Readonly<Record<string, string>>;
  /** Intents this entry may emit; from viewer.json `slots` (C2.3). */
  readonly intents?: readonly IntentKind[];
}

export type IntentKind = "sign" | "open" | "run";

export type Intent =
  | { readonly kind: "sign"; readonly about: string; readonly fields: Readonly<Record<string, string>> }
  | { readonly kind: "open"; readonly target: "screen" | "slot" | "file"; readonly id: string }
  | { readonly kind: "run"; readonly ray: string; readonly holes: Readonly<Record<string, string>> };

export type IntentRefusal =
  | { readonly reason: "outside-declaration"; readonly detail: string }
  | { readonly reason: "not-implemented"; readonly detail: string }
  | { readonly reason: "invalid-arguments"; readonly detail: string };

export type Checked<T> = { readonly ok: true; readonly value: T } | { readonly ok: false; readonly refusal: IntentRefusal };

const IMPLEMENTED: ReadonlySet<IntentKind> = new Set<IntentKind>(["sign", "open", "run"]);

/** Bind an entry's inputs from the closed set of shell values. Anything else is refused. */
export function bindInputs(entry: UiEntry, ctx: SlotContext): Checked<Record<string, string>> {
  const out: Record<string, string> = {};
  for (const [name, source] of Object.entries(entry.inputs ?? {})) {
    if (!(SHELL_VALUES as readonly string[]).includes(source)) {
      return { ok: false, refusal: { reason: "invalid-arguments", detail: `input ${name} binds ${source}, which is not a shell value` } };
    }
    const v = valueOf(source as ShellValue, ctx);
    if (v === undefined) {
      return { ok: false, refusal: { reason: "invalid-arguments", detail: `input ${name}: ${source} is not known for this subject` } };
    }
    out[name] = v;
  }
  return { ok: true, value: out };
}

function valueOf(v: ShellValue, ctx: SlotContext): string | undefined {
  switch (v) {
    case "subject.id":
      return ctx.subject.id;
    case "subject.kind":
      return ctx.subject.kind;
    case "subject.type":
      return ctx.subject.type;
    case "subject.path":
      return ctx.subject.path;
    case "subject.scope":
      return ctx.subject.scope;
    case "identity.name":
      return ctx.identityName;
  }
}

/** Does an entry apply to this subject? An unknown slot id is delivered but has no place (K). */
export function applies(entry: UiEntry, ctx: SlotContext): boolean {
  if (!entry.when) return true;
  if (entry.when.kind !== ctx.subject.kind) return false;
  return entry.when.type === undefined || entry.when.type === ctx.subject.type;
}

const SHA = /^sha256:[0-9a-f]{64}$/;

/**
 * Check a raw intent from a slot. Order matters (K): the declaration check comes before anything
 * looks for a handler, so an undeclared intent is "outside the declaration", not "not implemented".
 */
export function checkIntent(raw: unknown, entry: UiEntry, ctx: SlotContext): Checked<Intent> {
  if (typeof raw !== "object" || raw === null) {
    return { ok: false, refusal: { reason: "invalid-arguments", detail: "an intent is an object" } };
  }
  const r = raw as Record<string, unknown>;
  const kind = r["kind"];
  if (typeof kind !== "string") return { ok: false, refusal: { reason: "invalid-arguments", detail: "intent has no kind" } };
  if (!(entry.intents ?? []).includes(kind as IntentKind)) {
    return { ok: false, refusal: { reason: "outside-declaration", detail: `${entry.package}/${entry.name} does not declare ${kind}` } };
  }
  if (!IMPLEMENTED.has(kind as IntentKind)) {
    return { ok: false, refusal: { reason: "not-implemented", detail: `the shell has no ${kind} intent` } };
  }
  switch (kind) {
    case "sign": {
      const about = r["about"];
      const fields = r["fields"] ?? {};
      if (typeof about !== "string" || !SHA.test(about)) {
        return { ok: false, refusal: { reason: "invalid-arguments", detail: "sign.about must be a sha256 id" } };
      }
      const allowed = new Set([ctx.subject.id, ctx.subject.scope].filter((x): x is string => typeof x === "string"));
      if (!allowed.has(about)) {
        return { ok: false, refusal: { reason: "invalid-arguments", detail: "sign.about must be the slot's subject or its scope" } };
      }
      if (!entry.control) {
        return { ok: false, refusal: { reason: "outside-declaration", detail: `${entry.package}/${entry.name} names no control to sign with` } };
      }
      if (!isStringMap(fields)) return { ok: false, refusal: { reason: "invalid-arguments", detail: "sign.fields must map names to strings" } };
      return { ok: true, value: { kind: "sign", about, fields } };
    }
    case "open": {
      const target = r["target"];
      const id = r["id"];
      if ((target !== "screen" && target !== "slot" && target !== "file") || typeof id !== "string" || id === "") {
        return { ok: false, refusal: { reason: "invalid-arguments", detail: "open needs target screen|slot|file and an id" } };
      }
      return { ok: true, value: { kind: "open", target, id } };
    }
    case "run": {
      const ray = r["ray"];
      const holes = r["holes"] ?? {};
      if (typeof ray !== "string" || ray !== entry.ray) {
        return { ok: false, refusal: { reason: "outside-declaration", detail: "run names a ray its entry does not declare" } };
      }
      if (!isStringMap(holes)) return { ok: false, refusal: { reason: "invalid-arguments", detail: "run.holes must map names to strings" } };
      return { ok: true, value: { kind: "run", ray, holes } };
    }
  }
  return { ok: false, refusal: { reason: "not-implemented", detail: kind } };
}

function isStringMap(v: unknown): v is Record<string, string> {
  return typeof v === "object" && v !== null && !Array.isArray(v) && Object.values(v).every((x) => typeof x === "string");
}
