import { ChevronDown } from "lucide-react";
import { useState } from "react";
import type { PropsWithChildren } from "react";

/**
 * Collapsed-by-default container for options most users should not touch.
 *
 * Folding beats a simple/advanced mode switch: there is no wrong mode to get
 * stuck in, and the common path stays short. While collapsed it names what is
 * inside and says the defaults are fine — a bare triangle leaves people
 * wondering whether they skipped something they needed.
 */
export function AdvancedSection({
  label = "高级选项",
  hint,
  children,
}: PropsWithChildren<{ label?: string; hint?: string }>) {
  const [open, setOpen] = useState(false);
  return (
    <section className={`advanced-section${open ? " is-open" : ""}`}>
      <button type="button" onClick={() => setOpen((value) => !value)} aria-expanded={open}>
        {label}
        <ChevronDown size={15} aria-hidden="true" />
      </button>
      {!open && hint ? <p className="advanced-hint">{hint}</p> : null}
      {open ? <div className="advanced-body">{children}</div> : null}
    </section>
  );
}
