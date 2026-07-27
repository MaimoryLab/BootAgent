import { ExternalLink } from "lucide-react";

import { PageScaffold } from "../components/PageScaffold";
import { useWizard } from "../state/WizardContext";

export function ProvidersPage() {
  const { state } = useWizard();
  const status = state.status;

  if (!status) {
    return (
      <PageScaffold title="Provider">
        <div className="loading-block"><span className="spinner" />正在读取环境状态</div>
      </PageScaffold>
    );
  }

  const nameOf = (agentId: string) =>
    status.catalog.find((item) => item.id === agentId)?.name || agentId;

  return (
    <PageScaffold title="Provider" description="内置模型服务与当前指向它们的 Agent。">
      <div className="provider-list">
        {Object.entries(status.providers).map(([providerId, meta]) => {
          // Reverse lookup from the per-Agent bindings: this is what answers
          // "which Agents stop working if this endpoint does".
          const users = Object.entries(status.agents)
            .filter(([, agent]) => agent.provider === providerId)
            .map(([agentId]) => agentId);
          return (
            <article className="provider-card" key={providerId} data-testid={`provider-${providerId}`}>
              <header>
                <strong>{meta.name}</strong>
                <a className="provider-link" href={meta.home} target="_blank" rel="noreferrer">
                  <ExternalLink size={13} aria-hidden="true" />
                  官网
                </a>
              </header>
              <dl className="provider-endpoints">
                <div>
                  <dt>OpenAI 兼容</dt>
                  <dd>{meta.base_url}</dd>
                </div>
                {meta.anthropic_base_url ? (
                  <div>
                    <dt>Anthropic 兼容</dt>
                    <dd>{meta.anthropic_base_url}</dd>
                  </div>
                ) : null}
              </dl>
              <footer>
                {users.length ? (
                  <span className="provider-users">
                    {users.map((agentId) => (
                      <span className="provider-user-chip" key={agentId}>
                        {nameOf(agentId)}
                      </span>
                    ))}
                  </span>
                ) : (
                  <span className="provider-users is-empty">暂无 Agent 使用</span>
                )}
              </footer>
            </article>
          );
        })}
      </div>

      <p className="provider-note">
        也可以为单个 Agent 填写自定义 Base URL。自定义端点的协议兼容性由你自己保证，OneAgent 不会为它降级或改写请求。
      </p>
    </PageScaffold>
  );
}
