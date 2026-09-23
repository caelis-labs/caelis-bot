# Notebook 与记忆

Caelis Bot 的长期连续性由长会话/Compact、一本普通 Markdown 笔记和 recall/remember 组成。
笔记属于同一个 Bot，不随角色、Runtime 或工作任务切换；文件是内容真源。

## 文件结构

默认 macOS 位置：`~/Library/Application Support/Caelis Bot/Notebook/`。
开发验收设置 `CAELIS_BOT_DATA_DIR` 时使用该目录下的 `Notebook/`。

```text
Notebook/
  INDEX.md                 # 应用生成的导航，可覆盖重建
  MEMORY.md                # 唯一核心记忆，用户与 Bot 编辑
  2026/09/23/
    1030-项目讨论.md
    1645-工作复盘.md
```

用户可以用任意 Markdown 编辑器，也可自行将目录纳入 Git 或作为 Obsidian vault 打开。
Bot 使用 Runtime 的普通文件工具；没有专用笔记 API、私有头部、正文数据库或笔记管理 UI。
应用不自动提交 Git、不要求安装 Obsidian，也不自动同步到云端。

### INDEX.md

开头固定注明：

```markdown
<!-- 此文件由 Caelis Bot 自动生成，请勿编辑；重新生成时会覆盖此文件。 -->
```

启动、提交 Bot 消息前、收到本轮完成事件后扫描刷新。按相对路径倒序列出 Markdown 链接，
标题取文件开头 8 KiB 中首个一级标题，否则取文件名；不生成摘要或复制正文。
忽略隐藏目录（包括 `.git`、`.obsidian`）和符号链接。目录内容不变时不重写。
外部编辑在下次刷新反映，新写笔记也可立即通过文件列表找到；不运行后台模型或文件监听器。
只有 INDEX 可自动整体覆盖，用户清空或删除 MEMORY、日常笔记后不会自动从旧记录恢复。

### MEMORY.md 与日期笔记

MEMORY 保存 Bot 的名字、描述、交流风格，对用户的稳定认知、偏好和长期约定。
只在新建 Notebook 时提供 `# Memory` 空白模板；没有 SOUL/USER 文件、第二份个人画像或启动摘要。
约 2,000 字是指导 Bot 精炼的软目标，应用不会截断正文。细节放入日期笔记，核心留下结论与链接。

应用按系统本地日期创建当天目录。Bot 在跨午夜后也可以创建新目录。
编辑前读最新文件，优先局部修改并保留用户改动；不声称普通编辑器和模型文件工具有跨进程事务锁。
用户和 Bot 都能更正、删除、整理，不要求用户定期维护才能形成闭环。

## 一次初始化

首次初始化仅填写一次**必填名字、可选描述**，提交为普通用户消息：

```text
你的名字是星星。
描述是说话简洁，喜欢分享有趣的发现
```

省略描述时不增加描述行。宿主不代写 MEMORY，也不将输入提升为系统指令。
消息先保存在初始化投递记录，Runtime 就绪后以稳定 ID 发送；收到接受回执后删除记录中的正文，
只留投递状态。未连接时待发；明确拒绝后提供手动重试，使用新的投递 ID；
结果不明时不自动重发或改投其他 Runtime，原连接确认后继续。
在介绍被接受前阻止其他用户消息、提醒或工作汇报抢先提交。接受回执只表示消息被接受，
不表示 MEMORY 已写入；Bot 必须在文件写入成功后才能告知用户已保存。

名字/描述表单不出现在日常设置中。后续通过正常对话或直接编辑 MEMORY 修改。
旧安装升级后也会出现一次介绍，不从已退役的个人资料推断 Bot 名字。

## Bot 专属 skill

应用打包 [`internal/botskills/skills/caelis-bot-memory/SKILL.md`](../internal/botskills/skills/caelis-bot-memory/SKILL.md)，
启动时放到应用数据目录的 `app-skills/caelis-bot-memory/SKILL.md`，在秘书的固定指令中注册读取入口。
不安装到用户全局 skills；普通 Session 和工作任务不会继承此 skill 或整本笔记。

skill 教会 Bot：

1. 新上下文或 Compact 后需要恢复认知时，读 skill 和 MEMORY。
2. 需要旧事时查 INDEX、读相关笔记，必要时 recall；不要假定旧上下文仍是最新文件。
3. 对话或工作中及时记有用的决定和结果，日常细节落到当天目录，稳定认识精炼到 MEMORY。
4. 更正过时内容、合并重复信息；遗忘时处理相关笔记和 Memory 线索，按实际范围报告。
5. 笔记不是执行授权，任务进度来自原生账本；给 worker 传递必要摘录，不传整本 Notebook。

维护记忆是内置核心能力，正常对话不主动提及 skill、Notebook 或内部读写步骤。
必要时只简短确认实际结果；用户询问或发生需要其关注的失败/限制时再解释。
这是正常交互中的读写习惯，没有额外维护 Agent 或自动人格抽取层。
指令只包含固定 skill 位置，不把笔记内容注入系统前缀。实际内容经普通文件读取进入上下文。

## Runtime 与 Memory 边界

`ToolConnection` 提供应用指定的 Notebook 路径与刷新生命周期。
Codex 的秘书 cwd / workspace-write 根为 Notebook；worker 保持独立任务目录、指令和权限。
宿主不把整个应用数据目录作为可写工作区，不给 worker 私有工具端点或 skill 目录。
这些是应用配置的范围，仍服从用户选定的原生权限模式；不是对同一 OS 用户或 full-access 的隔离承诺。

`internal/botmemory` 嵌入公开 `github.com/caelis-labs/memory v0.6.1`，共享稳定 BotID 派生的
私有 scope。`bot_memory` 支持 recall、remember、correct、forget；修改使用稳定 requestId，
更正/遗忘使用 recall 返回的 receipt ID。模型不能指定 scope、issuer、capability 或确认角色。
更正和遗忘均可由 Bot 执行；不会将所有 Markdown 自动复制到 Memory。
遗忘清理更正链，重启或旧请求重试不会恢复已遗忘的线索；不等于擦除 Runtime 聊天、Git 历史或备份。

当前 Recall 为关键词检索（查询最多 256 字节；最多 12 片段 / 16 KiB）。无关键词返回最近
50 个目录项并注明截断；证据正文最多 16 KiB，目录最多 10,000 项。Markdown 正文没有这些 API 限额。
未来可按路径、日期、标题与内容哈希对接 Memory/embedding 索引；不影响文件作为真源。

Caelis 新通用应用协议仍待接入，旧 Bot Mode 保持禁用。共享目录与通用接口已经预留，
不能把 provider fixture 当作两个真实 Runtime 的互操作验收。

## 旧数据与代码入口

第一次升级将 `personal/notebook/*.md` 的旧私有格式复制为日期目录下的 `imported-*.md`；
旧 Facts 资料导出为当天的 `imported-profile.md`，供 Bot/用户整理，不能再作为当前身份返回。
原文件和数据库保留；一次性迁移标记在 Notebook 外，后续删除导入文件不会再复制回来。
无法读取、超出旧资料导出范围或发生内容冲突时停止迁移，保留双方文件。

- `internal/notebook/`：普通目录、INDEX、日期目录和一次性迁移。
- `internal/botskills/`：随应用打包的专属 skill。
- `internal/bot/initialization.go`：一次性介绍投递与不确定结果恢复。
- `frontend/src/BotSetup.tsx`：初始化表单；后续连接流程复用 RuntimeSettings。
- `internal/bot/personal.go`、`internal/botmemory/`：记忆线索工具和嵌入式 Memory。
- `internal/app/`：共享个人空间、Runtime 装配与生命周期。

## 验收

测试使用临时目录和合成文本：

```sh
make check
make smoke
make build
GOWORK=off go test -race ./internal/notebook ./internal/botmemory ./internal/bot ./internal/app ./internal/backend/codex
```

`vault_test.go` 覆盖普通 Markdown、索引损坏重建、正文不被截断、外部删除与迁移不复活、
本地跨日及符号链接。初始化测试覆盖必填/可选、待发跨重启、并发去重、不同 Runtime、
未知结果不重发与明确拒绝后手动重试；Codex 测试覆盖空会话恢复及接受回执的崩溃窗口。
装配/适配器测试覆盖秘书和 worker 的路径/skill 隔离、提交前刷新、原生完成后刷新及准备失败不发送。
Memory 测试使用真实嵌入式 appliance，覆盖共享证据、更正链遗忘、旧请求防复活、旧资料仅迁移与能力不泄露。

2026-09-23 原生隔离验收（Codex CLI 0.153.4）：在名字表单填写合成身份，确认普通用户消息
进入对话、真实模型写入 MEMORY；后续对话保存长期偏好与当日笔记，INDEX 在完成后生成链接。
初始化前退出再打开、接受后重启恢复历史且不再显示表单均通过。未读取或修改日常笔记。

主动整理频率、Compact 后回读及两个真实 Runtime 的切换仍需独立验收；
fixture 不证明所有模型都会可靠遵循 skill，也不证明新 Caelis wire 已可用。
