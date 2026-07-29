#!/usr/bin/env python3
"""Verify, without any API key, that a configured Agent adopts what we wrote.

The layer below this one proves an Agent installs and that its config file has
the right shape; agent_e2e_smoke.py proves an Agent answers a real request but
needs a real key. Neither ran where the Claude Code defect was found: an Agent
OneAgent reported as ``configured`` that answered ``Not logged in`` the moment
it started, because the credential never reached it.

The trick here is that telling adopted from not-adopted does not need a key.
Point the configuration at the discard port (127.0.0.1:9, nothing listens) with
a dummy credential and run the Agent:

* an adopted configuration fails at the network layer -- ``provider: oneagent``
  followed by ``Reconnecting`` for Codex, a connection-refused error for Claude
  Code -- because the Agent read our config and tried to reach our endpoint;
* an ignored configuration fails at the auth/login layer first -- ``Not logged
  in`` -- before any connection is attempted.

A valid key would still only ever produce a connection failure at the discard
port, so the dummy credential is enough to separate the two outcomes. This is
exactly the pair of results that distinguished the bug, which is why the check
belongs in automation rather than a manual smoke.

Two layers, mirroring the rest of the verification design:

* ``classify_adoption`` is pure and is unit-tested in ordinary CI against the
  two real-world outputs, so the discriminator itself is covered everywhere;
* running the actual Agents needs them installed, so it happens here, in the
  release-candidate workflow that already installs Agents -- but keylessly,
  removing the real-key threshold that let the defect through.

Usage::

    python3.12 scripts/agent_config_adopted_check.py
    python3.12 scripts/agent_config_adopted_check.py --agents codex,claude-code
"""
from __future__ import annotations

import argparse
import shlex
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
from oneagent.installer import InstallOptions, agent_env_path, install_many, redact
from scripts.verify_locked_agents import create_isolated_runtime

DEFAULT_AGENTS = ("codex", "claude-code")
# Port 9 is the discard service: nothing answers, so every adopted configuration
# fails with a connection error rather than a real response.
DISCARD_BASE_URL = "http://127.0.0.1:9"
DUMMY_API_KEY = "oneagent-discard-key"
DISCARD_MODEL = "oneagent-discard-model"
PROMPT = "Reply with the single word: ready"

# Output that means the Agent rejected the credential or never read the config
# we wrote -- the configuration was NOT adopted. These are the signatures of the
# "Not logged in" defect: the Agent talks about auth or login, which only
# happens when our configuration did not reach it. Checked first, so a verbose
# error that mentions both layers is reported as the failure it is.
NOT_ADOPTED_MARKERS = (
    "not logged in",
    "please run /login",
    "please login",
    "login required",
    "requires authentication",
    "authentication required",
    "no api key",
    "api key not found",
    "missing api key",
    "invalid api key",
    "unauthorized",
    "no credentials",
)

# Output that means the Agent read our configuration and tried to reach the
# endpoint we pointed it at -- the configuration WAS adopted. Against the
# discard port this is the passing outcome.
ADOPTED_MARKERS = (
    "provider: oneagent",  # Codex echoes the provider section it adopted
    "reconnecting",
    "connection refused",
    "connect econnrefused",
    "econnrefused",
    "connection reset",
    "could not connect",
    "failed to connect",
    "cannot connect",
    "fetch failed",  # Claude Code's Node client wrapping a socket error
    "network error",
    "unreachable",
    "could not resolve host",
    "name or service not known",
    "nodename nor servname",  # macOS getaddrinfo wording
    "os error 61",  # macOS: connection refused
    "os error 111",  # linux: connection refused
)


def classify_adoption(output: str) -> tuple[bool, str]:
    """Decide whether an Agent's output shows it adopted the configuration.

    Returns ``(adopted, reason)``. The discard port makes the two outcomes
    mutually exclusive -- an adopted config fails at the network layer, an
    ignored one at the auth layer -- so no real key is needed to tell them
    apart. Auth markers win: when in doubt, report the configuration as not
    adopted rather than pass a check that means nothing.
    """
    lowered = output.lower()
    for marker in NOT_ADOPTED_MARKERS:
        if marker in lowered:
            return False, f"auth/login error, config not adopted ({marker!r})"
    for marker in ADOPTED_MARKERS:
        if marker in lowered:
            return True, f"connection failure, config adopted ({marker!r})"
    return False, "output shows neither a connection failure nor an auth error"


def parse_env_file(path: Path) -> dict[str, str]:
    """Read the ``export``/``$env:`` assignments an Agent's env file holds.

    Sourcing this file is what the next-step hint tells the user to do; loading
    the same pairs into the child environment reproduces it without a shell.
    """
    values: dict[str, str] = {}
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if line.startswith("export "):
            name, sep, raw = line[len("export "):].partition("=")
            if not sep:
                continue
            try:
                parsed = shlex.split(raw)
                values[name.strip()] = parsed[0] if parsed else ""
            except ValueError:
                values[name.strip()] = raw.strip().strip("'\"")
        elif line.startswith("$env:"):
            name, sep, raw = line[len("$env:"):].partition("=")
            if sep:
                values[name.strip()] = raw.strip().strip("'\"")
    return values


@dataclass
class Outcome:
    agent: str
    step: str
    ok: bool | None  # None marks a skip rather than a pass or a failure
    detail: str


def configure_discard(runtime, agents: list[str]) -> dict:
    """Install the Agents and point them at the discard port with a dummy key.

    skip_test is deliberate: the discard port cannot answer a protocol probe,
    and the probe is not what this layer verifies -- adopting the config is.
    """
    return install_many(
        InstallOptions(
            agents=list(agents),
            provider="custom",
            api_base_url=DISCARD_BASE_URL,
            api_key=DUMMY_API_KEY,
            model=DISCARD_MODEL,
            configure=True,
            install_agent=True,
            locked_version=True,
            skip_test=True,
            home=runtime.home,
            os_id=runtime.os_id,
            timeout=900,
        ),
        runtime,
    )


# One-shot, non-interactive invocations: both Agents default to a REPL and would
# wait for input until the timeout. Add an Agent's line here when it gains a
# config adapter -- this is launch syntax, which is code, not lock data.
ONE_SHOT_ARGV = {
    "codex": lambda exe: [exe, "exec", PROMPT],
    "claude-code": lambda exe: [exe, "-p", PROMPT],
}


def exercise(agent_id: str, runtime, timeout: int) -> Outcome:
    meta = agent_catalog()[agent_id]
    command = str(meta["command"])
    executable = runtime.which(command)
    build_argv = ONE_SHOT_ARGV.get(agent_id)
    if not executable or build_argv is None:
        return Outcome(agent_id, "run", None, f"{command} not exercisable here; skipped")
    env = dict(runtime.env)
    env_file = agent_env_path(runtime, agent_id)
    if env_file.is_file():
        # Reproduce `source <env file>`: the credential route under test.
        env.update(parse_env_file(env_file))
    env.update({"CI": "1", "NO_COLOR": "1", "TERM": "dumb", "LANG": "C"})
    try:
        result = subprocess.run(
            build_argv(executable),
            text=True,
            capture_output=True,
            timeout=timeout,
            env=env,
            cwd=str(runtime.home),
        )
    except subprocess.TimeoutExpired:
        return Outcome(agent_id, "run", False, f"no output within {timeout}s; cannot confirm adoption")
    output = ((result.stdout or "") + (result.stderr or "")).strip()
    adopted, reason = classify_adoption(output)
    tail = redact(" ".join(output.split())[-200:], [DUMMY_API_KEY])
    return Outcome(agent_id, "run", adopted, f"{reason} | {tail}" if tail else reason)


def run_check(agents: list[str], timeout: int) -> list[Outcome]:
    root = Path(tempfile.mkdtemp(prefix="oneagent-adopt-"))
    outcomes: list[Outcome] = []
    try:
        runtime = create_isolated_runtime(root)
        result = configure_discard(runtime, agents)
        for entry in result.get("results", []):
            agent = str(entry.get("agent"))
            status = str(entry.get("status"))
            ok = status in {"configured", "installed"}
            outcomes.append(Outcome(agent, "configure", ok, status or str(entry.get("message"))))
        if not result.get("ok"):
            detail = redact(" ".join(str(result.get("log", "")).split())[-300:], [DUMMY_API_KEY])
            outcomes.append(Outcome("-", "configure", False, detail or "configuration did not complete"))
        else:
            for agent_id in agents:
                outcomes.append(exercise(agent_id, runtime, timeout))
    finally:
        shutil.rmtree(root, ignore_errors=True)
    return outcomes


def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(
        description="Keyless check that configured Agents adopt the configuration OneAgent wrote"
    )
    parser.add_argument("--agents", default=",".join(DEFAULT_AGENTS), help="Comma-separated Agent IDs")
    parser.add_argument("--timeout", type=int, default=120, help="Seconds to wait per Agent run")
    args = parser.parse_args(argv)
    agents = [item.strip() for item in args.agents.split(",") if item.strip()]

    outcomes = run_check(agents, args.timeout)
    print()
    width = max(len(item.agent) for item in outcomes)
    for item in outcomes:
        mark = "PASS" if item.ok else ("SKIP" if item.ok is None else "FAIL")
        print(f"[{mark}] {item.agent:<{width}}  {item.step:<10} {item.detail}")

    failures = [item for item in outcomes if item.ok is False]
    if failures:
        raise SystemExit(f"\n{len(failures)} check(s) failed")
    exercised = [item for item in outcomes if item.step == "run" and item.ok is True]
    if not exercised:
        print("\nNo Agent could be exercised; nothing verified.")
    else:
        print(f"\n{len(exercised)} Agent(s) adopted the configuration with no API key in play.")


if __name__ == "__main__":
    main()
