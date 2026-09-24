# 继续开发

当前主线是 Caelis Bot 的产品功能和 macOS 正式版质量，先读 `product.md`、`architecture.md`、
`roadmap.md`。产品已具备真实 Codex 工作闭环，不从早期工具链试验重新开始。

2026-09-24 原生任务气泡已接入：脚边收起、相同圆球、悬停原始 prompt、点击外部终端。
Codex 使用标准共享 Unix App Server，Bot 保持订阅并观察终端用户的新回合，不再长期轮询 Worker。
完整检查、smoke、构建、相关 race 与安装版隔离协议验收通过；原生浮动气泡的 hover、截断和
点击 Terminal GUI 仍需人工实机复验，不能把此前 POC 截图当成新构建的完整视觉证明。
Caelis 原生 Worker 也已接入相同入口，使用现有 Session 和用户凭据文件；主动关怀仍未产品化。详见[任务委派](task-delegation.md)及[验证状态](preparation-status.md)。

2026-09-24 设置与窗口体验已在本地整理：连接页去重、模型统一保存与草稿保留、
页面切换回到顶部；Dock 随聊天/设置的打开状态显示，关闭页面不退出应用。
当前成品由 `resources/character-pack.json` 固定，DMG 使用紧凑拖拽布局；打包额外依赖项目内固定版本 Python 工具环境。
完整检查、smoke、构建与 DMG 验证通过，已检查原生设置和 Finder 安装窗口；
物理快捷键与 Dock 点击、跨 Spaces 和完整 VoiceOver 仍待人工复验。详见 [验证状态](preparation-status.md)。
改动尚未发布；不要把本地 ad-hoc 开发镜像作为已公证版本分发。

2026-09-24 已本地修复后端通知误报：按方法和 item 类型消费 Codex 事件，非阻塞组件错误与
重试只写入可滚动清理的私有诊断日志；真正任务失败及关键生命周期不确定仍提示用户。
原生启动恢复用户 shell 导出环境，Bot/worker 及其 MCP/工具继承同一本机工具环境，
保留任务工作目录、原生权限与秘书私有能力边界；普通连接不重启共享 Caelis Host；显式更新通过原生生命周期启用新版。
检查、smoke、构建、相关 race 测试及真实 Context7 无模型启动验证通过；
新构建的原生窗口受正在运行的正式版单实例限制，未完成 GUI 验收。详见
[任务委派](task-delegation.md)与 [验证状态](preparation-status.md)。本轮尚未发布。

2026-09-24 更新切片：Sparkle 原生更新、忙碌时延后安装和公证后 R2 同步已本地接入。
R2 使用主仓库相同桶/域名下独立的 `caelis-bot/` 前缀，只保留最新正式版；同步失败
通过 `sync-r2.yml` 重试，不重新公证。Sparkle 公私钥和 R2 已配置为限定仓库可用的组织
变量/秘密；Sparkle 身份保留在本机钥匙串，新 R2 凭据于 2027-08-24 到期，见 [release.md](release.md)。
下一发行仍需验证公开签名更新及跨版本重启。当前旧版需手动
升级一次；本地 fixture、完整检查和实机/公证尚缺的验证范围见 [preparation-status.md](preparation-status.md)。

最新切片是[秘书与专业任务分离](task-delegation.md)：专业工作经 Bot 自有任务接口进入
独立受管目录，完成后有限唤醒秘书汇报；不依赖 Codex App 的私有 IPC。已有项目/worktree
授权与其他 App 任务的显式接管仍待后续实现，不能当成当前能力。

2026-09-23 当前切片是统一 Bot 产品层：`internal/bot` 持有身份、工具与提醒，
`internal/tasks` 持有产品目录、账本与汇报；Codex adapter 仅映射 native 执行/审批/回执。
`bot.json` 跨 Runtime 保留身份，计划和在途工作仍绑定原 Runtime。Notebook/Memory 的本地增量见文末。

Caelis 当前基线为正式 v0.62.0（`812264e`），固定协议哈希见 `protocol/caelis/manifest.json`。
共享原生 Worker、steering、模型/Team 配置和连接向导通过公开 HTTP/SSE 接入。
Runtime 更新现在区分安装版本与服务版本，依次启用服务、验证协议、重新连接 Bot；
已有新版程序可直接启用。任务忙碌或状态未知时拒绝替换，具体共享客户端边界见
[接入契约](caelis-integration.md)。历史真实模型证据见[正式版联调报告](caelis-release-acceptance.md)，
新增路径和仍待验证范围以[实现与验证状态](preparation-status.md)为准。

工作模型与 Bot 模型解耦；Agent team 是 Runtime 的共享配置，与 TUI `/team` 一致。
Bot 不按任务发现或指定另一套 team。详见[任务委派](task-delegation.md)。

Runtime Setup、头像图标、显式切换、AppKit 点击/拖动与窗口唤回等已有功能保留。
2026-09-24 [v0.1.0 正式版](https://github.com/caelis-labs/caelis-bot/releases/tag/v0.1.0)已发布。
Developer ID 改动已恢复并提交；公开 DMG 及其 App 的签名、公证票据、Gatekeeper、
校验和与源码提交已在本机重新下载验证。原 stash 仅作备份，不能再次当作待实施改动。
CI 单次公证等待 60 分钟，打包作业 150 分钟；仍在处理时保留签名原件及提交 ID，
跳过发布，可使用 `resume_run_id` 继续。详见[发行流程](release.md)与 preparation-status。

[能力契约](backend-contract.md)定义产品装配和 provider 分支，
[平台计划](backend-platform-plan.md)保留 Windows 落点与历史审查。Windows 原生实现仍在
macOS 完整发行后进行；不能把共享核心交叉编译当作 Windows 桌面或 ACL 验收。

2026-09-23 新增本地内容包 v1：共享原生校验/注册表、外观设置、独立头像选择、
基础 GLB 渲染与离线制作工具。社区接入请从 [内容包开发指南](content-packs.md) 开始；
[扩展边界](asset-extensions-plan.md) 定义预置内容与第三方导入的职责及新能力接入规则。
内容包不改变 Runtime、对话身份或任务权限，不能携带执行代码。

## 工作范围

- 本仓库：原生生命周期、连接适配、聊天/审批/附件、桌面行为、成品渲染、发行。
- 组织私有资产仓库：建模、衣服/饰品、新角色、作者工具、原始来源与视觉制作验收。
- 当前默认成品、哈希与许可由 `resources/character-pack.json` 固定，详见 `character-assets.md`。
- 制作历史已从公开根移出。不要把源目录、旧 Git 对象、参考图或制作截图重新加入主库。

## 修改后验证

`make check` 检查成品边界、运行时动作与 Go 行为；`make smoke` 只握手本地 Codex 并验证成品；
`make build` 构建 ad-hoc 签名应用。需要观察原生窗口时用 `script/build_and_run.sh --verify`。
没有本地 Codex 时可以单独运行 `npm run smoke:assets`；CI 不安装或携带 Codex。
`make smoke-caelis` 使用显式提供的固定 Caelis 二进制、临时 Store 与合成模型，
验证新通用协议和 macOS 原生工具，不代表真实模型或原生 UI 已验收。
`make smoke-caelis-live` 使用用户已配置的隔离 Host 进行真实模型调用；MiMo v2.6 Flash
与 GPT-6 Luna 已在新协议通过；需显式提供隔离 store/model，Fast 与自管 Host 参数见实测报告，不在日常 CI 中执行。
测试通过不代表所有角度无穿模、双屏/Spaces/全屏都已验收。

继续优先处理正式版可用性、轻量桌面行为和真实演示；角色资产经私库更新 PR 独立迭代。
Developer ID 和公证已接入正式发行；Windows 原生适配仍未实现。保持历史记录和当轮验证的区别。

## 2026-09-23 Notebook / Memory 收敛

统一产品层检查点为 `b449aad`，Notebook 检查点为 `bca03ce`。Notebook 为普通 Markdown 目录，
应用只维护 INDEX 和当天目录，MEMORY 与日期笔记由用户直接编辑或 Bot 用文件工具维护。
`internal/botskills/skills/caelis-bot-memory/SKILL.md` 只注入秘书；worker/普通 Session 不继承。
名字必填、描述可选的表单仅首次初始化出现，转成可见用户消息，由 Bot 写 MEMORY；
投递前保留待发正文，接受后只留状态，结果不明不自动重发。

旧「记忆与笔记」UI、专用笔记 CRUD 与 Facts 写入口已移除，已有数据一次性复制为日期笔记，
原数据保留。嵌入 Memory v0.6.1 的线索工具支持 recall/remember/correct/forget。
路径、边界和验收见 [个人空间](personal-memory.md)。该历史检查点中 Developer ID 尚在独立 stash，现已恢复发行。
Caelis 新协议已完成真实模型 Notebook 读写、INDEX 更新和重启后回读；跨 Runtime 资料迁移未在本轮重验。
