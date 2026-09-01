# 导入导出功能测试方案

## 测试目标

验证 Provider、Profile、MCP、Skill 的迁移在真实文件选择、序列化、解析、冲突和失败恢复场景下保持数据完整，不泄露密钥，也不破坏现有 v1 文件兼容性。

## 功能矩阵

| 场景 | 预期结果 | 自动化层级 |
| --- | --- | --- |
| v1 JSON 导出/导入，无 API Key | Key 不出现在文件中，目标端保留已有 Key | Vitest + Wails E2E |
| v1 明文 Key 导出 | 必须二次确认，文件明确包含 Key | Vitest |
| v1 加密 Key 导出 | 正确密码可恢复，错误密码拒绝且不写入 | Vitest |
| v1 旧字段 `base_url`、`api_key` | 兼容读取并丢弃 Profile 本地敏感字段 | Vitest |
| MCP 脱敏/明文/加密 | 分别符合 secret mode，Agent 绑定不泄露 | Go |
| 单 Skill 文件夹导入 | 预览显示名称、描述、文件数、大小和目标 Agent | Go + Wails E2E |
| 单 Skill ZIP 导入 | 与文件夹导入产生相同候选结果 | Go |
| 单 Skill ZIP 导出后重新解压 | `SKILL.md`、目录结构和 SHA-256 保持一致 | Go |
| v2 ZIP 多 Skill 导出 | 清单、配置和每个 Skill 包都可读取 | Go |
| v2 ZIP 缺少清单/版本错误 | 拒绝导入，不修改本地数据 | Go |
| v2 ZIP 路径穿越、重复路径、符号链接 | 拒绝导入 | Go |
| 导入覆盖已有 Skill | 预览列出覆盖项，确认后原子替换并保留备份 | Go + Wails E2E |
| 导入取消 | 不产生文件、不改变草稿和现有配置 | Vitest + Wails E2E |
| 选择空文件/非 JSON/损坏 ZIP | 显示可本地化错误，不显示堆栈或敏感路径 | Vitest + Go |
| 大量数据（1000 个 Profile/Skill） | 页面固定滚动、搜索结果唯一，无水平溢出 | Vitest + Playwright |
| 单文件超过大小限制 | 在解析阶段拒绝，不解压到持久目录 | Go |
| 跨平台路径（Windows 分隔符） | 统一转换为 ZIP `/` 路径，不允许绝对路径 | Go |

## 发布前执行

```text
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run typecheck && pnpm run test -- --run && pnpm run build
cd frontend && pnpm run test:e2e
python3 scripts/check-docs.py
```

人工验收需使用真实桌面二进制完成一次：导出 v1、导出单 Skill、导出 v2、重新选择文件导入，并确认取消和冲突预览不会改变原配置。
