import type { Locale } from "./index";

/**
 * Chrome and shared-component copy. Page prose stays in the page templates —
 * only strings reused across pages, or read by more than one component, belong
 * here.
 */
const strings = {
  "zh-CN": {
    "site.title": "OneAgent — 可信的本地 AI 开发环境激活器",
    "site.description": "用一个可信的本地流程，激活你自己的 Agent、账号和 Provider。",
    "skip.main": "跳到主要内容",
    "nav.home": "OneAgent 首页",
    "nav.downloads": "下载",
    "nav.quickstart": "快速开始",
    "nav.explorer": "配置",
    "nav.agents": "Agent",
    "nav.providers": "Provider",
    "nav.support": "支持",
    "nav.changelog": "更新日志",
    "nav.primary": "主导航",
    "nav.mobile": "移动端导航",
    "nav.openMenu": "打开导航菜单",
    /* Only ever shown on an English page, where it warns that the target has no
       translation yet. A Chinese reader never sees it, but the key exists in both
       dictionaries so useTranslations stays exhaustive. */
    "nav.chineseOnly": "仅中文",
    "nav.menu": "菜单",
    "cta.download": "下载预览版",
    "theme.toggle": "切换深色模式",
    "lang.switch": "切换语言",
    "breadcrumb.label": "面包屑",
    "breadcrumb.home": "首页",
    "footer.tagline": "可信的本地 AI 开发环境激活器。",
    "footer.boundary": "不提供共享 Key，不代理模型请求，不重新分发第三方 Agent。",
    "footer.start": "开始",
    "footer.capability": "能力",
    "footer.trust": "信任",
    "footer.downloadCenter": "下载中心",
    "footer.agentCatalog": "Agent 兼容目录",
    "footer.providerCatalog": "Provider 目录",
    "footer.supportFeedback": "支持与反馈",
    "footer.releaseIndex": "GitHub Releases",
    "footer.channel": "当前公开渠道：technical-preview-unsigned",
  },
  en: {
    "site.title": "OneAgent — a trustworthy local AI development environment activator",
    "site.description": "Activate your own agents, accounts and providers through one auditable local flow.",
    "skip.main": "Skip to main content",
    "nav.home": "OneAgent home",
    "nav.downloads": "Downloads",
    "nav.quickstart": "Quickstart",
    "nav.explorer": "Explorer",
    "nav.agents": "Agents",
    "nav.providers": "Providers",
    "nav.support": "Support",
    "nav.changelog": "Changelog",
    "nav.primary": "Primary navigation",
    "nav.mobile": "Mobile navigation",
    "nav.openMenu": "Open navigation menu",
    "nav.chineseOnly": "in Chinese",
    "nav.menu": "Menu",
    "cta.download": "Get the preview",
    "theme.toggle": "Toggle dark mode",
    "lang.switch": "Change language",
    "breadcrumb.label": "Breadcrumb",
    "breadcrumb.home": "Home",
    "footer.tagline": "A trustworthy local AI development environment activator.",
    "footer.boundary": "No shared keys, no proxied model requests, no redistribution of third-party agents.",
    "footer.start": "Start",
    "footer.capability": "Capabilities",
    "footer.trust": "Trust",
    "footer.downloadCenter": "Download centre",
    "footer.agentCatalog": "Agent compatibility",
    "footer.providerCatalog": "Provider catalogue",
    "footer.supportFeedback": "Support & feedback",
    "footer.releaseIndex": "GitHub Releases",
    "footer.channel": "Current public channel: technical-preview-unsigned",
  },
} as const;

export type UIKey = keyof (typeof strings)["zh-CN"];

export function useTranslations(locale: Locale) {
  return (key: UIKey): string => strings[locale][key] ?? strings["zh-CN"][key];
}
