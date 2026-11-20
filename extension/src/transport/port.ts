// The transport port (contract C1.2). The shell reaches the cockpit only through this interface:
// the CLI today (CliTransport), `cockpit serve --stdio` from cockpit v0.6.0 (StdioTransport, same
// objects). No registry reads, no trust decisions, no git calls happen in the shell (ADR-011 §6).

/** The cockpit declining to act (cockpit/refusal.go). Programs branch on `code`, never on `reason`. */
export interface Refusal {
  readonly code: string;
  readonly clause?: string;
  readonly reason: string;
}

/** An answer is either a value or a refusal. A crash (exit 1, unparseable output) is thrown instead. */
export type Outcome<T> =
  | { readonly ok: true; readonly value: T }
  | { readonly ok: false; readonly refusal: Refusal };

/** Something broke underneath: not a refusal. Shown as an error, never as "the cockpit said no". */
export class TransportError extends Error {
  constructor(
    message: string,
    readonly exitCode: number | null,
    readonly stderr: string,
  ) {
    super(message);
    this.name = "TransportError";
  }
}

/**
 * The call took longer than its timeout and was killed. Whether the cockpit got as far as signing is
 * unknown: the registry decides, never the shell.
 */
export class TransportTimeout extends TransportError {
  constructor(message: string, stderr: string) {
    super(message, null, stderr);
    this.name = "TransportTimeout";
  }
}

// --- request and result shapes (the JSON objects of cockpit/say.go, ask.go, publish.go) ---

export interface SayRequest {
  readonly template: string;
  /** sha256 foton id or hash the claim is about. */
  readonly subject: string;
  readonly fields?: Readonly<Record<string, string>>;
  readonly scope?: string;
  readonly expectHead?: string;
  /** SPEC §8.5 (v0.5.5): the template's hash, package revision and predicate as say --json listed them */
  readonly expectTemplate?: string;
  readonly expectRevision?: string;
  readonly expectPredicate?: string;
  /** Key name of identity.keyring. The shell sets it from the window's key choice, never from an intent. */
  readonly as?: string;
}

export interface SayResult {
  readonly claimId: string;
  readonly level?: string;
  readonly confirmation: string;
  readonly chain?: { readonly scope: string; readonly prev: string; readonly length?: number };
  /**
   * cockpit v0.5.2: the claim is signed and stored, but the registry was busy, so it is not committed yet; the next
   * write commits it. Never a reason to sign again.
   */
  readonly pending?: { readonly code: string; readonly reason: string };
}

export interface BoundTerm {
  readonly type: "iri" | "literal";
  readonly value: string;
  readonly datatype?: string;
}

export interface AskRequest {
  readonly query: string;
  readonly ref?: string;
  readonly sparql?: string;
  /** A query an allowed package brings (claims.allowedPackages). */
  readonly name?: string;
  /** Values for the query's variables, appended as VALUES: nothing is spliced into query text. */
  readonly bind?: Readonly<Record<string, string>>;
  /** Typed values (cockpit v0.5.3, SPEC §9.9): an IRI, or a literal with a datatype; never query text. */
  readonly bindTerms?: Readonly<Record<string, BoundTerm>>;
  readonly limit?: number;
  readonly dataset?: "claims" | "nanopub";
  readonly format?: "sparql-results+json";
}

/** The ask answer. Only the fields the shell reads are typed; the rest stays opaque. */
export interface AskResult {
  readonly query: string;
  readonly ref: string;
  readonly raw: string;
  readonly included?: readonly string[];
  readonly excluded?: readonly string[];
  readonly results?: SparqlResults;
  readonly [more: string]: unknown;
}

export interface SparqlResults {
  readonly head: { readonly vars: readonly string[] };
  readonly results: { readonly bindings: ReadonlyArray<Readonly<Record<string, SparqlTerm>>> };
}

export interface SparqlTerm {
  readonly type: "uri" | "literal" | "bnode" | "triple";
  readonly value: string;
  readonly datatype?: string;
  readonly "xml:lang"?: string;
}

export interface PublishRequest {
  readonly inputs: readonly string[];
  readonly outputs?: readonly string[];
  readonly outputsIn?: string;
  readonly cmd: string;
  readonly environment?: string;
  readonly envRef?: string;
}

export interface PublishResult {
  readonly fotonId?: string;
  readonly revision: string;
  readonly outputHashes?: Readonly<Record<string, string>>;
  readonly locators?: Readonly<Record<string, string>>;
  readonly executedIn?: string;
  readonly networkAllowed?: boolean;
}

export interface WorkflowRunRequest {
  readonly workflow: string;
  /** Hole bindings. Each value travels as one argv element; it is never parsed by a shell. */
  readonly bind: Readonly<Record<string, string>>;
  readonly check?: boolean;
}

export interface InstallRequest {
  readonly packageDir: string;
  readonly allow: boolean;
}

export interface DoctorReport {
  readonly ready: boolean;
  readonly steps: ReadonlyArray<{ readonly must: boolean; readonly text: string }>;
  readonly fields: Readonly<Record<string, unknown>>;
}

export interface TemplateField {
  readonly type: string;
  readonly required?: boolean;
  readonly values?: readonly string[];
}

export interface TemplateInfo {
  readonly name: string;
  /** What the claim may be about: "claim", "foton" or "either" (the listing's `about`). */
  readonly target?: string;
  readonly fields: Readonly<Record<string, TemplateField>>;
  /** The package that brings the template, when it comes from one. */
  readonly package?: string;
  /** What a claim from it says (`say --json`'s predicate): the dialog shows it, and say must still mean it. */
  readonly predicate?: string;
  /** `say --json`'s hash of the template as say signs with it, and a package template's sealed revision (v0.5.5) */
  readonly hash?: string;
  readonly revision?: string;
}

export interface QueryInfo {
  readonly name: string;
  readonly parameters: readonly string[];
  readonly package?: string;
}

export interface Listing {
  readonly templates: readonly TemplateInfo[];
  readonly queries: readonly QueryInfo[];
  readonly workflows: readonly string[];
}

/**
 * A person's confirmation of an operator command (ADR-011 §6). It can only be made by the UI
 * adapter after showing the person what will happen; an agent or a viewer cannot construct one.
 */
export class OperatorConfirmation {
  private static readonly mint = Symbol("confirmation");
  private constructor(
    readonly summary: string,
    readonly at: Date,
    token: symbol,
  ) {
    if (token !== OperatorConfirmation.mint) throw new Error("confirmations come from the UI only");
  }
  /** Called by the UI after the person pressed Confirm on a dialog that showed `summary`. */
  static fromPerson(summary: string, at: Date = new Date()): OperatorConfirmation {
    return new OperatorConfirmation(summary, at, OperatorConfirmation.mint);
  }
}

import type { UiListing } from "../packages/listing.js";

export interface Transport {
  ask(req: AskRequest): Promise<Outcome<AskResult>>;
  say(req: SayRequest): Promise<Outcome<SayResult>>;
  publish(req: PublishRequest): Promise<Outcome<PublishResult>>;
  workflowRun(req: WorkflowRunRequest): Promise<Outcome<Readonly<Record<string, unknown>>>>;
  /** Operator command: refused without a person's confirmation. */
  install(req: InstallRequest, confirmation: OperatorConfirmation | undefined): Promise<Outcome<Readonly<Record<string, unknown>>>>;
  /** The templates the cockpit allows here, with their fields: the only source of templates. */
  templates(): Promise<Outcome<TemplateInfo[]>>;
  list(): Promise<Outcome<Listing>>;
  doctor(): Promise<DoctorReport>;
  version(): Promise<Outcome<{ readonly text: string }>>;
  /** `cockpit ui list --json` (C2.4): the installed packages' ui declarations, checked in shape. */
  uiList(): Promise<Outcome<UiListing>>;
  /** A usable package's member bytes by content hash: `cockpit ui cat <package> <hash>` (v0.5.4). Checked again by the caller. */
  uiCat(pkg: string, hash: string): Promise<Uint8Array>;
  /** graph@1's data: `cockpit show --json` (v0.5.3): records, the trusted ring, names, admitted packages' build keys. */
  graph(): Promise<Outcome<{ readonly records: readonly unknown[]; readonly keys: Readonly<Record<string, unknown>>; readonly names: Readonly<Record<string, unknown>>; readonly packages: Readonly<Record<string, unknown>> }>>;
}
