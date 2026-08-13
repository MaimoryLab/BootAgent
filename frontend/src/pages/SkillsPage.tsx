import { FolderOpen, RefreshCw, Sparkles, Trash2, Upload } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { api } from "../backend/api";
import type { SkillCandidate, SkillChange, SkillSummary } from "../types/api";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";

const compactDescription = (value: string, limit = 100) => {
  const chars = Array.from(value);
  return chars.length > limit ? `${chars.slice(0, limit).join("")}...` : value;
};

export function SkillsPage() {
  const { t, locale } = useI18n();
  const [rows, setRows] = useState<SkillSummary[]>([]);
  const [candidates, setCandidates] = useState<SkillCandidate[]>([]);
  const [eligible, setEligible] = useState<string[]>([]);
  const [token, setToken] = useState("");
  const [changes, setChanges] = useState<Record<string, SkillChange>>({});
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [selectedCandidates, setSelectedCandidates] = useState<Record<string, boolean>>({});

  const dirty = Object.keys(changes).length > 0;
  useEffect(() => { void api.listSkills().then(setRows); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }); }, []);
  useEffect(() => { void api.setSkillDraftState(dirty, locale); }, [dirty, locale]);

  const scan = () => { setBusy(true); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }).finally(() => setBusy(false)); };
  const importSource = (source: string) => { setBusy(true); void api.previewSkillImport(source).then((r) => { const next = r.candidates ?? []; setCandidates(next); setSelectedCandidates(Object.fromEntries(next.map((item) => [item.id, true]))); setToken(r.token); setPreviewOpen(true); }).finally(() => setBusy(false)); };
  const candidateMap = useMemo(() => new Map(candidates.map((item) => [item.id, item])), [candidates]);
  const changesForApply = () => Object.entries(changes).map(([key, change]) => key.includes("::remove::") ? { ...change, id: change.id, delete: true } : change);
  const apply = () => { setBusy(true); void api.applySkills({ preview_token: token || undefined, changes: changesForApply() }).then((r) => { const successful = new Set((r.results ?? []).filter((item) => item.registry_updated).map((item) => item.agent)); if (successful.size) setChanges((current) => Object.fromEntries(Object.entries(current).filter(([, change]) => !change.targets?.some((agent) => successful.has(agent))))); scan(); }).finally(() => setBusy(false)); };
  const selectTarget = (id: string, hash: string, agent: string, checked: boolean, existing = false, existingTargets: string[] = []) => setChanges((current) => {
    const removalKey = `${id}::remove::${agent}`;
    if (!checked && existing) return { ...current, [removalKey]: { id, variant_hash: hash, targets: [agent], delete: true } };
    const next = { ...current };
    delete next[removalKey];
    const prior = next[id] ?? { id, variant_hash: hash, targets: existingTargets };
    const targets = new Set(prior.targets ?? []);
    checked ? targets.add(agent) : targets.delete(agent);
    next[id] = { ...prior, variant_hash: hash, targets: [...targets] };
    return next;
  });
  const uninstall = (id: string) => { setBusy(true); void api.uninstallSkill(id).then((result) => { setMessage(result.error || (result.backup_id ? `${t("备份")}: ${result.backup_id}` : "")); scan(); }).finally(() => setBusy(false)); };
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

  return <PageScaffold title={t("Skills")} description={t("管理本机 Skills 并同步到 Agent")} bodyClassName="skills-page" secondaryAction={<><button className="button button-secondary" onClick={scan} disabled={busy} title={t("重新扫描 Skills")}><RefreshCw size={16} />{t("重新扫描 Skills")}</button><button className="button button-secondary" onClick={() => importSource("folder")} disabled={busy} title={t("导入文件夹")}><FolderOpen size={16} />{t("导入文件夹")}</button><button className="button button-secondary" onClick={() => importSource("zip")} disabled={busy} title={t("导入 ZIP")}><Upload size={16} />{t("导入 ZIP")}</button></>} primaryLabel={t("应用 Skills")} onPrimary={apply} primaryDisabled={!dirty || busy}>
    <section className="content-section skills-page"><div className="section-heading"><div><h2><Sparkles size={18} /> Skills</h2>{message ? <p>{message}</p> : null}</div></div>
      {candidates.length ? <div className="skills-candidates"><strong>{t("导入预览")}</strong>{candidates.map((candidate) => <span key={`${candidate.id}-${candidate.hash}`} className="status-badge status-info">{candidate.id} · {candidate.hash.slice(0, 8)} · {candidate.files} files</span>)}</div> : null}
      {!rows.length ? <div className="empty-overview">{t("没有发现 Skills")}</div> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>{t("名称")}</th><th>{t("详情")}</th><th>{t("状态")}</th><th>{t("选择目标 Agent")}</th><th /></tr></thead><tbody>{rows.map((row) => { const candidate = candidateMap.get(row.id); const hash = candidate?.hash ?? changes[row.id]?.variant_hash ?? row.variant_hashes?.[0] ?? ""; const change = changes[row.id]; const detail = row.description || t("暂无详情"); return <tr key={row.id}><td className="skills-name-cell"><strong>{row.name || row.id}</strong></td><td className="skills-detail-cell"><span title={detail}>{compactDescription(detail)}</span><small>{row.id}</small></td><td>{row.conflict ? <span className="status-badge status-warning">{t("冲突")}</span> : <span className="status-badge status-success">{row.variants} variant</span>}</td><td><div className="mcp-targets">{eligible.map((agent) => { const selected = change?.targets?.includes(agent) ?? row.agents?.includes(agent) ?? false; return <label key={agent}><input type="checkbox" checked={selected} disabled={!hash} onChange={(event) => selectTarget(row.id, hash, agent, event.target.checked, row.agents?.includes(agent) ?? false, row.agents ?? [])} />{agent}</label>; })}</div></td><td><button className="icon-button is-danger" onClick={() => uninstall(row.id)} disabled={busy} title={t("卸载")}><Trash2 size={16} /></button></td></tr>; })}</tbody></table></div>}
    </section>
    {previewOpen ? <div className="mcp-modal-backdrop"><div className="mcp-modal skills-import-modal" role="dialog" aria-modal="true"><header><h2>{t("导入预览")}</h2><button className="icon-button" onClick={() => setPreviewOpen(false)} title={t("取消")}>×</button></header><p>{t("选择要导入的 Skill 和目标 Agent")}</p>{candidates.map((candidate) => { const change = changes[candidate.id] ?? { id: candidate.id, variant_hash: candidate.hash, targets: [] }; const detail = candidate.description || candidate.hash.slice(0, 12); return <div className="skills-import-row" key={`${candidate.id}-${candidate.hash}`}><label className="skills-import-name"><input type="checkbox" checked={selectedCandidates[candidate.id] ?? false} onChange={(event) => setSelectedCandidates((current) => ({ ...current, [candidate.id]: event.target.checked }))} /><strong>{candidate.name || candidate.id}</strong><small title={detail}>{compactDescription(detail)}</small></label><div className="mcp-targets">{eligible.map((agent) => <label key={agent}><input type="checkbox" checked={change.targets?.includes(agent) ?? false} disabled={!selectedCandidates[candidate.id]} onChange={(event) => selectTarget(candidate.id, candidate.hash, agent, event.target.checked)} />{agent}</label>)}</div></div>; })}<footer><button className="button button-secondary" onClick={() => setPreviewOpen(false)}>{t("取消")}</button><button className="button button-secondary" onClick={confirmImport} disabled={!candidates.some((item) => selectedCandidates[item.id])}>{t("确认导入")}</button><button className="button button-primary" onClick={applyImport} disabled={busy}>{busy ? t("应用中") : t("确认导入并应用")}</button></footer></div></div> : null}
  </PageScaffold>;
}
