// The kit a composition hands every viewer module as ctx.kit (core/compose.js), so a viewer module imports
// nothing and its pinned tree (viewer.json + module) covers all of its code. Plain ES module.
//   esc, markdown, termText, sha256Hex(ArrayBuffer), mermaid(source) → {svg, …} (sandboxed frame, mermaid.js)

import { termText } from "./ktonpkg-mirror/sparql.js";
import { renderMermaid } from "./mermaid-client.js";

export function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
}

function inline(s) {
  return esc(s)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[^*])\*([^*\s][^*]*)\*/g, "$1<em>$2</em>");
}

/** Minimal Markdown → HTML: everything escaped first; paragraphs, # headings, - / 1. lists, **bold**, *italic*, `code`. */
export function markdown(text) {
  const out = [];
  let list = null;
  let para = [];
  const flushPara = () => {
    if (para.length) out.push(`<p>${inline(para.join(" "))}</p>`);
    para = [];
  };
  const closeList = () => {
    if (list) out.push(`</${list}>`);
    list = null;
  };
  for (const line of String(text).split("\n")) {
    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    const li = /^\s*[-*]\s+(.*)$/.exec(line);
    const oli = /^\s*\d+\.\s+(.*)$/.exec(line);
    if (!line.trim()) {
      flushPara();
      closeList();
    } else if (h) {
      flushPara();
      closeList();
      const n = Math.min(h[1].length + 3, 6);
      out.push(`<h${n}>${inline(h[2])}</h${n}>`);
    } else if (li || oli) {
      flushPara();
      const kind = li ? "ul" : "ol";
      if (list !== kind) {
        closeList();
        out.push(`<${kind}>`);
        list = kind;
      }
      out.push(`<li>${inline((li ?? oli)[1])}</li>`);
    } else {
      closeList();
      para.push(line.trim());
    }
  }
  flushPara();
  closeList();
  return out.join("\n");
}

export async function sha256Hex(buf) {
  return [...new Uint8Array(await crypto.subtle.digest("SHA-256", buf))].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export const kit = { esc, markdown, termText, sha256Hex, mermaid: renderMermaid };
