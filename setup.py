"""Stage runtime resources into the package before a wheel or sdist is built.

`agents.lock.json` and the built frontend live at the repository root because
both the source checkout and the PyInstaller bundle read them from there. A
wheel, however, only carries files inside the package directory, so an
installed OneAgent would otherwise start and immediately fail with "Cannot load
Agent lock manifest". Copy them into `oneagent/_resources/` at build time so
`pip`/`uv tool` installs work without a repository present.

Kept as a build step rather than a manual pre-step so a wheel cannot be
produced without its resources.
"""

from __future__ import annotations

import shutil
from pathlib import Path

from setuptools import setup
from setuptools.command.build_py import build_py

ROOT = Path(__file__).resolve().parent
RESOURCES = ROOT / "oneagent" / "_resources"


def stage_resources() -> None:
    if RESOURCES.exists():
        shutil.rmtree(RESOURCES)
    RESOURCES.mkdir(parents=True)
    shutil.copy2(ROOT / "agents.lock.json", RESOURCES / "agents.lock.json")
    frontend_dist = ROOT / "frontend" / "dist"
    if frontend_dist.is_dir():
        shutil.copytree(frontend_dist, RESOURCES / "frontend" / "dist")


class BuildPyWithResources(build_py):
    def run(self) -> None:
        stage_resources()
        super().run()


# setuptools' PEP 517 backend execs this file with __name__ == "__main__". The
# guard lets the tests import stage_resources() without triggering a build.
if __name__ == "__main__":
    setup(cmdclass={"build_py": BuildPyWithResources})
