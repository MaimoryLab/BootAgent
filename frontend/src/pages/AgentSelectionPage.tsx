import { ChevronDown, PackageCheck } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { AgentRow } from "../components/AgentRow";
import { PageScaffold } from "../components/PageScaffold";
import { splitByRank } from "../state/ranking";
import type { AgentCatalogItem } from "../types/api";
import { useWizard } from "../state/WizardContext";

export function AgentSelectionPage() {
  const navigate = useNavigate();
  const { state, dispatch } = useWizard();
  const [showMore, setShowMore] = useState(false);
  // Ranked, not grouped by catalog group. Leading with the "auto" group put Kilo
  // and Aider on the first screen and folded Cursor, OpenClaw and Hermes away.
  // Guide-only Agents stay selectable: install_many answers them with a
  // guide-only result and writes nothing, which is the flow Review relies on.
  const { primary, secondary } = useMemo(() => splitByRank(state.status?.catalog), [state.status]);

  const renderRows = (agents: readonly AgentCatalogItem[]) => (
    <div className="agent-list">
      {agents.map((agent) => (
        <AgentRow
          key={agent.id}
          agent={agent}
          status={state.status?.agents[agent.id]}
          selected={state.selectedAgentIds.includes(agent.id)}
          onToggle={() => dispatch({ type: "TOGGLE_AGENT", agentId: agent.id })}
        />
      ))}
    </div>
  );

  return (
    <PageScaffold
      title="选择 Agent"
      description="选择要检测、安装或配置的开发工具，可以同时处理多个。"
      stepper
      primaryLabel="继续"
      onPrimary={() => navigate("/setup/mode")}
      primaryDisabled={!state.selectedAgentIds.length || state.statusState === "loading"}
      footerNote={state.selectedAgentIds.length ? `已选择 ${state.selectedAgentIds.length} 个 Agent` : "至少选择一个 Agent"}
      bodyClassName="agent-selection-body"
    >
      {state.statusState === "loading" ? <div className="loading-block"><span className="spinner" />正在检测本机环境</div> : null}
      {state.statusError ? <div className="notice notice-error">{state.statusError}</div> : null}
      {state.status ? (
        <>
          <section className="content-section">
            <div className="section-heading">
              <div>
                <h2>常用 Agent</h2>
                <p>可一键配置的使用锁定版本完成初始化，仅引导的只显示官方步骤。</p>
              </div>
              <PackageCheck size={19} aria-hidden="true" />
            </div>
            {renderRows(primary)}
          </section>

          <section className={`disclosure-section${showMore ? " is-open" : ""}`}>
            <button type="button" className="disclosure-trigger" onClick={() => setShowMore((value) => !value)} aria-expanded={showMore}>
              <ChevronDown size={18} />
              更多 Agent（{secondary.length}）
              <span>网关、平台账号与 IDE 扩展</span>
            </button>
            {showMore ? <div className="additional-agent-groups">{renderRows(secondary)}</div> : null}
          </section>

          <label className="toggle-row">
            <span>
              <strong>安装缺失的 Agent</strong>
              <small>仅调用 lock manifest 中允许的官方 npm 或 uv 包。</small>
            </span>
            <input
              type="checkbox"
              role="switch"
              checked={state.installMissingAgents}
              onChange={(event) => dispatch({ type: "SET_INSTALL_MISSING", value: event.target.checked })}
            />
          </label>
        </>
      ) : null}
    </PageScaffold>
  );
}
