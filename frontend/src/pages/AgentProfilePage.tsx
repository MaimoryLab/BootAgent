import { Check, Pencil, Plus, Save, X } from "lucide-react";
import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { ProviderSegment } from "../components/ProviderSegment";
import { useI18n } from "../i18n";
import { desktopApps, desktopProfileUsable, desktopProfiles, desktopProtocol, profileAgentIdForDesktop } from "../state/desktopSetup";
import { byProfileCreatedAt, byProviderCreatedAt, preferProviderWithKey } from "../state/ranking";
import { useWizard } from "../state/WizardContext";
import type { ProfileSummary, ProtocolId, ProviderId } from "../types/api";

interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  originalId: string;
}

function draftFrom(profile: ProfileSummary): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    originalId: profile.id,
  };
}

/** Profile selection is the only Agent edit surface. Keys and endpoints stay
 * in Provider management; this page only chooses the Profile's model. */
export function AgentProfilePage() {
  const { agentId = "" } = useParams();
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const app = status ? desktopApps(status).find((candidate) => candidate.id === agentId) || null : null;
  const owner = app ? profileAgentIdForDesktop(app) : agentId;
  const catalog = status?.catalog.find((item) => item.id === owner);
  const targetName = app?.name || catalog?.name || agentId;
  const currentAgent = status?.agents[owner];
  const protocol = (app ? desktopProtocol(app) : catalog?.protocol || "") as ProtocolId | "";
  const [selectedId, setSelectedId] = useState("");
  const [draft, setDraft] = useState<ProfileDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [applied, setApplied] = useState("");

  const profiles = useMemo(() => {
    if (!status) return [];
    const protocol = catalog?.protocol || "";
    const result = app ? desktopProfiles(status, app) : status.profiles.filter((profile) => profile.protocol === protocol);
    return byProfileCreatedAt(result);
  }, [app, catalog?.protocol, status]);

  useEffect(() => {
    if (!selectedId) {
      const current = app?.profileId || currentAgent?.profileId || "";
      if (current && profiles.some((profile) => profile.id === current)) setSelectedId(current);
      else if (profiles[0]) setSelectedId(profiles[0].id);
    }
  }, [app?.profileId, currentAgent?.profileId, profiles, selectedId]);

  if (!status) {
    return (
      <PageScaffold title={t("选择配置模版")}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  if (!agentId || (!app && (!catalog || catalog.configMode !== "auto"))) {
    return (
      <PageScaffold title={t("选择配置模版")} primaryLabel={t("返回总览")} onPrimary={() => navigate("/overview")}>
        <div className="empty-overview"><strong>{t("找不到可配置的 Agent")}</strong></div>
      </PageScaffold>
    );
  }

  const selected = profiles.find((profile) => profile.id === selectedId);
  const duplicateID = Boolean(draft && !draft.originalId && status.profiles.some((profile) => profile.id === draft.id.trim().toLowerCase()));
  const canSave = Boolean(draft?.id.trim() && draft?.provider && draft?.model.trim() && !duplicateID);
  const canApply = Boolean(selected && desktopProfileUsable(status, selected));

  const openCreate = () => {
    const usable = byProviderCreatedAt(status.providers).filter(([, meta]) =>
      protocol === "anthropic" ? meta.anthropic_base_url : meta.base_url,
    );
    const provider = preferProviderWithKey(usable)?.[0] || "jiekou";
    const current = selected || profiles[0];
    const baseID = `${owner || "agent"}-${provider}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
    const ids = new Set(status.profiles.map((profile) => profile.id));
    let id = baseID;
    let suffix = 2;
    while (ids.has(id)) id = `${baseID}-${suffix++}`;
    setFailure("");
    setApplied("");
    setDraft({
      id,
      label: t("{name} 配置模版", { name: targetName }),
      provider,
      model: current?.model || currentAgent?.model || "",
      originalId: "",
    });
  };

  const openEdit = (profile: ProfileSummary) => {
    setFailure("");
    setApplied("");
    setDraft(draftFrom(profile));
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!draft || !canSave) return;
    setBusy(true);
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
        protocol: app ? desktopProtocol(app) : catalog?.protocol || "",
      });
      setSelectedId(saved.profile.id);
      setDraft(null);
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存配置模版"), t).message);
    } finally {
      setBusy(false);
    }
  };

  const apply = async () => {
    if (!selected || !canApply) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      if (app) {
        await api.configureDesktopAgent(app.id, selected.id);
      } else {
        await api.activateAgent(owner, {
          provider: selected.provider,
          apiBaseUrl: "",
          apiKey: "",
          model: selected.model || "",
          profileId: selected.id,
          smallFastModel: "",
        });
      }
      setApplied(t("{name} 已应用", { name: selected.label || selected.id }));
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("应用配置模版失败"), t).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <PageScaffold
      title={t("选择配置模版")}
      description={t("为 {name} 选择关联的配置模版", { name: targetName })}
      backLabel={t("返回总览")}
      onBack={() => navigate("/overview")}
      primaryLabel={busy ? t("应用中") : t("应用")}
      onPrimary={() => void apply()}
      primaryDisabled={!canApply || busy}
      footerNote={selected?.label || t("选择一个配置模版")}
      // Stays secondary: this footer already has a primary ("应用"), and two
      // blue buttons side by side would compete for the same attention.
      secondaryAction={(
        <button className="button button-secondary" type="button" onClick={openCreate} disabled={Boolean(draft)}>
          <Plus size={15} />{t("创建配置模版")}
        </button>
      )}
    >
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      {applied ? <div className="notice notice-success">{applied}</div> : null}

      {draft ? (
        <form className="profile-editor desktop-profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{t("编辑配置模版")}</strong>
            <button className="icon-button" type="button" onClick={() => setDraft(null)} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>
          <div className="profile-editor-grid">
            <div className="field-stack">
              <label htmlFor="agent-profile-id">{t("配置模版 ID")}</label>
              {/* Escaped hyphen: compiled with the `v` flag, a literal `-` inside
                  a character class throws and the attribute then accepts anything. */}
              <input id="agent-profile-id" value={draft.id} pattern="[a-z0-9][a-z0-9_\-]{0,63}" onChange={(event) => setDraft({ ...draft, id: event.target.value })} disabled={Boolean(draft.originalId)} spellCheck={false} autoCorrect="off" autoCapitalize="none" required />
            </div>
            <div className="field-stack">
              <label htmlFor="agent-profile-label">{t("名称")}</label>
              <input id="agent-profile-label" value={draft.label} onChange={(event) => setDraft({ ...draft, label: event.target.value })} spellCheck={false} autoCorrect="off" autoCapitalize="none" />
            </div>
            <div className="profile-editor-wide">
              <ProviderSegment
                value={draft.provider}
                providers={status.providers}
                onAdd={() => navigate(`/providers/new?returnTo=${encodeURIComponent(`/agents/${agentId}`)}`)}
                onChange={(provider) => setDraft({ ...draft, provider })}
                protocol={protocol}
              />
            </div>
            <ProviderModelPicker
              key={`${protocol}:${draft.provider}`}
              provider={draft.provider}
              protocol={protocol}
              hasKey={Boolean(status.providers[draft.provider]?.has_key)}
              value={draft.model}
              onChange={(model) => setDraft({ ...draft, model })}
              inputId="agent-profile-model"
              wide
            />
          </div>
          <p className="profile-key-hint">
            {status.providers[draft.provider]?.has_key
              ? t("将使用模型服务已保存的 Key")
              : <>
                  {t("这个模型服务还没有 Key，请先到模型服务页面填写")} {" "}
                  <button className="provider-link" type="button" onClick={() => navigate("/providers")}>{t("前往模型服务")}</button>
                </>}
          </p>
          <footer>
            <button className="button button-secondary" type="button" onClick={() => setDraft(null)}>{t("取消")}</button>
            <button className="button button-primary" type="submit" disabled={!canSave || busy}><Save size={15} />{busy ? t("保存中") : t("保存配置模版")}</button>
          </footer>
        </form>
      ) : null}

      {profiles.length ? (
        <div className="profile-list desktop-profile-list">
          {profiles.map((profile) => {
            const usable = desktopProfileUsable(status, profile);
            const active = selectedId === profile.id;
            return (
              <article className={`profile-card profile-choice${active ? " is-selected" : ""}${!usable ? " is-disabled" : ""}`} key={profile.id} data-testid={`agent-profile-${profile.id}`}>
                <label className="profile-choice-main">
                  <input type="radio" name="agent-profile" checked={active} disabled={!usable} onChange={() => { setSelectedId(profile.id); setApplied(""); }} aria-label={t("选择 {name}", { name: profile.label })} />
                  <span className="profile-title"><strong>{profile.label}</strong><small>{profile.id}</small></span>
                  {active ? <Check size={16} aria-hidden="true" /> : null}
                </label>
                <p>{status.providers[profile.provider]?.name || profile.provider} · {profile.model || t("未指定模型")}</p>
                {!usable ? <small className="profile-key-hint">{t("这个配置模版还缺少模型服务 Key 或模型")}</small> : null}
                <button className="icon-button" type="button" onClick={() => openEdit(profile)} aria-label={t("编辑 {name}", { name: profile.label })} title={t("编辑")}><Pencil size={14} /></button>
              </article>
            );
          })}
        </div>
      ) : (
        <div className="empty-overview">
          <strong>{t("还没有可用的配置模版")}</strong>
          <span>{t("创建一个配置模版后即可应用到这个 Agent")}</span>
        </div>
      )}
    </PageScaffold>
  );
}
