from __future__ import annotations

import json
import socket
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

from .catalog import PROVIDERS
from .errors import OneAgentError


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


def provider_config_base(provider: str, custom_base: str, adapter: str) -> str:
    base_url = provider_base(provider, custom_base)
    if adapter == "claude-code" and provider in PROVIDERS and not custom_base:
        return PROVIDERS[provider]["anthropic_base_url"]
    return base_url


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


def chat_probe(
    *,
    provider: str,
    api_key: str,
    model: str,
    custom_base: str = "",
    timeout: float = 10,
) -> dict[str, Any]:
    if not api_key:
        raise OneAgentError("INVALID_REQUEST", "API key is required")
    base_url = provider_base(provider, custom_base)
    url = openai_base_url(base_url) + "/chat/completions"
    body = json.dumps(
        {
            "model": model or "gpt-4.1",
            "messages": [{"role": "user", "content": "ping"}],
            "max_tokens": 1,
        }
    ).encode()
    request = Request(
        url,
        data=body,
        method="POST",
        headers={"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"},
    )
    try:
        with urlopen(request, timeout=timeout) as response:
            return {
                "ok": response.status in {200, 204},
                "reachable": True,
                "status": response.status,
                "message": "Connection test passed.",
                "error_code": None,
                "retryable": False,
            }
    except (HTTPError, URLError, TimeoutError, socket.timeout) as exc:
        try:
            result = _request_error(exc)
            result.pop("models", None)
            return result
        finally:
            if isinstance(exc, HTTPError):
                exc.close()


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
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        return {
            "ok": False,
            "reachable": True,
            "models": [],
            "status": 200,
            "message": f"Model list response is not valid JSON: {exc}",
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
