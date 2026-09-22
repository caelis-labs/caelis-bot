# 资料与决策来源

整理日期：2026-09-19。资料分为用户已确认的产品决策、先前 Chat 创意、项目观察及
官方技术依据。创意与官方示例不是已实现能力；新的用户决策优先于旧讨论。

## 已确认的决策

| 主题 | 采用的方向 |
| --- | --- |
| 名称与仓库 | 对外 Caelis Bot；独立 `caelis-labs/caelis-bot` 公开仓库 |
| 默认形态 | 状态栏常驻、可调整比例的桌面宠物、按需展开的轻量入口 |
| 用户体验 | 面向非技术用户；对话、附件、审批、按需连接工具；不暴露 Session |
| 后端 | 先抽象内部契约，第一版包装 Codex App Server 原生协议；Caelis 后续适配 |
| 角色技术 | Blender → GLB → Three.js；Wails/Go 宿主、React/TypeScript UI |
| 开发顺序 | 前期工具链/火柴人真实工作已验证；当前可用性收尾与基础 3D 表现相邻推进，再验证有限空间场景；创作工具后置 |

旧讨论里的 `livebot`、Live2D 技术建议及前台 App 外壳不再作为当前产品决策。
“轻量入口”约束界面，不意味着仅能执行聊天或当前 Caelis Bot Mode 的有限能力。

## 桌面智能体深度研究与路线修订（2026-09-20）

来源：[桌面智能体深度研究](chatgpt-conversation://6aaef68a-e9c8-83e8-a5c6-08f3f8e27d4e)。
已取回报告并阅读完整内容。它是研究参考，不是实现指令、已验证技术能力或市场结论。
用户随后大体同意路线讨论，并明确要求早期确定框架/竞争方向，解决当前动作弱、
3D 优势不可见的问题。本节保留摘要；不复制报告全文或将截图中的参考角色作为新资产。

采用：真实任务事实与角色表达分层；Agent 高层意图＋本地行为调度；桌面数据与渲染场景
分离；空间关联需要可信来源；第二种不同身体验证角色解耦；注意力、打扰和长期使用验证。
不直接采用：仅聊天/状态动画就“不值得做”的绝对判断、强制 Coding 用户定位、
Caelis 优先于 Codex 的接入顺序、后台 Thread 自动生成多角色，以及过早建设公共 SDK/市场。

讨论时核对的公开能力（2026-09-20 快照，不是全面竞品审计）：

- [官方 Pets 文档](https://learn.chatgpt.com/docs/pets)已有浮动输入、活动/需决定状态、定制角色等入口。
- [OpenPets 项目 README](https://github.com/OpenPetsHQ/openpets)提供插件、定时调度及角色控制接口。

这些证据说明基础桌宠能力已有覆盖，不证明其他产品无法实现空间行为或本项目已建立壁垒。
差异化需要由持续委托、3D 情境表达、可信空间关系与低打扰体验验证。
当前截图与用户反馈记录的是表现力问题；静态图片不能证明播放器停播或定位其技术原因。
最终采用的阶段顺序与验证办法见[产品发展路线](roadmap.md)，框架边界见[架构](architecture.md)。

同日后续用户修订：复杂空间理解后置，但 Desktop、Dock、ActiveWindow 应从一开始简单建模；
早期确定安静/活跃随机待机、鼠标互动、工作动作和可独立离体的纸飞机等道具。
阶段重心为开发者持续自用和有特色的真实演示，暂不急于证明普通用户长期留存。
这项修订优先于前文研究中的留存验证顺序；最小概念与动作/道具不再后推到 C。
具体设计见[交互基线](desktop-behavior.md)，六套（四静两动）是本轮采用的初始设计值，非已交付事实。

## 原始 Chat：生成Agent创意

来源：[生成Agent创意](chatgpt-conversation://6aae411f-8d34-83e8-9ca7-1bc49fe23346)。
本次准备工作已取回可读取的对话文本；原始角色附件没有随文本取回，不假定本仓库
拥有那些资产。此处保存对后续开发有用的摘要，不复制整段聊天。

- 核心创意链为 Character DNA → 头像/全身角色 → 骨架与模型 → 行为 → Agent。
- 身份参考、标志性物件/识别锚点、构图参考、风格参考应分开描述，避免失去角色辨识度。
- 头像裁切适合传播，全身稿适合动画；不能直接把头像当动画母版。
- 考虑缩小到 32–64 px 时的轮廓、颜色与锚点可读性；这属于后续视觉验收目标，
  并非当前实现的固定显示尺寸。
- 角色随身物件可承担轻量提醒，例如纸飞机，但这是创意例子，不是已选定角色。
- 桌面窗口移动、模型局部动作和真实 Agent 工作状态分层，不让动作播放决定执行结果。

原 Chat 对 Live2D 等实现的建议属于当时探索；当前采用用户后来确认的 3D 路线。

## 视觉和制作来源

原始参考图、历史实验和制作调研保留在私有资产仓库；当前发行成品的来源与限制见
[成品合同](character-assets.md)及随包归属说明。公开产品不依赖相邻 Caelis 仓库的内部模块。

## 官方技术依据

以下是准备阶段使用的来源。网络文档会更新，具体方法签名应对照 `toolchain.json`
的本机版本和生成 schema，不根据最新文档直接宣称旧依赖支持某项特性。

| 来源 | 本项目采用的结论 / 查阅用途 |
| --- | --- |
| [Codex App Server](https://learn.chatgpt.com/docs/app-server) | 原生初始化、thread/turn/item、审批 server requests；本机 stdio JSONL，不添加 `jsonrpc` 版本头 |
| [Wails window options](https://v3.wails.io/features/windows/options) | 原生窗口参数；透明、焦点及 macOS 行为仍需锁定版本实测 |
| [Wails frameless](https://v3.wails.io/features/windows/frameless) | 无边框窗口基础；不据此推定局部点击穿透已实现 |
| [Wails system tray](https://v3.wails.io/features/menus/systray) | 状态栏/托盘入口与窗口生命周期设计依据 |
| [Three.js skeletal/morph 示例](https://threejs.org/examples/webgl_animation_skinning_morph.html) | GLB 骨骼、表情与 AnimationMixer 的参考；没有将示例模型当作本项目正式角色 |
| [Khronos glTF](https://www.khronos.org/gltf/) | 使用开放资产交付格式；由 Validator 检查文件结构 |
| [Blender glTF 导入导出说明](https://docs.blender.org/manual/en/latest/addons/import_export/scene_gltf2.html) | 导出能力和材质/动画限制；复杂约束、物理需烘焙或运行时处理 |
| [Blender 许可](https://www.blender.org/about/license/) | 软件许可与原创导出作品区分；第三方模型/纹理仍看各自许可 |
| [Three.js 许可](https://github.com/mrdoob/three.js/blob/dev/LICENSE) | 核对渲染库许可与分发要求 |

### Live2D 路线的历史取舍

准备阶段参考了 [SDK 许可](https://www.live2d.com/en/sdk/license/) 和
[Expandable Applications](https://www.live2d.com/en/sdk/license/expandable/) 条款。
支持用户添加/替换模型的产品需要特别核对可扩展应用授权，不能因提供预制角色或
允许手工替换文件，就假定普通 SDK 条款自动豁免。这里保留的是当时选型考虑，
不是对具体产品的法律定性；如重启 Live2D 路线，应按当时条款向授权方确认。

用户已选择 Blender → GLB，因此本项目当前不集成 Live2D。3D 路线仍需做好小尺寸
轮廓、骨架/动作、导出约束和实时渲染，技术优势不等于精细建模工作可以省略。

## 资料维护

后续新增决策更新 `product.md` / `architecture.md`，进度更新 `preparation-status.md`，
验收证据放在对应阶段文档。避免多个路线文档各自描述冲突的“当前状态”。
