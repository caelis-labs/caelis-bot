# 工具链

公共项目构建依赖 Apple Command Line Tools、Go、Node 24 和锁定的 npm/Go 依赖。
版本见 `toolchain.json` 和 `package-lock.json`。不需要 Blender、Python、美术插件或私库权限。
普通用户只需已打包的应用和可连接的本地 Codex，应用不携带 Codex 运行时。

```sh
make setup
make doctor
make check
make smoke
make build
make run
```

`script/env.sh` 设置本仓库缓存、`GOWORK=off` 与 macOS deployment target，不修改用户 shell。
`make doctor` 显示本机工具；缺少 Codex 时不影响前端/共享核心 CI，原生握手 smoke 需要 Codex。
协议生成使用记录的 Codex 开发基线；普通用户 CLI 版本以握手能力判断，不按发行版本设白名单。
`make schema` 是显式更新协议基线的维护操作，普通构建消费已提交的协议。

`make smoke` 不建立会话、不请求模型：只启动自己的 Codex App Server 子进程、完成 initialize
并回收；资产部分直接验证当前成品 SHA-256、glTF、Three.js 加载和动画更新。
`make check` 再检查行为恢复、输入抢占、手指/视角形变与宿主生命周期。
`make build` 制作可运行的 macOS app，附代码/素材授权和成品版本清单，再 ad-hoc 签名。
`make package` 生成只读压缩 DMG 和 SHA-256，挂载核验包内签名；本地命令不上传、不公证。
发布、分支保护和 release-please 流程见[发布维护](release.md)。

共享核心移植检查不代表 Windows 原生 GUI 已可用。GPU 和 Window Server 行为需通过
`script/build_and_run.sh --verify` 启动后的实际窗口确认。

建模工具链与导出流程由私有资产仓库独立锁定，交接方式见[成品合同](character-assets.md)。
