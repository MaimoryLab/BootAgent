# awesome-gpt-image-2 集成说明

- 上游仓库：https://github.com/freestylefly/awesome-gpt-image-2
- 固定提交：`685469889fb72fd5adefae45e1645d527edcb5e7`
- 引入文件：`data/style-library.json`
- 本地文件：`style-library.json`
- `style-library.json` SHA-256：`80f5cae039d0d6f312f0e2de2c9b3fc8a806640b0d517c120d704a71c5e4aa72`
- 上游许可证：MIT，许可证文本见同目录 `LICENSE`

## 集成边界

本项目只复用风格库的分类、风格、场景、模板指导和常见问题字段，不引入上游案例图片、第三方提示词全文、展示站、支付模块、Supabase 或远程生图 API。

上游免责声明明确不保证第三方案例和图片可商用。因此 `data/images/` 不进入 BootAgent 的源代码、发布包或推广资产。

## 升级方式

升级必须人工执行：更新固定 commit，重新下载 `data/style-library.json` 与 `LICENSE`，核对哈希，运行 `python3 scripts/promotion/build_prompt.py` 的测试和项目门禁，并在变更记录中说明模板字段变化。
