# CC Switch 配置 PPIO 指引

[English](../../en/tools/cc-switch.md) · **简体中文**

> 本文把 CC Switch 当作“本机配置 Profile 切换工具”使用。CC Switch 的界面、支持的 Agent 和配置字段可能随版本变化；安装和更新请以其官方项目入口为准。

## 1. 获取 CC Switch

请从 CC Switch 的官方 GitHub 仓库或官方项目网站进入下载和安装说明，不要使用来路不明的二次打包版本。

安装完成后，先打开 CC Switch，确认它可以正常启动，再开始添加 PPIO Profile。

## 2. 创建 PPIO Profile

在 CC Switch 中创建一个新的 Provider 或 Profile，字段按以下方式填写：

```text
名称：PPIO
Base URL：https://api.ppio.com/openai
API Key：你的 PPIO API Key
模型：从 PPIO /v1/models 返回的模型 ID
```

如果 CC Switch 的字段名称是 `Endpoint`、`API Base` 或 `Base URL`，只填写基础地址，不要把完整的 `/v1/chat/completions` 写入 Base URL。

## 3. 模型填写规则

优先使用 OneAgent 的“获取模型列表”结果。对于 OpenAI-compatible 配置，模型 ID 是服务端返回的原始 ID，不要自行添加 `openai/`、`ppio/` 等前缀，除非目标 Agent 的官方文档明确要求。

如果 CC Switch 支持模型测试，使用一个最小请求验证；不要在测试输入中粘贴私有代码、隐私数据或长文本。

## 4. 切换后的生效规则

切换 Profile 后：

1. 关闭正在运行的 Agent 终端或应用。
2. 重新打开 Agent。
3. 查看 Agent 当前识别的模型和 Provider。
4. 发起一次最小请求。

不同 Agent 的配置生效方式可能不同。不要根据 CC Switch 显示“已切换”就直接判断 Agent 已经重新加载配置。

## 5. 与 OneAgent 的关系

推荐顺序：

```text
OneAgent 探测 PPIO
→ OneAgent 获取模型列表
→ OneAgent 验证一次请求
→ CC Switch 保存同一组 PPIO Profile
→ 用户按项目切换 Profile
```

这样发生问题时，可以先回到 OneAgent 内置配置判断 PPIO 本身是否可用，再判断 CC Switch 的 Profile 是否正确。

## 6. 常见错误

### 把完整请求地址写进 Base URL

错误：

```text
https://api.ppio.com/openai/v1/chat/completions
```

推荐：

```text
https://api.ppio.com/openai
```

### 使用了不属于 PPIO 的模型 ID

先通过 OneAgent 获取当前模型列表，确认模型 ID 后再复制到 CC Switch。

### 切换后 Agent 仍然使用旧配置

关闭并重新启动 Agent。必要时重新打开终端，让新的环境变量生效。

### 把 Key 发给他人排错

不要发送完整 Key。只发送 HTTP 状态码、模型 ID 是否存在、Base URL 是否正确和脱敏后的日志。

## 7. 版本核验

发行包应记录：

```text
工具名称：CC Switch
工具来源：https://github.com/farion1231/cc-switch
验证日期：2026-07-21
验证内容：能创建 Profile、保存 PPIO Base URL、切换后能完成最小请求
```

每次升级 CC Switch 后，重新验证 Claude Code、Codex 和 OpenCode 至少一个代表性 Agent。

## 官方参考

- CC Switch 官方 GitHub 仓库：<https://github.com/farion1231/cc-switch>
- CC Switch 官方项目网站：<https://ccswitch.io/>
