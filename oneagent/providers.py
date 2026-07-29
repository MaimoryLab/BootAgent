from __future__ import annotations

import json
import re
import socket
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

from .catalog import (
    PROTOCOL_ANTHROPIC,
    PROTOCOL_OPENAI,
    PROTOCOL_RESPONSES,
    PROVIDERS,
    agent_protocol,
    fallback_probe_model,
)
from .errors import OneAgentError

__all__ = [
    "PROTOCOL_ANTHROPIC",
    "PROTOCOL_OPENAI",
    "PROTOCOL_RESPONSES",
    "agent_protocol",
    "anthropic_messages_url",
    "chat_probe",
    "list_models",
    "openai_base_url",
    "pick_chat_model",
    "protocol_label",
    "protocol_probe",
    "provider_base",
    "provider_config_base",
    "provider_home",
    "resolve_probe_model",
    "validate_base_url",
]


def validate_base_url(value: str) -> str:
    if not value:
        raise OneAgentError("INVALID_REQUEST", "Custom base URL is required")
    if any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise OneAgentError("INVALID_REQUEST", "Custom base URL contains control characters")
    parsed = urlparse(value)
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        raise OneAgentError("INVALID_REQUEST", "Custom base URL must start with http:// or https://")
    if parsed.username or parsed.password:
        raise OneAgentError("INVALID_REQUEST", "Custom base URL must not contain credentials")
    return value.rstrip("/")


def provider_base(provider: str, custom_base: str = "") -> str:
    if provider not in {*PROVIDERS, "custom"}:
        raise OneAgentError("INVALID_REQUEST", "Provider must be ppio, novita, or custom")
    if custom_base:
        return validate_base_url(custom_base)
    if provider == "custom":
        return validate_base_url(custom_base)
    return PROVIDERS[provider]["base_url"]


def provider_config_base(provider: str, custom_base: str, protocol: str) -> str:
    """The base URL a config write or probe targets for one inference protocol.

    Anthropic-speaking Agents are served from a separate route on the managed
    providers, so the decision is keyed on the protocol -- derived from the
    Agent's adapter by callers via agent_protocol() -- rather than on an Agent
    id. A custom base or a non-Anthropic protocol keeps the OpenAI-compatible
    URL.
    """
    base_url = provider_base(provider, custom_base)
    if protocol == PROTOCOL_ANTHROPIC and provider in PROVIDERS and not custom_base:
        return PROVIDERS[provider]["anthropic_base_url"]
    return base_url


PROTOCOL_LABELS = {
    PROTOCOL_OPENAI: "OpenAI Chat Completions",
    PROTOCOL_ANTHROPIC: "Anthropic Messages",
    PROTOCOL_RESPONSES: "OpenAI Responses",
}

# Endpoints answer "this model cannot serve this protocol" with several shapes:
# 400 INVALID_REQUEST_BODY "does not support endpoint", 500 "not implemented",
# and plain 404/405 when the route is absent. All are permanent for the pair, so
# they must not be reported as retryable.
_UNSUPPORTED_MARKERS = (
    "does not support endpoint",
    "not implemented",
    "unsupported endpoint",
    "unknown endpoint",
)


def protocol_label(protocol: str) -> str:
    return PROTOCOL_LABELS.get(protocol, protocol)


def provider_home(provider: str) -> str:
    if provider not in PROVIDERS:
        raise OneAgentError("INVALID_REQUEST", "Registration is only available for ppio or novita")
    return PROVIDERS[provider]["home"]


def openai_base_url(base_url: str) -> str:
    base = base_url.rstrip("/")
    for suffix in ("/chat/completions", "/responses", "/models"):
        if base.endswith(suffix):
            base = base[: -len(suffix)].rstrip("/")
    return base if base.endswith("/v1") else base + "/v1"


def anthropic_messages_url(base_url: str) -> str:
    """Anthropic bases arrive in both shapes: PPIO/Novita expose
    `.../anthropic` with no version segment, while a custom base is commonly
    pasted already ending in `/v1`. Normalise both to exactly one `/v1/messages`."""
    base = base_url.rstrip("/")
    for suffix in ("/v1/messages", "/messages"):
        if base.endswith(suffix):
            base = base[: -len(suffix)].rstrip("/")
            break
    if base.endswith("/v1"):
        return base + "/messages"
    return base + "/v1/messages"


def _request_error(exc: Exception, *, models: bool = False) -> dict[str, Any]:
    if isinstance(exc, HTTPError):
        if exc.code in {401, 403}:
            message = f"API key was rejected ({exc.code})."
            if models:
                message += " Enter model ID manually."
            return {
                "ok": False,
                "reachable": True,
                "models": [] if models else None,
                "status": exc.code,
                "message": message,
                "error_code": "API_KEY_REJECTED",
                "retryable": True,
            }
        if models and exc.code in {404, 405}:
            return {
                "ok": False,
                "reachable": True,
                "models": [],
                "status": exc.code,
                "message": f"This endpoint does not expose /v1/models ({exc.code}); enter model ID manually.",
                "error_code": "MODELS_UNSUPPORTED",
                "retryable": False,
            }
        return {
            "ok": False,
            "reachable": True,
            "models": [] if models else None,
            "status": exc.code,
            "message": f"Endpoint returned HTTP {exc.code}.",
            "error_code": "PROVIDER_UNREACHABLE",
            "retryable": exc.code >= 500,
        }
    reason = exc.reason if isinstance(exc, URLError) else str(exc)
    code = "TIMEOUT" if isinstance(reason, (socket.timeout, TimeoutError)) or "timed out" in str(reason).lower() else "PROVIDER_UNREACHABLE"
    return {
        "ok": False,
        "reachable": False,
        "models": [] if models else None,
        "status": 0,
        "message": f"Cannot reach endpoint: {reason}",
        "error_code": code,
        "retryable": True,
    }


def _protocol_request(
    protocol: str,
    *,
    provider: str,
    custom_base: str,
    api_key: str,
    model: str,
) -> Request:
    headers = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
    if protocol == PROTOCOL_ANTHROPIC:
        url = anthropic_messages_url(provider_config_base(provider, custom_base, protocol))
        headers["X-Api-Key"] = api_key
        headers["Anthropic-Version"] = "2023-06-01"
        body: dict[str, Any] = {
            "model": model,
            "messages": [{"role": "user", "content": "ping"}],
            "max_tokens": 1,
        }
    else:
        v1 = openai_base_url(provider_base(provider, custom_base))
        if protocol == PROTOCOL_RESPONSES:
            url = v1 + "/responses"
            # Reasoning models spend the budget before emitting text, so an
            # over-tight cap fails for a reason unrelated to protocol support.
            body = {"model": model, "input": "ping", "max_output_tokens": 16}
        else:
            url = v1 + "/chat/completions"
            body = {
                "model": model,
                "messages": [{"role": "user", "content": "ping"}],
                "max_tokens": 1,
            }
    return Request(url, data=json.dumps(body).encode(), method="POST", headers=headers)


def _unsupported_protocol(code: int, body: str) -> bool:
    if code in {404, 405, 501}:
        return True
    if code in {400, 422, 500}:
        lowered = body.lower()
        return any(marker in lowered for marker in _UNSUPPORTED_MARKERS)
    return False


def protocol_probe(
    *,
    protocol: str,
    provider: str,
    api_key: str,
    model: str,
    custom_base: str = "",
    timeout: float = 10,
) -> dict[str, Any]:
    """Send one minimal request over `protocol` and report whether the model
    actually serves it. Passing Chat Completions does not prove a model answers
    Responses or Anthropic Messages, so each Agent must be checked separately."""
    if not api_key:
        raise OneAgentError("INVALID_REQUEST", "API key is required")
    if protocol not in PROTOCOL_LABELS:
        raise OneAgentError("INVALID_REQUEST", f"Unknown inference protocol: {protocol}")
    request = _protocol_request(
        protocol,
        provider=provider,
        custom_base=custom_base,
        api_key=api_key,
        model=model or fallback_probe_model(provider),
    )
    label = protocol_label(protocol)
    try:
        with urlopen(request, timeout=timeout) as response:
            return {
                "ok": response.status in {200, 204},
                "reachable": True,
                "protocol": protocol,
                "status": response.status,
                "message": f"{label} connection test passed.",
                "error_code": None,
                "retryable": False,
            }
    except (HTTPError, URLError, TimeoutError, socket.timeout) as exc:
        try:
            if isinstance(exc, HTTPError):
                try:
                    body = exc.read().decode(errors="replace")
                except OSError:
                    body = ""
                if _unsupported_protocol(exc.code, body):
                    return {
                        "ok": False,
                        "reachable": True,
                        "protocol": protocol,
                        "status": exc.code,
                        "message": (
                            f"Model {model!r} does not support {label}. "
                            "Choose a model that serves this protocol."
                        ),
                        "error_code": "PROTOCOL_UNSUPPORTED",
                        "retryable": False,
                    }
            result = _request_error(exc)
            result.pop("models", None)
            result["protocol"] = protocol
            return result
        finally:
            if isinstance(exc, HTTPError):
                exc.close()


def chat_probe(
    *,
    provider: str,
    api_key: str,
    model: str,
    custom_base: str = "",
    timeout: float = 10,
) -> dict[str, Any]:
    return protocol_probe(
        protocol=PROTOCOL_OPENAI,
        provider=provider,
        api_key=api_key,
        model=model,
        custom_base=custom_base,
        timeout=timeout,
    )


def list_models(
    *,
    provider: str,
    api_key: str,
    custom_base: str = "",
    timeout: float = 10,
) -> dict[str, Any]:
    if not api_key:
        raise OneAgentError("INVALID_REQUEST", "API key is required")
    base_url = provider_base(provider, custom_base)
    request = Request(
        openai_base_url(base_url) + "/models",
        method="GET",
        headers={"Authorization": f"Bearer {api_key}"},
    )
    try:
        with urlopen(request, timeout=timeout) as response:
            raw = json.loads(response.read().decode())
    except (HTTPError, URLError, TimeoutError, socket.timeout) as exc:
        try:
            return _request_error(exc, models=True)
        finally:
            if isinstance(exc, HTTPError):
                exc.close()
    except (UnicodeDecodeError, json.JSONDecodeError):
        # The parser's own wording is left out. json reports a position rather
        # than the content, so nothing leaked -- but Go's decoder words it
        # differently and quotes the offending byte, so keeping the detail would
        # either be a permanent difference between the two implementations or a
        # message that republishes part of a body from an endpoint the user named.
        return {
            "ok": False,
            "reachable": True,
            "models": [],
            "status": 200,
            "message": "Model list response is not valid JSON.",
            "error_code": "MODELS_UNSUPPORTED",
            "retryable": False,
        }

    data = raw if isinstance(raw, list) else raw.get("data", []) if isinstance(raw, dict) else []
    if not isinstance(data, list):
        data = []
    models: list[str] = []
    for item in data:
        if isinstance(item, dict) and item.get("id"):
            models.append(str(item["id"]))
        elif isinstance(item, str):
            models.append(item)
    return {
        "ok": bool(models),
        "reachable": True,
        "models": models,
        "status": 200,
        "message": f"Found {len(models)} models." if models else "No model IDs returned; enter model ID manually.",
        "error_code": None if models else "MODELS_UNSUPPORTED",
        "retryable": False,
    }


# Model lists are provider-ordered and frequently lead with embedding, rerank
# or vision models. Probing a protocol endpoint with one of those fails for a
# reason unrelated to connectivity or the key, so pick the first ID that does
# not carry an obvious non-chat marker. Markers match on separators only, so
# an ID that merely contains a marker substring stays eligible.
_NON_CHAT_MODEL = re.compile(
    r"(?:^|[-_/.])(?:embed(?:ding)?s?|rerank(?:er)?|ocr|whisper|asr|tts|speech|vl|vision|image|sdx?|flux|guard(?:rail)?s?|moderation|sql)(?:[-_/.]|$)",
    re.IGNORECASE,
)


def pick_chat_model(models: list[str]) -> str:
    """Pick the first model that plausibly serves chat.

    `models` must be non-empty; callers only invoke this on a successful
    discovery. When every listed model looks non-chat the first entry is
    returned and the probe failure explains itself.
    """
    for model in models:
        if not _NON_CHAT_MODEL.search(model):
            return model
    return models[0]


def resolve_probe_model(
    *,
    provider: str,
    api_key: str,
    model: str = "",
    custom_base: str = "",
    timeout: float = 10,
) -> str:
    """The model to probe with when the user has not chosen one.

    Prefer a model the endpoint lists right now: hardcoded IDs go stale when
    a provider retires a model. A user-supplied model short-circuits
    discovery so a probe never costs an extra round trip, and an absent key
    skips it because list_models would refuse. Discovery failing is not
    fatal -- the catalog fallback is the narrow last resort.
    """
    if model:
        return model
    if not api_key:
        return fallback_probe_model(provider)
    listing = list_models(provider=provider, api_key=api_key, custom_base=custom_base, timeout=timeout)
    models = listing.get("models") or []
    if listing.get("ok") and models:
        return pick_chat_model(models)
    return fallback_probe_model(provider)
