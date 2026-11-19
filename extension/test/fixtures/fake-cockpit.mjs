#!/usr/bin/env node
// A stand-in for the cockpit executable. It appends its argv as one JSON line to FAKE_COCKPIT_LOG
// and answers from FAKE_COCKPIT_RESPONSES: a JSON list of
//   {"argv0": "say", "argv1"?: "...", "exit": 0, "stdout": "...", "stderr": "..."}
// The first entry whose argv0 (and argv1, if given) match is used. No match: exit 1.
import { appendFileSync, readFileSync } from "node:fs";

const argv = process.argv.slice(2);
if (process.env.FAKE_COCKPIT_LOG) appendFileSync(process.env.FAKE_COCKPIT_LOG, JSON.stringify(argv) + "\n");
const responses = JSON.parse(readFileSync(process.env.FAKE_COCKPIT_RESPONSES, "utf8"));
const hit = responses.find((r) => r.argv0 === argv[0] && (r.argv1 === undefined || r.argv1 === argv[1]));
if (!hit) {
  process.stderr.write(`fake cockpit: no response for ${argv[0]}\n`);
  process.exit(1);
}
if (hit.stdout) process.stdout.write(hit.stdout);
if (hit.stderr) process.stderr.write(hit.stderr);
process.exit(hit.exit ?? 0);
