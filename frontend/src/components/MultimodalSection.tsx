import { Activity, Images, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { api, describeError } from "../backend/api";
import type { Translate, TranslationKey } from "../i18n";
import { useI18n } from "../i18n";
import type {
  MultimodalCapability,
  MultimodalCapabilityId,
  MultimodalDetection,
  MultimodalProbe,
  MultimodalProvider,
  MultimodalSource,
} from "../types/api";

const CAPABILITY_ORDER: MultimodalCapabilityId[] = [
  "vision",
  "image-generation",
  "speech-to-text",
  "text-to-speech",
  "video-generation",
  "video-understanding",
];

const CAPABILITY_LABELS: Record<MultimodalCapabilityId, TranslationKey> = {
  vision: "图片理解",
  "image-generation": "图片生成",
  "speech-to-text": "语音识别",
  "text-to-speech": "语音合成",
  "video-generation": "视频生成",
  "video-understanding": "视频理解",
};

// Strongest evidence first. An ID rule is a guess from the name, so it sorts
// last and is never preselected over anything the Provider or a probe said.
const SOURCE_RANK: Record<MultimodalSource, number> = { probe: 0, declared: 1, catalog: 2, "id-rule": 3 };

export interface CapabilityOption {
  model: string;
  capability: MultimodalCapability;
}

export interface CapabilityGroup {
  id: MultimodalCapabilityId;
  options: CapabilityOption[];
}

/**
 * Regroups one Provider's models by capability. Within a capability, models
 * that can be installed come first, then by strength of evidence, then by ID,
 * so the first option is the sensible default.
 */
export function groupByCapability(entry: MultimodalProvider): CapabilityGroup[] {
  const groups = new Map<MultimodalCapabilityId, CapabilityOption[]>();
  for (const model of entry.models) {
    for (const capability of model.capabilities) {
      const options = groups.get(capability.id) ?? [];
      options.push({ model: model.id, capability });
      groups.set(capability.id, options);
    }
  }
  return CAPABILITY_ORDER.filter((id) => groups.has(id)).map((id) => ({
    id,
    options: (groups.get(id) ?? []).sort((left, right) => {
      const usable = Number(right.capability.installable) - Number(left.capability.installable);
      if (usable) return usable;
      const evidence = SOURCE_RANK[left.capability.source] - SOURCE_RANK[right.capability.source];
      return evidence || left.model.localeCompare(right.model);
    }),
  }));
}

export function sourceLabel(capability: MultimodalCapability, t: Translate): string {
  switch (capability.source) {
    case "probe":
      return capability.supported ? t("已实测通过") : t("实测未通过");
    case "declared":
      return t("模型服务声明");
    case "catalog":
      return t("内置目录");
    default:
      return t("按模型名推断");
  }
}

/** Why a capability can or cannot be installed yet, in the user's terms. */
export function installLabel(capability: MultimodalCapability, t: Translate): { text: string; tone: "success" | "warning" | "neutral" } {
  if (capability.installable) return { text: t("可安装"), tone: "success" };
  if (capability.source === "probe" && !capability.supported) return { text: t("不可安装"), tone: "warning" };
  if (capability.probe) return { text: t("实测通过后可安装"), tone: "neutral" };
  return { text: t("暂不可安装"), tone: "neutral" };
}

/**
 * The verdict of a probe the user asked for. Like the Profile model check,
 * "unverified" is worded as "could not verify", never as "not supported".
 */
export function describeProbe(check: MultimodalProbe, t: Translate): { ok: boolean; title: string; reason: string; detail: string } {
  const capability = t(CAPABILITY_LABELS[check.capability] ?? "图片理解");
  if (check.status === "available") {
    return {
      ok: true,
      title: t("「{model}」支持{capability}", { model: check.model, capability }),
      reason: t("模型服务接受了这次实测请求。"),
      detail: "",
    };
  }
  const title = check.status === "unverified"
    ? t("无法验证「{model}」的{capability}", { model: check.model, capability })
    : t("「{model}」不支持{capability}", { model: check.model, capability });
  let reason: string;
  switch (check.reason) {
    case "refused":
      reason = check.httpStatus
        ? t("模型服务拒绝了这次实测请求（HTTP {status}）。", { status: check.httpStatus })
        : t("模型服务拒绝了这次实测请求。");
      break;
    case "key":
      reason = t("模型服务拒绝了 API Key，换模型不能解决这个问题。");
      break;
    case "transient":
      reason = check.httpStatus
        ? t("模型服务暂时繁忙或出错（HTTP {status}），模型本身可能没有问题。", { status: check.httpStatus })
        : t("模型服务暂时繁忙或出错，模型本身可能没有问题。");
      break;
    case "network":
      reason = t("暂时连不上模型服务，模型本身可能没有问题。");
      break;
    case "no_key":
      reason = t("该模型服务还没有保存 API Key。");
      break;
    default:
      reason = t("无法向模型服务发送验证请求。");
  }
  return { ok: false, title, reason, detail: check.detail ? t("模型服务返回：{detail}", { detail: check.detail }) : "" };
}

type ProbeState = { checking: boolean; result?: MultimodalProbe; error?: string };

const probeKey = (provider: string, capability: string) => `${provider}::${capability}`;

/**
 * The Skills page's Multimodal capabilities section. Opening it lists models,
 * which is free; nothing billed is sent until the user presses a check button.
 */
export function MultimodalSection() {
  const { t } = useI18n();
  const [detection, setDetection] = useState<MultimodalDetection | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Record<string, string>>({});
  const [probes, setProbes] = useState<Record<string, ProbeState>>({});

  const detect = useCallback(() => {
    setLoading(true);
    setError("");
    return api.detectMultimodal()
      .then(setDetection)
      .catch((reason: unknown) => setError(describeError(reason, t("无法检测多模态能力")).message))
      .finally(() => setLoading(false));
  }, [t]);

  useEffect(() => { void detect(); }, [detect]);

  const probe = (providerId: string, capability: MultimodalCapabilityId, model: string) => {
    const key = probeKey(providerId, capability);
    setProbes((current) => ({ ...current, [key]: { checking: true } }));
    void api.probeMultimodal(providerId, model, capability)
      .then((result) => {
        setProbes((current) => ({ ...current, [key]: { checking: false, result } }));
        // A conclusive verdict is cached by the backend and outranks every
        // other source, so the list is re-read to show it.
        return detect();
      })
      .catch((reason: unknown) => {
        setProbes((current) => ({ ...current, [key]: { checking: false, error: describeError(reason, t("无法向模型服务发送验证请求。")).message } }));
      });
  };

  const providers = useMemo(() => detection?.providers ?? [], [detection]);
  const groupsByProvider = useMemo(() => new Map(providers.map((entry) => [entry.id, groupByCapability(entry)])), [providers]);

  return (
    <section className="content-section multimodal-section" aria-labelledby="multimodal-heading">
      <div className="section-heading">
        <div>
          <h2 id="multimodal-heading"><Images size={18} aria-hidden="true" /> {t("多模态能力")}</h2>
          <p>{t("用已保存 API Key 的模型服务查找能处理图片、音频和视频的模型。列出模型不收费；点击实测会发出真实请求。")}</p>
        </div>
        <button className="button button-secondary" type="button" onClick={() => void detect()} disabled={loading}>
          <RefreshCw size={16} aria-hidden="true" />
          {loading ? t("正在检测") : t("重新检测")}
        </button>
      </div>
      {error ? <p className="multimodal-error" role="alert">{error}</p> : null}
      {detection && !providers.length ? (
        <p className="multimodal-empty">
          {t("还没有保存 API Key 的模型服务。")} <Link to="/providers">{t("去添加模型服务")}</Link>
        </p>
      ) : null}
      {providers.map((entry) => {
        const groups = groupsByProvider.get(entry.id) ?? [];
        return (
          <div className="multimodal-provider" key={entry.id}>
            <header>
              <strong>{entry.name || entry.id}</strong>
              {!entry.listed ? <span className="multimodal-provider-note">{t("无法读取模型列表：{message}", { message: entry.message || entry.errorCode || "" })}</span> : null}
            </header>
            {!groups.length ? (
              <p className="multimodal-empty">{entry.listed ? t("没有找到多模态模型。") : t("可以稍后重新检测。")}</p>
            ) : (
              <ul className="multimodal-capabilities">
                {groups.map((group) => {
                  const key = probeKey(entry.id, group.id);
                  const chosen = group.options.find((option) => option.model === selected[key]) ?? group.options[0];
                  const install = installLabel(chosen.capability, t);
                  const state = probes[key];
                  const verdict = state?.result && state.result.model === chosen.model ? describeProbe(state.result, t) : null;
                  const selectID = `multimodal-${entry.id}-${group.id}`;
                  return (
                    <li key={group.id}>
                      <label className="multimodal-capability-name" htmlFor={selectID}>{t(CAPABILITY_LABELS[group.id])}</label>
                      <select
                        id={selectID}
                        value={chosen.model}
                        onChange={(event) => setSelected((current) => ({ ...current, [key]: event.target.value }))}
                      >
                        {group.options.map((option) => (
                          <option key={option.model} value={option.model}>{option.model}</option>
                        ))}
                      </select>
                      <span className="status-badge status-neutral">{sourceLabel(chosen.capability, t)}</span>
                      <span className={`status-badge status-${install.tone}`}>{install.text}</span>
                      {chosen.capability.probe === "minimal" ? (
                        <button
                          className="button button-secondary model-check-button"
                          type="button"
                          disabled={state?.checking}
                          onClick={() => probe(entry.id, group.id, chosen.model)}
                          title={t("向模型服务发送一次带小图片的真实请求，确认这个模型能做{capability}。可能产生少量费用。", { capability: t(CAPABILITY_LABELS[group.id]) })}
                        >
                          <Activity size={14} aria-hidden="true" />
                          {state?.checking ? t("正在实测") : t("实测")}
                        </button>
                      ) : null}
                      {verdict ? (
                        <div className={`model-check-notice multimodal-verdict${verdict.ok ? " is-ok" : ""}`} role={verdict.ok ? "status" : "alert"}>
                          <div className="model-check-body">
                            <strong>{verdict.title}</strong>
                            <span>{verdict.reason}</span>
                            {verdict.detail ? <span className="model-check-detail">{verdict.detail}</span> : null}
                          </div>
                        </div>
                      ) : null}
                      {state?.error ? <p className="multimodal-error" role="alert">{state.error}</p> : null}
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        );
      })}
    </section>
  );
}
