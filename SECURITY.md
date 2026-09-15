# Security

## Reporting

Email **michael.hackl@scinteco.com**. Please do not open a public issue first — this project is
about whether a record can be trusted, and a report that describes how to forge one is more useful
to an attacker than to anyone else until there is a fix.

A useful report says what you did, what the cockpit reported, and what it should have reported. If
you have a record that demonstrates it, the foton or claim id is worth more than a description.

This is a small project with no on-call rotation. You will get an acknowledgement as soon as
someone reads it, and an honest estimate rather than a promised window. If a report turns out to be
a documented limit rather than a defect, you will be told which limit and where it is written down
— several of the interesting cases below are exactly that.

## What the cockpit is responsible for

The cockpit runs no cryptography of its own. Signing, canonicalisation, identity and the registry
are the kton kernel's; every mutation and query shells out to `plankton`/`nekton`. A flaw in
signature verification, canonical form, or the record format is a **kton** issue — report those to
that project.

What is this project's responsibility:

- **The binding.** Acting against a repository other than the configured one, in either mode
  (SPEC §5). The whole project exists for this.
- **Verified, not declared.** Any way to get a record reported as trusted whose signature does not
  verify against a configured key, or to influence the reported trust tier from inside the record
  (SPEC §9.1).
- **The claim ceiling.** A claim registered outside `claims.allowedTemplates`, or a `reproduces`
  level the caller chose rather than the cockpit computed (SPEC §8).
- **The path denylist.** Getting `publish` to commit a signing key, or to write outside the
  repository (SPEC §7.3).
- **Carried evidence.** A certificate reported as `verified` that does not belong to the key that
  actually signed the record, or that chains to a root the repository did not configure
  (SPEC §11.2).
- **Container isolation.** Escaping the execution sandbox, or reaching the trust base or signing
  keys from inside it (SPEC §10.2).
- **Redaction.** Content from an excluded record reaching the caller by any route (SPEC §9.2).

## Known limits — documented, not defects

These are stated in the spec with their reasoning. A report about one of them is welcome as a
design discussion, but it is not a vulnerability:

- **A duplicated clone passes git mode**, and two directories whose paths both look right pass
  local mode. Neither mode catches everything, and SPEC §5.4 says which one misses what.
- **`cmd`, `environment` and the *selection* of inputs are signer statements, not proofs.** Hashes
  are computed from bytes and are checkable; that a given command produced them is attested
  (SPEC §7.2).
- **A verdict about carried evidence does not travel.** It is a property of the reading, not of the
  record, and a peer re-evaluates against its own roots (SPEC §11.2).
- **A stored transparency-log entry is not re-checked on read.** It was verified when attached and
  never again; its presence is not evidence that anyone checked it (SPEC §14.1).
- **Anchoring publishes the record, permanently.** The whole envelope goes to the log — the
  command, every path and hash, the repository name. That is disclosure by design, it is off by
  default, and it cannot be undone (SPEC §11.3).
- **The configuration is the ceiling and is trusted.** Anyone who can write `cockpit.config.json`
  can change which keys are trusted and which templates are allowed. Protecting that file is the
  operator's job; the cockpit cannot and does not police it.

## Keys

`keys/*.key` is a private signing key and is gitignored in the participant skeleton. `publish`
refuses any path matching `*.key` regardless of location, and `material.attach` refuses one too. If
you find a route that commits one anyway, that is a report worth sending.
