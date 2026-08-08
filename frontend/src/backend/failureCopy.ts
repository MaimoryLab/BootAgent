import type { Translate } from "../i18n";

/**
 * Localised copy for backend failures.
 *
 * Every Go message reaching the UI is English, and describeError passed it
 * through verbatim: normalizeWailsError wraps each backend failure in
 * OneAgentApiError, so the Chinese `t()` fallbacks callers supplied almost never
 * fired. This maps the stable error contract -- `error_code` from
 * internal/errors, plus the HTTP `status` a probe carries -- onto copy the i18n
 * table owns.
 *
 * Deliberately not string matching on the English message: that would break the
 * moment Go rewords anything, and the codes exist precisely so the UI does not
 * have to read prose. Where a code is too coarse to be useful on its own (an
 * HTTP status behind PROVIDER_UNREACHABLE covers 402, 429 and 500 alike) the
 * status refines it.
 */

export interface FailureCopy {
  /** The sentence to show in place of the backend message. */
  message: string;
  /** One line of what to do about it, when there is something to do. */
  hint?: string;
}

/**
 * True when `status` came from an actual HTTP response.
 *
 * A transport failure reports status 0 (`unsupportedStatus`,
 * internal/provider/client.go:21), so zero means the request never landed rather
 * than naming a response.
 */
export const isHTTPStatus = (status: number | undefined): boolean =>
  typeof status === "number" && status > 0;

/**
 * Copy for an HTTP status a Provider returned.
 *
 * 429 and 402 are the two a new user hits most -- rate limit and exhausted
 * credit -- and both arrived as a bare "Endpoint returned HTTP 429." with no
 * indication of which, or that the account rather than the config is the issue.
 */
function httpStatusCopy(status: number, t: Translate): FailureCopy | null {
  if (status === 401 || status === 403) {
    return { message: t("API Key 被拒绝"), hint: t("确认 Key 已完整粘贴，或到模型服务页面重新填写") };
  }
  if (status === 402) {
    return { message: t("账户额度不足"), hint: t("到模型服务官网充值或查看账户额度") };
  }
  if (status === 429) {
    return { message: t("请求过于频繁，或已达到额度上限"), hint: t("稍等一会儿再试，或到模型服务官网查看额度") };
  }
  if (status === 404 || status === 405) {
    return { message: t("端点不提供这个接口"), hint: t("确认 Base URL 是否正确，以及这个模型服务是否支持所选 API 类型") };
  }
  if (status >= 500) {
    return { message: t("模型服务暂时不可用（HTTP {status}）", { status }), hint: t("这是对方服务的问题，稍后重试") };
  }
  if (status > 0) return { message: t("模型服务返回了 HTTP {status}", { status }) };
  return null;
}

/**
 * Copy for an error code, with an optional HTTP status to refine it.
 *
 * Returns null when the code carries no more information than the backend
 * message already did -- INVALID_REQUEST is a validation message written for the
 * field it came from, and replacing it with a generic sentence would lose detail
 * the user needs.
 */
export function failureCopyFor(code: string | null | undefined, status: number | undefined, t: Translate): FailureCopy | null {
  switch (code) {
    case "API_KEY_REJECTED":
      return { message: t("API Key 被拒绝"), hint: t("确认 Key 已完整粘贴，或到模型服务页面重新填写") };
    case "PROVIDER_UNREACHABLE": {
      // A real status means the endpoint answered, so it is reachable and the
      // status is the story. Status 0 means the request never landed.
      const byStatus = isHTTPStatus(status) ? httpStatusCopy(status as number, t) : null;
      if (byStatus) return byStatus;
      return { message: t("无法连接到模型服务"), hint: t("检查网络连接和 Base URL 是否正确") };
    }
    case "TIMEOUT":
      // Distinguished from unreachable, which the backend has always tracked as a
      // separate code while producing the identical "Cannot reach endpoint"
      // sentence for both.
      return { message: t("连接模型服务超时"), hint: t("检查网络连接，或稍后重试") };
    case "PROTOCOL_UNSUPPORTED":
      return { message: t("这个模型不支持所选的 API 类型"), hint: t("换一个支持该 API 类型的模型，或改用其他 API 类型") };
    case "MODELS_UNSUPPORTED":
      return { message: t("无法获取模型列表"), hint: t("这个端点不提供模型列表，可以直接手动输入模型 ID") };
    case "CONFIG_WRITE_FAILED":
      return { message: t("无法写入配置文件"), hint: t("确认配置文件没有被其他程序占用，以及你对它有写入权限") };
    default:
      return null;
  }
}

/**
 * Hint for a failed runtime download.
 *
 * Separate from failureCopyFor because AGENT_INSTALL_FAILED covers npm exits and
 * installer launches too, and this advice only holds for the download path -- the
 * caller knows which it is, the code does not.
 *
 * What it has to say is counter-intuitive: `downloadSources`
 * (internal/install/bootstrap.go:364-372) always tries both the official source
 * and the mirror, and the preference only reorders them. So by the time this
 * message appears both hosts have already failed, and toggling the mirror setting
 * -- the one control a user would go looking for -- cannot change the outcome.
 * Saying nothing left them to find that out by trying.
 */
export const runtimeDownloadHint = (t: Translate): string =>
  t("官方源和镜像都已尝试过，切换下载源不会改变结果。请检查网络连接后重试");
