import { useI18n } from "../i18n";
import type { StatusResponse } from "../types/api";

const OS_NAMES: Record<string, string> = { macos: "macOS", windows: "Windows", linux: "Linux" };

interface EnvironmentSummaryProps {
  status: StatusResponse;
  selectedAgentIds: string[];
}

/**
 * What was found on this machine, shown before the first choice. A missing
 * runtime is only a problem for an Agent that installs through it, so each one
 * says whether the current selection needs it instead of reading as a fault.
 */
export function EnvironmentSummary({ status, selectedAgentIds }: EnvironmentSummaryProps) {
  const { t } = useI18n();
  const runtimes = (status.runtimes ?? []).filter((runtime) => runtime.supported || runtime.installed);
  const needed = new Set(
    selectedAgentIds
      .filter((id) => !status.agents?.[id]?.installed)
      .map((id) => status.capabilities.missingRuntime?.[id])
      .filter(Boolean),
  );
  // Counted from the two lists the page shows. status.agents also holds the
  // profile owners of desktop Agents, which would count those twice.
  const installed = (status.catalog ?? []).filter((item) => status.agents?.[item.id]?.installed).length
    + (status.desktopAgents ?? []).filter((app) => app.installed).length;

  return (
    <div className="environment-summary" role="group" aria-label={t("本机环境")}>
      <span>
        <small>{t("系统")}</small>
        {OS_NAMES[status.platform.os] ?? status.platform.os} · {status.platform.arch}
      </span>
      {runtimes.map((runtime) => (
        <span key={runtime.id} className={!runtime.installed && needed.has(runtime.id) ? "is-needed" : undefined}>
          <small>{runtime.name}</small>
          {runtime.installed
            ? runtime.version ? t("版本 {version}", { version: runtime.version }) : t("已安装")
            : !selectedAgentIds.length ? t("未安装")
            : needed.has(runtime.id) ? t("未安装，当前选择需要")
            : t("未安装，当前选择用不到")}
        </span>
      ))}
      <span>
        <small>{t("已安装的 Agent")}</small>
        {installed ? t("{count} 个", { count: installed }) : t("还没有")}
      </span>
    </div>
  );
}
