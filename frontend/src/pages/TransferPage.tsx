import { Dialogs } from "@wailsio/runtime";
import { CheckCheck, Eye, EyeOff, KeyRound, Upload } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeError } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { byProviderCreatedAt } from "../state/ranking";
import { makeTransfer, parseTransfer, stringifyTransfer } from "../state/settingsTransfer";
import { useWizard } from "../state/WizardContext";

const toggle = (selected: Set<string>, id: string) => {
  const next = new Set(selected);
  if (next.has(id)) next.delete(id); else next.add(id);
  return next;
};

const isDialogCancellation = (error: unknown) => error instanceof Error && /cancelled by user/i.test(error.message);

export function TransferPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [selectedProviders, setSelectedProviders] = useState(new Set<string>());
  const [selectedProfiles, setSelectedProfiles] = useState(new Set<string>());
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [success, setSuccess] = useState("");
  const [encryptionRequest, setEncryptionRequest] = useState(false);
  const [passwordRequest, setPasswordRequest] = useState<"export" | "import" | null>(null);
  const [passwordValue, setPasswordValue] = useState("");
  const [passwordVisible, setPasswordVisible] = useState(false);
  const encryptionResolver = useRef<((value: boolean | null) => void) | null>(null);
  const passwordResolver = useRef<((value: string | null) => void) | null>(null);

  const profiles = status?.profiles ?? [];
  const providers = status ? byProviderCreatedAt(status.providers) : [];
  const requiredProviders = useMemo(() => new Set(profiles.filter((profile) => selectedProfiles.has(profile.id)).map((profile) => profile.provider)), [profiles, selectedProfiles]);
  const exportProviders = new Set([...selectedProviders, ...requiredProviders]);
  const canExport = selectedProfiles.size > 0 || exportProviders.size > 0;
  const allSelected = providers.length > 0 && profiles.length > 0
    && selectedProviders.size === providers.length && selectedProfiles.size === profiles.length;
  const toggleAll = () => {
    if (allSelected) {
      setSelectedProviders(new Set());
      setSelectedProfiles(new Set());
      return;
    }
    setSelectedProviders(new Set(providers.map(([id]) => id)));
    setSelectedProfiles(new Set(profiles.map((profile) => profile.id)));
  };

  const askPassword = (mode: "export" | "import") => new Promise<string | null>((resolve) => {
    passwordResolver.current = resolve;
    setPasswordValue("");
    setPasswordVisible(false);
    setPasswordRequest(mode);
  });
  const askEncryption = () => new Promise<boolean | null>((resolve) => {
    encryptionResolver.current = resolve;
    setEncryptionRequest(true);
  });
  const finishEncryption = (value: boolean | null) => {
    if (value) {
      setPasswordValue("");
      setPasswordRequest("export");
    }
    encryptionResolver.current?.(value);
    encryptionResolver.current = null;
    setEncryptionRequest(false);
  };
  const finishPassword = (value: string | null) => {
    passwordResolver.current?.(value);
    passwordResolver.current = null;
    setPasswordRequest(null);
    setPasswordValue("");
    setPasswordVisible(false);
  };

  const exportFile = async () => {
    if (!canExport) return;
    setBusy(true);
    setFailure("");
    setSuccess("");
    try {
      const encrypt = await askEncryption();
      if (encrypt === null) return;
      const password = encrypt ? await askPassword("export") : "";
      if (encrypt && !password) return;
      let path: string;
      try {
        path = await Dialogs.SaveFile({
          Title: t("选择导出位置"),
          Filename: "oneagent-settings.json",
          CanCreateDirectories: true,
          Filters: [{ DisplayName: "JSON", Pattern: "*.json" }],
        });
      } catch (error) {
        if (isDialogCancellation(error)) return;
        throw error;
      }
      if (!path) return;
      const entries = await Promise.all([...exportProviders].map((id) => api.getProvider(id)));
      const selected = profiles.filter((profile) => selectedProfiles.has(profile.id));
      await api.writeTransferFile(path, stringifyTransfer(await makeTransfer(selected, entries, encrypt, password || "")));
      setSuccess(t("导出完成"));
    } catch (error) {
      setFailure(describeError(error, t("导出失败")).message);
    } finally {
      setBusy(false);
    }
  };

  const importFile = async () => {
    setBusy(true);
    setFailure("");
    setSuccess("");
    try {
      let path: string | string[];
      try {
        path = await Dialogs.OpenFile({ Title: t("选择导入文件"), Filters: [{ DisplayName: "JSON", Pattern: "*.json" }] });
      } catch (error) {
        if (isDialogCancellation(error)) return;
        throw error;
      }
      if (!path || Array.isArray(path)) return;
      const raw = await api.readTransferFile(path);
      const encrypted = (JSON.parse(raw) as { encrypted?: unknown }).encrypted;
      const password = encrypted ? await askPassword("import") : "";
      if (encrypted && !password) return;
      const data = await parseTransfer(raw, password || "");
      // create: false — an import restores Providers, so an ID that already
      // exists is the expected case and overwriting it is the point. Refusing
      // duplicates here would make re-importing a backup fail.
      for (const provider of data.providers ?? []) await api.saveProvider({ ...provider, create: false });
      for (const profile of data.profiles ?? []) await api.saveProfile({ id: profile.id, label: profile.label, provider: profile.provider, apiBaseUrl: "", apiKey: "", model: profile.model || "", configMode: "provider", protocol: profile.protocol || "" });
      await refreshStatus();
      setSuccess(t("导入完成"));
    } catch (error) {
      setFailure(describeError(error, t("导入失败")).message);
    } finally {
      setBusy(false);
    }
  };

  if (!status) return <PageScaffold title={t("导入导出")}><div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div></PageScaffold>;

  return (
    <PageScaffold
      title={t("导入导出")}
      description={t("选择要迁移的模型服务和配置模版")}
      bodyClassName="transfer-page"
      backLabel={t("设置")}
      onBack={() => navigate("/settings")}
      secondaryAction={<button className="button button-secondary" type="button" onClick={() => void importFile()} disabled={busy}><Upload size={15} />{t("导入")}</button>}
      primaryLabel={busy ? t("正在处理") : t("导出")}
      primaryDisabled={!canExport}
      primaryBusy={busy}
      onPrimary={() => void exportFile()}
    >
      {failure ? <div className="notice notice-error">{failure}</div> : null}
      {success ? <div className="notice notice-success">{success}</div> : null}
      {passwordRequest ? (
        <dialog className="transfer-password-dialog" open>
          <form onSubmit={(event) => { event.preventDefault(); finishPassword(passwordValue.trim() || null); }}>
            <h2>{passwordRequest === "export" ? t("请输入导出密码") : t("请输入导入密码")}</h2>
            <div className="secure-field">
              <KeyRound size={17} aria-hidden="true" />
              <input autoFocus type={passwordVisible ? "text" : "password"} value={passwordValue} onChange={(event) => setPasswordValue(event.target.value)} autoComplete="new-password" spellCheck={false} autoCorrect="off" autoCapitalize="none" required />
              <button type="button" onClick={() => setPasswordVisible((current) => !current)} aria-label={passwordVisible ? t("隐藏密钥") : t("显示密钥")}>
                {passwordVisible ? <EyeOff size={17} /> : <Eye size={17} />}
              </button>
            </div>
            <footer><button className="button button-secondary" type="button" onClick={() => finishPassword(null)}>{t("取消")}</button><button className="button button-primary" type="submit">{t("确认")}</button></footer>
          </form>
        </dialog>
      ) : null}
      {encryptionRequest ? (
        <dialog className="transfer-password-dialog" open>
          <form onSubmit={(event) => { event.preventDefault(); finishEncryption(true); }}>
            <h2>{t("导出设置")}</h2>
            <p>是否加密apikey</p>
            <footer>
              <button className="button button-secondary" type="button" onClick={() => finishEncryption(null)}>{t("取消")}</button>
              <button className="button button-secondary" type="button" onClick={() => finishEncryption(false)}>{t("不加密")}</button>
              <button className="button button-primary" type="submit">{t("加密")}</button>
            </footer>
          </form>
        </dialog>
      ) : null}
      <div className="transfer-actions">
        <button className="button button-secondary" type="button" onClick={toggleAll} disabled={busy || (!providers.length && !profiles.length)}>
          <CheckCheck size={15} />{t(allSelected ? "取消全选" : "全选")}
        </button>
      </div>
      <div className="transfer-grid">
        <section className="transfer-section">
          <header><div><h2>{t("模型服务")}</h2><p>{t("已选择 {count} 项", { count: exportProviders.size })}</p></div></header>
          <div className="transfer-list">
            {providers.map(([id, provider]) => {
              const required = requiredProviders.has(id);
              return <label className="transfer-row" key={id}><input type="checkbox" checked={selectedProviders.has(id) || required} disabled={required} onChange={() => setSelectedProviders(toggle(selectedProviders, id))} /><span><strong>{provider.name}</strong><small>{id}</small></span>{required ? <em>{t("配置模版依赖")}</em> : null}</label>;
            })}
          </div>
        </section>
        <section className="transfer-section">
          <header><div><h2>{t("配置模版")}</h2><p>{t("已选择 {count} 项", { count: selectedProfiles.size })}</p></div></header>
          <div className="transfer-list">
            {profiles.map((profile) => <label className="transfer-row" key={profile.id}><input type="checkbox" checked={selectedProfiles.has(profile.id)} onChange={() => setSelectedProfiles(toggle(selectedProfiles, profile.id))} /><span><strong>{profile.label || profile.id}</strong><small>{profile.id} · {status.providers[profile.provider]?.name || profile.provider}</small></span></label>)}
          </div>
        </section>
      </div>
    </PageScaffold>
  );
}
