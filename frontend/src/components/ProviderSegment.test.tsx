import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import type { StatusResponse } from "../types/api";
import { ProviderSegment, providerServes } from "./ProviderSegment";

const providers = {
  both: { name: "Both", home: "", base_url: "https://both.example/v1", anthropic_base_url: "https://both.example/anthropic", has_key: true },
  "chat-only": { name: "Chat only", home: "", base_url: "https://chat.example/v1", has_key: true },
  "bootagent-converter-anthropic": { name: "BootAgent Converter anthropic", home: "", base_url: "http://127.0.0.1:8787/v1", anthropic_base_url: "http://127.0.0.1:8787", has_key: true },
} as unknown as StatusResponse["providers"];

function open(protocol: "anthropic" | "responses" | "", convertible = false) {
  render(<ProviderSegment value="both" providers={providers} onAdd={vi.fn()} onChange={vi.fn()} protocol={protocol} convertible={convertible} />);
  fireEvent.click(screen.getByRole("combobox", { name: "模型服务" }));
  return screen.getAllByRole("option");
}

describe("ProviderSegment", () => {
  it("never offers the adapter's own Providers", () => {
    const options = open("");
    expect(options.map((option) => option.textContent)).not.toContain("BootAgent Converter anthropic");
  });

  it("says why a Provider cannot serve the step instead of only greying it out", () => {
    const chatOnly = open("anthropic").find((option) => option.textContent?.startsWith("Chat only"));
    expect(chatOnly?.textContent).toBe("Chat only（不支持此格式）");
    expect(chatOnly).toHaveAttribute("aria-disabled", "true");
  });

  it("lets a step that can use compatibility mode pick a Chat Completions-only Provider", () => {
    const chatOnly = open("anthropic", true).find((option) => option.textContent?.startsWith("Chat only"));
    expect(chatOnly?.textContent).toBe("Chat only");
    expect(chatOnly).not.toHaveAttribute("aria-disabled");
  });

  it("matches each protocol to the endpoint it is sent to", () => {
    expect(providerServes(providers.both, "anthropic")).toBe(true);
    expect(providerServes(providers["chat-only"], "anthropic")).toBe(false);
    expect(providerServes(providers["chat-only"], "anthropic", true)).toBe(true);
    expect(providerServes(providers["chat-only"], "responses")).toBe(true);
  });
});
