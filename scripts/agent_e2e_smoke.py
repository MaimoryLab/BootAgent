#!/usr/bin/env python3
"""Ask Codex and Claude Code to actually answer something.

The layer below this one proves an Agent installs, resolves on PATH and reports
the locked version, and the Python suites prove the config file has the right
shape. None of that proves the Agent works: a config can be structurally perfect
and still name an endpoint the Agent rejects, or a model the account cannot use.
Running the Agent is the only judge of "usable".

Needs a real key, so it never runs in ordinary CI:

    ONEAGENT_API_KEY=... python3.12 scripts/agent_e2e_smoke.py --provider ppio
    ONEAGENT_API_KEY=... python3.12 scripts/agent_e2e_smoke.py \
        --provider custom --api-base-url https://example.com/openai
"""
from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from oneagent.catalog import agent_catalog
from oneagent.installer import InstallOptions, Runtime, install_many, redact

# Codex speaks Responses and Claude Code speaks Anthropic Messages, so a key that
# satisfies one can still fail the other -- which is why both are exercised
# rather than one standing in for the pair.
AGENTS = ("codex", "claude-code")
PROMPT = "Reply with the single word: ready"


@dataclass
class Outcome:
    agent: str
    step: str
    ok: bool
    detail: str


def run(argv: list[str], *, env: dict[str, str], timeout: int, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        argv, text=True, capture_output=True, timeout=timeout, env=env, cwd=str(cwd) if cwd else None
    )


def configure(home: Path, provider: str, api_base_url: str, api_key: str, model: str, timeout: int) -> dict:
    """Full install_many, protocol probe included -- no --skip-test here."""
    return install_many(
        InstallOptions(
            agents=list(AGENTS),
            provider=provider,
            api_base_url=api_base_url,
            api_key=api_key,
            model=model,
            configure=True,
            skip_test=False,
            home=home,
            timeout=timeout,
        ),
        Runtime.create(home=home),
    )


def check_written_config(home: Path) -> list[Outcome]:
    """Each adapter writes a different shape; check what it actually promises."""
    results: list[Outcome] = []

    codex_config = home / ".codex" / "config.toml"
    if codex_config.is_file():
        text = codex_config.read_text(encoding="utf-8")
        env_var = "ONEAGENT_API_KEY_CODEX"
        env_file = home / ".oneagent" / "agents" / "codex.env"
        # Codex reads the key indirectly through env_key, so the variable it
        # names has to exist in the file the next-step hint sources.
        named = f'env_key = "{env_var}"' in text
        sourced = env_file.is_file() and env_var in env_file.read_text(encoding="utf-8")
        results.append(
            Outcome("codex", "config", named and sourced, f"env_key named={named} present_in_env_file={sourced}")
        )
    else:
        results.append(Outcome("codex", "config", False, f"missing {codex_config}"))

    claude_config = home / ".claude" / "settings.json"
    if claude_config.is_file():
        env = json.loads(claude_config.read_text(encoding="utf-8")).get("env", {})
        required = ["ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL", "ANTHROPIC_SMALL_FAST_MODEL"]
        missing = [key for key in required if not env.get(key)]
        results.append(
            Outcome("claude-code", "config", not missing, "all four variables set" if not missing else f"missing {missing}")
        )
    else:
        results.append(Outcome("claude-code", "config", False, f"missing {claude_config}"))
    return results


def agent_env(home: Path, agent_id: str, api_key: str) -> dict[str, str]:
    """The environment the next-step hint tells the user to set up."""
    env = {
        "HOME": str(home),
        "USERPROFILE": str(home),
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "LANG": "C",
        "TERM": "dumb",
        # Both Agents skip first-run interactive setup when told they are not on
        # a TTY; without this they wait for input and the smoke times out.
        "CI": "1",
        "NO_COLOR": "1",
    }
    if agent_id == "codex":
        env["ONEAGENT_API_KEY_CODEX"] = api_key
    return env


def exercise(agent_id: str, home: Path, api_key: str, timeout: int) -> Outcome:
    command = agent_catalog()[agent_id]["command"]
    executable = shutil.which(command)
    if not executable:
        return Outcome(agent_id, "run", False, f"{command} is not on PATH")
    argv = {
        # Non-interactive one-shot flags: both Agents default to a REPL.
        "codex": [executable, "exec", PROMPT],
        "claude-code": [executable, "-p", PROMPT],
    }[agent_id]
    try:
        result = run(argv, env=agent_env(home, agent_id, api_key), timeout=timeout, cwd=home)
    except subprocess.TimeoutExpired:
        return Outcome(agent_id, "run", False, f"no answer within {timeout}s")
    output = ((result.stdout or "") + (result.stderr or "")).strip()
    detail = redact(" ".join(output.split())[-300:], [api_key])
    if result.returncode != 0:
        return Outcome(agent_id, "run", False, f"exit {result.returncode}: {detail}")
    if not output:
        return Outcome(agent_id, "run", False, "exited 0 but produced no output")
    return Outcome(agent_id, "run", True, detail[:160])


def main() -> None:
    parser = argparse.ArgumentParser(description="End-to-end usability smoke for Codex and Claude Code")
    parser.add_argument("--provider", default="ppio")
    parser.add_argument("--api-base-url", default="")
    parser.add_argument("--model", default="", help="Leave empty to let discovery choose")
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--keep-home", action="store_true", help="Keep the temporary HOME for inspection")
    args = parser.parse_args()

    api_key = os.environ.get("ONEAGENT_API_KEY", "")
    if not api_key:
        raise SystemExit("ONEAGENT_API_KEY is required; this smoke needs a real credential")

    home = Path(tempfile.mkdtemp(prefix="oneagent-e2e-"))
    outcomes: list[Outcome] = []
    try:
        print(f"HOME: {home}")
        print(f"provider: {args.provider}  model: {args.model or '(discovered)'}")

        result = configure(home, args.provider, args.api_base_url, api_key, args.model, args.timeout)
        log = redact(result.get("log", ""), [api_key])
        if api_key in log:
            outcomes.append(Outcome("-", "redaction", False, "the install log contains the API key"))
        else:
            outcomes.append(Outcome("-", "redaction", True, "install log carries no credential"))
        for entry in result.get("results", []):
            agent = str(entry.get("agent"))
            ok = entry.get("status") in {"configured", "installed"}
            outcomes.append(Outcome(agent, "configure", ok, str(entry.get("status"))))
        probe = result.get("probe") or {}
        outcomes.append(
            Outcome("-", "probe", bool(probe.get("ok", False)), str(probe.get("message", "no probe result")))
        )

        outcomes.extend(check_written_config(home))
        for agent_id in AGENTS:
            outcomes.append(exercise(agent_id, home, api_key, args.timeout))
    finally:
        if args.keep_home:
            print(f"kept {home}")
        else:
            shutil.rmtree(home, ignore_errors=True)

    print()
    width = max(len(item.agent) for item in outcomes)
    for item in outcomes:
        print(f"[{'PASS' if item.ok else 'FAIL'}] {item.agent:<{width}}  {item.step:<10} {item.detail}")
    failures = [item for item in outcomes if not item.ok]
    if failures:
        raise SystemExit(f"\n{len(failures)} check(s) failed")
    print("\nBoth Agents answered a real request.")


if __name__ == "__main__":
    main()
