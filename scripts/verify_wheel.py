#!/usr/bin/env python3
"""Build the wheel, install it into a throwaway virtualenv and exercise it.

The wheel is the only artifact whose resources are staged rather than read from
the repository, so a packaging mistake is invisible to every other check: the
source checkout and the PyInstaller bundle keep working while `pip install`
produces something that fails on first run with "Cannot load Agent lock
manifest". This runs the installed console script from a directory outside the
repository so nothing can fall back to the working copy.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def venv_bin(env_dir: Path, name: str) -> Path:
    if sys.platform == "win32":
        return env_dir / "Scripts" / f"{name}.exe"
    return env_dir / "bin" / name


def build_wheel(destination: Path) -> Path:
    destination.mkdir(parents=True, exist_ok=True)
    # --no-cache-dir is load-bearing: pip happily serves a previously built
    # wheel for an unchanged version, so without it this check silently
    # validates a stale artifact instead of the current tree.
    subprocess.run(
        [
            sys.executable,
            "-m",
            "pip",
            "wheel",
            str(ROOT),
            "--no-deps",
            "--no-cache-dir",
            "-w",
            str(destination),
        ],
        check=True,
        cwd=ROOT,
    )
    wheels = sorted(destination.glob("oneagent_launcher-*.whl"))
    if not wheels:
        raise SystemExit(f"no wheel produced in {destination}")
    return wheels[-1]


def verify_wheel(wheel: Path) -> None:
    with tempfile.TemporaryDirectory() as tmp:
        workspace = Path(tmp).resolve()
        env_dir = workspace / "venv"
        # Spawned rather than venv.EnvBuilder(): relocatable CPython builds
        # (uv, python-build-standalone) resolve sys._base_executable
        # incorrectly in-process and their ensurepip then fails to start.
        subprocess.run([sys.executable, "-m", "venv", str(env_dir)], check=True)
        python = venv_bin(env_dir, "python")
        subprocess.run(
            [str(python), "-m", "pip", "install", "--quiet", "--disable-pip-version-check", str(wheel)],
            check=True,
        )

        # Resources must resolve to the staged copy inside site-packages.
        probe = subprocess.run(
            [
                str(python),
                "-c",
                "import json;"
                " from oneagent.catalog import resource_root, agent_catalog;"
                " from oneagent.server import static_root;"
                " root = resource_root();"
                " print(json.dumps({"
                "  'manifest': (root / 'agents.lock.json').is_file(),"
                "  'agents': len(agent_catalog()),"
                "  'frontend': (static_root() / 'index.html').is_file(),"
                " }))",
            ],
            check=True,
            capture_output=True,
            text=True,
            cwd=workspace,
        )
        state = json.loads(probe.stdout.strip().splitlines()[-1])
        if not state["manifest"]:
            raise SystemExit("installed wheel cannot find agents.lock.json")
        if state["agents"] < 1:
            raise SystemExit("installed wheel loaded an empty Agent catalog")
        if not state["frontend"]:
            raise SystemExit("installed wheel does not carry the built frontend")

        # The console script must run outside the repository.
        result = subprocess.run(
            [
                str(venv_bin(env_dir, "oneagent")),
                "--agent",
                "openclaw",
                "--check-agent-only",
                "--json",
                "--home",
                str(workspace / "home"),
            ],
            check=True,
            capture_output=True,
            text=True,
            cwd=workspace,
        )
        payload = json.loads(result.stdout.strip().splitlines()[-1])
        if not payload.get("ok"):
            raise SystemExit(f"installed CLI reported failure: {payload}")

        print(f"wheel verified: {state['agents']} agents, frontend bundled, CLI ok")


def main() -> None:
    parser = argparse.ArgumentParser(description="Build and smoke-test the OneAgent wheel")
    parser.add_argument("--wheel", type=Path, help="Verify an existing wheel instead of building one")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory() as tmp:
        wheel = args.wheel or build_wheel(Path(tmp) / "dist")
        verify_wheel(wheel)


if __name__ == "__main__":
    main()
