# ADR-004: Per-Agent Protocol Verification and the `PROTOCOL_UNSUPPORTED` Error Code

## Status

Accepted

## Date

2026-07-26

## Context

[ADR-003](ADR-003-three-platform-python-core-and-release-policy.md) froze the five
auto-configured Agents and their config adapter mapping, but the early connection
test only ever sent one kind of request: `POST <openai-base>/v1/chat/completions`.

That does not match what an Agent actually does once it is configured:

| Agent | Protocol written by the config |
| --- | --- |
| Codex | Responses (`installer.write_codex_config` hardcodes `wire_api = "responses"`) |
| Claude Code | Anthropic Messages |
| OpenCode, Kilo CLI, Aider | OpenAI-compatible |

The README had long stated that "the same model ID is not necessarily compatible
with the OpenAI, Anthropic, and Responses protocols at the same time", but that
constraint never reached any code path.

On 2026-07-26 we measured 36 text models on an OpenAI-compatible relay endpoint,
skipping 24 image, video, speech, and embedding models because calling them with a
chat payload can trigger a separately billed generation job:

| Protocol | Passed |
| --- | --- |
| Chat Completions | 31 / 36 |
| Anthropic Messages | 23 / 36 |
| Responses | 10 / 36 |

Of the 30 models that could be judged conclusively, only 10 support Responses. The
endpoint rejects the request in two explicit forms, both detectable:

- `400 INVALID_REQUEST_BODY`, with a message containing
  `does not support endpoint: responses`
- `500`, with a message containing `not implemented`

So the original implementation had a reproducible failure path: the user picks a
model that only supports Chat Completions, the connection test passes, BootAgent
writes the Codex config, Codex fails on its first request, and nothing in
BootAgent's output points at the root cause.

## Decision

### Protocol mapping

Each Agent's inference protocol is derived from the `config_adapter` field in
`agents.lock.json`. The mapping table lives in `internal/catalog` and uses the same
source as config writing, so the two cannot drift apart. An unregistered adapter
falls back to OpenAI-compatible.

### When verification runs

**Before** writing any config, `install_many` sends one minimal request for each
protocol involved in the selected Agents. The same protocol is probed only once.
`--skip-test` and `--check-agent-only` keep their original meaning and skip the
probe.

### Failure handling

When the probe finds a protocol incompatibility, that Agent fails with
`PROTOCOL_UNSUPPORTED` and **no config file and no environment summary are
written**. There is no "ignore the warning and write anyway" path: writing a config
that is guaranteed to fail only moves the error out of BootAgent and into the Agent,
where the readable error message is lost.

### Error code

`PROTOCOL_UNSUPPORTED` is added, with exit code `7`. This is an **additive
extension** of the [ADR-003](ADR-003-three-platform-python-core-and-release-policy.md)
error contract: no existing error code is modified or reused, and existing clients
behave exactly as before.

This error has `retryable = false`. Quota exhaustion (`429`), upstream overload
(`503`), and timeouts keep their existing retryable semantics -- a busy upstream is
not the same thing as a model that does not support the protocol.

### Local API

`POST /api/probe` accepts an optional `agents` array, probes each one according to
its protocol, and returns per-protocol results in the `protocols` field. When
`agents` is not supplied it keeps the original single-protocol OpenAI-compatible
behavior, which preserves compatibility for older clients.

## Consequences

- The user learns that a model is incompatible with an Agent before any config is
  written, instead of hitting a context-free error inside the Agent.
- The verdict for one model is independent per Agent: a model may be rejected by
  Codex and configure OpenCode successfully at the same time.
- Activating several Agents at once sends at most three probe requests instead of
  one.
- The list returned by `/v1/models` is still neither filtered nor annotated by
  protocol, so the user can still select a non-chat model. The probe is then
  guaranteed to fail, so no broken config is written, but the error message reads
  "this protocol is not supported" rather than "this is not a chat model". That
  problem is tracked separately.
- A formal Release Candidate must still gate on a real Agent's first request; a
  local probe is no substitute for formal acceptance against PPIO and Novita.
