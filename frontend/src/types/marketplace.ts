/**
 * Marketplace data model.
 *
 * The marketplace is a pure discovery layer: it does not read or affect the
 * real install state on the Skills / MCP management pages.
 *
 * Ships a static local catalog as the baseline. Live skillhub data is fetched
 * through the Go MarketplaceService proxy (the "backend holds URLs"
 * convention; api.skillhub.cn's CORS policy blocks the renderer), with the
 * bundled snapshot as the offline fallback.
 */

export type MarketplaceCategory =
  | "skill"
  | "mcp-server"
  | "plugin"
  | "ai-product"
  | "workflow"
  | "content";

export type MarketplaceItemType =
  | "installable" // copied prompt drives the install
  | "content" // article / subscription link, display only
  | "external-link" // standalone software, open URL only
  | "plugin" // plugin documentation or marketplace, open URL only
  | "agent-product"; // standalone AI product, open URL only

export type InstallableKind =
  | "skill"
  | "mcp"
  | "prompt-template"
  | "workflow-script";

/** Scene / use-case grouping shown in the filter sidebar */
export type MarketplaceScene =
  | "coding"       // 代码编写
  | "design"       // 界面设计
  | "reasoning"    // 推理规划
  | "memory"       // 记忆管理
  | "integration"  // 外部集成
  | "productivity" // 效率工具
  | "learning";    // 学习资讯

/** Source platform identifier for filter chips */
export type MarketplaceSource =
  | "skillhub"
  | "mcpservers"
  | "mcp-registry"
  | "npm"
  | "pypi"
  | "docker"
  | "vscode"
  | "huggingface"
  | "anthropic"
  | "community"
  | "official"
  | "github";

/**
 * Icon token for each card. Rendered as a coloured glyph well at the top of
 * the card. Uses lucide icon names so the card component can import and render
 * them without embedding SVG in the data file.
 */
export type MarketplaceIconName =
  | "Zap"
  | "Paintbrush"
  | "Brain"
  | "GitBranch"
  | "Globe"
  | "FileText"
  | "Workflow"
  | "BookOpen"
  | "Puzzle"
  | "Database"
  | "Terminal"
  | "Search"
  | "Layers"
  | "Code2"
  | "ExternalLink";

export interface MarketplaceItem {
  id: string;
  category: MarketplaceCategory;
  type: MarketplaceItemType;
  name: string;
  description: string;
  /** English description shown when the locale is "en"; falls back to description */
  descriptionEn?: string;
  /** Icon name from MarketplaceIconName; each card renders this in a coloured glyph well */
  icon: MarketplaceIconName;
  /** Accent hue for the icon well background, as a CSS oklch string */
  iconColor: string;
  /**
   * Display tags, already in Chinese. Kept as the fallback for sources that
   * provide plain strings (e.g. mcpservers) and for callers that predate
   * tagKeys; render sites prefer tagKeys when both are present.
   */
  tags?: string[];
  /**
   * Raw language-neutral tag keys (skillhub subCategory keys). Rendered via
   * localizeTag(key, locale) in data/tag-labels.ts so the same item shows
   * Chinese or English labels depending on the active locale.
   */
  tagKeys?: string[];
  /** Normalized capability facets used by search and future filters. */
  capabilities?: string[];
  /** Ecosystem integrations supported by the item (e.g. Claude Code, Docker). */
  integrations?: string[];
  /** Distribution/deployment modes (e.g. CLI, Docker, SaaS). */
  deploymentModes?: string[];
  /** Provenance and maintenance metadata for ranking and review. */
  trustLevel?: "official" | "verified" | "community";
  license?: string;
  updatedAt?: string;
  scene?: MarketplaceScene;
  scenes?: MarketplaceScene[];
  source?: MarketplaceSource;
  /** Whether the tool requires an API key to use */
  requiresApiKey?: boolean;
  /** Human-readable origin label, e.g. "来自 SkillHub" */
  sourceLabel?: string;
  /** Reference URL for the source — not the install target */
  sourceUrl?: string;
  repositoryUrl?: string;
  documentationUrl?: string;

  // ── installable items ────────────────────────────────────────────────────
  installableKind?: InstallableKind;
  /** Full prompt text to copy to the clipboard */
  installPrompt?: string;
  /** Short hint shown after the copy button, e.g. "粘贴到 Claude Code 对话框" */
  targetHint?: string;

  // ── external-link items (ecosystem tab) ─────────────────────────────────
  externalUrl?: string;

  /**
   * Raw URL of the item's README. Fetched client-side in the detail page.
   * For GitHub repos use the raw.githubusercontent.com form:
   * https://raw.githubusercontent.com/{owner}/{repo}/main/README.md
   */
  readmeUrl?: string;

  // ── skillhub-specific fields (only set for skillhub items) ──────────────────
  /** Remote image URL from skillhub; overrides the lucide icon when present */
  iconUrl?: string;
  stars?: number;
  downloads?: number;
  score?: number;
  githubStars?: number;
  githubForks?: number;
  githubLicense?: string;
  githubUpdatedAt?: string;
}

export interface MarketplaceCatalog {
  /** Semver string; bump when the shape changes */
  version: string;
  items: MarketplaceItem[];
  /** ISO 8601 string; set to a static value in the local catalog */
  builtAt: string;
  /**
   * True when the catalog was read from a local cache because the remote fetch
   * failed. Phase 1 never sets this; reserved for Phase 3.
   */
  stale?: boolean;
}
