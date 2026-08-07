import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../backend/api";
import type { ProviderEntry, StatusResponse } from "../types/api";
import { TransferPage } from "./TransferPage";

const dialogs = vi.hoisted(() => ({ Question: vi.fn(), SaveFile: vi.fn(), OpenFile: vi.fn() }));
vi.mock("@wailsio/runtime", async (importOriginal) => ({ ...await importOriginal<typeof import("@wailsio/runtime")>(), Dialogs: dialogs }));

const refreshStatus = vi.fn<() => Promise<void>>();
const status = {
  apiVersion: 1, platform: { os: "macos", arch: "arm64", shell: "bash" }, runtimes: [],
  capabilities: { canInstall: {}, missingRuntime: {}, supportedAgentIds: [] }, agents: {}, catalog: [], groups: [],
  providers: { ppio: { name: "PPIO", home: "", base_url: "https://api.example.test", has_key: true } },
  mirrors: [], paths: {}, backups: {}, environment: null, environmentError: null, desktopAgents: [], activeProfile: null, firstRun: false,
  profiles: [{ id: "team", label: "团队", provider: "ppio", model: "model", protocol: "responses", baseUrl: null, activatedAt: null, hasKey: true }],
} satisfies StatusResponse;

vi.mock("../state/WizardContext", () => ({ useWizard: () => ({ state: { status }, refreshStatus }) }));

describe("TransferPage", () => {
  afterEach(() => vi.restoreAllMocks());

  it("includes the provider required by a selected profile and uses the native save dialog", async () => {
    const provider = { id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true } satisfies ProviderEntry;
    vi.spyOn(api, "getProvider").mockResolvedValue(provider);
    const write = vi.spyOn(api, "writeTransferFile").mockResolvedValue();
    dialogs.Question.mockResolvedValue("不加密");
    dialogs.SaveFile.mockResolvedValue("/tmp/selected.json");

    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    const required = screen.getAllByRole("checkbox", { name: /PPIO/ }).find((checkbox) => checkbox.hasAttribute("disabled"));
    expect(required).toBeChecked();
    expect(required).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "导出" }));

    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(dialogs.SaveFile).toHaveBeenCalledOnce();
    expect(JSON.parse(write.mock.calls[0][1])).toMatchObject({ profiles: [{ id: "team" }], providers: [{ id: "ppio" }] });
  });

  it("continues encrypted export through the in-app password form", async () => {
    vi.spyOn(api, "getProvider").mockResolvedValue({ id: "ppio", name: "PPIO", home: "", base_url: "https://api.example.test", anthropic_base_url: "", api_key: "secret", built_in: true });
    const write = vi.spyOn(api, "writeTransferFile").mockResolvedValue();
    dialogs.Question.mockResolvedValue("加密");
    dialogs.SaveFile.mockResolvedValue("/tmp/selected.json");
    render(<MemoryRouter><TransferPage /></MemoryRouter>);
    fireEvent.click(screen.getByRole("checkbox", { name: /团队/ }));
    fireEvent.click(screen.getByRole("button", { name: "导出" }));
    const password = await screen.findByDisplayValue("");
    fireEvent.change(password, { target: { value: "passphrase" } });
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    const exported = JSON.parse(write.mock.calls[0][1]);
    expect(exported.encrypted).toHaveLength(1);
    expect(exported.providers[0].key_encrypted).toBe(0);
  });
});
