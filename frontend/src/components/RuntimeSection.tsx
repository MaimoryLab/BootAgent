import { Download, RefreshCw } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import { useTaskCenter } from "../state/TaskCenterContext";
import type { RuntimeStatus } from "../types/api";
import { AdvancedSection } from "./AdvancedSection";
import { DownloadProgress } from "./DownloadProgress";
import { MirrorSetting } from "./MirrorSetting";
import { StatusBadge } from "./StatusBadge";

interface RuntimeSectionProps {
  runtimes: RuntimeStatus[];
  /** Called after a successful install so the caller can refresh status. */
  onInstalled: () => void | Promise<void>;
}

/**
 * The directory the runtimes land in, taken from the backend rather than spelled
 * out here. `installPath` is an absolute, platform-correct path — on Windows it
 * reads C:\Users\<name>\.oneagent\runtimes\..., which a hardcoded "~/.oneagent"
 * would misreport. Two segments come off the end (the runtime id and its
 * versioned directory) to name the shared parent.
 */
export function runtimeRoot(runtimes: RuntimeStatus[]): string {
  for (const runtime of runtimes) {
    // Split on either separator rather than picking one for the whole string: a
    // path that mixes them (a drive letter followed by forward slashes) would
    // otherwise collapse to "C:" and render that as the install directory.
    const segments = runtime.installPath?.split(/[/\\]/) ?? [];
    if (segments.length < 3) continue;
    const separator = runtime.installPath.includes("\\") ? "\\" : "/";
    const parent = segments.slice(0, -2).join(separator);
    if (parent) return parent;
  }
  return "";
}

export function RuntimeSection({ runtimes, onInstalled }: RuntimeSectionProps) {
  const { t } = useI18n();
  const { resetProgress } = useTaskCenter();
  const [pending, setPending] = useState("");
  const [failure, setFailure] = useState("");

  const supported = runtimes.filter((runtime) => runtime.supported || runtime.installed);
  if (!supported.length) return null;
  const missing = supported.filter((runtime) => !runtime.installed);
  // Any supported runtime carries the same managed root, so the whole list is
  // the source rather than just the missing ones.
  const root = runtimeRoot(supported);

  const install = async (runtimeId: string) => {
    setPending(runtimeId);
    setFailure("");
    // A retry must not open on the previous attempt's finished bar.
    resetProgress(runtimeId);
    try {
      await api.installRuntime(runtimeId);
      await onInstalled();
    } catch (error) {
      setFailure(describeError(error, t("运行时安装失败")).message);
    } finally {
      setPending("");
      resetProgress(runtimeId);
    }
  };

  const summary = missing.length
    ? t("缺少 {count} 个运行时，安装后即可自动安装对应 Agent。", { count: missing.length })
    : t("Agent 安装所需的运行时都已就绪。");
  const body = (
    <>
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      <div className="runtime-list">
        {supported.map((runtime) => (
          <div className="runtime-row" key={runtime.id}>
            <span className="runtime-identity">
              <strong>{runtime.name}</strong>
              <small>
                {runtime.installed
                  ? runtime.version
                    ? t("版本 {version}", { version: runtime.version })
                    : t("版本未知")
                  : runtime.requiredByHint
                    ? t("{agents} 需要", { agents: runtime.requiredByHint })
                    : t("待安装")}
              </small>
            </span>
            <span className="runtime-fact">
              <small>{t("锁定版本")}</small>
              <span>{runtime.lockedVersion}</span>
            </span>
            <span className="runtime-fact">
              <small>{t("来源")}</small>
              <span>{runtime.managed ? t("由 OneAgent 安装") : runtime.installed ? t("本机已有") : runtime.source}</span>
            </span>
            <StatusBadge tone={runtime.installed ? "success" : "warning"}>
              {runtime.installed ? t("已安装") : t("未安装")}
            </StatusBadge>
            {runtime.installed ? (
              <span className="runtime-action-placeholder" aria-hidden="true" />
            ) : (
              <button
                className="button button-secondary"
                type="button"
                onClick={() => void install(runtime.id)}
                disabled={Boolean(pending)}
              >
                {pending === runtime.id ? <RefreshCw size={15} className="spin" /> : <Download size={15} />}
                {pending === runtime.id ? t("安装中") : t("安装")}
              </button>
            )}
            {pending === runtime.id ? <DownloadProgress runtimeId={runtime.id} pending /> : null}
          </div>
        ))}
      </div>
      {missing.length ? (
        <p className="runtime-note">
          {root
            ? t("运行时会安装到 {dir}，并写入登录 PATH，不需要管理员权限。").replace("{dir}", root)
            : t("运行时会安装到 OneAgent 的托管目录，并写入登录 PATH，不需要管理员权限。")}
        </p>
      ) : null}
      <MirrorSetting label={t("下载源")} />
    </>
  );

  return (
    <section className="overview-section">
      <AdvancedSection label={t("运行时")} hint={summary}>
        {body}
      </AdvancedSection>
    </section>
  );
}
