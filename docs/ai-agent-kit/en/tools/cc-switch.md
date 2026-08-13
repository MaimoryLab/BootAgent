# Configuring PPIO in CC Switch

[简体中文](../../zh/tools/cc-switch.md) · **English**

> This guide treats CC Switch as a local profile-switching tool. Its interface, supported
> agents, and configuration fields may change between versions — follow its official
> project page for installation and updates.

## 1. Get CC Switch

Reach the download and install instructions through CC Switch's official GitHub
repository or official project website. Do not use unofficial repackaged builds.

Once installed, open CC Switch and confirm it starts correctly before adding a PPIO
profile.

## 2. Create a PPIO profile

Create a new Provider or profile in CC Switch, with these fields:

```text
Name:     PPIO
Base URL: https://api.ppio.com/openai
API Key:  your PPIO API key
Model:    a model ID returned by PPIO /v1/models
```

Whether CC Switch labels the field `Endpoint`, `API Base`, or `Base URL`, enter only the
base address. Do not put the full `/v1/chat/completions` path into the base URL.

## 3. Filling in the model

Prefer the result of BootAgent's "fetch model list". For OpenAI-compatible configuration
the model ID is the raw ID the server returned — do not add an `openai/` or `ppio/`
prefix yourself unless the target agent's official documentation requires it.

If CC Switch offers a model test, verify with a minimal request. Do not paste private
code, personal data, or long text into the test input.

## 4. What takes effect after switching

After switching profiles:

1. Close the running agent terminal or application.
2. Reopen the agent.
3. Check which model and Provider the agent now reports.
4. Send one minimal request.

Agents differ in how configuration takes effect. Do not conclude that an agent has
reloaded its configuration just because CC Switch says the profile was switched.

## 5. How this relates to BootAgent

Recommended order:

```text
BootAgent probes PPIO
→ BootAgent fetches the model list
→ BootAgent verifies one request
→ CC Switch stores the same PPIO profile
→ You switch profiles per project
```

That way, when something breaks, you can return to BootAgent's built-in configuration to
tell whether PPIO itself is working before questioning the CC Switch profile.

## 6. Common mistakes

### Putting the full request path in the base URL

Wrong:

```text
https://api.ppio.com/openai/v1/chat/completions
```

Right:

```text
https://api.ppio.com/openai
```

### Using a model ID that is not a PPIO model

Fetch the current model list through BootAgent, confirm the model ID, and then copy it into
CC Switch.

### The agent still uses the old configuration after switching

Close and restart the agent. Open a new terminal if needed, so the new environment
variables apply.

### Sending your key to someone for debugging

Never send the full key. Send only the HTTP status code, whether the model ID exists,
whether the base URL is correct, and redacted logs.

## 7. Version verification

A release package should record:

```text
Tool name:   CC Switch
Tool source: https://github.com/farion1231/cc-switch
Verified on: 2026-07-21
Verified:    can create a profile, store the PPIO base URL, and complete a minimal
             request after switching
```

After every CC Switch upgrade, re-verify at least one representative agent among Claude
Code, Codex, and OpenCode.

## Official references

- CC Switch official GitHub repository: <https://github.com/farion1231/cc-switch>
- CC Switch official project website: <https://ccswitch.io/>
