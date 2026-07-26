#!/usr/bin/env python3
from __future__ import annotations

import os
import shutil
import tempfile
from pathlib import Path

from oneagent.catalog import current_platform, load_manifest
from oneagent.installer import InstallOptions, Runtime, install_many, installed_version


SECRET_ENV_MARKERS = ("KEY", "TOKEN", "SECRET", "PASSWORD")


def create_isolated_runtime(
    root: Path,
    *,
    os_id: str | None = None,
    base_env: dict[str, str] | None = None,
) -> Runtime:
    platform_id = os_id or current_platform()["os"]
    cleanroom = root.absolute()
    home = cleanroom / "home"
    npm_prefix = cleanroom / "npm-prefix"
    npm_bin = npm_prefix if platform_id == "windows" else npm_prefix / "bin"
    uv_tool_dir = cleanroom / "uv-tools"
    uv_bin = cleanroom / "uv-bin"
    cache = cleanroom / "cache"
    temp = cleanroom / "tmp"
    for path in (home, npm_prefix, npm_bin, uv_tool_dir, uv_bin, cache, temp):
        path.mkdir(parents=True, exist_ok=True)

    original = dict(base_env if base_env is not None else os.environ)
    env = {
        name: value
        for name, value in original.items()
        if not any(marker in name.upper() for marker in SECRET_ENV_MARKERS)
    }
    env.pop("UV_CONFIG_FILE", None)
    env.pop("PIP_CONFIG_FILE", None)
    original_path = env.get("PATH", "")
    isolated_path = os.pathsep.join(
        part for part in (str(uv_bin), str(npm_bin), original_path) if part
    )
    env.update(
        {
            "HOME": str(home),
            "USERPROFILE": str(home),
            "ONEAGENT_HOME": str(home),
            "NPM_CONFIG_PREFIX": str(npm_prefix),
            "NPM_CONFIG_CACHE": str(cache / "npm"),
            "NPM_CONFIG_USERCONFIG": str(cleanroom / "npmrc"),
            "UV_TOOL_DIR": str(uv_tool_dir),
            "UV_TOOL_BIN_DIR": str(uv_bin),
            "UV_CACHE_DIR": str(cache / "uv"),
            "XDG_CONFIG_HOME": str(cleanroom / "xdg-config"),
            "XDG_CACHE_HOME": str(cache / "xdg"),
            "APPDATA": str(cleanroom / "appdata"),
            "LOCALAPPDATA": str(cleanroom / "local-appdata"),
            "TMPDIR": str(temp),
            "TEMP": str(temp),
            "TMP": str(temp),
            "PATH": isolated_path,
        }
    )

    manifest = load_manifest()
    agent_commands = {
        str(meta["command"])
        for meta in manifest["agents"].values()
        if meta.get("config_mode") == "auto" and meta.get("command")
    }
    isolated_roots = (npm_prefix.resolve(), uv_bin.resolve(), uv_tool_dir.resolve())

    def isolated_which(command: str) -> str | None:
        candidate = shutil.which(command, path=env["PATH"])
        if not candidate or command not in agent_commands:
            return candidate
        resolved = Path(candidate).resolve()
        if any(resolved == root_path or root_path in resolved.parents for root_path in isolated_roots):
            return candidate
        return None

    return Runtime.create(home=home, os_id=platform_id, which=isolated_which, env=env)


def _verify(runtime: Runtime) -> dict[str, str]:
    manifest = load_manifest()
    verified: dict[str, str] = {}
    for agent_id, meta in manifest["agents"].items():
        if meta.get("config_mode") != "auto":
            continue
        result = install_many(
            InstallOptions(
                agents=[agent_id],
                configure=False,
                install_agent=True,
                check_agent_only=True,
                locked_version=True,
                home=runtime.home,
                os_id=runtime.os_id,
                timeout=900,
            ),
            runtime,
        )
        if not result["ok"]:
            message = result["results"][0].get("message") or "installation failed"
            raise RuntimeError(f"{agent_id}: {message}")
        actual = installed_version(runtime, meta)
        expected = str(meta["package"]["version"])
        if actual != expected:
            raise RuntimeError(f"{agent_id}: expected {expected}, got {actual or 'unavailable'}")
        verified[agent_id] = actual
    return verified


def verify_locked_agents(runtime: Runtime | None = None) -> dict[str, str]:
    if runtime is not None:
        return _verify(runtime)
    with tempfile.TemporaryDirectory() as tmp:
        return _verify(create_isolated_runtime(Path(tmp)))


def main() -> None:
    for agent_id, version in verify_locked_agents().items():
        print(f"{agent_id}: {version}")


if __name__ == "__main__":
    main()
