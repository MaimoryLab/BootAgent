#!/usr/bin/env python3
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import http.cookiejar
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor, Request, build_opener


ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from tests.support import LocalHTTPServer  # noqa: E402

GUI = ROOT / "scripts" / "gui.py"
CLIENT = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))
ORIGIN = ""


def fail(message):
    raise AssertionError(message)


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def request_json(url, payload=None):
    if payload is None:
        req = Request(url)
    else:
        data = json.dumps(payload).encode()
        req = Request(
            url,
            data=data,
            headers={"Content-Type": "application/json", "Origin": ORIGIN},
            method="POST",
        )
    try:
        with CLIENT.open(req, timeout=5) as res:
            return res.status, json.loads(res.read().decode())
    except HTTPError as exc:
        try:
            return exc.code, json.loads(exc.read().decode())
        finally:
            exc.close()


def bootstrap_client(base_url):
    global CLIENT, ORIGIN
    ORIGIN = base_url
    CLIENT = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))
    try:
        with CLIENT.open(base_url + "/", timeout=5) as response:
            response.read()
    except HTTPError as exc:
        try:
            exc.read()
        finally:
            exc.close()


def wait_for_server(base_url):
    for _ in range(50):
        try:
            status, _ = request_json(base_url + "/api/status")
            if status == 200:
                return
        except Exception:
            time.sleep(0.1)
    fail("GUI server did not start")


class MockModelHandler(BaseHTTPRequestHandler):
    status_code = 200
    models_status_code = 200

    def log_message(self, *_):
        return

    def do_GET(self):
        self.send_response(self.models_status_code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        if self.models_status_code == 200:
            self.wfile.write(b'{"data":[{"id":"model-a"},{"id":"model-b"}]}')
        else:
            self.wfile.write(b'{"error":"mock"}')

    def do_POST(self):
        self.send_response(self.status_code)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')


def start_model_server(status_code, models_status_code=200):
    server = LocalHTTPServer(("127.0.0.1", 0), MockModelHandler)
    server.RequestHandlerClass.status_code = status_code
    server.RequestHandlerClass.models_status_code = models_status_code
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, f"http://127.0.0.1:{server.server_port}/openai"


def write_executable(path, text):
    path.write_text(text)
    path.chmod(0o755)


def main():
    if not GUI.exists():
        fail("scripts/gui.py is missing")

    with tempfile.TemporaryDirectory() as tmpdir:
        tmp = Path(tmpdir)
        home = tmp / "home"
        fakebin = tmp / "bin"
        home.mkdir()
        fakebin.mkdir()

        npm_log = tmp / "npm.log"
        uv_log = tmp / "uv.log"
        write_executable(fakebin / "codex", "#!/usr/bin/env bash\necho fake codex\n")
        write_executable(fakebin / "claude", "#!/usr/bin/env bash\necho fake claude\n")
        write_executable(fakebin / "opencode", "#!/usr/bin/env bash\necho fake opencode\n")
        write_executable(fakebin / "kilo", "#!/usr/bin/env bash\necho fake kilo\n")
        write_executable(fakebin / "aider", "#!/usr/bin/env bash\necho fake aider\n")
        write_executable(fakebin / "cursor", "#!/usr/bin/env bash\necho fake cursor\n")
        write_executable(fakebin / "npm", f"#!/usr/bin/env bash\necho \"$@\" >> {npm_log}\n")
        write_executable(fakebin / "uv", f"#!/usr/bin/env bash\necho \"$@\" >> {uv_log}\n")
        write_executable(fakebin / "python3.12", "#!/usr/bin/env bash\necho Python 3.12.0\n")

        env = os.environ.copy()
        env["HOME"] = str(home)
        env["PATH"] = f"{fakebin}:/usr/bin:/bin"
        env["PYTHONUNBUFFERED"] = "1"

        port = free_port()
        proc = subprocess.Popen(
            [sys.executable, str(GUI), "--port", str(port), "--no-open"],
            cwd=str(ROOT),
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        base_url = f"http://127.0.0.1:{port}"
        try:
            wait_for_server(base_url)
            bootstrap_client(base_url)

            status, data = request_json(base_url + "/api/status")
            assert status == 200
            assert data["agents"]["codex"]["installed"] is True
            assert data["agents"]["claude-code"]["installed"] is True
            assert data["agents"]["opencode"]["installed"] is True
            assert data["agents"]["kilo-cli"]["installed"] is True
            assert data["agents"]["aider"]["installed"] is True
            assert data["agents"]["openclaw"]["guideOnly"] is True
            assert "catalog" in data
            assert {"auto", "gateway", "platform", "ide"} <= {group["id"] for group in data["groups"]}
            assert data["providers"]["ppio"]["base_url"] == "https://api.ppio.com/openai"
            assert data["providers"]["novita"]["base_url"] == "https://api.novita.ai/openai"

            model_server, model_base = start_model_server(200)
            try:
                status, data = request_json(base_url + "/api/probe", {
                    "provider": "custom",
                    "api_base_url": model_base,
                    "api_key": "sk-test",
                    "model": "gpt-test",
                })
                assert status == 200
                assert data["ok"] is True
                status, data = request_json(base_url + "/api/models", {
                    "provider": "custom",
                    "api_base_url": model_base,
                    "api_key": "sk-test",
                })
                assert status == 200
                assert data["models"] == ["model-a", "model-b"]
            finally:
                model_server.shutdown()

            status, data = request_json(base_url + "/api/install", {
                "agents": ["codex", "claude-code"],
                "provider": "ppio",
                "api_key": "sk-test",
                "model": "gpt-test",
                "configure": True,
                "install_agent": False,
                "skip_test": True,
            })
            assert status == 200
            assert data["ok"] is True
            assert "sk-test" not in data["log"]
            assert (home / ".codex" / "config.toml").exists()
            assert (home / ".claude" / "settings.json").exists()
            assert {item["agent"] for item in data["results"]} == {"codex", "claude-code"}

            status, data = request_json(base_url + "/api/install", {
                "agents": ["opencode", "kilo-cli", "aider", "openclaw", "cursor"],
                "provider": "ppio",
                "api_key": "sk-test",
                "model": "gpt-test",
                "configure": True,
                "install_agent": False,
                "skip_test": True,
            })
            assert status == 200
            assert data["ok"] is True
            assert "sk-test" not in json.dumps(data)
            by_agent = {item["agent"]: item["status"] for item in data["results"]}
            assert by_agent["opencode"] == "configured"
            assert by_agent["kilo-cli"] == "configured"
            assert by_agent["aider"] == "configured"
            assert by_agent["openclaw"] == "guide-only"
            assert by_agent["cursor"] == "guide-only"

            opencode_config = json.loads((home / ".config" / "opencode" / "opencode.jsonc").read_text())
            kilo_config = json.loads((home / ".config" / "kilo" / "kilo.jsonc").read_text())
            assert opencode_config["provider"]["oneagent"]["npm"] == "@ai-sdk/openai-compatible"
            assert opencode_config["provider"]["oneagent"]["options"]["baseURL"] == "https://api.ppio.com/openai/v1"
            # Each Agent reads its own variable so two of them can point at
            # different providers in one shell.
            assert opencode_config["provider"]["oneagent"]["options"]["apiKey"] == "{env:ONEAGENT_API_KEY_OPENCODE}"
            assert kilo_config["provider"]["oneagent"]["options"]["apiKey"] == "{env:ONEAGENT_API_KEY_KILO_CLI}"
            assert kilo_config["provider"]["oneagent"]["options"]["baseURL"] == "https://api.ppio.com/openai/v1"
            for agent in ("codex", "opencode", "kilo-cli"):
                assert (home / ".oneagent" / "agents" / f"{agent}.env").exists()
            assert (home / ".oneagent" / "aider.env").read_text().count("sk-test") == 1
            assert not (home / ".openclaw").exists()
            assert not (home / ".cursor").exists()

            status, data = request_json(base_url + "/api/install", {
                "agents": ["openclaw"],
                "configure": True,
                "install_agent": True,
            })
            assert status == 200
            assert data["ok"] is True
            assert data["results"][0]["status"] == "guide-only"
            assert not (home / ".openclaw").exists()

            status, data = request_json(base_url + "/api/install", {
                "agents": ["codex"],
                "configure": False,
                "install_agent": False,
            })
            assert status == 200
            assert data["ok"] is True
            assert "Model configuration skipped" in data["log"]

            (fakebin / "codex").unlink()
            status, data = request_json(base_url + "/api/install", {
                "agents": ["codex", "claude-code"],
                "provider": "ppio",
                "api_key": "sk-test",
                "model": "gpt-test",
                "configure": True,
                "install_agent": True,
                "skip_test": True,
            })
            assert status == 200
            npm_calls = npm_log.read_text()
            assert "@openai/codex" in npm_calls

            (fakebin / "claude").unlink()
            status, data = request_json(base_url + "/api/install", {
                "agents": ["claude-code"],
                "configure": False,
                "install_agent": True,
            })
            assert status == 200
            assert "@anthropic-ai/claude-code" in npm_log.read_text()

            (fakebin / "opencode").unlink()
            (fakebin / "kilo").unlink()
            (fakebin / "aider").unlink()
            status, data = request_json(base_url + "/api/install", {
                "agents": ["opencode", "kilo-cli", "aider", "openclaw"],
                "provider": "ppio",
                "api_key": "sk-test",
                "model": "gpt-test",
                "configure": True,
                "install_agent": True,
                "skip_test": True,
            })
            assert status == 200
            assert "opencode-ai" in npm_log.read_text()
            assert "@kilocode/cli" in npm_log.read_text()
            uv_calls = uv_log.read_text()
            assert "tool install" in uv_calls
            assert "--no-python-downloads" in uv_calls
            assert "aider-chat==0.86.2" in uv_calls
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()

    print("gui smoke tests passed")


if __name__ == "__main__":
    main()
