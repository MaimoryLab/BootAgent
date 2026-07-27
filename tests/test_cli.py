from __future__ import annotations

import json
import io
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import MagicMock, patch

from oneagent import cli
from oneagent.errors import OneAgentError


ROOT = Path(__file__).resolve().parents[1]


class CliContractTests(unittest.TestCase):
    def run_cli(self, *args: str, env: dict[str, str] | None = None):
        values = os.environ.copy()
        values["PYTHONPATH"] = str(ROOT)
        if env:
            values.update(env)
        return subprocess.run(
            [sys.executable, "-m", "oneagent.cli", *args],
            cwd=ROOT,
            env=values,
            text=True,
            capture_output=True,
            timeout=30,
        )

    def test_json_success_contract(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = self.run_cli(
                "--agent",
                "openclaw",
                "--check-agent-only",
                "--json",
                "--home",
                tmp,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            payload = json.loads(result.stdout)
            self.assertTrue(payload["ok"])
            self.assertEqual(payload["results"][0]["status"], "guide-only")

    def call(self, *args: str) -> tuple[int, str, str]:
        """Invoke cli.run in-process.

        run_cli uses a subprocess, which is the honest end-to-end check but is
        invisible to coverage. These call the entry point directly so the
        branches are measured.
        """
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            code = cli.run(list(args))
        return code, out.getvalue(), err.getvalue()

    def test_agent_subcommand_in_process_covers_both_output_paths(self):
        with tempfile.TemporaryDirectory() as tmp:
            code, out, _ = self.call("agent", "list", "--home", tmp)
            self.assertEqual(code, 0)
            self.assertIn("no Agent", out)

            code, out, _ = self.call(
                "agent", "set", "codex", "--provider", "ppio", "--model", "m",
                "--api-key", "K", "--home", tmp,
            )
            self.assertEqual(code, 0)
            self.assertIn("codex -> ppio / m", out)
            self.assertIn("next:", out)
            self.assertNotIn("K", out.replace("Key", "").replace("okay", ""))

            code, out, _ = self.call("agent", "list", "--home", tmp)
            self.assertEqual(code, 0)
            self.assertIn("ppio", out)

            code, out, _ = self.call("agent", "list", "--json", "--home", tmp)
            self.assertEqual(json.loads(out)["agents"]["codex"]["model"], "m")

            # Error paths on both the JSON and the plain branch.
            code, out, _ = self.call(
                "agent", "set", "cursor", "--api-key", "k", "--model", "m", "--json", "--home", tmp
            )
            self.assertEqual(code, 2)
            self.assertEqual(json.loads(out)["error_code"], "INVALID_REQUEST")

            code, _, err = self.call("agent", "set", "cursor", "--api-key", "k", "--model", "m", "--home", tmp)
            self.assertEqual(code, 2)
            self.assertIn("guide-only", err)

    def test_agent_set_reads_the_key_from_the_environment(self):
        with tempfile.TemporaryDirectory() as tmp:
            with patch.dict(os.environ, {"ONEAGENT_API_KEY": "FROM-ENV"}):
                code, _, _ = self.call(
                    "agent", "set", "codex", "--provider", "ppio", "--model", "m", "--home", tmp
                )
            self.assertEqual(code, 0)
            env = (Path(tmp) / ".oneagent" / "agents" / "codex.env").read_text(encoding="utf-8")
            self.assertIn("FROM-ENV", env)

    def test_agent_subcommand_lists_and_repoints_each_agent(self):
        with tempfile.TemporaryDirectory() as tmp:
            empty = self.run_cli("agent", "list", "--json", "--home", tmp)
            self.assertEqual(empty.returncode, 0, empty.stderr)
            self.assertEqual(json.loads(empty.stdout)["agents"], {})

            first = self.run_cli(
                "agent", "set", "codex", "--provider", "ppio", "--model", "m-a",
                "--api-key", "K-CODEX", "--json", "--home", tmp,
            )
            self.assertEqual(first.returncode, 0, first.stderr)
            payload = json.loads(first.stdout)
            self.assertEqual(payload["provider"], "ppio")
            self.assertTrue(payload["restart"])
            self.assertNotIn("K-CODEX", first.stdout)

            self.run_cli(
                "agent", "set", "opencode", "--provider", "novita", "--model", "m-b",
                "--api-key", "K-OPENCODE", "--json", "--home", tmp,
            )
            listed = json.loads(self.run_cli("agent", "list", "--json", "--home", tmp).stdout)["agents"]
            self.assertEqual(listed["codex"]["provider"], "ppio")
            self.assertEqual(listed["opencode"]["provider"], "novita")

            # Human-readable output names both Agents and their providers.
            plain = self.run_cli("agent", "list", "--home", tmp)
            self.assertIn("codex", plain.stdout)
            self.assertIn("novita", plain.stdout)

    def test_agent_subcommand_reports_errors_with_stable_exit_codes(self):
        with tempfile.TemporaryDirectory() as tmp:
            guide_only = self.run_cli(
                "agent", "set", "cursor", "--api-key", "k", "--model", "m", "--json", "--home", tmp
            )
            self.assertEqual(guide_only.returncode, 2)
            self.assertEqual(json.loads(guide_only.stdout)["error_code"], "INVALID_REQUEST")

            no_key = self.run_cli("agent", "set", "codex", "--model", "m", "--home", tmp)
            self.assertEqual(no_key.returncode, 2)
            self.assertIn("API key", no_key.stderr)

            # The empty listing still prints something on the plain path.
            plain = self.run_cli("agent", "list", "--home", tmp)
            self.assertIn("no Agent", plain.stdout)

    def test_agent_subcommand_can_reuse_a_saved_profile_key(self):
        with tempfile.TemporaryDirectory() as tmp:
            from oneagent.installer import Runtime, save_profile

            save_profile(
                Runtime.create(home=Path(tmp), os_id="linux", which=lambda _n: None),
                profile_id="team",
                provider="ppio",
                model="m",
                agent_ids=["codex"],
                api_key="STORED",
            )
            result = self.run_cli(
                "agent", "set", "codex", "--provider", "ppio", "--model", "m",
                "--profile", "team", "--json", "--home", tmp,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn("STORED", result.stdout)
            env = (Path(tmp) / ".oneagent" / "agents" / "codex.env").read_text(encoding="utf-8")
            self.assertIn("STORED", env)

    def test_json_invalid_agent_has_stable_exit_code(self):
        result = self.run_cli("--agent", "unknown", "--check-agent-only", "--json")
        self.assertEqual(result.returncode, 2)
        payload = json.loads(result.stdout)
        self.assertEqual(payload["error_code"], "INVALID_REQUEST")
        self.assertEqual(payload["status"], 400)
        self.assertFalse(payload["retryable"])

    def test_latest_and_locked_version_are_mutually_exclusive(self):
        result = self.run_cli("--check-agent-only", "--latest", "--locked-version", "--json")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(json.loads(result.stdout)["error_code"], "INVALID_REQUEST")


class CliUnitTests(unittest.TestCase):
    def test_registration_url_precedence(self):
        parser = cli.build_parser()
        args = parser.parse_args(["--provider", "novita", "--register-url", "https://example.test/register"])
        self.assertEqual(cli._registration_url(args), "https://example.test/register")
        args.register_url = ""
        with patch.dict(os.environ, {"ONEAGENT_REGISTER_URL": "https://env.test/register"}):
            self.assertEqual(cli._registration_url(args), "https://env.test/register")
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(cli._registration_url(args), "https://novita.ai/")
            args.provider = "custom"
            self.assertEqual(cli._registration_url(args), "https://ppio.com/")

    def test_api_key_resolution_modes(self):
        parser = cli.build_parser()
        args = parser.parse_args(["--check-agent-only"])
        self.assertEqual(cli._resolve_api_key(args), "")

        args = parser.parse_args(["--api-key", "argument-key"])
        with patch.dict(os.environ, {"ONEAGENT_API_KEY": "environment-key"}):
            self.assertEqual(cli._resolve_api_key(args), "environment-key")
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(cli._resolve_api_key(args), "argument-key")

        args = parser.parse_args(["--json"])
        with patch.dict(os.environ, {}, clear=True), patch.object(sys.stdin, "isatty", return_value=False):
            with self.assertRaises(OneAgentError):
                cli._resolve_api_key(args)

        args = parser.parse_args(["--provider", "ppio"])
        stderr = io.StringIO()
        with (
            patch.dict(os.environ, {}, clear=True),
            patch.object(sys.stdin, "isatty", return_value=True),
            patch("oneagent.cli.webbrowser.open") as browser,
            patch("oneagent.cli.getpass.getpass", return_value="interactive-key"),
            redirect_stderr(stderr),
        ):
            self.assertEqual(cli._resolve_api_key(args), "interactive-key")
        browser.assert_called_once_with("https://ppio.com/")
        self.assertIn("Create or copy", stderr.getvalue())

    def test_resolve_api_key_refuses_without_a_tty(self):
        parser = cli.build_parser()
        args = parser.parse_args(["--provider", "ppio"])
        with patch.dict(os.environ, {}, clear=True), patch.object(sys.stdin, "isatty", return_value=False):
            with self.assertRaises(OneAgentError) as missing:
                cli._resolve_api_key(args)
        self.assertEqual(missing.exception.code, "INVALID_REQUEST")
        self.assertIn("ONEAGENT_API_KEY", missing.exception.message)

    def test_run_splits_comma_separated_agents(self):
        result = {"ok": True, "code": 0, "log": "", "next": "", "results": [], "probe": None}
        with (
            patch("oneagent.cli.install_many", return_value=result) as many,
            patch("oneagent.cli.Runtime.create", return_value=MagicMock()),
        ):
            stdout = io.StringIO()
            with redirect_stdout(stdout):
                self.assertEqual(cli.run(["--agent", "codex, claude-code ", "--api-key", "k", "--json"]), 0)
        self.assertEqual(many.call_args.args[0].agents, ["codex", "claude-code"])

    def test_run_rejects_an_empty_agent_list(self):
        stderr = io.StringIO()
        with patch("oneagent.cli.Runtime.create", return_value=MagicMock()), redirect_stderr(stderr):
            self.assertEqual(cli.run(["--agent", ",", "--check-agent-only"]), 2)
        self.assertIn("At least one Agent", stderr.getvalue())

    def test_run_outputs_human_and_json_results(self):
        result = {"ok": True, "code": 0, "log": "configured", "next": "codex", "results": [], "probe": None}
        with patch("oneagent.cli.install_many", return_value=result), patch("oneagent.cli.Runtime.create", return_value=MagicMock()):
            stdout = io.StringIO()
            with redirect_stdout(stdout):
                self.assertEqual(cli.run(["--check-agent-only"]), 0)
            self.assertIn("configured", stdout.getvalue())
            self.assertIn("[oneagent] next: codex", stdout.getvalue())

            stdout = io.StringIO()
            with redirect_stdout(stdout):
                self.assertEqual(cli.run(["--check-agent-only", "--json"]), 0)
            self.assertTrue(json.loads(stdout.getvalue())["ok"])

    def test_run_maps_expected_and_unexpected_failures(self):
        with patch("oneagent.cli.install_many", side_effect=OneAgentError("CONFIG_WRITE_FAILED", "secret failed")), patch("oneagent.cli.Runtime.create", return_value=MagicMock()):
            stderr = io.StringIO()
            with redirect_stderr(stderr):
                self.assertEqual(cli.run(["--api-key", "secret", "--skip-test"]), 5)
            self.assertNotIn("secret failed", stderr.getvalue().replace("[redacted] failed", ""))
            self.assertIn("[redacted]", stderr.getvalue())

        with patch("oneagent.cli.install_many", side_effect=OneAgentError("INVALID_REQUEST", "bad")), patch("oneagent.cli.Runtime.create", return_value=MagicMock()):
            stdout = io.StringIO()
            with redirect_stdout(stdout):
                self.assertEqual(cli.run(["--api-key", "key", "--json"]), 2)
            self.assertEqual(json.loads(stdout.getvalue())["error_code"], "INVALID_REQUEST")

        with patch("oneagent.cli.install_many", side_effect=KeyboardInterrupt), patch("oneagent.cli.Runtime.create", return_value=MagicMock()):
            self.assertEqual(cli.run(["--api-key", "key"]), 130)

        with patch("oneagent.cli.install_many", side_effect=RuntimeError("details")), patch("oneagent.cli.Runtime.create", return_value=MagicMock()):
            stderr = io.StringIO()
            with redirect_stderr(stderr):
                self.assertEqual(cli.run(["--api-key", "key"]), 10)
            self.assertIn("Unexpected OneAgent failure", stderr.getvalue())

        with (
            patch("oneagent.cli.install_many", side_effect=RuntimeError("details")),
            patch("oneagent.cli.Runtime.create", return_value=MagicMock()),
            patch.dict(os.environ, {"ONEAGENT_DEBUG": "1"}),
        ):
            with self.assertRaisesRegex(RuntimeError, "details"):
                cli.run(["--api-key", "key"])

    def test_main_exits_with_run_code(self):
        with patch("oneagent.cli.run", return_value=7):
            with self.assertRaises(SystemExit) as error:
                cli.main()
        self.assertEqual(error.exception.code, 7)


if __name__ == "__main__":
    unittest.main()
