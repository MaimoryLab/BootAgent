# ADR-001: A built-in configuration path plus optional config tools

## Status

Accepted

## Date

2026-07-21

## Context

BootAgent aims to help different kinds of users activate a genuinely usable local AI
development environment. A user may need only one Provider, or may need to switch between
several Providers, accounts, and models.

Third-party tools such as CC Switch can manage local profiles for a user, but bundling
them into BootAgent would import their version, licence, install script, and configuration
compatibility risks.

## Decision

BootAgent offers three configuration layers:

1. BootAgent's built-in configuration: the default path, responsible for first-time
   Provider activation and agent configuration.
2. Third-party tools such as CC Switch: an optional path, documented separately and never
   installed automatically.
3. Manual configuration: the fallback, for unsupported agents, advanced users, and
   debugging.

BootAgent does not treat a third-party config tool, a third-party hosted API service, or a
shared Provider key as a core dependency.

Documentation for any config tool must state:

- Its official source.
- Which agents it supports.
- Its profile fields and base URL format.
- Where it stores the API key.
- Whether a restart is needed after switching.
- How to back up and restore configuration.

## Alternatives Considered

### Bundle CC Switch directly into the kit

- Upside: one less install step for the user.
- Downside: BootAgent would own a third party's versioning, licensing, signing, updates,
  and configuration compatibility.
- Conclusion: rejected. BootAgent offers it as an optional path through documentation.

### Support manual configuration only

- Upside: simple to implement, with clear compatibility boundaries.
- Downside: first-time users readily get the base URL, model ID, and config file location
  wrong.
- Conclusion: rejected. Built-in configuration is the default; manual is the fallback.

### Let any third-party config tool write private state files automatically

- Upside: would cover more agents.
- Downside: private file formats are unstable, easy to corrupt, and hard to audit.
- Conclusion: rejected. Only confirmed official configuration entry points are written.

## Consequences

### Positive

- The first-run configuration path is simple and testable.
- Third-party tools can upgrade independently without holding BootAgent back.
- Users can choose built-in configuration, CC Switch, or manual configuration according to
  their own familiarity.
- BootAgent takes on no long-term maintenance duty for a third-party config tool.

### Negative

- CC Switch users have to read an additional document.
- BootAgent has to maintain a config tool compatibility matrix.
- How a restart applies configuration cannot be made uniform across agents.

## Follow-up

- Add `verified_at`, `source_url`, and `supported_agents` metadata for each config tool.
- After every config tool upgrade, re-verify at least one representative agent.
- Before ever installing a third-party tool automatically, add signature verification,
  version pinning, and user confirmation.
