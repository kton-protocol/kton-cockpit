// The sandboxed Mermaid frame, served the H13 way (C1.5 v3): a VS Code webview cannot load a sandboxed frame from
// its resource URLs, so this serves exactly the frame's files from 127.0.0.1 on a random port (VS Code makes it
// reachable through asExternalUri). Only these three files, each checked against the vendored pin (PIN.json)
// before the server starts; anything else is 404; never data.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import { join } from "node:path";

export const MERMAID_FILES: Readonly<Record<string, string>> = {
  "/mermaid/frame.html": "text/html; charset=utf-8",
  "/mermaid/frame.js": "text/javascript; charset=utf-8",
  "/vendor/mermaid/mermaid.min.js": "text/javascript; charset=utf-8",
};

export interface MermaidServer {
  readonly port: number;
  /** the frame's URL on 127.0.0.1 (before asExternalUri) */
  readonly url: string;
  close(): void;
}

/** Start the server over the vendored kit; refuses to start when a file differs from its pin. */
export async function startMermaidServer(kit: string): Promise<MermaidServer> {
  const pin = JSON.parse(readFileSync(join(kit, "PIN.json"), "utf8")) as { files: Record<string, string> };
  const bytes = new Map<string, Buffer>();
  for (const path of Object.keys(MERMAID_FILES)) {
    const b = readFileSync(join(kit, path));
    const want = pin.files[path.slice(1)];
    const got = createHash("sha256").update(b).digest("hex");
    if (got !== want) throw new Error(`${path} differs from its pin (${got} != ${String(want)}): the Mermaid frame is not served`);
    bytes.set(path, b);
  }
  const server: Server = createServer((req, res) => {
    const path = new URL(req.url ?? "/", "http://127.0.0.1").pathname;
    const b = req.method === "GET" ? bytes.get(path) : undefined;
    if (!b) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { "content-type": MERMAID_FILES[path] as string, "cache-control": "no-store", "x-content-type-options": "nosniff" }).end(b);
  });
  const port = await new Promise<number>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const a = server.address();
      if (a && typeof a === "object") resolve(a.port);
      else reject(new Error("no port"));
    });
  });
  return { port, url: `http://127.0.0.1:${port}/mermaid/frame.html`, close: () => server.close() };
}
