from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from scripts.generate_third_party_licenses import (
    Dependency,
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
    def test_collects_only_rights_manifest_assets_and_copies_license_text(self) -> None:
        with tempfile.TemporaryDirectory() as output_dir:
            dependencies = collect_asset_dependencies(Path(output_dir))
            self.assertEqual([dependency.name for dependency in dependencies], ["codex", "opencode"])
            for dependency in dependencies:
                self.assertEqual(dependency.license, "MIT")
                for license_file in dependency.license_files:
                    self.assertTrue((Path(output_dir) / license_file).is_file())


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
