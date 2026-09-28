# Computer Use 依赖复核与可行路径

2026-09-28。范围为当前 macOS 基础 Computer Use 上线前的依赖选择与原生可行性验证。
本页保留选型时的结论；后续已按 Cua 路线接入包内驱动并完成 Dev Bot 闭环，见[当前集成状态](computer-use-integration.md)。尚未提交或发布。

## 决策边界

“完全不接受 MPL 依赖”是项目依赖政策；“使用 MPL 会迫使整个 Bot 开源”不是 MPL 的要求。
Mozilla 官方 [FAQ Q5–Q8、Q11](https://www.mozilla.org/en-US/MPL/2.0/FAQ/) 区分单纯使用、
未修改程序的分发和独立文件组成的 Larger Work。仅调用用户已安装的组件、没有提供其副本，
通常不触发分发时的源码提供义务；将它放入 Bot 的安装包仍属于分发，即使完全没有修改。

按 [MPL 2.0 §1.7、§1.10、§3.1–3.4](https://www.mozilla.org/en-US/MPL/2.0/)，
分发时需保留适用声明，告知接收者如何取得 MPL 覆盖部分的对应源码，且不能限制其 MPL 权利。
独立编写、不包含 MPL 代码的 Bot 源文件与角色资产不会仅因一起打包就改用 MPL。
把 MPL 代码复制进自有源文件，或修改覆盖文件，需要另行计入覆盖范围。

用户后续明确：Bot 本身采用 Apache-2.0；关注的是依赖不能迫使自有项目改变许可证，
在独立打包使用且履行必要义务的前提下可以接受 MPL。因而不再按“完全禁止 MPL”筛选。
Bot 自有代码继续采用 Apache-2.0，随包的 MPL 部分仍保留各自许可，不能将整个第三方制品改标为 Apache-2.0。
补齐实际发行制品的许可证、声明、匹配源码及依赖清单；无需仅为避免 MPL 强制用户另装运行时。

**推荐 Cua 作为首发主驱动，AXorcist 保留为 macOS 可行性备选。**
这是基于跨平台成本和既有接入的技术建议，尚未把任意应用操作或生产打包标记为通过。
初期不同时发行两套驱动，不在一次输入结果未知时切换驱动重试。

## Cua 0.30.2 的实际依赖

锁定 `cua-driver-rs-v0.30.2`，提交 `a2229c5b829153ec3b1828387bc72ca8f1f18704`。
Node 包、integrity 与实验安装位于 [原实验](../../experiments/desktop-control/README.md)。

```text
@trycua/cua-driver 0.30.2                 MIT
├─ @ubjs/core 0.31.0-3                  MPL-2.0
├─ @ubjs/node 0.31.0-3                  MPL-2.0
└─ @trycua/cua-driver-darwin-arm64 0.30.2  MIT AND MPL-2.0
   ├─ libcua_driver_sdk.dylib
   └─ cua_driver_node_runtime.node       MPL 派生 N-API runtime

cua-driver CLI / SDK / core
└─ cua-driver-contract
   └─ uniffi 0.31.0                     MPL-2.0
```

原生包的 `node-runtime-NOTICE.md` 明确指出 `.node` 来自
`uniffi-bindgen-react-native 0.31.0-3`，对应源码还包括匹配 Cua release 的
`scripts/build-node-runtime.mjs` 所描述的转换。只提供 Bot 源码、顶层 MIT LICENSE 或一个
不对应发行版本的仓库首页，都不能代替这部分材料的核对。

已读取固定提交的 [CLI manifest](https://github.com/trycua/cua/blob/a2229c5b829153ec3b1828387bc72ca8f1f18704/libs/cua-driver/rust/crates/cua-driver/Cargo.toml)、
[SDK manifest](https://github.com/trycua/cua/blob/a2229c5b829153ec3b1828387bc72ca8f1f18704/libs/cua-driver/rust/crates/cua-driver-sdk/Cargo.toml)、
[core manifest](https://github.com/trycua/cua/blob/a2229c5b829153ec3b1828387bc72ca8f1f18704/libs/cua-driver/rust/crates/cua-driver-core/Cargo.toml) 和
[contract manifest](https://github.com/trycua/cua/blob/a2229c5b829153ec3b1828387bc72ca8f1f18704/libs/cua-driver/rust/crates/cua-driver-contract/Cargo.toml)。
`uniffi` 在 contract 和 SDK 中不是可选依赖；core 也依赖 contract。
关闭 default features、换成 CLI/MCP、Python 或直接调用现有 Rust core，不能使该版本的声明依赖链脱离 MPL。
移除 UniFFI 将成为需要维护的源码改造，不属于本轮已验证的复用路径。
这不是逐符号二进制归属鉴定，也未将整个 Cua 单仓库的可选模型/服务都算进 Bot 所用制品。

## 不含 MPL 的 macOS 候选：AXorcist 库

选择 [AXorcist v0.2.0](https://github.com/openclaw/AXorcist/tree/c3838bfa47358202331c4cd4b71a783893825fd9)，
提交 `c3838bfa47358202331c4cd4b71a783893825fd9`。所选库使用系统 Accessibility，
最低 macOS 14，Swift tools 6.2；本机用 Swift 6.4 成功构建。

| 实际解析的包 | 固定版本 / 提交 | 许可 | 进入探针链接 |
| --- | --- | --- | --- |
| AXorcist | v0.2.0 / `c3838bfa47358202331c4cd4b71a783893825fd9` | MIT | 是 |
| apple/swift-log | 1.15.1 / `9c6fb14227f55d8f711ce3847dc2f419fb0ecacb` | Apache-2.0，含 NOTICE | 是，Logging |
| steipete/Commander | 0.3.0 / `3f5a3cb1f19ce7ca79fdc2111a09dfb3da200c1c` | MIT | 否；上游 CLI 依赖，仍被解析 |

探针链接清单仅有自身、AXorcist 和 Logging 的对象文件；`otool -L` 为 Apple 系统框架/Swift 库。
所选并实际解析的第三方包未发现 MPL；这不代表所有未来版本、可选包或最终应用制品已完成许可验收。
版本与锁文件见 [原生探针](../../experiments/desktop-control-axorcist/README.md)。

已验证的链路：组件角色/标识/值/边界 → AXPress → 新 AXValue 与列表计数。
自有原生窗口连续 **10/10** 次可逆切换通过，**0 张截图**，最后恢复起始状态；
该次测量每次 187–251 ms，不同于 Cua 探针的完整观察路径，不能据此宣称性能优劣。

发现 `.value()` 的 Any 返回路径在测试工具链下出现装箱空值，探针改用 typed
`Attribute<Int>` / `Attribute<String>` 读取，无上游补丁。
尚未验证真实模型自主规划、Bot 签名进程的 TCC、一般应用覆盖、键盘/滚动/拖拽及 Windows。
这个库降低 macOS 观察/动作实现成本，不直接替代 Cua 的跨平台服务、目标引用和序列宿主。

Peekaboo 也值得保留为候选：当前根 SwiftPM 的 AutomationKit 本身可独立依赖 AXorcist，
不应把整个 CLI/Agent 栈都算作库的必需依赖；本轮未构建或审计其完整传递依赖。
Windows 继续预留独立 UI Automation adapter；`uiautomation-rs` 的 Apache-2.0 manifest
仅作为候选线索，未在 Windows 完成构建或传递许可验收。

## 成熟度、实际使用方与平台选择

截至 2026-09-28，AXorcist 仓库未归档；最新正式版为 2026-09-23 发布的 v0.2.0，
包含 macOS arm64、x86_64 与 universal 制品。官方明确其底层为仅存在于 macOS 的 Accessibility，
CI 和 release 都限定 macOS；universal 指两种 Mac CPU 架构，不是 Windows 支持。
见 [平台说明](https://github.com/openclaw/AXorcist#readme) 和
[v0.2.0 release](https://github.com/openclaw/AXorcist/releases/tag/v0.2.0)。

已核对 OpenClaw 提交 `ff8c752d852f5d01da5a8a5e6abb7546c1328e83`：

```text
OpenClaw macOS app
  → PeekabooBridge / PeekabooAutomationKit 4.6.0
  → AXorcist 0.1.11
```

[OpenClaw manifest](https://github.com/openclaw/openclaw/blob/ff8c752d852f5d01da5a8a5e6abb7546c1328e83/apps/macos/Package.swift)
与 [锁文件](https://github.com/openclaw/openclaw/blob/ff8c752d852f5d01da5a8a5e6abb7546c1328e83/apps/macos/Package.resolved)
证实这是实际传递依赖，而非仅同属一个 GitHub 组织。
其 [官方桥接文档](https://docs.openclaw.ai/platforms/mac/peekaboo)还区分应用内 Peekaboo 服务、
本地 Bridge、Codex 插件和独立 Cua MCP 路径；不能据此说 OpenClaw 全平台都以 AXorcist 为底层。
我们的探针使用 v0.2.0，不能把 OpenClaw 对 0.1.11 的采用等同于替我们验收该版本。

成熟度判断：AXorcist 有真实消费者、持续维护和发布流程，适合复用 macOS 原语；
仍需固定版本和验证实际应用。[变更记录](https://github.com/openclaw/AXorcist/blob/v0.2.0/CHANGELOG.md)
近期仍在修复焦点检查、批处理丢命令、AX 数值转换、观察取消等关键行为。
原生 batch 的运行期错误语义也不等于 Bot 所需的逐步结果与停止条件；不能直接把批处理接口交给模型代替宿主编排。

Cua 的 [0.30.2 发行](https://github.com/trycua/cua/releases/tag/cua-driver-rs-v0.30.2)
已有 macOS 和 Windows x86_64/arm64 二进制及 SDK 包。
[平台矩阵](https://cua.ai/docs/reference/cua-driver/platform-support)记录 Windows UIA/Win32 与 macOS AX 等原生实现，
以及 Electron、Tauri、WPF/WinUI/WebView2 或 AppKit/SwiftUI/WKWebView 的上游验证。
这是上游能力证据，Bot 自身的 Windows 行为仍需独立验收。

| 取舍 | Cua | AXorcist |
| --- | --- | --- |
| 当前 macOS 核心链路 | 已有驱动探针与 Bot/Caelis 合成 provider 接入 | 原生库探针通过，产品适配尚未做 |
| Windows 路径 | 已有原生实现和发行制品，复用统一驱动接口 | 无 Windows 实现，需另接 UIA 库并维护两端语义 |
| 集成复杂度 | 当前 Node SDK 加原生 Rust/绑定，包体、签名、权限链要验证 | Swift 库与依赖较少，macOS 集成更直接 |
| 桌面覆盖 | 跨平台观察、输入、可选截图及部分浏览器路由 | macOS AX 查询、事件与输入；更完整产品行为仍由宿主组合 |
| 许可 | 混合 MIT/MPL，需对应源码与声明 | 所选库链为 MIT/Apache-2.0，义务较简单 |

选择 Cua 的理由是近期 Windows 计划与减少双平台适配维护，不是已证明它在每种 macOS 应用上更稳定。
当前两次探针路径和观察范围不同，不用 187–251 ms 与 1366–1454 ms 直接排名。
Cua 的后台输入、浏览器精确绑定、坐标空间等仍有[公开限制](https://cua.ai/docs/reference/cua-driver/limits)，
需要按操作能力呈现，而不是承诺“跨平台即全部等价”。

接入保持 Bot 自有 observe/perform 小工具面，由 Bot 原生宿主管理私有驱动进程与顺序执行；
不将完整第三方 MCP 目录注入 Worker，不加入 Cua 的 Agent/VM/云产品。
[官方嵌入建议](https://cua.ai/docs/concepts/choose-a-cua-driver-integration)支持 SDK 与宿主管理的私有进程路径。
正式版必须在实际签名 Bot 下确认 TCC 归属、停止回收与包体成本，不能依赖开发终端权限或用户机器恰好装有 Node。
基础版先验证语义点击、输入、滚动及重新观察；角色控制继续使用 Bot 侧协议，后续共用目标而不耦合具体驱动。

## 上线顺序

先交付基础 Computer Use：常驻 Bot 的结构化观察、短操作序列、真实效果反馈和停止。
截图按需补充，Worker 保持原生范围；角色 move/point、手柄和公共动作资产不再阻塞该能力。

1. 驱动与打包 PR：依据依赖政策选择路径，固定依赖并加入实际发行包的声明/源码取得说明；验证独立签名进程权限。
2. Bot 闭环 PR：真实模型从组件观察自主选目标，执行点击及必要的输入/滚动，重新观察确认效果。
   验证旧目标失效、用户停止、权限拒绝和结果不确定；未知输入不自动重放。
3. 正式启用 PR：完整 Bot 与实际工作应用验收、权限引导、跨 Runtime 的 Bot/Worker 隔离、发布制品检查。
   角色联动及 Windows 原生实现继续独立交付，不把夹具成功宣传为任意应用支持。

本轮只增加依赖探针与选型记录，未新增正式 Bot 能力或改变模型工作流，因此无需新增 Bot skill 指导。
产品接入时再按最终可用工具与恢复行为更新条件加载的英文 skill。

本机证据位于忽略目录 `.cache/computer-use-license-audit/` 和 `.cache/axorcist-probe/`；
不将上游 checkout、构建产物或真实桌面内容提交到公共仓库。

## 后续实际打包边界

当前包内 Node 24.21.0、Cua SDK/native 0.30.2、@ubjs/core、@ubjs/node 及
@ubjs/node-darwin 平台包 0.31.0-3 均显式固定；Cua 对应 Cargo.lock 的 UniFFI 为 0.31.0。
声明、MPL 正文和精确对应源码取得说明位于 `resources/computer-use/THIRD-PARTY-NOTICES.md`，
随实际应用包复制；自有源码仍为 Apache-2.0。额外的 Cua perception 扩展及其 AGPL
OmniParser 模型不在所选 SDK 包内，也不安装、调用或随包分发；此项与允许 MPL 无关。
参见[固定版本上游扩展边界](https://github.com/trycua/cua/blob/a2229c5b829153ec3b1828387bc72ca8f1f18704/libs/cua-driver/docs/perception-third-party-notices.md)。
后续若引入扩展、修改上游文件或升级版本，重新审核实际制品，不能沿用本次依赖结论。
