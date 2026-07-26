from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from oneagent.catalog import load_manifest, resolve_home
from oneagent.errors import OneAgentError
from oneagent.installer import (
    InstallOptions,
    Runtime,
    atomic_write,
    install_many,
    load_profile,
    redact,
    status_payload,
)
from oneagent.providers import openai_base_url, validate_base_url


class FakeProcessRunner:
    def __init__(self, *, fail_install_for: str = "") -> None:
        self.calls: list[list[str]] = []
        self.fail_install_for = fail_install_for

    def __call__(self, args, **_kwargs):
        values = [str(item) for item in args]
        self.calls.append(values)
        if "--version" in values:
            return subprocess.CompletedProcess(values, 0, "codex-cli 0.145.0\n", "")
        if self.fail_install_for and any(self.fail_install_for in item for item in values):
            return subprocess.CompletedProcess(values, 9, "", "failed")
        return subprocess.CompletedProcess(values, 0, "ok\n", "")


def fake_which(mapping: dict[str, str]):
    return lambda command: mapping.get(command)


class CatalogPolicyTests(unittest.TestCase):
    def test_manifest_has_five_locked_auto_agents(self):
        manifest = load_manifest()
        automatic = {key: value for key, value in manifest["agents"].items() if value["config_mode"] == "auto"}
        self.assertEqual(set(automatic), {"codex", "claude-code", "opencode", "kilo-cli", "aider"})
        expected_adapters = {
            "codex": "codex",
            "claude-code": "claude-code",
            "opencode": "opencode",
            "kilo-cli": "kilo-cli",
            "aider": "aider",
        }
        for agent_id, meta in automatic.items():
            package = meta["package"]
            with self.subTest(agent=agent_id):
                self.assertRegex(package["version"], r"^\d+\.\d+\.\d+")
                self.assertIn(package["manager"], {"npm", "uv"})
                self.assertTrue(package["source"].startswith("https://"))
                self.assertTrue(package["license"])
                self.assertTrue(package["license_url"].startswith("https://"))
                self.assertEqual(set(meta["platforms"]), {"macos", "linux", "windows"})
                self.assertEqual(meta["config_adapter"], expected_adapters[agent_id])

    def test_guide_agents_have_no_package_install_contract(self):
        manifest = load_manifest()
        for agent_id, meta in manifest["agents"].items():
            if meta["config_mode"] == "guide":
                with self.subTest(agent=agent_id):
                    self.assertNotIn("package", meta)
                    self.assertIsNone(meta.get("config_adapter"))
                    self.assertTrue(meta["guide"])


class PlatformAndSecurityTests(unittest.TestCase):
    def test_windows_home_prefers_userprofile(self):
        home = resolve_home({"USERPROFILE": r"C:\Users\测试 用户", "HOME": "/wrong"}, "windows")
        self.assertEqual(str(home), r"C:\Users\测试 用户")

    def test_custom_url_rejects_credentials_and_control_characters(self):
        with self.assertRaises(OneAgentError):
            validate_base_url("https://user:secret@example.com")
        with self.assertRaises(OneAgentError):
            validate_base_url("https://example.com/\nheader")

    def test_openai_url_normalizes_full_endpoint(self):
        self.assertEqual(
            openai_base_url("https://models.example.com/openai/v1/chat/completions"),
            "https://models.example.com/openai/v1",
        )

    def test_install_options_reject_mutually_exclusive_version_modes(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            with self.assertRaisesRegex(OneAgentError, "cannot be enabled together"):
                install_many(
                    InstallOptions(
                        agents=["codex"],
                        configure=False,
                        locked_version=True,
                        latest=True,
                    ),
                    runtime,
                )

    def test_redaction_removes_all_secret_occurrences(self):
        self.assertEqual(redact("key=abc abc", ["abc"]), "key=[redacted] [redacted]")

    def test_windows_secret_file_uses_powershell_quoting_and_acl(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = FakeProcessRunner()
            mapping = {"icacls": r"C:\Windows\System32\icacls.exe"}
            runtime = Runtime.create(
                home=Path(tmp) / "用户 Home",
                os_id="windows",
                runner=runner,
                which=fake_which(mapping),
                env={"USERNAME": "tester", "USERPROFILE": tmp},
            )
            result = install_many(
                InstallOptions(
                    agents=["codex"],
                    provider="ppio",
                    api_key="key'quoted",
                    model="model-a",
                    configure=True,
                    skip_test=True,
                    home=runtime.home,
                    os_id="windows",
                ),
                runtime,
            )
            self.assertTrue(result["ok"])
            env_text = (runtime.home / ".oneagent" / "env.ps1").read_text(encoding="utf-8")
            self.assertIn("$env:ONEAGENT_API_KEY = 'key''quoted'", env_text)
            self.assertTrue(any("/reset" in call for call in runner.calls))
            self.assertTrue(any("/inheritance:r" in call for call in runner.calls))
            profile = json.loads((runtime.home / ".oneagent" / "profile.json").read_text(encoding="utf-8"))
            self.assertNotIn("key", json.dumps(profile).lower())

    @unittest.skipIf(os.name == "nt", "Unix mode assertion")
    def test_unix_secret_files_are_private(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux")
            secret = Path(tmp) / ".oneagent" / "secret"
            secret.parent.mkdir()
            secret.write_text("old-value\n", encoding="utf-8")
            secret.chmod(0o644)
            backup = atomic_write(runtime, secret, "value\n", secret=True)
            self.assertEqual((Path(tmp) / ".oneagent").stat().st_mode & 0o777, 0o700)
            self.assertEqual(secret.stat().st_mode & 0o777, 0o600)
            self.assertIsNotNone(backup)
            self.assertEqual(backup.stat().st_mode & 0o777, 0o600)

    @unittest.skipUnless(os.name == "nt", "Real ACL assertion runs on Windows CI")
    def test_windows_secret_acl_allows_only_current_user_and_system(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="windows")
            secret = Path(tmp) / ".oneagent" / "secret.ps1"
            atomic_write(runtime, secret, "$env:KEY = 'value'\n", secret=True)
            script = r"""
$acl = Get-Acl -LiteralPath $env:ONEAGENT_ACL_TEST_PATH
$current = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$identities = @($acl.Access | ForEach-Object {
  $_.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value
} | Sort-Object -Unique)
[pscustomobject]@{
  current = $current
  identities = $identities
  protected = $acl.AreAccessRulesProtected
} | ConvertTo-Json -Compress
"""
            for path in [secret.parent, secret]:
                env = os.environ.copy()
                env["ONEAGENT_ACL_TEST_PATH"] = str(path)
                result = subprocess.run(
                    ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script],
                    text=True,
                    capture_output=True,
                    check=True,
                    env=env,
                )
                acl = json.loads(result.stdout)
                identities = acl["identities"]
                if isinstance(identities, str):
                    identities = [identities]
                self.assertTrue(acl["protected"])
                self.assertEqual(set(identities), {acl["current"], "S-1-5-18"})


class InstallerContractTests(unittest.TestCase):
    def test_locked_npm_install_uses_manifest_version_without_shell(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = FakeProcessRunner()
            runtime = Runtime.create(
                home=Path(tmp),
                os_id="linux",
                runner=runner,
                which=fake_which({"npm": "/fake/npm"}),
            )
            result = install_many(
                InstallOptions(
                    agents=["codex"],
                    api_key="secret",
                    model="model-a",
                    configure=True,
                    install_agent=True,
                    locked_version=True,
                    skip_test=True,
                    home=Path(tmp),
                ),
                runtime,
            )
            self.assertTrue(result["ok"])
            self.assertIn(["/fake/npm", "install", "-g", "@openai/codex@0.145.0"], runner.calls)

    def test_missing_package_manager_is_structured_failure(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=fake_which({}))
            result = install_many(
                InstallOptions(
                    agents=["codex"],
                    api_key="secret",
                    install_agent=True,
                    skip_test=True,
                    home=Path(tmp),
                ),
                runtime,
            )
            self.assertFalse(result["ok"])
            self.assertEqual(result["results"][0]["error_code"], "PREREQUISITE_MISSING")
            self.assertEqual(result["results"][0]["code"], 3)

    def test_windows_claude_requires_git(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = FakeProcessRunner()
            runtime = Runtime.create(
                home=Path(tmp),
                os_id="windows",
                runner=runner,
                which=fake_which({"npm": "npm.cmd", "icacls": "icacls.exe"}),
                env={"USERNAME": "tester", "USERPROFILE": tmp},
            )
            result = install_many(
                InstallOptions(
                    agents=["claude-code"],
                    api_key="secret",
                    install_agent=True,
                    skip_test=True,
                    home=Path(tmp),
                    os_id="windows",
                ),
                runtime,
            )
            self.assertFalse(result["ok"])
            self.assertEqual(result["results"][0]["error_code"], "PREREQUISITE_MISSING")

    def test_guide_only_agent_writes_no_private_configuration(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=fake_which({}))
            result = install_many(
                InstallOptions(agents=["openclaw"], configure=False, skip_test=True, home=Path(tmp)),
                runtime,
            )
            self.assertTrue(result["ok"])
            self.assertEqual(result["results"][0]["status"], "guide-only")
            self.assertFalse((Path(tmp) / ".openclaw").exists())

    def test_partial_failure_does_not_publish_environment_profile(self):
        with tempfile.TemporaryDirectory() as tmp:
            runner = FakeProcessRunner(fail_install_for="@openai/codex")
            runtime = Runtime.create(
                home=Path(tmp),
                os_id="linux",
                runner=runner,
                which=fake_which({"npm": "/fake/npm"}),
            )
            result = install_many(
                InstallOptions(
                    agents=["codex", "opencode"],
                    api_key="secret",
                    install_agent=True,
                    skip_test=True,
                    home=Path(tmp),
                ),
                runtime,
            )
            self.assertFalse(result["ok"])
            self.assertEqual({item["status"] for item in result["results"]}, {"failed", "configured"})
            profile, _ = load_profile(runtime)
            self.assertIsNone(profile)

            runtime.runner = FakeProcessRunner()
            retry = install_many(
                InstallOptions(
                    agents=["codex"],
                    profile_agents=["codex", "opencode"],
                    api_key="secret",
                    install_agent=True,
                    skip_test=True,
                    home=Path(tmp),
                ),
                runtime,
            )
            self.assertTrue(retry["ok"])
            profile, _ = load_profile(runtime)
            self.assertEqual(profile["agent_ids"], ["codex", "opencode"])

    def test_api_key_reaches_only_the_designated_secret_files(self):
        # The README promises the key never lands anywhere but the local secret
        # configs. The existing check covered two Agents and two files; this
        # configures every auto Agent and then walks the whole home directory,
        # so a new config writer that embeds the key cannot slip through.
        secret = "sentinel-whole-home-key"
        auto_agents = [
            agent_id
            for agent_id, meta in load_manifest()["agents"].items()
            if meta["config_mode"] == "auto"
        ]
        self.assertEqual(len(auto_agents), 5)

        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime.create(home=home, os_id="linux", which=fake_which({}))
            result = install_many(
                InstallOptions(
                    agents=auto_agents,
                    provider="ppio",
                    api_key=secret,
                    model="model-x",
                    configure=True,
                    install_agent=False,
                    skip_test=True,
                    home=home,
                ),
                runtime,
            )
            self.assertTrue(result["ok"], result)

            leaked = {
                path.relative_to(home).as_posix()
                for path in home.rglob("*")
                if path.is_file() and secret in path.read_text(encoding="utf-8", errors="ignore")
            }

            # Only the files that exist to hold credentials may contain it.
            # Codex, OpenCode and Kilo reference the key indirectly (env_key
            # and {env:...}), so their own configs must stay clean.
            self.assertEqual(
                leaked,
                {".oneagent/env", ".claude/settings.json", ".oneagent/aider.env"},
            )
            for relative in leaked:
                path = home / relative
                with self.subTest(path=relative):
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                    self.assertEqual(path.parent.stat().st_mode & 0o777, 0o700)

        # Nothing the caller receives may carry it either.
        self.assertNotIn(secret, json.dumps(result))
        self.assertNotIn(secret, result["log"])

    def test_profile_is_backed_up_and_status_recovers_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=fake_which({}))
            options = InstallOptions(agents=["openclaw"], configure=False, skip_test=True, home=Path(tmp))
            self.assertTrue(install_many(options, runtime)["ok"])
            self.assertTrue(install_many(options, runtime)["ok"])
            status = status_payload(runtime)
            self.assertEqual(status["apiVersion"], 1)
            self.assertEqual(status["environment"]["provider"], "existing-account")
            self.assertTrue(status["backups"]["profile"])


if __name__ == "__main__":
    unittest.main()
