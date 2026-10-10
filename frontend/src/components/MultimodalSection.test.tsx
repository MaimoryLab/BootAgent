import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { MultimodalDetection, MultimodalProvider } from "../types/api";

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
import { describeProbe, groupByCapability, groupInstalled, installLabel, MultimodalSection } from "./MultimodalSection";
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

function renderSection(onSkillsChanged = vi.fn()) {
  render(<MemoryRouter><I18nProvider><MultimodalSection agentLabels={labels} onSkillsChanged={onSkillsChanged} /></I18nProvider></MemoryRouter>);
  return onSkillsChanged;
}

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
    fireEvent.change(await screen.findByLabelText("图片理解"), { target: { value: "moonshotai/kimi-k3" } });
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
    await screen.findByText("Novita");
    fireEvent.change(screen.getByLabelText("能力"), { target: { value: "image-generation" } });
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

  it.each(["network", "key", "transient"])("shows a manual model's %s result despite an existing capability row", async (reason) => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [{ id: "known-whisper", capabilities: [{ id: "speech-to-text", source: "catalog", adapter: "openai-audio-transcriptions", supported: true, installable: true, probe: "minimal" }] }] }] }));
    probeMultimodal.mockResolvedValue({ providerId: "novita", model: "my-whisper", capability: "speech-to-text", adapter: "openai-audio-transcriptions", status: reason === "key" ? "unavailable" : "unverified", reason });
    renderSection();
    await screen.findByText("Novita");
    fireEvent.change(screen.getByLabelText("能力"), { target: { value: "speech-to-text" } });
    fireEvent.change(screen.getByLabelText("模型 ID"), { target: { value: "my-whisper" } });
    const manual = screen.getByText("手动填写模型 ID").closest("details") as HTMLElement;
    fireEvent.click(within(manual).getByRole("button", { name: /^实测$/ }));
    expect(await within(manual).findByText(reason === "key" ? "「my-whisper」不支持语音识别" : "无法验证「my-whisper」的语音识别")).toBeTruthy();
  });

  it("shows a rejected manual probe even when no capability row exists", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ ...gateway, models: [] }] }));
    probeMultimodal.mockRejectedValue(new Error("manual request failed"));
    renderSection();
    await screen.findByText("Novita");
    fireEvent.change(screen.getByLabelText("能力"), { target: { value: "speech-to-text" } });
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
    expect(await screen.findByText("视频生成：该模型服务目前没有可核实的公开接口，暂不支持。")).toBeTruthy();
  });

  it("offers install without a probe for a catalog video model", async () => {
    detectMultimodal.mockResolvedValue(detection({
      generatable: [...GENERATABLE, "novita-async"],
      providers: [{ ...gateway, models: [{ id: "wan2.7-t2v", capabilities: [{ id: "video-generation", source: "catalog", adapter: "novita-async", supported: true, installable: true, probe: "", recommended: true }] }] }],
    }));
    renderSection();
    expect(await screen.findByLabelText("视频生成")).toBeTruthy();
    expect(screen.getByText("内置目录")).toBeTruthy();
    const row = screen.getByLabelText("视频生成").closest("li") as HTMLElement;
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

  it("warns when ffmpeg is missing for video by frames", async () => {
    detectMultimodal.mockResolvedValue(detection({ ffmpeg: false, providers: [{ ...gateway, models: [{ id: "qwen/qwen3-vl-8b", capabilities: [{ id: "video-understanding", source: "declared", adapter: "frames-chat-vision", supported: true, installable: true, probe: "minimal" }] }] }] }));
    renderSection();
    expect(await screen.findByText(/没有在 PATH 上找到 ffmpeg/)).toBeTruthy();
  });

  it("points to the Providers page when no key is saved", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [] }));
    renderSection();
    expect(await screen.findByText("去添加模型服务")).toBeTruthy();
  });

  it("reports a failed listing without hiding the Provider", async () => {
    detectMultimodal.mockResolvedValue(detection({ providers: [{ id: "deepseek", name: "DeepSeek", keyEnv: "BOOTAGENT_DEEPSEEK_API_KEY", keyFile: "/Users/me/.bootagent/skill-env/deepseek.env", unsupported: [], listed: false, message: "API key was rejected (401).", errorCode: "API_KEY_REJECTED", models: [] }] }));
    renderSection();
    expect(await screen.findByText(/无法读取模型列表/)).toBeTruthy();
  });
});
