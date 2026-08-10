import { Check } from "lucide-react";
import { useLocation } from "react-router-dom";

import { type TranslationKey, useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

// Single source of truth for the onboarding sequence: order and labels. Pages
// no longer pass step numbers; the current step is derived from the route.
const steps: Array<{ path: string; label: TranslationKey | "Agent" }> = [
  { path: "/setup/agents", label: "Agent" },
  { path: "/setup/profile", label: "选择配置模版" },
  { path: "/setup/provider", label: "模型服务" },
  { path: "/setup/model", label: "模型" },
  { path: "/setup/review", label: "确认" },
  { path: "/setup/activation", label: "安装" },
];

export function SetupStepper() {
  const { t } = useI18n();
  const { pathname } = useLocation();
  const { state } = useWizard();
  const current = steps.findIndex((step) => step.path === pathname) + 1;

  return (
    <ol className="setup-stepper" aria-label={t("激活步骤")}>
      {steps.map((step, index) => {
        const number = index + 1;
        const skipped = (state.profileStepSkipped && step.path === "/setup/profile")
          || (state.reusedProfile && (step.path === "/setup/provider" || step.path === "/setup/model"));
        const complete = number < current && !skipped;
        const active = number === current;
        return (
          <li
            key={step.label}
            className={`stepper-item${active ? " is-active" : ""}${complete ? " is-complete" : ""}${skipped ? " is-skipped" : ""}`}
            aria-current={active ? "step" : undefined}
          >
            <span className="stepper-marker">{complete ? <Check size={14} /> : number}</span>
            <span className="stepper-label">{step.label === "Agent" ? step.label : t(step.label)}</span>
          </li>
        );
      })}
    </ol>
  );
}
