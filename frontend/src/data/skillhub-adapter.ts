/**
 * Adapts skillhub-hot.json entries to MarketplaceItem shape.
 */

import type { MarketplaceItem, MarketplaceScene } from "../types/marketplace";
import { localizeTag } from "./tag-labels";
import skillhubRaw from "./skillhub-hot.json";

// ── category mapping ──────────────────────────────────────────────────────────

// ── icon color by category ────────────────────────────────────────────────────

function iconColor(): string { return "oklch(62% 0.18 250)"; }

// ── scene from first subCategory ─────────────────────────────────────────────

function mapScenes(subs: string[]): MarketplaceScene[] {
  const scenes = new Set<MarketplaceScene>();
  for (const sub of subs) {
    if (sub.startsWith("agent-") || sub.startsWith("dev-")) scenes.add("coding");
    if (sub.startsWith("knowledge-")) scenes.add("memory");
    if (sub.startsWith("office-") || sub.startsWith("content-")) scenes.add("productivity");
    if (sub.startsWith("data-")) scenes.add("reasoning");
    if (sub.startsWith("design-")) scenes.add("design");
    if (sub.startsWith("biz-")) scenes.add("productivity");
    if (sub.startsWith("it-")) scenes.add("integration");
    if (sub.startsWith("itops-")) scenes.add("integration");
    if (sub.startsWith("life-")) scenes.add("productivity");
  }
  return [...scenes];
}

// ── adapter ───────────────────────────────────────────────────────────────────

/**
 * Normalised skillhub entry shape shared by the static snapshot
 * (skillhub-hot.json) and the live showcase API (useMarketplaceCatalog).
 */
export interface SkillhubEntry {
  id: string;
  name: string;
  description: string;
  descriptionEn?: string;
  iconUrl: string;
  category: string;
  subCategories: string[];
  stars: number;
  downloads: number;
  score: number;
  requiresApiKey: boolean;
  namespace: string;
  homepage?: string;
}

/** Maps one normalised skillhub entry to the MarketplaceItem shape. */
export function mapSkillhubEntry(entry: SkillhubEntry): MarketplaceItem {
  const scenes = mapScenes(entry.subCategories);
  // Keep the full bounded taxonomy for detail pages; cards intentionally cap
  // the visible row to three tags for scanability.
  const tagKeys = entry.subCategories.slice(0, 8);
  return {
    id: `skillhub-${entry.id}`,
    category: "skill",
    type: "installable",
    installableKind: "skill",
    name: entry.name,
    description: entry.description,
    descriptionEn: entry.descriptionEn,
    icon: "Zap",
    iconColor: iconColor(),
    iconUrl: entry.iconUrl || undefined,
    // tags keeps the pre-tagKeys Chinese labels for callers that only read
    // strings (search, older render paths); tagKeys carries the raw keys so
    // render sites can localise per the active locale.
    tags: tagKeys.map((key) => localizeTag(key, "zh-CN")),
    tagKeys,
    scene: scenes[0],
    scenes,
    source: "skillhub",
    requiresApiKey: entry.requiresApiKey,
    sourceLabel: "SkillHub",
    sourceUrl: `https://skillhub.cn/skills/${encodeURIComponent(entry.id)}`,
    // Plain-language prompt, no slash command: works in any CLI agent.
    // The skillhub CLI is the actual install channel; the agent runs it.
    installPrompt: `请帮我安装 SkillHub 上的 Skill「${entry.name}」（${entry.namespace}）。

执行步骤：
1. 检查本机是否已安装 skillhub CLI（运行 \`skillhub -v\`）。若未安装，先运行：
   curl -fsSL https://skillhub-1388575217.cos.ap-guangzhou.myqcloud.com/install/install.sh | bash
2. 找到当前 Agent 的 skills 目录（例如 Claude Code 是 ~/.claude/skills，其他 Agent 请查阅其文档）
3. 运行安装命令：
   skillhub install ${entry.namespace} --dir <skills 目录>
4. 安装完成后列出该 Skill 的说明，确认它已可用`,
    targetHint: "粘贴到任意命令行 Agent 对话框执行，它会调用 skillhub CLI 完成安装",
    stars: entry.stars,
    downloads: entry.downloads,
    score: entry.score,
  };
}

export const skillhubItems: MarketplaceItem[] =
  (skillhubRaw as SkillhubEntry[]).map(mapSkillhubEntry);
