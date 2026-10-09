import { useMemo } from "react";

import { MultimodalSection } from "../components/MultimodalSection";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

/**
 * Multimodal capabilities get their own page: below a long Skill library the
 * section was out of sight. The Skills it installs still show up, and can be
 * retargeted, on the Skills page.
 */
export function MultimodalPage() {
  const { t } = useI18n();
  const { state } = useWizard();
  const agentLabels = useMemo(
    () => Object.fromEntries((state.status?.catalog ?? []).map((agent) => [agent.id, agent.name])),
    [state.status],
  );
  return (
    <PageScaffold title={t("多模态能力")} bodyClassName="multimodal-page">
      <MultimodalSection agentLabels={agentLabels} />
    </PageScaffold>
  );
}
