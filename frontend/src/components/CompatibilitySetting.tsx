import { Radio } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

import { api } from "../backend/api";
import { useI18n } from "../i18n";
import { confirmAction } from "../state/confirmDelete";
import { agentsUsingConversion } from "../state/conversion";
import { useFeedback } from "../state/FeedbackContext";
import { useWizard } from "../state/WizardContext";
import type { ConversionConfig } from "../types/api";
import { StatusBadge } from "./StatusBadge";

/**
 * Compatibility mode, as a setting rather than a page.
 *
 * It is switched on where an Agent needs it -- the setup wizard, an Agent's
 * Profile list, a failed model check -- and ends when the last Agent using it
 * is given an ordinary Profile. What is left to set by hand is here: stopping
 * it for every Agent at once, and the address, local keys and model names the
 * adapter itself uses.
 */
export function CompatibilitySetting() {
  const { locale, t } = useI18n();
  const { state, refreshStatus } = useWizard();
  const feedback = useFeedback();
  // Stable across receipts, unlike `feedback`, so the load effect can depend on it.
  const reportFailure = feedback.failure;
  const [config, setConfig] = useState<ConversionConfig | null>(null);
  const [saved, setSaved] = useState<ConversionConfig | null>(null);
  const [saving, setSaving] = useState(false);
  const separator = locale === "en" ? ", " : "、";

  // Retryable from the receipt: without the settings the row has nothing to show.
  const load = useCallback(() => {
    void api.getConversion().then((loaded) => {
      setConfig(loaded);
      setSaved(loaded);
    }).catch((error) => reportFailure(error, t("无法读取兼容模式设置"), { key: "conversion-load", action: { label: t("重试"), run: load } }));
  }, [reportFailure, t]);
  useEffect(load, [load]);

  const persist = async (next: ConversionConfig, done: string, fallback: string) => {
    setSaving(true);
    try {
      const result = await api.saveConversion(next);
      setConfig(result);
      setSaved(result);
      feedback.show({ key: "conversion", tone: "success", message: done });
      void refreshStatus();
    } catch (error) {
      feedback.failure(error, fallback, { key: "conversion" });
    } finally {
      setSaving(false);
    }
  };

  const regenerateKey = async () => {
    setSaving(true);
    try {
      const result = await api.regenerateConversionKey();
      setConfig(result);
      setSaved(result);
      feedback.show({ key: "conversion-key", tone: "success", message: t("已重新生成本地 Key") });
      void refreshStatus();
    } catch (error) {
      feedback.failure(error, t("无法重新生成本地 Key"), { key: "conversion-key" });
    } finally {
      setSaving(false);
    }
  };

  // Who uses it comes from the shared status. Until that has loaded, the row
  // must not say that nobody does, nor let it be stopped without the warning
  // that names them.
  const known = Boolean(state.status);
  const users = agentsUsingConversion(state.status);
  const names = users.map((user) => user.name).join(separator);
  const running = Boolean(saved?.enabled);
  const dirty = Boolean(config && saved && (
    config.listen !== saved.listen
    || config.anthropic_model !== saved.anthropic_model
    || config.responses_model !== saved.responses_model
    || config.chat_model !== saved.chat_model
  ));

  // The switch only changes whether it runs. Fields being edited below are
  // left for their own button, so toggling never saves a half-typed address.
  const toggle = async (enabled: boolean) => {
    if (!saved) return;
    if (!enabled && users.length && !await confirmAction({
      title: t("停止兼容模式"),
      message: t("停止后 {agents} 将无法使用，直到重新开启兼容模式或换一个配置模版。", { agents: names }),
      confirmLabel: t("停止"),
      cancelLabel: t("取消"),
    })) return;
    if (enabled) await persist({ ...saved, enabled: true }, t("兼容模式已开启"), t("无法开启兼容模式"));
    else await persist({ ...saved, enabled: false }, t("兼容模式已停止"), t("无法停止兼容模式"));
  };

  const summary = !known ? t("正在读取环境状态") : running
    ? users.length ? t("{agents} 正在使用，使用期间需要保持 BootAgent 运行", { agents: names }) : t("没有 Agent 在使用")
    : users.length ? t("已停止，{agents} 暂时无法使用", { agents: names }) : t("Agent 需要时会自动开启");

  return (
    <>
      <div className="settings-row settings-toggle-row">
        <label className="toggle-row">
          <Radio size={16} aria-hidden="true" />
          <span>
            <strong>{t("兼容模式")}</strong>
            <small>{summary}</small>
          </span>
          <input
            type="checkbox"
            role="switch"
            aria-label={t("兼容模式")}
            checked={running}
            // Nothing to turn on while no Agent is pointed at it.
            disabled={!saved || !known || saving || (!running && users.length === 0)}
            onChange={(event) => void toggle(event.target.checked)}
          />
        </label>
      </div>
      {config ? (
        <details className="settings-row compatibility-advanced">
          <summary>{t("高级设置")}</summary>
          <div className="compatibility-advanced-body">
            <p>{t("让只支持特定格式的 Agent 也能用你的模型服务，在本机转换，不经过其他服务器。每个 Agent 用自己的本机 Key 连接，模型和 Key 仍由各自的配置模版管理。")}</p>
            <div className="provider-editor-grid">
              <div className="field-stack">
                <label htmlFor="conversion-listen">{t("监听地址")}</label>
                <input
                  id="conversion-listen"
                  value={config.listen}
                  onChange={(event) => setConfig({ ...config, listen: event.target.value })}
                  placeholder="127.0.0.1:8787"
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label>{t("本地鉴权")}</label>
                <div className="conversion-key-status">
                  <StatusBadge tone={config.has_api_key ? "success" : "neutral"}>
                    {config.has_api_key ? t("已安全生成") : t("启动时自动生成")}
                  </StatusBadge>
                  {config.has_api_key ? (
                    <button className="button button-secondary" type="button" onClick={() => void regenerateKey()} disabled={saving}>
                      {t("重新生成 Key")}
                    </button>
                  ) : null}
                </div>
              </div>
              {/* What each Agent is told the model is called. The model that
                  actually answers is the one in the Agent's own Profile. */}
              <div className="field-stack">
                <label htmlFor="conversion-anthropic-model">{t("Claude Code 看到的模型名")}</label>
                <input
                  id="conversion-anthropic-model"
                  value={config.anthropic_model}
                  onChange={(event) => setConfig({ ...config, anthropic_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-responses-model">{t("Codex 看到的模型名")}</label>
                <input
                  id="conversion-responses-model"
                  value={config.responses_model}
                  onChange={(event) => setConfig({ ...config, responses_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
              <div className="field-stack">
                <label htmlFor="conversion-chat-model">{t("其他 Agent 看到的模型名")}</label>
                <input
                  id="conversion-chat-model"
                  value={config.chat_model}
                  onChange={(event) => setConfig({ ...config, chat_model: event.target.value })}
                  spellCheck={false}
                  autoCorrect="off"
                  autoCapitalize="none"
                />
              </div>
            </div>
            <button
              className="button button-secondary"
              type="button"
              disabled={!dirty || saving}
              onClick={() => void persist({ ...config, enabled: running }, t("已保存兼容模式设置"), t("无法保存兼容模式设置"))}
            >
              {t("保存更改")}
            </button>
          </div>
        </details>
      ) : null}
    </>
  );
}
