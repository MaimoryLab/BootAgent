import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { ModelPicker } from "../components/ModelPicker";
import { PageScaffold } from "../components/PageScaffold";
import { useWizard } from "../state/WizardContext";

export function ModelSelectionPage() {
  const navigate = useNavigate();
  const { state, dispatch, secret } = useWizard();
  const requested = useRef(false);

  const loadModels = useCallback(async () => {
    dispatch({ type: "MODELS_LOADING" });
    try {
      const result = await api.models({
        provider: state.provider,
        apiBaseUrl: "",
        apiKey: secret.keyRef.current,
      });
      dispatch({ type: "MODELS_RESULT", result });
    } catch (error) {
      dispatch({ type: "MODELS_FAILED", message: describeError(error, "无法获取模型列表").message });
    }
  }, [dispatch, secret.keyRef, state.provider]);

  useEffect(() => {
    // SetupGuard has already verified the key; only model loading lives here.
    if (!requested.current) {
      requested.current = true;
      void loadModels();
    }
  }, [loadModels]);

  return (
    <PageScaffold
      title="选择模型"
      description="从当前 Key 可访问的模型中选择，接口不支持时可直接输入模型 ID。"
      stepper
      onBack={() => navigate("/setup/provider")}
      primaryLabel="继续"
      onPrimary={() => navigate("/setup/review")}
      primaryDisabled={!state.model.trim() || state.modelsState === "loading"}
      secondaryAction={
        <button className="button button-secondary" type="button" onClick={() => void loadModels()} disabled={state.modelsState === "loading"}>
          <RefreshCw size={15} className={state.modelsState === "loading" ? "spin" : ""} />
          刷新列表
        </button>
      }
    >
      {state.modelsState === "loading" ? <div className="loading-block"><span className="spinner" />正在读取模型列表</div> : null}
      {state.modelsMessage && state.modelsState !== "loading" ? (
        <div className={`notice ${state.modelsState === "success" ? "notice-success" : "notice-warning"}`}>{state.modelsMessage}</div>
      ) : null}
      <ModelPicker models={state.models} value={state.model} onChange={(value) => dispatch({ type: "SET_MODEL", value })} />
    </PageScaffold>
  );
}
