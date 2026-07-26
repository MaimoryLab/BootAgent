import { Check } from "lucide-react";
import { useLocation } from "react-router-dom";

import { useWizard } from "../state/WizardContext";

// Single source of truth for the setup sequence: order, labels, and which
// steps the existing-account mode skips. Pages no longer pass step numbers;
// the current step is derived from the route.
const steps = [
  { path: "/setup/agents", label: "Agent", skippedForExistingAccount: false },
  { path: "/setup/mode", label: "配置", skippedForExistingAccount: false },
  { path: "/setup/provider", label: "Provider", skippedForExistingAccount: true },
  { path: "/setup/model", label: "模型", skippedForExistingAccount: true },
  { path: "/setup/review", label: "确认", skippedForExistingAccount: false },
];

export function SetupStepper() {
  const { pathname } = useLocation();
  const { state } = useWizard();
  const skipsProvider = state.configMode === "existing-account";
  const current = steps.findIndex((step) => step.path === pathname) + 1;

  return (
    <ol className="setup-stepper" aria-label="激活步骤">
      {steps.map((step, index) => {
        const number = index + 1;
        const skipped = skipsProvider && step.skippedForExistingAccount;
        const complete = number < current && !skipped;
        const active = number === current;
        return (
          <li
            key={step.label}
            className={`stepper-item${active ? " is-active" : ""}${complete ? " is-complete" : ""}${skipped ? " is-skipped" : ""}`}
            aria-current={active ? "step" : undefined}
          >
            <span className="stepper-marker">{complete ? <Check size={14} /> : number}</span>
            <span className="stepper-label">{skipped ? "已跳过" : step.label}</span>
          </li>
        );
      })}
    </ol>
  );
}
