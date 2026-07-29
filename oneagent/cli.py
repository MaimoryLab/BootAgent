from __future__ import annotations

import argparse
import getpass
import json
import os
import sys
import webbrowser
from pathlib import Path

from .catalog import PROVIDERS
from .errors import OneAgentError
from .installer import (
    InstallOptions,
    Runtime,
    activate_agent,
    install_many,
    list_agent_bindings,
    read_profile_secret,
    redact,
)
from .providers import provider_home


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Install or configure one Agent with OneAgent")
    parser.add_argument("--agent", default="codex", help="Agent ID; comma-separated for several")
    parser.add_argument("--provider", default="ppio")
    parser.add_argument("--api-base-url", default="")
    parser.add_argument("--api-key", default="")
    parser.add_argument("--model", default="", help="Defaults to the provider's probe model")
    parser.add_argument("--register-url", metavar="URL", default="")
    parser.add_argument("--channel", default="direct")
    parser.add_argument("--install-agent", action="store_true")
    parser.add_argument("--check-agent-only", action="store_true")
    parser.add_argument("--skip-test", action="store_true")
    parser.add_argument("--no-open", action="store_true")
    parser.add_argument("--json", action="store_true", dest="json_output")
    parser.add_argument("--locked-version", action="store_true")
    parser.add_argument("--latest", action="store_true")
    parser.add_argument(
        "--registry",
        default="",
        help="Package registry: a mirror id (official, npmmirror) or an https:// URL. Defaults to the official registry.",
    )
    parser.add_argument("--home", type=Path, default=None, help=argparse.SUPPRESS)
    return parser


def build_agent_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="oneagent agent", description="Inspect and repoint individual Agents")
    sub = parser.add_subparsers(dest="action", required=True)

    listing = sub.add_parser("list", help="Show each Agent's provider and model")

    point = sub.add_parser("set", help="Point one Agent at a provider and model")
    point.add_argument("agent_id")
    point.add_argument("--provider", default="ppio")
    point.add_argument("--api-base-url", default="")
    point.add_argument("--api-key", default="")
    point.add_argument("--model", default="", help="Defaults to a model the provider lists")
    point.add_argument("--profile", default="", help="Reuse the key saved for this profile template")

    for target in (listing, point):
        target.add_argument("--json", action="store_true", dest="json_output")
        target.add_argument("--home", type=Path, default=None, help=argparse.SUPPRESS)
    return parser


def _registration_url(args: argparse.Namespace) -> str:
    if args.register_url:
        return args.register_url
    if os.environ.get("ONEAGENT_REGISTER_URL"):
        return os.environ["ONEAGENT_REGISTER_URL"]
    if args.provider in PROVIDERS:
        return provider_home(args.provider)
    return PROVIDERS["ppio"]["home"]


def _resolve_api_key(args: argparse.Namespace) -> str:
    if args.check_agent_only:
        return ""
    key = os.environ.get("ONEAGENT_API_KEY") or args.api_key
    if key:
        return key
    if not sys.stdin.isatty():
        raise OneAgentError(
            "INVALID_REQUEST",
            "API key is required; set ONEAGENT_API_KEY or pass --api-key (pasting interactively needs a TTY)",
        )
    register_url = _registration_url(args)
    if not args.no_open:
        webbrowser.open(register_url)
    print(f"Create or copy an API key from: {register_url}", file=sys.stderr)
    return getpass.getpass("Paste API key: ")


def _run_agent_command(argv: list[str]) -> int:
    args = build_agent_parser().parse_args(argv)
    runtime = Runtime.create(home=args.home)
    try:
        if args.action == "list":
            bindings = list_agent_bindings(runtime)
            if args.json_output:
                print(json.dumps({"ok": True, "agents": bindings}, ensure_ascii=False))
            elif not bindings:
                print("[oneagent] no Agent has been configured yet")
            else:
                for agent_id, binding in bindings.items():
                    print(f"{agent_id:14} {binding['provider']:10} {binding['model']}")
            return 0

        api_key = args.api_key or os.environ.get("ONEAGENT_API_KEY", "")
        if not api_key and args.profile:
            api_key = read_profile_secret(runtime, args.profile)
        result = activate_agent(
            runtime,
            args.agent_id,
            provider=args.provider,
            api_base_url=args.api_base_url,
            api_key=api_key,
            model=args.model,
        )
        if args.json_output:
            print(json.dumps(result, ensure_ascii=False))
        else:
            print(f"[oneagent] {result['agent']} -> {result['provider']} / {result['model']}")
            print(f"[oneagent] {result['restart']}")
            print(f"[oneagent] next: {result['next']}")
        return 0
    except OneAgentError as exc:
        if args.json_output:
            print(json.dumps(exc.to_dict(), ensure_ascii=False))
        else:
            print(f"[oneagent] error: {exc.message}", file=sys.stderr)
        return exc.exit_code


def run(argv: list[str] | None = None) -> int:
    raw = list(sys.argv[1:] if argv is None else argv)
    # "agent" is a subcommand; every other invocation keeps the original flat
    # flag form so existing scripts and the installers do not have to change.
    if raw and raw[0] == "agent":
        return _run_agent_command(raw[1:])
    parser = build_parser()
    args = parser.parse_args(argv)
    api_key = ""
    try:
        if args.locked_version and args.latest:
            raise OneAgentError("INVALID_REQUEST", "--locked-version and --latest cannot be used together")
        agent_ids = [part.strip() for part in args.agent.split(",") if part.strip()]
        api_key = _resolve_api_key(args)
        result = install_many(
            InstallOptions(
                agents=agent_ids,
                provider=args.provider,
                api_base_url=args.api_base_url,
                api_key=api_key,
                model=args.model,
                configure=not args.check_agent_only,
                install_agent=args.install_agent,
                check_agent_only=args.check_agent_only,
                skip_test=args.skip_test,
                locked_version=args.locked_version,
                latest=args.latest,
                registry=args.registry,
                channel=args.channel,
                home=args.home,
            ),
            Runtime.create(home=args.home),
        )
        if args.json_output:
            print(json.dumps(result, ensure_ascii=False))
        else:
            if result["log"]:
                print(result["log"])
            if result["next"]:
                for line in result["next"].splitlines():
                    print(f"[oneagent] next: {line}")
        return int(result["code"] if not result["ok"] else 0)
    except OneAgentError as exc:
        payload = exc.to_dict()
        if args.json_output:
            print(json.dumps(payload, ensure_ascii=False))
        else:
            print(f"[oneagent] error: {redact(exc.message, [api_key])}", file=sys.stderr)
        return exc.exit_code
    except KeyboardInterrupt:
        return 130
    except Exception as exc:
        error = OneAgentError("INTERNAL_ERROR", "Unexpected OneAgent failure")
        if args.json_output:
            print(json.dumps(error.to_dict(), ensure_ascii=False))
        else:
            print(f"[oneagent] error: {error.message}", file=sys.stderr)
        if os.environ.get("ONEAGENT_DEBUG") == "1":
            raise
        return error.exit_code


def main() -> None:
    raise SystemExit(run())


if __name__ == "__main__":
    main()
