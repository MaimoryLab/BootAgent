import { CheckCheck, Eye, EyeOff, KeyRound, Search, Sparkles, Upload, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { api, describeFailure } from "../backend/api";
import { ModalDialog } from "../components/ModalDialog";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { byProviderCreatedAt } from "../state/ranking";
import {
  makeTransfer,
  parseTransfer,
  stringifyTransfer,
  transferNeedsPassword,
  transferSummary,
  type KeyHandling,
  TransferFormatError,
  TransferPasswordError,
  TransferProviderShapeError,
  TransferVersionError,
} from "../state/settingsTransfer";
import { useWizard } from "../state/WizardContext";

const toggle = (selected: Set<string>, id: string) => {
  const next = new Set(selected);
  if (next.has(id)) next.delete(id); else next.add(id);
  return next;
};

/** What an import is about to replace, for the confirmation dialog. */
interface ImportPlan {
  raw: string;
  overwrittenProviders: string[];
  overwrittenProfiles: string[];
  newProviders: number;
  newProfiles: number;
  /** Whether the file supplies keys, which decides what the warning may claim. */
  carriesKeys: boolean;
}

export function TransferPage() {
  const { locale, t } = useI18n();
  const navigate = useNavigate();
  const { state, refreshStatus } = useWizard();
  const status = state.status;
  const [selectedProviders, setSelectedProviders] = useState(new Set<string>());
  const [selectedProfiles, setSelectedProfiles] = useState(new Set<string>());
  const [mcpServers, setMcpServers] = useState<{ id: string; type: string }[]>([]);
  const [selectedMcp, setSelectedMcp] = useState(new Set<string>());
  const [skills, setSkills] = useState<import("../types/api").SkillSummary[]>([]);
  const [selectedSkills, setSelectedSkills] = useState(new Set<string>());
  const [query, setQuery] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [success, setSuccess] = useState("");
  const [encryptionRequest, setEncryptionRequest] = useState(false);
  const [passwordRequest, setPasswordRequest] = useState<"export" | "import" | null>(null);
  const [passwordValue, setPasswordValue] = useState("");
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [importPlan, setImportPlan] = useState<ImportPlan | null>(null);
  const encryptionResolver = useRef<((value: KeyHandling | null) => void) | null>(null);
  const passwordResolver = useRef<((value: string | null) => void) | null>(null);
  const importResolver = useRef<((value: boolean) => void) | null>(null);

  /** Maps the transfer module's typed failures onto localised copy. */
  const describeTransferError = (error: unknown, fallback: string) => {
    if (error instanceof TransferVersionError) {
      return t("这个文件的版本（{version}）不受支持，当前只支持版本 1", { version: String(error.found ?? "?") });
    }
    if (error instanceof TransferPasswordError) return t("密码错误，或文件已损坏");
    if (error instanceof TransferProviderShapeError) return t("模型服务数据格式无效，文件可能被手工修改过");
    // A truncated or non-JSON file reaches here as a SyntaxError from JSON.parse,
    // whose message ("Unexpected token } in JSON at position 412") is not
    // something to show a user.
    if (error instanceof TransferFormatError || error instanceof SyntaxError) {
      return t("文件格式无效，请确认这是 BootAgent 导出的文件");
    }
    return describeFailure(error, fallback, t).message;
  };

  const profiles = status?.profiles ?? [];
  const providers = status ? byProviderCreatedAt(status.providers) : [];
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const filteredProviders = providers.filter(([id, provider]) => !normalizedQuery || `${id} ${provider.name}`.toLocaleLowerCase().includes(normalizedQuery));
  const filteredProfiles = profiles.filter((profile) => !normalizedQuery || `${profile.id} ${profile.label} ${status?.providers[profile.provider]?.name ?? profile.provider}`.toLocaleLowerCase().includes(normalizedQuery));
  const filteredMcpServers = mcpServers.filter((server) => !normalizedQuery || `${server.id} ${server.type}`.toLocaleLowerCase().includes(normalizedQuery));
  const filteredSkills = skills.filter((skill) => !normalizedQuery || `${skill.id} ${skill.name} ${skill.description}`.toLocaleLowerCase().includes(normalizedQuery));
  const requiredProviders = useMemo(() => new Set(profiles.filter((profile) => selectedProfiles.has(profile.id)).map((profile) => profile.provider)), [profiles, selectedProfiles]);
  const exportProviders = new Set([...selectedProviders, ...requiredProviders]);
  const canExport = selectedProfiles.size > 0 || exportProviders.size > 0 || selectedMcp.size > 0 || selectedSkills.size > 0;
  const visibleProviderIDs = filteredProviders.map(([id]) => id);
  const visibleProfileIDs = filteredProfiles.map((profile) => profile.id);
  const visibleMcpIDs = filteredMcpServers.map((server) => server.id);
  const visibleSkillIDs = filteredSkills.map((skill) => skill.id);
  const allSelected = (visibleProviderIDs.length > 0 || visibleProfileIDs.length > 0 || visibleMcpIDs.length > 0 || visibleSkillIDs.length > 0)
    && visibleProviderIDs.every((id) => selectedProviders.has(id) || requiredProviders.has(id))
    && visibleProfileIDs.every((id) => selectedProfiles.has(id))
    && visibleMcpIDs.every((id) => selectedMcp.has(id))
    && visibleSkillIDs.every((id) => selectedSkills.has(id));
  const toggleAll = () => {
    if (allSelected) {
      setSelectedProviders((current) => new Set([...current].filter((id) => !visibleProviderIDs.includes(id))));
      setSelectedProfiles((current) => new Set([...current].filter((id) => !visibleProfileIDs.includes(id))));
      setSelectedMcp((current) => new Set([...current].filter((id) => !visibleMcpIDs.includes(id))));
      setSelectedSkills((current) => new Set([...current].filter((id) => !visibleSkillIDs.includes(id))));
      return;
    }
    setSelectedProviders((current) => new Set([...current, ...visibleProviderIDs]));
    setSelectedProfiles((current) => new Set([...current, ...visibleProfileIDs]));
    setSelectedMcp((current) => new Set([...current, ...visibleMcpIDs]));
    setSelectedSkills((current) => new Set([...current, ...visibleSkillIDs]));
  };

  const askPassword = (mode: "export" | "import") => new Promise<string | null>((resolve) => {
    passwordResolver.current = resolve;
    setPasswordValue("");
    setPasswordVisible(false);
    setPasswordRequest(mode);
  });
  const askEncryption = () => new Promise<KeyHandling | null>((resolve) => {
    encryptionResolver.current = resolve;
    setEncryptionRequest(true);
  });
  const finishEncryption = (value: KeyHandling | null) => {
    if (value === "encrypted") {
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
  const askImport = (plan: ImportPlan) => new Promise<boolean>((resolve) => {
    importResolver.current = resolve;
    setImportPlan(plan);
  });
  const finishImport = (approved: boolean) => {
    importResolver.current?.(approved);
    importResolver.current = null;
    setImportPlan(null);
  };

  const exportFile = async () => {
    if (!canExport) return;
    setBusy(true);
    setFailure("");
    setSuccess("");
    try {
      // Every abort below reports itself. Returning silently left the page back
      // at rest with no way to tell whether a file had been written.
      const keys = await askEncryption();
      if (keys === null) return setSuccess(t("已取消导出"));
      const password = keys === "encrypted" ? await askPassword("export") : "";
      if (keys === "encrypted" && !password) return setSuccess(t("已取消导出"));
      const entries = await Promise.all([...exportProviders].map((id) => api.getProvider(id)));
      const mcp = selectedMcp.size ? JSON.parse(await api.exportMCP(keys === "encrypted" ? "encrypted" : keys === "plain" ? "plaintext" : "omit", password || "", keys === "plain", [...selectedMcp])) : undefined;
      const selected = profiles.filter((profile) => selectedProfiles.has(profile.id));
      if (selectedSkills.size) {
        if (selectedSkills.size > 1 || selectedProfiles.size || exportProviders.size || selectedMcp.size) throw new Error("单 Skill 导出不能与其他类型同时选择");
        const skill = skills.find((item) => selectedSkills.has(item.id));
        const hash = skill?.variant_hashes?.[0];
        if (!skill || !hash) throw new Error("选中的 Skill 没有可导出的版本");
        await api.writeTransferBytes(await api.exportSkill(skill.id, hash));
      } else {
        await api.writeTransferFile(stringifyTransfer(await makeTransfer(selected, entries, keys, password || "", mcp)));
      }
      // Only the plain-text case needs a warning; the default carries no key at
      // all, and saying so is reassurance rather than a caveat.
      setSuccess(keys === "plain"
        ? t("导出完成，文件中的 API Key 为明文，请妥善保管")
        : keys === "omit"
          ? t("导出完成，文件不包含 API Key")
          : t("导出完成"));
    } catch (error) {
      setFailure(describeTransferError(error, t("导出失败")));
    } finally {
      setBusy(false);
    }
  };

  const importFile = async () => {
    setBusy(true);
    setFailure("");
    setSuccess("");
    try {
      let candidateBytes: Uint8Array | null = null;
      if (typeof api.readTransferBytes === "function") {
        try { candidateBytes = await api.readTransferBytes(); } catch { candidateBytes = null; }
      }
      const binary = candidateBytes instanceof Uint8Array ? candidateBytes : new TextEncoder().encode(await api.readTransferFile());
      if (binary.length >= 2 && binary[0] === 0x50 && binary[1] === 0x4b) {
        const preview = await api.previewTransferV2(binary);
        const skills = preview.skills ?? [];
        if (!window.confirm(t("确认将 {count} 个 Skill 导入 BootAgent 库？", { count: String(skills.length) }))) return setSuccess(t("已取消导入"));
        await api.applyTransferV2(binary);
        await refreshStatus();
        setSuccess(t("导入完成"));
        return;
      }
      const raw = new TextDecoder().decode(binary);
      // Overwriting existing records is the point of an import, but it takes the
      // saved API keys with it, so it is confirmed the way deleting one is.
      const incoming = transferSummary(raw);
      // status is non-null by the time the button exists, but the early return
      // that proves it lives below this closure, so read through the same
      // fallback the lists above use.
      const savedProviders = status?.providers ?? {};
      const existingProviders = new Set(Object.values(savedProviders).map((provider) => provider.name));
      const existingProviderIDs = new Set(Object.keys(savedProviders));
      const existingProfiles = new Set(profiles.flatMap((profile) => [profile.id, profile.label]));
      const overwrittenProviders = incoming.providers.filter((name) => existingProviders.has(name) || existingProviderIDs.has(name));
      const overwrittenProfiles = incoming.profiles.filter((name) => existingProfiles.has(name));
      if (!await askImport({
        raw,
        overwrittenProviders,
        overwrittenProfiles,
        newProviders: incoming.providers.length - overwrittenProviders.length,
        newProfiles: incoming.profiles.length - overwrittenProfiles.length,
        carriesKeys: incoming.carriesKeys,
      })) return setSuccess(t("已取消导入"));
      // Array.isArray plus a length check, not the bare array: makeTransfer emits
      // `encrypted` either way and `[]` is truthy, so an unencrypted file used to
      // prompt for a password it had no use for.
      const password = transferNeedsPassword(raw) ? await askPassword("import") : "";
      if (transferNeedsPassword(raw) && !password) return setSuccess(t("已取消导入"));
      const data = await parseTransfer(raw, password || "");
      // create: false — an import restores Providers, so an ID that already
      // exists is the expected case and overwriting it is the point. Refusing
      // duplicates here would make re-importing a backup fail.
      for (const provider of data.providers ?? []) {
        const { carriesKey, ...entry } = provider;
        // keepExistingKey when the file supplied no key for this Provider. Without
        // it, importing a key-less export would write an empty APIKey over the
        // recipient's saved credential -- Store.Save writes the field
        // unconditionally (internal/provider/store.go:199).
        await api.saveProvider({ ...entry, create: false, keep_existing_key: !carriesKey });
      }
      for (const profile of data.profiles ?? []) await api.saveProfile({ id: profile.id, label: profile.label, provider: profile.provider, apiBaseUrl: "", apiKey: "", model: profile.model || "", configMode: "provider", protocol: profile.protocol || "" });
      if (data.mcp) await api.saveImportedMCP(await api.previewImportMCP(JSON.stringify(data.mcp), password || ""));
      await refreshStatus();
      setSuccess(t("导入完成"));
    } catch (error) {
      setFailure(describeTransferError(error, t("导入失败")));
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    void api.listMCP().then((items) => setMcpServers((items ?? []).map(({ id, type }) => ({ id, type })))).catch(() => setMcpServers([]));
    void api.listSkills().then((items) => setSkills(Array.isArray(items) ? items : [])).catch(() => setSkills([]));
  }, []);

  if (!status) return <PageScaffold title={t("导入导出")}><div className="loading-block"><span className="spinner" />{t("正在读取环境状态")}</div></PageScaffold>;

  return (
    <PageScaffold
      title={t("导入导出")}
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
        <ModalDialog
          className="transfer-password-dialog"
          label={passwordRequest === "export" ? t("请输入导出密码") : t("请输入导入密码")}
          onDismiss={() => finishPassword(null)}
        >
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
        </ModalDialog>
      ) : null}
      {encryptionRequest ? (
        <ModalDialog className="transfer-password-dialog" label={t("导出设置")} onDismiss={() => finishEncryption(null)}>
          {/* Not including keys is the default and the submit action: a transfer
              file describes which Providers and Profiles exist, which is useful on
              its own, and carrying live credentials is what turns it into a
              secret. The other two options state their own cost. */}
          <form onSubmit={(event) => { event.preventDefault(); finishEncryption("omit"); }}>
            <h2>{t("导出设置")}</h2>
            <p>{t("导出文件是否包含 API Key？")}</p>
            <ul className="transfer-consequences">
              <li>{t("默认不包含。导入方使用自己的 Key，或保留本机已保存的 Key。")}</li>
              <li>{t("选择「加密包含」后，密码无法找回，丢失密码等于文件作废。")}</li>
              <li>{t("选择「明文包含」将以明文保存 API Key，请只在你信任的位置存放该文件。")}</li>
            </ul>
            <footer>
              <button className="button button-secondary" type="button" onClick={() => finishEncryption(null)}>{t("取消")}</button>
              <button className="button button-secondary" type="button" onClick={() => finishEncryption("plain")}>{t("明文包含")}</button>
              <button className="button button-secondary" type="button" onClick={() => finishEncryption("encrypted")}>{t("加密包含")}</button>
              <button className="button button-primary" type="submit">{t("不包含 Key")}</button>
            </footer>
          </form>
        </ModalDialog>
      ) : null}
      {importPlan ? (
        <ModalDialog className="transfer-password-dialog" label={t("确认导入")} onDismiss={() => finishImport(false)}>
          <form onSubmit={(event) => { event.preventDefault(); finishImport(true); }}>
            <h2>{t("确认导入")}</h2>
            {/* The warning has to match the file. Claiming saved keys will be
                replaced is false for a key-less export, and a false warning about
                credentials is worse than none. */}
            <p>{importPlan.carriesKeys
              ? t("导入会覆盖同 ID 的模型服务和配置模版，包括已保存的 API Key，该操作无法撤销。")
              : t("导入会覆盖同 ID 的模型服务和配置模版，该操作无法撤销。")}</p>
            {importPlan.carriesKeys ? null : (
              <p className="transfer-reassurance">{t("导入的文件不包含 API Key，本机已保存的 Key 会保留")}</p>
            )}
            <ul className="transfer-consequences">
              {importPlan.overwrittenProviders.length ? (
                <li>{t("将覆盖 {count} 个模型服务：{names}", {
                  count: importPlan.overwrittenProviders.length,
                  names: importPlan.overwrittenProviders.join(locale === "en" ? ", " : "、"),
                })}</li>
              ) : null}
              {importPlan.overwrittenProfiles.length ? (
                <li>{t("将覆盖 {count} 个配置模版：{names}", {
                  count: importPlan.overwrittenProfiles.length,
                  names: importPlan.overwrittenProfiles.join(locale === "en" ? ", " : "、"),
                })}</li>
              ) : null}
              {importPlan.newProviders > 0 ? <li>{t("将新增 {count} 个模型服务", { count: importPlan.newProviders })}</li> : null}
              {importPlan.newProfiles > 0 ? <li>{t("将新增 {count} 个配置模版", { count: importPlan.newProfiles })}</li> : null}
            </ul>
            <footer>
              <button className="button button-secondary" type="button" onClick={() => finishImport(false)}>{t("取消")}</button>
              <button className="button button-primary" type="submit">{t("导入")}</button>
            </footer>
          </form>
        </ModalDialog>
      ) : null}
      <div className="transfer-toolbar">
        <label className="transfer-search">
          <Search size={16} aria-hidden="true" />
          <input aria-label={t("搜索导入导出内容")} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("搜索导入导出内容")} />
          {query ? <button type="button" className="icon-button" onClick={() => setQuery("")} aria-label={t("清空搜索")}><X size={15} /></button> : null}
        </label>
        <span className="transfer-search-count">{t("已选择 {count} 项", { count: exportProviders.size + selectedProfiles.size + selectedMcp.size + selectedSkills.size })}</span>
      </div>
      <div className="transfer-actions">
        <button className="button button-secondary" type="button" onClick={toggleAll} disabled={busy || (!providers.length && !profiles.length && !mcpServers.length && !skills.length)}>
          <CheckCheck size={15} />{t(allSelected ? "取消全选" : "全选")}
        </button>
      </div>
      <div className="transfer-grid">
        <section className="transfer-section">
          <header><div><h2>{t("模型服务")}</h2><p>{t("已选择 {count} 项", { count: exportProviders.size })}</p></div></header>
          <div className="transfer-list">
            {filteredProviders.map(([id, provider]) => {
              const required = requiredProviders.has(id);
              return <label className="transfer-row" key={id}><input type="checkbox" checked={selectedProviders.has(id) || required} disabled={required} onChange={() => setSelectedProviders(toggle(selectedProviders, id))} /><span><strong>{provider.name}</strong><small>{id}</small></span>{required ? <em>{t("配置模版依赖")}</em> : null}</label>;
            })}
          </div>
        </section>
        <section className="transfer-section">
          <header><div><h2>{t("配置模版")}</h2><p>{t("已选择 {count} 项", { count: selectedProfiles.size })}</p></div></header>
          <div className="transfer-list">
            {filteredProfiles.map((profile) => <label className="transfer-row" key={profile.id}><input type="checkbox" checked={selectedProfiles.has(profile.id)} onChange={() => setSelectedProfiles(toggle(selectedProfiles, profile.id))} /><span><strong>{profile.label || profile.id}</strong><small>{profile.id} · {status.providers[profile.provider]?.name || profile.provider}</small></span></label>)}
          </div>
        </section>
        <section className="transfer-section">
          <header><div><h2>{t("MCP 服务器")}</h2><p>{t("已选择 {count} 项", { count: selectedMcp.size })}</p></div></header>
          <div className="transfer-list">{filteredMcpServers.map((server) => <label className="transfer-row" key={server.id}><input type="checkbox" checked={selectedMcp.has(server.id)} onChange={() => setSelectedMcp(toggle(selectedMcp, server.id))} /><span><strong>{server.id}</strong><small>{server.type}</small></span></label>)}</div>
        </section>
        <section className="transfer-section">
          <header><div><h2><Sparkles size={18} /> {t("Skills")}</h2><p>{t("已选择 {count} 项", { count: selectedSkills.size })}</p></div></header>
          <div className="transfer-list transfer-list-scroll">
            {filteredSkills.length ? filteredSkills.map((skill) => <label className="transfer-row" key={skill.id}><input type="checkbox" checked={selectedSkills.has(skill.id)} onChange={() => setSelectedSkills(toggle(selectedSkills, skill.id))} /><span><strong>{skill.name || skill.id}</strong><small title={skill.description}>{skill.id} · {skill.variants} {t("个版本")}</small></span></label>) : <p className="transfer-empty">{normalizedQuery ? t("没有匹配的导出内容") : t("暂无可导出的 Skills")}</p>}
          </div>
        </section>
      </div>
    </PageScaffold>
  );
}
