#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import socket
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from oneagent.providers import PROTOCOL_ANTHROPIC, openai_base_url, provider_base, provider_config_base


FORMAL_RC_PROVIDERS = ("ppio", "novita")
LOCAL_SMOKE_PROFILES = {
    "apiproxy": {
        "openai_base": "https://apiproxy.paigod.work",
        "anthropic_base": "https://apiproxy.paigod.work",
        "openai_model": "openai/gpt-5.6-terra",
        "anthropic_model": "openai/gpt-5.6-terra",
        "responses_model": "openai/gpt-5.6-terra",
    }
}


@dataclass(frozen=True)
class ProviderSmokeConfig:
    provider: str
    api_key: str
    openai_model: str
    anthropic_model: str
    responses_model: str
    openai_base: str = ""
    anthropic_base: str = ""


def _request(label: str, request: Request, timeout: float) -> int:
    try:
        with urlopen(request, timeout=timeout) as response:
            if response.status < 200 or response.status >= 300:
                raise RuntimeError(f"{label}: unexpected HTTP {response.status}")
            response.read()
            return response.status
    except HTTPError as exc:
        try:
            raise RuntimeError(f"{label}: HTTP {exc.code}") from exc
        finally:
            exc.close()
    except (URLError, TimeoutError, socket.timeout) as exc:
        reason = exc.reason if isinstance(exc, URLError) else exc
        raise RuntimeError(f"{label}: request failed: {reason}") from exc


def _json_request(url: str, api_key: str, body: dict[str, Any], *, anthropic: bool = False) -> Request:
    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }
    if anthropic:
        headers["X-Api-Key"] = api_key
        headers["Anthropic-Version"] = "2023-06-01"
    return Request(url, data=json.dumps(body).encode(), method="POST", headers=headers)


def smoke_provider(config: ProviderSmokeConfig, *, timeout: float = 30) -> dict[str, int]:
    if not all([config.api_key, config.openai_model, config.anthropic_model, config.responses_model]):
        raise RuntimeError(f"{config.provider}: API key and all three protocol model IDs are required")
    openai = config.openai_base or provider_base(config.provider)
    anthropic = config.anthropic_base or provider_config_base(config.provider, "", PROTOCOL_ANTHROPIC)
    v1 = openai_base_url(openai)
    headers = {"Authorization": f"Bearer {config.api_key}"}
    results = {
        "models": _request("models", Request(v1 + "/models", method="GET", headers=headers), timeout),
        "chat": _request(
            "chat completions",
            _json_request(
                v1 + "/chat/completions",
                config.api_key,
                {
                    "model": config.openai_model,
                    "messages": [{"role": "user", "content": "Reply with OK."}],
                    "max_tokens": 8,
                },
            ),
            timeout,
        ),
        "responses": _request(
            "responses",
            _json_request(
                v1 + "/responses",
                config.api_key,
                {"model": config.responses_model, "input": "Reply with OK.", "max_output_tokens": 8},
            ),
            timeout,
        ),
        "anthropic": _request(
            "anthropic messages",
            _json_request(
                anthropic.rstrip("/") + "/v1/messages",
                config.api_key,
                {
                    "model": config.anthropic_model,
                    "messages": [{"role": "user", "content": "Reply with OK."}],
                    "max_tokens": 8,
                },
                anthropic=True,
            ),
            timeout,
        ),
    }
    return results


def load_api_key_json(path: Path, field: str) -> str:
    try:
        payload = json.loads(path.expanduser().read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"Cannot read API key JSON file: {exc}") from exc
    if not isinstance(payload, dict):
        raise RuntimeError("API key JSON file must contain an object")
    api_key = payload.get(field)
    if not isinstance(api_key, str) or not api_key.strip():
        raise RuntimeError(f"API key JSON field {field!r} is missing or empty")
    return api_key


def config_from_env(provider: str, *, api_key_override: str = "") -> ProviderSmokeConfig:
    prefix = provider.upper()
    defaults = LOCAL_SMOKE_PROFILES.get(provider, {})
    return ProviderSmokeConfig(
        provider=provider,
        api_key=api_key_override or os.environ.get(f"ONEAGENT_{prefix}_API_KEY", ""),
        openai_model=os.environ.get(f"ONEAGENT_{prefix}_OPENAI_MODEL", "")
        or defaults.get("openai_model", ""),
        anthropic_model=os.environ.get(f"ONEAGENT_{prefix}_ANTHROPIC_MODEL", "")
        or defaults.get("anthropic_model", ""),
        responses_model=os.environ.get(f"ONEAGENT_{prefix}_RESPONSES_MODEL", "")
        or defaults.get("responses_model", ""),
        openai_base=os.environ.get(f"ONEAGENT_{prefix}_OPENAI_BASE", "")
        or defaults.get("openai_base", ""),
        anthropic_base=os.environ.get(f"ONEAGENT_{prefix}_ANTHROPIC_BASE", "")
        or defaults.get("anthropic_base", ""),
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Run low-token Provider protocol checks without logging secrets")
    parser.add_argument(
        "--provider",
        choices=[*FORMAL_RC_PROVIDERS, *LOCAL_SMOKE_PROFILES, "all"],
        default="all",
    )
    parser.add_argument(
        "--api-key-json",
        type=Path,
        help="Read the API key from a local JSON file without putting it on the command line",
    )
    parser.add_argument("--api-key-field", default="OPENAI_API_KEY")
    parser.add_argument("--timeout", type=float, default=30)
    args = parser.parse_args()
    if args.api_key_json and args.provider == "all":
        parser.error("--api-key-json requires one explicit provider")
    try:
        api_key_override = load_api_key_json(args.api_key_json, args.api_key_field) if args.api_key_json else ""
    except RuntimeError as exc:
        parser.error(str(exc))
    providers = list(FORMAL_RC_PROVIDERS) if args.provider == "all" else [args.provider]
    for provider in providers:
        results = smoke_provider(
            config_from_env(provider, api_key_override=api_key_override),
            timeout=args.timeout,
        )
        summary = ", ".join(f"{name}=HTTP {status}" for name, status in results.items())
        print(f"{provider}: {summary}")


if __name__ == "__main__":
    main()
