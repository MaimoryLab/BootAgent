from __future__ import annotations

import hashlib
import json
import re
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

from oneagent import entrypoint
from oneagent.catalog import load_manifest
from scripts.check_release import validate_manifest


ROOT = Path(__file__).resolve().parents[1]


class ReleasePolicyTests(unittest.TestCase):
    def test_lock_manifest_never_uses_latest_or_unapproved_manager(self):
        manifest = load_manifest()
        for agent_id, meta in manifest["agents"].items():
            package = meta.get("package")
            if not package:
                continue
            with self.subTest(agent=agent_id):
                self.assertNotEqual(package["version"], "latest")
                self.assertIn(package["manager"], {"npm", "uv"})
                if package["manager"] == "npm":
                    self.assertRegex(package.get("integrity", ""), r"^sha512-")
                if agent_id == "aider":
                    self.assertEqual(package["manager"], "uv")
                self.assertTrue(package["source"].startswith("https://"))
                self.assertTrue(package["license_url"].startswith("https://"))

    def test_runtime_code_does_not_use_shell_true_or_curl_pipe(self):
        runtime_sources = "\n".join(path.read_text(encoding="utf-8") for path in (ROOT / "oneagent").glob("*.py"))
        self.assertNotRegex(runtime_sources, r"shell\s*=\s*True")
        self.assertNotRegex(runtime_sources, r"curl\s+[^\n|]+\|\s*(?:ba)?sh")

    def test_release_scripts_default_to_unsigned_preview(self):
        source = (ROOT / "scripts" / "build_release.py").read_text(encoding="utf-8")
        self.assertIn('default="technical-preview-unsigned"', source)
        self.assertIn("Stable macOS builds require signing", source)
        self.assertIn("Stable Windows builds require Authenticode", source)

    def test_source_launchers_require_python_312(self):
        for relative in ["scripts/install.sh", "scripts/install.ps1", "scripts/gui.py"]:
            with self.subTest(path=relative):
                self.assertIn("3.12", (ROOT / relative).read_text(encoding="utf-8"))

    def test_frontend_build_has_no_remote_assets_or_source_maps(self):
        dist = ROOT / "frontend" / "dist"
        if not dist.exists():
            self.skipTest("frontend production build is not present")
        self.assertFalse(list(dist.rglob("*.map")))
        text = "\n".join(
            path.read_text(encoding="utf-8", errors="ignore")
            for path in dist.rglob("*")
            if path.is_file() and path.suffix in {".html", ".js", ".css"}
        )
        self.assertNotRegex(text, r"https?://(?:fonts\.|cdn\.|unpkg\.|jsdelivr\.)")

    def test_packaged_entrypoint_routes_no_args_to_gui_and_args_to_cli(self):
        with patch.object(entrypoint.server, "main") as gui, patch.object(entrypoint.cli, "main") as cli:
            with patch("sys.argv", ["OneAgent"]):
                entrypoint.main()
            gui.assert_called_once_with()
            cli.assert_not_called()

        with patch.object(entrypoint.server, "main") as gui, patch.object(entrypoint.cli, "main") as cli:
            with patch("sys.argv", ["OneAgent", "--json"]):
                entrypoint.main()
            cli.assert_called_once_with()
            gui.assert_not_called()

    def test_source_archive_allowlist_excludes_runtime_and_concept_assets(self):
        source = (ROOT / "scripts" / "build_release.py").read_text(encoding="utf-8")
        allowlist = source.split("def source_files", 1)[1].split("def build_source_zip", 1)[0]
        self.assertNotIn('"node_modules"', allowlist)
        self.assertNotIn('"output"', allowlist)
        self.assertIn('"Dockerfile.test"', allowlist)
        self.assertIn('".dockerignore"', allowlist)

    def test_docker_cleanroom_contract_is_isolated_and_linux_only(self):
        dockerfile = (ROOT / "Dockerfile.test").read_text(encoding="utf-8")
        wrapper = (ROOT / "scripts" / "test_docker_cleanroom.sh").read_text(encoding="utf-8")
        runner = (ROOT / "scripts" / "run_container_cleanroom.sh").read_text(encoding="utf-8")
        dockerignore = (ROOT / ".dockerignore").read_text(encoding="utf-8")

        self.assertIn(
            "mcr.microsoft.com/playwright:v1.61.1-noble@sha256:5b8f294aff9041b7191c34a4bab3ac270157a28774d4b0660e9743297b697e48",
            dockerfile,
        )
        self.assertIn("USER pwuser", dockerfile)
        self.assertIn("Acquire::Retries=3", dockerfile)
        self.assertIn("--network none", wrapper)
        self.assertIn("--shm-size=1g", wrapper)
        self.assertNotIn("/var/run/docker.sock", wrapper)
        self.assertNotRegex(wrapper, r"(?:source|src)=\$?\{?HOME")
        self.assertIn('expected_platform="linux"', runner)
        self.assertIn('id -u', runner)
        self.assertIn("ONEAGENT_DISABLE_BROWSER", runner)
        self.assertIn("PASSWORD", runner)
        self.assertIn("npm run e2e", runner)
        self.assertIn("coverage-summary.json", runner)
        for required in [".git", ".env*", "frontend/node_modules", "release", "output"]:
            with self.subTest(pattern=required):
                self.assertIn(required, dockerignore)

    def test_macos_cleanroom_requires_real_darwin_and_native_binary(self):
        source = (ROOT / "tests" / "macos_cleanroom_test.sh").read_text(encoding="utf-8")
        self.assertIn('uname -s', source)
        self.assertIn('Darwin', source)
        self.assertIn('env -i', source)
        self.assertIn('stat -f', source)
        self.assertIn('ONEAGENT_PACKAGED_BINARY', source)
        self.assertIn('Unexpected preinstalled Agent', source)
        self.assertNotIn('ONEAGENT_TEST_OS', source)

    def test_ci_runs_container_and_real_macos_cleanrooms(self):
        ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
        rc = (ROOT / ".github" / "workflows" / "release-candidate.yml").read_text(encoding="utf-8")
        self.assertIn("container-cleanroom:", ci)
        self.assertIn("scripts/test_docker_cleanroom.sh", ci)
        self.assertIn("pyinstaller==6.21.0", ci)
        self.assertIn("tests/macos_cleanroom_test.sh", ci)
        self.assertIn("ONEAGENT_PACKAGED_BINARY", ci)
        self.assertIn("tests/macos_cleanroom_test.sh", rc)

    def test_release_manifest_checks_artifact_and_checksum_integrity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            artifact = root / "OneAgent-1.0.0-technical-preview-unsigned-linux-x64.zip"
            lock = {
                "schema_version": 1,
                "agents": {"codex": {"package": {"version": "1.2.3"}}},
            }
            with zipfile.ZipFile(artifact, "w") as archive:
                archive.writestr("OneAgent/agents.lock.json", json.dumps(lock))
                archive.writestr("OneAgent/THIRD_PARTY_NOTICES.md", "notices")

            digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
            manifest = root / "release-manifest-linux-x64.json"
            payload = {
                "schema_version": 1,
                "channel": "technical-preview-unsigned",
                "unsigned": True,
                "agent_versions": {"codex": "1.2.3"},
                "artifacts": [{"file": artifact.name, "sha256": digest, "bytes": artifact.stat().st_size}],
            }
            manifest.write_text(json.dumps(payload), encoding="utf-8")
            manifest_digest = hashlib.sha256(manifest.read_bytes()).hexdigest()
            checksum = root / "SHA256SUMS-linux-x64.txt"
            checksum.write_text(
                f"{digest}  {artifact.name}\n{manifest_digest}  {manifest.name}\n",
                encoding="utf-8",
            )
            self.assertEqual(validate_manifest(manifest), [])

            payload["artifacts"][0]["sha256"] = "0" * 64
            manifest.write_text(json.dumps(payload), encoding="utf-8")
            problems = validate_manifest(manifest)
            self.assertTrue(any("artifact checksum mismatch" in problem for problem in problems))
            self.assertTrue(any("checksum file mismatch" in problem for problem in problems))


if __name__ == "__main__":
    unittest.main()
