import {
  BookOpen,
  Brain,
  Check,
  ChevronDown,
  Code2,
  Copy,
  Database,
  ExternalLink,
  FileText,
  GitBranch,
  Globe,
  Layers,
  Puzzle,
  Search,
  ShoppingBag,
  Sparkles,
  Terminal,
  Workflow,
  Zap,
} from "lucide-react";
import { type ComponentType, useEffect, useRef, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { useMarketplaceCatalog } from "../data/useMarketplaceCatalog";
import { marketplaceTagPairs } from "../data/tag-labels";
import { EmptyState } from "../components/EmptyState";
import { ManagementSearch } from "../components/ManagementSearch";
import { MarketplaceRecommendationDialog } from "../components/MarketplaceRecommendationDialog";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n, type TranslationKey } from "../i18n";
import type {
  InstallableKind,
  MarketplaceCategory,
  MarketplaceIconName,
  MarketplaceItem,
  MarketplaceScene,
  MarketplaceSource,
} from "../types/marketplace";
import { hasActiveFilters, EMPTY_FILTERS } from "../components/MarketplaceFilterSidebar";
import type { FilterState } from "../components/MarketplaceFilterSidebar";
import { copyToClipboard } from "../utils/clipboard";
import { marketplaceIconCandidates } from "../data/marketplace-icons";

// ── icon registry ─────────────────────────────────────────────────────────────

export const ICON_MAP: Record<MarketplaceIconName, ComponentType<{ size?: number; strokeWidth?: number }>> = {
  Zap, Paintbrush: Zap, Brain, GitBranch, Globe, FileText,
  Workflow, BookOpen, Puzzle, Database, Terminal, Search, Layers,
  Code2, ExternalLink,
};

export function ItemIcon({ name, color, size = 22 }: { name: MarketplaceIconName; color: string; size?: number }) {
  const Icon = ICON_MAP[name] ?? Zap;
  return (
    <span
      className="marketplace-card-icon"
      style={{ "--item-icon-color": color } as React.CSSProperties}
      aria-hidden="true"
    >
      <Icon size={size} strokeWidth={1.6} />
    </span>
  );
}

// ── filter dropdown bar ───────────────────────────────────────────────────────

type KindFilterKey = InstallableKind | "content" | "external-link" | "plugin" | "agent-product";

// Option labels are i18n dictionary keys; Dropdown translates them on render.
const KIND_OPTIONS: { key: KindFilterKey; label: TranslationKey }[] = [
  { key: "skill", label: "Skill" },
  { key: "mcp", label: "MCP" },
  { key: "prompt-template", label: "提示词模板" },
  { key: "workflow-script", label: "工作流" },
  { key: "content", label: "内容" },
  { key: "external-link", label: "外部工具" },
  { key: "plugin", label: "插件" },
  { key: "agent-product", label: "独立 AI 产品" },
];

// SkillHub / MCP Servers / Anthropic are brand names and stay untranslated;
// they are still registered as keys so every label flows through t().
const SOURCE_OPTIONS: { key: MarketplaceSource; label: TranslationKey }[] = [
  { key: "skillhub", label: "SkillHub" },
  { key: "mcpservers", label: "MCP Servers" },
  { key: "mcp-registry", label: "MCP 官方 Registry" },
  { key: "npm", label: "npm" },
  { key: "pypi", label: "PyPI" },
  { key: "docker", label: "Docker Hub" },
  { key: "vscode", label: "VS Code Marketplace" },
  { key: "huggingface", label: "Hugging Face" },
  { key: "anthropic", label: "Anthropic" },
  { key: "community", label: "社区" },
  { key: "official", label: "官方" },
  { key: "github", label: "GitHub" },
];

const SCENE_OPTIONS: { key: MarketplaceScene; label: TranslationKey }[] = [
  { key: "coding", label: "代码编写" },
  { key: "design", label: "界面设计" },
  { key: "reasoning", label: "推理规划" },
  { key: "memory", label: "记忆管理" },
  { key: "integration", label: "外部集成" },
  { key: "productivity", label: "效率工具" },
  { key: "learning", label: "学习资讯" },
];

function Dropdown<K extends string>({
  label,
  options,
  selected,
  onToggle,
  radio,
  radioValue,
  onRadio,
  align = "left",
}: {
  label: TranslationKey;
  options: { key: K; label: TranslationKey }[];
  selected?: Set<K>;
  onToggle?: (key: K) => void;
  radio?: boolean;
  radioValue?: K | null;
  onRadio?: (val: K | null) => void;
  /**
   * Panel anchoring. The rightmost dropdown must use "right": its panel is
   * wider than its trigger, and growing rightward past the .page-body edge
   * widens the scrollable area (see .mf-dropdown-panel.is-right in app.css).
   */
  align?: "left" | "right";
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const activeCount = radio
    ? (radioValue !== null && radioValue !== undefined ? 1 : 0)
    : (selected?.size ?? 0);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  return (
    <div className="mf-dropdown" ref={ref}>
      <button
        type="button"
        className={`mf-dropdown-trigger${activeCount > 0 ? " is-active" : ""}`}
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        {activeCount > 0 ? `${t(label)}(${activeCount})` : t(label)}
        <ChevronDown size={13} className={`mf-chevron${open ? " is-open" : ""}`} aria-hidden="true" />
      </button>
      {open && (
        <div className={`mf-dropdown-panel${align === "right" ? " is-right" : ""}`} role="listbox">
          {radio ? (
            <>
              {([{ key: null as K | null, label: t("全部") }, ...options.map((o) => ({ key: o.key as K | null, label: t(o.label) }))]).map(({ key, label: optLabel }) => (
                <label key={String(key)} className="mf-dropdown-option">
                  <input
                    type="radio"
                    name={`mf-radio-${label}`}
                    checked={radioValue === key}
                    onChange={() => { onRadio?.(key); setOpen(false); }}
                  />
                  {optLabel}
                </label>
              ))}
            </>
          ) : (
            options.map(({ key, label: optLabel }) => (
              <label key={key} className="mf-dropdown-option">
                <input
                  type="checkbox"
                  checked={selected?.has(key) ?? false}
                  onChange={() => onToggle?.(key)}
                />
                {t(optLabel)}
              </label>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function FilterDropdownBar({
  filters,
  onChange,
}: {
  filters: FilterState;
  onChange: (next: FilterState) => void;
}) {
  const { t } = useI18n();

  const toggleKind = (k: KindFilterKey) => {
    const next = new Set(filters.kinds);
    next.has(k) ? next.delete(k) : next.add(k);
    onChange({ ...filters, kinds: next });
  };
  const toggleSource = (s: MarketplaceSource) => {
    const next = new Set(filters.sources);
    next.has(s) ? next.delete(s) : next.add(s);
    onChange({ ...filters, sources: next });
  };
  const toggleScene = (s: MarketplaceScene) => {
    const next = new Set(filters.scenes);
    next.has(s) ? next.delete(s) : next.add(s);
    onChange({ ...filters, scenes: next });
  };

  return (
    <div className="mf-dropdown-bar" aria-label={t("筛选")}>
      <Dropdown
        label="工具类型"
        options={KIND_OPTIONS as { key: KindFilterKey; label: TranslationKey }[]}
        selected={filters.kinds}
        onToggle={toggleKind}
      />
      <Dropdown
        label="来源"
        options={SOURCE_OPTIONS as { key: MarketplaceSource; label: TranslationKey }[]}
        selected={filters.sources}
        onToggle={toggleSource}
      />
      <Dropdown
        label="场景"
        options={SCENE_OPTIONS as { key: MarketplaceScene; label: TranslationKey }[]}
        selected={filters.scenes}
        onToggle={toggleScene}
      />
      <Dropdown<"yes" | "no">
        label="API Key"
        align="right"
        options={[
          { key: "no", label: "无需 API Key" },
          { key: "yes", label: "需要 API Key" },
        ]}
        radio
        radioValue={
          filters.requiresApiKey === null ? null :
          filters.requiresApiKey ? "yes" : "no"
        }
        onRadio={(v) => onChange({
          ...filters,
          requiresApiKey: v === null ? null : v === "yes",
        })}
      />
      {hasActiveFilters(filters) && (
        <button
          type="button"
          className="mf-clear-btn"
          onClick={() => onChange(EMPTY_FILTERS)}
        >
          {t("清除全部筛选")}
        </button>
      )}
    </div>
  );
}

// ── category tab metadata ─────────────────────────────────────────────────────

interface CategoryMeta {
  id: MarketplaceCategory | "all";
  labelKey: "全部" | "Skills" | "MCP 服务器" | "插件" | "独立 AI 产品" | "工作流与模板" | "内容与指南";
}

const CATEGORIES: CategoryMeta[] = [
  { id: "all", labelKey: "全部" },
  { id: "skill", labelKey: "Skills" },
  { id: "mcp-server", labelKey: "MCP 服务器" },
  { id: "plugin", labelKey: "插件" },
  { id: "ai-product", labelKey: "独立 AI 产品" },
  { id: "workflow", labelKey: "工作流与模板" },
  { id: "content", labelKey: "内容与指南" },
];

function marketplaceCategoryFromSearch(searchParams: URLSearchParams): MarketplaceCategory | "all" {
  const requested = searchParams.get("category");
  // Keep old management-page links valid while the visible taxonomy is type-based.
  if (requested === "agent-enhance") return "skill";
  return CATEGORIES.some(({ id }) => id === requested)
    ? requested as MarketplaceCategory | "all"
    : "all";
}

// ── kind badge ────────────────────────────────────────────────────────────────

const KIND_TONE: Record<string, "success" | "info" | "neutral"> = {
  skill: "success",
  mcp: "info",
  "prompt-template": "neutral",
  "workflow-script": "neutral",
  content: "info",
  "external-link": "neutral",
  plugin: "info",
  "agent-product": "neutral",
};

const KIND_LABEL_KEY: Record<string, "Skill" | "MCP" | "提示词模板" | "工作流" | "内容" | "外部工具" | "插件" | "独立 AI 产品"> = {
  skill: "Skill",
  mcp: "MCP",
  "prompt-template": "提示词模板",
  "workflow-script": "工作流",
  content: "内容",
  "external-link": "外部工具",
  plugin: "插件",
  "agent-product": "独立 AI 产品",
};

export function KindBadge({ item }: { item: MarketplaceItem }) {
  const { t } = useI18n();
  const key = item.type === "installable" ? (item.installableKind ?? "skill") : item.type;
  const tone = KIND_TONE[key] ?? "neutral";
  const labelKey = KIND_LABEL_KEY[key];
  if (!labelKey) return null;
  return <StatusBadge tone={tone}>{t(labelKey)}</StatusBadge>;
}

// ── compact card (list view) ──────────────────────────────────────────────────

/** Remote icon with a lucide fallback when the image fails to load. */
function CardIcon({ item }: { item: MarketplaceItem }) {
  const candidates = useMemo(() => marketplaceIconCandidates(item), [item]);
  const [candidateIndex, setCandidateIndex] = useState(0);
  const remoteIcon = candidates[candidateIndex];
  if (remoteIcon) {
    return (
      <span
        className="marketplace-card-icon"
        style={{ "--item-icon-color": item.iconColor } as React.CSSProperties}
        aria-hidden="true"
      >
        <img
          src={remoteIcon}
          width={24}
          height={24}
          alt=""
          style={{ borderRadius: 6 }}
          onError={() => setCandidateIndex((index) => index + 1)}
        />
      </span>
    );
  }
  return <ItemIcon name={item.icon} color={item.iconColor} />;
}

function MarketplaceItemCard({ item, onCopied }: { item: MarketplaceItem; onCopied: (item: MarketplaceItem) => void }) {
  const { t, locale } = useI18n();
  const navigate = useNavigate();

  const teaser = useMemo(() => {
    const description =
      locale === "en" && item.descriptionEn ? item.descriptionEn : item.description;
    const chars = Array.from(description);
    return chars.length > 58 ? `${chars.slice(0, 58).join("")}…` : description;
  }, [item.description, item.descriptionEn, locale]);

  // Labels resolve at render time so a locale switch relabels existing cards.
  const tagPairs = marketplaceTagPairs(item, locale).slice(0, 3);

  const copy = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!item.installPrompt) return;
    const ok = await copyToClipboard(item.installPrompt);
    if (ok) onCopied(item);
  };

  return (
    <article
      className="marketplace-card"
      data-type={item.type}
      role="button"
      tabIndex={0}
      onClick={() => navigate(`/marketplace/${encodeURIComponent(item.id)}`)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          navigate(`/marketplace/${encodeURIComponent(item.id)}`);
        }
      }}
      aria-label={item.name}
    >
      {/* Left column: icon well + kind badge stacked vertically */}
      <div className="marketplace-card-icon-wrap">
        <CardIcon item={item} />
        <KindBadge item={item} />
      </div>

      {/* Right column: name + teaser + tags */}
      <div className="marketplace-card-body">
        <strong className="marketplace-card-name">{item.name}</strong>
        <p className="marketplace-card-teaser">{teaser}</p>
        {tagPairs.length ? (
          <ul className="marketplace-tags" aria-label={t("标签")}>
            {tagPairs.map(([key, label]) => (
              <li key={key} className="marketplace-tag">{label}</li>
            ))}
          </ul>
        ) : null}
      </div>

      <ExternalLink size={14} className="marketplace-card-arrow" aria-hidden="true" />

      {/* Icon-only copy button pinned to the bottom-right corner */}
      {item.installPrompt ? (
        <button
          type="button"
          className="marketplace-card-copy"
          onClick={(e) => void copy(e)}
          title={t("复制安装提示词")}
          aria-label={t("复制安装提示词")}
        >
          <Copy size={12} />
        </button>
      ) : null}
    </article>
  );
}

// ── filter helpers (exported for tests) ──────────────────────────────────────

export function filterMarketplaceItems(
  items: MarketplaceItem[],
  category: MarketplaceCategory | "all",
  query: string,
  filters: FilterState = EMPTY_FILTERS,
): MarketplaceItem[] {
  const needle = query.trim().toLowerCase();
  return items.filter((item) => {
    if (category !== "all" && item.category !== category) return false;

    if (filters.kinds.size > 0) {
      const itemKind: KindFilterKey =
        item.type === "installable" ? (item.installableKind ?? "skill") : item.type;
      if (!filters.kinds.has(itemKind)) return false;
    }
    if (filters.sources.size > 0) {
      if (!item.source || !filters.sources.has(item.source)) return false;
    }
    if (filters.scenes.size > 0) {
      const itemScenes = item.scenes ?? (item.scene ? [item.scene] : []);
      if (!itemScenes.some((scene) => filters.scenes.has(scene))) return false;
    }
    if (filters.requiresApiKey !== null) {
      if ((item.requiresApiKey ?? false) !== filters.requiresApiKey) return false;
    }

    if (!needle) return true;
    return (
      item.name.toLowerCase().includes(needle) ||
      item.description.toLowerCase().includes(needle) ||
      item.descriptionEn?.toLowerCase().includes(needle) ||
      item.tags?.some((tag) => tag.toLowerCase().includes(needle))
    );
  });
}

// ── page ──────────────────────────────────────────────────────────────────────

export function MarketplacePage() {
  const { t } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeCategory = marketplaceCategoryFromSearch(searchParams);
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<FilterState>(EMPTY_FILTERS);
  // Bottom toast shown after the corner copy button; auto-dismisses.
  const [copyNotice, setCopyNotice] = useState("");
  const [recommendationOpen, setRecommendationOpen] = useState(false);
  const noticeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { items, live } = useMarketplaceCatalog();

  const handleCopied = (item: MarketplaceItem) => {
    setCopyNotice(t("已复制「{name}」的安装提示词，{hint}", {
      name: item.name,
      hint: item.targetHint ?? t("粘贴到任意命令行 Agent 对话框执行即可"),
    }));
    if (noticeTimer.current) clearTimeout(noticeTimer.current);
    noticeTimer.current = setTimeout(() => setCopyNotice(""), 4000);
  };

  useEffect(() => () => { if (noticeTimer.current) clearTimeout(noticeTimer.current); }, []);

  const visible = useMemo(
    () => filterMarketplaceItems(items, activeCategory, query, filters),
    [items, activeCategory, query, filters],
  );

  const counts = useMemo(
    () =>
      CATEGORIES.reduce<Record<string, number>>((acc, cat) => {
        acc[cat.id] =
          cat.id === "all"
            ? items.length
            : items.filter((item) => item.category === cat.id).length;
        return acc;
      }, {}),
    [items],
  );

  // Empty type tabs are hidden until a trusted adapter supplies an item.
  const visibleCategories = CATEGORIES.filter(({ id }) => id === "all" || (counts[id] ?? 0) > 0);
  const selectCategory = (category: MarketplaceCategory | "all") => {
    const next = new URLSearchParams(searchParams);
    if (category === "all") next.delete("category");
    else next.set("category", category);
    setSearchParams(next, { replace: true });
    setQuery("");
  };

  return (
    <PageScaffold
      title={t("工具市场")}
      description={t("发现并安装 Agent 扩展、MCP 服务器与配置模板")}
      bodyClassName="marketplace-page"
      footerNote={t("{count} 个工具 · {status}", { count: items.length, status: live ? t("实时数据") : t("离线快照") })}
      secondaryAction={(
        <button className="button button-secondary" type="button" onClick={() => setRecommendationOpen(true)}>
          <Sparkles size={15} />{t("帮我找工具")}
        </button>
      )}
    >
      <div className="marketplace-tabs" role="tablist" aria-label={t("工具分类")}>
        {visibleCategories.map(({ id, labelKey }) => (
          <button
            key={id}
            role="tab"
            type="button"
            aria-selected={activeCategory === id}
            className={`marketplace-tab${activeCategory === id ? " is-active" : ""}`}
            onClick={() => selectCategory(id)}
          >
            {t(labelKey)}
            {counts[id] ? (
              <span className="marketplace-tab-count">{counts[id]}</span>
            ) : null}
          </button>
        ))}
        {/* Data-source indicator: live showcase feed vs bundled snapshot */}
        <span className={`marketplace-live-indicator${live ? " is-live" : ""}`} role="status">
          {live ? <span className="marketplace-live-dot" aria-hidden="true" /> : null}
          {live ? t("实时数据") : t("离线快照")}
        </span>
      </div>

      <div className="management-toolbar marketplace-toolbar">
        <ManagementSearch
          value={query}
          onValueChange={setQuery}
          placeholder={t("搜索工具市场")}
        />
        <FilterDropdownBar filters={filters} onChange={setFilters} />
      </div>

      <div className="marketplace-layout">
        <div className="marketplace-content">
          {hasActiveFilters(filters) ? (
            <div className="marketplace-active-filters" role="status">
              <span className="marketplace-result-count">
                {t("{count} 个结果", { count: visible.length })}
              </span>
            </div>
          ) : null}

          {visible.length === 0 ? (
            <EmptyState
              icon={query || hasActiveFilters(filters) ? Search : ShoppingBag}
              title={
                query || hasActiveFilters(filters)
                  ? t("没有匹配的工具")
                  : t("这个分类暂无内容")
              }
              hint={
                query || hasActiveFilters(filters)
                  ? t("换一个关键词，或清空搜索")
                  : undefined
              }
            />
          ) : (
            <ul className="marketplace-grid" aria-label={t("工具列表")}>
              {visible.map((item) => (
                <li key={item.id}>
                  <MarketplaceItemCard item={item} onCopied={handleCopied} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* Bottom copy-notice bar */}
      {copyNotice ? (
        <div className="marketplace-copy-bar" role="status" aria-live="polite">
          <Check size={14} aria-hidden="true" />
          {copyNotice}
        </div>
      ) : null}
      {recommendationOpen ? <MarketplaceRecommendationDialog items={items} onDismiss={() => setRecommendationOpen(false)} /> : null}
    </PageScaffold>
  );
}
