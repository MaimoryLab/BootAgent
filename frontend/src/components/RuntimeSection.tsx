import { Boxes, Download, RefreshCw } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import type { RuntimeStatus } from "../types/api";
import { MirrorSetting } from "./MirrorSetting";
import { StatusBadge } from "./StatusBadge";

interface RuntimeSectionProps {
  runtimes: RuntimeStatus[];
  /** Called after a successful install so the caller can refresh status. */
  onInstalled: () => void | Promise<void>;
}

export function RuntimeSection({ runtimes, onInstalled }: RuntimeSectionProps) {
  const { t } = useI18n();
  const [pending, setPending] = useState("");
  const [failure, setFailure] = useState("");

  const supported = runtimes.filter((runtime) => runtime.supported || runtime.installed);
  if (!supported.length) return null;
  const missing = supported.filter((runtime) => !runtime.installed);

  const install = async (runtimeId: string) => {
    setPending(runtimeId);
    setFailure("");
    try {
      await api.installRuntime(runtimeId);
      await onInstalled();
    } catch (error) {
      setFailure(describeError(error, t("运行时安装失败")).message);
    } finally {
      setPending("");
    }
  };

  return (
    <section className="overview-section">
      <div className="section-heading">
        <div>
          <h2>{t("运行时")}</h2>
          <p>
            {missing.length
              ? t("缺少 {count} 个运行时，安装后即可自动安装对应 Agent。", { count: missing.length })
              : t("Agent 安装所需的运行时都已就绪。")}
          </p>
        </div>
        <Boxes size={19} aria-hidden="true" />
      </div>
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
          </div>
        ))}
      </div>
      {missing.length ? (
        <p className="runtime-note">
          {t("运行时会安装到 ~/.oneagent/runtimes，并写入登录 PATH，不需要管理员权限。")}
        </p>
      ) : null}
      <MirrorSetting label={t("下载源")} />
    </section>
  );
}
