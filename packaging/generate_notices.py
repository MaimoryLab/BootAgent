from __future__ import annotations

import json
import re
import shutil
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
FRONTEND = ROOT / "frontend"


def _license_name(value: object) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return str(value.get("type") or value.get("name") or "Unknown")
    if isinstance(value, list):
        return ", ".join(_license_name(item) for item in value)
    return "Unknown"


def _safe_name(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]+", "_", value).strip("_")


def _repository(value: object) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return str(value.get("url") or "")
    return ""


def _copy_license(package_dir: Path, destination: Path, label: str) -> str:
    candidates = sorted(
        path
        for path in package_dir.iterdir()
        if path.is_file() and re.match(r"^(licen[cs]e|copying|notice)(\..*)?$", path.name, re.IGNORECASE)
    ) if package_dir.is_dir() else []
    if not candidates:
        return ""
    target = destination / f"{_safe_name(label)}{candidates[0].suffix or '.txt'}"
    shutil.copy2(candidates[0], target)
    return target.name


def npm_packages(license_dir: Path) -> list[dict[str, str]]:
    lock_path = FRONTEND / "package-lock.json"
    if not lock_path.exists():
        return []
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    packages: list[dict[str, str]] = []
    for relative, lock_meta in sorted(lock.get("packages", {}).items()):
        if not relative.startswith("node_modules/") or lock_meta.get("dev"):
            continue
        package_dir = FRONTEND / relative
        package_json = package_dir / "package.json"
        if not package_json.exists():
            continue
        meta = json.loads(package_json.read_text(encoding="utf-8"))
        name = str(meta.get("name") or relative.removeprefix("node_modules/"))
        version = str(meta.get("version") or lock_meta.get("version") or "unknown")
        packages.append(
            {
                "name": name,
                "version": version,
                "license": _license_name(meta.get("license") or meta.get("licenses")),
                "source": str(meta.get("homepage") or _repository(meta.get("repository")) or ""),
                "license_file": _copy_license(package_dir, license_dir, f"npm-{name}-{version}"),
            }
        )
    return packages


def agent_targets() -> list[dict[str, str]]:
    manifest = json.loads((ROOT / "agents.lock.json").read_text(encoding="utf-8"))
    targets = []
    for agent_id, meta in manifest["agents"].items():
        package = meta.get("package")
        if not package:
            continue
        targets.append(
            {
                "id": agent_id,
                "name": str(meta["name"]),
                "package": f"{package['name']}@{package['version']}",
                "license": str(package["license"]),
                "source": str(package["source"]),
                "license_url": str(package["license_url"]),
            }
        )
    return targets


def generate(output_dir: Path) -> Path:
    output_dir.mkdir(parents=True, exist_ok=True)
    license_dir = output_dir / "licenses" / "npm"
    license_dir.mkdir(parents=True, exist_ok=True)
    npm = npm_packages(license_dir)
    agents = agent_targets()

    lines = [
        "# OneAgent Third-Party Notices",
        "",
        "This file lists software included in the OneAgent runtime and Agent installation targets referenced by the lock manifest.",
        "Agent packages are not bundled in OneAgent; they are installed from the listed upstream source only after user confirmation.",
        "",
        "## Runtime Components",
        "",
        f"- Python {sys.version_info.major}.{sys.version_info.minor}: PSF License, <https://www.python.org/psf-landing/>",
        "- PyInstaller bootloader: GPL-2.0-or-later with special exception, <https://pyinstaller.org/en/stable/license.html>",
        "",
        "## Frontend Runtime Packages",
        "",
        "| Package | Version | License | Source | Bundled license file |",
        "| --- | --- | --- | --- | --- |",
    ]
    for package in npm:
        lines.append(
            f"| `{package['name']}` | `{package['version']}` | {package['license']} | {package['source'] or 'package metadata'} | {package['license_file'] or 'not provided by package'} |"
        )
    lines.extend(
        [
            "",
            "## Agent Installation Targets (Not Bundled)",
            "",
            "| Agent | Locked package | License | Source | License reference |",
            "| --- | --- | --- | --- | --- |",
        ]
    )
    for agent in agents:
        lines.append(
            f"| {agent['name']} | `{agent['package']}` | {agent['license']} | {agent['source']} | {agent['license_url']} |"
        )
    path = output_dir / "THIRD_PARTY_NOTICES.md"
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return path


def main() -> None:
    output = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else ROOT / "build" / "metadata"
    print(generate(output))


if __name__ == "__main__":
    main()
