# claude-science-cockpit — what the cockpit guarantees

### Version 0.1
### Status: draft

## Foreword

This is not a protocol specification. The protocol is [kton](https://github.com/kton-protocol/kton),
and its `spec/SPEC.md` governs fotons, claims, signatures, content addressing and federation. Every
clause here sits on top of that one and adds nothing to it.

What this document specifies is a **cockpit**: the surface one party — a Claude session — is given
onto that protocol, and what may be relied upon when a record arrives through it. A reader who wants
to know what a foton *is* should read the kton spec. A reader who wants to know what it means that
*this* cockpit produced one should read this.

## Introduction

A cockpit is a narrowing. The protocol permits a great deal; a session is given three verbs and
nothing else. So the interesting statements here are not about capability but about **refusal** —
what the cockpit declines to do, and what it therefore does not have to be trusted about.

Two distinctions run through the whole document and are worth stating once.

**Proven versus attested.** A record's input and output *hashes* are computed by the substrate from
the bytes, so anyone holding the bytes can check them. The *command*, the *environment*, and *which
files were declared inputs at all* are statements by the signer. The signature makes the signer
accountable for them; it does not make them true. kton says the same of the command: `plankton
author` records it and never runs it.

**Checked versus configured.** The cockpit applies a configuration; it does not decide policy. Which
keys are trusted, which claim shapes are permitted, whether commands are executed — all come from a
file the cockpit never writes and no verb exposes. What the cockpit guarantees is that it applies
that file on every call, and refuses when it cannot.

## 1 Scope

### 1.1 In scope

- The three verbs, their arguments, their results, and what each result may be relied upon to mean.
- The binding between a call and the repository it acts on.
- The trust resolution applied to every record before it is returned.
- The optional surfaces: container execution, transparency-log anchoring, publishing the graph.

### 1.2 Out of scope

- Anything the kton spec governs. This document does not restate it and MUST NOT contradict it.
- Who belongs in a trust configuration. The cockpit applies one; composing it is an operator's
  decision about their own participant.
- What a claim *means*. Templates are federated data, not protocol; the cockpit passes a name
  through and never interprets a predicate.

## 2 Normative references

- **kton SPEC** — `spec/SPEC.md` in kton-protocol/kton. Clause references below written as
  "kton §n" refer to it.
- **Model Context Protocol** — the transport the three verbs are exposed over.
- **cockpit.config.schema.json** — in this repository. Normative for the configuration's shape; this
  document is normative for its meaning.

## 3 Terms

- **participant repository** — the directory a cockpit is bound to: a registry, an identity, a
  configuration, and the work being recorded.
- **the binding** — the check, performed on every call, that this is the repository the
  configuration was written for.
- **trust tier** — a named set of public keys in the configuration. A record is trusted when one of
  them verifies its signature, never otherwise.
- **covered / carried** — kton §5. A covered value is part of a record's identity; a carried one is
  attached to it. The distinction decides several clauses below and is not restated.

## 4 Conventions

**MUST**, **MUST NOT**, **SHOULD** and **MAY** are used in the usual sense.

Every normative clause carries a **Checked by:** line naming the test or example that exercises it.
This is deliberate and is the reason the document is expected to stay true: a specification nothing
checks drifts from the code, which is a failure this project has already had in its own test suite
and does not intend to repeat at the level of its contract.

Where something is *not* guaranteed, it says so under **Not guaranteed:**. A guarantee without its
limit is the more dangerous half of a true statement.

## 5 The binding

Every tool call MUST re-resolve the configuration and re-check the binding before doing anything
else. A cockpit MUST NOT cache the verdict across calls.

The reason is the incident this project exists for: several participant registries under one parent
directory, and a session cooperating against the wrong one. Nothing failed. The records were signed,
valid, and in the wrong project.

> **Checked by:** `TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier` and
> `TestAsk_RefusesASelfDeclaredReproductionCount` both rewrite the configuration *between* calls and
> require the next call to act on the new one — which a cached verdict would not.

### 5.1 Locating the configuration

The configuration is `cockpit.config.json`, and it MUST be found in exactly one of two places:

1. the starting directory, or
2. the root of the git repository containing it, if there is one.

A cockpit MUST NOT search further up. An unbounded walk lets a session in a directory with no
configuration of its own bind to an ancestor's — including out of a repository it is standing in —
which is the incident above, reached by a search.

> **Checked by:** `TestConfigGuard_DoesNotBindToAnAncestorsConfig`,
> `TestConfigGuard_ResolvesFromASubdirectoryOfTheRepo`.

`COCKPIT_REPO_DIR` MAY replace the starting directory. It exists for clients that cannot set a
working directory, is set by the operator alongside the command itself, and answers only *which
repository* — never which paths within it.

> **Checked by:** `TestConfigGuard_CockpitRepoDirEnvOverridesCwd`.

### 5.2 git mode (default)

The configuration names an owner and repository. The cockpit MUST read the repository's own
`git remote get-url origin` and refuse unless it names that owner and repository. The configuration
MUST sit at the repository root.

> **Checked by:** `TestConfigGuard_RefusesWhenTheConfigNamesADifferentRepo`,
> `TestGitMode_RefusesAConfigBelowTheRepositoryRoot`,
> `TestConfigGuard_RefusesOutsideAnyGitRepo`, `examples/03-anti-wrong-folder`.

This is a cross-check between two independent sources: the configuration's claim about itself, and
git's own metadata.

### 5.3 local mode

With `repo.mode: "local"` there is no git repository and therefore no remote to disagree with. The
configuration MUST declare its own absolute path, and the cockpit MUST refuse unless that path is
where the configuration actually is.

Local mode MUST be refused inside a git repository that has an origin. Accepting it there would
trade an independent second source for the configuration agreeing with itself, by naming a mode.

> **Checked by:** `TestLocalMode_GuardRefusesAConfigThatWasCopiedElsewhere`,
> `TestLocalMode_RefusesAConfigWithNoDeclaredRoot`, `TestLocalMode_RefusedInsideAGitRepoWithARemote`.

### 5.4 What neither mode catches

| mode | catches | does not catch |
|---|---|---|
| git | acting on a different repository than configured | a duplicated clone of the right one |
| local | a directory copied or moved from the one it was written for | two directories whose paths both look right |

Neither is complete, and a cockpit MUST NOT present either as if it were. Local mode is the stricter
shape for the original incident, which was sibling directories under one parent rather than a
different remote.

## 6 The surface

A cockpit MUST expose exactly three verbs: `publish`, `say`, `ask`. It MUST NOT expose a fourth, and
MUST NOT expose the configuration, the signing keys, the registry, or a general command channel
through any of them.

Operator subcommands (`init`, `doctor`, `show`) MAY exist on the binary. They MUST NOT be reachable
over the tool transport.

> **Checked by:** `TestAsk_UnknownQueryIsRejected`, `TestSay_RefusesATemplateOutsideTheConfiguredCeiling`.

### 6.1 Kernel version

A cockpit MUST verify, on the path a session actually takes, that the substrate binaries it will
invoke are new enough for the surfaces it depends on, and MUST refuse with an explanation rather
than letting the session meet a usage error.

> **Checked by:** `TestCheckKernel_RejectsAnOldKernelAndSaysWhy`,
> `TestEnsureKernel_AnswersOncePerBinDirAndRemembersTheVerdict`.

A cockpit MUST NOT invoke a substrate binary from `$PATH` or any location other than the one its
configuration names. Which build wrote a store decides whether that store reads as populated or as
empty-with-exit-0 (kton §11).

## 7 publish

Records a result as a signed foton, and commits, pushes and locates the bytes it describes.

### 7.1 What the caller supplies

Repo-relative input paths, repo-relative output paths, the command, and optionally an environment
reference and a corpus. **Nothing else.** The caller MUST NOT supply a signing key, a commit, a
permalink, or a foton id; the cockpit owns all of them.

> **Checked by:** `examples/01-publish`, `TestPublish_RecordsASignedFotonAndPushesIt`.

### 7.2 What is proven and what is attested

The output hashes are computed by the substrate from the bytes on disk and MAY be relied upon.

The command, the environment, and the *selection* of inputs are attested by the signature and must
not be relied upon as proven. An omitted input produces a foton that understates what the result
depended on, and nothing in the record reveals it.

This clause places no requirement on an implementation — there is nothing for a cockpit to do
differently — so it carries no check. It is addressed to whoever reads a record, and it is here
because a guarantee whose limit is left unstated is read as covering more than it does.

> **Not guaranteed:** that the declared inputs are all the inputs. §10.3 narrows this when the
> cockpit runs the command itself, and narrows it only to what *changed*.

### 7.3 Paths the cockpit refuses

A publish MUST refuse, before touching git, the substrate, or any container:

- an absolute path, or one escaping the repository;
- a path that could be read by git as a flag rather than a path;
- anything inside the configured keys directory, or with a `.key` extension anywhere;
- anything inside `.git`;
- the cockpit's own configuration.

Every denied path MUST be reported at once, not just the first.

> **Checked by:** `TestPublish_RefusesToCommitSigningKey`, `TestPublish_RefusesLeadingDashOutput`,
> the `TestValidatePublishPath_*` family.

> **Not guaranteed:** this is a path check, never a content check. Copying a key's bytes into an
> innocuously named file and publishing that is not caught, and closing it needs a different
> mechanism.

### 7.4 Ordering and failure

A publish makes two commits: the files, then the registry entry — because the foton embeds locators
that must exist before it is authored. Consequently:

- the returned `commitSha` is the **second**, the repository's real state after the call;
- the foton's own embedded locators pin the **first**;
- both resolve, since the bytes are identical in either.

> **Checked by:** `TestPublish_ReturnsTheActualFinalCommitSHA`, `examples/01-publish`.

If the repository does not commit (§12.1), there is no sha and the cockpit MUST return no
permalinks. It MUST NOT pin them to HEAD: a locator naming a commit that does not contain the bytes
is the failure locators exist to prevent.

> **Checked by:** `TestPublish_WithCommitsOffTheRecordIsStillMadeButCarriesNoLocators`.

### 7.5 The execution environment

`envRef` names where the work ran: an OCI image digest, a nix store path, a run-server id. It is
COVERED (kton §6.5), so the same command in a different pinned environment is a different foton.

It is supplied per call, because a session works across many environments and no configured value
could be right for all of them. Like the command, it is attested.

An `oci://` reference MUST carry a digest. A tag names whatever it points at today, so a reproduction
committing to "that image" commits to nothing. Other forms are passed through uninterpreted.

> **Checked by:** `TestPublish_TheCallerNamesTheEnvironmentAndItReachesTheFoton`,
> `TestPublish_RefusesASuppliedEnvRefWithoutADigest`, `TestPublish_AcceptsANonOciEnvironmentReference`,
> `TestPublish_TheEnvironmentPinChangesTheFotonIdentity`.

Where the cockpit ran the command itself (§10), it MUST record the environment it ran in and MUST
refuse a supplied value naming a different one.

> **Checked by:** `TestPublish_WillNotRecordAnEnvironmentOtherThanTheOneItRanIn`.

## 8 say

Binds a claim to a record.

### 8.1 The claim ceiling

The configuration names the permitted claim templates. A cockpit MUST refuse any other, and MUST NOT
allow a caller to introduce a claim shape.

> **Checked by:** `TestSay_RefusesATemplateOutsideTheConfiguredCeiling`, `examples/04-claim-ceiling`.

### 8.2 The reproduction precondition

For a `reproduces` claim the caller MUST NOT supply the level. The cockpit MUST run the comparison
itself and record what the substrate answers.

The level MUST be read from the substrate's verdict, never inferred from whether a normalizer was
offered: byte-identical outputs are L0 even when one was, because the substrate compares before
consulting it (kton §9). Inferring otherwise mislabels a genuine L0 in any repository with a default
normalizer, which an L0 policy then rejects.

If the outputs do not reproduce, **no claim is written at all.**

> **Checked by:** `TestSay_ReproducesRecordsL0ForIdenticalBytes`,
> `TestSay_ReproducesRefusesWhenTheOutputsDoNotMatch`,
> `TestReproduces_IdenticalBytesAreL0EvenWithViaPassed`, `examples/04-claim-ceiling`.

### 8.3 Registration

A cockpit MUST confirm, by querying the record back, that the claim registered — and MUST NOT report
success on the strength of having written it.

> **Checked by:** `TestSay_RecordsAClaimAndConfirmsItIsQueryable`.

## 9 ask

Queries the graph. Six queries: `producer`, `uses`, `lineage`, `reproductions`, `about`, `by`.

### 9.1 Verified, not declared

Every record a query finds MUST be verified before it is returned: the cockpit resolves its trust
tier by checking the signature against the configured public keys, and MUST NOT use the `keyid` the
envelope declares about itself, which its author wrote.

A record that no configured key verifies MUST be excluded from the answer, and its **content MUST
NOT appear anywhere in the result.** It MUST still be accounted for as found-and-excluded: "nothing
is there" and "something is there that you do not trust" are different answers.

> **Checked by:** `TestAsk_ProducerFindsTheJustPublishedFotonAndVerifiesIt`,
> `TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier`, `examples/05-trust-tiers`.

### 9.2 Filtering

A filter MAY narrow within the configured tiers. It MUST NOT widen past them. A tier name that is
not configured MUST be refused rather than matching nothing — an unknown name otherwise reads
exactly like "this repository trusts none of this".

> **Checked by:** `TestAsk_RefusesAnUnknownTrustTierName`,
> `TestAsk_ReproductionsCountIsScopedToTheRequestedTier`.

### 9.3 Counting reproductions

`reproductions` MUST be answered over the configured trust keys. If the substrate reports a
self-declared count — which it does when it had no trusted key to check against — the cockpit MUST
refuse rather than return it. A forgeable number under a field named for verification is worse than
no number.

> **Checked by:** `TestAsk_RefusesASelfDeclaredReproductionCount`,
> `TestAsk_ReproductionsCountsTheVerifiedProducer`.

### 9.4 Reading structurally

A cockpit MUST read the substrate's machine-readable surfaces where they exist, and MUST NOT parse
values out of human-facing text when a named field is available. A record's id in particular MUST
come from a field, never from a position in a line.

> **Checked by:** `TestParseLineageJSON_TakesTheIdFromItsNamedField`,
> `TestParseClaimsJSON_DecodesTheWholeAxis`.

> **Known exception (0.1):** the reproduction level in §8.2 was read from a printed line until the
> substrate gained a verdict for it. Any such exception MUST be named here and raised upstream, not
> left implied.

## 10 Execution *(optional)*

A repository MAY configure the cockpit to run the published command rather than record one.

### 10.1 What it changes

With execution configured, the string handed to the container runtime and the string pinned into the
foton are the same string, so the environment stops being attested and becomes observed.

> **Checked by:** `TestContainer_PublishRunsTheCommandAndPinsWhatItRanIn`, `examples/07-run-in-container`.

### 10.2 The container

The image MUST be digest-pinned. The run MUST be network-isolated unless the repository opts out,
and a cockpit MUST report when it did not isolate: such a run depended on something the foton does
not pin.

The mount MUST NOT expose the trust base. The configured keys, the substrate binaries, the
registries, `.git` and the configuration itself MUST be masked, because the repository is where they
live and the cockpit executes them **on the host** immediately afterwards.

> **Checked by:** `TestContainer_TheTrustBaseIsNotInTheRoom`, `TestContainer_TheDefaultRunHasNoNetwork`,
> `TestContainer_WithNetworkAllowedTheSameCommandReachesOut`, `TestContainer_OutputsAreOwnedByTheInvokingUser`.

A failed run MUST fail the publish and leave no commit.

> **Checked by:** `TestContainer_AFailedRunPublishesNothing`, `TestContainer_ARunThatProducesNoDeclaredOutputFails`.

### 10.3 Undeclared changes

Having run the command, the cockpit knows what changed. It MUST report files the run touched that
the publish did not declare, and MUST NOT adopt them as outputs.

Adopting them would put incidental files — a cache, a log — into the foton's identity, and two
identical runs would stop producing the same record. Reporting them is what makes a forgotten output
visible; it is the only point at which a declaration can be held against anything.

> **Checked by:** `TestContainer_ReportsAFileTheRunWroteButThePublishDidNotDeclare`,
> `TestContainer_AnUndeclaredChangeDoesNotFailThePublish`.

> **Not guaranteed:** inputs. Which files a command *read* is not observable without tracing, so the
> declaration in §7.2 is unimproved on that side — and it is the more consequential side.

## 11 Anchoring *(optional)*

A repository MAY have every record witnessed in a transparency log.

A signature says who signed. It does not say when, and it does not prevent a signer from later
producing a different record and calling that one the original. An anchor adds an independent,
append-only witness, stored beside the record as verification material (kton §8.1).

The cockpit MUST verify inclusion and the log's signed timestamp, and MUST verify that the entry
binds to this record — a hostile endpoint replaying a real but unrelated entry MUST be refused, not
reported as anchored. A failed anchor MUST fail the call: a witness reported but not recorded is
worse than none.

A custom log MUST have a pinned public key. A log that issues its own signed timestamp and is then
checked against its own key verifies nothing.

> **Checked by:** `TestAnchor_TheProofIsAttachedToTheRecordAndCommittedWithIt`,
> `TestAnchor_AFailedAnchorFailsThePublish`, `TestAnchor_RefusesACustomLogWithNoPinnedKey`,
> `TestAnchorLive_TheEntryTheCockpitReportsIsInThePublicLog` (against the public log).

**Disclosure.** Anchoring submits the whole envelope, not a digest of it. The log receives the
command, every input and output path and hash, the environment, and the locators — which name the
repository. For a private repository its name, file layout and commands become public and stay
public. File contents do not: only their hashes. A cockpit MUST state this where the setting is
configured, and MUST default to off.

## 12 Optional surfaces

### 12.1 Git

Committing and pushing MAY be turned off. Signing and registration are unaffected; what goes away is
the locators (§7.4). The binding (§5) MUST be unaffected — turning off writing to git MUST NOT turn
off checking which repository this is.

> **Checked by:** `TestConfigGuard_StillRefusesTheWrongRepoWithCommitsOff`,
> `TestPublish_WithPushOffItCommitsLocallyAndSaysSo`.

### 12.2 Publishing the graph

A repository MAY commit its aggregate for a viewer to read. The files MUST be regenerated and
committed **in the same commit as the record that caused them**; an aggregate published a commit
later means every reader is one record behind with no way to tell.

The key ring MUST be built from the configured trust tiers, not from whatever public keys are in the
registry. A viewer re-verifying against a wider ring than §9.1 uses would display as trusted exactly
what a query excludes.

> **Checked by:** `TestUnion_PublishedAlongsideTheRecordThatCausedIt`, `TestShow_TheRingIsTheConfiguredTrustTiers`,
> `TestShow_KeysAreFiledUnderTheKeyidASignatureCarries`, `examples/08-union-and-show`.

### 12.3 Reading records

A cockpit MUST obtain records through the substrate and MUST NOT parse the registry's files. The
store's layout is the substrate's, and a consumer that assumes one shape loses records silently
rather than failing (kton §11).

## 13 What a cockpit does not do

From the governing design, restated normatively:

- No canonicalization, hashing, signing, registry or chain logic of its own. Every mutation and query
  goes through the substrate.
- No trust logic beyond applying the configuration.
- No fourth verb, and no state of its own between calls beyond that static configuration.
- **Deletable:** every operation MUST remain performable by hand with the substrate binaries. A
  cockpit that made something reachable only through itself would have become the thing it narrows.

A capability gap in the substrate MUST be raised against the protocol, not worked around here.

## 14 Conformance

An implementation conforms if every MUST above holds. Because each clause names its check,
conformance is demonstrable rather than asserted:

```bash
go test ./...                 # clauses 5–9, 11–13
go test -tags docker ./...    # clause 10, against a real container runtime
go test -tags live ./...      # clause 11's public-log half
examples/run-all.sh           # one property per directory, end to end over the tool surface
```

The default suite does not skip. A suite that degrades to skips reports success having exercised
nothing, which is how the property in §5 went unverified for months in this implementation's own
history.
