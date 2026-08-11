import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { ProviderEntry, StatusResponse } from "../types/api";
import { TransferPage } from "./TransferPage";

const refreshStatus = vi.fn<() => Promise<void>>();
const status = {
  apiVersion: 1, platform: { os: "macos", arch: "arm64", shell: "bash" }, runtimes: [],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] }, agents: {}, catalog: [], groups: [],
  providers: { ppio: { name: "PPIO", home: "", base_url: "https://api.example.test", has_key: true } },
  mirrors: [], paths: {}, backups: {}, environment: null, environmentError: null, desktopAgents: [], activeProfile: null, firstRun: false,
  profiles: [{ id: "team", label: "团队", provider: "ppio", model: "model", protocol: "responses", baseUrl: null, activatedAt: null }],
} satisfies StatusResponse;

vi.mock("../state/WizardContext", () => ({ useWizard: () => ({ state: { status }, refreshStatus }) }));

describe("TransferPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("includes the provider required by a selected profile", async () => {
    const provider = { id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true } satisfies ProviderEntry;
    vi.spyOn(api, "getProvider").mockResolvedValue(provider);
    const write = vi.spyOn(api, "writeTransferFile").mockResolvedValue();

    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    const required = screen.getAllByRole("checkbox", { name: /PPIO/ }).find((checkbox) => checkbox.hasAttribute("disabled"));
    expect(required).toBeChecked();
    expect(required).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    fireEvent.click(screen.getByRole("button", { name: "明文包含" }));

    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(JSON.parse(write.mock.calls[0][0])).toMatchObject({ profiles: [{ id: "team" }], providers: [{ id: "ppio" }] });
  });

  it("selects all providers and profiles", () => {
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "全选" }));
    expect(screen.getByRole("checkbox", { name: /团队/ })).toBeChecked();
    screen.getAllByRole("checkbox", { name: /PPIO/ }).forEach((checkbox) => expect(checkbox).toBeChecked());
    expect(screen.getByRole("button", { name: "取消全选" })).toBeTruthy();
  });

  it("continues encrypted export through the in-app password form", async () => {
    vi.spyOn(api, "getProvider").mockResolvedValue({ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true });
    const write = vi.spyOn(api, "writeTransferFile").mockResolvedValue();
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    fireEvent.click(screen.getByRole("button", { name: "加密包含" }));
    const password = await screen.findByDisplayValue("");
    fireEvent.change(password, { target: { value: "passphrase" } });
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const exported = JSON.parse(write.mock.calls[0][0]);
    expect(exported.encrypted).toHaveLength(1);
    expect(exported.providers[0].key_encrypted).toBe(0);
  });

  // The dialog decides between two irreversible outcomes, so it has to name both.
  it("states the consequence of each export encryption choice", () => {
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    // No I18nProvider here, so translate() returns the Chinese keys unchanged.
    expect(screen.getByText("导出文件是否包含 API Key？")).toBeTruthy();
    // The default is stated first, and the two costly options each name their cost.
    expect(screen.getByText(/默认不包含/)).toBeTruthy();
    expect(screen.getByText(/密码无法找回/)).toBeTruthy();
    expect(screen.getByText(/以明文保存 API Key/)).toBeTruthy();
  });

  it("says the exported file holds plain-text keys when encryption is declined", async () => {
    vi.spyOn(api, "getProvider").mockResolvedValue({ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true });
    vi.spyOn(api, "writeTransferFile").mockResolvedValue();
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    fireEvent.click(screen.getByRole("button", { name: "明文包含" }));
    await waitFor(() => expect(screen.getByText(/API Key 为明文/)).toBeTruthy());
  });

  // The default action. Exporting a config file should not hand over a working
  // credential unless that was explicitly chosen.
  it("writes no API key when the default is taken", async () => {
    vi.spyOn(api, "getProvider").mockResolvedValue({ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true });
    const write = vi.spyOn(api, "writeTransferFile").mockResolvedValue();
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    fireEvent.click(screen.getByRole("button", { name: "不包含 Key" }));

    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const raw = write.mock.calls[0][0];
    expect(raw).not.toContain("secret");
    expect(Object.hasOwn(JSON.parse(raw).providers[0], "apikey")).toBe(false);
    expect(screen.getByText(/文件不包含 API Key/)).toBeTruthy();
  });

  it("reports a cancelled export instead of returning silently", async () => {
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.getByText("已取消导出")).toBeTruthy());
  });

  /** An export of one Provider and one Profile, both colliding with saved records. */
  const collidingFile = JSON.stringify({
    version: 1,
    timestamp: "2026-08-08T00:00:00.000Z",
    providers: [{ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", built_in: true, apikey: "incoming" }],
    profiles: [{ id: "team", label: "团队", provider: "ppio", model: "model", protocol: "responses" }],
    encrypted: [],
  });

  const startImport = (raw: string) => {
    vi.spyOn(api, "readTransferFile").mockResolvedValue(raw);
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("button", { name: "导入" }));
  };

  it("names what an import will overwrite before writing anything", async () => {
    const saveProvider = vi.spyOn(api, "saveProvider");
    startImport(collidingFile);
    await waitFor(() => expect(screen.getByText("确认导入")).toBeTruthy());
    expect(screen.getByText(/将覆盖 1 个模型服务：PPIO/)).toBeTruthy();
    expect(screen.getByText(/将覆盖 1 个配置模版/)).toBeTruthy();
    // The confirmation has to come before the write, or naming it is pointless.
    expect(saveProvider).not.toHaveBeenCalled();
  });

  it("writes nothing when the import confirmation is declined", async () => {
    const saveProvider = vi.spyOn(api, "saveProvider");
    const saveProfile = vi.spyOn(api, "saveProfile");
    startImport(collidingFile);
    await waitFor(() => expect(screen.getByText("确认导入")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await waitFor(() => expect(screen.getByText("已取消导入")).toBeTruthy());
    expect(saveProvider).not.toHaveBeenCalled();
    expect(saveProfile).not.toHaveBeenCalled();
  });

  it("does not ask for a password when the file is not encrypted", async () => {
    // `encrypted: []` is truthy, so this used to prompt for a password that
    // decrypted nothing -- and cancelling it aborted the import silently.
    const saveProvider = vi.spyOn(api, "saveProvider").mockResolvedValue({ entry: { id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "incoming", built_in: true }, reapplied: null, failures: null });
    vi.spyOn(api, "saveProfile").mockResolvedValue({ profile: status.profiles[0], reapplied: null, failures: null });
    refreshStatus.mockResolvedValue();
    startImport(collidingFile);
    await waitFor(() => expect(screen.getByText("确认导入")).toBeTruthy());
    // Scoped to the dialog: its confirm button and the page's own import button
    // share the same label.
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "导入" }));
    await waitFor(() => expect(saveProvider).toHaveBeenCalled());
    expect(screen.queryByText("请输入导入密码")).toBeNull();
    expect(screen.getByText("导入完成")).toBeTruthy();
  });

  /** An export taken with the default: Providers and Profiles, no keys. */
  const keylessFile = JSON.stringify({
    version: 1,
    timestamp: "2026-08-08T00:00:00.000Z",
    providers: [{ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", built_in: true }],
    profiles: [{ id: "team", label: "团队", provider: "ppio", model: "model", protocol: "responses" }],
    encrypted: [],
  });

  // The trap this whole change had to avoid: Store.Save writes APIKey
  // unconditionally, so importing a key-less file would blank the recipient's
  // saved credential. keep_existing_key is what stops that.
  it("keeps the saved key when the file carries none", async () => {
    const saveProvider = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: { id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "kept", built_in: true },
      reapplied: null, failures: null,
    });
    vi.spyOn(api, "saveProfile").mockResolvedValue({ profile: status.profiles[0], reapplied: null, failures: null });
    refreshStatus.mockResolvedValue();
    startImport(keylessFile);
    await waitFor(() => expect(screen.getByText("确认导入")).toBeTruthy());
    // The confirmation says the saved keys survive, so the user is not warned
    // about a loss that is not going to happen.
    expect(screen.getByText(/本机已保存的 Key 会保留/)).toBeTruthy();
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "导入" }));

    await waitFor(() => expect(saveProvider).toHaveBeenCalled());
    expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({ keep_existing_key: true }));
  });

  it("replaces the saved key when the file does carry one", async () => {
    const saveProvider = vi.spyOn(api, "saveProvider").mockResolvedValue({
      entry: { id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "incoming", built_in: true },
      reapplied: null, failures: null,
    });
    vi.spyOn(api, "saveProfile").mockResolvedValue({ profile: status.profiles[0], reapplied: null, failures: null });
    refreshStatus.mockResolvedValue();
    startImport(collidingFile);
    await waitFor(() => expect(screen.getByText("确认导入")).toBeTruthy());
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "导入" }));

    await waitFor(() => expect(saveProvider).toHaveBeenCalled());
    expect(saveProvider).toHaveBeenCalledWith(expect.objectContaining({ keep_existing_key: false, api_key: "incoming" }));
  });

  it("localises a malformed import file instead of showing the JSON parser error", async () => {
    startImport("{ not json");
    await waitFor(() => expect(screen.getByText(/文件格式无效/)).toBeTruthy());
    expect(screen.queryByText(/Unexpected token/)).toBeNull();
  });

  it("names the unsupported version rather than calling the file invalid", async () => {
    startImport(JSON.stringify({ version: 2, providers: [], profiles: [], encrypted: [] }));
    await waitFor(() => expect(screen.getByText(/版本（2）不受支持/)).toBeTruthy());
  });
});
