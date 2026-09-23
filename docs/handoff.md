# 继续开发

当前主线是 Caelis Bot 的产品功能和 macOS 预发布质量，先读 `product.md`、`architecture.md`、
`roadmap.md`。产品已具备真实 Codex 工作闭环，不从早期工具链试验重新开始。

最新切片是[秘书与专业任务分离](task-delegation.md)：专业工作经 Bot 自有任务接口进入
独立受管目录，完成后有限唤醒秘书汇报；不依赖 Codex App 的私有 IPC。已有项目/worktree
授权与其他 App 任务的显式接管仍待后续实现，不能当成当前能力。

当前新增 Caelis Control Host adapter 与运行时管理，见[接入说明](caelis-integration.md)。
使用公开固定协议，Control 独占受管工作、汇报与提醒授权；不复制 Codex 的 MCP/报告循环。
Caelis 接入检查点为 `e6699a8`。本轮继续落地已批准的 Runtime Setup：首次选择、原生检测与安装、
Caelis 模型配置、Codex 登录及设置中的独立管理；使用原生重启应用后端选择。
本地检查点包含上述实现，保留 Codex 原有记录。实现契约见 `internal/backend/api/setup.go` 与
`docs/backend-contract.md`；视觉草稿位于 `docs/design/runtime-setup-v1.md`。
同时包含完整头像图标、明确的运行时切换入口，以及 AppKit 独立单击/双击/拖动识别与窗口唤回修复。
完整验证与限制以 preparation-status 为准；尚未发布。

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
`make smoke-caelis` 需要显式提供外部 Caelis 二进制；使用隔离 Host 和可控模型，见接入说明。
`make smoke-caelis-live` 使用用户已配置的隔离 Host 进行真实模型调用；MiMo v2.6 Flash
已通过，需显式提供 store/model，不在日常 CI 中执行。
测试通过不代表所有角度无穿模、双屏/Spaces/全屏都已验收。

继续优先处理预发布可用性、轻量桌面行为和真实演示；角色资产经私库更新 PR 独立迭代。
Developer ID、公证及 Windows 原生适配后置。保持历史记录和当轮验证的区别。
