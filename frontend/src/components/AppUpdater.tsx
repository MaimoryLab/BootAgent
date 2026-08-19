import { Dialogs } from "@wailsio/runtime";
import { useEffect, useRef } from "react";

import { api, describeFailure, failureLine, isCancellationError } from "../backend/api";
import { OTA_PROGRESS_TARGET } from "../backend/wails";
import { useI18n } from "../i18n";
import { taskCanceller, taskKey, updateTaskRoute, useTaskCenter } from "../state/TaskCenterContext";

const OTA_TASK_ID = taskKey("update", OTA_PROGRESS_TARGET);

export function AppUpdater() {
  const { t } = useI18n();
  const taskCenter = useTaskCenter();
  const latest = useRef(taskCenter);
  const checked = useRef(false);
  const active = useRef(false);
  const restarting = useRef(false);
  latest.current = taskCenter;

  useEffect(() => {
    active.current = true;
    if (checked.current) return () => { active.current = false; };
    checked.current = true;
    if (latest.current.isTaskRunning(OTA_TASK_ID)) return () => { active.current = false; };

    void (async () => {
      let version: string;
      try {
        version = await api.checkUpdate();
      } catch (error) {
        // Staying silent is right for a check that merely could not reach a
        // source -- the user did not ask, and a startup dialog about a transient
        // network failure is noise. UPDATE_LOCATION_BLOCKED is the exception: the
        // backend only reaches that check once a newer release exists, so it
        // means an update is waiting and this installation can never apply it.
        // Left silent, the app stops updating for good with nothing said, and the
        // only place the reason appears is a settings page the user has no reason
        // to open.
        const failure = describeFailure(error, t("检查更新失败"), t);
        if (active.current && failure.code === "UPDATE_LOCATION_BLOCKED") {
          void Dialogs.Warning({
            Title: t("BootAgent 更新"),
            Message: failure.hint ? `${failure.message}\n\n${failure.hint}` : failure.message,
          }).catch(() => {});
        }
        return;
      }
      if (!active.current || !version || latest.current.isTaskRunning(OTA_TASK_ID)) return;

      const updateLabel = t("更新");
      let choice: string;
      try {
        choice = await Dialogs.Question({
          Title: t("BootAgent 更新"),
          Message: t("发现 BootAgent 新版本 {version}，现在下载吗？", { version }),
          Buttons: [
            { Label: updateLabel, IsDefault: true },
            { Label: t("暂不"), IsCancel: true },
          ],
        });
      } catch {
        return;
      }
      if (!active.current || choice !== updateLabel || !latest.current.startTask({
        id: OTA_TASK_ID,
        kind: "update",
        target: OTA_PROGRESS_TARGET,
        progressTarget: OTA_PROGRESS_TARGET,
        title: t("更新 BootAgent {version}", { version }),
        route: updateTaskRoute(OTA_PROGRESS_TARGET),
      })) return;

      try {
        const request = api.downloadUpdate();
        latest.current.setTaskCanceller(OTA_TASK_ID, taskCanceller(request));
        await request;
        latest.current.finishTask(OTA_TASK_ID, { kind: "success", message: t("更新已下载") });
        latest.current.setTaskAction(OTA_TASK_ID, {
          label: t("重启并更新"),
          run: async () => {
            if (restarting.current) return;
            restarting.current = true;
            try {
              await api.restartUpdate();
            } catch (error) {
              latest.current.setTaskMessage(OTA_TASK_ID, describeFailure(error, t("无法重启并更新"), t).message);
            } finally {
              restarting.current = false;
            }
          },
        });
      } catch (error) {
        latest.current.finishTask(OTA_TASK_ID, isCancellationError(error)
          ? { kind: "cancelled", message: t("已取消") }
          : { kind: "failure", message: failureLine(error, t("更新失败"), t) });
      }
    })();

    return () => { active.current = false; };
  }, [t]);

  return null;
}
