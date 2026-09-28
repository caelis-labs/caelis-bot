# 气泡、截图主题与 Auto-review 接入评估

日期：2026-09-28。当前分支已整合 Bot PR #37（主线 `0274933347d2ff452847f94ac03bfeceff2e7740`）。
Core 公共协议仍固定到 #85，包含 #37 所需的 content-v1；合并保留关怀结果账本、callback
格式账本、一次环境初始化和独立 Dev 身份。

## 显示改动

聊天与头顶气泡共用逐字呈现缓冲。后端仍按原节奏提供权威文本，前端在动画帧内按 Unicode
字素展示增量；中文、组合 emoji、重音字符不拆开。大块到达最多用 450 ms 追平，完成时
最多用 120 ms 展示剩余正文；审批、停止和执行状态不等待动画。历史、非追加修正、后台页面
及减少动态效果设置直接展示原文。新消息按 item 身份分离，整条消息复制与原生回执不使用动画缓冲。

头顶气泡收起时保留紧凑横排；阅读展开后两个按钮在顶部右侧一行，正文/表格使用下方完整宽度。
长内容与宽表格分别保留滚动，不覆盖正文，不改变仅阅读不激活窗口、链接不触发主体点击的行为。

## 玻璃材质与截图主题

正式气泡改用独立的 Clear Glass；阅读底色从预览的 90% 提至 94%，文字保持完全不透明。
快捷输入和设置继续使用原有 Regular 材质。macOS 26+ 使用公共 NSGlassEffectView；旧系统保留
原生毛玻璃回退，减少透明度/增强对比度使用实色。预览已链接生产材质实现，深浅色检查只改变
预览窗口 appearance，不修改系统偏好。

F1 截图工具栏的问题是 CALayer 提前保存了固定 CGColor，深色窗口出现后没有重新解析语义色。
工具栏与备注输入底色现在在挂载和 appearance 变化时重算；图标使用 labelColor，选中态与
Ask Bot 的前景/底色随外观刷新。颜色选择本身仍代表标注颜色。

## Auto-review 结论

| 路径 | 当前状态 | 处理方向 |
| --- | --- | --- |
| Codex 常驻 Bot 与内部 Worker | 默认 `on-request + auto_review + workspace-write`，已有原生审查事件投影 | 继续使用原生审查；尊重用户明确选择的手动/只读模式与组织策略 |
| Codex MCP/连接器 | 需要审批的工具交给原生 reviewer；已有明确 Bot 工具集合免额外审批，连接器可有独立配置 | 不改成全局免审批，不覆盖连接器/组织策略 |
| Codex Computer Use | #37 使用原生能力；应用访问确认仍可直接向用户展示 | 保留原生应用授权；网站/域名访问中符合条件的请求由原生 Auto-review 处理 |
| Caelis Application 原生命令/文件 | 公共 profile 只支持 `manual`，独立 Gateway 固定 manual | Core 提供通用 reviewer 装配后再接入 |
| Caelis Application callback / Bot Cua | 当前工具描述无公共 reviewer 策略契约，不能认定回调已被 Guardian 审查 | 将通用工具策略/动作证据入口纳入 Core 需求；不要在 Bot 中伪造审批同意 |
| 系统权限、登录、用户信息选择 | 属于原生系统或用户输入流程 | 保留必要的人工操作 |

Codex 证据在 `internal/backend/codex/{bot,execution,tasks,recovery}.go` 和对应回归；手动选择由
`ExecutionSettings.ApprovalMode` 保留。[官方 Auto-review 文档](https://developers.openai.com/codex/auto-review)
说明 reviewer 只处理符合条件的审批，保留 sandbox，并区分 Computer Use 应用确认。
[App Server 文档](https://developers.openai.com/codex/app-server) 说明 apps reviewer 可有独立覆盖。
本次没有改写用户全局 Codex 配置，也未用付费模型验证 Guardian 的风险判断质量。

Core #85 的 `control/application/bindings.go` 拒绝非 manual，
`app/gatewayapp/application_runtime.go` 使用 `DefaultApprovalMode: kernel.ApprovalModeManual`；
当前主线 `649033e255e6e3a9a4d3767ad03966fee7b22926` 再次核对仍有该限制。
Core 普通 Session/Worker 的策略由其原生配置装配，不等于 Application 已接入同一路径。

已登记 [Core #88](https://github.com/caelis-labs/caelis/issues/88)：可协商 reviewer 配置、审查生命周期、
确切动作身份、失败/拒绝恢复、Application callback 边界及 Session/Worker 隔离。
Core 保持通用能力，上层负责选择和装配。待公共契约合并后，Bot 再做能力探测、配置装配及原生联调；
当前继续诚实保留 Caelis 的人工审批。

## 验证

- 逐字呈现测试覆盖分块、多帧、密集增量、大块追平、最终 Markdown 完整性、字素、历史、修正和立即展示。
- 生产前端通过 `script/build_and_run.sh --bubble-preview` 运行在隔离原生 WebKit 非激活面板中。
  Issue 表格展开后正文宽 318 pt，按钮宽 58 pt 且只占顶部一行；简短链接表格高度 308 pt，完整 URL 表格 428 pt。
  长文高度 476 pt、可滚动内容 1218 pt、正文视口 412 pt；记录到 631 个不同文本帧。
  顶部避让展开仍为 476 pt，未超出 480 pt 上限。
- Clear Glass 深浅色原生预览记录 `glass-clear` 与 alpha 0.94；实色呈现分支为不透明 RGB。
  证据 `.cache/bubble-glass-{dark,light,solid}.json`。实色检查注入渲染分支，未切换全局系统无障碍设置；
  旧 macOS 的材质回退未在旧硬件实测。
- 截图主题回归在修复前因浅/深底色断言失败，修复后浅→深→浅切换及既有截图生命周期检查通过。
  原生窗口已查看；证据 `.cache/capture-theme-{before,after,live}.log` 和
  `.cache/capture-preview.png.{dark,light}-toolbar.png`。
- UI fixture 只使用合成消息，不连接日常 Store 或模型。完整 gate 结果见 `docs/preparation-status.md`。
- 本轮是显示改进与审批能力评估，未新增 Bot 工具或模型工作流，不需要改 Bot-facing skill。
