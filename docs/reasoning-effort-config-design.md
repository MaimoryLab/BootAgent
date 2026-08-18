# Issue 1 技术设计文档：思考深度配置支持

## 概述

**目标**：使思考深度（`reasoningEffort`）成为 BootAgent 中可配置、可持久化、可在界面中随时调整的属性，类似于 `model` 和 `provider`。

**当前状态**：DSH (DeepSeek Harness) 的配置写入逻辑 **故意忽略** `reasoningEffort` 字段（见 `internal/config/write.go:418-420`），原因是旧实现认为「思考深度是模型特定的，不应跨模型保留」。

**新需求**：用户应该能够在 Profile 配置页面选择思考深度，并在每次激活 Agent 时应用该配置。

---

## 架构分析

### 数据流

```
前端 ProfilePage
    ↓ (save)
API /profiles/:id
    ↓
profile.Store.SaveProfile()
    ↓ 持久化到
~/.bootagent/profiles/store.json
    ↓ (激活时读取)
app.ActivateAgent()
    ↓
config.WriteDSH() / WriteDSHOfficial()
    ↓ 写入到
~/.dsh/settings.yaml
```

### 涉及的层次

1. **数据模型层**（Go 后端）
   - `internal/profile/store.go` - `Profile` 和 `storedProfile` 结构体
   - Schema version 升级（当前 v2 → v3）

2. **配置写入层**（Go 后端）
   - `internal/config/write.go` - `WriteDSH` 和 `WriteDSHOfficial` 函数
   - 其他 Agent 的配置写入函数（是否都支持？需调研）

3. **应用层**（Go 后端）
   - `internal/app/agent.go` - `ActivateAgent` 和 `writeManagedAgentConfig`
   - `internal/binding/services.go` - `ActivateAgentOptions` 结构体

4. **前端类型定义**
   - `frontend/src/types/api.ts` - `ProfileSummary`、`ProfileDraft` 等类型

5. **前端 UI**
   - `frontend/src/pages/AgentProfilePage.tsx` - Profile 配置表单
   - 可能需要新增 `<ReasoningEffortSelect>` 组件

6. **测试**
   - `internal/profile/store_test.go` - 数据模型测试
   - `internal/config/write_test.go` - DSH 写入测试（需更新 `TestWriteDSHReplacesItsOwnRouteWholesale` 等）
   - `internal/app/agent_test.go` - 激活流程测试
   - 前端单元测试

---

## 风险评估

### 1. Schema 迁移风险 ⚠️ **中等**

**问题**：现有用户的 `~/.bootagent/profiles/store.json` 是 `version: 2`，新增字段后需要处理向后兼容。

**现有迁移机制**：

- `internal/profile/store.go:127-139` - `loadStore()` 函数
- 当前只处理 v1 → v2 迁移（`migrateToV2()`）

**方案**：

- 添加 `migrateToV3()` 函数
- 对于旧 Profile（无 `reasoningEffort` 字段），有两个选择：
  1. **默认为 `nil`**（推荐）：表示「未配置」，Agent 使用其默认值
  2. **默认为 `"medium"`**：强制设置一个初始值

**推荐**：使用 `nil`（Go 中为 `*string`），因为：

- 不同模型的默认思考深度不同（DeepSeek 默认是什么？需确认）
- 用户可能之前从未接触过这个概念，不应该强制设置
- UI 中可以显示为「未设置（使用模型默认值）」

**测试覆盖**：

- 加载 v2 store → 自动迁移到 v3，字段为 `nil`
- 保存新 Profile 时正确写入 v3
- v3 store 能正确反序列化

---

### 2. 不同 Agent 的支持差异 ⚠️ **高**

**问题**：`reasoningEffort` 是 DeepSeek 特有的参数，其他 Agent 可能不支持。

**需要调研的 Agent**（从 `writeManagedAgentConfig` switch 中）：

```go
- codex          // Codex 是否支持？
- claude-code    // Claude 使用 extended_thinking，不是 reasoningEffort
- opencode       // OpenAI 兼容，可能支持 reasoning_effort（o1/o3 系列）
- kilo-cli       // 未知
- openclaw       // 未知
- aider          // 未知
- dsh            // ✅ 确认支持（本次重点）
- hermes         // 未知
- kimi-code      // Kimi 是否支持？
- workbuddy      // 未知
- zcode          // 未知
```

**现实问题**：

1. **字段名不统一**：
   - DeepSeek: `reasoningEffort`
   - OpenAI: `reasoning_effort`（驼峰 vs 下划线）
   - Claude: `extended_thinking`（完全不同的名字）

2. **可选值不统一**：
   - DeepSeek: `low`, `medium`, `high` （需确认）
   - OpenAI o1: `low`, `medium`, `high`
   - Claude: `enabled` / `disabled`（布尔语义）

**方案 A - 最小化方案（推荐给第一阶段）**：

- Profile 中只存储 `reasoningEffort` 字段
- **仅在 DSH 激活时应用**
- 其他 Agent 忽略该字段
- UI 中添加说明「仅适用于支持该功能的 Agent」

**方案 B - 通用化方案（未来扩展）**：

- Profile 中存储 `thinkingConfig` 对象：

  ```json
  {
    "thinkingConfig": {
      "deepseek": {"reasoningEffort": "high"},
      "claude": {"extendedThinking": true},
      "openai": {"reasoning_effort": "high"}
    }
  }
  ```

- 每个 Agent 的 Writer 从对应的子字段读取

**推荐**：先实现方案 A，验证需求后再考虑方案 B。

---

### 3. UI 交互设计风险 ⚠️ **中等**

**问题**：思考深度的可选值是什么？如何在 UI 中展示？

**需要确认的问题**：

1. DSH 支持的完整枚举值（`low`, `medium`, `high`, `xhigh`, `max`？）
2. 是否需要「自动」或「未设置」选项？
3. 不同模型的思考深度是否有差异（如 v4-pro vs v4-flash）？

**UI 方案**：

```tsx
<select name="reasoningEffort">
  <option value="">未设置（使用模型默认）</option>
  <option value="low">低</option>
  <option value="medium">中</option>
  <option value="high">高</option>
</select>
```

或使用 Radio 按钮组（更清晰）：

```tsx
<fieldset>
  <legend>思考深度（仅适用于支持的模型）</legend>
  <label><input type="radio" name="reasoningEffort" value="" /> 未设置</label>
  <label><input type="radio" name="reasoningEffort" value="low" /> 低</label>
  <label><input type="radio" name="reasoningEffort" value="medium" /> 中</label>
  <label><input type="radio" name="reasoningEffort" value="high" /> 高</label>
</fieldset>
```

**测试需求**：

- 手动测试：创建 Profile → 设置思考深度 → 激活 → 验证 `~/.dsh/settings.yaml`
- 自动化测试：E2E 测试涵盖完整流程

---

### 4. 配置读取逻辑的风险 ⚠️ **低**

**问题**：`internal/config/discovery.go` 的 `ReadDSHConfig` 是否需要读取 `reasoningEffort`？

**当前行为**：

- `ReadDSHConfig` 只读取 `baseURL` 和 `model`
- 用于 Desktop App 的「配置检测」功能

**需求判断**：

- 如果只是「激活时写入」，不需要修改 `ReadDSHConfig`
- 如果需要「显示当前配置的思考深度」，需要添加该字段

**推荐**：第一阶段不修改，因为：

- `reasoningEffort` 不影响「Agent 是否已激活」的判断
- 用户可以在 DSH 的 Web UI 中查看当前配置

---

### 5. 测试覆盖风险 ⚠️ **中等**

**现有测试的修改需求**：

1. **`TestWriteDSHReplacesItsOwnRouteWholesale`**（`write_test.go:821`）：
   - 当前测试验证「旧配置的字段被完全替换」
   - **需要确认**：`reasoningEffort` 是否应该被保留（如果用户在 DSH UI 中手动修改过）？
   - **建议**：和 `model` 一样，每次激活时用 Profile 中的值**覆盖**

2. **新增测试用例**：
   - `TestWriteDSHWithReasoningEffort` - 验证字段正确写入
   - `TestWriteDSHOfficialWithReasoningEffort` - 验证官方路由也支持
   - `TestProfileWithReasoningEffortRoundTrip` - Schema v3 序列化/反序列化

---

## 实现步骤（分阶段）

### Phase 1: 数据模型与持久化（低风险）

**文件**：

- `internal/profile/store.go`

**改动**：

```go
// storedProfile - 添加字段
type storedProfile struct {
    // ... 现有字段
    ReasoningEffort *string `json:"reasoningEffort,omitempty"`
}

// Profile - 添加字段
type Profile struct {
    // ... 现有字段
    ReasoningEffort *string
}

// migrateToV3 - 新增迁移函数
func migrateToV3(data []byte) ([]byte, error) {
    // v2 → v3：无需修改数据，只需升级 version 字段
    var doc map[string]any
    if err := json.Unmarshal(data, &doc); err != nil {
        return nil, err
    }
    doc["version"] = 3
    return json.Marshal(doc)
}

// loadStore - 更新版本检查
const currentVersion = 3
```

**测试**：

```bash
go test ./internal/profile -v
```

**验证**：手动创建 v2 store，加载后应自动迁移。

---

### Phase 2: 配置写入层（中风险）

**文件**：

- `internal/config/write.go`

**改动**：

```go
// WriteDSH - 添加 reasoningEffort 参数
func (w Writer) WriteDSH(ctx context.Context, path, providerName, baseURL, apiKey, model string, reasoningEffort *string) error {
    // ...
    selection := &yaml.Node{
        Kind: yaml.MappingNode,
        Content: []*yaml.Node{
            {Kind: yaml.ScalarNode, Value: "provider"},
            {Kind: yaml.ScalarNode, Value: dshOwnedRoute},
            {Kind: yaml.ScalarNode, Value: "model"},
            {Kind: yaml.ScalarNode, Value: model},
        },
    }
    if reasoningEffort != nil && *reasoningEffort != "" {
        selection.Content = append(selection.Content,
            &yaml.Node{Kind: yaml.ScalarNode, Value: "reasoningEffort"},
            &yaml.Node{Kind: yaml.ScalarNode, Value: *reasoningEffort},
        )
    }
    // ...
}

// WriteDSHOfficial - 同样添加 reasoningEffort 参数
func (w Writer) WriteDSHOfficial(ctx context.Context, path, apiKey, model string, reasoningEffort *string) error {
    // ... 类似改动
}
```

**测试**：

```bash
go test ./internal/config -run TestWriteDSH -v
```

**风险点**：

- 需要更新所有调用 `WriteDSH` 的地方（3 处）
- 旧测试用例需要传入 `nil`

---

### Phase 3: 应用层传递（中风险）

**文件**：

- `internal/app/agent.go`
- `internal/binding/services.go`

**改动**：

```go
// services.go - ActivateAgentOptions
type ActivateAgentOptions struct {
    // ... 现有字段
    ReasoningEffort *string `json:"reasoningEffort,omitempty"`
}

// agent.go - writeManagedAgentConfig
func writeManagedAgentConfig(ctx context.Context, writer configWriter.Writer, agentID string, agent catalog.Agent, path, providerID, providerName, baseURL, apiKey, model, smallFastModel string, reasoningEffort *string) error {
    switch agent.ConfigAdapter {
    case "dsh":
        if providerID == "deepseek" {
            return writer.WriteDSHOfficial(ctx, path, apiKey, model, reasoningEffort)
        }
        return writer.WriteDSH(ctx, path, providerName, baseURL, apiKey, model, reasoningEffort)
    // ... 其他 case 传 nil
    }
}

// agent.go - activateAgentLocked
// 从 profile 读取 reasoningEffort 并传递
```

**测试**：

```bash
go test ./internal/app -run TestActivateAgent -v
```

---

### Phase 4: API 层（低风险）

**文件**：

- `internal/binding/binding.go`

**改动**：

- 确认 `SaveProfile` API 是否需要修改
- 通常只需确保 JSON 反序列化正确处理新字段

**测试**：

```bash
go test ./internal/binding -v
```

---

### Phase 5: 前端类型定义（低风险）

**文件**：

- `frontend/src/types/api.ts`

**改动**：

```typescript
export interface ProfileSummary {
  // ... 现有字段
  reasoningEffort?: string;
}

export interface ProfileDraft {
  // ... 现有字段
  reasoningEffort?: string;
}
```

**测试**：

```bash
cd frontend && npm run typecheck
```

---

### Phase 6: 前端 UI（高风险）

**文件**：

- `frontend/src/pages/AgentProfilePage.tsx`

**改动**：

1. 添加表单控件（`<select>` 或 `<input type="radio">`）
2. 在 `save()` 函数中包含 `reasoningEffort` 字段
3. 添加说明文字：「思考深度仅适用于支持该功能的模型（如 DeepSeek）」

**测试**：

- 手动测试：创建 Profile → 填写 → 保存 → 激活 → 验证文件
- E2E 测试（如果有）

---

### Phase 7: 端到端验证（关键）

**验证清单**：

1. ✅ 创建新 Profile，设置 `reasoningEffort: "high"`
2. ✅ 保存后，`~/.bootagent/profiles/store.json` 包含该字段
3. ✅ 激活 DSH Agent，`~/.dsh/settings.yaml` 的 `agent-default-model` 包含 `reasoningEffort: high`
4. ✅ 启动 DSH (`dsh web`)，验证配置生效
5. ✅ 修改 Profile 的 `reasoningEffort` 为 `low`，重新激活
6. ✅ 验证配置已更新
7. ✅ 创建一个**不设置** `reasoningEffort` 的 Profile
8. ✅ 激活后，`~/.dsh/settings.yaml` 不包含该字段（或使用 DSH 默认值）

---

## 未解决的问题（需要调研）

> **2026-08-14 更新：以下三个问题均已调研完毕并落地，见文末「第二阶段：跨 Agent 适配」。**

### 1. DSH 的思考深度枚举值

**结论**：`off` / `high` / `max`（读自 `@deepseek-ai/dsh-llm-deepseek` 的 serialize.d.ts；缺省时 adapter 默认为 `high`）。已实现于 `ValidateDSHOfficialReasoningEffort`。

### 2. DeepSeek API 的默认思考深度

**结论**：不设置时由 dsh 的 llm-deepseek adapter 决定（`high`）。Profile 层用空字符串表达「未设置（模型默认）」。

### 3. 其他 Agent 的支持情况

**结论**：见下方适配矩阵。

---

## 回滚计划

如果实现过程中发现严重问题，回滚步骤：

1. **Schema 已升级到 v3**：
   - 添加向下兼容逻辑，忽略 `reasoningEffort` 字段
   - 不会丢失用户数据（只是不生效）

2. **前端已部署**：
   - 隐藏表单控件（CSS `display: none`）
   - 或在后端 API 层忽略该字段

3. **配置文件已写入**：
   - DSH 会忽略不认识的字段（YAML 格式容错）
   - 用户手动删除该行即可

---

## 时间估算

| 阶段 | 工作量 | 风险 |
| ------ | -------- | ------ |
| Phase 1: 数据模型 | 2 小时 | 低 |
| Phase 2: 配置写入 | 3 小时 | 中 |
| Phase 3: 应用层 | 2 小时 | 中 |
| Phase 4: API 层 | 1 小时 | 低 |
| Phase 5: 前端类型 | 1 小时 | 低 |
| Phase 6: 前端 UI | 3 小时 | 高 |
| Phase 7: 端到端验证 | 2 小时 | 高 |
| **总计** | **14 小时** | |

**建议**：分两个 PR：

- PR 1：Phase 1-4（后端，低风险）
- PR 2：Phase 5-7（前端 + 集成，需要人工验证）

---

## 下一步

1. ✅ **调研 DSH 的思考深度枚举值**（优先级最高）
2. ✅ **确认 DeepSeek API 默认值**
3. 🔄 **实现 Phase 1-2**（数据模型 + 配置写入）
4. 🔄 **编写测试并验证**
5. 🔄 **实现 Phase 3-7**（应用层 → 前端）
6. 🔄 **端到端测试**

---

## 第二阶段：跨 Agent 适配（2026-08-14 完成）

### 适配矩阵

| Agent | 配置位置 | 支持的值 | 映射逻辑 | 写入层 | 验证层 |
| ------- | --------- | --------- | --------- | ------- | ------- |
| **DeepSeek Harness** | `~/.dsh/settings.yaml`<br>`agent-default-model.reasoningEffort` | `off` / `high` / `max` | 直通 | `WriteDSHOfficial` | `ValidateDSHOfficialReasoningEffort` |
| **Codex** | `~/.codex/config.toml`<br>`model_reasoning_effort` | `none` / `low` / `medium` / `high` / `xhigh` | `off→none`<br>`max→xhigh`<br>其余直通 | `WriteCodex` | `codexReasoningEffort` |
| **OpenCode / Kilo** | `opencode.json` / `kilo.jsonc`<br>模型条目 `options.reasoningEffort` | `low` / `medium` / `high` | 拒绝 `off` 和 `max`<br>其余直通 | `WriteOpenAICompatible` | `validateOpenAIReasoningEffort` |
| **aider** | `~/.bootagent/aider.env`<br>`AIDER_REASONING_EFFORT` | `low` / `medium` / `high` | 拒绝 `off` 和 `max`<br>其余直通 | `WriteAider` | `validateOpenAIReasoningEffort` |
| **Claude Code** | 有意忽略 | — | 静默丢弃 | `WriteClaude` 不收 effort | — |
| **其余（OpenClaw / Hermes / Kimi Code / WorkBuddy / ZCode）** | 有意忽略 | — | 静默丢弃 | 各 Writer 不收 effort | — |

**Claude Code 为何忽略**：其文档化的思考控制是 token 预算（`MAX_THINKING_TOKENS`）和 always-think 布尔开关，没有档位标尺；把五档映射到二者任何一个都是在发明该工具未承诺的语义。`ANTHROPIC_EXTENDED_THINKING` 环境变量行为不稳定（社区报告了配置优先级冲突），不作为写入目标。

**其余 Agent 为何忽略**：这些 Writer 写的配置形态是从单一观测版本读出的、无文档的文件（见 `WriteZCode` 的注释），在应用自有的文件里发明键有破坏其状态的风险。

### Profile 词汇表

统一五档，验证在 `ValidateProfileReasoningEffort`：

```
"" (空) | "off" | "low" | "medium" | "high" | "max"
```

前端所有 provider 均显示全部五档；后端在激活时按 Agent 能力拒绝不支持的档位。

### 前端改动

- **ProfilesPage** / **AgentProfilePage**：移除 `provider === "deepseek"` 限制，所有 provider 显示完整五档
- **i18n.tsx**：新增 `low（低）` / `medium（中）` 和通用说明文案
- **说明文案**：从「目前仅 DeepSeek Harness 会应用此设置」改为「各 Agent 支持的档位不同，应用时不支持的档位会明确报错」

### 后端改动

- **write.go**：新增 `ValidateProfileReasoningEffort`（保存时的并集门）、`codexReasoningEffort`（映射+校验）、`validateOpenAIReasoningEffort`（aider/OpenCode/Kilo 共用门）；`WriteCodex` / `WriteAider` / `WriteOpenAICompatible` 各增 effort 参数
- **status.go**：`SaveProfile` 的校验从 dsh 专属换成 Profile 并集门
- **agent.go**：`writeManagedAgentConfig` 向支持的 adapter 透传 effort，注释记录每个忽略者的理由
- **write_test.go / agent_test.go**：新增映射、拒绝、清除、绑定记录原值等测试

### 实现决策

1. **诚实报错 vs 静默降级**：Agent 能表达但值不在其标尺上时明确拒绝（返回 `InvalidRequest` 错误并列出该 Agent 的标尺），而非静默降级；Agent 结构上无处可写时静默丢弃（与 dsh 非官方路由的既有语义一致）
2. **Profile 层统一词汇表**：保存时校验并集五档，激活时由各 adapter 再收窄——「由做决定的那一层配置」
3. **空字符串语义**：各 Writer 收到空字符串时不写 effort 键，且清除自己先前写入的值，由 Agent 使用其自身默认值
4. **Codex 两端映射**：`off→none`、`max→xhigh` 是语义精确的翻译而非降级，Codex 对该枚举严格校验、未知值会拒绝启动，所以映射不到的值绝不能落盘

### 未来工作

若有新 Agent 支持 reasoning effort，参考本次实现模式：

1. 在 `write.go` 新增校验/映射函数
2. 修改对应 Writer，解析并写入 Agent 配置
3. 在 `write_test.go` 添加覆盖测试
4. 更新本文档的适配矩阵

---

## 参考资料

- `internal/config/write.go:418-420` - 当前忽略 `reasoningEffort` 的注释
- `internal/profile/store.go:127-139` - Schema 迁移机制
- `frontend/src/pages/AgentProfilePage.tsx` - Profile 配置页面
- DeepSeek API 文档（待补充链接）
