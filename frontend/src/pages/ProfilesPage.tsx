import { KeyRound, Layers, Pencil, Play, Plus, Save, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure, isCancellationError } from "../backend/api";
import { CardUsers } from "../components/CardUsers";
import { PageScaffold } from "../components/PageScaffold";
import { ProviderModelPicker } from "../components/ProviderModelPicker";
import { ProviderSegment } from "../components/ProviderSegment";
import { SelectField } from "../components/SelectField";
import { useI18n } from "../i18n";
import { confirmDelete } from "../state/confirmDelete";
import { byProviderCreatedAt, preferProviderWithKey } from "../state/ranking";
import { isConverterID } from "../state/conversion";
import { installTaskRoute, taskKey, useTaskCenter } from "../state/TaskCenterContext";
import { useWizard } from "../state/WizardContext";
import { PROTOCOL_LABELS, type ProfileSummary, type ProtocolId, type ProviderId } from "../types/api";

// A Profile no longer carries its own key: the Provider it points at owns one,
// and asking twice for the same secret was the bug worth deleting.
interface ProfileDraft {
  id: string;
  label: string;
  provider: ProviderId;
  model: string;
  reasoningEffort: string;
  context1M: boolean;
  protocol: string;
  originalId: string;
}

/**
 * What an apply is about to rewrite, for the confirmation dialog.
 *
 * Applying used to write every configurable Agent whose protocol matched the
 * Profile, with the count shown nowhere and no way to stop it. Four Agents map to
 * `openai` and a fifth falls through to it, so one press could rewrite five
 * config files -- including Agents the user had deliberately pointed somewhere
 * else. Deleting a Profile in this same file has always asked first.
 *
 * `selected` starts as the Agents already following this Profile rather than as
 * every protocol match, because those are different situations: re-applying to a
 * follower is what the button is for, while pulling in an Agent that never
 * followed it is a new decision the user should make deliberately.
 */
interface ApplyPlan {
  profile: ProfileSummary;
  // Every protocol match, so the dialog can offer the ones not yet bound.
  candidates: { id: string; name: string; bound: boolean }[];
  selected: Set<string>;
}

function editDraft(profile: ProfileSummary, protocol: string): ProfileDraft {
  return {
    id: profile.id,
    label: profile.label,
    provider: profile.provider,
    model: profile.model || "",
    reasoningEffort: profile.reasoningEffort || "",
    context1M: Boolean(profile.context1M),
    protocol,
    originalId: profile.id,
  };
}

export function ProfilesPage() {
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const { startTask, finishTask, setTaskCanceller } = useTaskCenter();
  const status = state.status;
  const [editor, setEditor] = useState<ProfileDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [applying, setApplying] = useState("");
  const [failure, setFailure] = useState("");
  const [applied, setApplied] = useState("");
  const [applyPlan, setApplyPlan] = useState<ApplyPlan | null>(null);

  if (!status) {
    return (
      <PageScaffold title={t("配置模版")}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  const profiles = status.profiles.filter((profile) => !isConverterID(profile.id));
  const configurableAgents = status.catalog.filter((agent) => agent.configMode === "auto");
  const nameOf = (agentId: string) =>
    status.catalog.find((item) => item.id === agentId)?.name || agentId;
  const protocolOf = (profile: ProfileSummary) =>
    profile.protocol || status.catalog.find((agent) => status.agents[agent.id]?.profileId === profile.id)?.protocol || "";
  const providerHasKey = Boolean(editor && status.providers[editor.provider]?.has_key);
  // label is absent on purpose: the backend fills it in from the existing value
  // or the ID, so demanding it here was stricter than the write path.
  const canSave = Boolean(editor?.id.trim() && editor.model.trim() && editor.protocol);

  // Both derived from the Provider, and both recomputed on a Provider switch, so
  // they have to be one function each rather than inline in openCreate. The
  // numeric suffix depends only on the saved Profiles, which cannot change while
  // the editor is open, so calling these again for the previous Provider
  // reproduces exactly what was seeded -- that is how a switch tells an untouched
  // field from one the user typed.
  const suggestProfileID = (provider: ProviderId) => {
    const baseID = `profile-${provider}`.toLowerCase().replace(/[^a-z0-9_-]/g, "-");
    const ids = new Set(profiles.map((profile) => profile.id.toLowerCase()));
    let id = baseID;
    let suffix = 2;
    while (ids.has(id)) id = `${baseID}-${suffix++}`;
    return id;
  };

  const suggestProfileLabel = (provider: ProviderId) =>
    t("{name} 配置模版", { name: status.providers[provider]?.name || provider });

  const openCreate = () => {
    const [provider = "ppio", providerMeta] = preferProviderWithKey(byProviderCreatedAt(Object.fromEntries(Object.entries(status.providers).filter(([providerId]) => !isConverterID(providerId))))) || [];
    setFailure("");
    setEditor({
      id: suggestProfileID(provider),
      label: suggestProfileLabel(provider),
      provider,
      // Pre-filled from the Provider so a first-time user is not asked to invent
      // a model ID. Empty for a custom Provider, whose endpoint we know nothing
      // about, which leaves the field required exactly as before.
      model: providerMeta?.default_model || "",
      reasoningEffort: "",
      context1M: false,
      protocol: "",
      originalId: "",
    });
  };

  const providerForProtocol = (protocol: string, current: ProviderId) => {
    if (!protocol || (status.providers[current] && (protocol === "anthropic" ? status.providers[current].anthropic_base_url : status.providers[current].base_url))) return current;
    const usable = byProviderCreatedAt(Object.fromEntries(Object.entries(status.providers).filter(([providerId]) => !isConverterID(providerId)))).filter(([, provider]) =>
      protocol === "anthropic" ? provider.anthropic_base_url : provider.base_url,
    );
    return preferProviderWithKey(usable)?.[0] || current;
  };

  // Switching Provider re-seeds the model, ID and name, but only where the field
  // still holds what the previous Provider seeded, or nothing at all. Anything
  // the user typed is theirs to keep -- silently replacing it would be the more
  // annoying bug. Leaving a stale value behind is equally wrong in each case: a
  // model ID is not portable between Providers, and an ID and name naming the
  // Provider you just switched away from are simply false.
  //
  // Only while creating: an existing Profile's ID is its storage key and the
  // field is disabled, and its name is one the user has already lived with, so
  // neither is ours to rewrite.
  const changeProvider = (draft: ProfileDraft, provider: ProviderId): ProfileDraft => {
    const previous = status.providers[draft.provider]?.default_model || "";
    const model = draft.model.trim() && draft.model !== previous
      ? draft.model
      : status.providers[provider]?.default_model || "";
    if (draft.originalId) return { ...draft, provider, model };
    const id = draft.id.trim() && draft.id !== suggestProfileID(draft.provider)
      ? draft.id
      : suggestProfileID(provider);
    const label = draft.label.trim() && draft.label !== suggestProfileLabel(draft.provider)
      ? draft.label
      : suggestProfileLabel(provider);
    return { ...draft, provider, model, id, label };
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor || !canSave) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      // Switching the Provider or model rewrites every Agent already following
      // this Profile, so the outcome has to be reported rather than applied
      // silently -- the Agent's own config file moves with it.
      const result = await api.saveProfile({
        id: editor.id.trim().toLowerCase(),
        label: editor.label.trim(),
        provider: editor.provider,
        apiBaseUrl: "",
        apiKey: "",
        model: editor.model.trim(),
        reasoningEffort: editor.reasoningEffort,
        context1M: editor.context1M,
        configMode: "provider",
        protocol: editor.protocol,
      });
      const reapplied = result.reapplied ?? [];
      const failures = Object.entries(result.failures ?? {});
      if (failures.length) {
        setFailure(t("{agents} 重新应用失败：{message}", {
          agents: failures.map(([agentId]) => nameOf(agentId)).join(locale === "en" ? ", " : "、"),
          message: failures[0][1] ?? "",
        }));
      } else if (reapplied.length) {
        setApplied(t("已重新应用到 {agents}", {
          agents: reapplied.map(nameOf).join(locale === "en" ? ", " : "、"),
        }));
      }
      await refreshStatus();
      setEditor(null);
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存配置模版"), t).message);
    } finally {
      setBusy(false);
    }
  };

  // Opens the confirmation rather than writing anything. The dialog is where the
  // scope becomes visible and editable; runApply below does the writing.
  const askApply = (profile: ProfileSummary) => {
    const matches = configurableAgents.filter((agent) => agent.protocol === protocolOf(profile));
    if (!profile.model || !matches.length) return;
    const candidates = matches.map((agent) => ({
      id: agent.id,
      name: agent.name,
      bound: status.agents[agent.id]?.profileId === profile.id,
    }));
    // Pre-selecting the followers only. When none follow it yet -- a Profile being
    // applied for the first time -- an empty selection would make the primary
    // button do nothing, so the single match is offered instead. With several
    // matches and no followers the user picks, because there is no basis to guess.
    const bound = candidates.filter((candidate) => candidate.bound).map((candidate) => candidate.id);
    const initial = bound.length ? bound : candidates.length === 1 ? [candidates[0].id] : [];
    setFailure("");
    setApplyPlan({ profile, candidates, selected: new Set(initial) });
  };

  const toggleApplyTarget = (agentId: string) => {
    setApplyPlan((plan) => {
      if (!plan) return plan;
      const selected = new Set(plan.selected);
      if (selected.has(agentId)) selected.delete(agentId);
      else selected.add(agentId);
      return { ...plan, selected };
    });
  };

  const runApply = async (profile: ProfileSummary, targets: string[]) => {
    const model = profile.model;
    if (!model || !targets.length) return;
    const group = `profile:${profile.id}:${Date.now()}`;
    const agents = targets.filter((agentId) => startTask({
      id: taskKey("install", agentId),
      kind: "install",
      target: agentId,
      title: t("应用 {profile} 到 {agent}", {
        profile: profile.label || profile.id,
        agent: status.catalog.find((agent) => agent.id === agentId)?.name || agentId,
      }),
      route: installTaskRoute(agentId),
      progressTarget: status.capabilities.missingRuntime[agentId],
      group,
    }));
    if (!agents.length) return;
    setApplying(profile.id);
    setFailure("");
    try {
      const outcomes = await Promise.allSettled(agents.map(async (agentId) => {
        try {
          await api.activateAgent(agentId, {
            provider: profile.provider,
            apiBaseUrl: "",
            apiKey: "",
            model,
            profileId: profile.id,
          });
          finishTask(taskKey("install", agentId), { kind: "success", message: t("{name} 已应用", { name: profile.label || profile.id }) });
        } catch (error) {
          finishTask(taskKey("install", agentId), { kind: "failure", message: describeFailure(error, t("应用配置模版失败"), t).message });
          throw error;
        }
      }));
      const failure = outcomes.find((outcome): outcome is PromiseRejectedResult => outcome.status === "rejected");
      if (failure) {
        throw failure.reason;
      }
      await refreshStatus();
      navigate("/overview", { replace: true });
    } catch (error) {
      const cancelled = isCancellationError(error);
      const message = cancelled ? "" : describeFailure(error, t("无法应用配置模版"), t).message;
      if (!cancelled) setFailure(message);
    } finally {
      setApplying("");
    }
  };

  const remove = async (profile: ProfileSummary, users: typeof configurableAgents) => {
    if (users.length) {
      setFailure(t("配置模版正在被 {agents} 使用，无法删除", {
        agents: users.map((agent) => agent.name).join(locale === "en" ? ", " : "、"),
      }));
      return;
    }
    // Asked after the in-use check, so a Profile that cannot be deleted anyway
    // explains itself rather than prompting first and refusing afterwards.
    if (!await confirmDelete({
      title: t("删除配置模版"),
      message: t("确定删除配置模版「{name}」吗？该操作无法撤销。", { name: profile.label }),
      confirmLabel: t("删除"),
      cancelLabel: t("取消"),
    })) return;
    setBusy(true);
    setFailure("");
    try {
      await api.deleteProfile(profile.id);
      if (editor?.originalId === profile.id) setEditor(null);
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("无法删除配置模版"), t).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <PageScaffold
      title={t("配置模版")}
      description={t("在这里创建配置模版，再将它应用到所选 Agent")}
      bodyClassName="management-page"
      secondaryAction={(
        <button className="button button-primary" type="button" onClick={openCreate} disabled={Boolean(editor)}>
          <Plus size={15} />
          {t("新增配置模版")}
        </button>
      )}
    >

      {editor ? (
        <form className="profile-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{editor.originalId ? t("编辑 {name}", { name: editor.label || editor.id }) : t("创建配置模版")}</strong>
            <button className="icon-button" type="button" onClick={() => setEditor(null)} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>

          <div className="profile-editor-grid">
            <div className="field-stack">
              <label htmlFor="profile-id">{t("配置模版 ID")}</label>
              <input
                id="profile-id"
                value={editor.id}
                onChange={(event) => setEditor({ ...editor, id: event.target.value })}
                /* Escaped hyphen: compiled with the `v` flag, a literal `-` in a
                   character class throws, and that error left the attribute
                   accepting every value. */
                pattern="[a-z0-9][a-z0-9_\-]{0,63}"
                placeholder={t("例如 team-ppio")}
                disabled={Boolean(editor.originalId)}
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="none"
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
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="none"
              />
              <small id="profile-label-hint">{t("留空则使用配置模版 ID")}</small>
            </div>
            <div className="profile-editor-wide">
              <div className="field-stack">
                <label htmlFor="profile-protocol">{t("API 类型")}</label>
                <SelectField
                  id="profile-protocol"
                  label={t("API 类型")}
                  value={editor.protocol}
                  onChange={(protocol) => setEditor({ ...changeProvider(editor, providerForProtocol(protocol, editor.provider)), protocol })}
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
                onChange={(provider) => setEditor(changeProvider(editor, provider))}
                protocol={editor.protocol as ProtocolId}
              />
            </div>
            <ProviderModelPicker
              key={`${editor.protocol}:${editor.provider}`}
              provider={editor.provider}
              protocol={editor.protocol}
              hasKey={providerHasKey}
              value={editor.model}
              onChange={(model) => setEditor({ ...editor, model })}
              inputId="profile-model"
              wide
            />
            {/* The full Profile vocabulary. Each Agent narrows it when the
                Profile is applied: DeepSeek Harness dispatches off/high/max,
                Codex maps every level onto its own enum, aider and
                OpenCode/Kilo take low/medium/high, and the rest ignore the
                setting. Unset keeps each model's own default. */}
            <div className="profile-editor-wide">
              <div className="field-stack">
                <label htmlFor="profile-reasoning-effort">{t("思考深度")}</label>
                <SelectField
                  id="profile-reasoning-effort"
                  label={t("思考深度")}
                  value={editor.reasoningEffort}
                  onChange={(reasoningEffort) => setEditor({ ...editor, reasoningEffort })}
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
                <small>{t("各 Agent 支持的档位不同，应用时不支持的档位会明确报错")}</small>
              </div>
            </div>
            <label className="toggle-row profile-editor-wide">
              <span><strong>{t("启用1m上下文")}</strong></span>
              <input type="checkbox" role="switch" checked={editor.context1M} onChange={(event) => setEditor({ ...editor, context1M: event.target.checked })} />
            </label>
            {/* The key is the Provider's, so this only reports whether that
                Provider has one and links to where it is set. */}
            <p className="profile-key-hint profile-editor-wide">
              {providerHasKey ? t("将使用模型服务已保存的 Key") : t("这个模型服务还没有 Key，先到模型服务页面填写")}
              {providerHasKey ? null : (
                <>
                  {" "}
                  <button className="provider-link" type="button" onClick={() => navigate("/providers")}>{t("前往模型服务")}</button>
                </>
              )}
            </p>
          </div>

          <footer>
            <button className="button button-secondary" type="button" onClick={() => setEditor(null)}>{t("取消")}</button>
            <button className="button button-primary" type="submit" disabled={!canSave || busy}>
              <Save size={15} />
              {busy ? t("保存中") : t("保存配置模版")}
            </button>
          </footer>
        </form>
      ) : null}

      {failure ? <p className="agent-manage-error">{failure}</p> : null}
      {applied ? <div className="agent-manage-applied"><strong>{t("应用完成")}</strong><span>{applied}</span></div> : null}

      {!profiles.length && !editor ? (
        <div className="empty-overview">
          <Layers size={26} />
          <strong>{t("还没有配置模版")}</strong>
          <span>{t("在这里创建配置模版，再将它应用到所选 Agent")}</span>
        </div>
      ) : (
        <div className="profile-list">
          {profiles.map((profile) => {
            const agents = configurableAgents.filter((agent) => agent.protocol === protocolOf(profile));
            const users = configurableAgents.filter((agent) => status.agents[agent.id]?.profileId === profile.id);
            const canApply = Boolean(
              profile.model && agents.length
                && status.providers[profile.provider]?.has_key,
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
                      {status.providers[profile.provider]?.has_key ? t("模型服务已有 Key") : t("模型服务缺少 Key")}
                    </span>
                    <button className="icon-button" type="button" onClick={() => { setEditor(editDraft(profile, protocolOf(profile))); setFailure(""); }} aria-label={t("编辑 {name}", { name: profile.label })} title={t("编辑")}>
                      <Pencil size={14} />
                    </button>
                    {/* disabled while busy: without it a double-click sent two
                        deletes, and the second one used to report the Profile it
                        had just removed as unknown. */}
                    <button className="icon-button is-danger" type="button" disabled={busy} onClick={() => void remove(profile, users)} aria-label={t("删除 {name}", { name: profile.label })} title={users.length ? t("配置模版正在被 {agents} 使用，无法删除", { agents: users.map((agent) => agent.name).join(locale === "en" ? ", " : "、") }) : t("删除")}>
                      <Trash2 size={14} />
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
                  <CardUsers users={users} />
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => askApply(profile)}
                    disabled={!canApply || Boolean(applying)}
                    title={canApply
                      ? t("选择要应用到的 Agent（{count} 个可选）", { count: agents.length })
                      : t("请先补全模型和 API mode，并为模型服务保存 Key")}
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
      {applyPlan ? (
        <dialog className="apply-targets-dialog" open>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const { profile, selected } = applyPlan;
              setApplyPlan(null);
              void runApply(profile, [...selected]);
            }}
          >
            <h2>{t("应用到 Agent")}</h2>
            <p>{t("「{name}」会写入下列选中 Agent 的配置文件，覆盖它们当前的模型服务与模型。", {
              name: applyPlan.profile.label || applyPlan.profile.id,
            })}</p>
            <ul className="apply-targets">
              {applyPlan.candidates.map((candidate) => (
                <li key={candidate.id}>
                  <label>
                    <input
                      type="checkbox"
                      checked={applyPlan.selected.has(candidate.id)}
                      onChange={() => toggleApplyTarget(candidate.id)}
                    />
                    <span>{candidate.name}</span>
                    {/* Which Agents already follow this Profile is the reason the
                        defaults are what they are, so it is stated rather than
                        left for the user to infer from the pre-ticked boxes. */}
                    <small>{candidate.bound ? t("正在使用此配置模版") : t("当前未使用此配置模版")}</small>
                  </label>
                </li>
              ))}
            </ul>
            <footer>
              <button className="button button-secondary" type="button" onClick={() => setApplyPlan(null)}>
                {t("取消")}
              </button>
              <button className="button button-primary" type="submit" disabled={!applyPlan.selected.size}>
                {t("应用到 {count} 个 Agent", { count: applyPlan.selected.size })}
              </button>
            </footer>
          </form>
        </dialog>
      ) : null}
    </PageScaffold>
  );
}
