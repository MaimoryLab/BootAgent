# 工具市场计划

> 状态：阶段一实施中（2026-08-24）。

## 背景与定位

工具市场是 Agent 能力的统一发现中心，解决「不知道有哪些扩展、安装流程分散」的痛点。
纯发现层，不读取也不影响 Skills / MCP 管理页面的真实安装状态。

安装方式统一采用「复制提示词」——卡片上提供一键复制按钮，用户将提示词粘贴到对应
Agent 对话框即可完成安装，无需引导式流程。

## 分类体系

| 类目 ID | 显示名 | 说明 |
| --- | --- | --- |
| `agent-enhance` | 单 Agent 增强 | Skills Pack、配置模板 |
| `cross-agent` | 跨 Agent 协作 | 全局记忆、会话迁移工具 |
| `mcp-server` | MCP 服务器 | Sequential Thinking 等 |
| `news` | 资讯与学习 | 周报订阅、最佳实践 |
| `ecosystem` | 生态推荐 | 独立运行软件，仅外链，不集成 |

「BootAgent 管理」（卸载 Agent、清理配置等）**不属于市场条目**，属于
`AgentManageRow` 的操作下拉菜单改造，计划在阶段二实施。

## 工具类型

1. **可安装内容**（`installable`）：Skills / MCP / System Prompt Template /
   Workflow Script，通过复制提示词安装。
2. **内容型条目**（`content`）：文章、订阅链接，仅展示，无安装操作。
3. **外部工具链接**（`external-link`）：独立软件，放「生态推荐」分区，
   点击跳转外部 URL，不集成到 BootAgent。

## 数据模型

类型定义见 `frontend/src/types/marketplace.ts`。

核心字段：

- `id / category / type / name / description / tags`
- `installableKind`：`skill | mcp | prompt-template | workflow-script`
- `installPrompt`：要复制的提示词正文（`type === "installable"` 时必填）
- `targetHint`：粘贴目标提示，例如「复制后粘贴到 Claude Code 对话框中」
- `sourceLabel / sourceUrl`：参考来源标注，非安装地址
- `externalUrl`：外部工具链接（`type === "external-link"` 时使用）

## 内容来源与获取策略

### 阶段一的内容来源（静态快照）

静态目录由 `manifests/marketplace.lock.json` 管理，随 Go 二进制通过 `go:embed`
提供给前端；SkillHub 与 MCP Servers 的在线数据由 Go 侧来源适配器归一化后增量追加，
静态目录仍作为离线基线。

### 阶段三的内容来源（远程索引，计划）

- 维护一份中心化索引 JSON，部署在我们控制的静态托管上（参考来源：
  skillhub.cloud.tencent.com、mcpservers.org）。
- 拉取走 Go 后端新增的 `MarketplaceService`，前端不在 renderer 中直接
  fetch 外部域名，符合「后端持有 URL」的既有惯例。
- 失败时读取 `~/.bootagent/marketplace-cache.json` 兜底，返回值中带
  `stale: true` 标记，前端提示「离线，数据可能不是最新」。
- 需要评估与 local-first 基线的兼容性后单独决策是否引入远程请求。

## 路由与导航

```
/marketplace          工具市场主页（含分类 Tab）
```

`NavigationSidebar.tsx` 的 `navItems` 新增一条 `{ to: "/marketplace", ... }`。

## 文件清单

**新增：**

- `frontend/src/types/marketplace.ts` — 数据模型类型定义
- `manifests/marketplace.lock.json` — 嵌入式静态目录
- `frontend/src/utils/clipboard.ts` — 剪贴板工具函数
- `frontend/src/components/CopyPromptButton.tsx` — 复制按钮组件
- `frontend/src/pages/MarketplacePage.tsx` — 市场主页
- `frontend/src/pages/MarketplacePage.test.tsx` — 页面单元测试

**修改：**

- `frontend/src/App.tsx` — 新增 `/marketplace` 路由
- `frontend/src/components/NavigationSidebar.tsx` — 新增导航项
- `frontend/src/i18n.tsx` — 新增中英文 key
- `frontend/src/styles/app.css` — 新增 `.marketplace-*` 样式

## 阶段计划

### 阶段一（已实施）

- `MarketplacePage` + 嵌入式静态目录 + 复制提示词流程 + 生态推荐 Tab
- `MarketplaceService.Catalog` 通过 Wails 向前端提供目录
- 通过 `pnpm run typecheck`、`pnpm run test`、`pnpm run build`

### 阶段二（待排期）

- `AgentActionsMenu` 下拉组件，改造 `AgentManageRow`
- 新增卸载 Agent、清理配置文件的 Go binding
- 风险高于阶段一（涉及破坏性操作），需独立评估和测试

### 阶段三（待评估）

- 接入远程索引拉取 + 本地缓存 + 离线兜底
- 需单独评估与项目 local-first 基线的张力

## 架构约束

- 不在 renderer 直接 fetch 外部域名（阶段一无网络请求）
- 不读取 Skills / MCP 管理页面的真实安装状态，两者数据独立
- 卡片视觉复用 `.provider-card` 规范（`--radius-card`、同 border/padding）
- i18n：中文为源语言，新增 key 必须同步加入 `english` 字典
- `assetsInlineLimit: Number.MAX_SAFE_INTEGER`：禁止任何外部/CDN 引用

## 在线数据源（2026-08-25 落地）

运行时在线拉取已实现，静态目录由 Go manifest 提供：

- **skillhub 部分走 showcase API 后端代理**：`useMarketplaceCatalog` hook
  （`frontend/src/data/useMarketplaceCatalog.ts`）在首次挂载时通过
  `MarketplaceService.FetchShowcase` 请求 `https://api.skillhub.cn/api/v1/showcase/hot`，
  成功后用与快照相同的映射逻辑（`skillhub-adapter.ts` 导出的
  `mapSkillhubEntry`）替换目录中 `source === "skillhub"` 的条目，并在
  列表页 Tab 栏显示「实时数据」绿点标识。在线数据的 `subCategories` 是
  `[{key,name}]` 对象数组、`labels.requires_api_key` 是字符串
  `"true"/"false"`，由 `normalizeShowcaseSkill` 归一化成快照条目形状。
- **CORS 边界**：renderer 不直接访问 SkillHub；Go `MarketplaceService` 负责
  后端请求，避免 WebView 的 Origin 白名单限制。
- **快照兜底**：fetch 失败（断网、超时、payload 异常）保持嵌入式 manifest
  快照，标识显示「离线快照」。模块级 promise 缓存保证
  列表页与详情页共用同一次请求，每次应用会话至多 fetch 一次。
- **其他来源仍为快照**：统一写入 `manifests/marketplace.lock.json`，不再在
  `frontend/src/data/` 中维护条目代码或快照文件。
- **MCP Servers 在线适配**：列表主源通过
  `https://mcpservers.org/all?page=` 与 `/search?query=&page=` 获取公开分页 HTML，
  后端按卡片解析名称、分类、图标、仓库、官方标识和 GitHub stars；SkillHub 的
  `/api/v1/mcp/servers` 只作为有界补充和主目录故障回退。打开目录卡片后，Go binding
  从对应详情页的服务端序列化记录获取官网、仓库、标签、更新时间和统计，并延迟提取
  `markdownContent` 作为 README。旧版 SkillHub slug 卡片继续走原有单条接口。请求由
  Go binding 发起，详情失败时保留列表摘要，不影响静态目录。
- **详情页增强**：skillhub 条目详情页额外通过后端代理读取
  `https://api.skillhub.cn/api/v1/skills/{slug}` 渲染安全审核
  （科恩/三堡）、版本信息与作者区块，失败静默降级不阻塞页面
  （受上述同样的 CORS 白名单限制）。
- **后续项：双语安装提示词**。`installPrompt` / `targetHint` 目前仅中文
  （提示词是给中文用户的 Agent 执行的）；英文 locale 下的双语提示词
  工作量较大，待后续排期。

> 注：市场目录和 SkillHub 数据均由后端持有外部 URL，renderer 只接收公开结果。
