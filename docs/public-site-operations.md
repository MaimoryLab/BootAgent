# OneAgent 公开分发站运营与发布手册

状态：实施中，适用于 `technical-preview-unsigned` 和未来逐平台 Stable 发布。

## 1. 固定架构

- `frontend/` 是随 Launcher 打包的本地七页向导，继续服从无 CDN、资源内联和本地 API 安全约束。
- `site/` 是独立的 Astro 静态站，只提供产品说明、下载、教程、兼容目录、安全政策、支持和企业服务页面。
- 两者不共享路由、状态或运行时组件；首期只复用品牌语言、真实截图和 Agent 标识资产。
- 官网不进入 OneAgent 安装包，网站构建失败不能改变 Launcher 的本地运行行为。

## 2. 唯一发行事实源

公开下载数据由三层组成：

1. `release/release-manifest-<platform>-<arch>.json`：构建产生的版本、平台、架构、Agent 锁定版本和 artifact 哈希。
2. `release/SHA256SUMS-<platform>-<arch>.txt`：artifact 与 manifest 的独立校验记录。
3. `distribution/channels.json`：人工审核的平台公开状态、原生构建/cleanroom 证据和下载渠道。

`scripts/build_release_index.py` 校验三层一致性并生成 `site/src/generated/release-index.json`。可公开下载的平台必须同时满足：

- manifest 和 checksum 文件存在；
- artifact 文件大小与 SHA-256 完全一致；
- `native_build=true`；
- `cleanroom=verified` 且 evidence 非空；
- 有且只有一个 primary 官方下载渠道；
- 外部镜像使用 HTTPS，并声明与 artifact 完全相同的 `verified_sha256`；
- available 渠道有明确的 `published_at`；
- 渠道与签名状态一致，unsigned 构建不能进入 Stable。

网站的 `/release-index.json` 与下载页读取同一份生成数据，禁止再维护手工版本表。

## 3. 本地开发和验收

```bash
cd site
npm ci
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

`npm run prepare:data` 会从仓库根目录的 manifest、渠道配置、`agents.lock.json` 和 Provider 公开配置重新生成网站数据，并把当前标记为 available 的官方同包 artifact 复制到网站构建目录。

`site/src/generated/` 与 `site/public/downloads/` 都是可再生目录，**均不提交到 Git**：`catalog.json` 是 `agents.lock.json` 加 Provider 配置的纯函数，提交它只会让每次构建产生 diff；`release-index.json` 含 artifact 校验和，提交等于把某一台机器的构建结果冻进仓库。干净检出下 `npm run build` 会先跑 `prepare:data` 重新生成，无需额外步骤。

模拟 GitHub Pages 子路径部署：

```bash
SITE_URL=https://example.com BASE_PATH=/OneAgent npm run build
```

**这份产物不能用 `astro preview` 在本地查看。** `BaseLayout.astro` 会输出绝对 URL 的 `<base href>`，而同一份 CSP 声明了 `base-uri 'self'`：从本机 origin 伺服时浏览器拒绝该 base 标签，样式表与 Agent 标识全部 404，**而每个页面仍然返回 200，只是退化成无样式 HTML**。部署到 Pages 时 base 与页面同源，`'self'` 放行，因此线上不受影响。

跑完这条命令后要恢复可预览的产物，直接重新 `npm run build` 即可。`site.spec.ts` 有一条断言同时检查无 4xx、背景色取自本站样式表、图片全部解码成功，正是为了让这种「200 但坏了」的状态在测试里可见而不是靠肉眼发现。

## 4. 受控预览发布

`.github/workflows/technical-preview.yml` 的顺序固定为：

1. 四个平台原生构建 unsigned preview；
2. 打包 CLI 冒烟和包体检查；
3. macOS arm64 执行真实 cleanroom；
4. 汇总所有平台 manifest 与 checksum；
5. 生成公开 release index、执行网站单元/类型/完整性、三档视口浏览器与可访问性检查；
6. 只把 `distribution/channels.json` 中标记为 available 的平台 artifact、manifest、checksum 和 release index 放进 Draft GitHub prerelease；
7. 人工核对并在 GitHub 上公开该 prerelease；
8. 再次手动运行工作流，关闭 `create_draft_release`、开启 `deploy_pages`；工作流会丢弃本次重建的包体，重新下载已公开 prerelease 的不可变资产来生成下载页，通过门禁后部署 GitHub Pages。

手动触发需要指定 preview tag。`deploy_pages` 默认关闭；Tag 触发只创建不可变 Draft，不会自动部署网站。Draft 只在人工复核平台状态、发行说明和下载校验后公开。工作流拒绝覆盖同一 tag 下已有的不同字节；任何包体变化都必须先提升 OneAgent 版本并使用新 tag，不得在同一版本下替换 artifact。

四个平台的构建结果仍会作为 CI artifact 保留用于验证，但未标记 available 的 Windows、Linux 或 macOS x64 包不会进入 GitHub Release，也不会被复制到网站下载目录。

GitHub repository variables：

- `ONEAGENT_PUBLIC_SUPPORT_URL`：公开 Issues、Discussions 或其他可访问的反馈入口；为空时网站明确显示尚未启用。
- `ONEAGENT_PUBLIC_BUSINESS_EMAIL`：公开商务邮箱；为空时企业页不展示虚构联系方式。

域名、DNS、备案、Pages 开关和签名证书属于仓库外部依赖。

## 5. 镜像与撤回

新增官网外镜像时：

1. 上传现有官方 artifact，禁止重新压缩、追加文件和二次签名；
2. 下载镜像文件并重新计算 SHA-256；
3. 在 `distribution/channels.json` 中使用 `kind=mirror` 并填写与 manifest 完全相同的 `verified_sha256`；
4. 在镜像的 `audit` 对象中记录 `uploaded_by`、`uploaded_at`、`verified_at`、`withdrawal_owner` 和 `withdrawn`；撤回时补充 `withdrawn_at`。镜像必须使用 HTTPS，且不能成为 primary 官方渠道；
5. 重新构建网站，生成器会拒绝哈希未确认的外部镜像。

发生错误或安全事件时，将目标状态改为 `withdrawn`、移除下载链接并重建网站，同时撤回所有渠道。禁止在同一个版本号下替换为不同字节的文件；修复后必须发布新版本。

## 6. Provider 合作披露

Provider 的公开商业数据保存在 `distribution/providers.json`，与 Agent rank 和运行时兼容逻辑分离。

- `relationship=none`：只链接官方主页。
- `relationship=referral` 或 `sponsor`：必须同时填写 disclosure 和 referral URL。
- 商业关系不能改变 Agent/Provider 技术结论、默认选择、连接探测或页面排序。
- OneAgent 不代理推理请求、不托管 Key、不代收充值，不承诺 Provider 的永久价格或固定免费额度。
- 首期只链接官方价格页，不在仓库内复制容易过期的价格表。

## 7. 支持、统计与增长

- 文档、校验信息与已知限制保持公开，社群不是下载前置条件。
- 应用继续默认无遥测；网站当前也不加载客户端分析脚本。
- 如后续启用无 Cookie 聚合统计，允许事件仅限页面访问、下载点击、快速开始入口和企业联系入口；禁止设备指纹、跨站身份、API Key、本地路径和模型请求内容。
- 初始漏斗目标：主页到下载页 20%，下载用户进入快速开始 40%，每 100 次下载的重复性安装支持请求少于 20。

## 8. 企业服务与产品化门禁

首期只销售三个可复用结果：团队启用、环境标准化和商业支持。内部镜像仍只能分发 OneAgent 官方同包产物，不得包含未授权 Agent 二进制。

在至少获得 3 个设计合作方、其中 2 个付费试点前，不开发专业版许可证系统。若试点反复提出团队模板、合规报告、组织版本策略或内部升级策略，必须新建 ADR，重新评估权限、秘密管理、迁移与隐私边界。

## 9. Stable 门禁

- macOS：Developer ID、notarization、stapled ticket、原生 cleanroom。
- Windows：有效 Authenticode、原生构建、SmartScreen 场景验证。
- Linux：按实际架构原生构建和 cleanroom。
- 每个平台独立进入 Stable；网站允许同一时间存在不同成熟度。
- Stable 初期仍采用手动升级说明，不在本阶段加入自动更新。
