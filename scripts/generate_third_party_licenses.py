#!/usr/bin/env python3
"""Generate and verify the license material shipped in BootAgent artifacts.

The inventory is derived from the same production build tags and frozen pnpm
installation used by release builds. Generated output is tracked so a release
does not depend on registry or module-cache availability after compilation.
"""

from __future__ import annotations

import argparse
import dataclasses
import difflib
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
from collections.abc import Iterable, Iterator, Sequence
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_OUTPUT = ROOT / "third_party"
FRONTEND = ROOT / "frontend"
ICON_ROOT = FRONTEND / "src" / "components" / "icons"
ASSET_RIGHTS_MANIFEST = ICON_ROOT / "asset-rights.json"
TARGETS = (
    ("macos-x64", "darwin", "amd64", "1"),
    ("macos-arm64", "darwin", "arm64", "1"),
    ("windows-x64", "windows", "amd64", "0"),
    ("windows-arm64", "windows", "arm64", "0"),
)
GO_LICENSES = {
    "github.com/adrg/xdg": "MIT",
    "github.com/dustin/go-humanize": "MIT",
    "github.com/go-ole/go-ole": "MIT",
    "github.com/google/uuid": "BSD-3-Clause",
    "github.com/mattn/go-isatty": "MIT",
    "github.com/ncruces/go-strftime": "MIT",
    "github.com/pelletier/go-toml/v2": "MIT",
    "github.com/remyoudompheng/bigfft": "BSD-3-Clause",
    "github.com/wailsapp/wails/v3": "MIT",
    "github.com/tailscale/hujson": "BSD-3-Clause",
    "golang.org/x/sys": "BSD-3-Clause",
    "golang.org/x/mod": "BSD-3-Clause",
    "golang.org/x/net": "BSD-3-Clause",
    "gopkg.in/yaml.v3": "MIT AND Apache-2.0",
    "modernc.org/libc": "BSD-3-Clause",
    "modernc.org/mathutil": "BSD-3-Clause",
    "modernc.org/memory": "BSD-3-Clause",
    "modernc.org/sqlite": "BSD-3-Clause",
}
LICENSE_PREFIXES = ("license", "licence", "copying", "notice", "copyright")


@dataclasses.dataclass(frozen=True, order=True)
class Dependency:
    ecosystem: str
    name: str
    version: str
    license: str
    platforms: tuple[str, ...]
    license_files: tuple[str, ...]
    # Set only for a dependency BootAgent ships in altered form. MIT and similar
    # permissive licences allow the change but require it to be stated, and this
    # file is what reaches the user, so the statement has to be here rather than
    # only in the source manifest. Empty means redistributed unmodified.
    modification: str = ""

    def as_json(self) -> dict[str, Any]:
        return dataclasses.asdict(self)


def parse_json_stream(text: str) -> Iterator[dict[str, Any]]:
    """Parse the concatenated JSON objects emitted by `go list -json`."""
    decoder = json.JSONDecoder()
    index = 0
    while index < len(text):
        while index < len(text) and text[index].isspace():
            index += 1
        if index >= len(text):
            return
        value, index = decoder.raw_decode(text, index)
        if not isinstance(value, dict):
            raise ValueError("expected a JSON object in go list output")
        yield value


def run(argv: Sequence[str], *, cwd: Path = ROOT, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(
        argv,
        cwd=cwd,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def safe_component(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9._+-]+", "_", value).strip("_")


def find_license_files(directory: Path) -> list[Path]:
    files = [
        path
        for path in directory.iterdir()
        if path.is_file() and path.name.casefold().startswith(LICENSE_PREFIXES)
    ]
    return sorted(files, key=lambda path: path.name.casefold())


def copy_license_files(
    source_directory: Path,
    destination_directory: Path,
    relative_root: Path,
) -> tuple[str, ...]:
    sources = find_license_files(source_directory)
    if not sources:
        raise RuntimeError(f"no license or notice file found in {source_directory}")
    destination_directory.mkdir(parents=True, exist_ok=True)
    copied: list[str] = []
    for source in sources:
        destination = destination_directory / source.name
        text = source.read_text(encoding="utf-8")
        normalized = "\n".join(line.rstrip() for line in text.splitlines()).rstrip() + "\n"
        destination.write_text(normalized, encoding="utf-8")
        copied.append(destination.relative_to(relative_root).as_posix())
    return tuple(copied)


def collect_go_module_platforms() -> dict[tuple[str, str], set[str]]:
    modules: dict[tuple[str, str], set[str]] = {}
    for platform, goos, goarch, cgo in TARGETS:
        env = os.environ.copy()
        env.update({"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": cgo})
        output = run(
            [
                "go",
                "list",
                "-tags",
                "wails,production",
                "-deps",
                "-json",
                "./cmd/bootagent-desktop",
            ],
            env=env,
        )
        for package in parse_json_stream(output):
            module = package.get("Module")
            if not isinstance(module, dict) or module.get("Main"):
                continue
            path = str(module.get("Path", "")).strip()
            version = str(module.get("Version", "")).strip()
            if path and version:
                modules.setdefault((path, version), set()).add(platform)
    return modules


def module_directory(path: str, version: str) -> Path:
    output = run(["go", "mod", "download", "-json", f"{path}@{version}"])
    metadata = json.loads(output)
    directory = metadata.get("Dir")
    if not directory:
        raise RuntimeError(f"go mod download did not return a directory for {path}@{version}")
    return Path(directory)


def collect_go_dependencies(output: Path) -> list[Dependency]:
    dependencies: list[Dependency] = []
    for (name, version), platforms in sorted(collect_go_module_platforms().items()):
        license_name = GO_LICENSES.get(name)
        if license_name is None:
            raise RuntimeError(
                f"Go module {name}@{version} has no reviewed license classification; "
                "add it to GO_LICENSES after review"
            )
        destination = output / "licenses" / "go" / f"{safe_component(name)}@{safe_component(version)}"
        copied = copy_license_files(module_directory(name, version), destination, output)
        dependencies.append(
            Dependency(
                ecosystem="go",
                name=name,
                version=version,
                license=license_name,
                platforms=tuple(sorted(platforms)),
                license_files=copied,
            )
        )
    return dependencies


def pnpm_license_inventory() -> dict[str, list[dict[str, Any]]]:
    return json.loads(run(["pnpm", "licenses", "list", "--prod", "--json"], cwd=FRONTEND))


def npm_license_source(name: str, package_path: Path) -> Path:
    if find_license_files(package_path):
        return package_path
    if name == "@wailsio/runtime":
        return module_directory("github.com/wailsapp/wails/v3", "v3.0.0-beta.8")
    raise RuntimeError(f"npm package {name} has no bundled license file in {package_path}")


def copy_license_file(source: Path, destination_directory: Path, relative_root: Path) -> tuple[str, ...]:
    if not source.is_file():
        raise RuntimeError(f"asset license source does not exist: {source}")
    destination_directory.mkdir(parents=True, exist_ok=True)
    destination = destination_directory / "LICENSE"
    text = source.read_text(encoding="utf-8")
    normalized = "\n".join(line.rstrip() for line in text.splitlines()).rstrip() + "\n"
    destination.write_text(normalized, encoding="utf-8")
    return (destination.relative_to(relative_root).as_posix(),)


def collect_asset_dependencies(output: Path) -> list[Dependency]:
    manifest = json.loads(ASSET_RIGHTS_MANIFEST.read_text(encoding="utf-8"))
    if manifest.get("schemaVersion") != 1:
        raise RuntimeError("unsupported asset rights manifest schema")
    dependencies: list[Dependency] = []
    for name, rights in sorted(manifest.get("assets", {}).items()):
        asset = ICON_ROOT / str(rights["file"])
        digest = hashlib.sha256(asset.read_bytes()).hexdigest()
        expected = str(rights["sha256"]).lower()
        if digest != expected:
            raise RuntimeError(f"asset hash mismatch for {name}: {digest} != {expected}")
        license_source = ICON_ROOT / str(rights["licenseSource"])
        copied = copy_license_file(
            source=license_source,
            destination_directory=output / "licenses" / "assets" / safe_component(name),
            relative_root=output,
        )
        # An asset may be shipped altered, so long as the change is declared.
        # Requiring the note whenever the flag is set keeps "modified: true" from
        # reaching the notices as an unexplained assertion.
        modification = ""
        if rights.get("modified"):
            modification = str(rights.get("modificationNote", "")).strip()
            if not modification:
                raise RuntimeError(f"asset {name} is marked modified but records no modificationNote")
        dependencies.append(
            Dependency(
                ecosystem="asset",
                name=name,
                version=digest,
                license=str(rights["license"]),
                platforms=("frontend",),
                license_files=copied,
                modification=modification,
            )
        )
    return dependencies


def collect_npm_dependencies(output: Path) -> list[Dependency]:
    dependencies: list[Dependency] = []
    inventory = pnpm_license_inventory()
    for license_name, packages in sorted(inventory.items()):
        for package in sorted(packages, key=lambda value: str(value.get("name", ""))):
            name = str(package.get("name", "")).strip()
            versions = package.get("versions") or []
            paths = package.get("paths") or []
            if not name or len(versions) != 1 or not paths:
                raise RuntimeError(f"unexpected pnpm license entry: {package!r}")
            version = str(versions[0])
            package_path = Path(str(paths[0]))
            source = npm_license_source(name, package_path)
            destination = output / "licenses" / "npm" / f"{safe_component(name)}@{safe_component(version)}"
            copied = copy_license_files(source, destination, output)
            dependencies.append(
                Dependency(
                    ecosystem="npm",
                    name=name,
                    version=version,
                    license=license_name,
                    platforms=("frontend",),
                    license_files=copied,
                )
            )
    return dependencies


def render_notice(dependencies: Iterable[Dependency]) -> str:
    rows = sorted(dependencies)
    lines = [
        "# BootAgent Third-Party Notices",
        "",
        "This file is generated by `scripts/generate_third_party_licenses.py`.",
        "Do not edit it or the adjacent license copies by hand.",
        "",
        "The packages below are linked into a production Go binary or bundled into",
        "the embedded frontend. Full license and notice texts are included under",
        "`licenses/` and are shipped with every binary artifact.",
        "",
        "| Ecosystem | Package | Version | Targets | License | License files |",
        "| --- | --- | --- | --- | --- | --- |",
    ]
    for dependency in rows:
        files = "<br>".join(f"`{path}`" for path in dependency.license_files)
        platforms = ", ".join(dependency.platforms)
        lines.append(
            f"| {dependency.ecosystem} | `{dependency.name}` | `{dependency.version}` | "
            f"{platforms} | {dependency.license} | {files} |"
        )
    modified = [dependency for dependency in rows if dependency.modification]
    if modified:
        lines.extend([
            "",
            "## Modified third-party material",
            "",
            "Everything above is redistributed unmodified except the entries listed",
            "here. Their licenses permit modification and require it to be stated.",
            "",
        ])
        for dependency in modified:
            lines.append(f"- `{dependency.name}` ({dependency.ecosystem}): {dependency.modification}")
    lines.append("")
    return "\n".join(lines)


def generate(output: Path) -> None:
    if output.exists():
        shutil.rmtree(output)
    output.mkdir(parents=True)
    dependencies = collect_go_dependencies(output) + collect_npm_dependencies(output) + collect_asset_dependencies(output)
    dependencies.sort()
    (output / "THIRD_PARTY_NOTICES.md").write_text(render_notice(dependencies), encoding="utf-8")
    manifest = {
        "schema_version": 1,
        "generated_by": "scripts/generate_third_party_licenses.py",
        "dependencies": [dependency.as_json() for dependency in dependencies],
    }
    (output / "manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )


def tree_files(root: Path) -> dict[str, bytes]:
    if not root.exists():
        return {}
    return {
        path.relative_to(root).as_posix(): path.read_bytes()
        for path in sorted(root.rglob("*"))
        if path.is_file()
    }


def compare_trees(actual: Path, expected: Path) -> list[str]:
    actual_files = tree_files(actual)
    expected_files = tree_files(expected)
    differences: list[str] = []
    for name in sorted(expected_files.keys() - actual_files.keys()):
        differences.append(f"missing: {name}")
    for name in sorted(actual_files.keys() - expected_files.keys()):
        differences.append(f"unexpected: {name}")
    for name in sorted(actual_files.keys() & expected_files.keys()):
        if actual_files[name] != expected_files[name]:
            differences.append(f"changed: {name}")
            if name.endswith((".md", ".json", ".txt", "LICENSE", "NOTICE")):
                before = actual_files[name].decode("utf-8", errors="replace").splitlines()
                after = expected_files[name].decode("utf-8", errors="replace").splitlines()
                differences.extend(
                    difflib.unified_diff(before, after, fromfile=f"tracked/{name}", tofile=f"generated/{name}", lineterm="")
                )
    return differences


def verify_binary(binary: Path, platform: str, manifest_path: Path) -> None:
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    allowed = {
        (entry["name"], entry["version"])
        for entry in manifest["dependencies"]
        if entry["ecosystem"] == "go" and platform in entry["platforms"]
    }
    actual: set[tuple[str, str]] = set()
    for line in run(["go", "version", "-m", str(binary)]).splitlines():
        fields = line.split("\t")
        if len(fields) >= 4 and fields[1] == "dep":
            actual.add((fields[2], fields[3]))
    missing = sorted(actual - allowed)
    if missing:
        formatted = ", ".join(f"{name}@{version}" for name, version in missing)
        raise RuntimeError(f"{binary} contains unreviewed Go modules for {platform}: {formatted}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    parser.add_argument("--check", action="store_true", help="fail if tracked output differs")
    parser.add_argument("--verify-go-binary", type=Path, action="append", default=[])
    parser.add_argument("--verify-platform")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.verify_go_binary:
        if not args.verify_platform:
            raise SystemExit("--verify-platform is required with --verify-go-binary")
        for binary in args.verify_go_binary:
            verify_binary(binary, args.verify_platform, args.output / "manifest.json")
        return 0
    if args.check:
        with tempfile.TemporaryDirectory(prefix="bootagent-licenses-") as directory:
            generated = Path(directory) / "third_party"
            generate(generated)
            differences = compare_trees(args.output, generated)
        if differences:
            print("third-party license bundle is stale; regenerate with:", file=sys.stderr)
            print("  python3 scripts/generate_third_party_licenses.py", file=sys.stderr)
            print("\n".join(differences), file=sys.stderr)
            return 1
        return 0
    generate(args.output)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
