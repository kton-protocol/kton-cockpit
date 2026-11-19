// The composition frame's side of the sandboxed Mermaid frame (VERTICLE-HOSTS §13, H12).
// One hidden <iframe sandbox="allow-scripts"> (no allow-same-origin: an opaque origin) loads
// ../mermaid/frame.html, the only frame with style-src 'unsafe-inline'. It gets a MessagePort once; all
// requests and answers then go over that port, so no origin is trusted afterwards. The handoff itself has to
// use targetOrigin "*": an opaque-origin frame cannot be named (C1.5 v3: the one "*" the contract allows; it
// carries only the port, and the frame takes the first port and ignores every later window message).
//   renderMermaid(source) → {svg, width, height, labels, foreignObjects}   (mermaid/frame.js)
// A host names where its Mermaid frame lives with <meta name="kton-mermaid-frame" content="…"> (VS Code:
// 127.0.0.1 via asExternalUri, H13); otherwise it is ../mermaid/frame.html beside core/.

let port = null;
let ready = null;
let seq = 0;
const waiting = new Map();

function start() {
  if (ready) return ready;
  ready = new Promise((resolve, reject) => {
    const f = document.createElement("iframe");
    f.setAttribute("sandbox", "allow-scripts");
    f.setAttribute("aria-hidden", "true");
    f.setAttribute("title", "mermaid");
    // laid out but not seen: Mermaid measures text, so the frame cannot be display:none. Set through the
    // CSSOM, which a strict style-src allows (a style attribute would not be).
    Object.assign(f.style, { position: "absolute", left: "-10000px", top: "0", width: "1600px", height: "1200px", border: "0", visibility: "hidden" });
    // A host may name where its Mermaid frame lives (the VS Code host must: a webview's resource URLs cannot
    // be iframe targets); otherwise it sits beside the composition files.
    const named = document.querySelector('meta[name="kton-mermaid-frame"]')?.getAttribute("content");
    f.src = named || new URL("../mermaid/frame.html", import.meta.url).href;
    const timer = setTimeout(() => reject(new Error("the Mermaid frame did not load")), 30000);
    f.addEventListener("load", () => {
      const ch = new MessageChannel();
      port = ch.port1;
      port.onmessage = (e) => {
        if (e.data?.type === "ready") {
          clearTimeout(timer);
          return resolve();
        }
        const w = waiting.get(e.data?.id);
        if (!w) return;
        waiting.delete(e.data.id);
        e.data.error ? w.reject(new Error(e.data.error)) : w.resolve(e.data);
      };
      f.contentWindow.postMessage({ type: "kton:mermaid-port" }, "*", [ch.port2]);
    });
    document.body.append(f);
  });
  return ready;
}

export async function renderMermaid(source) {
  await start();
  return new Promise((resolve, reject) => {
    const id = ++seq;
    waiting.set(id, { resolve, reject });
    port.postMessage({ id, source });
  });
}
