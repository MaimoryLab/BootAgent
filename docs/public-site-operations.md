# OneAgent 公开站运营与发布手册

状态：实施中。

## 架构边界

- `frontend/` 是随桌面 App 打包的 React 客户端。
- `site/` 是独立构建和部署的 Astro 静态站，不进入 App 包体。
- `.github/workflows/technical-preview.yml` 只构建 App 资产并创建 Draft GitHub Release。
- `.github/workflows/site.yml` 只测试、构建和部署 GitHub Pages。

两个工作流没有 artifact 或 job 依赖。发布者人工审核并发布 Draft Release 后，`release.published` 事件会触发站点重建。

## 版本事实源

公开站在构建时调用 GitHub Releases API，只读取已发布、非 Draft 的 Release。页面上的版本标签、发布日期、下载地址、文件大小和可用的 SHA-256 digest 均来自该 API；没有 Release 时页面明确显示尚未发布。

站点不读取 App 的本地 `release/` 目录，不复制下载资产，也不维护版本回退值。Agent 目录直接读取 `agents.lock.json`，Provider 披露直接读取 `distribution/providers.json`。

私有仓库构建需要提供具有 `contents:read` 权限的 `GITHUB_TOKEN`。独立 Pages 工作流使用当前任务的 GitHub token；未提供 token 的本地构建若无法读取私有仓库，会渲染“尚无已发布版本”。

## 本地验证

```bash
cd site
npm ci
npm test
npm run build
npx playwright install chromium
npm run test:e2e
```

模拟 GitHub Pages 子路径部署：

```bash
SITE_URL=https://example.com BASE_PATH=/OneAgent npm run build
```

该子路径产物的绝对 `<base href>` 只适用于配置的 origin。恢复本地预览时重新运行普通 `npm run build`。

## 发布顺序

1. 运行 `Technical Preview Packages`，构建并验证各平台 App 资产。
2. 工作流以新 tag 创建不可变 Draft prerelease；已有 tag 会直接失败，不覆盖资产。
3. 人工检查资产、校验和、签名状态和发行说明后发布 Release。
4. `Public Site` 工作流自动从默认分支构建站点，从该 Release 读取版本与下载信息并部署 Pages。
5. 仅修改站点、Agent 目录或 Provider 披露时，合入 `main` 即可独立部署，不触发 App 构建。

GitHub repository variables：

- `ONEAGENT_PUBLIC_SUPPORT_URL`：公开支持入口；为空时不展示虚构地址。
- `ONEAGENT_PUBLIC_BUSINESS_EMAIL`：公开商务邮箱；为空时不展示虚构邮箱。

## Provider 与稳定版边界

Provider 商业数据保存在 `distribution/providers.json`，不能影响 Agent rank、兼容性结论、默认选择或连接测试。

Stable 仍需按平台满足签名、公证和原生 cleanroom 门禁。GitHub Release 是公开版本与资产的事实源，不替代 App 发布流程中的产物验证。
