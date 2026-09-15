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

**Section references.** A bare §N is a clause of *this* document. A clause of the kton protocol
specification is always written **kton §N**. The two numbering schemes collide — this document's §11
is "Carried evidence" where kton's is "Registry, resolution, and completeness", and §13 and §14
differ likewise — so the prefix is not decoration. Go comments in this repository use `SPEC §N` for
this document, since there is no ambient "this document" at a call site.

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

### 8.4 Building on someone else's result

A repository MAY require that a record have been independently reproduced before it will let that
record be the basis of its own work — the `corpus` of a publish (§7.1).

The count MUST be the verified one (§9.3). A self-declared ↻N would make the threshold satisfiable
by relabelling a keyid, which is the opposite of what a corroboration bar is for.

The check MUST happen before anything is written. A publish that failed it after committing would
leave the basis recorded and the conclusion refused.

> **Checked by:** `TestPublish_RefusesACorpusRecordNobodyElseHasReproduced`.

> **Not guaranteed:** that a corroborated record is correct. Independent reproduction says several
> parties produced the same bytes, which is a statement about agreement, not about truth.

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

A verdict about a record MUST NOT be confused with a failure to reach one. The substrate answers
both through the same exit status, and they have opposite correct handlings:

| what the kernel says | what it means | handling |
|---|---|---|
| wrong key | no configured key signed this | exclude the record |
| structurally invalid | the signature is genuine; the record is one `add` refuses | exclude the record |
| an operational error | the pubkey path is unreadable, the hex is malformed | **fail the call** |

The last row is not pedantry. Reading an operational error as a verdict is a bug this cockpit has
already had: a typo'd path in a trust tier exited non-zero, and "this cockpit cannot read your
configured key" was reported as "this signer is not trusted" — a broken trust configuration wearing
the appearance of a working one. The first two rows must not fail the call for the mirror-image
reason: this cockpit reads stores it did not write, and a kernel may tighten its structural rules,
so one record a newer kernel refuses would otherwise deny every answer that touched it.

> **Checked by:** `TestAsk_ProducerFindsTheJustPublishedFotonAndVerifiesIt`,
> `TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier`, `examples/05-trust-tiers`,
> `TestVerifyFoton_ExitOneIsARealErrorNotASilentMismatch`,
> `TestVerify_ExitThreeExcludesTheRecordRatherThanFailingTheCall`,
> `TestVerifyExitCodes_OnlyTwoAndThreeAreRecordVerdicts`.

### 9.2 Filtering

A filter MAY narrow. It MUST NOT widen: no dimension can surface a record the trust configuration
would otherwise exclude, and none can reach past the configured tiers.

| dimension | narrows to |
|---|---|
| `trustTier` | records a key in that configured tier verified |
| `signer` | records a specific configured key verified — never a keyid a record declares about itself |
| `level` | claims at a given reproduction level |
| `scope` | claims chained under one scope |
| `minReproductions` | a `reproductions` answer reaching at least that many verified producers |

A value naming something this repository does not have MUST be refused rather than matching
nothing. An unknown tier, level or signer would otherwise empty the answer and read exactly like
"this repository trusts none of this" — a typo presenting as a finding.

A dimension that has nothing to act on is not an error: a foton carries no level and no scope, so a
lineage answer is narrowed by tier and signer only. The report in §9.5 still names what was asked
for, so a reader can see that a filter did not bite rather than assuming it did.

An excluded record's content MUST NOT reach the caller by any route. Not through the decoded
records, and not through the human-readable text returned alongside them: that text MUST be
assembled from included records only, never produced whole and then cleaned up.

That distinction is the clause, not an implementation note. Redacting after the fact requires
knowing which part of a line belongs to which record, and the cockpit did once assume a record's id
was the first hash on its line — an assumption nothing guaranteed and that the substrate's own
documentation now contradicts. Building the answer from records that passed leaves nothing to
redact.

> **Checked by:** `TestAsk_FilterDimensionsNarrowAndAreValidated`,
> `TestAsk_RefusesAnUnknownTrustTierName`, `TestAsk_ReproductionsCountIsScopedToTheRequestedTier`,
> `TestAsk_MinReproductionsSaysTheThresholdWasNotMet`,
> `TestAsk_AboutExcludesAClaimThatVerifiesAgainstNoConfiguredTier` (nothing included),
> `TestAsk_RawCarriesOnlyIncludedRecords` (one included, one filtered out).

### 9.5 The active filter travels with the answer

Every answer MUST report which filter was applied. "No trustworthy cleanup was found" and "no
cleanup was found" are different findings, and a reader who cannot see which filter ran cannot tell
them apart.

A threshold that is not met MUST say so rather than return silence: no single record was at fault,
there were simply not enough of them.

> **Checked by:** `TestAsk_MinReproductionsSaysTheThresholdWasNotMet`,
> `TestAsk_FilterDimensionsNarrowAndAreValidated`.

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

## 11 Carried evidence *(optional)*

A record MAY carry external evidence about itself: who a signing key belongs to, that the record
existed by a given time, or anything a scheme this cockpit has never heard of expresses. kton §8.1
stores such evidence as opaque bytes under a scheme token the kernel never interprets, and leaves
deciding what any of it is worth to a cockpit. This clause is that decision.

### 11.1 Carrying

A cockpit MUST attach every piece of evidence its configuration declares to every record it writes,
foton and claim alike. It MUST NOT refuse a scheme for being unfamiliar: the kernel accepts an
unlisted one, and a cockpit that did not would turn its own scheme list into a protocol version.

A cockpit MUST refuse to attach a private key, and MUST refuse a path outside the repository —
evidence is committed with the record it is about, so bytes nobody receiving the record can see are
not evidence. Both MUST be refused when the configuration is loaded, not when a record is written: a
publish that discovered a missing certificate halfway would leave the record registered and its
evidence absent.

Evidence MUST be attached before the record is committed, for the reason §11.3 gives for the anchor:
a record and the evidence about it belong in one commit, because a later one can be missing from a
partial share.

A cockpit MUST NOT let a failure to attach be silent. The record is valid either way — kton §8.1
guarantees material never affects validity — so the caller MUST be told that a record exists and
carries less than was intended, which is a different state from no record at all.

> **Checked by:** `TestMaterial_AnUnknownSchemeIsCarriedNotRejected`,
> `TestValidateMaterial_AnUnlistedSchemeIsAccepted`,
> `TestValidateMaterial_RefusesWhatCannotBeCarriedSafely`,
> `TestCheckMaterialFiles_RefusesEvidenceThatIsNotThere`,
> `TestCheckMaterialFiles_RefusesARootThatIsNotACertificate`, `TestMaterial_ClaimsCarryItToo`.

### 11.2 Evaluating and reporting

A cockpit MUST evaluate the evidence it is able to evaluate, and MUST report, per item, which of
those two happened. Exactly three verdicts, and no fourth:

- **verified** — this cockpit checked it and it holds.
- **carried** — it travels with the record and nothing here evaluated it.
- **failed** — this cockpit checked it and it does not hold.

*Verified* and *carried* MUST NOT be collapsed into one another. Counting unevaluated evidence as
verified overstates it; omitting it understates it; and a reader can tell neither from a record that
says nothing. A *verified* verdict MUST name what produced it.

These three are kton §8.1's own recommendation for a consumer, not a vocabulary invented here: it
asks for *verified here* (naming who checked), *carried* (nobody here evaluated it) and *failed*,
having just forbidden the kernel from reporting any verdict of its own.

Recognition MUST be by what the bytes are, not by what the scheme token claims. A certificate filed
under a house-specific scheme is still a certificate and MUST still be checked. This is the same
rule as §9.1 in a different place: what a record says about itself does not decide what it is.

A certificate MUST be reported as verified only when it belongs to the key that actually verified
the record. A certificate for some other key may be impeccable and still says nothing here, and
accepting it would reintroduce "declared, not verified" by another route.

A cockpit MUST NOT fall back to the host's root store when a repository configures no trust anchor.
The answer a reader gets MUST NOT depend on which machine ran the query; with no configured root the
correct verdict is *carried*.

A *failed* item MUST NOT on its own exclude the record. Whose word counts is a trust decision, and
trust here is §9.1 plus the configuration, not the evidence judging itself.

A failure MUST say which failure it is, in a machine-readable field rather than in prose. Damaged or
forged bytes and a perfectly good certificate wired to the wrong record are both *failed*, and they
demand opposite responses — one is investigated, the other is a line in the configuration. Leaving
that difference in a sentence would make a caller parse prose to act, which is the mistake this
project removed from `say`'s reproduction level.

Evidence attached to a record that was excluded MUST NOT be reported. It is content from a record
the caller was not given, and §9.2 already forbids that content reaching them by any route; material
is one more route.

A cockpit MUST make the same verdict available to the operator before any record is written.
Evidence that will never verify — a certificate for the wrong key, or one with no configured root to
judge it against — otherwise produces valid records that report *carried* indefinitely while the
operator believes an identity was established. Nothing fails, so nothing says so.

> **Not guaranteed: the verdict does not travel.** A cockpit MUST NOT store a verdict alongside the
> evidence. What is attached is the evidence and only the evidence, so a peer that mirrors the record
> receives bytes and re-evaluates them against its own configured roots — and may legitimately reach
> a different answer about identical bytes.
>
> This is stated rather than implied because operators reliably assume the opposite. *Verified* reads
> like a property of the record; it is a property of the reading. A record is not "a verified record"
> anywhere but in the repository whose configuration produced that word, which is the same shape as
> §9.1: a trust tier is this repository's judgement, not a label the record carries.

> **Checked by:** `TestEvaluate_CertificateBoundToSigningKeyAndChainingToAConfiguredRoot`,
> `TestEvaluate_CertificateForAnotherKeyIsFailed`, `TestEvaluate_ExpiredCertificateIsFailed`,
> `TestEvaluate_RecognisesTheArtifactNotTheSchemeToken`,
> `TestEvaluate_NoConfiguredRootsIsCarriedNotVerified`,
> `TestEvaluate_UnreadableEvidenceIsCarriedAndSaysSo`,
> `TestEvaluate_RekorEntrySaysItWasVerifiedWhenAttached`,
> `TestX509Roots_NoConfiguredRootsIsNilNotTheSystemPool`,
> `TestMaterial_ACertificateForTheSigningKeyIsAttachedAndReportedVerified`,
> `TestMaterial_ACertificateForSomebodyElseIsReportedFailed`,
> `TestMaterial_EvidenceOnAnExcludedRecordIsNotReported`,
> `TestMaterial_NoConfiguredMaterialMeansNoneReported`,
> `TestMaterial_PreflightReportsTheVerdictBeforeAnythingIsPublished`,
> `TestEvaluate_EveryFailureNamesWhichOne`, `TestEvaluate_OnlyFailuresCarryAReason`,
> `TestMaterial_TheVerdictIsNeverStored`.

### 11.3 Anchoring

A repository MAY have every record witnessed in a transparency log. An anchor is one kind of carried
evidence — the kind that answers *when* — and everything in §11.1 and §11.2 applies to it.

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

## 14 Open questions

Recorded here rather than left implied, and phrased as what is missing rather than what is planned.

### 14.1 Keyless identity is not wired

A record signed by this cockpit carries an ed25519 key, and §11.2 lets a repository bind that key to
a person: an X.509 identity certificate rides with every record and is reported as verified when it
belongs to the key that actually signed and chains to a root the repository configured. That is the
organisational-PKI route, and it is built. What §9.1 establishes is still only *which key* signed;
what §11.2 adds is a checkable statement about whose key that is.

The keyless route (Fulcio/OIDC) is not wired, and exactly one thing is missing: obtaining an OIDC
token and exchanging it at Fulcio for a certificate. Nothing below that is blocked.
`sigstore.Anchor(url, env, verifierPEM)` takes an arbitrary verifier PEM, so the log would accept a
Fulcio certificate today; the hex-ed25519 narrowing sits only in `kton anchor`'s argument parsing,
and a cockpit is not obliged to reach the library through another cockpit's CLI. That is a
dependency decision to make when there is a token to exchange, and `kton anchor` moves into a
cockpit anyway (kton #103), which is the moment to make it — not a protocol request, and it was
raised as one in error.

Re-verifying a *stored* transparency-log entry is a separate matter and is deliberately not done.
Nothing re-checks one, and kton §8.1 now states that as a property rather than leaving it to be
discovered: a kernel verifies material neither when it stores it nor when it hands it back, and
"presence is not a check". `kton anchor` verifies before attaching and never again. Doing that check on the read path would be this cockpit
growing its own transparency-log cryptography, which §13 forbids. So a stored entry reports as
*carried*, with a detail saying it was verified when attached and is not re-checked now — the
presence of an entry is not evidence that anyone ever checked it.

The two routes also answer different questions. A short-lived certificate over an ephemeral key fits
an open federation, where signers are not known in advance and the certificate is what carries the
identity. A durable key with a durable certificate fits an internal sign-off, where the signers are
known and a list of keys is the point. The kernel's own note reaches the same split. A deployment
picks one; the cockpit does not pick for it.

### 14.2 Determinism is possible, not required

§10 lets a repository pin the environment by running the command itself. Nothing requires it: a
record that becomes the basis of a claim may have been produced anywhere. A repository that needs
that guarantee currently configures it and is trusted to have done so.

## 15 Conformance

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
