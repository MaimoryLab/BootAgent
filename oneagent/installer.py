from __future__ import annotations

import getpass
import json
import os
import re
import shlex
import shutil
import subprocess
import tempfile
import tomllib
from dataclasses import dataclass, field, replace
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Iterable
from urllib.parse import urlparse

from .catalog import (
    AGENT_GROUPS,
    OFFICIAL_NPM_REGISTRY,
    PACKAGE_MIRRORS,
    PROVIDERS,
    agent_catalog,
    current_platform,
    fallback_probe_model,
    public_catalog,
    public_mirrors,
    public_providers,
    resolve_home,
)
from .errors import EXIT_CODES, OneAgentError
from .providers import (
    agent_protocol,
    list_models,
    openai_base_url,
    protocol_label,
    protocol_probe,
    provider_base,
    provider_config_base,
    resolve_probe_model,
)


Runner = Callable[..., subprocess.CompletedProcess[str]]


@dataclass
class InstallOptions:
    agents: list[str]
    profile_agents: list[str] | None = None
    provider: str = "ppio"
    api_base_url: str = ""
    api_key: str = ""
    model: str = ""
    configure: bool = True
    install_agent: bool = False
    check_agent_only: bool = False
    skip_test: bool = False
    locked_version: bool = False
    latest: bool = False
    channel: str = "cli"
    home: Path | None = None
    os_id: str | None = None
    timeout: int = 180
    # Empty means the official registry. A mirror is always an explicit choice:
    # switching automatically on a network error would leave the user unable to
    # tell where a package came from.
    registry: str = ""


@dataclass
class Runtime:
    home: Path
    os_id: str
    runner: Runner = subprocess.run
    which: Callable[[str], str | None] = shutil.which
    env: dict[str, str] = field(default_factory=lambda: os.environ.copy())

    @classmethod
    def create(
        cls,
        *,
        home: Path | None = None,
        os_id: str | None = None,
        runner: Runner = subprocess.run,
        which: Callable[[str], str | None] = shutil.which,
        env: dict[str, str] | None = None,
    ) -> "Runtime":
        platform_id = os_id or current_platform()["os"]
        values = env.copy() if env is not None else os.environ.copy()
        return cls(home or resolve_home(values, platform_id), platform_id, runner, which, values)


def redact(text: str, secrets: Iterable[str]) -> str:
    output = text
    for secret in secrets:
        if secret:
            output = output.replace(secret, "[redacted]")
    return output


def _timestamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S")


def backup_file(path: Path) -> Path | None:
    if not path.exists():
        return None
    candidate = path.with_name(f"{path.name}.backup-{_timestamp()}")
    counter = 1
    while candidate.exists():
        candidate = path.with_name(f"{path.name}.backup-{_timestamp()}-{counter}")
        counter += 1
    shutil.copy2(path, candidate)
    return candidate


def _run_acl(runtime: Runtime, path: Path, *, directory: bool) -> None:
    icacls = runtime.which("icacls")
    if not icacls:
        raise OneAgentError("CONFIG_WRITE_FAILED", "Windows ACL tool icacls was not found")
    username = runtime.env.get("USERNAME") or getpass.getuser()
    current_grant = f"{username}:(OI)(CI)F" if directory else f"{username}:F"
    system_grant = "*S-1-5-18:(OI)(CI)F" if directory else "*S-1-5-18:F"
    commands = [
        [icacls, str(path), "/reset"],
        [icacls, str(path), "/inheritance:r", "/grant:r", current_grant, system_grant],
    ]
    for args in commands:
        result = runtime.runner(
            args,
            text=True,
            capture_output=True,
            timeout=30,
            env=runtime.env,
        )
        if result.returncode != 0:
            raise OneAgentError("CONFIG_WRITE_FAILED", f"Failed to secure Windows ACL for {path}")


def secure_path(runtime: Runtime, path: Path, *, directory: bool) -> None:
    if runtime.os_id == "windows":
        _run_acl(runtime, path, directory=directory)
    else:
        path.chmod(0o700 if directory else 0o600)


def ensure_private_dir(runtime: Runtime, path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)
    secure_path(runtime, path, directory=True)


def atomic_write(runtime: Runtime, path: Path, content: str, *, secret: bool = False) -> Path | None:
    backup: Path | None = None
    temporary: Path | None = None
    try:
        ensure_private_dir(runtime, path.parent)
        backup = backup_file(path)
        if backup and secret:
            try:
                secure_path(runtime, backup, directory=False)
            except OneAgentError:
                try:
                    backup.unlink()
                except OSError as exc:
                    raise OneAgentError(
                        "CONFIG_WRITE_FAILED",
                        f"Cannot remove insecure secret backup {backup}: {exc}",
                    ) from exc
                raise
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as handle:
            handle.write(content)
            temporary = Path(handle.name)
        # Secure the temporary inode before publishing it. On Windows this
        # prevents an ACL failure from replacing the user's existing file.
        secure_path(runtime, temporary, directory=False)
        os.replace(temporary, path)
    except OneAgentError:
        raise
    except OSError as exc:
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Cannot write {path}: {exc}") from exc
    finally:
        if temporary and temporary.exists():
            try:
                temporary.unlink()
            except OSError as exc:
                kind = "secret " if secret else ""
                raise OneAgentError(
                    "CONFIG_WRITE_FAILED",
                    f"Cannot remove temporary {kind}file {temporary}: {exc}",
                ) from exc
    return backup


def _powershell_quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


_ID_PATTERN = re.compile(r"^[a-z0-9][a-z0-9_-]{0,63}$")


def validate_agent_id(agent_id: str) -> str:
    """Reject an Agent ID that could not be a single path segment.

    Validated here, where the path is built, so every caller inherits the
    check: the ID names a file that holds a plaintext credential, and a
    traversal would place that key outside the private directory.
    """
    if not isinstance(agent_id, str) or not _ID_PATTERN.fullmatch(agent_id):
        raise OneAgentError("INVALID_REQUEST", f"Invalid Agent ID: {agent_id!r}")
    return agent_id


def _config_path(runtime: Runtime, meta: dict[str, Any]) -> Path | None:
    raw = meta.get("windows_config_path") if runtime.os_id == "windows" else meta.get("config_path")
    if not raw:
        raw = meta.get("config_path")
    return runtime.home / raw if raw else None


def _env_path(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / ("env.ps1" if runtime.os_id == "windows" else "env")


def agent_env_var(agent_id: str, suffix: str = "API_KEY") -> str:
    """Environment variable an Agent reads its credential from.

    Each Agent gets its own name so three Agents that all speak
    OpenAI-compatible can point at different providers in the same shell. A
    single shared ONEAGENT_API_KEY made that impossible: whichever env file
    was sourced last won.
    """
    stem = re.sub(r"[^A-Za-z0-9]+", "_", agent_id).strip("_").upper()
    return f"ONEAGENT_{suffix}_{stem}"


def needs_env_file(meta: dict[str, Any]) -> bool:
    """Whether this Agent's credential travels through an env file.

    Read from the manifest rather than a set of ids written here: the set had
    left Claude Code out while it was the one Agent that could not authenticate
    without one, and nothing in the code said which Agents were meant to be in
    it or why.
    """
    return str(meta.get("credential_delivery", "")) in {"oneagent_env", "native_env"}


def agent_env_path(runtime: Runtime, agent_id: str) -> Path:
    name = validate_agent_id(agent_id)
    suffix = "env.ps1" if runtime.os_id == "windows" else "env"
    return runtime.home / ".oneagent" / "agents" / f"{name}.{suffix}"


def _env_assignments(runtime: Runtime, values: dict[str, str]) -> str:
    if runtime.os_id == "windows":
        return "".join(f"$env:{name} = {_powershell_quote(value)}\n" for name, value in values.items())
    return "".join(f"export {name}={shlex.quote(value)}\n" for name, value in values.items())


def _shared_env_content(runtime: Runtime, api_key: str, base_url: str) -> str:
    return _env_assignments(
        runtime, {"ONEAGENT_API_KEY": api_key, "ONEAGENT_API_BASE_URL": base_url}
    )


def write_shared_env(runtime: Runtime, api_key: str, base_url: str) -> Path:
    """Legacy shared credential file.

    Kept because configs written by earlier versions still reference
    ONEAGENT_API_KEY; dropping it would break those Agents on upgrade. New
    configs read their own variable via write_agent_env.
    """
    path = _env_path(runtime)
    atomic_write(runtime, path, _shared_env_content(runtime, api_key, base_url), secret=True)
    return path


def write_agent_env(
    runtime: Runtime,
    agent_id: str,
    api_key: str,
    base_url: str,
    *,
    meta: dict[str, Any] | None = None,
    model: str = "",
) -> Path:
    """Write the env file an Agent's credential reaches it through.

    Two shapes, declared per Agent as credential_delivery in the lock manifest:

    oneagent_env -- the config file this adapter wrote references ONEAGENT_*
    names (Codex's env_key, OpenCode's {env:...}), so those are what the file
    has to define.

    native_env -- the Agent only reads variable names it defines itself. Claude
    Code is the case that proves this matters: it ignores the credential in its
    own settings.json and answers "Not logged in" until ANTHROPIC_AUTH_TOKEN is
    in the environment. Writing only ONEAGENT_* for it produced an Agent that
    OneAgent reported as configured and that could not authenticate.
    """
    path = agent_env_path(runtime, agent_id)
    values = {
        agent_env_var(agent_id): api_key,
        agent_env_var(agent_id, "API_BASE_URL"): base_url,
        # Also define the shared names so a shell that sources only this
        # file still satisfies a config written before per-Agent variables.
        "ONEAGENT_API_KEY": api_key,
        "ONEAGENT_API_BASE_URL": base_url,
    }
    native = (meta or {}).get("env_vars") or {}
    if native:
        # The Agent's own names, so sourcing this file is enough to start it.
        for field, value in (("api_key", api_key), ("base_url", base_url), ("model", model)):
            name = native.get(field)
            if name and value:
                values[str(name)] = value
        small_fast = native.get("small_fast_model")
        if small_fast and model:
            values[str(small_fast)] = model
    atomic_write(runtime, path, _env_assignments(runtime, values), secret=True)
    return path


def write_codex_config(runtime: Runtime, meta: dict[str, Any], provider_name: str, base_url: str, model: str) -> Path:
    path = _config_path(runtime, meta)
    assert path is not None
    managed = (
        'model_provider = "oneagent"\n'
        f'model = {json.dumps(model)}\n\n'
        "[model_providers.oneagent]\n"
        f"name = {json.dumps(provider_name)}\n"
        f"base_url = {json.dumps(base_url)}\n"
        f'env_key = {json.dumps(agent_env_var("codex"))}\n'
        'wire_api = "responses"\n'
    )
    existing = ""
    if path.exists():
        try:
            existing = path.read_text(encoding="utf-8")
        except OSError as exc:
            raise OneAgentError("CONFIG_WRITE_FAILED", f"Cannot read existing TOML configuration {path}: {exc}") from exc
    content = _merge_codex_toml(existing, managed, path)
    atomic_write(runtime, path, content)
    return path


def _merge_codex_toml(existing: str, managed: str, path: Path) -> str:
    if not existing.strip():
        return managed
    try:
        parsed = tomllib.loads(existing)
    except tomllib.TOMLDecodeError as exc:
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Existing TOML configuration is invalid: {path}: {exc}") from exc

    top_level_kept: list[str] = []
    table_kept: list[str] = []
    removed_top_level: set[str] = set()
    managed_section_found = False
    in_table = False
    skip_managed_section = False
    top_level_key = re.compile(r"^\s*(model_provider|model)\s*=")
    managed_section = re.compile(r"^\[model_providers\.oneagent(?:\..+)?\]$")

    for line in existing.splitlines():
        stripped = line.strip()
        if stripped.startswith("["):
            header = re.sub(r"\s+", "", stripped.split("#", 1)[0])
            in_table = True
            skip_managed_section = bool(managed_section.fullmatch(header))
            if skip_managed_section:
                managed_section_found = True
                continue
        if skip_managed_section:
            continue
        if not in_table:
            match = top_level_key.match(line)
            if match:
                removed_top_level.add(match.group(1))
                continue
        (table_kept if in_table else top_level_kept).append(line)

    provider_tables = parsed.get("model_providers")
    if isinstance(provider_tables, dict) and "oneagent" in provider_tables and not managed_section_found:
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Unsupported OneAgent TOML table syntax in {path}")
    for key in ("model_provider", "model"):
        if key in parsed and key not in removed_top_level:
            raise OneAgentError("CONFIG_WRITE_FAILED", f"Unsupported TOML key syntax for {key} in {path}")

    sections = ["\n".join(top_level_kept).strip(), managed.rstrip(), "\n".join(table_kept).strip()]
    merged = "\n\n".join(section for section in sections if section) + "\n"
    try:
        tomllib.loads(merged)
    except tomllib.TOMLDecodeError as exc:
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Cannot merge TOML configuration {path}: {exc}") from exc
    return merged


def _load_json(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {}
    text = path.read_text(encoding="utf-8")
    if not text.strip():
        return {}
    try:
        value = json.loads(text)
    except json.JSONDecodeError as exc:
        # OpenCode and Kilo use .jsonc, where comments are valid. OneAgent
        # rewrites these files with json.dumps, which would drop the comments,
        # so refusing is right -- but reporting a valid JSONC file as invalid
        # JSON leaves the user with no idea what to change.
        if path.suffix == ".jsonc" and re.search(r"(?:^|\s)(?://|/\*)", text):
            raise OneAgentError(
                "CONFIG_WRITE_FAILED",
                f"{path} contains JSONC comments, which OneAgent cannot preserve when it rewrites the file. "
                "Remove the comments, or configure this Agent manually and leave the file untouched.",
            ) from exc
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Existing JSON configuration is invalid: {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Existing JSON configuration must contain an object: {path}")
    return value


def write_claude_config(runtime: Runtime, meta: dict[str, Any], base_url: str, api_key: str, model: str) -> Path:
    path = _config_path(runtime, meta)
    assert path is not None
    data = _load_json(path)
    env = data.setdefault("env", {})
    if not isinstance(env, dict):
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Existing Claude Code env configuration must contain an object: {path}")
    env["ANTHROPIC_BASE_URL"] = base_url
    env["ANTHROPIC_AUTH_TOKEN"] = api_key
    env["ANTHROPIC_MODEL"] = model
    env["ANTHROPIC_SMALL_FAST_MODEL"] = model
    atomic_write(runtime, path, json.dumps(data, ensure_ascii=False, indent=2) + "\n", secret=True)
    return path


def write_openai_compatible_config(
    runtime: Runtime,
    meta: dict[str, Any],
    provider_name: str,
    base_url: str,
    model: str,
    schema: str,
    agent_id: str = "opencode",
) -> Path:
    path = _config_path(runtime, meta)
    assert path is not None
    data = _load_json(path)
    data["$schema"] = schema
    providers = data.setdefault("provider", {})
    if not isinstance(providers, dict):
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Existing provider configuration must contain an object: {path}")
    providers["oneagent"] = {
        "npm": "@ai-sdk/openai-compatible",
        "name": provider_name,
        "options": {
            "baseURL": openai_base_url(base_url),
            "apiKey": "{env:" + agent_env_var(agent_id) + "}",
        },
        "models": {model: {"name": model}},
    }
    data["model"] = f"oneagent/{model}"
    atomic_write(runtime, path, json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    return path


def write_aider_config(runtime: Runtime, meta: dict[str, Any], base_url: str, api_key: str) -> Path:
    path = _config_path(runtime, meta)
    assert path is not None
    api_base = openai_base_url(base_url)
    if runtime.os_id == "windows":
        content = (
            f"$env:OPENAI_API_BASE = {_powershell_quote(api_base)}\n"
            f"$env:OPENAI_API_KEY = {_powershell_quote(api_key)}\n"
        )
    else:
        content = f"export OPENAI_API_BASE={shlex.quote(api_base)}\nexport OPENAI_API_KEY={shlex.quote(api_key)}\n"
    atomic_write(runtime, path, content, secret=True)
    return path


def _version_from_output(text: str) -> str | None:
    match = re.search(r"(?<!\d)(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)", text)
    return match.group(1) if match else None


def installed_version(runtime: Runtime, meta: dict[str, Any]) -> str | None:
    command = meta.get("command")
    executable = runtime.which(command) if command else None
    if not executable:
        return None
    try:
        result = runtime.runner(
            [executable, *meta.get("version_args", ["--version"])],
            text=True,
            capture_output=True,
            timeout=30,
            env=runtime.env,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None
    return _version_from_output((result.stdout or "") + "\n" + (result.stderr or ""))


def _python_312_for_uv(runtime: Runtime) -> str:
    direct = runtime.which("python3.12")
    if direct:
        return direct
    for command in ("python3", "python"):
        executable = runtime.which(command)
        if not executable:
            continue
        try:
            result = runtime.runner(
                [executable, "--version"],
                text=True,
                capture_output=True,
                timeout=30,
                env=runtime.env,
            )
        except (OSError, subprocess.TimeoutExpired):
            continue
        version = _version_from_output((result.stdout or "") + "\n" + (result.stderr or ""))
        if result.returncode == 0 and version and version.startswith("3.12."):
            return executable
    if runtime.os_id == "windows":
        launcher = runtime.which("py")
        if launcher:
            try:
                result = runtime.runner(
                    [launcher, "-3.12", "--version"],
                    text=True,
                    capture_output=True,
                    timeout=30,
                    env=runtime.env,
                )
            except (OSError, subprocess.TimeoutExpired):
                result = None
            if result and result.returncode == 0:
                return "3.12"
    raise OneAgentError(
        "PREREQUISITE_MISSING",
        "An existing Python 3.12 installation is required for Aider; OneAgent will not download Python automatically",
    )


def _installer_failure_detail(runtime: Runtime, result: subprocess.CompletedProcess[str]) -> str:
    text = (result.stderr or "") + "\n" + (result.stdout or "")
    secrets = [
        value
        for key, value in runtime.env.items()
        if value and any(marker in key.upper() for marker in ("KEY", "TOKEN", "SECRET", "PASSWORD"))
    ]
    text = redact(text, secrets)
    text = re.sub(r"\x1b\[[0-9;]*m", "", text)
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    return " | ".join(lines[-3:])[:600]


def _require_prerequisites(runtime: Runtime, agent_id: str, meta: dict[str, Any]) -> None:
    package = meta.get("package") or {}
    manager = package.get("manager")
    if manager == "npm" and not runtime.which("npm"):
        raise OneAgentError("PREREQUISITE_MISSING", f"npm is required to install {meta['name']}")
    if manager == "uv":
        if not runtime.which("uv"):
            raise OneAgentError("PREREQUISITE_MISSING", "uv is required to install Aider")
        _python_312_for_uv(runtime)
    if runtime.os_id == "windows" and agent_id == "claude-code" and not runtime.which("git"):
        raise OneAgentError("PREREQUISITE_MISSING", "Git for Windows / Git Bash is required for Claude Code")


def resolve_registry(value: str) -> str:
    """Resolve a mirror id or explicit URL to a registry address.

    HTTPS only, and credentials are refused: a registry URL ends up in the
    installer environment and in the install log, so a token embedded in it
    would leak into both. validate_base_url is not reused because it permits
    http:// -- acceptable for a Provider endpoint the user names, not for the
    address a package is fetched from.
    """
    if not value:
        return OFFICIAL_NPM_REGISTRY
    known = PACKAGE_MIRRORS.get(value)
    if known:
        return str(known["registry"])
    if any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise OneAgentError("INVALID_REQUEST", "Registry URL contains control characters")
    parsed = urlparse(value)
    if parsed.scheme != "https" or not parsed.netloc:
        raise OneAgentError("INVALID_REQUEST", "Registry URL must start with https://")
    if parsed.username or parsed.password:
        raise OneAgentError("INVALID_REQUEST", "Registry URL must not contain credentials")
    return value if value.endswith("/") else f"{value}/"


def verify_npm_integrity(
    runtime: Runtime, npm: str, spec: str, expected: str, registry: str, timeout: int
) -> None:
    """Check the registry's integrity for a pinned spec against the manifest.

    The manifest records a sha512 for every npm package but nothing ever read
    it, so the version was pinned and the bytes were not. That gap only becomes
    exploitable once a mirror is allowed: npm verifies a download against the
    integrity the registry itself served, which secures the transfer but takes
    the registry's word for what the package is. Comparing that value with the
    manifest closes the loop -- npm proves the bytes match what the registry
    declared, and this proves the declaration matches the official release.
    """
    if not expected:
        return
    try:
        result = runtime.runner(
            [npm, "view", spec, "dist.integrity", f"--registry={registry}"],
            text=True,
            capture_output=True,
            timeout=timeout,
            env=runtime.env,
        )
    except subprocess.TimeoutExpired as exc:
        raise OneAgentError(
            "AGENT_INSTALL_FAILED", f"Timed out reading the checksum for {spec}", retryable=True
        ) from exc
    except OSError as exc:
        raise OneAgentError("AGENT_INSTALL_FAILED", f"Cannot read the checksum for {spec}: {exc}") from exc
    if result.returncode != 0:
        raise OneAgentError(
            "AGENT_INSTALL_FAILED",
            f"{spec} is not available on {registry}",
            retryable=True,
        )
    reported = (result.stdout or "").strip()
    if reported != expected:
        # Fail closed and name both values: a mismatch means the registry is
        # serving something other than the locked release, which is exactly the
        # case a mirror has to be held to.
        raise OneAgentError(
            "AGENT_INSTALL_FAILED",
            f"Checksum mismatch for {spec} on {registry}: manifest expects {expected}, registry reports {reported or '(none)'}",
        )


def install_locked_agent(
    runtime: Runtime,
    agent_id: str,
    meta: dict[str, Any],
    *,
    enforce_locked: bool,
    latest: bool,
    timeout: int,
    registry: str = "",
) -> dict[str, object]:
    command = meta.get("command")
    executable = runtime.which(command) if command else None
    package = meta.get("package") or {}
    locked = package.get("version")
    current = installed_version(runtime, meta) if executable else None
    if executable and not enforce_locked:
        return {"installed": False, "version": current, "lockedVersion": locked}
    if executable and enforce_locked and current == locked:
        return {"installed": False, "version": current, "lockedVersion": locked}
    _require_prerequisites(runtime, agent_id, meta)
    manager = package.get("manager")
    package_name = package.get("name")
    resolved_registry = resolve_registry(registry)
    env = runtime.env
    if manager == "npm":
        npm = runtime.which("npm")
        assert npm is not None
        spec = package_name if latest else f"{package_name}@{locked}"
        if resolved_registry != OFFICIAL_NPM_REGISTRY:
            # npm reads the registry from its environment, so no argument has to
            # be threaded through the install command itself.
            env = {**env, "npm_config_registry": resolved_registry}
        if not latest:
            # Only meaningful for a pinned spec: the manifest's checksum
            # describes the locked version, not whatever floats at the tag.
            verify_npm_integrity(
                runtime, npm, spec, str(package.get("integrity", "")), resolved_registry, timeout
            )
        args = [npm, "install", "-g", spec]
    elif manager == "uv":
        uv = runtime.which("uv")
        assert uv is not None
        python = _python_312_for_uv(runtime)
        spec = package_name if latest else f"{package_name}=={locked}"
        args = [
            uv,
            "tool",
            "install",
            "--force",
            "--python",
            python,
            "--no-python-downloads",
            spec,
        ]
    else:
        raise OneAgentError("PREREQUISITE_MISSING", f"No allowlisted package manager for {meta['name']}")
    try:
        result = runtime.runner(args, text=True, capture_output=True, timeout=timeout, env=env)
    except subprocess.TimeoutExpired as exc:
        raise OneAgentError("TIMEOUT", f"Installing {meta['name']} timed out", retryable=True) from exc
    except OSError as exc:
        raise OneAgentError("AGENT_INSTALL_FAILED", f"Cannot start installer for {meta['name']}: {exc}") from exc
    if result.returncode != 0:
        detail = _installer_failure_detail(runtime, result)
        message = f"Installing {meta['name']} failed with exit code {result.returncode}"
        if detail:
            message += f": {detail}"
        raise OneAgentError(
            "AGENT_INSTALL_FAILED",
            message,
            retryable=True,
        )
    return {
        "installed": True,
        "version": locked if not latest else None,
        "lockedVersion": locked,
        "registry": resolved_registry,
    }


def _next_step(runtime: Runtime, agent_id: str, model: str) -> str:
    """The command that starts this Agent against what was just written.

    Derived from the manifest -- the command name and whether a credential
    arrives through an env file are both declared there. Spelling the commands
    out here meant Claude Code's line said plain "claude" while its credential
    sat in a file nothing told the user to source.
    """
    meta = agent_catalog().get(agent_id)
    if not meta or meta.get("config_mode") != "auto":
        return ""
    # Every auto Agent has a command; test_release_policy holds the manifest to
    # that, so this does not need a second guard here.
    command = str(meta["command"])
    # Each Agent sources its own file, so two Agents pointing at different
    # providers do not overwrite each other's credential in one shell.
    joiner = ";" if runtime.os_id == "windows" else "&&"
    if agent_id == "aider":
        # Aider's credential is the config the adapter writes, which is itself a
        # shell script, and the model is a launch argument rather than a field.
        source = (
            '. "$HOME\\.oneagent\\aider.ps1"'
            if runtime.os_id == "windows"
            else "source ~/.oneagent/aider.env"
        )
        return f"{source} {joiner} {command} --model openai/{model}"
    if needs_env_file(meta):
        source = (
            f'. "$HOME\\.oneagent\\agents\\{agent_id}.env.ps1"'
            if runtime.os_id == "windows"
            else f"source ~/.oneagent/agents/{agent_id}.env"
        )
        return f"{source} {joiner} {command}"
    return command


def _write_agent_config(
    runtime: Runtime,
    agent_id: str,
    meta: dict[str, Any],
    provider_name: str,
    base_url: str,
    api_key: str,
    model: str,
) -> Path:
    adapter = meta.get("config_adapter")
    if adapter == "codex":
        return write_codex_config(runtime, meta, provider_name, base_url, model)
    if adapter == "claude-code":
        return write_claude_config(runtime, meta, base_url, api_key, model)
    if adapter == "opencode":
        return write_openai_compatible_config(
            runtime, meta, provider_name, base_url, model, "https://opencode.ai/config.json", agent_id
        )
    if adapter == "kilo-cli":
        return write_openai_compatible_config(
            runtime, meta, provider_name, base_url, model, "https://app.kilo.ai/config.json", agent_id
        )
    if adapter == "aider":
        return write_aider_config(runtime, meta, base_url, api_key)
    raise OneAgentError("INVALID_REQUEST", f"Unsupported auto-config Agent: {agent_id}")


def profile_path(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / "profile.json"




def profiles_dir(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / "profiles"


def profile_store_path(runtime: Runtime, profile_id: str) -> Path:
    return profiles_dir(runtime) / f"{validate_profile_id(profile_id)}.json"


def secrets_path(runtime: Runtime, profile_id: str) -> Path:
    # Validate at the point the path is built, not only at the callers: this
    # decides where a plaintext key is written, so every future caller has to
    # inherit the check rather than remember it.
    suffix = "env.ps1" if runtime.os_id == "windows" else "env"
    return runtime.home / ".oneagent" / "secrets" / f"{validate_profile_id(profile_id)}.{suffix}"


def validate_profile_id(profile_id: str) -> str:
    if not isinstance(profile_id, str) or not _ID_PATTERN.fullmatch(profile_id):
        raise OneAgentError(
            "INVALID_REQUEST",
            "Profile ID must start with a lowercase letter or digit and use only lowercase letters, digits, '-' or '_'",
        )
    return profile_id


def active_profile_id(runtime: Runtime) -> str | None:
    path = profile_path(runtime)
    if not path.exists():
        return None
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if isinstance(value, dict) and value.get("schema_version") == 2 and isinstance(value.get("active"), str):
        # Reject a pointer that is not a legal ID rather than returning it: the
        # value reaches secrets_path, so a traversal here would build a key path
        # outside the secrets directory.
        try:
            return validate_profile_id(str(value["active"]))
        except OneAgentError:
            return None
    return None


def agents_dir(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / "agents"


def agent_binding_path(runtime: Runtime, agent_id: str) -> Path:
    return agents_dir(runtime) / f"{validate_agent_id(agent_id)}.json"


def read_agent_binding(runtime: Runtime, agent_id: str) -> tuple[dict[str, Any] | None, str | None]:
    """What this Agent is currently pointed at, or why that cannot be read."""
    path = agent_binding_path(runtime, agent_id)
    if not path.exists():
        return None, None
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return None, str(exc)
    if not isinstance(value, dict):
        return None, f"Agent binding for {agent_id} is corrupt"
    if value.get("schema_version") != 1:
        return None, f"Unsupported Agent binding schema for {agent_id}"
    return value, None


def write_agent_binding(
    runtime: Runtime,
    agent_id: str,
    *,
    provider: str,
    base_url: str,
    model: str,
    profile_ref: str = "",
) -> dict[str, Any]:
    """Record an Agent's own provider and model. Never its key.

    Bindings answer "what is this Agent pointed at" without reading the
    Agent's own config file, whose shape differs per adapter. The credential
    stays in the sibling .env file so this stays safe to read and report.
    """
    existing, _ = read_agent_binding(runtime, agent_id)
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    binding = {
        "schema_version": 1,
        "agent_id": validate_agent_id(agent_id),
        "provider": provider,
        "base_url": base_url,
        "model": model,
        "profile_ref": profile_ref or (existing or {}).get("profile_ref") or "",
        "created_at": (existing or {}).get("created_at") or now,
        "updated_at": now,
    }
    ensure_private_dir(runtime, agents_dir(runtime))
    atomic_write(
        runtime,
        agent_binding_path(runtime, agent_id),
        json.dumps(binding, ensure_ascii=False, indent=2) + "\n",
    )
    return binding


def list_agent_bindings(runtime: Runtime) -> dict[str, dict[str, Any]]:
    directory = agents_dir(runtime)
    if not directory.is_dir():
        return {}
    bindings: dict[str, dict[str, Any]] = {}
    for path in sorted(directory.glob("*.json")):
        try:
            agent_id = validate_agent_id(path.stem)
        except OneAgentError:
            continue
        value, error = read_agent_binding(runtime, agent_id)
        if value and not error:
            bindings[agent_id] = value
    return bindings


def _read_stored_profile(runtime: Runtime, profile_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = profile_store_path(runtime, profile_id)
    if not path.exists():
        return None, f"Profile {profile_id} is missing"
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return None, str(exc)
    if not isinstance(value, dict):
        return None, f"Profile {profile_id} is corrupt"
    return value, None


def _write_profile_store(runtime: Runtime, profile_id: str, stored: dict[str, Any]) -> Path:
    ensure_private_dir(runtime, profiles_dir(runtime))
    path = profile_store_path(runtime, profile_id)
    atomic_write(runtime, path, json.dumps(stored, ensure_ascii=False, indent=2) + "\n")
    return path


def _write_profile_pointer(runtime: Runtime, profile_id: str) -> Path:
    pointer = {"schema_version": 2, "active": profile_id}
    path = profile_path(runtime)
    atomic_write(runtime, path, json.dumps(pointer, ensure_ascii=False, indent=2) + "\n")
    return path


def _write_profile_secret(runtime: Runtime, profile_id: str, api_key: str, base_url: str) -> None:
    if not api_key:
        return
    atomic_write(runtime, secrets_path(runtime, profile_id), _shared_env_content(runtime, api_key, base_url), secret=True)


def read_profile_secret(runtime: Runtime, profile_id: str) -> str:
    """The key stored for a profile template, or "" when none is held.

    Lets an Agent be pointed back at a saved provider without the user
    re-pasting the key. Returns the value only to the caller that is about to
    write a config with it; it is never put into a response.
    """
    path = secrets_path(runtime, profile_id)
    if not path.exists():
        return ""
    try:
        content = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise OneAgentError("CONFIG_WRITE_FAILED", f"Cannot read stored key for profile {profile_id}: {exc}") from exc
    pattern = (
        r"^\$env:ONEAGENT_API_KEY\s*=\s*'(.*)'$"
        if runtime.os_id == "windows"
        else r"^export ONEAGENT_API_KEY=(.*)$"
    )
    match = re.search(pattern, content, re.MULTILINE)
    if not match:
        return ""
    raw = match.group(1)
    if runtime.os_id == "windows":
        return raw.replace("''", "'")
    return next(iter(shlex.split(raw)), "")


def load_profile(runtime: Runtime) -> tuple[dict[str, Any] | None, str | None]:
    """The active environment profile.

    profile.json schema_version 2 is a pointer into the profiles/ store.
    Legacy schema_version 1 files are migrated in place on first read --
    atomic_write backs the original up first -- so an existing installation
    upgrades instead of failing.
    """
    path = profile_path(runtime)
    if not path.exists():
        return None, None
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return None, str(exc)
    if not isinstance(value, dict):
        return None, "Unsupported environment profile schema"
    schema = value.get("schema_version")
    if schema == 1:
        return _migrate_profile_v1(runtime, value)
    if schema != 2 or not isinstance(value.get("active"), str):
        return None, "Unsupported environment profile schema"
    # The pointer is a file on disk, so its value is untrusted input even though
    # we wrote it: anything that can edit profile.json could otherwise name a
    # path outside the store and have it read back as a profile.
    try:
        active = validate_profile_id(str(value["active"]))
    except OneAgentError as exc:
        return None, exc.message
    return _read_stored_profile(runtime, active)


def _migrate_profile_v1(runtime: Runtime, legacy: dict[str, Any]) -> tuple[dict[str, Any] | None, str | None]:
    profile_id = "default"
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    stored = {
        "schema_version": 2,
        "id": profile_id,
        "label": profile_id,
        "provider": legacy.get("provider"),
        "base_url": legacy.get("base_url"),
        "model": legacy.get("model"),
        "config_mode": legacy.get("config_mode") or "provider",
        "agent_ids": legacy.get("agent_ids") or [],
        "created_at": legacy.get("activated_at") or now,
        "activated_at": legacy.get("activated_at") or now,
    }
    try:
        _write_profile_store(runtime, profile_id, stored)
        _write_profile_pointer(runtime, profile_id)
    except OneAgentError as exc:
        return None, f"Cannot migrate legacy profile: {exc.message}"
    return stored, None


def list_profiles(runtime: Runtime) -> list[dict[str, Any]]:
    """Stored profiles in stable order, annotated with key presence.

    Profiles never carry key material; whether a matching secret file exists
    decides if the profile can be activated without re-pasting the key.
    """
    directory = profiles_dir(runtime)
    if not directory.is_dir():
        return []
    profiles: list[dict[str, Any]] = []
    for path in sorted(directory.glob("*.json")):
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if isinstance(value, dict) and isinstance(value.get("id"), str):
            profiles.append({**value, "has_key": secrets_path(runtime, str(value["id"])).exists()})
    return profiles


def save_profile(
    runtime: Runtime,
    *,
    profile_id: str,
    label: str = "",
    provider: str,
    api_base_url: str = "",
    model: str,
    agent_ids: list[str],
    api_key: str = "",
) -> dict[str, Any]:
    profile_id = validate_profile_id(profile_id)
    base_url = provider_base(provider, api_base_url)
    existing, _ = _read_stored_profile(runtime, profile_id)
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    stored = {
        "schema_version": 2,
        "id": profile_id,
        "label": label or (existing or {}).get("label") or profile_id,
        "provider": provider,
        "base_url": api_base_url or None,
        "model": model,
        "config_mode": "provider",
        "agent_ids": sorted(set(agent_ids)),
        "created_at": (existing or {}).get("created_at") or now,
        "activated_at": (existing or {}).get("activated_at"),
    }
    _write_profile_store(runtime, profile_id, stored)
    if api_key:
        _write_profile_secret(runtime, profile_id, api_key, base_url)
    return stored


def write_profile(
    runtime: Runtime,
    *,
    agents: list[str],
    configure: bool,
    provider: str,
    base_url: str,
    model: str,
    api_key: str = "",
) -> Path:
    current, _ = load_profile(runtime)
    merged_agents = set(agents)
    profile_id = "default"
    if current and isinstance(current.get("id"), str):
        profile_id = str(current["id"])
    if current and current.get("provider") == (provider if configure else "existing-account") and current.get("model") == (model if configure else None):
        merged_agents.update(current.get("agent_ids") or [])
    now = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    stored = {
        "schema_version": 2,
        "id": profile_id,
        "label": (current or {}).get("label") or profile_id,
        "provider": provider if configure else "existing-account",
        "base_url": base_url if configure else None,
        "model": model if configure else None,
        "config_mode": "provider" if configure else "existing-account",
        "agent_ids": sorted(merged_agents),
        "created_at": (current or {}).get("created_at") or now,
        "activated_at": now,
    }
    _write_profile_store(runtime, profile_id, stored)
    if configure:
        _write_profile_secret(runtime, profile_id, api_key, base_url)
    return _write_profile_pointer(runtime, profile_id)


def _sharpen_model_diagnosis(probes: dict[str, dict[str, Any]], options: InstallOptions) -> None:
    """Relabel probe failures that really mean "unknown model".

    Endpoints refuse an unknown model with the same 404/400 shapes they use
    for an unsupported protocol, so a bare probe verdict reads "model does
    not support <protocol>" and sends the user hunting a protocol mismatch.
    When model discovery succeeds and the model is absent from the list,
    rewrite the failing verdicts to name the real problem plus a few valid
    IDs. Discovery failing (or listing the model) leaves verdicts untouched:
    then "wrong model" and "unreachable endpoint" are indistinguishable, and
    blocking on a guess would lock offline users out.
    """
    failing = [verdict for verdict in probes.values() if not verdict["ok"]]
    if not failing:
        return
    listing = list_models(
        provider=options.provider,
        custom_base=options.api_base_url,
        api_key=options.api_key,
        timeout=options.timeout,
    )
    models = listing.get("models") or []
    if not listing.get("ok") or not models or options.model in models:
        return
    sample = ", ".join(models[:5])
    for verdict in failing:
        verdict["error_code"] = "MODELS_UNSUPPORTED"
        verdict["retryable"] = False
        verdict["message"] = (
            f"Model {options.model!r} was not found in the endpoint's model list; "
            f"the {protocol_label(str(verdict.get('protocol', '')))} probe refused it. "
            f"Available models include: {sample}."
        )


def install_many(options: InstallOptions, runtime: Runtime | None = None) -> dict[str, Any]:
    runtime = runtime or Runtime.create(home=options.home, os_id=options.os_id)
    catalog = agent_catalog()
    # Resolve the model once, here, rather than at each of the places that write
    # it into a config or a next-step hint. An empty model reaches this far when
    # a caller omits it entirely, and writing "" into an Agent config would
    # produce a file that looks configured but cannot answer a request.
    # Resolution prefers a model the endpoint lists right now; the catalog
    # fallback only covers discovery failure. skip_test opts out of every
    # network round trip, discovery included.
    if not options.model:
        if options.skip_test:
            resolved_model = fallback_probe_model(options.provider)
        else:
            resolved_model = resolve_probe_model(
                provider=options.provider,
                api_key=options.api_key,
                custom_base=options.api_base_url,
                timeout=options.timeout,
            )
        options = replace(options, model=resolved_model)
    if options.locked_version and options.latest:
        raise OneAgentError("INVALID_REQUEST", "locked_version and latest cannot be enabled together")
    if options.timeout <= 0:
        raise OneAgentError("INVALID_REQUEST", "timeout must be greater than zero")
    if not options.agents:
        raise OneAgentError("INVALID_REQUEST", "At least one Agent is required")
    profile_agents = options.profile_agents or options.agents
    if not set(options.agents).issubset(profile_agents):
        raise OneAgentError("INVALID_REQUEST", "profile_agents must include every requested Agent")
    for agent_id in [*options.agents, *profile_agents]:
        if agent_id not in catalog:
            raise OneAgentError("INVALID_REQUEST", f"Unknown Agent: {agent_id}")

    auto_agents = [agent_id for agent_id in options.agents if catalog[agent_id]["config_mode"] == "auto"]
    base_url = ""
    provider_name = ""
    if options.configure and auto_agents and not options.check_agent_only:
        if not options.api_key:
            raise OneAgentError("INVALID_REQUEST", "API key is required")
        base_url = provider_base(options.provider, options.api_base_url)
        provider_name = PROVIDERS.get(options.provider, {"name": "Custom"})["name"]
        env_agents = [agent for agent in auto_agents if needs_env_file(catalog[agent])]
        for agent in env_agents:
            write_agent_env(
                runtime,
                agent,
                options.api_key,
                base_url,
                meta=catalog[agent],
                model=options.model,
            )
        if env_agents:
            # Configs written by earlier versions still name ONEAGENT_API_KEY.
            # Keep the shared file until those have been rewritten.
            write_shared_env(runtime, options.api_key, base_url)

    # Verify the model over each protocol the selected Agents actually speak,
    # before any config is written. A model that answers Chat Completions may
    # still reject Responses or Anthropic Messages, and writing a config for a
    # pair the endpoint refuses only moves the failure into the Agent itself.
    probes: dict[str, dict[str, Any]] = {}
    if options.configure and auto_agents and not options.skip_test and not options.check_agent_only:
        for protocol in sorted({agent_protocol(str(catalog[agent]["config_adapter"])) for agent in auto_agents}):
            probes[protocol] = protocol_probe(
                protocol=protocol,
                provider=options.provider,
                custom_base=options.api_base_url,
                api_key=options.api_key,
                model=options.model,
                timeout=options.timeout,
            )
        _sharpen_model_diagnosis(probes, options)

    results: list[dict[str, Any]] = []
    logs: list[str] = []
    next_steps: list[str] = []
    successful: list[str] = []
    first_exit_code = 0

    for agent_id in options.agents:
        meta = catalog[agent_id]
        if runtime.os_id not in meta.get("platforms", []):
            error = OneAgentError("PREREQUISITE_MISSING", f"{meta['name']} is not supported on {runtime.os_id}")
            results.append({"agent": agent_id, "status": "failed", "code": error.exit_code, "error_code": error.code, "message": error.message, "retryable": False})
            first_exit_code = first_exit_code or error.exit_code
            continue
        if meta["config_mode"] == "guide":
            results.append({"agent": agent_id, "status": "guide-only", "message": meta["guide"], "retryable": False})
            logs.append(f"## {agent_id}\nGuide only. {meta['guide']}")
            next_steps.append(meta["guide"])
            successful.append(agent_id)
            continue
        try:
            install_info = {"installed": False, "version": installed_version(runtime, meta), "lockedVersion": (meta.get("package") or {}).get("version")}
            if options.install_agent:
                install_info = install_locked_agent(
                    runtime,
                    agent_id,
                    meta,
                    enforce_locked=options.locked_version,
                    latest=options.latest,
                    timeout=options.timeout,
                    registry=options.registry,
                )
                if install_info.get("registry"):
                    # Recorded so the user can tell afterwards which registry a
                    # package actually came from.
                    logs.append(f"## {agent_id}\nregistry: {install_info['registry']}")
            elif meta.get("command") and not runtime.which(meta["command"]):
                package = meta.get("package") or {}
                manager = package.get("manager")
                if manager == "npm":
                    package_spec = f"{package.get('name')}@{package.get('version')}"
                    install_command = f"npm install -g {package_spec}"
                elif manager == "uv":
                    package_spec = f"{package.get('name')}=={package.get('version')}"
                    install_command = (
                        f"uv tool install --force --python python3.12 --no-python-downloads {package_spec}"
                    )
                else:
                    install_command = f"Unsupported package manager: {manager or 'missing'}"
                logs.append(f"## {agent_id}\nofficial install: {install_command}")
            if options.check_agent_only:
                command_present = bool(meta.get("command") and runtime.which(meta["command"])) or bool(install_info["installed"])
                status = "installed" if command_present else "skipped"
                results.append({"agent": agent_id, "status": status, **install_info, "retryable": False})
                logs.append(f"## {agent_id}\nAgent check complete.")
                successful.append(agent_id)
                continue
            config_path = None
            if options.configure:
                adapter = str(meta.get("config_adapter") or "")
                verdict = probes.get(agent_protocol(adapter))
                if verdict is not None and not verdict["ok"]:
                    raise OneAgentError(
                        str(verdict["error_code"] or "PROVIDER_UNREACHABLE"),
                        f"{meta['name']}: {verdict['message']}",
                        retryable=bool(verdict["retryable"]),
                    )
                config_base_url = provider_config_base(
                    options.provider,
                    options.api_base_url,
                    adapter,
                )
                config_path = _write_agent_config(
                    runtime,
                    agent_id,
                    meta,
                    provider_name,
                    config_base_url,
                    options.api_key,
                    options.model,
                )
                # Record the binding only after the config write succeeded, so a
                # failed write never leaves a binding claiming a state the
                # Agent's own config does not have.
                write_agent_binding(
                    runtime,
                    agent_id,
                    provider=options.provider,
                    base_url=config_base_url,
                    model=options.model,
                )
            results.append(
                {
                    "agent": agent_id,
                    "status": "configured" if options.configure else "skipped",
                    "config": str(config_path) if config_path else "",
                    **install_info,
                    "retryable": False,
                }
            )
            logs.append(f"## {agent_id}\n{'Configured' if options.configure else 'Model configuration skipped'}.")
            next_step = _next_step(runtime, agent_id, options.model)
            if next_step:
                next_steps.append(next_step)
            successful.append(agent_id)
        except OneAgentError as exc:
            first_exit_code = first_exit_code or exc.exit_code
            results.append(
                {
                    "agent": agent_id,
                    "status": "failed",
                    "code": exc.exit_code,
                    "error_code": exc.code,
                    "message": exc.message,
                    "retryable": exc.retryable,
                }
            )
            logs.append(f"## {agent_id}\n{exc.message}")

    # Surface the failing protocol first: with several Agents selected the GUI
    # shows one probe, and the actionable one is the protocol that refused.
    probe_result = None
    for verdict in probes.values():
        if probe_result is None or (not verdict["ok"] and probe_result["ok"]):
            probe_result = verdict
    for protocol, verdict in sorted(probes.items()):
        if not verdict["ok"]:
            first_exit_code = first_exit_code or EXIT_CODES.get(str(verdict["error_code"]), 6)
            logs.append(f"## provider ({protocol})\n{verdict['message']}")

    failed = [result for result in results if result["status"] == "failed"]
    if not failed and (probe_result is None or probe_result["ok"]) and not options.check_agent_only:
        write_profile(
            runtime,
            agents=profile_agents,
            configure=options.configure,
            provider=options.provider,
            base_url=base_url,
            model=options.model,
            api_key=options.api_key,
        )
    text = redact("\n\n".join(logs), [options.api_key])
    return {
        "ok": not failed and (probe_result is None or bool(probe_result["ok"])),
        "code": first_exit_code,
        "results": results,
        "log": text,
        "next": "\n".join(next_steps),
        "probe": probe_result,
        "probes": probes,
    }


def public_profile_summary(item: dict[str, Any]) -> dict[str, Any]:
    """The client-facing shape of a stored profile (camelCase, no secrets)."""
    return {
        "id": str(item.get("id")),
        "label": str(item.get("label") or item.get("id")),
        "provider": item.get("provider"),
        "baseUrl": item.get("base_url"),
        "model": item.get("model"),
        "agentIds": list(item.get("agent_ids") or []),
        "activatedAt": item.get("activated_at"),
        "hasKey": bool(item.get("has_key")),
    }


def _restart_hint(agent_id: str) -> str:
    """How the user makes a rewritten config take effect.

    Agents read their config at startup, so a rewrite is invisible to an
    already-running process. Saying "activated" without this is how a user
    concludes the switch silently failed.
    """
    meta = agent_catalog().get(agent_id) or {}
    command = str(meta.get("command", ""))
    if not command:
        return f"Restart {agent_id}"
    if agent_id == "aider":
        return f"Restart {command} in a shell that sources ~/.oneagent/aider.env"
    if needs_env_file(meta):
        # Restarting alone is not enough when the credential lives in a file:
        # the new shell has to source it, or the Agent starts unauthenticated.
        return (
            f"Quit any running {command} process, then start it again in a shell that sources "
            f"~/.oneagent/agents/{agent_id}.env"
        )
    return f"Quit any running {command} process, then start it again"


def activate_agent(
    runtime: Runtime,
    agent_id: str,
    *,
    provider: str,
    api_base_url: str = "",
    api_key: str,
    model: str,
    timeout: int = 180,
) -> dict[str, Any]:
    """Point one Agent at a provider and model, leaving every other Agent alone.

    Per-Agent credentials make this genuinely local: only this Agent's config
    and env file change, so a failure cannot leave two Agents disagreeing and
    there is no cross-file rollback to get right.
    """
    catalog = agent_catalog()
    agent_id = validate_agent_id(agent_id)
    meta = catalog.get(agent_id)
    if meta is None:
        raise OneAgentError("INVALID_REQUEST", f"Unknown Agent: {agent_id}")
    if meta["config_mode"] != "auto":
        raise OneAgentError("INVALID_REQUEST", f"{agent_id} is guide-only and has no managed configuration")
    if not api_key:
        raise OneAgentError("INVALID_REQUEST", "API key is required")

    resolved_model = model or resolve_probe_model(
        provider=provider, api_key=api_key, custom_base=api_base_url, timeout=timeout
    )
    provider_name = PROVIDERS.get(provider, {"name": "Custom"})["name"]
    config_base_url = provider_config_base(provider, api_base_url, str(meta["config_adapter"]))

    if needs_env_file(meta):
        write_agent_env(
            runtime,
            agent_id,
            api_key,
            provider_base(provider, api_base_url),
            meta=meta,
            model=resolved_model,
        )
    config_path = _write_agent_config(
        runtime, agent_id, meta, provider_name, config_base_url, api_key, resolved_model
    )
    binding = write_agent_binding(
        runtime, agent_id, provider=provider, base_url=config_base_url, model=resolved_model
    )
    return {
        "ok": True,
        "agent": agent_id,
        "config": str(config_path),
        "provider": provider,
        "model": resolved_model,
        "binding": binding,
        "restart": _restart_hint(agent_id),
        "next": _next_step(runtime, agent_id, resolved_model),
    }


def status_payload(runtime: Runtime | None = None) -> dict[str, Any]:
    runtime = runtime or Runtime.create()
    catalog = agent_catalog()
    agents: dict[str, Any] = {}
    paths: dict[str, str] = {"env_file": str(_env_path(runtime)), "profile": str(profile_path(runtime))}
    capabilities: dict[str, Any] = {"canInstall": {}, "supportedAgentIds": []}
    # Read once: the per-Agent bindings are one small directory, and reading
    # inside the loop would re-scan it for every Agent in the catalog.
    bindings = list_agent_bindings(runtime)
    for agent_id, meta in catalog.items():
        path = _config_path(runtime, meta)
        if path:
            paths[f"{agent_id}_config"] = str(path)
        command = meta.get("command")
        installed = bool(command and runtime.which(command))
        package = meta.get("package") or {}
        manager = package.get("manager")
        if manager == "npm":
            can_install = bool(runtime.which("npm"))
        elif manager == "uv":
            try:
                can_install = bool(runtime.which("uv") and _python_312_for_uv(runtime))
            except OneAgentError:
                can_install = False
        else:
            can_install = False
        if runtime.os_id == "windows" and agent_id == "claude-code":
            can_install = can_install and bool(runtime.which("git"))
        capabilities["canInstall"][agent_id] = can_install
        if runtime.os_id in meta.get("platforms", []):
            capabilities["supportedAgentIds"].append(agent_id)
        binding = bindings.get(agent_id)
        agents[agent_id] = {
            "installed": installed,
            "config": str(path) if path else "",
            "configured": bool(path and path.exists()),
            "guideOnly": meta["config_mode"] == "guide",
            "version": installed_version(runtime, meta) if installed and meta["config_mode"] == "auto" else None,
            "lockedVersion": package.get("version"),
            "canInstall": can_install,
            # What this Agent is pointed at, independent of every other Agent.
            "provider": binding.get("provider") if binding else None,
            "model": binding.get("model") if binding else None,
            "baseUrl": binding.get("base_url") if binding else None,
            "updatedAt": binding.get("updated_at") if binding else None,
        }
    profile, profile_error = load_profile(runtime)
    profiles = [public_profile_summary(item) for item in list_profiles(runtime)]
    return {
        "apiVersion": 1,
        "platform": {**current_platform(), "os": runtime.os_id},
        "capabilities": capabilities,
        "agents": agents,
        "catalog": public_catalog(),
        "groups": AGENT_GROUPS,
        "providers": public_providers(),
        "mirrors": public_mirrors(),
        "paths": paths,
        "backups": {
            "codex": bool(list((runtime.home / ".codex").glob("config.toml.backup-*"))),
            "claude-code": bool(list((runtime.home / ".claude").glob("settings.json.backup-*"))),
            "env": bool(list((runtime.home / ".oneagent").glob(f"{_env_path(runtime).name}.backup-*"))),
            "profile": bool(list((runtime.home / ".oneagent").glob("profile.json.backup-*"))),
        },
        "profiles": profiles,
        "activeProfile": active_profile_id(runtime),
        "environment": profile,
        "environmentError": profile_error,
    }
