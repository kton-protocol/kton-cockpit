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
}

export const FOOTER = "A kton signature is not yet a QMS e-signature: every signature here is test-only.";
export const RULES_NOTE = "Rules shown for information: not enforced until the cockpit's rule gate exists.";

export interface SignPlan {
  readonly control: ControlInfo;
  readonly template: string;
  readonly subject: string;
  readonly fields: Readonly<Record<string, string>>;
  readonly meaning?: string;
  readonly as: string;
  readonly keyRole: "person" | "agent";
  readonly needsDialog: boolean;
  readonly footer: string;
  readonly rules: string;
  /** Hash of exactly what is shown; a dialog confirmation is bound to it. */
  readonly digest: string;
}

/** Build the plan from an intent. The intent supplies only `about` and field values. */
export function plan(
  intent: Extract<Intent, { kind: "sign" }>,
  control: ControlInfo,
  templates: readonly TemplateInfo[],
  identity: WindowIdentity,
): Outcome<SignPlan> {
  const tpl = templates.find((t) => t.name === control.template && (t.package === undefined || t.package === control.package));
  if (!tpl) return refuse("template.not-allowed", `${control.package}/${control.name} signs ${control.template}, which is not installed here`);

  const fields: Record<string, string> = {};
  for (const [k, v] of Object.entries(intent.fields)) {
    if (k === "as" || k === "meaning" && control.meaning !== undefined) {
      return refuse("argument", `an intent cannot set ${k}`);
    }
    const f = tpl.fields[k];
    if (!f) return refuse("argument", `template ${tpl.name} has no field ${k}`);
    if (f.values && !f.values.includes(v)) return refuse("argument", `${k} must be one of ${f.values.join("|")}`);
    fields[k] = v;
  }
  if (control.meaning !== undefined) {
    if (!tpl.fields["meaning"]) return refuse("argument", `template ${tpl.name} has no meaning field`);
    fields["meaning"] = control.meaning;
  }
  for (const [k, f] of Object.entries(tpl.fields)) {
    if (f.required && fields[k] === undefined) return refuse("argument", `${k} is required by ${tpl.name}`);
  }

  const key = keyFor(identity, control.kind);
  // No fallback: a decision needs the person key, a record the agent key; a missing one is named.
  if (!key.keyName) return refuse("identity.key-missing", `this window has no ${key.role} key, so it cannot sign a ${control.kind}`);
  const shown = { template: tpl.name, subject: intent.about, fields, as: key.keyName, control: `${control.package}/${control.name}` };
  const digest = createHash("sha256").update(JSON.stringify(shown, Object.keys(shown).sort())).update(JSON.stringify(Object.entries(fields).sort())).digest("hex");
  const meaning = fields["meaning"];
  return {
    ok: true,
    value: {
      control,
      template: tpl.name,
      subject: intent.about,
      fields,
      ...(meaning !== undefined ? { meaning } : {}),
      as: key.keyName,
      keyRole: key.role,
      needsDialog: control.kind === "decision",
      footer: FOOTER,
      rules: RULES_NOTE,
      digest,
    },
  };
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
  const out = await t.say({ template: p.template, subject: p.subject, fields: p.fields, as: p.as });
  if (!out.ok) return out;
  return { ok: true, value: { id: out.value.claimId, ...(out.value.pending ? { pending: out.value.pending } : {}) } };
}

function refuse(code: string, reason: string): { ok: false; refusal: Refusal } {
  return { ok: false, refusal: { code, reason } };
}
