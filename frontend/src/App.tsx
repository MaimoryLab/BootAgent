import { Navigate, Route, Routes } from "react-router-dom";

import { AppWindow } from "./components/AppWindow";
import { ActivationPage } from "./pages/ActivationPage";
import { AgentSelectionPage } from "./pages/AgentSelectionPage";
import { ConfigModePage } from "./pages/ConfigModePage";
import { EnvironmentOverviewPage } from "./pages/EnvironmentOverviewPage";
import { ModelSelectionPage } from "./pages/ModelSelectionPage";
import { ProviderKeyPage } from "./pages/ProviderKeyPage";
import { ReviewPage } from "./pages/ReviewPage";
import { useWizard } from "./state/WizardContext";

function SetupGuard({ stage, children }: { stage: "mode" | "provider" | "model" | "review" | "activation"; children: React.ReactNode }) {
  const { state } = useWizard();
  if (!state.selectedAgentIds.length) return <Navigate to="/setup/agents" replace />;
  if (stage === "mode") return children;
  if (!state.configMode) return <Navigate to="/setup/mode" replace />;
  if ((stage === "provider" || stage === "model") && state.configMode === "existing-account") {
    return <Navigate to="/setup/review" replace />;
  }
  if (stage === "model" && !state.hasApiKey) {
    return <Navigate to="/setup/provider" replace />;
  }
  if ((stage === "review" || stage === "activation") && state.configMode === "provider" && !state.model) {
    return <Navigate to="/setup/model" replace />;
  }
  // The activation page only renders a run in progress or its outcome; a deep
  // link that has neither goes back to the review page instead of a dead end.
  if (stage === "activation" && state.activationState === "idle" && !state.activationRequested) {
    return <Navigate to="/setup/review" replace />;
  }
  return children;
}

export default function App() {
  return (
    <AppWindow>
      <Routes>
        <Route path="/" element={<Navigate to="/setup/agents" replace />} />
        <Route path="/setup/agents" element={<AgentSelectionPage />} />
        <Route path="/setup/mode" element={<SetupGuard stage="mode"><ConfigModePage /></SetupGuard>} />
        <Route path="/setup/provider" element={<SetupGuard stage="provider"><ProviderKeyPage /></SetupGuard>} />
        <Route path="/setup/model" element={<SetupGuard stage="model"><ModelSelectionPage /></SetupGuard>} />
        <Route path="/setup/review" element={<SetupGuard stage="review"><ReviewPage /></SetupGuard>} />
        <Route path="/setup/activation" element={<SetupGuard stage="activation"><ActivationPage /></SetupGuard>} />
        <Route path="/overview" element={<EnvironmentOverviewPage />} />
        <Route path="*" element={<Navigate to="/setup/agents" replace />} />
      </Routes>
    </AppWindow>
  );
}
