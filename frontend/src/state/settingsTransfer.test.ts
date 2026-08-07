import { describe, expect, it } from "vitest";

import { makeTransfer, parseTransfer } from "./settingsTransfer";

describe("settings transfer", () => {
  it("round-trips encrypted provider keys", async () => {
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "secret", built_in: false }];
    const file = await makeTransfer([], providers, true, "password");
    expect(file.encrypted).toHaveLength(1);
    expect(file.providers[0]).toMatchObject({ key_encrypted: 0 });
    expect(file.profiles).toEqual([]);
    expect(await parseTransfer(JSON.stringify(file), "password")).toMatchObject({ providers });
    expect(file.timestamp).toBeTruthy();
    await expect(parseTransfer(JSON.stringify(file), "wrong")).rejects.toThrow();
  });

  it("drops legacy profile endpoint and local key fields", async () => {
    const parsed = await parseTransfer(JSON.stringify({
      version: 1,
      timestamp: "2026-01-01T00:00:00Z",
      providers: [],
      profiles: [{ id: "team", label: "Team", provider: "demo", model: "m", protocol: "openai", base_url: "https://old", api_key: "legacy", hasKey: true }],
      encrypted: [],
    }));
    expect(parsed.profiles[0]).toEqual({ id: "team", label: "Team", provider: "demo", model: "m", protocol: "openai" });
  });
});
