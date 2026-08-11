import { useLocation, useNavigate, useParams } from "react-router-dom";

import { DownloadProgress } from "../components/DownloadProgress";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { installTaskRoute, useTaskCenter } from "../state/TaskCenterContext";

export function InstallTaskPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const location = useLocation();
  const { agentId = "" } = useParams();
  const kind = location.pathname.startsWith("/tasks/update/") ? "update" : "install";
  const { tasks, cancelTask, dismissTask } = useTaskCenter();
  const target = decodeURIComponent(agentId);
  const task = tasks.find((item) => item.kind === kind && item.target === target);
  if (!task) {
    // Reached by reloading this route after tasks were cleared, or by following a
    // link to one that has been dismissed. A bare "no tasks" left the user unsure
    // whether their install had been lost; the overview is where the real state
    // is, so say that rather than only offering a button.
    return (
      <PageScaffold
        title={t("暂无任务")}
        description={t("这个任务已经结束或被关闭。安装结果请在环境总览中查看")}
        primaryLabel={t("进入总览")}
        onPrimary={() => navigate("/overview")}
      />
    );
  }
  const running = task.state === "running";
  const title = running
    ? kind === "update" ? t("更新中") : t("正在安装")
    : task.state === "success" ? kind === "update" ? t("更新完成") : t("安装完成")
      : task.state === "cancelled" ? t("已取消") : t("需要处理部分问题");
  return (
    <PageScaffold
      title={`${title} · ${task.title}`}
      description={task.message || t("每个 Agent 的结果彼此独立，失败项可以单独重试")}
      primaryLabel={t("进入总览")}
      onPrimary={() => navigate("/overview")}
      footerNote={running ? t("请保持此窗口打开") : undefined}
    >
      {task.progressTarget ? <DownloadProgress target={task.progressTarget} pending={running} /> : null}
      <LogDisclosure log={task.log || ""} open />
      {running ? (
        <button className="button button-secondary task-close-action" type="button" onClick={() => cancelTask(task.id)}>{t("取消任务")}</button>
      ) : (
        <button className="button button-secondary task-close-action" type="button" onClick={() => { dismissTask(task.id); navigate("/overview"); }}>{t("关闭任务")}</button>
      )}
    </PageScaffold>
  );
}

export { installTaskRoute };
