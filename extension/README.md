# kton workbench (VS Code extension)

One VS Code extension for working in a kton registry: an inbox of requests and proposals, a
decision screen, and a sign dialog. It reads with `cockpit ask` and writes with `cockpit say`. It
holds no kton logic of its own: every answer it shows comes from the cockpit, and every claim it
makes goes through the cockpit's own guards.

This is the shell (contract C1). Further surfaces arrive later as kton packages, not as more
extensions. The recorder (`kton.kton-recorder`) stays a separate extension: the workbench hands over to it
and checks that it is installed. The workgroup extension (`kton.kton-workgroup`) folds into the
workbench later; until then its lanes show "Open in terminal" as not available.

## What it does

- **Home**: decisions waiting, with their options; the inbox count; open items by area; lanes.
- **Inbox → thread → answer**: a request opens with its text and claim id. The answer (`done`)
  goes through the sign dialog.
- **Decide**: options, consequences and the recommendation, then the sign dialog. The rules shown
  on the screen are information only ("not enforced"): the cockpit's guard decides.
- **Sign dialog**: the one way to sign with the person key. It shows the template, the subject, the
  fields and the key. Sign stays disabled until "I know I sign this now" is ticked, and Enter never
  signs.
- **Compose / say / ask** from the command palette. Templates and their fields come from the
  cockpit's listing (`cockpit say --json`), never from files in the checkout.
- **Surfaces**: a status-bar switch filters lanes and areas in the same window. A Claude session
  for a surface opens in its own window ("open a surface session in a new window").

### Two keys per window, no fallback

A window has a **person key** (setting `kton.identity.personKey`) and an **agent key**
(`kton.identity.agentKey`):

- **Person key**: decisions and answers, always through the sign dialog.
- **Agent key**: records, without a dialog.
- **No fallback**: neither key ever stands in for the other. If one is missing, the status bar
  says so and the actions that need it are disabled.
- **Identity**: one per window. Open a second VS Code window to work as someone else.
- **Decisions**: a `decides` counts as a decision only when a person signed it. A person here is
  this window's person key, or a signer in one of the tiers named in `kton.identity.personTiers`.
  A `decides` signed by an agent key is shown as a record.

### Identity is "unverified"

The status bar shows the configured key names marked **unverified**. The workbench does not
claim to know who holds a key until the cockpit has a verb that answers "who am I" for a window.
It never takes a name from a claim's `by`. Without `.kton/local/identity.json` the window reads
but cannot sign.

## Transport: the CLI, until `cockpit serve --stdio`

The workbench runs the `cockpit` binary (setting `kton.cockpit.path`) with an argument vector,
never through a shell:

- **Arguments**: no flag value or positional argument it passes may start with `-`.
- **Answers**: every answer is shape-checked before use; a refusal (exit 2) is shown with its
  reason.
- **Timeouts**: a call that runs out of time (`kton.cockpit.timeoutSeconds`) is reported as "not
  signed". The registry is the authority on whether a claim was made.
- **Polling**: the inbox is polled every `kton.poll.seconds`. Polls move to cockpit-runtime's jobs
  module once ADR-017 is built.
- **Later**: when `cockpit serve --stdio` (ADR-011 P5, planned for cockpit v0.6.0) exists, the
  transport switches to it. The `Transport` interface in `src/transport/port.ts` is the seam.

The queries the views run are SPARQL files in a folder: `kton.queries.dir`, default
`<workspace>/.kton/queries`. It needs `proposals.rq`, `decides.rq`, `messages.rq`, `answers.rq`
and `lanes.rq`. `test/e2e/vocabulary/queries/` has a neutral example set.

No network calls, no telemetry. Every webview has a host-owned Content Security Policy:

- `default-src 'none'`;
- styles and scripts by nonce only;
- `connect-src 'none'`;
- no inline style attributes.

A kton signature is not yet a QMS e-signature: the extension runs in **TEST MODE**, and every
signature it makes is test-only.

## Build and test

Node 22 (`.nvmrc`). From `extension/`:

```console
$ npm ci
$ npm run typecheck
$ npm test              # unit tests (node --test), nothing skips
$ npm run package       # out/kton-workbench.vsix
```

CI (`.github/workflows/ci.yml`, job `extension`) runs these when `extension/**` changed and
uploads the VSIX. There is no linter configured, so CI checks types only.

`npm run test:integration` runs the transport against a real `cockpit` binary and registry. It
needs `COCKPIT_IT_BIN` and `COCKPIT_IT_REPO`, and fails without them: it is not part of `npm test`.

### The e2e (local, opt-in)

`test/e2e/run.sh` runs the PR gate flows in a real editor:

- **Flows**: S1–S4, S7, S9 and S10, numbered as in the shell flows.
- **Setup**: it seeds a throwaway registry with test identities (`qa-decider`, its agent key
  `qa-decider-agent`, `qa-requestor`), starts code-server with the packaged VSIX, and drives it
  with Playwright.
- **Checks**: after every signature it checks the registry from outside the editor.

```console
$ npm run package
$ test/e2e/run.sh /tmp/wb-e2e-$$ /path/to/cockpit      # a fresh, absolute work dir
```

- **Pinning**: images are pinned by digest: node 22, code-server 4.96.4, Playwright 1.49.1, and a
  static docker CLI from `docker:27-cli`. Playwright comes from `test/e2e/package-lock.json`
  through `npm ci`.
- **Output** in `test/e2e/out/`: screenshots, `smoke-results.json`, and `run.json` (a
  `kton-testrun/1` record; `testrun.schema.json`, checked by `npm test`). Only PNG and JSON are
  allowed there, and the runner fails if anything else appears.
- **Cleanup**: the work dir, with its test keys, is removed at the end (`KEEP=1` keeps it).

**Run it only on a dedicated, throwaway machine or runner.** The cockpit starts its SPARQL engine
through docker, so the code-server and sidecar containers get the host's docker socket. Access to
the docker socket is root on the host. For that reason the e2e is not a CI job here.

## The gate for a PR

Before a PR to `extension/` is merged:

- the unit tests pass (`npm test`, no skips);
- the VSIX builds;
- the e2e passes locally for the flows the PR touches.

The PR description lists its run.

## License

Apache-2.0, see [LICENSE](LICENSE).
