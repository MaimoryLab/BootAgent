"""Stage runtime resources into the package before a wheel or sdist is built.

The copying itself lives in scripts/stage_resources.py so the tests can
exercise it without setuptools installed; this module only wires it into the
build. Loaded by explicit path rather than a plain import so it does not depend
on the repository root being on sys.path during a PEP 517 build.
"""

from __future__ import annotations

import importlib.util
from pathlib import Path

from setuptools import setup
from setuptools.command.build_py import build_py

ROOT = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location(
    "oneagent_stage_resources", ROOT / "scripts" / "stage_resources.py"
)
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)
stage_resources = _module.stage_resources


class BuildPyWithResources(build_py):
    def run(self) -> None:
        stage_resources(ROOT)
        super().run()


if __name__ == "__main__":
    setup(cmdclass={"build_py": BuildPyWithResources})
