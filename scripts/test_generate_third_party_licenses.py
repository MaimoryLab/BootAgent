from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from scripts.generate_third_party_licenses import (
    ASSET_RIGHTS_MANIFEST,
    Dependency,
    ICON_ROOT,
    collect_asset_dependencies,
    compare_trees,
    copy_license_files,
    parse_json_stream,
    render_notice,
)


class ParseJSONStreamTests(unittest.TestCase):
    def test_parses_concatenated_go_list_objects(self) -> None:
        payload = '{"ImportPath":"one","Module":{"Path":"example.com/a","Version":"v1.0.0"}}\n' \
            '{"ImportPath":"two","Module":{"Path":"example.com/b","Version":"v2.0.0"}}\n'

        values = list(parse_json_stream(payload))

        self.assertEqual([value["ImportPath"] for value in values], ["one", "two"])


class LicenseCopyTests(unittest.TestCase):
    def test_normalizes_line_endings_and_trailing_whitespace(self) -> None:
        with tempfile.TemporaryDirectory() as source_dir, tempfile.TemporaryDirectory() as output_dir:
            source = Path(source_dir)
            output = Path(output_dir)
            (source / "LICENSE").write_bytes(b"Line one  \r\nLine two\r\n\r\n")

            copied = copy_license_files(source, output / "licenses" / "pkg", output)

            self.assertEqual(copied, ("licenses/pkg/LICENSE",))
            self.assertEqual((output / "licenses/pkg/LICENSE").read_bytes(), b"Line one\nLine two\n")


class AssetDependencyTests(unittest.TestCase):
    def test_collects_every_rights_manifest_asset_and_copies_license_text(self) -> None:
        # Derived from the manifest rather than a hardcoded pair of names: this
        # reads the real repository file, so listing the assets literally meant
        # the test failed every time an icon was added or removed, which says
        # nothing about whether the bundle is correct.
        manifest = json.loads(ASSET_RIGHTS_MANIFEST.read_text(encoding="utf-8"))
        expected = sorted(manifest["assets"])

        with tempfile.TemporaryDirectory() as output_dir:
            dependencies = collect_asset_dependencies(Path(output_dir))
            self.assertEqual([dependency.name for dependency in dependencies], expected)
            for dependency in dependencies:
                self.assertEqual(dependency.license, "MIT")
                self.assertTrue(dependency.license_files)
                for license_file in dependency.license_files:
                    self.assertTrue((Path(output_dir) / license_file).is_file())

    def test_every_shipped_image_asset_is_accounted_for(self) -> None:
        # The generator walks the manifest, never the directory, so an asset file
        # that nobody registered is redistributed with no source, licence or hash
        # recorded -- and --check still passes. That is the failure this guards:
        # docs/distribution-compliance-policy.md makes the rights inventory a
        # release precondition.
        #
        # The three groups exist because the three cases carry different
        # evidence. A third-party file under `assets` needs a source, licence
        # text, owner and hash. Our own artwork under `firstPartyAssets` has none
        # of those to give -- it is covered by this repository's licence. A vendor
        # icon under `trademarkAssets` is used to identify a product rather than
        # redistributed as artwork, so there is no grant to record either; both
        # would assert a provenance they do not have if forced into `assets`.
        # What still matters is that no file is in none of the lists, which is
        # what this checks.
        manifest = json.loads(ASSET_RIGHTS_MANIFEST.read_text(encoding="utf-8"))
        accounted = {
            (ICON_ROOT / str(entry["file"])).resolve()
            for group in ("assets", "firstPartyAssets", "trademarkAssets")
            for entry in manifest.get(group, {}).values()
        }
        on_disk = {
            path.resolve()
            for path in (ICON_ROOT / "assets").iterdir()
            if path.is_file() and path.suffix.lower() in {".svg", ".png", ".jpg", ".jpeg", ".webp"}
        }

        unaccounted = sorted(path.name for path in on_disk - accounted)
        self.assertEqual(
            unaccounted,
            [],
            "image assets present but absent from asset-rights.json: "
            f"{unaccounted}. Register a third-party asset under `assets` with its "
            "source, licence, copyright owner and SHA-256; record our own artwork "
            "under `firstPartyAssets`; record a vendor icon used only to identify "
            "a product under `trademarkAssets`; or delete it.",
        )

    def test_first_party_assets_claim_no_third_party_rights(self) -> None:
        # The two groups must not blur together. A first-party entry carrying a
        # licence or owner field would read as an attribution, and a file listed
        # in both would make it ambiguous which basis applies.
        manifest = json.loads(ASSET_RIGHTS_MANIFEST.read_text(encoding="utf-8"))
        third_party_files = {str(entry["file"]) for entry in manifest["assets"].values()}
        for name, entry in manifest.get("firstPartyAssets", {}).items():
            self.assertNotIn(
                str(entry["file"]),
                third_party_files,
                f"{name} is listed as both first-party and third-party",
            )
            self.assertTrue(str(entry.get("note", "")).strip(), f"{name} records no note")
            for field in ("source", "license", "licenseSource", "copyrightOwner", "sha256"):
                self.assertNotIn(
                    field,
                    entry,
                    f"{name} is first-party but carries the third-party field {field!r}",
                )


class NoticeRenderingTests(unittest.TestCase):
    def test_renders_target_platforms_and_license_paths_deterministically(self) -> None:
        dependencies = [
            Dependency(
                ecosystem="go",
                name="example.com/windows-only",
                version="v1.2.3",
                license="MIT",
                platforms=("windows",),
                license_files=("licenses/go/example.com_windows-only@v1.2.3/LICENSE",),
            ),
            Dependency(
                ecosystem="npm",
                name="react",
                version="19.2.8",
                license="MIT",
                platforms=("frontend",),
                license_files=("licenses/npm/react@19.2.8/LICENSE",),
            ),
        ]

        notice = render_notice(dependencies)

        self.assertIn("`example.com/windows-only` | `v1.2.3` | windows | MIT", notice)
        self.assertIn("`react` | `19.2.8` | frontend | MIT", notice)
        self.assertLess(notice.index("example.com/windows-only"), notice.index("react"))


class TreeComparisonTests(unittest.TestCase):
    def test_reports_missing_and_changed_generated_files(self) -> None:
        with tempfile.TemporaryDirectory() as actual_dir, tempfile.TemporaryDirectory() as expected_dir:
            actual = Path(actual_dir)
            expected = Path(expected_dir)
            (actual / "same.txt").write_text("same", encoding="utf-8")
            (expected / "same.txt").write_text("same", encoding="utf-8")
            (actual / "changed.txt").write_text("old", encoding="utf-8")
            (expected / "changed.txt").write_text("new", encoding="utf-8")
            (expected / "missing.txt").write_text("missing", encoding="utf-8")

            differences = compare_trees(actual, expected)

        self.assertIn("changed: changed.txt", differences)
        self.assertIn("missing: missing.txt", differences)


if __name__ == "__main__":
    unittest.main()
