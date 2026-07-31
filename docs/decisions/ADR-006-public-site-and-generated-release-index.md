# ADR-006：独立公开站与 GitHub Release 事实源

- 状态：Accepted（2026-07-31 修订）
- 日期：2026-07-28

## 背景

OneAgent 的 React 前端是随本地 Wails Launcher 打包的操作界面。公开下载、搜索内容、发行证据和企业服务需要静态可索引页面，两者的安全、缓存、路由和发布周期不同。站点构建直接读取 Release API 和仓库 JSON，保持独立发布周期。

## 决策

1. 在同一仓库维护独立 `site/` Astro 静态站，不把营销路由加入本地 Launcher。
2. App 工作流只创建 GitHub Release；站点工作流由站点变更、Release 发布或人工操作独立触发。
3. 公开版本、发布日期、下载资产、大小和摘要只读取 GitHub Releases API，不读取本地 App 构建目录，也不维护手工回退版本。
4. Agent 兼容目录直接读取 `agents.lock.json`；Provider 商业披露直接读取独立数据文件，不能影响 rank 或技术结论。
5. 网站默认不加载客户端分析脚本；Launcher 保持默认无遥测。

## 后果

- Launcher 无需为官网 SEO、域名或外部托管做重构。
- Draft 和本地构建不会出现在官网；只有已发布 GitHub Release 能产生版本和下载按钮。
- GitHub Pages 发布不构建 App，App 发布也不构建或部署 Pages。
- 网站只需要 Node 工具链；App 源代码包不再携带站点源码。
