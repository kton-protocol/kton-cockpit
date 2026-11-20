// The e2e's fault injector for `cockpit serve --stdio` (SPEC §12.5): the stdio twin of the cockpit-faults wrapper
// in run.sh. TEST ONLY. The window's kton.cockpit.path stays cockpit-faults; that wrapper hands `serve …` to this
// proxy, so the extension never learns of it.
//
//   node stdio-faults.mjs <work dir> serve --stdio
//
// It starts the real cockpit (`/usr/local/bin/cockpit serve --stdio`, or $COCKPIT_REAL), passes every frame both
// ways, and injects the cockpit-faults catalogue per method, from <work dir>/faults/mode, read at each request:
//   say-refuse   say       answered -32001 {code: template.not-allowed, clause, reason}; never forwarded
//   say-garbage  say       answered with a framed body that is not JSON; never forwarded
//   ask-garbage  ask       the same, for ask
//   say-slow     say       held for 120 s, then forwarded; a $/cancelRequest for it is answered -32800 here and the
//                          real cockpit never sees the say (so nothing is signed)
//   say-locked   say       -32001 store.busy while <work dir>/faults/registry.lock exists, forwarded once it is gone
//   say-pending  say       forwarded; its result gets pending {code: store.busy, …} (signed, not yet committed)
//   say-gone     say       the process goes: the real cockpit is killed and this proxy exits 137, unanswered
//   say-stdin-closed say   this proxy closes its stdin and stays, the say unanswered: the window's next write is
//                          EPIPE (kton-cockpit #196, S1)
// Always: ui/list's answer gets one unknown slot on hostile-good (S5.3 version skew), as the wrapper does for
// `ui list`. Every injection is logged to <work dir>/faults/stdio.log (one JSON per line).
import { spawn } from "node:child_process";
import { appendFileSync, closeSync, existsSync, readFileSync } from "node:fs";

const WORK = process.argv[2];
const args = process.argv.slice(3);
const REAL = process.env.COCKPIT_REAL ?? "/usr/local/bin/cockpit";
const mode = () => {
  try {
    return readFileSync(`${WORK}/faults/mode`, "utf8").trim() || "normal";
  } catch {
    return "normal";
  }
};
const log = (m) => {
  try {
    appendFileSync(`${WORK}/faults/stdio.log`, JSON.stringify({ t: new Date().toISOString(), ...m }) + "\n");
  } catch {
    /* the log is evidence only */
  }
};

const server = spawn(REAL, args, { stdio: ["pipe", "pipe", "inherit"] });
server.on("error", (e) => {
  process.stderr.write(`stdio-faults: cannot run ${REAL}: ${e.message}\n`);
  process.exit(127);
});
server.on("exit", (code, sig) => process.exit(code ?? (sig ? 128 + 9 : 1)));
server.stdin.on("error", () => {});

const frame = (body) => {
  const b = Buffer.isBuffer(body) ? body : Buffer.from(typeof body === "string" ? body : JSON.stringify(body), "utf8");
  return Buffer.concat([Buffer.from(`Content-Length: ${b.length}\r\n\r\n`, "ascii"), b]);
};
const toClient = (body) => process.stdout.write(frame(body));
const toServer = (raw) => server.stdin.write(frame(raw));
const answer = (id, m) => toClient({ jsonrpc: "2.0", id, ...m });
const refuse = (id, code, clause, reason) => answer(id, { error: { code: -32001, message: reason, data: { code, clause, reason } } });

/** Frames of one stream: calls fn(rawBody) for each Content-Length message. */
function reader(fn) {
  let buf = Buffer.alloc(0);
  return (d) => {
    buf = Buffer.concat([buf, d]);
    for (;;) {
      const end = buf.indexOf("\r\n\r\n");
      if (end < 0) return;
      const m = /Content-Length:\s*(\d+)/i.exec(buf.subarray(0, end).toString("ascii"));
      if (!m) throw new Error("stdio-faults: a header without Content-Length");
      const len = Number(m[1]);
      if (buf.length < end + 4 + len) return;
      const body = buf.subarray(end + 4, end + 4 + len);
      buf = buf.subarray(end + 4 + len);
      fn(body);
    }
  };
}

const key = (id) => (typeof id === "number" ? `n${id}` : `s${String(id)}`); // §12.5: 1 and "1" are different ids
const held = new Map(); // say-slow: id key -> {timer, raw}
const rewrite = new Map(); // id key -> (result) => result

// client -> server
process.stdin.on(
  "data",
  reader((raw) => {
    let msg;
    try {
      msg = JSON.parse(raw.toString("utf8"));
    } catch {
      return toServer(raw); // not ours to judge: the cockpit answers it
    }
    const { id, method } = msg;
    if (method === "$/cancelRequest") {
      const h = held.get(key(msg.params?.id));
      if (h) {
        clearTimeout(h.timer);
        held.delete(key(msg.params.id));
        log({ fault: "say-slow", cancelled: msg.params.id });
        return answer(msg.params.id, { error: { code: -32800, message: "request cancelled" } });
      }
      return toServer(raw);
    }
    if (id === undefined) return toServer(raw);
    if (method === "ui/list") rewrite.set(key(id), versionSkew);
    const m = mode();
    if (method === "ask" && m === "ask-garbage") {
      log({ fault: m, id });
      return toClient("this is not json");
    }
    if (method !== "say") return toServer(raw);
    switch (m) {
      case "say-refuse":
        log({ fault: m, id });
        return refuse(id, "template.not-allowed", "template", "the template decides is not allowed here");
      case "say-garbage":
        log({ fault: m, id });
        return toClient("this is not json");
      case "say-slow": {
        log({ fault: m, id, held: true });
        const timer = setTimeout(() => {
          held.delete(key(id));
          toServer(raw);
        }, 120_000);
        held.set(key(id), { timer, raw });
        return;
      }
      case "say-locked":
        if (existsSync(`${WORK}/faults/registry.lock`)) {
          log({ fault: m, id });
          return refuse(id, "store.busy", "store", "the registry is busy (git lock held); nothing was signed");
        }
        return toServer(raw);
      case "say-pending":
        log({ fault: m, id });
        rewrite.set(key(id), (r) => ({ pending: { code: "store.busy", reason: "the registry was busy; the next write commits it" }, ...r }));
        return toServer(raw);
      case "say-stdin-closed":
        log({ fault: m, id });
        process.stdin.destroy(); // the fd stays open until it is closed
        closeSync(0);
        return;
      case "say-gone":
        log({ fault: m, id });
        server.removeAllListeners("exit");
        server.kill("SIGKILL");
        return process.exit(137);
      default:
        return toServer(raw);
    }
  }),
);
process.stdin.on("end", () => server.stdin.end()); // the cockpit answers what runs, then ends; so do we

// server -> client
server.stdout.on(
  "data",
  reader((raw) => {
    let msg;
    try {
      msg = JSON.parse(raw.toString("utf8"));
    } catch {
      return toClient(raw);
    }
    const fn = msg.id !== undefined ? rewrite.get(key(msg.id)) : undefined;
    if (!fn) return toClient(raw);
    rewrite.delete(key(msg.id));
    if (msg.error || msg.result === undefined) return toClient(raw);
    return toClient({ ...msg, result: fn(msg.result) });
  }),
);

/** S5.3 version skew: a newer package names a slot this shell does not know (the cockpit-faults `ui list` edit). */
function versionSkew(l) {
  const p = l?.packages?.find?.((x) => x.name === "hostile-good" && Array.isArray(x.ui));
  if (p) p.ui.push({ file: "ui/sidebar.json", slot: "sidebar-ad", label: "Sidebar ad", when: { kind: "claim" } });
  return l;
}
