# Multimodal Skills: Design

Status: In progress. Phases 1 to 3 are implemented: detection; generation and install for Claude Code and Codex; native speech and async video on the routes the vendors document. Branch `codex/multimodal-skills`.

## Goal

A user's main Agent model is often text-only (DeepSeek, most coding models),
while the same Provider key can reach models that see images, generate images
or video, and transcribe or speak audio. BootAgent detects those models, writes
a Skill per capability that tells the Agent how to call them, installs the
Skills into the Agents the user picks, and gives the user a prompt to try each
one.

## Decisions

These were settled before this design and are not reopened here.

| Question | Decision |
| --- | --- |
| Detection | A built-in capability catalog first; a live probe only when the user asks for one. |
| Capabilities in v1 | Image understanding, image generation, speech-to-text, text-to-speech, video generation and video understanding. |
| Where the Skill gets the API key | An environment variable that BootAgent writes into the Agent's own configuration. No key in any Skill file. |
| "One-click install and use" | BootAgent installs the Skill through the existing Skill distribution; each Skill comes with example prompts the user copies into the Agent. |
| Agents without native Skills | Get the same files as a guide, plus a prompt that points the Agent at it. |

## Scope

In scope: the six capabilities above, for the built-in Providers listed under
Provider landscape and for custom Providers through OpenAI-compatible routes
only.

Out of scope for v1: image editing and inpainting, realtime audio, voice
cloning, music, a local loopback proxy, bundling ffmpeg or any other binary,
and choosing between several Providers for the same capability.

## Provider landscape

Endpoints differ by capability far more than chat does. Every route below was
read from the vendor's own documentation on 2026-10-09; the request path comes
from the page's API playground, since the Markdown exports omit it.

| Provider | Image understanding | Image / video generation | Audio |
| --- | --- | --- | --- |
| DeepSeek | `deepseek-flash` takes images on Chat Completions | None | None |
| Moonshot | `kimi-k3`, `kimi-k2.6` and the `kimi-k2.7-code` models take image and video input on Chat Completions | None | None |
| Novita | Vision chat models on the OpenAI route | `ming-image-0.1-design` on `POST /openai/v1/images/generations`. No video generation model is documented any more. | MiniMax Speech 2.8 Turbo and HD: synchronous `POST /v3/<model>`, asynchronous `POST /v3/async/<model>`. Fish Audio on `POST /v4beta/txt2speech`. No speech-to-text route is documented. |
| PPIO | Vision chat models on the OpenAI route | None documented. The unified video route (`/v3/video/create`) that was documented when this design began now returns 404 and is in neither the sitemap nor the docs index. | MiniMax Speech 2.8 Turbo and HD on the same paths as Novita |
| JieKou.AI | Vision chat models on the OpenAI route | GPT Image (`gpt-image-2`, `gpt-image-2-oai`, `gpt-image-2.5-flare-oai`, `gpt-image-2.5-sunburst-oai`) on `POST /openai/v1/images/generations`. Async video, for example `POST /v3/async/wan2.7-t2v` and `POST /v3/async/kling-v3.0-4k-t2v`, returning a `task_id`. | MiniMax Speech 2.8 Turbo and HD on the same paths as Novita |

All three gateways share one native API family. `GET /v3/async/task-result?task_id=`
answers with `task.status` (`TASK_STATUS_QUEUED`, `TASK_STATUS_PROCESSING`,
`TASK_STATUS_SUCCEED`, `TASK_STATUS_FAILED`), `task.reason`, `task.eta`, and
`images[]`, `videos[]` or `audios[]` whose `*_url` expires after `*_url_ttl`
seconds (3600 by default; the TTL is an integer for images and a string for video
and audio). The synchronous MiniMax speech route returns the audio as hex in
`data.audio` and reports refusals in `base_resp.status_code` under HTTP 200.

The catalog's `base_url` is the OpenAI-compatible route (for example
`https://api.novita.ai/openai`). Native async and audio routes sit at the host
root, so the modality catalog carries its own native base per Provider instead
of deriving it from `base_url`: `https://api.novita.ai`, `https://api.ppio.com`
and `https://api.highwayapi.ai`.

### What Phase 3 found and how it was handled

- Because the native API family is shared, the adapters are named after Novita,
  where it was first documented, and serve all three: `novita-audio` for the
  synchronous speech route, `novita-async` for submit, poll and download.
- `ppio-video` is reserved and not implemented: PPIO no longer documents a
  video route. The catalog marks video generation unsupported for PPIO and
  Novita, and the UI says so for each.
- JieKou.AI's generation endpoints, which the first draft could not find, are now
  documented, so its async video and GPT Image models are in the catalog.
- Speech-to-text has no native route on any of the three; it stays on the
  OpenAI-compatible route with a hand-entered model.
- JieKou.AI's GPT Image models are on the images route, not chat output, so they
  are catalog entries on `openai-images`. Gemini image output and chat audio
  input are still undocumented anywhere (JieKou.AI's Gemini page covers text and
  tools only), so `openai-chat-image-output` and `openai-chat-audio-input` stay
  uninstallable with no probe.
- The catalog lists only models whose request body was read: two video models,
  four GPT Image models, `ming-image-0.1-design` and the two MiniMax Speech
  models. A catalog entry may carry `options` (the request fields beyond the
  prompt it documents, from a closed vocabulary) and `recommended` (preselected
  in the UI, usually the cheaper of two).

## Architecture

| Area | Change |
| --- | --- |
| `manifests/modalities.lock.json`, `internal/catalog` | New embedded catalog: per Provider, the native base and model rules mapping a model to its capabilities and an adapter. |
| `internal/modality` (new) | Catalog matching, probes, and the adapter vocabulary shared by probes and the generated script. |
| `internal/provider/capability.go` | `ModelCapabilities` gains a `Modalities` map; schema version 2. |
| `internal/skill` | A generator that renders a Skill tree for one capability, Provider and model. |
| `internal/config` | Per-Agent writers for the Skill environment variables. |
| `internal/app` | Use cases: list a Provider's models with capabilities, probe one, install and uninstall capability Skills. |
| `internal/binding` | A `MultimodalService`; `frontend/bindings`, `frontend/src/backend/wails.ts` and `frontend/src/types/api.ts` regenerated and updated together. |
| `frontend/src/pages/SkillsPage.tsx` | A Multimodal capabilities section. |

## Detection

### What `/models` returns

The public `/models` listings of Novita, PPIO and JieKou.AI were fetched on
2026-10-09 (95, 66 and 189 models). DeepSeek and Moonshot require a key for the
listing, so they were not sampled.

- All three return the same schema, including `input_modalities`,
  `output_modalities`, `model_type` and `endpoints`. 207 of the 350 entries
  declare a non-text modality.
- Every entry has `model_type: "chat"`. Dedicated generation and audio models
  (Kling, Wan, Seedance, GLM TTS and ASR, Fish Audio) are not listed at all.
  `/models` can find understanding models and chat models that output images;
  it cannot find the async generation and audio models.
- Model IDs are largely shared: 16 multimodal IDs are byte-identical on all
  three, 23 between Novita and PPIO. Open models use `vendor/model`
  (`moonshotai/kimi-k3`); JieKou.AI lists closed models bare (`claude-opus-5-5`,
  `gemini-3.5-flash`). Gateways add their own suffixes (`-p`, `-dst`, `-z`, `-cc`,
  `-dd`, `-r`, `-as`, `-oai`).
- The calling convention for understanding is the same everywhere: Chat
  Completions with image or video content parts. Generation differs by vendor.
- Declared metadata is not always right. `openai/gpt-oss-120b` and
  `openai/gpt-oss-20b` are declared as taking images, which does not match the
  model. `zai-org/glm-5.3-flash` takes video on Novita and PPIO but not on
  JieKou.AI. Members of one family differ: `qwen/qwen3.7-plus` takes images,
  `qwen/qwen3.7-max` does not.

### Resolution order

For each model ID from `ListModels`, the first source that answers wins:

1. A probe verdict in the capability cache.
2. A catalog entry for the model, for the capabilities it names.
3. The model's declared `input_modalities` / `output_modalities`, when present.
4. An ID rule from the table below.
5. Unknown: not offered.

The catalog sits above the declaration, which the first draft did not have: a
catalog entry names a verified route, while a declaration only says a model
outputs images, not how a gateway returns them. `ming-image-0.1-design` is
declared with image output on Novita and is reached on the images route; the
declaration alone would have put it on the unverified chat route.

Async generation and audio models never appear in `/models`, so they come only
from the catalog's per-Provider generation entries, which also name the adapter.
An ID rule or declared metadata can establish that a model understands images or
video, or outputs images through chat; it cannot establish a generation
endpoint. A model found by an ID rule alone may be installed only after a
successful probe.

Two details the implementation settles:

- Probe verdicts answer per capability; declared metadata answers for the whole
  model. A model that declares modalities gets nothing more from an ID rule, so a
  Provider that declares a model text-only is believed over a rule that matches
  its name.
- Declared audio or video output on a chat model (`gpt-image-2.5-*`,
  `gemini-omni-flash`, `ming-image-*`, `qwen3.5-omni-flash`) maps to no
  capability. There is no chat route for text-to-speech or video generation, and
  how a gateway would return that output is unknown.

### Chat adapters

A capability found on a chat model uses one of four adapters. Two are verified
calling conventions; the other two are known only from declared metadata, and
their request and response format across gateways is not yet checked, so neither
a declaration nor an ID rule is enough to install them. They install only after a
probe passes, and each gets a probe once its format is verified (Phase 3).

| Capability | Adapter | Installable from a declaration |
| --- | --- | --- |
| Image understanding | `openai-chat-vision` | Yes |
| Video understanding (native) | `openai-chat-video` | Yes |
| Image generation via chat output | `openai-chat-image-output` | No, probe first. JieKou.AI's `gemini-*-image` and `gpt-image-*`, whose `endpoints` is empty |
| Speech-to-text via chat audio input | `openai-chat-audio-input` | No, probe first. `*-omni-*`, `gemini-*`, `mimo-v2.6-*` |

### ID rules

Each rule is a regular expression matched against the model ID lower-cased with
any `vendor/` prefix removed; gateway suffixes need no stripping because the
rules are anchored at the start. Hits are against the 2026-10-09 snapshot:
across all three listings the rules made no claim that contradicted the declared
metadata.

| Rule | Excludes | Capabilities | Hits |
| --- | --- | --- | --- |
| `^gpt-image-` | | Image output | 5 |
| `^gemini-.*-image` | | Image input and output | 8 |
| `^ming-image-` | | Image output | 3 |
| `(^\|-)omni(-\|$)` | `captioner` | Image and audio input | 3 |
| `^gemini-` | `-image` | Image, video and audio input | 15 |
| `^claude-` | | Image input | 29 |
| `^(gpt-4o\|gpt-4\.1\|gpt-5\|gpt-6\|o3)` | `^gpt-oss` | Image input | 38 |
| `^grok-(4\|code)` | | Image input | 12 |
| `^kimi-k(2\.[5-9]\|3)` | | Image and video input | 13 |
| `^deepseek-(v4\.[1-9]-flash\|v4-flash-vision)` | | Image input | 8 |
| `(^\|-)ocr(-\|$)` | | Image input | 4 |
| `(^\|-)vl(-\|$)`, `^glm-[\d.]+v` | | Image input | 12 |
| `^gemma-[34]-` | | Image input | 6 |
| `^llama-4-` | | Image input | 4 |
| `^minimax-m3` | | Image and video input | 3 |
| `^qwen3\.(5\|6\|8)-(\d+b\|flash\|max\|plus)` | | Image and video input | 26 |
| `^mimo-v2\.(5$\|6-)` | | Image and video input | 6 |

Three rules had to be narrowed after a first pass matched text-only models:
`deepseek-v4-flash` (only `v4.1` and the vision variant take images),
`qwen3.7-max` and `qwen3.8-2.4t-a95b` (text-only, hence the explicit Qwen
suffixes), and `mimo-v2.5-pro` (text-only, unlike `mimo-v2.5`).

The rules cover 194 of the 207 declared multimodal entries. The 13 left over are
covered by their declared metadata; adding rules for them is not worth it while
gateways declare modalities:
`glm-5.3-flash`, `step-3.7-flash`, `autoglm-phone-9b-multilingual`,
`ernie-5.0-thinking-exp`, `ernie-5.0-thinking-preview`, `qwen3.7-plus`,
`nova-2-Lite`, and the two `gpt-oss` entries, whose declaration is doubtful.

The rules matter most for Providers that declare nothing: DeepSeek and Moonshot
(not sampled here) and custom OpenAI-compatible endpoints.

### Catalog file

`modalities.lock.json` holds the ID rules, plus per Provider the native base URL
and the generation and audio models `/models` does not list:

```json
{
  "id_rules": [
    { "pattern": "^kimi-k(2\\.[5-9]|3)", "capabilities": ["vision", "video-understanding"] },
    { "pattern": "^gemini-", "exclude": "-image", "capabilities": ["vision", "video-understanding", "speech-to-text"] }
  ],
  "providers": {
    "novita": {
      "native_base_url": "https://api.novita.ai",
      "generation": [
        { "id": "kling-v3.0-4k-t2v", "capabilities": ["video-generation"], "adapter": "novita-async" }
      ]
    }
  }
}
```

Listing models is an unbilled `GET`, so opening the section may run it. Nothing
else is sent until the user acts.

### Probe

Probes run only on a click, following the rule already set for the Profile model
check: every billed request is the user's to send. Each probe reuses the
refused / key / transient / network classification from
`internal/app/profile_model_check.go`.

| Capability | Probe | Cost |
| --- | --- | --- |
| Image understanding | Chat request with a 32x32 PNG, `max_tokens: 1` | Minimal |
| Speech-to-text | 0.5 s of silent 16 kHz WAV to `/audio/transcriptions` | Minimal |
| Text-to-speech | The text `ok` to `/audio/speech`, voice `alloy` | Minimal |
| Image generation | One image from `/images/generations`, no size sent | One image; the button says so and a dialog asks first, and the backend refuses the probe without that confirmation |
| Text-to-speech (native) | The text `ok` to `POST <native base>/v3/<model>` with a documented system voice; the verdict is read from `base_resp.status_code` too | Minimal |
| Video generation | Not probed | A probe is a full billed generation that cannot be cancelled reliably, so this capability is catalog-only |
| Video understanding | Native video models: catalog-only. Frame sampling: inherits the image-understanding verdict | None |

Implementation notes:

- The probe image is 32x32 rather than the 8x8 first planned: some vision
  endpoints refuse images under 10 pixels a side, which would fail the probe for
  a reason unrelated to the model. It still costs a few tokens.
- The image probe sends no size. "The smallest size the adapter allows" is not
  knowable for a generic OpenAI-compatible route: valid sizes differ by model
  (`dall-e-2` allows 256x256, `gpt-image-1` does not), and a rejected size would
  fail the probe for a reason unrelated to the endpoint. The confirmation says it
  is one billed image.
- A chat probe keeps the 10 s client timeout; audio probes allow 60 s and the
  image probe 180 s, since producing one image can take a minute.
- The image probe ships in Phase 2, not Phase 3 as first planned: without it a
  hand-entered image model (below) could never be installed.

### Models entered by hand

`/models` does not list dedicated image, transcription or speech models, so
detection alone would leave image generation, speech-to-text and text-to-speech
with nothing to install on most Providers. The section therefore lets the user
type a model ID they know (`gpt-image-1`, `whisper-1`, `tts-1`) for those
capabilities and image understanding. The ID is probed on the capability's
OpenAI-compatible route (`openai-images`, `openai-audio-transcriptions`,
`openai-audio-speech`, `openai-chat-vision`), and installs only once that probe
passes. This is the user naming a model, not BootAgent guessing an endpoint; a
probe verdict for a model in no listing is still shown and installable.

### Cache

`~/.bootagent/capabilities.json` keeps probe verdicts under the existing
Provider fingerprint, so editing the endpoint or key discards them:

```json
{
  "schema_version": 2,
  "providers": {
    "novita": {
      "fingerprint": "…",
      "models": {
        "qwen/qwen3-vl-8b": {
          "probed_at": "…",
          "protocols": {},
          "modalities": {
            "vision": { "supported": true, "source": "probe", "adapter": "openai-chat-vision", "probed_at": "…" }
          }
        }
      }
    }
  }
}
```

Catalog matches and declared metadata are recomputed on every read and are not
cached; only probe verdicts are, and only conclusive ones: the Provider accepted
the request, or refused it for this model. A rejected key, a busy endpoint or a
network failure says nothing about the model and is not stored.

The store reads a version 1 file as version 2 with no modalities, and writes
version 2. Discarding version 1 would throw away the protocol verdicts and make
the next activation spend a billed probe to rebuild them. The file stays derived
data that may be deleted at any time.

## Generated Skill

One Skill per capability, bound to one Provider and model the user chose (the
catalog's recommended model preselected):

```text
bootagent-vision/
  SKILL.md
  config.json
  scripts/run.mjs
```

IDs: `bootagent-vision`, `bootagent-image-generation`, `bootagent-speech-to-text`,
`bootagent-text-to-speech`, `bootagent-video-generation`,
`bootagent-video-understanding`.

`SKILL.md` has the frontmatter Agents use to decide when to load a Skill, then:
when to use it, the exact command (`node <this Skill's directory>/scripts/run.mjs
<subcommand> …`) with every argument, where outputs are written, what each exit
code means and what to tell the user, and two rules: confirm with the user before
any generation that costs money, and never print environment variables. The
description is emitted as a quoted YAML scalar, so a Provider name with quotes
or a colon cannot break the frontmatter.

| Capability | Subcommand | Output |
| --- | --- | --- |
| Image understanding | `describe --image <path>… [--prompt] [--max-tokens]` | The answer on stdout |
| Image generation | `generate --prompt <text> [--size] [--n 1-4] [--out <dir>]` | Saved file paths on stdout |
| Speech-to-text | `transcribe --audio <path> [--language] [--prompt]` | The transcript on stdout |
| Text-to-speech | `speak --text <text> [--voice] [--format] [--out]` (native route: also `--speed`) | The saved file path on stdout |
| Video generation | `video --prompt <text> [options the model documents] [--out]`, and `video-result --task <id>` | The saved file path on stdout; `task: <id>` on stderr |

Every Skill also has `check`, which confirms the key is available without
sending a request. Exit codes: 0 success, 2 bad arguments or input, 3 setup
(Node older than 18, no key), 4 refused by the Provider, 5 key rejected, 6
unreachable, busy or timed out, 7 (video only) submitted but not finished in time.

Video polls the task with the Provider's `eta` as the pace, clamped to 1 to 15
s, until 10 minutes have passed by default. A busy or unreachable poll is
retried; a refused or failed task is final, since retrying spends money. A
timeout prints the task ID so `video-result` can collect it later without
submitting again, and `SKILL.md` tells the Agent never to retry on its own.
Results are streamed to disk as they download, without the key, and a partial
file is removed on failure. Outputs default to `bootagent-output/` in the
working directory and never overwrite an existing file. Result URLs are
downloaded without the key, because they may be on another host.

`config.json` is non-secret: Provider ID, adapter, model, base URLs and the
name of the environment variable that holds the key. The key itself is never in
the tree.

`scripts/run.mjs` is a dependency-free script for Node 18 or later (built-in
`fetch`). It implements the adapters (`openai-chat-vision`, `openai-images`,
`openai-audio-transcriptions`, `openai-audio-speech`, `openai-chat-video`,
`openai-chat-image-output`, `openai-chat-audio-input`, `novita-async`,
`novita-audio`, `ppio-video`), submits and polls async tasks with a timeout
(video: 10 minutes by default), and downloads results before their URLs expire.
Video understanding uses a native video model when the catalog names one;
otherwise it samples frames with `ffmpeg` if it is on `PATH` and sends them to
the image-understanding model, and reports the capability as unavailable when
`ffmpeg` is missing. Error output is redacted of the key.

Each Skill carries its own copy of the script. Skills are distributed as
self-contained trees through the existing hash, variant and backup machinery,
and a shared script outside the tree would bypass it.

Changing the model or Provider regenerates the tree. A new hash is a new variant,
published over the previous managed one by the install use case, which replaces
what is at the destination only when its hash is a stored variant of the same
Skill. It does not go through `ApplySkills`, contrary to the first draft:
`ApplySkills` takes a preview token the generator has no use for, and its
`verifyManagedPublishTarget` compares the destination with the *new* hash, so it
refuses to replace one managed variant with another. A Skill of the same ID that
BootAgent did not write is left alone, per Agent.

`"generated"` joins `agent`, `folder` and `zip` as an import source in the
registry. The import path still refuses it: only the generator writes it.

## Credential delivery

The variable name is `BOOTAGENT_<PROVIDER>_API_KEY`, with the Provider ID upper
cased and every character outside `[A-Z0-9]` replaced with `_`. Base URLs are not
secret and stay in `config.json`.

| Agent | Where the variable is written | Notes |
| --- | --- | --- |
| Claude Code | `env` in `~/.claude/settings.json` | Already a secret file under ADR-008; the Bash tool inherits `env`. |
| Codex | `[shell_environment_policy.set]` in `~/.codex/config.toml` | `set` is applied after Codex's default `KEY`/`SECRET`/`TOKEN` exclusion, so the variable survives it. The table is merged with the user's own entries and placed after the user's tables; inline-table and dotted-key spellings of it are refused rather than duplicated. Every writer of `config.toml` (Profile, MCP, Skill) writes it as a secret whenever the old or the new content holds a `BOOTAGENT_*` key, since the backup it takes is of the old content. |
| OpenCode | To be confirmed | How its shell tool receives configured environment variables is not yet checked. |
| Hermes | To be confirmed | Same as OpenCode. |

Fallback for an Agent with no configurable environment: a private
`~/.bootagent/skill-env/<provider>.env`, the same pattern ADR-008 uses for
Aider. `run.mjs` reads it only when the variable is not already set.

Lifecycle. One reconcile makes every Agent's `BOOTAGENT_*` variables exactly
what its generated Skills need, and runs after every change that can alter that:

- Install writes the variable for each target Agent.
- Saving a Provider with a new key rewrites it, on the same reapply path that
  already rewrites Agent configs (`SaveProvider` reports it in `reapplied`). A new
  endpoint also re-renders the Provider's Skills, since `config.json` carries the
  base URL.
- Uninstalling the last Skill that uses a Provider removes the variable. So does
  removing a generated Skill's target on the Skills page, or restoring a backup.
- Deleting a Provider that a generated Skill still uses is refused, as deleting
  one an Agent uses already is.
- A generated Skill reaching an Agent with no native writer (through the Skills
  page) gets the fallback file; the file is deleted, not backed up, when no Agent
  needs it.
- Only variants BootAgent rendered here earn a key. A `bootagent-*` tree imported
  from a ZIP can name any base URL, and writing the real key for it would hand
  the key to whoever wrote the ZIP.
- Every write follows the existing order: private directory, backup, temp file in
  the same directory, tighten permissions, atomic replace. A writer with nothing
  to write does not parse the file, so an unrelated problem in an Agent's config
  does not fail a Skill change.

Accepted risk: the key is visible to every command the Agent runs, so the Agent's
model can read it. That is the exposure Claude Code's own token already has under
ADR-008. The install confirmation states it. Because it extends ADR-008 to Codex
and to keys for Providers the Agent does not otherwise use, it is recorded in
[ADR-011](decisions/ADR-011-provider-keys-for-multimodal-skills.md).

## Install and prompts

The Skills page gets a Multimodal capabilities section:

1. Each Provider with a key, with the capabilities found and each one's source:
   declared by the Provider, ID rule, catalog, or probed.
2. Per capability, a model choice and, where the probe table allows, a
   Probe button.
3. Target Agents: those with `skills_path` in `agents.lock.json` and a native
   writer for the variable. In Phase 2 that is Claude Code and Codex; OpenCode
   and Hermes follow in Phase 4.
4. Install: a confirmation names the variable, states that the Agent and its
   model can read it, and notes the Node 18 requirement. Then the tree is
   generated, stored and published, the variables are written, and the result is
   shown per Agent with a reminder that running Agents need a restart.

Install re-checks the evidence on the backend rather than trusting the page: an
ID rule alone, or a declaration for an unverified chat adapter, is refused until
a probe passes. Install sends no billed request.

After install, each Skill shows two or three example prompts, in Chinese and
English, each with a copy button, for example "Look at ./screenshot.png and find
the error message in the UI" for image understanding. Uninstall reuses the
existing Skill uninstall (`SkillService.Uninstall`), which keeps a backup and now
also runs the reconcile, so no separate multimodal uninstall exists.

### Agents without Skills

Kilo CLI, Aider, OpenClaw, Kimi Code, pi, dsh and the desktop apps have no Skills
directory. BootAgent writes the same tree to `~/.bootagent/multimodal/<capability>/`
and the variables to the fallback env file, and shows a prompt such as
"Read ~/.bootagent/multimodal/vision/SKILL.md and use it to …". It does not edit
those Agents' instruction files.

## Security and privacy

- The key never enters the Skill tree, `config.json`, the Skill registry, logs,
  the capability cache, React state or test artifacts. Golden tests assert the
  absence of the key in every generated file.
- Inputs are read from local paths and sent only to the user's own Provider.
  Outputs are downloaded into the Agent's working directory; BootAgent uploads
  nothing anywhere else.
- BootAgent itself sends billed requests only on a click. Spending done later by
  the Agent through a Skill is governed by `SKILL.md`, which requires the Agent
  to confirm generation with the user first.
- No new network listener.

## Tests

- Resolution order and ID rules, replayed against the saved `/models` snapshot of
  each sampled gateway in `internal/modality/testdata/` (only `id`, the two
  modality lists, `model_type` and `endpoints` are kept): no rule may contradict
  declared metadata, and a model
  found by an ID rule alone is not installable before a probe.
- Capability cache schema migration from version 1 and fingerprint invalidation.
- Generator golden files per capability and adapter, with the no-key assertion.
- Environment writers per Agent against fixtures, including the Codex
  `shell_environment_policy` merge with an existing table and the secret write
  for `config.toml`.
- Probe classification through the existing fake HTTP doer.
- `run.mjs` driven against an `httptest` server from a Go test, skipped when
  `node` is not installed.
- Frontend: detection rendering, probe confirmation for billed probes, install
  results, prompt copy.

## Phases

1. Catalog, cache version 2, detection list, image-understanding probe.
2. Generator, install and env delivery for Claude Code and Codex: image
   understanding, image generation, speech-to-text and text-to-speech over the
   OpenAI-compatible adapters.
3. Native speech and async video on the routes Novita, PPIO and JieKou.AI
   document; catalog image models on the images route. PPIO video is not
   implemented (no documented route).
4. Video understanding; OpenCode, Hermes and the guide for Agents without Skills.

## Open questions

- Gemini image output and chat audio input need a documented request and
  response format before their adapters can be written. A native speech-to-text
  route, and PPIO and Novita video generation, need documenting by the vendors.
- How OpenCode and Hermes pass configured environment variables to their shell
  tools.
- Hermes is installed by its official script, so Node may be absent. If so, should
  `SKILL.md` point at BootAgent's managed Node runtime by absolute path, or should
  the Skill not be offered to Hermes?
- Whether the UI shows a price per call. The catalog has no pricing today; v1 shows
  a generic cost note only.

## Sources

- [DeepSeek: V4-Flash-Vision release](https://api-docs.deepseek.com/news/news260821/), [vision guide](https://api-docs.deepseek.com/guides/vision/), [updates](https://api-docs.deepseek.com/updates)
- [Kimi: vision models](https://platform.kimi.ai/docs/guide/use-kimi-vision-model), [API overview](https://platform.kimi.ai/docs/overview)
- [Novita: task result](https://docs.novita.ai/api-reference/model-apis-task-result), [MiniMax Speech 2.8 Turbo](https://docs.novita.ai/api-reference/model-apis-minimax-speech-2.8-turbo), [MiniMax Speech 2.8 Turbo async](https://docs.novita.ai/api-reference/model-apis-minimax-speech-2.8-turbo-async), [Ming Image](https://docs.novita.ai/api-reference/model-apis-ming-image-txt2img), [Fish Audio TTS](https://docs.novita.ai/api-reference/model-apis-fish-audio-text-to-speech)
- [PPIO: task result](https://ppio.com/docs/models/reference-get-async-task-result), [MiniMax Speech 2.8 Turbo](https://ppio.com/docs/models/reference-minimax-speech-2.8-turbo)
- [JieKou.AI: task result](https://docs.jiekou.ai/docs/models/reference-get-async-task-result), [Wan 2.7 text-to-video](https://docs.jiekou.ai/docs/models/reference-wan2.7-t2v), [Kling v3.0 4K text-to-video](https://docs.jiekou.ai/docs/models/reference-kling-v3.0-4k-t2v), [GPT Image](https://docs.jiekou.ai/docs/models/reference-gpt-image), [Gemini](https://docs.jiekou.ai/docs/providers/gemini), [MiniMax Speech 2.8 Turbo](https://docs.jiekou.ai/docs/models/reference-minimax-speech-2.8-turbo)
- [Codex: configuration reference](https://developers.openai.com/codex/config-reference)
