import unittest
from unittest import mock

from scripts.sync_gitee_ota_release import (
    ATTEMPTS,
    RequestFailed,
    selected_assets,
    wanted_asset,
    with_retry,
)


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


if __name__ == "__main__":
    unittest.main()
