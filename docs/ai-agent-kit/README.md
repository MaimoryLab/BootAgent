# AI Agent Kit

Documentation for setting up an AI Agent environment with BootAgent — for individual
developers, teams, and organizations that redistribute the kit.

**English** · [简体中文](./zh/README.md)

The English translation is in progress. Pages that are not translated yet link to the
Chinese original, which is complete and current.

## Reading order

1. [Start here](./en/00-start-here.md)
2. [PPIO account and Provider setup](./en/01-ppio-account.md)
3. [Create and store an API key](./en/02-api-key.md)
4. [Choosing a config tool](./en/03-config-tools.md)
5. [Agent categories and install guides](./en/04-agent-guides.md)
6. [Verify your first request](./en/05-first-request.md)

If you use CC Switch, also read [Configuring PPIO in CC Switch](./en/tools/cc-switch.md).

Distributors: see the [kit manifest](./en/manifest.md) for what a release package
contains and what must never be bundled.

## Scope

PPIO is used as the running example. BootAgent also ships **Novita** as a built-in
Provider and supports any custom OpenAI-compatible endpoint. The setup steps are the
same for all three — only the base URL and where the key comes from differ. The source
of truth for built-in Providers is `providers.lock.json` at the repository root.

The product boundary is defined by the
[product boundary baseline](../product-boundary-baseline.md). A distributing
organization may offer its own redemption codes or onboarding material outside this
project, but that does not change BootAgent's core flow.
