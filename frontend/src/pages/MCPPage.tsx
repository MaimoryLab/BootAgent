import { Edit3, Plus, RefreshCw, Save, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { api } from "../backend/api";
import type { MCPServerSummary, MCPSpec } from "../types/api";
import { useI18n } from "../i18n";
import { PageScaffold } from "../components/PageScaffold";

const emptySpec: MCPSpec = { type: "stdio", command: "", args: [], env: {} };

export function MCPPage() {
  const { t, locale } = useI18n();
  const [rows, setRows] = useState<MCPServerSummary[]>([]);
  const [scanning, setScanning] = useState(false);
  const [draft, setDraft] = useState<Record<string, MCPSpec>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MCPSpec>(emptySpec);
  const dirty = Object.keys(draft).length > 0;

  const refresh = () => {
    setScanning(true);
    void api.scanMCP().then((result) => setRows(result.servers ?? [])).finally(() => setScanning(false));
  };
  useEffect(() => {
    void api.listMCP().then(setRows);
    refresh();
  }, []);
  useEffect(() => { void api.setMCPDraftState(dirty, locale); }, [dirty, locale]);

  const agentsFor = (row: MCPServerSummary) => row.agents ?? [];
  const openNew = () => { setEditing(""); setForm({ ...emptySpec, args: [], env: {} }); };
  const openEdit = async (row: MCPServerSummary) => {
    try {
      const detail = await api.getMCP(row.id, agentsFor(row)[0] ?? "");
      setEditing(row.id); setForm(detail.variants?.[0]?.spec ?? emptySpec);
    } catch { /* normalized bridge error is surfaced by the page-level shell */ }
  };
  const saveDraft = () => {
    if (editing === null || !(form.command ?? "").trim()) return;
    setDraft((current) => ({ ...current, [editing]: { ...form, command: (form.command ?? "").trim() } }));
    setRows((current) => current.some((row) => row.id === editing) ? current : [...current, { id: editing, type: form.type ?? "stdio", agents: [], variants: 1, conflict: false, has_secrets: Object.keys(form.env ?? {}).length > 0 }]);
    setEditing(null);
  };
  const apply = async () => {
    const changes = Object.entries(draft).map(([id, spec]) => ({ id, spec, agents: rows.find((row) => row.id === id) ? agentsFor(rows.find((row) => row.id === id)!) : [] }));
    const result = await api.applyMCP({ changes });
    if ((result.results ?? []).every((item) => item.registry_updated)) setDraft({});
  };
  const visibleRows = useMemo(() => rows.slice().sort((a, b) => a.id.localeCompare(b.id)), [rows]);

  return <PageScaffold title={t("MCP 服务器")} description={t("在已初始化的 Agent 之间同步 MCP 服务器")}>
    <section className="content-section mcp-page">
      <div className="section-heading"><div><h2>{t("MCP Registry")}</h2><p>{t("配置只在点击应用更改后写入 Agent")}</p></div><div className="mcp-actions"><button className="button" onClick={refresh} disabled={scanning} title={t("重新扫描")}><RefreshCw size={16} />{t("重新扫描")}</button><button className="button" onClick={openNew}><Plus size={16} />{t("新增")}</button><button className="button button-primary" onClick={apply} disabled={!dirty}><Save size={16} />{t("应用更改")}</button></div></div>
      {scanning ? <div className="mcp-scan-status"><span className="spinner" />{t("正在后台扫描")}</div> : null}
      {!visibleRows.length ? <div className="empty-overview">{t("尚未发现 MCP 服务器")}</div> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>{t("服务器")}</th><th>{t("传输")}</th><th>{t("来源 Agent")}</th><th>{t("状态")}</th><th /></tr></thead><tbody>{visibleRows.map((row) => <tr key={row.id}><td><strong>{row.id}</strong></td><td>{row.type}</td><td>{agentsFor(row).join(", ") || t("待应用")}</td><td><span className={row.conflict ? "status-badge status-warning" : draft[row.id] ? "status-badge status-info" : "status-badge status-success"}>{row.conflict ? t("冲突") : draft[row.id] ? t("待应用") : t("已同步")}</span>{row.has_secrets ? <small className="mcp-secret-note">{t("包含秘密字段")}</small> : null}</td><td className="mcp-row-actions"><button className="icon-button" onClick={() => void openEdit(row)} title={t("编辑")}><Edit3 size={16} /></button><button className="icon-button is-danger" onClick={() => setDraft((current) => ({ ...current, [row.id]: { type: "stdio" } }))} title={t("删除")}><Trash2 size={16} /></button></td></tr>)}</tbody></table></div>}
    </section>
    {editing !== null ? <div className="mcp-modal-backdrop"><div className="mcp-modal" role="dialog" aria-modal="true"><header><h2>{editing ? t("编辑 MCP 服务器") : t("新增 MCP 服务器")}</h2><button className="icon-button" onClick={() => setEditing(null)} title={t("关闭")}><X size={18} /></button></header><label>{t("服务器 ID")}<input value={editing} onChange={(event) => setEditing(event.target.value)} disabled={Boolean(draft[editing])} /></label><label>{t("传输")}<select value={form.type ?? "stdio"} onChange={(event) => setForm({ ...form, type: event.target.value })}><option value="stdio">stdio</option><option value="http">http</option><option value="sse">sse</option></select></label>{form.type === "stdio" ? <label>{t("命令")}<input value={form.command ?? ""} onChange={(event) => setForm({ ...form, command: event.target.value })} /></label> : <label>{t("URL")}<input value={form.url ?? ""} onChange={(event) => setForm({ ...form, url: event.target.value })} /></label>}<label>{t("高级 JSON")}<textarea value={JSON.stringify(form, null, 2)} onChange={(event) => { try { setForm(JSON.parse(event.target.value)); } catch { /* retain the last valid draft */ } }} rows={10} /></label><footer><button className="button" onClick={() => setEditing(null)}><X size={16} />{t("取消")}</button><button className="button button-primary" onClick={saveDraft}><Save size={16} />{t("保存草稿")}</button></footer></div></div> : null}
  </PageScaffold>;
}
