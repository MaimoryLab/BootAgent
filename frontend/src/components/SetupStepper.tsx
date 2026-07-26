import { Check } from "lucide-react";

import { useWizard } from "../state/WizardContext";

const steps = ["Agent", "配置", "Provider", "模型", "确认"];

export function SetupStepper({ current }: { current: number }) {
  const { state } = useWizard();
  const skipsProvider = state.configMode === "existing-account";

  return (
    <ol className="setup-stepper" aria-label="激活步骤">
      {steps.map((label, index) => {
        const number = index + 1;
        const skipped = skipsProvider && (number === 3 || number === 4);
        const complete = number < current && !skipped;
        const active = number === current;
        return (
          <li
            key={label}
            className={`stepper-item${active ? " is-active" : ""}${complete ? " is-complete" : ""}${skipped ? " is-skipped" : ""}`}
            aria-current={active ? "step" : undefined}
          >
            <span className="stepper-marker">{complete ? <Check size={14} /> : number}</span>
            <span className="stepper-label">{skipped ? "已跳过" : label}</span>
          </li>
        );
      })}
    </ol>
  );
}
