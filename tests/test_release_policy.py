from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

from oneagent import catalog, entrypoint
from oneagent.catalog import load_manifest
from scripts import build_release
from scripts.check_release import validate_manifest
from scripts.stage_resources import stage_resources


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

    def test_no_bare_http_server_reintroduces_the_reverse_dns_stall(self):
        # http.server's server_bind() calls socket.getfqdn(). On a host with no
        # reverse resolver that blocks ~35s per process, which is why the macOS
        # CI jobs used to spend 44s on a 9s test step. Every server must come
        # from a subclass that overrides the bind.
        offenders: list[str] = []
        for directory in ("oneagent", "scripts", "tests"):
            for path in sorted((ROOT / directory).glob("*.py")):
                text = path.read_text(encoding="utf-8")
                for line_number, line in enumerate(text.splitlines(), start=1):
                    if re.search(r"(?<![A-Za-z0-9_])HTTPServer\(\(", line):
                        offenders.append(f"{directory}/{path.name}:{line_number}")
        self.assertEqual(
            offenders,
            [],
            "use LocalHTTPServer (tests) or OneAgentHTTPServer (product) instead of a bare HTTPServer",
        )

    def test_release_scripts_default_to_unsigned_preview(self):
        source = (ROOT / "scripts" / "build_release.py").read_text(encoding="utf-8")
        self.assertIn('default="technical-preview-unsigned"', source)
        self.assertIn("Stable macOS builds require signing", source)
        self.assertIn("Stable Windows builds require Authenticode", source)

    def test_stable_signature_gate_inspects_the_artifact_not_an_env_var(self):
        # Setting ONEAGENT_MACOS_SIGNED=1 used to be enough to publish an
        # unsigned build as Stable. The gate must interrogate the binary.
        calls: list[list[str]] = []

        def refuse(args, **_kwargs):
            calls.append([str(item) for item in args])
            return subprocess.CompletedProcess(args, 1, "", "code object is not signed at all")

        with tempfile.TemporaryDirectory() as tmp:
            bundle = Path(tmp) / "OneAgent"
            bundle.mkdir()
            with patch.dict(os.environ, {"ONEAGENT_MACOS_SIGNED": "1", "ONEAGENT_WINDOWS_SIGNED": "1"}):
                # The pre-flight check is satisfied by the env var alone...
                build_release.ensure_unsigned_channel("stable", "macos")
                # ...but the artifact check still refuses an unsigned bundle.
                with patch.object(build_release.subprocess, "run", side_effect=refuse):
                    with self.assertRaises(SystemExit) as macos:
                        build_release.verify_stable_signature("stable", "macos", bundle)
                    self.assertIn("Developer ID signature", str(macos.exception))
                    self.assertEqual(calls[0][0], "codesign")

                    calls.clear()
                    with self.assertRaises(SystemExit) as windows:
                        build_release.verify_stable_signature("stable", "windows", bundle)
                    self.assertIn("Authenticode", str(windows.exception))
                    self.assertEqual(calls[0][0], "powershell")

            # A missing toolchain must fail closed rather than skip the check.
            with patch.object(build_release.subprocess, "run", side_effect=OSError("codesign missing")):
                with self.assertRaises(SystemExit):
                    build_release.verify_stable_signature("stable", "macos", bundle)

            # The unsigned preview channel never invokes the signing tools.
            with patch.object(build_release.subprocess, "run", side_effect=AssertionError("must not run")):
                build_release.verify_stable_signature("technical-preview-unsigned", "macos", bundle)
                build_release.verify_stable_signature("technical-preview-unsigned", "windows", bundle)

    def test_wheel_build_stages_the_runtime_resources(self):
        # A wheel only carries files inside the package directory. Without this
        # staging step an installed OneAgent starts and immediately fails with
        # "Cannot load Agent lock manifest". Staged into a throwaway tree so the
        # real working copy is untouched.
        with tempfile.TemporaryDirectory() as tmp:
            fake = Path(tmp).resolve()
            (fake / "oneagent").mkdir()
            (fake / "frontend" / "dist" / "assets").mkdir(parents=True)
            shutil.copy2(ROOT / "agents.lock.json", fake / "agents.lock.json")
            (fake / "frontend" / "dist" / "index.html").write_text("<!doctype html>", encoding="utf-8")
            (fake / "frontend" / "dist" / "assets" / "app.js").write_text("//", encoding="utf-8")

            staged = stage_resources(fake)
            self.assertEqual(
                json.loads((staged / "agents.lock.json").read_text(encoding="utf-8")),
                json.loads((ROOT / "agents.lock.json").read_text(encoding="utf-8")),
            )
            self.assertTrue((staged / "frontend" / "dist" / "index.html").is_file())
            self.assertTrue((staged / "frontend" / "dist" / "assets" / "app.js").is_file())

            # Re-staging must not accumulate files removed from the source.
            (fake / "frontend" / "dist" / "assets" / "app.js").unlink()
            staged = stage_resources(fake)
            self.assertFalse((staged / "frontend" / "dist" / "assets" / "app.js").exists())

        # The build must actually invoke the staging, and the declared package
        # data must cover what it produces.
        setup_source = (ROOT / "setup.py").read_text(encoding="utf-8")
        self.assertIn("stage_resources", setup_source)
        self.assertIn("build_py", setup_source)
        pyproject = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
        self.assertIn("_resources/agents.lock.json", pyproject)
        self.assertIn("_resources/frontend/dist", pyproject)

    def test_source_checkout_wins_over_a_stale_staging_directory(self):
        # Building a wheel locally leaves oneagent/_resources/ in the working
        # tree; it must never shadow the repository's real manifest.
        with tempfile.TemporaryDirectory() as tmp:
            # resource_root() resolves symlinks, and macOS hands out /var paths
            # that are really /private/var.
            root = Path(tmp).resolve()
            package = root / "oneagent"
            package.mkdir()
            (root / "agents.lock.json").write_text("{}", encoding="utf-8")
            stale = package / "_resources"
            stale.mkdir()
            (stale / "agents.lock.json").write_text("{}", encoding="utf-8")

            with patch.object(catalog, "__file__", str(package / "catalog.py")):
                self.assertEqual(catalog.resource_root(), root)
                # With no manifest beside the package this is an installed
                # distribution, so the staged copy is the only source.
                (root / "agents.lock.json").unlink()
                self.assertEqual(catalog.resource_root(), stale)

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
