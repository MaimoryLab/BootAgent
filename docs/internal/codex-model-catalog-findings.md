# Codex 桌面端模型列表切换：机制与接入结论

调研目的：让用户在 Codex 桌面端的模型选择器里，直接切换 BootAgent 配好的
Provider 所支持的模型，而不是只能用 `config.toml` 里写死的那一个。

结论：**Codex 原生支持这件事**，机制是 `config.toml` 的 `model_catalog_json`
键指向一份模型目录文件。下面每一条都在本机实测过（`codex-cli 0.146.0` 与
ChatGPT 桌面端自带的 `0.147.0-alpha.1.2`），不是读文档或猜的。

## 1. 机制

`~/.codex/config.toml` 里加一行：

```toml
model_catalog_json = "my-catalog.json"
```

相对路径以 `CODEX_HOME`（默认 `~/.codex`）为基准，绝对路径同样可用，两者都实测通过。

文件内容是一个对象，`models` 是数组：

```json
{
  "models": [
    { "slug": "deepseek/deepseek-v3", "display_name": "DeepSeek V3", "...": "..." },
    { "slug": "qwen/qwen3-coder",     "display_name": "Qwen3 Coder", "...": "..." }
  ]
}
```

验证命令（这是 Codex 自己的调试入口，比读二进制可靠）：

```bash
CODEX_HOME=/path/to/home codex debug models
```

它会把解析后的目录原样打印成 JSON。解析失败时报错会点名缺哪个字段，可以据此逐个补齐。

## 2. 这个机制已经有人在用

本机 `~/.codex/config.toml` 里已有：

```toml
model_catalog_json = "cc-switch-model-catalog.json"
```

即 CC Switch 已经在用这条路径注册了三个模型（`openai/gpt-5.6-sol` /
`-terra` / `-luna`）。也就是说这不是理论方案，是竞品已验证的落地路径。

## 3. 桌面端 UI 确实读它，不只是 CLI

这一点必须单独确认，否则做完发现只有 CLI 生效。两条证据：

- 桌面端 `/Applications/ChatGPT.app/Contents/Resources/codex` 是它自带的二进制
  （`0.147.0-alpha.1.2`），`strings` 里 `model_catalog_json` 命中 20 次，且我用
  **这个二进制**跑 `debug models` 成功解析了自造的目录文件。
- 桌面端状态文件 `~/.codex/.codex-global-state.json` 的
  `electron-persisted-atom-state` 里有 `seen-model-upgrade-list: ["gpt-5.6-sol"]`
  和 `composer-model-picker-menu-view-v1: "advanced"`——目录里的模型名出现在了
  桌面端自己的选择器状态里。

## 4. 最小必需字段：12 个

用二分法逐个补齐缺失字段测出来的（完整 schema 有 26 个字段，不必全填）：

```json
{
  "slug": "deepseek/deepseek-v3",
  "display_name": "DeepSeek V3",
  "supported_reasoning_levels": [{ "effort": "medium", "description": "Medium reasoning" }],
  "shell_type": "shell_command",
  "visibility": "list",
  "supported_in_api": true,
  "priority": 1000,
  "base_instructions": "You are Codex, a coding agent.",
  "support_verbosity": false,
  "truncation_policy": { "limit": 10000, "mode": "bytes" },
  "supports_parallel_tool_calls": false,
  "experimental_supported_tools": []
}
```

值得补但非必需的：`context_window` / `max_context_window` /
`effective_context_window_percent`（不填则为 `null`，UI 上下文占比可能显示异常）、
`description`、`default_reasoning_level`、`input_modalities`。

## 5. 三条硬约束（会导致 Codex 启动失败，不是降级）

实测出来的失败模式，接入时必须处理：

| 情况 | 行为 |
| --- | --- |
| `model_catalog_json` 指向的文件不存在 | `Error: No such file or directory (os error 2)` — **硬失败** |
| `models` 是空数组 | `Error: ... must contain at least one model` — **硬失败** |
| `visibility` 取其他值 | 只接受 `list` / `hide` / `none`，`hidden` 会报 unknown variant |

第一条对 BootAgent 最关键：**写了这个键就必须保证文件存在**。顺序必须是先写目录
文件、再写 `config.toml`，与现有 `WriteCodex` 先写 `auth.json` 再写 `config.toml`
的理由一致（指向一个不可用的东西比暂时没被引用更糟）。

## 6. 与 BootAgent 现有实现的兼容性

好消息：**不冲突，且现有合并逻辑已经会保留这个键。**

`mergeCodexTOML`（`internal/config/write.go:278`）的 `topLevelKeyPattern` 是
`^\s*(model_provider|model)\s*=`，只重写这两个顶层键；`managedSectionPattern` 只
接管 `[model_providers.bootagent]`。我写了一个探针测试验证：用户已有的
`model_catalog_json`、`model_reasoning_effort`、`[mcp_servers.node_repl]` 在一次
BootAgent 写入后全部原样保留。

注意 `model_catalog_json` 不匹配 `topLevelKeyPattern`（它不是 `model =` 而是
`model_catalog_json =`，正则要求 `model` 后紧跟可选空白再 `=`），所以不会被误删。

## 7. 接入方案（未实施，需产品决定）

现状是 `WriteCodex`（`write.go:35`）写单个 `model = "<一个模型>"`。要支持列表，
思路与 `WriteWorkBuddy`（`write.go:127`）已经在做的事一致——那个适配器维护一个
模型**数组**，按 `id` 做 append-if-absent，所以 WorkBuddy 的 UI 能给出列表。

Codex 版本需要：

1. 新增一个 catalog 写入器，把 Provider 的模型列表（`ListModels` 已经能从
   `/v1/models` 拿到）转成上面的 12 字段结构，写到
   `~/.codex/bootagent-model-catalog.json`。
2. `WriteCodex` 的 managed 块加一行 `model_catalog_json = "bootagent-model-catalog.json"`，
   `model` 仍写用户选中的那个作为默认。
3. 写入顺序：catalog 文件 → `auth.json` → `config.toml`。

上面两个原本待决的问题，Codex++ 都已经给出了可抄的答案，见下一节。

## 8. Codex++ 的做法（`output/reference/codex-plusplus`）

这是 `BigPizzaV3/CodexPlusPlus` 的一个 fork（Rust + Tauri），本地那份正在做
「按模型粒度配置上下文窗口」。它用**两条互补的机制**，侵入性差别很大。

### 机制 A：写 catalog 文件 + 注入 config.toml 指针（与第 1 节同一机制）

`apply_model_catalog_to_config`（`crates/codex-plus-core/src/relay_config.rs:1550`）。
它把 catalog 写到 **每个 profile 一个文件**：`model-catalogs/<profile-id>.json`，
再把相对路径写进 `model_catalog_json`。切换供应商时换指针，互不污染——
`CHANGELOG.md:52` 记着他们修过「供应商切换时 `model_catalog_json`、旧
`model_provider`、旧 `auth.json` 被带到新供应商」的隔离 bug，值得引以为戒。

**它怎么解决「抢同一个键」——这正是我们的待决问题 1。**
`relay_config.rs:1560-1573` 的策略是：读现有 `model_catalog_json`，若它不等于本
profile 自己会生成的路径，就**原样返回、不覆盖**（注释明确写着「用户已手写
`model_catalog_json` 指针时保留」，并有 `preserves_user_model_catalog_json` 测试守着）。
只有当现有指针指向自己生成的那个文件时才重新生成。

这与 BootAgent「不破坏用户既有配置」的边界一致，可以直接采用。代价同前：用户已有
第三方 catalog 时拿不到 BootAgent 的列表——Codex++ 接受了这个代价。

**它怎么解决「元数据从哪来」——待决问题 2。**
`build_model_catalog_json_with_capabilities`（`crates/codex-plus-core/src/model_suffix.rs:223`）
不逐字段手填，而是**先取一份模板再覆盖少数字段**：
`crates/codex-plus-core/assets/codex-models.json` 内置了 6 个真实模型条目
（`gpt-5.5`、`gpt-5.4`、`gpt-5.4-mini`、`gpt-5.3-codex`、`gpt-5.2`、
`codex-auto-review`，`context_window` 均 272000）作为模板，未知 slug 走
`model_template_entry` 回退。只覆盖 `slug` / `display_name` / `context_window` /
`max_context_window` / `priority` / `visibility` / `supported_in_api` 等。

一个细节值得抄：`model_suffix.rs:250-251` 把 `effective_context_window_percent`
显式写成 `100` 而不是默认 `95`，注释说明理由是「默认 95 会让 1M 显示为 950K」。
我们若照默认值填，UI 上下文占比同样会失真。

窗口大小用后缀语法从用户输入解析，如 `deepseek-v4-pro[1M]`、`[200K]`
（`model_list` textarea，`apps/codex-plus-manager/src/App.tsx`）。

### 机制 B：CDP 注入，改写桌面端运行时的模型白名单

这条是机制 A 之外的，用来对付**桌面端 UI 自己的模型白名单**——即 catalog 里有
模型、但 ChatGPT 应用的选择器仍不显示的情况。

启动时给应用加 `--remote-debugging-port`（`crates/codex-plus-core/src/launcher.rs:2232`），
通过 Chromium DevTools Protocol 注入 `assets/inject/renderer-inject.js`（10604 行）。
其中 `patchModelArray`（`:6409`）和 `patchModelContainer`（`:6432`）把自定义模型名
塞进渲染进程里的模型数组、`availableModels` / `available_models` 集合，并把
`hidden` 改成 `false`；还 patch 了 `Response.prototype.json`、Statsig 动态配置
（`patchStatsigModelDynamicConfig`）和 app-server 的 `list-models-for-host` 请求。
模型清单本身由本地服务的 `/codex-model-catalog` 端点提供
（`model_catalog.rs:133` 的 `read_codex_model_catalog_from_home`，它会去读
config.toml、auth.json，并向 Provider 的 `/v1/models` 拉取）。

README 强调它「不修改官方应用的 `app.asar`，也不向安装目录写入补丁文件」——
所有改动都在运行时内存里。

### 对 BootAgent 的取舍

**机制 A 可以采用，机制 B 不行。**

机制 B 要求：以调试端口启动别人的应用、注入脚本改写其运行时行为、patch 其网络
响应。这与 BootAgent 既定的产品边界冲突——`docs/product-boundary-baseline.md` 的
定位是「检测、安装、配置」，不含运行时干预；而且上一次审计已确认 BootAgent 不启动
后台服务，机制 B 需要一个常驻本地服务来提供 `/codex-model-catalog`。

## 9. 更正：GUI 确实有一层过滤，catalog 不足以突破

**上一版这里的结论是错的**，记录下来以免重犯。当时我用 `codex debug models` 验证了
非 `openai/` 前缀的模型能被解析，就推断「catalog 就够、机制 B 多余」。但
`codex debug models` 验证的是**配置解析层**，而选择器的过滤发生在**渲染层**，两者
是不同的关卡。反编译 `app.asar` 后找到了真正的过滤代码。

### 过滤代码的确切位置

`app.asar` → `/webview/assets/app-initial-CKNQDTeE.js`（14.5 MB，已 minify）。
关键是这个谓词函数（变量名是压缩后的）：

```js
function _7r({additionalAvailableModels:e, authMethod:t, availableModels:n, model:r, useHiddenModels:i}) {
  return e?.has(r.model) === !0 || (i && t !== `amazonBedrock` ? n.has(r.model) : !r.hidden)
}
```

它被 `g7r` 在 `list-models-for-host` 响应的 `select` 里逐条调用，只有返回 true 的
模型才进 `models` 数组、才出现在选择器里。三条通路：

1. `additionalAvailableModels.has(model)` —— 额外白名单，命中即放行（优先级最高）
2. `useHiddenModels` 为真时：查 `availableModels` 集合
3. 否则（**默认路径**）：看 `!model.hidden`

默认值在同文件：

```js
f7r = []                                  // availableModels 的默认值：空数组
p7r = { availableModels: new Set(f7r), useHiddenModels: !1, defaultModel: s7r }
s7r = `gpt-5.5`
```

`availableModels` 由远端动态配置填充（`u7r` 解析 `available_models` /
`use_hidden_models` / `default_model` 三个字段，配置 ID 是 `107580212`，经
`j7r()` 读取）。本机 `~/Library/Application Support/` 下没有它的缓存文件，说明是
运行时向服务端拉的。

### 为什么这挡住了第三方模型

默认 `useHiddenModels = false`，所以走第 3 条：`!model.hidden`。而
`hidden` 这个字段**不是 catalog 里的 `visibility`**——它是 codex 后端在构造
`list-models-for-host` 响应时决定的。`codex` 二进制里同时存在 `hidden` 和
`isDefault` 两个字符串（与 JS 侧字段名对应），而 catalog schema 只接受
`visibility: list|hide|none`。也就是说 `visibility` 是 catalog 的输入词汇，
`hidden` 是响应的输出词汇，中间的映射由 codex 决定，BootAgent 写 catalog 只能影响
输入端，无法直接把 `hidden` 置为 false。

这解释了 Codex++ 为什么必须做机制 B：它的 `patchModelArray`
（`renderer-inject.js:6409`）干的正是**把 `item.hidden` 改成 `false`**，并往
`availableModels` / `available_models` 集合里塞名字——即直接改写上面那三条通路的
输入。它还 patch 了 `patchStatsigModelDynamicConfig`，那正是对应 `107580212` 这个
动态配置。它的测试 `injection_script_unlocks_custom_model_catalog`
（`tests/cdp_bridge.rs:1275`）断言了 `available_models`、`modelWhitelistUnlock`、
`String(name) === "107580212"` 这些字符串，与我读到的过滤代码一一对上。

### BootAgent 在应用层面能做什么

**结论：在不注入渲染进程的前提下，没有办法让第三方模型 ID 出现在 ChatGPT 桌面端的
模型选择器里。** 那个过滤是渲染层对远端动态配置的判断，BootAgent 能写的只有
`~/.codex/` 下的文件，触碰不到 `additionalAvailableModels`、`useHiddenModels`
或响应里的 `hidden`。

三条理论路径，都不可取：

| 路径 | 为什么不行 |
| --- | --- |
| CDP 注入改写渲染层（Codex++ 机制 B） | 需要以调试端口启动别人的应用、注入脚本、patch 其网络响应，并常驻一个本地服务。与 `product-boundary-baseline.md` 的「检测、安装、配置」定位冲突，也与「不启动后台服务」冲突 |
| 改 `app.asar` | 修改他人已签名的应用，破坏 codesign，触发 Gatekeeper；分发补丁还涉及版权。Codex++ 自己都明确不这么做 |
| 伪造模型 ID 冒充 openai 名字 | 即便让 slug 长得像官方模型，请求仍会带着这个名字发给用户配的 Provider，Provider 认不出来。且这是欺骗性配置，会让用户以为在用某个模型而实际不是 |

**因此 BootAgent 对 Codex 桌面端的现实边界是：配置 Provider 与默认模型（`model` 键
写哪个就用哪个），但不提供「在桌面端 UI 里切换第三方模型」的能力。** 用户要换模型，
路径是回到 BootAgent 改配置、或用 Codex CLI（CLI 侧读 catalog，不经过渲染层过滤，
所以 CLI 是可以列出并切换第三方模型的——这一点第 1 节已实测）。

这个差异值得在 UI 或文档里对用户说清，否则用户会以为 BootAgent 配好了但应用「坏了」。

### 仍未验证

我没有实际启动 GUI 肉眼确认过滤效果，因为那要改动本机 `~/.codex/config.toml`
（当前指向 CC Switch 的 catalog）。以上是对 minify 后代码的静态分析 + Codex++ 的
patch 目标反向印证，两条独立证据一致。若要最终确认，可在一台干净机器上配好第三方
Provider 后打开应用观察选择器。

## 复现环境

```
codex-cli 0.146.0                      (~/.local/bin/codex)
codex-cli 0.147.0-alpha.1.2            (ChatGPT.app 自带)
macOS arm64
```

实测用的隔离环境是 `CODEX_HOME=/tmp/cxtest`，没有改动本机 `~/.codex` 下任何文件。
