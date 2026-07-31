import { ShieldCheck } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { ReviewGroup, ReviewRow } from "../components/ReviewGroup";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

export function ReviewPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();

  const startActivation = () => {
    // A successful activation clears the key, so re-activating from here must
    // collect it again instead of submitting an empty one.
    if (state.configMode === "provider" && !state.hasApiKey) {
      navigate("/setup/provider");
      return;
    }
    dispatch({ type: "REQUEST_ACTIVATION" });
    navigate("/setup/activation");
  };
  const selectedCatalog = useMemo(
    () => state.selectedAgentIds.flatMap((id) => state.status?.catalog.find((agent) => agent.id === id) ?? []),
    [state.selectedAgentIds, state.status],
  );
  const automatic = selectedCatalog.filter((agent) => agent.configMode === "auto");
  const guideOnly = selectedCatalog.filter((agent) => agent.guideOnly);
  const providerName = state.configMode === "provider" ? state.status?.providers[state.provider]?.name || state.provider : t("已有账号 / 本机配置");

  return (
    <PageScaffold
      title={t("确认激活")}
      description={t("核对安装、配置和备份范围。API Key 不会显示在此页。")}
      stepper
      onBack={() => navigate(state.configMode === "provider" ? "/setup/model" : "/setup/mode")}
      primaryLabel={t("开始激活")}
      onPrimary={startActivation}
      footerNote={<span className="secure-note"><ShieldCheck size={15} />{t("覆盖前会自动创建时间戳备份")}</span>}
    >
      <div className="review-columns">
        <ReviewGroup title={t("将处理")}>
          {selectedCatalog.map((agent) => (
            <ReviewRow
              key={agent.id}
              label={agent.name}
              value={state.status?.agents[agent.id]?.installed ? t("检测并配置") : state.installMissingAgents && !agent.guideOnly ? t("安装并配置") : agent.guideOnly ? t("显示引导") : t("只写配置")}
            />
          ))}
        </ReviewGroup>

        <ReviewGroup title={t("模型服务")}>
          <ReviewRow label={t("配置方式")} value={providerName} />
          {state.configMode === "provider" ? <ReviewRow label={t("模型")} value={state.model} /> : null}
          {state.configMode === "provider" ? (
            <ReviewRow
              label="Base URL"
              value={state.status?.providers[state.provider]?.base_url || ""}
            />
          ) : null}
        </ReviewGroup>

        <ReviewGroup title={t("本地写入")}>
          {state.configMode === "provider"
            ? automatic.map((agent) => (
                <ReviewRow key={agent.id} label={agent.name} value={state.status?.paths[`${agent.id}_config`] || t("由 Agent 官方配置合约决定")} />
              ))
            : <ReviewRow label={t("模型配置")} value={t("跳过，不覆盖已有设置")} muted />}
          <ReviewRow label={t("环境摘要")} value={state.status?.paths.profile || "~/.oneagent/profile.json"} />
          {guideOnly.length ? <ReviewRow label={t("仅引导项目")} value={t("{count} 个，不写私有配置", { count: guideOnly.length })} muted /> : null}
        </ReviewGroup>
      </div>
    </PageScaffold>
  );
}
