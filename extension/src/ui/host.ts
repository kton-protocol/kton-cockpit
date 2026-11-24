// The UI host port: the only place the shell touches VS Code. The controller talks to this
// interface, so it is unit-tested with a fake host and runs in VS Code through src/extension.ts.

export interface TreeNode {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
  readonly tooltip?: string;
  readonly children?: readonly TreeNode[];
  /** Command run when the node is clicked. */
  readonly command?: { readonly id: string; readonly args: readonly unknown[] };
  /** Shown but not usable (e.g. a later fold-in); the tooltip says why. */
  readonly disabled?: boolean;
}

export type StatusId = "identity" | "mode" | "poll" | "decisions" | "surface" | "slots" | "index";
export type ViewId = "home" | "inbox" | "lanes";

import type { LeafViewer, LoadedViewer } from "../packages/model.js";

export interface UiHost {
  /**
   * A composition panel (C1.5 v3): kton-web's composition frame with the host-owned CSP. The host serves the
   * checked bytes of these viewer trees and nothing else; `urls` says where each module is (the plan the frame
   * gets names those). Messages from the frame arrive after this returns (it loads first).
   */
  showComposition(key: string, title: string, viewers: readonly LoadedViewer[], onMessage: (msg: unknown) => void): { post: (msg: unknown) => void; urls: ReadonlyMap<string, string> };
  /** A leaf panel: the vendored viewer's own page (tree-checked against the package's pin), the host's head and CSP. */
  showLeaf(key: string, title: string, leaf: LeafViewer, onMessage: (msg: unknown) => void): { post: (msg: unknown) => void };
  setStatus(id: StatusId, text: string, tooltip?: string, command?: string): void;
  setTree(view: ViewId, nodes: readonly TreeNode[]): void;
  /**
   * Show a panel with HTML. Messages from the page come back through `onMessage`. Pages are untrusted:
   * the controller treats every message as an intent and validates it.
   */
  /** reveal false: render in place without moving the focus to it. */
  showPanel(key: string, title: string, html: string, onMessage?: (msg: unknown) => void, reveal?: boolean): void;
  /** The keys of the panels shown with showPanel that are still open. */
  openPanels(): string[];
  /** Open a file as it was at a commit, read-only (kton-src), at its line; says what was opened or why not. */
  openSource?(t: import("./sources.js").SourceTarget): Promise<string>;
  /** Whether an approval on one commit holds at another (read-only git over the mirrors, kton.sources.dir). */
  approvalStaleness?(repo: string, approved: string, head: string, paths: readonly string[]): Promise<import("./sources.js").Staleness>;
  /** The commits between two commits that touched a test file or a helper it imports (read-only git over the mirrors). */
  changesBetween?(repo: string, from: string, to: string, path: string): Promise<import("./sources.js").Changes>;
  /** The commits under a rerun at head since the last reviewed state, each with its review (read-only git over the mirrors). */
  reviewedState?(repo: string, head: string, reviewOf: (sha: string) => string | undefined, base?: string): Promise<import("./sources.js").ReviewedState | { why: string }>;
  /** A run artifact's bytes as a data URL for a thumbnail, only after its sha256 checks (undefined: not shown). */
  artifactThumb?(t: import("./artifacts.js").ArtifactTarget): Promise<string | undefined>;
  /** A run artifact, read-only, checked against its sha256: "Opened …" / "Shown …" when shown, else why not. */
  openArtifact?(t: import("./artifacts.js").ArtifactTarget): Promise<string>;
  /**
   * The sign dialog for a person-key signature (C1.4, O1): resolves true only after the person ticked
   * "I know I sign this now as <person>" and pressed Sign with a deliberate act. Enter, Escape and
   * closing the dialog never sign.
   */
  /** `view`: the plan and the shown signer, for the structured dialog; the detail is the same plan as text. */
  signDialog(title: string, detail: string, person: string, view?: { readonly plan: import("../sign.js").SignPlan; readonly signer: import("../identity.js").ShownSigner }): Promise<boolean>;
  /**
   * What became of the signature the last sign dialog asked for, shown in that dialog (S7.3: a timeout
   * shows as failed there, and the registry decides whether a claim exists).
   */
  signOutcome(ok: boolean, text: string): void;
  /** Close a panel shown with showPanel (a sent compose form). */
  closePanel(key: string): void;
  pick(title: string, items: readonly string[]): Promise<string | undefined>;
  input(prompt: string, value?: string): Promise<string | undefined>;
  info(text: string): void;
  error(text: string): void;
  /** The local output channel (O7): ask calls and refused intents, never signed. */
  log(line: string): void;
  /** Open a folder in a NEW window (a surface's own session). */
  openWindow(folder: string): Promise<void>;
  /** Run another extension's command if that extension is installed; false if it is not. */
  runExternal(extensionId: string, command: string): Promise<boolean>;
}
