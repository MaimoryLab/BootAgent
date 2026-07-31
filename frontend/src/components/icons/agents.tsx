/**
 * Agent marks, taken from each project's own published artwork.
 *
 * Earlier revisions drew these by hand to keep one monochrome style. That
 * inverted the priority: a glyph has to be recognised before it is tidy, and a
 * hand-drawn "claw" read as a cup. Every Agent here has an official mark, so
 * every mark is the official one, in the brand's own colours.
 *
 * Uniformity now comes from the container rather than from redrawing: one square
 * box, one size per context, consistent padding. That is how a folder of
 * unrelated app icons still reads as a set.
 *
 * Trademark note: these identify which Agent a row refers to — nominative use.
 * They are not OneAgent product artwork and are not recoloured or restyled.
 * Sources are recorded per entry; assets live beside this file and are inlined
 * at build time, since the release policy forbids external references.
 */
import { Bot } from "lucide-react";

import { sourceTranslate, type Translate, type TranslationKey } from "../../i18n";

import aiderMark from "./assets/aider.png";
import claudeMark from "./assets/claude-code.svg";
import codexMark from "./assets/codex.svg";
import cursorMark from "./assets/cursor.svg";
import hermesMark from "./assets/hermes.png";
import kiloMark from "./assets/kilo-cli.svg";
import openclawMark from "./assets/openclaw.svg";
import opencodeMark from "./assets/opencode.svg";

/** Where each mark came from, so the next person can re-check it. */
const MARKS: Record<string, { src: string; source: string }> = {
  codex: { src: codexMark, source: "lobehub/icons-static-svg openai (MIT)" },
  "claude-code": { src: claudeMark, source: "claude.ai/favicon.svg" },
  cursor: { src: cursorMark, source: "cursor.com/favicon.svg" },
  opencode: { src: opencodeMark, source: "lobehub/icons-static-svg opencode (MIT)" },
  openclaw: { src: openclawMark, source: "openclaw/openclaw docs/assets/pixel-lobster.svg (MIT)" },
  hermes: { src: hermesMark, source: "hermes-agent.nousresearch.com/icon.png" },
  "kilo-cli": { src: kiloMark, source: "kilocode.ai/favicon/favicon.svg" },
  aider: { src: aiderMark, source: "aider.chat/assets/icons/favicon-32x32.png" },
};

/** One-line positioning shown on hover; never a restatement of the name. */
const TAGLINES: Record<string, TranslationKey> = {
  codex: "OpenAI 的终端编码代理",
  "claude-code": "Anthropic 的终端编码代理",
  opencode: "开源终端编码代理",
  "kilo-cli": "多模型编排的命令行代理",
  aider: "结对编程式的仓库编辑代理",
  cursor: "AI 编辑器，按官方方式安装",
  openclaw: "多渠道 AI 网关，常驻运行",
  hermes: "自我成长型 Agent 框架",
};

export const AGENT_ICON_IDS = Object.keys(MARKS);

export function agentTagline(agentId: string, t: Translate = sourceTranslate): string {
  const tagline = TAGLINES[agentId];
  return tagline ? t(tagline) : "";
}

/** The provenance of an Agent's mark, for the reference notes and tests. */
export function agentMarkSource(agentId: string): string {
  return MARKS[agentId]?.source ?? "";
}

export function AgentIcon({ agentId, size = 18 }: { agentId: string; size?: number }) {
  const mark = MARKS[agentId];
  if (!mark) {
    // An Agent added to agents.lock.json before its mark is fetched still
    // renders rather than leaving a blank square.
    return <Bot size={size} strokeWidth={1.8} aria-hidden="true" />;
  }
  return (
    <img
      className="agent-mark"
      src={mark.src}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
      // Marks are square but not identically padded; contain keeps the tallest
      // and widest ones from overflowing the box they share.
      style={{ objectFit: "contain" }}
      draggable={false}
    />
  );
}
