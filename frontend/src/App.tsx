import { Navigate, Route, Routes } from "react-router-dom";

import { AppWindow } from "./components/AppWindow";
import { ActivationPage } from "./pages/ActivationPage";
import { AgentDetailPage } from "./pages/AgentDetailPage";
import { AgentSelectionPage } from "./pages/AgentSelectionPage";
import { ConfigModePage } from "./pages/ConfigModePage";
import { EnvironmentOverviewPage } from "./pages/EnvironmentOverviewPage";
import { ModelSelectionPage } from "./pages/ModelSelectionPage";
import { ProfilesPage } from "./pages/ProfilesPage";
import { ProviderKeyPage } from "./pages/ProviderKeyPage";
import { ProvidersPage } from "./pages/ProvidersPage";
import { ReviewPage } from "./pages/ReviewPage";
import { I18nProvider } from "./i18n";
import { WizardProvider, useWizard } from "./state/WizardContext";

function SetupGuard({ stage, children }: { stage: "mode" | "provider" | "model" | "review" | "activation"; children: React.ReactNode }) {
  const { state } = useWizard();
  if (!state.selectedAgentIds.length) return <Navigate to="/setup/agents" replace />;
  if (stage === "mode") return children;
  if (!state.configMode) return <Navigate to="/setup/mode" replace />;
  if ((stage === "provider" || stage === "model") && state.configMode === "existing-account") {
    return <Navigate to="/setup/review" replace />;
  }
  if (stage === "model" && (!state.hasApiKey || state.connectionState !== "success")) {
    // A non-empty key alone proves nothing: only a successful probe unlocks the
    // model step. Editing the key resets connectionState, so a stale verdict
    // cannot reach this guard.
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

function WorkspaceRoutes() {
  return (
    <AppWindow>
      <Routes>
        <Route path="/" element={<Navigate to="/overview" replace />} />
        <Route path="/setup/agents" element={<AgentSelectionPage />} />
        <Route path="/setup/mode" element={<SetupGuard stage="mode"><ConfigModePage /></SetupGuard>} />
        <Route path="/setup/provider" element={<SetupGuard stage="provider"><ProviderKeyPage /></SetupGuard>} />
        <Route path="/setup/model" element={<SetupGuard stage="model"><ModelSelectionPage /></SetupGuard>} />
        <Route path="/setup/review" element={<SetupGuard stage="review"><ReviewPage /></SetupGuard>} />
        <Route path="/setup/activation" element={<SetupGuard stage="activation"><ActivationPage /></SetupGuard>} />
        <Route path="/overview" element={<EnvironmentOverviewPage />} />
        <Route path="/agents/:agentId" element={<AgentDetailPage />} />
        <Route path="/providers" element={<ProvidersPage />} />
        <Route path="/providers/new" element={<ProvidersPage create />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="*" element={<Navigate to="/setup/agents" replace />} />
      </Routes>
    </AppWindow>
  );
}

export default function App() {
  return (
    <I18nProvider>
      <WizardProvider>
        <WorkspaceRoutes />
      </WizardProvider>
    </I18nProvider>
  );
}
