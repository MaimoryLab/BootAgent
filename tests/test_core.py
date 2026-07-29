from __future__ import annotations

import json
import os
import subprocess
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from oneagent.catalog import agent_catalog, load_manifest, resolve_home
from oneagent.errors import OneAgentError
from oneagent.installer import (
    InstallOptions,
    Runtime,
    _restart_hint,
    activate_agent,
    agent_env_path,
    agent_env_var,
    atomic_write,
    install_many,
    list_agent_bindings,
    load_profile,
    read_agent_binding,
    read_profile_secret,
    redact,
    save_profile,
    secrets_path,
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
        if "view" in values and "dist.integrity" in values:
            # install_locked_agent compares the registry's checksum with the
            # manifest before installing, so the stand-in has to answer with the
            # value the manifest holds for whichever package is being asked
            # about; anything else is treated as a tampered registry.
            name = values[2].rsplit("@", 1)[0]
            integrity = ""
            for meta in agent_catalog().values():
                package = meta.get("package") or {}
                if package.get("name") == name:
                    integrity = str(package.get("integrity", ""))
                    break
            return subprocess.CompletedProcess(values, 0, f"{integrity}\n", "")
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


class PerAgentCredentialTests(unittest.TestCase):
    def _install(self, tmp: str, agents: list[str], *, provider: str, key: str, model: str):
        runtime = Runtime.create(
            home=Path(tmp),
            os_id="linux",
            runner=FakeProcessRunner(),
            which=fake_which({"npm": "/fake/npm", "uv": "/fake/uv"}),
        )
        return install_many(
            InstallOptions(
                agents=agents,
                provider=provider,
                api_key=key,
                model=model,
                configure=True,
                skip_test=True,
                home=Path(tmp),
            ),
            runtime,
        )

    def test_env_var_name_is_derived_per_agent(self):
        self.assertEqual(agent_env_var("codex"), "ONEAGENT_API_KEY_CODEX")
        # A hyphen is not legal in a shell variable name.
        self.assertEqual(agent_env_var("kilo-cli"), "ONEAGENT_API_KEY_KILO_CLI")
        self.assertEqual(agent_env_var("codex", "API_BASE_URL"), "ONEAGENT_API_BASE_URL_CODEX")

    def test_agent_env_path_rejects_a_traversing_id(self):
        # The ID names a file holding a plaintext key, so it must stay one path
        # segment: the same defect that let a tampered profile pointer escape.
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _n: None)
            for bad in ("../../evil", "a/b", "..", "/abs", "Codex", ""):
                with self.subTest(bad=bad):
                    with self.assertRaises(OneAgentError):
                        agent_env_path(runtime, bad)

    def test_two_agents_can_target_different_providers_at_once(self):
        # The point of the split: a single ONEAGENT_API_KEY meant whichever env
        # file was sourced last decided the credential for every Agent.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            self._install(tmp, ["codex"], provider="ppio", key="key-for-codex", model="model-a")
            self._install(tmp, ["opencode"], provider="novita", key="key-for-opencode", model="model-b")

            codex_env = (home / ".oneagent" / "agents" / "codex.env").read_text(encoding="utf-8")
            opencode_env = (home / ".oneagent" / "agents" / "opencode.env").read_text(encoding="utf-8")

            # Each file carries its own Agent's key under its own variable, and
            # neither leaks the other's.
            self.assertIn("ONEAGENT_API_KEY_CODEX=key-for-codex", codex_env)
            self.assertNotIn("key-for-opencode", codex_env)
            self.assertIn("ONEAGENT_API_KEY_OPENCODE=key-for-opencode", opencode_env)
            self.assertNotIn("key-for-codex", opencode_env)

            # And each config reads the variable its own file defines.
            config = tomllib.loads((home / ".codex" / "config.toml").read_text(encoding="utf-8"))
            self.assertEqual(config["model_providers"]["oneagent"]["env_key"], "ONEAGENT_API_KEY_CODEX")
            opencode = json.loads((home / ".config" / "opencode" / "opencode.jsonc").read_text(encoding="utf-8"))
            self.assertEqual(
                opencode["provider"]["oneagent"]["options"]["apiKey"],
                "{env:ONEAGENT_API_KEY_OPENCODE}",
            )

    def test_next_step_points_at_the_agents_own_env_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = self._install(tmp, ["codex"], provider="ppio", key="k", model="m")
        self.assertIn("source ~/.oneagent/agents/codex.env && codex", result["next"])


class AgentActivationTests(unittest.TestCase):
    def _runtime(self, tmp: str) -> Runtime:
        return Runtime(
            home=Path(tmp),
            os_id="linux",
            runner=FakeProcessRunner(),
            which=lambda name: f"/usr/bin/{name}",
            env={},
        )

    def test_activating_one_agent_leaves_the_others_untouched(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            activate_agent(runtime, "codex", provider="ppio", api_key="K-CODEX", model="m-a")
            activate_agent(runtime, "opencode", provider="novita", api_key="K-OPENCODE", model="m-b")

            bindings = list_agent_bindings(runtime)
            self.assertEqual(bindings["codex"]["provider"], "ppio")
            self.assertEqual(bindings["opencode"]["provider"], "novita")

            # Repointing codex must not disturb opencode's binding or key.
            activate_agent(runtime, "codex", provider="novita", api_key="K-CODEX-2", model="m-c")
            bindings = list_agent_bindings(runtime)
            self.assertEqual(bindings["codex"]["model"], "m-c")
            self.assertEqual(bindings["opencode"]["model"], "m-b")
            opencode_env = (Path(tmp) / ".oneagent" / "agents" / "opencode.env").read_text(encoding="utf-8")
            self.assertIn("K-OPENCODE", opencode_env)
            self.assertNotIn("K-CODEX", opencode_env)

    def test_activation_never_returns_the_key_and_always_says_how_to_reload(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = activate_agent(
                self._runtime(tmp), "codex", provider="ppio", api_key="SECRET-KEY", model="m"
            )
        self.assertNotIn("SECRET-KEY", json.dumps(result))
        # An Agent reads its config at startup, so "activated" without a reload
        # instruction reads as a silent failure.
        self.assertTrue(result["restart"])

    def test_activation_rejects_guide_only_and_unknown_agents(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            for agent_id in ("cursor", "no-such-agent", "../escape"):
                with self.subTest(agent=agent_id):
                    with self.assertRaises(OneAgentError):
                        activate_agent(runtime, agent_id, provider="ppio", api_key="k", model="m")
            # A missing key is refused before anything is written.
            with self.assertRaises(OneAgentError):
                activate_agent(runtime, "codex", provider="ppio", api_key="", model="m")
            self.assertEqual(list_agent_bindings(runtime), {})

    def test_a_saved_profile_key_can_be_reused_without_repasting(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            save_profile(
                runtime,
                profile_id="team-ppio",
                provider="ppio",
                model="m",
                agent_ids=["codex"],
                api_key="STORED-KEY",
            )
            self.assertEqual(read_profile_secret(runtime, "team-ppio"), "STORED-KEY")
            # Absent profile yields no key rather than an error.
            self.assertEqual(read_profile_secret(runtime, "missing"), "")

    def test_unreadable_bindings_are_reported_and_skipped_not_raised(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            directory = Path(tmp) / ".oneagent" / "agents"
            directory.mkdir(parents=True)

            for content, expected in (
                ("{", "Expecting"),
                ("[]", "corrupt"),
                ('{"schema_version":9}', "Unsupported"),
            ):
                with self.subTest(content=content):
                    (directory / "codex.json").write_text(content, encoding="utf-8")
                    value, error = read_agent_binding(runtime, "codex")
                    self.assertIsNone(value)
                    self.assertIn(expected, error or "")

            # A bad file is skipped by the listing rather than failing the whole
            # status payload, and a filename that is not a legal ID is ignored.
            (directory / "Bad-Name.json").write_text('{"schema_version":1}', encoding="utf-8")
            activate_agent(runtime, "opencode", provider="ppio", api_key="k", model="m")
            self.assertEqual(list(list_agent_bindings(runtime)), ["opencode"])

        with tempfile.TemporaryDirectory() as tmp:
            # No directory at all is not an error either.
            self.assertEqual(list_agent_bindings(self._runtime(tmp)), {})

    def test_restart_hint_covers_every_managed_agent(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            for agent_id in ("codex", "claude-code", "opencode", "kilo-cli", "aider"):
                with self.subTest(agent=agent_id):
                    result = activate_agent(
                        runtime, agent_id, provider="ppio", api_key="k", model="m"
                    )
                    self.assertTrue(result["restart"])
        self.assertIn("aider.env", _restart_hint("aider"))
        # An Agent with no known launch command still gets an instruction.
        self.assertEqual(_restart_hint("future-agent"), "Restart future-agent")

    def test_reading_a_profile_key_handles_both_shells_and_bad_files(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            save_profile(
                runtime, profile_id="p1", provider="ppio", model="m",
                agent_ids=["codex"], api_key="quoted key",
            )
            # shlex round-trips a key that needed quoting.
            self.assertEqual(read_profile_secret(runtime, "p1"), "quoted key")

            # A secret file with no recognisable assignment yields no key.
            secrets_path(runtime, "p1").write_text("# nothing here\n", encoding="utf-8")
            self.assertEqual(read_profile_secret(runtime, "p1"), "")

            # An unreadable secret file is a reported failure, not a silent "".
            # Returning "" there would make activation fail later with a
            # confusing "API key is required" instead of the real cause.
            with patch.object(Path, "read_text", side_effect=OSError("denied")):
                with self.assertRaises(OneAgentError):
                    read_profile_secret(runtime, "p1")

        with tempfile.TemporaryDirectory() as tmp:
            windows = Runtime(
                home=Path(tmp), os_id="windows", runner=FakeProcessRunner(),
                which=lambda name: f"C:\\{name}.exe", env={"USERNAME": "tester"},
            )
            with patch("oneagent.installer._run_acl"):
                save_profile(
                    windows, profile_id="p2", provider="ppio", model="m",
                    agent_ids=["codex"], api_key="it's",
                )
                self.assertEqual(read_profile_secret(windows, "p2"), "it's")

    def test_status_reports_each_agent_binding_separately(self):
        with tempfile.TemporaryDirectory() as tmp:
            runtime = self._runtime(tmp)
            activate_agent(runtime, "codex", provider="ppio", api_key="k1", model="m-a")
            payload = status_payload(runtime)
        self.assertEqual(payload["agents"]["codex"]["provider"], "ppio")
        self.assertEqual(payload["agents"]["codex"]["model"], "m-a")
        # An Agent never pointed anywhere reports no binding instead of a stale
        # or borrowed one.
        self.assertIsNone(payload["agents"]["opencode"]["provider"])


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
            # and {env:...}), so their own configs must stay clean. Each of
            # those three now has its own env file so they can point at
            # different providers at once; .oneagent/env stays as the
            # compatibility copy for configs written before that split. The
            # per-profile secret store (ADR-006) holds a copy so profiles can
            # be re-activated without re-pasting the key.
            self.assertEqual(
                leaked,
                {
                    ".oneagent/env",
                    ".oneagent/agents/codex.env",
                    ".oneagent/agents/opencode.env",
                    ".oneagent/agents/kilo-cli.env",
                    ".claude/settings.json",
                    ".oneagent/aider.env",
                    ".oneagent/secrets/default.env",
                },
            )

        # Nothing the caller receives may carry it either.
        self.assertNotIn(secret, json.dumps(result))
        self.assertNotIn(secret, result["log"])

    @unittest.skipIf(os.name == "nt", "Unix mode assertion")
    def test_every_secret_file_is_written_with_owner_only_mode(self):
        # Split from the leak test so that one keeps running on Windows: the
        # set of files carrying the key is platform-independent, but chmod is
        # a no-op on NTFS, where the same guarantee comes from the ACL path.
        secret = "sentinel-permission-key"
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = Runtime.create(home=home, os_id="linux", which=fake_which({}))
            install_many(
                InstallOptions(
                    agents=["codex", "claude-code", "aider"],
                    provider="ppio",
                    api_key=secret,
                    model="model-x",
                    configure=True,
                    skip_test=True,
                    home=home,
                ),
                runtime,
            )
            for relative in (".oneagent/env", ".claude/settings.json", ".oneagent/aider.env"):
                path = home / relative
                with self.subTest(path=relative):
                    self.assertTrue(path.is_file())
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                    self.assertEqual(path.parent.stat().st_mode & 0o777, 0o700)

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
