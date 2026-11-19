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
  /** where the sandboxed Mermaid frame is served (H13: 127.0.0.1 through asExternalUri); none: no diagrams */
  readonly mermaidFrame?: string;
}

export function compositionCsp(nonce: string, cspSource: string, mermaidOrigin?: string): string {
  return [
    "default-src 'none'",
    `script-src 'nonce-${nonce}' ${cspSource} 'wasm-unsafe-eval'`,
    `style-src ${cspSource}`,
    `img-src ${cspSource} data: blob:`,
    `frame-src blob:${mermaidOrigin ? ` ${mermaidOrigin}` : ""}`,
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
<meta http-equiv="Content-Security-Policy" content="${compositionCsp(nonce, uris.cspSource, uris.mermaidFrame ? new URL(uris.mermaidFrame).origin : undefined)}">${uris.mermaidFrame ? `\n<meta name="kton-mermaid-frame" content="${esc(uris.mermaidFrame)}">` : ""}
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<link rel="stylesheet" href="${esc(uris.css)}">
</head><body><div id="kton-root" data-settled="false">Waiting for the host…</div>
<script type="module" nonce="${nonce}" src="${esc(uris.frame)}"></script></body></html>`;
}

/**
 * A leaf's CSP (a page viewer in its own frame: graph, render, feed). As the composition frame's, plus what a
 * page needs: its own <style> blocks by nonce, img-src/frame-src blob: for checked bytes (kton-web point d), and
 * connect-src limited to the webview's own resource origin, because the kernel wasm and its manifest are fetched
 * from the vendored kit (runtime/wasm/). No network: nothing else is reachable.
 */
export function leafCsp(nonce: string, cspSource: string): string {
  return [
    "default-src 'none'",
    `script-src 'nonce-${nonce}' ${cspSource} 'wasm-unsafe-eval'`,
    `style-src 'nonce-${nonce}' ${cspSource}`,
    `img-src ${cspSource} data: blob:`,
    "frame-src blob:",
    "font-src 'none'",
    `connect-src ${cspSource}`,
    "form-action 'none'",
    `base-uri ${cspSource}`,
  ].join("; ");
}

/**
 * The viewer's own page (its index.html, vendored and tree-checked), with a head the host owns (C1.5): the
 * page's CSP metas are dropped, the host's is set, <base> points at the viewer's folder, and every <script> and
 * <style> gets the nonce. Inline event handlers are not made to work: the CSP refuses them.
 */
export function leafPage(html: string, nonce: string, cspSource: string, base: string): string {
  const head = `<meta http-equiv="Content-Security-Policy" content="${leafCsp(nonce, cspSource)}">\n<base href="${esc(base)}">\n`;
  return html
    .replace(/<meta[^>]+http-equiv=["']?Content-Security-Policy["']?[^>]*>/gi, "")
    .replace(/<base\b[^>]*>/gi, "")
    .replace(/<(script|style)\b(?![^>]*\bnonce=)/gi, `<$1 nonce="${nonce}"`)
    .replace(/<head([^>]*)>/i, (_m, a: string) => `<head${a}>\n${head}`);
}
