import { Check, RefreshCw } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import type { AgentStatus, ProfileSummary } from "../types/api";

/**
 * One-click switching between the Profiles that already cover this Agent.
 *
 * A Profile already is the "named bundle of Provider + model" that a quick
 * switcher needs, so this reuses them rather than inventing a second grouping
 * concept. Only Profiles listing this Agent are offered: applying one that does
 * not would silently widen its scope.
 *
 * The switch writes immediately with no connection test. That is the point of a
 * quick switch, and it is safe to offer because ActivateAgent backs the config
 * file up before replacing it — but it means a Profile whose key is missing
 * fails at the backend, so those are disabled here instead.
 */
export function AgentQuickSwitch({
  agentId,
  status,
  profiles,
  onSwitched,
}: {
  agentId: string;
  status: AgentStatus;
  profiles: ProfileSummary[];
  onSwitched: () => void | Promise<void>;
}) {
  const { t } = useI18n();
  const [pending, setPending] = useState("");
  const [failure, setFailure] = useState("");

  const candidates = profiles.filter((profile) => profile.agentIds?.includes(agentId));
  // One Profile means the only option is the current state; nothing to switch to.
  if (candidates.length < 2) return null;

  const switchTo = async (profile: ProfileSummary) => {
    setPending(profile.id);
    setFailure("");
    try {
      await api.activateAgent(agentId, {
        provider: profile.provider,
        apiBaseUrl: "",
        // Left empty on purpose: the backend resolves the key from the Profile
        // secret or the Provider record, so the switch never handles a plaintext
        // key and never needs the user to re-enter one.
        apiKey: "",
        model: profile.model || "",
        profileId: profile.id,
        smallFastModel: "",
      });
      await onSwitched();
    } catch (error) {
      setFailure(describeError(error, t("切换失败")).message);
    } finally {
      setPending("");
    }
  };

  return (
    <div className="quick-switch">
      <span className="quick-switch-label">{t("快速切换")}</span>
      <div className="quick-switch-options" role="group" aria-label={t("快速切换")}>
        {candidates.map((profile) => {
          const active = status.profileId === profile.id;
          // hasKey covers the Profile's own secret; a Provider key would also do,
          // but the summary cannot see it, so this stays conservative.
          const usable = profile.hasKey && Boolean(profile.model);
          return (
            <button
              key={profile.id}
              type="button"
              className={`quick-switch-option${active ? " is-active" : ""}`}
              onClick={() => void switchTo(profile)}
              disabled={active || Boolean(pending) || !usable}
              aria-pressed={active}
              title={usable ? `${profile.provider} · ${profile.model}` : t("这个 Profile 还缺少 Key 或模型")}
            >
              {pending === profile.id ? <RefreshCw size={13} className="spin" /> : null}
              {active && !pending ? <Check size={13} /> : null}
              {profile.label || profile.id}
            </button>
          );
        })}
      </div>
      {failure ? <small className="quick-switch-error">{failure}</small> : null}
    </div>
  );
}
