import { Check, Plus, Save, X } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { desktopProfileIsShared, desktopProfileUsable, desktopProfiles, profileAgentIdForDesktop } from "../state/desktopSetup";
import { useWizard } from "../state/WizardContext";
import type { ProviderId } from "../types/api";

interface Draft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
}

export function DesktopProfilePage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch, refreshStatus } = useWizard();
  const app = state.status?.desktopAgent;
  const [draft, setDraft] = useState<Draft | null>(null);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState("");

  const profiles = app && state.status ? desktopProfiles(state.status, app) : [];
  const owner = app ? profileAgentIdForDesktop(app) : "";
  const status = state.status;
  const selectedProfile = status && state.desktopProfileId
    ? profiles.find((profile) => profile.id === state.desktopProfileId && desktopProfileUsable(status, profile))
    : undefined;

  useEffect(() => {
    const appProfileId = app?.profileId;
    const current = app && status && appProfileId
      ? desktopProfiles(status, app).find((profile) => profile.id === appProfileId && desktopProfileUsable(status, profile))
      : undefined;
    if (!state.desktopProfileId && current && appProfileId) {
      dispatch({ type: "SET_DESKTOP_PROFILE", value: appProfileId });
    }
  }, [app, dispatch, state.desktopProfileId, status]);

  if (!app || !state.status || !state.selectedAgentIds.includes(app.id)) {
    return (
      <PageScaffold title={t("选择配置模板")} primaryLabel={t("返回")} onPrimary={() => navigate("/setup/desktop/agents")}>
        <div className="empty-overview">{t("请先选择一个桌面 Agent")}</div>
      </PageScaffold>
    );
  }

  const defaultProvider = Object.keys(state.status.providers)[0] || "ppio";
  const canCreate = Boolean(draft?.id.trim() && draft?.model.trim() && draft?.provider);

  const openCreate = () => {
    setFailure("");
    setDraft({
      id: `${owner}-profile`,
      label: `${app.name} Profile`,
      provider: defaultProvider,
      model: state.status?.agents[owner]?.model || "",
    });
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!draft || !canCreate) return;
    setSaving(true);
    setFailure("");
    try {
      const saved = await api.saveProfile({
        id: draft.id.trim().toLowerCase(),
        label: draft.label.trim(),
        provider: draft.provider,
        apiBaseUrl: "",
        apiKey: "",
        model: draft.model.trim(),
        configMode: "provider",
        agentIds: [owner],
      });
      await refreshStatus();
      dispatch({ type: "SET_DESKTOP_PROFILE", value: saved.id });
      setDraft(null);
    } catch (error) {
      setFailure(describeError(error, t("无法保存 Profile")).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <PageScaffold
      title={t("选择配置模板")}
      description={desktopProfileIsShared(app) ? t("ChatGPT Desktop 与 Codex 共用 Profile。") : t("为这个桌面 Agent 选择专属 Profile。")}
      stepper
      backLabel={t("返回")}
      onBack={() => navigate("/setup/desktop/agents")}
      primaryLabel={t("继续")}
      onPrimary={() => navigate("/setup/desktop/install")}
      primaryDisabled={!selectedProfile}
      footerNote={selectedProfile?.label || t("选择一个 Profile")}
    >
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      <div className="profile-toolbar">
        <button className="button button-secondary" type="button" onClick={openCreate} disabled={Boolean(draft)}>
          <Plus size={15} />
          {t("创建 Profile")}
        </button>
      </div>

      {draft ? (
        <form className="profile-editor desktop-profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{t("创建 Profile")}</strong>
            <button className="icon-button" type="button" onClick={() => setDraft(null)} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>
          <div className="profile-editor-grid">
            <div className="field-stack">
              <label htmlFor="desktop-profile-id">Profile ID</label>
              <input id="desktop-profile-id" value={draft.id} pattern="[a-z0-9][a-z0-9_-]{0,63}" onChange={(event) => setDraft({ ...draft, id: event.target.value })} required />
            </div>
            <div className="field-stack">
              <label htmlFor="desktop-profile-label">{t("名称")}</label>
              <input id="desktop-profile-label" value={draft.label} onChange={(event) => setDraft({ ...draft, label: event.target.value })} />
            </div>
            <div className="field-stack">
              <label htmlFor="desktop-profile-provider">Provider</label>
              <select id="desktop-profile-provider" value={draft.provider} onChange={(event) => setDraft({ ...draft, provider: event.target.value })}>
                {Object.entries(state.status.providers).map(([id, provider]) => <option key={id} value={id}>{provider.name || id}</option>)}
              </select>
            </div>
            <div className="field-stack">
              <label htmlFor="desktop-profile-model">{t("模型")}</label>
              <input id="desktop-profile-model" value={draft.model} onChange={(event) => setDraft({ ...draft, model: event.target.value })} placeholder={t("例如 deepseek/deepseek-v3")} required />
            </div>
          </div>
          <p className="profile-key-hint">{t("将使用 Provider 已保存的 Key。")}</p>
          <button className="button button-primary" type="submit" disabled={!canCreate || saving}>
            <Save size={15} />{saving ? t("保存中") : t("保存 Profile")}
          </button>
        </form>
      ) : null}

      {profiles.length ? (
        <div className="profile-list desktop-profile-list">
          {profiles.map((profile) => {
            const provider = state.status?.providers[profile.provider];
            const usable = status ? desktopProfileUsable(status, profile) : false;
            const active = state.desktopProfileId === profile.id;
            return (
              <label className={`profile-card profile-choice${active ? " is-selected" : ""}${!usable ? " is-disabled" : ""}`} key={profile.id}>
                <input
                  type="radio"
                  name="desktop-profile"
                  value={profile.id}
                  checked={active}
                  disabled={!usable}
                  onChange={() => dispatch({ type: "SET_DESKTOP_PROFILE", value: profile.id })}
                  aria-label={t("选择 {name}", { name: profile.label })}
                />
                <span className="profile-title"><strong>{profile.label}</strong><small>{profile.id}</small></span>
                {active ? <Check size={16} aria-hidden="true" /> : null}
                <p>{provider?.name || profile.provider} · {profile.model || t("未指定模型")}</p>
                {!usable ? <small className="profile-key-hint">{t("这个 Profile 还缺少 Key 或模型")}</small> : null}
              </label>
            );
          })}
        </div>
      ) : (
        <div className="empty-overview">
          <strong>{t("还没有可用的 Profile")}</strong>
          <span>{desktopProfileIsShared(app) ? t("先创建一个包含 Codex 的 Profile。") : t("先创建一个属于该桌面 Agent 的 Profile。")}</span>
        </div>
      )}
    </PageScaffold>
  );
}
