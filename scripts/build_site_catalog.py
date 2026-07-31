#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from oneagent.catalog import ADAPTER_PROTOCOLS, AGENT_GROUPS


class SiteCatalogError(ValueError):
    pass


PROTOCOL_STATUSES = {"implementation-supported", "release-candidate-required", "verified", "unsupported"}


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SiteCatalogError(f"Cannot read {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise SiteCatalogError(f"{path} must contain a JSON object")
    return value


def require_https_url(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise SiteCatalogError(f"{label} must be a non-empty HTTPS URL")
    normalized = value.strip()
    parsed = urlsplit(normalized)
    if parsed.scheme != "https" or not parsed.netloc:
        raise SiteCatalogError(f"{label} must use HTTPS")
    return normalized


def build_site_catalog(lock_path: Path, providers_path: Path) -> dict[str, Any]:
    lock = load_json(lock_path)
    provider_config = load_json(providers_path)
    if lock.get("schema_version") != 1 or not isinstance(lock.get("agents"), dict):
        raise SiteCatalogError("Unsupported Agent lock schema")
    if provider_config.get("schema_version") != 1 or not isinstance(provider_config.get("providers"), dict):
        raise SiteCatalogError("Unsupported public Provider schema")

    agents: list[dict[str, Any]] = []
    for agent_id, meta in lock["agents"].items():
        if not isinstance(meta, dict):
            raise SiteCatalogError(f"Agent {agent_id} must be an object")
        config_mode = meta.get("config_mode")
        if config_mode not in {"auto", "guide"}:
            raise SiteCatalogError(f"Agent {agent_id} has unsupported config_mode")
        package = meta.get("package") if isinstance(meta.get("package"), dict) else None
        adapter = str(meta.get("config_adapter") or "")
        managed_install = config_mode == "auto" and package is not None
        managed_config = config_mode == "auto" and bool(adapter)
        command_value = meta.get("command")
        command = command_value.strip() if isinstance(command_value, str) and command_value.strip() else None
        config_path_value = meta.get("config_path")
        config_path = config_path_value.strip() if isinstance(config_path_value, str) and config_path_value.strip() else None
        if config_mode == "auto" and command is None:
            raise SiteCatalogError(f"Agent {agent_id}.command must be a non-empty string")
        if managed_config and config_path is None:
            raise SiteCatalogError(f"Agent {agent_id}.config_path must be a non-empty string")
        agents.append(
            {
                "id": agent_id,
                "name": str(meta.get("name") or agent_id),
                "group": str(meta.get("group") or "other"),
                "rank": int(meta.get("rank", 99)),
                "command": command,
                "configPath": config_path,
                "platforms": [str(value) for value in meta.get("platforms", [])],
                "lockedVersion": str(package.get("version")) if package and package.get("version") else None,
                "source": str(package.get("source")) if package and package.get("source") else None,
                "license": str(package.get("license")) if package and package.get("license") else None,
                "licenseUrl": str(package.get("license_url")) if package and package.get("license_url") else None,
                "guide": str(meta.get("guide")) if meta.get("guide") else None,
                "protocol": ADAPTER_PROTOCOLS.get(adapter) if managed_config else None,
                "support": {
                    "managedInstall": managed_install,
                    "officialInstallGuide": config_mode == "guide",
                    "managedConfig": managed_config,
                },
            }
        )
    agents.sort(key=lambda agent: (agent["rank"], agent["id"]))

    providers: list[dict[str, Any]] = []
    for provider_id, meta in provider_config["providers"].items():
        if not isinstance(meta, dict):
            raise SiteCatalogError(f"Provider {provider_id} must be an object")
        relationship = meta.get("relationship", "none")
        if relationship not in {"none", "referral", "sponsor"}:
            raise SiteCatalogError(f"Provider {provider_id} has unsupported relationship")
        if relationship != "none" and (not meta.get("disclosure") or not meta.get("referral_url")):
            raise SiteCatalogError(f"Provider {provider_id} commercial relationship requires disclosure and referral_url")
        if relationship == "none" and (meta.get("disclosure") or meta.get("referral_url")):
            raise SiteCatalogError(f"Provider {provider_id} cannot carry undisclosed commercial fields")
        protocols = meta.get("protocols", {})
        if not isinstance(protocols, dict):
            raise SiteCatalogError(f"Provider {provider_id}.protocols must be an object")
        for protocol, status in protocols.items():
            if not isinstance(protocol, str) or status not in PROTOCOL_STATUSES:
                raise SiteCatalogError(f"Provider {provider_id} has an unsupported protocol status")
        home = require_https_url(meta.get("home"), f"Provider {provider_id}.home")
        referral_url = require_https_url(meta.get("referral_url"), f"Provider {provider_id}.referral_url") if relationship != "none" else ""
        providers.append(
            {
                "id": provider_id,
                "name": str(meta.get("name") or provider_id),
                "home": home,
                "relationship": relationship,
                "disclosure": str(meta.get("disclosure") or ""),
                "referralUrl": referral_url,
                "protocols": [{"id": str(key), "status": str(value)} for key, value in protocols.items()],
                "order": int(meta.get("order", 99)),
            }
        )
    providers.sort(key=lambda provider: (provider["order"], provider["name"]))

    return {
        "schema_version": 2,
        "groups": AGENT_GROUPS,
        "agents": agents,
        "providers": providers,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Project the runtime catalog into public, read-only website data")
    parser.add_argument("--lock", type=Path, default=ROOT / "agents.lock.json")
    parser.add_argument("--providers", type=Path, default=ROOT / "distribution" / "providers.json")
    parser.add_argument("--output", type=Path, default=ROOT / "site" / "src" / "generated" / "catalog.json")
    args = parser.parse_args()
    try:
        catalog = build_site_catalog(args.lock, args.providers)
    except SiteCatalogError as exc:
        raise SystemExit(str(exc)) from exc
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(catalog, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(args.output)


if __name__ == "__main__":
    main()
