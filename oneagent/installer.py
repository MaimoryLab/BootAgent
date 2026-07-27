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
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Iterable

from .catalog import AGENT_GROUPS, PROVIDERS, agent_catalog, current_platform, public_catalog, resolve_home
from .errors import EXIT_CODES, OneAgentError
from .providers import (
    agent_protocol,
    list_models,
    openai_base_url,
    protocol_label,
    protocol_probe,
    provider_base,
    provider_config_base,
)


Runner = Callable[..., subprocess.CompletedProcess[str]]


@dataclass
class InstallOptions:
    agents: list[str]
    profile_agents: list[str] | None = None
    provider: str = "ppio"
    api_base_url: str = ""
    api_key: str = ""
    model: str = "gpt-4.1"
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


def _config_path(runtime: Runtime, meta: dict[str, Any]) -> Path | None:
    raw = meta.get("windows_config_path") if runtime.os_id == "windows" else meta.get("config_path")
    if not raw:
        raw = meta.get("config_path")
    return runtime.home / raw if raw else None


def _env_path(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / ("env.ps1" if runtime.os_id == "windows" else "env")


def _shared_env_content(runtime: Runtime, api_key: str, base_url: str) -> str:
    if runtime.os_id == "windows":
        return (
            f"$env:ONEAGENT_API_KEY = {_powershell_quote(api_key)}\n"
            f"$env:ONEAGENT_API_BASE_URL = {_powershell_quote(base_url)}\n"
        )
    return (
        f"export ONEAGENT_API_KEY={shlex.quote(api_key)}\n"
        f"export ONEAGENT_API_BASE_URL={shlex.quote(base_url)}\n"
    )


def write_shared_env(runtime: Runtime, api_key: str, base_url: str) -> Path:
    path = _env_path(runtime)
    atomic_write(runtime, path, _shared_env_content(runtime, api_key, base_url), secret=True)
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
        'env_key = "ONEAGENT_API_KEY"\n'
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
        "options": {"baseURL": openai_base_url(base_url), "apiKey": "{env:ONEAGENT_API_KEY}"},
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


def install_locked_agent(
    runtime: Runtime,
    agent_id: str,
    meta: dict[str, Any],
    *,
    enforce_locked: bool,
    latest: bool,
    timeout: int,
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
    if manager == "npm":
        npm = runtime.which("npm")
        assert npm is not None
        spec = package_name if latest else f"{package_name}@{locked}"
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
        result = runtime.runner(args, text=True, capture_output=True, timeout=timeout, env=runtime.env)
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
    return {"installed": True, "version": locked if not latest else None, "lockedVersion": locked}


def _next_step(runtime: Runtime, agent_id: str, model: str) -> str:
    if runtime.os_id == "windows":
        env = '. "$HOME\\.oneagent\\env.ps1"'
        aider_env = '. "$HOME\\.oneagent\\aider.ps1"'
    else:
        env = "source ~/.oneagent/env"
        aider_env = "source ~/.oneagent/aider.env"
    return {
        "codex": f"{env}; codex" if runtime.os_id == "windows" else f"{env} && codex",
        "claude-code": "claude",
        "opencode": f"{env}; opencode" if runtime.os_id == "windows" else f"{env} && opencode",
        "kilo-cli": f"{env}; kilo" if runtime.os_id == "windows" else f"{env} && kilo",
        "aider": f"{aider_env}; aider --model openai/{model}" if runtime.os_id == "windows" else f"{aider_env} && aider --model openai/{model}",
    }.get(agent_id, "")


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
        return write_openai_compatible_config(runtime, meta, provider_name, base_url, model, "https://opencode.ai/config.json")
    if adapter == "kilo-cli":
        return write_openai_compatible_config(runtime, meta, provider_name, base_url, model, "https://app.kilo.ai/config.json")
    if adapter == "aider":
        return write_aider_config(runtime, meta, base_url, api_key)
    raise OneAgentError("INVALID_REQUEST", f"Unsupported auto-config Agent: {agent_id}")


def profile_path(runtime: Runtime) -> Path:
    return runtime.home / ".oneagent" / "profile.json"


def load_profile(runtime: Runtime) -> tuple[dict[str, Any] | None, str | None]:
    path = profile_path(runtime)
    if not path.exists():
        return None, None
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return None, str(exc)
    if not isinstance(value, dict) or value.get("schema_version") != 1:
        return None, "Unsupported environment profile schema"
    return value, None


def write_profile(
    runtime: Runtime,
    *,
    agents: list[str],
    configure: bool,
    provider: str,
    base_url: str,
    model: str,
) -> Path:
    current, _ = load_profile(runtime)
    merged_agents = set(agents)
    if current and current.get("provider") == (provider if configure else "existing-account") and current.get("model") == (model if configure else None):
        merged_agents.update(current.get("agent_ids") or [])
    profile = {
        "schema_version": 1,
        "provider": provider if configure else "existing-account",
        "base_url": base_url if configure else None,
        "model": model if configure else None,
        "config_mode": "provider" if configure else "existing-account",
        "agent_ids": sorted(merged_agents),
        "activated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
    }
    path = profile_path(runtime)
    atomic_write(runtime, path, json.dumps(profile, ensure_ascii=False, indent=2) + "\n")
    return path


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
        if any(agent in {"codex", "opencode", "kilo-cli"} for agent in auto_agents):
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
                )
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


def status_payload(runtime: Runtime | None = None) -> dict[str, Any]:
    runtime = runtime or Runtime.create()
    catalog = agent_catalog()
    agents: dict[str, Any] = {}
    paths: dict[str, str] = {"env_file": str(_env_path(runtime)), "profile": str(profile_path(runtime))}
    capabilities: dict[str, Any] = {"canInstall": {}, "supportedAgentIds": []}
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
        agents[agent_id] = {
            "installed": installed,
            "config": str(path) if path else "",
            "configured": bool(path and path.exists()),
            "guideOnly": meta["config_mode"] == "guide",
            "version": installed_version(runtime, meta) if installed and meta["config_mode"] == "auto" else None,
            "lockedVersion": package.get("version"),
            "canInstall": can_install,
        }
    profile, profile_error = load_profile(runtime)
    return {
        "apiVersion": 1,
        "platform": {**current_platform(), "os": runtime.os_id},
        "capabilities": capabilities,
        "agents": agents,
        "catalog": public_catalog(),
        "groups": AGENT_GROUPS,
        "providers": PROVIDERS,
        "paths": paths,
        "backups": {
            "codex": bool(list((runtime.home / ".codex").glob("config.toml.backup-*"))),
            "claude-code": bool(list((runtime.home / ".claude").glob("settings.json.backup-*"))),
            "env": bool(list((runtime.home / ".oneagent").glob(f"{_env_path(runtime).name}.backup-*"))),
            "profile": bool(list((runtime.home / ".oneagent").glob("profile.json.backup-*"))),
        },
        "environment": profile,
        "environmentError": profile_error,
    }
