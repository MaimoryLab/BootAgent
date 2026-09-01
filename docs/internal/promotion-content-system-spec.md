# BootAgent 版本推广内容系统定版需求

**状态：** Draft for implementation（定版约束已冻结）
**适用范围：** 每次 BootAgent 发布新版本时，自动生成小红书与 X 两套推广内容
**维护位置：** `docs/internal/`
**最后更新：** 2026-08-25

## 1. 目标与边界

版本发布事件触发后，系统自动读取 release 信息，生成一份统一 Brief，并分别产出：

- 小红书：一张 3:4 竖版推广封面 + 一篇可直接复制的中文推文。
- X：一张 16:9 横版推广封面 + 一篇可直接发布的英文优先推文。
- 两条渠道共享产品能力事实、版本事实、品牌 Token、素材禁用项和审核结果，但不共享最终文案结构。
- 文案面向首次接触 BootAgent 的小白用户；默认读者不熟悉 CLI、Provider、Profile、MCP、runtime 或配置文件。

第一阶段只实现：

1. 版本事件接入。
2. Brief 生成。
3. 两套图片与推文生成。
4. 人工审核。
5. PNG 与 Markdown 导出。

第一阶段不自动操作小红书或 X 账号，不保存登录 Cookie，不自动发布。发布排期、平台 API 和数据回写属于第二阶段。

## 2. 已冻结的内容策略

### 2.1 统一品牌主张

主张：**Agent 很多，管理只需一个。**

产品定位：BootAgent 是本地桌面工作台，用于统一管理 AI 编程 Agent，覆盖检测、安装、模型服务配置、Profile、MCP 和日常维护。

传播重点顺序：

1. 小白用户能用 BootAgent 完成什么。
2. 使用前后的具体变化：少找工具、少改配置、少记命令。
3. 一个清晰、可想象的使用场景。
4. 用户能立即获得的结果和下一步行动。
5. 版本更新内容作为可信背书；只有影响用户体验的变化才进入正文。

产品能力优先级：

- 统一管理多个 AI 编程 Agent。
- 可视化完成检测、安装、更新和启动。
- 连接模型服务，选择模型，并复用配置模版。
- 准备 Node.js、uv 和 Aider 所需运行时。
- 管理 Provider、Profile、MCP 和本地备份。
- 在本机维护一套可重复的 Agent 工作环境。

技术修复的表达规则：

- 普通 bug 修复、内部重构、测试补充、依赖升级默认不进入主标题或主体段落。
- 只有用户能直接感知的修复，才以“体验更顺”“退出更符合预期”这类结果语言作为辅助信息出现。
- 版本号只作为角标或文末信息，不作为小白用户的主要阅读入口。
- 不把 Agent 名称、协议名和内部实现名堆在第一屏；需要出现时必须顺带解释它对用户有什么用。

### 2.2 小红书文案风格（冻结）

- **语言：** 简体中文。
- **语气：** 像一个已经踩过坑、正在分享解决方案的产品用户；清楚、具体、友好，不端着。
- **默认认知：** 读者不知道 Agent、Provider、Profile、MCP 是什么，首段不能依赖这些术语。
- **结构：** 用户痛点 → BootAgent 能做什么 → 3 个以内使用结果 → 简化后的上手方式 → 版本体验补充 → CTA → 话题。
- **内容占比：** 产品能力与使用结果约 70%–80%；版本更新与 bug 修复约 10%–20%；品牌和 CTA 约 10%。
- **长度：** 标题 18–28 个汉字；正文 180–420 个汉字；最多 6 个话题标签。
- **表达：** 短段落、短句、适量 emoji；每段最多 1 个 emoji，不使用连续表情刷屏。
- **标题方向：** 搜索型和结果型优先，例如“AI 工具太多？用 BootAgent 一起管理”、“不想反复改配置？试试这个 Agent 工作台”。
- **CTA：** 引导读者查看下载页、评论区提问或收藏教程；海报本身不贴网址。
- **禁止：** 空泛的“革命性”“颠覆性”“百分百成功”；未经 release 事实支持的性能、兼容性和数量承诺；把第三方 Agent 写成 BootAgent 自有产品。

### 2.3 X 文案风格（冻结）

- **语言：** 英文优先；必要的产品名、Agent 名、命令和配置字段保持原文。
- **语气：** clear, approachable, useful, builder-friendly；先讲结果，再讲实现。
- **默认认知：** 读者可能不了解 Agent 管理工具，首句要能独立理解。
- **结构：** user problem → what BootAgent does → 2–4 个 user outcomes → optional release note → CTA → link。
- **内容占比：** 产品能力与使用结果约 70%；版本更新与 bug 修复约 10%–20%；品牌和 CTA 约 10%。
- **长度：** 单条 240 字符以内；需要补充信息时拆为最多 3 条 thread；不使用长段落。
- **表达：** 短句、动词开头、少术语；技术词第一次出现时必须解释用户收益；可使用 1–2 个 emoji，但默认不使用表情也成立。
- **CTA：** 引导查看下载、体验或了解完整能力；链接放在推文中，不放在封面内。
- **禁止：** 直接翻译小红书正文；堆叠中文营销话术；没有证据的 benchmark、star 增长、用户数量或“best/fastest”表述。

### 2.4 小白用户内容验收

每条推广文案必须先通过以下问题：

1. 不知道 Agent 是什么的人，能否在前两句理解 BootAgent 是做什么的？
2. 文案是否至少讲清楚两个具体能力，而不是只说“更方便”“更强大”？
3. 是否把“少找工具、少改配置、少记命令”翻译成用户能想象的结果？
4. 是否把技术名词降级为补充说明，而不是开场主语？
5. 删除版本号和 bug 修复后，文案是否仍然成立？如果不成立，说明产品价值写得不够。
6. 是否给出一个不需要专业背景也能执行的 CTA？

不通过时，优先重写产品价值段，不优先增加版本细节。

## 3. 推广图片生成工具定版

### 3.1 唯一默认生成器

**GPT Image 2** 作为唯一默认的推广图片生成器。

编排层必须通过统一的 `ImageGenerator` 适配接口调用，不允许在同一条工作流中按失败情况随机切换多个模型。这样可以保证不同版本之间的画面风格、可追溯性和成本可控。

建议接口字段：

```text
ImageGenerator.generate({
  channel: "xiaohongshu" | "x",
  aspectRatio: "3:4" | "16:9",
  prompt: string,
  seed?: string,
  version: string,
  outputPath: string
})
```

### 3.2 工具使用边界

- 图片生成器只负责生成无文字或少量非关键装饰元素的视觉底图。
- 版本号、标题、CTA、网址、命令、API Key、产品能力说明必须由排版层使用可编辑文字写入，不能烘焙进图片。
- 每个渠道每个版本只生成 1 张主封面；失败时允许同一 prompt 重试 1 次，仍失败则进入人工处理，不自动换模型。
- 必须记录：模型名、prompt 版本、输入版本号、生成时间、输出路径、审核状态。
- API Key、Cookie、用户隐私、未公开 roadmap 不得进入 prompt。
- 推广封面不得生成二维码、网址、仿真 UI 截图或容易被误认为真实产品界面的内容。

## 4. 推广图片视觉系统定版

### 4.1 共享品牌 Token

| Token | 值 | 用途 |
| --- | --- | --- |
| `background` | `#F8F4EC` | 主背景、留白 |
| `surface` | `#FBF9F5` | 卡片、内容承载面 |
| `ink` | `#211F1B` | 标题、深色块 |
| `brand` | `#A94622` | 主焦糖橙、重点信息 |
| `accent` | `#D97B4F` | 高亮、标签、微装饰 |
| `muted` | `#827C70` | 辅助信息、说明文字 |

视觉约束：每张封面最多使用 4 个颜色角色；优先使用米白 + 炭黑 + 焦糖橙，避免赛博霓虹、玻璃拟态、随机渐变和泛科技 stock image。

### 4.2 共享构图原则

- 视觉核心只有一个：产品结果或一个抽象的工作流/Agent 主题。
- 留出明确的文字安全区；关键文字不放在复杂纹理上。
- 使用克制的编辑型产品视觉：大块留白、圆角卡片、轻微纸张/颗粒质感、清晰的模块节奏。
- 不把版本号做成主标题；版本号最多作为角标或辅助标签。
- 不在封面中放仓库地址、下载地址、二维码和长段落。
- 任何包含截图的版本，截图必须来自真实产品，并且 API Key、路径中的隐私信息全部打码。

### 4.3 小红书封面规格

- 画布：`1080 × 1440`，比例 `3:4`。
- 视觉重心：上方标题 + 中部产品/工作流视觉 + 下方极短行动提示。
- 标题：最多 2 行，突出用户结果；建议 72–120 px。
- 辅助说明：最多 2 行；不超过 36–40 px 的视觉层级。
- 版本标签：`BootAgent · vX.Y.Z`，只做辅助标识。
- 封面不贴链接；下载入口和仓库入口放正文或置顶评论。

### 4.4 X 封面规格

- 画布：`1600 × 900`，比例 `16:9`。
- 视觉重心：左侧短标题 + 右侧单一抽象视觉或产品结果视觉。
- 标题：最多 2 行，适合缩略图阅读。
- 辅助说明：最多 1 行，强调 release 变化。
- 版本标签：可保留，但不得大于主标题的 25%。
- 封面不放长链接；链接放推文正文。

## 5. 固定提示词

以下提示词作为模板冻结。版本事件只允许替换 `{VERSION}`、`{RELEASE_THEME}`、`{FEATURE_FOCUS}`、`{PRODUCT_VISUAL}` 等变量，不得改变核心风格约束。

### 5.1 小红书主封面 Prompt

```text
Create a polished editorial product-promotion cover for BootAgent, a local desktop workbench for managing AI coding agents.
Canvas ratio: 3:4 portrait, clean composition with generous negative space.
Use this exact visual direction: warm off-white paper background #F8F4EC, deep charcoal #211F1B, caramel orange #A94622, soft orange accent #D97B4F, restrained editorial-tech style, rounded cards, subtle paper grain, crisp geometric workflow motifs, calm and trustworthy, not cyberpunk, not neon, not glassmorphism, not stock photography.
The visual subject should communicate {PRODUCT_VISUAL} and the release theme {RELEASE_THEME}.
Keep a quiet safe area in the upper half for live typography.
Do not render any readable text, URL, QR code, API key, fake UI copy, logo imitation, or watermark inside the image.
Do not create a collage. Use one clear hero visual with 2–3 supporting geometric details.
This is a background visual for a Chinese social-media cover; leave enough clean space for a Chinese headline and a small version label.
```

### 5.2 X 主封面 Prompt

```text
Create a refined editorial product-promotion banner for BootAgent, a local desktop workbench for managing AI coding agents.
Canvas ratio: 16:9 landscape, strong left-to-right reading flow.
Use this exact visual direction: warm off-white #F8F4EC, deep charcoal #211F1B, caramel orange #A94622, soft orange accent #D97B4F, minimal editorial-tech composition, rounded modular cards, subtle paper grain, precise workflow or agent-network motif, premium but approachable.
The visual subject should communicate {PRODUCT_VISUAL} and the release theme {RELEASE_THEME}.
Reserve a quiet area on the left for a short English release headline and a small version label.
Do not render any readable text, URL, QR code, API key, fake UI copy, logo imitation, or watermark inside the image.
Avoid cyberpunk neon, excessive gradients, generic robots, server-room clichés, stock photography, and busy collages.
Use one clear hero visual and a restrained supporting motif.
```

### 5.3 Prompt 变量规则

- `{VERSION}`：只接受 release tag，例如 `v0.7.0`；不能直接拼接未验证版本。
- `{RELEASE_THEME}`：从 changelog 提炼的 3–8 个词，例如 `visual agent setup`。
- `{FEATURE_FOCUS}`：一个用户结果，不超过 12 个词。
- `{PRODUCT_VISUAL}`：抽象的产品结果，例如 `a clean visual setup flow for multiple AI agents`。
- 任何变量为空时，工作流必须阻止生成并提示补齐，不允许让模型自由猜测。

## 6. 固定输入与输出

### 输入对象 `PromotionBrief`

```json
{
  "version": "vX.Y.Z",
  "release_url": "...",
  "release_date": "YYYY-MM-DD",
  "changelog_facts": [],
  "feature_focus": "",
  "target_audience": "",
  "product_visual": "",
  "download_cta": "",
  "known_limits": [],
  "safety_notes": []
}
```

### 输出对象 `PromotionBundle`

```text
promotion/
  {version}/
    xiaohongshu/
      cover.png
      post.md
      brief.json
      generation.json
    x/
      cover.png
      post.md
      brief.json
      generation.json
    review.md
```

### `review.md` 最低检查项

- 版本号与 release 一致。
- 所有功能描述都能在 changelog 或产品文档中找到依据。
- 两个平台文案没有互相复制粘贴。
- 图片没有 URL、二维码、API Key 和错误 Logo。
- 图片比例、品牌色、安全区符合规范。
- 小红书海报未贴链接；X 链接只出现在推文正文。
- 审核人、审核时间、修改意见可追溯。

## 7. 状态机约束

```text
RELEASE_DETECTED
  → BRIEF_READY
  → ASSETS_GENERATING
  → ASSETS_READY
  → HUMAN_REVIEW
  → APPROVED
  → EXPORTED
```

失败状态：

- `BRIEF_INVALID`：缺少版本事实或变量，回到输入检查。
- `GENERATION_FAILED`：同一渠道最多重试 1 次，之后进入人工处理。
- `REVIEW_REJECTED`：必须记录原因，回到 Brief 或单渠道生成节点。
- `EXPORT_FAILED`：保留审核结果，允许重新导出，不重复生成图片。

状态转移必须写入日志，日志中不得保存 API Key、Cookie 或完整用户环境变量。

## 8. 面向小白用户的生成工作流

```text
读取 release 与产品能力事实
  → 先生成“小白用户 Brief”
  → 生成产品能力优先的两条渠道文案
  → 生成一张表达产品结果的封面
  → 将版本修复作为辅助信息补充
  → 人工检查是否无需专业背景即可理解
  → 审核通过后导出
```

`PromotionBrief` 生成时必须优先填充：

- `product_capabilities`
- `user_problems`
- `user_outcomes`
- `simple_use_case`
- `feature_focus`

`changelog_facts` 仍然必须保留，但默认只用于事实校验和可选的辅助段落，不得自动成为标题。

## 9. awesome-gpt-image-2 风格库集成

### 9.1 集成范围

BootAgent 只离线复用 `freestylefly/awesome-gpt-image-2` 的 `data/style-library.json` 元数据：13 个分类、19 个风格、10 个场景和 22 个模板的标题、指导与常见问题。固定上游提交、文件哈希和 MIT 许可证记录在 `scripts/promotion/awesome-gpt-image-2/UPSTREAM.md`。

不引入上游案例图片、第三方提示词全文、Vite 展示站、Supabase、Stripe、支付宝或上游 `api/generate-image.js`。上游生图接口依赖登录、积分、次元 API，固定方形低质量 JPEG，与本系统的渠道比例和本地优先边界不一致，因此不直接复用。

### 9.2 离线 Prompt Builder

使用 `scripts/promotion/build_prompt.py` 从渠道 `brief.json` 生成可审计的 `prompt.json`：

```text
python3 scripts/promotion/build_prompt.py \\
  --brief outputs/promotion/v0.7.3/xiaohongshu/brief.json \\
  --channel xiaohongshu \\
  --output outputs/promotion/v0.7.3/xiaohongshu/prompt.json
```

Builder 负责选择海报模板、合并渠道比例、产品能力、用户结果和本地安全约束；不访问网络、不读取密钥、不调用远程 API。生成结果记录模板 ID、上游 commit、数据哈希、渠道、画布规格和 `remote_generation: false`，供既有 `ImageGenerator` 或人工生图环节消费。

### 9.3 复用、修改与新增文件

- 可直接复用：`style-library.json` 中的模板元数据、`guidance` 和 `pitfalls`；`poster-layout-system` 作为当前推广海报默认模板。
- 新增：`scripts/promotion/awesome-gpt-image-2/style-library.json`、`LICENSE`、`UPSTREAM.md`、`scripts/promotion/build_prompt.py`、`scripts/test_promotion_prompt_builder.py`。
- 修改：`NOTICE` 与本规格文档。
- 暂不修改：Go/Wails 服务、React 页面、Provider/API Key 存储、Skill 安装逻辑和远程生图链路。

### 9.4 更新与安全要求

上游只允许固定提交人工升级；升级时必须重新核对 SHA-256、许可证和模板字段。不得运行时拉取上游数据。第三方案例图片与提示词来源不完整、商用权不确定，不得进入 BootAgent 发布包或推广资产。Prompt Builder 输出不得包含 API Key、Cookie、用户 HOME、完整环境变量或未公开 roadmap。

## 10. 定版后的实施计划

### Iteration 1：数据与模板

- 定义 `PromotionBrief`、`PromotionBundle` 和状态枚举。
- 增加 release tag / changelog 解析器。
- 增加 `product_capabilities`、`user_problems`、`user_outcomes`、`simple_use_case` 字段。
- 建立面向小白用户的小红书与 X 文案模板和字段校验。
- 将品牌 Token 与固定 prompt 模板放入版本化配置。

### Iteration 2：生成与资产落盘

- 实现 `ImageGenerator` 适配接口，默认绑定 GPT Image 2。
- 实现每渠道单次生成、单次重试和生成元数据记录。
- 生成 PNG、Markdown、JSON 与 review 素材包。
- 增加图片比例、文件存在性和 prompt 变量完整性检查。

### Iteration 3：审核工作台

- 展示 Brief、产品能力事实、两套封面、两套推文和版本事实来源。
- 增加“小白用户可理解性”检查：首句定位、能力解释、用户结果、术语降级、CTA 可执行。
- 支持单渠道退回，不影响另一渠道已通过的资产。
- 支持修改文案后重新审核；图片默认不重复生成。
- 提供敏感词、链接、API Key、产品事实和版本事实检查。

### Iteration 4：导出与后续扩展

- 一键导出当前版本完整推广包。
- 预留排期字段和平台发布适配器，但默认关闭。
- 后续再评估小红书与 X 的账号授权、自动发布和数据回写。

## 10. 验收标准

实现完成后，使用一个真实 release 做端到端验收：

1. 版本事件能生成正确的 `PromotionBrief`。
2. 两个渠道都能得到一张符合比例和品牌规范的封面。
3. 两个渠道都能得到独立文案，且内容事实一致、表达方式不同。
4. 生成失败只重试一次，不会无限循环或随机切换模型。
5. 审核不通过可以回退到正确节点，不丢失已生成资产。
6. 导出的推广包目录结构稳定，能被人工直接使用。
7. 日志不包含 API Key、Cookie、私密路径或完整环境变量。
8. 未经人工审核，系统不会执行任何平台发布动作。
