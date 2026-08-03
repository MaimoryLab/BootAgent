import { describe, expect, it } from "vitest";

import {
  compatibilityFor,
  parseExplorerSearch,
  recommendedCombination,
  serializeExplorerSearch,
  type SiteCatalogV2,
} from "./explorer";

const catalog: SiteCatalogV2 = {
  schema_version: 2,
  groups: [{ id: "auto", name: "Auto" }, { id: "ide", name: "IDE" }],
  agents: [
    {
      id: "responses-agent",
      name: "Responses Agent",
      group: "auto",
      rank: 1,
      command: "responses-agent",
      configPath: ".responses/config.json",
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
      id: "anthropic-agent",
      name: "Anthropic Agent",
      group: "auto",
      rank: 2,
      command: "anthropic-agent",
      configPath: ".anthropic/config.json",
      platforms: ["macos", "linux"],
      lockedVersion: "2.0.0",
      source: null,
      license: null,
      licenseUrl: null,
      guide: null,
      protocol: "anthropic",
      support: { managedInstall: true, officialInstallGuide: false, managedConfig: true },
    },
    {
      id: "guided-agent",
      name: "Guided Agent",
      group: "ide",
      rank: 3,
      command: null,
      configPath: null,
      platforms: ["windows"],
      lockedVersion: null,
      source: null,
      license: null,
      licenseUrl: null,
      guide: "Use the official account flow.",
      protocol: null,
      support: { managedInstall: false, officialInstallGuide: true, managedConfig: false },
    },
  ],
  providers: [
    {
      id: "first",
      name: "First Provider",
      home: "https://first.example/",
      relationship: "none",
      disclosure: "",
      referralUrl: "",
      order: 1,
      protocols: [
        { id: "responses", status: "release-candidate-required" },
        { id: "anthropic", status: "implementation-supported" },
      ],
    },
    {
      id: "verified",
      name: "Verified Provider",
      home: "https://verified.example/",
      relationship: "none",
      disclosure: "",
      referralUrl: "",
      order: 2,
      protocols: [{ id: "anthropic", status: "verified" }],
    },
  ],
};

describe("provider compatibility", () => {
  it("maps provider registry statuses without upgrading preview support to ready", () => {
    expect(compatibilityFor(catalog.agents[0], catalog.providers[0])).toBe("preview-gate");
    expect(compatibilityFor(catalog.agents[1], catalog.providers[0])).toBe("supported");
    expect(compatibilityFor(catalog.agents[1], catalog.providers[1])).toBe("verified");
    expect(compatibilityFor(catalog.agents[2], catalog.providers[0])).toBe("unsupported");
  });

  it("recommends the first ranked combination that may truthfully reach ready", () => {
    expect(recommendedCombination(catalog)).toEqual({ agentId: "anthropic-agent", providerId: "first" });
  });
});

describe("Explorer URL state", () => {
  it("parses a compatible, shareable selection", () => {
    expect(
      parseExplorerSearch(
        "?agent=anthropic-agent&provider=first&platform=linux&protocol=anthropic",
        catalog,
      ),
    ).toEqual({ agent: "anthropic-agent", provider: "first", platform: "linux", protocol: "anthropic" });
  });

  it("drops unknown values and combinations that contradict the selected agent", () => {
    expect(
      parseExplorerSearch(
        "?agent=responses-agent&provider=verified&platform=windows&protocol=anthropic",
        catalog,
      ),
    ).toEqual({ agent: "responses-agent", provider: null, platform: null, protocol: null });
  });

  it("serializes only active values in a stable order", () => {
    expect(
      serializeExplorerSearch({ agent: "anthropic-agent", provider: "first", platform: null, protocol: "anthropic" }),
    ).toBe("agent=anthropic-agent&provider=first&protocol=anthropic");
  });
});
