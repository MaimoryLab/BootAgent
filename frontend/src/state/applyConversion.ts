import { api } from "../backend/api";
import type { Translate } from "../i18n";
import type { ProfileSummary, StatusResponse } from "../types/api";
import { canConvertProtocol, isConverterID } from "./conversion";
import { confirmAction } from "./confirmDelete";
import { desktopApps } from "./desktopSetup";

/**
 * The Agents among `agentIds` that compatibility mode can be switched on for:
 * command-line Agents speaking Anthropic Messages or OpenAI Responses, and
 * Claude Desktop. Other desktop apps share another Agent's settings, so they
 * follow it rather than being pointed at the adapter on their own.
 */
export function conversionTargets(status: StatusResponse, agentIds: string[]): string[] {
  return agentIds.filter((agentId) => {
    if (desktopApps(status).some((app) => app.id === agentId)) return agentId === "claude-desktop";
    const agent = status.catalog.find((item) => item.id === agentId);
    return Boolean(agent && agent.configMode === "auto" && canConvertProtocol(agent.protocol));
  });
}

/**
 * Asks before compatibility mode is switched on for an Agent: it sends two
 * small checks, which may be billed. Each Agent has its own route, so nothing
 * else changes and there is nothing more to warn about.
 */
export async function confirmConversion(t: Translate): Promise<boolean> {
  return confirmAction({
    title: t("使用兼容模式"),
    message: t("BootAgent 会在本机开启兼容模式，并先发送两次测试请求，可能产生少量费用。"),
    confirmLabel: t("开启并应用"),
    cancelLabel: t("取消"),
  });
}

/**
 * The Chat Completions Profile compatibility mode forwards to for `source`:
 * the source itself when it already is one, an existing Profile with the same
 * Provider and model, or a new one saved from it.
 */
async function chatProfileFor(status: StatusResponse, source: ProfileSummary, t: Translate): Promise<string> {
  if (source.protocol === "openai") return source.id;
  const existing = status.profiles.find((profile) =>
    !isConverterID(profile.id) && profile.protocol === "openai" && profile.provider === source.provider && profile.model === source.model,
  );
  if (existing) return existing.id;
  const ids = new Set(status.profiles.map((profile) => profile.id));
  const base = `${source.id}-compat`.slice(0, 64);
  let id = base;
  for (let suffix = 2; ids.has(id); suffix += 1) id = `${base.slice(0, 60)}-${suffix}`;
  const { profile } = await api.saveProfile({
    id,
    label: t("{name}（兼容模式）", { name: source.label || source.id }),
    provider: source.provider,
    apiBaseUrl: "",
    apiKey: "",
    model: source.model || "",
    reasoningEffort: source.reasoningEffort || "",
    context1M: Boolean(source.context1M),
    configMode: "provider",
    protocol: "openai",
  });
  return profile.id;
}

/**
 * Points each Agent at the local adapter, forwarding to `source`'s Provider and
 * model over Chat Completions. Stops at the first failure and throws it; the
 * backend has already restored that Agent.
 */
export async function applyConversion(status: StatusResponse, agentIds: string[], source: ProfileSummary, t: Translate): Promise<void> {
  const target = await chatProfileFor(status, source, t);
  for (const agentId of agentIds) {
    if (agentId === "claude-desktop") await api.configureDesktopAgentWithConversion(agentId, target);
    else await api.activateAgentWithConversion(agentId, target);
  }
}
