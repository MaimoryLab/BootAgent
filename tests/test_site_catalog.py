from __future__ import annotations

import json
from pathlib import Path

import pytest

from scripts.build_site_catalog import SiteCatalogError, build_site_catalog


def write_json(path: Path, value: object) -> Path:
    path.write_text(json.dumps(value), encoding="utf-8")
    return path


def catalog_inputs(tmp_path: Path) -> tuple[Path, Path]:
    lock = {
        "schema_version": 1,
        "agents": {
            "managed": {
                "name": "Managed Agent",
                "group": "auto",
                "rank": 1,
                "command": "managed-agent",
                "config_mode": "auto",
                "config_adapter": "codex",
                "config_path": ".managed/config.toml",
                "credential_delivery": "oneagent_env",
                "env_vars": {"SECRET_TOKEN": "must-not-leak"},
                "platforms": ["macos", "linux"],
                "package": {
                    "manager": "npm",
                    "name": "managed-agent",
                    "version": "1.2.3",
                    "integrity": "sha512-private-build-detail",
                    "source": "https://example.com/source",
                    "license": "MIT",
                    "license_url": "https://example.com/license",
                },
            },
            "guided": {
                "name": "Guided Agent",
                "group": "ide",
                "rank": 2,
                "command": "guided-agent",
                "config_mode": "guide",
                "platforms": ["windows"],
                "guide": "Use the official account flow.",
            },
        },
    }
    providers = {
        "schema_version": 1,
        "providers": {
            "example": {
                "name": "Example",
                "home": "https://example.com/",
                "relationship": "none",
                "disclosure": "",
                "referral_url": "",
                "order": 1,
                "protocols": {
                    "openai": "implementation-supported",
                    "responses": "release-candidate-required",
                },
            }
        },
    }
    return write_json(tmp_path / "agents.json", lock), write_json(tmp_path / "providers.json", providers)


def test_projects_public_site_catalog_v2_without_secret_bearing_fields(tmp_path: Path) -> None:
    lock_path, providers_path = catalog_inputs(tmp_path)

    catalog = build_site_catalog(lock_path, providers_path)

    assert catalog["schema_version"] == 2
    assert catalog["agents"][0]["command"] == "managed-agent"
    assert catalog["agents"][0]["configPath"] == ".managed/config.toml"
    assert catalog["agents"][1]["configPath"] is None

    serialized = json.dumps(catalog)
    for forbidden in ("SECRET_TOKEN", "must-not-leak", "credential_delivery", "integrity"):
        assert forbidden not in serialized


def test_managed_configuration_requires_a_public_config_path(tmp_path: Path) -> None:
    lock_path, providers_path = catalog_inputs(tmp_path)
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    del lock["agents"]["managed"]["config_path"]
    write_json(lock_path, lock)

    with pytest.raises(SiteCatalogError, match="config_path"):
        build_site_catalog(lock_path, providers_path)


def test_guide_only_agents_may_omit_a_launch_command(tmp_path: Path) -> None:
    lock_path, providers_path = catalog_inputs(tmp_path)
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    lock["agents"]["guided"]["command"] = "  "
    write_json(lock_path, lock)

    catalog = build_site_catalog(lock_path, providers_path)

    guided = next(agent for agent in catalog["agents"] if agent["id"] == "guided")
    assert guided["command"] is None
