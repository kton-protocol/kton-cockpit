// HTML of the panels (pure functions; tested without VS Code). Every value is escaped. Every page gets
// the host-owned CSP: default-src 'none', the style and the one script by nonce, connect-src 'none',
// no 'unsafe-inline' anywhere and no style= attributes. Scripts only post intents back to the shell.
// Elements a test needs carry a stable data-testid.

import type { Row } from "./queries.js";
import { idOf, kindOf } from "./queries.js";
import type { SignPlan } from "../sign.js";
import type { ShownSigner } from "../identity.js";
import type { TemplateInfo } from "../transport/port.js";

export function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);
}

/** The CSP every shell page carries (asserted for each page in test/pages.test.ts). */
export function csp(nonce: string): string {
  return `default-src 'none'; style-src 'nonce-${nonce}'; script-src 'nonce-${nonce}'; connect-src 'none'; img-src 'none'; form-action 'none'; base-uri 'none'`;
}

const STYLE = `body{font-family:var(--vscode-font-family);color:var(--vscode-foreground);background:var(--vscode-editor-background);padding:12px 18px;line-height:1.5}
h1{font-size:18px;margin:0 0 8px} h2{font-size:15px} .muted{color:var(--vscode-descriptionForeground)} .box{border:1px solid var(--vscode-panel-border);border-radius:6px;padding:10px 12px;margin:10px 0}
.opt{display:flex;gap:10px;align-items:flex-start;margin:6px 0} .rec{font-size:11px;border:1px solid currentColor;border-radius:4px;padding:0 5px;margin-left:6px}
button{font:inherit;padding:4px 12px;border-radius:4px;border:1px solid var(--vscode-button-border,transparent);background:var(--vscode-button-background);color:var(--vscode-button-foreground);cursor:pointer}
button:disabled{opacity:.5;cursor:default}
table{border-collapse:collapse} td,th{border-bottom:1px solid var(--vscode-panel-border);padding:4px 8px;text-align:left;vertical-align:top} code{font-family:var(--vscode-editor-font-family)}
label.field{display:block;margin:8px 0} label.field span{display:block} input[type=text],textarea{font:inherit;width:100%;max-width:640px;box-sizing:border-box}
.error{color:var(--vscode-errorForeground)} .confirm{display:flex;gap:8px;align-items:center;margin:12px 0} .actions{display:flex;gap:8px}
pre.box{white-space:pre-wrap;font-family:var(--vscode-editor-font-family)} .outcome{margin-top:10px;font-weight:600}`;

function page(title: string, body: string, nonce: string, script = "", style = ""): string {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${csp(nonce)}">
<title>${esc(title)}</title>
<style nonce="${nonce}">${STYLE}${style}</style></head><body>${body}${script ? `<script nonce="${nonce}">${script}</script>` : ""}</body></html>`;
}

export interface DecisionOption {
  readonly id: string;
  readonly label: string;
  readonly consequence?: string;
}

/** The options field of a proposal: a JSON array of {id, label, consequence}. Anything else: no options. */
export function parseOptions(raw: string | undefined): DecisionOption[] {
  if (!raw) return [];
  try {
    const v = JSON.parse(raw) as unknown;
    if (!Array.isArray(v)) return [];
    return v
      .filter((o): o is Record<string, unknown> => typeof o === "object" && o !== null)
      .filter((o) => typeof o["id"] === "string" && typeof o["label"] === "string")
      .map((o) => ({ id: String(o["id"]), label: String(o["label"]), ...(typeof o["consequence"] === "string" ? { consequence: o["consequence"] } : {}) }));
  } catch {
    return [];
  }
}

/** A signer as the trust config names it, or "unknown signer" when the view gives no tier for it. */
export function signerLabel(label: string | undefined, tier: string | undefined): string {
  return label && tier ? `${label} (${tier})` : "unknown signer";
}

export const RULES_LABEL = "Rules are shown for information only: they are not enforced until the cockpit's rule gate exists.";

export interface DecisionView {
  /** Why signing is not possible in this window (no person key, no identity file), if it is not. */
  readonly cannotSign?: string;
  /** Claims of the decision's template about this proposal by signers that are not a person: records. */
  readonly records?: readonly Row[];
}

/** Screen 3: a decision with its options. A button posts a sign intent; the shell opens the sign dialog. */
export function decisionPage(row: Row, nonce: string, view: DecisionView = {}): string {
  const id = idOf(row["msg"] ?? "");
  const opts = parseOptions(row["options"]);
  const rec = row["recommended"];
  const off = view.cannotSign ? " disabled" : "";
  const list = opts
    .map(
      (o) => `<div class="opt" data-testid="option-${esc(o.id)}"><button data-choice="${esc(o.id)}" data-testid="choose-${esc(o.id)}" aria-label="Choose ${esc(o.id)}"${off}>Choose ${esc(o.id)}</button>
<div><b>${esc(o.id)} · ${esc(o.label)}</b>${o.id === rec ? '<span class="rec" data-testid="recommended">recommended</span>' : ""}<div class="muted" data-testid="consequence-${esc(o.id)}">${esc(o.consequence ?? "")}</div></div></div>`,
    )
    .join("");
  const records = (view.records ?? [])
    .map((r) => `<div class="box" data-testid="agent-record">Recorded by ${esc(signerLabel(r["by"], r["byTier"]))}: <b>${esc(r["choice"] ?? r["outcome"] ?? "")}</b>. <span class="muted">Not a decision: only a person key decides.</span></div>`)
    .join("");
  const body = `<h1>${esc(row["title"] ?? "Decision")}</h1>
<div class="muted">${esc(row["area"] ?? "")} · from ${esc(signerLabel(row["from"], row["fromTier"]))} · ${esc(row["when"] ?? "")} · <code>${esc(id)}</code></div>
<div class="box" data-testid="proposal-text">${esc(row["text"] ?? "")}</div>
<div class="muted">Why: ${esc(row["why"] ?? "")}</div>
<div class="box" data-testid="guard"><b>Rules</b><div class="muted">${esc(RULES_LABEL)}</div></div>
<div class="box"><b>Options</b>${list || '<div class="muted">This proposal has no options.</div>'}</div>
${records}
${view.cannotSign ? `<div class="error" data-testid="cannot-sign">${esc(view.cannotSign)}</div>` : '<div class="muted">Choosing opens the sign dialog. Nothing is signed before you confirm there and press Sign.</div>'}`;
  const script = `const vscode=acquireVsCodeApi();for(const b of document.querySelectorAll('button[data-choice]')){b.addEventListener('click',()=>vscode.postMessage({kind:'sign',about:${JSON.stringify(id)},fields:{answers:${JSON.stringify(id)},outcome:'https://schema.org/AcceptAction',choice:b.dataset.choice}}));}`;
  return page(row["title"] ?? "Decision", body, nonce, script);
}

export interface ThreadView {
  readonly cannotSign?: string;
  /** The message is still open: it can be answered with the closing template (S2.3). */
  readonly answerable?: boolean;
  /** An answer text kept after a cancelled dialog. */
  readonly draft?: string;
  readonly error?: string;
}

/** Screen 2: one message, what answered it, and (while open) the answer form. */
export function threadPage(row: Row, answers: readonly Row[], nonce: string, view: ThreadView = {}): string {
  const id = idOf(row["msg"] ?? "");
  const ans = answers
    .map((a) => `<div class="box" data-testid="answer"><b>${esc(kindOf(a["kind"] ?? "done"))}</b> · ${esc(signerLabel(a["by"], a["byTier"]))} · ${esc(a["when"] ?? "")}<div>${esc(a["outcome"] ?? a["text"] ?? "")}</div></div>`)
    .join("");
  const form = view.answerable
    ? `<div class="box" data-testid="answer-form"><b>Answer</b>
<label class="field"><span>Outcome (required)</span><textarea id="outcome" rows="3" data-testid="answer-outcome">${esc(view.draft ?? "")}</textarea></label>
${view.error ? `<div class="error" data-testid="answer-error">${esc(view.error)}</div>` : ""}
<button id="answer" data-testid="answer-send"${view.cannotSign ? " disabled" : ""}>Answer with done…</button>
${view.cannotSign ? `<div class="error" data-testid="cannot-sign">${esc(view.cannotSign)}</div>` : '<div class="muted">Answering opens the sign dialog.</div>'}</div>`
    : "";
  const body = `<h1>${esc(row["title"] ?? "Message")}</h1>
<div class="muted">${esc(kindOf(row["kind"] ?? ""))} · ${esc(row["area"] ?? "")} · from ${esc(signerLabel(row["from"], row["fromTier"]))} · ${esc(row["when"] ?? "")} · <code data-testid="claim-id">${esc(id)}</code></div>
<div class="box" data-testid="message-text">${esc(row["text"] ?? "")}</div>
${row["why"] ? `<div class="muted">Why: ${esc(row["why"])}</div>` : ""}
<h2>Answers</h2>${ans || '<div class="muted">No answer yet.</div>'}${form}`;
  const script = view.answerable
    ? `const vscode=acquireVsCodeApi();document.getElementById('answer').addEventListener('click',()=>vscode.postMessage({kind:'sign',about:${JSON.stringify(id)},fields:{answers:${JSON.stringify(id)},outcome:document.getElementById('outcome').value}}));`
    : "";
  return page(row["title"] ?? "Message", body, nonce, script);
}

export interface ComposeView {
  readonly values: Readonly<Record<string, string>>;
  readonly error?: string;
  readonly missing?: readonly string[];
  readonly cannotSign?: string;
}

/** Compose a claim with a listed template: its fields from the cockpit's listing, plus what it is about. */
export function composePage(tpl: TemplateInfo, nonce: string, view: ComposeView): string {
  const names = ["about", ...Object.keys(tpl.fields)];
  const field = (k: string) => {
    const f = k === "about" ? { type: "ref", required: true } : tpl.fields[k];
    const miss = view.missing?.includes(k);
    return `<label class="field"><span>${esc(k)}${f?.required ? " (required)" : ""}${f?.values ? ` — one of ${esc(f.values.join(" | "))}` : ""}${miss ? ' <b class="error" data-testid="missing-' + esc(k) + '">is required</b>' : ""}</span>
<input type="text" data-field="${esc(k)}" data-testid="field-${esc(k)}" value="${esc(view.values[k] ?? "")}"></label>`;
  };
  const body = `<h1>Compose ${esc(tpl.name)}</h1>
<div class="muted">The fields come from the cockpit's template listing. "about" is the sha256 id of what the claim is about.</div>
${names.map(field).join("")}
${view.error ? `<div class="error" data-testid="compose-error">${esc(view.error)}</div>` : ""}
<div class="actions"><button id="send" data-testid="compose-send"${view.cannotSign ? " disabled" : ""}>Send…</button></div>
${view.cannotSign ? `<div class="error" data-testid="cannot-sign">${esc(view.cannotSign)}</div>` : '<div class="muted">Send opens the sign dialog; cancelling it keeps this draft.</div>'}`;
  const script = `const vscode=acquireVsCodeApi();document.getElementById('send').addEventListener('click',()=>{const v={};for(const i of document.querySelectorAll('input[data-field]'))v[i.dataset.field]=i.value;vscode.postMessage({kind:'compose',values:v});});`;
  return page(`Compose ${tpl.name}`, body, nonce, script);
}

export function resultsPage(title: string, rows: readonly Row[], nonce: string): string {
  const vars = [...new Set(rows.flatMap((r) => Object.keys(r)))];
  const table = rows.length
    ? `<table data-testid="results"><tr>${vars.map((v) => `<th>${esc(v)}</th>`).join("")}</tr>${rows.map((r) => `<tr>${vars.map((v) => `<td>${esc(r[v] ?? "")}</td>`).join("")}</tr>`).join("")}</table>`
    : '<div class="muted">No rows.</div>';
  return page(title, `<h1>${esc(title)}</h1><div class="muted">${rows.length} rows</div>${table}`, nonce);
}

/** The sign dialog's text (C1.4): exactly what `cockpit say` will sign, the signer, footer and rules note. */
export function signDialogDetail(p: SignPlan, signer: ShownSigner): string {
  const who = signer.verified ? `${signer.name} (${signer.tier})` : `${signer.hint} — unverified: ${signer.why}`;
  const fields = Object.entries(p.fields)
    .map(([k, v]) => `  ${k} = ${v}`)
    .join("\n");
  return [
    `Template: ${p.template}  (control ${p.control.package}/${p.control.name})`,
    ...(p.control.title ? [`Control: ${p.control.title}`] : []),
    ...(p.predicate !== undefined ? [`Says: ${p.predicate}`] : []),
    `About: ${p.subject}`,
    `Fields:\n${fields || "  (none)"}`,
    ...(p.meaning !== undefined ? [p.meaningField ? `Meaning: ${p.meaning} (signed as ${p.meaningField} = ${p.meaning})` : `Act: ${p.meaning} (the control's meaning, not a field of the claim)`] : []),
    ...(p.expect.template ? [`Template as listed: ${p.expect.template.slice(0, 19)}…${p.expect.revision ? ` · package revision ${p.expect.revision.slice(0, 19)}…` : ""}`] : []),
    ...(p.scope !== undefined ? [`Into scope: ${p.scope}`] : []),
    ...(p.opened?.length ? [`Evidence you opened in this view (${p.opened.length}):\n${p.opened.map((r) => `  ${r}${(p.fields["lookedAt"] ?? "").split(/\s+/).includes(r) ? "  (in lookedAt)" : "  (not in lookedAt: you took it out)"}`).join("\n")}`] : []),
    `Signs with: ${p.keyRole} key "${p.as}" · ${who}`,
    "",
    p.rules,
    p.footer,
  ].join("\n");
}

/** A looked-at reference as a person reads it: the decoded file name and where it is; the full reference stays. */
export function refLabel(ref: string): { title: string; sub: string; loc: string } {
  const dec = (x: string) => {
    try {
      return decodeURIComponent(x);
    } catch {
      return x;
    }
  };
  let m = /^kton-src:\/([^/]+)\/(.+?)\?([0-9a-f]{7,40})(?:#L(\d+))?$/.exec(ref);
  if (m) {
    const path = dec(m[2] ?? "");
    const file = path.split("/").pop() ?? path;
    const dir = path.slice(0, path.length - file.length);
    return { title: file, sub: m[4] ? `line ${m[4]}` : "", loc: `${m[1]}${dir ? ` · ${dir}` : ""} · at ${(m[3] ?? "").slice(0, 12)}` };
  }
  m = /^run-artifact:([^/]+)\/(.+)@sha256:([0-9a-f]{64})$/.exec(ref);
  if (m) {
    const name = dec(m[2] ?? "");
    const file = name.split("/").pop() ?? name;
    return { title: file, sub: /\.(png|jpe?g|gif|webp)$/i.test(file) ? "screenshot, hash checked" : "run artifact, hash checked", loc: `run ${m[1]} · ${name.slice(0, name.length - file.length)} · sha256 ${(m[3] ?? "").slice(0, 12)}` };
  }
  return { title: ref, sub: "", loc: "" };
}

const DIALOG_STYLE = `body{padding:0 0 12px;max-width:760px} .runbar{display:flex;flex-wrap:wrap;gap:2px 10px;align-items:center;margin:0;padding:4px 18px;font-size:12px;background:var(--vscode-inputValidation-warningBackground,#f6f5d2);border-bottom:1px solid var(--vscode-inputValidation-warningBorder,#b89500)}
.runbar b{letter-spacing:.06em;font-size:11px;color:var(--vscode-editorWarning-foreground,#bf8803);text-transform:uppercase}
.dlg{padding:14px 18px 0} .eyebrow{font-size:12px;color:var(--vscode-descriptionForeground)} .dlg h1{margin:4px 0 4px}
.blk{padding:12px 0;border-top:1px solid var(--vscode-widget-border,var(--vscode-panel-border))} .blk:first-of-type{border-top:0}
.blk h2{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--vscode-descriptionForeground);font-weight:600;margin:0 0 6px}
.ln{display:grid;grid-template-columns:minmax(7em,max-content) minmax(0,1fr);gap:0 12px;font-size:13px;margin:2px 0} .ln .k{color:var(--vscode-descriptionForeground)} .ln .v{overflow-wrap:anywhere}
.hide{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
.mono{font-family:var(--vscode-editor-font-family);font-size:12px} .big{font-size:20px;font-weight:600}
.badge{font-size:11px;padding:1px 7px;border-radius:9px;background:var(--vscode-badge-background);color:var(--vscode-badge-foreground);font-weight:400;letter-spacing:0}
.pill{font-size:11px;padding:0 7px;border-radius:9px;border:1px solid currentColor;white-space:nowrap}
.ok{color:var(--vscode-testing-iconPassed,#388a34)} .warn{color:var(--vscode-editorWarning-foreground,#bf8803)}
.chip{font-size:11px;padding:1px 7px;border-radius:9px;border:1px solid var(--vscode-widget-border,#ccc);color:var(--vscode-descriptionForeground)}
ul.ev{list-style:none;margin:0;padding:0} ul.ev li{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:2px 8px;padding:6px 0;border-bottom:1px solid var(--vscode-widget-border,var(--vscode-panel-border))}
ul.ev .t{font-weight:600;overflow-wrap:anywhere} ul.ev .pill{align-self:start} .blk > .muted{font-size:12px;margin-top:4px} .foot{padding-left:18px;padding-right:18px} ul.ev .t .muted{font-weight:400} .loc{display:block;font-family:var(--vscode-editor-font-family);font-size:11px;color:var(--vscode-descriptionForeground);overflow-wrap:anywhere}
.info{display:flex;gap:8px;margin:8px 0 0;padding:6px 10px;font-size:12px;border-left:3px solid var(--vscode-focusBorder);background:var(--vscode-editorWidget-background)}
.foot{position:sticky;bottom:0;background:var(--vscode-editor-background);padding:10px 0 4px;border-top:1px solid var(--vscode-widget-border,var(--vscode-panel-border))}
#cancel{background:var(--vscode-button-secondaryBackground);color:var(--vscode-button-secondaryForeground)} #sign:not(:disabled){background:var(--vscode-button-background);color:var(--vscode-button-foreground)}
#sign:disabled{background:var(--vscode-button-background);color:var(--vscode-button-foreground);opacity:.5}`;

/**
 * The sign dialog's structure, from the same plan the text detail is made from: the test-key banner,
 * what you sign (act, scope), what it is about, the fields, the evidence opened in this view (decoded names, the full
 * reference in the tooltip), the signer, the record (template, says, hashes) and the rules note. Every line keeps its
 * text ("Template: …", "choice = a", "Into scope: …"): only the look changes, nothing that is signed.
 */
export function signDialogBody(p: SignPlan, signer: ShownSigner, title: string): string {
  // a line keeps its text ("Template: x", "choice = a") for anyone reading it as text; the separator is only hidden
  const ln = (k: string, v: string, attrs = "", sep = ": ") => `<div class="ln"${attrs}><span class="k">${esc(k)}<span class="hide">${esc(sep)}</span></span><span class="v">${v}</span></div>`;
  const looked = (p.fields["lookedAt"] ?? "").split(/\s+/).filter(Boolean);
  const allOpened = looked.length > 0 && p.opened?.length && looked.every((r) => p.opened?.includes(r));
  const fields = Object.entries(p.fields)
    .map(([k, v]) => ln(k, k === "lookedAt" && allOpened ? `${looked.length} references: the evidence below<span class="hide"> ${esc(v)}</span>` : `<span class="mono">${esc(v)}</span>`, ` data-field="${esc(k)}"`, " = "))
    .join("");
  const who = signer.verified ? `${signer.name} (${signer.tier})` : `${signer.hint} — unverified: ${signer.why}`;
  const about = /^(?:https:\/\/kton\.dev\/o\/|sha256:)([0-9a-f]{64})$/.exec(p.subject);
  const opened = p.opened?.length
    ? `<section class="blk"><h2>Evidence you opened in this view <span class="badge" data-opened-count="${p.opened.length}"><span class="hide">(</span>${p.opened.length}<span class="hide">)</span></span></h2><ul class="ev">${p.opened
        .map((r) => {
          const l = refLabel(r);
          const inLooked = looked.includes(r);
          return `<li><div><div class="t" title="${esc(r)}" tabindex="0">${esc(l.title)}${l.sub ? ` <span class="muted">· ${esc(l.sub)}</span>` : ""}<span class="hide"> ${esc(r)}</span></div>${l.loc ? `<span class="loc">${esc(l.loc)}</span>` : ""}</div><span class="pill ${inLooked ? "ok" : "warn"}"${inLooked ? " data-in-lookedat" : ""}><span class="hide">(</span>${inLooked ? "in lookedAt" : "not in lookedAt: you took it out"}<span class="hide">)</span></span></li>`;
        })
        .join("")}</ul></section>`
    : "";
  return `<p class="runbar"><b>Test keys</b><span>${esc(p.footer)}</span></p>
<div class="dlg" data-testid="sign-detail">
<div class="eyebrow">${esc(title)}</div>
<h1>${esc(p.control.title ?? "Sign")}</h1>
<section class="blk"><h2>You sign</h2>
${p.meaning !== undefined ? (p.meaningField ? ln("Meaning", `<b>${esc(p.meaning)}</b> <span class="muted">(signed as ${esc(p.meaningField)} = ${esc(p.meaning)})</span>`, " data-act-meaning") : ln("Act", `<b>${esc(p.meaning)}</b> <span class="muted">(the control's meaning, not a field of the claim)</span>`, " data-act-meaning")) : ""}
${p.scope !== undefined ? ln("Into scope", `<span class="mono">${esc(p.scope)}</span>`, " data-into-scope") : ""}</section>
<section class="blk"><h2>About</h2>${ln("About", about ? `<span class="hide">${esc(p.subject)} </span><span class="chip" title="${esc(p.subject)}" tabindex="0">claim <span class="mono">${esc((about[1] ?? "").slice(0, 8))}</span></span>` : `<span class="mono">${esc(p.subject)}</span>`)}</section>
<section class="blk"><h2>Fields</h2>${fields || '<div class="muted">(none)</div>'}</section>
${opened}
<section class="blk"><h2>Signs as</h2>${ln("Signs with", `<b>${esc(p.as)}</b> <span class="muted">${esc(p.keyRole)} key "${esc(p.as)}"</span> <span class="hide">· ${esc(who)}</span><span class="pill ${signer.verified ? "ok" : "warn"}">${signer.verified ? esc(who) : "unverified"}</span>`, " data-signer")}${signer.verified ? "" : `<div class="muted">${esc(signer.why)}</div>`}</section>
<section class="blk"><h2>Record</h2>
${ln("Template", `${esc(p.template)} <span class="muted">(control ${esc(p.control.package)}/${esc(p.control.name)})</span>`)}
${p.control.title ? ln("Control", esc(p.control.title)) : ""}
${p.predicate !== undefined ? ln("Says", `<span class="mono">${esc(p.predicate)}</span>`) : ""}
${p.expect.template ? ln("Template as listed", `<span class="mono" title="${esc(p.expect.template)}">${esc(p.expect.template.slice(0, 19))}…</span>${p.expect.revision ? `<span class="hide"> · package revision ${esc(p.expect.revision.slice(0, 19))}…</span>` : ""}`) : ""}
${p.expect.revision ? ln("Package revision", `<span class="mono" title="${esc(p.expect.revision)}">${esc(p.expect.revision.slice(0, 19))}…</span>`) : ""}
<div class="info">${esc(p.rules)}</div></section>
</div>`;
}

/**
 * The sign dialog for a person-key signature (C1.4, O1: "I know I signed this now").
 * Sign is not the default and stays disabled until the person ticks the confirmation. Enter never
 * activates anything here; Escape and Cancel close without signing. The page posts
 * {type: "sign", confirmed: true} only from a click on the enabled Sign button; then it shows
 * "Signing…" and, when the shell reports it, what became of the signature (ok or failed).
 * With `view` (the plan and the shown signer) the dialog is structured; without it, the text detail.
 * Cancel takes the focus when the page opens (a script, not autofocus); Sign is the primary, Cancel secondary.
 */
export function signDialogPage(title: string, detail: string, person: string, nonce: string, view?: { readonly plan: SignPlan; readonly signer: ShownSigner }): string {
  const top = view ? signDialogBody(view.plan, view.signer, title) : `<h1>${esc(title)}</h1>
<pre class="box" data-testid="sign-detail">${esc(detail)}</pre>`;
  const body = `${top}
<div class="${view ? "foot dlg" : ""}">
<label class="confirm"><input type="checkbox" id="iknow" data-testid="sign-confirm"> I know I sign this now as ${esc(person)}</label>
<div class="actions"><button id="sign" type="button" data-testid="sign-button" disabled>Sign</button><button id="cancel" type="button" data-testid="sign-cancel">Cancel</button></div>
<div class="muted">Enter does not sign. Escape or Cancel closes without signing.</div>
<div class="outcome" id="outcome" data-testid="sign-outcome" role="status"></div></div>`;
  const script = `const vscode=acquireVsCodeApi();const box=document.getElementById('iknow');const signBtn=document.getElementById('sign');const cancelBtn=document.getElementById('cancel');const out=document.getElementById('outcome');
box.checked=false;signBtn.disabled=true;cancelBtn.focus();
box.addEventListener('change',()=>{signBtn.disabled=!box.checked;});
document.addEventListener('keydown',(e)=>{if(e.key==='Enter'){e.preventDefault();e.stopPropagation();}if(e.key==='Escape'){e.preventDefault();vscode.postMessage({type:'cancel'});}},true);
signBtn.addEventListener('click',()=>{if(box.checked&&!signBtn.disabled){signBtn.disabled=true;box.disabled=true;cancelBtn.disabled=true;out.textContent='Signing…';out.dataset.state='signing';vscode.postMessage({type:'sign',confirmed:true});}});
cancelBtn.addEventListener('click',()=>vscode.postMessage({type:'cancel'}));
window.addEventListener('message',(e)=>{const m=e.data;if(m&&m.type==='outcome'){out.textContent=String(m.text);out.dataset.state=m.ok?'signed':'failed';}});`;
  return page(title, body, nonce, script, view ? DIALOG_STYLE : "");
}

/**
 * A run artifact's image, read-only: the bytes (already checked against the claim's sha256) inlined as a data URI. The
 * page's CSP allows images from data: only; no script.
 */
export function artifactImagePage(title: string, info: string, mediaType: string, base64: string, nonce: string): string {
  const type = /^image\/(png|jpeg|gif|webp)$/.test(mediaType) ? mediaType : "image/png";
  const body = `<h1>${esc(title)}</h1><div class="muted" data-testid="artifact-info">${esc(info)}</div>
<img data-testid="artifact-image" alt="${esc(title)}" style="max-width:100%;border:1px solid var(--vscode-panel-border)" src="data:${type};base64,${base64}">`;
  return page(title, body, nonce).replace("img-src 'none'", "img-src data:");
}
