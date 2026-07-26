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
    },
    "novita": {
        "name": "Novita",
        "home": "https://novita.ai/",
        "base_url": "https://api.novita.ai/openai",
        "anthropic_base_url": "https://api.novita.ai/anthropic",
    },
}

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


def resource_root() -> Path:
    bundle_root = getattr(sys, "_MEIPASS", None)
    if bundle_root:
        return Path(bundle_root)
    return Path(__file__).resolve().parents[1]


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
            }
        )
    return items


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
