/**
 * Adapts mcpservers-catalog.json (scraped from mcpservers.org sitemap +
 * per-page og: meta tags) to MarketplaceItem shape.
 *
 * Install prompts are written as plain-language task descriptions — no slash
 * commands — so any CLI agent (Claude Code, Codex, OpenCode…) can execute
 * them: the agent locates its own MCP config file and merges the server in.
 */

import type { MarketplaceItem } from "../types/marketplace";
import mcpRaw from "./mcpservers-catalog.json";

interface MCPServerEntry {
  slug: string;
  name: string;
  description: string;
  github: string | null;
  config: Record<string, unknown> | null;
  pageUrl: string;
  official: boolean;
}

/** Plain-language install prompt an agent can act on without slash commands. */
function buildInstallPrompt(entry: MCPServerEntry): string {
  const configBlock = entry.config
    ? `参考配置（server 名可自行调整）：
${JSON.stringify({ mcpServers: { [entry.slug.replace(/-mcp-server$/, "")]: entry.config } }, null, 2)}`
    : entry.github
      ? `项目地址：${entry.github}
请先阅读该项目的 README，从中获取推荐的服务器启动配置。`
      : `详情页：${entry.pageUrl}
请先查阅该页面获取安装配置。`;

  return `请帮我为当前使用的 Agent 添加 MCP 服务器「${entry.name}」。

${configBlock}

执行步骤：
1. 识别当前 Agent 的 MCP 配置位置（例如 Claude Code 是 ~/.claude.json 或项目内 .mcp.json，Codex 是 ~/.codex/config.toml，其他 Agent 请查阅其文档）
2. 将上面的服务器配置合并进去，不要覆盖已有的其他服务器
3. 如果配置需要 API Key 或 Token，请保留占位符并告诉我去哪里获取
4. 完成后验证该 MCP 服务器能正常连接`;
}

/**
 * GitHub org/user avatar for the repo owner. GitHub serves
 * https://github.com/{owner}.png as a redirect to the avatar, which <img>
 * follows transparently. Entries without a github URL keep the lucide
 * Puzzle fallback (no iconUrl).
 */
function githubAvatarUrl(github: string | null): string | undefined {
  if (!github) return undefined;
  const match = /^https:\/\/github\.com\/([^/]+)/.exec(github);
  return match ? `https://github.com/${match[1]}.png?size=64` : undefined;
}

export const mcpserversItems: MarketplaceItem[] = (mcpRaw as MCPServerEntry[]).map((entry) => ({
  id: `mcp-${entry.slug}`,
  category: "mcp-server",
  type: "installable",
  installableKind: "mcp",
  name: entry.name,
  description: entry.description,
  icon: "Puzzle",
  iconColor: "oklch(55% 0.15 160)",
  iconUrl: githubAvatarUrl(entry.github),
  tags: entry.official ? ["官方", "MCP"] : ["MCP"],
  scene: "integration",
  source: "mcpservers",
  requiresApiKey: entry.config != null && JSON.stringify(entry.config).toLowerCase().includes("token"),
  sourceLabel: "MCP Servers",
  sourceUrl: entry.pageUrl,
  installPrompt: buildInstallPrompt(entry),
  targetHint: "粘贴到任意命令行 Agent 对话框执行，它会自动完成 MCP 配置",
  externalUrl: entry.github ?? undefined,
  readmeUrl: entry.github
    ? `${entry.github.replace("https://github.com/", "https://raw.githubusercontent.com/").replace(/\/tree\/main\//, "/main/")}/main/README.md`.replace(/\/main\/main\//, "/main/")
    : undefined,
}));
