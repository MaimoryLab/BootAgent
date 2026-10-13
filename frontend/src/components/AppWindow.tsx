import { type PropsWithChildren, useEffect, useRef } from "react";

import { NavigationSidebar } from "./NavigationSidebar";
import { useFeedback } from "../state/FeedbackContext";
import { useWizard } from "../state/WizardContext";
import { useI18n } from "../i18n";

/** The prefix internal/app/status.go puts on a migration that failed. */
const MIGRATION_FAILED_PREFIX = "无法迁移";

export function AppWindow({ children }: PropsWithChildren) {
  const { t } = useI18n();
  const { state } = useWizard();
  const feedback = useFeedback();
  const notice = state.status?.migrationNotice ?? "";
  // The notice is in every status poll for the life of the process. It used to
  // be a paragraph above every page, taking height from each of them; it is now
  // one receipt, shown once, that stays until the user closes it.
  const shown = useRef("");
  useEffect(() => {
    if (!notice || shown.current === notice) return;
    shown.current = notice;
    const failed = notice.startsWith(MIGRATION_FAILED_PREFIX);
    feedback.show({
      key: "migration",
      tone: failed ? "failure" : "warning",
      message: failed ? t("没能迁移旧版配置") : t("已迁移旧版配置"),
      detail: notice,
    });
  }, [notice, feedback, t]);
  return (
    <div className="desktop-stage" onDragStart={(event) => event.preventDefault()}>
      <div className="app-window">
        <NavigationSidebar />
        <main className="app-main">{children}</main>
      </div>
    </div>
  );
}
