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

import { readFile } from "node:fs/promises";
import { join } from "node:path";
import type { Transport } from "./transport/port.js";

/** What .kton/local/identity.json says (ADR-016). A hint: nothing in it is verified. */
export interface IdentityHint {
  readonly sessionId: string;
  readonly planktonKey?: string;
  readonly nektonKey?: string;
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
  /** Hex key id; the agent IRI is https://kton.dev/agent/<keyId>. */
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

/** Status bar text: both keys, each with its verified name, marked unverified, or named as missing. */
export function statusText(person: ShownSigner | "missing", agent: ShownSigner | "missing"): string {
  const one = (label: string, s: ShownSigner | "missing") =>
    s === "missing" ? `${label}: missing` : s.verified ? `${label}: ${s.name} (${s.tier})` : `${label}: ${s.hint} (unverified: ${s.why})`;
  return `${one("person key", person)} · ${one("agent key", agent)}`;
}

/** A key the window was given (a key name), or not. */
export function hasKey(k: WindowKey): boolean {
  return k.keyName !== "";
}
