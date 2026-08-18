#!/usr/bin/env python3
"""Mirror one GitHub release's user-facing artifacts to Gitee.

Why this exists rather than a ready-made action: uploads to Gitee from a GitHub
runner measured ~15 kB/s. The wholesale action we used first pushes every
attachment of every release serially, so a run needed hours and was always
cancelled part way -- five runs in a row, never one success, and the release on
Gitee ended up missing SHA256SUMS and two artifacts.

Two decisions make the transfer finish inside a job:

* Only what a person downloads is mirrored. The in-app updater reads GitHub
  directly (cmd/bootagent-desktop/main_wails.go pins MaimoryLab/BootAgent), so
  the six ota-*.zip files no one fetches by hand are 42 of the release's 104 MiB
  and buy nothing here.
* Already-uploaded names are skipped, so a cancelled or timed-out run resumes
  instead of starting over. That is what makes a slow link survivable.

SHA256SUMS goes first because the download page's integrity copy points at it,
and it is the one file that must never be the casualty of a truncated run.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

GITEE_API = "https://gitee.com/api/v5"
GITHUB_API = "https://api.github.com"

# Container formats a person installs from, plus the checksum manifest. Anything
# else in a release -- ota-*.zip today -- is machine-facing and stays on GitHub.
INSTALLER_SUFFIXES = (".dmg", ".exe", ".deb", ".rpm", ".appimage", ".msi", ".pkg")
CHECKSUM_NAME = "SHA256SUMS"


def wanted(name: str) -> bool:
    if name == CHECKSUM_NAME:
        return True
    # ota- artifacts can carry an installer suffix on some platforms, so the
    # prefix is checked before the suffix rather than after.
    if name.startswith("ota-"):
        return False
    return name.lower().endswith(INSTALLER_SUFFIXES)


def upload_order(names: list[str]) -> list[str]:
    """Checksums first; the rest alphabetical so a partial run is predictable."""
    return sorted(names, key=lambda name: (name != CHECKSUM_NAME, name))


def request_json(url: str, token: str | None = None) -> dict | list:
    req = urllib.request.Request(url, headers={"Accept": "application/json"})
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=60) as response:
        return json.load(response)


def github_release(repo: str, tag: str, token: str | None) -> dict:
    return request_json(f"{GITHUB_API}/repos/{repo}/releases/tags/{tag}", token)  # type: ignore[return-value]


def gitee_release(owner: str, repo: str, tag: str, token: str) -> dict | None:
    query = urllib.parse.urlencode({"access_token": token})
    try:
        return request_json(f"{GITEE_API}/repos/{owner}/{repo}/releases/tags/{tag}?{query}")  # type: ignore[return-value]
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None
        raise


def create_gitee_release(owner: str, repo: str, tag: str, name: str, body: str, token: str) -> dict:
    payload = urllib.parse.urlencode({
        "access_token": token,
        "tag_name": tag,
        "name": name or tag,
        # Gitee rejects an empty body, and the tag is the honest minimum.
        "body": body or tag,
        "target_commitish": "main",
    }).encode()
    req = urllib.request.Request(f"{GITEE_API}/repos/{owner}/{repo}/releases", data=payload, method="POST")
    with urllib.request.urlopen(req, timeout=60) as response:
        return json.load(response)


def multipart_body(field: str, filename: str, payload: bytes, fields: dict[str, str]) -> tuple[bytes, str]:
    boundary = "----BootAgentGiteeSync"
    parts: list[bytes] = []
    for key, value in fields.items():
        parts.append(
            f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode()
        )
    parts.append(
        f'--{boundary}\r\nContent-Disposition: form-data; name="{field}"; filename="{filename}"\r\n'
        f"Content-Type: application/octet-stream\r\n\r\n".encode()
    )
    parts.append(payload)
    parts.append(f"\r\n--{boundary}--\r\n".encode())
    return b"".join(parts), f"multipart/form-data; boundary={boundary}"


def download(url: str, token: str | None) -> bytes:
    # The browser URL is public for a public repo; the token is sent anyway so a
    # private release works without a second code path.
    req = urllib.request.Request(url, headers={"Accept": "application/octet-stream"})
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    with urllib.request.urlopen(req, timeout=600) as response:
        return response.read()


def upload(owner: str, repo: str, release_id: int, name: str, payload: bytes, token: str) -> None:
    body, content_type = multipart_body("file", name, payload, {"access_token": token})
    req = urllib.request.Request(
        f"{GITEE_API}/repos/{owner}/{repo}/releases/{release_id}/attach_files",
        data=body,
        method="POST",
    )
    req.add_header("Content-Type", content_type)
    with urllib.request.urlopen(req, timeout=1800) as response:
        response.read()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True, help="release tag present on GitHub")
    parser.add_argument("--github-repo", default="MaimoryLab/BootAgent")
    parser.add_argument("--gitee-owner", default="maimory")
    parser.add_argument("--gitee-repo", default="BootAgent")
    parser.add_argument("--dry-run", action="store_true", help="report the plan and upload nothing")
    args = parser.parse_args()

    gitee_token = os.environ.get("GITEE_TOKEN", "")
    if not gitee_token and not args.dry_run:
        print("GITEE_TOKEN is not set", file=sys.stderr)
        return 2
    github_token = os.environ.get("GITHUB_TOKEN") or None

    source = github_release(args.github_repo, args.tag, github_token)
    mirrorable = {asset["name"]: asset["browser_download_url"] for asset in source.get("assets", []) if wanted(asset["name"])}
    if not mirrorable:
        print(f"{args.tag}: no user-facing artifacts on GitHub", file=sys.stderr)
        return 1

    if args.dry_run:
        skipped = [asset["name"] for asset in source.get("assets", []) if not wanted(asset["name"])]
        print(f"{args.tag}: would mirror {len(mirrorable)}, skip {len(skipped)}")
        for name in upload_order(list(mirrorable)):
            print(f"  mirror {name}")
        for name in sorted(skipped):
            print(f"  skip   {name}")
        return 0

    target = gitee_release(args.gitee_owner, args.gitee_repo, args.tag, gitee_token)
    if target is None:
        target = create_gitee_release(
            args.gitee_owner, args.gitee_repo, args.tag,
            source.get("name") or args.tag, source.get("body") or "", gitee_token,
        )
        print(f"{args.tag}: created release on Gitee")
    present = {asset["name"] for asset in target.get("assets", [])}
    release_id = int(target["id"])

    pending = [name for name in upload_order(list(mirrorable)) if name not in present]
    if not pending:
        print(f"{args.tag}: already complete on Gitee ({len(mirrorable)} artifacts)")
        return 0

    print(f"{args.tag}: {len(present & set(mirrorable))} present, uploading {len(pending)}")
    failures: list[str] = []
    for name in pending:
        try:
            payload = download(mirrorable[name], github_token)
            upload(args.gitee_owner, args.gitee_repo, release_id, name, payload, gitee_token)
            print(f"  uploaded {name} ({len(payload) // 1024} KiB)")
        except (urllib.error.URLError, OSError) as error:
            # Kept going rather than aborting: at this throughput a run that dies
            # on one artifact should still leave the others in place for the next
            # one to skip.
            print(f"  FAILED {name}: {error}", file=sys.stderr)
            failures.append(name)

    if failures:
        print(f"{args.tag}: {len(failures)} artifact(s) not uploaded; re-run to resume", file=sys.stderr)
        # The checksum manifest is what the download page's integrity copy points
        # at, so losing it is a failure even when everything else landed.
        return 1 if CHECKSUM_NAME in failures else 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
