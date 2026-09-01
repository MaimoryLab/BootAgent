# 对标 cc-switch 的 UX 与 Agent 适配优化方案

> 状态：待评审。起草于 2026-08-17。对照对象为 cc-switch v3.19.2 源码（Tauri 2 + Rust），
> 本仓库现状核对至 `feat/skills-mcp-ui` 分支（`08de571`）。文中 cc-switch 的行为均出自
> 其源码调研，BootAgent 的行为均带本仓库 `file:line`。

## 1. 对照结论：两边的产品模型差异

cc-switch 是「切换器」：打开就是供应商卡片列表，单击即切换；托盘不开窗口也能切，
且托盘里直接显示订阅用量。它的适配深度不均——Claude Code / Codex 的配置是整文件
覆盖（靠切换前回填保住用户改动，回填失败仅告警继续），只有 OpenClaw / Hermes 做到
注释级保留，只有 Claude Desktop 有配置漂移检测。

BootAgent 是「配置中心」：owned-key 白名单合并（`internal/config/write.go`）、语法冲突
拒写、逐 Agent 考证凭据落点，写入正确性和可迁移性显著更强。代价是高频动作路径长
（切换最短 3 步）、正确性优势用户不可见（备份存在但界面看不到）、能力约束靠激活时
报错而非选择时预告。

由此得出本方案的主线：**把高频动作降到 1 步；把写入正确性变成看得见的界面；把
Agent 能力差异从代码分支提升为声明式数据；补上 Claude Desktop 这一空白。**

## 2. 目标与非目标

目标（全部可度量，见 §11 汇总）：

1. Overview 上切换 Profile 从 3 步降到 1–2 步，托盘可直切。
2. 外部修改从「完全无感知」变为「刷新即见、精确到键、可恢复可采纳」。
3. reasoning / context / 协议等能力约束在选择时预告，激活期报错仅剩兜底。
4. 已有手工配置可一键导入为 Provider + Profile，不动 live 文件。
5. macOS / Windows 上支持 Claude Desktop（直连模式）。

非目标（明确不做，防止范围蔓延）：

- **本地代理路由**（协议互转、故障转移、请求级用量计费）。这是 cc-switch 的护城河，
  但依赖常驻中间人代理，与本产品「不落敏感数据」的承诺冲突面大，且现有
  「本地格式转换」功能已占据类似生态位。是否演进为可选路由层是独立的产品决策，
  不在本方案内。
- 用量统计（依赖代理截流或会话日志解析，二期单独评估）。
- Linux 桌面 Agent 支持（`internal/app/desktopapp.go:235` 的现状维持）。

## 3. 里程碑总览

| 里程碑 | 优先级 | 规模估算 | 依赖 |
| --- | --- | --- | --- |
| M1 一步切换（行内快切 + 托盘直切） | P0 | 中（前端为主 + 托盘 Go） | 无 |
| M2 配置健康（漂移检测 + 备份可视化 + 激活前预览） | P0 | 大 | 无，可与 M1 并行 |
| M3 Capability 矩阵声明化 | P1 | 中偏大 | 无 |
| M4 现有配置反向导入 | P1 | 中 | 复用 M2 的读回强化 |
| M5 Claude Desktop 直连适配 | P1 | 大 | M3 软前置（协议约束落点） |
| M6 防腐与技术债 | P2 | 小 × 4，穿插进行 | 无 |

每个里程碑独立可发布，不做大爆炸式合并。

## 4. M1 一步切换

### 对标点

cc-switch 的日活体验建立在「1 步切换」上：主界面点卡片即切，托盘
`handle_provider_tray_event` 直切且显示用量。BootAgent 目前最短路径 3 步
（`/profiles` → 应用 → 勾选目标确认，或 overview → Agent 详情 → 选 Profile），托盘
（`cmd/bootagent-desktop/main_wails.go:61-118`）只服务格式转换功能，且文案硬编码中文。

### 改动点

- 前端 `frontend/src/pages/EnvironmentOverviewPage.tsx`、`frontend/src/components/AgentRow.tsx`
  / `AgentManageRow.tsx`：已安装且非 guide-only 的 Agent 行内加 Profile 快切下拉。
  数据无需新接口：`StatusResponse` 已含 `profiles`（`ProfileSummary` 带 `protocol`）与
  `catalog`（`CatalogItem.Protocol`），前端即可完成协议兼容过滤。
- 应用动作复用现有激活链路：`internal/app/agent.go:47`（`ActivateAgent`，统一写锁
  `writeMu` 天然防并发交错）；桌面 Agent 走既有 `writeDesktopAgentConfig` 路径。
- 托盘：`cmd/bootagent-desktop/main_wails.go` 增加「Agent → 兼容 Profile」二级菜单，
  当前绑定项打勾；直接调用 Go 用例（与 `SetConversionTargetProfile` 同构），不经前端。
  托盘文案同步接入语言设置，消除硬编码中文。

### 方案要点

- 下拉只列协议兼容的 Profile；不兼容的置灰并展示原因（复用 `agent.go:130-132` 的
  校验语义，文案前移）。
- 应用中行内显示进行态；成功后行内更新 Profile 名与 restart 提示；失败行内展示后端
  错误，不跳页、绑定不变。
- 无兼容 Profile 时下拉给「去创建」入口，指向 `/profiles`。

### 验收标准（M5 Claude Desktop）

1. Overview 上对任一已安装 CLI Agent 切换 Profile：点击展开 + 点击选择，共 2 次交互
   完成（无确认弹窗；不再需要进入 Agent 详情页）。桌面 Agent 同样可用。
2. 协议不匹配的 Profile 在下拉中置灰且带原因文案（例如「该 Profile 是 Anthropic
   协议，Codex 需要 OpenAI」），不可选中，而不是选中后报错。
3. 应用失败（如网络探测失败、配置写入被拒）时：行内展示错误信息，Agent 原绑定与
   live 文件均不变（以 `AgentStatus.detected` 复核）。
4. 托盘出现「切换 Profile」分组：每个已安装 Agent 一个子菜单，当前项打勾；主窗口
   关闭状态下点击可完成切换，完成后菜单勾选状态刷新。
5. 并发保护：新增一条并发测试证明快速连续切换两次不产生交错写入（断言最终 live
   与最后一次选择一致，备份序列完整）。
6. 托盘菜单文案跟随应用语言设置。
7. 门禁：`go test ./...`、`go test -race ./...`、frontend `pnpm run test` /
   `pnpm run test:e2e` 全绿；e2e 新增行内快切主路径用例。托盘路径 e2e 无法覆盖，
   附手工验收清单（macOS / Windows / Linux 各跑一遍）。

## 5. M2 配置健康：漂移检测、备份可视化、激活前预览

### 对标点（M2 配置健康）

cc-switch 只对 Claude Desktop 做漂移检测（expected vs actual base_url、非法模型名），
但它的黄条 +「重新切换当前供应商可修复」动作提示是很好的交互范式。BootAgent 的
owned-key 模型让我们能把这件事做到**全部 13 个适配器、精确到键**——这是 cc-switch
整文件覆盖的架构做不到的，是本方案里最能拉开差距的一项。

### 现状地基（已核实）

- `internal/config/discovery.go` 已有逐 Agent 读回（`ReadPIConfig:109`、
  `ReadOpenClawConfig:136` 等），刻意只读 BootAgent 自有条目；结果经
  `internal/app/status.go:507` 进入 `AgentStatus.detected`（`baseUrl` / `model` /
  `managedByBootAgent` / `unreadable` 四个字段）。
- 期望值基准已存在：Agent 绑定（`internal/profile` 的 `AgentBinding`，含 provider /
  baseURL / model / reasoningEffort）。
- 备份基建已存在：`securefs.AtomicWrite` 自动备份，`~/.bootagent/backup` 每目标保留
  3 份，Settings 可调——但 UI 完全没有入口。

### 改动点（M2 配置健康）

- `internal/config`：把各适配器隐含在 merge 实现里的 owned-key 集合提取为公共的
  `OwnedState(binding) → map[key]expected` + `ReadOwnedState(live) → map[key]actual`，
  drift = 两者逐键对比。`Detected` 从「baseURL + model」扩展到 reasoning / context
  等全部受管键；API key 只比对存在性与哈希，任何界面与日志不出现明文。
- `internal/app`：新增 `AgentConfigHealth` 用例与 DTO；漂移计算挂在既有
  `StatusResponse` 组装路径上（status 每次刷新即重算，天然覆盖「窗口重新聚焦 /
  手动刷新」两个触发时机，不引入文件监听）。
- 两个修复动作：**恢复受管值**（用已存绑定重放 `activateAgentLocked`）与
  **采纳外部修改**（把 live 实际值写回绑定，若绑定挂 Profile 则提示「仅本 Agent
  生效」或「更新 Profile」二选一）。
- 备份可视化：新增列出 / 回滚用例（回滚前先对当前文件再做一次备份），前端在
  Agent 详情页展示备份历史。
- 激活前 diff 预览：应用 Profile 的确认界面展示「将写入哪些文件、改哪些键、
  前后值」（owned-key 模型的天然能力；密钥显示为掩码）。
- DTO 变化按 AGENTS.md 红线三处同步：重新生成 `frontend/bindings`、更新
  `frontend/src/backend/wails.ts` 与 `frontend/src/types/api.ts`。

### 验收标准（M2 配置健康）

1. 手工把 `~/.codex/config.toml` 的受管 `model` 改成其他值 → 窗口重新聚焦后
   overview 该行出现漂移徽标；详情页显示「model：期望 X，实际 Y」，精确到键。
2. **零误报是硬标准**：外部修改非受管内容（用户自己的 section、注释、无关键）不
   触发漂移。13 个适配器各至少一条正例 + 一条反例测试（golden fixtures 扩展）。
3. 「恢复受管值」后 live 恢复到绑定描述的状态，用户自有字段原样保留（复用既有
   merge 语义测试断言）。
4. 「采纳外部修改」后徽标消失，绑定更新，重启 BootAgent 后状态一致（幂等）。
5. API key 漂移显示为「凭据已被外部修改」，明文不出现在 DTO、界面、日志（对日志
   与 DTO 序列化做 grep 断言测试）。
6. 配置文件缺失 / 解析失败时健康区块给出确定态文案（沿用 `Detected.Unreadable`
   语义），不崩溃、不误报漂移。
7. 备份历史：详情页能看到该 Agent 配置目标最近 N 份备份（时间戳排序）；一键回滚
   前自动再备份当前文件；回滚后展示恢复结果。
8. 激活前预览与实际写入一致：预览列出的键值改动与落盘后的读回逐键相等（测试用
   同一 OwnedState 函数断言，防止两套实现漂移）。
9. 门禁全绿，含 `go test -race`（status 刷新与激活并发路径）。

## 6. M3 Capability 矩阵声明化

### 对标点（M3 Capability 矩阵）

cc-switch 新增一个 Agent 要动 41 个文件里的 `AppType` match（Hermes 实际摸了 15 个
文件 56 处）。BootAgent 的 CLI Agent 已数据驱动（`manifests/agents.lock.json` +
`internal/catalog/types.go:20`），但能力约束仍是代码：reasoning 三套收窄映射
（`internal/config/write.go:74-153`）、7 个 Agent 的静默丢弃清单
（`internal/app/agent.go:237-248`）、ZCode 协议写死 openai
（`internal/config/write.go:311-316`）。后果是 UI 无法预知约束，只能激活时报错。

### 改动点（M3 Capability 矩阵）

- `internal/catalog/types.go` 的 `Agent` 增加 `Capabilities` 子结构（示意）：

  ```json
  "capabilities": {
    "reasoning": { "levels": ["off", "low", "medium", "high", "xhigh"],
                   "map": { "max": "xhigh", "off": "none" } },
    "context_window": "none | boolean_1m | numeric",
    "protocols": ["openai"],
    "restart": "required | hot | none"
  }
  ```

  按现状**翻译而非改动行为**：Codex / Aider / OpenCode / Kilo / dsh-official 各自的
  词表照搬现有映射函数；7 个不支持 reasoning 的 Agent 写显式 `"reasoning": null`。
  显式否定与「漏写字段」在 schema 校验中区分开：漏写即加载失败，杜绝「忘记声明」
  静默通过。
- `internal/catalog` 加载期校验词表合法性；`CatalogItem` 向前端暴露所需投影
  （沿用 `SelectsModel` 的先例：UI 需要词表本身时暴露数组，需要决策时暴露布尔）。
- `internal/app/agent.go` 与 `internal/config/write.go`：映射函数保留为实现，但由
  capability 数据选择路由；`ModelSelection` 字段并入 capabilities（保留 JSON 兼容读取）。
- 前端：应用 Profile 弹窗与 Agent 详情页，对每个目标 Agent 显示该 Profile 的
  reasoning 将「直传 / 映射为 X / 被忽略（原因）」；档位选择器按能力联动。

### 验收标准（M3 Capability 矩阵）

1. `agents.lock.json` 全部 13 个 CLI Agent 都有显式 capabilities；缺失或词表非法时
   `catalog.LoadEmbedded` 报错，配单测。
2. **行为零回归**：改造前后 reasoning / context 的落盘结果完全一致——以现有
   `internal/config/write_test.go` 与 golden fixtures 为准绳，改造不修改任何既有
   fixture 内容即视为达标。
3. 应用 Profile 弹窗中（举例验收）：勾选 Claude Code 时 reasoning=high 的 Profile
   显示「Claude Code 无档位设置，将忽略」；勾选 Codex 显示「high 直传」；勾选
   dsh 官方 route 时 medium 显示「不支持」。文案与后端行为同源（同一 capability
   数据），新增测试断言 UI 展示逻辑与后端翻译函数在全档位 × 全 Agent 矩阵上一致。
4. 原「激活时才报 reasoning 不支持」的路径全部前移为选择时预告；激活期该类报错
   仅剩数据竞争兜底（能触发它的 UI 路径不复存在）。
5. 扩展性演示测试：注入一个 test-only Agent 词表（不改 Go 代码），翻译与 UI 约束
   即生效。
6. 门禁全绿 + bindings / `wails.ts` / `api.ts` 三处同步。

## 7. M4 现有配置反向导入

### 对标点（M4 反向导入）

cc-switch 有三套回流机制（首启动把 live 导入为 default 供应商、additive 类每次启动
反向同步、切换前回填）。BootAgent 因 owned-key 模型不需要回填，但「把用户已有的
手工配置变成 Provider + Profile」的入口完全缺失——现有 `discovery.go` 刻意只读
BootAgent 自有条目（`ReadPIConfig:106-108` 的注释就是这个立场），对手工配置无能为力。
这挡住了存量用户的迁移。

### 改动点（M4 反向导入）

- `internal/config/discovery.go`：新增 foreign 读取路径（与现有 owned 读取并列，
  不改变后者语义），按适配器解析「用户手工配置的 baseURL / model / key 所在」。
  只在用户显式点击导入时才读 key，读取结果直接进 secret 存储。
- 新用例 `ImportDetectedConfig`：生成 custom Provider（baseURL 未知时）+ Profile +
  该 Agent 的绑定。**导入不写任何 Agent live 文件。**
- 前端：Setup 向导与 overview 空态出现「检测到已有配置，导入为 Profile」卡片。

### 验收标准（M4 反向导入）

1. 机器上已有手配的 `~/.claude/settings.json`（第三方 baseURL + key + model）：首启
   向导出现导入卡片，一键后生成 Provider（baseURL 正确）+ Profile（model / 协议
   正确）+ 绑定；全程 live 文件字节不变（对文件内容做前后哈希断言——mtime 不可靠）。
2. key 进 secret 存储，UI 不回显明文，DTO 与日志无泄漏（grep 断言）。
3. 幂等：重复导入同一配置不产生重复 Provider / Profile（按 baseURL + model 判重，
   命中时提示并复用）。
4. 至少覆盖三种格式代表：Codex（TOML）、Claude Code（JSON env）、OpenCode
   （JSON provider 段）的导入测试。
5. 无法解析（损坏 / 未来版本 shape）时给出确定失败文案，不产生半成品 Provider。

## 8. M5 Claude Desktop 直连适配

### 对标点与机制依据

cc-switch 对 Anthropic 官方桌面客户端的适配依赖其自带的「第三方部署模式」：
`claude_desktop_config.json` 的 `deploymentMode`（`1p`/`3p`）+ `Claude-3p/configLibrary/`
下的 profile JSON（`inferenceGatewayBaseUrl` / `inferenceGatewayApiKey` /
`inferenceModels` / `inferenceProvider: "gateway"`）。cc-switch 用固定 UUID 注册自有
profile，写前 4 文件快照、失败整体回滚，恢复官方时只摘除自己的痕迹。

BootAgent 桌面端已支持 ChatGPT Desktop / WorkBuddy / ZCode / DSH Desktop，唯独缺
Claude Desktop。本里程碑只做**直连模式**：Anthropic 协议 Provider + `claude-*` 安全
模型名。模型映射玩法（在 Claude Desktop 里用非 Claude 模型）依赖本地代理，属 §2
非目标。

### 改动点（M5 Claude Desktop）

- `internal/desktopapp/registry.go` 新增 Definition：`inspect`（检测安装 + 读 profile
  与 `deploymentMode` 现状）、`ManualInstall: true`（给官网链接）、`open`。
  仅注册于 macOS / Windows。
- 新写入适配器（落在 `internal/desktopapp` 或 `internal/config`）：
  - `deploymentMode` 读-改-写只碰这一个键（两个目录的 config 都写）；
  - `configLibrary/<固定UUID>.json` 写入 gateway profile（品牌名 BootAgent）；
  - `_meta.json` 只增删自己的条目，`appliedId` 移交逻辑对齐 cc-switch 语义；
  - 恢复官方：`deploymentMode=1p` + 删自有 profile + meta 摘除；
  - 4 文件组事务：写前快照、任一步失败全部回滚（`securefs` 原子写之上补组语义）。
- 模型名校验：`claude-{sonnet,opus,haiku,fable}-` 前缀 + 非空尾；拒绝 `[1m]` 后缀
  混入模型名（如需 1M 声明，翻译为 profile 的 `supports1m` 字段）。这是 Claude
  Desktop fail-all validator 的硬约束——一个非法名会导致整组模型拒收。
- Provider 侧：仅 Anthropic 协议 Provider 可选（M3 的 protocols 能力有了落点），
  探测走 `anthropic_base_url`。

### 验收标准

1. macOS 装有 Claude Desktop 时，桌面 Agent 列表出现该项；应用 Anthropic 协议
   Profile 后：两个 `claude_desktop_config.json` 的 `deploymentMode` 为 `3p` 且**其余
   键逐字节不动**（fixture 断言）；profile 文件含正确的 base_url / key / 模型数组；
   `_meta.json` 仅多出自己一条。
2. 重启 Claude Desktop 后模型菜单出现所配 `claude-*` 模型且能正常对话——人工验收，
   结果（客户端版本号 + 截图）记入 PR。
3. 恢复官方：`deploymentMode=1p`、自有 profile 删除、meta 摘除自己、用户自建的其他
   profile 条目原样保留（fixture 断言）。
4. 非法模型名（非 claude 前缀、`claude-sonnet-` 空尾、带 `[1m]`）在 UI 选择阶段即
   拒绝；写入路径保留第二道校验（对齐 cc-switch 的同类测试用例集）。
5. 模拟 profile 写入失败 → 4 文件全部回滚到写前内容（单测）。
6. 非 Anthropic 协议 Profile 对 Claude Desktop 置灰并显示原因（M1 的交互范式）。
7. Windows 的 `Claude` / `Claude-3p` 目录探测（含非精确目录名回退）有单测；Linux
   上该 Agent 完全不出现。
8. 发布说明标注：该机制基于官方客户端未公开文档的行为，客户端更新可能失效；
   `inspect` 检测到 profile 结构不符时降级为「不可配置 + 提示」，不硬写。

## 9. M6 防腐与技术债（穿插进行）

| 项 | 内容 | 验收 |
| --- | --- | --- |
| Kilo JSONC 注释保留 | 现走 `internal/jsonorder` 只保 key 序不保注释，是「保留用户手改」承诺唯一破口。实现文本级注释保留写入（参照 cc-switch 对 OpenClaw/Hermes 的 section 级文本编辑思路） | 带注释 fixture 写入后注释逐字保留；既有合并语义测试零回归 |
| shape 防腐探测 | ZCode 配置 shape 是逆向观测（3.1.1→3.1.3 已变过），dsh 仍是 preview。激活前对目标文件做已知 schema 指纹校验，未知结构拒写并给出明确文案 | 伪造「未来版」shape 的 fixture 触发降级路径；正常 shape 不受影响 |
| dsh 双路径一般化 | `internal/app/agent.go:275-278` 的官方/通用 route 分支（provider × agent 二维适配唯一先例）改为 Provider 目录侧声明 | 该 per-agent 分支消失；`WriteDSHOfficial` 行为零回归（fixtures 不变） |
| 平台不可用 Agent 置灰 | 当前不支持平台的 Agent 完全不渲染（docs/status-dot-ui-plan.md 已记录）。改为置灰 + 「在 macOS/Windows 可用」 | Linux 上可见 ChatGPT Desktop 灰条与说明文案 |

## 10. 统一验收门禁（每个里程碑发布前都要过）

```text
go test ./...
go test -race ./...
go vet ./...          # CI 同时跑 staticcheck
cd frontend && pnpm run test && pnpm run build && pnpm run test:e2e
python3 scripts/check-docs.py
```

另加三条纪律：

1. DTO 变化必须三处同步（重新生成 `frontend/bindings`、`frontend/src/backend/wails.ts`
   和 `frontend/src/types/api.ts`），CodeGraph 查 `AgentService` 复核漏改位置。
2. 行为改动与 bug 修复配聚焦测试；密钥不进 DTO、日志、错误信息。
3. 涉及 live 文件写入的改动，golden fixtures 先行：先写「期望落盘内容」再写实现。

## 11. 预期结果度量汇总

| 维度 | 现状 | 完成后 | 归属 |
| --- | --- | --- | --- |
| Overview 切换 Profile 步数 | 3 步（进详情页） | 2 次点击行内完成 | M1 |
| 无窗口切换 | 不可能（托盘仅转换功能） | 托盘直切 + 状态勾选 | M1 |
| 外部修改感知 | 完全无感知 | 刷新/聚焦即见，键级定位，可恢复可采纳，零误报 | M2 |
| 备份 | 存在但不可见 | 可列出、可回滚（回滚前再备份） | M2 |
| 激活前可预知写入内容 | 否 | 逐键 diff 预览，与落盘一致 | M2 |
| reasoning 约束暴露时机 | 激活时报错 / 静默忽略 | 选择时预告（直传/映射/忽略+原因） | M3 |
| 新 Agent 能力接入成本 | 改 3 处 Go 映射/清单 | 改 `agents.lock.json` 一处 | M3 |
| 存量手工配置迁移 | 手动重录 | 一键导入，live 文件零改动 | M4 |
| Claude Desktop | 不支持 | macOS/Windows 直连模式，含完整还原 | M5 |
| 「保留用户手改」承诺 | Kilo 注释会丢 | 13/13 适配器含注释全保留 | M6 |

## 12. 风险

1. **Claude Desktop 机制未公开文档**（最大单点风险）：`3p` / `configLibrary` 行为来自
   对 cc-switch 的逆向与实测，官方客户端更新可能改 schema 或收紧 validator。缓解：
   M5 验收第 8 条的降级路径 + M6 的 shape 探测思路同样适用于它；把「客户端版本 ×
   验证结果」记录进 PR 形成追踪基线。
2. **托盘跨平台差异**：Linux 各桌面环境托盘菜单能力不一。缓解：托盘直切按
   「macOS/Windows 保证、Linux 尽力」定验收，主窗口路径是全平台兜底。
3. **capabilities 翻译错误**：把三套映射搬进数据时抄错一档。缓解：M3 验收第 2 条的
   「fixtures 一字不改」硬标准 + 全档位 × 全 Agent 矩阵一致性测试。
4. **漂移检测误报**引发用户不信任。缓解：M2 验收第 2 条把零误报列为硬标准，反例
   fixture 全适配器覆盖；徽标文案永远给出「期望 / 实际」证据而非笼统告警。
