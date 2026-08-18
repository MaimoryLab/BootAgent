"""Selection rules for the Gitee mirror.

The upload runs at roughly 15 kB/s from a GitHub runner, so what gets picked and
in which order is the whole difference between a run that finishes and the five
cancelled ones that left the release missing its checksum manifest.
"""

import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location(
    "sync_gitee_release", pathlib.Path(__file__).with_name("sync-gitee-release.py")
)
assert spec and spec.loader
sync = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sync)

# The v0.7.2 release, which is what exposed the problem.
RELEASE = [
    "BootAgent-darwin-amd64.dmg",
    "BootAgent-darwin-arm64.dmg",
    "BootAgent-linux-amd64.deb",
    "BootAgent-linux-amd64.rpm",
    "BootAgent-linux-arm64.deb",
    "BootAgent-linux-arm64.rpm",
    "BootAgent-windows-amd64-installer.exe",
    "BootAgent-windows-arm64-installer.exe",
    "SHA256SUMS",
    "ota-BootAgent-darwin-amd64.zip",
    "ota-BootAgent-darwin-arm64.zip",
    "ota-BootAgent-linux-amd64.zip",
    "ota-BootAgent-linux-arm64.zip",
    "ota-BootAgent-windows-amd64.zip",
    "ota-BootAgent-windows-arm64.zip",
]


class SelectionTest(unittest.TestCase):
    def test_every_installer_and_the_manifest_are_mirrored(self):
        picked = [name for name in RELEASE if sync.wanted(name)]
        self.assertEqual(len(picked), 9, picked)
        self.assertIn("SHA256SUMS", picked)
        for name in picked:
            self.assertFalse(name.startswith("ota-"), name)

    def test_ota_artifacts_are_never_mirrored(self):
        # They are 42 of the release's 104 MiB and the in-app updater reads
        # GitHub directly, so mirroring them only costs the run its time.
        for name in RELEASE:
            if name.startswith("ota-"):
                self.assertFalse(sync.wanted(name), name)

    def test_an_ota_artifact_with_an_installer_suffix_is_still_skipped(self):
        # The prefix has to be checked before the suffix, or a platform that ships
        # ota-*.exe would slip back in.
        self.assertFalse(sync.wanted("ota-BootAgent-windows-amd64.exe"))
        self.assertFalse(sync.wanted("ota-BootAgent-linux-amd64.deb"))

    def test_source_archives_are_not_mirrored(self):
        # Gitee generates these itself for every tag; uploading them would both
        # duplicate and waste the budget.
        self.assertFalse(sync.wanted("v0.7.2.zip"))
        self.assertFalse(sync.wanted("v0.7.2.tar.gz"))

    def test_the_manifest_is_uploaded_before_any_installer(self):
        # A truncated run must not be the reason the download page's integrity
        # copy points at a missing file.
        order = sync.upload_order([name for name in RELEASE if sync.wanted(name)])
        self.assertEqual(order[0], "SHA256SUMS")

    def test_order_is_otherwise_stable(self):
        # So a resumed run picks up predictably rather than in API order.
        order = sync.upload_order(["BootAgent-linux-arm64.rpm", "BootAgent-darwin-amd64.dmg", "SHA256SUMS"])
        self.assertEqual(order, ["SHA256SUMS", "BootAgent-darwin-amd64.dmg", "BootAgent-linux-arm64.rpm"])

    def test_case_is_not_a_way_past_the_filter(self):
        self.assertTrue(sync.wanted("BootAgent-darwin-arm64.DMG"))
        self.assertFalse(sync.wanted("BootAgent-notes.txt"))


if __name__ == "__main__":
    unittest.main()
