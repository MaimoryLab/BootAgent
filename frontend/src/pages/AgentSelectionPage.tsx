import { PackageCheck } from "lucide-react";
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";

import { AgentRow } from "../components/AgentRow";
import { EditionTag } from "../components/EditionTag";
import { AgentIcon } from "../components/icons/agents";
import { PageScaffold } from "../components/PageScaffold";
import { RuntimePrompt } from "../components/RuntimePrompt";
import { StatusBadge } from "../components/StatusBadge";
import { useI18n } from "../i18n";
import { desktopApps, desktopProtocol } from "../state/desktopSetup";
import { byRank } from "../state/ranking";
import { useWizard } from "../state/WizardContext";
import type { AgentCatalogItem } from "../types/api";

export function AgentSelectionPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  // Keep every Agent available in one scrollable list, ordered by catalog rank.
  // Guide-only Agents stay selectable: install_many answers them with a
  // guide-only result and writes nothing, which is the flow Review relies on.
  const agents = useMemo(() => byRank(state.status?.catalog), [state.status]);
  const desktop = state.status ? desktopApps(state.status) : [];

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
  const selectedDesktop = desktop.find((item) => item.id === selectedAgent);
  const selectedName = selectedDesktop?.name ?? state.status?.catalog.find((item) => item.id === selectedAgent)?.name ?? selectedAgent;
  const continueSetup = () => {
    const protocol = state.setupKind === "desktop"
      ? selectedDesktop && desktopProtocol(selectedDesktop)
      : state.status?.catalog.find((item) => item.id === selectedAgent)?.protocol;
    const hasProfile = Boolean(protocol && state.status?.profiles.some((profile) => profile.protocol === protocol));
    dispatch({ type: "SET_PROFILE_STEP_SKIPPED", value: !hasProfile });
    navigate(hasProfile ? "/setup/profile" : "/setup/provider");
  };

  return (
    <PageScaffold
      title={t("选择 Agent")}
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
                {/* Guide-only rows stay selectable: install_many answers them
                    with a guide-only result and writes nothing. */}
              </div>
              <PackageCheck size={19} aria-hidden="true" />
            </div>
            <div className="agent-tabs" role="tablist" aria-label={t("Agent 类型")}>
              <button className={`agent-tab${state.setupKind === "desktop" ? " is-active" : ""}`} role="tab" aria-selected={state.setupKind === "desktop"} type="button" onClick={() => dispatch({ type: "START_DESKTOP_SETUP" })}>{t("桌面 Agent")}</button>
              <button className={`agent-tab${state.setupKind !== "desktop" ? " is-active" : ""}`} role="tab" aria-selected={state.setupKind !== "desktop"} type="button" onClick={() => dispatch({ type: "START_SETUP" })}>{t("命令行 Agent")}</button>
            </div>
            {state.setupKind !== "desktop" ? renderRows(agents) : (
              <div className="agent-list agent-selection-list">
                {desktop.map((app) => {
                  const selected = selectedAgent === app.id;
                  return <label key={app.id} className={`agent-row${selected ? " is-selected" : ""}${!app.supported ? " is-disabled" : ""}`}>
                    <input type="radio" name="desktop-agent-choice" checked={selected} disabled={!app.supported} onChange={() => dispatch({ type: "SELECT_AGENT", agentId: app.id })} aria-label={t("选择 {name}", { name: app.name })} />
                    {/* The Agent's own mark, matching the CLI rows above and the
                        overview card. A literal AppWindow here gave every desktop
                        Agent the same glyph and downgraded ChatGPT Desktop, which
                        has a registered mark of its own. */}
                    <span className="agent-icon"><AgentIcon agentId={app.id} size={20} /></span>
                    <span className="agent-copy"><span className="agent-name-line"><strong>{app.name}</strong><EditionTag edition={app.edition} /></span><span>{app.installed
                      ? t("已安装，可直接应用配置模版")
                      : app.manualInstall ? t("配置完成后前往官网自行安装")
                      // Not "官方桌面应用" for a third-party build: the mark is the
                      // model vendor's, so without this the row would read as if
                      // the vendor published the download.
                      : app.unofficial ? t("第三方桌面应用，非官方出品")
                      : t("安装官方桌面应用")}</span></span>
                    <StatusBadge tone={app.installed ? "success" : app.supported ? "warning" : "neutral"}>{app.installed ? t("已安装") : app.supported ? t("待安装") : t("不支持")}</StatusBadge>
                  </label>;
                })}
              </div>
            )}
          </section>

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
