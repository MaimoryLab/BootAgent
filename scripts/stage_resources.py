#!/usr/bin/env python3
"""Copy the runtime resources a wheel must carry into the package directory.

`agents.lock.json` and the built frontend live at the repository root because
both the source checkout and the PyInstaller bundle read them from there. A
wheel only carries files inside the package directory, so without this step an
installed OneAgent starts and fails with "Cannot load Agent lock manifest".

Deliberately free of any setuptools import: `setup.py` drives this from a
build_py hook, but the tests must be able to exercise it on a bare Python 3.12,
which no longer ships setuptools.
"""

from __future__ import annotations

import shutil
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RESOURCES = ROOT / "oneagent" / "_resources"


def prune_stale_build_output(build_lib: Path | str) -> Path | None:
    """Drop a previously copied _resources tree from setuptools' build dir.

    build_py copies package data into build/lib but never prunes entries that
    changed or disappeared, and build/ survives between builds. Left alone, a
    stale copy is packaged in preference to whatever staging just produced --
    including an outdated agents.lock.json, which would silently undo the
    version locking the release policy depends on.
    """
    stale = Path(build_lib) / "oneagent" / "_resources"
    if stale.exists():
        shutil.rmtree(stale)
        return stale
    return None


def stage_resources(root: Path | None = None) -> Path:
    """Refresh <package>/_resources and return it."""
    base = root or ROOT
    resources = base / "oneagent" / "_resources"
    if resources.exists():
        shutil.rmtree(resources)
    resources.mkdir(parents=True)
    shutil.copy2(base / "agents.lock.json", resources / "agents.lock.json")
    frontend_dist = base / "frontend" / "dist"
    if frontend_dist.is_dir():
        shutil.copytree(frontend_dist, resources / "frontend" / "dist")
    return resources


if __name__ == "__main__":
    print(stage_resources())
