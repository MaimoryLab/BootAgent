import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TaskCenterProvider } from "../state/TaskCenterContext";
import { TaskCenter } from "../components/TaskCenter";
import { useConversationMigration } from "./useConversationMigration";

const bridge = vi.hoisted(() => ({
  migrate: vi.fn(),
  question: vi.fn(),
}));

vi.mock("@wailsio/runtime", () => ({ Dialogs: { Question: bridge.question } }));
vi.mock("../backend/api", async () => {
  const errors = await import("../backend/errors");
  return {
    api: { migrateConversations: bridge.migrate, onInstallOutput: () => () => {} },
    describeFailure: errors.describeFailure,
  };
});

function Trigger({ name }: { name: string }) {
  const migration = useConversationMigration();
  return (
    <section>
      <button type="button" disabled={migration.running} onClick={() => void migration.run()}>{name}</button>
    </section>
  );
}

describe("useConversationMigration", () => {
  beforeEach(() => {
    bridge.migrate.mockReset();
    bridge.question.mockReset();
    bridge.question.mockResolvedValue("继续迁移");
  });

  it("shares one lock and outcome across both entry points", async () => {
    let complete!: (value: { files: number; threads: number }) => void;
    bridge.migrate.mockReturnValue(new Promise((resolve) => { complete = resolve; }));
    const user = userEvent.setup();
    render(
      <TaskCenterProvider>
        <TaskCenter />
        <Trigger name="命令行迁移" />
        <Trigger name="桌面迁移" />
      </TaskCenterProvider>,
    );

    await user.click(screen.getByRole("button", { name: "命令行迁移" }));
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "命令行迁移" })).toBeDisabled();
      expect(screen.getByRole("button", { name: "桌面迁移" })).toBeDisabled();
    });
    expect(bridge.migrate).toHaveBeenCalledTimes(1);

    complete({ files: 2, threads: 3 });
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "命令行迁移" })).toBeEnabled();
      expect(screen.getByRole("button", { name: "桌面迁移" })).toBeEnabled();
    });
    expect(screen.getAllByText(/已迁移 2 个对话文件和 3 条索引记录/)).toHaveLength(1);
  });
});
