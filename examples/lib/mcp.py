#!/usr/bin/env python3
"""One MCP call to the cockpit, from a shell script.

    mcp.py <tool> '<json arguments>' [--field NAME]

The cockpit exposes its three verbs over MCP and nowhere else, so the examples speak MCP rather
than reaching for the binaries underneath: what they demonstrate is what a session actually
gets, not an approximation assembled from plankton and git.

Exits non-zero when the cockpit refuses, printing its reason. That matters more than it looks —
every guard in the cockpit arrives as a refusal, so an example that carried on past one would
report success for something the cockpit declined.
"""
import json, os, subprocess, sys

REPO = os.environ.get("COCKPIT_REPO_DIR") or os.getcwd()


class Cockpit:
    def __init__(self, repo=REPO):
        env = dict(os.environ, COCKPIT_REPO_DIR=repo)
        self.p = subprocess.Popen([os.path.join(repo, "bin", "cockpit"), "mcp"],
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  text=True, env=env, bufsize=1)
        self.n = 0
        self.request("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                    "clientInfo": {"name": "cockpit-examples", "version": "1"}})
        self._send({"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}})

    def _send(self, msg):
        self.p.stdin.write(json.dumps(msg) + "\n")
        self.p.stdin.flush()

    def request(self, method, params):
        self.n += 1
        self._send({"jsonrpc": "2.0", "id": self.n, "method": method, "params": params})
        while True:
            line = self.p.stdout.readline()
            if not line:
                sys.exit("the cockpit closed its output before answering")
            msg = json.loads(line)
            if msg.get("id") == self.n:
                if "error" in msg:
                    sys.exit(f"{method} failed: {msg['error']}")
                return msg["result"]

    def call(self, tool, arguments):
        res = self.request("tools/call", {"name": tool, "arguments": arguments})
        if res.get("isError"):
            text = "".join(c.get("text", "") for c in res.get("content", []))
            print(text, file=sys.stderr)
            sys.exit(2)
        return res.get("structuredContent", {})


def main():
    args = sys.argv[1:]
    field = None
    if "--field" in args:
        i = args.index("--field")
        field = args[i + 1]
        args = args[:i] + args[i + 2:]
    if len(args) != 2:
        sys.exit("usage: mcp.py <tool> '<json arguments>' [--field NAME]")

    out = Cockpit().call("cockpit_" + args[0], json.loads(args[1]))
    if field:
        v = out.get(field)
        print("" if v is None else (v if isinstance(v, str) else json.dumps(v)))
    else:
        print(json.dumps(out, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
