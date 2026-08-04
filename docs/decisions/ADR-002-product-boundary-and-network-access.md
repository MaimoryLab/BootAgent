# ADR-002: Fixing OneAgent's Network Access, Distribution and User Scope Boundary

## Status

Accepted

## Date

2026-07-21

## Context

OneAgent targets individuals, teams and organizational redistributors who need a
local AI development environment. It provides guided Provider signup, API Key
configuration, model selection and guided Agent installation.

The official installation source for some Agents may be unreachable from the
user's current network. If we were to bundle a VPN, a proxy, node subscriptions
or cross-border relays in order to raise the "one-click success rate", the
product would turn from a local configurator into a network access service, and
would take on compliance, operational and security risk.

The starter bundle also involves third-party Agents, open source licenses,
configuration tools such as CC Switch, and user API Keys, so the distribution,
permission and data boundaries need to be fixed.

## Decision

OneAgent is fixed as:

> A local activation guide for Provider accounts and model APIs, a coordinator
> for official Agent installation, and a local configurator.

Explicitly out of scope:

- VPN, proxy, dedicated lines, or any ability to bypass network restrictions.
- Shared Provider API Keys or a unified gateway.
- Automatic login, captcha handling, or automatic claiming of account benefits.
- Distribution of commercial Agent packages without permission.
- Uploading Keys, prompts, code or full request content by default.

Software download follows a fallback chain of "official source → authorized
mirror → manual user install → documented guidance". When the network is
unreachable, the product may only report it as unreachable and offer the manual
path; it does not offer a way around the network.

Users may make direct use of the public benefits shown on their current Provider
account page, or any other legitimate quota, but OneAgent does not promise a
fixed free quota and does not write any benefit into the core business model.

API Keys are fixed to a model where the user's own Key is stored locally. A
unified gateway is a separate project and is not part of the current MVP.

CC Switch is fixed as documentation for an optional local configuration tool. It
is not packaged, not installed by default, and does not replace a Provider.

## Alternatives Considered

### Bundle a VPN or proxy to guarantee downloads succeed

- Pro: the user experience looks smoother on the surface.
- Con: changes what the product is, and adds network service, data security and
  operational responsibility.
- Conclusion: rejected.

### Put every Agent package straight into the archive

- Pro: better offline install experience.
- Con: high cost in licensing, commercial authorization, version updates and
  security verification.
- Conclusion: rejected. The first version uses official sources, authorized
  mirrors and detection of manual installs.

### Collect API Keys and request logs by default

- Pro: the platform can troubleshoot and gather statistics centrally.
- Con: creates a high-risk central store of sensitive data, beyond what a local
  launcher needs.
- Conclusion: rejected. Only local logs and optional minimal anonymous events are
  kept.

## Consequences

### Positive

- The product boundary can be stated the same way to everyone.
- Different users share one activation flow.
- An unreachable network is no longer read by the product as a need to bypass
  network restrictions.
- Responsibility boundaries for software distribution, API Keys and third-party
  tools are clear.
- If a unified gateway or network service is wanted later, it can be assessed
  independently without contaminating the MVP.

### Negative

- One-click download cannot be guaranteed to succeed on some networks.
- Users may need to install manually, or use a compliant network provided by
  their organization.
- Official sources, authorized mirrors and version verification data have to be
  maintained.
- Public benefits cannot be guaranteed by OneAgent.

## Release Gate

The following must be checked before release:

- The package and scripts contain no VPN, proxy, node subscription or
  cross-border relay.
- Public pages do not use wording such as "bypass the firewall", "break through
  restrictions" or "fixed free quota".
- User Keys do not pass through an OneAgent server.
- Agent licenses, upstream addresses, versions and checksums are complete.
- Provider benefit descriptions link to the official page and note that what the
  account actually shows is authoritative.
- CC Switch appears only as optional configuration documentation.
- There is a clear, compliant manual install hint when the network is
  unreachable.
