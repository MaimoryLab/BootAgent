import io
import unittest
import urllib.error
import urllib.request
from unittest import mock

from scripts.sync_gitee_ota_release import (
    ATTEMPTS,
    RequestFailed,
    gitee_release,
    request_json,
    selected_assets,
    wanted_asset,
    with_retry,
)


def respond(payload: bytes):
    """Stand in for urlopen, answering every request with HTTP 200 and payload."""
    response = mock.MagicMock()
    response.__enter__.return_value.read.return_value = payload
    return mock.patch("scripts.sync_gitee_ota_release.urllib.request.urlopen", return_value=response)


class SyncGiteeOTAReleaseTests(unittest.TestCase):
    def test_only_updater_assets_are_selected(self) -> None:
        release = {
            "assets": [
                {"name": "BootAgent-darwin-arm64.dmg"},
                {"name": "ota-BootAgent-darwin-arm64.zip"},
                {"name": "ota-BootAgent-windows-amd64.zip"},
                {"name": "SHA256SUMS"},
                {"name": "v0.8.5.zip"},
            ]
        }
        self.assertEqual(
            [asset["name"] for asset in selected_assets(release)],
            ["ota-BootAgent-darwin-arm64.zip", "ota-BootAgent-windows-amd64.zip", "SHA256SUMS"],
        )

    def test_rejects_similarly_named_files(self) -> None:
        self.assertFalse(wanted_asset("ota-BootAgent-darwin-amd64.zip.sig"))
        self.assertFalse(wanted_asset("ota-other-linux-amd64.zip"))
        self.assertFalse(wanted_asset("BootAgent-linux-amd64.deb"))


class RetryTests(unittest.TestCase):
    """Gitee returns 403 intermittently, so a transient failure must not fail the sync."""

    def test_retries_a_transient_status_then_succeeds(self) -> None:
        attempts = []

        def action() -> str:
            attempts.append(None)
            if len(attempts) < 3:
                raise RequestFailed("Gitee said no", 403)
            return "uploaded"

        with mock.patch("scripts.sync_gitee_ota_release.time.sleep"):
            self.assertEqual(with_retry("upload", action), "uploaded")
        self.assertEqual(len(attempts), 3)

    def test_gives_up_after_the_attempt_limit(self) -> None:
        attempts = []

        def action() -> None:
            attempts.append(None)
            raise RequestFailed("Gitee is down", 502)

        with mock.patch("scripts.sync_gitee_ota_release.time.sleep"):
            with self.assertRaises(RequestFailed):
                with_retry("upload", action)
        self.assertEqual(len(attempts), ATTEMPTS)

    def test_does_not_retry_a_configuration_error(self) -> None:
        """A bad token answers the same way every time; retrying only delays the failure."""
        attempts = []

        def action() -> None:
            attempts.append(None)
            raise RequestFailed("unauthorized", 401)

        with mock.patch("scripts.sync_gitee_ota_release.time.sleep"):
            with self.assertRaises(RequestFailed):
                with_retry("upload", action)
        self.assertEqual(len(attempts), 1)

    def test_retries_a_connection_drop_that_carries_no_status(self) -> None:
        attempts = []

        def action() -> str:
            attempts.append(None)
            if len(attempts) < 2:
                raise RequestFailed("connection reset", None)
            return "done"

        with mock.patch("scripts.sync_gitee_ota_release.time.sleep"):
            self.assertEqual(with_retry("download", action), "done")
        self.assertEqual(len(attempts), 2)


class GiteeReleaseTests(unittest.TestCase):
    def test_a_tag_without_a_release_reads_as_missing(self) -> None:
        """Gitee answers HTTP 200 with `null` here, observed for v0.8.6 before its first sync."""
        with respond(b"null"):
            self.assertIsNone(gitee_release("maimory", "BootAgent", "v0.8.6", "token"))

    def test_a_404_reads_as_missing(self) -> None:
        error = urllib.error.HTTPError("https://gitee.com", 404, "Not Found", {}, io.BytesIO(b"{}"))
        with mock.patch("scripts.sync_gitee_ota_release.urllib.request.urlopen", side_effect=error):
            self.assertIsNone(gitee_release("maimory", "BootAgent", "v0.8.6", "token"))

    def test_an_existing_release_is_returned(self) -> None:
        with respond(b'{"id": 1132764, "tag_name": "v0.8.5", "assets": []}'):
            release = gitee_release("maimory", "BootAgent", "v0.8.5", "token")
        self.assertEqual(release["id"], 1132764)

    def test_a_non_object_response_is_reported_not_crashed_on(self) -> None:
        """The message must name the request; Request has no .method unless one was passed."""
        request = urllib.request.Request("https://gitee.com/api/v5/x")
        with respond(b"[]"):
            with self.assertRaisesRegex(RuntimeError, r"^GET https://gitee\.com/api/v5/x returned a non-object"):
                request_json(request)


if __name__ == "__main__":
    unittest.main()
