import { describe, expect, it } from "vitest";

import { CUSTOM_PROVIDER_ID, activationReducer, initialActivationState, validateDemoBaseUrl } from "./activation";
import type { SiteCatalogV2 } from "./explorer";
import { DEMO_AGENT_STATES, demoModelsFor, missingDemoAgentIds } from "./demo-environment";

const catalog: SiteCatalogV2 = {
  schema_version: 2,
  groups: [{ id: "auto", name: "Auto" }, { id: "ide", name: "IDE" }],
  agents: [
    {
      id: "managed",
      name: "Managed",
      group: "auto",
      rank: 1,
      command: "managed",
      configPath: ".managed/config",
      platforms: ["macos"],
      lockedVersion: "1.0.0",
      source: null,
      license: null,
      licenseUrl: null,
      guide: null,
      protocol: "anthropic",
      support: { managedInstall: true, officialInstallGuide: false, managedConfig: true },
    },
    {
      id: "preview",
      name: "Preview",
      group: "auto",
      rank: 2,
      command: "preview",
      configPath: ".preview/config",
      platforms: ["macos"],
      lockedVersion: "1.0.0",
      source: null,
      license: null,
      licenseUrl: null,
      guide: null,
      protocol: "responses",
      support: { managedInstall: true, officialInstallGuide: false, managedConfig: true },
    },
    {
      id: "guided",
      name: "Guided",
      group: "ide",
      rank: 3,
      command: null,
      configPath: null,
      platforms: ["macos"],
      lockedVersion: null,
      source: null,
      license: null,
      licenseUrl: null,
      guide: "Use the official flow.",
      protocol: null,
      support: { managedInstall: false, officialInstallGuide: true, managedConfig: false },
    },
  ],
  providers: [
    {
      id: "provider",
      name: "Provider",
      home: "https://provider.example/",
      relationship: "none",
      disclosure: "",
      referralUrl: "",
      order: 1,
      protocols: [
        { id: "anthropic", status: "implementation-supported" },
        { id: "responses", status: "release-candidate-required" },
      ],
    },
  ],
};

function scanned() {
  return activationReducer(
    activationReducer(initialActivationState(), { type: "start" }, catalog),
    { type: "scan-complete" },
    catalog,
  );
}

describe("activation demo state machine", () => {
  it("moves a supported managed combination to ready", () => {
    let state = scanned();
    state = activationReducer(state, { type: "select-agent", agentId: "managed" }, catalog);
    state = activationReducer(state, { type: "select-mode", mode: "provider" }, catalog);
    state = activationReducer(state, { type: "select-provider", providerId: "provider" }, catalog);
    state = activationReducer(state, { type: "verify" }, catalog);
    state = activationReducer(state, { type: "verify-complete" }, catalog);
    state = activationReducer(state, { type: "select-model", model: "deepseek/deepseek-v3" }, catalog);
    state = activationReducer(state, { type: "confirm" }, catalog);

    expect(state).toMatchObject({ phase: "ready", agentId: "managed", providerId: "provider", compatibility: "supported" });
  });

  it("never upgrades release-candidate-required support to ready", () => {
    let state = scanned();
    state = activationReducer(state, { type: "select-agent", agentId: "preview" }, catalog);
    state = activationReducer(state, { type: "select-mode", mode: "provider" }, catalog);
    state = activationReducer(state, { type: "select-provider", providerId: "provider" }, catalog);
    state = activationReducer(state, { type: "verify" }, catalog);
    state = activationReducer(state, { type: "verify-complete" }, catalog);

    expect(state.phase).toBe("preview-gate");
    // Nothing downstream may rescue a gated combination.
    expect(activationReducer(state, { type: "confirm" }, catalog).phase).toBe("preview-gate");
  });

  it("routes guide-only agents without asking for a provider", () => {
    const state = activationReducer(scanned(), { type: "select-agent", agentId: "guided" }, catalog);

    expect(state).toMatchObject({ phase: "guide-only", agentId: "guided", providerId: null });
  });

  it("resets every terminal state without retaining a selection", () => {
    const state = activationReducer(
      {
        phase: "error",
        agentId: "managed",
        configMode: "provider",
        providerId: "provider",
        customBaseUrl: "https://api.example.com",
        model: "deepseek/deepseek-v3",
        compatibility: "supported",
      },
      { type: "reset" },
      catalog,
    );

    expect(state).toEqual(initialActivationState());
  });
});

describe("demo environment fixture", () => {
  it("references only real catalog agents", async () => {
    const realCatalog = (await import("./catalog")).catalog;
    expect(missingDemoAgentIds(realCatalog, DEMO_AGENT_STATES)).toEqual([]);
  });

  it("offers a model list for every provider the real catalog publishes", async () => {
    const realCatalog = (await import("./catalog")).catalog;
    for (const provider of realCatalog.providers) {
      expect(demoModelsFor(provider.id).length, `${provider.id} has no demo models`).toBeGreaterThan(0);
    }
  });
});

// These mirror validate_base_url in oneagent/providers.py. If the kernel's rules
// change, this suite should fail — the demo claiming an endpoint is acceptable
// when the product would reject it is the failure mode worth catching.
describe("custom endpoint validation matches the kernel", () => {
  it("accepts http and https and strips trailing slashes", () => {
    expect(validateDemoBaseUrl("https://api.example.com/openai")).toEqual({
      ok: true,
      value: "https://api.example.com/openai",
    });
    expect(validateDemoBaseUrl("http://localhost:8080/v1//")).toEqual({
      ok: true,
      value: "http://localhost:8080/v1",
    });
  });

  it("rejects an empty value", () => {
    expect(validateDemoBaseUrl("")).toEqual({ ok: false, reason: "required" });
  });

  it("rejects a scheme the kernel does not allow", () => {
    for (const value of ["ftp://api.example.com", "file:///etc/passwd", "api.example.com", "://nohost"]) {
      expect(validateDemoBaseUrl(value).ok, value).toBe(false);
    }
    expect(validateDemoBaseUrl("ftp://api.example.com")).toEqual({ ok: false, reason: "scheme" });
  });

  it("rejects credentials embedded in the URL", () => {
    expect(validateDemoBaseUrl("https://user:secret@api.example.com")).toEqual({
      ok: false,
      reason: "credentials",
    });
    expect(validateDemoBaseUrl("https://user@api.example.com")).toEqual({ ok: false, reason: "credentials" });
  });

  it("rejects control characters", () => {
    // Written as escapes on purpose: a literal newline, tab or DEL in the source
    // is invisible to a reviewer and an editor may silently normalise it away.
    for (const value of [
      "https://api.example.com/\nopenai",
      "https://api.example.com/\topenai",
      "https://api.example.com/\u007f",
      "https://api.example.com/\u0000",
    ]) {
      expect(validateDemoBaseUrl(value), JSON.stringify(value)).toEqual({
        ok: false,
        reason: "control-characters",
      });
    }
  });
});

describe("configuration mode branches", () => {
  const atMode = () => activationReducer(scanned(), { type: "select-agent", agentId: "managed" }, catalog);

  it("asks how to configure before asking for a provider", () => {
    expect(atMode()).toMatchObject({ phase: "mode", agentId: "managed", configMode: null });
  });

  it("skips provider and model when an existing account is reused", () => {
    const state = activationReducer(atMode(), { type: "select-mode", mode: "existing-account" }, catalog);

    expect(state).toMatchObject({
      phase: "ready",
      configMode: "existing-account",
      providerId: null,
      model: null,
    });
  });

  it("requires a verified connection before a model can be chosen", () => {
    let state = activationReducer(atMode(), { type: "select-mode", mode: "provider" }, catalog);
    state = activationReducer(state, { type: "select-provider", providerId: "provider" }, catalog);
    state = activationReducer(state, { type: "verify" }, catalog);
    state = activationReducer(state, { type: "verify-complete" }, catalog);

    expect(state.phase).toBe("model");
    // Confirming without a model must not reach ready.
    expect(activationReducer(state, { type: "confirm" }, catalog).phase).toBe("model");

    state = activationReducer(state, { type: "select-model", model: "deepseek/deepseek-v3" }, catalog);
    expect(activationReducer(state, { type: "confirm" }, catalog)).toMatchObject({
      phase: "ready",
      model: "deepseek/deepseek-v3",
    });
  });
});

describe("custom provider path", () => {
  const atProvider = () => {
    let state = activationReducer(scanned(), { type: "select-agent", agentId: "managed" }, catalog);
    state = activationReducer(state, { type: "select-mode", mode: "provider" }, catalog);
    return activationReducer(state, { type: "select-provider", providerId: CUSTOM_PROVIDER_ID }, catalog);
  };

  it("holds at the provider step until the endpoint validates", () => {
    let state = activationReducer(atProvider(), { type: "set-custom-base-url", value: "not-a-url" }, catalog);
    expect(activationReducer(state, { type: "verify" }, catalog).phase).toBe("provider");

    state = activationReducer(state, { type: "set-custom-base-url", value: "https://api.example.com/openai" }, catalog);
    expect(activationReducer(state, { type: "verify" }, catalog).phase).toBe("verifying");
  });

  it("ignores an endpoint typed against a built-in provider", () => {
    let state = activationReducer(scanned(), { type: "select-agent", agentId: "managed" }, catalog);
    state = activationReducer(state, { type: "select-mode", mode: "provider" }, catalog);
    state = activationReducer(state, { type: "select-provider", providerId: "provider" }, catalog);
    state = activationReducer(state, { type: "set-custom-base-url", value: "https://elsewhere.example" }, catalog);

    expect(state.customBaseUrl).toBe("");
  });

  it("clears a typed endpoint when switching back to a built-in provider", () => {
    let state = activationReducer(atProvider(), { type: "set-custom-base-url", value: "https://api.example.com" }, catalog);
    state = activationReducer(state, { type: "select-provider", providerId: "provider" }, catalog);

    expect(state).toMatchObject({ providerId: "provider", customBaseUrl: "" });
  });
});
