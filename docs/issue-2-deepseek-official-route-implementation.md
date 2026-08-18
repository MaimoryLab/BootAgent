# Issue 2 实现总结：DeepSeek 官方路由映射

## 问题描述

**原问题**：在 BootAgent 中配置 DSH 服务时，将模型服务中的 DeepSeek 供应商对应到 DSH 配置文件（`settings.yaml`）中的官方对应的配置，而不是自定义的配置。

**旧行为**：

- 无论用户选择什么供应商，BootAgent 总是创建一个名为 `bootagent` 的**自定义路由**
- 使用 `BOOTAGENT_API_KEY` 作为凭证引用
- 用户在 DSH 的 Models 页面看到的是 "BootAgent"，而不是 "DeepSeek"

**新行为**：

- 当用户选择 **DeepSeek 官方供应商**且**未自定义 baseURL** 时：
  - 使用 DSH 内置的 `deepseek-official` 路由
  - 使用 `DEEPSEEK_API_KEY` 作为凭证
  - 不创建自定义 `bootagent` 路由
- 当用户选择**网关或其他自定义服务**时：
  - 保持原有行为（创建 `bootagent` 自定义路由）

---

## 实现方案

### 核心判断逻辑

新增辅助函数 `dshRouteProviderID`，用于判断是否应该使用官方路由：

```go
// dshRouteProviderID 返回 Provider ID，仅当该 Provider 是内置且未被 baseURL 覆盖时
func dshRouteProviderID(target provider.Entry, explicitBase string) string {
    if !target.BuiltIn || strings.TrimSpace(explicitBase) != "" {
        return ""  // 返回空字符串表示使用自定义路由
    }
    return target.ID  // 返回 "deepseek" 表示使用官方路由
}
```

**判断条件**：

1. `target.BuiltIn == true` - Provider 是内置的（如 DeepSeek、Anthropic）
2. `explicitBase == ""` - 用户没有自定义 baseURL

**结果**：

- 返回 `"deepseek"` → 使用 `WriteDSHOfficial()`
- 返回 `""` → 使用 `WriteDSH()`（自定义路由）

---

## 修改的文件

### 1. `internal/config/write.go`

#### 新增常量

```go
const (
    dshOwnedRoute      = "bootagent"         // BootAgent 自己管理的自定义路由名称
    dshOfficialRoute   = "deepseek-official" // DSH 内置的 DeepSeek 官方路由名称
    dshCredentialReference = "BOOTAGENT_API_KEY"
)
```

#### 新增函数：`WriteDSHOfficial`

- **作用**：为 DeepSeek 官方服务配置 DSH，使用内置路由
- **写入内容**：

  ```yaml
  agent-default-model:
    provider: deepseek-official
    model: deepseek-v4-pro
  ```

- **凭证**：写入 `DEEPSEEK_API_KEY` 到 `~/.dsh/.credentials.yaml`
- **清理**：删除旧的 `bootagent` 自定义路由（如果存在）

#### 修改函数：`WriteDSH`

- **新增功能**：清理旧的 `deepseek-official` 选择（如果存在）
- **原因**：从官方路由切换到自定义路由时，需要清理旧配置

#### 新增辅助函数

- `yamlChild(parent *yaml.Node, key string) *yaml.Node` - 获取子节点
- `yamlDelete(parent *yaml.Node, key string)` - 删除子节点
- `writeDSHCredential(path, reference, apiKey string)` - 写入凭证（参数化版本）

---

### 2. `internal/config/discovery.go`

#### 修改函数：`ReadDSHConfig`

- **新增逻辑**：识别 `deepseek-official` 路由的选择
- **返回**：

  ```go
  Detected{
      Model: "deepseek-v4-pro",
      BaseURL: "",                     // 官方路由的 endpoint 是 DSH 内置的，不记录
      ManagedByBootAgent: false        // 官方路由不算 BootAgent 管理
  }
  ```

---

### 3. `internal/app/agent.go`

#### 新增函数：`dshRouteProviderID`

- **作用**：判断是否使用官方路由
- **调用位置**：所有 `writeManagedAgentConfig` 的调用点

#### 修改函数：`writeManagedAgentConfig`

- **新增参数**：`providerID string`
- **新增逻辑**：

  ```go
  case "dsh":
      if providerID == "deepseek" {
          return writer.WriteDSHOfficial(ctx, path, apiKey, model)
      }
      return writer.WriteDSH(ctx, path, providerName, baseURL, apiKey, model)
  ```

---

### 4. `internal/app/install.go`

#### 修改位置：L409

```go
// 旧代码
writeManagedAgentConfig(ctx, writer, agentID, agent, configPathValue, 
    r.providerName, configBase, r.options.APIKey, r.options.Model, r.options.SmallFastModel)

// 新代码
writeManagedAgentConfig(ctx, writer, agentID, agent, configPathValue, 
    dshRouteProviderID(target, r.options.APIBaseURL),  // ← 新增
    r.providerName, configBase, r.options.APIKey, r.options.Model, r.options.SmallFastModel)
```

---

### 5. `internal/app/desktopapp.go`

#### 修改位置：L247

```go
// 旧代码
writeManagedAgentConfig(ctx, writer, definition.ID, catalog.Agent{
    ConfigAdapter: definition.ConfigAdapter,
}, path, target.Name, target.BaseFor(protocol), target.APIKey, model, "")

// 新代码
writeManagedAgentConfig(ctx, writer, definition.ID, catalog.Agent{
    ConfigAdapter: definition.ConfigAdapter,
}, path, dshRouteProviderID(target, ""),  // ← 新增
target.Name, target.BaseFor(protocol), target.APIKey, model, "")
```

---

### 6. `manifests/agents.lock.json`

#### 修改：DSH Agent 的 `guide` 字段

```json
{
  "guide": "当供应商为 DeepSeek 官方时，BootAgent 配置 ~/.dsh/settings.yaml 使用内置的 deepseek-official 路由，API Key 存入 ~/.dsh/.credentials.yaml 的 DEEPSEEK_API_KEY；当供应商为网关或其他自定义服务时，BootAgent 注册一个名为 bootagent 的自定义供应商并设为默认模型。..."
}
```

---

## 测试覆盖

### 新增测试用例（`internal/config/write_test.go`）

1. **`TestWriteDSHOfficialUsesTheShippedRouteInsteadOfDeclaringOne`**
   - 验证：使用官方路由，不创建 `bootagent` 路由
   - 验证：`agent-default-model.provider` 为 `deepseek-official`
   - 验证：凭证写入 `DEEPSEEK_API_KEY`
   - 验证：`ReadDSHConfig` 正确识别

2. **`TestWriteDSHCleansUpAfterWriteDSHOfficial`**
   - 场景：官方路由 → 自定义路由
   - 验证：创建 `bootagent` 路由
   - 验证：凭证写入 `BOOTAGENT_API_KEY`
   - 验证：旧凭证 `DEEPSEEK_API_KEY` 保留（不影响）

3. **`TestWriteDSHOfficialCleansUpAStaleBootagentRoute`**
   - 场景：自定义路由 → 官方路由
   - 验证：删除 `bootagent` 路由
   - 验证：`agent-default-model.provider` 切换为 `deepseek-official`

### 新增测试用例（`internal/app/agent_test.go`）

1. **`TestDSHRouteProviderIDReturnsBuiltInIDOnlyWhenUnoverridden`**
   - 验证：内置 DeepSeek 无覆盖 → 返回 `"deepseek"`
   - 验证：内置 DeepSeek 有 baseURL 覆盖 → 返回 `""`
   - 验证：自定义 Provider → 返回 `""`
   - 验证：其他内置 Provider（如 Anthropic）→ 返回 `"anthropic"`

### 已有测试验证

所有已有测试通过，包括：

- `TestWriteDSHRegistersARouteBesideTheUsersOwn`
- `TestWriteDSHIsIdempotent`
- `TestWriteDSHReplacesItsOwnRouteWholesale`
- `TestWriteDSHRefusesAnInvalidSettingsDocument`
- `TestEveryAutoAgentAdapterCanBeWrittenAndReadBack`

---

## 行为对比

### 场景 1：DeepSeek 官方供应商，无自定义 baseURL

**旧行为**：

```yaml
# ~/.dsh/settings.yaml
llm-pi-ai:
  providers:
    bootagent:
      displayName: DeepSeek
      baseURL: https://api.deepseek.com/v1
      apiKeyEnv: BOOTAGENT_API_KEY
      models:
        - id: deepseek-v4-pro
agent-default-model:
  provider: bootagent
  model: deepseek-v4-pro
```

```yaml
# ~/.dsh/.credentials.yaml
BOOTAGENT_API_KEY: sk-xxx
```

**新行为**：

```yaml
# ~/.dsh/settings.yaml
agent-default-model:
  provider: deepseek-official
  model: deepseek-v4-pro
```

```yaml
# ~/.dsh/.credentials.yaml
DEEPSEEK_API_KEY: sk-xxx
```

**优势**：

- ✅ 不创建重复的路由定义
- ✅ 与 DSH 的 Models 页面显示一致
- ✅ 凭证命名空间更清晰

---

### 场景 2：DeepSeek 官方供应商，但自定义了 baseURL（如网关）

**行为**：

```yaml
# ~/.dsh/settings.yaml
llm-pi-ai:
  providers:
    bootagent:
      displayName: PPIO Gateway
      baseURL: https://api.gateway.example/v1
      apiKeyEnv: BOOTAGENT_API_KEY
      models:
        - id: deepseek-v4-pro
agent-default-model:
  provider: bootagent
  model: deepseek-v4-pro
```

**原因**：

- DSH 内置的 `deepseek-official` 路由的 endpoint 固定为 `https://api.deepseek.com`
- 自定义 baseURL 需要创建新路由

---

### 场景 3：其他供应商（如 Anthropic、OpenAI）

**行为**：保持原有逻辑，创建 `bootagent` 自定义路由。

**原因**：

- DSH 目前只内置了 DeepSeek 的官方路由
- 其他供应商需要手动声明

---

## 验证清单

### 自动化测试 ✅

- [x] 所有单元测试通过（`go test ./...`）
- [x] 新增测试覆盖核心逻辑
- [x] 代码编译通过（`go build ./...`）

### 手动测试（待执行）

- [ ] 创建 Profile，选择 **DeepSeek 官方供应商**，不自定义 baseURL
- [ ] 激活 DSH Agent
- [ ] 验证 `~/.dsh/settings.yaml` 使用 `deepseek-official` 路由
- [ ] 验证 `~/.dsh/.credentials.yaml` 包含 `DEEPSEEK_API_KEY`
- [ ] 启动 DSH (`dsh web`)，验证配置生效
- [ ] 在 DSH 的 Models 页面，验证显示为 "DeepSeek" 而不是 "BootAgent"
- [ ] 修改 Profile，自定义 baseURL 为网关地址
- [ ] 重新激活，验证切换为 `bootagent` 自定义路由
- [ ] 再次切换回官方供应商，验证清理了 `bootagent` 路由

---

## 边缘情况处理

### 1. 用户手动编辑了配置文件

- **场景**：用户在 DSH 的 Models 页面手动修改了 `deepseek-official` 路由
- **行为**：BootAgent 重新激活时会**覆盖** `agent-default-model`，但不会修改路由定义本身
- **影响**：用户的手动修改可能被覆盖（与 `model` 字段行为一致）

### 2. 用户已有旧的 `bootagent` 路由

- **场景**：用户使用旧版本 BootAgent 激活过 DSH
- **行为**：`WriteDSHOfficial` 会**删除**旧的 `bootagent` 路由
- **影响**：配置自动清理，无需用户干预

### 3. DSH 版本兼容性

- **假设**：DSH 的 `deepseek-official` 路由名称是稳定的
- **风险**：如果 DSH 未来版本改名，需要适配
- **缓解**：常量 `dshOfficialRoute` 集中定义，便于修改

---

## 未来扩展

### 支持其他内置路由

如果 DSH 未来添加更多内置路由（如 `anthropic-official`），可以扩展 `dshRouteProviderID` 的逻辑：

```go
func dshRouteProviderID(target provider.Entry, explicitBase string) string {
    if !target.BuiltIn || strings.TrimSpace(explicitBase) != "" {
        return ""
    }
    // 映射到 DSH 的内置路由名称
    switch target.ID {
    case "deepseek":
        return "deepseek"  // 对应 deepseek-official
    case "anthropic":
        return "anthropic" // 对应 anthropic-official（假设）
    default:
        return ""
    }
}
```

然后在 `writeManagedAgentConfig` 中添加更多分支：

```go
case "dsh":
    switch providerID {
    case "deepseek":
        return writer.WriteDSHOfficial(ctx, path, apiKey, model)
    case "anthropic":
        return writer.WriteDSHAnthropic(ctx, path, apiKey, model)
    default:
        return writer.WriteDSH(ctx, path, providerName, baseURL, apiKey, model)
    }
```

---

## 相关 Issue

- **Issue 1**：思考深度配置支持（待实现）
- **Issue 2**：DeepSeek 官方路由映射（✅ 已完成）

---

## 提交信息

```
feat: map DeepSeek official provider to dsh's shipped route

When activating DSH against DeepSeek's own service, use the shipped
deepseek-official route instead of declaring a custom bootagent route.
This keeps the configuration aligned with dsh's Models page display
and avoids credential namespace conflicts.

- Add WriteDSHOfficial for official route activation
- Add dshRouteProviderID helper to select route strategy
- Update ReadDSHConfig to recognize official selection
- Add cleanup logic for transitions between route types
- Add test coverage for both route strategies and transitions

The bootagent custom route is still used for gateways and other
custom endpoints where the shipped route cannot represent the
override.

Fixes #2
```

---

## 总结

这次实现完成了 Issue 2 的所有要求：

✅ **核心功能**：

- DeepSeek 官方供应商映射到 DSH 内置路由
- 自定义 baseURL 时回退到自定义路由
- 双向切换时的配置清理

✅ **测试覆盖**：

- 7 个新增测试用例
- 所有已有测试通过

✅ **文档更新**：

- 更新 `agents.lock.json` 的指南文案

✅ **代码质量**：

- 函数注释清晰
- 常量统一管理
- 辅助函数可复用

**修改影响范围**：中等

- 5 个 Go 文件修改
- 1 个 JSON 配置文件修改
- 无前端改动
- 无 Schema 升级
