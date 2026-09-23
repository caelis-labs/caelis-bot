# 继续开发

当前主线是 Caelis Bot 的产品功能和 macOS 预发布质量，先读 `product.md`、`architecture.md`、
`roadmap.md`。产品已具备真实 Codex 工作闭环，不从早期工具链试验重新开始。

最新切片是[秘书与专业任务分离](task-delegation.md)：专业工作经 Bot 自有任务接口进入
独立受管目录，完成后有限唤醒秘书汇报；不依赖 Codex App 的私有 IPC。已有项目/worktree
授权与其他 App 任务的显式接管仍待后续实现，不能当成当前能力。

2026-09-23 当前切片是统一 Bot 产品层：`internal/bot` 持有身份、工具与提醒，
`internal/tasks` 持有产品目录、账本与汇报；Codex adapter 仅映射 native 执行/审批/回执。
`bot.json` 跨 Runtime 保留身份，计划和在途工作仍绑定原 Runtime。Notebook/Memory 的本地增量见文末。

Caelis 正在并行重做通用应用基础设施；直接交给其 Agent 的
[handoff prompt](caelis-core-rebuild-handoff.md) 包含删除旧 Bot Mode 的授权与 A01–A12 审计标准。
桌面当前明确禁用旧 Bot Mode，无 fallback；旧 adapter/协议 fixture 仅保留作替换参考。
新 wire/schema 交还后接入，不能把旧 MiMo 联调结果视为新协议通过。

Runtime Setup、头像图标、显式切换、AppKit 点击/拖动与窗口唤回等已有功能保留。
Developer ID 工作单独保存为本地 stash `deferred: Developer ID signing pipeline (2026-09-23)`；
当前产品工作树使用原有 ad-hoc 预览签名。完整验证与限制以 preparation-status 为准；尚未发布。

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
`make smoke-caelis` 属于旧 Bot Mode 的历史 fixture，需要显式提供外部 Caelis 二进制，
不能作为新协议或当前桌面可用性的证据。
`make smoke-caelis-live` 使用用户已配置的隔离 Host 进行真实模型调用；MiMo v2.6 Flash
曾在旧模式通过，需显式提供 store/model，不在日常 CI 中执行。
测试通过不代表所有角度无穿模、双屏/Spaces/全屏都已验收。

继续优先处理预发布可用性、轻量桌面行为和真实演示；角色资产经私库更新 PR 独立迭代。
Developer ID、公证及 Windows 原生适配后置。保持历史记录和当轮验证的区别。

## 2026-09-23 Notebook / Memory 收敛

统一产品层检查点为 `b449aad`，本轮改动尚未提交。Notebook 为普通 Markdown 目录，
应用只维护 INDEX 和当天目录，MEMORY 与日期笔记由用户直接编辑或 Bot 用文件工具维护。
`internal/botskills/skills/caelis-bot-memory/SKILL.md` 只注入秘书；worker/普通 Session 不继承。
名字必填、描述可选的表单仅首次初始化出现，转成可见用户消息，由 Bot 写 MEMORY；
投递前保留待发正文，接受后只留状态，结果不明不自动重发。

旧「记忆与笔记」UI、专用笔记 CRUD 与 Facts 写入口已移除，已有数据一次性复制为日期笔记，
原数据保留。嵌入 Memory v0.6.1 的线索工具支持 recall/remember/correct/forget。
路径、边界和验收见 [个人空间](personal-memory.md)。Developer ID 仍在独立 stash。
Caelis 新协议仍待接入，fixture 的共享文件/记忆验证不代替两个真实 Runtime 联调。
