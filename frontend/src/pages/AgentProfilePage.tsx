import { Check, Pencil, Plus, Save, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { ModelCheckButton, ModelCheckNotice } from "../components/ModelCheckNotice";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { ProviderSegment } from "../components/ProviderSegment";
import { SelectField } from "../components/SelectField";
import { useI18n } from "../i18n";
import { desktopApps, desktopProfileUsable, desktopProfiles, desktopProtocol, profileAgentIdForDesktop } from "../state/desktopSetup";
import { confirmAction } from "../state/confirmDelete";
import { byProfileCreatedAt, byProviderCreatedAt, preferProviderWithKey } from "../state/ranking";
import { converterProfileName, isConverterID } from "../state/conversion";
import { useWizard } from "../state/WizardContext";
import type { DesktopAgentProfileAssessment, ProfileModelCheck, ProfileSummary, ProtocolId, ProviderId } from "../types/api";

type SaveRequest = Parameters<typeof api.saveProfile>[0];

// The last model edit saved from this page, so a check that then fails can
// offer the model the Profile held before it. Only an edit on the same
// Provider qualifies: a model ID is not portable between Providers.
interface ModelEdit {
  profileId: string;
  previousModel: string;
  request: SaveRequest;
}

// What flushing an inline model edit did, which Apply and 检测模型 act on: a
// failed save must stop them, and a saved one supplies the model to use.
type CommitOutcome =
  | { kind: "unchanged" }
  | { kind: "failed" }
  | { kind: "saved"; model: string; edit: ModelEdit | null };

interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  reasoningEffort: string;
  context1M: boolean;
  originalId: string;
}

function draftFrom(profile: ProfileSummary): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    reasoningEffort: profile.reasoningEffort || "",
    context1M: Boolean(profile.context1M),
    originalId: profile.id,
  };
}

/** Profile selection is the only Agent edit surface. Keys and endpoints stay
 * in Provider management; this page only chooses the Profile's model. */
export function AgentProfilePage() {
  const { agentId = "" } = useParams();
  const navigate = useNavigate();
  const { locale, t } = useI18n();
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
  // The model of the selected Profile, edited in place. null means "showing what
  // is stored"; a string means the field has been touched and not yet committed.
  const [modelDraft, setModelDraft] = useState<string | null>(null);
  const [savingModel, setSavingModel] = useState(false);
  const [assessment, setAssessment] = useState<DesktopAgentProfileAssessment | null>(null);
  const [assessing, setAssessing] = useState(false);
  // A check runs only when the user presses 检测模型: it is a billed request.
  const [checked, setChecked] = useState<ProfileModelCheck | null>(null);
  const [checking, setChecking] = useState(false);
  const [lastEdit, setLastEdit] = useState<ModelEdit | null>(null);
  const checkGeneration = useRef(0);
  const statusRef = useRef(status);
  statusRef.current = status;
  useEffect(() => {
    setChecked(null);
    setChecking(false);
    return () => { checkGeneration.current += 1; };
  }, [agentId]);

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

  const selected = profiles.find((profile) => profile.id === selectedId);
  const requiresConversion = Boolean(app?.id === "claude-desktop" && selected?.protocol === "openai");

  useEffect(() => {
    if (!app || !selected || !requiresConversion) {
      setAssessment(null);
      setAssessing(false);
      return;
    }
    let active = true;
    setAssessment(null);
    setAssessing(true);
    void api.assessDesktopAgentProfile(app.id, selected.id).then((result) => {
      if (active) setAssessment(result);
    }).catch((error) => {
      if (active) setFailure(describeFailure(error, t("无法检查协议兼容性"), t).message);
    }).finally(() => {
      if (active) setAssessing(false);
    });
    return () => { active = false; };
  }, [app, requiresConversion, selected, t]);

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

  const displayProfileName = (profile: ProfileSummary) => converterProfileName(profile.id, profile.label || profile.id, t);
  const duplicateID = Boolean(draft && !draft.originalId && status.profiles.some((profile) => profile.id === draft.id.trim().toLowerCase()));
  const canSave = Boolean(draft?.id.trim() && draft?.provider && draft?.model.trim() && !duplicateID);
  const canApply = Boolean(selected && desktopProfileUsable(status, selected, app) && (
    !requiresConversion || assessment?.compatibility === "convertible"
  ));

  const separator = locale === "en" ? ", " : "、";
  const nameOf = (id: string) =>
    status.catalog.find((item) => item.id === id)?.name
      || desktopApps(status).find((candidate) => candidate.id === id)?.name
      || id;

  /**
   * Saves a Profile and reports the Agents the save could not rewrite. Those
   * used to be dropped here, so an inline model change that broke an Agent on
   * the Profile showed nothing at all. Nothing is sent to the model: that is
   * what 检测模型 is for.
   */
  const persistProfile = async (request: SaveRequest, invalidateCheck = true) => {
    if (invalidateCheck) {
      checkGeneration.current += 1;
      setChecking(false);
    }
    if (checked?.profileId === request.id) setChecked(null);
    const before = status.profiles.find((profile) => profile.id === request.id);
    const result = await api.saveProfile(request);
    const failures = Object.entries(result.failures ?? {});
    if (failures.length) {
      setFailure(t("{agents} 重新应用失败：{message}", {
        agents: failures.map(([id]) => nameOf(id)).join(separator),
        message: failures[0][1] ?? "",
      }));
    }
    const edit = before && before.provider === request.provider && before.model && before.model !== request.model
      ? { profileId: request.id, previousModel: before.model, request }
      : null;
    setLastEdit(edit);
    // Returned as well as stored: 检测模型 reads it right after a flush, before
    // React has rendered the state update.
    return { result, edit };
  };

  // Saves the model back without checking it again; the user can press
  // 检测模型 once more if they want the old one confirmed.
  const revertModel = async () => {
    if (!checked?.previousModel || lastEdit?.profileId !== checked.profileId) return;
    const current = statusRef.current?.profiles.find((profile) => profile.id === checked.profileId);
    if (current?.model !== checked.model || current.provider !== lastEdit.request.provider) return;
    const request = { ...lastEdit.request, model: checked.previousModel };
    setBusy(true);
    setFailure("");
    try {
      await persistProfile(request);
      setChecked(null);
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存配置模版"), t).message);
    } finally {
      setBusy(false);
    }
  };

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
    setDraft({
      id,
      label: t("{name} 配置模版", { name: targetName }),
      provider,
      model: current?.model || currentAgent?.model || "",
      reasoningEffort: "",
      context1M: false,
      originalId: "",
    });
  };

  const openEdit = (profile: ProfileSummary) => {
    if (isConverterID(profile.id)) return;
    setFailure("");
    setDraft(draftFrom(profile));
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!draft || !canSave) return;
    setBusy(true);
    setFailure("");
    try {
      const { result: saved } = await persistProfile({
        id: draft.id.trim().toLowerCase(),
        label: draft.label.trim(),
        provider: draft.provider,
        apiBaseUrl: "",
        apiKey: "",
        model: draft.model.trim(),
        reasoningEffort: draft.reasoningEffort,
        context1M: draft.context1M,
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

  /**
   * Writes just the model back to the selected Profile.
   *
   * Committed on blur and on picking from the list rather than on every
   * keystroke: ProviderModelPicker reports each character, and saving those would
   * write a Profile per letter typed. Everything else about the Profile is passed
   * through unchanged, so this cannot quietly reset a field the editor owns.
   */
  const commitModel = async (profile: ProfileSummary, next: string, invalidateCheck = true): Promise<CommitOutcome> => {
    const model = next.trim();
    setModelDraft(null);
    if (!model || model === (profile.model || "")) return { kind: "unchanged" };
    setSavingModel(true);
    setFailure("");
    try {
      const { edit } = await persistProfile({
        id: profile.id,
        label: profile.label,
        provider: profile.provider,
        apiBaseUrl: "",
        apiKey: "",
        model,
        reasoningEffort: profile.reasoningEffort || "",
        context1M: Boolean(profile.context1M),
        configMode: "provider",
        protocol: profile.protocol || (app ? desktopProtocol(app) : catalog?.protocol || ""),
      }, invalidateCheck);
      await refreshStatus();
      return { kind: "saved", model, edit };
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存配置模版"), t).message);
      return { kind: "failed" };
    } finally {
      setSavingModel(false);
    }
  };

  /**
   * Checks the selected Profile's model for this Agent, on the user's request.
   * An uncommitted inline edit is saved first, so the check covers the model on
   * screen rather than the one stored before it.
   */
  const checkModel = async () => {
    if (!selected || checking || busy || savingModel) return;
    const generation = ++checkGeneration.current;
    setChecking(true);
    setFailure("");
    try {
      // This save belongs to the check; other saves and leaving the page
      // invalidate its generation, including while this flush is awaiting.
      const flushed: CommitOutcome = modelDraft !== null ? await commitModel(selected, modelDraft, false) : { kind: "unchanged" };
      if (flushed.kind === "failed" || checkGeneration.current !== generation) return;
      const check = await api.checkProfileModel(selected.id, [app?.id || owner]);
      if (checkGeneration.current !== generation) return;
      const edit = flushed.kind === "saved" ? flushed.edit : lastEdit;
      setChecked({ ...check, previousModel: edit?.profileId === selected.id && edit.request.model === check.model ? edit.previousModel : undefined });
    } catch (error) {
      if (checkGeneration.current === generation) setFailure(describeFailure(error, t("无法检测模型"), t).message);
    } finally {
      if (checkGeneration.current === generation) setChecking(false);
    }
  };

  const apply = async () => {
    if (!selected || !canApply || busy || savingModel || checking) return;
    // Busy before anything is awaited: setting it only after the flush below
    // left 应用 enabled while the save was in flight, so a second click started
    // a second Apply.
    setBusy(true);
    setFailure("");
    // An uncommitted inline model edit is flushed first, so clicking 应用 straight
    // after typing applies what is on screen rather than the older stored model.
    const flushed: CommitOutcome = modelDraft !== null ? await commitModel(selected, modelDraft) : { kind: "unchanged" };
    if (flushed.kind === "failed") {
      // The Profile still holds the old model; applying now would apply that
      // while the field showed the new one. The save's error is already shown.
      setBusy(false);
      return;
    }
    // The model the flush saved, not selected.model: that is the value from
    // before the click, and activating with it would write the old model back
    // over the Agents the flush had just moved to the new one.
    const model = flushed.kind === "saved" ? flushed.model : selected.model || "";
    try {
      if (app) {
        if (requiresConversion) {
          if (!await confirmAction({
            title: t("启用协议适配"),
            message: t("BootAgent 将在本机启动协议适配服务，并自动把 Claude Desktop 指向该服务。是否继续？"),
            confirmLabel: t("启用并应用"),
            cancelLabel: t("取消"),
          })) {
            setBusy(false);
            return;
          }
          await api.configureDesktopAgentWithConversion(app.id, selected.id);
        } else {
          await api.configureDesktopAgent(app.id, selected.id);
        }
      } else {
        await api.activateAgent(owner, {
          provider: selected.provider,
          apiBaseUrl: "",
          apiKey: "",
          model,
          profileId: selected.id,
        });
      }
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("应用配置模版失败"), t).message);
      setBusy(false);
      return;
    }
    // Straight back to the overview, which is where the result is visible: the
    // Agent's row now names this Profile. Staying here left the user on a
    // screen whose job was done, with a success notice as the only feedback and
    // a second click needed to see the effect. `replace` so Back does not
    // return to a selection that has already been applied.
    navigate("/overview", { replace: true });
  };

  return (
    <PageScaffold
      title={t("选择配置模版")}
      backLabel={t("返回总览")}
      onBack={() => navigate("/overview")}
      primaryLabel={busy ? t("应用中") : requiresConversion ? t("启用协议适配并应用") : t("应用")}
      onPrimary={() => void apply()}
      primaryDisabled={!canApply || busy || savingModel || checking}
      footerNote={selected ? displayProfileName(selected) : t("选择一个配置模版")}
      // Stays secondary: this footer already has a primary ("应用"), and two
      // blue buttons side by side would compete for the same attention.
      secondaryAction={(
        <button className="button button-secondary" type="button" onClick={openCreate} disabled={Boolean(draft)}>
          <Plus size={15} />{t("创建配置模版")}
        </button>
      )}
    >
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      {checked && !draft ? (
        <ModelCheckNotice
          check={checked}
          providerId={profiles.find((profile) => profile.id === checked.profileId)?.provider || ""}
          nameOf={nameOf}
          returnTo={`/agents/${encodeURIComponent(agentId)}`}
          profileName={checked.profileId === selected?.id ? undefined : profiles.find((profile) => profile.id === checked.profileId)?.label}
          busy={busy || savingModel || checking}
          onRevert={() => void revertModel()}
          onRecheck={checked.profileId === selected?.id ? () => void checkModel() : undefined}
          onDismiss={() => setChecked(null)}
        />
      ) : null}
      {requiresConversion ? (
        <div className={`notice ${assessment?.compatibility === "unusable" ? "notice-error" : "notice-info"}`} role="status">
          {assessing
            ? t("正在检查模型服务的协议兼容性")
            : assessment?.compatibility === "convertible"
              ? t("该模型服务不能直接使用 Claude Desktop 所需的 Anthropic Messages。BootAgent 可以在本机转换请求格式。")
              : assessment?.message || t("无法确认该配置模版可以通过协议适配使用")}
        </div>
      ) : null}

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
            {/* The full Profile vocabulary; the Agent this page configures
                narrows it when the config is written. Unset keeps the model's
                own default. */}
            <div className="field-stack">
              <label htmlFor="agent-profile-reasoning-effort">{t("思考深度")}</label>
              <SelectField
                id="agent-profile-reasoning-effort"
                label={t("思考深度")}
                value={draft.reasoningEffort}
                onChange={(reasoningEffort) => setDraft({ ...draft, reasoningEffort })}
                options={[
                  { value: "", label: t("未设置（模型默认）") },
                  { value: "off", label: t("off（关闭）") },
                  { value: "minimal", label: t("minimal（最低）") },
                  { value: "low", label: t("low（低）") },
                  { value: "medium", label: t("medium（中）") },
                  { value: "high", label: t("high（高）") },
                  { value: "xhigh", label: t("xhigh（极高）") },
                  { value: "max", label: t("max（最大）") },
                ]}
              />
            </div>
            <label className="toggle-row">
              <span><strong>{t("启用1m上下文")}</strong></span>
              <input type="checkbox" role="switch" checked={draft.context1M} onChange={(event) => setDraft({ ...draft, context1M: event.target.checked })} />
            </label>
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

      {/* The editor is a page of its own; the list returns when it closes.
          Rendering it below the form read as the previous page appended. */}
      {draft ? null : profiles.length ? (
        <>
        <div className="profile-list desktop-profile-list">
          {profiles.map((profile) => {
            const usable = desktopProfileUsable(status, profile, app);
            const active = selectedId === profile.id;
            const displayName = displayProfileName(profile);
            return (
            <article className={`profile-card profile-choice${active ? " is-selected" : ""}${!usable ? " is-disabled" : ""}`} key={profile.id} data-testid={`agent-profile-${profile.id}`} onClick={() => { if (usable) setSelectedId(profile.id); }}>
                <label className="profile-choice-main">
                  <input type="radio" name="agent-profile" checked={active} disabled={!usable} onChange={() => setSelectedId(profile.id)} aria-label={t("选择 {name}", { name: displayName })} />
                  <span className="profile-title"><strong>{displayName}</strong><small>{profile.id}</small></span>
                  {active ? <Check size={16} aria-hidden="true" /> : null}
                </label>
                {/* The selected Profile's model is editable right here, so the
                    common change -- same Profile, different model -- costs no
                    trip through the editor. The others stay one line of text;
                    a picker on every card would be a wall of inputs. */}
                <p>{status.providers[profile.provider]?.name || profile.provider} · {profile.model || t("未指定模型")}</p>
                {!usable ? <small className="profile-key-hint">{t("这个配置模版还缺少模型服务 Key 或模型")}</small> : null}
                {!isConverterID(profile.id) ? <button className="icon-button" type="button" onClick={(event) => { event.stopPropagation(); openEdit(profile); }} aria-label={t("编辑 {name}", { name: profile.label })} title={t("编辑")}><Pencil size={14} /></button> : null}
              </article>
            );
          })}
        </div>
        {selected && !isConverterID(selected.id) ? (
          <section className="profile-model-panel">
            <header>
              <strong>{t("编辑 {name}", { name: displayProfileName(selected) })}</strong>
              <span className="profile-model-panel-actions">
                {savingModel ? <small>{t("正在保存模型")}</small> : null}
                <ModelCheckButton
                  checking={checking}
                  disabled={busy || savingModel || !(modelDraft ?? selected.model ?? "").trim()}
                  onCheck={() => void checkModel()}
                />
              </span>
            </header>
            <ProviderModelPicker
              key={`${selected.id}:${selected.model ?? ""}`}
              provider={selected.provider}
              protocol={selected.protocol || protocol}
              hasKey={Boolean(status.providers[selected.provider]?.has_key)}
              value={modelDraft ?? selected.model ?? ""}
              onChange={setModelDraft}
              onBlur={() => { if (modelDraft !== null) void commitModel(selected, modelDraft); }}
              inputId={`agent-profile-model-${selected.id}`}
              inputLabel={t("模型")}
            />
          </section>
        ) : null}
        </>
      ) : (
        <div className="empty-overview">
          <strong>{t("还没有可用的配置模版")}</strong>
          <span>{t("创建一个配置模版后即可应用到这个 Agent")}</span>
        </div>
      )}
    </PageScaffold>
  );
}
