# 制作与发布 Caelis 内容包

Caelis Bot 的应用代码保持免费开源。角色、完整服装变体和头像是独立的数据内容，
不携带执行代码，不改变助手的身份、对话、Runtime 或权限。你可以用熟悉的建模和绘图工具创作，
不需要官方的私有制作工程或 API 密钥。

当前实现是本地内容包 v1：**GLB 角色/完整服装变体 + PNG 头像**。
支持在设置中导入、选择、保留多个版本、回退与移除。独立蒙皮衣服、
挂点配件、桌面装饰及第三方动态 SVG 尚未开放；不要在包内放置这些未支持字段。
内置角色的增强表情、手指、近身动作与 SVG 动画使用另一份现有成品合同，不能直接当作社区合同。

2026-09-28 的[控制 POC](architecture.md)先验证模型语义动作与真实桌面联动，
官方/社区包的公共 profile 扩展待可行性和产品价值成立后决定。尚未开放 profile 文件或新字段，继续使用下述现有格式。

## 五分钟跑通第一个包

以下命令在公开 `caelis-bot` 仓库根目录执行，使用 `go.mod` 指定的 Go 版本。
示例几何和火柴人采用仓库的 Apache-2.0 授权，与默认人物的独立资产授权区分。

```sh
# 构建离线工具；不需要启动或登录 Caelis/Codex。
mkdir -p .cache/content-demo
GOWORK=off go build -o .cache/content-pack ./cmd/content-pack

# 从仓库提供的通用火柴人成品开始，输出目录必须不存在。
.cache/content-pack init \
  --dir .cache/content-demo/orbit \
  --id example.orbit \
  --name 'Orbit' \
  --author 'Caelis Bot contributors' \
  --license Apache-2.0 \
  --license-file LICENSE \
  --model frontend/public/models/stick.glb

.cache/content-pack pack \
  --out .cache/content-demo/orbit-1.0.0.caelispack \
  .cache/content-demo/orbit

.cache/content-pack verify .cache/content-demo/orbit-1.0.0.caelispack
```

打开 Caelis Bot → **设置 → 外观 → 导入**，选择生成的 `.caelispack`，然后在“角色与服装”
选择 Orbit。导入只安装内容，不自动替换正在使用的形象。切回 Caelis 即恢复内置表现。
示例使用通用火柴人及其基础动作。`internal/contentpack/testdata/basic.glb` 是无骨架、
无动画的几何校验夹具，仅供开发测试，不作为预置角色发行。

这是开发工具而非运行时依赖。用户只需要 `.caelispack` 和 App，不需要 Go、Node 或 Blender。
工具当前从源码构建，还没有单独发布的工具二进制。

## 用自己的作品替换

1. **制作**：创建自己的模型、服装或头像，保留作者与来源记录；确认允许以所选授权分发。
2. **导出**：模型导出为自包含 GLB；几何、材质、纹理、骨架、动画均嵌入文件。
   Blender 导出使用 glTF Binary / GLB；应用变换，确认 +Y 向上、正面朝 +Z，动画以原地动作导出。
   不启用 Draco、Meshopt 或外部纹理；v1 支持基础材质和 `KHR_materials_unlit`。
3. **初始化目录**：把上面的 `--model` 改为自己文件，并改包 ID、显示名、作者与许可证。
   可加 `--avatar /absolute/path/portrait.png`。只制作头像时省略 `--model`，保留 `--avatar`。
4. **编辑清单**：需要更多服装、角色或头像时，在 `manifest.json` 增加条目和文件路径，见下例。
5. **打包验证**：运行 `pack` 和 `verify`。两者使用与 App 相同的 Go 校验器，完全离线。
6. **实机预览**：导入后主动切换，检查角色比例、透明区域点击、常用动作、聊天头像、明暗背景、
   小尺寸可读性与减少动态效果。校验通过不代表美术效果合格。
7. **发布**：提供 `.caelispack`、版本、许可证和预览图。无需让使用者替换 App 或重新签名。

校验不通过时，工具退出码非零并给出具体文件/原因，不输出一个“尽量能用”的包。
CLI `-h` 查看各子命令参数。源目录只能包含清单、列明的成品和目录，制作工程和预览草稿放在外面。

## 格式与目录

`.caelispack` 是受限 ZIP；根目录必须有 `manifest.json`，不带外层同名文件夹。
建议用工具打包，确保哈希、大小和 ZIP 元数据一致；相同目录与清单产生相同字节。

```text
my-character/
  manifest.json
  LICENSE.txt
  portrait.png
  summer.glb
  winter.glb
```

下面是可编辑的源清单。`files` 中的哈希/大小由 `pack` 重新计算，因此源目录中可留占位值；
发行包中必须是正确值。不能漏掉文件，也不能把未引用的脚本或额外素材塞进包里。

```json
{
  "format": "caelis-content",
  "formatVersion": 1,
  "id": "yourname.mascot",
  "version": "1.0.0",
  "name": "My Mascot",
  "author": "Your name",
  "license": "Your-License-Identifier",
  "licenseFile": "LICENSE.txt",
  "characters": [{
    "id": "mascot",
    "name": "Mascot",
    "variants": [
      {"id": "summer", "name": "Summer", "model": "summer.glb", "avatar": "portrait", "capability": "basic-3d-v1"},
      {"id": "winter", "name": "Winter", "model": "winter.glb", "avatar": "portrait", "capability": "basic-3d-v1"}
    ]
  }],
  "avatars": [{"id": "portrait", "name": "Portrait", "image": "portrait.png", "capability": "png-v1"}],
  "files": [
    {"path": "LICENSE.txt", "sha256": "", "size": 0},
    {"path": "portrait.png", "sha256": "", "size": 0},
    {"path": "summer.glb", "sha256": "", "size": 0},
    {"path": "winter.glb", "sha256": "", "size": 0}
  ]
}
```

`id` 为 `发布者命名空间.包名`，每段小写字母开头，只允许小写字母、数字、短横线，最长 48 字符。
角色、变体、头像 ID 使用单段相同规则。ID 是稳定引用，不是显示名，发布后不要随改名改变。
命名空间是本地组织名称，不构成作者身份验证；所有导入均明确显示“本地导入”，不显示官方认证标记。

版本使用三段数字，例如 `1.2.0`。同一包同一版本不能覆盖不同字节；修订内容须提升版本。
`formatVersion` 控制格式，`capability` 控制表现合同，两者独立于包自己的版本。
未知字段、格式版本、必需能力会被拒绝，避免静默忽略后得到不完整外观。

路径使用大小写精确匹配的 ASCII 相对路径，只允许字母、数字、`_`、`-`、`.`、`/`。
禁止绝对路径、`..`、反斜杠、大小写重复、Windows 保留名、软链接及特殊文件。
GLB、PNG、TXT 使用小写扩展名。`licenseFile` 必须引用包内 TXT 文件；授权标识不等于授权证明。
许可证文本保留具体使用、二创、再分发条件以及原作者署名。当前 v1 为整包同一授权；
混合授权内容应拆包或由作者在随包授权文本中明确列出各项适用条款，不能抹去原有义务。

## `basic-3d-v1` 的能力与预算

| 项目 | 合同 |
| --- | --- |
| 外形 | 自包含 glTF 2.0 GLB，三角面；无需默认人物骨架、骨骼名或专属 metadata |
| 显示 | 按静止姿态包围盒等比适配桌宠窗口，脚底落在固定锚点；保持原始材质，不套用默认人物材质修正 |
| 动作 | 可选 `idle`、`working`、`attention`、`nod`、`celebrate`；没有 idle 时静止，没有其他动作时回到 idle |
| 生命周期 | idle/working 循环；attention/nod/celebrate 一次播放后回到当前状态；减少动态效果时停止连续播放 |
| 衣服 | 每个变体是可以独立运行的完整 GLB，不做应用端蒙皮拼装 |
| 文件 | 每文件 ≤16 MiB，解压总量及归档各 ≤64 MiB，最多 128 个资源文件，清单 ≤64 KiB |
| 图像 | PNG 头像；GLB 内 PNG/JPEG 纹理；单张最大 2048×2048，GLB 内总纹理预算 16 Mi 像素 |
| 几何 | 最多 128 个 mesh、每 mesh 32 个 primitive；几何及场景实例顶点预算各 250,000 |
| 骨架 | 最多 512 个节点、16 个 skin、每 skin 128 个关节；节点图不得循环或重复归属 |
| 动画 | 最多 32 个片段；片段内每组时间递增且在 0–60 秒内，总输入关键帧 ≤100,000 |
| 访问器 | 最多 2048 个，单个元素数 ≤250,000、总分量数 ≤4,000,000；不支持 sparse accessor |
| 扩展 | 仅允许 `KHR_materials_unlit`；不允许外部 URI、压缩几何解码器或自定义 shader |

动作应为原地动作，主要轮廓不要超出静止姿态很多，否则可能超出窗口。模型导入会适配初始尺寸，
不会替你修复穿模、翻转法线、错误权重或过大的动画位移。面部/手指/视角修正 metadata 在社区
基础合同中不启用；可以把所需动作直接烘焙到片段。片段缺失不会影响对话与任务状态显示。

所有坐标/关键帧必须有限；模型数据本身的范围和引用也会验证。尺寸预算是上限，不是建议目标，
实际日常桌宠应远小于上限。原生验证器是导入边界；Three.js 加载与视觉验收是额外关卡。
某些通过格式检查但在 GPU 上无法加载的资源会触发内置回退，并在外观页显示提示。

## `png-v1` 头像

使用透明背景 PNG，推荐正方形并按 32px 实际显示尺寸检查轮廓。只有 PNG 位图，不能嵌入 SVG、HTML
或外部链接。头像可以跟随某一服装变体，也可作为独立包安装，在“聊天头像”中单独选择。
默认内置的 SVG 动画不会强加到社区 PNG 上；社区动态头像将另行定义版本化能力。
应用图标和状态栏品牌图标不属于社区外观替换范围。

## 迭代、二创与分发

- **自己迭代**：编辑源目录成品和清单，提升 `version`，输出一个新文件名，再导入。
  新版本与旧版本并存，主动选择新版本；切回旧版本即可回退。移除正在使用的版本前先切换外观。
- **从成品开始二创**：先阅读并遵守随包许可证，允许修改/再分发时运行：

  ```sh
  .cache/content-pack unpack --out .cache/my-remix downloaded.caelispack
  ```

  使用自己的包 ID，保留原始授权和署名，明确修改内容。不要复用原作者的 ID/版本冒充更新。
  工具只负责安全读取与还原目录，不授予原本没有的二创或商业权利。
- **批量生产**：同一角色不同服装可以放在一个包的多个 `variants` 中；也可以按发行主题拆成
  独立包。每个包独立版本和授权。不要依赖另一包的磁盘路径、骨骼内部对象或文件下载地址。
- **分享给用户**：分发验证过的 `.caelispack`，附版本、作者、许可证和效果预览。最终模型可被提取，
  不要把私钥、账号、制作备注或不希望分发的源工程放进去。

默认人物与头像仍按 [角色资产授权](../ASSET-LICENSE.md) 分发，不能把“可以导入二创包”理解为
“所有内置人物均可随意二创再分发”。示例和通用火柴人与该人物授权独立。

## 创作者 CI 示例

自己的内容仓库可以维护成品源目录和制作工具，产出公开格式即可；下面的验证脚本不接触私人凭据。
发布流水线请锁定一个已验收的 Caelis Bot 提交，构建该提交的 `cmd/content-pack`，不要跟随浮动 main。

```sh
# 在锁定提交的工具仓库内先 go build -o /tmp/content-pack ./cmd/content-pack。
# 以下在你的内容仓库运行，输出放在源目录之外；替换版本与路径。
mkdir -p release
/tmp/content-pack pack --out release/my-mascot-1.0.1.caelispack content/my-mascot
/tmp/content-pack verify release/my-mascot-1.0.1.caelispack
```

校验成功后才上传成品，发行版本不可覆盖。完整模型还应经过实际 App 视觉验收。
这套流程不要求给创作者应用源码写权限，也不要求 App CI 下载私人制作工程。

## 扩展与维护者落点

- `internal/contentpack`：唯一原生解析/校验/安装/目录/资源读取实现，CLI 与 App 共用。
- `cmd/content-pack`：初始化、收据生成、确定性打包、验证与解包；不管理账号或安装 Runtime。
- `internal/desktop/content.go`：宿主操作入口；原生文件选择，不接受模型输出指定任意文件路径。
- `frontend/src/appearance.ts`：跨窗口外观快照；与后端对话、审批和模型配置分离。
- `frontend/src/character/pet.ts`：预载替换、旧加载隔离与 GPU 资源释放；通用与内置增强表现分开。
- `frontend/src/AppearanceSettings.tsx`：用户导入与选择入口。
- `internal/contentpack/testdata` 与相关测试：简单成品、恶意归档、持久化、回退、资源路由和能力降级案例。

新增能力时先修改合同与这些正反例，随后同时实现原生校验、渲染器、安全回退和文档，再开放给作者。
不要通过新 metadata 暗中开启专属骨架逻辑，也不要让内容包创建线程、执行代码或获得桌面权限。
配件应另有挂点/体型合同，蒙皮服装需要绑定与遮挡合同，桌面装饰需要宿主行为预设；它们不能伪装成
已经支持的 `basic-3d-v1`。

共享核心已保留未来 Windows 接口；文件对话框、数据目录、GPU 与窗口仍需 Windows 原生验收。

## 官方内置资产交付

# 角色成品与仓库边界

应用代码在 `caelis-labs/caelis-bot` 公开维护；制作工程、参考图、作者工具和历史实验
在组织的 private `caelis-labs/caelis-bot-assets` 维护。公开项目不依赖 Blender、私库检出或私人凭据。
应用代码采用 Apache-2.0；内置角色、头像和品牌图标按根目录 `ASSET-LICENSE.md` 单独授权。
通用火柴人与纸飞机为 Apache-2.0。资产授权不限制应用源码本身。

## 成品合同 v1 / v2 / v3

`resources/character-pack.json` 是唯一的成品版本、文件 SHA-256 和授权清单。
它区分 `character.id` 和 `variant.id`：同一角色可以有多个服装成品；新角色可以有独立的变体。
每个变体交付可独立运行的完整 GLB，避免在应用端动态拼装不同蒙皮。内置变体以当前清单为准；本地导入的内容使用独立内容包合同。
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

v3 为官方角色变体增加可选 `portrait`（`version: 1`），交付头像帧序列成品。
`sourceSHA256` 必须匹配该变体 GLB；`poster` 是中性头肩 PNG，`clips` 是动作名到
静态 WebP 图集的映射，文件位于 `frontend/public/portraits/<versioned-variant>/`。
图集按行排列，使用 `frameSize / columns / frameCount / fps` 描述；当前 Caelis 为
128px、12 列、120 帧、20fps、6 秒循环。播放器圆形裁切，动作起止使用同一中性姿态。
每张解码预算最多 8 MiB，最多 32 个命名动作，资源仍受整包预算、哈希、尺寸与来源授权校验。

语义映射为 `companion`（必需回退）、`think`、`scan`、`focus`、`listen`、`waiting`、
`delight`、`dreaming`。已有语义可直接替换图集，缺失动作回退 `companion`；增加新的工作
语义需同时更新公开状态映射。批量制作动作、角色和服装/装饰版本仍在私库完成，交付
完整帧序列和清单即可复用播放器。独立饰品合成、社区动态图集导入尚未开放；本地
`.caelispack` 继续使用 v1 的 PNG 合同。运行时不依赖制作工具或 POC 导出程序。

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
增加新字符或服装先扩充清单的 `characters/variants`，同时检查现有外观选择界面的兼容性。
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

### 可选手持接触校准

`desktopPetFingerRig.gripBones.R.contact.version=1` 可携带 `thumbOffset`、`indexOffset`
（三维指骨局部坐标）与 `rotation`（相对手腕的 xyzw 四元数）。纸飞机下折边放在两个
实际指腹接触点的中间，朝向随手腕转动；缺失或无效数据沿用原挂点，旧资产兼容。
这只是持物挂点数据，不提供任意物体抓取或碰撞求解，也不改变道具生命周期。

风格化道具也可选择 `gripBones.R.presentation={version:1, mode:"hover", handOffset,
rootOffset, rotation}`：`handOffset` 是手腕骨局部坐标，`rootOffset` 是角色坐标系中的
悬浮位移，`rotation` 是角色坐标系中的道具 xyzw 朝向。挂点随手移动，抬升方向与机翼
朝向随角色转动，不受手腕翻转影响。该模式优先于接触校准；向量分量须在 ±0.3 内，
四元数须非零且所有分量有限。旧模型和无效数据继续使用原挂点。

悬浮展示可附加 `handRotation`（角色坐标系中的掌面 xyzw 朝向），由现有手腕求解器
在托物动作中渐变应用，同时带动前臂扭转。纸飞机在掌面距目标小于 0.25 弧度后显现，
避免抬手途中擦过袖口；该挂点只可隐藏道具，不能覆盖调用方的释放/中断隐藏状态。

## 原地动作与宿主责任

| clip 名（区分大小写） | 优先级 | 用途 / 建议动作 | 播放方式与建议时长 |
| --- | --- | --- | --- |
| `idle` | 必须 | 安静站立、轻微呼吸，可少量眨眼；轮廓基本稳定 | 无缝循环，3–5 秒 |
| `working` | 必须 | 思考或轻微专注姿态，不需要打字桌子等道具 | 无缝循环，3–5 秒；不能持续大幅晃动 |
| `attention` | 必须 | 抬头/看向用户/小幅抬手；喝水、下班提醒和待确认事项共用 | 单次，1–2 秒，回到待机姿态 |
| `nod` | 必须 | 轻点头一次，回应用户 | 单次，0.6–1.2 秒，回到待机姿态 |
| `celebrate` | 必须 | 小幅开心、轻举手或轻摆身体，用于完成反馈 | 单次，1–2 秒，回到待机姿态 |
| `wave` | 可后置 | 用户主动唤回时的招呼；不会每次显示都播放 | 单次，1–2 秒 |
| `sleepy` | 可后置 | 困倦/休息，仅作外观表现 | 循环；不表示应用或后台任务真的暂停 |

可以先交骨骼绑定和 idle，其余动作随后补齐。上述后三个必须动作名称与当前 Agent
工具的 attention/nod/celebrate 一致；idle/working 由宿主事实驱动，不新增模型循环。
等待审批只触发一次 attention，随后保持安静，不反复催促。完成、失败、授权范围都以
消息和原生事件为准，动作不会产生授权、改变任务状态或自动重试。

## GLB / Blender 交付约定

- 同一骨架、同一 GLB 内独立命名 clips；制作侧私有保留可重新导出的源工程。
- 导出后的 glTF 空间 Y 向上、角色正面朝 +Z（相机在 +Z）；Blender 自身坐标由导出器
  正常转换。应用对象变换，脚底中心为原点，提供静止高度，宿主再做统一显示缩放。
- 初版全部为原地动画：不使用根骨位移推动窗口，不让脚底漂移。循环首尾衔接；单次
  动作在 neutral/idle 姿态附近起止，方便短过渡。无需跨文件骨骼重定向。
- 约束、驱动器和必要物理效果烘焙到关键帧；GLB 能独立播放，不依赖 Blender 插件或脚本。
- 交付静止包围盒和各动作最大包围盒，尤其是手臂、头发、裙摆的极值。首版不要离开
  原有站位大幅跳跃；宿主需据此适配点击区域、窗口边界与气泡位置，不能沿用静态 mask。
- 纹理嵌入 GLB，优先通用 PBR 材质；列出网格/三角面/材质/纹理数量和最大纹理尺寸。
  第一版建议总面数控制在约 5 万以内、纹理优先 1K/2K；这是起始预算，最终以实机表现为准。
- 附动作名称、时长、是否循环的文本表与每个动作一段预览；注明资产、字体和纹理来源许可。
  不需要先做角色市场、换装系统、表情捕捉或完整动作库。

## 宿主接入责任

本仓库负责 AnimationMixer 与状态映射、动作被打断后的回落、隐藏/减少动态效果策略、
动态点击范围、GPU 资源释放，以及新模型下的拖动/缩放/气泡验收。角色制作会话不需要
更改 Bot 身份、Codex adapter、审批或调度器，也不要直接覆盖当前 stick.glb 验收资产。


当前近身回应、六套待机、视角修正、手指和飞机跑由本仓库姿态层叠加，成品仍保留上述五段 clips。
角色变体必须保持公共骨骼/形变合同，衣服穿模与动作自然度在私库制作验收，宿主中断/恢复由公共测试验证。
新动作分为共同能力和角色专属道具，不把纸飞机列为所有角色的必需动作。
