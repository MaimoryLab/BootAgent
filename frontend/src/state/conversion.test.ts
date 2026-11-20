import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

import { sourceTranslate, translate } from "../i18n";
import type { ProbeResponse, StatusResponse } from "../types/api";
import { agentsUsingConversion, conversionDisplay, converterProfileName, probeOffersConversion, providerSuggestsConversion, userProviders } from "./conversion";

describe("converterProfileName", () => {
  it("names every generated adapter Profile by the Agent that uses it", () => {
    expect(converterProfileName("bootagent-converter-anthropic", "stored", sourceTranslate)).toBe("兼容模式（Claude Code）");
    expect(converterProfileName("bootagent-converter-responses", "stored", sourceTranslate)).toBe("兼容模式（Codex）");
    expect(converterProfileName("bootagent-converter-chat", "stored", sourceTranslate)).toBe("兼容模式（OpenCode / Aider）");
  });

  it("supports legacy IDs and preserves unknown names", () => {
    expect(converterProfileName("bootagent_converter_responses", "stored", sourceTranslate)).toBe("兼容模式（Codex）");
    expect(converterProfileName("team-profile", "团队配置", sourceTranslate)).toBe("团队配置");
  });

  it("uses the matching English display names without changing stored labels", () => {
    const english = (key: Parameters<typeof translate>[1]) => translate("en", key);
    expect(converterProfileName("bootagent-converter-anthropic", "stored", english)).toBe("Compatibility mode (Claude Code)");
    expect(converterProfileName("bootagent-converter-responses", "stored", english)).toBe("Compatibility mode (Codex)");
  });
});

describe("probeOffersConversion", () => {
  const failed = (convertible: ProbeResponse["convertible"]): ProbeResponse => ({
    ok: false, reachable: true, status: 404, message: "unsupported", error_code: "PROTOCOL_UNSUPPORTED", retryable: false, convertible,
  });

  it("offers compatibility mode only for the protocol the backend says it can serve", () => {
    expect(probeOffersConversion(failed(["responses"]), "responses")).toBe(true);
    expect(probeOffersConversion(failed(["responses"]), "anthropic")).toBe(false);
    expect(probeOffersConversion(failed(null), "responses")).toBe(false);
  });

  it("never offers it for Chat Completions or a passing test", () => {
    expect(probeOffersConversion(failed(["openai"]), "openai")).toBe(false);
    expect(probeOffersConversion({ ...failed(["responses"]), ok: true }, "responses")).toBe(false);
    expect(probeOffersConversion(null, "responses")).toBe(false);
  });
});

describe("providerSuggestsConversion", () => {
  type Provider = StatusResponse["providers"][string];
  const provider = (protocols: Provider["protocols"]): Provider => ({ name: "P", home: "", base_url: "https://api.example.test", protocols });
  const confirmed = "implementation-supported";

  it("suggests it when the Agent's format is unconfirmed but Chat Completions is confirmed", () => {
    for (const level of ["route-present-unverified", "release-candidate-required", "not-supported"] as const) {
      expect(providerSuggestsConversion(provider({ openai: confirmed, responses: level }), "responses")).toBe(true);
      expect(providerSuggestsConversion(provider({ openai: confirmed, anthropic: level }), "anthropic")).toBe(true);
    }
    // A protocol the table does not mention has not been confirmed either.
    expect(providerSuggestsConversion(provider({ openai: confirmed }), "responses")).toBe(true);
  });

  it("stays quiet when the Agent's format is confirmed", () => {
    expect(providerSuggestsConversion(provider({ openai: confirmed, responses: confirmed }), "responses")).toBe(false);
    expect(providerSuggestsConversion(provider({ openai: confirmed, anthropic: confirmed }), "anthropic")).toBe(false);
  });

  it("stays quiet when Chat Completions, which it converts to, is not confirmed", () => {
    expect(providerSuggestsConversion(provider({ openai: "route-present-unverified", responses: "route-present-unverified" }), "responses")).toBe(false);
  });

  it("stays quiet for an Agent that already speaks Chat Completions", () => {
    expect(providerSuggestsConversion(provider({ openai: confirmed }), "openai")).toBe(false);
    expect(providerSuggestsConversion(provider({ openai: confirmed }), undefined)).toBe(false);
  });

  it("stays quiet for a Provider with no table, such as a custom one", () => {
    expect(providerSuggestsConversion(provider(undefined), "responses")).toBe(false);
    expect(providerSuggestsConversion(provider(null), "responses")).toBe(false);
    expect(providerSuggestsConversion(undefined, "responses")).toBe(false);
  });

  // The fixtures above are invented. This reads the table that ships, so the
  // pairing a first-time user actually meets is what is asserted: the default
  // Provider with Codex gets the suggestion, a Provider that has confirmed
  // Responses does not.
  it("agrees with the shipped protocol table", () => {
    const manifestPath = join(dirname(fileURLToPath(import.meta.url)), "../../../manifests/providers.lock.json");
    const manifest = JSON.parse(readFileSync(manifestPath, "utf8")) as { providers: Record<string, Provider & { order: number }> };
    const suggested = (id: string, protocol: string) => providerSuggestsConversion(manifest.providers[id], protocol);
    const first = Object.entries(manifest.providers).sort(([, a], [, b]) => a.order - b.order)[0][0];

    expect(first).toBe("jiekou");
    expect(suggested("jiekou", "responses")).toBe(true);
    expect(suggested("deepseek", "responses")).toBe(false);
    // Claude Code's format is confirmed on both, so neither gets a suggestion.
    expect(suggested("jiekou", "anthropic")).toBe(false);
    expect(suggested("deepseek", "anthropic")).toBe(false);
  });
});

describe("compatibility mode status helpers", () => {
  const ppio = { id: "ppio-chat", label: "PPIO Chat", provider: "ppio", model: "deepseek-v3", protocol: "openai" };
  const other = { id: "other-chat", label: "Other Chat", provider: "other", model: "glm-5", protocol: "openai" };
  const route = { id: "bootagent-converter-for-codex", label: "BootAgent codex", provider: "bootagent-converter-for-codex", model: "gpt-5.6-sol", protocol: "responses" };
  const status = {
    providers: {
      ppio: { name: "PPIO" },
      "bootagent-converter-responses": { name: "BootAgent Converter responses" },
    },
    catalog: [{ id: "codex", name: "Codex" }, { id: "claude-code", name: "Claude Code" }],
    agents: {
      codex: { profileId: "bootagent-converter-for-codex" },
      "claude-code": { profileId: "team-ppio" },
    },
    desktopAgents: [{ id: "claude-desktop", name: "Claude Desktop", profileId: "bootagent-converter-for-claude-desktop" }],
    profiles: [ppio, other, route],
    conversionRoutes: { codex: "ppio-chat", "claude-desktop": "other-chat" },
  } as unknown as StatusResponse;

  it("hides the adapter's own Providers from pickers", () => {
    expect(Object.keys(userProviders(status.providers))).toEqual(["ppio"]);
  });

  it("lists every Agent pointed at the adapter with the model service it really uses", () => {
    expect(agentsUsingConversion(status).map((user) => [user.name, user.target?.id])).toEqual([
      ["Codex", "ppio-chat"],
      ["Claude Desktop", "other-chat"],
    ]);
    expect(agentsUsingConversion(null)).toEqual([]);
  });

  it("names a converter-bound Agent after its own target", () => {
    expect(conversionDisplay(status, "codex", route as never, sourceTranslate)).toEqual({ profile: ppio, name: "PPIO Chat（兼容模式）" });
    // A route whose target was deleted still says it is compatibility mode.
    expect(conversionDisplay({ ...status, conversionRoutes: {} }, "codex", route as never, sourceTranslate).name).toBe("兼容模式");
    expect(conversionDisplay(status, "claude-code", ppio as never, sourceTranslate)).toEqual({ profile: ppio, name: "PPIO Chat" });
  });
});
