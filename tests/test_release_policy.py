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
from oneagent.installer import Runtime, status_payload
from scripts import build_release
from scripts.check_release import inspect_zip, validate_manifest
from scripts.stage_resources import prune_stale_build_output, stage_resources


ROOT = Path(__file__).resolve().parents[1]


class ReleasePolicyTests(unittest.TestCase):
    def test_every_auto_agent_declares_what_the_installer_relies_on(self):
        # The installer derives the start command, the restart hint and the
        # credential route from these fields instead of restating them per Agent,
        # so a missing one is a broken Agent rather than a missing default. The
        # credential route in particular: Claude Code shipped reported as
        # configured while starting unauthenticated, because nothing declared
        # that it only reads its own variable names.
        manifest = load_manifest()
        for agent_id, meta in manifest["agents"].items():
            if meta.get("config_mode") != "auto":
                continue
            with self.subTest(agent=agent_id):
                self.assertTrue(meta.get("command"), f"{agent_id} has no command")
                self.assertTrue(meta.get("config_path"), f"{agent_id} has no config_path")
                self.assertTrue(meta.get("config_adapter"), f"{agent_id} has no config_adapter")
                delivery = meta.get("credential_delivery")
                self.assertIn(delivery, {"oneagent_env", "native_env", "config_file"})
                if delivery == "native_env":
                    # Otherwise the declaration promises native variables and the
                    # env file would define none of them.
                    declared = meta.get("env_vars") or {}
                    self.assertTrue(declared.get("api_key"), f"{agent_id} names no credential variable")
                    for field, name in declared.items():
                        self.assertRegex(str(name), r"^[A-Z][A-Z0-9_]*$", f"{agent_id}.{field}")

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
        # Scans oneagent/ and scripts/ to match the Docker cleanroom's
        # policy-scan exactly. When this test covered only oneagent/, a
        # violation under scripts/ passed every local check and surfaced only
        # after a full container run in CI.
        for directory in ("oneagent", "scripts"):
            sources = "\n".join(
                path.read_text(encoding="utf-8") for path in sorted((ROOT / directory).glob("*.py"))
            )
            with self.subTest(directory=directory):
                # The cleanroom greps raw text, so a comment containing the
                # literal token trips it too. Describe the rule without
                # spelling it out rather than softening the scan.
                self.assertNotRegex(sources, r"shell\s*=\s*True")
                self.assertNotRegex(sources, r"curl\s+[^\n|]+\|\s*(?:ba)?sh")

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

    def test_external_tools_are_resolved_to_an_absolute_path(self):
        # Windows ships npm as npm.cmd and CreateProcess only appends .exe to a
        # bare name, so subprocess.run(["npm", ...]) raises FileNotFoundError
        # there. shutil.which honours PATHEXT; shell=True is not an option.
        with patch.object(build_release.shutil, "which", return_value=r"C:\Program Files\nodejs\npm.cmd"):
            self.assertEqual(
                build_release.resolve_tool("npm", "install Node.js"),
                r"C:\Program Files\nodejs\npm.cmd",
            )
        with patch.object(build_release.shutil, "which", return_value=None):
            with self.assertRaises(SystemExit) as missing:
                build_release.resolve_tool("npm", "install Node.js")
        self.assertIn("install Node.js", str(missing.exception))

        source = (ROOT / "scripts" / "build_release.py").read_text(encoding="utf-8")
        self.assertNotIn('run(["npm"', source)
        self.assertIn('resolve_tool("npm"', source)

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

    def test_stale_build_output_cannot_ship_an_outdated_manifest(self):
        # setuptools copies package data into build/lib and never prunes it,
        # and build/ survives between builds. A wheel was therefore packaging
        # whatever _resources happened to be there -- verified by breaking the
        # staging step and still getting a complete wheel. For a project whose
        # premise is version locking, shipping a stale agents.lock.json is the
        # worst version of that bug.
        with tempfile.TemporaryDirectory() as tmp:
            build_lib = Path(tmp).resolve() / "lib"
            stale = build_lib / "oneagent" / "_resources"
            (stale / "frontend" / "dist").mkdir(parents=True)
            (stale / "agents.lock.json").write_text('{"stale": true}', encoding="utf-8")

            self.assertEqual(prune_stale_build_output(build_lib), stale)
            self.assertFalse(stale.exists())
            # Sibling modules in build/lib must survive; only the staged tree goes.
            self.assertTrue((build_lib / "oneagent").is_dir())

            # Nothing staged yet is not an error.
            self.assertIsNone(prune_stale_build_output(build_lib))

        setup_source = (ROOT / "setup.py").read_text(encoding="utf-8")
        self.assertIn("prune_stale_build_output(self.build_lib)", setup_source)

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

    def test_source_archive_carries_no_generated_or_installed_files(self):
        # The allowlist names whole directories and walks them, so adding a root
        # silently picks up whatever a build left inside it. site/src/generated
        # holds the public release index, whose checksums describe one machine's
        # artifacts -- exactly what ADR-006 keeps out of the tree. Asserting on
        # the resolved file list rather than the source text is what catches it.
        selected = build_release.source_files()
        self.assertTrue(selected, "source archive must not be empty")
        offenders = [
            str(path.relative_to(ROOT))
            for path in selected
            if {"generated", "node_modules", "__pycache__"} & set(path.parts)
            # public/downloads holds copied release artifacts; the page that
            # renders them is ordinary source and shares only the word.
            or path.is_relative_to(ROOT / "site" / "public" / "downloads")
        ]
        self.assertEqual(offenders, [])
        # Nothing in a source archive should be release-sized; a stray artifact
        # or bundled dependency tree shows up here first.
        oversized = [
            f"{path.relative_to(ROOT)} ({path.stat().st_size // 1024} KiB)"
            for path in selected
            if path.stat().st_size > 1_000_000
        ]
        self.assertEqual(oversized, [])

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

    def test_install_path_is_verified_offline_in_ci_and_for_real_before_release(self):
        # The command that installs an Agent was only ever exercised through a
        # replaced runner that ignored its argv, so the contract suite has to run
        # everywhere, and the real install has to run before a release.
        ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
        rc = (ROOT / ".github" / "workflows" / "release-candidate.yml").read_text(encoding="utf-8")
        self.assertIn("tests.test_install_contract", ci)
        self.assertIn("tests.test_install_contract", rc)
        # Real installation stays out of ordinary CI: it would hit the registry
        # on every commit.
        self.assertNotIn("real_install_test.sh", ci)
        self.assertIn("tests/real_install_test.sh", rc)
        self.assertIn("scripts/verify_locked_agents.py", rc)

    def test_existing_configuration_is_exercised_in_both_cleanrooms(self):
        # Every cleanroom stage starts from an empty HOME, so a machine that
        # already has Agent configuration -- the normal case for a tool meant to
        # manage them over time -- had no coverage anywhere. That is the state
        # status_payload used to report as configured with a null provider.
        container = (ROOT / "scripts" / "run_container_cleanroom.sh").read_text(encoding="utf-8")
        macos = (ROOT / "tests" / "macos_cleanroom_test.sh").read_text(encoding="utf-8")
        ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
        self.assertIn("tests/existing_config_test.sh", container)
        self.assertIn("tests/existing_config_test.sh", macos)
        self.assertIn("tests.test_config_discovery", ci)

    def test_config_readers_never_extract_a_credential(self):
        # Three of the five formats hold the key in plain text next to the
        # endpoint, so a reader that reached for one field too many would put it
        # straight into /api/status.
        source = (ROOT / "oneagent" / "installer.py").read_text(encoding="utf-8")
        readers = source.split("def _detected(", 1)[1].split("def _write_agent_config", 1)[0]
        for forbidden in ["AUTH_TOKEN", "API_KEY", "apiKey", "api_key", "hasKey"]:
            with self.subTest(field=forbidden):
                # env_key is the variable *name* Codex reads from, not a value.
                cleaned = readers.replace('"env_key"', "").replace("env_key", "")
                self.assertNotIn(forbidden, cleaned)

    def test_mirrors_are_public_read_only_registries_not_our_own_storage(self):
        # An authorised mirror is permitted; redistributing a proprietary Agent
        # from storage we run is not. Enforced here as well as in the install
        # contract because it is a distribution rule, not an install detail.
        for mirror_id, meta in catalog.PACKAGE_MIRRORS.items():
            with self.subTest(mirror=mirror_id):
                self.assertTrue(str(meta["registry"]).startswith("https://"))
                self.assertTrue(str(meta["upstream"]).startswith("https://"))
                for forbidden in ("oneagent", "maimory"):
                    self.assertNotIn(forbidden, str(meta["registry"]).lower())

    def test_frontend_output_may_not_reference_a_cdn(self):
        # The policy forbids CDN and remote-font references in frontend output,
        # but only source maps were ever checked. Agent marks are inlined for
        # this reason, and nothing should quietly start fetching one instead.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)

            def archive_with(name: str, body: bytes) -> Path:
                path = root / f"OneAgent-{abs(hash(body)) % 10**6}.zip"
                with zipfile.ZipFile(path, "w") as archive:
                    archive.writestr("OneAgent/agents.lock.json", "{}")
                    archive.writestr("OneAgent/THIRD_PARTY_NOTICES.md", "notices")
                    archive.writestr(name, body)
                return path

            remote = archive_with(
                "OneAgent/frontend/dist/assets/app.js",
                b'var a="<img src=\'https://cdn.example/mark.svg\'>"',
            )
            self.assertTrue(
                any("remote asset reference" in problem for problem in inspect_zip(remote))
            )

            font = archive_with(
                "OneAgent/frontend/dist/assets/app.css",
                b'@import url("https://fonts.googleapis.com/css?family=Inter");',
            )
            self.assertTrue(any("remote asset reference" in problem for problem in inspect_zip(font)))

            # An inlined mark and an XML namespace are not resource loads, and a
            # link in copy is only a link — none may trip the scan.
            clean = archive_with(
                "OneAgent/frontend/dist/assets/app.js",
                b'var a="data:image/svg+xml,%3Csvg xmlns=\'http://www.w3.org/2000/svg\'%3E"',
            )
            self.assertEqual(
                [p for p in inspect_zip(clean) if "remote asset" in p],
                [],
            )

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


class LockIsTheSourceOfTruthTests(unittest.TestCase):
    """Behaviour must be derived from agents.lock.json, not restated in Python.

    The defect that motivated this suite: a fact lived in two places (the lock
    and a hardcoded id set), the lock changed, and the behaviour did not. Each
    test iterates the manifest and asserts the runtime follows whatever it
    declares, so a new Agent added only to the lock works without a code edit.
    """

    def test_backups_are_derived_from_each_agents_config_path(self):
        manifest = load_manifest()
        auto_with_config = [
            agent_id
            for agent_id, meta in manifest["agents"].items()
            if meta.get("config_mode") == "auto" and meta.get("config_path")
        ]
        # More than the two the payload used to hardcode, or the test proves
        # nothing about generalisation.
        self.assertGreater(len(auto_with_config), 2)
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime.create(home=home, os_id="linux", which=lambda _name: None)
            target = "opencode"
            config_path = home / manifest["agents"][target]["config_path"]
            config_path.parent.mkdir(parents=True, exist_ok=True)
            (config_path.parent / f"{config_path.name}.backup-20260729").write_text("old", encoding="utf-8")

            backups = status_payload(runtime)["backups"]
            # Every auto Agent with a config_path is a key, and only the one with
            # a backup on disk reports True -- the glob came from config_path.
            for agent_id in auto_with_config:
                self.assertIn(agent_id, backups)
                self.assertEqual(backups[agent_id], agent_id == target, agent_id)

    def test_windows_install_gating_follows_windows_prerequisites(self):
        manifest = load_manifest()
        gated = {
            agent_id
            for agent_id, meta in manifest["agents"].items()
            if meta.get("windows_prerequisites")
        }
        self.assertIn("claude-code", gated)
        with tempfile.TemporaryDirectory() as tmp:
            # npm present, every prerequisite absent: only Agents that declare a
            # Windows prerequisite lose canInstall.
            runtime = Runtime.create(
                home=Path(tmp),
                os_id="windows",
                which=lambda name: "npm.cmd" if name == "npm" else None,
                env={"USERPROFILE": tmp},
            )
            can_install = status_payload(runtime)["capabilities"]["canInstall"]
            for agent_id, meta in manifest["agents"].items():
                if meta.get("config_mode") != "auto" or (meta.get("package") or {}).get("manager") != "npm":
                    continue
                with self.subTest(agent=agent_id):
                    self.assertEqual(can_install[agent_id], agent_id not in gated)

    def test_providers_module_decides_by_protocol_not_agent_id(self):
        # provider_config_base used to compare the adapter against "claude-code";
        # the inference protocol is the real input. Restating an Agent id here
        # would silently re-couple providers.py to the lock's identifiers.
        source = (ROOT / "oneagent" / "providers.py").read_text(encoding="utf-8")
        self.assertNotIn('"claude-code"', source)
        self.assertNotIn("'claude-code'", source)


if __name__ == "__main__":
    unittest.main()
