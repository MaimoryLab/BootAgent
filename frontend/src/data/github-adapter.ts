import type { MarketplaceCategory, MarketplaceIconName, MarketplaceItem, MarketplaceScene } from "../types/marketplace";
import { marketplaceIconUrl } from "./marketplace-icons";

interface GitHubRepositorySpec {
  repo: string;
  name: string;
  description: string;
  descriptionEn: string;
  category: MarketplaceCategory;
  scene: MarketplaceScene;
  tags: string[];
  icon: MarketplaceIconName;
  stars: number;
  forks?: number;
  license?: string;
  branch?: string;
  docs?: string;
  homepage?: string;
}

type RepositoryRow = [string, string, string, string, MarketplaceCategory, MarketplaceScene, string[], MarketplaceIconName, number, number | undefined, string | undefined, string | undefined, string?];

const repositories: GitHubRepositorySpec[] = ([
  ["TencentCloud/TencentDB-Agent-Memory", "TencentDB Agent Memory", "团队级 Agent 记忆中心，将对话、文档和代码沉淀为可治理的记忆、Skill、Wiki 与 Code-Graph。", "A team memory hub that turns conversations, docs, and code into governed memory, Skills, Wiki, and Code-Graph assets.", "ai-product", "memory", ["Agent 记忆", "Skill", "知识图谱"], "Database", 24503, 2255, "Apache-2.0", "main", "https://github.com/TencentCloud/TencentDB-Agent-Memory"],
  ["zhaoxuya520/reverse-skill", "Reverse Skill", "面向逆向工程、授权渗透和安全研究的 AI Skill 路由包。", "An AI Skill router pack for reverse engineering, authorized penetration testing, and security research.", "plugin", "coding", ["安全研究", "Skill Router"], "Puzzle", 20443, undefined, "MIT", "main"],
  ["ayghri/i-have-adhd", "I Have ADHD", "让 Coding Agent 输出更聚焦、可扫描，减少把答案埋在冗长日志中的 Skill。", "A Skill that keeps coding-agent output focused and scannable.", "plugin", "productivity", ["Coding Agent", "输出优化"], "Zap", 24364, 1555, "MIT", "main"],
  ["virgiliojr94/book-to-skill", "Book to Skill", "将技术书 PDF 转换为可在 Claude Code 中学习和使用的 Skill。", "Turn a technical book PDF into a Skill ready for Claude Code.", "plugin", "learning", ["Claude Code", "学习"], "BookOpen", 25610, 2648, "MIT", "main"],
  ["volcengine/OpenViking", "OpenViking", "面向 Agent 的上下文数据库，统一管理记忆、RAG 知识和 Skills。", "A context database for Agents that unifies memory, RAG knowledge, and Skills.", "ai-product", "memory", ["上下文", "RAG", "Skills"], "Layers", 33372, 2541, "Apache-2.0", "main"],
  ["diegosouzapw/OmniRoute", "OmniRoute", "兼容多种模型和 Agent 的开源 AI Gateway，支持路由、Fallback、MCP 与 A2A。", "An open AI Gateway for multi-provider routing, fallbacks, MCP, and A2A.", "ai-product", "integration", ["AI Gateway", "多模型", "MCP"], "Globe", 55449, undefined, "MIT", "main"],
  ["citrolabs/ego-lite", "Ego Lite", "为 Coding Agent 提供共享登录态的浏览器自动化能力。", "A browser for AI agents that enables automation with shared logged-in browser state.", "ai-product", "integration", ["浏览器自动化", "Agent"], "Globe", 13568, 703, "MIT", "main"],
  ["earendil-works/pi", "Pi", "统一 LLM API、Agent Loop、TUI 和 Coding Agent CLI 的工具包。", "An AI agent toolkit with a unified LLM API, agent loop, TUI, and coding CLI.", "ai-product", "coding", ["Agent Toolkit", "Coding CLI"], "Terminal", 97411, 12046, "MIT", "main"],
  ["apache/maka", "Apache Maka", "本地优先的 AI Agent 工作区，记录模型消息、工具调用和权限决策。", "A local-first AI agent workspace that records messages, tool calls, and permission decisions.", "ai-product", "productivity", ["Agent 工作区", "本地优先"], "Workflow", 3457, 342, "Apache-2.0", "main"],
  ["akitaonrails/ai-memory", "AI Memory", "为 Coding CLI 提供长期记忆和跨 Agent 厂商的工作交接。", "Long-term memory and handoff for agent coding CLIs across vendors.", "plugin", "memory", ["长期记忆", "Coding CLI"], "Brain", 4699, 337, "MIT", "main"],
  ["mattpocock/skills", "Skills for Real Engineers", "来自真实工程工作区的 Agent Skills 集合。", "A collection of practical Agent Skills from a real engineering workspace.", "plugin", "coding", ["Skills", "工程实践"], "Code2", 237031, 20174, "MIT", "main"],
  ["Panniantong/Agent-Reach", "Agent-Reach", "让 Agent 读取和搜索 Twitter、Reddit、YouTube、GitHub、Bilibili 等互联网内容。", "Give an AI agent read and search access to the wider internet.", "ai-product", "integration", ["联网 Agent", "搜索"], "Search", 75358, undefined, "MIT", "main"],
  ["shareAI-lab/learn-claude-code", "Learn Claude Code", "从零构建类 Claude Code 的 Agent Harness，适合学习 Agent 工程实现。", "A small Claude Code-like agent harness built from scratch for learning.", "ai-product", "learning", ["Agent Harness", "教程"], "BookOpen", 75318, undefined, "MIT", "main"],
  ["AstrBotDevs/AstrBot", "AstrBot", "支持多平台、插件和多模型的开源 AI Agent 助手与开发框架。", "An open AI agent assistant and framework with multi-platform, plugin, and multi-model support.", "ai-product", "integration", ["多平台", "插件框架"], "Workflow", 39615, 2838, "AGPL-3.0", "master", "https://astrbot.app"],
  ["CherryHQ/cherry-studio", "Cherry Studio", "统一管理聊天、自治 Agent 和多种助手的 AI productivity studio。", "An AI productivity studio for chat, autonomous agents, and many assistants.", "ai-product", "productivity", ["AI 工作台", "多模型"], "Layers", 51077, undefined, "AGPL-3.0", "main"],
  ["HKUDS/nanobot", "Nanobot", "轻量、自托管的个人 AI Agent 框架，支持 WebUI、记忆、MCP 和多 Agent 工作流。", "A lightweight self-hosted personal AI agent framework with WebUI, memory, MCP, and workflows.", "ai-product", "productivity", ["个人 Agent", "自托管"], "Brain", 47402, undefined, "MIT", "main"],
  ["zhayujie/CowAgent", "CowAgent", "支持多模型、多渠道和记忆能力的开源 Agent Harness。", "An open AI assistant and agent harness with multi-model, multi-channel, and memory support.", "ai-product", "productivity", ["Agent Harness", "多渠道"], "Workflow", 46678, undefined, "MIT", "main"],
  ["iOfficeAI/AionUi", "AionUi", "面向多个 CLI Agent 的开源协作工作台。", "An open workspace for collaborating with multiple CLI agents.", "ai-product", "productivity", ["协作工作台", "CLI Agent"], "Layers", 32290, undefined, "AGPL-3.0", "main"],
  ["agentscope-ai/QwenPaw", "QwenPaw", "易部署、可扩展的个人 AI 助手，支持本地或云端运行。", "An easy-to-deploy extensible personal AI assistant for local or cloud use.", "ai-product", "productivity", ["个人助手", "多渠道"], "Sparkles", 34476, undefined, "Apache-2.0", "main"],
  ["langgenius/dify", "Dify", "用于构建 Agent 工作流和 RAG 应用的开源 AI 应用平台。", "An open platform for building Agentic workflows and RAG applications.", "ai-product", "integration", ["Agent 工作流", "RAG"], "Workflow", 153528, undefined, "Apache-2.0", "main", "https://dify.ai"],
  ["lobehub/lobehub", "LobeHub", "用于组织、调度和运营多 Agent 团队的 AI 工作台。", "An AI workspace for organizing, scheduling, and operating teams of agents.", "ai-product", "productivity", ["Agent 团队", "工作台"], "Layers", 81998, undefined, "Apache-2.0", "main", "https://lobehub.com"],
  ["open-webui/open-webui", "Open WebUI", "支持 Ollama 和 OpenAI API 的自托管 AI 界面。", "A self-hosted AI interface supporting Ollama and OpenAI-compatible APIs.", "ai-product", "productivity", ["自托管", "AI 界面"], "Globe", 149939, undefined, "MIT", "main", "https://openwebui.com"],
  ["aaif-goose/goose", "Goose", "可安装、执行、编辑和测试的可扩展开源 AI Agent。", "An extensible open-source AI agent that can install, execute, edit, and test.", "ai-product", "coding", ["开源 Agent", "自动化"], "Terminal", 53480, undefined, "Apache-2.0", "main", "https://block.github.io/goose"],
  ["activepieces/activepieces", "Activepieces", "带有数百个集成的 AI Workflow、Agent 和 MCP 自动化平台。", "An AI workflow and automation platform with agents, MCP, and hundreds of integrations.", "ai-product", "integration", ["AI 自动化", "Workflow"], "Workflow", 24039, undefined, "MIT", "main", "https://www.activepieces.com"],
] as unknown as RepositoryRow[]).map(([repo, name, description, descriptionEn, category, scene, tags, icon, stars, forks, license, branch, homepage]) => ({ repo, name, description, descriptionEn, category, scene, tags, icon, stars, forks, license, branch, homepage }));

function githubRawReadme(repo: string, branch: string): string {
  return `https://raw.githubusercontent.com/${repo}/${branch}/README.md`;
}

function installPrompt(spec: GitHubRepositorySpec): string {
  return `请帮我安装或部署 GitHub 项目「${spec.name}」。

仓库：${spec.repo}

执行步骤：
1. 打开仓库 README，确认系统要求、依赖和官方安装方式
2. 按 README 的推荐方式安装（优先使用 release、包管理器或 Docker，不要猜测参数）
3. 保留现有配置和数据，不覆盖用户文件
4. 安装完成后运行项目提供的健康检查或最小示例验证结果`;
}

/** Maps curated GitHub repositories into the same detail contract as other market sources. */
export const githubItems: MarketplaceItem[] = repositories.map((spec) => {
  const repositoryUrl = `https://github.com/${spec.repo}`;
  const branch = spec.branch ?? "main";
  return {
    id: `github-${spec.repo.toLowerCase().replace(/[^a-z0-9]+/g, "-")}`,
    category: spec.category,
    type: spec.category === "plugin" ? "plugin" : "agent-product",
    name: spec.name,
    description: spec.description,
    descriptionEn: spec.descriptionEn,
    icon: spec.icon,
    iconUrl: marketplaceIconUrl({ repositoryUrl }),
    iconColor: spec.category === "plugin" ? "oklch(62% 0.15 35)" : "oklch(58% 0.15 250)",
    tags: spec.tags,
    scene: spec.scene,
    source: "github",
    sourceLabel: "GitHub",
    sourceUrl: repositoryUrl,
    repositoryUrl,
    documentationUrl: spec.docs ?? `${repositoryUrl}#readme`,
    externalUrl: spec.homepage ?? repositoryUrl,
    readmeUrl: githubRawReadme(spec.repo, branch),
    installPrompt: installPrompt(spec),
    targetHint: "详情页提供 GitHub README 和安装提示词；请按项目官方文档执行",
    githubStars: spec.stars,
    githubForks: spec.forks,
    githubLicense: spec.license,
  };
});
