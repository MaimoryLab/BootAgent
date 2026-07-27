#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import re
import zipfile
from pathlib import Path


FORBIDDEN_PARTS = {"node_modules", "output", ".git", "coverage", "test-results", "playwright-report"}
FORBIDDEN_AGENT_BINARIES = {"codex", "codex.exe", "claude", "claude.exe", "opencode", "opencode.exe", "kilo", "kilo.exe", "aider", "aider.exe"}
SECRET_PATTERNS = [
    re.compile(rb"sk-[A-Za-z0-9_-]{20,}"),
    re.compile(rb"Bearer\s+[A-Za-z0-9._-]{24,}", re.IGNORECASE),
]


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def inspect_zip(path: Path) -> list[str]:
    problems: list[str] = []
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        for name in names:
            parts = set(Path(name).parts)
            basename = Path(name).name.lower()
            if parts & FORBIDDEN_PARTS:
                problems.append(f"forbidden path in {path.name}: {name}")
            if name.endswith(".map"):
                problems.append(f"source map in {path.name}: {name}")
            if basename in FORBIDDEN_AGENT_BINARIES:
                problems.append(f"Agent binary in {path.name}: {name}")
            if name.endswith((".js", ".css", ".html", ".json", ".md", ".txt", ".toml", ".ps1", ".sh", ".py")):
                data = archive.read(name)
                for pattern in SECRET_PATTERNS:
                    if pattern.search(data):
                        problems.append(f"possible secret in {path.name}: {name}")
        required = {"agents.lock.json", "THIRD_PARTY_NOTICES.md"}
        basenames = {Path(name).name for name in names}
        for missing in sorted(required - basenames):
            problems.append(f"missing {missing} in {path.name}")
    return problems


def archive_agent_versions(path: Path) -> dict[str, str] | None:
    with zipfile.ZipFile(path) as archive:
        names = [name for name in archive.namelist() if Path(name).name == "agents.lock.json"]
        if len(names) != 1:
            return None
        lock = json.loads(archive.read(names[0]).decode())
    return {
        agent_id: str(meta["package"]["version"])
        for agent_id, meta in lock.get("agents", {}).items()
        if meta.get("package")
    }


def validate_manifest(path: Path) -> list[str]:
    problems: list[str] = []
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return [f"invalid release manifest {path.name}: {exc}"]
    if data.get("schema_version") != 1:
        problems.append(f"unsupported release manifest schema: {path.name}")
    channel = data.get("channel")
    if channel == "technical-preview-unsigned" and not data.get("unsigned"):
        problems.append(f"unsigned marker missing: {path.name}")
    if channel == "stable" and data.get("unsigned"):
        problems.append(f"Stable manifest cannot be unsigned: {path.name}")

    artifacts = data.get("artifacts")
    if not isinstance(artifacts, list) or not artifacts:
        return [*problems, f"artifact list is missing: {path.name}"]
    expected_versions = data.get("agent_versions")
    for entry in artifacts:
        if not isinstance(entry, dict) or not isinstance(entry.get("file"), str):
            problems.append(f"invalid artifact entry: {path.name}")
            continue
        filename = entry["file"]
        if Path(filename).name != filename:
            problems.append(f"artifact filename must not contain a path: {filename}")
            continue
        artifact = path.parent / filename
        if not artifact.is_file():
            problems.append(f"manifest artifact is missing: {filename}")
            continue
        if artifact.stat().st_size != entry.get("bytes"):
            problems.append(f"artifact size mismatch: {filename}")
        if sha256(artifact) != entry.get("sha256"):
            problems.append(f"artifact checksum mismatch: {filename}")
        if channel and not filename.endswith("-source.zip") and channel not in filename:
            problems.append(f"artifact channel mismatch: {filename}")
        try:
            archive_versions = archive_agent_versions(artifact)
        except (OSError, ValueError, KeyError, json.JSONDecodeError, zipfile.BadZipFile) as exc:
            problems.append(f"cannot read lock manifest from {filename}: {exc}")
        else:
            if archive_versions is None:
                problems.append(f"lock manifest is missing or duplicated: {filename}")
            elif archive_versions != expected_versions:
                problems.append(f"locked Agent versions mismatch: {filename}")

    suffix = path.name.removeprefix("release-manifest-").removesuffix(".json")
    checksum_path = path.parent / f"SHA256SUMS-{suffix}.txt"
    if not checksum_path.is_file():
        problems.append(f"checksum file is missing: {checksum_path.name}")
        return problems
    checksums: dict[str, str] = {}
    for line in checksum_path.read_text(encoding="utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})  ([^/\\]+)", line)
        if not match:
            problems.append(f"invalid checksum line in {checksum_path.name}: {line}")
            continue
        checksums[match.group(2)] = match.group(1)
    expected_files = [path, *(path.parent / entry["file"] for entry in artifacts if isinstance(entry, dict) and isinstance(entry.get("file"), str))]
    for expected in expected_files:
        if not expected.is_file():
            continue
        if checksums.get(expected.name) != sha256(expected):
            problems.append(f"checksum file mismatch: {expected.name}")
    return problems


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate OneAgent release archives")
    parser.add_argument("path", type=Path, nargs="?", default=Path("release"))
    args = parser.parse_args()
    manifests = list(args.path.glob("release-manifest-*.json"))
    archives = list(args.path.glob("OneAgent-*.zip"))
    problems: list[str] = []
    if not manifests:
        problems.append("release manifest is missing")
    if not archives:
        problems.append("release archive is missing")
    for manifest in manifests:
        problems.extend(validate_manifest(manifest))
    for archive in archives:
        problems.extend(inspect_zip(archive))
    if problems:
        raise SystemExit("\n".join(problems))
    print(f"release policy checks passed ({len(archives)} archives)")


if __name__ == "__main__":
    main()
