# 仓库拆分落地记录

2026-09-22：按用户授权，将本地初始项目压扁为公开基线，制作工程独立维护。

- 公开产品：<https://github.com/caelis-labs/caelis-bot>。
- 私有制作：`caelis-labs/caelis-bot-assets`，归属 caelis-labs 组织。
- 原始检查点 `1a9f2587c9799a7d496bb2a817f9c44b3e5c1c1e` 与全部旧历史私有保全；
  不作为公开仓库的父提交、分支或标签。
- 私库保留原目录结构、逐文件迁移 SHA-256、制作记录和历史检查。
  44 个 Blender 工程及相关参考、脚本和历史 GLB 均迁出公共项目。
- 公共项目仅保留当前成品、运行时合同和实际行为检查；Blender 不再是 doctor、check、smoke 或 build 依赖。
- 成品分离授权，代码继续 Apache-2.0；完整规则见根目录 `ASSET-LICENSE.md`。
- 私库 CI 负责验证与推送成品更新分支，专用 GitHub App 创建 PR、公共 CI 验证；不自动合并。

操作约定、版本格式、可替换服装/角色的边界和回滚方式见[角色成品合同](character-assets.md)。
完整迁移清单、旧历史恢复方法和制作命令在私库，不把私人路径或制作源记录重复放进公开目录。
首次公开采用全新 Git 根，日常 `make check` 防止制作源回流。
