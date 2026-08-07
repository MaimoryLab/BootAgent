import { useNavigate, useParams } from "react-router-dom";

import { DownloadProgress } from "../components/DownloadProgress";
import { LogDisclosure } from "../components/LogDisclosure";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { installTaskRoute, useTaskCenter } from "../state/TaskCenterContext";

export function InstallTaskPage() {
  const { t } = useI18n();
  const navigate = useNavigate();
  const { agentId = "" } = useParams();
  const { tasks, cancelTask, dismissTask } = useTaskCenter();
  const target = decodeURIComponent(agentId);
  const task = tasks.find((item) => item.kind === "install" && item.target === target);
  if (!task) {
    return <PageScaffold title={t("暂无任务")} primaryLabel={t("进入总览")} onPrimary={() => navigate("/overview")} />;
  }
  const running = task.state === "running";
  const title = running ? t("正在安装") : task.state === "success" ? t("安装完成") : task.state === "cancelled" ? t("已取消") : t("需要处理部分问题");
  return (
    <PageScaffold
      title={`${title} · ${task.title}`}
      description={task.message || t("每个 Agent 的结果彼此独立，失败项可以单独重试")}
      primaryLabel={t("进入总览")}
      onPrimary={() => navigate("/overview")}
      footerNote={running ? t("请保持此窗口打开") : undefined}
    >
      {task.progressTarget ? <DownloadProgress target={task.progressTarget} pending={running} /> : null}
      <LogDisclosure log={task.log || ""} open={running} />
      {running ? (
        <button className="button button-secondary" type="button" onClick={() => cancelTask(task.id)}>{t("取消任务")}</button>
      ) : (
        <button className="button button-secondary" type="button" onClick={() => { dismissTask(task.id); navigate("/overview"); }}>{t("关闭任务")}</button>
      )}
    </PageScaffold>
  );
}

export { installTaskRoute };
