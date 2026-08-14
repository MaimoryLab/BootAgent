import { ShieldCheck } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { ReviewGroup, ReviewRow } from "../components/ReviewGroup";
import { useI18n } from "../i18n";
import { profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import { wizardNeedsModel } from "../state/wizardReducer";

export function ReviewPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  const needsModel = wizardNeedsModel(state);

  const selectedCatalog = useMemo(
    () => state.selectedAgentIds.flatMap((id) => state.status?.catalog.find((agent) => agent.id === id) ?? []),
    [state.selectedAgentIds, state.status],
  );
  const automatic = selectedCatalog.filter((agent) => agent.configMode === "auto");
  const guideOnly = selectedCatalog.filter((agent) => agent.guideOnly);
  const desktop = state.setupKind === "desktop" && state.status
    ? selectedDesktopApp(state.status, state.selectedAgentIds)
    : undefined;
  const providerName = state.status?.providers[state.provider]?.name || state.provider;
  const providerHasKey = Boolean(state.status?.providers[state.provider]?.has_key);
  // Default name only; the user can override it before installing. The id is
  // derived from the Agent and Provider, which is unique per pairing and needs
  // no separate field.
  const targetID = desktop ? profileAgentIdForDesktop(desktop) : state.selectedAgentIds[0] ?? "agent";
  const targetName = desktop?.name || selectedCatalog[0]?.name || state.selectedAgentIds[0] || "";
  const defaultId = `${targetID}-${state.provider}`.replace(/[^a-z0-9_-]/gi, "-").toLowerCase();
  const defaultLabel = `${targetName} · ${providerName}`;
  const profileLabel = state.profileLabel || defaultLabel;

  const startActivation = () => {
    // The install request intentionally carries no key; the backend resolves
    // it from the selected Provider. Keep the review gate in sync with that
    // source of truth.
    if (!providerHasKey) {
      navigate("/setup/provider");
      return;
    }
    if (!state.profileId) dispatch({ type: "SET_PROFILE_ID", value: defaultId });
    if (!state.profileLabel) dispatch({ type: "SET_PROFILE_LABEL", value: defaultLabel });
    dispatch({ type: "REQUEST_ACTIVATION" });
    navigate("/setup/activation");
  };

  return (
    <PageScaffold
      title={t("确认激活")}
      description={t("核对安装、配置和备份范围。API Key 不会显示在此页")}
      stepper
      onBack={() => navigate(state.profileId ? "/setup/profile" : needsModel ? "/setup/model" : "/setup/provider")}
      primaryLabel={t("开始安装")}
      onPrimary={startActivation}
      footerNote={<span className="secure-note"><ShieldCheck size={15} />{t("覆盖前会自动创建时间戳备份")}</span>}
    >
      <div className="review-columns">
        <ReviewGroup title={t("将处理")}>
          {desktop ? (
            <ReviewRow label={desktop.name} value={desktop.installed ? t("检测并配置") : t("安装并配置")} />
          ) : selectedCatalog.map((agent) => (
            <ReviewRow
              key={agent.id}
              label={agent.name}
              value={state.status?.agents[agent.id]?.installed ? t("检测并配置") : agent.guideOnly ? t("显示引导") : t("安装并配置")}
            />
          ))}
        </ReviewGroup>

        <ReviewGroup title={t("模型服务")}>
          <ReviewRow label={t("模型服务")} value={providerName} />
          {needsModel
            ? <ReviewRow label={t("模型")} value={state.model} />
            // Stated rather than omitted: a missing row reads as something the
            // wizard forgot to collect, and the user would go looking for it.
            : <ReviewRow label={t("模型")} value={t("由 Agent 自行选择")} />}
          <ReviewRow label="Base URL" value={state.status?.providers[state.provider]?.base_url || ""} />
        </ReviewGroup>

        <ReviewGroup title={t("本地写入")}>
          {desktop ? <ReviewRow label={t("桌面应用配置")} value={desktop.configPath || t("由桌面 Agent 决定")} /> : null}
          {automatic.map((agent) => (
            <ReviewRow key={agent.id} label={agent.name} value={state.status?.paths[`${agent.id}_config`] || t("由 Agent 官方配置合约决定")} />
          ))}
          <ReviewRow label={t("环境摘要")} value={state.status?.paths.profile || "~/.bootagent/profile.json"} />
          {guideOnly.length ? <ReviewRow label={t("仅引导项目")} value={t("{count} 个，不写私有配置", { count: guideOnly.length })} muted /> : null}
        </ReviewGroup>
      </div>

      {!state.reusedProfile ? <div className="field-stack">
        <label htmlFor="review-profile-label">{t("配置模版名称")}</label>
        <input
          id="review-profile-label"
          className="text-field"
          value={profileLabel}
          onChange={(event) => dispatch({ type: "SET_PROFILE_LABEL", value: event.target.value })}
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="none"
        />
        <small>{t("这次安装会保存为一个配置模版，之后可以直接应用")}</small>
      </div> : null}
    </PageScaffold>
  );
}
