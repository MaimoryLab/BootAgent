export const agentDescriptions: Record<string, string> = {
  codex: "OpenAI 的终端编码 Agent，OneAgent 管理 Responses 协议配置。",
  "claude-code": "Anthropic 的终端编码 Agent，使用 Anthropic-compatible 配置。",
  cursor: "AI 编辑器；OneAgent 只提供官方安装与登录引导，不写入私有配置。",
  opencode: "开源终端编码 Agent，使用 OpenAI-compatible 协议。",
  openclaw: "多渠道 Agent 网关；按官方流程安装、onboard 与管理服务。",
  hermes: "自我成长型 Agent 框架；模型与运行配置由官方工具管理。",
  "gemini-cli": "Google 官方 CLI；使用 Google 登录、Gemini API Key 或 Vertex AI。",
  "kilo-cli": "多模型命令行 Agent，OneAgent 可管理安装与 OpenAI-compatible 配置。",
  aider: "结对编程式仓库编辑 Agent，模型在启动命令中明确选择。",
  kiro: "官方账号型开发 Agent；使用官方登录与配置流程。",
  cline: "IDE 扩展；在扩展界面选择兼容 Provider。",
  continue: "IDE 扩展；在 Continue 配置中添加兼容模型。",
  "qwen-code": "Qwen 的编码工具；按官方文档完成安装和 Provider 配置。",
  "kilo-vscode": "Kilo Code 的 VS Code 扩展；由扩展界面管理 Provider。",
};

export const groupLabels: Record<string, string> = {
  auto: "OneAgent 可管理",
  gateway: "Gateway Agent",
  platform: "官方账号 Agent",
  ide: "IDE 扩展",
};

export const protocolLabels: Record<string, string> = {
  openai: "OpenAI Chat Completions",
  anthropic: "Anthropic Messages",
  responses: "OpenAI Responses",
};

export const protocolStatusLabels: Record<string, string> = {
  "implementation-supported": "实现已接入",
  "release-candidate-required": "需 Release Candidate 实证",
  verified: "已验证",
  unsupported: "不支持",
};

export const platformLabels: Record<string, string> = {
  macos: "macOS",
  windows: "Windows",
  linux: "Linux",
};
