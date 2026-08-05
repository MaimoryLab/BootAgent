import { ExternalLink, KeyRound, Pencil, Plus, Save, Trash2, X } from "lucide-react";
import { useState, type FormEvent } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { SecureKeyField } from "../components/SecureKeyField";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { byProviderCreatedAt } from "../state/ranking";
import type { ProviderEntry } from "../types/api";

const emptyProvider: ProviderEntry = {
  id: "",
  name: "",
  home: "",
  base_url: "",
  anthropic_base_url: "",
  api_key: "",
  built_in: false,
};

export function ProvidersPage({ create = false }: { create?: boolean }) {
  const navigate = useNavigate();
  const { locale, t } = useI18n();
  const [searchParams] = useSearchParams();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [editor, setEditor] = useState<ProviderEntry | null>(create ? { ...emptyProvider } : null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [applied, setApplied] = useState("");
  const requestedReturn = searchParams.get("returnTo");
  const returnTo = requestedReturn?.startsWith("/") && !requestedReturn.startsWith("//") ? requestedReturn : "/providers";
  const nameOf = (agentId: string) =>
    status?.catalog.find((item) => item.id === agentId)?.name || agentId;

  const closeEditor = () => {
    if (create) navigate(returnTo);
    else setEditor(null);
  };

  const edit = async (providerId: string) => {
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      setEditor(await api.getProvider(providerId));
    } catch (error) {
      setFailure(describeError(error, t("无法读取 Provider")).message);
    } finally {
      setBusy(false);
    }
  };

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!editor) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      // Changing an endpoint or key rewrites every Agent already using this
      // Provider, so the outcome has to be reported rather than silently applied.
      const result = await api.saveProvider(editor);
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
      setFailure(describeError(error, t("无法保存 Provider")).message);
    } finally {
      setBusy(false);
    }
  };

  const remove = async (providerId: string, name: string) => {
    if (!window.confirm(t("删除 Provider“{name}”？", { name }))) return;
    setBusy(true);
    setFailure("");
    setApplied("");
    try {
      await api.deleteProvider(providerId);
      if (editor?.id === providerId) setEditor(null);
      await refreshStatus();
    } catch (error) {
      setFailure(describeError(error, t("无法删除 Provider")).message);
    } finally {
      setBusy(false);
    }
  };

  if (!status) {
    return (
      <PageScaffold title={create ? t("新增 Provider") : "Provider"}>
        <div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div>
      </PageScaffold>
    );
  }

  return (
    <PageScaffold title={create ? t("新增 Provider") : "Provider"} description={t("管理模型服务、端点与本机保存的 API Key。")}>
      {!create ? (
        <div className="provider-toolbar">
          <button className="button button-secondary" type="button" onClick={() => { setEditor({ ...emptyProvider }); setFailure(""); setApplied(""); }}>
            <Plus size={15} />
            {t("新增 Provider")}
          </button>
        </div>
      ) : null}

      {editor ? (
        <form className="provider-editor" onSubmit={(event) => void save(event)}>
          <header>
            <strong>{editor.id ? t("编辑 {name}", { name: editor.name || editor.id }) : t("新增 Provider")}</strong>
            <button className="icon-button" type="button" onClick={closeEditor} aria-label={t("关闭编辑")} title={t("关闭编辑")}>
              <X size={16} />
            </button>
          </header>
          <div className="provider-editor-grid">
            <div className="field-stack">
              <label htmlFor="provider-id">Provider ID</label>
              <input
                id="provider-id"
                value={editor.id}
                onChange={(event) => setEditor({ ...editor, id: event.target.value })}
                pattern="[a-z0-9][a-z0-9-]{0,63}"
                placeholder={t("例如 siliconflow")}
                disabled={editor.built_in}
                required
              />
            </div>
            <div className="field-stack">
              <label htmlFor="provider-name">{t("名称")}</label>
              <input id="provider-name" value={editor.name} onChange={(event) => setEditor({ ...editor, name: event.target.value })} required />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-base-url">{t("OpenAI 兼容 Base URL")}</label>
              <input id="provider-base-url" type="url" value={editor.base_url} onChange={(event) => setEditor({ ...editor, base_url: event.target.value })} placeholder="https://api.example.com/openai" required />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-anthropic-url">{t("Anthropic 兼容 Base URL（可选）")}</label>
              <input id="provider-anthropic-url" type="url" value={editor.anthropic_base_url} onChange={(event) => setEditor({ ...editor, anthropic_base_url: event.target.value })} placeholder="https://api.example.com/anthropic" />
            </div>
            <div className="field-stack provider-editor-wide">
              <label htmlFor="provider-home">{t("官网（可选）")}</label>
              <input id="provider-home" type="url" value={editor.home} onChange={(event) => setEditor({ ...editor, home: event.target.value })} placeholder="https://example.com/" />
            </div>
            {/* This is the only place a key is entered: the Profile reads it
                from here instead of asking again. */}
            <div className="provider-editor-wide">
              <SecureKeyField value={editor.api_key} onChange={(value) => setEditor({ ...editor, api_key: value })} />
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
                    <button className="icon-button is-danger" type="button" onClick={() => void remove(providerId, meta.name)} aria-label={t("删除 {name}", { name: meta.name })} title={t("删除")}>
                      <Trash2 size={14} />
                    </button>
                  ) : null}
                </span>
              </header>
              <dl className="provider-endpoints">
                <div><dt>{t("OpenAI 兼容")}</dt><dd>{meta.base_url}</dd></div>
                {meta.anthropic_base_url ? <div><dt>{t("Anthropic 兼容")}</dt><dd>{meta.anthropic_base_url}</dd></div> : null}
              </dl>
              <footer>
                {users.length ? (
                  <span className="provider-users">
                    {users.map((agentId) => <span className="provider-user-chip" key={agentId}>{nameOf(agentId)}</span>)}
                  </span>
                ) : <span className="provider-users is-empty">{t("暂无 Agent 使用")}</span>}
                <span className={`provider-key-state${meta.has_key ? " has-key" : ""}`}>
                  <KeyRound size={12} />{meta.has_key ? t("已保存 Key") : t("未保存 Key")}
                </span>
              </footer>
            </article>
          );
        })}
      </div> : null}

      {!create ? <p className="provider-note">{t("用户 Provider 的协议兼容性由你自己保证，OneAgent 不会为它降级或改写请求。")}</p> : null}
    </PageScaffold>
  );
}
