import { PackageCheck } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { AgentRow } from "../components/AgentRow";
import { MirrorSetting } from "../components/MirrorSetting";
import { PageScaffold } from "../components/PageScaffold";
import { RuntimePrompt } from "../components/RuntimePrompt";
import { useI18n } from "../i18n";
import { byRank } from "../state/ranking";
import { useWizard } from "../state/WizardContext";
import type { AgentCatalogItem } from "../types/api";
import { DesktopAgentSelectionPage } from "./DesktopAgentSelectionPage";

export function AgentSelectionPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  // Keep every Agent available in one scrollable list, ordered by catalog rank.
  // Guide-only Agents stay selectable: install_many answers them with a
  // guide-only result and writes nothing, which is the flow Review relies on.
  const agents = useMemo(() => byRank(state.status?.catalog), [state.status]);

  const renderRows = (agents: readonly AgentCatalogItem[]) => (
      <div className="agent-list agent-selection-list">
      {agents.map((agent) => (
        <AgentRow
          key={agent.id}
          agent={agent}
          status={state.status?.agents[agent.id]}
          selected={state.selectedAgentIds.includes(agent.id)}
          onToggle={() => dispatch({ type: "SELECT_AGENT", agentId: agent.id })}
          single
        />
      ))}
    </div>
  );
  const selectedAgent = state.selectedAgentIds[0] ?? "";
  const selectedName = state.status?.catalog.find((item) => item.id === selectedAgent)?.name ?? selectedAgent;
  const continueSetup = () => {
    const protocol = state.status?.catalog.find((item) => item.id === selectedAgent)?.protocol;
    const hasProfile = Boolean(protocol && state.status?.profiles.some((profile) => profile.protocol === protocol));
    dispatch({ type: "SET_PROFILE_STEP_SKIPPED", value: !hasProfile });
    navigate(hasProfile ? "/setup/profile" : "/setup/provider");
  };

  // Desktop installation uses this same route and the same five-step shell;
  // its first-step row is different because the desktop app is not in the CLI
  // catalog.
  if (state.setupKind === "desktop") return <DesktopAgentSelectionPage />;

  return (
    <PageScaffold
      title={t("选择 Agent")}
      description={t("选择这次要安装并配置的开发工具，每次安装一个")}
      stepper
      primaryLabel={t("继续")}
      onPrimary={continueSetup}
      primaryDisabled={!selectedAgent || state.statusState === "loading"}
      footerNote={selectedAgent ? selectedName : t("选择一个 Agent")}
      bodyClassName="agent-selection-body"
    >
      {state.statusState === "loading" ? <div className="loading-block"><span className="spinner" />{t("正在检测本机环境")}</div> : null}
      {state.statusError ? <div className="notice notice-error">{state.statusError}</div> : null}
      {state.status ? (
        <>
          <section className="content-section">
            <div className="section-heading">
              <div>
                <h2>{t("选择 Agent")}</h2>
                <p>{t("选择这次要安装并配置的开发工具，每次安装一个")}</p>
                {/* Guide-only rows stay selectable: install_many answers them
                    with a guide-only result and writes nothing. */}
              </div>
              <PackageCheck size={19} aria-hidden="true" />
            </div>
            {renderRows(agents)}
          </section>

          <MirrorSetting label={t("Agent 安装源")} />

          {/* Installing a selected Agent needs its package manager. Offering the
              runtime here, before the wizard collects a key and a model, keeps the
              activation run from failing on a prerequisite the user cannot fix
              from the last step. */}
          <RuntimePrompt
            runtimes={state.status.runtimes ?? []}
            missingRuntime={state.status.capabilities.missingRuntime ?? {}}
            selectedAgentIds={state.selectedAgentIds}
            agents={state.status.agents}
            onInstalled={refreshStatus}
          />
        </>
      ) : null}
    </PageScaffold>
  );
}
