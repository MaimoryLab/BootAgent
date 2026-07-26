#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import zipfile
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / "build"
RELEASE = ROOT / "release"


def run(args: list[str], *, cwd: Path = ROOT) -> None:
    subprocess.run(args, cwd=cwd, check=True)


def version() -> str:
    namespace: dict[str, object] = {}
    exec((ROOT / "oneagent" / "__init__.py").read_text(encoding="utf-8"), namespace)
    return str(namespace["__version__"])


def target() -> tuple[str, str]:
    system = platform.system().lower()
    os_id = "macos" if system == "darwin" else "windows" if system == "windows" else "linux"
    machine = platform.machine().lower()
    arch = "arm64" if machine in {"arm64", "aarch64"} else "x64"
    return os_id, arch


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def zip_tree(source: Path, destination: Path, root_name: str) -> None:
    with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in sorted(source.rglob("*")):
            if not path.is_file():
                continue
            relative = Path(root_name) / path.relative_to(source)
            info = zipfile.ZipInfo.from_file(path, str(relative))
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = (stat.S_IMODE(path.stat().st_mode) & 0xFFFF) << 16
            archive.writestr(info, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)


def source_files() -> list[Path]:
    roots = [
        "oneagent",
        "scripts",
        "packaging",
        "docs",
        "tests",
        ".github",
        "frontend/src",
        "frontend/e2e",
        "frontend/dist",
        "build/metadata/THIRD_PARTY_NOTICES.md",
        "build/metadata/licenses",
    ]
    files: list[Path] = []
    for item in roots:
        path = ROOT / item
        if path.is_file():
            files.append(path)
        elif path.exists():
            files.extend(candidate for candidate in path.rglob("*") if candidate.is_file() and "__pycache__" not in candidate.parts)
    for item in [
        ".dockerignore",
        "Dockerfile.test",
        "README.md",
        "agents.lock.json",
        "pyproject.toml",
        "frontend/package.json",
        "frontend/package-lock.json",
        "frontend/tsconfig.json",
        "frontend/vite.config.ts",
        "frontend/playwright.config.ts",
        "frontend/index.html",
    ]:
        path = ROOT / item
        if path.exists():
            files.append(path)
    return sorted(set(files))


def build_source_zip(current_version: str) -> Path:
    destination = RELEASE / f"OneAgent-{current_version}-source.zip"
    root_name = f"OneAgent-{current_version}"
    with zipfile.ZipFile(destination, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for path in source_files():
            archive.write(path, Path(root_name) / path.relative_to(ROOT))
    return destination


def ensure_unsigned_channel(channel: str, os_id: str) -> None:
    """Fail fast before a long build when the Stable prerequisites are absent.

    This is only a pre-flight check. Whether the produced binary carries a real
    signature is decided by verify_stable_signature() against the artifact
    itself, because an environment variable proves nothing.
    """
    if channel != "stable":
        return
    if os_id == "macos" and os.environ.get("ONEAGENT_MACOS_SIGNED") != "1":
        raise SystemExit("Stable macOS builds require signing and notarization evidence")
    if os_id == "windows" and os.environ.get("ONEAGENT_WINDOWS_SIGNED") != "1":
        raise SystemExit("Stable Windows builds require Authenticode signing evidence")


def _signature_check(args: list[str], failure: str) -> None:
    try:
        result = subprocess.run(args, cwd=ROOT, capture_output=True, text=True)
    except OSError as exc:
        raise SystemExit(f"{failure}: cannot run {args[0]}: {exc}") from exc
    if result.returncode != 0:
        detail = (result.stderr or result.stdout or "").strip().splitlines()
        raise SystemExit(f"{failure}: {detail[-1] if detail else f'{args[0]} exit {result.returncode}'}")


def verify_stable_signature(channel: str, os_id: str, bundle: Path) -> None:
    """Prove the Stable artifact is really signed.

    The previous gate only read ONEAGENT_MACOS_SIGNED / ONEAGENT_WINDOWS_SIGNED,
    so exporting either variable was enough to publish an unsigned build as
    Stable -- the exact "degrade into a fake pass" failure the release policy
    forbids. Interrogate the binary instead.
    """
    if channel != "stable":
        return
    if os_id == "macos":
        _signature_check(
            ["codesign", "--verify", "--deep", "--strict", "--verbose=2", str(bundle)],
            "Stable macOS builds require a valid Developer ID signature",
        )
        _signature_check(
            ["spctl", "--assess", "--type", "execute", str(bundle)],
            "Stable macOS builds must pass Gatekeeper assessment",
        )
        _signature_check(
            ["xcrun", "stapler", "validate", str(bundle)],
            "Stable macOS builds require a stapled notarization ticket",
        )
    elif os_id == "windows":
        executable = bundle / "OneAgent.exe"
        _signature_check(
            [
                "powershell",
                "-NoProfile",
                "-NonInteractive",
                "-Command",
                "$ErrorActionPreference='Stop';"
                f" $signature = Get-AuthenticodeSignature -LiteralPath '{executable}';"
                " if ($signature.Status -ne 'Valid') { Write-Error \"Authenticode status:"
                " $($signature.Status)\"; exit 1 }",
            ],
            "Stable Windows builds require a valid Authenticode signature",
        )


def main() -> None:
    parser = argparse.ArgumentParser(description="Build a platform-native OneAgent onedir archive")
    parser.add_argument("--channel", choices=["technical-preview-unsigned", "stable"], default="technical-preview-unsigned")
    parser.add_argument("--skip-frontend", action="store_true")
    parser.add_argument("--source", action="store_true", help="Also create the source ZIP")
    args = parser.parse_args()

    os_id, arch = target()
    ensure_unsigned_channel(args.channel, os_id)
    current_version = version()
    RELEASE.mkdir(parents=True, exist_ok=True)
    (BUILD / "metadata").mkdir(parents=True, exist_ok=True)

    if not args.skip_frontend:
        run(["npm", "run", "build"], cwd=ROOT / "frontend")
    if not (ROOT / "frontend" / "dist" / "index.html").exists():
        raise SystemExit("frontend/dist is missing; build the React frontend first")
    if list((ROOT / "frontend" / "dist").rglob("*.map")):
        raise SystemExit("Source maps are forbidden in release assets")

    run([sys.executable, str(ROOT / "packaging" / "generate_notices.py"), str(BUILD / "metadata")])

    dist_path = BUILD / "pyinstaller-dist"
    work_path = BUILD / "pyinstaller-work"
    if dist_path.exists():
        shutil.rmtree(dist_path)
    if work_path.exists():
        shutil.rmtree(work_path)
    run(
        [
            sys.executable,
            "-m",
            "PyInstaller",
            "--noconfirm",
            "--clean",
            "--distpath",
            str(dist_path),
            "--workpath",
            str(work_path),
            str(ROOT / "packaging" / "oneagent.spec"),
        ]
    )

    bundle = dist_path / "OneAgent"
    if not bundle.is_dir():
        raise SystemExit(f"PyInstaller output not found: {bundle}")
    # Verify the real artifact before it is archived, so an unsigned build can
    # never reach a Stable ZIP.
    verify_stable_signature(args.channel, os_id, bundle)
    artifact_name = f"OneAgent-{current_version}-{args.channel}-{os_id}-{arch}.zip"
    artifact = RELEASE / artifact_name
    zip_tree(bundle, artifact, "OneAgent")

    artifacts = [artifact]
    if args.source:
        artifacts.append(build_source_zip(current_version))

    lock = json.loads((ROOT / "agents.lock.json").read_text(encoding="utf-8"))
    manifest = {
        "schema_version": 1,
        "oneagent_version": current_version,
        "channel": args.channel,
        "unsigned": args.channel == "technical-preview-unsigned",
        "platform": os_id,
        "arch": arch,
        "python": platform.python_version(),
        "built_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "agent_versions": {
            agent_id: meta["package"]["version"]
            for agent_id, meta in lock["agents"].items()
            if meta.get("package")
        },
        "artifacts": [{"file": path.name, "sha256": sha256(path), "bytes": path.stat().st_size} for path in artifacts],
    }
    manifest_path = RELEASE / f"release-manifest-{os_id}-{arch}.json"
    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    checksum_path = RELEASE / f"SHA256SUMS-{os_id}-{arch}.txt"
    checksum_path.write_text("".join(f"{sha256(path)}  {path.name}\n" for path in [*artifacts, manifest_path]), encoding="utf-8")
    print(artifact)


if __name__ == "__main__":
    main()
