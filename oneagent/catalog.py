from __future__ import annotations

import json
import os
import platform
import sys
from pathlib import Path
from typing import Any

from .errors import OneAgentError


PROVIDERS = {
    "ppio": {
        "name": "PPIO",
        "home": "https://ppio.com/",
        "base_url": "https://api.ppio.com/openai",
        "anthropic_base_url": "https://api.ppio.com/anthropic",
        # Last-resort probe model, used only when the endpoint's model list
        # cannot be fetched (resolve_probe_model normally picks a live ID).
        # It must still exist on this provider; staleness degrades only this
        # narrow fallback path. The IDs differ per provider (PPIO publishes
        # deepseek-v3, Novita deepseek_v3), so this cannot be one shared
        # constant.
        "fallback_probe_model": "deepseek/deepseek-v3",
    },
    "novita": {
        "name": "Novita",
        "home": "https://novita.ai/",
        "base_url": "https://api.novita.ai/openai",
        "anthropic_base_url": "https://api.novita.ai/anthropic",
        "fallback_probe_model": "deepseek/deepseek_v3",
    },
}


def fallback_probe_model(provider: str) -> str:
    """Last-resort probe model when the endpoint's model list is unavailable.

    Normal probes resolve a live model through resolve_probe_model(); this
    covers only the narrow path where discovery fails. Custom endpoints get
    the most widely published ID as a best guess; the user can always run the
    probe again after selecting a real model.
    """
    meta = PROVIDERS.get(provider)
    if meta:
        return str(meta["fallback_probe_model"])
    return "deepseek/deepseek-v3"

AGENT_GROUPS = [
    {"id": "auto", "name": "One-click configurable"},
    {"id": "gateway", "name": "Gateway agents"},
    {"id": "platform", "name": "Official account agents"},
    {"id": "ide", "name": "IDE extensions"},
]

PROTOCOL_OPENAI = "openai"
PROTOCOL_ANTHROPIC = "anthropic"
PROTOCOL_RESPONSES = "responses"

# Which inference protocol each Agent speaks once configured. A model ID that
# answers one protocol is not guaranteed to answer the others, so this drives
# both the connection test and the config write.
ADAPTER_PROTOCOLS = {
    "codex": PROTOCOL_RESPONSES,
    "claude-code": PROTOCOL_ANTHROPIC,
    "opencode": PROTOCOL_OPENAI,
    "kilo-cli": PROTOCOL_OPENAI,
    "aider": PROTOCOL_OPENAI,
}


def agent_protocol(adapter: str) -> str:
    return ADAPTER_PROTOCOLS.get(adapter, PROTOCOL_OPENAI)


# Package registries a user may install from when the official one is not
# reachable. The product boundary allows an authorised mirror (priority 2 in the
# software-acquisition policy) provided it carries a licence, a pinned version,
# a checksum and the upstream address -- so each entry records its upstream, and
# install_locked_agent verifies the manifest's integrity value against whatever
# the mirror served.
#
# This changes where a package is fetched from, not how the user reaches the
# network: OneAgent opens no tunnel and forwards no traffic, which is what
# separates it from the proxying the boundary forbids. Nothing here may point at
# storage OneAgent operates -- redistributing a proprietary Agent needs a
# licence that pointing at a public read-only mirror does not.
OFFICIAL_NPM_REGISTRY = "https://registry.npmjs.org/"

PACKAGE_MIRRORS = {
    "official": {
        "name": "官方源",
        "registry": OFFICIAL_NPM_REGISTRY,
        "upstream": OFFICIAL_NPM_REGISTRY,
        "note": "npm 官方 registry，默认使用。",
    },
    "npmmirror": {
        "name": "npmmirror（阿里云）",
        "registry": "https://registry.npmmirror.com/",
        "upstream": OFFICIAL_NPM_REGISTRY,
        "note": "官方源的公开只读镜像，包体与校验值均与官方一致；官方源不可达时可用。",
    },
}


def public_mirrors() -> list[dict[str, str]]:
    """Mirror choices for the UI, upstream included so the origin is visible."""
    return [
        {
            "id": mirror_id,
            "name": str(meta["name"]),
            "registry": str(meta["registry"]),
            "upstream": str(meta["upstream"]),
            "note": str(meta["note"]),
        }
        for mirror_id, meta in PACKAGE_MIRRORS.items()
    ]


def resource_root() -> Path:
    """Locate agents.lock.json and the built frontend across all three layouts.

    A source checkout keeps them beside the package, PyInstaller unpacks them
    into _MEIPASS, and a wheel carries them inside the package because only
    files under the package directory survive installation. The checkout is
    checked first so a stale staging directory left behind by a local wheel
    build can never shadow the real manifest.
    """
    bundle_root = getattr(sys, "_MEIPASS", None)
    if bundle_root:
        return Path(bundle_root)
    module_dir = Path(__file__).resolve().parent
    checkout = module_dir.parent
    if (checkout / "agents.lock.json").is_file():
        return checkout
    return module_dir / "_resources"


def load_manifest(path: Path | None = None) -> dict[str, Any]:
    manifest_path = path or resource_root() / "agents.lock.json"
    try:
        data = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise OneAgentError("INVALID_REQUEST", f"Cannot load Agent lock manifest: {exc}") from exc
    if data.get("schema_version") != 1 or not isinstance(data.get("agents"), dict):
        raise OneAgentError("INVALID_REQUEST", "Unsupported Agent lock manifest schema")
    return data


def agent_catalog() -> dict[str, dict[str, Any]]:
    return load_manifest()["agents"]


def public_providers() -> dict[str, dict[str, str]]:
    """Provider fields the API exposes.

    PROVIDERS also carries internals such as the fallback probe model. Sending
    the constant wholesale leaked those into /api/status the moment one was
    added, so project the public fields explicitly instead.
    """
    public: dict[str, dict[str, str]] = {}
    for provider_id, meta in PROVIDERS.items():
        entry = {"name": str(meta["name"]), "home": str(meta["home"]), "base_url": str(meta["base_url"])}
        anthropic = meta.get("anthropic_base_url")
        if anthropic:
            entry["anthropic_base_url"] = str(anthropic)
        public[provider_id] = entry
    return public


def public_catalog() -> list[dict[str, object]]:
    items = []
    for agent_id, meta in agent_catalog().items():
        items.append(
            {
                "id": agent_id,
                "name": meta["name"],
                "group": meta["group"],
                "configMode": meta["config_mode"],
                "guideOnly": meta["config_mode"] == "guide",
                "lockedVersion": (meta.get("package") or {}).get("version"),
                "protocol": (
                    agent_protocol(str(meta.get("config_adapter") or ""))
                    if meta["config_mode"] == "auto"
                    else None
                ),
                "platforms": meta.get("platforms", []),
                "platformNote": meta.get("windows_note", "") if current_platform()["os"] == "windows" else "",
                # How prominently to show it, independent of whether OneAgent can
                # install it: an overview is judged by whether the tools people
                # actually use are on it.
                "rank": meta.get("rank", 99),
            }
        )
    # Sorted here so every client shows the same order without re-deriving it.
    return sorted(items, key=lambda item: (item["rank"], str(item["id"])))


def current_platform() -> dict[str, str]:
    system = platform.system().lower()
    if system == "darwin":
        os_id = "macos"
    elif system == "windows":
        os_id = "windows"
    else:
        os_id = "linux"
    machine = platform.machine().lower()
    arch = "arm64" if machine in {"arm64", "aarch64"} else "x64"
    shell = "powershell" if os_id == "windows" else "bash"
    return {"os": os_id, "arch": arch, "shell": shell}


def resolve_home(env: dict[str, str] | None = None, os_id: str | None = None) -> Path:
    values = env if env is not None else os.environ
    if values.get("ONEAGENT_HOME"):
        return Path(values["ONEAGENT_HOME"]).expanduser()
    platform_id = os_id or current_platform()["os"]
    if platform_id == "windows":
        if values.get("USERPROFILE"):
            return Path(values["USERPROFILE"])
        if values.get("HOMEDRIVE") and values.get("HOMEPATH"):
            return Path(values["HOMEDRIVE"] + values["HOMEPATH"])
    if values.get("HOME"):
        return Path(values["HOME"])
    return Path.home()
