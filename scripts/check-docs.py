#!/usr/bin/env python3
"""Check Markdown cross-links and the language split of outward-facing docs.

Two failures this catches that a build cannot: a relative link that stopped
resolving after a file moved, and Chinese text left in a document that is
declared English-only. Both are invisible to `go vet` and `pnpm run build`,
which is how docs drift in the first place.

Usage: python3 scripts/check-docs.py
"""

from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent

# Inline links, minus image embeds. Bare autolinks (<https://...>) never point at
# a repository path, so they are out of scope.
LINK = re.compile(r"(?<!!)\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")
CJK = re.compile(r"[一-鿿]")

# Documents that must not contain Chinese prose. Chinese is correct and expected
# outside these: AGENTS.md and docs/internal/ are maintainer-facing by decision.
ENGLISH_ONLY = [
    "README.md",
    "docs/product-boundary-baseline.md",
    "docs/distribution-compliance-policy.md",
    "docs/public-site-operations.md",
    "docs/decisions",
    "docs/ai-agent-kit/en",
]

# The kit root README is the language chooser, so it names the other language.
ENGLISH_ONLY_EXCEPTIONS = {"docs/ai-agent-kit/README.md"}

# Product names and identifiers that are not translated. A line whose only CJK
# is one of these is not a translation gap.
ALLOWED_CJK_LINES = ("简体中文",)


def tracked_markdown() -> list[pathlib.Path]:
    import subprocess

    out = subprocess.run(
        ["git", "ls-files", "*.md"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=True,
    ).stdout
    return [ROOT / line for line in out.splitlines() if line]


def check_links(paths: list[pathlib.Path]) -> list[str]:
    errors = []
    for path in paths:
        text = path.read_text(encoding="utf-8")
        for match in LINK.finditer(text):
            target = match.group(1)
            if target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            anchor = target.split("#", 1)[0]
            if not anchor:
                continue
            resolved = (path.parent / anchor).resolve()
            if not resolved.exists():
                line = text[: match.start()].count("\n") + 1
                rel = path.relative_to(ROOT)
                errors.append(f"{rel}:{line}: broken link -> {target}")
    return errors


def check_language(paths: list[pathlib.Path]) -> list[str]:
    errors = []
    for path in paths:
        rel = path.relative_to(ROOT).as_posix()
        if rel in ENGLISH_ONLY_EXCEPTIONS:
            continue
        if not any(rel == p or rel.startswith(p + "/") for p in ENGLISH_ONLY):
            continue
        for number, line in enumerate(path.read_text(encoding="utf-8").split("\n"), 1):
            if not CJK.search(line):
                continue
            if any(token in line for token in ALLOWED_CJK_LINES):
                continue
            errors.append(f"{rel}:{number}: Chinese text in an English-only doc")
    return errors


def main() -> int:
    paths = tracked_markdown()
    errors = check_links(paths) + check_language(paths)
    if errors:
        for error in errors:
            print(error, file=sys.stderr)
        print(f"\n{len(errors)} problem(s) in {len(paths)} markdown files", file=sys.stderr)
        return 1
    print(f"ok: {len(paths)} markdown files, links resolve, language split holds")
    return 0


if __name__ == "__main__":
    sys.exit(main())
