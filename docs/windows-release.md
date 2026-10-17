# Windows preview 发布与更新

Windows 与 macOS 共用仓库、产品基础版本和公共合同，按平台独立验收和发版。
Windows 使用 `vX.Y.Z-windows.preview.N`，指向 main 已包含的确切提交；不要求每个 Mac
release 同时产出 Windows 包。不建立长期 Windows 产品分支，平台代码用适配器隔离。

首批只发布 **unsigned NSIS Setup.exe + 免安装 ZIP**。MSIX 的
`script/package-windows.ps1` 保留为开发 fixture，拒绝发行 tag，不进入发布任务。
兼容下限是 Windows 10 22H2 x64（19045）及 WebView2；目前实际验收系统为 Windows 11。
Server 2022+ 有交互式桌面的环境单独验收；没有承诺 Server Core 或 ARM64。

## 本地构建

在 PowerShell 7、仓库锁定的 Go/Node 下：

```powershell
./script/check-windows.ps1
./script/smoke-windows.ps1 -CodexBinary 'C:/absolute/path/to/codex.exe'
./script/build-windows.ps1 -Channel preview -PreviewRevision 2 -OutputDirectory dist/windows-preview-2
./script/package-nsis-windows.ps1 -SkipBuild -Channel preview -ApplicationDirectory dist/windows-preview-2
./script/package-zip-windows.ps1 -SkipBuild -Channel preview -ApplicationDirectory dist/windows-preview-2
```

未设置 `BOT_RELEASE_TAG` 的包带 `-local`，不会进入更新 feed。preview 构建目录必须是新的，
避免旧文件混入；开发启动使用 `script/build_and_run-windows.ps1 -DataDirectory <隔离目录>`。
新代码、编译、原生组件测试、完整 GUI 和精确包生命周期是不同证据，见[实施状态](windows-adaptation-plan.md)。

原生应用更新边界可用两份已校验的 Caelis 发布二进制单独验证：

```powershell
$env:CAELIS_BOT_TEST_PREVIOUS_BINARY='C:/absolute/path/to/v0.72.1/caelis.exe'
$env:CAELIS_BOT_TEST_BINARY='C:/absolute/path/to/v0.72.2/caelis.exe'
go test ./internal/app -run '^TestWindowsCaelisApplicationUpdatePreservesActiveWork$' -count=1 -timeout 130s -v
```

该测试使用临时 Store/用户目录和本机合成模型，按原生一次性审批创建 Worker，验证活跃请求更新交接。
环境变量只用于显式测试，不提供实际账号；测试成功不替代 GUI、安装包或真实模型桌面演示。

## GitHub CI

`.github/workflows/windows-preview.yml` 只接受从 main 手动运行，分两次：

1. 创建或选择已落在 main 上的独立 Windows tag，运行 `mode=build` 和该 tag。
   CI 校验源版本，运行 Windows 检查，生成两种包及 `.sha256/.build.json`。
   构建 job 无发布私钥；Actions artifact 保留 30 天，应及时验收/归档。
2. 下载本次**准确字节**做原生验收，将短记录保存到
   `docs/releases/windows/vX.Y.Z-windows.preview.N.json` 并合入 main。
   再运行 `mode=publish`，提供同一 tag 和原 `artifact_run`。发布 job 不重建验收包。

记录格式（摘要、长度、系统、时间填实际值；未通过的检查保持 false）：

```json
{
  "schema": 1,
  "tag": "v0.14.0-windows.preview.1",
  "source": "exact-40-character-source-sha",
  "channel": "preview",
  "platform": "windows-amd64",
  "os": "actual system and build",
  "testedAt": "actual ISO-8601 date",
  "artifacts": [
    {"format":"nsis","file":"Caelis-Bot-0.14.0-windows.preview.1-windows-amd64-preview-Setup.exe","sha256":"actual-sha256","length":0},
    {"format":"zip","file":"Caelis-Bot-0.14.0-windows.preview.1-windows-amd64-preview.zip","sha256":"actual-sha256","length":0}
  ],
  "checks": {
    "nativeShell": false,
    "runtimeAndComputerUse": false,
    "activeTaskHandoff": false,
    "oldClientReconnect": false,
    "setupLifecycle": false,
    "zipLifecycle": false,
    "autostart": false,
    "dataPreserved": false
  }
}
```

`nativeShell` 覆盖桌宠、气泡、聊天/设置、附件/图片、审批、任务卡片、通知和快捷键；
`runtimeAndComputerUse` 包括两 Runtime 的核心工作和 Caelis 下真实 DTW 桌面任务；
handoff/reconnect 覆盖活跃任务、原回执和旧 Client；生命周期覆盖安装、升级、卸载/原位更新、
Unicode 路径。不得把组件测试填成完整链路通过，保留未通过明细与复现日志。

只创建 GitHub prerelease，`latest=false`；不修改 Mac appcast、latest.json 或 R2。
现有 release 会阻止重写；上传中途失败会留下 draft，核对已有文件后处理，不能覆盖已公开字节。
尚未配置远端 environment/Secret，也没有运行或发布本 workflow。

## 签名资料

复用本机文档目录的 `Caelis Release Materials/Windows/update-signing/release.pem`，
不要为新版本重新生成。GitHub environment **windows-release** 限制 main；Secret
**WINDOWS_UPDATE_PRIVATE_KEY** 保存 PEM 全文。构建 job 不读取它，发布 job 用受限临时文件，结束即清除。
`internal/windowsupdate/public-key.txt` 随 App 内置；脚本校验私钥与它匹配。
本机独立备份目录 README 记录用途；私钥不进入源码、日志、缓存 artifact 或发布包。

```text
node script/windows-update-manifest.mjs create DIST TAG SOURCE_SHA ACCEPTANCE_JSON PRIVATE_KEY_PATH
node script/windows-update-manifest.mjs verify DIST TAG SOURCE_SHA
```

签名绑定 tag、source、渠道、平台及两种制品的摘要/长度。Ed25519 更新签名与 Authenticode
独立：早期无需购买代码签名服务，但不能保证 unsigned 包在每台机器都不被 SmartScreen 或组织策略阻止。
密钥丢失不能从公钥恢复；直接轮换会使旧客户端无法自动验证新包。

## 自动更新和恢复

默认后台检查/下载，设置中显示就绪后由用户选择重启安装；开发包不访问发行 feed。
Setup 更新自身安装位置；ZIP 更新原解压目录，数据保持在 AppData。自启动默认关闭，
只管理当前安装自己注册的 HKCU Run 条目；移动 ZIP 后需重新注册。

更新前冻结新输入、保留原回执；允许强制关闭归属已确认的观察终端，不停止 Worker/共享 Host。
成功保存交接状态才写 commit；助手等原进程退出并校验 commit 后安装。
ZIP 在安装目录内创建当前用户私有的暂存/备份目录，按归属清单同卷替换并回滚，
因此下载目录与安装目录可位于不同磁盘。失败保留原事务和恢复资料，成功校验后清理暂存；
外来文件不删除。NSIS 安装失败可能需要手动重装。
缺失、waiting 或 installing 回执保持未知，不自动重放；明确 failed 后用户重新检查会建立新事务，
旧诊断保留。Windows Caelis 终端按同一 Store/Session 发现 Host，避免固定旧端口。

两版本 Host/TUI、完整 Application 带活跃对话/Worker 的重开换 Host，以及安装组件测试已通过；
它们尚未串成 GUI 安装 App→App 带任务升级验收。真实登录、低磁盘、
下载中断与完整 UI 仍需按[实施台账](windows-adaptation-issues.md)验收，不能据此提前发布。
