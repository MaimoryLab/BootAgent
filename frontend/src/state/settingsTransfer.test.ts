import { describe, expect, it } from "vitest";

import { makeTransfer, parseTransfer } from "./settingsTransfer";

describe("settings transfer", () => {
  it("round-trips encrypted provider keys", async () => {
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "secret", built_in: false }];
    const file = await makeTransfer([], providers, "encrypted", "password");
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

  // The default: a transfer file describes which Providers and Profiles exist,
  // and carrying live credentials is what turns it into a secret. Exporting one
  // should not hand the recipient a working key unless that was asked for.
  it("omits API keys unless the caller asks for them", async () => {
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "secret", built_in: false }];
    const file = await makeTransfer([], providers);
    expect(file.encrypted).toEqual([]);
    // No apikey property at all, not an empty one: a reader has to be able to
    // tell "this file has no keys" from "this key was blank".
    expect(Object.hasOwn(file.providers[0], "apikey")).toBe(false);
    expect(JSON.stringify(file)).not.toContain("secret");
  });

  it("reports whether an incoming file supplied each key", async () => {
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "secret", built_in: false }];
    const without = await parseTransfer(JSON.stringify(await makeTransfer([], providers)));
    // carriesKey false is what tells the caller to keep the saved credential
    // rather than writing this empty one over it.
    expect(without.providers[0].carriesKey).toBe(false);
    expect(without.providers[0].api_key).toBe("");

    const withPlain = await parseTransfer(JSON.stringify(await makeTransfer([], providers, "plain")));
    expect(withPlain.providers[0].carriesKey).toBe(true);
    expect(withPlain.providers[0].api_key).toBe("secret");
  });

  it("treats a deliberately blank key as supplied", async () => {
    // An empty string was written on purpose by a plain-text export of a Provider
    // with no key; that is a value, not an absence.
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "", built_in: false }];
    const parsed = await parseTransfer(JSON.stringify(await makeTransfer([], providers, "plain")));
    expect(parsed.providers[0].carriesKey).toBe(true);
  });
});
