import type { Locale } from "./index";

/**
 * Catalogue enum labels. protocolLabels and platformLabels are absent on
 * purpose — their values are proper nouns ("OpenAI Chat Completions", "macOS")
 * and stay in src/lib/content.ts, shared by both languages.
 */
const catalog = {
  "zh-CN": {
    agentDescriptions: {
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
    } as Record<string, string>,
    groupLabels: {
      auto: "OneAgent 可管理",
      gateway: "网关 Agent",
      platform: "官方账号 Agent",
      ide: "IDE 扩展",
    } as Record<string, string>,
    protocolStatusLabels: {
      "implementation-supported": "实现已接入",
      "release-candidate-required": "需 Release Candidate 实证",
      verified: "已验证",
      unsupported: "不支持",
    } as Record<string, string>,
    agentFallbackDescription: "按项目公开能力提供安装或配置支持。",
  },
  en: {
    agentDescriptions: {
      codex: "OpenAI's terminal coding agent; OneAgent manages its Responses protocol configuration.",
      "claude-code": "Anthropic's terminal coding agent, configured through an Anthropic-compatible endpoint.",
      cursor: "AI editor. OneAgent only points you at the official install and sign-in, and writes no private config.",
      opencode: "Open-source terminal coding agent using the OpenAI-compatible protocol.",
      openclaw: "Multi-channel agent gateway; install, onboard and run it through the official flow.",
      hermes: "Self-improving agent framework; its own tooling owns model and runtime configuration.",
      "gemini-cli": "Google's official CLI, using Google sign-in, a Gemini API key or Vertex AI.",
      "kilo-cli": "Multi-model command-line agent. OneAgent can manage both install and OpenAI-compatible config.",
      aider: "Pair-programming repository editor; the model is named explicitly at launch.",
      kiro: "Account-based development agent using the vendor's own sign-in and configuration.",
      cline: "IDE extension; choose a compatible provider inside the extension.",
      continue: "IDE extension; add a compatible model in Continue's own configuration.",
      "qwen-code": "Qwen's coding tool; follow the official docs for install and provider setup.",
      "kilo-vscode": "Kilo Code's VS Code extension; providers are managed in the extension UI.",
    } as Record<string, string>,
    groupLabels: {
      auto: "OneAgent-managed",
      gateway: "Gateway agent",
      platform: "Vendor-account agent",
      ide: "IDE extension",
    } as Record<string, string>,
    protocolStatusLabels: {
      "implementation-supported": "Implemented",
      "release-candidate-required": "Needs release-candidate evidence",
      verified: "Verified",
      unsupported: "Unsupported",
    } as Record<string, string>,
    agentFallbackDescription: "Install or configuration support follows the project's published capabilities.",
  },
} as const;

export function useCatalogLabels(locale: Locale) {
  return catalog[locale];
}
