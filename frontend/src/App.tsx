import { Navigate, Route, Routes } from "react-router-dom";

import { AppWindow } from "./components/AppWindow";
import { ActivationPage } from "./pages/ActivationPage";
import { AgentDetailPage } from "./pages/AgentDetailPage";
import { AgentSelectionPage } from "./pages/AgentSelectionPage";
import { EnvironmentOverviewPage } from "./pages/EnvironmentOverviewPage";
import { ModelSelectionPage } from "./pages/ModelSelectionPage";
import { ProfilesPage } from "./pages/ProfilesPage";
import { ProviderKeyPage } from "./pages/ProviderKeyPage";
import { ProvidersPage } from "./pages/ProvidersPage";
import { ReviewPage } from "./pages/ReviewPage";
import { I18nProvider, useI18n } from "./i18n";
import { TaskCenterProvider } from "./state/TaskCenterContext";
import { WizardProvider, useWizard } from "./state/WizardContext";

function SetupGuard({ stage, children }: { stage: "provider" | "model" | "review" | "activation"; children: React.ReactNode }) {
  const { state } = useWizard();
  if (!state.selectedAgentIds.length) return <Navigate to="/setup/agents" replace />;
  if (stage === "model" && (!state.hasApiKey || !state.keyVerified)) {
    // A non-empty key alone proves nothing: only a successful probe unlocks the
    // model step. Editing the key or switching Provider clears keyVerified, so a
    // stale verdict cannot reach this guard.
    //
    // Only this step is gated on the key. A finished install clears it on
    // purpose, and gating the later steps too would eject the user from the
    // results page the moment it succeeded; ReviewPage's start handler sends
    // them back for a key instead.
    return <Navigate to="/setup/provider" replace />;
  }
  if ((stage === "review" || stage === "activation") && !state.model) {
    return <Navigate to="/setup/model" replace />;
  }
  // The activation page only renders a run in progress or its outcome; a deep
  // link that has neither goes back to the review page instead of a dead end.
  if (stage === "activation" && state.activationState === "idle" && !state.activationRequested) {
    return <Navigate to="/setup/review" replace />;
  }
  return children;
}

/**
 * Landing route. A machine with no ~/.oneagent has nothing to show on the
 * overview, so it opens onboarding instead. The decision waits for the status
 * call: redirecting on a null status would send every launch to onboarding for
 * one frame and then bounce it back.
 */
function LandingRoute() {
  const { t } = useI18n();
  const { state } = useWizard();
  if (!state.status) {
    if (state.statusState === "error") return <Navigate to="/overview" replace />;
    return <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>;
  }
  return <Navigate to={state.status.firstRun ? "/setup/agents" : "/overview"} replace />;
}

function WorkspaceRoutes() {
  return (
    <AppWindow>
      <Routes>
        <Route path="/" element={<LandingRoute />} />
        <Route path="/setup/agents" element={<AgentSelectionPage />} />
        <Route path="/setup/provider" element={<SetupGuard stage="provider"><ProviderKeyPage /></SetupGuard>} />
        <Route path="/setup/model" element={<SetupGuard stage="model"><ModelSelectionPage /></SetupGuard>} />
        <Route path="/setup/review" element={<SetupGuard stage="review"><ReviewPage /></SetupGuard>} />
        <Route path="/setup/activation" element={<SetupGuard stage="activation"><ActivationPage /></SetupGuard>} />
        <Route path="/overview" element={<EnvironmentOverviewPage />} />
        <Route path="/agents/:agentId" element={<AgentDetailPage />} />
        <Route path="/providers" element={<ProvidersPage />} />
        <Route path="/providers/new" element={<ProvidersPage create />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </AppWindow>
  );
}

export default function App() {
  return (
    <I18nProvider>
      <TaskCenterProvider>
        <WizardProvider>
          <WorkspaceRoutes />
        </WizardProvider>
      </TaskCenterProvider>
    </I18nProvider>
  );
}
