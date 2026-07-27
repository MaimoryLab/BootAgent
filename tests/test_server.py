from __future__ import annotations

import http.cookiejar
import json
import os
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener

from oneagent.errors import OneAgentError
from oneagent.installer import Runtime
from oneagent.server import (
    MAX_BODY_BYTES,
    OneAgentHTTPServer,
    create_server,
    main as server_main,
    read_json,
)
from tests.support import LocalHTTPServer


class ProviderHandler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        return

    def _status(self):
        authorization = self.headers.get("Authorization", "")
        if "key-401" in authorization:
            return 401
        if "key-403" in authorization:
            return 403
        if "key-500" in authorization:
            return 500
        if "key-timeout" in authorization:
            time.sleep(0.3)
        if self.path.endswith("/models") and "key-404" in authorization:
            return 404
        if self.path.endswith("/models") and "key-405" in authorization:
            return 405
        return 200

    def do_GET(self):
        status = self._status()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        try:
            if status == 200 and "key-list" in self.headers.get("Authorization", ""):
                self.wfile.write(b'[{"id":"model-list"}]')
            elif status == 200:
                self.wfile.write(b'{"data":[{"id":"model-a"},{"id":"model-b"}]}')
            else:
                self.wfile.write(b'{"error":"provider error"}')
        except BrokenPipeError:
            pass

    def do_POST(self):
        status = self._status()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        try:
            self.wfile.write(b'{"ok":true}')
        except BrokenPipeError:
            pass


class LocalServer:
    def __init__(self, server):
        self.server = server
        self.thread = threading.Thread(target=server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self.server

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)


def request_json(opener, url, payload=None, *, origin=None):
    headers = {}
    data = None
    if payload is not None:
        data = json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    if origin:
        headers["Origin"] = origin
    request = Request(url, data=data, headers=headers, method="POST" if payload is not None else "GET")
    try:
        with opener.open(request, timeout=2) as response:
            return response.status, dict(response.headers), json.loads(response.read().decode())
    except HTTPError as exc:
        try:
            return exc.code, dict(exc.headers), json.loads(exc.read().decode())
        finally:
            exc.close()


class ServerContractTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.static = self.root / "dist"
        (self.static / "assets").mkdir(parents=True)
        (self.static / "index.html").write_text("<!doctype html><div id='root'></div>", encoding="utf-8")
        (self.static / "assets" / "app-DiZRmN5o.js").write_text("console.log('ok')", encoding="utf-8")
        (self.static / "assets" / "payload.bin").write_bytes(b"not an allowed web asset")
        self.home = self.root / "home"
        self.home.mkdir()
        self.runtime = Runtime.create(home=self.home, os_id="linux", which=lambda _name: None)
        self.server = create_server("127.0.0.1", 0)
        self.base = f"http://127.0.0.1:{self.server.server_port}"
        self.origin = self.base
        self.cookies = http.cookiejar.CookieJar()
        self.opener = build_opener(HTTPCookieProcessor(self.cookies))
        self.patches = [
            patch("oneagent.server.static_root", return_value=self.static),
            patch("oneagent.server._runtime", return_value=self.runtime),
            patch.dict(os.environ, {"ONEAGENT_DISABLE_BROWSER": "1"}),
        ]
        for item in self.patches:
            item.start()
        self.local = LocalServer(self.server)
        self.local.__enter__()

    def tearDown(self):
        self.local.__exit__(None, None, None)
        for item in reversed(self.patches):
            item.stop()
        self.tmp.cleanup()

    def bootstrap(self):
        with self.opener.open(self.base + "/", timeout=2) as response:
            self.assertEqual(response.status, 200)
            self.assertIn("default-src 'self'", response.headers["Content-Security-Policy"])
            self.assertEqual(response.headers["Cache-Control"], "no-store")
        self.assertTrue(any(cookie.name == "oneagent_session" and cookie.value for cookie in self.cookies))

    def post(self, path, payload, *, origin=True):
        return request_json(self.opener, self.base + path, payload, origin=self.origin if origin is True else origin)

    def test_status_is_additive_and_does_not_require_session(self):
        status, _, payload = request_json(self.opener, self.base + "/api/status")
        self.assertEqual(status, 200)
        self.assertEqual(payload["apiVersion"], 1)
        self.assertEqual(payload["platform"]["os"], "linux")
        self.assertEqual(payload["agents"]["codex"]["lockedVersion"], "0.145.0")
        self.assertIn("capabilities", payload)

    def test_bind_does_not_perform_reverse_dns_lookup(self):
        # http.server's default server_bind() calls socket.getfqdn(), which blocks
        # until it times out on hosts with no reverse resolver. That stalled GUI
        # startup by ~70s on CI runners and would do the same on offline machines.
        with patch("socket.getfqdn", side_effect=AssertionError("reverse DNS lookup during bind")) as getfqdn:
            server = create_server("127.0.0.1", 0)
            try:
                self.assertEqual(server.server_name, "127.0.0.1")
                self.assertEqual(server.server_port, server.socket.getsockname()[1])
            finally:
                server.server_close()
        getfqdn.assert_not_called()

    def test_server_refuses_to_bind_outside_localhost(self):
        # The whole security model assumes a loopback-only listener: the Origin
        # allowlist and the session cookie are meaningless if the socket is
        # reachable from the network. Nothing covered this guard, so dropping
        # it in a refactor would have kept the suite green.
        for host in ("0.0.0.0", "::", "192.168.1.10", "localhost", ""):
            with self.subTest(host=host):
                with self.assertRaises(OneAgentError) as refused:
                    create_server(host, 0)
                self.assertEqual(refused.exception.code, "INVALID_REQUEST")
                self.assertIn("127.0.0.1", refused.exception.message)

    def test_main_reports_a_rejected_host_without_a_traceback(self):
        # serve_forever is stubbed so that a missing guard fails this test
        # immediately. Left unstubbed, main() would bind 0.0.0.0 and block, and
        # the regression would surface as a CI job timeout rather than a
        # failure naming the cause.
        with patch("oneagent.server.webbrowser.open") as browser, patch.object(
            OneAgentHTTPServer,
            "serve_forever",
            side_effect=AssertionError("main() must reject a non-loopback host before serving"),
        ):
            with self.assertRaises(SystemExit) as exited:
                server_main(["--host", "0.0.0.0"])
        self.assertIn("127.0.0.1", str(exited.exception))
        browser.assert_not_called()

    def test_missing_frontend_build_serves_a_readable_page(self):
        # Running the GUI from a source checkout before `npm run build` must
        # explain itself rather than surface a 404 or a traceback.
        with tempfile.TemporaryDirectory() as empty:
            with patch("oneagent.server.static_root", return_value=Path(empty)):
                try:
                    self.opener.open(self.base + "/", timeout=2)
                    self.fail("expected HTTP 503")
                except HTTPError as exc:
                    body = exc.read().decode()
                    self.assertEqual(exc.code, 503)
                    self.assertIn("frontend build is missing", body)
                    self.assertEqual(exc.headers["Cache-Control"], "no-store")
                    self.assertIn("oneagent_session=", exc.headers["Set-Cookie"])
                    self.assertIn("HttpOnly", exc.headers["Set-Cookie"])
                    exc.close()

    def test_post_requires_origin_and_session_cookie(self):
        status, _, payload = self.post("/api/models", {"provider": "ppio", "api_key": "x"}, origin=self.origin)
        self.assertEqual(status, 403)
        self.assertEqual(payload["error_code"], "INVALID_ORIGIN")
        self.bootstrap()
        status, _, payload = self.post("/api/models", {"provider": "ppio", "api_key": "x"}, origin=False)
        self.assertEqual(status, 403)
        self.assertEqual(payload["error_code"], "INVALID_ORIGIN")
        status, _, payload = self.post("/api/models", {"provider": "ppio", "api_key": "x"}, origin="http://127.0.0.1:1")
        self.assertEqual(status, 403)
        self.assertEqual(payload["error_code"], "INVALID_ORIGIN")

    def test_static_assets_have_safe_cache_and_traversal_is_rejected(self):
        self.bootstrap()
        with self.opener.open(self.base + "/assets/app-DiZRmN5o.js", timeout=2) as response:
            self.assertIn("immutable", response.headers["Cache-Control"])
            self.assertEqual(response.headers["X-Content-Type-Options"], "nosniff")
        with self.assertRaises(HTTPError) as blocked_type:
            self.opener.open(self.base + "/assets/payload.bin", timeout=2)
        self.assertEqual(blocked_type.exception.code, 404)
        blocked_type.exception.close()
        with self.assertRaises(HTTPError) as error:
            self.opener.open(self.base + "/%2e%2e/agents.lock.json", timeout=2)
        self.assertEqual(error.exception.code, 404)
        error.exception.close()

    def test_json_body_rejects_invalid_negative_and_oversized_lengths(self):
        class RejectRead:
            def read(self, _length):
                raise AssertionError("invalid Content-Length must be rejected before reading the body")

        for value in ["not-a-number", "-1", str(MAX_BODY_BYTES + 1)]:
            with self.subTest(content_length=value):
                handler = SimpleNamespace(headers={"Content-Length": value}, rfile=RejectRead())
                with self.assertRaises(OneAgentError):
                    read_json(handler)

    def test_register_urls_and_invalid_provider(self):
        self.bootstrap()
        status, _, payload = self.post("/api/open-register", {"provider": "ppio", "agents": ["codex"]})
        self.assertEqual(status, 200)
        self.assertTrue(payload["url"].startswith("https://ppio.com/?"))
        status, _, payload = self.post("/api/open-register", {"provider": "novita", "agents": ["aider"]})
        self.assertEqual(status, 200)
        self.assertTrue(payload["url"].startswith("https://novita.ai/?"))
        status, _, payload = self.post("/api/open-register", {"provider": "custom"})
        self.assertEqual(status, 400)
        self.assertEqual(payload["error_code"], "INVALID_REQUEST")

    def test_probe_and_models_map_provider_failures(self):
        provider = LocalHTTPServer(("127.0.0.1", 0), ProviderHandler)
        with LocalServer(provider):
            custom_base = f"http://127.0.0.1:{provider.server_port}"
            self.bootstrap()
            for key, expected in [("key-401", "API_KEY_REJECTED"), ("key-403", "API_KEY_REJECTED"), ("key-500", "PROVIDER_UNREACHABLE")]:
                with self.subTest(key=key):
                    status, _, payload = self.post(
                        "/api/probe",
                        {"provider": "custom", "api_base_url": custom_base, "api_key": key, "model": "m"},
                    )
                    self.assertEqual(status, 200)
                    self.assertEqual(payload["error_code"], expected)
            for key, expected in [("key-404", "MODELS_UNSUPPORTED"), ("key-405", "MODELS_UNSUPPORTED"), ("key-500", "PROVIDER_UNREACHABLE")]:
                with self.subTest(key=key):
                    status, _, payload = self.post(
                        "/api/models",
                        {"provider": "custom", "api_base_url": custom_base, "api_key": key},
                    )
                    self.assertEqual(status, 200)
                    self.assertEqual(payload["error_code"], expected)
            status, _, payload = self.post(
                "/api/models",
                {"provider": "custom", "api_base_url": custom_base, "api_key": "key-200"},
            )
            self.assertEqual(status, 200)
            self.assertEqual(payload["models"], ["model-a", "model-b"])
            status, _, payload = self.post(
                "/api/models",
                {"provider": "custom", "api_base_url": custom_base, "api_key": "key-list"},
            )
            self.assertEqual(status, 200)
            self.assertEqual(payload["models"], ["model-list"])

    def test_probe_tests_the_protocol_of_each_selected_agent(self):
        calls: list[tuple[str, str]] = []

        def fake_probe(*, protocol, model, **_kwargs):
            calls.append((protocol, model))
            # Mirror the real endpoint: the model serves chat but refuses Responses.
            if protocol == "responses":
                return {
                    "ok": False, "reachable": True, "protocol": protocol, "status": 400,
                    "message": "does not support OpenAI Responses",
                    "error_code": "PROTOCOL_UNSUPPORTED", "retryable": False,
                }
            return {
                "ok": True, "reachable": True, "protocol": protocol, "status": 200,
                "message": "passed", "error_code": None, "retryable": False,
            }

        self.bootstrap()
        with patch("oneagent.server.protocol_probe", side_effect=fake_probe):
            # Codex alone -> Responses only, and the refusal must surface.
            _, _, payload = self.post(
                "/api/probe",
                {"provider": "custom", "api_base_url": "https://x.test/v1", "api_key": "k",
                 "model": "glm", "agents": ["codex"]},
            )
            self.assertFalse(payload["ok"])
            self.assertEqual(payload["error_code"], "PROTOCOL_UNSUPPORTED")
            self.assertEqual(sorted(payload["protocols"]), ["responses"])

            calls.clear()
            # OpenCode alone speaks Chat Completions, so the same model passes.
            _, _, payload = self.post(
                "/api/probe",
                {"provider": "custom", "api_base_url": "https://x.test/v1", "api_key": "k",
                 "model": "glm", "agents": ["opencode"]},
            )
            self.assertTrue(payload["ok"])
            self.assertEqual(sorted(payload["protocols"]), ["openai"])

            calls.clear()
            # Mixed selection: every distinct protocol probed once, and the
            # failing one is reported even though another succeeded.
            _, _, payload = self.post(
                "/api/probe",
                {"provider": "custom", "api_base_url": "https://x.test/v1", "api_key": "k",
                 "model": "glm", "agents": ["codex", "opencode", "kilo-cli", "openclaw"]},
            )
            self.assertEqual(sorted(p for p, _ in calls), ["openai", "responses"])
            self.assertFalse(payload["ok"])
            self.assertEqual(payload["error_code"], "PROTOCOL_UNSUPPORTED")

            calls.clear()
            # No agents -> unchanged OpenAI-compatible behaviour for older clients.
            _, _, payload = self.post(
                "/api/probe",
                {"provider": "custom", "api_base_url": "https://x.test/v1", "api_key": "k", "model": "glm"},
            )
            self.assertEqual([p for p, _ in calls], ["openai"])
            self.assertTrue(payload["ok"])

    def test_provider_timeout_is_retryable(self):
        provider = LocalHTTPServer(("127.0.0.1", 0), ProviderHandler)
        with LocalServer(provider), patch.dict(os.environ, {"ONEAGENT_HTTP_TIMEOUT": "0.05"}):
            self.bootstrap()
            status, _, payload = self.post(
                "/api/probe",
                {
                    "provider": "custom",
                    "api_base_url": f"http://127.0.0.1:{provider.server_port}",
                    "api_key": "key-timeout",
                    "model": "m",
                },
            )
            self.assertEqual(status, 200)
            self.assertEqual(payload["error_code"], "TIMEOUT")
            self.assertTrue(payload["retryable"])

    def test_install_writes_profile_without_leaking_key(self):
        self.bootstrap()
        secret = "sentinel-api-secret"
        status, _, payload = self.post(
            "/api/install",
            {
                "agents": ["codex", "openclaw"],
                "provider": "ppio",
                "api_key": secret,
                "model": "model-a",
                "configure": True,
                "install_agent": False,
                "skip_test": True,
            },
        )
        self.assertEqual(status, 200)
        self.assertTrue(payload["ok"])
        self.assertNotIn(secret, json.dumps(payload))
        profile = json.loads((self.home / ".oneagent" / "profiles" / "default.json").read_text(encoding="utf-8"))
        self.assertEqual(profile["agent_ids"], ["codex", "openclaw"])
        self.assertNotIn(secret, json.dumps(profile))

    def test_existing_account_mode_runs_agent_workflow_and_writes_profile(self):
        self.bootstrap()
        status, _, payload = self.post(
            "/api/install",
            {
                "agents": ["codex"],
                "configure": False,
                "install_agent": False,
            },
        )
        self.assertEqual(status, 200)
        self.assertTrue(payload["ok"])
        self.assertEqual(payload["results"][0]["status"], "skipped")
        self.assertIn("Model configuration skipped", payload["log"])
        profile = json.loads((self.home / ".oneagent" / "profiles" / "default.json").read_text(encoding="utf-8"))
        self.assertEqual(profile["config_mode"], "existing-account")
        self.assertEqual(profile["provider"], "existing-account")
        self.assertEqual(profile["agent_ids"], ["codex"])
        self.assertIsNone(profile["base_url"])
        self.assertIsNone(profile["model"])
        self.assertFalse((self.home / ".oneagent" / "env").exists())

    def test_unknown_agent_is_invalid_request(self):
        self.bootstrap()
        status, _, payload = self.post(
            "/api/install",
            {"agents": ["unknown"], "configure": False, "install_agent": False},
        )
        self.assertEqual(status, 400)
        self.assertEqual(payload["error_code"], "INVALID_REQUEST")

    def test_install_request_types_and_version_modes_are_strict(self):
        self.bootstrap()
        for payload in (
            {"agents": [], "configure": False},
            {"agents": ["codex"], "configure": "false"},
            {"agents": ["codex"], "configure": False, "install_agent": 1},
            {"agents": ["codex"], "configure": False, "timeout": 0},
            {"agents": ["codex"], "configure": False, "locked_version": True, "latest": True},
            {"agents": ["codex"], "profile_agents": ["opencode"], "configure": False},
        ):
            with self.subTest(payload=payload):
                status, _, response = self.post("/api/install", payload)
                self.assertEqual(status, 400)
                self.assertEqual(response["error_code"], "INVALID_REQUEST")

    def test_register_rejects_unknown_agent(self):
        self.bootstrap()
        status, _, payload = self.post("/api/open-register", {"provider": "ppio", "agents": ["unknown"]})
        self.assertEqual(status, 400)
        self.assertEqual(payload["error_code"], "INVALID_REQUEST")

    def test_profiles_endpoint_lists_saved_profiles_without_key_material(self):
        self.bootstrap()
        status, _, body = request_json(self.opener, self.base + "/api/profiles")
        self.assertEqual(status, 200)
        self.assertEqual(body, {"ok": True, "profiles": [], "activeProfile": None})

        secret = "sentinel-profile-key"
        status, _, payload = self.post(
            "/api/profiles",
            {
                "provider": "ppio",
                "model": "deepseek-v3",
                "agents": ["codex", "opencode"],
                "api_key": secret,
                "label": "PPIO DeepSeek",
            },
        )
        self.assertEqual(status, 200)
        self.assertEqual(payload["profile"]["id"], "ppio-deepseek-v3")
        self.assertTrue(payload["profile"]["hasKey"])
        self.assertNotIn(secret, json.dumps(payload))

        # Saving stores a profile but does not activate it.
        status, _, body = request_json(self.opener, self.base + "/api/profiles")
        self.assertEqual(status, 200)
        self.assertEqual([item["id"] for item in body["profiles"]], ["ppio-deepseek-v3"])
        self.assertEqual(body["profiles"][0]["agentIds"], ["codex", "opencode"])
        self.assertIsNone(body["activeProfile"])
        self.assertNotIn(secret, json.dumps(body))

    def test_profiles_endpoint_validates_input(self):
        self.bootstrap()
        for payload in (
            {"provider": "ppio", "agents": ["codex"]},                # no model
            {"provider": "ppio", "model": "m"},                       # no agents
            {"provider": "nope", "model": "m", "agents": ["codex"]},  # unknown provider
            {"profile_id": "../x", "provider": "ppio", "model": "m", "agents": ["codex"]},
        ):
            with self.subTest(payload=payload):
                status, _, response = self.post("/api/profiles", payload)
                self.assertEqual(status, 400)
                self.assertEqual(response["error_code"], "INVALID_REQUEST")

    def test_install_activates_the_default_profile(self):
        self.bootstrap()
        status, _, payload = self.post(
            "/api/install",
            {
                "agents": ["codex"],
                "provider": "ppio",
                "api_key": "sentinel-install-key",
                "model": "m",
                "install_agent": False,
                "skip_test": True,
            },
        )
        self.assertEqual(status, 200)
        self.assertTrue(payload["ok"])
        status, _, body = request_json(self.opener, self.base + "/api/profiles")
        self.assertEqual(status, 200)
        self.assertEqual(body["activeProfile"], "default")
        self.assertEqual([item["id"] for item in body["profiles"]], ["default"])
        self.assertTrue(body["profiles"][0]["hasKey"])
        self.assertNotIn("sentinel-install-key", json.dumps(body))


if __name__ == "__main__":
    unittest.main()
