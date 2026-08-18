# Recent Work Summary

> 更新：2026-07-31。本文记录当前可复核的 Go/Wails 收尾结果；旧的 Python 计数和命令不再是验收依据。
> 补注（2026-08-04）：本文提到的 `cmd/bootagent-release`、`cmd/bootagent-rc`、
> `cmd/bootagent-provider-smoke` 已于 `23805b0` 移除，职责交给
> `.github/workflows/build-artifacts.yml`。相关命令是历史背景，不可执行。

## 已完成

- Go backend 覆盖 catalog、Provider、安装、配置发现/写入、profile、secret、备份、权限和 CLI。
- React 已切换到生成的 Wails bindings；桌面生产路径不使用 HTTP API。
- 配置写入使用 Go golden fixtures，直接锁定 JSON/TOML 输出，不启动第二套 runtime。
- `cmd/bootagent-release` 生成原生 Wails/Go 包、源码 ZIP、manifest、SHA-256 和第三方 notices。
- `cmd/bootagent-rc` 覆盖隔离 npm prefix、真实最新版本、PATH 解析和无密钥配置采用。
- `cmd/bootagent-provider-smoke` 覆盖 models、Chat Completions、Responses、Anthropic Messages。
- Docker/macOS cleanroom、Go race、React/site 构建和 Wails binding diff 均有独立入口。

## 当前验证入口

```text
go test ./...
go test -race ./...
bash tests/install_test.sh
go run ./cmd/bootagent-release build --channel technical-preview-unsigned --source
go run ./cmd/bootagent-release check release
```

需要真实网络和受保护凭据的检查只在 Release Candidate workflow 执行。Aider 的 Python 3.12 是其上游安装流程的可选外部前置条件，不属于 BootAgent 构建或发行包。

## 设计结论

- `agents.lock.json` 是 Agent 元数据唯一真源；Go catalog 不复制包名和来源，版本由包管理器解析。
- shell wrapper 只定位已构建的 CLI，不按需构建、不调用解释器。
- source map、远程资源、secret、Agent 二进制和任何语言 runtime 都不能进入发行 ZIP。
- Wails Alpha 阶段只发布 `technical-preview-unsigned`；Stable 签名/公证另行验收。
