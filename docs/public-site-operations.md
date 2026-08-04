# OneAgent 公开站运营与发布手册

状态：已迁出。站点自己的构建命令、环境变量和部署步骤见
[MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site) 的 README。

公开站曾经是本仓库的 `site/` 目录，现在是独立仓库。本文件只保留仍然约束本仓库的部分，
不再重复站点侧的操作步骤——两处各写一份必然慢慢分叉。

本文原先描述的 `.github/workflows/technical-preview.yml` 和 `.github/workflows/site.yml`
都已不存在（本仓库当前只有 `build-artifacts.yml`），按那两个工作流写的发布顺序因此
已经失效，不要照着执行。

## 仍然由本仓库承担的部分

**GitHub Release 是公开版本与资产的事实源。** 站点在构建时调用 GitHub Releases API，
只读取已发布、非 Draft 的 Release；页面上的版本标签、发布日期、下载地址、文件大小和
SHA-256 都来自那里，站点不读取本地 `release/` 目录，不复制下载资产，也不维护版本回退值。
所以本仓库这边的义务是：Release 一旦发布就是公开事实，资产、校验和、签名状态必须在发布
**之前**检查完毕。

**`providers.lock.json` 是商业披露字段的真源。** Provider 的 `relationship`、
`disclosure`、`referral_url` 在这里维护，且不能影响 Agent rank、兼容性结论、默认选择
或连接测试。这条边界属于本仓库，站点只把结果展示出来。

**改 lock 文件不会自动改变站上内容。** 站点把 `agents.lock.json` 和
`providers.lock.json` vendor 到它自己的 `data/` 目录，从发行 tag 刷新而不是跟随本仓库
`main`。这是刻意的：站描述的是已发布版本支持什么，跟着 `main` 会把已合并但未发布的
Agent 宣传成可用。新增 Agent 或调整披露字段后，需要到站点仓库按其 `data/README.md`
刷新一次。

**Stable 门禁不变。** 各平台的签名、公证和原生 cleanroom 门禁仍是 App 发布流程的要求，
GitHub Release 不替代产物验证。

## 历史背景

设计决策记录在
[ADR-006](decisions/ADR-006-public-site-and-generated-release-index.md)。该 ADR 中
「在同一仓库维护 `site/`」的部分已被本次拆分取代；不把营销路由加入本地 Launcher 的
结论仍然有效。
