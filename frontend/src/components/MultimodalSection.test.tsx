import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MultimodalDetection, MultimodalProvider } from "../types/api";

const detectMultimodal = vi.fn<() => Promise<MultimodalDetection>>();
const probeMultimodal = vi.fn();

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("../backend/api", () => ({
  api: {
    detectMultimodal: () => detectMultimodal(),
    probeMultimodal: (...args: unknown[]) => probeMultimodal(...args),
  },
  describeError: (error: unknown, fallback: string) => ({ message: error instanceof Error ? error.message : fallback }),
}));

import { I18nProvider, sourceTranslate } from "../i18n";
import { describeProbe, groupByCapability, installLabel, MultimodalSection } from "./MultimodalSection";

const gateway: MultimodalProvider = {
  id: "novita",
  name: "Novita",
  listed: true,
  models: [
    {
      id: "moonshotai/kimi-k3",
      capabilities: [
        { id: "vision", source: "id-rule", adapter: "openai-chat-vision", supported: true, installable: false, probe: "minimal" },
        { id: "video-understanding", source: "id-rule", adapter: "openai-chat-video", supported: true, installable: false, probe: "" },
      ],
    },
    {
      id: "qwen/qwen3-vl-8b",
      capabilities: [{ id: "vision", source: "declared", adapter: "openai-chat-vision", supported: true, installable: true, probe: "minimal" }],
    },
  ],
};

function renderSection() {
  return render(<MemoryRouter><I18nProvider><MultimodalSection /></I18nProvider></MemoryRouter>);
}

beforeEach(() => {
  detectMultimodal.mockReset();
  probeMultimodal.mockReset();
  localStorage.setItem("bootagent.locale", "zh-CN");
});

describe("groupByCapability", () => {
  it("orders capabilities canonically and puts installable, better-evidenced models first", () => {
    const groups = groupByCapability(gateway);
    expect(groups.map((group) => group.id)).toEqual(["vision", "video-understanding"]);
    expect(groups[0].options.map((option) => option.model)).toEqual(["qwen/qwen3-vl-8b", "moonshotai/kimi-k3"]);
  });
});

describe("installLabel", () => {
  it("never calls a guess installable, and says a probe would settle it", () => {
    expect(installLabel(gateway.models[0].capabilities[0], sourceTranslate).text).toBe("实测通过后可安装");
    expect(installLabel(gateway.models[0].capabilities[1], sourceTranslate).text).toBe("暂不可安装");
    expect(installLabel({ ...gateway.models[0].capabilities[0], source: "probe", supported: false }, sourceTranslate).text).toBe("不可安装");
  });
});

describe("describeProbe", () => {
  it("words an unreachable provider as unverified, not as unsupported", () => {
    const parts = describeProbe({ providerId: "novita", model: "m", capability: "vision", status: "unverified", reason: "network" }, sourceTranslate);
    expect(parts.ok).toBe(false);
    expect(parts.title).toBe("无法验证「m」的图片理解");
    expect(parts.reason).toContain("模型本身可能没有问题");
  });

  it("names a refusal with its status and the provider's words", () => {
    const parts = describeProbe({ providerId: "novita", model: "m", capability: "vision", status: "unavailable", reason: "refused", httpStatus: 400, detail: "no images" }, sourceTranslate);
    expect(parts.title).toBe("「m」不支持图片理解");
    expect(parts.reason).toContain("HTTP 400");
    expect(parts.detail).toContain("no images");
  });
});

describe("MultimodalSection", () => {
  it("lists on open and sends no probe until the button is pressed", async () => {
    detectMultimodal.mockResolvedValue({ providers: [gateway] });
    renderSection();
    expect(await screen.findByText("Novita")).toBeTruthy();
    expect(detectMultimodal).toHaveBeenCalledTimes(1);
    expect(probeMultimodal).not.toHaveBeenCalled();
    // The probe button warns of cost before it is pressed.
    const button = screen.getAllByRole("button", { name: /实测/ })[0];
    expect(button.getAttribute("title")).toContain("可能产生少量费用");
  });

  it("probes the selected model and shows the verdict", async () => {
    detectMultimodal.mockResolvedValue({ providers: [gateway] });
    probeMultimodal.mockResolvedValue({ providerId: "novita", model: "moonshotai/kimi-k3", capability: "vision", status: "available" });
    renderSection();
    const select = await screen.findByLabelText("图片理解");
    fireEvent.change(select, { target: { value: "moonshotai/kimi-k3" } });
    expect(screen.getByText("实测通过后可安装")).toBeTruthy();
    fireEvent.click(screen.getAllByRole("button", { name: /实测/ })[0]);
    await waitFor(() => expect(probeMultimodal).toHaveBeenCalledWith("novita", "moonshotai/kimi-k3", "vision"));
    expect(await screen.findByText("「moonshotai/kimi-k3」支持图片理解")).toBeTruthy();
    // The cached verdict is re-read rather than patched in locally.
    await waitFor(() => expect(detectMultimodal).toHaveBeenCalledTimes(2));
  });

  it("offers no probe for a capability with no live check", async () => {
    detectMultimodal.mockResolvedValue({ providers: [{ ...gateway, models: [gateway.models[0]] }] });
    renderSection();
    await screen.findByText("Novita");
    // One button for vision; none for video understanding.
    expect(screen.getAllByRole("button", { name: /^实测$/ })).toHaveLength(1);
  });

  it("points to the Providers page when no key is saved", async () => {
    detectMultimodal.mockResolvedValue({ providers: [] });
    renderSection();
    expect(await screen.findByText("去添加模型服务")).toBeTruthy();
  });

  it("reports a failed listing without hiding the Provider", async () => {
    detectMultimodal.mockResolvedValue({ providers: [{ id: "deepseek", name: "DeepSeek", listed: false, message: "API key was rejected (401).", errorCode: "API_KEY_REJECTED", models: [] }] });
    renderSection();
    expect(await screen.findByText(/无法读取模型列表/)).toBeTruthy();
  });
});
