#!/usr/bin/env python3
"""Mirror one GitHub release's OTA assets to Gitee.

Only the files used by BootAgent's updater are mirrored. This deliberately does
not enumerate historical GitHub releases or upload platform installers, keeping
Gitee's cumulative release attachment usage bounded.
"""

from __future__ import annotations

import argparse
import json
import mimetypes
import os
import re
import secrets
import tempfile
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

TAG_RE = re.compile(r"^v\d+\.\d+\.\d+$")
OTA_ASSET_RE = re.compile(r"^ota-BootAgent-(?:darwin|linux|windows)-(?:amd64|arm64)\.zip$")
GITHUB_API = "https://api.github.com"
GITEE_API = "https://gitee.com/api/v5"


def wanted_asset(name: str) -> bool:
    return name == "SHA256SUMS" or OTA_ASSET_RE.fullmatch(name) is not None


def selected_assets(release: dict[str, object]) -> list[dict[str, object]]:
    assets = release.get("assets", [])
    if not isinstance(assets, list):
        raise RuntimeError("GitHub release assets response was not a list")
    return [asset for asset in assets if isinstance(asset, dict) and wanted_asset(str(asset.get("name", "")))]


def request_json(request: urllib.request.Request) -> dict[str, object]:
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            payload = response.read()
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{request.method} {request.full_url} failed ({error.code}): {body[:500]}") from error
    result = json.loads(payload) if payload else {}
    if not isinstance(result, dict):
        raise RuntimeError(f"{request.method} {request.full_url} returned a non-object response")
    return result


def github_release(owner: str, repo: str, tag: str, token: str) -> dict[str, object]:
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    url = f"{GITHUB_API}/repos/{owner}/{repo}/releases/tags/{urllib.parse.quote(tag)}"
    return request_json(urllib.request.Request(url, headers=headers))


def gitee_release(owner: str, repo: str, tag: str, token: str) -> dict[str, object] | None:
    url = f"{GITEE_API}/repos/{owner}/{repo}/releases/tags/{urllib.parse.quote(tag)}"
    request = urllib.request.Request(url, headers={"Authorization": f"token {token}", "Accept": "application/json"})
    try:
        return request_json(request)
    except RuntimeError as error:
        if " failed (404):" in str(error):
            return None
        raise


def create_gitee_release(
    owner: str, repo: str, token: str, release: dict[str, object]
) -> dict[str, object]:
    values = urllib.parse.urlencode(
        {
            "access_token": token,
            "tag_name": release["tag_name"],
            "name": release.get("name") or release["tag_name"],
            "body": release.get("body") or "-",
            "target_commitish": release.get("target_commitish") or "main",
        }
    ).encode()
    url = f"{GITEE_API}/repos/{owner}/{repo}/releases"
    return request_json(urllib.request.Request(url, data=values, method="POST"))


def download_asset(asset: dict[str, object], directory: Path, token: str) -> Path:
    name = str(asset["name"])
    url = str(asset["browser_download_url"])
    headers = {"Accept": "application/octet-stream"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    target = directory / name
    request = urllib.request.Request(url, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=120) as response, target.open("wb") as output:
            while chunk := response.read(1024 * 1024):
                output.write(chunk)
    except (OSError, urllib.error.URLError) as error:
        target.unlink(missing_ok=True)
        raise RuntimeError(f"failed to download GitHub asset {name}: {error}") from error
    return target


def multipart_file(field: str, path: Path, token: str) -> tuple[bytes, str]:
    boundary = f"bootagent-{secrets.token_hex(16)}"
    content_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
    prefix = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"access_token\"\r\n\r\n{token}\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{path.name}\"\r\n"
        f"Content-Type: {content_type}\r\n\r\n"
    ).encode()
    suffix = f"\r\n--{boundary}--\r\n".encode()
    return prefix + path.read_bytes() + suffix, boundary


def upload_asset(owner: str, repo: str, token: str, release_id: object, path: Path) -> None:
    body, boundary = multipart_file("file", path, token)
    url = f"{GITEE_API}/repos/{owner}/{repo}/releases/{release_id}/attach_files"
    request_json(
        urllib.request.Request(
            url,
            data=body,
            method="POST",
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
        )
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--github-owner", default="MaimoryLab")
    parser.add_argument("--gitee-owner", default="maimory")
    parser.add_argument("--repo", default="BootAgent")
    args = parser.parse_args()

    if TAG_RE.fullmatch(args.version) is None:
        parser.error("version must match vX.Y.Z")
    gitee_token = os.environ.get("GITEE_TOKEN", "").strip()
    if not gitee_token:
        parser.error("GITEE_TOKEN is required")
    github_token = os.environ.get("GH_TOKEN", "").strip()

    source = github_release(args.github_owner, args.repo, args.version, github_token)
    assets = selected_assets(source)
    names = {str(asset["name"]) for asset in assets}
    if "SHA256SUMS" not in names or not any(name.startswith("ota-") for name in names):
        raise RuntimeError(f"GitHub release {args.version} is missing required OTA assets or SHA256SUMS")

    target = gitee_release(args.gitee_owner, args.repo, args.version, gitee_token)
    if target is None:
        target = create_gitee_release(args.gitee_owner, args.repo, gitee_token, source)
    existing = {
        str(asset.get("name", ""))
        for asset in target.get("assets", [])
        if isinstance(asset, dict)
    }
    pending = [asset for asset in assets if str(asset["name"]) not in existing]
    print(f"Syncing {len(pending)} missing OTA assets for {args.version}; {len(assets) - len(pending)} already present")

    with tempfile.TemporaryDirectory(prefix="bootagent-gitee-release-") as temp:
        directory = Path(temp)
        for asset in pending:
            path = download_asset(asset, directory, github_token)
            print(f"Uploading {path.name}")
            upload_asset(args.gitee_owner, args.repo, gitee_token, target["id"], path)
            path.unlink()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
