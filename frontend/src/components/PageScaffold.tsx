import { ArrowLeft, ArrowRight } from "lucide-react";
import type { PropsWithChildren, ReactNode } from "react";

import { useI18n } from "../i18n";
import { SetupStepper } from "./SetupStepper";

interface PageScaffoldProps extends PropsWithChildren {
  title: string;
  /** Short count beside the title, for deferrable news that has not earned a
   *  banner of its own. Empty string renders nothing. */
  titleBadge?: string;
  description?: string;
  /** Show the setup stepper; it derives the current step from the route. */
  stepper?: boolean;
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
  titleBadge,
  description,
  stepper,
  footerNote,
  backLabel,
  onBack,
  primaryLabel,
  onPrimary,
  primaryDisabled,
  primaryBusy,
  secondaryAction,
  bodyClassName = "",
  children,
}: PageScaffoldProps) {
  const { t } = useI18n();
  return (
    <section className="page-scaffold">
      <header className="page-header">
        <div>
          <h1>
            {title}
            {titleBadge ? <span className="title-badge">{titleBadge}</span> : null}
          </h1>
          {description ? <p>{description}</p> : null}
        </div>
        {stepper ? <SetupStepper /> : null}
      </header>
      <div className={`page-body ${bodyClassName}`}>{children}</div>
      <footer className="page-footer">
        <div className="page-footer-leading">
          {onBack ? (
            <button className="button button-secondary" type="button" onClick={onBack}>
              <ArrowLeft size={16} />
              {backLabel ?? t("返回")}
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
