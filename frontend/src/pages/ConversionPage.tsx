import { Radio } from "lucide-react";
import { useEffect, useState } from "react";

import { api, describeFailure } from "../backend/api";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { useI18n } from "../i18n";
import type { ConversionConfig, StatusResponse } from "../types/api";

export function ConversionPage() {
  const { t } = useI18n();
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
    try { setConfig(await api.saveConversion(config)); }
    catch (error) { setFailure(describeFailure(error, t("无法保存格式转换设置"), t).message); }
    finally { setSaving(false); }
  };

  const profiles = (status?.profiles ?? []).filter((profile) => profile.model);
  return (
    <PageScaffold
      title={t("本地格式转换")}
      description={t("将 Anthropic Messages 和 OpenAI Responses 转为 OpenAI Chat Completions")}
      bodyClassName="settings-page conversion-page"
      primaryLabel={t("保存格式转换设置")}
      onPrimary={() => void save()}
      primaryDisabled={!config || !config.target_profile}
      primaryBusy={saving}
    >
      {failure ? <p className="settings-field-error" role="status">{failure}</p> : null}
      {config ? <section className="settings-section">
        <div className="settings-row"><Radio size={16} aria-hidden="true" /><label className="toggle-row conversion-toggle">
          <span><strong>{t("启用本地 API 格式转换")}</strong><small>{t("监听本机端口并将请求转发到目标 Profile")}</small></span>
          <input type="checkbox" role="switch" checked={config.enabled} onChange={(event) => setConfig({ ...config, enabled: event.target.checked })} />
        </label></div>
        <div className="settings-row conversion-field"><label htmlFor="conversion-target"><strong>{t("目标 Profile")}</strong></label><SelectField id="conversion-target" label={t("目标 Profile")} value={config.target_profile} onChange={(value) => setConfig({ ...config, target_profile: value })} options={profiles.map((profile) => ({ value: profile.id, label: profile.label || profile.id }))} /></div>
        <div className="settings-row conversion-field"><label htmlFor="conversion-listen"><strong>{t("监听地址")}</strong></label><input id="conversion-listen" value={config.listen} onChange={(event) => setConfig({ ...config, listen: event.target.value })} /></div>
        <div className="settings-row conversion-field"><label htmlFor="conversion-key"><strong>{t("本地 API Key")}</strong></label><input id="conversion-key" type="password" value={config.api_key} onChange={(event) => setConfig({ ...config, api_key: event.target.value })} /></div>
        <div className="settings-row conversion-field"><label htmlFor="conversion-anthropic-model"><strong>{t("Anthropic 模型")}</strong></label><input id="conversion-anthropic-model" value={config.anthropic_model} onChange={(event) => setConfig({ ...config, anthropic_model: event.target.value })} /></div>
        <div className="settings-row conversion-field"><label htmlFor="conversion-responses-model"><strong>{t("Responses 模型")}</strong></label><input id="conversion-responses-model" value={config.responses_model} onChange={(event) => setConfig({ ...config, responses_model: event.target.value })} /></div>
      </section> : null}
    </PageScaffold>
  );
}
