// The sign dialog model (contract C1.4; decision O1: a person key for decisions, an agent key for records).
//
// - The one path for every signature. It shows what `cockpit say` will actually sign: the control
//   and its template from the cockpit's listing, the subject, the field values. The meaning comes
//   from the control/template, never from labels an intent brings (review check 2).
// - Decisions sign with the person key and need the dialog every time: "I know I signed this now",
//   never one click. Records sign with the agent key and need no dialog per write.
// - The person key is reachable only with the dialog's consent for exactly this plan: sign() gates on
//   the KEY (keyRole "person"), not on the needsDialog label (review check 6).
// - Footer: a kton signature is not yet a QMS e-signature. Rules show as "not enforced" until the
//   cockpit's rule gate (ADR-013) exists.
// - The result returns the claim id (kton-mignon: to chain replaces).

import { createHash } from "node:crypto";
import type { Intent } from "./intents.js";
import type { WindowIdentity } from "./identity.js";
import { keyFor } from "./identity.js";
import type { Outcome, Refusal, TemplateInfo, Transport } from "./transport/port.js";

/** A control as the installed package declares it (ktonpkg controls/). */
export interface ControlInfo {
  readonly package: string;
  readonly name: string;
  readonly template: string;
  /** Fixed by the control, if it fixes one; otherwise a template field the person picks in the dialog. */
  readonly meaning?: string;
  /** O1: a decision (person key, dialog) or a record (agent key, no dialog). Declared by the package. */
  readonly kind: "decision" | "record";
  /** the control's title, shown in the dialog (and covered by the digest, as everything shown) */
  readonly title?: string;
  /** why this control's template is the one signed (e.g. the proposal's vocabulary), shown in the dialog and digested */
  readonly why?: string;
}

export const FOOTER = "A kton signature is not yet a QMS e-signature: every signature here is test-only.";
export const RULES_NOTE = "Rules shown for information: not enforced until the cockpit's rule gate exists.";

export interface SignPlan {
  readonly control: ControlInfo;
  readonly template: string;
  readonly subject: string;
  readonly fields: Readonly<Record<string, string>>;
  readonly meaning?: string;
  /** the scope the claim goes into (an area's or an entry's records scope), shown in the dialog; none: the registry's default */
  readonly scope?: string;
  /** the evidence the view session opened (offered for lookedAt): listed in the dialog before signing */
  readonly opened?: readonly string[];
  /** what the values were checked against before the dialog (option checkAgainst): shown in the dialog, digested */
  readonly checks?: { readonly query: string; readonly differs: readonly string[]; readonly values: readonly string[] };
  readonly as: string;
  readonly keyRole: "person" | "agent";
  readonly needsDialog: boolean;
  readonly footer: string;
  readonly rules: string;
  /** What the claim says (the template's predicate), when the listing names it. Shown in the dialog. */
  readonly predicate?: string;
  /** The field the control's meaning fixes (its value is the meaning), if the template has one; else the meaning is
   * the act's (the control's), shown as such and not a claim field (composition-format decision 4). */
  readonly meaningField?: string;
  /**
   * What say must still sign with (SPEC §8.5, cockpit v0.5.5): the template's hash and, for a package template, its
   * sealed revision, and its predicate, all as `say --json` listed them when the dialog was built. say refuses
   * template.changed otherwise. The digest covers them.
   */
  readonly expect: { readonly template?: string; readonly revision?: string; readonly predicate?: string };
  /** Hash of exactly what is shown; a dialog confirmation is bound to it. */
  readonly digest: string;
}

/**
 * The field a control's meaning fixes: the template's `meaning` field, else the one field whose fixed values include
 * the meaning (a control meaning approved fixes a field whose values include approved, e.g. an outcome). Undefined: the meaning is the
 * act's, not a field of the claim.
 */
export function meaningField(control: Pick<ControlInfo, "meaning">, tpl: TemplateInfo): string | undefined {
  if (control.meaning === undefined) return undefined;
  if (tpl.fields["meaning"]) return "meaning";
  const carrying = meaningCarriers(control, tpl);
  return carrying.length === 1 ? carrying[0] : undefined;
}

/** The template's fields whose fixed values include the control's meaning (more than one: which it fixes is unclear). */
function meaningCarriers(control: Pick<ControlInfo, "meaning">, tpl: TemplateInfo): string[] {
  if (control.meaning === undefined || tpl.fields["meaning"]) return [];
  return Object.entries(tpl.fields).filter(([, f]) => f.values?.includes(control.meaning as string)).map(([k]) => k);
}

/** The template a control signs: its own package's (named <package>/<name> on a clash, cockpit v0.5.3), else by name. */
export function templateFor(control: Pick<ControlInfo, "package" | "template">, templates: readonly TemplateInfo[]): TemplateInfo | undefined {
  const mine = (t: TemplateInfo) => t.package === undefined || t.package === control.package;
  return templates.find((t) => t.name === `${control.package}/${control.template}` && mine(t)) ?? templates.find((t) => t.name === control.template && mine(t));
}

/** Build the plan from an intent. The intent supplies only `about` and field values (and the area's records scope). */
export function plan(
  intent: Extract<Intent, { kind: "sign" }> & { readonly scope?: string; readonly opened?: readonly string[]; readonly checks?: SignPlan["checks"] },
  control: ControlInfo,
  templates: readonly TemplateInfo[],
  identity: WindowIdentity,
): Outcome<SignPlan> {
  const tpl = templateFor(control, templates);
  if (!tpl) return refuse("template.not-allowed", `${control.package}/${control.name} signs ${control.template}, which is not installed here`);

  // what say must still sign with (SPEC §8.5): without the listing's hash (and a package template's revision) the
  // gap between the dialog and say is open, so nothing is signed (cockpit-runtime N1 on #187)
  if (!tpl.hash) return refuse("cockpit.version", `the cockpit lists no hash for the template ${tpl.name}: signing needs cockpit v0.5.5 or later (say --json with hash and revision), so that what the dialog shows is what is signed`);
  if (tpl.package !== undefined && !tpl.revision) return refuse("cockpit.version", `the cockpit lists no package revision for the template ${tpl.name}: signing needs cockpit v0.5.5 or later (say --json with hash and revision)`);
  const carriers = meaningCarriers(control, tpl);
  if (carriers.length > 1) return refuse("control.meaning", `${control.package}/${control.name} means ${control.meaning}, a value of several fields of ${tpl.name} (${carriers.join(", ")}): which one it fixes is not clear, so nothing is signed`);
  const fixed = meaningField(control, tpl);
  const fields: Record<string, string> = {};
  for (const [k, v] of Object.entries(intent.fields)) {
    if (k === "as") return refuse("argument", "an intent cannot set as");
    // the meaning shown is the meaning signed: a field the control's meaning fixes takes no other value
    if (k === fixed && v !== control.meaning) return refuse("argument", `${control.package}/${control.name} means ${control.meaning}: its ${k} is ${control.meaning}, not ${v}`);
    const f = tpl.fields[k];
    if (!f) return refuse("argument", `template ${tpl.name} has no field ${k}`);
    if (f.values && !f.values.includes(v)) return refuse("argument", `${k} must be one of ${f.values.join("|")}`);
    fields[k] = v;
  }
  // a meaning declared on the control (C3.4, ktonpkg v0.9.2) is signed in the field it fixes; with none, it is the
  // act's meaning (composition-format decision 4), shown as that and covered by the digest, never as a claim field
  if (fixed) fields[fixed] = control.meaning as string;
  for (const [k, f] of Object.entries(tpl.fields)) {
    if (f.required && fields[k] === undefined) return refuse("argument", `${k} is required by ${tpl.name}`);
  }

  const key = keyFor(identity, control.kind);
  // No fallback: a decision needs the person key, a record the agent key; a missing one is named.
  if (!key.keyName) return refuse("identity.key-missing", `this window has no ${key.role} key, so it cannot sign a ${control.kind}`);
  const meaning = control.meaning ?? fields["meaning"];
  const expect = { ...(tpl.hash ? { template: tpl.hash } : {}), ...(tpl.revision ? { revision: tpl.revision } : {}), ...(tpl.predicate ? { predicate: tpl.predicate } : {}) };
  const shown = { template: tpl.name, expect, subject: intent.about, fields, as: key.keyName, control: `${control.package}/${control.name}`, meaning: meaning ?? null, meaningField: fixed ?? null, scope: intent.scope ?? null, opened: intent.opened ?? [], ...(control.title ? { title: control.title } : {}), ...(control.why ? { why: control.why } : {}), ...(intent.checks ? { checks: intent.checks } : {}) };
  // canonical JSON (keys sorted at every level): a key list as JSON.stringify's replacer would also filter the nested
  // objects' keys, and leave expect.revision and expect.predicate out of the digest
  const digest = createHash("sha256").update(canonical(shown)).update(JSON.stringify(Object.entries(fields).sort())).digest("hex");
  return {
    ok: true,
    value: {
      control,
      template: tpl.name,
      ...(tpl.predicate !== undefined ? { predicate: tpl.predicate } : {}),
      ...(fixed ? { meaningField: fixed } : {}),
      expect,
      subject: intent.about,
      fields,
      ...(meaning !== undefined ? { meaning } : {}),
      ...(intent.scope !== undefined ? { scope: intent.scope } : {}),
      ...(intent.opened?.length ? { opened: [...intent.opened] } : {}),
      ...(intent.checks ? { checks: intent.checks } : {}),
      as: key.keyName,
      keyRole: key.role,
      needsDialog: control.kind === "decision",
      footer: FOOTER,
      rules: RULES_NOTE,
      digest,
    },
  };
}

/** JSON with every object's keys sorted, at every level. */
function canonical(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonical).join(",")}]`;
  if (v && typeof v === "object") return `{${Object.keys(v).sort().map((k) => `${JSON.stringify(k)}:${canonical((v as Record<string, unknown>)[k])}`).join(",")}}`;
  return JSON.stringify(v ?? null);
}

/** The person pressed Sign in the dialog that showed exactly this plan. Bound to the plan's digest. */
export class DialogConsent {
  private static readonly mint = Symbol("consent");
  private constructor(
    readonly digest: string,
    token: symbol,
  ) {
    if (token !== DialogConsent.mint) throw new Error("consent comes from the dialog only");
  }
  static fromDialog(p: SignPlan): DialogConsent {
    return new DialogConsent(p.digest, DialogConsent.mint);
  }
}

/**
 * Sign. A plan that signs with the person key (or is marked as needing the dialog) without the dialog's
 * consent for this very plan is refused before the cockpit is called.
 */
export async function sign(t: Transport, p: SignPlan, consent: DialogConsent | undefined): Promise<Outcome<{ readonly id: string; readonly pending?: { readonly code: string; readonly reason: string } }>> {
  if (p.keyRole === "person" || p.needsDialog) {
    if (!(consent instanceof DialogConsent)) return refuse("dialog.required", "a decision is signed through the sign dialog, never in one click");
    if (consent.digest !== p.digest) return refuse("dialog.mismatch", "the dialog showed something else than what would be signed");
  }
  const out = await t.say({ template: p.template, subject: p.subject, fields: p.fields, as: p.as, ...(p.scope !== undefined ? { scope: p.scope } : {}), ...(p.expect.template ? { expectTemplate: p.expect.template } : {}), ...(p.expect.revision ? { expectRevision: p.expect.revision } : {}), ...(p.expect.predicate ? { expectPredicate: p.expect.predicate } : {}) });
  if (!out.ok) return out;
  return { ok: true, value: { id: out.value.claimId, ...(out.value.pending ? { pending: out.value.pending } : {}) } };
}

function refuse(code: string, reason: string): { ok: false; refusal: Refusal } {
  return { ok: false, refusal: { code, reason } };
}
