# ADR-005: Channel-Neutral Binary Distribution and Compliance Gates

> Addendum (2026-08-04): `cmd/oneagent-release`, `cmd/oneagent-rc`, and
> `cmd/oneagent-provider-smoke`, all mentioned below, were removed in `23805b0`;
> their responsibilities moved to `.github/workflows/build-artifacts.yml`. The
> commands here are historical background and are not executable.

## Status

Accepted

## Date

2026-07-27

## Context

The early release plan was built around GitHub Actions, four-platform artifacts, and
GitHub Releases. The current product goal has narrowed to distributing the OneAgent
binary package directly, where the download channel may be GitHub, the official
site, a file-sharing service, or a corporate cloud drive, and is not tied to any one
platform.

If every channel repackages independently, or third-party Agent binaries get
re-hosted on a file-sharing service, the result is untraceable versions,
inconsistent checksums, missed licence obligations, brand confusion, and incomplete
security withdrawals. A channel being available also does not mean the package
contents carry redistribution rights.

## Decision

### Distribution model

OneAgent uses a "one official build, many identical mirrors" distribution model:

- GitHub, the official site, file-sharing services, and corporate cloud drives are
  interchangeable download channels, nothing more.
- Every channel for a given version must distribute byte-identical artifacts with
  identical SHA-256 checksums.
- Channel operators must not recompress, substitute files, add promotional content,
  or alter version identifiers.
- Every mirror is recorded in the channel ledger, which supports withdrawal
  synchronized across channels. The ledger is a manual process with no automated
  gate.

### Package boundary

- By default, only OneAgent's own code and the runtime dependencies whose licence
  obligations have been satisfied are distributed.
- Third-party Agent binaries are not distributed; being officially downloadable is
  not equivalent to being redistributable.
- Agent installation keeps its fallback order: official source, authorized mirror,
  manual user installation, then `guide-only`.
- Node.js, Git Bash, VPNs, proxies, shared Keys, and third-party configuration tools
  are not bundled. Aider's Python 3.12 is an external upstream prerequisite that
  applies only when the user chooses Aider.

### Current release scope

- The current target is a technical preview binary package that can be downloaded
  and run directly. Per-channel distribution does not require all four platforms to
  be complete, but each platform that is actually released must still be built
  natively on the corresponding operating system, with the `ci.yml` cleanroom job or
  the Release Candidate process as the platform acceptance evidence.
- Each artifact declares only the target environments it was actually built and
  verified on, and makes no compatibility promise for environments that were not
  built.
- Platform signing, notarization, store distribution, and auto-update are out of
  scope for the current stage.
- `technical-preview-unsigned` remains in use until the higher-tier release gates
  are met; the Stable label is not used. The Stable bar itself remains in force and
  is verified against the artifacts by the later signing stage of
  `cmd/oneagent-release` (macOS `codesign` / Windows Authenticode); the current
  stage simply does not publish Stable.

### Compliance gates

All channels share one set of rights, brand, secret, security, privacy, package, and
channel-ledger checks. For the full specification see
[OneAgent multi-channel distribution and compliance policy](../distribution-compliance-policy.md).

## Relationship To Previous Decisions

- The ADR-002 boundaries on network access, shared Keys, third-party Agents, and
  configuration tools remain in force.
- The ADR-003 version pinning, config adaptation, and permission constraints remain
  in force; its old runtime implementation has been replaced by the Go/Wails path in
  ADR-007.
- The part of ADR-003 that made "all four platforms simultaneously" the current
  release bar is narrowed by this ADR: the coupling requiring all four platforms to
  be complete at once is removed, and no single platform's native build and
  acceptance requirement is removed. The platform matrix can be a later expansion
  and no longer blocks the current channel distribution goal.
- The ADR-004 per-Agent protocol verification remains in force.

## Alternatives Considered

### Designate one channel as the authoritative source and mark the rest as mirrors

- Pro: conceptually simple, with a clear chain of trust.
- Con: recipients have to cross-check across channels to establish trust, and offline
  copies and corporate cloud drives cannot be cross-checked at all. When the
  authoritative source is unreachable, mirror artifacts degrade to unverifiable.
- Conclusion: rejected. Verifiability must be contained in the artifact itself rather
  than depend on the download source.

### Allow channels to repackage, with the channel regenerating the manifest and checksums

- Pro: channels are free to add their own notes or adjust the directory layout.
- Con: this amounts to each channel issuing its own set of checksums; recipients
  cannot tell which set is official, and malicious tampering becomes
  indistinguishable from well-intentioned repackaging.
- Conclusion: rejected. Channel-specific notes belong on the channel page, not in the
  package.

### Adopt GPG or sigstore release signing in place of self-attesting checksums

- Pro: provides proof of provenance rather than only proof of integrity.
- Con: it requires key management, rotation, and revocation processes, and recipients
  also have to install verification tooling. We do not yet have even macOS/Windows
  code signing certificates, so doing release signing first is out of order.
- Conclusion: not adopted for now. Reassess after obtaining code signing certificates
  and entering the Stable path; that will require a new ADR.

### Keep all compliance rules scattered across the README and the product boundary baseline

- Pro: no increase in the number of documents.
- Con: the state before this work landed already proved that this drifts -- the same
  set of rules had different wording and granularity in two places, there was no way
  to tell which one governed at release time, and at one point the docs claimed a
  gate had been lifted while the code still enforced it.
- Conclusion: rejected. Replaced by a layered split: the product boundary baseline
  owns the "may we do this" admission decision, this ADR and the compliance policy
  own the "how do we ship it" operational constraints, and the README keeps a
  reader-facing summary that links to the policy. None of the three restates the same
  rule.

## Consequences

### Positive

- GitHub, file-sharing services, and the official site can use the same artifact, with
  no channel branches to maintain.
- Users can tell from the SHA-256 whether a mirror has been substituted.
- Not mixing third-party Agent packages into OneAgent lowers copyright, supply chain,
  and version maintenance risk.
- When a vulnerability, key leak, or copyright complaint occurs, every channel can be
  located and withdrawn in sync.

### Negative

- When the network is unreachable, one-click installation cannot be guaranteed by
  re-hosting third-party Agent binaries.
- The channel ledger, licence inventory, and cross-channel withdrawal process have to
  be maintained.
- Platforms that are not covered must not appear in release promises just because the
  source is theoretically compatible.

## Release Gate

- SHA-256 identical across all mirrors.
- No unauthorized Agent binaries, shared credentials, proxy tools, or user configs in
  the package.
- Rights inventory, third-party licences, version manifest, and release notes all
  complete.
- Automatic installation sources confined to a fixed allowlist.
- Technical preview status, target environments, and known limitations stated
  explicitly.
- Channel owner, link, upload time, and withdrawal status traceable.
