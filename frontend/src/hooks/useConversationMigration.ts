import { Dialogs } from "@wailsio/runtime";

import { api, describeFailure } from "../backend/api";
import { useI18n } from "../i18n";
import { taskKey, useTaskCenter } from "../state/TaskCenterContext";

const TASK_ID = taskKey("migration", "codex-conversations");

export function useConversationMigration() {
  const { t } = useI18n();
  const { startTask, finishTask, taskFor } = useTaskCenter();
  const task = taskFor(TASK_ID);

  const run = async () => {
    const confirmLabel = t("继续迁移");
    const choice = await Dialogs.Question({
      Title: t("迁移对话"),
      Message: t("将所有 Codex 与 ChatGPT Desktop 历史对话迁入 BootAgent。此操作不创建备份，无法自动恢复。"),
      Buttons: [{ Label: confirmLabel }, { Label: t("取消"), IsCancel: true }],
    }).catch(() => "");
    if (choice !== confirmLabel || !startTask({
      id: TASK_ID,
      kind: "migration",
      target: "codex-conversations",
      title: t("迁移对话"),
      route: "",
      openable: false,
      cancellable: false,
    })) return;

    try {
      const result = await api.migrateConversations();
      finishTask(TASK_ID, {
        kind: "success",
        message: t("已迁移 {files} 个对话文件和 {threads} 条索引记录", { files: result.files, threads: result.threads }),
      });
    } catch (error) {
      finishTask(TASK_ID, { kind: "failure", message: describeFailure(error, t("无法迁移对话"), t).message });
    }
  };

  return {
    run,
    running: task?.state === "running",
  };
}
