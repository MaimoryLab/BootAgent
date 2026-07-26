import type { LucideIcon } from "lucide-react";
import { Check } from "lucide-react";

interface ChoiceRowProps {
  selected: boolean;
  title: string;
  description: string;
  icon: LucideIcon;
  onSelect: () => void;
  detail?: string;
}

export function ChoiceRow({ selected, title, description, icon: Icon, onSelect, detail }: ChoiceRowProps) {
  return (
    <button className={`choice-row${selected ? " is-selected" : ""}`} type="button" onClick={onSelect}>
      <span className="choice-icon" aria-hidden="true">
        <Icon size={21} />
      </span>
      <span className="choice-copy">
        <strong>{title}</strong>
        <span>{description}</span>
        {detail ? <small>{detail}</small> : null}
      </span>
      <span className="choice-check" aria-hidden="true">{selected ? <Check size={15} /> : null}</span>
    </button>
  );
}
