# OneAgent Product Boundary Baseline

## Status

- Status: Frozen / fixed baseline
- Effective date: 2026-07-27
- Scope: OneAgent, the local launcher, public or targeted download packages, all
  download mirrors, documentation, and release pages
- How to change it: any requirement that breaks the "prohibited scope" in this
  document must add an ADR and pass a compliance review

## 1. One-sentence definition

> OneAgent is a tool that helps users activate, configure, and launch a local AI development environment.

It connects Providers, models, Agents, IDEs, and local config tools. The goal is to help
the user complete their first successful AI Agent request.

OneAgent is not a VPN, a proxy, a cross-border network access service, a shared API Key
platform, or a collection of commercial Agent packages.

## 2. Target users

OneAgent is for everyone who needs a local AI development environment:

- Individual users trying an AI Agent for the first time.
- Developers who already have a Provider account and an API Key.
- Advanced users who need multiple Providers, accounts, or model Profiles.
- Companies, communities, and other organization distributors.

Organization distributors may offer programs, redemption codes, or benefit descriptions
outside the project, but that content is not part of the OneAgent core product model.

## 3. Capabilities explicitly allowed

### 3.1 Local launch and configuration

- Launch a local GUI on `127.0.0.1`.
- Detect the Agents and runtime environment on the machine.
- Call official npm, uv tool, Git, or system install sources.
- Detect what the user installed manually.
- Write to config entry points that have already been confirmed as official.
- Call a Provider's OpenAI-compatible API for model discovery and minimal request
  validation.

### 3.2 Official builds, documentation, and licensed mirrors

The following are permitted:

- Binary and source packages built officially by OneAgent.
- Same-package mirrors on GitHub, the official site, file-hosting services, and
  enterprise cloud drives.
- Official Provider registration, API Key, and model documentation.
- Official Agent install instructions.
- Mirrors of open-source software where the license permits.
- Pinned versions, SHA-256 checksums, upstream addresses, and license files.
- Project templates and instructions that organization distributors maintain outside
  the package.

All channels carrying the same version must use identical artifacts and an identical
SHA-256. Channel operators must not repackage, replace files, or add promotional content
inside the archive.

### 3.3 Config tools

The following are permitted:

- OneAgent's built-in configuration.
- Standalone instructions for third-party local Profile tools such as CC Switch.
- Manual configuration and backup restore instructions.

Config tools are responsible for local configuration management only, not for network
circumvention.

## 4. Capabilities explicitly prohibited

The following must not enter the product code, the archives, the official documentation,
promotional pages, or automation scripts:

- VPNs, proxies, "airport" services, dedicated lines, or any other tool for bypassing
  network restrictions.
- Automatic configuration of proxy nodes, proxy subscriptions, or cross-border network
  routing.
- Product wording such as "download over the wall", "break through restrictions", or
  "reach restricted sites without a proxy".
- Automatic login, automatic CAPTCHA solving, or automatic claiming of account benefits.
- Using OneAgent's servers as a relay proxy for users to reach overseas websites.
- Writing a long-lived Provider Key into an archive, frontend code, or a public script.
- Redistributing commercial Agent packages without permission.
- Mirroring open-source Agent binaries or source without license confirmation.
- Producing encrypted, password-protected, split, or disguised packages to evade channel
  review.
- Using third-party brands, logos, or copy in a way that creates the misimpression that
  OneAgent is an official or joint product.
- Publishing archives that share a version number across channels but differ in content
  or checksum.
- Uploading user API Keys, prompts, source code, or complete model requests by default.

## 5. Software acquisition policy

Every Agent must be configured with one download strategy:

| Priority | Strategy | Notes |
| --- | --- | --- |
| 1 | Official install source | npm, uv tool, official Git, official release page |
| 2 | Licensed mirror | Has a license, a pinned version, a checksum, and an upstream address |
| 3 | User manual install | OneAgent only detects the path and version |
| 4 | Documentation guidance | Does not run the install; only shows the official steps |

If the official site is unreachable from the user's current network, OneAgent only shows
"install source unreachable" plus optional manual install paths. It does not offer a
proxy or any method of bypassing network restrictions.

## 6. Providers and API Keys

### 6.1 The user's own account model: the default

```text
The user signs up for or logs in to a Provider
→ The user creates an API Key
→ The Key is stored on the user's own machine
→ OneAgent writes the Agent configuration
```

Users may use the published benefits shown on their Provider account page, or any other
lawful quota, but OneAgent makes no promise of a fixed free quota, of permanently free
access, or of any particular account eligibility.

### 6.2 Unified gateway model: not part of the current MVP

If a unified API gateway is offered later, it must add its own product design and
compliance review, covering at least:

- Per-user tokens.
- Tenant-level and user-level quotas.
- Rate limiting.
- Cost and balance alerts.
- A key revocation mechanism.
- Log redaction and a data retention policy.
- Terms of service, a privacy policy, and operator entity information.

Until that review is complete, embedding a shared Provider Key in the launcher package
is prohibited.

## 7. Config tool boundary

### OneAgent built-in configuration

- The default path for all users.
- Responsible for the first Provider setup and request validation.
- Does not depend on a third-party config tool.

### CC Switch

- An optional local Profile tool.
- Not bundled into the launcher package.
- Not installed by OneAgent by default.
- Uses the official project entry point and the current version's instructions.
- Must not treat any CC Switch service address as a Provider Base URL.
- After switching, requires the user to restart the target Agent and run a minimal
  request validation.

### Manual configuration

- The fallback path for every Agent.
- Provides only verified fields, config paths, and restore procedures.
- Does not modify unknown private state files.

## 8. Data and privacy baseline

### Not collected by default

- API Keys.
- Account passwords.
- Prompts and source code.
- Complete request bodies and model responses.
- Unnecessary personal information such as ID numbers, phone numbers, or student IDs.

### If activation statistics are needed

Only the minimal set of events, disclosed and consented to, may be collected:

```text
package_version
agent_id
provider_id
model_id
result_status
timestamp
```

Event collection should be off by default or offer an explicit choice, and it must not
indirectly upload Keys or user content through error logs, crash reports, or debug mode.

## 9. Fixed product copy

### Usage

> OneAgent helps you sign up for or log in to a Provider, create an API Key, and install and configure a local Agent.

### Network unreachable

> The official install source is currently unreachable. Please use the compliant network access provided by your organization or network service provider, or install manually and return to OneAgent to detect it. OneAgent does not provide VPN, proxy, or network-restriction-bypassing features.

### Published benefits

> A Provider's new-user benefits, referral benefits, and public descriptions are whatever the account page currently shows. OneAgent does not guarantee a fixed quota or permanently free access.

### API Key

> You create the API Key in your official Provider account, and it is stored only on your own machine. Do not send the Key to anyone else or commit it to a code repository.

## 10. Fixed scope of the current version

### Included in V1

- PPIO, Novita, and Custom OpenAI-compatible Providers.
- Local config paths for Codex, Claude Code, OpenCode, Kilo CLI, and Aider.
- Official install and configuration guidance for other Agents.
- Model list retrieval and first-request validation.
- OneAgent built-in configuration.
- Optional CC Switch documentation.
- Manual configuration and backup restore.
- Generic project templates.

### Not included in V1

- Business logic specific to an organization distributor.
- Promises of a fixed free quota.
- VPN, proxy, and cross-border network relay.
- Shared Provider Keys.
- A unified API gateway.
- Distribution of unlicensed Agent packages.
- Telemetry on by default.

### V1 distribution form

- Distribute the official OneAgent binaries directly through GitHub, the official site,
  file-hosting services, enterprise cloud drives, or other lawful channels.
- All channels serve only as mirrors; they do not change package contents, version,
  release status, or checksums.
- Third-party Agent binaries are not placed in the OneAgent archive by default.
- Each artifact declares only the target environments it was actually built and verified
  for, and promises nothing for unverified platforms.
- Platform stores, automatic updates, macOS notarization, and Windows Authenticode are
  out of scope for the current stage.
- The detailed rules are governed by the
  [Multi-channel distribution and compliance policy](distribution-compliance-policy.md).

## 11. Release gates

Before every public or organization-distributor package is released, confirm:

- [ ] The package contains no Keys, proxy nodes, VPNs, or unlicensed binaries.
- [ ] Every third-party file in the package has a source, a license, and a redistribution basis.
- [ ] Every Agent has an official source, a license, or manual install instructions.
- [ ] Every mirror has a version, SHA-256, owner, upload time, and withdrawable status.
- [ ] The same version has exactly identical file contents and SHA-256 across all channels.
- [ ] Provider documentation links have been rechecked.
- [ ] CC Switch is still optional documentation, not a hidden dependency.
- [ ] API Keys do not appear in logs, screenshots, telemetry, or command-line arguments.
- [ ] When the network is unreachable, users are not guided toward bypassing network restrictions.
- [ ] The published-benefits copy promises no fixed free quota.
- [ ] The release notes declare only the target environments the current artifacts were actually built and verified for.
- [ ] Every platform actually released has native build and cleanroom acceptance evidence (`ci.yml` or the Release Candidate process).
- [ ] The channel ledger has been synced manually for this release (the ledger is a manual process; there is no automated gate).
- [ ] Branding and copy create no misimpression of official partnership, a domestic edition, or authorized agency.
- [ ] Cross-channel withdrawal, complaint, and security incident procedures are ready.
- [ ] The official latest version is installed by default; release packages ship a package name and license manifest; reproduction tasks may pin an explicit version.
- [ ] Unsigned builds are clearly marked `technical-preview-unsigned` and do not use a Stable label.

For the complete gates and evidence requirements, see
[OneAgent multi-channel distribution and compliance policy](distribution-compliance-policy.md).

## 12. Change control

The following changes must add an ADR and redo the compliance assessment:

- Moving from local configuration to a unified API gateway.
- Adding proxy, dedicated line, or cross-border network capability.
- Automatically installing a third-party config tool.
- Redistributing commercial Agent packages.
- Allowing channel operators to repackage, modify official artifacts, or maintain
  channel-specific builds.
- Using third-party brand assets or official-partnership wording.
- Collecting user accounts, organization identity, or request contents.
- Extending the launcher into a public commercial API service.

## Official references

- Interim Provisions of the People's Republic of China on the Administration of
  International Networking of Computer Information Networks (2024 revision):
  <https://xzfg.moj.gov.cn/law/detail?LawID=460>
- Ministry of Industry and Information Technology notice on cleaning up and regulating
  the internet network access service market:
  <https://www.miit.gov.cn/zwgk/zcwj/wjfb/tz/art/2017/art_a940645e940946e1a62cd6c90a4e994e.html>
- Personal Information Protection Law of the People's Republic of China:
  <https://www.cac.gov.cn/2021-08/20/c_1631050028355286.htm>
- Copyright Law of the People's Republic of China:
  <https://www.npc.gov.cn/c2/c30834/202011/t20201119_308796.html>
- Regulations on the Protection of Computer Software:
  <https://xzfg.moj.gov.cn/front/law/detail?LawID=581&Query=>
- Interim Measures for the Administration of Generative Artificial Intelligence Services:
  <https://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm>
- Provisions on the Governance of the Online Information Content Ecosystem:
  <https://www.cac.gov.cn/2019-12/20/c_1578375159509309.htm>
- Anti-Unfair Competition Law of the People's Republic of China:
  <https://www.npc.gov.cn/npc/c2/c30834/202506/t20250627_446247.html>
- PPIO official site: <https://ppio.com/>
- PPIO API Key documentation: <https://resource.ppio.com/docs/support/api-key>
- CC Switch official repository: <https://github.com/farion1231/cc-switch>
