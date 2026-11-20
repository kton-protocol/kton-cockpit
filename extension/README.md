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
  this window's person key, recognised by its **key id**, or a signer in one of the tiers named in
  `kton.identity.personTiers`. A `decides` signed by any other key is shown as a record.
  - **Key id**: derived from the public key the window is configured with,
    `<keyring>/<personKey>-claims.pub` (keyring from `.kton/local/identity.json`). When
    `cockpit doctor --json` answers for the same key, its `nektonKeyId` must agree.
  - **Labels**: a signer's label (a key file name, unique only within a tier) is shown, never
    compared.

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
- **A busy registry**: a `say` answered with `pending: {code: store.busy}` is signed and stored, and the next
  write commits it. The workbench shows it as signed, not yet committed, and never signs it again. Only a
  **refusal** `store.busy` is retried, because it means nothing was signed. Any other refusal or error is never
  retried. **This needs cockpit v0.5.2 or later**: before v0.5.2, a refusal (`store`, or `store.busy` on the
  untagged main d96539f) could come after the claim was signed, so do not point the workbench at an older cockpit
  for signing.
- **Polling**: the inbox is polled every `kton.poll.seconds`. Polls move to cockpit-runtime's jobs
  module once ADR-017 is built.
- **Transport**: the command line is the default. `kton.cockpit.transport: stdio` opts in to one
  `cockpit serve --stdio` per window (ADR-011 P5, SPEC §12.5, cockpit v0.5.6 or later). The `Transport`
  interface in `src/transport/port.ts` is the seam.

The queries the views run come from two places:

- **By name from an admitted package**: `decides` and `answers`, which decide what is a decision
  and what is closed. The shell asks `ask {query: "sparql", name}`; the package is installed with
  `cockpit install <package> --allow` (`claims.allowedPackages`). A `decides.rq` or `answers.rq`
  in the checkout is never used. `decides` must return the signer IRI `?s` and its tier `?byTier`.
- **From a folder** (display only, until `ui list`): `proposals.rq`, `messages.rq` and
  `lanes.rq` in `kton.queries.dir`, default `<workspace>/.kton/queries`.

`test/e2e/vocabulary/` has a neutral example of both: `queries/` and `package-queries/`. The
latter is built into `test/e2e/vocabulary-package/` by `test/e2e/build-vocabulary-package.sh`.

No network calls, no telemetry. Every webview has a host-owned Content Security Policy:

- `default-src 'none'`;
- styles and scripts by nonce only;
- `connect-src 'none'`;
- no inline style attributes.

A kton signature is not yet a QMS e-signature: the extension runs in **TEST MODE**, and every
signature it makes is test-only.

## Packages and compositions

The admitted packages' ui declarations (C2) come from `cockpit ui list --json`:

- **Loading.** A package's bytes are read by content hash from its installed folder and checked again at
  load. A package that fails any check renders nothing and is listed under **Packages** on Home with
  its reason; so is one that is not admitted. Only a package with `allowed: true` and no refusal
  contributes slots and compositions.
- **Slots.** A `review-view` or `record-view` entry is offered on the subjects its `when` names (today:
  under each open proposal on Home). `home-tile` and `status-item` show their query's one number. A
  slot name this shell does not know is listed, never rendered.
- **Compositions.** They are planned with the ktonpkg JS mirror and rendered by kton-web's
  `core/compose.js` in a panel. Both are vendored at a pinned kton-web commit in `media/kton-web/`
  (`PIN.json`; `scripts/vendor-kton-web.mjs --check`). The panel's CSP is the host's: scripts by
  nonce and from the webview origin, `connect-src 'none'`, no `'unsafe-inline'`.
- **What a block may ask.**
  - `query@1` runs the package's named query (`cockpit ask {query: sparql, name, bind}`), never
    query text.
  - `content@1` and `product@1` answer from package bytes by hash.
  - An act runs only as the plan declares it, through its control, whose kind decides the key: a
    decision goes through the sign dialog with the person key, a record is written with the agent
    key and no dialog.

- **Leaf viewers.** A page viewer that runs in its own frame (kton-web's graph, `frame: leaf`) is pinned by a
  package at a kton-web commit and must be the tree this shell vendors. It gets the state its slot binds and
  asks for its data: `graph@1` is answered with the registry's records, keys and names, which the viewer
  verifies in its own wasm. A leaf may fetch its pinned files (the wasm) from its own resource origin
  (`connect-src` the webview origin); a composition frame keeps `connect-src 'none'` (C1.5 v3).
- **Layout** (ktonpkg v0.9.5). The renderer places parts in grids, tabs and splits. The host tells it
  `host.narrow = 640`: a layout that declares `narrow` switches below 640 px of composition width (a full
  editor column is wider; a split editor or a narrow window is not); a layout without `narrow` never changes.
  The open tab and a split's ratio are view state: the host holds them like vars (never as acts) and gives
  them back when the webview reloads.
- **Diagrams.** Mermaid runs only in a sandboxed frame, served from 127.0.0.1 on a random port (exactly its
  three pinned files) and named in the composition frame's `frame-src`; the diagram is shown as an `<img>`.

## QM areas (C3)

An `activity` entry with an `area` object (ktonpkg v0.9.3) is a QM area. Home lists the areas under **QM areas**,
by `order`, then package name. The shell writes two compositions from the declaration, with its own blocks
(`media/workbench/`: `rows` and `notice`, tree-checked like any viewer), and plans and renders them as above:

- **The area** (subject: its records scope):
  - the governing SOP and its state, from the document register;
  - the records (the entry's list query; an empty scope reads "Planned, 0 records");
  - the findings of the named package, kept to the area's rule ids, or "not installed" when it is not;
  - the RACI; a row click picks the person the training query is filtered by (unfiltered until then: an optional
    param, ktonpkg v0.9.10);
  - training, documents (from the register, as links) and approvals.
- **A record**, opened from a list row: its fields (the record query, bound to the record's IRI), the area's actions,
  its approvals and its history. Its subject is the record's latest claim, as the record query answers it in the
  shell, never a claim a frame names.
- **Actions** are the area's controls. A decision goes through the sign dialog with the person key, a record is
  written with the agent key and no dialog; both go into the area's records scope, and the re-check before `say`
  applies. Fields the template requires and the act leaves open are asked by the shell (a pick for values).
- **Approvals** get a column `counts`: an approval counts only as `approved` signed by the window's person key or
  a key in a person tier; a rejected outcome or an agent key's approval does not.
- The register is the area's own package, else the admitted package that brings the queries `documents` and
  `document` (`$key`). There is no code for any one area: a test checks the source for the S6 packages' names.

Everything about packages comes from the cockpit: `cockpit ui list --json` (v0.5.4) for packages, admission,
refusals, ui entries, blocks, compositions (with their hash) and members (role, name, hash), and
`cockpit ui cat <package> <hash>` for a member's bytes, checked against the hash again. The shell reads no
package folder. `graph@1` reads
`cockpit show --json`: union, keys, names and the admitted packages' build keys (`packages`, passed on
unchanged; a label, never trust). Planned params go to the cockpit as typed terms (`bindTerms`); a query or
template name two packages bring is asked for as `<package>/<name>`.

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

- **Flows**: S1–S7, S9 and S10, numbered as in the shell flows. S6 (QM areas) runs in a second registry,
  `repo-s6`, opened as a second folder: its packages' field aliases would rename the lanes vocabulary's fields,
  since the view reads one alias file per registry.
- **Setup**: it seeds a throwaway registry with test identities (`qa-decider`, its agent key
  `qa-decider-agent`, `qa-requestor`), starts code-server with the packaged VSIX, and drives it
  with Playwright.
- **Checks**: after every signature it checks the registry from outside the editor.

```console
$ npm run package
$ test/e2e/build-cockpit.sh /tmp/wb-cockpit/cockpit       # this checkout's cockpit (ktonpkg v0.9.7)
$ test/e2e/run.sh /tmp/wb-e2e-$$ /tmp/wb-cockpit/cockpit   # a fresh, absolute work dir
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
