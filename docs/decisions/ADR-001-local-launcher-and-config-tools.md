# ADR-001：本地启动器采用内置配置加可选配置工具模式

## Status

Accepted

## Date

2026-07-21

## Context

OneAgent 的目标是帮助不同类型的用户激活一个真正可用的本地 AI 开发环境。用户可能只使用一个 Provider，也可能需要在多个 Provider、账号和模型之间切换。

CC Switch 等第三方工具可以帮助用户管理本机 Profile，但把它们直接打进 OneAgent 会引入版本、许可证、安装脚本和配置兼容性风险。

## Decision

OneAgent 采用三层配置方式：

1. OneAgent 内置配置：默认路径，负责首次 Provider 激活和 Agent 配置。
2. CC Switch 等第三方工具：可选路径，通过独立文档使用，不自动安装。
3. 手动配置：兜底路径，用于未支持 Agent、高级用户和故障排查。

OneAgent 不把第三方配置工具、第三方托管 API 服务或共享 Provider Key 作为核心依赖。

所有配置工具说明必须注明：

- 官方来源。
- 支持的 Agent。
- Profile 字段和 Base URL 格式。
- API Key 保存位置。
- 切换后是否需要重启。
- 配置备份和恢复方式。

## Alternatives Considered

### 把 CC Switch 直接内置到启动包

- 优点：用户少一步安装。
- 缺点：需要承担第三方版本、许可证、签名、更新和配置兼容性问题。
- 结论：拒绝。OneAgent 通过文档提供可选入口。

### 只支持手动配置

- 优点：实现简单，兼容性边界清楚。
- 缺点：首次用户容易在 Base URL、模型 ID 和配置文件位置上出错。
- 结论：拒绝。内置配置作为默认路径，手动配置作为兜底。

### 允许任意第三方配置工具自动写入私有状态文件

- 优点：可以覆盖更多 Agent。
- 缺点：私有文件格式不稳定，容易破坏用户配置，也难以审计。
- 结论：拒绝。只写已经确认的官方配置入口。

## Consequences

### Positive

- 第一次配置路径简单、可测试。
- 第三方工具可以独立升级，不拖累 OneAgent。
- 用户可以按照熟悉程度选择内置配置、CC Switch 或手动配置。
- OneAgent 不需要承担第三方配置工具的长期维护责任。

### Negative

- 使用 CC Switch 的用户需要阅读额外文档。
- OneAgent 需要维护配置工具兼容矩阵。
- 不同 Agent 的重启和配置生效方式无法完全统一。

## Follow-up

- 为每个配置工具增加 `verified_at`、`source_url` 和 `supported_agents` 元数据。
- 每次配置工具升级后，重新验证至少一个代表性 Agent。
- 如果未来要自动安装第三方工具，先增加签名校验、版本锁定和用户确认。

