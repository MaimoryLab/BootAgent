import {
  BookOpen,
  Brain,
  Check,
  Clock3,
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
  RefreshCw,
  Search,
  ShoppingBag,
  Sparkles,
  Terminal,
  Workflow,
  Zap,
} from "lucide-react";
import { type ComponentType, useEffect, useRef, useMemo, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";

import { useMarketplaceCatalog } from "../data/useMarketplaceCatalog";
import { marketplaceTagPairs } from "../data/tag-labels";
import { EmptyState } from "../components/EmptyState";
import { ManagementSearch } from "../components/ManagementSearch";
import { MarketplaceRecommendationDialog } from "../components/MarketplaceRecommendationDialog";
import { MarketplaceRecommendationHistoryDialog } from "../components/MarketplaceRecommendationHistoryDialog";
import { PageScaffold } from "../components/PageScaffold";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n, type TranslationKey } from "../i18n";
import type {
  MarketplaceCategory,
  MarketplaceIconName,
  MarketplaceItem,
  MarketplaceKind,
  MarketplaceScene,
  MarketplaceSource,
} from "../types/marketplace";
import { hasActiveFilters, EMPTY_FILTERS } from "../components/MarketplaceFilterSidebar";
import type { FilterState } from "../components/MarketplaceFilterSidebar";
import { copyToClipboard } from "../utils/clipboard";
import { marketplaceIconCandidates } from "../data/marketplace-icons";
import { marketplaceCategories, marketplaceKinds } from "../data/marketplace-taxonomy";
import { recordMarketplaceEvent } from "../utils/marketplace-telemetry";

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

// Option labels are i18n dictionary keys; Dropdown translates them on render.
const KIND_OPTIONS: { key: MarketplaceKind; label: TranslationKey }[] = [
  { key: "skill", label: "Skill" },
  { key: "mcp", label: "MCP" },
  { key: "prompt-template", label: "提示词模板" },
  { key: "workflow-script", label: "工作流" },
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
        <span>{t(label)}</span>
        <span
          className={`mf-filter-count${activeCount > 0 ? " is-visible" : ""}`}
          aria-hidden="true"
        >
          {activeCount > 0 ? activeCount : 0}
        </span>
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
  items,
}: {
  filters: FilterState;
  onChange: (next: FilterState) => void;
  items: MarketplaceItem[];
}) {
  const { t } = useI18n();

  const availableKinds = useMemo(() => new Set(items.flatMap(marketplaceKinds)), [items]);
  const availableSources = useMemo(() => new Set(items.flatMap((item) => item.source ? [item.source] : [])), [items]);
  const availableScenes = useMemo(() => new Set(items.flatMap((item) => item.scenes ?? (item.scene ? [item.scene] : []))), [items]);

  const toggleKind = (k: MarketplaceKind) => {
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
      {hasActiveFilters(filters) ? (
        <button
          type="button"
          className="mf-clear-btn"
          onClick={() => onChange(EMPTY_FILTERS)}
        >
          {t("清除全部筛选")}
        </button>
      ) : null}
      <Dropdown
        label="工具类型"
        options={KIND_OPTIONS.filter((option) => availableKinds.has(option.key))}
        selected={filters.kinds}
        onToggle={toggleKind}
      />
      <Dropdown
        label="来源"
        options={SOURCE_OPTIONS.filter((option) => availableSources.has(option.key)) as { key: MarketplaceSource; label: TranslationKey }[]}
        selected={filters.sources}
        onToggle={toggleSource}
      />
      <Dropdown
        label="场景"
        options={SCENE_OPTIONS.filter((option) => availableScenes.has(option.key)) as { key: MarketplaceScene; label: TranslationKey }[]}
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
    </div>
  );
}

// ── category tab metadata ─────────────────────────────────────────────────────

interface CategoryMeta {
  id: MarketplaceCategory | "all";
  labelKey: "全部" | "Skills" | "MCP 服务器" | "插件" | "独立 AI 产品" | "工作流与模板";
}

const CATEGORIES: CategoryMeta[] = [
  { id: "all", labelKey: "全部" },
  { id: "skill", labelKey: "Skills" },
  { id: "mcp-server", labelKey: "MCP 服务器" },
  { id: "plugin", labelKey: "插件" },
  { id: "ai-product", labelKey: "独立 AI 产品" },
  { id: "workflow", labelKey: "工作流与模板" },
];

function marketplaceCategoryFromSearch(searchParams: URLSearchParams): MarketplaceCategory | "all" {
  const requested = searchParams.get("category");
  // Keep old management-page links valid while the visible taxonomy is type-based.
  if (requested === "agent-enhance") return "skill";
  return CATEGORIES.some(({ id }) => id === requested)
    ? requested as MarketplaceCategory | "all"
    : "all";
}

const FILTER_PARAM_VALUES = {
  kinds: new Set(KIND_OPTIONS.map(({ key }) => key)),
  sources: new Set(SOURCE_OPTIONS.map(({ key }) => key)),
  scenes: new Set(SCENE_OPTIONS.map(({ key }) => key)),
};

function parseSetParam<K extends string>(searchParams: URLSearchParams, name: string, allowed: Set<K>): Set<K> {
  return new Set((searchParams.get(name) ?? "").split(",").filter((value): value is K => allowed.has(value as K)));
}

export function parseMarketplaceFilters(searchParams: URLSearchParams): FilterState {
  const apiKey = searchParams.get("apiKey");
  return {
    kinds: parseSetParam(searchParams, "kind", FILTER_PARAM_VALUES.kinds),
    sources: parseSetParam(searchParams, "source", FILTER_PARAM_VALUES.sources),
    scenes: parseSetParam(searchParams, "scene", FILTER_PARAM_VALUES.scenes),
    requiresApiKey: apiKey === "yes" ? true : apiKey === "no" ? false : null,
  };
}

export function serializeMarketplaceFilters(searchParams: URLSearchParams, filters: FilterState): URLSearchParams {
  const next = new URLSearchParams(searchParams);
  const setOrDelete = (name: string, values: Set<string>) => {
    if (values.size) next.set(name, [...values].sort().join(","));
    else next.delete(name);
  };
  setOrDelete("kind", filters.kinds);
  setOrDelete("source", filters.sources);
  setOrDelete("scene", filters.scenes);
  if (filters.requiresApiKey === null) next.delete("apiKey");
  else next.set("apiKey", filters.requiresApiKey ? "yes" : "no");
  return next;
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
  const key = marketplaceKinds(item)[0];
  const tone = KIND_TONE[key] ?? "neutral";
  const labelKey = KIND_LABEL_KEY[key];
  if (!labelKey) return null;
  return <StatusBadge tone={tone}>{t(labelKey)}</StatusBadge>;
}

function VirtualMarketplaceGrid({ items, onCopied }: { items: MarketplaceItem[]; onCopied: (item: MarketplaceItem) => void }) {
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(() => window.innerHeight);
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);
  const columns = viewportWidth >= 900 ? 3 : viewportWidth >= 640 ? 2 : 1;
  // The row pitch includes the grid gap. CSS below uses the same card heights;
  // keeping this value stable prevents the spacer elements from creating blank
  // bands or overlapping cards while the live catalog grows.
  const rowHeight = viewportWidth < 640 ? 130 : 118;
  const cardHeight = rowHeight - 10;
  const gridGap = 10;
  const overscan = 2;
  useEffect(() => {
    const container = document.querySelector<HTMLElement>(".page-body.marketplace-page");
    const update = () => {
      setScrollTop(container?.scrollTop ?? 0);
      setViewportHeight(container?.clientHeight ?? window.innerHeight);
      setViewportWidth(window.innerWidth);
    };
    update();
    container?.addEventListener("scroll", update, { passive: true });
    window.addEventListener("resize", update);
    return () => { container?.removeEventListener("scroll", update); window.removeEventListener("resize", update); };
  }, []);
  // Keep the DOM bounded once the live adapters make a facet moderately
  // large. Rendering every card for a 50-item first page made a filter look
  // as if it had added results when the unfiltered virtual list was already
  // showing only its viewport. Small result sets remain fully accessible.
  if (items.length <= 24) {
    return <ul className="marketplace-grid" aria-label="工具列表">{items.map((item) => <li key={item.id}><MarketplaceItemCard item={item} onCopied={onCopied} /></li>)}</ul>;
  }
  const rowCount = Math.ceil(items.length / columns);
  const topRow = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const visibleRows = Math.ceil(viewportHeight / rowHeight) + overscan * 2;
  const endRow = Math.min(rowCount, topRow + visibleRows);
  const start = topRow * columns;
  const end = Math.min(items.length, endRow * columns);
  return <>
    <div style={{ height: topRow * rowHeight }} aria-hidden="true" />
    <ul className="marketplace-grid" aria-label="工具列表">
      {items.slice(start, end).map((item) => <li key={item.id}><MarketplaceItemCard item={item} onCopied={onCopied} /></li>)}
    </ul>
    <div style={{ height: Math.max(0, (rowCount - endRow) * rowHeight + (endRow < rowCount ? gridGap : 0)) }} aria-hidden="true" />
  </>;
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
  const location = useLocation();
  const returnTo = `${location.pathname}${location.search}`;
  const openDetail = () => {
    recordMarketplaceEvent("item_open", item.source);
    navigate(`/marketplace/${encodeURIComponent(item.id)}`, { state: { returnTo, item } });
  };

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
      data-item-id={item.id}
      role="button"
      tabIndex={0}
      onClick={openDetail}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openDetail();
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
          onClick={(e) => { recordMarketplaceEvent("install_prompt_copy", item.source); void copy(e); }}
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
  const seenIDs = new Set<string>();
  return items.filter((item) => {
    if (seenIDs.has(item.id)) return false;
    seenIDs.add(item.id);
    if (category !== "all" && !marketplaceCategories(item).includes(category)) return false;

    if (filters.kinds.size > 0) {
      const itemKinds = new Set(marketplaceKinds(item));
      if (![...filters.kinds].every((kind) => itemKinds.has(kind))) return false;
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
    // Include stable identifiers and public origin URLs in the local index.
    // The backend adapter can find a remote MCP by slug even when its display
    // name is localized; dropping it here would make that successful request
    // appear to have returned no result.
    const searchable = [
      item.id,
      item.name,
      item.description,
      item.descriptionEn,
      item.source,
      item.sourceLabel,
      item.sourceUrl,
      item.repositoryUrl,
      item.documentationUrl,
      item.installationUrl,
      item.externalUrl,
      item.readmeUrl,
      ...(item.tags ?? []),
    ];
    return searchable.some((value) => value?.toLowerCase().includes(needle));
  });
}

// ── page ──────────────────────────────────────────────────────────────────────

export function MarketplacePage() {
  const { t } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeCategory = marketplaceCategoryFromSearch(searchParams);
  const query = searchParams.get("q") ?? "";
  const filters = parseMarketplaceFilters(searchParams);
  // Bottom toast shown after the corner copy button; auto-dismisses.
  const [copyNotice, setCopyNotice] = useState("");
  const [recommendationOpen, setRecommendationOpen] = useState(false);
  const noticeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const { items, live, version, loading, sources = [], refresh = () => {}, refreshing = false, lastUpdated = "" } = useMarketplaceCatalog({
    query,
    category: activeCategory === "all" ? undefined : activeCategory,
    sources: [...filters.sources],
    facetKey: [
      [...filters.kinds].sort().join(","),
      [...filters.scenes].sort().join(","),
      filters.requiresApiKey === null ? "" : String(filters.requiresApiKey),
    ].join("|"),
  });

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
            : items.filter((item) => marketplaceCategories(item).includes(cat.id as MarketplaceCategory)).length;
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
    next.delete("q");
    setSearchParams(next, { replace: true });
  };

  const updateQuery = (value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value.trim()) next.set("q", value);
    else next.delete("q");
    setSearchParams(next, { replace: true });
  };

  const updateFilters = (nextFilters: FilterState) => {
    recordMarketplaceEvent("filter_change");
    const next = serializeMarketplaceFilters(searchParams, nextFilters);
    setSearchParams(next, { replace: true });
  };

  const historyOpen = searchParams.get("recommendationHistory") === "1";
  const openHistory = () => {
    const next = new URLSearchParams(searchParams);
    next.set("recommendationHistory", "1");
    setSearchParams(next);
  };
  const closeHistory = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("recommendationHistory");
    setSearchParams(next, { replace: true });
  };

  return (
    <PageScaffold
      title={t("工具市场")}
      bodyClassName="marketplace-page"
      footerNote={t("{count} 个工具 · {status}", { count: items.length, status: refreshing ? t("正在刷新工具市场") : live ? t("实时数据") : t("离线快照") })}
      secondaryAction={(
        <span className="marketplace-actions">
          <button className="button button-secondary" type="button" onClick={refresh} disabled={loading || refreshing} aria-busy={loading || refreshing} title={lastUpdated ? `${t("刷新市场")} · ${lastUpdated}` : t("刷新市场")}>
            <RefreshCw size={15} className={loading || refreshing ? "is-spinning" : undefined} />
            {t("刷新市场")}
          </button>
          <button className="button button-secondary" type="button" onClick={openHistory}><Clock3 size={15} />{t("推荐历史")}</button>
          <button className="button button-primary" type="button" onClick={() => setRecommendationOpen(true)}><Sparkles size={15} />{t("帮我找工具")}</button>
        </span>
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
        {sources.length > 0 ? <span className="marketplace-source-status" role="status" title={sources.map((source) => `${source.id}: ${source.state}${source.error ? ` (${source.error})` : ""}`).join("; ")}>
          {sources.map((source) => `${source.id} ${source.item_count}${source.total > source.item_count ? `/${source.total}` : ""}`).join(" · ")}
        </span> : null}
      </div>

      <div className="management-toolbar marketplace-toolbar">
        <ManagementSearch
          value={query}
          onValueChange={updateQuery}
          placeholder={t("搜索工具市场")}
        />
        <FilterDropdownBar filters={filters} onChange={updateFilters} items={items} />
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

          {loading && items.length === 0 ? (
            <EmptyState icon={ShoppingBag} title={t("正在加载工具市场")} hint={t("正在同步可用工具，请稍候")} />
          ) : visible.length === 0 ? (
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
            <VirtualMarketplaceGrid items={visible} onCopied={handleCopied} />
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
      {recommendationOpen ? <MarketplaceRecommendationDialog items={items} catalogVersion={version} onDismiss={() => setRecommendationOpen(false)} /> : null}
      {historyOpen ? <MarketplaceRecommendationHistoryDialog onDismiss={closeHistory} /> : null}
    </PageScaffold>
  );
}
