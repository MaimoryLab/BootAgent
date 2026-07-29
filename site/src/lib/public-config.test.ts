import { describe, expect, it } from "vitest";
import { optionalEmail, optionalHttpsUrl } from "./public-config";

describe("public deployment configuration", () => {
  it("accepts absent values without inventing public contact details", () => {
    expect(optionalHttpsUrl(undefined, "support URL")).toBeNull();
    expect(optionalEmail("", "business email")).toBeNull();
  });

  it("only accepts HTTPS support links", () => {
    expect(optionalHttpsUrl("https://github.com/example/issues", "support URL")).toBe("https://github.com/example/issues");
    expect(() => optionalHttpsUrl("javascript:alert(1)", "support URL")).toThrow("must use HTTPS");
  });

  it("rejects malformed business email values", () => {
    expect(optionalEmail("business@example.com", "business email")).toBe("business@example.com");
    expect(() => optionalEmail("not-an-email", "business email")).toThrow("valid email address");
  });
});
