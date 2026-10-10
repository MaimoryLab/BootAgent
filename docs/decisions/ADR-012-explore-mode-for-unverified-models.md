# ADR-012: Explore Mode for Models BootAgent Cannot Call

## Status

Accepted

## Date

2026-10-10

- Related: [ADR-011](ADR-011-provider-keys-for-multimodal-skills.md), whose key
  delivery explore Skills use unchanged
- Design: [Multimodal Skills](../multimodal-skills-design.md#explore-mode)

## Context

A generated Skill is installed only on evidence: a Provider's declaration, the
catalog, or a probe that passed. An ID rule alone is never enough, because a
Skill that calls a model the wrong way fails only once the Agent uses it.

That rule leaves user-added Providers with almost nothing. Their `/models`
listings rarely declare modalities, so every capability is a guess from the
model name, and some of their routes are not the OpenAI-compatible ones
BootAgent can probe. A gateway that serves image generation at `/v3/<model>`,
for example, can never pass the `openai-images` probe, so its image models stay
"not installable yet" whatever the user does. The user often knows, or can find
out, how the model is called; BootAgent cannot.

## Decision

The user may install any capability in explore mode, on any model a keyed
Provider lists or the user types. Explore mode claims nothing about the model:

- The Skill says BootAgent does not know how the model is called. It tells the
  Agent to read its notes first, then ask the user for documentation or an
  example, then read the Provider's public documentation, and to stop and ask
  rather than guess by sending requests. It asks the user before any request
  that may be billed, and allows one request at a time until one has worked.
- The script sends only the requests the Agent composes, and only to the origin
  of the Provider's saved base URL. It adds the key itself in the way the Agent
  names (`Authorization: Bearer`, a named header, or none), refuses headers that
  carry credentials, and never follows a redirect, so the key cannot be sent to
  another host. Its output is redacted like every other Skill's; media in a
  response is saved to `~/.bootagent/output` and printed as a path.
- What the Agent learns is kept in `~/.bootagent/skill-notes/<provider>-<capability>.md`,
  outside the Skill tree, so writing it never makes the managed tree look
  edited, and it survives a reinstall.
- `explore` is not in the catalog's adapter vocabulary. The catalog cannot name
  it and nothing is probed over it; only the user's choice installs it, and the
  UI marks such Skills.

## Alternatives Considered

- **Relax the evidence rule for user-added Providers.** Installs a Skill that
  calls the model the standard way, which is exactly what fails for the
  gateways that motivated this. It also hides the uncertainty from the Agent.
- **Let the Agent call the Provider with `curl`.** The Agent would need the key
  in its command lines, where it reaches logs and transcripts, and nothing would
  stop documentation or a page the Agent read from steering the key to another
  host.
- **Ask the user for the route in BootAgent and write a dedicated adapter.**
  Needs a request and response schema language in the UI, and still fails on
  the first detail the user gets wrong. The Agent can read documentation and
  iterate with the user; BootAgent cannot.

## Consequences

- Whether an explore Skill works depends on the Agent and the documentation.
  BootAgent guarantees only that the key stays on the Provider's origin and that
  spending is asked for.
- The Agent composes request bodies, so it can call any route on that origin
  the key allows, not just the one capability. The origin lock and the consent
  rule bound this; the key was already visible to the Agent's commands under
  ADR-011.
- Notes are not removed on uninstall. They hold no key (the script strips
  key-shaped text before saving) and describe the user's own Provider.
