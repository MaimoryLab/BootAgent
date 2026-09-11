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
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

TAG_RE = re.compile(r"^v\d+\.\d+\.\d+$")
OTA_ASSET_RE = re.compile(r"^ota-BootAgent-(?:darwin|linux|windows)-(?:amd64|arm64)\.zip$")
GITHUB_API = "https://api.github.com"
GITEE_API = "https://gitee.com/api/v5"
# Gitee answers intermittently: BootAgent's own updater notes roughly a third of
# consecutive SHA256SUMS fetches returning 403. The action this script replaced
# retried three times, so transient failures must not fail the release sync.
ATTEMPTS = 3
RETRY_DELAY_SECONDS = 5
# Server-side and rate-limit responses are worth another attempt; a 401/404 is a
# configuration error that will answer the same way every time.
RETRYABLE_STATUSES = frozenset({403, 408, 429, 500, 502, 503, 504})


def wanted_asset(name: str) -> bool:
    return name == "SHA256SUMS" or OTA_ASSET_RE.fullmatch(name) is not None


def selected_assets(release: dict[str, object]) -> list[dict[str, object]]:
    assets = release.get("assets", [])
    if not isinstance(assets, list):
        raise RuntimeError("GitHub release assets response was not a list")
    return [asset for asset in assets if isinstance(asset, dict) and wanted_asset(str(asset.get("name", "")))]


class RequestFailed(RuntimeError):
    """An HTTP request that failed, carrying the status so retry can judge it."""

    def __init__(self, message: str, status: int | None) -> None:
        super().__init__(message)
        self.status = status

    def retryable(self) -> bool:
        return self.status is None or self.status in RETRYABLE_STATUSES


def with_retry(description: str, action):
    """Run action, retrying the failures Gitee produces intermittently."""
    for attempt in range(1, ATTEMPTS + 1):
        try:
            return action()
        except RequestFailed as error:
            if attempt == ATTEMPTS or not error.retryable():
                raise
            print(f"{description} failed (attempt {attempt}/{ATTEMPTS}), retrying: {error}")
            time.sleep(RETRY_DELAY_SECONDS * attempt)
    raise AssertionError("unreachable")


def request_json(request: urllib.request.Request, timeout: float = 60) -> dict[str, object]:
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = response.read()
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", errors="replace")
        raise RequestFailed(
            f"{request.method} {request.full_url} failed ({error.code}): {body[:500]}", error.code
        ) from error
    except (OSError, urllib.error.URLError) as error:
        # A dropped connection has no status; it is the transient case retry exists for.
        raise RequestFailed(f"{request.method} {request.full_url} failed: {error}", None) from error
    result = json.loads(payload) if payload else {}
    if not isinstance(result, dict):
        raise RuntimeError(f"{request.method} {request.full_url} returned a non-object response")
    return result


def github_release(owner: str, repo: str, tag: str, token: str) -> dict[str, object]:
    headers = {"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    url = f"{GITHUB_API}/repos/{owner}/{repo}/releases/tags/{urllib.parse.quote(tag)}"
    return with_retry(
        "read GitHub release", lambda: request_json(urllib.request.Request(url, headers=headers))
    )


def gitee_release(owner: str, repo: str, tag: str, token: str) -> dict[str, object] | None:
    url = f"{GITEE_API}/repos/{owner}/{repo}/releases/tags/{urllib.parse.quote(tag)}"
    request = urllib.request.Request(url, headers={"Authorization": f"token {token}", "Accept": "application/json"})
    try:
        return with_retry("read Gitee release", lambda: request_json(request))
    except RequestFailed as error:
        # No release for this tag yet, which is the normal first-sync case.
        if error.status == 404:
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
    return with_retry(
        "create Gitee release",
        lambda: request_json(urllib.request.Request(url, data=values, method="POST")),
    )


def download_asset(asset: dict[str, object], directory: Path, token: str) -> Path:
    name = str(asset["name"])
    url = str(asset["browser_download_url"])
    headers = {"Accept": "application/octet-stream"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    target = directory / name
    request = urllib.request.Request(url, headers=headers)

    def fetch() -> None:
        try:
            with urllib.request.urlopen(request, timeout=120) as response, target.open("wb") as output:
                while chunk := response.read(1024 * 1024):
                    output.write(chunk)
        except urllib.error.HTTPError as error:
            target.unlink(missing_ok=True)
            raise RequestFailed(f"failed to download GitHub asset {name}: {error}", error.code) from error
        except (OSError, urllib.error.URLError) as error:
            # A partial file must not survive: the next attempt reopens it with
            # "wb", but a failure that exhausts the retries would otherwise leave
            # a truncated asset behind for the upload step to send.
            target.unlink(missing_ok=True)
            raise RequestFailed(f"failed to download GitHub asset {name}: {error}", None) from error

    with_retry(f"download {name}", fetch)
    return target


def multipart_envelope(field: str, path: Path, token: str) -> tuple[bytes, bytes, str]:
    """Return the multipart prefix and suffix that wrap the file's bytes."""
    boundary = f"bootagent-{secrets.token_hex(16)}"
    content_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
    prefix = (
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"access_token\"\r\n\r\n{token}\r\n"
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"{field}\"; filename=\"{path.name}\"\r\n"
        f"Content-Type: {content_type}\r\n\r\n"
    ).encode()
    suffix = f"\r\n--{boundary}--\r\n".encode()
    return prefix, suffix, boundary


def upload_asset(owner: str, repo: str, token: str, release_id: object, path: Path) -> None:
    """Attach one file to a Gitee release, streaming it rather than buffering it.

    The body is assembled on disk and handed to urllib as an open file, with an
    explicit Content-Length: urllib cannot size a file object itself, and without
    the header it would fall back to chunked encoding, which Gitee's endpoint
    does not accept. An OTA zip is over 100 MB, so reading it into memory to
    build the request costs more than the request itself.
    """
    prefix, suffix, boundary = multipart_envelope("file", path, token)
    url = f"{GITEE_API}/repos/{owner}/{repo}/releases/{release_id}/attach_files"
    with tempfile.NamedTemporaryFile(prefix="bootagent-upload-", suffix=".multipart") as body:
        body.write(prefix)
        with path.open("rb") as source:
            while chunk := source.read(1024 * 1024):
                body.write(chunk)
        body.write(suffix)
        length = body.tell()

        def send() -> dict[str, object]:
            # Rewound per attempt: a retry has to resend from the start.
            body.seek(0)
            return request_json(
                urllib.request.Request(
                    url,
                    data=body,
                    method="POST",
                    headers={
                        "Content-Type": f"multipart/form-data; boundary={boundary}",
                        "Content-Length": str(length),
                    },
                ),
                timeout=30 * 60,
            )

        with_retry(f"upload {path.name}", send)


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
