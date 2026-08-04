import { Check, RefreshCw } from "lucide-react";
import { useState } from "react";

import { api, describeError } from "../backend/api";
import { useI18n } from "../i18n";
import type { AgentStatus, ProfileSummary, StatusResponse } from "../types/api";

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
  providers,
  onSwitched,
}: {
  agentId: string;
  status: AgentStatus;
  profiles: ProfileSummary[];
  providers?: StatusResponse["providers"];
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
        // Left empty on purpose: the backend resolves the key from the Provider
        // (with a legacy Profile-secret fallback), so this switch never handles
        // a plaintext key or asks the user to re-enter one.
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
          const providerHasKey = Boolean(providers?.[profile.provider]?.has_key);
          // A Provider key is authoritative; hasKey only covers old Profiles
          // that still have a legacy secret file.
          const usable = Boolean(profile.model) && (providerHasKey || profile.hasKey);
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
