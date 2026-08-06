import { KeyRound, Layers, Pencil, Play, Plus, Save, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError, isCancellationError } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderSegment } from "../components/ProviderSegment";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, useTaskCenter, useTaskRoute } from "../state/TaskCenterContext";
import { byProviderCreatedAt } from "../state/ranking";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS, type ProfileSummary, type ProtocolId, type ProviderId } from "../types/api";
import { SelectField } from "../components/SelectField";

// A Profile no longer carries its own key: the Provider it points at owns one,
// and asking twice for the same secret was the bug worth deleting.
interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  protocol: string;
  originalId: string;
}

function editDraft(profile: ProfileSummary, protocol: string): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    protocol,
    originalId: profile.id,
  };
}

export function ProfilesPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const { startTask, finishTask, setTaskCanceller } = useTaskCenter();
  const route = useTaskRoute();
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
  const protocolOf = (profile: ProfileSummary) =>
    profile.protocol || status.catalog.find((agent) => status.agents[agent.id]?.profileId === profile.id)?.protocol || "";
  const providerHasKey = Boolean(editor && status.providers[editor.provider]?.has_key);
  // label is absent on purpose: the backend fills it in from the existing value
  // or the ID, so demanding it here was stricter than the write path.
  const canSave = Boolean(editor?.id.trim() && editor.model.trim() && editor.protocol);

  const openCreate = () => {
    const [provider = "ppio", providerMeta] = byProviderCreatedAt(status.providers)[0] || [];
    const baseID = `profile-${provider}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
    const ids = new Set(profiles.map((profile) => profile.id.toLowerCase()));
    let id = baseID;
    let suffix = 2;
    while (ids.has(id)) id = `${baseID}-${suffix++}`;
    setFailure("");
    setEditor({
      id,
      label: `${providerMeta?.name || provider} Profile`,
      provider,
      model: "",
      protocol: "",
      originalId: "",
    });
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor || !canSave) return;
    setBusy(true);
    setFailure("");
    try {
      await api.saveProfile({
        id: editor.id.trim().toLowerCase(),
        label: editor.label.trim(),
        provider: editor.provider,
        apiBaseUrl: "",
        apiKey: "",
        model: editor.model.trim(),
        configMode: "provider",
        protocol: editor.protocol,
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
    const candidates = configurableAgents.filter((agent) => agent.protocol === protocolOf(profile)).map((agent) => agent.id);
    if (!profile.model || !candidates.length) return;
    const group = `profile:${profile.id}:${Date.now()}`;
    const agents = candidates.filter((agentId) => startTask({
      id: taskKey("install", agentId),
      kind: "install",
      target: agentId,
      title: t("安装 {name}", { name: status.catalog.find((agent) => agent.id === agentId)?.name || agentId }),
      route,
      progressTarget: status.capabilities.missingRuntime[agentId],
      group,
    }));
    if (!agents.length) return;
    setApplying(profile.id);
    setFailure("");
    try {
      const request = api.install({
        agents,
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
      const cancel = taskCanceller(request);
      for (const agentId of agents) setTaskCanceller(taskKey("install", agentId), cancel);
      const result = await request;
      for (const agentId of agents) {
        const item = result.results.find((candidate) => candidate.agent === agentId);
        finishTask(taskKey("install", agentId), !item || item.status === "failed"
          ? { kind: "failure", message: item?.message || t("应用 Profile 失败") }
          : { kind: "success", message: t("{name} 已应用", { name: profile.label || profile.id }) });
      }
      if (!result.ok) {
        throw new Error(result.results.find((item) => item.status === "failed")?.message || t("应用 Profile 失败"));
      }
      await refreshStatus();
      navigate("/overview", { replace: true });
    } catch (error) {
      const cancelled = isCancellationError(error);
      const message = cancelled ? "" : describeError(error, t("无法应用 Profile")).message;
      for (const agentId of agents) finishTask(taskKey("install", agentId), { kind: cancelled ? "cancelled" : "failure", message });
      if (!cancelled) setFailure(message);
    } finally {
      setApplying("");
    }
  };

  return (
    <PageScaffold title={t("配置模板")} description={t("在这里创建 Profile，再将它应用到所选 Agent。")}>
      <div className="profile-toolbar">
        <button className="button button-secondary" type="button" onClick={openCreate} disabled={Boolean(editor)}>
          <Plus size={15} />
          {t("新增 Profile")}
        </button>
      </div>

      {editor ? (
        <form className="profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{editor.originalId ? t("编辑 {name}", { name: editor.label || editor.id }) : t("创建 Profile")}</strong>
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
              <div className="field-stack">
                <label htmlFor="profile-protocol">{t("API 类型")}</label>
                <SelectField
                  id="profile-protocol"
                  label={t("API 类型")}
                  value={editor.protocol}
                  onChange={(protocol) => setEditor({ ...editor, protocol })}
                  options={[
                    { value: "", label: t("请选择 API 类型") },
                    ...(Object.keys(PROTOCOL_LABELS) as ProtocolId[]).map((protocol) => ({ value: protocol, label: PROTOCOL_LABELS[protocol] })),
                  ]}
                />
              </div>
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
          <span>{t("在这里创建 Profile，再将它应用到所选 Agent。")}</span>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((profile) => {
            const agents = configurableAgents.filter((agent) => agent.protocol === protocolOf(profile));
            const users = configurableAgents.filter((agent) => status.agents[agent.id]?.profileId === profile.id);
            const canApply = Boolean(
              profile.model && agents.length
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
                    <button className="icon-button" type="button" onClick={() => { setEditor(editDraft(profile, protocolOf(profile))); setFailure(""); }} aria-label={t("编辑 {name}", { name: profile.label })} title={t("编辑")}>
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
                  {users.length ? (
                    <span className="provider-users">
                      {users.map((agent) => <span className="provider-user-chip" key={agent.id}>{agent.name}</span>)}
                    </span>
                  ) : <span className="provider-users is-empty">{t("暂无 Agent 使用")}</span>}
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => void apply(profile)}
                    disabled={!canApply || Boolean(applying)}
                    title={canApply ? t("安装缺失的 Agent 并应用此 Profile") : t("请先补全模型和 API mode，并为 Provider 保存 Key")}
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
