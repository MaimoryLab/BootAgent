import { describe, expect, it } from "vitest";

import { anthropicClientBaseURL } from "./providerUrl";

describe("anthropicClientBaseURL", () => {
  it.each([
    ["https://api.example.test/anthropic", "https://api.example.test/anthropic"],
    ["https://api.example.test/anthropic/v1", "https://api.example.test/anthropic"],
    ["https://api.example.test/anthropic/messages", "https://api.example.test/anthropic"],
    ["https://api.example.test/anthropic/v1/messages/", "https://api.example.test/anthropic"],
  ])("normalizes %s for an Anthropic client", (input, expected) => {
    expect(anthropicClientBaseURL(input)).toBe(expected);
  });
});
