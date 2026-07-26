from __future__ import annotations

import argparse
import json
import mimetypes
import os
import re
import secrets
import socketserver
import sys
import webbrowser
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import quote, unquote, urlsplit

from .catalog import agent_catalog, resource_root
from .errors import OneAgentError
from .installer import InstallOptions, Runtime, install_many, status_payload
from .providers import chat_probe, list_models, provider_home


MAX_BODY_BYTES = 65536
ALLOWED_MIME_PREFIXES = ("text/", "image/", "font/")
ALLOWED_MIME_TYPES = {
    "application/javascript",
    "application/json",
    "application/manifest+json",
}
CSP = (
    "default-src 'self'; "
    "script-src 'self'; "
    "style-src 'self'; "
    "img-src 'self' data:; "
    "font-src 'self'; "
    "connect-src 'self'; "
    "object-src 'none'; "
    "base-uri 'none'; "
    "frame-ancestors 'none'; "
    "form-action 'self'"
)


def static_root() -> Path:
    return resource_root() / "frontend" / "dist"


def json_response(handler: BaseHTTPRequestHandler, status: int, payload: dict[str, object]) -> None:
    data = json.dumps(payload, ensure_ascii=False).encode()
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(data)))
    handler.send_header("Cache-Control", "no-store")
    handler.send_header("X-Content-Type-Options", "nosniff")
    handler.send_header("Content-Security-Policy", CSP)
    handler.end_headers()
    handler.wfile.write(data)


def error_response(handler: BaseHTTPRequestHandler, exc: Exception) -> None:
    if isinstance(exc, OneAgentError):
        json_response(handler, exc.status, exc.to_dict())
        return
    error = OneAgentError("INTERNAL_ERROR", "Unexpected OneAgent server failure", status=500)
    if os.environ.get("ONEAGENT_DEBUG") == "1":
        error.message = str(exc)
    json_response(handler, error.status, error.to_dict())


def read_json(handler: BaseHTTPRequestHandler) -> dict[str, object]:
    try:
        length = int(handler.headers.get("Content-Length", "0"))
    except ValueError as exc:
        raise OneAgentError("INVALID_REQUEST", "Invalid Content-Length") from exc
    if length < 0 or length > MAX_BODY_BYTES:
        raise OneAgentError("INVALID_REQUEST", "Request body is too large")
    if length == 0:
        return {}
    try:
        value = json.loads(handler.rfile.read(length).decode())
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise OneAgentError("INVALID_REQUEST", "Request body must be valid JSON") from exc
    if not isinstance(value, dict):
        raise OneAgentError("INVALID_REQUEST", "Request body must contain a JSON object")
    return value


def _allowed_origins(handler: BaseHTTPRequestHandler) -> set[str]:
    port = handler.server.server_port
    return {f"http://127.0.0.1:{port}", f"http://localhost:{port}"}


def assert_session(handler: BaseHTTPRequestHandler) -> None:
    origin = handler.headers.get("Origin")
    if not origin or origin not in _allowed_origins(handler):
        raise OneAgentError("INVALID_ORIGIN", "Invalid or missing Origin", status=403)
    cookie = SimpleCookie()
    cookie.load(handler.headers.get("Cookie", ""))
    supplied = cookie.get("oneagent_session")
    expected = getattr(handler.server, "session_token", "")
    if not supplied or not secrets.compare_digest(supplied.value, expected):
        raise OneAgentError("INVALID_ORIGIN", "Missing or invalid OneAgent session", status=403)


def _safe_static_file(request_path: str) -> Path:
    root = static_root().resolve()
    raw_path = unquote(urlsplit(request_path).path)
    if any(part == ".." for part in Path(raw_path).parts) or "\x00" in raw_path:
        raise OneAgentError("INVALID_REQUEST", "Invalid static path", status=404)
    relative = raw_path.lstrip("/") or "index.html"
    candidate = (root / relative).resolve()
    try:
        candidate.relative_to(root)
    except ValueError as exc:
        raise OneAgentError("INVALID_REQUEST", "Invalid static path", status=404) from exc
    if not candidate.is_file():
        raise OneAgentError("INVALID_REQUEST", "Static file not found", status=404)
    return candidate


def _mime_type(path: Path) -> str:
    mime, _ = mimetypes.guess_type(path.name)
    value = mime or "application/octet-stream"
    if not value.startswith(ALLOWED_MIME_PREFIXES) and value not in ALLOWED_MIME_TYPES:
        raise OneAgentError("INVALID_REQUEST", "Static file type is not allowed", status=404)
    return value


def static_response(handler: BaseHTTPRequestHandler, request_path: str) -> None:
    try:
        path = _safe_static_file(request_path)
    except OneAgentError as exc:
        if urlsplit(request_path).path in {"/", "/index.html"}:
            body = (
                "<!doctype html><meta charset='utf-8'><title>OneAgent</title>"
                "<p>OneAgent frontend build is missing. Run the frontend build before starting the GUI.</p>"
            ).encode()
            handler.send_response(503)
            handler.send_header("Content-Type", "text/html; charset=utf-8")
            handler.send_header("Content-Length", str(len(body)))
            handler.send_header("Cache-Control", "no-store")
            handler.send_header("Content-Security-Policy", CSP)
            handler.send_header(
                "Set-Cookie",
                f"oneagent_session={handler.server.session_token}; HttpOnly; SameSite=Strict; Path=/api",
            )
            handler.end_headers()
            handler.wfile.write(body)
            return
        raise exc
    mime_type = _mime_type(path)
    body = path.read_bytes()
    handler.send_response(200)
    handler.send_header("Content-Type", mime_type)
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("X-Content-Type-Options", "nosniff")
    handler.send_header("Content-Security-Policy", CSP)
    if path.name == "index.html":
        handler.send_header("Cache-Control", "no-store")
        handler.send_header(
            "Set-Cookie",
            f"oneagent_session={handler.server.session_token}; HttpOnly; SameSite=Strict; Path=/api",
        )
    elif path.parent.name == "assets" and re.search(r"[-.][A-Za-z0-9_-]{8,}[-.]", path.name):
        handler.send_header("Cache-Control", "public, max-age=31536000, immutable")
    else:
        handler.send_header("Cache-Control", "no-cache")
    handler.end_headers()
    handler.wfile.write(body)


def _timeout() -> float:
    try:
        return float(os.environ.get("ONEAGENT_HTTP_TIMEOUT", "10"))
    except ValueError:
        return 10


def _runtime() -> Runtime:
    return Runtime.create()


def _string_field(payload: dict[str, object], name: str, default: str = "") -> str:
    value = payload.get(name, default)
    if not isinstance(value, str):
        raise OneAgentError("INVALID_REQUEST", f"{name} must be a string")
    return value


def _bool_field(payload: dict[str, object], name: str, default: bool = False) -> bool:
    value = payload.get(name, default)
    if not isinstance(value, bool):
        raise OneAgentError("INVALID_REQUEST", f"{name} must be a boolean")
    return value


def _agents_field(payload: dict[str, object]) -> list[str]:
    if "agents" in payload:
        agents = payload["agents"]
    else:
        agent = payload.get("agent", "codex")
        agents = [agent]
    if not isinstance(agents, list) or not agents or not all(isinstance(item, str) and item for item in agents):
        raise OneAgentError("INVALID_REQUEST", "agents must be a non-empty array of Agent IDs")
    unknown = [agent for agent in agents if agent not in agent_catalog()]
    if unknown:
        raise OneAgentError("INVALID_REQUEST", f"Unknown Agent: {unknown[0]}")
    return agents


def _optional_agents_field(payload: dict[str, object], name: str) -> list[str] | None:
    if name not in payload or payload[name] is None:
        return None
    value = payload[name]
    if not isinstance(value, list) or not value or not all(isinstance(item, str) and item for item in value):
        raise OneAgentError("INVALID_REQUEST", f"{name} must be a non-empty array of Agent IDs")
    unknown = [agent for agent in value if agent not in agent_catalog()]
    if unknown:
        raise OneAgentError("INVALID_REQUEST", f"Unknown Agent: {unknown[0]}")
    return value


def _timeout_field(payload: dict[str, object]) -> int:
    value = payload.get("timeout", 180)
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0 or value > 3600:
        raise OneAgentError("INVALID_REQUEST", "timeout must be an integer between 1 and 3600")
    return value


def probe_payload(payload: dict[str, object]) -> dict[str, object]:
    return chat_probe(
        provider=_string_field(payload, "provider", "ppio"),
        custom_base=_string_field(payload, "api_base_url"),
        api_key=_string_field(payload, "api_key"),
        model=_string_field(payload, "model", "gpt-4.1") or "gpt-4.1",
        timeout=_timeout(),
    )


def models_payload(payload: dict[str, object]) -> dict[str, object]:
    return list_models(
        provider=_string_field(payload, "provider", "ppio"),
        custom_base=_string_field(payload, "api_base_url"),
        api_key=_string_field(payload, "api_key"),
        timeout=_timeout(),
    )


def install_payload(payload: dict[str, object]) -> dict[str, object]:
    agents = _agents_field(payload)
    configure = _bool_field(payload, "configure", True)
    return install_many(
        InstallOptions(
            agents=agents,
            profile_agents=_optional_agents_field(payload, "profile_agents"),
            provider=_string_field(payload, "provider", "ppio"),
            api_base_url=_string_field(payload, "api_base_url"),
            api_key=_string_field(payload, "api_key"),
            model=_string_field(payload, "model", "gpt-4.1") or "gpt-4.1",
            configure=configure,
            install_agent=_bool_field(payload, "install_agent"),
            # The GUI's existing-account path still completes the selected
            # Agent workflow and records a non-secret environment profile.
            # check_agent_only is reserved for the explicit CLI flag.
            check_agent_only=False,
            skip_test=_bool_field(payload, "skip_test"),
            locked_version=_bool_field(payload, "locked_version"),
            latest=_bool_field(payload, "latest"),
            channel="gui",
            timeout=_timeout_field(payload),
        ),
        _runtime(),
    )


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args) -> None:
        return

    def do_GET(self) -> None:
        try:
            path = urlsplit(self.path).path
            if path == "/api/status":
                json_response(self, 200, status_payload(_runtime()))
                return
            if path == "/favicon.ico" and not (static_root() / "favicon.ico").exists():
                self.send_response(204)
                self.end_headers()
                return
            static_response(self, self.path)
        except Exception as exc:
            error_response(self, exc)

    def do_POST(self) -> None:
        try:
            assert_session(self)
            payload = read_json(self)
            if self.path == "/api/probe":
                json_response(self, 200, probe_payload(payload))
            elif self.path == "/api/models":
                json_response(self, 200, models_payload(payload))
            elif self.path == "/api/install":
                json_response(self, 200, install_payload(payload))
            elif self.path == "/api/open-register":
                provider = _string_field(payload, "provider", "ppio")
                url = provider_home(provider)
                agents = _agents_field(payload)
                query = f"?agent={quote(','.join(agents))}&provider={quote(provider)}&channel=gui"
                target = url + query
                if os.environ.get("ONEAGENT_DISABLE_BROWSER") != "1":
                    webbrowser.open(target)
                json_response(self, 200, {"ok": True, "url": target, "message": f"Opened {url}"})
            else:
                raise OneAgentError("INVALID_REQUEST", "API route not found", status=404)
        except Exception as exc:
            error_response(self, exc)


class OneAgentHTTPServer(HTTPServer):
    session_token: str

    def server_bind(self) -> None:
        # http.server's server_bind() resolves the bind address through
        # socket.getfqdn(), a reverse DNS lookup that blocks until it times out
        # on hosts with no reverse resolver -- offline machines, locked-down
        # networks and CI runners, which is exactly where OneAgent runs. It only
        # populates self.server_name, which OneAgent never reads, so bind
        # directly and keep GUI startup instant.
        socketserver.TCPServer.server_bind(self)
        host, port = self.server_address[:2]
        self.server_name = host
        self.server_port = port


def create_server(host: str, port: int) -> OneAgentHTTPServer:
    if host != "127.0.0.1":
        raise OneAgentError("INVALID_REQUEST", "Refusing to bind outside 127.0.0.1")
    server = OneAgentHTTPServer((host, port), Handler)
    server.session_token = secrets.token_urlsafe(32)
    return server


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description="OneAgent local GUI")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--no-open", action="store_true")
    args = parser.parse_args(argv)
    try:
        server = create_server(args.host, args.port)
    except OneAgentError as exc:
        raise SystemExit(exc.message) from exc
    url = f"http://{args.host}:{server.server_port}/"
    print(url, flush=True)
    if not args.no_open:
        webbrowser.open(url)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
