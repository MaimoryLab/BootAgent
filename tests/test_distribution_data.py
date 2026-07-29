from __future__ import annotations

import hashlib
import json
import tempfile
import unittest
from pathlib import Path

from scripts.build_release_index import ReleaseIndexError, build_release_index, copy_available_release_assets
from scripts.build_site_catalog import SiteCatalogError, build_site_catalog


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class ReleaseIndexTests(unittest.TestCase):
    def write_fixture(self, root: Path, *, artifact_bytes: bytes = b"oneagent") -> tuple[Path, Path]:
        release_dir = root / "release"
        release_dir.mkdir()
        artifact = release_dir / "OneAgent-0.2.0-dev-technical-preview-unsigned-macos-arm64.zip"
        artifact.write_bytes(artifact_bytes)
        manifest = {
            "schema_version": 1,
            "oneagent_version": "0.2.0-dev",
            "channel": "technical-preview-unsigned",
            "unsigned": True,
            "platform": "macos",
            "arch": "arm64",
            "python": "3.12.13",
            "built_at": "2026-07-26T10:14:28Z",
            "agent_versions": {"codex": "0.145.0"},
            "artifacts": [
                {"file": artifact.name, "sha256": digest(artifact), "bytes": artifact.stat().st_size}
            ],
        }
        manifest_path = release_dir / "release-manifest-macos-arm64.json"
        manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        (release_dir / "SHA256SUMS-macos-arm64.txt").write_text(
            f"{digest(artifact)}  {artifact.name}\n{digest(manifest_path)}  {manifest_path.name}\n",
            encoding="utf-8",
        )
        channels = {
            "schema_version": 1,
            "product": {
                "name": "OneAgent",
                "tagline": "用一个可信的本地流程，激活你自己的 Agent、账号和 Provider。",
            },
            "channels": {
                "technical-preview-unsigned": {
                    "label": "未签名技术预览版",
                    "published_at": "2026-07-28T00:00:00Z",
                    "targets": [
                        {
                            "platform": "macos",
                            "arch": "arm64",
                            "status": "available",
                            "verification": {
                                "native_build": True,
                                "cleanroom": "verified",
                                "evidence": "security/#release-evidence",
                            },
                            "mirrors": [
                                {
                                    "id": "website",
                                    "label": "官网下载",
                                    "kind": "official",
                                    "url": "downloads/{file}",
                                    "primary": True,
                                }
                            ],
                        },
                        {
                            "platform": "windows",
                            "arch": "x64",
                            "status": "verification-pending",
                            "verification": {
                                "native_build": False,
                                "cleanroom": "not-recorded",
                                "evidence": None,
                            },
                            "mirrors": [],
                        },
                    ],
                },
                "stable": {"label": "Stable", "published_at": None, "targets": []},
            },
        }
        channels_path = root / "channels.json"
        channels_path.write_text(json.dumps(channels, indent=2) + "\n", encoding="utf-8")
        return release_dir, channels_path

    def test_builds_a_public_index_from_verified_manifests(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            index = build_release_index(release_dir, channels_path)

        preview = next(channel for channel in index["channels"] if channel["channel"] == "technical-preview-unsigned")
        self.assertEqual(index["latest"]["technical-preview-unsigned"], "0.2.0-dev")
        self.assertIsNone(index["latest"]["stable"])
        self.assertTrue(preview["unsigned"])
        macos = next(target for target in preview["targets"] if target["id"] == "macos-arm64")
        self.assertEqual(macos["status"], "available")
        self.assertEqual(macos["artifacts"][0]["downloads"][0]["url"], f"downloads/{macos['artifacts'][0]['file']}")
        self.assertEqual(macos["artifacts"][0]["kind"], "binary")
        windows = next(target for target in preview["targets"] if target["id"] == "windows-x64")
        self.assertEqual(windows["artifacts"], [])

    def test_rejects_an_available_target_without_cleanroom_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            channels = json.loads(channels_path.read_text(encoding="utf-8"))
            channels["channels"]["technical-preview-unsigned"]["targets"][0]["verification"]["cleanroom"] = "not-recorded"
            channels_path.write_text(json.dumps(channels), encoding="utf-8")
            with self.assertRaisesRegex(ReleaseIndexError, "cleanroom"):
                build_release_index(release_dir, channels_path)

    def test_rejects_hash_drift_between_manifest_and_artifact(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            artifact = next(release_dir.glob("OneAgent-*.zip"))
            artifact.write_bytes(b"tampered")
            with self.assertRaisesRegex(ReleaseIndexError, "SHA-256"):
                build_release_index(release_dir, channels_path)

    def test_rejects_an_available_target_without_a_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            (release_dir / "release-manifest-macos-arm64.json").unlink()
            with self.assertRaisesRegex(ReleaseIndexError, "manifest"):
                build_release_index(release_dir, channels_path)

    def test_rejects_an_external_mirror_without_the_verified_artifact_hash(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            channels = json.loads(channels_path.read_text(encoding="utf-8"))
            channels["channels"]["technical-preview-unsigned"]["targets"][0]["mirrors"].append(
                {
                    "id": "domestic-mirror",
                    "label": "国内镜像",
                    "kind": "mirror",
                    "url": "https://downloads.example.com/{file}",
                    "verified_sha256": "0" * 64,
                    "primary": False,
                    "audit": {
                        "uploaded_by": "release-manager",
                        "uploaded_at": "2026-07-28T01:00:00Z",
                        "verified_at": "2026-07-28T01:05:00Z",
                        "withdrawal_owner": "security@example.com",
                        "withdrawn": False,
                        "withdrawn_at": None,
                    },
                }
            )
            channels_path.write_text(json.dumps(channels), encoding="utf-8")
            with self.assertRaisesRegex(ReleaseIndexError, "External mirror SHA-256"):
                build_release_index(release_dir, channels_path)

    def test_rejects_an_available_target_without_one_primary_official_channel(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            channels = json.loads(channels_path.read_text(encoding="utf-8"))
            channels["channels"]["technical-preview-unsigned"]["targets"][0]["mirrors"][0]["primary"] = False
            channels_path.write_text(json.dumps(channels), encoding="utf-8")
            with self.assertRaisesRegex(ReleaseIndexError, "primary official"):
                build_release_index(release_dir, channels_path)

    def test_rejects_an_available_channel_without_a_publish_timestamp(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            channels = json.loads(channels_path.read_text(encoding="utf-8"))
            channels["channels"]["technical-preview-unsigned"]["published_at"] = None
            channels_path.write_text(json.dumps(channels), encoding="utf-8")
            with self.assertRaisesRegex(ReleaseIndexError, "published_at"):
                build_release_index(release_dir, channels_path)

    def test_rejects_a_download_url_that_escapes_the_site_base(self):
        with tempfile.TemporaryDirectory() as tmp:
            release_dir, channels_path = self.write_fixture(Path(tmp))
            channels = json.loads(channels_path.read_text(encoding="utf-8"))
            channels["channels"]["technical-preview-unsigned"]["targets"][0]["mirrors"][0]["url"] = "../{file}"
            channels_path.write_text(json.dumps(channels), encoding="utf-8")
            with self.assertRaisesRegex(ReleaseIndexError, "download root"):
                build_release_index(release_dir, channels_path)

    def test_public_release_assets_only_include_available_targets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            release_dir, channels_path = self.write_fixture(root)
            (release_dir / "unverified-windows.zip").write_bytes(b"not public")
            index = build_release_index(release_dir, channels_path)
            destination = root / "public-release"
            copy_available_release_assets(index, release_dir, destination)

            self.assertEqual(
                {path.name for path in destination.iterdir()},
                {
                    "OneAgent-0.2.0-dev-technical-preview-unsigned-macos-arm64.zip",
                    "release-manifest-macos-arm64.json",
                    "SHA256SUMS-macos-arm64.txt",
                    "release-index.json",
                },
            )


class SiteCatalogTests(unittest.TestCase):
    def test_projects_agent_support_without_turning_guides_into_managed_installs(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            lock_path = root / "agents.lock.json"
            lock_path.write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "agents": {
                            "managed": {
                                "name": "Managed",
                                "group": "auto",
                                "config_mode": "auto",
                                "config_adapter": "codex",
                                "rank": 2,
                                "platforms": ["macos"],
                                "package": {"version": "1.0.0", "source": "https://example.com", "license": "MIT"},
                            },
                            "guided": {
                                "name": "Guided",
                                "group": "platform",
                                "config_mode": "guide",
                                "rank": 1,
                                "platforms": ["macos", "windows"],
                                "guide": "Use the official installer.",
                            },
                        },
                    }
                ),
                encoding="utf-8",
            )
            provider_path = root / "providers.json"
            provider_path.write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "providers": {
                            "example": {
                                "name": "Example",
                                "home": "https://example.com",
                                "relationship": "referral",
                                "disclosure": "商业合作",
                                "referral_url": "https://example.com/ref",
                                "protocols": {"openai": "implementation-supported"},
                            }
                        },
                    }
                ),
                encoding="utf-8",
            )
            catalog = build_site_catalog(lock_path, provider_path)

        self.assertEqual([agent["id"] for agent in catalog["agents"]], ["guided", "managed"])
        guided = catalog["agents"][0]
        self.assertFalse(guided["support"]["managedInstall"])
        self.assertTrue(guided["support"]["officialInstallGuide"])
        self.assertFalse(guided["support"]["managedConfig"])
        self.assertIsNone(guided["protocol"])
        managed = catalog["agents"][1]
        self.assertEqual(managed["protocol"], "responses")
        self.assertTrue(managed["support"]["managedInstall"])
        self.assertTrue(managed["support"]["managedConfig"])
        self.assertEqual(catalog["providers"][0]["relationship"], "referral")

    def test_rejects_undisclosed_or_unsafe_provider_links(self):
        lock_path = Path(__file__).resolve().parents[1] / "agents.lock.json"
        with tempfile.TemporaryDirectory() as tmp:
            provider_path = Path(tmp) / "providers.json"
            provider_path.write_text(
                json.dumps(
                    {
                        "schema_version": 1,
                        "providers": {
                            "unsafe": {
                                "name": "Unsafe",
                                "home": "javascript:alert(1)",
                                "relationship": "none",
                                "disclosure": "",
                                "referral_url": "https://example.com/ref",
                                "protocols": {"openai": "implementation-supported"},
                            }
                        },
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(SiteCatalogError, "undisclosed commercial fields"):
                build_site_catalog(lock_path, provider_path)
            payload = json.loads(provider_path.read_text(encoding="utf-8"))
            payload["providers"]["unsafe"]["referral_url"] = ""
            provider_path.write_text(json.dumps(payload), encoding="utf-8")
            with self.assertRaisesRegex(SiteCatalogError, "must use HTTPS"):
                build_site_catalog(lock_path, provider_path)


if __name__ == "__main__":
    unittest.main()
