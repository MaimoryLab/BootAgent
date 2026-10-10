import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MultimodalDetection, MultimodalProvider, StatusResponse } from "../types/api";

const detectMultimodal = vi.fn<() => Promise<MultimodalDetection>>();
const probeMultimodal = vi.fn();
const installMultimodal = vi.fn();
const uninstallSkill = vi.fn();
const copyToClipboard = vi.fn((_text: string) => Promise.resolve(true));

vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(), Off: vi.fn() } }));
vi.mock("../backend/api", () => ({
  api: {
    detectMultimodal: () => detectMultimodal(),
    probeMultimodal: (...args: unknown[]) => probeMultimodal(...args),
    installMultimodal: (...args: unknown[]) => installMultimodal(...args),
    uninstallSkill: (...args: unknown[]) => uninstallSkill(...args),
  },
  describeError: (error: unknown, fallback: string) => ({ message: error instanceof Error ? error.message : fallback }),
}));
vi.mock("../utils/clipboard", () => ({ copyToClipboard: (text: string) => copyToClipboard(text) }));

import { I18nProvider, sourceTranslate } from "../i18n";
import { accountsFor, describeProbe, groupByCapability, groupInstalled, installLabel, MultimodalSection, orderAccounts } from "./MultimodalSection";
import { EXAMPLE_PROMPTS } from "./multimodalPrompts";

const GENERATABLE = ["openai-chat-vision", "openai-images", "openai-audio-transcriptions", "openai-audio-speech", "frames-chat-vision"];

const gateway: MultimodalProvider = {
  id: "novita",
  name: "Novita",
  keyEnv: "BOOTAGENT_NOVITA_API_KEY",
  keyFile: "/Users/me/.bootagent/skill-env/novita.env",
  unsupported: [],
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

function detection(partial: Partial<MultimodalDetection> = {}): MultimodalDetection {
  return {
    providers: [gateway],
    installed: [],
    targets: ["claude-code", "codex"],
    generatable: GENERATABLE,
    manual: [
      { capability: "image-generation", adapter: "openai-images", probe: "confirm" },
      { capability: "speech-to-text", adapter: "openai-audio-transcriptions", probe: "minimal" },
    ],
    fallback: [],
    ffmpeg: true,
    ...partial,
  };
}

const labels = { "claude-code": "Claude Code", codex: "Codex" };

function renderSection(onSkillsChanged = vi.fn(), providers?: StatusResponse["providers"]) {
  render(<MemoryRouter><I18nProvider><MultimodalSection agentLabels={labels} providers={providers} onSkillsChanged={onSkillsChanged} /></I18nProvider></MemoryRouter>);
  return onSkillsChanged;
}

const builtIn = (name: string, order: number) => ({ name, home: "https://example.com", base_url: "https://example.com/v1", order, has_key: true });
const custom = (name: string, created: string) => ({ name, home: "", base_url: "https://gateway.example/v1", custom: true, has_key: true, created_at: created });
const visionOnly = (id: string, name: string): MultimodalProvider => ({ ...gateway, id, name, models: [gateway.models[1]] });

beforeEach(() => {
  for (const mock of [detectMultimodal, probeMultimodal, installMultimodal, uninstallSkill, copyToClipboard]) mock.mockClear();
  detectMultimodal.mockReset();
  localStorage.setItem("bootagent.locale", "zh-CN");
});

describe("groupByCapability", () => {
  it("orders capabilities canonically and puts installable, better-evidenced models first", () => {
    const groups = groupByCapability(gateway);
    expect(groups.map((group) => group.id)).toEqual(["vision", "video-understanding"]);
    expect(groups[0].options.map((option) => option.model)).toEqual(["qwen/qwen3-vl-8b", "moonshotai/kimi-k3"]);
  });
});

describe("orderAccounts", () => {
  it("lists the accounts the user added first, newest first, then built-ins in catalog order", () => {
    const ordered = orderAccounts(
      [visionOnly("ppio", "PPIO"), visionOnly("novita", "Novita"), visionOnly("old", "Old gateway"), visionOnly("new", "New gateway"), visionOnly("unknown", "Unknown")],
      { novita: builtIn("Novita", 3), ppio: builtIn("PPIO", 2), old: custom("Old gateway", "2026-01-01T00:00:00Z"), new: custom("New gateway", "2026-09-01T00:00:00Z") },
    );
    expect(ordered.map((entry) => entry.id)).toEqual(["new", "old", "ppio", "novita", "unknown"]);
  });

  it("leaves out BootAgent's protocol converters", () => {
    const ordered = orderAccounts(
      [visionOnly("bootagent-converter-chat", "BootAgent Converter chat"), visionOnly("ppio", "PPIO")],
      { "bootagent-converter-chat": custom("BootAgent Converter chat", "2026-10-01T00:00:00Z"), ppio: builtIn("PPIO", 2) },
    );
    expect(ordered.map((entry) => entry.id)).toEqual(["ppio"]);
  });

  it("keeps the backend order without status records", () => {
    expect(orderAccounts([visionOnly("ppio", "PPIO"), visionOnly("mine", "Mine")]).map((entry) => entry.id)).toEqual(["ppio", "mine"]);
  });

  it("only lists accounts with a model for the capability", () => {
    expect(accountsFor([gateway, { ...gateway, id: "empty", models: [] }], "vision").map((account) => account.provider.id)).toEqual(["novita"]);
    expect(accountsFor([gateway], "image-generation")).toEqual([]);
  });
});

describe("recommended models", () => {
  it("preselects the catalog's recommended model among equally usable ones", () => {
    const speech = (id: string, recommended: boolean) => ({ id, capabilities: [{ id: "text-to-speech" as const, source: "catalog" as const, adapter: "novita-audio", supported: true, installable: true, probe: "minimal" as const, recommended }] });
    const groups = groupByCapability({ ...gateway, models: [speech("minimax-speech-2.8-hd", false), speech("minimax-speech-2.8-turbo", true)] });
    expect(groups[0].options[0].model).toBe("minimax-speech-2.8-turbo");
  });
});

describe("installLabel", () => {
  it("never calls a guess installable, and says what would settle it", () => {
    const [guess, video] = gateway.models[0].capabilities;
    expect(installLabel(guess, GENERATABLE, sourceTranslate)).toMatchObject({ text: "实测通过后可安装", ready: false });
    expect(installLabel(video, GENERATABLE, sourceTranslate)).toMatchObject({ text: "暂不可安装", ready: false });
    expect(installLabel({ ...guess, source: "probe", supported: false }, GENERATABLE, sourceTranslate).text).toBe("不可安装");
    expect(installLabel(gateway.models[1].capabilities[0], GENERATABLE, sourceTranslate)).toMatchObject({ text: "可安装", ready: true });
    // Confirmed, but on a route this version cannot write a Skill for.
    expect(installLabel({ ...video, installable: true, source: "declared" }, GENERATABLE, sourceTranslate)).toMatchObject({ text: "暂不支持安装", ready: false });
  });
});

describe("describeProbe", () => {
  it("words an unreachable provider as unverified, not as unsupported", () => {
    const parts = describeProbe({ providerId: "novita", model: "m", capability: "vision", adapter: "openai-chat-vision", status: "unverified", reason: "network" }, sourceTranslate);
    expect(parts.ok).toBe(false);
    expect(parts.title).toBe("无法验证「m」的图片理解");
    expect(parts.reason).toContain("模型本身可能没有问题");
  });

  it("names a refusal with its status and the provider's words", () => {
    const parts = describeProbe({ providerId: "novita", model: "m", capability: "vision", adapter: "openai-chat-vision", status: "unavailable", reason: "refused", httpStatus: 400, detail: "no images" }, sourceTranslate);
    expect(parts.title).toBe("「m」不支持图片理解");
    expect(parts.reason).toContain("HTTP 400");
    expect(parts.detail).toContain("no images");
  });
});

describe("groupInstalled", () => {
  it("makes one card per Skill and unions its Agents", () => {
    const cards = groupInstalled([
      { skillId: "bootagent-vision", capability: "vision", providerId: "novita", model: "a", adapter: "openai-chat-vision", agents: ["codex"] },
      { skillId: "bootagent-vision", capability: "vision", providerId: "ppio", model: "b", adapter: "openai-chat-vision", agents: ["claude-code"] },
    ]);
    expect(cards).toHaveLength(1);
    expect(cards[0].agents).toEqual(["claude-code", "codex"]);
    expect(cards[0].variants).toHaveLength(2);
  });
});

describe("example prompts", () => {
  it("has bilingual prompts for every capability that can be installed", () => {
    for (const capability of ["vision", "image-generation", "speech-to-text", "text-to-speech", "video-generation"] as const) {
      const prompts = EXAMPLE_PROMPTS[capability] ?? [];
      expect(prompts.length).toBeGreaterThanOrEqual(2);
      for (const prompt of prompts) {
        expect(prompt.zh).toMatch(/[一-鿿]/);
        expect(prompt.en).not.toMatch(/[一-鿿]/);
      }
    }
  });
});

describe("MultimodalSection", () => {
  it("lists on open and sends no probe until a button is pressed", async () => {
    detectMultimodal.mockResolvedValue(detection());
    renderSection();
    expect(await screen.findByText("Novita")).toBeTruthy();
    expect(detectMultimodal).toHaveBeenCalledTimes(1);
    expect(probeMultimodal).not.toHaveBeenCalled();
    expect(installMultimodal).not.toHaveBeenCalled();
    const button = screen.getAllByRole("button", { name: /^实测$/ })[0];
    expect(button.getAttribute("title")).toContain("可能产生少量费用");
  });

  it("probes the selected model and shows the verdict", async () => {
    detectMultimodal.mockResolvedValue(detection());
    probeMultimodal.mockResolvedValue({ providerId: "novita", model: "moonshotai/kimi-k3", capability: "vision", adapter: "openai-chat-vision", status: "available" });
    renderSection();
    fireEvent.change(await screen.findByLabelText("Novita 的图片理解模型"), { target: { value: "moonshotai/kimi-k3" } });
    expect(screen.getByText("实测通过后可安装")).toBeTruthy();
    // No install button while the evidence is only a guess.
    expect(screen.queryByRole("button", { name: /^安装$/ })).toBeNull();
    fireEvent.click(screen.getAllByRole("button", { name: /^实测$/ })[0]);
    await waitFor(() => expect(probeMultimodal).toHaveBeenCalledWith("novita", "moonshotai/kimi-k3", "vision", "openai-chat-vision", false));
    expect(await screen.findByText("「moonshotai/kimi-k3」支持图片理解")).toBeTruthy();
    await waitFor(() => expect(detectMultimodal).toHaveBeenCalledTimes(2));
  });

  it("asks before an image probe and sends nothing when the user declines", async () => {
    detectMultimodal.mockResolvedValue(detection());
    probeMultimodal.mockResolvedValue({ providerId: "novita", model: "gpt-image-1", capability: "image-generation", adapter: "openai-images", status: "available" });
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: /^图片生成/ }));
    expect(screen.getByText("还没有已保存 API Key 的模型服务能做图片生成。")).toBeTruthy();
    // One account: nothing to choose between.
    expect(screen.queryByLabelText("模型服务")).toBeNull();
    fireEvent.change(screen.getByLabelText("模型 ID"), { target: { value: "gpt-image-1" } });
    const manualProbe = within(screen.getByText("手动填写模型 ID").closest("details") as HTMLElement).getByRole("button", { name: /^实测$/ });
    expect(manualProbe.getAttribute("title")).toContain("发送前会先确认");
    fireEvent.click(manualProbe);
    const confirm = screen.getByRole("dialog");
    expect(within(confirm).getByText(/真实生成一张图片/)).toBeTruthy();
    fireEvent.click(within(confirm.querySelector("footer") as HTMLElement).getByRole("button", { name: "取消" }));
    expect(probeMultimodal).not.toHaveBeenCalled();
    fireEvent.click(manualProbe);
    fireEvent.click(screen.getByRole("button", { name: "发送实测请求" }));
    await waitFor(() => expect(probeMultimodal).toHaveBeenCalledWith("novita", "gpt-image-1", "image-generation", "openai-images", true));
  });

  it("installs after stating the key exposure, then shows results and prompts", async () => {
    detectMultimodal.mockResolvedValue(detection());
    installMultimodal.mockResolvedValue({ skillId: "bootagent-vision", results: [
      { agent: "claude-code", installed: true, envWritten: true },
      { agent: "codex", installed: false, envWritten: false, error: "A Skill with this name that BootAgent did not write is installed here; it was left alone" },
    ] });
    const changed = renderSection();
    fireEvent.click(await screen.findByRole("button", { name: /^安装$/ }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText(/BOOTAGENT_NOVITA_API_KEY/)).toBeTruthy();
    expect(within(dialog).getByText("API Key 对 Agent 可见")).toBeTruthy();
    // Both Agents are preselected; deselecting one narrows the request.
    fireEvent.click(within(dialog).getByLabelText("Codex"));
    fireEvent.click(within(dialog).getByLabelText("Codex"));
    fireEvent.click(within(dialog).getByRole("button", { name: "安装" }));
    await waitFor(() => expect(installMultimodal).toHaveBeenCalledWith({ provider: "novita", model: "qwen/qwen3-vl-8b", capability: "vision", agents: ["claude-code", "codex"], guide: false }));
    expect(await within(dialog).findByText("已安装")).toBeTruthy();
    expect(within(dialog).getByText(/did not write is installed here/)).toBeTruthy();
    expect(within(dialog).getByText(/需要重启/)).toBeTruthy();
    const copy = within(dialog).getAllByRole("button", { name: "复制提示词" });
    expect(copy.length).toBeGreaterThanOrEqual(4);
    fireEvent.click(copy[0]);
    expect(copyToClipboard).toHaveBeenCalledWith(EXAMPLE_PROMPTS.vision![0].zh);
    fireEvent.click(within(dialog).getByRole("button", { name: "完成" }));
    expect(changed).toHaveBeenCalled();
    expect(installMultimodal).toHaveBeenCalledTimes(1);
  });

  it("shows installed Skills and uninstalls through the Skill library", async () => {
    detectMultimodal.mockResolvedValue(detection({ installed: [
      { skillId: "bootagent-vision", capability: "vision", providerId: "novita", model: "qwen/qwen3-vl-8b", adapter: "openai-chat-vision", agents: ["claude-code", "codex"] },
    ] }));
    uninstallSkill.mockResolvedValue({ registry_updated: true });
    const changed = renderSection();
    expect(await screen.findByText("qwen/qwen3-vl-8b（Novita）已安装到 Claude Code、Codex")).toBeTruthy();
    expect(screen.getByText("已安装到 Claude Code、Codex")).toBeTruthy();
    expect(screen.getByRole("button", { name: "重新安装" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "卸载 bootagent-vision" }));
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "卸载" }));
    await waitFor(() => expect(uninstallSkill).toHaveBeenCalledWith("bootagent-vision"));
    await waitFor(() => expect(changed).toHaveBeenCalled());
  });

  it("allows guide-only installation when no Agent can take the Skill", async () => {
    detectMultimodal.mockResolvedValue(detection({ targets: [] }));
    installMultimodal.mockResolvedValue({ skillId: "bootagent-vision", results: [{ agent: "guide", installed: true, envWritten: true }] });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: /^安装$/ }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByRole("button", { name: "安装" })).toBeDisabled();
    fireEvent.click(within(dialog).getByLabelText("其他 Agent（生成说明文件）"));
    fireEvent.click(within(dialog).getByRole("button", { name: "安装" }));
    await waitFor(() => expect(installMultimodal).toHaveBeenCalledWith({ provider: "novita", model: "qwen/qwen3-vl-8b", capability: "vision", agents: [], guide: true }));
  });

  it.each(["network", "key", "transient"])("shows a manual model's %s result beside an account row for the capability", async (reason) => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [{ id: "known-whisper", capabilities: [{ id: "speech-to-text", source: "catalog", adapter: "openai-audio-transcriptions", supported: true, installable: true, probe: "minimal" }] }] }] }));
    probeMultimodal.mockResolvedValue({ providerId: "novita", model: "my-whisper", capability: "speech-to-text", adapter: "openai-audio-transcriptions", status: reason === "key" ? "unavailable" : "unverified", reason });
    renderSection();
    // The only capability on offer is the one opened by default.
    expect(await screen.findByRole("tab", { name: /^语音识别/, selected: true })).toBeTruthy();
    expect(screen.getByText("known-whisper")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("模型 ID"), { target: { value: "my-whisper" } });
    const manual = screen.getByText("手动填写模型 ID").closest("details") as HTMLElement;
    fireEvent.click(within(manual).getByRole("button", { name: /^实测$/ }));
    expect(await within(manual).findByText(reason === "key" ? "「my-whisper」不支持语音识别" : "无法验证「my-whisper」的语音识别")).toBeTruthy();
  });

  it("shows a rejected manual probe even when no capability row exists", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [] }] }));
    probeMultimodal.mockRejectedValue(new Error("manual request failed"));
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: /^语音识别/ }));
    fireEvent.change(screen.getByLabelText("模型 ID"), { target: { value: "my-whisper" } });
    const manual = screen.getByText("手动填写模型 ID").closest("details") as HTMLElement;
    fireEvent.click(within(manual).getByRole("button", { name: /^实测$/ }));
    expect(await within(manual).findByRole("alert")).toHaveTextContent("manual request failed");
  });

  it("keeps the install dialog open through cancel, close and Esc while installing", async () => {
    let finish: (value: unknown) => void = () => undefined;
    detectMultimodal.mockResolvedValue(detection());
    installMultimodal.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const changed = renderSection();
    fireEvent.click(await screen.findByRole("button", { name: /^安装$/ }));
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "安装" }));
    expect(within(dialog).getByRole("button", { name: "取消" })).toBeDisabled();
    expect(within(dialog).getByTitle("关闭")).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "取消" }));
    fireEvent.click(within(dialog).getByTitle("关闭"));
    fireEvent(dialog, new Event("cancel", { bubbles: true, cancelable: true }));
    expect(screen.getByRole("dialog")).toBe(dialog);
    await act(async () => finish({ skillId: "bootagent-vision", results: [{ agent: "codex", installed: true, envWritten: true }] }));
    expect(within(dialog).getByText("已安装")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "完成" }));
    await waitFor(() => expect(detectMultimodal).toHaveBeenCalledTimes(2));
    expect(changed).toHaveBeenCalledOnce();
  });

  it("offers no probe for a capability with no live check", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [gateway.models[0]] }], manual: [] }));
    renderSection();
    await screen.findByText("Novita");
    expect(screen.getAllByRole("button", { name: /^实测$/ })).toHaveLength(1);
  });

  it("says which capabilities a Provider documents no API for", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, unsupported: ["video-generation"] }] }));
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: /^视频生成/ }));
    expect(screen.getByText("Novita：该模型服务目前没有可核实的公开接口，暂不支持视频生成。")).toBeTruthy();
  });

  it("offers install without a probe for a catalog video model", async () => {
    detectMultimodal.mockResolvedValue(detection({
      generatable: [...GENERATABLE, "novita-async"],
      providers: [{ ...gateway, models: [{ id: "wan2.7-t2v", capabilities: [{ id: "video-generation", source: "catalog", adapter: "novita-async", supported: true, installable: true, probe: "", recommended: true }] }] }],
    }));
    renderSection();
    expect(await screen.findByRole("tab", { name: /^视频生成/, selected: true })).toBeTruthy();
    expect(screen.getByText("内置目录")).toBeTruthy();
    const row = screen.getByText("wan2.7-t2v").closest("li") as HTMLElement;
    expect(within(row).queryByRole("button", { name: /^实测$/ })).toBeNull();
    expect(within(row).getByRole("button", { name: /^安装$/ })).toBeTruthy();
  });

  it("writes a guide for other Agents and names the fallback file", async () => {
    detectMultimodal.mockResolvedValue(detection({ targets: ["claude-code", "codex", "opencode"], fallback: ["opencode"] }));
    installMultimodal.mockResolvedValue({ skillId: "bootagent-vision", guidePath: "/Users/me/.bootagent/multimodal/vision", results: [{ agent: "guide", installed: true, envWritten: true }] });
    renderSection();
    fireEvent.click(await screen.findByRole("button", { name: /^安装$/ }));
    const dialog = screen.getByRole("dialog");
    // OpenCode is preselected and has no configurable environment.
    expect(within(dialog).getByText(/私有文件 \/Users\/me\/\.bootagent\/skill-env\/novita\.env/)).toBeTruthy();
    for (const name of ["Claude Code", "Codex", "opencode"]) fireEvent.click(within(dialog).getByLabelText(name));
    fireEvent.click(within(dialog).getByLabelText("其他 Agent（生成说明文件）"));
    fireEvent.click(within(dialog).getByRole("button", { name: "安装" }));
    await waitFor(() => expect(installMultimodal).toHaveBeenCalledWith({ provider: "novita", model: "qwen/qwen3-vl-8b", capability: "vision", agents: [], guide: true }));
    expect(await within(dialog).findByText("其他 Agent（说明文件）")).toBeTruthy();
    expect(within(dialog).getByText("其他 Agent：说明文件在 /Users/me/.bootagent/multimodal/vision")).toBeTruthy();
    fireEvent.click(within(dialog).getAllByRole("button", { name: "复制提示词" })[0]);
    expect(copyToClipboard).toHaveBeenCalledWith(expect.stringContaining("先读 /Users/me/.bootagent/multimodal/vision/SKILL.md"));
  });

  it("warns once per tab when ffmpeg is missing for video by frames", async () => {
    const frames: MultimodalProvider = { ...gateway, models: [{ id: "qwen/qwen3-vl-8b", capabilities: [{ id: "video-understanding", source: "declared", adapter: "frames-chat-vision", supported: true, installable: true, probe: "minimal" }] }] };
    detectMultimodal.mockResolvedValue(detection({ ffmpeg: false, providers: [frames, { ...frames, id: "ppio", name: "PPIO" }] }));
    renderSection();
    expect(await screen.findAllByText(/没有在 PATH 上找到 ffmpeg/)).toHaveLength(1);
  });

  it("points to the Providers page when no key is saved", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [] }));
    renderSection();
    expect(await screen.findByText("去添加模型服务")).toBeTruthy();
  });

  it("puts generation first and opens the first capability an account can provide", async () => {
    detectMultimodal.mockResolvedValue(detection());
    renderSection();
    const tabs = await screen.findAllByRole("tab");
    expect(tabs.map((tab) => tab.textContent)).toEqual(["图片生成", "视频生成", "图片理解1", "视频理解1", "语音合成", "语音识别"]);
    expect(screen.getByRole("tab", { name: /^图片理解/ })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tabpanel")).toHaveAttribute("aria-labelledby", "multimodal-tab-vision");
  });

  it("opens image generation by default when an account can do it", async () => {
    const images = { id: "gpt-image-1", capabilities: [{ id: "image-generation" as const, source: "probe" as const, adapter: "openai-images", supported: true, installable: true, probe: "confirm" as const }] };
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [...gateway.models, images] }] }));
    renderSection();
    expect(await screen.findByRole("tab", { name: /^图片生成/, selected: true })).toBeTruthy();
    expect(screen.getByText("gpt-image-1")).toBeTruthy();
    expect(screen.queryByText("还没有已保存 API Key 的模型服务能做图片生成。")).toBeNull();
  });

  it("switches capability by click and arrow keys", async () => {
    detectMultimodal.mockResolvedValue(detection());
    renderSection();
    const vision = await screen.findByRole("tab", { name: /^图片理解/ });
    fireEvent.keyDown(vision, { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: /^视频理解/ })).toHaveAttribute("aria-selected", "true");
    expect(document.activeElement).toBe(screen.getByRole("tab", { name: /^视频理解/ }));
    fireEvent.keyDown(document.activeElement as HTMLElement, { key: "Home" });
    expect(screen.getByRole("tab", { name: /^图片生成/ })).toHaveAttribute("aria-selected", "true");
    fireEvent.click(screen.getByRole("tab", { name: /^图片理解/ }));
    expect(screen.getByText("qwen/qwen3-vl-8b", { selector: "option" })).toBeTruthy();
  });

  it("lists the account the user added before built-in ones and marks it", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [visionOnly("ppio", "PPIO"), visionOnly("mine", "我的网关")] }));
    renderSection(vi.fn(), { ppio: builtIn("PPIO", 2), mine: custom("我的网关", "2026-09-01T00:00:00Z") });
    await screen.findByRole("tab", { name: /^图片理解2/ });
    const accounts = screen.getByRole("tabpanel").querySelectorAll(".multimodal-account");
    expect([...accounts].map((item) => item.querySelector("strong")?.textContent)).toEqual(["我的网关", "PPIO"]);
    expect(within(accounts[0] as HTMLElement).getByText("用户添加")).toBeTruthy();
    expect(within(accounts[1] as HTMLElement).queryByText("用户添加")).toBeNull();
  });

  it("names the accounts that have no model for the open capability", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [gateway, { ...gateway, id: "ppio", name: "PPIO", models: [] }] }));
    renderSection();
    expect(await screen.findByText("PPIO 的模型列表里没有能做图片理解的模型。")).toBeTruthy();
    // With two accounts, the manual entry asks which one to probe on.
    fireEvent.click(screen.getByRole("tab", { name: /^图片生成/ }));
    expect(screen.getByLabelText("模型服务")).toBeTruthy();
  });

  it("marks the tab of an installed capability", async () => {
    detectMultimodal.mockResolvedValue(detection({ installed: [
      { skillId: "bootagent-vision", capability: "vision", providerId: "novita", model: "qwen/qwen3-vl-8b", adapter: "openai-chat-vision", agents: ["codex"] },
    ] }));
    renderSection();
    expect(await screen.findByRole("tab", { name: /^图片理解已安装/ })).toBeTruthy();
  });

  it("reports a failed listing without hiding the Provider", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ id: "deepseek", name: "DeepSeek", keyEnv: "BOOTAGENT_DEEPSEEK_API_KEY", keyFile: "/Users/me/.bootagent/skill-env/deepseek.env", unsupported: [], listed: false, message: "API key was rejected (401).", errorCode: "API_KEY_REJECTED", models: [] }] }));
    renderSection();
    expect(await screen.findByText("DeepSeek：无法读取模型列表：API key was rejected (401).")).toBeTruthy();
  });
});
