# Bot 核心 skill：渐进式加载

2026-09-25 已实现于应用，尚未发布。英文正文的唯一来源是
[`internal/botskills/skills/caelis-bot-memory/`](../../internal/botskills/skills/caelis-bot-memory/SKILL.md)。
不保留另一份待同步的技能稿。

## 加载方式与作用域

沿用标准文件技能的渐进式披露：模型先收到名称、description 和文件位置，需要使用时
读取 `SKILL.md`，再按场景读取辅助文件。description 提供 Caelis Bot 基本身份和必须加载的原因；
身份恢复、记忆和工作方法在正文展开。参见 [官方 Skills 文档](https://learn.chatgpt.com/docs/build-skills)。

`botskills.Install` 打包并安装完整目录到应用私有的 `app-skills/caelis-bot-memory/`。
`botskills.Instructions` 从同一份 frontmatter 生成短技能目录，宿主仅向驻留会话传递。
没有正文、references 或 MEMORY 的自动注入，也没有第二份固定秘书行为 prompt。

Codex 和 Caelis 均使用公开的会话 instructions 承载这份应用技能目录，用原生文件工具
按需读取文件。实际模型请求验收确认 description 可见、正文最初不在上下文中，正文和
任务 reference 只有在相应读取后才进入上下文。目录不经过用户环境 skill 列表的预算裁剪。

这是应用范围的文件技能目录，不是注册到 Codex `skills/list` 或 Caelis 内置 `Skill` 工具。
Caelis 的 application 会话不会继承全局 skills；现有公开 instructions + 原生 Read 已足以
实现所需的渐进读取，不需要新增 Runtime 私有调用、启用 `inherit.skills` 或全文注入。
这进一步明确了早期方案中“标准 loader”一词：本实现复用文件技能模式与原生文件读取，
没有引入另一套 Bot 专有加载工具。用户可选择的普通技能仍由原生 `skills/list` 提供。

目录不装到用户全局 skills，不位于 worker 的共同祖先；worker 的会话 instructions 也不含
Bot 目录。隔离由宿主装配和 Runtime 会话落实，不在 Bot-facing 技能中描述开发者部署规则。
应用更新技能不会修改 Notebook 的 MEMORY.md 或日期笔记。

## 技能结构

```text
caelis-bot-memory/
  SKILL.md
  references/
    memory.md
    tasks.md
    reminders.md
    proactive-care.md
    expression.md
```

根文件只保留核心记忆恢复、持续身份、日常工作方式和条件路由。辅助模块按需展开对应
能力，不复制实时工具 schema，不要求每轮读取所有 references。新增能力优先扩展对应
模块；需要新模块时增加明确的加载条件。所有文案使用英文、直接面向 Bot。

委派 prompt 直接传递任务正文。宿主继续保留来源、所有权、原生权限和生命周期记录，
不向每个任务重复发送三份请求、JSON 包装和通用限制。Bot 从任务模块学习保留用户真实
约束、合理拆解、复用任务、核对结果和恢复未完成工作。

## 验证与边界

- 单元测试检查完整目录打包、metadata/body 分离、relative references 可达和驻留/worker 装配隔离。
- 安装版 Codex App Server + 本机合成 Responses 验证实际 provider 请求：只有 metadata，
  原生 exec_command 读取根文件后得到正文，再读 references/tasks.md 后得到该模块；同一
  Runtime 的 worker 请求没有 Bot skill。所有配置、数据均在临时目录。
- 安装版 Caelis Host + 本机合成 provider 同样验证 native Read 渐进读取、独立 worker
  无 Bot skill/记忆，以及既有配置更新与重启恢复。
- 合成 provider 验证传输与原生读取能力，不证明所有真实模型每次都会主动读技能。
  首次工作、上下文丢失和压缩后的重读要求由常驻 description 提供；真实模型的长期
  遵循效果仍需日常使用观察，不承诺绝对不失忆。

`AGENTS.md` 已规定：新增或调整功能时检查是否需要更新此技能，并在交接中说明结论。
