import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OneAgentApiError } from "../backend/errors";
import { MirrorSetting } from "./MirrorSetting";

const getSettings = vi.fn();
const saveSettings = vi.fn();

vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: {
      getSettings: () => getSettings(),
      saveSettings: (settings: unknown) => saveSettings(settings),
    },
    describeError: errors.describeError,
  };
});

const heading = /下载源/;

describe("MirrorSetting", () => {
  beforeEach(() => {
    getSettings.mockReset();
    saveSettings.mockReset();
    getSettings.mockResolvedValue({ schema_version: 1, prefer_mirror: false, mirror_from_region: false });
    saveSettings.mockImplementation(async (settings: { prefer_mirror: boolean }) => ({
      schema_version: 1,
      prefer_mirror: settings.prefer_mirror,
      mirror_from_region: false,
    }));
  });

  it("keeps the preference folded away and off by default", async () => {
    render(<MirrorSetting />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    // Folded: the label is visible but the switch is not rendered until opened.
    expect(screen.getByRole("button", { name: heading })).toBeTruthy();
    expect(screen.queryByRole("switch")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: heading }));
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(false);
  });

  it("persists the preference when switched on", async () => {
    render(<MirrorSetting />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("button", { name: heading }));

    await userEvent.click(screen.getByRole("switch"));
    await waitFor(() =>
      expect(saveSettings).toHaveBeenCalledWith({ schema_version: 1, prefer_mirror: true, mirror_from_region: false }),
    );
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(true);
  });

  // The copy has to say the mirror covers Agent packages too, or a user who
  // turned it on for a slow runtime download will not know why npm changed host.
  it("names both downloads it governs", async () => {
    render(<MirrorSetting />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("button", { name: heading }));
    expect(screen.getByText(/同时作用于运行时下载和 npm 安装的 Agent/)).toBeTruthy();
  });

  // A switch that looks on while the preference was never stored would send the
  // user back to a slow host on the next launch with no way to tell.
  it("reverts the switch and explains when saving fails", async () => {
    saveSettings.mockImplementation(async () => {
      throw new OneAgentApiError("磁盘只读", "INTERNAL_ERROR", false, 500);
    });
    render(<MirrorSetting />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("button", { name: heading }));

    await userEvent.click(screen.getByRole("switch"));
    await waitFor(() => expect(screen.getByText("磁盘只读")).toBeTruthy());
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(false);
  });

  // An unreadable settings file already falls back to the official source in Go;
  // the UI must agree rather than showing a blank or stuck control.
  it("renders the default when settings cannot be read", async () => {
    getSettings.mockImplementation(async () => {
      throw new OneAgentApiError("读取失败", "INTERNAL_ERROR", false, 500);
    });
    render(<MirrorSetting />);
    await userEvent.click(screen.getByRole("button", { name: heading }));
    await waitFor(() => expect((screen.getByRole("switch") as HTMLInputElement).disabled).toBe(false));
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(false);
  });

  // A box that is already ticked on first run has to say why, or it reads as
  // something the user set and forgot.
  it("explains a tick that came from the system region", async () => {
    getSettings.mockResolvedValue({ schema_version: 1, prefer_mirror: true, mirror_from_region: true });
    render(<MirrorSetting />);
    await waitFor(() => expect(screen.getByText(/已根据系统地区设置默认使用镜像/)).toBeTruthy());

    await userEvent.click(screen.getByRole("button", { name: heading }));
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(true);
    expect(screen.getByText(/已根据系统语言\/地区自动开启/)).toBeTruthy();
  });

  // Turning off a regional default must persist as the user's own choice, so the
  // next launch does not tick it again.
  it("stores an explicit off over a regional default", async () => {
    getSettings.mockResolvedValue({ schema_version: 1, prefer_mirror: true, mirror_from_region: true });
    render(<MirrorSetting />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("button", { name: heading }));

    await userEvent.click(screen.getByRole("switch"));
    await waitFor(() =>
      expect(saveSettings).toHaveBeenCalledWith({ schema_version: 1, prefer_mirror: false, mirror_from_region: false }),
    );
    expect((screen.getByRole("switch") as HTMLInputElement).checked).toBe(false);
    // The regional explanation is gone: it is now a stored choice.
    expect(screen.queryByText(/已根据系统语言\/地区自动开启/)).toBeNull();
  });

  // The Profiles page labels it "Agent 安装源" because that is what the page's
  // action installs; the control behind the label is the same one.
  it("accepts a caller-supplied label", async () => {
    render(<MirrorSetting label="Agent 安装源" />);
    await waitFor(() => expect(getSettings).toHaveBeenCalled());
    expect(screen.getByRole("button", { name: /Agent 安装源/ })).toBeTruthy();
  });
});
