from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from unittest.mock import patch

from oneagent.installer import Runtime
from scripts.provider_rc_smoke import (
    ProviderSmokeConfig,
    config_from_env,
    load_api_key_json,
    main as provider_smoke_main,
    smoke_provider,
)
from scripts.verify_locked_agents import create_isolated_runtime, verify_locked_agents
from tests.support import LocalHTTPServer


ROOT = Path(__file__).resolve().parents[1]


class ProtocolHandler(BaseHTTPRequestHandler):
    requests: list[dict[str, object]] = []

    def log_message(self, *_args):
        return

    def _record(self, body: bytes = b""):
        self.requests.append(
            {
                "path": self.path,
                "authorization": self.headers.get("Authorization"),
                "x_api_key": self.headers.get("X-Api-Key"),
                "body": json.loads(body.decode()) if body else None,
            }
        )
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def do_GET(self):
        self._record()

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        self._record(self.rfile.read(length))


class ReleaseCandidateScriptTests(unittest.TestCase):
    def test_provider_smoke_script_can_run_directly(self):
        result = subprocess.run(
            [sys.executable, str(ROOT / "scripts" / "provider_rc_smoke.py"), "--help"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--provider", result.stdout)

    def test_provider_smoke_covers_all_protocols_without_key_in_urls(self):
        ProtocolHandler.requests = []
        server = LocalHTTPServer(("127.0.0.1", 0), ProtocolHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            base = f"http://127.0.0.1:{server.server_port}"
            results = smoke_provider(
                ProviderSmokeConfig(
                    provider="ppio",
                    api_key="sentinel-rc-key",
                    openai_model="chat-model",
                    anthropic_model="anthropic-model",
                    responses_model="responses-model",
                    openai_base=base,
                    anthropic_base=base + "/anthropic",
                ),
                timeout=2,
            )
        finally:
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)
        self.assertEqual(set(results), {"models", "chat", "responses", "anthropic"})
        self.assertEqual(
            [item["path"] for item in ProtocolHandler.requests],
            ["/v1/models", "/v1/chat/completions", "/v1/responses", "/anthropic/v1/messages"],
        )
        self.assertTrue(all("sentinel-rc-key" not in str(item["path"]) for item in ProtocolHandler.requests))
        self.assertEqual(ProtocolHandler.requests[-1]["x_api_key"], "sentinel-rc-key")

    def test_apiproxy_profile_keeps_three_protocol_model_slots(self):
        with patch.dict(os.environ, {}, clear=True):
            config = config_from_env("apiproxy")
        self.assertEqual(config.openai_base, "https://apiproxy.paigod.work")
        self.assertEqual(config.anthropic_base, "https://apiproxy.paigod.work")
        self.assertEqual(config.openai_model, "openai/gpt-5.6-terra")
        self.assertEqual(config.anthropic_model, "openai/gpt-5.6-terra")
        self.assertEqual(config.responses_model, "openai/gpt-5.6-terra")

    def test_formal_provider_does_not_inherit_temporary_proxy_defaults(self):
        with patch.dict(os.environ, {}, clear=True):
            config = config_from_env("ppio")
        self.assertEqual(config.openai_model, "")
        self.assertEqual(config.anthropic_model, "")
        self.assertEqual(config.responses_model, "")
        self.assertEqual(config.openai_base, "")
        self.assertEqual(config.anthropic_base, "")

    def test_local_json_key_is_loaded_without_appearing_in_cli_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            auth_file = Path(tmp) / "auth.json"
            auth_file.write_text(json.dumps({"OPENAI_API_KEY": "sentinel-json-key"}), encoding="utf-8")
            self.assertEqual(load_api_key_json(auth_file, "OPENAI_API_KEY"), "sentinel-json-key")
            with patch.object(
                sys,
                "argv",
                [
                    "provider_rc_smoke.py",
                    "--provider",
                    "apiproxy",
                    "--api-key-json",
                    str(auth_file),
                ],
            ), patch(
                "scripts.provider_rc_smoke.smoke_provider",
                return_value={"models": 200, "chat": 200, "responses": 200, "anthropic": 200},
            ) as smoke, patch("builtins.print") as output:
                provider_smoke_main()
        config = smoke.call_args.args[0]
        self.assertEqual(config.api_key, "sentinel-json-key")
        self.assertNotIn("sentinel-json-key", " ".join(str(call) for call in output.call_args_list))

    def test_formal_all_mode_excludes_local_apiproxy_profile(self):
        with patch.object(
            sys,
            "argv",
            ["provider_rc_smoke.py", "--provider", "all"],
        ), patch(
            "scripts.provider_rc_smoke.smoke_provider",
            return_value={"models": 200, "chat": 200, "responses": 200, "anthropic": 200},
        ) as smoke, patch("builtins.print"):
            provider_smoke_main()
        self.assertEqual([call.args[0].provider for call in smoke.call_args_list], ["ppio", "novita"])

    def test_locked_agent_verifier_rejects_version_mismatch(self):
        manifest = {
            "agents": {
                "agent": {
                    "name": "Agent",
                    "config_mode": "auto",
                    "command": "agent",
                    "version_args": ["--version"],
                    "package": {"version": "1.2.3"},
                }
            }
        }
        with tempfile.TemporaryDirectory() as tmp:
            runtime = Runtime.create(home=Path(tmp), os_id="linux", which=lambda _name: "/bin/agent")
            runtime.runner = lambda args, **_kwargs: subprocess.CompletedProcess(args, 0, "agent 9.9.9", "")
            with patch("scripts.verify_locked_agents.load_manifest", return_value=manifest), patch(
                "scripts.verify_locked_agents.install_many",
                return_value={"ok": True, "results": [{"status": "installed"}]},
            ):
                with self.assertRaisesRegex(RuntimeError, "expected 1.2.3"):
                    verify_locked_agents(runtime)

    def test_locked_agent_verifier_isolates_home_package_managers_and_secrets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            toolchain = root / "toolchain"
            toolchain.mkdir()
            npm = toolchain / ("npm.cmd" if os.name == "nt" else "npm")
            npm.write_text("", encoding="utf-8")
            npm.chmod(0o755)
            system_codex = toolchain / ("codex.cmd" if os.name == "nt" else "codex")
            system_codex.write_text("", encoding="utf-8")
            system_codex.chmod(0o755)
            runtime = create_isolated_runtime(
                root / "cleanroom",
                os_id="windows" if os.name == "nt" else "linux",
                base_env={
                    "PATH": str(toolchain),
                    "OPENAI_API_KEY": "sentinel-rc-secret",
                    "UV_CONFIG_FILE": str(root / "real-uv.toml"),
                    "SAFE_VALUE": "preserved",
                },
            )
            self.assertIsNone(runtime.which("codex"))
            if os.name != "nt":
                aider_target = Path(runtime.env["UV_TOOL_DIR"]) / "aider-chat" / "bin" / "aider"
                aider_target.parent.mkdir(parents=True)
                aider_target.write_text("#!/bin/sh\n", encoding="utf-8")
                aider_target.chmod(0o755)
                aider_link = Path(runtime.env["UV_TOOL_BIN_DIR"]) / "aider"
                aider_link.symlink_to(aider_target)
                self.assertEqual(Path(runtime.which("aider") or ""), aider_link)
        self.assertEqual(runtime.env["HOME"], str(root / "cleanroom" / "home"))
        self.assertEqual(runtime.env["ONEAGENT_HOME"], runtime.env["HOME"])
        self.assertTrue(runtime.env["NPM_CONFIG_PREFIX"].startswith(str(root / "cleanroom")))
        self.assertTrue(runtime.env["UV_TOOL_DIR"].startswith(str(root / "cleanroom")))
        self.assertTrue(runtime.env["UV_TOOL_BIN_DIR"].startswith(str(root / "cleanroom")))
        self.assertTrue(runtime.env["NPM_CONFIG_USERCONFIG"].startswith(str(root / "cleanroom")))
        self.assertTrue(runtime.env["XDG_CONFIG_HOME"].startswith(str(root / "cleanroom")))
        self.assertNotIn("UV_CONFIG_FILE", runtime.env)
        self.assertNotIn("OPENAI_API_KEY", runtime.env)
        self.assertEqual(runtime.env["SAFE_VALUE"], "preserved")


if __name__ == "__main__":
    unittest.main()
