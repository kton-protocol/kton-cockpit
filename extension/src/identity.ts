// Identity of a window (contract C1.3; decision O1: two keys per window).
//
// - One person per window. Changing the person means another window.
// - Two keys per window: the PERSON key signs decisions, always through the sign dialog; the
//   AGENT (home) key signs records stored in nekton, without a dialog per write. An agent-key record
//   never counts as a decision.
// - Name and tier are shown only from the cockpit's re-verified answer for this render, looked up by
//   key in the reader's trust config (view vocabulary 1: nk:signer → rdfs:label, nk:tier). Never
//   derived here, never from viewer data, never cached (review checks 1 and 3). The name in
//   .kton/local/identity.json is a hint and is shown as one.
// - A key the window was not given is MISSING and named as such; neither key stands in for the other.

import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { DoctorReport, Transport } from "./transport/port.js";

/** What .kton/local/identity.json says (ADR-016). A hint: nothing in it is verified. */
export interface IdentityHint {
  readonly sessionId: string;
  readonly planktonKey?: string;
  readonly nektonKey?: string;
  /** The folder `say --as NAME` takes NAME's keys from. */
  readonly keyring?: string;
}

/** Read the checkout's identity file. Missing or malformed: undefined (the window shows "no identity"). */
export async function readIdentityHint(repoRoot: string): Promise<IdentityHint | undefined> {
  try {
    const o = JSON.parse(await readFile(join(repoRoot, ".kton", "local", "identity.json"), "utf8")) as Record<string, unknown>;
    if (typeof o["session_id"] !== "string" || o["session_id"] === "") return undefined;
    return {
      sessionId: o["session_id"],
      ...(typeof o["plankton_key"] === "string" ? { planktonKey: o["plankton_key"] } : {}),
      ...(typeof o["nekton_key"] === "string" ? { nektonKey: o["nekton_key"] } : {}),
      ...(typeof o["keyring"] === "string" ? { keyring: o["keyring"] } : {}),
    };
  } catch {
    return undefined;
  }
}

/**
 * Where name and tier may come from. "none" until kton-cockpit adds an identity verb that returns
 * them re-verified for both keys of a window: everything shows "unverified".
 * "view" asks the claim view; on cockpit/kton-layout its rdfs:label is the key FILE name and the
 * view is still vocabulary 0, so it is not used for the status bar yet.
 */
export type SignerSource = "none" | "view";

export type KeyRole = "person" | "agent";

export interface WindowKey {
  readonly role: KeyRole;
  /** Key name of identity.keyring, passed to `say` as `as`. */
  readonly keyName: string;
  /**
   * The key id of the key this window's claims are signed with (`<keyName>-claims`), as the claim view
   * names its signer: https://kton.dev/agent/<keyId>. Derived from that key's public half (see
   * claimKeyId); "" when it could not be. A decision is recognised by this id, never by a label.
   */
  readonly keyId: string;
}

export interface WindowIdentity {
  /** The name from identity.json: a hint only. */
  readonly hintName: string;
  readonly person: WindowKey;
  readonly agent: WindowKey;
}

export type ShownSigner =
  | { readonly verified: true; readonly keyId: string; readonly name: string; readonly tier: string }
  | { readonly verified: false; readonly keyId: string; readonly hint: string; readonly why: string };

/** The view-vocabulary-1 query. KEYID is bound as a literal (VALUES), nothing spliced into the text. */
export const SIGNER_QUERY = `PREFIX nk: <https://kton.dev/v/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
SELECT ?name ?tier WHERE {
  BIND(IRI(CONCAT("https://kton.dev/agent/", STR(?KEYID))) AS ?agent)
  ?agent rdfs:label ?name ; nk:tier ?tier .
}`;

/**
 * Resolve how a key is shown, from the cockpit's answer, for this render only. There is no cache:
 * a trust-config change shows at the next render.
 */
export async function showSigner(t: Transport, keyId: string, hint: string, source: SignerSource = "none"): Promise<ShownSigner> {
  if (source === "none") {
    return { verified: false, keyId, hint, why: "this cockpit has no identity verb yet" };
  }
  if (!/^[0-9a-f]{16,128}$/.test(keyId)) {
    return { verified: false, keyId, hint, why: "not a key id" };
  }
  const out = await t.ask({ query: "sparql", sparql: SIGNER_QUERY, bind: { KEYID: keyId }, format: "sparql-results+json", limit: 2 });
  if (!out.ok) return { verified: false, keyId, hint, why: out.refusal.reason };
  const rows = out.value.results?.results.bindings ?? [];
  const row = rows[0];
  if (rows.length !== 1 || !row || !row["name"] || !row["tier"]) {
    return { verified: false, keyId, hint, why: rows.length === 0 ? "the key is not in this reader's trust config" : "the trust config names the key twice" };
  }
  return { verified: true, keyId, name: row["name"].value, tier: row["tier"].value };
}

/** Which key a control signs with: decisions with the person key (dialog), records with the agent key. */
export function keyFor(identity: WindowIdentity, kind: "decision" | "record"): WindowKey {
  return kind === "decision" ? identity.person : identity.agent;
}

/**
 * Status bar text: the two key NAMES, short and distinct ("person key demo-person · agent key demo-person-agent ·
 * unverified"); a verified key shows its verified name and tier; a missing key is named as missing. The hint
 * from identity.json and the reason a key is unverified go in the tooltip (statusTooltip), not in the bar.
 */
export function statusText(person: ShownSigner | "missing", agent: ShownSigner | "missing", names: { readonly person: string; readonly agent: string }): string {
  const one = (label: string, s: ShownSigner | "missing", name: string) => (s === "missing" ? `${label} missing` : s.verified ? `${label} ${s.name} (${s.tier})` : `${label} ${name}`);
  const unverified = [person, agent].some((s) => s !== "missing" && !s.verified);
  return `${one("person key", person, names.person)} · ${one("agent key", agent, names.agent)}${unverified ? " · unverified" : ""}`;
}

/** The status bar item's tooltip: per key, the hint from identity.json and why it is unverified (or its tier). */
export function statusTooltip(person: ShownSigner | "missing", agent: ShownSigner | "missing"): string {
  const one = (label: string, s: ShownSigner | "missing") =>
    s === "missing" ? `${label}: missing` : s.verified ? `${label}: ${s.name}, tier ${s.tier} (verified by the cockpit)` : `${label}: ${s.hint} — unverified: ${s.why}`;
  return `${one("Person key", person)}\n${one("Agent key", agent)}`;
}

/** A key the window was given (a key name), or not. */
export function hasKey(k: WindowKey): boolean {
  return k.keyName !== "";
}

/** A key id as the kernels make it: the first 16 hex digits of the SHA-256 of the 32-byte public key. */
export function keyIdOfPublicKey(publicHex: string): string | undefined {
  const hex = publicHex.trim().toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(hex)) return undefined;
  return createHash("sha256").update(Buffer.from(hex, "hex")).digest("hex").slice(0, 16);
}

/** The key id in a claim view signer IRI (https://kton.dev/agent/<keyid>); undefined for anything else. */
export function signerKeyId(signer: string | undefined): string | undefined {
  const m = /^https:\/\/kton\.dev\/agent\/([0-9a-f]{16})$/.exec(signer ?? "");
  return m?.[1];
}

export type ClaimKeyId = { readonly ok: true; readonly keyId: string; readonly checkedByDoctor: boolean } | { readonly ok: false; readonly why: string };

/**
 * The key id a window key signs its claims with, from the public key the window is configured with:
 * `<keyring>/<keyName>-claims.pub`, the public half of what `say --as keyName` signs with. Never from a
 * label. When the cockpit's `doctor` answers for the same key (its default claim key is that file's
 * private half), its key id, which it derives from the private seed, must agree; if it does not, the
 * .pub is not this key's and the window has no key id.
 */
export async function claimKeyId(hint: IdentityHint | undefined, keyName: string, doctor?: DoctorReport): Promise<ClaimKeyId> {
  if (!keyName) return { ok: false, why: "no key configured" };
  if (!hint?.keyring) return { ok: false, why: "the identity file names no keyring" };
  let id: string | undefined;
  try {
    id = keyIdOfPublicKey(await readFile(join(hint.keyring, `${keyName}-claims.pub`), "utf8"));
  } catch {
    return { ok: false, why: `no public key ${keyName}-claims.pub in the keyring` };
  }
  if (!id) return { ok: false, why: `${keyName}-claims.pub is not an ed25519 public key in hex` };
  const ident = doctor?.fields["identity"] as { nektonKeyId?: unknown } | undefined;
  const same = hint.nektonKey !== undefined && hint.nektonKey === join(hint.keyring, `${keyName}-claims.key`);
  if (same && typeof ident?.nektonKeyId === "string" && ident.nektonKeyId !== "") {
    if (ident.nektonKeyId !== id) return { ok: false, why: `${keyName}-claims.pub does not belong to the key the cockpit signs with (doctor: ${ident.nektonKeyId})` };
    return { ok: true, keyId: id, checkedByDoctor: true };
  }
  return { ok: true, keyId: id, checkedByDoctor: false };
}
