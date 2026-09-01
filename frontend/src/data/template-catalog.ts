import type { InstallableKind, MarketplaceIconName, MarketplaceItem, MarketplaceScene, MarketplaceSource } from "../types/marketplace";
import { marketplaceIconUrl } from "./marketplace-icons";

interface TemplateSpec {
  id: string;
  name: string;
  description: string;
  descriptionEn: string;
  kind: InstallableKind;
  scene: MarketplaceScene;
  tags: string[];
  icon: MarketplaceIconName;
  source: MarketplaceSource;
  sourceLabel: string;
  url: string;
  repository?: string;
  readme?: string;
  license?: string;
}

const specs: TemplateSpec[] = [
  {
    id: "template-awesome-copilot", name: "Awesome Copilot",
    description: "GitHub 官方维护的 Copilot 指令、Prompt 文件、Agent 和 Collection 集合，可按需复制到项目。",
    descriptionEn: "GitHub's official collection of Copilot instructions, prompt files, agents, and reusable collections.",
    kind: "prompt-template", scene: "coding", tags: ["Prompt 文件", "GitHub Copilot", "代码开发"], icon: "FileText",
    source: "github", sourceLabel: "GitHub", url: "https://github.com/github/awesome-copilot", repository: "https://github.com/github/awesome-copilot", readme: "https://raw.githubusercontent.com/github/awesome-copilot/main/README.md", license: "MIT",
  },
  {
    id: "template-openai-cookbook", name: "OpenAI Cookbook",
    description: "OpenAI 官方示例库，包含可复用的提示策略、结构化输出、评测与 Agent 工作流范例。",
    descriptionEn: "OpenAI's official examples for prompting, structured outputs, evaluations, and agent workflows.",
    kind: "prompt-template", scene: "reasoning", tags: ["提示工程", "结构化输出", "评测"], icon: "BookOpen",
    source: "github", sourceLabel: "GitHub", url: "https://developers.openai.com/cookbook", repository: "https://github.com/openai/openai-cookbook", readme: "https://raw.githubusercontent.com/openai/openai-cookbook/main/README.md", license: "MIT",
  },
  {
    id: "template-claude-cookbooks", name: "Claude Cookbooks",
    description: "Anthropic 官方实践集合，覆盖检索、分类、工具调用、多模态与可观测工作流。",
    descriptionEn: "Anthropic's official practical examples for retrieval, classification, tool use, multimodality, and observability.",
    kind: "workflow-script", scene: "reasoning", tags: ["Claude", "工具调用", "RAG"], icon: "Workflow",
    source: "github", sourceLabel: "GitHub", url: "https://github.com/anthropics/claude-cookbooks", repository: "https://github.com/anthropics/claude-cookbooks", readme: "https://raw.githubusercontent.com/anthropics/claude-cookbooks/main/README.md", license: "MIT",
  },
  {
    id: "template-gemini-cookbook", name: "Gemini Cookbook",
    description: "Google 官方 Notebook 示例，提供 Gemini 提示、函数调用、长上下文与多模态模板。",
    descriptionEn: "Google's official notebooks for Gemini prompting, function calling, long context, and multimodal patterns.",
    kind: "prompt-template", scene: "reasoning", tags: ["Gemini", "Notebook", "多模态"], icon: "BookOpen",
    source: "github", sourceLabel: "GitHub", url: "https://github.com/google-gemini/cookbook", repository: "https://github.com/google-gemini/cookbook", readme: "https://raw.githubusercontent.com/google-gemini/cookbook/main/README.md", license: "Apache-2.0",
  },
  {
    id: "workflow-n8n-ai-starter-kit", name: "n8n Self-hosted AI Starter Kit",
    description: "n8n 官方自托管 AI 工作流起步套件，组合 n8n、Ollama、Qdrant 与 PostgreSQL。",
    descriptionEn: "n8n's official self-hosted AI workflow starter kit with Ollama, Qdrant, and PostgreSQL.",
    kind: "workflow-script", scene: "integration", tags: ["n8n", "自托管", "RAG"], icon: "Workflow",
    source: "github", sourceLabel: "GitHub", url: "https://docs.n8n.io/deploy/host-n8n/deploy-with-the-ai-starter-kit", repository: "https://github.com/n8n-io/self-hosted-ai-starter-kit", readme: "https://raw.githubusercontent.com/n8n-io/self-hosted-ai-starter-kit/main/README.md", license: "Apache-2.0",
  },
  {
    id: "workflow-n8n-templates", name: "n8n Workflow Templates",
    description: "n8n 官方工作流目录，可按应用、任务和 AI 场景筛选并导入现成自动化流程。",
    descriptionEn: "n8n's official workflow directory for importing automations by app, task, and AI use case.",
    kind: "workflow-script", scene: "integration", tags: ["n8n", "自动化", "模板目录"], icon: "Workflow",
    source: "official", sourceLabel: "n8n", url: "https://n8n.io/workflows/",
  },
  {
    id: "workflow-dify-gallery", name: "Dify Workflows",
    description: "Dify 官方工作流展示与复用入口，覆盖 Agent、知识库、内容处理和业务自动化。",
    descriptionEn: "Dify's official workflow gallery for agents, knowledge bases, content processing, and business automation.",
    kind: "workflow-script", scene: "integration", tags: ["Dify", "Agent 工作流", "知识库"], icon: "Workflow",
    source: "official", sourceLabel: "Dify", url: "https://dify.ai/workflows",
  },
];

function installPrompt(spec: TemplateSpec): string {
  const action = spec.kind === "prompt-template" ? "选择并复制其中适合当前任务的 Prompt 模板" : "选择并导入其中适合当前任务的工作流";
  return `请帮我使用「${spec.name}」。\n\n官方来源：${spec.url}\n\n执行步骤：\n1. 阅读官方文档或 README，确认许可证和运行要求\n2. ${action}\n3. 只修改当前项目需要的变量、模型和凭据占位符，不覆盖已有配置\n4. 使用最小示例验证模板或工作流能够运行`;
}

export const templateItems: MarketplaceItem[] = specs.map((spec) => ({
  id: spec.id,
  category: "workflow",
  type: "installable",
  installableKind: spec.kind,
  name: spec.name,
  description: spec.description,
  descriptionEn: spec.descriptionEn,
  icon: spec.icon,
  iconColor: spec.kind === "prompt-template" ? "oklch(58% 0.14 250)" : "oklch(58% 0.14 155)",
  iconUrl: marketplaceIconUrl({ repositoryUrl: spec.repository, externalUrl: spec.url }),
  tags: spec.tags,
  capabilities: spec.kind === "prompt-template" ? ["prompting", "examples"] : ["workflow", "automation"],
  integrations: [],
  deploymentModes: spec.repository ? ["GitHub"] : ["Web"],
  trustLevel: "official",
  license: spec.license,
  scene: spec.scene,
  source: spec.source,
  sourceLabel: spec.sourceLabel,
  sourceUrl: spec.repository ?? spec.url,
  repositoryUrl: spec.repository,
  documentationUrl: spec.url,
  externalUrl: spec.url,
  readmeUrl: spec.readme,
  installPrompt: installPrompt(spec),
  targetHint: "先查看官方文档，再由 Agent 按当前环境完成导入和验证",
}));
