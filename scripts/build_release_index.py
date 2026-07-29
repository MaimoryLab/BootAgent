#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
from datetime import datetime
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]


class ReleaseIndexError(ValueError):
    pass


PLATFORM_LABELS = {"macos": "macOS", "windows": "Windows", "linux": "Linux"}
ARCH_LABELS = {"arm64": "Apple silicon / ARM64", "x64": "Intel / AMD 64-bit"}
TARGET_STATUSES = {"available", "verification-pending", "planned", "withdrawn"}
CLEANROOM_STATUSES = {"verified", "not-recorded", "failed"}


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def load_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ReleaseIndexError(f"Cannot read {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ReleaseIndexError(f"{path} must contain a JSON object")
    return value


def parse_checksums(path: Path) -> dict[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise ReleaseIndexError(f"Missing checksum file: {path}") from exc
    checksums: dict[str, str] = {}
    for line in lines:
        if not line.strip():
            continue
        parts = line.split(maxsplit=1)
        if len(parts) != 2:
            raise ReleaseIndexError(f"Malformed checksum line in {path}: {line}")
        digest, filename = parts
        filename = filename.lstrip("* ")
        if len(digest) != 64 or any(character not in "0123456789abcdef" for character in digest.lower()):
            raise ReleaseIndexError(f"Malformed SHA-256 in {path}: {digest}")
        if filename in checksums:
            raise ReleaseIndexError(f"Duplicate checksum entry in {path}: {filename}")
        checksums[filename] = digest.lower()
    return checksums


def require_string(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise ReleaseIndexError(f"{label} must be a non-empty string")
    return value


def require_timestamp(value: Any, label: str) -> str:
    text = require_string(value, label)
    try:
        parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
    except ValueError as exc:
        raise ReleaseIndexError(f"{label} must be an ISO-8601 timestamp") from exc
    if parsed.tzinfo is None:
        raise ReleaseIndexError(f"{label} must include a timezone")
    return text


def validate_manifest(path: Path, release_dir: Path) -> dict[str, Any]:
    manifest = load_json(path)
    if manifest.get("schema_version") != 1:
        raise ReleaseIndexError(f"Unsupported release manifest schema in {path}")
    for field in ["oneagent_version", "channel", "platform", "arch", "python"]:
        require_string(manifest.get(field), f"{path.name}.{field}")
    require_timestamp(manifest.get("built_at"), f"{path.name}.built_at")
    if not isinstance(manifest.get("unsigned"), bool):
        raise ReleaseIndexError(f"{path.name}.unsigned must be a boolean")
    if manifest["channel"] == "technical-preview-unsigned" and not manifest["unsigned"]:
        raise ReleaseIndexError(f"{path.name} cannot describe a signed technical preview")
    if manifest["channel"] == "stable" and manifest["unsigned"]:
        raise ReleaseIndexError(f"{path.name} cannot describe an unsigned Stable build")
    artifacts = manifest.get("artifacts")
    if not isinstance(artifacts, list) or not artifacts:
        raise ReleaseIndexError(f"{path.name}.artifacts must not be empty")
    if not isinstance(manifest.get("agent_versions"), dict):
        raise ReleaseIndexError(f"{path.name}.agent_versions must be an object")

    checksum_path = release_dir / f"SHA256SUMS-{manifest['platform']}-{manifest['arch']}.txt"
    checksums = parse_checksums(checksum_path)
    manifest_digest = sha256(path)
    if checksums.get(path.name) != manifest_digest:
        raise ReleaseIndexError(f"Manifest SHA-256 does not match {checksum_path.name}: {path.name}")

    validated_artifacts: list[dict[str, Any]] = []
    for item in artifacts:
        if not isinstance(item, dict):
            raise ReleaseIndexError(f"{path.name}.artifacts entries must be objects")
        filename = require_string(item.get("file"), f"{path.name}.artifacts.file")
        if Path(filename).name != filename:
            raise ReleaseIndexError(f"Artifact file must be a basename: {filename}")
        artifact_path = release_dir / filename
        if not artifact_path.is_file():
            raise ReleaseIndexError(f"Missing artifact referenced by manifest: {filename}")
        expected_digest = require_string(item.get("sha256"), f"{filename}.sha256").lower()
        actual_digest = sha256(artifact_path)
        if expected_digest != actual_digest:
            raise ReleaseIndexError(f"Artifact SHA-256 does not match manifest: {filename}")
        if checksums.get(filename) != actual_digest:
            raise ReleaseIndexError(f"Artifact SHA-256 does not match {checksum_path.name}: {filename}")
        if item.get("bytes") != artifact_path.stat().st_size:
            raise ReleaseIndexError(f"Artifact byte count does not match manifest: {filename}")
        validated_artifacts.append({"file": filename, "sha256": actual_digest, "bytes": artifact_path.stat().st_size})
    manifest["artifacts"] = validated_artifacts
    return manifest


def artifact_kind(filename: str) -> str:
    return "source" if filename.endswith("-source.zip") else "binary"


def target_id(platform_id: str, arch: str) -> str:
    return f"{platform_id}-{arch}"


def build_release_index(release_dir: Path, channels_path: Path) -> dict[str, Any]:
    release_dir = release_dir.resolve()
    channels_config = load_json(channels_path)
    if channels_config.get("schema_version") != 1:
        raise ReleaseIndexError("Unsupported distribution channel schema")
    product = channels_config.get("product")
    channels = channels_config.get("channels")
    if not isinstance(product, dict) or not isinstance(channels, dict):
        raise ReleaseIndexError("Distribution config requires product and channels objects")

    manifests: dict[tuple[str, str, str], dict[str, Any]] = {}
    versions: dict[str, set[str]] = {}
    for path in sorted(release_dir.glob("release-manifest-*.json")):
        manifest = validate_manifest(path, release_dir)
        key = (manifest["channel"], manifest["platform"], manifest["arch"])
        if key in manifests:
            raise ReleaseIndexError(f"Duplicate release manifest for {'/'.join(key)}")
        manifests[key] = manifest
        versions.setdefault(manifest["channel"], set()).add(manifest["oneagent_version"])

    output_channels: list[dict[str, Any]] = []
    latest: dict[str, str | None] = {}
    configured_keys: set[tuple[str, str, str]] = set()
    for channel_id, channel_config in channels.items():
        if not isinstance(channel_config, dict):
            raise ReleaseIndexError(f"Channel {channel_id} must be an object")
        label = require_string(channel_config.get("label"), f"channels.{channel_id}.label")
        targets = channel_config.get("targets")
        if not isinstance(targets, list):
            raise ReleaseIndexError(f"channels.{channel_id}.targets must be an array")
        channel_versions = versions.get(channel_id, set())
        if len(channel_versions) > 1:
            raise ReleaseIndexError(f"Channel {channel_id} contains multiple current versions: {sorted(channel_versions)}")
        current_version = next(iter(channel_versions), None)
        latest[channel_id] = current_version
        output_targets: list[dict[str, Any]] = []
        channel_unsigned: bool | None = None

        for target in targets:
            if not isinstance(target, dict):
                raise ReleaseIndexError(f"Channel {channel_id} target must be an object")
            platform_id = require_string(target.get("platform"), f"channels.{channel_id}.targets.platform")
            arch = require_string(target.get("arch"), f"channels.{channel_id}.targets.arch")
            status = require_string(target.get("status"), f"{channel_id}/{platform_id}/{arch}.status")
            if status not in TARGET_STATUSES:
                raise ReleaseIndexError(f"Unsupported target status: {status}")
            verification = target.get("verification")
            mirrors = target.get("mirrors")
            if not isinstance(verification, dict) or not isinstance(mirrors, list):
                raise ReleaseIndexError(f"{channel_id}/{platform_id}/{arch} requires verification and mirrors")
            cleanroom = verification.get("cleanroom")
            if cleanroom not in CLEANROOM_STATUSES:
                raise ReleaseIndexError(f"Unsupported cleanroom status for {channel_id}/{platform_id}/{arch}")
            key = (channel_id, platform_id, arch)
            if key in configured_keys:
                raise ReleaseIndexError(f"Duplicate distribution target: {channel_id}/{platform_id}/{arch}")
            configured_keys.add(key)
            manifest = manifests.get(key)

            validated_mirrors: list[dict[str, Any]] = []

            if status == "available":
                if manifest is None:
                    raise ReleaseIndexError(f"Available target has no release manifest: {channel_id}/{platform_id}/{arch}")
                if verification.get("native_build") is not True:
                    raise ReleaseIndexError(f"Available target lacks native build evidence: {channel_id}/{platform_id}/{arch}")
                if cleanroom != "verified" or not verification.get("evidence"):
                    raise ReleaseIndexError(f"Available target lacks cleanroom evidence: {channel_id}/{platform_id}/{arch}")
                if not mirrors:
                    raise ReleaseIndexError(f"Available target has no verified download channel: {channel_id}/{platform_id}/{arch}")
                mirror_ids: set[str] = set()
                for mirror in mirrors:
                    if not isinstance(mirror, dict):
                        raise ReleaseIndexError(f"Mirror for {channel_id}/{platform_id}/{arch} must be an object")
                    mirror_id = require_string(mirror.get("id"), "mirror.id")
                    if mirror_id in mirror_ids:
                        raise ReleaseIndexError(f"Duplicate mirror id for {channel_id}/{platform_id}/{arch}: {mirror_id}")
                    mirror_ids.add(mirror_id)
                    kind = require_string(mirror.get("kind"), "mirror.kind")
                    if kind not in {"official", "mirror"}:
                        raise ReleaseIndexError(f"Unsupported mirror kind: {kind}")
                    url_template = require_string(mirror.get("url"), "mirror.url")
                    parsed_url = urlsplit(url_template)
                    if "{file}" not in url_template:
                        raise ReleaseIndexError(f"Mirror URL must include {{file}}: {mirror_id}")
                    if parsed_url.scheme not in {"", "https"} or (not parsed_url.scheme and url_template.startswith("/")):
                        raise ReleaseIndexError(f"Mirror URL must be relative or HTTPS: {mirror_id}")
                    if ".." in PurePosixPath(unquote(parsed_url.path)).parts:
                        raise ReleaseIndexError(f"Mirror URL must not escape its download root: {mirror_id}")
                    if kind == "mirror" and parsed_url.scheme != "https":
                        raise ReleaseIndexError(f"External mirror must use HTTPS: {mirror_id}")
                    if kind == "mirror":
                        audit = mirror.get("audit")
                        if not isinstance(audit, dict):
                            raise ReleaseIndexError(f"External mirror requires an audit record: {mirror_id}")
                        for field in ["uploaded_by", "withdrawal_owner"]:
                            require_string(audit.get(field), f"mirror.audit.{field}")
                        for field in ["uploaded_at", "verified_at"]:
                            require_timestamp(audit.get(field), f"mirror.audit.{field}")
                        if not isinstance(audit.get("withdrawn"), bool):
                            raise ReleaseIndexError(f"mirror.audit.withdrawn must be a boolean: {mirror_id}")
                        if audit["withdrawn"]:
                            require_timestamp(audit.get("withdrawn_at"), "mirror.audit.withdrawn_at")
                            continue
                    validated_mirrors.append(
                        {
                            "id": mirror_id,
                            "label": require_string(mirror.get("label"), "mirror.label"),
                            "kind": kind,
                            "url": url_template,
                            "primary": bool(mirror.get("primary", False)),
                            "verified_sha256": mirror.get("verified_sha256"),
                        }
                    )
                primary_mirrors = [mirror for mirror in validated_mirrors if mirror["primary"]]
                if len(primary_mirrors) != 1 or primary_mirrors[0]["kind"] != "official":
                    raise ReleaseIndexError(
                        f"Available target requires exactly one primary official channel: {channel_id}/{platform_id}/{arch}"
                    )
                if not any(mirror["kind"] == "official" for mirror in validated_mirrors):
                    raise ReleaseIndexError(f"Available target has no official download channel: {channel_id}/{platform_id}/{arch}")

            artifacts: list[dict[str, Any]] = []
            if manifest is not None:
                if current_version != manifest["oneagent_version"]:
                    raise ReleaseIndexError(f"Manifest version drift for {channel_id}/{platform_id}/{arch}")
                channel_unsigned = manifest["unsigned"] if channel_unsigned is None else channel_unsigned
                if channel_unsigned != manifest["unsigned"]:
                    raise ReleaseIndexError(f"Channel {channel_id} mixes signed and unsigned manifests")
                for artifact in manifest["artifacts"]:
                    downloads: list[dict[str, Any]] = []
                    if status == "available":
                        for mirror in validated_mirrors:
                            verified_sha = mirror["verified_sha256"]
                            if mirror["kind"] == "mirror" and (
                                not isinstance(verified_sha, str) or verified_sha.lower() != artifact["sha256"]
                            ):
                                raise ReleaseIndexError(f"External mirror SHA-256 is not verified for {artifact['file']}")
                            downloads.append(
                                {
                                    "id": mirror["id"],
                                    "label": mirror["label"],
                                    "kind": mirror["kind"],
                                    "url": mirror["url"].replace("{file}", artifact["file"]),
                                    "primary": mirror["primary"],
                                }
                            )
                    artifacts.append(
                        {
                            **artifact,
                            "kind": artifact_kind(artifact["file"]),
                            "downloads": downloads,
                        }
                    )
                if status == "available" and not any(item["kind"] == "binary" for item in artifacts):
                    raise ReleaseIndexError(f"Available target has no binary artifact: {channel_id}/{platform_id}/{arch}")

            output_targets.append(
                {
                    "id": target_id(platform_id, arch),
                    "platform": platform_id,
                    "platformLabel": PLATFORM_LABELS.get(platform_id, platform_id),
                    "arch": arch,
                    "archLabel": ARCH_LABELS.get(arch, arch),
                    "status": status,
                    "verification": {
                        "native_build": bool(verification.get("native_build", False)),
                        "cleanroom": cleanroom,
                        "evidence": verification.get("evidence"),
                    },
                    "python": manifest.get("python") if manifest else None,
                    "built_at": manifest.get("built_at") if manifest else None,
                    "agent_versions": manifest.get("agent_versions", {}) if manifest else {},
                    "artifacts": artifacts,
                }
            )

        published_at = channel_config.get("published_at")
        if any(target["status"] == "available" for target in output_targets):
            published_at = require_timestamp(published_at, f"channels.{channel_id}.published_at")

        output_channels.append(
            {
                "channel": channel_id,
                "label": label,
                "published_at": published_at,
                "version": current_version,
                "unsigned": channel_unsigned if channel_unsigned is not None else channel_id != "stable",
                "status": "available" if any(target["status"] == "available" for target in output_targets) else "unavailable",
                "targets": output_targets,
            }
        )

    unknown = sorted(set(manifests) - configured_keys)
    if unknown:
        formatted = ", ".join("/".join(key) for key in unknown)
        raise ReleaseIndexError(f"Release manifests have no channel target configuration: {formatted}")

    return {
        "schema_version": 1,
        "product": product,
        "latest": latest,
        "channels": output_channels,
    }


def copy_available_downloads(index: dict[str, Any], release_dir: Path, destination: Path) -> None:
    reset_destination(destination)
    for channel in index["channels"]:
        for target in channel["targets"]:
            if target["status"] != "available":
                continue
            for artifact in target["artifacts"]:
                source = release_dir / artifact["file"]
                shutil.copy2(source, destination / artifact["file"])


def reset_destination(destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    for child in destination.iterdir():
        if child.is_file() or child.is_symlink():
            child.unlink()
        else:
            raise ReleaseIndexError(f"Download destination contains an unexpected directory: {child}")


def copy_available_release_assets(index: dict[str, Any], release_dir: Path, destination: Path) -> None:
    reset_destination(destination)
    copied: set[str] = set()
    for channel in index["channels"]:
        for target in channel["targets"]:
            if target["status"] != "available":
                continue
            metadata = [
                f"release-manifest-{target['platform']}-{target['arch']}.json",
                f"SHA256SUMS-{target['platform']}-{target['arch']}.txt",
            ]
            for filename in metadata:
                source = release_dir / filename
                if not source.is_file():
                    raise ReleaseIndexError(f"Available release metadata is missing: {filename}")
                if filename not in copied:
                    shutil.copy2(source, destination / filename)
                    copied.add(filename)
            for artifact in target["artifacts"]:
                filename = artifact["file"]
                if filename not in copied:
                    shutil.copy2(release_dir / filename, destination / filename)
                    copied.add(filename)
    (destination / "release-index.json").write_text(
        json.dumps(index, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Validate native release manifests and build the public release index")
    parser.add_argument("--release-dir", type=Path, default=ROOT / "release")
    parser.add_argument("--channels", type=Path, default=ROOT / "distribution" / "channels.json")
    parser.add_argument("--output", type=Path, default=ROOT / "site" / "src" / "generated" / "release-index.json")
    parser.add_argument("--copy-downloads", type=Path)
    parser.add_argument("--copy-release-assets", type=Path)
    args = parser.parse_args()

    try:
        index = build_release_index(args.release_dir, args.channels)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(index, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        if args.copy_downloads:
            copy_available_downloads(index, args.release_dir, args.copy_downloads)
        if args.copy_release_assets:
            copy_available_release_assets(index, args.release_dir, args.copy_release_assets)
    except ReleaseIndexError as exc:
        raise SystemExit(str(exc)) from exc
    print(args.output)


if __name__ == "__main__":
    main()
