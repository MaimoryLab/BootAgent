import { Check, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";
import { desktopProtocol, profileAgentIdForDesktop, selectedDesktopApp } from "../state/desktopSetup";
import { byProfileCreatedAt } from "../state/ranking";

export function ProfileSelectionPage() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { state, dispatch } = useWizard();
  const [selectedId, setSelectedId] = useState("");
  const desktop = state.setupKind === "desktop" && state.status
    ? selectedDesktopApp(state.status, state.selectedAgentIds)
    : undefined;
  const agentId = desktop
    ? profileAgentIdForDesktop(desktop)
    : state.selectedAgentIds[0] || "";
  const protocol = desktop && state.status
    ? desktopProtocol(desktop)
    : state.status?.catalog.find((agent) => agent.id === agentId)?.protocol;
  const profiles = useMemo(
    () => byProfileCreatedAt(state.status?.profiles.filter((profile) => profile.protocol === protocol && profile.model?.trim()) ?? []),
    [protocol, state.status?.profiles],
  );
  const selected = profiles.find((profile) => profile.id === selectedId);

  useEffect(() => {
    if (!selectedId && profiles.length) setSelectedId(profiles[0].id);
    if (state.status && !profiles.length) navigate("/setup/provider", { replace: true });
  }, [navigate, profiles, selectedId, state.status]);

  if (!state.status || !profiles.length) {
    return null;
  }
  const status = state.status;

  const choose = () => {
    if (!selected) return;
    dispatch({
      type: "SELECT_PROFILE",
      provider: selected.provider,
      profileId: selected.id,
      profileLabel: selected.label,
      model: selected.model || "",
      keyVerified: Boolean(status.providers[selected.provider]?.has_key),
    });
    navigate("/setup/review");
  };

  return (
    <PageScaffold
      title={t("Profile选择")}
      description={t("选择一个已有 Profile，或新建 Profile")}
      stepper
      onBack={() => navigate("/setup/agents")}
      primaryLabel={t("继续")}
      onPrimary={choose}
      primaryDisabled={!selected}
      secondaryAction={<button className="button button-secondary" type="button" onClick={() => { dispatch({ type: "START_NEW_PROFILE" }); navigate("/setup/provider"); }}><Plus size={15} />{t("新建 Profile")}</button>}
    >
      <div className="profile-list">
        {profiles.map((profile) => {
          const active = selectedId === profile.id;
          return (
            <article className={`profile-card profile-choice${active ? " is-selected" : ""}`} key={profile.id}>
              <label className="profile-choice-main">
                <input type="radio" name="setup-profile" checked={active} onChange={() => setSelectedId(profile.id)} aria-label={t("选择 {name}", { name: profile.label })} />
                <span className="profile-title"><strong>{profile.label}</strong><small>{profile.id}</small></span>
                {active ? <Check size={16} aria-hidden="true" /> : null}
              </label>
              <p>{status.providers[profile.provider]?.name || profile.provider} · {profile.model}</p>
            </article>
          );
        })}
      </div>
    </PageScaffold>
  );
}
