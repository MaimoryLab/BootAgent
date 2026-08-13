import { FolderOpen, RefreshCw, Sparkles, Trash2, Upload } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { api } from "../backend/api";
import type { SkillCandidate, SkillChange, SkillSummary } from "../types/api";
import { PageScaffold } from "../components/PageScaffold";
import { useI18n } from "../i18n";

export function SkillsPage() {
  const { t, locale } = useI18n();
  const [rows, setRows] = useState<SkillSummary[]>([]);
  const [candidates, setCandidates] = useState<SkillCandidate[]>([]);
  const [eligible, setEligible] = useState<string[]>([]);
  const [token, setToken] = useState("");
  const [changes, setChanges] = useState<Record<string, SkillChange>>({});
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  const dirty = Object.keys(changes).length > 0;
  useEffect(() => { void api.listSkills().then(setRows); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }); }, []);
  useEffect(() => { void api.setSkillDraftState(dirty, locale); }, [dirty, locale]);

  const scan = () => { setBusy(true); void api.scanSkills().then((r) => { setRows(r.skills ?? []); setCandidates(r.candidates ?? []); setEligible(r.eligible_agents ?? []); setToken(r.preview_token ?? ""); }).finally(() => setBusy(false)); };
  const importSource = (source: string) => { setBusy(true); void api.previewSkillImport(source).then((r) => { setCandidates(r.candidates ?? []); setToken(r.token); }).finally(() => setBusy(false)); };
  const candidateMap = useMemo(() => new Map(candidates.map((item) => [item.id, item])), [candidates]);
  const apply = () => { setBusy(true); void api.applySkills({ preview_token: token || undefined, changes: Object.values(changes) }).then((r) => { const successful = new Set((r.results ?? []).filter((item) => item.registry_updated).map((item) => item.agent)); if (successful.size) setChanges((current) => Object.fromEntries(Object.entries(current).filter(([, change]) => !change.targets?.some((agent) => successful.has(agent))))); scan(); }).finally(() => setBusy(false)); };
  const selectTarget = (id: string, hash: string, agent: string, checked: boolean) => setChanges((current) => { const prior = current[id] ?? { id, variant_hash: hash, targets: [] }; const targets = new Set(prior.targets ?? []); checked ? targets.add(agent) : targets.delete(agent); return { ...current, [id]: { ...prior, variant_hash: hash, targets: [...targets] } }; });
  const uninstall = (id: string) => { setBusy(true); void api.uninstallSkill(id).then((result) => { setMessage(result.error || (result.backup_id ? `${t("备份")}: ${result.backup_id}` : "")); scan(); }).finally(() => setBusy(false)); };

  return <PageScaffold title={t("Skills")} description={t("管理本机 Skills 并同步到 Agent")} bodyClassName="skills-page" secondaryAction={<><button className="button button-secondary" onClick={scan} disabled={busy} title={t("重新扫描 Skills")}><RefreshCw size={16} />{t("重新扫描 Skills")}</button><button className="button button-secondary" onClick={() => importSource("folder")} disabled={busy} title={t("导入文件夹")}><FolderOpen size={16} />{t("导入文件夹")}</button><button className="button button-secondary" onClick={() => importSource("zip")} disabled={busy} title={t("导入 ZIP")}><Upload size={16} />{t("导入 ZIP")}</button></>} primaryLabel={t("应用 Skills")} onPrimary={apply} primaryDisabled={!dirty || busy}>
    <section className="content-section skills-page"><div className="section-heading"><div><h2><Sparkles size={18} /> Skills</h2>{message ? <p>{message}</p> : null}</div></div>
      {candidates.length ? <div className="skills-candidates"><strong>{t("导入预览")}</strong>{candidates.map((candidate) => <span key={`${candidate.id}-${candidate.hash}`} className="status-badge status-info">{candidate.id} · {candidate.hash.slice(0, 8)} · {candidate.files} files</span>)}</div> : null}
      {!rows.length ? <div className="empty-overview">{t("没有发现 Skills")}</div> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>Skill</th><th>{t("状态")}</th><th>{t("选择目标 Agent")}</th><th /></tr></thead><tbody>{rows.map((row) => { const candidate = candidateMap.get(row.id); const hash = candidate?.hash ?? ""; const change = changes[row.id]; return <tr key={row.id}><td><strong>{row.name || row.id}</strong><small>{row.description}</small></td><td>{row.conflict ? <span className="status-badge status-warning">{t("冲突")}</span> : <span className="status-badge status-success">{row.variants} variant</span>}</td><td><div className="mcp-targets">{eligible.map((agent) => <label key={agent}><input type="checkbox" checked={change?.targets?.includes(agent) ?? row.agents?.includes(agent) ?? false} disabled={!hash} onChange={(event) => selectTarget(row.id, hash, agent, event.target.checked)} />{agent}</label>)}</div></td><td><button className="icon-button is-danger" onClick={() => uninstall(row.id)} disabled={busy} title={t("卸载")}><Trash2 size={16} /></button></td></tr>; })}</tbody></table></div>}
    </section>
  </PageScaffold>;
}
