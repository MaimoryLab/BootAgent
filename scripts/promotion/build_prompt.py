#!/usr/bin/env python3
"""Build an offline, auditable GPT-Image2 prompt from a PromotionBrief.

This intentionally consumes only the upstream style-library metadata. It does
not download images, call a remote image API, or read secrets.
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
LIBRARY = ROOT / "scripts" / "promotion" / "awesome-gpt-image-2" / "style-library.json"
UPSTREAM_COMMIT = "685469889fb72fd5adefae45e1645d527edcb5e7"
EXPECTED_LIBRARY_SHA256 = "80f5cae039d0d6f312f0e2de2c9b3fc8a806640b0d517c120d704a71c5e4aa72"

CHANNELS = {
    "xiaohongshu": {"aspect_ratio": "3:4", "size": "1080x1440", "language": "zh-CN"},
    "x": {"aspect_ratio": "16:9", "size": "1600x900", "language": "en"},
}

LOCAL_CONSTRAINTS = [
    "产品概念海报，不是产品截图，不复刻真实产品界面。",
    "不要生成真实 Agent 图标、品牌 Logo、厂商标志或未经确认的产品名称。",
    "不要生成 URL、二维码、API Key、Cookie、密码、水印、乱码或伪文字。",
    "所有元素必须完整位于画布内部，禁止裁切、溢出、漂浮、断裂、重复和错误透视。",
    "保持单一主视觉，避免 UI 卡片、面板拼贴、复杂仪表盘和信息过载。",
    "避免赛博朋克、霓虹、玻璃拟态、廉价 SaaS 模板和泛科技 stock image。",
]


def load_library() -> tuple[dict[str, Any], str]:
    raw = LIBRARY.read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    if digest != EXPECTED_LIBRARY_SHA256:
        raise ValueError(f"style library hash mismatch: {digest}")
    data = json.loads(raw)
    if data.get("version") != 1 or not isinstance(data.get("templates"), list):
        raise ValueError("unsupported style library schema")
    return data, digest


def require_brief(brief: dict[str, Any]) -> None:
    required = ["version", "feature_focus", "target_audience", "product_visual"]
    missing = [key for key in required if not str(brief.get(key, "")).strip()]
    if missing:
        raise ValueError("missing brief fields: " + ", ".join(missing))
    if not str(brief["version"]).startswith("v"):
        raise ValueError("brief version must be a verified release tag")


def select_template(library: dict[str, Any], brief: dict[str, Any]) -> dict[str, Any]:
    templates = library["templates"]
    wanted = str(brief.get("template_id", "poster-layout-system")).strip()
    for template in templates:
        if template.get("id") == wanted:
            return template
    raise ValueError(f"template not found: {wanted}")


def lines(value: Any) -> list[str]:
    if not isinstance(value, list):
        return []
    return [str(item).strip() for item in value if str(item).strip()]


def build_prompt(brief: dict[str, Any], channel: str) -> dict[str, Any]:
    if channel not in CHANNELS:
        raise ValueError(f"unsupported channel: {channel}")
    require_brief(brief)
    library, library_hash = load_library()
    template = select_template(library, brief)
    spec = CHANNELS[channel]
    locale_key = "zh" if channel == "xiaohongshu" else "en"
    guidance = lines(template.get("guidance", {}).get(locale_key))
    pitfalls = lines(template.get("pitfalls", {}).get(locale_key))
    capabilities = lines(brief.get("product_capabilities"))
    outcomes = lines(brief.get("user_outcomes"))
    copy = (
        "10+ AI 工具，一处管理；安装、配置、启动，都更简单。"
        if channel == "xiaohongshu"
        else "10+ AI tools. One workspace. Install, configure, launch."
    )
    prompt_parts = [
        "生成一张完整、单一场景的产品概念推广海报。",
        f"产品：BootAgent；目标受众：{brief['target_audience']}。",
        f"产品视觉方向：{brief['product_visual']}。",
        f"主题：{brief['feature_focus']}。",
        f"画布比例：{spec['aspect_ratio']}，输出尺寸：{spec['size']}。",
        f"构图指导：{'；'.join(guidance)}",
        f"需要表达的产品能力：{'；'.join(capabilities)}。" if capabilities else "",
        f"用户结果：{'；'.join(outcomes)}。" if outcomes else "",
        "只允许在后期排版层加入少量准确文字，不要在底图中生成正文。",
        f"渠道短文案参考：{copy}",
        f"模板常见问题：{'；'.join(pitfalls)}",
        "本地强约束：" + "；".join(LOCAL_CONSTRAINTS),
    ]
    prompt = "".join(part for part in prompt_parts if part)
    return {
        "schema_version": 1,
        "generator": "GPT Image 2",
        "channel": channel,
        "aspect_ratio": spec["aspect_ratio"],
        "canvas": spec["size"],
        "language": spec["language"],
        "version": brief["version"],
        "template_id": template["id"],
        "template_title": template["title"].get(locale_key, template["title"].get("en", "")),
        "upstream": {
            "repository": library["repository"],
            "commit": UPSTREAM_COMMIT,
            "source_path": "data/style-library.json",
            "style_library_sha256": library_hash,
        },
        "prompt": prompt,
        "negative_constraints": LOCAL_CONSTRAINTS + pitfalls,
        "remote_generation": False,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--brief", type=Path, required=True)
    parser.add_argument("--channel", choices=sorted(CHANNELS), required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    brief = json.loads(args.brief.read_text(encoding="utf-8"))
    result = build_prompt(brief, args.channel)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {args.output}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
