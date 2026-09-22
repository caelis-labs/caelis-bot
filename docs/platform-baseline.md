# 跨平台实现基线

修订日期：2026-09-19。当前只实施 macOS，完整发行后才开始 Windows；Linux 暂不列入计划。
接口保持可扩展，不为后置平台提前建设原生驱动、打包或专属产品流程。
这是实现与验收基线，不是已发布平台列表。
保持 Wails/Go + React/TypeScript + Three.js、Blender → GLB；版本继续以
`go.mod`、`package-lock.json` 和 `toolchain.json` 为准，不因在线文档更新自动升级。

## 平台范围

| 平台 | 实现基线 | 当前证据与限制 |
| --- | --- | --- |
| macOS | 唯一当前实现与发行目标，arm64 优先；部署目标目前 12.0，WKWebView/AppKit | 在 macOS 27 / M4 单屏 Retina 实测；12.0 最低版本和 Intel 原生宿主尚未验收，不能由 deployment target 推断兼容 |
| Windows | 仅保留接口扩展与共享核心编译；macOS 完整发行后再确定系统支持范围并实施 | 原生驱动、托盘、安装包、系统输入与虚拟桌面策略未实现；当前入口明确返回不支持错误 |

Linux 不进入里程碑、常规编译矩阵或发行承诺。此前 X11/Wayland 调研只是风险资料，
不构成待实施计划；如将来重新立项，再做独立平台评估。
“macOS 完整发行”要求真实后端闭环、普通用户安装/连接/恢复、签名公证与发布验收，
不是当前 P1 本地 ad-hoc 包能够启动。
早期 pre-release 例外允许 ad-hoc 包与用户手动信任，Developer ID/公证暂时后置。

macOS Spaces 指同一显示器上的虚拟桌面及原生全屏应用；它不是多显示器迁移。
当前产品要求宠物随 Space 保持可见，不抢焦点；主动隐藏后仍保持隐藏。
外接显示器自动迁移、混合 DPI、拔屏恢复仍需独立实机验收。

Wails 的多平台支持不等于本产品的原生能力已经移植。上游
[平台依赖](https://v3.wails.io/getting-started/installation/) 说明了 WebView2、AppKit
与 GTK/WebKit 的不同；本仓库同时核对了已下载的 **v3.0.0-beta.6** 平台源码。
未来 Windows DPI 评估参考 [Microsoft 的 Per-Monitor V2 指引](https://learn.microsoft.com/en-us/windows/win32/hidpi/high-dpi-desktop-application-development-on-windows)。

## 所有权与平台边界

- `internal/desktop` 的状态、比例校验、附件暂存、事件顺序不导入 Wails/Cocoa/Win32/GTK。
  `driver` 是原生桌面行为边界。平台适配器负责 OS 线程、坐标转换、窗口、菜单、焦点、
  原生拖动、命中区域、显示器/桌面变化和资源回收。
- `runtime_darwin.go` / `native_darwin.*` 只在 `darwin && cgo` 构建。
  `runtime_unsupported.go` 让其他构建明确失败于启动，不创建替代主窗口、不写偏好，
  不用可编译的空壳冒充移植完成。新增平台驱动时同步收窄这个 build tag。
- 前端只管理显示和临时输入，调用共同的宿主命令；不判断 GOOS，不导入系统 API。
  真实原生能力尚未可用时，宿主拒绝启动；未来部分能力降级要有明确类型和产品行为，
  不用 `success`、无效按钮或整块矩形拦截来掩盖缺失。
- P2 的进程、文件字节、凭据与后端协议仍由 Go 服务/adapter 持有；进程退出树、
  取消和未知结果恢复在各 OS 实测。Windows 原生路径/可执行文件与 WSL 是不同环境，
  不能混用。POSIX signal 不能直接充当 Windows 进程生命周期契约。
  P2.1 的 `process_darwin.go` 实现本机运行时发现和子进程回收，兼容性由标准握手与消费接口检查；`process_unsupported.go`
  在其他平台明确拒绝启动；协议/client 核心不导入桌面宿主。P2 已实现原生中断及 backgroundTerminals/clean，
  macOS 合成活跃/后台工具已实测回收；逃逸的独立进程、崩溃和跨平台清理不由此保证。

## 几何、输入与持久化契约

1. 共享 `Rect` / `Placement` 使用逻辑单位，主显示器左下为原点，Y 向上，允许负坐标。
   这沿用已有数据；AppKit 直接映射，未来 Windows 驱动负责转换。
   混合 DPI 必须按所属显示器转换局部偏移，不能把全局坐标统一乘一个比例。
   renderer backing pixels 与设备 DPI 不写入 `Placement.Scale`，该值仅为用户的角色比例。
2. 现有 `placement.json` 是本机 macOS 的未版本化几何，不跨 OS 同步或直接复用。
   在新驱动落地前引入带版本、平台坐标约定和显示器身份的记录，并保留旧 macOS 数据迁移测试。
   不为一个尚无驱动的平台提前迁移现有用户偏好。
3. 跨 Space 跟随、物理显示器回收和用户显式拖动是三种事件。Space 切换不得改保存位置、
   抢焦点、打开输入框或唤回被隐藏的宠物。物理显示器移除才重新夹取可用区域。
4. 所有平台必须支持非激活角色、原生拖动、透明区向其他应用透传、显式打开才输入，
   外部点击收起且原点击到达目标。不能只用 Three.js raycast 或定向自动点击证明穿透。
   macOS AppKit 菜单内滑杆是当前实现；未来 Windows 可使用当地等价原生菜单/弹出层，
   但调整必须留在发起位置，不跳回角色。
5. 偏好写入保留同目录临时文件、sync、替换和错误回报。Go `os.Rename` 在非 Unix 系统
   [不承诺原子性](https://pkg.go.dev/os#Rename)，故不能把 macOS 的原子写入结论外推到 Windows。
   POSIX 0600/0700 与 Windows 用户目录 ACL 分开验证；Windows native host 发布前需要
   明确原子替换/失败恢复与访问权限实现。目前共享测试覆盖重复保存后的正确读回。
6. 文件选择器必须返回该平台原生路径；路径只由 Go 持有。P2 验证 Unicode、空格、
   符号链接、大小写和 Windows 长路径/UNC，不把清理字符串视作完整的文件身份或权限验证。

## 前端与资源基线

当前前端在 WKWebView 上验收；未来 Windows 实施时另验 WebView2：WebGL2/GLB、透明合成、
字体/滚动、有限多行输入、Enter/Shift+Enter、中文候选、焦点/原生文件 sheet。
TypeScript/Vite 编译及 Node 资产 smoke 不证明WebView2 的实际表现。

GLB 与 Caelis 自有 logo 可共享；当前 `frontend/public/icons` 由 SF Symbols 导出，
不能把 macOS 资产的可用性直接视为跨平台分发许可。其他平台原生包启用前选择许可明确的
共享图标或平台资源并保留来源。Blender 只属于开发/导出工具，不作为用户运行时依赖。

## 自动检查与发布门槛

`make check` 现在包含 `script/check-portability.mjs`，也可单独运行：

```sh
# 任意开发平台（先安装锁定版本依赖；npm run build 生成 main.go 嵌入资源）
npm ci
npm run build
npm run check:portability
# macOS 的快捷入口
make check-portability
```

检查在当前宿主执行 `CGO_ENABLED=0 go test ./...`，并为 darwin/windows 的
amd64/arm64 编译桌面与 Codex 协议核心测试程序及“不支持宿主”入口，同时禁止 Wails/cgo 依赖泄入核心。
输出放 `.cache/portability`。**交叉编译不执行外平台测试，也不链接或验收原生 GUI。**
macOS 的 cgo 测试、`make build` 和原生 Run 仍独立执行，防止只验证了错误入口。
构建/运行/签名的 Bash 脚本仍限定 macOS；新的 Node 检查入口不依赖 Bash/Homebrew。

当前四目标检查用于防止接口绑死 macOS，不代表开始 Windows 实现。Windows 本机执行
与原生 GUI 未验证。macOS 完整发行后新增 Windows native host 时，合并门槛为：

- 当地 runner 执行共享测试、前端构建和原生编译；最低系统版本实际启动；记录 WebView 版本。
- 实机完成托盘/隐藏唤回/退出、点击拖动/缩放、位置恢复、外部焦点/穿透、IME/文件选择、
  虚拟桌面/全屏与多显示器 DPI/热插拔；不支持的行为有明确产品降级，不静默改变能力。
- 使用当地原生 Codex 完成 initialize 并记录测试发行号；另验真实提交/审批/取消/退出，
  不能以当前 macOS initialize smoke 宣称另外平台的 App Server 路径成立。
- 安装/卸载、签名、权限与资源许可、空闲成本完成当地检查，再更新支持矩阵。

目前没有已运行的跨 OS CI；上述 runner 门槛是后续原生平台启用条件，而非已完成证据。
