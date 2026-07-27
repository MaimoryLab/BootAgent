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
from .installer import InstallOptions, Runtime, install_many, redact
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
    parser.add_argument("--home", type=Path, default=None, help=argparse.SUPPRESS)
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


def run(argv: list[str] | None = None) -> int:
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
