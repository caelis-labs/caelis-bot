# 气泡、截图主题与 Auto-review 接入评估

日期：2026-09-28。当前分支已整合 Bot PR #37（主线 `0274933347d2ff452847f94ac03bfeceff2e7740`）。
Core 公共协议现已固定到 #89，包含 #85 环境修复及 #37 所需的 content-v1。
Guardian 后续实现、工具策略和 release 条件见[联调报告](../caelis-guardian-acceptance.md)。
以下显示验证仍保留其当时的证据边界。

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

原公共入口缺口 #88 已由 Core #89 合并。Bot 已接入显式 Guardian 装配和 callback 审查，
按低风险记忆/读取直通、Worker 派生需审查、Computer Use 每 App 每任务 Turn 一次授权分配成本。
Codex 继续用原生 Auto-review 与 Computer Use；不修改全局设置或绕过原生拒绝。
item identity 已由 Core #91 补齐；完整实现、验证与发布条件见[联调与发布条件](../caelis-guardian-acceptance.md)。

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
- 此处显示改进不需要改 skill；后续 Guardian / App 授权及只读工具工作流已更新英文 Bot skill。
