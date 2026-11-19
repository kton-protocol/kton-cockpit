// The composition frame's page in a VS Code webview: kton-web's viewers/compose/ (frame.js, vendored) with
// a head the host builds and a CSP the host owns (C1.5 v3). The tenant page's own CSP meta is not used.
//
// - scripts: the nonce'd entry (frame.js) and the module imports it makes, which come from the webview's own
//   resource origin (the vendored kit and the viewer trees this host wrote after checking them); plus
//   'wasm-unsafe-eval' (Oxigraph, Go wasm profiles).
// - no network: connect-src 'none'; the bridge (acquireVsCodeApi) is the only way out.
// - img-src data: and blob: (diagram SVG as an <img>, product and content bytes); frame-src blob: (a product
//   PDF). styles only from the resource origin, never 'unsafe-inline' (the Mermaid frame, the one exception,
//   is not in this kton-web pin).

export interface PageUris {
  /** webview.cspSource */
  readonly cspSource: string;
  /** the vendored viewers/compose/frame.js */
  readonly frame: string;
  /** the vendored core/compose.css */
  readonly css: string;
}

export function compositionCsp(nonce: string, cspSource: string): string {
  return [
    "default-src 'none'",
    `script-src 'nonce-${nonce}' ${cspSource} 'wasm-unsafe-eval'`,
    `style-src ${cspSource}`,
    `img-src ${cspSource} data: blob:`,
    "frame-src blob:",
    "font-src 'none'",
    "connect-src 'none'",
    "form-action 'none'",
    "base-uri 'none'",
  ].join("; ");
}

const esc = (s: string) => s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] as string);

export function compositionPage(title: string, nonce: string, uris: PageUris): string {
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${compositionCsp(nonce, uris.cspSource)}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<link rel="stylesheet" href="${esc(uris.css)}">
</head><body><div id="kton-root" data-settled="false">Waiting for the host…</div>
<script type="module" nonce="${nonce}" src="${esc(uris.frame)}"></script></body></html>`;
}
