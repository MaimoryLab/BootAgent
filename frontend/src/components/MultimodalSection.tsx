import { Activity, Check, Copy, Download, Images, RefreshCw, Trash2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type KeyboardEvent } from "react";
import { Link } from "react-router-dom";

import { api, describeError } from "../backend/api";
import type { Translate, TranslationKey } from "../i18n";
import { useI18n } from "../i18n";
import { isConverterID } from "../state/conversion";
import { byProviderCreatedAt } from "../state/ranking";
import type {
  InstalledMultimodal,
  MultimodalCapability,
  MultimodalCapabilityId,
  MultimodalDetection,
  MultimodalInstallResult,
  MultimodalProbe,
  MultimodalProbeKind,
  MultimodalProvider,
  MultimodalSource,
  StatusResponse,
} from "../types/api";
import { copyToClipboard } from "../utils/clipboard";
import { ModalDialog } from "./ModalDialog";
import { EXAMPLE_PROMPTS } from "./multimodalPrompts";

// Also the tab order. Generation leads: it is what users come to this page
// for, while understanding is something most chat models already do.
export const CAPABILITY_ORDER: MultimodalCapabilityId[] = [
  "image-generation",
  "video-generation",
  "vision",
  "video-understanding",
  "text-to-speech",
  "speech-to-text",
];

export const CAPABILITY_LABELS: Record<MultimodalCapabilityId, TranslationKey> = {
  vision: "图片理解",
  "image-generation": "图片生成",
  "speech-to-text": "语音识别",
  "text-to-speech": "语音合成",
  "video-generation": "视频生成",
  "video-understanding": "视频理解",
};

// Shown in the manual entry hint: model IDs a user is likely to know.
const MANUAL_EXAMPLES: Partial<Record<MultimodalCapabilityId, string>> = {
  vision: "gpt-4o-mini",
  "image-generation": "gpt-image-1",
  "speech-to-text": "whisper-1",
  "text-to-speech": "tts-1",
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
 * that can be installed come first, then the catalog's recommended one, then
 * by strength of evidence, then by ID, so the first option is the sensible
 * default.
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
      const recommended = Number(Boolean(right.capability.recommended)) - Number(Boolean(left.capability.recommended));
      if (recommended) return recommended;
      const evidence = SOURCE_RANK[left.capability.source] - SOURCE_RANK[right.capability.source];
      return evidence || left.model.localeCompare(right.model);
    }),
  }));
}

/**
 * Orders the keyed Providers the way the Providers page lists accounts: the
 * ones the user added first, newest first, then built-ins in catalog order.
 * The backend lists built-ins first; a Provider the status does not know keeps
 * its place after the known ones.
 */
export function orderAccounts(providers: MultimodalProvider[], status?: StatusResponse["providers"]): MultimodalProvider[] {
  const rank = new Map(byProviderCreatedAt(status ?? {}).map(([id], index) => [id, index]));
  return providers
    // BootAgent's own protocol converters hold a key but are not accounts;
    // the Providers page hides them too.
    .filter((entry) => !isConverterID(entry.id))
    .map((entry, index) => ({ entry, index }))
    .sort((left, right) => (rank.get(left.entry.id) ?? rank.size + left.index) - (rank.get(right.entry.id) ?? rank.size + right.index))
    .map(({ entry }) => entry);
}

/** One capability's accounts: every keyed Provider with a model for it, in account order. */
export function accountsFor(providers: MultimodalProvider[], capability: MultimodalCapabilityId): Array<{ provider: MultimodalProvider; options: CapabilityOption[] }> {
  return providers.flatMap((provider) => {
    const group = groupByCapability(provider).find((item) => item.id === capability);
    return group ? [{ provider, options: group.options }] : [];
  });
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

/**
 * Whether a capability can be installed now, and if not, why not, in the
 * user's terms. `generatable` is the backend's list of routes this version can
 * write a Skill for.
 */
export function installLabel(capability: MultimodalCapability, generatable: string[], t: Translate): { text: string; tone: "success" | "warning" | "neutral"; ready: boolean } {
  if (capability.source === "probe" && !capability.supported) return { text: t("不可安装"), tone: "warning", ready: false };
  if (capability.installable && generatable.includes(capability.adapter)) return { text: t("可安装"), tone: "success", ready: true };
  if (capability.installable) return { text: t("暂不支持安装"), tone: "neutral", ready: false };
  if (capability.probe) return { text: t("实测通过后可安装"), tone: "neutral", ready: false };
  return { text: t("暂不可安装"), tone: "neutral", ready: false };
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

/** One card per installed Skill: a capability may be on several models across Agents. */
export function groupInstalled(installed: InstalledMultimodal[]): Array<{ skillId: string; capability: MultimodalCapabilityId; variants: InstalledMultimodal[]; agents: string[] }> {
  const groups = new Map<string, InstalledMultimodal[]>();
  for (const entry of installed) groups.set(entry.skillId, [...(groups.get(entry.skillId) ?? []), entry]);
  return CAPABILITY_ORDER.flatMap((capability) => [...groups.entries()]
    .filter(([, variants]) => variants[0].capability === capability)
    .map(([skillId, variants]) => ({ skillId, capability, variants, agents: [...new Set(variants.flatMap((variant) => variant.agents))].sort() })));
}

// `key` is where the result is shown: an account's row, or a tab's manual entry.
type ProbeState = { model: string; checking: boolean; result?: MultimodalProbe; error?: string };
type ProbeTarget = { key: string; provider: MultimodalProvider; capability: MultimodalCapabilityId; model: string; adapter: string; kind: MultimodalProbeKind };
type InstallTarget = { provider: MultimodalProvider; capability: MultimodalCapabilityId; model: string };

const rowKey = (provider: string, capability: string) => `${provider}::${capability}`;
const manualKey = (capability: string) => `manual::${capability}`;

interface MultimodalSectionProps {
  /** Agent ID to display name. */
  agentLabels?: Record<string, string>;
  /** The status's Provider records: account order and which ones the user added. */
  providers?: StatusResponse["providers"];
  /** Called after an install or uninstall changed the Skill library. */
  onSkillsChanged?: () => void;
}

/**
 * The Multimodal page. Each Provider with a saved key is an account; one tab
 * per capability lists the accounts that can provide it, the ones the user
 * added first. Opening it lists models, which is free; nothing billed is sent
 * until the user presses a check button, and a check that produces billed
 * output asks first.
 */
export function MultimodalSection({ agentLabels = {}, providers: statusProviders, onSkillsChanged }: MultimodalSectionProps) {
  const { t, locale } = useI18n();
  const [detection, setDetection] = useState<MultimodalDetection | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [active, setActive] = useState<MultimodalCapabilityId | null>(null);
  const [selected, setSelected] = useState<Record<string, string>>({});
  const [probes, setProbes] = useState<Record<string, ProbeState>>({});
  const [confirmProbe, setConfirmProbe] = useState<ProbeTarget | null>(null);
  const [installTarget, setInstallTarget] = useState<InstallTarget | null>(null);
  const [uninstallTarget, setUninstallTarget] = useState<{ skillId: string; capability: MultimodalCapabilityId; agents: string[] } | null>(null);
  const [uninstalling, setUninstalling] = useState(false);
  const [manual, setManual] = useState<Partial<Record<MultimodalCapabilityId, { provider: string; model: string }>>>({});
  const nameOf = (agentId: string) => agentLabels[agentId] ?? agentId;
  const separator = locale === "en" ? ", " : "、";

  const detect = useCallback(() => {
    setLoading(true);
    setError("");
    return api.detectMultimodal()
      .then(setDetection)
      .catch((reason: unknown) => setError(describeError(reason, t("无法检测多模态能力")).message))
      .finally(() => setLoading(false));
  }, [t]);

  useEffect(() => { void detect(); }, [detect]);

  const runProbe = (target: ProbeTarget, confirmed: boolean) => {
    setProbes((current) => ({ ...current, [target.key]: { model: target.model, checking: true } }));
    void api.probeMultimodal(target.provider.id, target.model, target.capability, target.adapter, confirmed)
      .then((result) => {
        setProbes((current) => ({ ...current, [target.key]: { model: target.model, checking: false, result } }));
        // Show the probed model in its account's row; a conclusive verdict is
        // cached by the backend and outranks every other source once re-read.
        setSelected((current) => ({ ...current, [rowKey(target.provider.id, target.capability)]: target.model }));
        return detect();
      })
      .catch((reason: unknown) => {
        setProbes((current) => ({ ...current, [target.key]: { model: target.model, checking: false, error: describeError(reason, t("无法向模型服务发送验证请求。")).message } }));
      });
  };
  const probe = (target: ProbeTarget) => (target.kind === "confirm" ? setConfirmProbe(target) : runProbe(target, false));

  const uninstall = () => {
    if (!uninstallTarget) return;
    setUninstalling(true);
    void api.uninstallSkill(uninstallTarget.skillId)
      .then((result) => {
        if (result.error) setError(result.error);
        setUninstallTarget(null);
        onSkillsChanged?.();
        return detect();
      })
      .catch((reason: unknown) => setError(describeError(reason, t("卸载失败")).message))
      .finally(() => setUninstalling(false));
  };

  const accounts = useMemo(() => orderAccounts(detection?.providers ?? [], statusProviders), [detection, statusProviders]);
  const byCapability = useMemo(() => new Map(CAPABILITY_ORDER.map((id) => [id, accountsFor(accounts, id)])), [accounts]);
  const installedCards = useMemo(() => groupInstalled(detection?.installed ?? []), [detection]);
  const providerName = (id: string) => accounts.find((entry) => entry.id === id)?.name || statusProviders?.[id]?.name || id;
  const generatable = detection?.generatable ?? [];
  // Until the user picks a tab: the first capability something can be done with.
  const current = active
    ?? CAPABILITY_ORDER.find((id) => byCapability.get(id)?.length || installedCards.some((card) => card.capability === id))
    ?? CAPABILITY_ORDER[0];

  const onTabKey = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const last = CAPABILITY_ORDER.length - 1;
    const next = CAPABILITY_ORDER[event.key === "Home" ? 0 : event.key === "End" ? last : (index + (event.key === "ArrowRight" ? 1 : -1) + CAPABILITY_ORDER.length) % CAPABILITY_ORDER.length];
    setActive(next);
    document.getElementById(`multimodal-tab-${next}`)?.focus();
  };

  const renderPanel = (capability: MultimodalCapabilityId) => {
    const capabilityName = t(CAPABILITY_LABELS[capability]);
    const offering = byCapability.get(capability) ?? [];
    const card = installedCards.find((item) => item.capability === capability);
    const unsupported = accounts.filter((entry) => entry.unsupported.includes(capability));
    const without = accounts.filter((entry) => entry.listed && !entry.unsupported.includes(capability) && !offering.some((account) => account.provider.id === entry.id));
    const manualRoute = (detection?.manual ?? []).find((route) => route.capability === capability);
    const manualState = manual[capability];
    const manualProvider = accounts.find((entry) => entry.id === manualState?.provider) ?? accounts[0];
    const manualProbe = probes[manualKey(capability)];
    const example = MANUAL_EXAMPLES[capability];
    const chosenFor = (entry: MultimodalProvider, options: CapabilityOption[]) => options.find((option) => option.model === selected[rowKey(entry.id, capability)]) ?? options[0];
    // Said once for the tab, not on every account that samples frames.
    const needsFFmpeg = Boolean(detection && !detection.ffmpeg) && offering.some(({ provider: entry, options }) => chosenFor(entry, options).capability.adapter === "frames-chat-vision");
    return (
      <>
        {card ? (
          <article className="multimodal-installed-card">
            <header>
              <strong>{t("已安装")}</strong>
              <code>{card.skillId}</code>
              <button className="icon-button row-action is-danger" type="button" title={t("卸载")} aria-label={t("卸载 {name}", { name: card.skillId })} onClick={() => setUninstallTarget({ skillId: card.skillId, capability: card.capability, agents: card.agents })}>
                <Trash2 size={16} aria-hidden="true" />
              </button>
            </header>
            {card.variants.map((variant) => (
              <p className="multimodal-installed-line" key={`${variant.providerId}-${variant.model}`}>
                {variant.agents.length
                  ? t("{model}（{provider}）已安装到 {agents}", { model: variant.model, provider: providerName(variant.providerId), agents: variant.agents.map(nameOf).join(separator) })
                  : t("{model}（{provider}）", { model: variant.model, provider: providerName(variant.providerId) })}
              </p>
            ))}
            {card.variants.filter((variant) => variant.guidePath).map((variant) => (
              <GuidePrompt key={variant.guidePath} path={variant.guidePath ?? ""} capability={card.capability} />
            ))}
            <ExamplePrompts capability={card.capability} />
          </article>
        ) : null}

        {offering.length ? (
          <ul className="multimodal-accounts">
            {offering.map(({ provider: entry, options }) => {
              const key = rowKey(entry.id, capability);
              const chosen = chosenFor(entry, options);
              const install = installLabel(chosen.capability, generatable, t);
              const installedHere = (detection?.installed ?? []).find((item) => item.providerId === entry.id && item.capability === capability && item.model === chosen.model);
              const state = probes[key];
              const verdict = state?.result && state.result.model === chosen.model ? describeProbe(state.result, t) : null;
              const selectID = `multimodal-${entry.id}-${capability}`;
              return (
                <li className="multimodal-account" key={entry.id}>
                  <header className="provider-title">
                    <strong>{entry.name || entry.id}</strong>
                    {statusProviders?.[entry.id]?.custom ? <small>{t("用户添加")}</small> : null}
                  </header>
                  <div className="multimodal-account-row">
                    {options.length > 1 ? (
                      <>
                        <label className="visually-hidden" htmlFor={selectID}>{t("{provider} 的{capability}模型", { provider: entry.name || entry.id, capability: capabilityName })}</label>
                        <select id={selectID} value={chosen.model} onChange={(event) => setSelected((current) => ({ ...current, [key]: event.target.value }))}>
                          {options.map((option) => <option key={option.model} value={option.model}>{option.model}</option>)}
                        </select>
                      </>
                    ) : <code className="multimodal-model">{chosen.model}</code>}
                    <span className="status-badge status-neutral">{sourceLabel(chosen.capability, t)}</span>
                    {installedHere
                      ? <span className="status-badge status-success">{t("已安装到 {agents}", { agents: installedHere.agents.map(nameOf).join(separator) })}</span>
                      : <span className={`status-badge status-${install.tone}`}>{install.text}</span>}
                    <span className="multimodal-account-actions">
                      {chosen.capability.probe ? (
                        <ProbeButton
                          checking={state?.checking}
                          kind={chosen.capability.probe}
                          capability={capabilityName}
                          onClick={() => probe({ key, provider: entry, capability, model: chosen.model, adapter: chosen.capability.adapter, kind: chosen.capability.probe })}
                        />
                      ) : null}
                      {install.ready ? (
                        <button className="button button-primary model-check-button" type="button" onClick={() => setInstallTarget({ provider: entry, capability, model: chosen.model })}>
                          <Download size={14} aria-hidden="true" />
                          {installedHere ? t("重新安装") : t("安装")}
                        </button>
                      ) : null}
                    </span>
                  </div>
                  {verdict ? <Verdict verdict={verdict} /> : null}
                  {state?.error && state.model === chosen.model ? <p className="multimodal-error" role="alert">{state.error}</p> : null}
                </li>
              );
            })}
          </ul>
        ) : (
          <p className="multimodal-empty">{t("还没有已保存 API Key 的模型服务能做{capability}。", { capability: capabilityName })}</p>
        )}

        {needsFFmpeg ? (
          <p className="multimodal-provider-note">{t("视频理解要先用 ffmpeg 抽帧。BootAgent 没有在 PATH 上找到 ffmpeg；请在 Agent 运行的环境里安装它。")}</p>
        ) : null}
        {offering.length && without.length ? (
          <p className="multimodal-provider-note">{t("{providers} 的模型列表里没有能做{capability}的模型。", { providers: without.map((entry) => entry.name || entry.id).join(separator), capability: capabilityName })}</p>
        ) : null}
        {unsupported.length ? (
          <p className="multimodal-provider-note">{t("{providers}：该模型服务目前没有可核实的公开接口，暂不支持{capability}。", { providers: unsupported.map((entry) => entry.name || entry.id).join(separator), capability: capabilityName })}</p>
        ) : null}

        {manualRoute && manualProvider ? (
          <details className="multimodal-manual" key={capability}>
            <summary>{t("手动填写模型 ID")}</summary>
            <p>{example
              ? t("如果你知道能做{capability}的模型 ID（例如 {example}），可以填写后实测，通过后即可安装。", { capability: capabilityName, example })
              : t("如果你知道能做{capability}的模型 ID，可以填写后实测，通过后即可安装。", { capability: capabilityName })}</p>
            <div className="multimodal-manual-row">
              {accounts.length > 1 ? (
                <select aria-label={t("模型服务")} value={manualProvider.id} onChange={(event) => setManual((all) => ({ ...all, [capability]: { provider: event.target.value, model: all[capability]?.model ?? "" } }))}>
                  {accounts.map((entry) => <option key={entry.id} value={entry.id}>{entry.name || entry.id}</option>)}
                </select>
              ) : null}
              <input aria-label={t("模型 ID")} placeholder={t("模型 ID")} value={manualState?.model ?? ""} onChange={(event) => setManual((all) => ({ ...all, [capability]: { provider: manualProvider.id, model: event.target.value } }))} />
              <ProbeButton
                checking={manualProbe?.checking}
                disabled={!manualState?.model.trim()}
                kind={manualRoute.probe}
                capability={capabilityName}
                onClick={() => probe({ key: manualKey(capability), provider: manualProvider, capability, model: (manualState?.model ?? "").trim(), adapter: manualRoute.adapter, kind: manualRoute.probe })}
              />
            </div>
            {manualProbe?.result ? <Verdict verdict={describeProbe(manualProbe.result, t)} /> : null}
            {manualProbe?.error ? <p className="multimodal-error" role="alert">{manualProbe.error}</p> : null}
          </details>
        ) : null}
      </>
    );
  };

  const unlisted = accounts.filter((entry) => !entry.listed);
  return (
    <section className="content-section multimodal-section" aria-labelledby="multimodal-heading">
      <div className="section-heading">
        <div>
          <h2 id="multimodal-heading"><Images size={18} aria-hidden="true" /> {t("多模态能力")}</h2>
          <p>{t("按能力查看已保存 API Key 的模型服务能做什么。列出模型不收费；点击实测会发出真实请求。")}</p>
        </div>
        <button className="button button-secondary" type="button" onClick={() => void detect()} disabled={loading}>
          <RefreshCw size={16} aria-hidden="true" />
          {loading ? t("正在检测") : t("重新检测")}
        </button>
      </div>
      {error ? <p className="multimodal-error" role="alert">{error}</p> : null}
      {unlisted.map((entry) => (
        <p className="multimodal-provider-note" key={entry.id}>{t("{provider}：无法读取模型列表：{message}", { provider: entry.name || entry.id, message: entry.message || entry.errorCode || "" })}</p>
      ))}

      {detection && !accounts.length ? (
        <p className="multimodal-empty">
          {t("还没有保存 API Key 的模型服务。")} <Link to="/providers">{t("去添加模型服务")}</Link>
        </p>
      ) : null}

      {detection && (accounts.length || installedCards.length) ? (
        <>
          <div className="marketplace-tabs multimodal-tabs" role="tablist" aria-label={t("多模态能力分类")}>
            {CAPABILITY_ORDER.map((id, index) => {
              const count = byCapability.get(id)?.length ?? 0;
              const installed = installedCards.some((card) => card.capability === id);
              return (
                <button
                  key={id}
                  role="tab"
                  type="button"
                  id={`multimodal-tab-${id}`}
                  aria-controls="multimodal-panel"
                  aria-selected={current === id}
                  tabIndex={current === id ? 0 : -1}
                  className={`marketplace-tab${current === id ? " is-active" : ""}`}
                  onClick={() => setActive(id)}
                  onKeyDown={(event) => onTabKey(event, index)}
                >
                  {t(CAPABILITY_LABELS[id])}
                  {installed ? <span className="multimodal-tab-installed" title={t("已安装")}><Check size={12} aria-hidden="true" /><span className="visually-hidden">{t("已安装")}</span></span> : null}
                  {count ? <span className="marketplace-tab-count" title={t("{count} 个账号可用", { count })}>{count}</span> : null}
                </button>
              );
            })}
          </div>
          <div className="multimodal-panel" id="multimodal-panel" role="tabpanel" aria-labelledby={`multimodal-tab-${current}`}>
            {renderPanel(current)}
          </div>
        </>
      ) : null}

      {confirmProbe ? (
        <ModalDialog className="mcp-modal multimodal-modal" label={t("确认实测")} onDismiss={() => setConfirmProbe(null)}>
          <header><h2>{t("确认实测")}</h2><button className="icon-button" type="button" onClick={() => setConfirmProbe(null)} title={t("取消")}><X size={18} /></button></header>
          <p>{t("这次实测会让「{model}」真实生成一张图片，按模型服务的价格计费。确定要发送吗？", { model: confirmProbe.model })}</p>
          <footer>
            <button className="button button-secondary" type="button" onClick={() => setConfirmProbe(null)}>{t("取消")}</button>
            <button className="button button-primary" type="button" onClick={() => { runProbe(confirmProbe, true); setConfirmProbe(null); }}>{t("发送实测请求")}</button>
          </footer>
        </ModalDialog>
      ) : null}

      {installTarget && detection ? (
        <InstallDialog
          target={installTarget}
          targets={detection.targets}
          fallback={detection.fallback}
          current={detection.installed.filter((item) => item.capability === installTarget.capability)}
          providerName={providerName}
          nameOf={nameOf}
          onDone={() => { setInstallTarget(null); onSkillsChanged?.(); void detect(); }}
          onCancel={() => setInstallTarget(null)}
        />
      ) : null}

      {uninstallTarget ? (
        <ModalDialog className="mcp-modal multimodal-modal" label={t("卸载 {name}", { name: uninstallTarget.skillId })} onDismiss={() => setUninstallTarget(null)}>
          <header><h2>{t("卸载 {name}", { name: uninstallTarget.skillId })}</h2><button className="icon-button" type="button" onClick={() => setUninstallTarget(null)} title={t("取消")}><X size={18} /></button></header>
          <p>{t("将从 {agents} 移除这个 Skill。BootAgent 会保留一份备份；没有其他 Skill 再用到的 API Key 环境变量也会一并移除。", { agents: uninstallTarget.agents.map(nameOf).join(separator) })}</p>
          <footer>
            <button className="button button-secondary" type="button" onClick={() => setUninstallTarget(null)}>{t("取消")}</button>
            <button className="button button-primary" type="button" disabled={uninstalling} onClick={uninstall}>{uninstalling ? t("正在卸载") : t("卸载")}</button>
          </footer>
        </ModalDialog>
      ) : null}
    </section>
  );
}

function ProbeButton({ checking, disabled, kind, capability, onClick }: { checking?: boolean; disabled?: boolean; kind: MultimodalProbeKind; capability: string; onClick: () => void }) {
  const { t } = useI18n();
  const title = kind === "confirm"
    ? t("向模型服务发送一次真实的生成请求，确认这个模型能做{capability}。会产生一次生成的费用，发送前会先确认。", { capability })
    : t("向模型服务发送一次最小的真实请求，确认这个模型能做{capability}。可能产生少量费用。", { capability });
  return (
    <button className="button button-secondary model-check-button" type="button" disabled={checking || disabled} onClick={onClick} title={title}>
      <Activity size={14} aria-hidden="true" />
      {checking ? t("正在实测") : t("实测")}
    </button>
  );
}

function Verdict({ verdict }: { verdict: ReturnType<typeof describeProbe> }) {
  return (
    <div className={`model-check-notice multimodal-verdict${verdict.ok ? " is-ok" : ""}`} role={verdict.ok ? "status" : "alert"}>
      <div className="model-check-body">
        <strong>{verdict.title}</strong>
        <span>{verdict.reason}</span>
        {verdict.detail ? <span className="model-check-detail">{verdict.detail}</span> : null}
      </div>
    </div>
  );
}

/**
 * For Agents without Skills: the guide's path and a prompt that points the
 * Agent at it, in both languages, with copy buttons.
 */
function GuidePrompt({ path, capability }: { path: string; capability: MultimodalCapabilityId }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState("");
  const example = EXAMPLE_PROMPTS[capability]?.[0];
  const prompts = [
    { lang: "zh-CN", text: `先读 ${path}/SKILL.md，按里面的说明完成：${example?.zh ?? ""}` },
    { lang: "en", text: `First read ${path}/SKILL.md and follow it to do this: ${example?.en ?? ""}` },
  ];
  const copy = (text: string) => {
    void copyToClipboard(text).then((ok) => {
      if (!ok) return;
      setCopied(text);
      window.setTimeout(() => setCopied((current) => (current === text ? "" : current)), 1500);
    });
  };
  return (
    <div className="multimodal-prompts multimodal-guide">
      <span className="multimodal-prompts-title">{t("其他 Agent：说明文件在 {path}", { path })}</span>
      <ul>
        {prompts.map((item) => (
          <li key={item.lang}>
            <span lang={item.lang}>{item.text}</span>
            <button className="icon-button" type="button" onClick={() => copy(item.text)} title={t("复制提示词")} aria-label={t("复制提示词")}>
              {copied === item.text ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ExamplePrompts({ capability }: { capability: MultimodalCapabilityId }) {
  const { t } = useI18n();
  const [copied, setCopied] = useState("");
  const prompts = EXAMPLE_PROMPTS[capability] ?? [];
  if (!prompts.length) return null;
  const copy = (text: string) => {
    void copyToClipboard(text).then((ok) => {
      if (!ok) return;
      setCopied(text);
      window.setTimeout(() => setCopied((current) => (current === text ? "" : current)), 1500);
    });
  };
  return (
    <div className="multimodal-prompts">
      <span className="multimodal-prompts-title">{t("在 Agent 里试试")}</span>
      <ul>
        {prompts.flatMap((prompt) => [
          { text: prompt.zh, lang: "zh-CN" },
          { text: prompt.en, lang: "en" },
        ]).map((item) => (
          <li key={item.text}>
            <span lang={item.lang}>{item.text}</span>
            <button className="icon-button" type="button" onClick={() => copy(item.text)} title={t("复制提示词")} aria-label={t("复制提示词")}>
              {copied === item.text ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

interface InstallDialogProps {
  target: InstallTarget;
  targets: string[];
  /** Targets whose key goes to the private fallback file. */
  fallback: string[];
  /** What is installed for this capability now, from any Provider. */
  current: InstalledMultimodal[];
  providerName: (id: string) => string;
  nameOf: (agentId: string) => string;
  onDone: () => void;
  onCancel: () => void;
}

function InstallDialog({ target, targets, fallback, current, providerName, nameOf, onDone, onCancel }: InstallDialogProps) {
  const { t, locale } = useI18n();
  const [agents, setAgents] = useState<string[]>(targets);
  const [guide, setGuide] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<MultimodalInstallResult | null>(null);
  const [error, setError] = useState("");
  const capability = t(CAPABILITY_LABELS[target.capability]);
  const separator = locale === "en" ? ", " : "、";
  const replacing = current.filter((item) => (item.providerId !== target.provider.id || item.model !== target.model) && item.agents.some((agent) => agents.includes(agent)));

  const install = () => {
    setBusy(true);
    setError("");
    void api.installMultimodal({ provider: target.provider.id, model: target.model, capability: target.capability, agents, guide })
      .then(setResult)
      .catch((reason: unknown) => setError(describeError(reason, t("安装失败")).message))
      .finally(() => setBusy(false));
  };

  const title = t("安装{capability} Skill", { capability });
  const close = () => {
    if (!busy) (result ? onDone : onCancel)();
  };
  return (
    <ModalDialog className="mcp-modal multimodal-modal" label={title} onDismiss={close}>
      <header><h2>{title}</h2><button className="icon-button" type="button" disabled={busy} onClick={close} title={t("关闭")}><X size={18} /></button></header>
      {result ? (
        <>
          <ul className="multimodal-results">
            {result.results.map((item) => (
              <li key={item.agent} className={item.installed && item.envWritten ? "is-ok" : "is-failed"}>
                <strong>{item.agent === "guide" ? t("其他 Agent（说明文件）") : nameOf(item.agent)}</strong>
                <span>{item.installed && item.envWritten ? t("已安装") : item.error || t("安装失败")}</span>
              </li>
            ))}
          </ul>
          {result.results.some((item) => item.installed) ? (
            <>
              <p className="multimodal-note">{t("正在运行的 Agent 需要重启，才能读到新写入的环境变量。")}</p>
              {result.guidePath ? <GuidePrompt path={result.guidePath} capability={target.capability} /> : null}
              <ExamplePrompts capability={target.capability} />
            </>
          ) : null}
          <footer><button className="button button-primary" type="button" onClick={onDone}>{t("完成")}</button></footer>
        </>
      ) : (
        <>
          <p>{t("用「{model}」（{provider}）为 Agent 提供{capability}。", { model: target.model, provider: providerName(target.provider.id), capability })}</p>
          <fieldset className="multimodal-agents" disabled={busy}>
            <legend>{t("安装到")}</legend>
            {targets.map((agent) => (
              <label key={agent}>
                <input type="checkbox" checked={agents.includes(agent)} onChange={(event) => setAgents((list) => (event.target.checked ? [...list, agent] : list.filter((item) => item !== agent)))} />
                {nameOf(agent)}
              </label>
            ))}
            <label>
              <input type="checkbox" checked={guide} onChange={(event) => setGuide(event.target.checked)} />
              {t("其他 Agent（生成说明文件）")}
            </label>
          </fieldset>
          {replacing.length ? (
            <p className="multimodal-note">{t("将替换这些 Agent 当前使用的 {models}。", { models: replacing.map((item) => `${item.model}（${providerName(item.providerId)}）`).join(separator) })}</p>
          ) : null}
          <div className="model-check-notice multimodal-key-notice" role="note">
            <div className="model-check-body">
              <strong>{t("API Key 对 Agent 可见")}</strong>
              <span>{t("安装会把 {provider} 的 API Key 写进所选 Agent 自己的配置文件，变量名是 {name}。Agent 运行的任何命令都能读到它，Agent 使用的模型也可能看到。", { provider: providerName(target.provider.id), name: target.provider.keyEnv })}</span>
              {guide || agents.some((agent) => fallback.includes(agent)) ? (
                <span>{t("{agents}没有可配置的环境变量，Key 会写进 BootAgent 的私有文件 {path}，由 Skill 的脚本读取。", { agents: [...agents.filter((agent) => fallback.includes(agent)).map(nameOf), ...(guide ? [t("其他 Agent（生成说明文件）")] : [])].join(separator), path: target.provider.keyFile })}</span>
              ) : null}
              <span>{t("Skill 的脚本需要 Node.js 18 或更高版本。之后每次调用都由 Agent 发起，按模型服务的价格计费；生成类 Skill 会要求 Agent 先征得你的同意。")}</span>
            </div>
          </div>
          {error ? <p className="multimodal-error" role="alert">{error}</p> : null}
          <footer>
            <button className="button button-secondary" type="button" disabled={busy} onClick={close}>{t("取消")}</button>
            <button className="button button-primary" type="button" disabled={busy || (!agents.length && !guide)} onClick={install}>{busy ? t("正在安装") : t("安装")}</button>
          </footer>
        </>
      )}
    </ModalDialog>
  );
}
