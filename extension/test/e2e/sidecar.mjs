// The registry side of the e2e: runs `cockpit` for the smoke test while the window is open (seeding a
// claim mid-run for S1.6, a claim by a key outside the trust config for S1.3, and the registry checks
// after every signature). Runs in its own container with the cockpit binary; the smoke test asks by
// writing $WORK/rpc/<id>.req.json = {argv: [...], stdin?: "..."} and reads <id>.res.json =
// {code, stdout, stderr}. argv goes to execFile: never through a shell. {stop: true} ends it.
import { execFile } from "node:child_process";
import { readdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const WORK = process.argv[2];
const RPC = join(WORK, "rpc");
const env = { ...process.env, HOME: join(WORK, "home"), TMPDIR: join(WORK, "tmp") };
let stop = false;

// the registry to run in: the lanes one (repo), the S6 one (repo-s6: the QM areas' packages bring field
// aliases that would rename the lanes vocabulary's fields, since the view reads one alias file per registry), or
// the role views' one (repo-roles: a copy of test-execution's dryrun1.runs with a stand-in dryrun1.triage and a
// copy of verification's dryrun1-coverage federated)
function run({ argv, stdin, repo }) {
  const dir = ["repo-s6", "repo-dm", "repo-dm2", "repo-roles"].includes(repo) ? repo : "repo";
  // repo-dm federates test-execution's runs: the first ask after a say rebuilds the view with the federation, which
  // took over 2 minutes on ai1 (the old 120 s timeout killed it: exit 1, empty stderr)
  const timeout = dir.startsWith("repo-dm") ? 600_000 : 120_000;
  return new Promise((resolve) => {
    const child = execFile("cockpit", argv, { cwd: join(WORK, dir), env, shell: false, timeout, maxBuffer: 32 * 1024 * 1024 }, (err, stdout, stderr) => {
      resolve({ code: err ? (typeof err.code === "number" ? err.code : 1) : 0, stdout: String(stdout), stderr: err && err.killed ? `killed after ${timeout / 1000} s (${err.signal})` : String(stderr) });
    });
    if (stdin !== undefined) child.stdin.end(stdin);
  });
}

while (!stop) {
  for (const f of readdirSync(RPC).filter((x) => x.endsWith(".req.json")).sort()) {
    const id = f.slice(0, -".req.json".length);
    const req = JSON.parse(readFileSync(join(RPC, f), "utf8"));
    renameSync(join(RPC, f), join(RPC, `${id}.taken.json`));
    if (req.stop) {
      stop = true;
      break;
    }
    const res = Array.isArray(req.argv) && req.argv.every((a) => typeof a === "string") ? await run(req) : { code: 1, stdout: "", stderr: "argv must be a list of strings" };
    writeFileSync(join(RPC, `${id}.tmp`), JSON.stringify(res));
    renameSync(join(RPC, `${id}.tmp`), join(RPC, `${id}.res.json`));
  }
  await new Promise((r) => setTimeout(r, 200));
}
