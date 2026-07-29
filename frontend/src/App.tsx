import { Navigate, Route, Routes } from "react-router-dom";

import { AppWindow } from "./components/AppWindow";
import { LandingPage } from "./pages/LandingPage";
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

function LandingRoute() {
  const { state } = useWizard();
  // Wait for the first status read before choosing. The fetch starts in an
  // effect, so the initial render has no status and a state of "idle" rather
  // than "loading" — treating that as "nothing configured" would show the
  // landing page to a returning user before their Agents ever loaded.
  if (!state.status && state.statusState !== "error") {
    return <div className="loading-block"><span className="spinner" />正在读取环境状态</div>;
  }
  // An Agent already pointed somewhere, or a previously activated profile,
  // means this is a returning user: send them to their own environment. The
  // landing page is for someone who has not configured anything yet, and has
  // nothing to tell a user whose Agents are already running.
  const configured =
    Boolean(state.status?.environment) ||
    Object.values(state.status?.agents ?? {}).some((agent) => agent.provider);
  if (configured) {
    return <Navigate to="/overview" replace />;
  }
  return <LandingPage />;
}

/** The product itself: every route that belongs inside the app window. */
function WorkspaceRoutes() {
  return (
    <AppWindow>
      <Routes>
        <Route path="/setup/agents" element={<AgentSelectionPage />} />
        <Route path="/setup/mode" element={<SetupGuard stage="mode"><ConfigModePage /></SetupGuard>} />
        <Route path="/setup/provider" element={<SetupGuard stage="provider"><ProviderKeyPage /></SetupGuard>} />
        <Route path="/setup/model" element={<SetupGuard stage="model"><ModelSelectionPage /></SetupGuard>} />
        <Route path="/setup/review" element={<SetupGuard stage="review"><ReviewPage /></SetupGuard>} />
        <Route path="/setup/activation" element={<SetupGuard stage="activation"><ActivationPage /></SetupGuard>} />
        <Route path="/overview" element={<EnvironmentOverviewPage />} />
        <Route path="/agents/:agentId" element={<AgentDetailPage />} />
        <Route path="/providers" element={<ProvidersPage />} />
        <Route path="/profiles" element={<ProfilesPage />} />
        <Route path="*" element={<Navigate to="/setup/agents" replace />} />
      </Routes>
    </AppWindow>
  );
}

export default function App() {
  // One provider around both, because "/" has to read status to know whether
  // this is a returning user. The landing page renders outside AppWindow: it is
  // a full-page document, not a view inside the app's window chrome.
  return (
    <WizardProvider>
      <Routes>
        <Route path="/" element={<LandingRoute />} />
        <Route path="*" element={<WorkspaceRoutes />} />
      </Routes>
    </WizardProvider>
  );
}
