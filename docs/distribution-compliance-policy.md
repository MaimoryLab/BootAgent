# BootAgent multi-channel distribution and compliance policy

## Status

- Status: Frozen / normative policy
- Effective date: 2026-07-27
- Applies to: BootAgent maintainers, build engineers, channel uploaders, organizational
  distributors, and documentation maintainers
- Applicable channels: GitHub Release, the official site, domestic and overseas
  file-sharing services, corporate cloud drives, and any other public or targeted
  download channel
- Exceptions: any exception must first add an ADR that records the rights basis, the
  risk, the owner, and the withdrawal plan

This document is BootAgent's engineering and operations compliance baseline. It does not
replace formal legal advice on any specific piece of software, licence, trademark, or
mode of operation.

## 1. Fixed product definition

> BootAgent is an environment activator that helps users detect, install, configure, and
> verify AI development tools through a local GUI.

BootAgent may distribute its own build artifacts through multiple lawful channels, but
the distribution channel does not change the rights requirements or the security
requirements that apply to the package. File-sharing services, corporate cloud drives,
and GitHub are download mirrors and nothing more; they are not a substitute for a
redistribution authorization covering third-party software.

By default BootAgent does not distribute third-party Agent binaries. Agents must be
obtained from an official installation source, a mirror covered by written
authorization, a manual user installation, or documentation guidance.

## 2. Identical package across channels

For a given version, every channel must use exactly the same official build artifact.
Channel operators must not recompress it, substitute files, append promotional content,
or re-sign it.

Every public release contains at least:

- The BootAgent binary archive.
- The SHA-256 matching that archive.
- The version, the build time, the target environments, and the release status.
- The third-party licence inventory and full licence texts. The generated source is
  [third_party/THIRD_PARTY_NOTICES.md](../third_party/THIRD_PARTY_NOTICES.md), with
  dependency texts under `third_party/licenses/`; every binary artifact carries the same
  files at its root. `scripts/generate_third_party_licenses.py --check` derives the
  inventory from the production Go targets and frozen frontend dependency graph, and CI
  blocks a stale or incomplete bundle.
- The Agent version pinning manifest and the official sources.
- Release notes, known issues, and a withdrawal contact.

A source ZIP, an SBOM, and signatures may be shipped as additional artifacts. If a given
version claims to provide those files, then every official mirror must carry the same
version and the same checksums.

Every channel must record the following in the release ledger:

```text
release_version
artifact_name
sha256
channel
download_url
uploaded_at
uploaded_by
status
withdrawn_at
withdrawal_reason
```

`status` may only be `active`, `deprecated`, or `withdrawn`. Once the original file has
been reprocessed by a file-sharing service, or its checksum has changed, it must not
continue to be marked as an official mirror.

## 3. Permitted package contents

The following may enter the BootAgent release package:

- BootAgent's own Go code and the built React static assets.
- BootAgent's own icons, documentation, and Profiles.
- Third-party dependencies whose licence explicitly permits redistribution and whose
  corresponding obligations have already been satisfied.
- `agents.lock.json`, the official installation entry points, and the manual
  configuration instructions.
- Entry points to Provider websites, registration, API Keys, and model documentation.
- Standalone documentation for third-party configuration tools such as CC Switch.

For every third-party file, the rights inventory must be able to answer: the source, the
version, the licence, whether binary redistribution is permitted, what Notice has to be
included, whether source code has to be provided, and who reviewed it.

## 4. Prohibited package contents

The following must not enter the binary package, the source package, file-sharing
attachments, the release notes, or the automation scripts:

- Codex, Claude Code, Cursor, Kiro, OpenClaw, Hermes, or other third-party Agent
  binaries that are not covered by a redistribution authorization.
- Software downloaded from npm, uv, GitHub, or another source and copied straight into
  the package without a completed licence review.
- Third-party software that has been modified, cracked, patched, or made to bypass
  signature checks.
- VPNs, proxies, node subscriptions, "airport" relay services, dedicated lines, or other
  tools and configurations for bypassing network restrictions.
- Shared accounts, shared API Keys, long-lived tokens, cookies, CAPTCHA-solving, or bulk
  registration tools.
- User HOME contents, historical configuration, logs, prompts, source code,
  authentication files, or test Keys.
- Unauthorized third-party logos, fonts, promotional images, and brand assets that could
  easily create the impression of an official partnership.
- Encrypted archives, password-protected archives, split archives, or disguised file
  extensions produced in order to evade channel review.
- Source maps, test caches, concept diagrams, Docker test images, and other development
  artifacts that are not required at run time.

## 5. Agent acquisition and installation rules

The Agent acquisition order is fixed as:

1. The official package manager or the official release source.
2. A mirror with an explicit licence or written authorization.
3. Detection by BootAgent after the user installs manually.
4. `guide-only` mode, which shows only the official instructions.

Automatic installation must satisfy all of the following:

- The package manager, the package name, and the platform are on a fixed allowlist.
- By default an Agent installs the latest version resolved by the package manager; when
  reproducibility is needed, the caller specifies the version explicitly.
- Commands are executed with an argument array; `shell=True` and dynamically
  concatenated shell commands are prohibited.
- Before installing, the software name, the source, the version policy, and the actions
  about to be taken are shown and confirmed by the user.
- API Keys, tokens, and account information must not appear in command-line arguments.
- Node.js, Git, VPNs, and system-level networking components are not installed
  automatically. Aider's external Python 3.12 is required by the upstream installation
  flow itself, only when the user explicitly chooses Aider, and never enters the
  BootAgent package.
- Executing a `curl | bash` that has not been pinned and reviewed is prohibited.

When an official source is unreachable, the only permitted response is to report it as
unreachable and offer a manual installation entry point. BootAgent does not configure
proxies, does not provide instructions for bypassing network restrictions, and does not
re-host third-party binaries on a file-sharing service as a stopgap substitute.

## 6. Providers, accounts, and API Keys

- The user registers or signs in through the Provider's official channels themselves.
- The user creates and manages API Keys themselves.
- BootAgent makes no promise of a fixed free quota, permanent free use, eligibility for a
  particular campaign, or account benefits.
- Keys are stored only in the permitted key file on the user's own machine, and do not
  enter Profiles, logs, screenshots, telemetry, URLs, command lines, the release
  package, or test reports.
- The GUI must display the actual Base URL and the destination address that data is
  about to be sent to.
- A custom Provider must reject URL credentials, invalid schemes, and control
  characters.
- BootAgent does not operate a shared Key pool, does not forward model requests on the
  user's behalf, and does not describe a local activator as a unified API gateway.

If a unified gateway, remote configuration, an account system, download statistics,
crash reporting, or request proxying is added in the future, the obligations relating to
personal information, network data, and generative AI services must be assessed
independently; the default boundaries of a local tool cannot simply be carried over.

## 7. Brand and public copy

Using the textual names of third-party products to describe compatibility is permitted,
but it must not imply that BootAgent is an official product, a domestic edition, a joint
edition, or an authorized agent of the vendor in question.

Recommended wording:

> BootAgent is an independently developed environment configuration tool that helps users
> install or configure some third-party AI development tools. The relevant products and
> trademarks belong to their respective rights holders.

Prohibited wording:

- "Claude Code, official domestic edition"
- "Codex, cracked portable edition"
- "Cursor, domestic enhanced edition"
- "Official OpenAI partner installer"
- "Claude/Codex built in, no official account needed"

The product's primary visual identity uses BootAgent's own assets or generic icons; a
third-party logo is not used as BootAgent's brand mark.

## 8. Channel operations rules

- Channel upload accounts must be managed by a named owner; an untraceable personal
  throwaway account must not be the sole source.
- The official page publishes at least one trustworthy source for the SHA-256; a
  checksum shown on a file-sharing page cannot be the only basis for verification.
- Channel operators are not permitted to change the version, platform, architecture, or
  release status in the file name.
- No channel-specific promotion, redemption codes, shared Keys, or organizational
  business logic is embedded inside the archive.
- When a file-sharing service or platform requires removal, the notice, the version, the
  link, the person who handled it, and the outcome should be retained.
- On discovering a copyright issue, a key leak, malicious tampering, or a high-severity
  vulnerability, every channel must withdraw in sync; deleting only the GitHub version
  is prohibited.
- A withdrawn version must not continue to be distributed under a different link; once
  fixed, a new version with new checksums must be published.

## 9. Release gate

All of the following must pass before any public or targeted distribution:

### Rights and brand

- [ ] Every third-party file in the package has a source, a licence, and a
      redistribution basis.
- [ ] The licences, Notices, or source code that have to be included are included.
- [ ] No unauthorized Agent binaries, fonts, logos, or promotional material are
      included.
- [ ] The product name and the promotional copy do not create confusion about an
      official partnership or about origin.

### Security and privacy

- [ ] The secret scan found no Keys, tokens, cookies, accounts, or local authentication
      files.
- [ ] The package contains no VPNs, proxies, nodes, bypass scripts, or shared
      credentials.
- [ ] The React build has no source maps, remote scripts, remote fonts, or CDN run-time
      dependencies.
- [ ] API Keys do not enter logs, URLs, command lines, Profiles, telemetry, or test
      artifacts.
- [ ] Every automatic installation command comes from the fixed allowlist and is
      executed with an argument array.

### Release integrity

- [ ] Binary startup and a smoke test of the core flows completed in a fresh temporary
      HOME.
- [ ] The package contains no Agent binaries, user configuration, test caches, or
      leftovers from previous builds.
- [ ] The version manifest, the licence inventory, the SHA-256, and the release notes
      are all complete.
- [ ] Every channel uploaded the same artifact, with matching checksums.
- [ ] The channel ledger contains the uploader, the time, the link, and the status.
- [ ] Cross-channel withdrawal and a security incident response path are ready.

Any single failure must block the release. The gate must not be skipped on the grounds
that it is "only a file-sharing link", "only a small-scale test", or that "users accept
the risk themselves".

## 10. Retention of compliance evidence

For every version, retain at least:

- The build commit, the build environment, and a summary of the build log.
- The release package file manifest, and an SBOM or an equivalent dependency inventory.
- Third-party licence and rights review records.
- The results of the secret, malware, and release policy scans.
- The binary cleanroom smoke test results.
- The SHA-256 and the channel ledger.
- Records of withdrawals, complaints, and security incident handling.

The evidence must not retain user API Keys, Authorization headers, complete model
requests, or the contents of real user directories.

## 11. References

- Copyright Law of the People's Republic of China:
  <https://www.npc.gov.cn/c2/c30834/202011/t20201119_308796.html>
- Regulations on Computer Software Protection:
  <https://xzfg.moj.gov.cn/front/law/detail?LawID=581&Query=>
- Personal Information Protection Law of the People's Republic of China:
  <https://www.cac.gov.cn/2021-08/20/c_1631050028355286.htm>
- Interim Measures for the Management of Generative AI Services:
  <https://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm>
- Provisions on the Governance of the Online Information Content Ecosystem:
  <https://www.cac.gov.cn/2019-12/20/c_1578375159509309.htm>
- Anti-Unfair Competition Law of the People's Republic of China:
  <https://www.npc.gov.cn/npc/c2/c30834/202506/t20250627_446247.html>
