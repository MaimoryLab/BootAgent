import type { SiteAgent, SiteCatalogV2 } from "./explorer";

export type DemoStatus = "ready" | "attention" | "guide-only" | "not-installed";
export type DemoVersion = "locked" | "behind" | null;

export interface DemoAgentState {
  installed: boolean;
  configured: boolean;
  status: DemoStatus;
  version: DemoVersion;
  hasBackup: boolean;
}

export const DEMO_AGENT_STATES: Readonly<Record<string, DemoAgentState>> = {
  codex: { installed: true, configured: false, status: "attention", version: "behind", hasBackup: true },
  "claude-code": { installed: true, configured: true, status: "ready", version: "locked", hasBackup: true },
  cursor: { installed: true, configured: false, status: "guide-only", version: null, hasBackup: false },
  opencode: { installed: false, configured: false, status: "not-installed", version: null, hasBackup: false },
};

/* The product discovers models from the endpoint's own /v1/models. These lists
   stand in for one such response. The first entry of each is the provider's real
   fallback_probe_model from oneagent/catalog.py — including the fact that the
   same DeepSeek build is published under a different id per provider, which is
   why the product cannot share one constant. */
export const DEMO_MODELS: Readonly<Record<string, readonly string[]>> = {
  ppio: ["deepseek/deepseek-v3", "qwen/qwen3-coder-480b", "moonshotai/kimi-k2"],
  novita: ["deepseek/deepseek_v3", "qwen/qwen3-235b-a22b", "meta-llama/llama-4-maverick"],
};

/* A custom endpoint has no published catalog, so the demo shows the product's
   documented recovery instead of inventing a list: type the id by hand. */
export const DEMO_CUSTOM_MODEL_HINT = "deepseek/deepseek-v3";

export function demoModelsFor(providerId: string | null): readonly string[] {
  return (providerId && DEMO_MODELS[providerId]) || [];
}

export function missingDemoAgentIds(
  catalog: SiteCatalogV2,
  states: Readonly<Record<string, DemoAgentState>> = DEMO_AGENT_STATES,
): string[] {
  const ids = new Set(catalog.agents.map((agent) => agent.id));
  return Object.keys(states).filter((id) => !ids.has(id)).sort();
}

export function demoStateFor(agent: SiteAgent): DemoAgentState {
  const explicit = DEMO_AGENT_STATES[agent.id];
  if (explicit) return explicit;
  if (agent.support.officialInstallGuide) {
    return { installed: false, configured: false, status: "guide-only", version: null, hasBackup: false };
  }
  return { installed: false, configured: false, status: "not-installed", version: null, hasBackup: false };
}
