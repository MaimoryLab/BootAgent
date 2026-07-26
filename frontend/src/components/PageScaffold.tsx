import { ArrowLeft, ArrowRight } from "lucide-react";
import type { PropsWithChildren, ReactNode } from "react";

import { SetupStepper } from "./SetupStepper";

interface PageScaffoldProps extends PropsWithChildren {
  title: string;
  description?: string;
  step?: number;
  footerNote?: ReactNode;
  backLabel?: string;
  onBack?: () => void;
  primaryLabel?: string;
  onPrimary?: () => void;
  primaryDisabled?: boolean;
  primaryBusy?: boolean;
  secondaryAction?: ReactNode;
  bodyClassName?: string;
}

export function PageScaffold({
  title,
  description,
  step,
  footerNote,
  backLabel = "返回",
  onBack,
  primaryLabel,
  onPrimary,
  primaryDisabled,
  primaryBusy,
  secondaryAction,
  bodyClassName = "",
  children,
}: PageScaffoldProps) {
  return (
    <section className="page-scaffold">
      <header className="page-header">
        <div>
          <h1>{title}</h1>
          {description ? <p>{description}</p> : null}
        </div>
        {step ? <SetupStepper current={step} /> : null}
      </header>
      <div className={`page-body ${bodyClassName}`}>{children}</div>
      <footer className="page-footer">
        <div className="page-footer-leading">
          {onBack ? (
            <button className="button button-secondary" type="button" onClick={onBack}>
              <ArrowLeft size={16} />
              {backLabel}
            </button>
          ) : null}
          {footerNote ? <div className="footer-note">{footerNote}</div> : null}
        </div>
        <div className="page-footer-actions">
          {secondaryAction}
          {primaryLabel && onPrimary ? (
            <button
              className="button button-primary"
              type="button"
              onClick={onPrimary}
              disabled={primaryDisabled || primaryBusy}
              aria-busy={primaryBusy}
            >
              {primaryBusy ? <span className="spinner spinner-on-blue" /> : null}
              {primaryLabel}
              {!primaryBusy ? <ArrowRight size={16} /> : null}
            </button>
          ) : null}
        </div>
      </footer>
    </section>
  );
}
