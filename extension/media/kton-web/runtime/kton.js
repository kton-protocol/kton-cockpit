// The kit's one door to the protocol. A viewer says which PROFILE it needs and gets back exactly
// the functions that profile declares - nothing else.
//
// A profile is a SIZE knob, not a security boundary. Every export computes locally; nothing here
// can write anywhere without the page doing its own POST. Withholding plktBuildClaim from a viewer
// protects nothing - it only keeps the viewer honest about how much protocol it touches, and turns
// a stray call into "not in profile" instead of `undefined is not a function`.
//
// The default is "full" because 1 MB gzip is affordable and one binary is one thing to reason
// about. Reach for a smaller profile when a deployment actually needs the bytes.

const cache = new Map(); // profile -> Promise<api>

// Node has no document and no fetch-from-disk; a browser has no fs. The door is the same either
// way, because the second place that instantiates a profile is the place the two drift apart -
// tools/fixtures.mjs used to carry its own copy of these fifteen lines, which is how a check ends
// up testing something the viewers do not run. It matters more now than it did: a verticle's MCP
// side is a Node module, and it must reach the kernel through the same loader the UI uses.
const inNode = typeof process !== "undefined" && process.versions && process.versions.node
    && typeof document === "undefined";

async function readLocal(url) {
    const { readFileSync } = await import("node:fs");
    const { fileURLToPath } = await import("node:url");
    return readFileSync(url.protocol === "file:" ? fileURLToPath(url) : url.pathname);
}

/** Load a wasm profile.
 *  `base` is the directory holding wasm_exec.js and wasm/, resolved against THIS file - so a viewer
 *  three directories away still just calls loadKton() and does not carry a path it could get wrong.
 *  Works in a browser and in Node; the MCP side of a verticle is Node and needs the same door. */
export function loadKton({ profile = "full", base } = {}) {
  const root = new URL(base ?? "./", import.meta.url);
  const key = profile + "@" + root.href;
  if (!cache.has(key)) cache.set(key, instantiate(profile, root));
  return cache.get(key);
}

async function instantiate(profile, root) {
  const manifest = inNode
    ? JSON.parse(String(await readLocal(new URL("wasm/manifest.json", root))))
    : await (await fetch(new URL("wasm/manifest.json", root))).json();
  const entry = manifest[profile];
  if (!entry) {
    throw new Error(
      `kton: no wasm profile "${profile}". Built profiles: ${Object.keys(manifest).join(", ")}. ` +
      `Add one in wasm/profiles.json and run wasm/build.sh.`
    );
  }

  await loadGoRuntime(new URL("wasm_exec.js", root));
  // a fresh instantiation must not inherit the previous profile's symbols, or a check can pass on
  // something the loader never actually registered
  for (const k of Object.keys(globalThis).filter((x) => x.startsWith("plkt"))) delete globalThis[k];
  const go = new globalThis.Go();
  let bytes;
  if (inNode) bytes = await readLocal(new URL("wasm/" + entry.file, root));
  else {
    const res = await fetch(new URL("wasm/" + entry.file, root));
    if (!res.ok) throw new Error(`kton: cannot fetch ${entry.file} (HTTP ${res.status})`);
    bytes = await res.arrayBuffer();
  }
  const mod = await WebAssembly.instantiate(bytes, go.importObject);
  go.run(mod.instance); // returns at the first select{}; the exports are live from here on

  return buildApi(profile, entry.exports);
}

// wasm_exec.js is a classic script that sets globalThis.Go - it is not an ES module, so it gets a
// <script> tag rather than an import. Loaded once, whatever how many profiles ask for it.
let goRuntime = null;
async function loadGoRuntime(url) {
  if (globalThis.Go) return;
  if (goRuntime) return goRuntime;
  if (inNode) {
    // Go ships wasm_exec.js with the toolchain; the copy in runtime/ is the same file, and using
    // the local one keeps a Node run on exactly the bytes the browser gets.
    const src = String(await readLocal(url));
    goRuntime = Promise.resolve(new Function(src).call(globalThis));
    return goRuntime;
  }
  goRuntime = new Promise((ok, fail) => {
    const s = document.createElement("script");
    s.src = url.href;
    s.onload = () => ok();
    s.onerror = () => fail(new Error(`kton: cannot load ${url.href}`));
    document.head.appendChild(s);
  });
  return goRuntime;
}

function buildApi(profile, exports) {
  const has = (name) => exports.includes(name);
  const denied = (name, group) => () => {
    throw new Error(
      `kton: ${name}() is not in profile "${profile}". Use a profile that includes the ` +
      `"${group}" group (see wasm/profiles.json), or ask for a smaller one on purpose.`
    );
  };

  const api = { profile, exports: [...exports] };

  api.buildGraph = has("plktBuildGraph")
    ? (union, keys = {}, names = {}) => {
        const str = (v) => (typeof v === "string" ? v : JSON.stringify(v ?? {}));
        const out = globalThis.plktBuildGraph(str(union), str(keys), str(names));
        const g = typeof out === "string" ? JSON.parse(out) : out;
        if (g && g.error) throw new Error("kton: buildGraph: " + g.error);
        return g;
      }
    : denied("buildGraph", "graph");

  // Hash a fetch stream without holding the bytes: WebCrypto cannot stream, this can, at any size.
  api.sha256Stream = has("plktSha256Init")
    ? async (source) => {
        const body = source instanceof Response ? source.body : source;
        globalThis.plktSha256Init();
        if (!body) throw new Error("kton: sha256Stream needs a ReadableStream or Response");
        const reader = body.getReader();
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          globalThis.plktSha256Update(value);
        }
        return globalThis.plktSha256Final();
      }
    : denied("sha256Stream", "verify");

  const wrap = (name, group, fn) => (has(name) ? fn : denied(name.replace(/^plkt/, "").replace(/^./, (c) => c.toLowerCase()), group));
  const unwrap = (out, what) => {
    if (out && out.error) throw new Error(`kton: ${what}: ${out.error}`);
    return out;
  };

  api.buildClaim = wrap("plktBuildClaim", "claim", (spec) =>
    unwrap(globalThis.plktBuildClaim(typeof spec === "string" ? spec : JSON.stringify(spec)), "buildClaim"));
  api.sealClaim = wrap("plktSealClaim", "claim", (payloadB64, sigB64, pubHex) =>
    unwrap(globalThis.plktSealClaim(payloadB64, sigB64, pubHex), "sealClaim"));
  api.keyIRI = wrap("plktKeyIRI", "claim", (pubHex) =>
    unwrap(globalThis.plktKeyIRI(pubHex), "keyIRI").iri);

  return api;
}
