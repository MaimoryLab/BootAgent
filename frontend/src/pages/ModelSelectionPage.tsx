import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { ModelPicker } from "../components/ModelPicker";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

export function ModelSelectionPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  const requested = useRef(false);

  const modelMessage = state.modelsMessage.replace(/^Found (\d+) models\.$/, (_, count: string) => t("找到 {count} 个模型", { count }));

  const loadModels = useCallback(async () => {
    dispatch({ type: "MODELS_LOADING" });
    try {
      const result = await api.models({
        provider: state.provider,
        apiBaseUrl: "",
        // The backend resolves the empty request key from the selected Provider.
        apiKey: "",
      });
      dispatch({ type: "MODELS_RESULT", result });
    } catch (error) {
      dispatch({ type: "MODELS_FAILED", message: describeFailure(error, t("无法获取模型列表"), t).message });
    }
  }, [dispatch, state.provider, t]);

  useEffect(() => {
    // SetupGuard has already verified the key; only model loading lives here.
    if (!requested.current) {
      requested.current = true;
      void loadModels();
    }
  }, [loadModels]);

  return (
    <PageScaffold
      title={t("选择模型")}
      stepper
      onBack={() => navigate(state.profileId ? "/setup/profile" : "/setup/provider")}
      primaryLabel={t("继续")}
      onPrimary={() => navigate("/setup/review")}
      primaryDisabled={!state.model.trim() || state.modelsState === "loading"}
      secondaryAction={
        <button className="button button-secondary" type="button" onClick={() => void loadModels()} disabled={state.modelsState === "loading"}>
          <RefreshCw size={15} className={state.modelsState === "loading" ? "spin" : ""} />
          {t("刷新列表")}
        </button>
      }
    >
      {state.modelsState === "loading" ? <div className="loading-block"><span className="spinner" />{t("正在读取模型列表")}</div> : null}
      {modelMessage && state.modelsState !== "loading" ? (
        <div className={`notice ${state.modelsState === "success" ? "notice-success" : "notice-warning"}`}>{modelMessage}</div>
      ) : null}
      <ModelPicker models={state.models} value={state.model} onChange={(value) => dispatch({ type: "SET_MODEL", value })} />
    </PageScaffold>
  );
}
