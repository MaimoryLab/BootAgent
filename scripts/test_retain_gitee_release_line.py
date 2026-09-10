import os
import sys
import unittest
from unittest.mock import call, patch

sys.path.insert(0, os.path.dirname(__file__))
import retain_gitee_release_line as policy  # noqa: E402


class RetainGiteeReleaseLineTests(unittest.TestCase):
    def test_release_line(self):
        self.assertEqual(policy.release_line("v0.8.4"), (0, 8))
        self.assertIsNone(policy.release_line("0.8.4"))
        self.assertIsNone(policy.release_line("v0.8"))

    @patch.dict(os.environ, {"GITEE_TOKEN": "test-token"})
    @patch.object(policy.GiteeAPI, "delete_release")
    @patch.object(policy.GiteeAPI, "releases", return_value=[
        {"id": 1, "tag_name": "v0.7.3"},
        {"id": 2, "tag_name": "v0.8.0"},
        {"id": 3, "tag_name": "v0.9.0"},
        {"id": 4, "tag_name": "draft"},
    ])
    def test_apply_deletes_only_older_release_lines(self, _releases, delete):
        with patch.object(sys, "argv", ["retain", "--version", "v0.8.4", "--apply"]):
            self.assertEqual(policy.main(), 0)
        self.assertEqual(delete.call_args_list, [call(1), call(3)])


if __name__ == "__main__":
    unittest.main()
