import { ExternalLink } from "lucide-react";
import { useEffect, useState } from "react";

import { api } from "../backend/api";
import { useI18n } from "../i18n";
import { MarketplaceExternalLink } from "./MarketplaceExternalLink";
import { StatusBadge } from "./StatusBadge";

/**
 * Extra detail blocks for skillhub items, fetched from the public skillhub
 * detail API:
 *
 *   GET https://api.skillhub.cn/api/v1/skills/{slug}
 *
 * The request goes through the Go MarketplaceService proxy first: the API
 * only echoes CORS headers for skillhub's own origins, so a direct browser
 * fetch is blocked in the packaged app. The direct fetch remains as the
 * fallback for environments without a Wails bridge (plain-browser dev).
 *
 * Renders security-review verdicts (Keen Lab / Sanbu Lab), latest-version
 * info and the author. Fails silently: any fetch/parse error renders null so
 * the detail page never blocks on this section.
 */

const DETAIL_API_BASE = "https://api.skillhub.cn/api/v1/skills/";

interface SecurityReport {
  status?: string;
  statusText?: string;
  reportUrl?: string;
}

interface SkillhubDetail {
  securityReports?: {
    keen?: SecurityReport;
    sanbu?: SecurityReport;
  };
  latestVersion?: {
    version?: string;
    changelog?: string;
    createdAt?: number;
  };
  owner?: {
    displayName?: string;
    handle?: string;
  };
  skill?: {
    stats?: {
      downloads?: number;
      installs?: number;
      stars?: number;
      comments?: number;
      versions?: number;
    };
    sourceUrl?: string;
  };
}

/** 12,345 -> "12.3k"; same compact format as the hero stats strip. */
function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

function SecurityReportRow({ labKey, report }: { labKey: "科恩实验室" | "三堡实验室"; report: SecurityReport }) {
  const { t } = useI18n();
  const text = report.statusText || report.status;
  if (!text) return null;
  return (
    <div className="detail-meta-row detail-skillhub-security-row">
      <dt>{t(labKey)}</dt>
      <dd>
        <StatusBadge tone={report.status === "benign" ? "success" : "warning"}>{text}</StatusBadge>
        {report.reportUrl ? (
          <MarketplaceExternalLink href={report.reportUrl} className="detail-meta-icon-link" aria-label={`${t(labKey)} - ${t("查看报告")}`}>
            <ExternalLink size={12} aria-hidden="true" />
          </MarketplaceExternalLink>
        ) : null}
      </dd>
    </div>
  );
}

export function SkillhubDetailSection({ slug }: { slug: string }) {
  const { t } = useI18n();
  const [state, setState] = useState<"loading" | "success" | "error">("loading");
  const [detail, setDetail] = useState<SkillhubDetail | null>(null);

  useEffect(() => {
    let cancelled = false;
    setState("loading");
    setDetail(null);

    // The timer only bounds the fallback fetch; the Go proxy enforces its own
    // 10s deadline server-side.
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 8000);

    const load = async (): Promise<SkillhubDetail> => {
      try {
        return JSON.parse(await api.marketplaceSkillDetail(slug)) as SkillhubDetail;
      } catch {
        // No Wails bridge or the proxy failed: try the direct fetch, which
        // works wherever CORS allows (plain-browser dev, not the packaged app).
        const res = await fetch(`${DETAIL_API_BASE}${encodeURIComponent(slug)}`, { signal: controller.signal });
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return (await res.json()) as SkillhubDetail;
      }
    };

    load()
      .then((data) => {
        if (cancelled) return;
        setDetail(data);
        setState("success");
      })
      .catch(() => {
        if (!cancelled) setState("error");
      })
      .finally(() => clearTimeout(timer));

    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [slug]);

  if (state === "loading") {
    return (
      <div className="readme-loading" role="status" aria-live="polite">
        <span className="spinner" aria-hidden="true" />
        {t("正在加载详情")}
      </div>
    );
  }

  // Silent degradation: broken network or unexpected payload hides the block.
  if (state === "error" || !detail) return null;

  const keen = detail.securityReports?.keen;
  const sanbu = detail.securityReports?.sanbu;
  const hasSecurity = Boolean(keen?.statusText || keen?.status || sanbu?.statusText || sanbu?.status);

  const version = detail.latestVersion?.version;
  const installs = detail.skill?.stats?.installs ?? 0;
  const comments = detail.skill?.stats?.comments ?? 0;
  const hasVersion = Boolean(version || installs > 0 || comments > 0);

  const authorName = detail.owner?.displayName || detail.owner?.handle;
  const sourceUrl = detail.skill?.sourceUrl;

  if (!hasSecurity && !hasVersion && !authorName) return null;

  return (
    <dl className="detail-skillhub-summary">
      {version ? (
        <div className="detail-meta-row">
          <dt>{t("最新版本")}</dt>
          <dd>{version}</dd>
        </div>
      ) : null}
      {installs > 0 ? (
        <div className="detail-meta-row">
          <dt>{t("安装量")}</dt>
          <dd>{formatCount(installs)}</dd>
        </div>
      ) : null}
      {comments > 0 ? (
        <div className="detail-meta-row">
          <dt>{t("评论数")}</dt>
          <dd>{formatCount(comments)}</dd>
        </div>
      ) : null}
      {authorName ? (
        <div className="detail-meta-row">
          <dt>{t("作者")}</dt>
          <dd>
            <span>{authorName}</span>
            {sourceUrl ? (
              <MarketplaceExternalLink href={sourceUrl} className="detail-meta-icon-link" aria-label={t("上游来源")}>
                <ExternalLink size={12} aria-hidden="true" />
              </MarketplaceExternalLink>
            ) : null}
          </dd>
        </div>
      ) : null}
      {hasSecurity ? (
        <>
          {keen ? <SecurityReportRow labKey="科恩实验室" report={keen} /> : null}
          {sanbu ? <SecurityReportRow labKey="三堡实验室" report={sanbu} /> : null}
        </>
      ) : null}
    </dl>
  );
}
