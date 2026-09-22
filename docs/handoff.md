# 继续开发

当前主线是 Caelis Bot 的产品功能和 macOS 预发布质量，先读 `product.md`、`architecture.md`、
`roadmap.md`。产品已具备真实 Codex 工作闭环，不从早期工具链试验重新开始。

最新切片是[秘书与专业任务分离](task-delegation.md)：专业工作经 Bot 自有任务接口进入
独立受管目录，完成后有限唤醒秘书汇报；不依赖 Codex App 的私有 IPC。已有项目/worktree
授权与其他 App 任务的显式接管仍待后续实现，不能当成当前能力。

该实现已提交为 `ef2f557`。后续后端/平台边界见
[Caelis 接入与 Windows 计划](backend-platform-plan.md)：优先收敛后端装配和能力契约，
Caelis adapter 对接公开 Control Host，完整秘书能力另需 Caelis Control 的受限委派扩展。
Windows 已列出窗口、IPC、进程和私有存储落点，原生实施仍在 macOS 发行后启动。
该计划来自两个仓库的代码审查，未做 Caelis 真实联调或 Windows 原生验收。

公共边界首片已落地：`internal/app` 接管产品装配/退出，后端可选能力已命名，
配置选项由 provider 提供，固定角色与工具通信分别移至 `botpolicy` / `localipc`。
以[能力契约](backend-contract.md)作为 Caelis 后续补全的入口；它明确必需能力与授权语义。
Codex 数据保留原位置，第二个后端和跨后端切换仍未启用。最新验证见 preparation-status。

## 工作范围

- 本仓库：原生生命周期、连接适配、聊天/审批/附件、桌面行为、成品渲染、发行。
- 组织私有资产仓库：建模、衣服/饰品、新角色、作者工具、原始来源与视觉制作验收。
- 当前默认成品、哈希与许可由 `resources/character-pack.json` 固定，详见 `character-assets.md`。
- 制作历史已从公开根移出。不要把源目录、旧 Git 对象、参考图或制作截图重新加入主库。

## 修改后验证

`make check` 检查成品边界、运行时动作与 Go 行为；`make smoke` 只握手本地 Codex 并验证成品；
`make build` 构建 ad-hoc 签名应用。需要观察原生窗口时用 `script/build_and_run.sh --verify`。
没有本地 Codex 时可以单独运行 `npm run smoke:assets`；CI 不安装或携带 Codex。
测试通过不代表所有角度无穿模、双屏/Spaces/全屏都已验收。

继续优先处理预发布可用性、轻量桌面行为和真实演示；角色资产经私库更新 PR 独立迭代。
Developer ID、公证及 Windows 原生适配后置。保持历史记录和当轮验证的区别。
