/**
 * Localised display labels for skillhub subCategory keys.
 *
 * Adapters keep the data layer language-neutral by storing the raw keys in
 * MarketplaceItem.tagKeys; the render layer resolves the label for the active
 * locale through localizeTag at paint time.
 */

import { translateIfKnown, type Locale } from "../i18n";
import type { MarketplaceItem } from "../types/marketplace";

// ── subCategory key -> Chinese label ─────────────────────────────────────────

const SUB_LABEL: Record<string, string> = {
  "agent-context": "上下文管理",
  "agent-memory": "记忆增强",
  "agent-framework": "Agent框架",
  "agent-workflow": "工作流",
  "agent-tool-use": "工具调用",
  "agent-task-automation": "任务自动化",
  "agent-multi-agent": "多智能体",
  "agent-prompt": "提示词",
  "dev-git": "Git",
  "dev-api": "API",
  "dev-debug": "调试",
  "dev-frontend": "前端",
  "dev-backend": "后端",
  "dev-code-review": "代码审查",
  "dev-test": "测试",
  "dev-devops": "DevOps",
  "knowledge-retrieval": "知识检索",
  "knowledge-summary": "摘要",
  "knowledge-doc-qa": "文档问答",
  "knowledge-note": "笔记",
  "office-doc": "文档",
  "office-spreadsheet": "表格",
  "office-slides": "幻灯片",
  "office-pdf": "PDF",
  "office-email": "邮件",
  "data-visualization": "数据可视化",
  "data-insight": "数据洞察",
  "data-report": "数据报告",
  "data-web-scraping": "网页抓取",
  "data-metrics-monitoring": "指标监控",
  "data-competitor": "竞品分析",
  "data-user-analysis": "用户分析",
  "content-article": "文章创作",
  "content-marketing-copy": "营销文案",
  "content-rewrite": "改写润色",
  "content-short-video-script": "短视频脚本",
  "biz-project-management": "项目管理",
  "it-monitoring": "监控",
  "it-security": "安全",
  "design-ui": "UI设计",
  "design-image": "图像",
  "life-travel": "旅行",
  "life-health": "健康",
};

// ── subCategory key -> English label ─────────────────────────────────────────

const SUB_LABEL_EN: Record<string, string> = {
  "agent-context": "Context",
  "agent-memory": "Memory",
  "agent-framework": "Framework",
  "agent-workflow": "Workflow",
  "agent-tool-use": "Tool Use",
  "agent-task-automation": "Automation",
  "agent-multi-agent": "Multi-Agent",
  "agent-prompt": "Prompts",
  "dev-git": "Git",
  "dev-api": "API",
  "dev-debug": "Debug",
  "dev-frontend": "Frontend",
  "dev-backend": "Backend",
  "dev-code-review": "Code Review",
  "dev-test": "Testing",
  "dev-devops": "DevOps",
  "knowledge-retrieval": "Retrieval",
  "knowledge-summary": "Summary",
  "knowledge-doc-qa": "Doc Q&A",
  "knowledge-note": "Notes",
  "office-doc": "Docs",
  "office-spreadsheet": "Sheets",
  "office-slides": "Slides",
  "office-pdf": "PDF",
  "office-email": "Email",
  "data-visualization": "Data Viz",
  "data-insight": "Insights",
  "data-report": "Reports",
  "data-web-scraping": "Scraping",
  "data-metrics-monitoring": "Metrics",
  "data-competitor": "Competitors",
  "data-user-analysis": "User Analytics",
  "content-article": "Articles",
  "content-marketing-copy": "Marketing",
  "content-rewrite": "Rewrite",
  "content-short-video-script": "Video Scripts",
  "biz-project-management": "PM",
  "it-monitoring": "Monitoring",
  "it-security": "Security",
  "design-ui": "UI Design",
  "design-image": "Images",
  "life-travel": "Travel",
  "life-health": "Health",
};

/**
 * Bounded-width fallback for unmapped keys in either locale: the last dash
 * segments of the key (e.g. "agent-tool-use" -> "tool-use"), truncated so one
 * long key cannot blow up the single-line tag row.
 */
function fallbackLabel(key: string): string {
  const short = key.includes("-") ? key.split("-").slice(1).join("-") : key;
  const chars = Array.from(short);
  return chars.length > 8 ? `${chars.slice(0, 7).join("")}…` : short;
}

/** Display label for one raw subCategory key in the given locale. */
export function localizeTag(key: string, locale: Locale): string {
  const mapped = locale === "en" ? SUB_LABEL_EN[key] : SUB_LABEL[key];
  return mapped ?? fallbackLabel(key);
}

/**
 * [key, label] pairs for one item's tag row. tagKeys localise through the
 * SUB_LABEL tables above; sources without tagKeys (e.g. mcpservers) provide
 * plain display strings that translate only when registered in the i18n
 * dictionary ("官方"/"MCP") and pass through otherwise.
 */
export function marketplaceTagPairs(
  item: Pick<MarketplaceItem, "tags" | "tagKeys">,
  locale: Locale,
): Array<readonly [key: string, label: string]> {
  if (item.tagKeys?.length) {
    return item.tagKeys.map((key) => [key, localizeTag(key, locale)] as const);
  }
  return (item.tags ?? []).map((tag) => [tag, translateIfKnown(locale, tag)] as const);
}
