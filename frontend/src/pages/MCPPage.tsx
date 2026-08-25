import { Edit3, Plus, RefreshCw, RotateCcw, Save, Server, ShoppingBag, Trash2, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";

import { api } from "../backend/api";
import type { MCPServerSummary, MCPSpec } from "../types/api";
import { useI18n } from "../i18n";
import { AgentSummaryBar } from "../components/AgentSummaryBar";
import { AgentTargetGroup } from "../components/AgentTargetGroup";
import { EmptyState } from "../components/EmptyState";
import { ManagementSearch } from "../components/ManagementSearch";
import { ModalDialog } from "../components/ModalDialog";
import { PageScaffold } from "../components/PageScaffold";
import { SelectField } from "../components/SelectField";
import { useWizard } from "../state/WizardContext";

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

export function parseAdvancedSpecJSON(value: string): MCPSpec {
  const text = value.trim();
  const decode = (input: string): MCPSpec => {
    const parsed = JSON.parse(input) as MCPSpec & { mcpServers?: Record<string, MCPSpec> };
    if (parsed.mcpServers && typeof parsed.mcpServers === "object") {
      const first = Object.values(parsed.mcpServers)[0];
      if (first && typeof first === "object") return normalizeAdvancedSpec(first);
    }
    return normalizeAdvancedSpec(parsed);
  };
  try {
    return decode(text);
  } catch (firstError) {
    const missingObjects = (text.match(/{/g)?.length ?? 0) - (text.match(/}/g)?.length ?? 0);
    if (text.startsWith("{") && missingObjects > 0) {
      try {
        return decode(text + "}".repeat(missingObjects));
      } catch {
        // Keep the original error for genuinely invalid JSON.
      }
    }
    throw firstError;
  }
}

export function parseAdvancedServerID(value: string): string | undefined {
  try {
    const parsed = JSON.parse(value.trim()) as { mcpServers?: Record<string, MCPSpec> };
    if (parsed.mcpServers && typeof parsed.mcpServers === "object") return Object.keys(parsed.mcpServers)[0];
  } catch {
    return undefined;
  }
  return undefined;
}

export function isMCPDraftComplete(id: string | null, spec: MCPSpec, commandLine: string): boolean {
  if (!id?.trim() || !["stdio", "http", "sse"].includes(spec.type ?? "")) return false;
  return spec.type === "stdio" ? Boolean(parseStdioCommandLine(commandLine).command) : Boolean(spec.url?.trim());
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

export function filterMCPRows(rows: MCPServerSummary[], query: string): MCPServerSummary[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => [row.id, row.type].some((value) => value?.toLowerCase().includes(needle)));
}

export function MCPPage() {
  const { t, locale } = useI18n();
  const { state } = useWizard();
  const [rows, setRows] = useState<MCPServerSummary[]>([]);
  const [scanning, setScanning] = useState(false);
  const [draft, setDraft] = useState<Record<string, MCPSpec>>({});
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<MCPSpec>(emptySpec);
  const [commandLine, setCommandLine] = useState("");
  const [advancedJSON, setAdvancedJSON] = useState("");
  const [eligibleAgents, setEligibleAgents] = useState<string[]>([]);
  const [targets, setTargets] = useState<Record<string, string[]>>({});
  const [deleted, setDeleted] = useState<Record<string, boolean>>({});
  const [query, setQuery] = useState("");
  const dirty = Object.keys(draft).length > 0 || Object.keys(targets).length > 0 || Object.keys(deleted).length > 0;
  const dirtyCount = new Set([...Object.keys(draft), ...Object.keys(targets), ...Object.keys(deleted)]).size;

  const agentLabels = useMemo(
    () => Object.fromEntries((state.status?.catalog ?? []).map((agent) => [agent.id, agent.name])),
    [state.status],
  );

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
  const selectedFor = (row: MCPServerSummary) => targets[row.id] ?? agentsFor(row);
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
    if (!isMCPDraftComplete(editing, form, commandLine)) return;
    const id = editing as string;
    const next = normalizeAdvancedSpec({ ...form, ...command });
    setDraft((current) => ({ ...current, [id]: next }));
    setRows((current) => current.some((row) => row.id === id) ? current : [...current, { id, type: form.type ?? "stdio", agents: [], variants: 1, conflict: false, has_secrets: Object.keys(form.env ?? {}).length > 0 }]);
    setEditing(null);
  };
  const apply = async () => {
    const ids = new Set([...Object.keys(draft), ...Object.keys(targets), ...Object.keys(deleted)]);
    const changes = await Promise.all([...ids].map(async (id) => ({ id, spec: deleted[id] ? undefined : draft[id] ?? (await api.getMCP(id, "")).variants?.[0]?.spec ?? emptySpec, delete: Boolean(deleted[id]), agents: targets[id] ?? agentsFor(rows.find((row) => row.id === id)!) })));
    const result = await api.applyMCP({ changes });
    if ((result.results ?? []).every((item) => item.registry_updated)) { setDraft({}); setTargets({}); setDeleted({}); }
    refresh();
  };
  const visibleRows = useMemo(() => filterMCPRows(rows.slice().sort((a, b) => a.id.localeCompare(b.id)), query), [rows, query]);
  const summaryRows = useMemo(() => visibleRows.filter((row) => !deleted[row.id]), [visibleRows, deleted]);
  const summaryCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const agent of eligibleAgents) counts[agent] = summaryRows.filter((row) => selectedFor(row).includes(agent)).length;
    return counts;
    // selectedFor reads `targets`, which the dependency list carries directly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [eligibleAgents, summaryRows, targets]);
  const bulkToggle = (agent: string, enabled: boolean) => {
    setTargets((all) => {
      const next = { ...all };
      for (const row of summaryRows) {
        const current = new Set(next[row.id] ?? agentsFor(row));
        if (current.has(agent) === enabled) continue;
        enabled ? current.add(agent) : current.delete(agent);
        next[row.id] = [...current];
      }
      return next;
    });
  };
  const previewForm = previewMCPForm(form, commandLine);
  const updateCommandLine = (value: string) => { setCommandLine(value); const next = { ...form, ...parseStdioCommandLine(value), type: "stdio" }; setForm(next); setAdvancedJSON(JSON.stringify(next, null, 2)); };
  const updateAdvancedJSON = (value: string) => {
    setAdvancedJSON(value);
    try {
      const next = parseAdvancedSpecJSON(value);
      setForm(next);
      const serverID = parseAdvancedServerID(value);
      if (serverID) setEditing(serverID);
      if (next.type === "stdio") setCommandLine(formatStdioCommandLine(next));
    } catch { /* keep the editable text until it becomes valid JSON */ }
  };
  const finishAdvancedJSON = () => {
    try {
      const next = parseAdvancedSpecJSON(advancedJSON);
      setForm(next); setAdvancedJSON(JSON.stringify(next, null, 2));
      const serverID = parseAdvancedServerID(advancedJSON);
      if (serverID) setEditing(serverID);
      if (next.type === "stdio") setCommandLine(formatStdioCommandLine(next));
    } catch { /* leave invalid text visible for correction */ }
  };

  return <PageScaffold title={t("MCP 服务器")} description={t("在已初始化的 Agent 之间同步 MCP 服务器")} bodyClassName="mcp-page" secondaryAction={<div className="management-footer-actions"><Link className="button button-secondary" to="/marketplace?category=mcp-server"><ShoppingBag size={16} />{t("去市场发现")}</Link><button className="button button-secondary" onClick={refresh} disabled={scanning} title={t("重新扫描")}><RefreshCw size={16} />{t("重新扫描")}</button><button className="button button-secondary" onClick={openNew}><Plus size={16} />{t("新增")}</button></div>} primaryLabel={dirtyCount ? t("应用更改（{count}）", { count: dirtyCount }) : t("应用更改")} onPrimary={() => void apply()} primaryDisabled={!dirty}>
    <section className="content-section mcp-page">
      <div className="section-heading"><div><h2>{t("MCP Registry")}</h2><p>{t("配置只在点击应用更改后写入 Agent")}</p></div></div>
      {scanning ? <div className="mcp-scan-status"><span className="spinner" />{t("正在后台扫描")}</div> : null}
      {rows.length ? <div className="management-toolbar"><ManagementSearch value={query} onValueChange={setQuery} placeholder={t("搜索 MCP 服务器")} /><AgentSummaryBar agents={eligibleAgents} counts={summaryCounts} total={summaryRows.length} onToggleAll={bulkToggle} labels={agentLabels} /></div> : null}
      {!rows.length ? <EmptyState icon={Server} title={t("尚未发现 MCP 服务器")} hint={t("点击新增来登记第一个 MCP 服务器")} action={<Link className="list-empty-link" to="/marketplace?category=mcp-server">{t("或者去工具市场发现")}</Link>} /> : !visibleRows.length ? <EmptyState icon={Server} title={t("没有匹配的 MCP 服务器")} hint={t("换一个关键词，或清空搜索")} /> : <div className="mcp-table-wrap"><table className="mcp-table"><thead><tr><th>{t("服务器")}</th><th>{t("传输")}</th><th>{t("同步目标")}</th><th>{t("状态")}</th><th /></tr></thead><tbody>{visibleRows.map((row) => { const isDeleted = Boolean(deleted[row.id]); return <tr key={row.id} className={isDeleted ? "mcp-row-deleted" : undefined}><td><strong>{row.id}</strong></td><td>{row.type}</td><td><AgentTargetGroup agents={eligibleAgents} selected={selectedFor(row)} disabled={isDeleted} labels={agentLabels} onToggle={(agent, checked) => setTargets((all) => { const current = new Set(all[row.id] ?? agentsFor(row)); checked ? current.add(agent) : current.delete(agent); return { ...all, [row.id]: [...current] }; })} /></td><td><span className={isDeleted ? "status-badge status-warning" : row.conflict ? "status-badge status-warning" : mcpRowPending(row.id, draft, targets) ? "status-badge status-info" : "status-badge status-success"}>{isDeleted ? t("待删除") : row.conflict ? t("冲突") : mcpRowPending(row.id, draft, targets) ? t("待应用") : t("已同步")}</span>{row.has_secrets ? <small className="mcp-secret-note">{t("包含秘密字段")}</small> : null}</td><td className="mcp-row-actions"><button className="icon-button row-action" disabled={isDeleted} onClick={() => void openEdit(row)} title={t("编辑")}><Edit3 size={16} /></button><button className={`icon-button row-action${isDeleted ? " is-restore" : " is-danger"}`} onClick={() => setDeleted((all) => ({ ...all, [row.id]: !isDeleted }))} title={isDeleted ? t("恢复") : t("删除")}>{isDeleted ? <RotateCcw size={16} /> : <Trash2 size={16} />}</button></td></tr>; })}</tbody></table></div>}
    </section>
    {editing !== null ? <ModalDialog className="mcp-modal" label={editing ? t("编辑 MCP 服务器") : t("新增 MCP 服务器")} onDismiss={() => setEditing(null)}><header><h2>{editing ? t("编辑 MCP 服务器") : t("新增 MCP 服务器")}</h2><button className="icon-button" onClick={() => setEditing(null)} title={t("关闭")}><X size={18} /></button></header><label>{t("服务器 ID")}<input value={editing} onChange={(event) => setEditing(event.target.value)} disabled={Boolean(draft[editing])} /></label><div className="mcp-transport-field"><span>{t("传输")}</span><SelectField value={form.type ?? "stdio"} options={[{ value: "stdio", label: "stdio" }, { value: "http", label: "http" }, { value: "sse", label: "sse" }]} onChange={(type) => { const next = changeMCPTransport(form, type); setForm(next); setCommandLine(type === "stdio" ? formatStdioCommandLine(next) : ""); setAdvancedJSON(JSON.stringify(next, null, 2)); }} label={t("传输")} /></div>{form.type === "stdio" ? <label>{t("命令")}<input value={commandLine} onChange={(event) => updateCommandLine(event.target.value)} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label> : <label>{t("URL")}<input value={form.url ?? ""} onChange={(event) => { const next = { ...form, url: event.target.value }; setForm(next); setAdvancedJSON(JSON.stringify(next, null, 2)); }} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} /></label>}<label>{t("高级 JSON")}<textarea value={advancedJSON} onChange={(event) => updateAdvancedJSON(event.target.value)} onBlur={finishAdvancedJSON} rows={10} /></label><footer><button className="button" onClick={() => setEditing(null)}><X size={16} />{t("取消")}</button><button className="button button-primary" onClick={saveDraft}><Save size={16} />{t("保存草稿")}</button></footer></ModalDialog> : null}
  </PageScaffold>;
}
