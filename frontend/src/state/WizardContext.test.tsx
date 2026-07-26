import { act, renderHook, waitFor } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../api/client";
import type { StatusResponse } from "../types/api";
import { useWizard, WizardProvider } from "./WizardContext";

const status = {
  apiVersion: 1,
  platform: { os: "linux", arch: "x64", shell: "bash" },
  capabilities: { canInstall: {}, supportedAgentIds: [] },
  agents: {},
  catalog: [],
  groups: [],
  providers: {},
  paths: {},
  backups: {},
  environment: null,
  environmentError: null,
} satisfies StatusResponse;

const wrapper = ({ children }: PropsWithChildren) => <WizardProvider>{children}</WizardProvider>;

describe("WizardProvider", () => {
  afterEach(() => vi.restoreAllMocks());

  it("loads status and keeps the API key only in a ref", async () => {
    vi.spyOn(api, "status").mockResolvedValue(status);
    const { result } = renderHook(() => useWizard(), { wrapper });
    await waitFor(() => expect(result.current.state.statusState).toBe("success"));

    act(() => result.current.secret.setApiKey("sentinel-secret"));
    expect(result.current.secret.keyRef.current).toBe("sentinel-secret");
    expect(result.current.state.hasApiKey).toBe(true);
    expect(JSON.stringify(result.current.state)).not.toContain("sentinel-secret");

    act(() => result.current.secret.clearApiKey());
    expect(result.current.secret.keyRef.current).toBe("");
    expect(result.current.state.hasApiKey).toBe(false);
  });

  it("exposes status load errors without throwing", async () => {
    vi.spyOn(api, "status").mockRejectedValue(new Error("offline"));
    const { result } = renderHook(() => useWizard(), { wrapper });
    await waitFor(() => expect(result.current.state.statusState).toBe("error"));
    expect(result.current.state.statusError).toBe("offline");
  });
});
