# ADR-007：独立公开站与机器生成发行索引

- 状态：Accepted
- 日期：2026-07-28

## 背景

OneAgent 的 React 前端是随本地 Launcher 打包的操作界面。公开下载、搜索内容、发行证据和企业服务需要静态可索引页面，两者的安全、缓存、路由和发布周期不同。手工维护下载页版本与哈希会产生事实漂移。

## 决策

1. 在同一仓库维护独立 `site/` Astro 静态站，不把营销路由加入本地 Launcher。
2. 平台 manifest 与 SHA256SUMS 保持构建事实源；人工渠道状态放入 `distribution/channels.json`。
3. 通过 `scripts/build_release_index.py` 验证并生成公开 `/release-index.json`，下载页面只消费该数据。
4. Agent 兼容目录由 `agents.lock.json` 生成只读投影；Provider 商业披露放在独立数据文件，不能影响 rank 或技术结论。
5. 网站默认不加载客户端分析脚本；Launcher 保持默认无遥测。

## 后果

- Launcher 无需为官网 SEO、域名或外部托管做重构。
- 发布站点必须拿到受验证的原生 artifact 才能显示下载按钮。
- GitHub Pages、自有对象存储或其他镜像可以更换，但同版本包体与 SHA-256 不得变化。
- 网站新增独立 Node 依赖和 CI 作业；源代码包包含网站源码但不包含 `node_modules`、`site/dist` 或复制后的下载目录。
