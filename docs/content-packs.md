# 内容包与角色成品合同

角色、完整服装变体和头像是独立的数据内容，不携带执行代码，不改变 Bot 的长期身份、
对话、任务、Runtime 或权限。默认角色身份与对话身份分离；切换角色或服装不创建新助手。

当前有两份独立合同：用户导入的 `.caelispack` v1，以及随应用交付的内置成品合同 v1/v2/v3。
内置增强表现不等于社区能力；不能把内置 metadata、SVG 或头像图集直接加入社区包。

## 仓库、授权与成品边界

应用代码在公开 `caelis-labs/caelis-bot` 维护，采用 Apache-2.0。官方角色制作在 private
`caelis-labs/caelis-bot-assets` 完成，公开仓库只接收列明的成品、版本、哈希和来源说明。
公开构建不依赖 Blender、制作工程、私库检出或私人凭据。第三方作者可使用自己的工具，
无需官方私有工程或 API 密钥。

内置角色、头像和品牌图标按根目录 `ASSET-LICENSE.md` 独立授权；通用火柴人与纸飞机为
Apache-2.0。代码授权不替代角色授权，允许导入内容也不授予内置角色的二创或再分发权利。
来源记录见 [caelis-SOURCES.md](../frontend/public/models/caelis-SOURCES.md)。原素材权利不会因换装而变化。
GLB 可被提取；交付包不能包含私钥、制作备注、参考材料或源工程。

## 本地导入合同 v1

支持完整 GLB 角色/服装变体（`basic-3d-v1`）和静态 PNG 头像（`png-v1`），包括仅头像包。
设置中的外观页提供导入、选择、多个版本并存、回退和移除。导入只安装内容，需主动选择。
应用图标和状态栏品牌图标不属于社区替换范围。独立蒙皮衣服、挂点配件、桌面装饰、
第三方 SVG 和动态图集没有导入合同。

`.caelispack` 是受限 ZIP；根目录必须有 `manifest.json`，不带外层文件夹。
源目录仅包含清单、列明的成品及其目录。下面的源清单允许 `files` 暂留空哈希和零大小，
`pack` 会重新计算；发行包中每个文件必须具有正确的 SHA-256 和字节数。

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
    "variants": [{
      "id": "summer", "name": "Summer", "model": "summer.glb",
      "avatar": "portrait", "capability": "basic-3d-v1"
    }]
  }],
  "avatars": [{"id": "portrait", "name": "Portrait", "image": "portrait.png", "capability": "png-v1"}],
  "files": [
    {"path": "LICENSE.txt", "sha256": "", "size": 0},
    {"path": "portrait.png", "sha256": "", "size": 0},
    {"path": "summer.glb", "sha256": "", "size": 0}
  ]
}
```

- 包 ID 为 `发布者命名空间.包名`，两段均以小写字母开头，只允许小写字母、数字和短横线，
  每段最多 48 字符。角色、变体和头像 ID 使用同样的单段规则；ID 是稳定引用，不随显示名改变。
- 命名空间不构成作者认证；导入内容显示为本地导入。版本为三段非负整数，每段最多六位，
  除零外不带前导零。同一 ID/版本不可覆盖不同字节；修改成品必须提升版本。
- `formatVersion`、表现 `capability` 与包自身版本独立。未知字段、重复 JSON 键、未知格式或能力
  均拒绝，不通过忽略字段降级为不完整外观。
- 路径为大小写精确匹配的 ASCII 相对路径，最多 160 字符，仅含字母、数字、`_`、`-`、`.`、`/`。
  禁止绝对路径、`.`/`..` 路径段、反斜杠、大小写重复、Windows 保留名、软链接和特殊文件。
- 资源扩展名仅为小写 `.glb`、`.png`、`.txt`；文件必须被引用，不能夹带脚本或额外素材。
  `licenseFile` 必须引用包内 TXT。授权标识不等于授权证明，文本应保留署名及使用、修改、分发条件。
  v1 使用整包授权字段；混合来源须在许可证中明确各项条款，或拆包交付。

### `basic-3d-v1` 与 `png-v1`

| 项目 | 合同与预算 |
| --- | --- |
| 模型 | 自包含 glTF 2.0 GLB，三角面，几何、材质、纹理、骨架、动画嵌入；无需默认人物骨架或 metadata |
| 显示 | 按静止姿态包围盒等比适配，脚底落在固定锚点，保持模型材质，不套用默认人物材质修正 |
| 服装 | 每个变体是独立可运行的完整 GLB，应用不做蒙皮拼装或跨文件骨骼重定向 |
| 动作 | 可选 `idle`、`working`、`attention`、`nod`、`celebrate`；无 idle 时静止，其他片段缺失时回到 idle |
| 播放 | idle/working 循环，其余三个为单次反馈后回到当前状态；减少动态效果时停止连续播放 |
| 文件 | 每文件 ≤16 MiB，解压总量和归档各 ≤64 MiB，最多 128 个资源文件，清单 ≤64 KiB |
| 内容数量 | 最多 16 个角色、每角色 16 个变体、32 个头像；至少有角色或头像之一 |
| 图像 | 静态 PNG 头像；GLB 内 PNG/JPEG 纹理；单张 ≤2048×2048，GLB 总纹理预算 16 Mi 像素 |
| 几何 | 最多 128 个 mesh、每 mesh 32 个 primitive；几何及场景实例顶点预算各 250,000 |
| 骨架 | 最多 512 个节点、16 个 skin、每 skin 128 个关节；节点图不得循环或重复归属 |
| 动画 | 最多 32 个片段，时间递增且在 0–60 秒内，总输入关键帧 ≤100,000 |
| 访问器 | 最多 2048 个，单个元素数 ≤250,000，总分量数 ≤4,000,000；不支持 sparse accessor |
| 扩展 | 仅允许 `KHR_materials_unlit`；无外部 URI、Draco/Meshopt 解码器或自定义 shader |

导出以 +Y 向上、正面朝 +Z，使用原地动作，循环首尾衔接，单次动作在中性姿态附近起止。
约束和必要物理效果须烘焙为关键帧，不能依赖作者工具或脚本。所有坐标和关键帧必须有限，
范围与引用也会验证。预算是上限；模型应避免过大的位移和轮廓变化，以免超出桌宠窗口。
基础合同不启用内置面部、手指和视角修正 metadata，可将需要的表现烘焙到动画。

PNG 头像推荐透明背景、正方形，并按 32px 显示尺寸检查。头像可跟随变体或在聊天中独立选择，
不接受 APNG、SVG、HTML 或外链；内置 SVG 动画不会应用到社区 PNG。

### 创建与验证

在公开仓库根目录使用 `go.mod` 指定的 Go 版本。CLI 是离线开发工具，用户只需 App 和成品包。

```sh
mkdir -p .cache/content-demo
GOWORK=off go build -o .cache/content-pack ./cmd/content-pack
.cache/content-pack init \
  --dir .cache/content-demo/orbit \
  --id example.orbit --name Orbit --author 'Caelis Bot contributors' \
  --license Apache-2.0 --license-file LICENSE \
  --model frontend/public/models/stick.glb
.cache/content-pack pack \
  --out .cache/content-demo/orbit-1.0.0.caelispack .cache/content-demo/orbit
.cache/content-pack verify .cache/content-demo/orbit-1.0.0.caelispack
```

`init` 的输出目录必须不存在。替换 `--model` 可使用自己的 GLB；加 `--avatar portrait.png`
指定头像，只做头像时省略 `--model`。多个角色或变体通过编辑清单增加。`pack` 和 `verify`
使用与 App 相同的 Go 校验器；相同源目录和清单确定性地产生相同包，失败时退出码非零。
可用 `unpack --out <新目录> <文件.caelispack>` 安全还原有权修改的包，工具不授予二创权利。

导入后主动选择，检查比例、常用动作、透明区域点击、头像、小尺寸可读性和减少动态效果。
格式检查不等于 Three.js/GPU 加载成功或原生视觉验收；加载失败可回退内置外观。
验证器不会修复穿模、错误权重、法线或动画位移。发布前应完成实际 App 预览。

迭代时提升版本并输出新文件；旧版并存，可主动切回。移除正在使用的版本前先切换外观。
二创须保留原授权与署名、说明修改并使用自己的包 ID；不要冒充原作者更新。
发行时附版本、作者、许可证和预览；创作者 CI 应锁定已验收的应用提交来构建验证工具。

## 内置成品合同 v1 / v2 / v3

[resources/character-pack.json](../resources/character-pack.json) 记录成品版本、默认角色/变体、
逐文件 SHA-256 和许可证。`character.id` 与 `variant.id` 分离，每个变体交付完整 GLB。
当前模型与品牌成品由清单决定，不依赖独立纹理下载。发布后的包版本及变体版本标识不可用
不同字节覆盖；修订需新版本，回滚使用已审核的旧成品与清单。

| 合同 | 内置交付能力 |
| --- | --- |
| v1 | 完整角色 GLB、回退模型、道具与 PNG/应用品牌成品；模型能力为 `body-v1`、`face-v1`、`hands-v1`、`view-v1`、`drag-run-v1` |
| v2 | 在 v1 上增加 `branding.animatedAvatar`，固定路径 `frontend/assets/caelis-avatar-v1.svg` |
| v3 | 在相同基础上为变体增加可选 `portrait`（`version: 1`），交付 PNG 中性头像及静态 WebP 帧图集 |

内置 GLB 使用 Y 向上、正面 +Z、脚底原点和五个原地 clips：
`idle / working / attention / nod / celebrate`。脸部形变、手指、近身手势、视角修正和拖动跑步
依赖 [角色渲染器](../frontend/src/character/pet.ts) 消费的骨骼和 metadata，不是通用 GLB 自动能力。
修改这些语义需同时审查公开运行时和资产合同。纸飞机是独立可选道具，不是所有角色的共同能力。

v2 SVG 使用 `layered-2d-v1` 的 128×128 画布：`head` 绕 (64,90) 转动，
`eye-left` / `eye-right` 绕 (43,83) / (86,83) 闭眼，`look-left` / `look-right` 接收局部视线偏移。
五个分组标记必须唯一。仅允许受限的路径、椭圆、分组和渐变；禁止脚本、样式、事件、链接、
外部资源、实体和内置动画，大小 ≤32 KiB。仅校验后的本地成品可内联到聊天 DOM，动画由宿主调度。
v1 仍使用 PNG；Dock 和应用图标保持各自品牌成品。

v3 `portrait.sourceSHA256` 必须匹配变体 GLB，头像资源的许可证也必须与模型一致。
`poster` 为中性头肩 PNG，`clips` 将动作名映射到静态 WebP 图集，路径位于
`frontend/public/portraits/<versioned-variant>/`。图集按行排列，以 `frameSize / columns / frameCount / fps`
描述：帧边长 64–256、列数 1–16、帧数 1–240、fps 1–30，帧数须整除列数，
每张解码预算 ≤8 MiB，最多 32 个动作；图像尺寸必须与清单一致。
具体图集参数由成品清单确定；播放器圆形裁切，起止使用相同中性姿态。
`companion` 是必需回退，当前状态还映射 `think / scan / focus / listen / waiting / delight / dreaming`；
缺失动作回退 `companion`。新增工作语义需同步修改公开状态映射，社区导入仍仅支持 PNG。

### 交付验证与宿主责任

[script/asset-pack.mjs](../script/asset-pack.mjs) 接收清单和列明的成品，拒绝未知合同、任意目标路径、
软链接、遗漏文件、错误哈希、外部 GLB 引用和不允许的制作 metadata。内置清单最多 64 个文件，
单文件 ≤16 MiB、总量 ≤64 MiB；SVG 和头像另有上述限制。公开交付目录不能包含整个制作仓库。

```sh
node script/asset-pack.mjs verify /absolute/path/to/finished-release --strict
node script/asset-pack.mjs import /absolute/path/to/finished-release
make check
make smoke
make build
```

交付只更新成品及清单，经公开验证和人工外观审查后纳入应用；制作侧不能借资产更新修改应用
源码或工作流。来源、署名和授权随成品保留；私有制作过程与凭据不进入公开交付。
上述检查不证明原生 GUI 或平台发行验收，相关边界见 [development.md](development.md) 和 [release.md](release.md)。

宿主负责状态到动作的映射、AnimationMixer、动作中断/恢复、隐藏与减少动态效果、动态点击范围、
窗口边界、气泡位置和 GPU 资源释放。idle/working 来自宿主事实，不运行空闲 LLM 循环。
审批提醒保持克制；成功、失败和授权以消息及原生事件为准，动画不授予权限、不改任务状态或触发重试。
内置姿态层的持物校准只调整道具挂点和呈现，不提供任意物体抓取或碰撞求解，也不改变道具生命周期。

本地包的解析、验证、安装、目录和资源读取由 [internal/contentpack](../internal/contentpack/manifest.go)
负责；[cmd/content-pack](../cmd/content-pack/main.go) 与 App 共用这一边界。
[原生外观入口](../internal/desktop/content.go) 使用原生文件选择，
[外观状态](../frontend/src/appearance.ts) 与后端对话和审批分离。
新增格式或表现能力必须同时实现合同、原生验证、渲染、安全回退和正反例，才能开放给作者。
