#!/usr/bin/env python3
"""Retain only the current MAJOR.MINOR release line in Gitee.

Gitee's release attachment quota is cumulative. The Git tags remain in the
repository, but old Gitee Release objects (and their OTA attachments) are
removed before the next release is mirrored. The script is intentionally
opt-in: without --apply it only prints the releases that would be deleted.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

TAG_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
API_ROOT = "https://gitee.com/api/v5"


def release_line(tag: str) -> tuple[int, int] | None:
    match = TAG_RE.fullmatch(tag.strip())
    return (int(match.group(1)), int(match.group(2))) if match else None


class GiteeAPI:
    def __init__(self, token: str, owner: str, repo: str) -> None:
        self.token = token
        self.base = f"{API_ROOT}/repos/{urllib.parse.quote(owner)}/{urllib.parse.quote(repo)}"

    def request(self, method: str, path: str) -> object:
        url = self.base + path
        request = urllib.request.Request(
            url,
            method=method,
            headers={"Authorization": f"token {self.token}", "Accept": "application/json"},
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                payload = response.read()
        except urllib.error.HTTPError as error:
            body = error.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"Gitee API {method} {path} failed ({error.code}): {body[:500]}") from error
        if not payload:
            return None
        return json.loads(payload)

    def releases(self) -> list[dict[str, object]]:
        result: list[dict[str, object]] = []
        page = 1
        while True:
            payload = self.request("GET", f"/releases?per_page=100&page={page}")
            if not isinstance(payload, list):
                raise RuntimeError("Gitee releases response was not a list")
            result.extend(item for item in payload if isinstance(item, dict))
            if len(payload) < 100:
                return result
            page += 1

    def delete_release(self, release_id: int) -> None:
        self.request("DELETE", f"/releases/{release_id}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True, help="release tag, for example v0.8.5")
    parser.add_argument("--owner", default="maimory")
    parser.add_argument("--repo", default="BootAgent")
    parser.add_argument("--apply", action="store_true", help="delete old releases; default is dry-run")
    args = parser.parse_args()

    line = release_line(args.version)
    if line is None:
        print("version must match vX.Y.Z", file=sys.stderr)
        return 2
    token = os.environ.get("GITEE_TOKEN", "").strip()
    if not token:
        print("GITEE_TOKEN is required", file=sys.stderr)
        return 2

    api = GiteeAPI(token, args.owner, args.repo)
    stale: list[tuple[int, str]] = []
    for release in api.releases():
        tag = str(release.get("tag_name", ""))
        release_id = release.get("id")
        if isinstance(release_id, int) and release_line(tag) not in (None, line):
            stale.append((release_id, tag))

    if not stale:
        print(f"Gitee release line v{line[0]}.{line[1]} already retained; nothing to delete")
        return 0
    action = "would delete" if not args.apply else "deleting"
    for release_id, tag in sorted(stale, key=lambda item: item[1]):
        print(f"{action} Gitee release {tag} (id={release_id})")
        if args.apply:
            api.delete_release(release_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
