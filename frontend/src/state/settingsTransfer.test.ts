import { describe, expect, it } from "vitest";

import { makeTransfer, parseTransfer } from "./settingsTransfer";

describe("settings transfer", () => {
  it("round-trips encrypted provider keys", async () => {
    const providers = [{ id: "demo", name: "Demo", home: "", base_url: "https://example.test", anthropic_base_url: "", api_key: "secret", built_in: false }];
    const file = await makeTransfer([], providers, true, "password");
    expect(file.encrypted).toBeTruthy();
    expect(await parseTransfer(JSON.stringify(file), "password")).toMatchObject({ providers });
    await expect(parseTransfer(JSON.stringify(file), "wrong")).rejects.toThrow();
  });
});
