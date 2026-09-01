#!/usr/bin/env python3
"""Tests for the offline promotion prompt builder."""

from __future__ import annotations

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MODULE_PATH = ROOT / "scripts" / "promotion" / "build_prompt.py"
spec = importlib.util.spec_from_file_location("build_prompt", MODULE_PATH)
assert spec and spec.loader
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class PromotionPromptBuilderTests(unittest.TestCase):
    def setUp(self) -> None:
        self.brief = json.loads(
            (ROOT / "outputs/promotion/v0.7.3/xiaohongshu/brief.json").read_text(encoding="utf-8")
        )

    def test_builds_chinese_poster_prompt_from_current_brief(self) -> None:
        result = builder.build_prompt(self.brief, "xiaohongshu")
        self.assertEqual(result["template_id"], "poster-layout-system")
        self.assertEqual(result["aspect_ratio"], "3:4")
        self.assertEqual(result["canvas"], "1080x1440")
        self.assertEqual(result["language"], "zh-CN")
        self.assertIn("第一次接触 AI 编程工具的小白用户", result["prompt"])
        self.assertIn("不要生成真实 Agent 图标", result["prompt"])
        self.assertEqual(result["upstream"]["commit"], builder.UPSTREAM_COMMIT)
        self.assertFalse(result["remote_generation"])

    def test_builds_x_prompt_with_different_channel_contract(self) -> None:
        result = builder.build_prompt(
            json.loads((ROOT / "outputs/promotion/v0.7.3/x/brief.json").read_text(encoding="utf-8")),
            "x",
        )
        self.assertEqual(result["aspect_ratio"], "16:9")
        self.assertEqual(result["canvas"], "1600x900")
        self.assertEqual(result["language"], "en")
        self.assertIn("10+ AI tools. One workspace.", result["prompt"])

    def test_missing_product_fields_are_rejected(self) -> None:
        brief = dict(self.brief)
        brief["product_visual"] = ""
        with self.assertRaisesRegex(ValueError, "product_visual"):
            builder.build_prompt(brief, "xiaohongshu")

    def test_invalid_release_tag_is_rejected(self) -> None:
        brief = dict(self.brief)
        brief["version"] = "latest"
        with self.assertRaisesRegex(ValueError, "verified release tag"):
            builder.build_prompt(brief, "xiaohongshu")

    def test_output_is_json_serializable_without_secrets(self) -> None:
        result = builder.build_prompt(self.brief, "xiaohongshu")
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "prompt.json"
            path.write_text(json.dumps(result, ensure_ascii=False), encoding="utf-8")
            loaded = json.loads(path.read_text(encoding="utf-8"))
        serialized = json.dumps(loaded, ensure_ascii=False).lower()
        self.assertNotIn("sk-test", serialized)
        self.assertNotIn("authorization: bearer", serialized)
        self.assertNotIn("cookie=", serialized)
        self.assertNotIn("api_key=", serialized)


if __name__ == "__main__":
    unittest.main()
