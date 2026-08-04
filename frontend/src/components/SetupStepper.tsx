import { Check } from "lucide-react";
import { useLocation } from "react-router-dom";

import { type TranslationKey, useI18n } from "../i18n";

// Single source of truth for the onboarding sequence: order and labels. Pages
// no longer pass step numbers; the current step is derived from the route.
const steps: Array<{ path: string; label: TranslationKey | "Agent" | "Provider" }> = [
  { path: "/setup/agents", label: "Agent" },
  { path: "/setup/provider", label: "Provider" },
  { path: "/setup/model", label: "模型" },
  { path: "/setup/review", label: "确认" },
  { path: "/setup/activation", label: "安装" },
];

const desktopSteps: Array<{ path: string; label: TranslationKey | "Agent" }> = [
  { path: "/setup/desktop/agents", label: "Agent" },
  { path: "/setup/desktop/profile", label: "配置模板" },
  { path: "/setup/desktop/install", label: "安装" },
];

export function SetupStepper() {
  const { t } = useI18n();
  const { pathname } = useLocation();
  const desktop = pathname.startsWith("/setup/desktop/");
  const activeSteps = desktop ? desktopSteps : steps;
  const current = activeSteps.findIndex((step) => step.path === pathname) + 1;

  return (
    <ol className="setup-stepper" aria-label={t("激活步骤")}>
      {activeSteps.map((step, index) => {
        const number = index + 1;
        const complete = number < current;
        const active = number === current;
        return (
          <li
            key={step.label}
            className={`stepper-item${active ? " is-active" : ""}${complete ? " is-complete" : ""}`}
            aria-current={active ? "step" : undefined}
          >
            <span className="stepper-marker">{complete ? <Check size={14} /> : number}</span>
            <span className="stepper-label">{step.label === "Agent" || step.label === "Provider" ? step.label : t(step.label)}</span>
          </li>
        );
      })}
    </ol>
  );
}
