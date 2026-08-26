import { ArrowLeft, ArrowRight, BookOpen, Brain, Check, Code2, Copy, Database, Download, ExternalLink, FileText, GitBranch, Globe, Layers, Puzzle, Search, Star, Terminal, Workflow, Zap } from "lucide-react";
import { type ComponentType, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

import { useMarketplaceCatalog } from "../data/useMarketplaceCatalog";
import { marketplaceTagPairs } from "../data/tag-labels";
import { PageScaffold } from "../components/PageScaffold";
import { MarketplaceExternalLink } from "../components/MarketplaceExternalLink";
import { ReadmeSection } from "../components/ReadmeSection";
import { SkillhubDetailSection } from "../components/SkillhubDetailSection";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n, type TranslationKey } from "../i18n";
import type { MarketplaceIconName, MarketplaceItem } from "../types/marketplace";
import { copyToClipboard } from "../utils/clipboard";

/** 12,345 -> "12.3k"; keeps the stats strip compact like skillhub's. */
function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// ── icon registry (same as MarketplacePage, kept local to avoid circular dep) ─

const ICON_MAP: Record<MarketplaceIconName, ComponentType<{ size?: number; strokeWidth?: number }>> = {
  Zap, Paintbrush: Zap, Brain, GitBranch, Globe, FileText,
  Workflow, BookOpen, Puzzle, Database, Terminal, Search, Layers,
  Code2, ExternalLink,
};

function ItemIcon({ name, color, size = 28, iconUrl }: { name: MarketplaceIconName; color: string; size?: number; iconUrl?: string }) {
  // Remote icons can 404; fall back to the lucide glyph when loading fails.
  const [failed, setFailed] = useState(false);
  if (iconUrl && !failed) {
    return (
      <span
        className="detail-icon-well"
        style={{ "--item-icon-color": color } as React.CSSProperties}
        aria-hidden="true"
      >
        <img
          src={iconUrl}
          width={size}
          height={size}
          alt=""
          style={{ borderRadius: 8 }}
          onError={() => setFailed(true)}
        />
      </span>
    );
  }
  const Icon = ICON_MAP[name] ?? Zap;
  return (
    <span
      className="detail-icon-well"
      style={{ "--item-icon-color": color } as React.CSSProperties}
      aria-hidden="true"
    >
      <Icon size={size} strokeWidth={1.5} />
    </span>
  );
}

// ── kind label ────────────────────────────────────────────────────────────────

const KIND_TONE: Record<string, "success" | "info" | "neutral"> = {
  skill: "success", mcp: "info",
  "prompt-template": "neutral", "workflow-script": "neutral",
  content: "info", "external-link": "neutral",
  plugin: "info", "agent-product": "neutral",
};
// Values are i18n dictionary keys, translated with t() at render time.
const KIND_LABEL: Record<string, TranslationKey> = {
  skill: "Skill", mcp: "MCP",
  "prompt-template": "提示词模板", "workflow-script": "工作流",
  content: "内容", "external-link": "外部工具",
  plugin: "插件", "agent-product": "独立 AI 产品",
};

// ── copy-prompt section ───────────────────────────────────────────────────────

function InstallSection({ item }: { item: MarketplaceItem }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState(false);

  if (!item.installPrompt) return null;

  const handleCopy = async () => {
    if (copied) return;
    const ok = await copyToClipboard(item.installPrompt!);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <section className="detail-install-section">
      <h2 className="detail-section-title">{t("安装方式")}</h2>
      <p className="detail-install-desc">{item.targetHint ?? t("复制下方提示词，粘贴到对应的 Agent 对话框中执行。")}</p>

      {/* Prompt preview box */}
      <div className="detail-prompt-box">
        <header className="detail-prompt-header">
          <span className="detail-prompt-label">{t("安装提示词")}</span>
          <button
            className={`button button-secondary detail-copy-btn${copied ? " is-copied" : ""}`}
            type="button"
            onClick={() => void handleCopy()}
            aria-label={copied ? t("已复制") : t("复制安装提示词")}
          >
            {copied ? <Check size={14} /> : <Copy size={14} />}
            {copied ? t("已复制") : t("复制")}
          </button>
        </header>
        <pre className="detail-prompt-body">{item.installPrompt}</pre>
      </div>

      {copied ? (
        <p className="detail-copy-feedback" role="status">
          {t("已复制到剪贴板，粘贴到 Agent 对话框中执行即可。")}
        </p>
      ) : null}
    </section>
  );
}

// ── meta sidebar ──────────────────────────────────────────────────────────────

// Values are i18n dictionary keys, translated with t() at render time.
const SCENE_LABEL: Record<string, TranslationKey> = {
  coding: "代码编写", design: "界面设计", reasoning: "推理规划",
  memory: "记忆管理", integration: "外部集成", productivity: "效率工具", learning: "学习资讯",
};
// Brand names (SkillHub / MCP Servers / Anthropic) are registered as keys
// with identical English values; 社区/官方 actually translate.
const SOURCE_LABEL: Record<string, TranslationKey> = {
  skillhub: "SkillHub", mcpservers: "MCP Servers", anthropic: "Anthropic",
  community: "社区", official: "官方",
  github: "GitHub",
};

function MetaSidebar({ item }: { item: MarketplaceItem }) {
  const { t, locale } = useI18n();
  const kindKey = item.type === "installable" ? (item.installableKind ?? "skill") : item.type;
  const kindLabel = KIND_LABEL[kindKey];
  const sceneLabel = item.scene ? SCENE_LABEL[item.scene] : undefined;
  const sourceLabel = item.source ? SOURCE_LABEL[item.source] : undefined;
  // Tag labels resolve per locale: raw tagKeys localise, plain tags translate
  // when they are dictionary keys (see data/tag-labels.ts).
  const tagPairs = marketplaceTagPairs(item, locale);

  return (
    <aside className="detail-meta-sidebar">
      <dl className="detail-meta-list">
        <div className="detail-meta-row">
          <dt>{t("类型")}</dt>
          <dd>
            <StatusBadge tone={KIND_TONE[kindKey] ?? "neutral"}>
              {kindLabel ? t(kindLabel) : kindKey}
            </StatusBadge>
          </dd>
        </div>

        {item.scene ? (
          <div className="detail-meta-row">
            <dt>{t("场景")}</dt>
            <dd>{sceneLabel ? t(sceneLabel) : item.scene}</dd>
          </div>
        ) : null}

        {item.source ? (
          <div className="detail-meta-row">
            <dt>{t("来源")}</dt>
            <dd>
              {item.sourceUrl ? (
                <MarketplaceExternalLink href={item.sourceUrl} className="detail-meta-link">
                  {sourceLabel ? t(sourceLabel) : item.source}
                  <ExternalLink size={11} aria-hidden="true" />
                </MarketplaceExternalLink>
              ) : (
                sourceLabel ? t(sourceLabel) : item.source
              )}
            </dd>
          </div>
        ) : null}

        {item.repositoryUrl ? (
          <div className="detail-meta-row">
            <dt>{t("GitHub 仓库")}</dt>
            <dd>
              <MarketplaceExternalLink href={item.repositoryUrl} className="detail-meta-link">
                {t("查看仓库")} <ExternalLink size={11} aria-hidden="true" />
              </MarketplaceExternalLink>
            </dd>
          </div>
        ) : null}

        {item.documentationUrl ? (
          <div className="detail-meta-row">
            <dt>{t("文档")}</dt>
            <dd>
              <MarketplaceExternalLink href={item.documentationUrl} className="detail-meta-link">
                {t("查看文档")} <ExternalLink size={11} aria-hidden="true" />
              </MarketplaceExternalLink>
            </dd>
          </div>
        ) : null}

        {item.githubLicense || item.githubUpdatedAt ? (
          <div className="detail-meta-row">
            <dt>{t("项目数据")}</dt>
            <dd className="detail-meta-stack">
              {item.githubStars !== undefined ? <span>{t("{count} Stars", { count: formatCount(item.githubStars) })}</span> : null}
              {item.githubForks !== undefined ? <span>{t("{count} Forks", { count: formatCount(item.githubForks) })}</span> : null}
              {item.githubLicense ? <span>{item.githubLicense}</span> : null}
              {item.githubUpdatedAt ? <span>{item.githubUpdatedAt.slice(0, 10)}</span> : null}
            </dd>
          </div>
        ) : null}

        <div className="detail-meta-row">
          <dt>{t("需要 API Key")}</dt>
          <dd>
            {item.requiresApiKey ? (
              <span className="detail-meta-apikey is-required">{t("是")}</span>
            ) : (
              <span className="detail-meta-apikey">{t("否")}</span>
            )}
          </dd>
        </div>

        {tagPairs.length ? (
          <div className="detail-meta-row detail-meta-tags-row">
            <dt>{t("标签")}</dt>
            <dd>
              <ul className="detail-tags">
                {tagPairs.map(([key, label]) => (
                  <li key={key} className="marketplace-tag">{label}</li>
                ))}
              </ul>
            </dd>
          </div>
        ) : null}
      </dl>

      {item.source === "skillhub" ? (
        <SkillhubDetailSection slug={item.id.replace(/^skillhub-/, "")} />
      ) : null}

      {item.externalUrl ? (
        <MarketplaceExternalLink
          className="button button-primary detail-visit-btn"
          href={item.externalUrl}
        >
          <ExternalLink size={15} />
          {t("访问官网")}
        </MarketplaceExternalLink>
      ) : null}
    </aside>
  );
}

// ── page ──────────────────────────────────────────────────────────────────────

export function MarketplaceDetailPage() {
  const { itemId = "" } = useParams();
  const navigate = useNavigate();
  const { t, locale } = useI18n();

  const { items } = useMarketplaceCatalog();
  const item = items.find((i) => i.id === decodeURIComponent(itemId));

  if (!item) {
    return (
      <PageScaffold
        title={t("工具未找到")}
        onBack={() => navigate("/marketplace")}
        backLabel={t("返回工具市场")}
      >
        <p className="detail-not-found">{t("找不到该工具，它可能已被移除。")}</p>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold
      title=""
      bodyClassName="detail-page"
      onBack={() => navigate("/marketplace")}
      backLabel={t("返回工具市场")}
    >
      {/* Hero header */}
      <div className="detail-hero">
        <ItemIcon name={item.icon} color={item.iconColor} size={32} iconUrl={item.iconUrl} />
        <div className="detail-hero-meta">
          <h1 className="detail-hero-title">{item.name}</h1>
          <p className="detail-hero-desc">
            {locale === "en" && item.descriptionEn ? item.descriptionEn : item.description}
          </p>
          {/* Stats strip, skillhub-style: stars / downloads / source */}
          {(item.stars ?? 0) > 0 || (item.downloads ?? 0) > 0 ? (
            <div className="detail-hero-stats">
              {(item.stars ?? 0) > 0 ? (
                <span className="detail-stat">
                  <Star size={13} aria-hidden="true" />
                  {formatCount(item.stars!)}
                </span>
              ) : null}
              {(item.downloads ?? 0) > 0 ? (
                <span className="detail-stat">
                  <Download size={13} aria-hidden="true" />
                  {formatCount(item.downloads!)}
                </span>
              ) : null}
              {item.sourceLabel ? (
                <span className="detail-stat detail-stat-source">{item.sourceLabel}</span>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>

      {/* Two-column body */}
      <div className="detail-body">
        <div className="detail-main">
          {item.installPrompt ? (
            <InstallSection item={item} />
          ) : item.externalUrl ? (
            <section className="detail-install-section">
              <h2 className="detail-section-title">{t("访问方式")}</h2>
              <p className="detail-install-desc">{t("点击下方按钮访问该工具的官方页面。")}</p>
              <MarketplaceExternalLink
                className="button button-primary"
                href={item.externalUrl}
              >
                <ExternalLink size={15} />
                {t("访问官网")}
              </MarketplaceExternalLink>
            </section>
          ) : null}

          {item.type === "installable" && item.installableKind === "skill" ? (
            <Link className="detail-management-link" to="/skills">
              {t("安装完成后，可在 Skills 页管理它。")}
              <ArrowRight size={13} aria-hidden="true" />
            </Link>
          ) : null}

          {item.source === "skillhub" || item.readmeUrl ? (
            <section className="detail-readme-section">
              <h2 className="detail-section-title">{t("README")}</h2>
              {item.source === "skillhub" ? (
                <ReadmeSection skillhubSlug={item.id.replace(/^skillhub-/, "")} />
              ) : item.readmeUrl ? (
                <ReadmeSection readmeUrl={item.readmeUrl} />
              ) : null}
            </section>
          ) : null}
        </div>

        <MetaSidebar item={item} />
      </div>
    </PageScaffold>
  );
}
