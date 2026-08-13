import { ExternalLink, FlaskConical, KeyRound, Pencil, Plus, Save, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { CardUsers } from "../components/CardUsers";
import { ConnectionStatus } from "../components/ConnectionStatus";
import { PageScaffold } from "../components/PageScaffold";
import { SecureKeyField } from "../components/SecureKeyField";
import { useI18n } from "../i18n";
import { confirmDelete } from "../state/confirmDelete";
import { useWizard } from "../state/WizardContext";
import { byProviderCreatedAt } from "../state/ranking";
import type { AsyncState } from "../state/wizardReducer";
import type { ProbeResponse, ProviderEntry } from "../types/api";

const emptyProvider: ProviderEntry = {
  id: "",
  name: "",
  home: "",
  base_url: "",
  anthropic_base_url: "",
  api_key: "",
  built_in: false,
};

/**
 * A free ID for a new Provider.
 *
 * The ID is a storage key, not something a user should have to invent, but it
 * still has to be unique and match the backend's pattern — and a collision is now
 * refused rather than silently overwriting the existing Provider. Suggesting a
 * valid unused value means the common path never has to think about it. The
 * numeric suffix loop mirrors the one in ProfilesPage.openCreate.
 */
function suggestProviderID(taken: Iterable<string>, base = "provider"): string {
  const slug = base.toLowerCase().replace(/[^a-z0-9-]/g, "-").replace(/^-+/, "") || "provider";
  const used = new Set([...taken].map((id) => id.toLowerCase()));
  let id = slug;
  let suffix = 2;
  while (used.has(id)) id = `${slug}-${suffix++}`;
  return id;
}

export function ProvidersPage({ create = false }: { create?: boolean }) {
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const [searchParams] = useSearchParams();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [editor, setEditor] = useState<ProviderEntry | null>(create ? { ...emptyProvider } : null);
  // Tracks whether the open editor is creating rather than editing. Derived from
  // the route on a /providers/new load, but set explicitly by the inline "add"
  // button, which opens the same editor on the list route.
  const [creating, setCreating] = useState(create);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [applied, setApplied] = useState("");
  // Local rather than the wizard reducer's connection state: that one belongs to
  // the setup flow, and sharing it would let a test here overwrite what the
  // wizard is showing, and the reverse.
  const [probeState, setProbeState] = useState<AsyncState>("idle");
  const [probeResult, setProbeResult] = useState<ProbeResponse | null>(null);
  // Optional. Empty means the backend picks one, and ConnectionStatus already
  // explains a failure caused by an auto-selected model -- which is why this is
  // not required to press the button.
  const [probeModel, setProbeModel] = useState("");
  const requestedReturn = searchParams.get("returnTo");
  const requestedProvider = searchParams.get("provider");
  const returnTo = requestedReturn?.startsWith("/") && !requestedReturn.startsWith("//") ? requestedReturn : "/providers";
  const openedProvider = useRef("");
  const prefilled = useRef(false);
  const nameOf = (agentId: string) =>
    status?.catalog.find((item) => item.id === agentId)?.name || agentId;

  /**
   * Which protocols the test covers, expressed as the Agent IDs the probe API
   * takes. Derived from the endpoints the user filled in: an Anthropic base URL is
   * only meaningful if something speaks Anthropic Messages to it.
   *
   * The Agent IDs are a means of naming protocols, not a claim about those Agents
   * -- `protocolsForAgents` is the backend's only entry point for choosing them.
   * An empty list would default to OpenAI alone, which would silently skip the
   * Anthropic field.
   */
  const probeAgentIds = (() => {
    if (!editor) return [];
    const agents: string[] = [];
    if (editor.base_url.trim()) agents.push("opencode");
    if (editor.anthropic_base_url?.trim()) agents.push("claude-code");
    return agents;
  })();
  // Nothing to test against: both endpoint fields are empty and this Provider has
  // no stored endpoints to fall back on.
  const canProbe = Boolean(editor && (probeAgentIds.length || (!creating && status?.providers[editor.id])));

  // A result describes the values that produced it, so opening or closing the
  // editor has to drop it rather than leave a verdict about another Provider on
  // screen.
  const clearProbe = () => {
    setProbeState("idle");
    setProbeResult(null);
    setProbeModel("");
  };

  const closeEditor = () => {
    clearProbe();
    if (create) navigate(returnTo);
    else {
      setEditor(null);
      setCreating(false);
    }
  };

  const edit = async (providerId: string) => {
    setBusy(true);
    setFailure("");
    setApplied("");
    clearProbe();
    try {
      setEditor(await api.getProvider(providerId));
      setCreating(false);
    } catch (error) {
      setFailure(describeFailure(error, t("无法读取模型服务"), t).message);
    } finally {
      setBusy(false);
    }
  };

  /**
   * Tests the endpoints and key currently in the editor, without saving them.
   *
   * `draft: true` is what makes that possible: the backend otherwise resolves the
   * Provider from disk, so a new Provider could not be tested at all and an edited
   * endpoint would be tested in its old form.
   *
   * Nothing is written here. The key travels to the local backend for the request
   * and is persisted only when the user presses Save -- testing a key that turns
   * out to be wrong must not leave it on disk.
   */
  const testConnection = async () => {
    if (!editor) return;
    setProbeState("loading");
    setProbeResult(null);
    setFailure("");
    try {
      const result = await api.probe({
        provider: editor.id.trim(),
        apiBaseUrl: editor.base_url.trim(),
        anthropicBaseUrl: editor.anthropic_base_url?.trim() || "",
        apiKey: editor.api_key,
        model: probeModel.trim(),
        // Which protocols to probe follows from which endpoints the user filled
        // in, not from a selected Agent: this page has no Agent context, and the
        // question here is whether the endpoints work.
        agents: probeAgentIds,
        draft: true,
      });
      setProbeResult(result);
      setProbeState(result.ok ? "success" : "error");
    } catch (error) {
      setProbeResult(null);
      setProbeState("error");
      setFailure(describeFailure(error, t("连接测试失败"), t).message);
    }
  };

  // Suggests an ID once status has loaded. The /providers/new route mounts the
  // editor before status arrives, so this cannot be done where the state is
  // initialised. The ref keeps it a suggestion: it fills the field once and never
  // overwrites what the user typed, even though status refreshes on every save.
  useEffect(() => {
    if (!create || prefilled.current || !status) return;
    prefilled.current = true;
    setEditor((current) => (current && !current.id ? { ...current, id: suggestProviderID(Object.keys(status.providers)) } : current));
  }, [create, status]);

  useEffect(() => {
    if (!create && !editor && requestedProvider && requestedProvider !== openedProvider.current && status?.providers[requestedProvider]) {
      openedProvider.current = requestedProvider;
      void edit(requestedProvider);
    }
  }, [create, editor, requestedProvider, status]);

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      // Changing an endpoint or key rewrites every Agent already using this
      // Provider, so the outcome has to be reported rather than silently applied.
      // create tells Go to refuse an ID that is taken instead of overwriting that
      // Provider. Only the caller knows which of the two this is: a complete
      // entry whose ID is not on disk looks the same either way.
      const result = await api.saveProvider({ ...editor, create: creating });
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
      if (create) navigate(returnTo);
      else setEditor(null);
    } catch (error) {
      setFailure(describeFailure(error, t("无法保存模型服务"), t).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (providerId: string, name: string, users: string[]) => {
    if (users.length) {
      setFailure(t("模型服务正在被 {agents} 使用，无法删除", {
        agents: users.map(nameOf).join(locale === "en" ? ", " : "、"),
      }));
      return;
    }
    // The saved API key goes with it, which is the part a user does not get back.
    if (!await confirmDelete({
      title: t("删除模型服务"),
      message: t("确定删除模型服务「{name}」吗？已保存的 API Key 会一并删除，该操作无法撤销。", { name }),
      confirmLabel: t("删除"),
      cancelLabel: t("取消"),
    })) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      await api.deleteProvider(providerId);
      if (editor?.id === providerId) setEditor(null);
      await refreshStatus();
    } catch (error) {
      setFailure(describeFailure(error, t("无法删除模型服务"), t).message);
    } finally {
      setBusy(false);
    }
  };

  if (!status) {
    return (
      <PageScaffold title={create ? t("新增模型服务") : t("模型服务")}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold
      title={create ? t("新增模型服务") : t("模型服务")}
      description={t("管理模型服务、端点与本机保存的 API Key")}
      bodyClassName="management-page"
      secondaryAction={!create ? (
        <button className="button button-primary" type="button" onClick={() => { setEditor({ ...emptyProvider, id: suggestProviderID(Object.keys(status.providers)) }); setCreating(true); setFailure(""); setApplied(""); }}>
          <Plus size={15} />
          {t("新增模型服务")}
        </button>
      ) : null}
    >

      {editor ? (
        <form className="provider-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{creating ? t("新增模型服务") : t("编辑 {name}", { name: editor.name || editor.id })}</strong>
            <button className="icon-button" type="button" onClick={closeEditor} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>
          <div className="provider-editor-grid">
            <div className="field-stack">
              <label htmlFor="provider-id">{t("模型服务 ID")}</label>
              <input
                id="provider-id"
                value={editor.id}
                onChange={(event) => setEditor({ ...editor, id: event.target.value })}
                /* Escaped hyphen: the browser compiles `pattern` with the `v` flag,
                   where a literal `-` in a character class is a syntax error. The
                   unescaped form threw during validation and the error was
                   swallowed, so this attribute accepted anything — "ACME!!"
                   included — leaving Go as the only check. */
                pattern="[a-z0-9][a-z0-9\-]{0,63}"
                placeholder={t("例如 siliconflow")}
                disabled={editor.built_in}
                spellCheck={false}
                autoCorrect="off"
                autoCapitalize="none"
                required
              />
              {/* The rule was only enforced by `pattern`, so a user learned it by
                  being rejected. Stated here instead, next to the prefilled value
                  they are free to keep. */}
              <small>{t("仅供本机识别，可保留默认值。小写字母、数字或连字符")}</small>
            </div>
            <div className="field-stack">
              <label htmlFor="provider-name">{t("名称")}</label>
              <input id="provider-name" value={editor.name} onChange={(event) => setEditor({ ...editor, name: event.target.value })} spellCheck={false} autoCorrect="off" autoCapitalize="none" required />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-base-url">{t("OpenAI 兼容 Base URL")}</label>
                <input id="provider-base-url" type="url" value={editor.base_url} onChange={(event) => setEditor({ ...editor, base_url: event.target.value })} placeholder="https://api.example.com/openai/v1" spellCheck={false} autoCorrect="off" autoCapitalize="none" />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-anthropic-url">{t("Anthropic 兼容 Base URL")}</label>
              <input id="provider-anthropic-url" type="url" value={editor.anthropic_base_url} onChange={(event) => setEditor({ ...editor, anthropic_base_url: event.target.value })} placeholder="https://api.example.com/anthropic/v1" spellCheck={false} autoCorrect="off" autoCapitalize="none" />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-home">{t("官网（可选）")}</label>
              <input id="provider-home" type="url" value={editor.home} onChange={(event) => setEditor({ ...editor, home: event.target.value })} placeholder="https://example.com/" spellCheck={false} autoCorrect="off" autoCapitalize="none" />
            </div>
            {/* This is the only place a key is entered: the Profile reads it
                from here instead of asking again. */}
            <div className="provider-editor-wide">
              <SecureKeyField value={editor.api_key} onChange={(value) => setEditor({ ...editor, api_key: value })} />
            </div>
            {/* Verifying here rather than only on the Agent page, which is what
                #157 asked for: the endpoint and key are entered above, so this is
                where a wrong one should be caught. */}
            <div className="provider-editor-wide provider-verify">
              <div className="provider-verify-row">
                <div className="field-stack">
                  <label htmlFor="provider-probe-model">{t("测试模型（可选）")}</label>
                  <input
                    id="provider-probe-model"
                    value={probeModel}
                    onChange={(event) => setProbeModel(event.target.value)}
                    placeholder={t("留空则自动选择")}
                    spellCheck={false}
                    autoCorrect="off"
                    autoCapitalize="none"
                  />
                  <small>{t("仅用于本次验证，不会写入模型服务配置")}</small>
                </div>
                <button
                  className="button button-secondary"
                  type="button"
                  onClick={() => void testConnection()}
                  disabled={!canProbe || probeState === "loading" || busy}
                  title={canProbe ? t("验证可用性") : t("请先填写至少一个 Base URL")}
                >
                  <FlaskConical size={15} />
                  {probeState === "loading" ? t("验证中") : t("验证可用性")}
                </button>
              </div>
              <ConnectionStatus state={probeState} result={probeResult} />
            </div>
          </div>
          <footer>
            <button className="button button-secondary" type="button" onClick={closeEditor}>{t("取消")}</button>
            <button className="button button-primary" type="submit" disabled={busy}>
              <Save size={15} />
              {busy ? t("保存中") : t("保存")}
            </button>
          </footer>
        </form>
      ) : null}

      {failure ? <p className="agent-manage-error">{failure}</p> : null}
      {applied ? <div className="agent-manage-applied"><strong>{t("应用完成")}</strong><span>{applied}</span></div> : null}

      {!create ? <div className="provider-list">
        {byProviderCreatedAt(status.providers).map(([providerId, meta]) => {
          const users = Object.entries(status.agents)
            .filter(([, agent]) => agent.provider === providerId)
            .map(([agentId]) => agentId);
          return (
            <article className="provider-card" key={providerId} data-testid={`provider-${providerId}`}>
              <header>
                <span className="provider-title">
                  <strong>{meta.name}</strong>
                  {meta.custom ? <small>{t("用户添加")}</small> : null}
                </span>
                <span className="provider-card-actions">
                  {meta.home ? (
                    <a className="provider-link" href={meta.home} target="_blank" rel="noreferrer">
                      <ExternalLink size={13} aria-hidden="true" />{t("官网")}
                    </a>
                  ) : null}
                  <button className="icon-button" type="button" onClick={() => void edit(providerId)} aria-label={t("编辑 {name}", { name: meta.name })} title={t("编辑")}>
                    <Pencil size={14} />
                  </button>
                  {meta.custom ? (
                      <button className="icon-button is-danger" type="button" disabled={busy} onClick={() => void remove(providerId, meta.name, users)} aria-label={t("删除 {name}", { name: meta.name })} title={users.length ? t("模型服务正在被 {agents} 使用，无法删除", { agents: users.map(nameOf).join(locale === "en" ? ", " : "、") }) : t("删除")}>
                      <Trash2 size={14} />
                    </button>
                  ) : null}
                </span>
              </header>
              {/* The Anthropic row always occupies its slot. It used to render
                  only when the endpoint was set, which made an OpenAI-only
                  Provider one row shorter than its neighbours in the same grid
                  row. Stating the absence also answers the question the missing
                  row left the user to infer. */}
              <dl className="provider-endpoints">
                <div><dt>{t("OpenAI 兼容")}</dt><dd>{meta.base_url}</dd></div>
                <div>
                  <dt>{t("Anthropic 兼容")}</dt>
                  {meta.anthropic_base_url
                    ? <dd>{meta.anthropic_base_url}</dd>
                    : <dd className="is-unsupported">{t("不支持")}</dd>}
                </div>
              </dl>
              <footer>
                <CardUsers users={users.map((agentId) => ({ id: agentId, name: nameOf(agentId) }))} />
                <span className={`provider-key-state${meta.has_key ? " has-key" : ""}`}>
                  <KeyRound size={12} />{meta.has_key ? t("已保存 Key") : t("未保存 Key")}
                </span>
              </footer>
            </article>
          );
        })}
      </div> : null}

      {!create ? <p className="provider-note">{t("用户模型服务的协议兼容性由你自己保证，BootAgent 不会为它降级或改写请求")}</p> : null}
    </PageScaffold>
  );
}
