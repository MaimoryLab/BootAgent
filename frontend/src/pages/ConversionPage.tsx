import { Power } from "lucide-react";
import { useEffect, useState } from "react";

import { api, describeFailure } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import type { ConversionConfig, StatusResponse } from "../types/api";

export function ConversionPage() {
  const { t } = useI18n();
  const { refreshStatus } = useWizard();
  const [config, setConfig] = useState<ConversionConfig | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [failure, setFailure] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    void Promise.all([api.getConversion(), api.status()]).then(([loaded, nextStatus]) => {
      setConfig(loaded);
      setStatus(nextStatus);
    }).catch((error) => setFailure(describeFailure(error, t("无法读取格式转换设置"), t).message));
  }, [t]);

  const save = async () => {
    if (!config) return;
    setSaving(true);
    setFailure("");
    try { setConfig(await api.saveConversion(config)); void refreshStatus(); }
    catch (error) { setFailure(describeFailure(error, t("无法保存格式转换设置"), t).message); }
    finally { setSaving(false); }
  };

  const toggle = async () => {
    if (!config) return;
    setSaving(true);
    setFailure("");
    try { setConfig(await api.saveConversion({ ...config, enabled: !config.enabled })); void refreshStatus(); }
    catch (error) { setFailure(describeFailure(error, t("无法切换格式转换"), t).message); }
    finally { setSaving(false); }
  };

  const profiles = (status?.profiles ?? []).filter((profile) => profile.protocol === "openai" && profile.model);
  return (
    <PageScaffold
      title={t("本地格式转换")}
      description={t("将 Anthropic Messages 和 OpenAI Responses 转为 OpenAI Chat Completions")}
      bodyClassName="management-page conversion-page"
      footerNote={config ? <span className={`conversion-footer-status${config.enabled ? " is-running" : " is-stopped"}`}>{config.enabled ? t("格式转换正在监听") : t("格式转换当前已停止")}</span> : null}
      secondaryAction={config ? <button className={`button button-secondary conversion-action${config.enabled ? " is-running" : " is-stopped"}`} type="button" onClick={() => void toggle()} disabled={saving || !config.target_profile}><Power size={15} />{config.enabled ? t("停止格式转换") : t("启动格式转换")}</button> : null}
      primaryLabel={t("保存格式转换设置")}
      onPrimary={() => void save()}
      primaryDisabled={!config || !config.target_profile}
      primaryBusy={saving}
    >
      {failure ? <p className="settings-field-error" role="status">{failure}</p> : null}
      {config ? <div className="provider-editor conversion-editor">
        <header><strong>{t("格式转换设置")}</strong></header>
        <div className="provider-editor-grid">
          <div className="field-stack provider-editor-wide">
            <label htmlFor="conversion-target">{t("目标 Profile")}</label>
            <SelectField id="conversion-target" label={t("目标 Profile")} value={config.target_profile} onChange={(value) => setConfig({ ...config, target_profile: value })} options={profiles.map((profile) => ({ value: profile.id, label: profile.label || profile.id }))} />
          </div>
          <div className="field-stack">
            <label htmlFor="conversion-listen">{t("监听地址")}</label>
            <input id="conversion-listen" value={config.listen} onChange={(event) => setConfig({ ...config, listen: event.target.value })} placeholder="127.0.0.1:8787" spellCheck={false} autoCorrect="off" autoCapitalize="none" />
          </div>
          <div className="field-stack">
            <label htmlFor="conversion-key">{t("本地 API Key")}</label>
            <input id="conversion-key" type="password" value={config.api_key} onChange={(event) => setConfig({ ...config, api_key: event.target.value })} autoComplete="off" />
          </div>
          <div className="field-stack">
            <label htmlFor="conversion-anthropic-model">{t("Anthropic 模型")}</label>
            <input id="conversion-anthropic-model" value={config.anthropic_model} onChange={(event) => setConfig({ ...config, anthropic_model: event.target.value })} spellCheck={false} autoCorrect="off" autoCapitalize="none" />
          </div>
          <div className="field-stack">
            <label htmlFor="conversion-responses-model">{t("Responses 模型")}</label>
            <input id="conversion-responses-model" value={config.responses_model} onChange={(event) => setConfig({ ...config, responses_model: event.target.value })} spellCheck={false} autoCorrect="off" autoCapitalize="none" />
          </div>
        </div>
      </div> : null}
    </PageScaffold>
  );
}
