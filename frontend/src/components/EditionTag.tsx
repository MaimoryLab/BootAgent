import { useI18n } from "../i18n";

/**
 * A small region label for products that ship as two separate applications.
 *
 * This is deliberately not part of the Agent's name. The name also travels into
 * error messages, install progress and logs, where "WorkBuddy AI" is what the
 * vendor calls the product and a bolted-on "(International)" would be noise. The
 * region only matters when the two are on screen together, which is exactly
 * where this renders.
 *
 * Renders nothing for single-build products, so callers do not have to check.
 */
export function EditionTag({ edition }: { edition?: string }) {
  const { t } = useI18n();
  if (edition !== "cn" && edition !== "intl") return null;
  const label = edition === "cn" ? t("国内版") : t("国际版");
  return <small className="edition-tag">{label}</small>;
}
