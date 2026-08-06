import { createContext, type PropsWithChildren, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";

const english = {
  "Agent 管家": "Agent Manager",
  "主导航": "Main navigation",
  "工作区": "Workspace",
  "激活环境": "Environment",
  "配置模板": "Profiles",
  "语言": "Language",
  "外观": "Appearance",
  "跟随系统": "System",
  "浅色": "Light",
  "深色": "Dark",
  "返回": "Back",
  "返回总览": "Back to overview",
  "继续": "Continue",
  "保存": "Save",
  "保存中": "Saving",
  "取消": "Cancel",
  "关闭编辑": "Close editor",
  "编辑": "Edit",
  "编辑配置": "Edit configuration",
  "详情": "Details",
  "删除": "Delete",
  "名称": "Name",
  "模型": "Model",
  "版本": "Version",
  "未知": "Unknown",
  "暂无": "None",
  "未记录": "Not recorded",
  "未绑定": "Not linked",
  // A token says which of the two things is absent, rather than sharing one
  // vague word between them: two pills both reading "not linked" left no way to
  // tell whether the Provider or the Profile was the missing one.
  "无 Provider": "No provider",
  "无 Profile": "No profile",
  "未安装": "Not installed",
  "已安装": "Installed",
  "待安装": "Not installed",
  "仅引导": "Guide only",
  "重试": "Retry",
  "已完成": "Completed",
  "失败": "Failed",
  "已取消": "Cancelled",
  "正在处理": "Processing",
  "等待执行": "Waiting",
  "高级选项": "Advanced options",
  "模型服务": "Provider",
  "检测状态": "Detected state",
  "备注": "Note",
  "新增 Provider": "Add provider",
  "搜索模型": "Search models",
  "模型列表": "Model list",
  "没有匹配的模型": "No matching models",
  "或手动输入模型 ID": "Or enter a model ID",
  "手动输入模型 ID": "Enter a model ID",
  "例如 gpt-4.1": "For example, gpt-4.1",
  "例如 deepseek/deepseek-v3": "For example, deepseek/deepseek-v3",
  "例如 siliconflow": "For example, siliconflow",
  "例如 team-ppio": "For example, team-ppio",
  "留空则使用 Profile ID": "Leave blank to use the Profile ID",
  "例如 团队 PPIO": "For example, Team PPIO",
  "粘贴你的 API Key": "Paste your API key",
  "隐藏密钥": "Hide API key",
  "显示密钥": "Show API key",
  "密钥只发送到当前本机服务，并保存在本机私有配置中。": "The key is sent only to the local service and stored in private local configuration.",
  "激活步骤": "Setup steps",
  "已跳过": "Skipped",
  "配置": "Configure",
  "编辑这个 Agent 关联的 Profile": "Edit the profile linked to this agent",
  "确认": "Review",
  "尚未测试连接": "Connection not tested",
  "正在验证端点和 Key": "Validating endpoint and key",
  "连接失败": "Connection failed",
  "查看安装日志": "View installation log",
  "任务中心": "Task center",
  "有任务正在运行": "A task is running",
  "暂无任务": "No tasks",
  "进行中": "In progress",
  "任务正在运行": "This task is already running",
  "取消任务": "Cancel task",
  "关闭任务": "Dismiss task",
  "返回任务页面：{title}": "Return to task page: {title}",
  "下载进度": "Download progress",
  "已下载 {done} MB / {total} MB（{percent}%）": "Downloaded {done} MB of {total} MB ({percent}%)",
  "已下载 {done} MB": "Downloaded {done} MB",
  "按官方方式安装与登录": "Install and sign in using the official method",
  "已检测到": "Detected",
  "官方安装": "Official install",
  "官方文档": "Official docs",
  "显示官方安装与配置步骤": "Show official installation and setup steps",
  "支持检测、安装与初始化配置": "Supports detection, installation, and initial setup",
  "选择 {name}": "Select {name}",
  "配置无法解析": "Configuration could not be parsed",
  "配置文件当前指向 {url}": "Configuration file currently points to {url}",
  "检测到的配置，非 OneAgent 写入": "Detected configuration not written by OneAgent",
  "未配置": "Not configured",
  "{version}（锁定 {lockedVersion}）": "{version} (locked to {lockedVersion})",
  "选择 Agent": "Select agents",
  "选择要安装的桌面 Agent，每次安装一个。": "Choose the desktop agent to install. One per run.",
  "安装应用后，会把选定的 Profile 应用到对应的配置。": "After installation, the selected profile is applied to the matching configuration.",
  "不支持": "Unsupported",
  "与 {name} 共用配置": "Shares configuration with {name}",
  "已安装，可直接应用 Profile": "Installed; a profile can be applied now",
  "安装官方桌面应用": "Install the official desktop application",
  "检测到本机已有此应用": "This application is already installed",
  "选择配置模板": "Select a profile",
  "Profile选择": "Select profile",
  "选择一个已有 Profile，或新建 Profile。": "Choose an existing profile or create a new one.",
  "新建 Profile": "Create profile",
  "创建 Profile": "Create profile",
  "选择一个 Profile": "Select a profile",
  "还没有可用的 Profile": "No usable profiles yet",
  "安装桌面 Agent": "Install desktop agent",
  "按引导安装桌面 Agent": "Follow the guide to install a desktop Agent",
  "确认安装": "Review installation",
  "将重新检查": "Recheck",
  "选择这次要安装并配置的开发工具，每次安装一个。": "Choose the development tool to install and configure. One per run.",
  "选择一个 Agent": "Select one agent",
  "正在检测本机环境": "Detecting the local environment",
  "常用 Agent": "Popular agents",
  "可一键配置的默认安装最新版本，仅引导的只显示官方步骤。": "One-click agents install the latest version by default; guide-only agents show official steps.",
  "更多 Agent（{count}）": "More agents ({count})",
  "网关、平台账号与 IDE 扩展": "Gateways, platform accounts, and IDE extensions",
  "无法读取已保存的 API Key": "Could not read the saved API key",
  "连接测试失败": "Connection test failed",
  "无法打开注册页面": "Could not open the registration page",
  "连接模型服务": "Connect a model provider",
  "Key 不会进入日志、URL 或前端持久化状态。": "The key is never written to logs, URLs, or persistent frontend state.",
  "继续选择模型": "Continue to model selection",
  "注册并获取 Key": "Register and get a key",
  "自定义模型名称（可选）": "Custom model name (optional)",
  "填写后将用此模型测试连接；留空时自动选择。": "When provided, this model is used for the connection test. Leave blank to select automatically.",
  "可选，仅用于测试连接；实际配置模型在下一步选择。": "Optional; used only for the connection test. Choose the configured model in the next step.",
  "测试连接": "Test connection",
  "修改这个 Agent 指向的 Provider 与模型": "Change which Provider and model this agent points at",
  "留空则使用该 Provider 已保存的 Key。": "Leave blank to use the key already saved for this Provider.",
  "未测试连接也可以应用，配置文件会先备份。": "You can apply without testing. The configuration file is backed up first.",
  "连接测试通过后才能继续选择模型。": "Pass the connection test before selecting a model.",
  "连接测试是可选的，可以直接继续选择模型。": "Connection testing is optional; you can continue to select a model directly.",
  "无法获取模型列表": "Could not load the model list",
  "选择模型": "Select a model",
  "从当前 Key 可访问的模型中选择，接口不支持时可直接输入模型 ID。": "Choose a model accessible with this key, or enter a model ID when discovery is unavailable.",
  "刷新列表": "Refresh list",
  "正在读取模型列表": "Loading models",
  "确认激活": "Review activation",
  "核对安装、配置和备份范围。API Key 不会显示在此页。": "Review the installation, configuration, and backup scope. The API key is not shown here.",
  "开始安装": "Start installation",
  "配置模板名称": "Profile name",
  "这次安装会保存为一个配置模板，之后可以直接应用": "This run is saved as a profile you can apply",
  "覆盖前会自动创建时间戳备份": "A timestamped backup is created before overwrite",
  "将处理": "Agents",
  "检测并配置": "Detect and configure",
  "安装并配置": "Install and configure",
  "显示引导": "Show guide",
  "只写配置": "Configure only",
  "本地写入": "Local changes",
  "桌面应用配置": "Desktop app configuration",
  "由桌面 Agent 决定": "Defined by the desktop agent",
  "由 Agent 官方配置合约决定": "Defined by the agent's official configuration contract",
  "环境摘要": "Environment summary",
  "仅引导项目": "Guide-only items",
  "{count} 个，不写私有配置": "{count}; no private configuration written",
  "激活失败": "Activation failed",
  "重试失败": "Retry failed",
  "正在安装": "Installing",
  "安装完成": "Installation complete",
  "安装命令行 Agent": "Install CLI agent",
  "安装 Agent": "Install agent",
  "尚未安装任何 Agent": "No agents installed yet",
  "需要处理部分问题": "Some items need attention",
  "安装请求同步执行，完成后将显示每个 Agent 的最终状态。": "Installation runs synchronously. Each agent's final status appears when it completes.",
  "每个 Agent 的结果彼此独立，失败项可以单独重试。": "Each agent has an independent result. Failed items can be retried individually.",
  "进入总览": "Open overview",
  "请保持此窗口打开": "Keep this window open",
  "下一步命令": "Next command",
  "环境总览": "Environment overview",
  "正在读取环境状态": "Loading environment status",
  "本机已安装 Agent 及其当前配置。": "Installed agents and their current local configuration.",
  "无法读取环境状态": "Could not load environment status",
  "请刷新后重试。": "Refresh and try again.",
  "本机已安装 Agent 及其当前 Provider、Profile 与模型。": "Installed agents and their current providers, profiles, and models.",
  "刷新状态": "Refresh status",
  "桌面 Agent": "Desktop Agent",
  "与 {name} 共用配置文件 {path}；安装和启动不会改动配置": "Shares the {path} configuration file with {name}; installation and launch do not modify it",
  "配置文件：{path}": "Configuration file: {path}",
  "状态": "Status",
  "位置": "Location",
  "打开": "Open",
  "已检测到应用，但版本信息不可用": "The app was detected, but version information is unavailable",
  "应用状态检测不可用": "Desktop app status could not be inspected",
  "{name} 已打开": "{name} opened",
  "{name} 安装完成": "{name} installed",
  "{name} 已安装": "{name} is already installed",
  "官方安装器已启动": "The official installer was started",
  "{name} 操作失败": "{name} action failed",
  "命令行 Agent": "Command-line Agent",
  "已安装 Agent": "Installed agents",
  "共 {count} 个": "Total: {count}",
  "尚未安装任何命令行 Agent": "No command-line agents installed",
  "启动": "Launch",
  "在新终端窗口中启动，并载入 OneAgent 写入的配置": "Open a new terminal window running this agent with the configuration OneAgent wrote",
  "无法启动 Agent": "Could not launch the agent",
  "更新": "Update",
  "更新中": "Updating",
  "更新完成": "Update complete",
  "更新 {name}": "Update {name}",
  "执行 npm update": "Run npm update",
  "无法更新 Agent": "Could not update the agent",
  "运行时": "Runtimes",
  "缺少 {count} 个运行时，安装后即可自动安装对应 Agent。": "{count} runtime(s) missing. Install them to enable automatic agent installation.",
  "Agent 安装所需的运行时都已就绪。": "Every runtime needed to install agents is ready.",
  "运行时安装失败": "Could not install the runtime",
  "版本 {version}": "Version {version}",
  "版本未知": "Version unknown",
  "{agents} 需要": "Required by {agents}",
  "锁定版本": "Locked version",
  "来源": "Source",
  "由 OneAgent 安装": "Installed by OneAgent",
  "本机已有": "Already on this machine",
  "安装": "Install",
  "安装 {name}": "Install {name}",
  "安装中": "Installing",
  "运行时会安装到 {dir}，并写入登录 PATH，不需要管理员权限。": "Runtimes install into {dir} and are added to your login PATH. No administrator rights needed.",
  "运行时会安装到 OneAgent 的托管目录，并写入登录 PATH，不需要管理员权限。": "Runtimes install into OneAgent's managed directory and are added to your login PATH. No administrator rights needed.",
  "运行时下载": "Runtime downloads",
  "下载源": "Download source",
  "Agent 安装源": "Agent install source",
  "默认使用官方源。国内网络较慢时可以改用镜像。": "Uses the official source by default. Switch to a mirror if it is slow from your network.",
  "正在优先使用国内镜像。": "Downloading from a regional mirror first.",
  "已根据系统地区设置默认使用镜像。可以改回官方源。": "A mirror is the default here based on your system region. You can switch back to the official source.",
  "优先使用国内镜像": "Prefer a regional mirror",
  "同时作用于运行时下载和 npm 安装的 Agent；运行时仍校验固定哈希并在下载失败时回退。Aider（uv）不受影响。": "Applies to runtime downloads and npm-installed agents. Runtime downloads still verify their pinned hash and fall back after a failed download. Aider (uv) is unaffected.",
  "已根据系统语言/地区自动开启。运行时仍校验固定哈希，npm 使用镜像 registry 的包元数据。": "Enabled automatically from your system language and region. Runtimes still verify their pinned hash; npm uses package metadata from the mirror registry.",
  "无法保存下载设置": "Could not save the download setting",
  "需要先安装运行时": "A runtime is needed first",
  "所选 Agent 通过 {runtimes} 安装，本机还没有。现在安装，或在激活时自动安装。": "The selected agents install through {runtimes}, which is not on this machine yet. Install it now, or let activation install it.",
  "安装 {name} {version}": "Install {name} {version}",
  "按引导安装命令行 Agent": "Follow the guide to install your first command-line agent",
  "找不到可配置的 Agent": "Configurable agent not found",
  "找不到 Agent": "Agent not found",
  "{id} 不在可一键配置的范围内。": "{id} is not available for one-click setup.",
  "未指定 Agent。": "No agent specified.",
  "应用配置失败": "Could not apply configuration",
  "应用中": "Applying",
  "应用": "Apply",
  "{name} 已应用": "{name} applied",
  "当前指向": "Current target",
  "配置文件": "Configuration file",
  "备份": "Backups",
  "已有历史备份": "Previous backup available",
  "这个 Agent 已有配置，不是 OneAgent 写入的": "This agent has configuration not written by OneAgent",
  "未知端点": "Unknown endpoint",
  "当前指向 {target}。应用后会被替换，原文件会先备份到同目录的": "Currently points to {target}. Applying will replace it; the original file will first be backed up in the same directory as",
  "时间戳": "timestamp",
  "将测试 {protocol} 协议": "Testing the {protocol} protocol",
  "可以指定具体模型。留空时由端点的模型列表自动选择，多数情况保持默认即可。": "Optionally choose a specific model. Leave blank for endpoint discovery; the default works in most cases.",
  "留空则由端点的模型列表自动选择": "Leave blank to select from the endpoint's model list",
  "快速小模型": "Fast small model",
  "找不到桌面 Agent": "Desktop agent not found",
  "留空则与主模型相同": "Leave blank to use the primary model",
  "已写入配置": "Configuration written",
  "无法读取 Provider": "Could not load provider",
  "无法保存 Provider": "Could not save provider",
  "已重新应用到 {agents}": "Reapplied to {agents}",
  "{agents} 重新应用失败：{message}": "Could not reapply to {agents}: {message}",
  "删除 Provider“{name}”？": "Delete provider \"{name}\"?",
  "无法删除 Provider": "Could not delete provider",
  "管理模型服务、端点与本机保存的 API Key。": "Manage model providers, endpoints, and locally saved API keys.",
  "编辑 {name}": "Edit {name}",
  "用户添加": "User added",
  "官网": "Website",
  "官网（可选）": "Website (optional)",
  "OpenAI 兼容 Base URL": "OpenAI-compatible base URL",
  "API 地址：": "API URL: ",
  "Anthropic 兼容 Base URL（可选）": "Anthropic-compatible base URL (optional)",
  "OpenAI 兼容": "OpenAI-compatible",
  "Anthropic 兼容": "Anthropic-compatible",
  "删除 {name}": "Delete {name}",
  "暂无 Agent 使用": "Not used by any agent",
  "已保存 Key": "Key saved",
  "未保存 Key": "No saved key",
  "用户 Provider 的协议兼容性由你自己保证，OneAgent 不会为它降级或改写请求。": "You are responsible for custom provider protocol compatibility. OneAgent does not downgrade or rewrite requests.",
  "无法保存 Profile": "Could not save profile",
  "应用 Profile 失败": "Could not apply profile",
  "{name} 已应用到 {count} 个 Agent": "Applied {name} to {count} agents",
  "无法应用 Profile": "Could not apply profile",
  "在这里创建 Profile，再将它应用到所选 Agent。": "Create profiles here, then apply them to selected agents.",
  "新增 Profile": "Add profile",
  "将使用 Provider 已保存的 Key。": "Uses the provider's saved key.",
  "这个 Provider 还没有 Key，先到 Provider 页面填写。": "This provider has no key yet. Add one on the Provider page.",
  "前往 Provider": "Open Provider",
  "保存 Profile": "Save profile",
  "编辑 Profile": "Edit profile",
  "为 {name} 选择关联的 Profile。": "Choose the profile linked to {name}.",
  "这个 Provider 还没有 Key，请先到 Provider 页面填写。": "This provider has no key yet. Add one on the Provider page.",
  "这个 Profile 还缺少 Provider Key 或模型": "This profile is missing a provider key or model",
  "创建一个 Profile 后即可应用到这个 Agent。": "Create a profile, then apply it to this agent.",
  "未指定": "Not specified",
  "应用完成": "Applied",
  "还没有 Profile": "No profiles yet",
  "走一遍安装引导，它会保存 Provider、模型和 API mode。": "Run the install guide; it saves the provider, model, and API mode.",
  "Provider 已有 Key": "Provider key saved",
  "Provider 缺少 Key": "Provider key missing",
  "未指定模型": "No model specified",
  "未选择 Agent": "No agents selected",
  "适用：{agents}": "Agents: {agents}",
  "安装缺失的 Agent 并应用此 Profile": "Install missing agents and apply this profile",
  "请先补全模型和 API mode，并为 Provider 保存 Key": "Add a model and API mode, and save a key for the provider",
  "应用到 Agent": "Apply to agents",
  "无法读取本机状态": "Could not read local status",
  "OneAgent 请求失败": "OneAgent request failed",
  "无法调用本机 OneAgent 服务": "Could not call the local OneAgent service",
  "OpenAI 的终端编码代理": "OpenAI's terminal coding agent",
  "Anthropic 的终端编码代理": "Anthropic's terminal coding agent",
  "开源终端编码代理": "Open-source terminal coding agent",
  "多模型编排的命令行代理": "Command-line agent with multi-model orchestration",
  "结对编程式的仓库编辑代理": "Pair-programming repository editing agent",
  "AI 编辑器，按官方方式安装": "AI editor installed through the official channel",
  "多渠道 AI 网关，常驻运行": "Persistent multi-channel AI gateway",
  "自我成长型 Agent 框架": "Self-improving agent framework",
} as const;

export type Locale = "zh-CN" | "en";
export type TranslationKey = keyof typeof english;
export type TranslationValues = Record<string, string | number>;
export type Translate = (key: TranslationKey, values?: TranslationValues) => string;

export const LOCALE_STORAGE_KEY = "oneagent.locale";

function interpolate(template: string, values: TranslationValues = {}): string {
  return template.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : placeholder,
  );
}

export function translate(locale: Locale, key: TranslationKey, values?: TranslationValues): string {
  return interpolate(locale === "en" ? english[key] : key, values);
}

export const sourceTranslate: Translate = (key, values) => translate("zh-CN", key, values);

function preferredLocale(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_STORAGE_KEY);
    if (saved === "en" || saved === "zh-CN") return saved;
  } catch {
    // Storage can be unavailable in hardened webviews; system language still works.
  }
  return typeof navigator !== "undefined" && !navigator.language.toLowerCase().startsWith("zh") ? "en" : "zh-CN";
}

let activeLocale: Locale | undefined;

export function currentLocale(): Locale {
  try {
    const saved = localStorage.getItem(LOCALE_STORAGE_KEY);
    if (saved === "en" || saved === "zh-CN") return saved;
  } catch {
    // Fall through to the active provider or system language.
  }
  return activeLocale ?? preferredLocale();
}

interface I18nContextValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: Translate;
}

const fallback: I18nContextValue = {
  locale: "zh-CN",
  setLocale: () => undefined,
  t: sourceTranslate,
};

const I18nContext = createContext<I18nContextValue>(fallback);

export function I18nProvider({ children }: PropsWithChildren) {
  const [locale, setLocaleState] = useState<Locale>(preferredLocale);
  const localeRef = useRef(locale);
  localeRef.current = locale;
  activeLocale = locale;
  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      localStorage.setItem(LOCALE_STORAGE_KEY, next);
    } catch {
      // The in-memory choice remains active when persistence is unavailable.
    }
  }, []);
  const t = useCallback<Translate>((key, values) => translate(localeRef.current, key, values), []);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const value = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nContextValue {
  return useContext(I18nContext);
}
