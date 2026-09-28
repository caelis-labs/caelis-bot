# Bot Control / Computer Use 首片验证

2026-09-28 的原始 POC 记录（历史范围）。后续[包内 Cua 集成与 Dev Bot E2E](computer-use-integration.md)已推进，本页保留最初默认关闭实验的证据。尚未提交或发布。阶段边界见
[交付计划](bot-control-delivery-plan.md)，操作步骤见
[实验 README](../../experiments/desktop-control/README.md)。

后续依赖复核与基础 Computer Use 优先上线的顺序见[选型记录](computer-use-dependency-audit.md)。
本页保留 Cua 实验的原始验证范围，不表示已通过正式分发许可验收或已决定替换驱动。

## 当前结论

第一片应优先引入真实 Cua Driver：**组件语义、状态和动作引用为主，坐标用于空间映射，截图按需补充**。
本轮已经验证真实原生应用的可逆操作，无需截图让模型反复猜测点击位置。
它证明了驱动和 Runtime 的连接可行，尚未证明真实 LLM 能自主规划，也不是完整角色控制 MVP。

实际链路为：

```text
常驻 Bot 的工具目录
  → Go 宿主监督的私有 SDK 子进程
  → @trycua/cua-driver 0.30.2
  → 自有 AppKit 测试窗口的 Accessibility 元数据 / AXPress
  → 新观察与实际筛选状态
  → Caelis application callback content-v1
  → 合成 provider 的下一次模型请求
```

SDK 安装于独立 `experiments/desktop-control`，固定版本与 integrity；未加入主应用 npm 依赖或安装包。
此 POC 只绑定一个已知 bundle ID 和窗口标题，只允许点击夹具的 `Only incomplete` 筛选框。
不能选择其他应用、关闭窗口或操作系统菜单；不借用开发助手自身的 Computer Use 私有工具代替产品接入。

## 接口和行为

- `bot_desktop_observe {}` 返回 observation、组件名称/角色/值、动作、边界和窗口内容，无截图。
- `bot_desktop_perform {observation, steps}` 接受最多 8 项的有限序列。
  当前仅验证 click；第一次 UI 变更后立即重观察，返回已派发步骤及剩余步骤，旧引用作废。
  同一批剩余步骤不会盲目点击变化后的页面；未来角色 move/point 的本地连续执行另行实现。
- 模型接收短目标引用；原生 Cua token、PID 和窗口身份由适配器持有。
  `dispatched` 仅表示已投递，测试以新的组件值和可见任务数判断真实效果。
- 子进程走继承管道，无监听端口或全局 MCP 注册；环境不继承 Bot 凭据或模型密钥。
  取消/超时关闭该通道并报告 unknown，不自动重启或重放输入动作。
- 开关仅由宿主显式启用。Bot MCP 只增加列出的具体工具名，不整体自动批准第三方服务。
  Worker 的原生能力不变。
- 独立的 `bot_desktop_capture` 是可选图像补充，要求当前模型明确支持图片。
  其原生采集限制 macOS 14+、单显示器、256 KiB 内联图片；本轮主链路不需要该工具。

## 实际证据

| 验证 | 结果与范围 |
| --- | --- |
| 真实 Cua SDK + 原生窗口 | 10/10 次切换通过；checkbox 值与可见任务数同步变化，最终恢复起始状态 |
| 图片依赖 | 10 次驱动探针及语义工具联调均未请求截图 |
| 时延 | 本机驱动探针单次点击加读回 1366–1454 ms；仅该小型窗口的观测，非性能承诺 |
| 引用与步骤 | 重观察后拒绝旧 observation；双步骤请求只派发第一步并返回剩余步骤 |
| Caelis 回传 | 真实 Host + 合成 provider 的 B15 图片内容块、B16 真实 SDK 结构化观察/点击/恢复通过 |
| Worker 隔离 | 在常驻工具已启用后实际派生 Worker；模型请求不含桌面工具、夹具内容、专用指导或配置 |
| 子进程 | 真实进程/管道测试通过：不继承测试凭据、取消报告不确定并回收子进程、不自动重启 |
| 回执恢复 | content-v1 catalog 跨重启保存；关闭实验后仍保留原调用结果格式，未知结果不重放 |
| 全量检查 | `make check`、`make smoke`、ad-hoc `make build` 通过；原 Apple Development 身份构建也通过 |
| 原生窗口 | 已检查自有 AppKit 夹具的真实 AX 窗口、筛选框和任务数 |

联调修复了实际问题：`wire.JSONValue` 为 `any`，直接放入 `json.Marshal` 的 `[]byte`
会将内容数组再次编码成 Base64 字符串。现在保留为 `json.RawMessage`，并验证下一次 provider
请求中的真实图片内容块，而非仅判断 JSON 内有图片字符串。

公开 schema/wire 固定到 [Caelis #82](https://github.com/caelis-labs/caelis/pull/82) 合并提交
`369cd58b6d26cfd43057e0393fcab7d7c2c84cd8`。验收二进制从 PR 最终 head
`ce4258e0e8b3b461f39bc082a8e783c08221c6e2` 构建；已核对二者 Git tree 完全一致，
均为 `58f0b9180656a72622017e0e69fc504cad2e0b4e`。未改兄弟仓库。

本机日志及包含观察内容的证据留在忽略目录 `.cache/desktop-control/evidence.json`、
`.cache/desktop-poc-{check,smoke,build,development-build,cua-host,driver-tests}.log`，不提交桌面内容。

## 发现的限制

1. 原生结果的 `elementsComplete` 为 false，即使没有 degraded/truncated。
   交互元素列表和窗口树文本需要一起消费；静态任务数存在于树文本，不能把控件列表当完整页面。
   原生返回还含系统菜单；本适配器只保留夹具 AXWindow 子树。
2. Cua 坐标与 Bot 桌面逻辑点尚未校准。本轮以 element token 执行，明确标注原生几何，
   不直接拿它控制角色位置。负坐标、多显示器、混合 DPI 和窗口移动后的共享目标仍待原生验证。
3. SDK 的成功权限属于启动它的开发宿主，不等于签名 Bot 的 TCC 权限已验证。
   完整 Bot 采集探针遇到日常安装版的单实例占用，未取得新二进制的原生运行证据；未退出日常 Bot。
4. 构建初次报告 Apple Development 身份不可用，是沙箱内 Keychain 查询返回 0 个身份。
   沙箱外只读核对配置文件及有效身份指纹一致，随后原身份构建成功；无需重建证书。
5. 尚未做真实 LLM 自主决策、新语义工具的完整 Codex 原生模型路径、任意应用/网页、
   用户接管仲裁、长任务、角色位移或 Windows 原生验证。Go 跨平台编译不能替代这些门槛。

## 依赖与后续门槛

实际锁定依赖包括顶层 MIT 的 `@trycua/cua-driver@0.30.2`，原生包声明 `MIT AND MPL-2.0`，
UniFFI JS 绑定 `@ubjs/core` / `@ubjs/node@0.31.0-3` 声明 MPL-2.0。
因此不将整条依赖链称为纯 MIT 或已完成分发许可验收；正式打包前核对精确制品的许可材料。
参照 [官方 SDK](https://cua.ai/docs/reference/cua-driver/sdk-reference) 与
[进程集成](https://cua.ai/docs/concepts/choose-a-cua-driver-integration)。

下一门槛是让真实 Bot 模型基于组件观察自主完成同一可逆任务，并验证新进程权限身份；
随后校准共享目标坐标、接入现有角色的 move/point 有限序列，再让角色与应用操作使用同一目标。
完整手柄、公共动作资产 schema、虚拟 HID、广泛应用支持和 Windows 原生适配继续分片后置。
Bot 的英文 skill 已增加条件加载指导，未把这些后置能力写成当前可用功能。
