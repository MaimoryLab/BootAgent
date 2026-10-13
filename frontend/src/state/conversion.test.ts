import { describe, expect, it } from "vitest";

import { sourceTranslate, translate } from "../i18n";
import type { ProbeResponse, StatusResponse } from "../types/api";
import { agentsUsingConversion, conversionDisplay, converterProfileName, probeOffersConversion, userProviders } from "./conversion";

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
