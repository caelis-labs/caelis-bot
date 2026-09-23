# 角色成品与仓库边界

应用代码在 `caelis-labs/caelis-bot` 公开维护；制作工程、参考图、作者工具和历史实验
在组织的 private `caelis-labs/caelis-bot-assets` 维护。公开项目不依赖 Blender、私库检出或私人凭据。
应用代码采用 Apache-2.0；内置角色、头像和品牌图标按根目录 `ASSET-LICENSE.md` 单独授权。
通用火柴人与纸飞机为 Apache-2.0。资产授权不限制应用源码本身。

独立内容包、本地导入与创作者工具边界见
[角色与装饰内容包扩展方案](asset-extensions-plan.md)。其中本地内容包 v1 已实现，创作者入口见[制作与发布内容包](content-packs.md)。
本页记录官方内置成品交付合同，两者版本与校验入口独立。

## 成品合同 v1 / v2

`resources/character-pack.json` 是唯一的成品版本、文件 SHA-256 和授权清单。
它区分 `character.id` 和 `variant.id`：同一角色可以有多个服装成品；新角色可以有独立的变体。
每个变体交付可独立运行的完整 GLB，避免在应用端动态拼装不同蒙皮。当前仅内置
`caelis / sage-dress`；这不是已实现角色/服装切换界面的声明。
角色选择不改变 Bot 身份、对话、任务或授权。

当前合同要求：Y 向上、正面 +Z、脚底原点；五个原地 clips `idle / working / attention / nod / celebrate`；
脸部形变、近身手势、手指、视角修正与拖动跑步沿用当前 `frontend/src/character` 消费的骨骼和 metadata。
改变这些语义需要同时审查应用合同和资产，不能仅升级私库版本。
`script/asset-pack.mjs` 拒绝未知合同版本、任意目标路径、软链接、遗漏文件、错误哈希、
外部 GLB 引用和制作备注；单文件上限 16 MiB，整包 64 MiB。
当前模型使用嵌入的材质和几何，不依赖独立贴图下载。

v2 在相同模型合同上增加 `branding.animatedAvatar`，固定指向
`frontend/assets/caelis-avatar-v1.svg`。这是独立授权的 2D 成品，制作源仍在私库。
SVG 只允许路径、椭圆、分组和渐变；禁止脚本、样式、事件、链接、外部资源、实体和
内置动画，限制 32 KiB。校验通过的本地成品才会内联到聊天 DOM，运行时不加载远程 SVG。

`layered-2d-v1` 使用 128×128 画布：`head` 绕 (64,90) 小幅转动；`eye-left`、
`eye-right` 分别绕 (43,83)、(86,83) 闭眼；内部的 `look-left`、`look-right`
接受局部视线偏移。五组标记必须唯一，基准美术角度放在它们内部的静态分组中。
本地调度与可视区管理属于公开运行时，不随成品携带脚本。
旧 v1 包继续使用 PNG，聊天可正常构建；Dock 和 App 图标仍使用各自的品牌成品。

制作时身体、衣服、头发、脸和饰品仍保持独立层，共享骨架。服装制作与穿模检查在私库进行；
成品需要通过公开运行时的过渡、拖动、手势和形变测试。完整历史 GLB 不进入公开应用测试夹具。
纸飞机是可选角色道具，不属于所有人型角色必须具有的共同能力。

## 更新与回滚

1. 在私库完成制作和视觉检查，按锁定的应用提交导出成品。
2. 私库执行 `npm run release:prepare -- <版本>` 生成 `release/`，清理制作 metadata，记录私有 provenance。
3. 提交私库。其 CI 在锁定的公开应用版本运行完整成品验证和运行时测试。
4. 私库 CI 用仅安装到本公开仓库的 GitHub App 临时 token 推送 `assets/<pack>-<版本>` 分支；
   同一个发布 job 创建 PR，公开 CI 无需私库权限。
5. PR 显示成品版本、哈希及成品文件差异；通过 CI 后由维护者审核小尺寸外观与常用动作，再合并。
   不自动合并。版本已经发布后不可用不同字节覆盖同名版本，需递增版本。
6. 回滚直接撤销该 PR 的成品提交；不需要 Blender，也不需要访问私库。

本地手动交接（私库先生成 `release`）：

```sh
node script/asset-pack.mjs import /absolute/path/to/caelis-bot-assets/release
make check
make smoke
make build
```

`release/` 只能包含清单和列明的成品。导入工具不接受整个制作仓库。
增加新字符或服装先扩充清单的 `characters/variants`，验证后再单独实现产品选择界面。
新增输出格式或动作能力要修改公共合同，并由维护者审核，不能让私库 CI 修改应用源码或工作流。

## 自动化凭据与边界

私库配置 `PRODUCT_APP_ID`（Actions variable）及 `PRODUCT_APP_PRIVATE_KEY`（Actions secret）。
专用 GitHub App 为 `Caelis Character Publisher`，仅安装到公开 `caelis-bot` 仓库，
权限只有 Contents / Pull requests 写入和必需的 Metadata 读取；没有管理、Secrets 或工作流修改权限。
官方 Action 为每次发布签发短期 installation token，结束后撤销。个人 GitHub token 不进入 CI。
公开仓库的 `GITHUB_TOKEN` 保持只读，组织现有 PR 审查设置不变；没有自动批准或自动合并步骤。
轮换密钥时在 App 设置生成新私钥、替换私库 secret，确认一次发布成功后撤销旧密钥。

GLB 本身包含可提取的网格、骨骼和动画；私有化保护制作工程与过程，并不加密运行成品。
来源说明见 `frontend/public/models/caelis-SOURCES.md`；原参考材料的权利不会因拆分或换装而自动变化。
