import { Activity, AlertTriangle, CheckCircle2 } from "lucide-react";
import { useNavigate } from "react-router-dom";

import type { Translate } from "../i18n";
import { useI18n } from "../i18n";
import type { ProfileModelCheck } from "../types/api";

const protocolLabels: Record<string, string> = {
  anthropic: "Anthropic Messages",
  openai: "OpenAI Chat Completions",
  responses: "OpenAI Responses",
};

/**
 * The headline and explanation for a model check, without the actions.
 *
 * "unverified" is worded as "could not verify", never as "not available": a
 * network failure is no evidence against the model, and telling the user it
 * is broken would send them to abandon a model that works.
 */
export function describeModelCheck(check: ProfileModelCheck, t: Translate, nameOf: (agentId: string) => string, separator: string) {
  const agents = (check.agents ?? []).map(nameOf).join(separator);
  const protocol = protocolLabels[check.protocol ?? ""] ?? check.protocol ?? "";
  if (check.status === "available") {
    return {
      title: t("模型「{model}」可用", { model: check.model }),
      reason: protocol ? t("模型服务已通过 {protocol} 正常响应。", { protocol }) : t("模型服务已正常响应。"),
      agents: agents ? t("已检测的 Agent：{agents}", { agents }) : "",
      detail: "",
      hint: "",
    };
  }
  const title = check.status === "unverified"
    ? t("无法验证模型「{model}」", { model: check.model })
    : t("模型「{model}」不可用", { model: check.model });
  let reason: string;
  switch (check.reason) {
    case "refused":
      reason = check.httpStatus
        ? t("模型服务拒绝了该模型（{protocol}，HTTP {status}）。", { protocol, status: check.httpStatus })
        : t("模型服务拒绝了该模型（{protocol}）。", { protocol });
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
  return {
    title,
    reason,
    agents: agents ? t("受影响的 Agent：{agents}", { agents }) : "",
    detail: check.detail ? t("模型服务返回：{detail}", { detail: check.detail }) : "",
    hint: check.reason === "refused" && check.protocol === "responses"
      ? t("Codex 使用 Responses API。请选择支持 Responses 的模型；如果模型服务仅支持 Chat Completions，可使用 API 协议适配，并应用适配器生成的 Codex 配置模版。")
      : "",
  };
}

interface ModelCheckButtonProps {
  checking: boolean;
  disabled?: boolean;
  onCheck: () => void;
  /** Names the Profile for screen readers where several buttons share a page. */
  ariaLabel?: string;
}

/**
 * The only way a model check starts. Saving and applying a Profile never send
 * one on their own: each is a billed request to the provider, so the user
 * decides when it goes out, and the button says so before they press it.
 */
export function ModelCheckButton({ checking, disabled, onCheck, ariaLabel }: ModelCheckButtonProps) {
  const { t } = useI18n();
  return (
    <button
      className="button button-secondary model-check-button"
      type="button"
      disabled={disabled || checking}
      // Keeps focus in a model field being edited: its blur would start a save
      // that disables this button before the click lands. The check saves the
      // typed model itself.
      onMouseDown={(event) => event.preventDefault()}
      onClick={onCheck}
      aria-label={ariaLabel}
      title={t("向模型服务发送一次真实请求，确认 Agent 能调用这个模型。可能产生少量费用。")}
    >
      <Activity size={14} aria-hidden="true" />
      {checking ? t("正在检测") : t("检测模型")}
    </button>
  );
}

interface ModelCheckNoticeProps {
  check: ProfileModelCheck;
  /** Provider the Profile uses, for the key remedies' link. */
  providerId: string;
  nameOf: (agentId: string) => string;
  /** Where the Provider editor returns to once the key is fixed. */
  returnTo: string;
  /** Names the Profile when the page lists several. */
  profileName?: string;
  busy?: boolean;
  /** Saves the Profile back to check.previousModel. Omitted where no save path exists. */
  onRevert?: () => void;
  onRecheck?: () => void;
  onDismiss: () => void;
}

/**
 * The result of a check the user asked for. A failure leaves a next step, not
 * just a problem: going back to the model the user had before, fixing the key
 * that was refused, or checking again once the network is back. Which remedy
 * is offered follows from why the check failed -- going back to the old model
 * cannot fix a rejected key.
 */
export function ModelCheckNotice({ check, providerId, nameOf, returnTo, profileName, busy, onRevert, onRecheck, onDismiss }: ModelCheckNoticeProps) {
  const { t, locale } = useI18n();
  const navigate = useNavigate();
  const parts = describeModelCheck(check, t, nameOf, locale === "en" ? ", " : "、");
  const ok = check.status === "available";
  const keyProblem = check.reason === "key" || check.reason === "no_key";
  const canRevert = Boolean(!ok && onRevert && check.previousModel && check.previousModel !== check.model && !keyProblem);
  const editKey = () => navigate(`/providers?provider=${encodeURIComponent(providerId)}&returnTo=${encodeURIComponent(returnTo)}`);
  const Icon = ok ? CheckCircle2 : AlertTriangle;
  // The live region holds the message only. Buttons inside it would be read out
  // with it. A failure is assertive; a pass only needs to be announced politely.
  return (
    <div className={`model-check-notice${ok ? " is-ok" : ""}`}>
      <Icon size={16} aria-hidden="true" />
      <div className="model-check-body">
        <div className="model-check-body" role={ok ? "status" : "alert"}>
          <strong>{profileName ? `${profileName} · ${parts.title}` : parts.title}</strong>
          <span>{parts.reason}</span>
          {parts.agents ? <span>{parts.agents}</span> : null}
          {parts.detail ? <span className="model-check-detail">{parts.detail}</span> : null}
          {parts.hint ? <span>{parts.hint}</span> : null}
        </div>
        <div className="model-check-actions">
          {parts.hint ? (
            <button className="button button-secondary" type="button" disabled={busy} onClick={() => navigate("/conversion")}>
              {t("打开 API 协议适配")}
            </button>
          ) : null}
          {canRevert ? (
            <button className="button button-secondary" type="button" disabled={busy} onClick={onRevert}>
              {t("改回 {model}", { model: check.previousModel ?? "" })}
            </button>
          ) : null}
          {!ok && keyProblem ? (
            <button className="button button-secondary" type="button" disabled={busy} onClick={editKey}>
              {check.reason === "no_key" ? t("为模型服务添加 API Key") : t("检查模型服务的 API Key")}
            </button>
          ) : null}
          {!ok && onRecheck ? (
            <button className="button button-secondary" type="button" disabled={busy} onClick={onRecheck}>
              {busy ? t("正在检测") : t("重新检测")}
            </button>
          ) : null}
          <button className="button button-secondary" type="button" disabled={busy} onClick={onDismiss}>{ok ? t("关闭") : t("忽略")}</button>
        </div>
      </div>
    </div>
  );
}
