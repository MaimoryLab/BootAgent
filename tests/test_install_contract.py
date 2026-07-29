"""Contracts for the real installation path.

Every other suite replaces Runtime.runner and never looks at the command it was
handed, so the argv that actually installs an Agent was unverified: a lock entry
could lose its version and every test would still pass. These assert on the
command instead of executing it, which keeps them offline and instant.
"""

from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from oneagent.catalog import (
    OFFICIAL_NPM_REGISTRY,
    PACKAGE_MIRRORS,
    agent_catalog,
    public_mirrors,
)
from oneagent.errors import OneAgentError
from oneagent.installer import (
    InstallOptions,
    Runtime,
    install_locked_agent,
    install_many,
    resolve_registry,
)


class RecordingRunner:
    """Captures argv and env, and answers --version and npm view from a table.

    The integrity check runs `npm view <spec> dist.integrity` before installing,
    so a runner that answers everything with a generic success would fail that
    check instead of reaching the install.
    """

    def __init__(
        self,
        versions: dict[str, str] | None = None,
        returncode: int = 0,
        integrity: str | None = None,
        integrity_returncode: int = 0,
    ) -> None:
        self.calls: list[list[str]] = []
        self.envs: list[dict[str, str]] = []
        self.versions = versions or {}
        self.returncode = returncode
        self.integrity = integrity
        self.integrity_returncode = integrity_returncode

    def __call__(self, args, **kwargs):  # noqa: ANN001 - mirrors subprocess.run
        argv = [str(item) for item in args]
        self.calls.append(argv)
        self.envs.append(dict(kwargs.get("env") or {}))
        if "view" in argv and "dist.integrity" in argv:
            if self.integrity_returncode:
                return subprocess.CompletedProcess(argv, self.integrity_returncode, "", "not found")
            # Default to whatever the manifest holds, so a test that is not
            # about integrity does not have to restate the hash.
            reported = self.integrity if self.integrity is not None else self._manifest_integrity(argv)
            return subprocess.CompletedProcess(argv, 0, f"{reported}\n", "")
        for command, version in self.versions.items():
            if argv[0].endswith(command) and "--version" in argv:
                return subprocess.CompletedProcess(argv, 0, f"{command} {version}\n", "")
        if "install" in argv:
            return subprocess.CompletedProcess(argv, self.returncode, "", "boom" if self.returncode else "")
        return subprocess.CompletedProcess(argv, 0, "", "")

    @staticmethod
    def _manifest_integrity(argv: list[str]) -> str:
        spec = argv[2]
        name = spec.rsplit("@", 1)[0]
        for meta in agent_catalog().values():
            package = meta.get("package") or {}
            if package.get("name") == name:
                return str(package.get("integrity", ""))
        return ""

    @property
    def install_calls(self) -> list[list[str]]:
        return [call for call in self.calls if "install" in call]

    @property
    def registries(self) -> list[str]:
        return [env["npm_config_registry"] for env in self.envs if "npm_config_registry" in env]


def runtime_with(
    *,
    runner: RecordingRunner,
    present: set[str] | None = None,
    os_id: str = "linux",
    env: dict[str, str] | None = None,
) -> Runtime:
    available = {"npm", "uv", "python3.12"} if present is None else present
    return Runtime(
        home=Path("/tmp/oneagent-contract"),
        os_id=os_id,
        runner=runner,
        which=lambda name: f"/usr/bin/{name}" if name in available else None,
        env=env if env is not None else {},
    )


class InstallCommandTests(unittest.TestCase):
    """The two Agents this verification covers, plus the uv outlier."""

    def install(self, agent_id: str, runtime: Runtime, **kwargs):
        result = install_locked_agent(
            runtime,
            agent_id,
            agent_catalog()[agent_id],
            enforce_locked=kwargs.pop("enforce_locked", True),
            latest=kwargs.pop("latest", False),
            timeout=kwargs.pop("timeout", 180),
        )
        return result

    def test_installs_the_version_the_lock_manifest_names(self):
        # The version must come from the manifest, so bumping the lock is enough
        # to change what gets installed and a hardcoded version cannot drift.
        catalog = agent_catalog()
        for agent_id in ("codex", "claude-code"):
            with self.subTest(agent=agent_id):
                package = catalog[agent_id]["package"]
                runner = RecordingRunner()
                result = self.install(agent_id, runtime_with(runner=runner, present={"npm"}))
                self.assertEqual(
                    runner.install_calls[0],
                    ["/usr/bin/npm", "install", "-g", f"{package['name']}@{package['version']}"],
                )
                self.assertTrue(result["installed"])
                self.assertEqual(result["version"], package["version"])

    def test_pinned_by_default_and_floating_only_when_asked(self):
        # A spec without @version is what "latest" means; it must never be the
        # default, since the whole premise of the lock manifest is pinning.
        pinned = RecordingRunner()
        self.install("codex", runtime_with(runner=pinned, present={"npm"}))
        locked = agent_catalog()["codex"]["package"]["version"]
        self.assertEqual(pinned.install_calls[0][-1], f"@openai/codex@{locked}")

        floating = RecordingRunner()
        self.install("codex", runtime_with(runner=floating, present={"npm"}), latest=True)
        self.assertEqual(floating.install_calls[0][-1], "@openai/codex")

    def test_already_at_the_locked_version_installs_nothing(self):
        catalog = agent_catalog()
        locked = catalog["codex"]["package"]["version"]
        runner = RecordingRunner(versions={"codex": locked})
        result = self.install("codex", runtime_with(runner=runner, present={"npm", "codex"}))
        self.assertEqual(runner.install_calls, [])
        self.assertFalse(result["installed"])
        self.assertEqual(result["version"], locked)

    def test_an_older_installation_is_upgraded_to_the_locked_version(self):
        catalog = agent_catalog()
        locked = catalog["codex"]["package"]["version"]
        runner = RecordingRunner(versions={"codex": "0.0.1"})
        result = self.install("codex", runtime_with(runner=runner, present={"npm", "codex"}))
        self.assertEqual(runner.install_calls[0][-1], f"@openai/codex@{locked}")
        self.assertTrue(result["installed"])

    def test_an_existing_install_is_left_alone_when_the_lock_is_not_enforced(self):
        runner = RecordingRunner(versions={"codex": "0.0.1"})
        result = self.install(
            "codex", runtime_with(runner=runner, present={"npm", "codex"}), enforce_locked=False
        )
        self.assertEqual(runner.install_calls, [])
        self.assertEqual(result["version"], "0.0.1")

    def test_aider_uses_uv_with_an_existing_python_and_no_downloads(self):
        # Aider is the one uv package; --no-python-downloads is a product
        # promise (OneAgent never fetches a Python runtime behind the user).
        runner = RecordingRunner()
        self.install("aider", runtime_with(runner=runner, present={"uv", "python3.12"}))
        argv = runner.install_calls[0]
        self.assertEqual(argv[:4], ["/usr/bin/uv", "tool", "install", "--force"])
        self.assertIn("--no-python-downloads", argv)
        self.assertTrue(argv[-1].startswith("aider-chat=="))


class RegistryTests(unittest.TestCase):
    """An authorised mirror changes where a package comes from, nothing else."""

    def install(self, runtime: Runtime, agent_id: str = "codex", **kwargs):
        return install_locked_agent(
            runtime,
            agent_id,
            agent_catalog()[agent_id],
            enforce_locked=True,
            latest=kwargs.pop("latest", False),
            timeout=180,
            registry=kwargs.pop("registry", ""),
        )

    def test_the_official_registry_is_the_default_and_sets_nothing(self):
        # A mirror must always be an explicit choice: switching automatically
        # would leave the user unable to tell where a package came from.
        runner = RecordingRunner()
        result = self.install(runtime_with(runner=runner, present={"npm"}))
        self.assertEqual(runner.registries, [])
        self.assertEqual(result["registry"], OFFICIAL_NPM_REGISTRY)

    def test_a_named_mirror_reaches_npm_through_its_environment(self):
        runner = RecordingRunner()
        result = self.install(runtime_with(runner=runner, present={"npm"}), registry="npmmirror")
        expected = PACKAGE_MIRRORS["npmmirror"]["registry"]
        self.assertEqual(result["registry"], expected)
        self.assertTrue(runner.registries)
        self.assertTrue(all(value == expected for value in runner.registries))
        # The command itself is unchanged: the pinned spec still governs.
        locked = agent_catalog()["codex"]["package"]["version"]
        self.assertEqual(runner.install_calls[0][-1], f"@openai/codex@{locked}")

    def test_an_explicit_https_url_is_accepted_and_normalised(self):
        self.assertEqual(resolve_registry("https://npm.example.com"), "https://npm.example.com/")
        self.assertEqual(resolve_registry("https://npm.example.com/"), "https://npm.example.com/")
        self.assertEqual(resolve_registry(""), OFFICIAL_NPM_REGISTRY)
        self.assertEqual(resolve_registry("official"), OFFICIAL_NPM_REGISTRY)

    def test_plaintext_and_malformed_registries_are_refused(self):
        # A registry URL lands in the installer environment and the install log,
        # so http:// and embedded credentials are both unacceptable.
        for value in [
            "http://npm.example.com",
            "ftp://npm.example.com",
            "https://user:pass@npm.example.com",
            "https://",
            "not-a-url",
            "https://npm.example.com/\nX",
        ]:
            with self.subTest(registry=value):
                with self.assertRaises(OneAgentError) as caught:
                    resolve_registry(value)
                self.assertEqual(caught.exception.code, "INVALID_REQUEST")

    def test_every_declared_mirror_records_its_upstream_and_uses_https(self):
        # The product boundary admits a mirror only with a licence, a pinned
        # version, a checksum and an upstream address; the first and last are
        # this table's responsibility.
        for mirror_id, meta in PACKAGE_MIRRORS.items():
            with self.subTest(mirror=mirror_id):
                self.assertTrue(str(meta["registry"]).startswith("https://"))
                self.assertTrue(str(meta["upstream"]).startswith("https://"))
                self.assertTrue(meta["note"])
                self.assertTrue(meta["name"])

    def test_the_ui_sees_each_mirror_with_its_upstream(self):
        # The UI has to be able to show where a package comes from, so the
        # projection may not drop the upstream address.
        published = public_mirrors()
        self.assertEqual({item["id"] for item in published}, set(PACKAGE_MIRRORS))
        for item in published:
            with self.subTest(mirror=item["id"]):
                self.assertTrue(item["upstream"].startswith("https://"))
                self.assertTrue(item["registry"].startswith("https://"))
                self.assertTrue(item["note"])

    def test_no_mirror_points_at_storage_oneagent_operates(self):
        # Redistributing a proprietary Agent needs a licence that pointing at a
        # public read-only mirror does not, so a mirror may never be ours.
        for mirror_id, meta in PACKAGE_MIRRORS.items():
            with self.subTest(mirror=mirror_id):
                host = str(meta["registry"]).lower()
                for forbidden in ("oneagent", "maimory"):
                    self.assertNotIn(forbidden, host)


class IntegrityTests(unittest.TestCase):
    def install(self, runtime: Runtime, agent_id: str = "codex", **kwargs):
        return install_locked_agent(
            runtime,
            agent_id,
            agent_catalog()[agent_id],
            enforce_locked=True,
            latest=kwargs.pop("latest", False),
            timeout=180,
            registry=kwargs.pop("registry", ""),
        )

    def test_the_manifest_checksum_is_checked_before_installing(self):
        # Recorded in the lock manifest but never read until now: the version was
        # pinned and the bytes were not.
        for agent_id in ("codex", "claude-code"):
            with self.subTest(agent=agent_id):
                runner = RecordingRunner()
                self.install(runtime_with(runner=runner, present={"npm"}), agent_id)
                view = [call for call in runner.calls if "view" in call]
                self.assertEqual(len(view), 1)
                self.assertIn("dist.integrity", view[0])
                expected = agent_catalog()[agent_id]["package"]["integrity"]
                self.assertTrue(expected.startswith("sha512-"))
                # And the check happens before the install, not after.
                self.assertLess(runner.calls.index(view[0]), runner.calls.index(runner.install_calls[0]))

    def test_a_registry_serving_a_different_package_is_refused(self):
        # The case a mirror has to be held to: npm would verify the download
        # against whatever the registry declared, so only the manifest can say
        # the declaration is the official release.
        runner = RecordingRunner(integrity="sha512-tampered")
        with self.assertRaises(OneAgentError) as caught:
            self.install(runtime_with(runner=runner, present={"npm"}), registry="npmmirror")
        self.assertEqual(caught.exception.code, "AGENT_INSTALL_FAILED")
        self.assertIn("Checksum mismatch", str(caught.exception))
        self.assertIn("sha512-tampered", str(caught.exception))
        self.assertEqual(runner.install_calls, [])

    def test_a_missing_version_on_a_mirror_is_not_silently_downgraded(self):
        # The boundary requires reporting an unreachable source rather than
        # falling back to some other version.
        runner = RecordingRunner(integrity_returncode=1)
        with self.assertRaises(OneAgentError) as caught:
            self.install(runtime_with(runner=runner, present={"npm"}), registry="npmmirror")
        self.assertEqual(caught.exception.code, "AGENT_INSTALL_FAILED")
        self.assertIn("not available", str(caught.exception))
        self.assertEqual(runner.install_calls, [])

    def test_the_checksum_query_uses_the_same_registry_as_the_install(self):
        # Checking the official registry and installing from a mirror would
        # verify the wrong thing.
        runner = RecordingRunner()
        self.install(runtime_with(runner=runner, present={"npm"}), registry="npmmirror")
        expected = PACKAGE_MIRRORS["npmmirror"]["registry"]
        view = [call for call in runner.calls if "view" in call][0]
        self.assertIn(f"--registry={expected}", view)

    def test_latest_skips_the_checksum_because_the_manifest_cannot_describe_it(self):
        runner = RecordingRunner()
        self.install(runtime_with(runner=runner, present={"npm"}), latest=True)
        self.assertEqual([call for call in runner.calls if "view" in call], [])

    def test_an_unreachable_registry_fails_closed_rather_than_installing(self):
        # A checksum that cannot be read is not a checksum that matched, so both
        # a hung registry and a missing npm must stop the install.
        def hangs(args, **kwargs):  # noqa: ANN001
            raise subprocess.TimeoutExpired([str(item) for item in args], 1)

        with self.assertRaises(OneAgentError) as timed_out:
            self.install(runtime_with(runner=hangs, present={"npm"}))
        self.assertEqual(timed_out.exception.code, "AGENT_INSTALL_FAILED")
        self.assertIn("Timed out", str(timed_out.exception))
        self.assertTrue(timed_out.exception.retryable)

        def broken(args, **kwargs):  # noqa: ANN001
            raise OSError("npm vanished")

        with self.assertRaises(OneAgentError) as failed:
            self.install(runtime_with(runner=broken, present={"npm"}))
        self.assertEqual(failed.exception.code, "AGENT_INSTALL_FAILED")
        self.assertIn("Cannot read the checksum", str(failed.exception))

    def test_a_package_without_a_recorded_checksum_is_not_blocked(self):
        # uv entries carry no npm integrity; the guard must be "verify when a
        # value exists", not "refuse everything unverified".
        runner = RecordingRunner()
        runtime = runtime_with(runner=runner, present={"npm"})
        meta = json.loads(json.dumps(agent_catalog()["codex"]))
        meta["package"].pop("integrity")
        install_locked_agent(
            runtime, "codex", meta, enforce_locked=True, latest=False, timeout=180, registry=""
        )
        self.assertEqual([call for call in runner.calls if "view" in call], [])
        self.assertEqual(len(runner.install_calls), 1)

    def test_uv_packages_are_not_subject_to_the_npm_checksum_path(self):
        runner = RecordingRunner()
        self.install(runtime_with(runner=runner, present={"uv", "python3.12"}), "aider")
        self.assertEqual([call for call in runner.calls if "dist.integrity" in call], [])


class InstallManyRegistryTests(unittest.TestCase):
    """The registry has to reach install_many and show up in its log."""

    def run_install(self, *, registry: str, versions: dict[str, str] | None = None):
        runner = RecordingRunner(versions=versions)
        present = {"npm", "codex"} if versions else {"npm"}
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime(
                home=Path(tmp),
                os_id="linux",
                runner=runner,
                which=lambda name: f"/usr/bin/{name}" if name in present else None,
                env={},
            )
            result = install_many(
                InstallOptions(
                    agents=["codex"],
                    configure=False,
                    install_agent=True,
                    check_agent_only=True,
                    locked_version=True,
                    registry=registry,
                    home=Path(tmp),
                    os_id="linux",
                ),
                runtime,
            )
        return runner, result

    def test_the_log_names_the_registry_a_package_came_from(self):
        # Without this the user has no way to tell afterwards whether a package
        # came from the official source or a mirror.
        _, result = self.run_install(registry="npmmirror")
        expected = PACKAGE_MIRRORS["npmmirror"]["registry"]
        self.assertIn(f"registry: {expected}", result["log"])

    def test_nothing_is_logged_when_no_installation_happened(self):
        # Already at the locked version: install_locked_agent short-circuits and
        # reports no registry, so the log must not claim one was used.
        locked = agent_catalog()["codex"]["package"]["version"]
        _, result = self.run_install(registry="npmmirror", versions={"codex": locked})
        self.assertNotIn("registry:", result["log"])


class PrerequisiteTests(unittest.TestCase):
    def install(self, agent_id: str, runtime: Runtime):
        return install_locked_agent(
            runtime, agent_id, agent_catalog()[agent_id], enforce_locked=True, latest=False, timeout=180
        )

    def test_missing_npm_is_reported_before_anything_runs(self):
        # Otherwise the failure surfaces as a FileNotFoundError from the runner,
        # which tells the user nothing about what to install.
        runner = RecordingRunner()
        runtime = runtime_with(runner=runner, present=set())
        for agent_id in ("codex", "claude-code"):
            with self.subTest(agent=agent_id):
                with self.assertRaises(OneAgentError) as caught:
                    self.install(agent_id, runtime)
                self.assertEqual(caught.exception.code, "PREREQUISITE_MISSING")
                self.assertIn("npm", str(caught.exception))
        self.assertEqual(runner.install_calls, [])

    def test_claude_code_on_windows_requires_git(self):
        # Declared as windows_prerequisites in the lock manifest; Claude Code
        # shells out to Git Bash there.
        runtime = runtime_with(runner=RecordingRunner(), present={"npm"}, os_id="windows")
        with self.assertRaises(OneAgentError) as caught:
            self.install("claude-code", runtime)
        self.assertEqual(caught.exception.code, "PREREQUISITE_MISSING")
        self.assertIn("Git", str(caught.exception))

        # With git present the same call proceeds.
        with_git = RecordingRunner()
        self.install("claude-code", runtime_with(runner=with_git, present={"npm", "git"}, os_id="windows"))
        self.assertEqual(len(with_git.install_calls), 1)

    def test_codex_on_windows_does_not_require_git(self):
        runner = RecordingRunner()
        self.install("codex", runtime_with(runner=runner, present={"npm"}, os_id="windows"))
        self.assertEqual(len(runner.install_calls), 1)


class InstallFailureTests(unittest.TestCase):
    def install(self, runtime: Runtime, agent_id: str = "codex"):
        return install_locked_agent(
            runtime, agent_id, agent_catalog()[agent_id], enforce_locked=True, latest=False, timeout=180
        )

    def test_a_failing_installer_is_retryable_and_keeps_its_output(self):
        runner = RecordingRunner(returncode=1)
        with self.assertRaises(OneAgentError) as caught:
            self.install(runtime_with(runner=runner, present={"npm"}))
        self.assertEqual(caught.exception.code, "AGENT_INSTALL_FAILED")
        self.assertTrue(caught.exception.retryable)
        self.assertIn("boom", str(caught.exception))

    def test_a_timeout_is_reported_as_a_timeout(self):
        integrity = agent_catalog()["codex"]["package"]["integrity"]

        def slow(args, **kwargs):  # noqa: ANN001
            argv = [str(item) for item in args]
            if "install" in argv:
                raise subprocess.TimeoutExpired(argv, 1)
            return subprocess.CompletedProcess(argv, 0, f"{integrity}\n", "")

        runtime = runtime_with(runner=slow, present={"npm"})
        with self.assertRaises(OneAgentError) as caught:
            self.install(runtime)
        self.assertEqual(caught.exception.code, "TIMEOUT")
        self.assertTrue(caught.exception.retryable)

    def test_installer_output_cannot_leak_a_credential_from_the_environment(self):
        # The failure detail quotes the installer's stderr, and npm echoes the
        # environment it ran with on some errors.
        secret = "sk-contract-should-not-appear"

        integrity = agent_catalog()["codex"]["package"]["integrity"]

        def leaky(args, **kwargs):  # noqa: ANN001
            argv = [str(item) for item in args]
            if "install" not in argv:
                return subprocess.CompletedProcess(argv, 0, f"{integrity}\n", "")
            return subprocess.CompletedProcess(argv, 1, "", f"failed with ANTHROPIC_API_KEY={secret}")

        runtime = runtime_with(
            runner=leaky, present={"npm"}, env={"ANTHROPIC_API_KEY": secret, "PATH": "/usr/bin"}
        )
        with self.assertRaises(OneAgentError) as caught:
            self.install(runtime)
        self.assertNotIn(secret, str(caught.exception))
        self.assertIn("[redacted]", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
