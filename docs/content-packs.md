# 制作与发布 Caelis 内容包

Caelis Bot 的应用代码保持免费开源。角色、完整服装变体和头像是独立的数据内容，
不携带执行代码，不改变助手的身份、对话、Runtime 或权限。你可以用熟悉的建模和绘图工具创作，
不需要官方的私有制作工程或 API 密钥。

当前实现是本地内容包 v1：**GLB 角色/完整服装变体 + PNG 头像**。
支持在设置中导入、选择、保留多个版本、回退与移除。独立蒙皮衣服、
挂点配件、桌面装饰及第三方动态 SVG 尚未开放；不要在包内放置这些未支持字段。
内置角色的增强表情、手指、近身动作与 SVG 动画使用另一份现有成品合同，不能直接当作社区合同。

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
