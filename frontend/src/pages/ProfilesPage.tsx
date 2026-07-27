import { KeyRound, Layers, Plus } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { useWizard } from "../state/WizardContext";

export function ProfilesPage() {
  const navigate = useNavigate();
  const { state } = useWizard();
  const status = state.status;

  if (!status) {
    return (
      <PageScaffold title="配置模板">
        <div className="loading-block"><span className="spinner" />正在读取环境状态</div>
      </PageScaffold>
    );
  }

  const profiles = status.profiles;
  const nameOf = (agentId: string) =>
    status.catalog.find((item) => item.id === agentId)?.name || agentId;
  const configured = Object.entries(status.agents).filter(([, agent]) => agent.provider);

  if (!profiles.length) {
    return (
      <PageScaffold
        title="配置模板"
        description="把一组 Provider 与模型存成模板，之后可以直接套用到别的 Agent。"
        primaryLabel={configured.length ? "从现有 Agent 创建" : undefined}
        onPrimary={configured.length ? () => navigate(`/agents/${configured[0][0]}`) : undefined}
      >
        <div className="empty-overview">
          <Layers size={26} />
          <strong>还没有配置模板</strong>
          <span>
            {configured.length
              ? "已配置的 Agent 可以直接存为模板，省去重复填写 Provider、模型和密钥。"
              : "先配置一个 Agent，就可以把那组设置存成模板。"}
          </span>
        </div>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold title="配置模板" description="套用模板不会改动其他 Agent。">
      <div className="profile-list">
        {profiles.map((profile) => (
          <article className="profile-card" key={profile.id} data-testid={`profile-${profile.id}`}>
            <header>
              <strong>{profile.label}</strong>
              {/* Whether a credential is held, never the credential: there is no
                  API that returns it and none should be added for display. */}
              <span className={`profile-key${profile.hasKey ? " has-key" : ""}`}>
                <KeyRound size={12} aria-hidden="true" />
                {profile.hasKey ? "已保存密钥" : "未保存密钥"}
              </span>
            </header>
            <p className="profile-target">
              {status.providers[profile.provider]?.name || profile.provider}
              <span aria-hidden="true"> · </span>
              {profile.model || "未指定模型"}
            </p>
            {profile.agentIds.length ? (
              <p className="profile-agents">
                适用：{profile.agentIds.map(nameOf).join("、")}
              </p>
            ) : null}
            <footer>
              <button
                className="button button-secondary"
                type="button"
                onClick={() => navigate(`/agents/${profile.agentIds[0] || configured[0]?.[0] || "codex"}?profile=${profile.id}`)}
              >
                <Plus size={14} />
                应用到 Agent
              </button>
            </footer>
          </article>
        ))}
      </div>
    </PageScaffold>
  );
}
