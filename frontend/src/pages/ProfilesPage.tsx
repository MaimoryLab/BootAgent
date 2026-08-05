import { KeyRound, Layers, Pencil, Play, Plus, Save, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { AgentRow } from "../components/AgentRow";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import type { ProfileSummary, ProviderId } from "../types/api";

// A Profile no longer carries its own key: the Provider it points at owns one,
// and asking twice for the same secret was the bug worth deleting.
interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  protocol: string;
  agentIds: string[];
  originalId: string;
}

function editDraft(profile: ProfileSummary): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    protocol: profile.protocol || "",
    agentIds: [...(profile.agentIds ?? [])],
    originalId: profile.id,
  };
}

export function ProfilesPage() {
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  const status = state.status;
  const [editor, setEditor] = useState<ProfileDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState("");
  const [failure, setFailure] = useState("");

  if (!status) {
    return (
      <PageScaffold title={t("配置模板")}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  const profiles = status.profiles;
  const configurableAgents = status.catalog.filter((agent) => agent.configMode === "auto");
  const nameOf = (agentId: string) =>
    status.catalog.find((item) => item.id === agentId)?.name || agentId;
  const protocolOf = (profile: ProfileSummary) => profile.protocol || status.catalog.find((agent) => (profile.agentIds ?? []).includes(agent.id))?.protocol || "";
  const providerHasKey = Boolean(editor && status.providers[editor.provider]?.has_key);
  // label is absent on purpose: the backend fills it in from the existing value
  // or the ID, so demanding it here was stricter than the write path.
  const canSave = Boolean(editor?.id.trim() && editor.model.trim() && (editor?.protocol || editor?.agentIds.length));

  // Creating a Profile goes through onboarding: it collects the Agent, Provider,
  // model and name in order, tests the saved Provider connection, and the
  // install writes the Profile itself.
  const startSetup = () => {
    dispatch({ type: "START_SETUP" });
    navigate("/setup/agents");
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor || !canSave) return;
    setBusy(true);
    setFailure("");
    try {
      await api.saveProfile({
        id: editor.id.trim(),
        label: editor.label.trim(),
        provider: editor.provider,
        apiBaseUrl: "",
        apiKey: "",
        model: editor.model.trim(),
        configMode: "provider",
        protocol: editor.protocol || status.catalog.find((agent) => editor.agentIds.includes(agent.id))?.protocol || "",
      });
      await refreshStatus();
      setEditor(null);
    } catch (error) {
      setFailure(describeError(error, t("无法保存 Profile")).message);
    } finally {
      setBusy(false);
    }
  };

  const apply = async (profile: ProfileSummary) => {
    const agents = configurableAgents.filter((agent) => agent.protocol === protocolOf(profile)).map((agent) => agent.id);
    if (!profile.model || !agents.length) return;
    setApplying(profile.id);
    setFailure("");
    try {
      const result = await api.install({
        agents,
        profile_agents: agents,
        provider: profile.provider,
        // The endpoint belongs to the Provider. Do not replay a stale copy
        // retained by an older Profile record.
        api_base_url: "",
        api_key: "",
        model: profile.model,
        profile_id: profile.id,
        configure: true,
        install_agent: true,
        skip_test: false,
      });
      if (!result.ok) {
        throw new Error(result.results.find((item) => item.status === "failed")?.message || t("应用 Profile 失败"));
      }
      await refreshStatus();
      navigate("/overview", { replace: true });
    } catch (error) {
      setFailure(describeError(error, t("无法应用 Profile")).message);
    } finally {
      setApplying("");
    }
  };

  return (
    <PageScaffold title={t("配置模板")} description={t("在这里创建 Profile，再将它应用到所选 Agent。")}>
      <div className="profile-toolbar">
        <button className="button button-secondary" type="button" onClick={startSetup}>
          <Plus size={15} />
          {t("新增 Profile")}
        </button>
      </div>

      {editor ? (
        <form className="profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{t("编辑 {name}", { name: editor.label || editor.id })}</strong>
            <button className="icon-button" type="button" onClick={() => setEditor(null)} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>

          <div className="profile-editor-grid">
            <div className="field-stack">
              <label htmlFor="profile-id">Profile ID</label>
              <input
                id="profile-id"
                value={editor.id}
                onChange={(event) => setEditor({ ...editor, id: event.target.value })}
                pattern="[a-z0-9][a-z0-9_-]{0,63}"
                placeholder={t("例如 team-ppio")}
                disabled={Boolean(editor.originalId)}
                required
              />
            </div>
            <div className="field-stack">
              <label htmlFor="profile-label">{t("名称")}</label>
              {/* Optional, matching the backend: an empty label falls back to the
                  existing one, then to the ID (internal/profile/write.go:71-74).
                  The hint says so, or a Profile saved without a name looks like it
                  lost it. */}
              <input
                id="profile-label"
                value={editor.label}
                onChange={(event) => setEditor({ ...editor, label: event.target.value })}
                placeholder={t("例如 团队 PPIO")}
                aria-describedby="profile-label-hint"
              />
              <small id="profile-label-hint">{t("留空则使用 Profile ID")}</small>
            </div>
            <div className="profile-editor-wide">
              <ProviderSegment
                value={editor.provider}
                providers={status.providers}
                onAdd={() => navigate(`/providers/new?returnTo=${encodeURIComponent("/profiles")}`)}
                onChange={(provider) => setEditor({ ...editor, provider })}
              />
            </div>
            <div className="field-stack profile-editor-wide">
              <label htmlFor="profile-model">{t("模型")}</label>
              <input id="profile-model" value={editor.model} onChange={(event) => setEditor({ ...editor, model: event.target.value })} placeholder={t("例如 deepseek/deepseek-v3")} required />
            </div>
            {/* The key is the Provider's, so this only reports whether that
                Provider has one and links to where it is set. */}
            <p className="profile-key-hint profile-editor-wide">
              {providerHasKey ? t("将使用 Provider 已保存的 Key。") : t("这个 Provider 还没有 Key，先到 Provider 页面填写。")}
              {providerHasKey ? null : (
                <>
                  {" "}
                  <button className="provider-link" type="button" onClick={() => navigate("/providers")}>{t("前往 Provider")}</button>
                </>
              )}
            </p>
            <fieldset className="profile-agent-picker profile-editor-wide">
              <legend>{t("适用 Agent")}</legend>
              <div className="agent-list">
                {configurableAgents.map((agent) => (
                  <AgentRow
                    key={agent.id}
                    agent={agent}
                    status={status.agents[agent.id]}
                    selected={editor.agentIds.includes(agent.id)}
                    onToggle={() => setEditor({
                      ...editor,
                      agentIds: editor.agentIds.includes(agent.id)
                        ? editor.agentIds.filter((id) => id !== agent.id)
                        : [...editor.agentIds, agent.id],
                    })}
                  />
                ))}
              </div>
            </fieldset>
          </div>

          <footer>
            <button className="button button-secondary" type="button" onClick={() => setEditor(null)}>{t("取消")}</button>
            <button className="button button-primary" type="submit" disabled={!canSave || busy}>
              <Save size={15} />
              {busy ? t("保存中") : t("保存 Profile")}
            </button>
          </footer>
        </form>
      ) : null}

      {failure ? <p className="agent-manage-error">{failure}</p> : null}

      {!profiles.length && !editor ? (
        <div className="empty-overview">
          <Layers size={26} />
          <strong>{t("还没有 Profile")}</strong>
          <span>{t("走一遍安装引导，它会保存 Provider、模型和适用 Agent。")}</span>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((profile) => {
            const canApply = Boolean(
              profile.model && (profile.agentIds ?? []).length
                && (status.providers[profile.provider]?.has_key || profile.hasKey),
            );
            return (
              <article className="profile-card" key={profile.id} data-testid={`profile-${profile.id}`}>
                <header>
                  <span className="profile-title"><strong>{profile.label}</strong><small>{profile.id}</small></span>
                  <span className="profile-card-actions">
                    {/* The badge tracks the Provider now: a Profile without a
                        keyed Provider cannot be applied, and that is what to show. */}
                    <span className={`profile-key${status.providers[profile.provider]?.has_key ? " has-key" : ""}`}>
                      <KeyRound size={12} aria-hidden="true" />
                      {status.providers[profile.provider]?.has_key ? t("Provider 已有 Key") : t("Provider 缺少 Key")}
                    </span>
                    <button className="icon-button" type="button" onClick={() => { setEditor(editDraft(profile)); setFailure(""); }} aria-label={t("编辑 {name}", { name: profile.label })} title={t("编辑")}>
                      <Pencil size={14} />
                    </button>
                  </span>
                </header>
                <p className="profile-target">
                  {status.providers[profile.provider]?.name || profile.provider}
                  <span aria-hidden="true"> · </span>
                  {profile.model || t("未指定模型")}
                </p>
                <small className="profile-api-url" title={profile.baseUrl || status.providers[profile.provider]?.base_url || undefined}>
                  {t("API 地址：")}{profile.baseUrl || status.providers[profile.provider]?.base_url || t("未记录")}
                </small>
                <p className="profile-agents">
                  API mode: {protocolOf(profile) || "-"}
                </p>
                <footer>
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => void apply(profile)}
                    disabled={!canApply || Boolean(applying)}
                    title={canApply ? t("安装缺失的 Agent 并应用此 Profile") : t("请先补全模型和 Agent，并为 Provider 保存 Key")}
                  >
                    <Play size={14} />
                    {applying === profile.id ? t("应用中") : t("应用到 Agent")}
                  </button>
                </footer>
              </article>
            );
          })}
        </div>
      )}
    </PageScaffold>
  );
}
