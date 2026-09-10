import unittest

from scripts.sync_gitee_ota_release import selected_assets, wanted_asset


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


if __name__ == "__main__":
    unittest.main()
