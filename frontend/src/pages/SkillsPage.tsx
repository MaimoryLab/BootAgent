import { FolderOpen, RefreshCw, RotateCcw, Sparkles, Trash2, Upload, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { api } from "../backend/api";
import type { SkillCandidate, SkillChange, SkillSummary } from "../types/api";
import { AgentSummaryBar } from "../components/AgentSummaryBar";
import { AgentTargetGroup } from "../components/AgentTargetGroup";
import { EmptyState } from "../components/EmptyState";
import { ManagementSearch } from "../components/ManagementSearch";
import { ModalDialog } from "../components/ModalDialog";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";
import { useWizard } from "../state/WizardContext";

const compactDescription = (value: string, limit = 100) => {
  const chars = Array.from(value);
  return chars.length > limit ? `${chars.slice(0, limit).join("")}...` : value;
};

export function filterSkillRows(rows: SkillSummary[], query: string): SkillSummary[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => [row.id, row.name, row.description].some((value) => value?.toLowerCase().includes(needle)));
}

/** Agents each visible row currently targets, draft state included. */
export function skillSelectedAgents(row: SkillSummary, changes: Record<string, SkillChange>): string[] {
  const change = changes[row.id];
  const agents = new Set(change?.targets ?? row.agents ?? []);
  Object.values(changes)
    .filter((entry) => entry.delete && entry.id === row.id)
    .forEach((entry) => (entry.targets ?? []).forEach((agent) => agents.delete(agent)));
  return [...agents];
}

/**
 * Pure draft transition for one (row, agent) toggle. Extracted from the page
 * so the merge is testable: deselecting a scanned agent records a removal
 * entry, anything else edits the row's target set, and repeated or bulk
 * applications converge on the same draft.
 */
export function applySkillTarget(
  current: Record<string, SkillChange>,
  row: { id: string; hash: string; scannedAgents: string[] },
  agent: string,
  checked: boolean,
): Record<string, SkillChange> {
  const { id, hash, scannedAgents } = row;
  const removalKey = `${id}::remove::${agent}`;
  if (!checked && scannedAgents.includes(agent)) {
    return { ...current, [removalKey]: { id, variant_hash: hash, targets: [agent], delete: true } };
  }
  const next = { ...current };
  delete next[removalKey];
  const prior = next[id] ?? { id, variant_hash: hash, targets: scannedAgents };
  const targets = new Set(prior.targets ?? []);
  checked ? targets.add(agent) : targets.delete(agent);
  next[id] = { ...prior, variant_hash: hash, targets: [...targets] };
  return next;
}

export function SkillsPage() {
  const { t, locale } = useI18n();
  const { state } = useWizard();
  const [rows, setRows] = useState<SkillSummary[]>([]);
  const [candidates, setCandidates] = useState<SkillCandidate[]>([]);
  const [eligible, setEligible] = useState<string[]>([]);
  const [token, setToken] = useState("");
  const [changes, setChanges] = useState<Record<string, SkillChange>>({});
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, boolean>>({});
  const [pendingDeletes, setPendingDeletes] = useState<Record<string, boolean>>({});
  const [pendingDeleteTargets, setPendingDeleteTargets] = useState<Record<string, string[]>>({});

  const agentLabels = useMemo(
    () => Object.fromEntries((state.status?.catalog ?? []).map((agent) => [agent.id, agent.name])),
    [state.status],
  );

  const dirty = Object.keys(changes).length > 0 || Object.keys(pendingDeletes).length > 0;
  const dirtyCount = new Set([
    ...Object.values(changes).map((change) => change.id),
    ...Object.keys(pendingDeletes),
  ]).size;
  useEffect(() => { void api.listSkills().then(setRows); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }); }, []);
  useEffect(() => { void api.setSkillDraftState(dirty, locale); }, [dirty, locale]);

  const scan = () => { setBusy(true); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }).finally(() => setBusy(false)); };
  const importSource = (source: string) => { setBusy(true); void api.previewSkillImport(source).then((r) => { const next = r.candidates ?? []; setCandidates(next); setSelectedCandidates(Object.fromEntries(next.map((item) => [item.id, true]))); setToken(r.token); setPreviewOpen(true); }).finally(() => setBusy(false)); };
  const candidateMap = useMemo(() => new Map(candidates.map((item) => [item.id, item])), [candidates]);
  const changesForApply = () => [...Object.entries(changes).map(([key, change]) => key.includes("::remove::") ? { ...change, id: change.id, delete: true } : change), ...Object.keys(pendingDeletes).map((id) => ({ id, variant_hash: rows.find((row) => row.id === id)?.variant_hashes?.[0] ?? "", targets: pendingDeleteTargets[id] ?? [], delete: true, delete_skill: true }))];
  const apply = () => {
    const pendingRemovals = Object.entries(changes).filter(([key, change]) => key.includes("::remove::") && change.delete).map(([, change]) => change);
    const pendingDeleteIDs = new Set(Object.keys(pendingDeletes));
    setRows((current) => current.filter((row) => !pendingDeleteIDs.has(row.id)).map((row) => {
      const agents = new Set(row.agents ?? []);
      pendingRemovals.filter((change) => change.id === row.id).forEach((change) => (change.targets ?? []).forEach((agent) => agents.delete(agent)));
      return { ...row, agents: [...agents] };
    }));
    setBusy(true); void api.applySkills({ preview_token: token || undefined, changes: changesForApply() }).then((r) => { const successful = new Set((r.results ?? []).filter((item) => item.registry_updated).map((item) => item.agent)); if (successful.size) setChanges((current) => Object.fromEntries(Object.entries(current).filter(([, change]) => !change.targets?.some((agent) => successful.has(agent))))); if (!(r.results ?? []).some((item) => item.error)) { setPendingDeletes({}); setPendingDeleteTargets({}); } scan(); }).finally(() => setBusy(false));
  };
  const selectTarget = (id: string, hash: string, agent: string, checked: boolean, existingTargets: string[] = []) =>
    setChanges((current) => applySkillTarget(current, { id, hash, scannedAgents: existingTargets }, agent, checked));
  const toggleDelete = (id: string, agents: string[]) => setPendingDeletes((current) => {
    const next = { ...current, [id]: !current[id] };
    if (!current[id]) setPendingDeleteTargets((targets) => ({ ...targets, [id]: [...agents] }));
    else setPendingDeleteTargets((targets) => { const nextTargets = { ...targets }; delete nextTargets[id]; return nextTargets; });
    return next;
  });
  const confirmImport = () => {
    const imported = candidates.filter((item) => selectedCandidates[item.id]);
    const next = imported.map((item) => changes[item.id] ?? { id: item.id, variant_hash: item.hash, targets: [] });
    setChanges((current) => ({ ...current, ...Object.fromEntries(next.map((change) => [change.id, change])) }));
    setPreviewOpen(false);
  };
  const applyImport = () => {
    const imported = candidates.filter((item) => selectedCandidates[item.id]);
    const targetChanges = imported.map((item) => changes[item.id]).filter((change): change is SkillChange => Boolean(change && change.targets?.length));
    if (!targetChanges.length) { setMessage(t("请选择至少一个目标 Agent")); return; }
    setBusy(true);
    void api.applySkills({ preview_token: token, changes: targetChanges }).then((result) => {
      const failed = (result.results ?? []).filter((item) => item.error);
      setMessage(failed.length ? failed.map((item) => `${item.agent || "Skill"}: ${item.error}`).join("; ") : t("导入并应用完成"));
      setCandidates([]); setToken(""); setSelectedCandidates({}); setChanges({}); setPreviewOpen(false); scan();
    }).finally(() => setBusy(false));
  };

  const visibleRows = useMemo(() => filterSkillRows(rows, query), [rows, query]);
  const summaryRows = useMemo(() => visibleRows.filter((row) => !pendingDeletes[row.id]), [visibleRows, pendingDeletes]);
  const summaryCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const agent of eligible) counts[agent] = summaryRows.filter((row) => skillSelectedAgents(row, changes).includes(agent)).length;
    return counts;
  }, [eligible, summaryRows, changes]);
  const bulkToggle = (agent: string, enabled: boolean) => {
    // One setChanges call folding every row, so the whole bulk edit is a
    // single state transition computed against one consistent draft.
    setChanges((current) => {
      let next = current;
      for (const row of summaryRows) {
        const hash = candidateMap.get(row.id)?.hash ?? current[row.id]?.variant_hash ?? row.variant_hashes?.[0] ?? "";
        if (!hash) continue;
        if (skillSelectedAgents(row, next).includes(agent) === enabled) continue;
        next = applySkillTarget(next, { id: row.id, hash, scannedAgents: row.agents ?? [] }, agent, enabled);
      }
      return next;
    });
  };

  return <PageScaffold title={t("Skills")} description={t("管理本机 Skills 并同步到 Agent")} bodyClassName="skills-page" secondaryAction={<><button className="button button-secondary" onClick={scan} disabled={busy} title={t("重新扫描 Skills")}><RefreshCw size={16} />{t("重新扫描 Skills")}</button><button className="button button-secondary" onClick={() => importSource("folder")} disabled={busy} title={t("导入文件夹")}><FolderOpen size={16} />{t("导入文件夹")}</button><button className="button button-secondary" onClick={() => importSource("zip")} disabled={busy} title={t("导入 ZIP")}><Upload size={16} />{t("导入 ZIP")}</button></>} primaryLabel={dirtyCount ? t("应用 Skills（{count}）", { count: dirtyCount }) : t("应用 Skills")} onPrimary={apply} primaryDisabled={!dirty || busy}>
    <section className="content-section skills-page"><div className="section-heading"><div><h2><Sparkles size={18} /> Skills</h2>{message ? <p>{message}</p> : null}</div></div>
      {rows.length ? <div className="management-toolbar"><ManagementSearch value={query} onValueChange={setQuery} placeholder={t("搜索 Skills")} /><AgentSummaryBar agents={eligible} counts={summaryCounts} total={summaryRows.length} onToggleAll={bulkToggle} disabled={busy} labels={agentLabels} /></div> : null}
      {!rows.length ? <EmptyState icon={Sparkles} title={t("没有发现 Skills")} hint={t("通过导入文件夹或导入 ZIP 添加第一个 Skill")} /> : !visibleRows.length ? <EmptyState icon={Sparkles} title={t("没有匹配的 Skills")} hint={t("换一个关键词，或清空搜索")} /> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>{t("名称")}</th><th>{t("详情")}</th><th>{t("状态")}</th><th>{t("选择目标 Agent")}</th><th /></tr></thead><tbody>{visibleRows.map((row) => { const candidate = candidateMap.get(row.id); const hash = candidate?.hash ?? changes[row.id]?.variant_hash ?? row.variant_hashes?.[0] ?? ""; const detail = row.description || t("暂无详情"); const isDeleted = Boolean(pendingDeletes[row.id]); return <tr key={row.id} className={isDeleted ? "mcp-row-deleted" : undefined}><td className="skills-name-cell"><strong>{row.name || row.id}</strong></td><td className="skills-detail-cell"><span title={detail}>{compactDescription(detail)}</span><small>{row.id}</small></td><td>{isDeleted ? <span className="status-badge status-warning">{t("待删除")}</span> : row.conflict ? <span className="status-badge status-warning">{t("冲突")}</span> : <span className="status-badge status-success">{row.variants} variant</span>}</td><td><AgentTargetGroup agents={eligible} selected={skillSelectedAgents(row, changes)} disabled={!hash || isDeleted || busy} labels={agentLabels} onToggle={(agent, checked) => selectTarget(row.id, hash, agent, checked, row.agents ?? [])} /></td><td className="mcp-row-actions"><button className={`icon-button row-action${isDeleted ? " is-restore" : " is-danger"}`} onClick={() => toggleDelete(row.id, row.agents ?? [])} disabled={busy} title={isDeleted ? t("恢复") : t("删除")}>{isDeleted ? <RotateCcw size={16} /> : <Trash2 size={16} />}</button></td></tr>; })}</tbody></table></div>}
    </section>
    {previewOpen ? <ModalDialog className="mcp-modal skills-import-modal" label={t("导入预览")} onDismiss={() => setPreviewOpen(false)}><header><h2>{t("导入预览")}</h2><button className="icon-button" onClick={() => setPreviewOpen(false)} title={t("取消")}><X size={18} /></button></header><p>{t("选择要导入的 Skill 和目标 Agent")}</p>{candidates.map((candidate) => { const change = changes[candidate.id] ?? { id: candidate.id, variant_hash: candidate.hash, targets: [] }; const detail = candidate.description || candidate.hash.slice(0, 12); return <div className="skills-import-row" key={`${candidate.id}-${candidate.hash}`}><label className="skills-import-name"><input type="checkbox" checked={selectedCandidates[candidate.id] ?? false} onChange={(event) => setSelectedCandidates((current) => ({ ...current, [candidate.id]: event.target.checked }))} /><strong>{candidate.name || candidate.id}</strong><small title={detail}>{compactDescription(detail)}</small></label><AgentTargetGroup agents={eligible} selected={change.targets ?? []} disabled={!selectedCandidates[candidate.id]} labels={agentLabels} onToggle={(agent, checked) => selectTarget(candidate.id, candidate.hash, agent, checked)} /></div>; })}<footer><button className="button button-secondary" onClick={() => setPreviewOpen(false)}>{t("取消")}</button><button className="button button-secondary" onClick={confirmImport} disabled={!candidates.some((item) => selectedCandidates[item.id])}>{t("确认导入")}</button><button className="button button-primary" onClick={applyImport} disabled={busy}>{busy ? t("应用中") : t("确认导入并应用")}</button></footer></ModalDialog> : null}
  </PageScaffold>;
}
