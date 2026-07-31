import { KeyRound, UserCheck } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { ChoiceRow } from "../components/ChoiceRow";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

export function ConfigModePage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  return (
    <PageScaffold
      title={t("配置方式")}
      description={t("选择由 OneAgent 初始化模型服务，或保留已有官方账号与本机配置。")}
      stepper
      onBack={() => navigate("/setup/agents")}
      primaryLabel={t("继续")}
      onPrimary={() => navigate(state.configMode === "existing-account" ? "/setup/review" : "/setup/provider")}
      primaryDisabled={!state.configMode}
      footerNote={state.configMode === "existing-account" ? t("Provider 与模型步骤将标记为已跳过") : undefined}
    >
      <div className="choice-list">
        <ChoiceRow
          selected={state.configMode === "provider"}
          title={t("配置模型服务")}
          description={t("选择 PPIO、Novita 或自定义 OpenAI-compatible 服务。")}
          detail={t("OneAgent 将测试连接、发现模型并写入本机配置。")}
          icon={KeyRound}
          onSelect={() => dispatch({ type: "SET_CONFIG_MODE", value: "provider" })}
        />
        <ChoiceRow
          selected={state.configMode === "existing-account"}
          title={t("使用已有账号或配置")}
          description={t("只检测或安装 Agent，不写入第三方模型服务配置。")}
          detail={t("适合已登录官方账号，或已经维护本机配置的用户。")}
          icon={UserCheck}
          onSelect={() => dispatch({ type: "SET_CONFIG_MODE", value: "existing-account" })}
        />
      </div>
      <div className="notice notice-info">{t("跳过配置不会删除或覆盖现有 Agent 设置。")}</div>
    </PageScaffold>
  );
}
