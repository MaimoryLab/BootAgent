"""Reading the configuration a user already had.

status_payload used to report configured=True with provider, model and baseUrl
all null for a config OneAgent had not written itself: the file was seen but
never read. The overview therefore said "not configured" while a configuration
was live, and Apply would silently overwrite it.
"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from oneagent.catalog import agent_catalog
from oneagent.installer import (
    InstallOptions,
    Runtime,
    detect_agent_config,
    install_many,
    read_aider_config,
    read_claude_config,
    read_codex_config,
    read_openai_compatible_config,
    status_payload,
)


class FakeRunner:
    def __call__(self, args, **_kwargs):
        import subprocess

        values = [str(item) for item in args]
        if "--version" in values:
            return subprocess.CompletedProcess(values, 0, "codex-cli 0.145.0\n", "")
        if "view" in values and "dist.integrity" in values:
            name = values[2].rsplit("@", 1)[0]
            for meta in agent_catalog().values():
                package = meta.get("package") or {}
                if package.get("name") == name:
                    return subprocess.CompletedProcess(values, 0, f"{package.get('integrity','')}\n", "")
        return subprocess.CompletedProcess(values, 0, "ok\n", "")


def runtime_for(home: Path) -> Runtime:
    return Runtime(
        home=home,
        os_id="linux",
        runner=FakeRunner(),
        which=lambda name: f"/usr/bin/{name}",
        env={},
    )


class ReaderTests(unittest.TestCase):
    """Each reader follows the provider the file selects, not ours."""

    def test_codex_reads_the_provider_the_file_actually_selects(self):
        detected = read_codex_config(
            'model_provider = "someone-else"\n'
            'model = "gpt-5-mini"\n'
            "[model_providers.someone-else]\n"
            'base_url = "https://api.other-vendor.com/v1"\n'
            'env_key = "OTHER_KEY"\n'
        )
        self.assertEqual(detected["baseUrl"], "https://api.other-vendor.com/v1")
        self.assertEqual(detected["model"], "gpt-5-mini")
        self.assertFalse(detected["managedByOneAgent"])

    def test_codex_ignores_our_table_when_the_file_selects_another(self):
        # Reading [model_providers.oneagent] unconditionally would report a
        # configuration the Agent is not using.
        detected = read_codex_config(
            'model_provider = "vendor"\n'
            'model = "m"\n'
            "[model_providers.vendor]\n"
            'base_url = "https://vendor.example/v1"\n'
            "[model_providers.oneagent]\n"
            'base_url = "https://ours.example/v1"\n'
        )
        self.assertEqual(detected["baseUrl"], "https://vendor.example/v1")
        # Our table is present, so the file has been through OneAgent...
        self.assertTrue(detected["managedByOneAgent"])

    def test_claude_requires_every_declared_variable_to_count_as_managed(self):
        declared = agent_catalog()["claude-code"]["env_vars"]
        partial = read_claude_config(json.dumps({"env": {declared["base_url"]: "https://x.example"}}))
        self.assertEqual(partial["baseUrl"], "https://x.example")
        self.assertFalse(partial["managedByOneAgent"])

        full = read_claude_config(json.dumps({"env": {name: "v" for name in declared.values()}}))
        self.assertTrue(full["managedByOneAgent"])

    def test_openai_compatible_strips_the_provider_prefix_from_the_model(self):
        detected = read_openai_compatible_config(
            json.dumps(
                {
                    "provider": {"mine": {"options": {"baseURL": "https://mine.example/v1"}}},
                    "model": "mine/local-llm",
                }
            )
        )
        self.assertEqual(detected["baseUrl"], "https://mine.example/v1")
        self.assertEqual(detected["model"], "local-llm")

    def test_jsonc_comments_are_named_as_such_rather_than_broken_json(self):
        detected = read_openai_compatible_config('{\n  // a comment\n  "model": "x/y"\n}')
        self.assertIn("JSONC", detected["unreadable"])

    def test_aider_is_parsed_line_by_line_and_never_executed(self):
        # The file holds the key, and it is user-editable; executing it would
        # both leak the credential and run arbitrary shell.
        detected = read_aider_config(
            "export OPENAI_API_BASE='https://hand.example/v1'\n"
            "export OPENAI_API_KEY='sk-must-not-be-read'\n"
        )
        self.assertEqual(detected["baseUrl"], "https://hand.example/v1")
        self.assertNotIn("sk-must-not-be-read", json.dumps(detected))

    def test_aider_never_claims_to_know_who_wrote_its_script(self):
        # A hand-written script and ours are the same two exports, so there is no
        # marker to tell them apart. Reporting either as managed would be a guess.
        ours = read_aider_config(
            "export OPENAI_API_BASE='https://a.example/v1'\nexport OPENAI_API_KEY='k'\n"
        )
        theirs = read_aider_config("export OPENAI_API_BASE='https://a.example/v1'\n")
        self.assertFalse(ours["managedByOneAgent"])
        self.assertFalse(theirs["managedByOneAgent"])

    def test_powershell_assignments_are_understood_too(self):
        detected = read_aider_config("$env:OPENAI_API_BASE = 'https://win.example/v1'\n")
        self.assertEqual(detected["baseUrl"], "https://win.example/v1")

    def test_a_reader_reports_a_reason_instead_of_raising(self):
        for detected in [
            read_codex_config("model_provider = \nbroken ["),
            read_claude_config("{not json"),
            read_openai_compatible_config("[]"),
            read_openai_compatible_config("{ not json"),
            read_claude_config("[]"),
        ]:
            self.assertTrue(detected["unreadable"])
            self.assertEqual(detected["baseUrl"], "")

    def test_wrongly_typed_fields_are_ignored_rather_than_reported_as_values(self):
        # These files are user-editable, so any field can be the wrong shape. A
        # non-string endpoint must read as absent, not crash and not stringify.
        codex = read_codex_config(
            "model_provider = 1\nmodel = 2\n[model_providers.p]\nbase_url = 3\n"
        )
        self.assertEqual(codex["baseUrl"], "")
        self.assertEqual(codex["model"], "")

        # A selected provider whose table is not a table, and options that are
        # not an object.
        self.assertEqual(read_openai_compatible_config(json.dumps({"provider": {"p": "nope"}, "model": "p/m"}))["baseUrl"], "")
        self.assertEqual(
            read_openai_compatible_config(json.dumps({"provider": {"p": {"options": []}}, "model": "p/m"}))["baseUrl"],
            "",
        )
        self.assertEqual(
            read_openai_compatible_config(json.dumps({"provider": {"p": {"options": {"baseURL": 7}}}, "model": "p/m"}))["baseUrl"],
            "",
        )
        # And a model with no provider prefix still reads as a model.
        self.assertEqual(read_openai_compatible_config(json.dumps({"model": "bare-model"}))["model"], "bare-model")
        self.assertEqual(read_claude_config(json.dumps({"env": {"ANTHROPIC_BASE_URL": 5}}))["baseUrl"], "")
        self.assertEqual(read_claude_config(json.dumps({"env": "not-an-object"}))["baseUrl"], "")

    def test_an_aider_script_is_read_whatever_its_quoting(self):
        detected = read_aider_config('export OPENAI_API_BASE="https://theirs.example/v1"\nexport SOMETHING=1\n')
        self.assertEqual(detected["baseUrl"], "https://theirs.example/v1")
        # An unquoted value, and comments, are both fine.
        self.assertEqual(
            read_aider_config("# a note\nexport OPENAI_API_BASE=https://bare.example/v1\n")["baseUrl"],
            "https://bare.example/v1",
        )


class NoCredentialLeavesDiskTests(unittest.TestCase):
    """The reason readers extract only an endpoint and a model."""

    def test_no_config_format_can_put_its_key_into_the_status_payload(self):
        secret = "sk-detected-must-never-surface"
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / ".codex").mkdir()
            (home / ".claude").mkdir()
            (home / ".config" / "opencode").mkdir(parents=True)
            (home / ".oneagent").mkdir()
            # Three of the five formats hold the credential in plain text.
            (home / ".codex" / "config.toml").write_text(
                f'model_provider = "p"\nmodel = "m"\n[model_providers.p]\nbase_url = "https://a.example"\napi_key = "{secret}"\n',
                encoding="utf-8",
            )
            (home / ".claude" / "settings.json").write_text(
                json.dumps({"env": {"ANTHROPIC_BASE_URL": "https://b.example", "ANTHROPIC_AUTH_TOKEN": secret}}),
                encoding="utf-8",
            )
            (home / ".config" / "opencode" / "opencode.jsonc").write_text(
                json.dumps({"provider": {"p": {"options": {"baseURL": "https://c.example", "apiKey": secret}}}, "model": "p/m"}),
                encoding="utf-8",
            )
            (home / ".oneagent" / "aider.env").write_text(
                f"export OPENAI_API_BASE='https://d.example'\nexport OPENAI_API_KEY='{secret}'\n",
                encoding="utf-8",
            )

            payload = status_payload(runtime_for(home))
            self.assertNotIn(secret, json.dumps(payload, ensure_ascii=False))
            # And the endpoints were still read, so this is not passing by
            # reading nothing at all.
            self.assertEqual(payload["agents"]["codex"]["detected"]["baseUrl"], "https://a.example")
            self.assertEqual(payload["agents"]["claude-code"]["detected"]["baseUrl"], "https://b.example")
            self.assertEqual(payload["agents"]["aider"]["detected"]["baseUrl"], "https://d.example")

    def test_detected_carries_no_field_about_the_credential_at_all(self):
        # Even a boolean would say whether this machine has a key configured.
        detected = read_claude_config(json.dumps({"env": {"ANTHROPIC_AUTH_TOKEN": "sk-x"}}))
        self.assertEqual(set(detected), {"baseUrl", "model", "managedByOneAgent", "unreadable"})


class StatusIntegrationTests(unittest.TestCase):
    def test_a_hand_written_config_is_reported_instead_of_nulls(self):
        # The defect this work exists for.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / ".codex").mkdir()
            (home / ".codex" / "config.toml").write_text(
                'model_provider = "vendor"\nmodel = "gpt-5-mini"\n'
                '[model_providers.vendor]\nbase_url = "https://api.other-vendor.com/v1"\n',
                encoding="utf-8",
            )
            agent = status_payload(runtime_for(home))["agents"]["codex"]
            # The binding is still empty -- OneAgent did not write this.
            self.assertIsNone(agent["provider"])
            # But the configuration is no longer invisible.
            self.assertEqual(agent["detected"]["baseUrl"], "https://api.other-vendor.com/v1")
            self.assertEqual(agent["detected"]["model"], "gpt-5-mini")
            self.assertFalse(agent["detected"]["managedByOneAgent"])

    def test_one_broken_config_does_not_take_down_the_whole_status(self):
        # It used to: a config edited into invalid TOML raised out of
        # status_payload and blanked the entire UI.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / ".codex").mkdir()
            (home / ".claude").mkdir()
            (home / ".codex" / "config.toml").write_text("model_provider = \n[[[", encoding="utf-8")
            (home / ".claude" / "settings.json").write_text(
                json.dumps({"env": {"ANTHROPIC_BASE_URL": "https://fine.example"}}), encoding="utf-8"
            )
            payload = status_payload(runtime_for(home))
            self.assertTrue(payload["agents"]["codex"]["detected"]["unreadable"])
            # A healthy Agent beside it still reports normally.
            self.assertEqual(payload["agents"]["claude-code"]["detected"]["baseUrl"], "https://fine.example")

    def test_guide_only_agents_report_nothing_detected(self):
        # OneAgent never writes their config, so it has nothing to say about it.
        with tempfile.TemporaryDirectory() as tmp:
            payload = status_payload(runtime_for(Path(tmp)))
            for agent_id, meta in agent_catalog().items():
                if meta.get("config_mode") == "auto":
                    continue
                with self.subTest(agent=agent_id):
                    self.assertIsNone(payload["agents"][agent_id]["detected"])

    def test_what_we_write_is_what_we_read_back(self):
        # The round trip is the assertion worth having: it holds both directions
        # to the same contract, so a change to either is caught here.
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            runtime = runtime_for(home)
            catalog = agent_catalog()
            auto = [agent for agent, meta in catalog.items() if meta.get("config_mode") == "auto"]
            install_many(
                InstallOptions(
                    agents=auto,
                    provider="ppio",
                    api_key="K-ROUNDTRIP",
                    model="round-trip-model",
                    configure=True,
                    skip_test=True,
                    home=home,
                    os_id="linux",
                ),
                runtime,
            )
            for agent_id in auto:
                with self.subTest(agent=agent_id):
                    detected = detect_agent_config(runtime, catalog[agent_id])
                    self.assertIsNotNone(detected)
                    self.assertIsNone(detected["unreadable"])
                    self.assertTrue(detected["baseUrl"], f"{agent_id} reports no endpoint")
                    if agent_id == "aider":
                        # Its config is a script we own outright, with no marker a
                        # hand-written equivalent would lack, and the model is a
                        # launch argument rather than a field.
                        self.assertFalse(detected["managedByOneAgent"])
                    else:
                        self.assertTrue(
                            detected["managedByOneAgent"],
                            f"{agent_id} was written by us but does not look managed",
                        )
                        self.assertEqual(detected["model"], "round-trip-model")

    def test_an_agent_with_no_reader_says_so_rather_than_guessing(self):
        # A lock entry can name an adapter before a reader for it exists; the
        # honest answer is that we cannot read it, not a guessed endpoint.
        meta = dict(agent_catalog()["codex"])
        meta["config_adapter"] = "some-future-format"
        with tempfile.TemporaryDirectory() as tmp:
            detected = detect_agent_config(runtime_for(Path(tmp)), meta)
        self.assertIn("解析器", detected["unreadable"])
        self.assertEqual(detected["baseUrl"], "")

    def test_an_unreadable_file_reports_the_reason_not_the_contents(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / ".codex").mkdir()
            config = home / ".codex" / "config.toml"
            config.write_text('model_provider = "p"\n', encoding="utf-8")
            config.chmod(0o000)
            try:
                detected = detect_agent_config(runtime_for(home), agent_catalog()["codex"])
            finally:
                config.chmod(0o600)
        # Skipped when running as root, where the chmod does not deny us.
        if detected["unreadable"] is None:
            self.skipTest("file permissions do not restrict this user")
        self.assertIn("无法读取", detected["unreadable"])

    def test_a_reader_that_throws_cannot_break_the_status_request(self):
        # The guarantee matters more than any single reader being correct: one
        # unexpected shape must not blank the whole UI.
        import oneagent.installer as installer

        def explode(_text: str) -> dict:
            raise RuntimeError("unexpected shape")

        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            (home / ".codex").mkdir()
            (home / ".codex" / "config.toml").write_text('model_provider = "p"\n', encoding="utf-8")
            original = installer._CONFIG_READERS["codex"]
            installer._CONFIG_READERS["codex"] = explode
            try:
                detected = detect_agent_config(runtime_for(home), agent_catalog()["codex"])
                payload = status_payload(runtime_for(home))
            finally:
                installer._CONFIG_READERS["codex"] = original
        self.assertEqual(detected["unreadable"], "配置解析失败")
        self.assertEqual(payload["agents"]["codex"]["detected"]["unreadable"], "配置解析失败")

    def test_an_empty_or_absent_config_is_distinguished(self):
        with tempfile.TemporaryDirectory() as tmp:
            home = Path(tmp)
            catalog = agent_catalog()
            # Absent: nothing to report.
            self.assertIsNone(detect_agent_config(runtime_for(home), catalog["codex"]))
            # Present but empty: worth saying so, since the file exists.
            (home / ".codex").mkdir()
            (home / ".codex" / "config.toml").write_text("   \n", encoding="utf-8")
            self.assertIn("空", detect_agent_config(runtime_for(home), catalog["codex"])["unreadable"])


if __name__ == "__main__":
    unittest.main()
