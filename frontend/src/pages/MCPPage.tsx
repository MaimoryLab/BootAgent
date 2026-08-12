import { Edit3, Plus, RefreshCw, Save, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

import { api } from "../backend/api";
import type { MCPServerSummary, MCPSpec } from "../types/api";
import { useI18n } from "../i18n";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";

const emptySpec: MCPSpec = { type: "stdio", command: "", args: [], env: {} };

export function parseStdioCommandLine(value: string): Pick<MCPSpec, "command" | "args"> {
  const parts = value.trim().split(/\s+/).filter(Boolean);
  return { command: parts[0] ?? "", args: parts.slice(1) };
}

export function formatStdioCommandLine(spec: Pick<MCPSpec, "command" | "args">): string {
  return [spec.command ?? "", ...(spec.args ?? [])].filter(Boolean).join(" ");
}

export function previewMCPForm(spec: MCPSpec, commandLine: string): MCPSpec {
  return spec.type === "stdio" ? { ...spec, ...parseStdioCommandLine(commandLine) } : spec;
}

export function normalizeAdvancedSpec(input: MCPSpec): MCPSpec {
  const type = input.type || (input.url ? "http" : input.command ? "stdio" : undefined);
  if (!type) return input;
  return type === "stdio"
    ? { ...input, type, url: undefined, headers: undefined }
    : { ...input, type, command: undefined, args: undefined, cwd: undefined, env: undefined };
}

export function changeMCPTransport(spec: MCPSpec, type: string): MCPSpec {
  if (type === "stdio") {
    return { ...spec, type, url: undefined, headers: undefined };
  }
  return { ...spec, type, command: undefined, args: undefined, cwd: undefined, env: undefined };
}

export function mcpRowPending(id: string, draft: Record<string, MCPSpec>, targets: Record<string, string[]>): boolean {
  return Boolean(draft[id] || targets[id]);
}

export function MCPPage() {
  const { t, locale } = useI18n();
  const [rows, setRows] = useState<MCPServerSummary[]>([]);
  const [scanning, setScanning] = useState(false);
  const [draft, setDraft] = useState<Record<string, MCPSpec>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MCPSpec>(emptySpec);
  const [commandLine, setCommandLine] = useState("");
  const [advancedJSON, setAdvancedJSON] = useState("");
  const [eligibleAgents, setEligibleAgents] = useState<string[]>([]);
  const [targets, setTargets] = useState<Record<string, string[]>>({});
  const dirty = Object.keys(draft).length > 0 || Object.keys(targets).length > 0;

  const refresh = () => {
    setScanning(true);
    void api.scanMCP().then((result) => { setRows(result.servers ?? []); setEligibleAgents(result.eligible_agents ?? []); }).finally(() => setScanning(false));
  };
  useEffect(() => {
    void api.listMCP().then(setRows);
    refresh();
  }, []);
  useEffect(() => { void api.setMCPDraftState(dirty, locale); }, [dirty, locale]);

  const agentsFor = (row: MCPServerSummary) => row.agents ?? [];
  const openNew = () => { setEditing(""); setForm({ ...emptySpec, args: [], env: {} }); setCommandLine(""); setAdvancedJSON(JSON.stringify(emptySpec, null, 2)); };
  const openEdit = async (row: MCPServerSummary) => {
    try {
      const detail = await api.getMCP(row.id, agentsFor(row)[0] ?? "");
      const spec = detail.variants?.[0]?.spec ?? emptySpec;
      setEditing(row.id); setForm(spec); setCommandLine(spec.type === "stdio" ? formatStdioCommandLine(spec) : ""); setAdvancedJSON(JSON.stringify(spec, null, 2));
    } catch { /* normalized bridge error is surfaced by the page-level shell */ }
  };
  const saveDraft = () => {
    const command = form.type === "stdio" ? parseStdioCommandLine(commandLine) : { command: form.command ?? "", args: form.args ?? [] };
    if (editing === null || (form.type === "stdio" ? !command.command : !(form.url ?? "").trim())) return;
    const next = normalizeAdvancedSpec({ ...form, ...command });
    setDraft((current) => ({ ...current, [editing]: next }));
    setRows((current) => current.some((row) => row.id === editing) ? current : [...current, { id: editing, type: form.type ?? "stdio", agents: [], variants: 1, conflict: false, has_secrets: Object.keys(form.env ?? {}).length > 0 }]);
    setEditing(null);
  };
  const apply = async () => {
    const ids = new Set([...Object.keys(draft), ...Object.keys(targets)]);
    const changes = await Promise.all([...ids].map(async (id) => ({ id, spec: draft[id] ?? (await api.getMCP(id, "")).variants?.[0]?.spec ?? emptySpec, agents: targets[id] ?? agentsFor(rows.find((row) => row.id === id)!) })));
    const result = await api.applyMCP({ changes });
    if ((result.results ?? []).every((item) => item.registry_updated)) { setDraft({}); setTargets({}); }
    refresh();
  };
  const visibleRows = useMemo(() => rows.slice().sort((a, b) => a.id.localeCompare(b.id)), [rows]);
  const previewForm = previewMCPForm(form, commandLine);
  const updateCommandLine = (value: string) => { setCommandLine(value); const next = { ...form, ...parseStdioCommandLine(value), type: "stdio" }; setForm(next); setAdvancedJSON(JSON.stringify(next, null, 2)); };
  const updateAdvancedJSON = (value: string) => {
    setAdvancedJSON(value);
    try {
      const next = normalizeAdvancedSpec(JSON.parse(value) as MCPSpec);
      setForm(next);
      if (next.type === "stdio") setCommandLine(formatStdioCommandLine(next));
    } catch { /* keep the editable text until it becomes valid JSON */ }
  };
  const finishAdvancedJSON = () => {
    try {
      const next = normalizeAdvancedSpec(JSON.parse(advancedJSON) as MCPSpec);
      setForm(next); setAdvancedJSON(JSON.stringify(next, null, 2));
      if (next.type === "stdio") setCommandLine(formatStdioCommandLine(next));
    } catch { /* leave invalid text visible for correction */ }
  };

  return <PageScaffold title={t("MCP 服务器")} description={t("在已初始化的 Agent 之间同步 MCP 服务器")}>
    <section className="content-section mcp-page">
      <div className="section-heading"><div><h2>{t("MCP Registry")}</h2><p>{t("配置只在点击应用更改后写入 Agent")}</p></div><div className="mcp-actions"><button className="button" onClick={refresh} disabled={scanning} title={t("重新扫描")}><RefreshCw size={16} />{t("重新扫描")}</button><button className="button" onClick={openNew}><Plus size={16} />{t("新增")}</button><button className="button button-primary" onClick={apply} disabled={!dirty}><Save size={16} />{t("应用更改")}</button></div></div>
      {scanning ? <div className="mcp-scan-status"><span className="spinner" />{t("正在后台扫描")}</div> : null}
      {!visibleRows.length ? <div className="empty-overview">{t("尚未发现 MCP 服务器")}</div> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>{t("服务器")}</th><th>{t("传输")}</th><th>{t("来源 Agent")}</th><th>{t("同步目标")}</th><th>{t("状态")}</th><th /></tr></thead><tbody>{visibleRows.map((row) => <tr key={row.id}><td><strong>{row.id}</strong></td><td>{row.type}</td><td>{agentsFor(row).join(", ") || t("待应用")}</td><td><div className="mcp-targets">{eligibleAgents.map((agent) => { const selected = (targets[row.id] ?? agentsFor(row)).includes(agent); return <label key={agent}><input type="checkbox" checked={selected} onChange={(event) => { const current = new Set(targets[row.id] ?? agentsFor(row)); event.target.checked ? current.add(agent) : current.delete(agent); setTargets((all) => ({ ...all, [row.id]: [...current] })); }} />{agent}</label>; })}</div></td><td><span className={row.conflict ? "status-badge status-warning" : mcpRowPending(row.id, draft, targets) ? "status-badge status-info" : "status-badge status-success"}>{row.conflict ? t("冲突") : mcpRowPending(row.id, draft, targets) ? t("待应用") : t("已同步")}</span>{row.has_secrets ? <small className="mcp-secret-note">{t("包含秘密字段")}</small> : null}</td><td className="mcp-row-actions"><button className="icon-button" onClick={() => void openEdit(row)} title={t("编辑")}><Edit3 size={16} /></button><button className="icon-button is-danger" onClick={() => setDraft((current) => ({ ...current, [row.id]: { type: "stdio" } }))} title={t("删除")}><Trash2 size={16} /></button></td></tr>)}</tbody></table></div>}
    </section>
    {editing !== null ? <div className="mcp-modal-backdrop"><div className="mcp-modal" role="dialog" aria-modal="true"><header><h2>{editing ? t("编辑 MCP 服务器") : t("新增 MCP 服务器")}</h2><button className="icon-button" onClick={() => setEditing(null)} title={t("关闭")}><X size={18} /></button></header><label>{t("服务器 ID")}<input value={editing} onChange={(event) => setEditing(event.target.value)} disabled={Boolean(draft[editing])} /></label><div className="mcp-transport-field"><span>{t("传输")}</span><SelectField value={form.type ?? "stdio"} options={[{ value: "stdio", label: "stdio" }, { value: "http", label: "http" }, { value: "sse", label: "sse" }]} onChange={(type) => { const next = changeMCPTransport(form, type); setForm(next); setCommandLine(type === "stdio" ? formatStdioCommandLine(next) : ""); setAdvancedJSON(JSON.stringify(next, null, 2)); }} label={t("传输")} /></div>{form.type === "stdio" ? <label>{t("命令")}<input value={commandLine} onChange={(event) => updateCommandLine(event.target.value)} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label> : <label>{t("URL")}<input value={form.url ?? ""} onChange={(event) => setForm({ ...form, url: event.target.value })} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label>}<label>{t("高级 JSON")}<textarea value={advancedJSON} onChange={(event) => updateAdvancedJSON(event.target.value)} onBlur={finishAdvancedJSON} rows={10} /></label><footer><button className="button" onClick={() => setEditing(null)}><X size={16} />{t("取消")}</button><button className="button button-primary" onClick={saveDraft}><Save size={16} />{t("保存草稿")}</button></footer></div></div> : null}
  </PageScaffold>;
}
