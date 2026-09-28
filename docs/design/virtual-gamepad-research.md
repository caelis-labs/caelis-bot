# 系统虚拟手柄可行性调研

2026-09-28。状态：**可选方向，未安装驱动、创建虚拟设备或验证游戏**。
最新 [POC MVP](bot-control-delivery-plan.md)将模型语义动作和桌面任务联动放在主线，系统手柄整体后置。
本页保留可行性资料，不再要求模型使用物理按钮/轴，也不承诺角色语义动作可直接通用于任意游戏。
将来如独立立项，可先验证 Windows，macOS 保留独立路径。实体手柄输入与虚拟设备输出仍是不同适配器。

## 1. “真实虚拟手柄”意味着什么

宿主创建 OS 可枚举的虚拟设备，报告布局/状态并处理连接、断开与可选反馈；
游戏通过自己支持的 HID、XInput、GameInput、SDL 或 Apple Game Controller 路径读取它。
这不是给游戏窗口发送普通键盘事件，也不是只在 Bot WebView 里伪造 navigator.getGamepads()。

[W3C Gamepad 标准布局](https://www.w3.org/TR/2025/WD-gamepad-20250710/#remapping)
适合参考按钮位置和归一化值，不是设备驱动的握手 API。
协议使用稳定归一化输入；adapter 将其转换为系统报告。型号、VID/PID、报告描述符和安装细节不进入模型工具。
键盘/文字/鼠标保持各自类型，不为了统一而假装所有电脑行为都是手柄按钮。

默认路由到 Bot 内部，不注册或保持一个会响应输入的系统设备。
显式开启已授权游戏控制后，先归零旧接收端，再连接系统接收端并更新绑定代次。
系统手柄通常是系统范围设备，不能承诺只有指定游戏能读取；指定游戏只是产品租约的目标与前台检查依据。
若需要强隔离，要用独立会话/环境或经过验证的专门机制，不能由 endpointRef 虚构隔离。

## 2. 可复用候选与限制

| 候选 | 适合复用 | 限制与判断 |
| --- | --- | --- |
| HIDMaestro | Windows 虚拟游戏手柄 SDK/驱动，MIT 授权文本 | 后续独立候选，届时再评估成熟度；UMDF2、C++/C#，需管理员安装/信任证书，尚未在 Bot 测试 |
| WinUHid | Windows 通用虚拟 HID 底座，MIT | 更低层，需要自行处理描述符、报告与游戏兼容；用于第二候选，不等于完整 XInput 产品 |
| libvirtualhid | C++ 跨平台 API、设备生命周期与归一化状态设计参考 | 库为 MIT，但驱动/broker 为 LB-SAL 且另需授权，不纳入默认开源依赖 |
| ViGEmBus / vgamepad | Windows 既有 Xbox/DS4 兼容实验 | ViGEm 已退休，vgamepad Windows 依赖它；不作新产品长期默认底座 |
| Apple CoreHID / IOHIDUserDevice | macOS 原生虚拟 HID 探索 | entitlement、签名及具体消费者支持都需核验；可创建 HID 不保证游戏识别 |

来源：[HIDMaestro](https://github.com/hifihedgehog/HIDMaestro)、
[其 LICENSE](https://github.com/hifihedgehog/HIDMaestro/blob/34e5cd1890eed8d6d2ce8c0477ce19629665b94b/LICENSE)、
[WinUHid](https://github.com/cgutman/WinUHid)、
[libvirtualhid 许可证分层](https://github.com/LizardByte/libvirtualhid/blob/2db2cc2d7f428a53a480985fed041d77dc121f51/LICENSE.md)、
[ViGEm 退休说明](https://docs.nefarius.at/projects/ViGEm/End-of-Life/)、
[vgamepad 的依赖](https://github.com/yannbouteiller/vgamepad)。

HIDMaestro 当前 v1.9.0 release 记录修复了此前版本可能阻止 Windows 启动的缺陷。
这是明确的版本选择与安装恢复验收项；不能依据“用户态驱动”营销描述推断不存在系统影响。
参见 [发布记录](https://github.com/hifihedgehog/HIDMaestro/releases/tag/v1.9.0)。
PoC 应锁定完整版本和依赖，先在可恢复 Windows 测试环境验证；本轮没有安装任何候选。

libvirtualhid 的旧介绍曾将 macOS gamepad 写为未来计划，当前固定源码已描述带 entitlement 的 root broker，
并要求 Windows/macOS 游戏手柄授权。选择以实际消费提交及其许可证为准，不沿用旧博客结论。
其 [macOS 文档](https://github.com/LizardByte/libvirtualhid/blob/2db2cc2d7f428a53a480985fed041d77dc121f51/docs/macos-gamepad.md)
报告 Steam 测试器验证，但也要求逐个目标消费者验证；不是 Caelis Bot 的实测结论。

## 3. macOS 的关键不确定性

Apple [HIDVirtualDevice](https://developer.apple.com/documentation/corehid/hidvirtualdevice)
提供虚拟 HID 服务；相关 [entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com.apple.developer.hid.virtual.device)
与开发者签名不是可忽略的产品发行前提。
Apple 工程师在 [2026 年官方论坛答复](https://developer.apple.com/forums/thread/812774) 中明确表示，
模拟设备可能可行，但 Game Controller 对虚拟 HID 的接受不能保证，还存在避免回环的过滤。

因此 macOS 实验至少区分：OS 枚举、Steam/SDL 能读、Apple Game Controller 能读、目标游戏能读。
普通键鼠可用、浏览器测试器可用或 hidutil 可见，都不足以证明通用游戏支持。
不承诺所有 macOS 版本，也不以降低系统保护作为发行方案。系统手柄暂不可用时，内部 Bot 控制照常工作。

## 4. 将来如何与 Bot 宿主对接

[实验语义协议](bot-action-protocol-v1.md)面向虚拟角色的 move/point 等动作，不与游戏手柄输入强行同构。
未来设备适配可以复用来源、时间预算、停止和回执机制；具体游戏输入协议与映射届时另行验证。
不能为这个可选方向提前改变本次 POC 的模型接口。

OS adapter 负责按固定频率提交/保持状态，模型只提交短时间片和重新观察。
设备反馈只代表设备/消费者事实：震动不是游戏成功回执。图像观察仍走 Computer Use 的受控媒体链路。
模型延迟决定可行游戏类别；首轮选择暂停/回合或低时效场景。快速实时游戏需要测感知—决策—输入延迟，
不能由手柄设备创建成功推断模型已经有足够的游戏控制能力，也不自动引入第二个 Agent 循环。

控制通道需处理完整中立帧、超时、进程退出、失焦、崩溃和用户停止。
宿主与驱动间应有独立 watchdog；不能依赖已经崩溃的 Bot 再发送 release。
停止与断连先释放，再销毁设备；切换不能残留按住状态，也不能把刚才的 Bot 按键重放到游戏。
系统设备可被其他消费者读取的事实必须在产品授权范围和测试中体现。

## 5. 有限 PoC 与判定

以下仅在未来系统手柄独立立项时适用，不属于当前角色与 Computer Use MVP 的验收清单。

1. 无系统驱动：相同输入驱动两个不同角色，工具 schema 和模型指导不变，输入归零与中断确定。
2. Windows 虚拟手柄：安装/卸载/重启后状态、XInput 与 HID/GameInput 测试器、目标游戏各自记录。
3. macOS：先核验自有团队 entitlement/签名可行性，再做枚举、Steam/SDL、Apple Framework 与目标游戏矩阵。
4. 检验 Bot 默认模式无 OS 输入；外部模式失焦、断连、崩溃、用户停止及路线切换后无粘键。
5. 测截图到模型到输入的总延迟、短序列准确率、停止响应和设备回收；记录失败，不以演示一次代替兼容。
6. 比较维护状态、全部组件许可、发行安装成本与实际识别率，再定生产依赖；候选均不满足才考虑窄范围自研。

系统虚拟手柄不是 Caelis Core 的专用新工具，应由 Bot 的可插拔 OS adapter 承担。
Core 仍只需要通用工具调用、取消、类型化多模态结果与受控资源，
该需求见 [Caelis #81](https://github.com/caelis-labs/caelis/issues/81)，已由 [PR #82](https://github.com/caelis-labs/caelis/pull/82)
实现候选承接并进入独立 Review；最终 Bot 集成另行验收，系统手柄不成为本次 POC 前置条件。
