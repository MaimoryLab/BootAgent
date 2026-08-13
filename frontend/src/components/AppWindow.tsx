import type { PropsWithChildren } from "react";

import { NavigationSidebar } from "./NavigationSidebar";
import { useWizard } from "../state/WizardContext";
import { useI18n } from "../i18n";

export function AppWindow({ children }: PropsWithChildren) {
  const { t } = useI18n();
  const { state } = useWizard();
  return (
    <div className="desktop-stage">
      <div className="app-window">
        <NavigationSidebar />
        <main className="app-main">
          {state.status?.migrationNotice ? <p className="provider-note">{state.status.migrationNotice}</p> : null}
          {children}
        </main>
      </div>
    </div>
  );
}
