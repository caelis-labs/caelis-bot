# 微信 Remote Channel 首轮只读可行性调研

日期：2026-10-10。Bot 工作树 HEAD：`8a623f54f4e430681cf29b49a5928f0d18f78916`。范围：唯一候选 `@tencent-weixin/openclaw-weixin-cli`，未登录、扫码、安装、发送或修改产品代码。

## 结论

**技术上可做一个文本优先的薄适配；当前不能把独立客户端方案认定为官方支持、可直接发布的微信渠道。** 候选 CLI 是 OpenClaw 安装器，不是可独立收发的 CLI/SDK。实际收发实现为 `@tencent-weixin/openclaw-weixin`，作为 OpenClaw Gateway 插件运行。腾讯仓库公开了基于插件客户端源码写成的 HTTP JSON 协议说明，但明确限定它不能代表完整服务端合同。公开材料未给 Caelis Bot 这种独立桌面客户端的授权、App ID、频率、主动发信、撤销等保证。腾讯仓库中询问独立桌面客户端接入的 [#265](https://github.com/Tencent/openclaw-weixin/issues/265) 截至本次查看仍无维护者回复；这不能证明允许或禁止。

建议将 feature 拆成 **离线合同/模拟服务 POC → 官方边界确认 → 经用户授权的单账号实机 POC → 产品实现**。首版只做本人私聊文本及最终回复；不安装或启动 OpenClaw，也不执行安装器。若必须依赖未承诺的 iLink 客户端端点，明确记录其上游升级和服务策略风险，发布前取得官方许可/支持边界依据。不要把 MIT 源码许可解释成后端服务接入许可。

## 包与证据

从 npm registry `latest` 元数据读取并把 tarball 下载到任务私有目录 `/private/tmp/caelis-weixin-research.7n3125/`，未执行脚本或加载 JS。SHA-512 与 registry `dist.integrity` 完全匹配；完整性只证明与 registry 当前发布字节一致，不证明代码安全、发布者身份或服务许可。

| 包 | 已发布版本 | registry SHA-512 | 发布内容与依赖 |
| --- | --- | --- | --- |
| [`@tencent-weixin/openclaw-weixin-cli`](https://registry.npmjs.org/@tencent-weixin%2fopenclaw-weixin-cli/latest) | 2.1.4 | `sha512-+hHCsfLqwFSd4pPyB9tp++/agWFrWZeRNj/Zy4FZDd1N7BlYoQA0FOLen/wbv3liM+CVZYGCXgDA5kvpF7prEg==` | 6 个文件，`weixin-installer` → `cli.mjs`，`engines.node >=22`，MIT；没有 install/postinstall 脚本。`install` 会检测 OpenClaw、按宿主版本选可变的 npm dist-tag、运行 `openclaw plugins install`、交互登录、重启 Gateway；部分旧版还改 OpenClaw 扩展符号链接。 |
| [`@tencent-weixin/openclaw-weixin`](https://registry.npmjs.org/@tencent-weixin%2fopenclaw-weixin/latest) | 2.4.9 | `sha512-SfaYehR1Cwq2VV5HxJBp9sVilMms420VfZlMbF4YjRbWomr5+GxfXp9HkeU6y5TbnOc4Ysq0qPw1yBvJwbenBA==` | 125 个文件，TS 源码、编译 JS 和插件 manifest，MIT、Node `>=22.13.0`、`openclaw` peer `>=2026.5.12`，直接依赖 `qrcode-terminal` 0.12.0 与 `zod` ^4.3.6；没有 install/postinstall，`prepublishOnly` 用于发布前构建。无平台专属二进制、`os` 或 `cpu` 限制。 |

同一个 [`tencent-weixin` npm 组织](https://www.npmjs.com/org/tencent-weixin?activeTab=packages)列出这两个包；插件有 [Tencent 仓库](https://github.com/Tencent/openclaw-weixin)、[v2.4.9 release](https://github.com/Tencent/openclaw-weixin/releases/tag/v2.4.9) 和 [MIT LICENSE](https://github.com/Tencent/openclaw-weixin/blob/main/LICENSE)。这些关联是较强的来源证据，但包名和组织名本身不能当安全审计。CLI tarball 没有给出可独立收发接口；其发布 `cli.mjs` 是安装/登录/重启编排程序。上游 [README](https://github.com/Tencent/openclaw-weixin/blob/main/README.md) 同样要求先安装 OpenClaw，并提供手动安装真正插件的命令。

版本差异：CLI 2.1.4 的兼容矩阵把 OpenClaw `>=2026.3.22` 指向 `latest` 插件，安装时解析为当时最新版本，不是固定 2.4.9。插件 2.4.9 自身运行时检查下限为 `2026.3.22`，但 npm peer 与 manifest 安装下限为 `2026.5.12`；开发测试版本是 `2026.8.1`。`>=` 不能保证后续 OpenClaw 兼容，[2.4.9 更新记录](https://github.com/Tencent/openclaw-weixin/blob/main/CHANGELOG.md)就修复了 2026.9.x 入站回复故障。任何 POC 都应锁定两个版本和 tarball integrity。

## 账户、协议与运行依赖

- **账户形态。** QR 登录返回 `ilink_bot_id`、扫描者 `ilink_user_id`、`bot_token` 和可选 `baseurl`，是微信 ClawBot/机器人通道，不是读取或代发个人微信普通会话。插件 manifest 只声明 `chatTypes: ["direct"]`，多 bot 账号可用，但 Bot 首版应限制一个绑定账号、一个经本机确认的发信人。上游 [OpenClaw 微信文档](https://github.com/openclaw/openclaw/blob/main/docs/channels/wechat.md)也只宣传私聊/媒体，不宣传群聊。
- **扫码与凭据。** `POST /ilink/bot/get_bot_qrcode?bot_type=3`，随后 `GET /ilink/bot/get_qrcode_status`；状态包括等待、已扫描、验证码、过期、IDC 重定向、已绑定和确认。已发布插件把 token、base URL、扫描者 ID 写进 OpenClaw 状态目录的 `openclaw-weixin/accounts/{accountId}.json`，尝试 `0600`；`accounts.json`、轮询 cursor、各会话 `context_token` 又分文件保存。它不是 macOS Keychain。源码有本地 `clearWeixinAccount`，却未发现对外的 `auth.logout` 或服务端撤销 API；`notifyStop` 只是停止通知，不能视为撤销授权。Bot 后续应由原生安全存储保存凭据和 token，停用立刻取消轮询/发送，移除时清理本机 token、cursor、context token、授权绑定；服务端撤销路径必须实机/官方核实，不对用户承诺本地删除即服务端失效。
- **收发。** 官方仓库 [协议说明](https://github.com/Tencent/openclaw-weixin/blob/main/docs/protocol.md)列出 HTTPS JSON：`getupdates` 为带 `get_updates_buf` 的长轮询，`sendmessage` 带 bot token、目标 `@im.wechat` ID、`client_id`、`context_token`、消息体；`getconfig/sendtyping` 提供打字状态。没有 webhook、编辑已发消息、按 `client_id` 查询回执的公开端点。来源消息 `message_id` 为可能超过 JS 安全整数的 ID，发布源码专门按字符串无损解析。不要用 JSON 浮点数做去重键。
- **稳定性边界。** 官方协议文档自己说明：这些是当前客户端类型/请求构造，不是完整服务端合同；示例并非已验证的最小请求。`sendmessage` 的 HTTP 成功/`ret=0` 也不是用户可见投递回执；腾讯仓库有[用户报告 `ret=0` 但手机未收到](https://github.com/Tencent/openclaw-weixin/issues/290)，该报告不能推广为所有账号故障。源码发信每次生成新的 `client_id`；无服务端幂等/查询保证，未知结果不能新 ID 重发。缺 `context_token` 时插件仍尝试发送，但官方文档不保证服务端接受。主动消息、回复窗口与限频均需实机及官方确认；不要首版承诺计划提醒能主动推送。
- **OpenClaw 依赖。** 已发布插件入口导入多个 `openclaw/plugin-sdk/*`，消息处理要求 Gateway 注入 `channelRuntime`，并调用其授权、路由、会话与回复调度。插件 `package.json` 无独立 SDK 的 `exports`/`main` 承诺。直接 import `dist/src/api` 也会经账户/状态模块引入 OpenClaw，升级时内部路径不稳定。所谓“CLI 独立使用”只适用于安装器的可执行形式，不适用于渠道收发。
- **Node 与平台。** Bot 的 Node 24.21.0 是当前开发/前端构建工具；`script/build.sh` 发行资源未放入 Node runtime。运行原插件必须另有 Node 与 OpenClaw Gateway，不能假定用户机器已有开发环境。若实现独立的原生 Go 薄客户端，可不增加运行时 Node；只复用 MIT 源码所公开的请求/状态机设计并保留版权声明，仍受服务端合同风险约束。两包本身没有 macOS/Windows 架构二进制门槛，但 Bot 只发行 macOS；Windows 11 x64 UI/安装/安全存储/实机验收仍独立未完成。

## 对照现有 Bot 的最小范围

本工作树 `docs/product.md` 与 `docs/architecture.md` 定义一个常驻 Bot 身份。`internal/telegram` 已有单私聊配对、原生 `backend.SubmitRemote`、接收队列与 cursor 同步落盘、原始请求/回执、输出 ledger、停止/审批、安全存储。`frontend/src/MessagingSettings.tsx` 是渠道总览，`TelegramSettings.tsx` 是详情，`internal/desktop/telegram.go` 提供原生设置方法，`internal/app/app.go` 装配 Bridge；`internal/backend/remote_input.go` 拦截恢复中或未初始化输入。微信可沿用这些 **边界/端口**，不能直接复用 Telegram 的消息模型、编辑 API 或账号凭据。

1. 新的 `internal/weixin` 传输只处理二维码状态、一个账号/发信人绑定、取消式长轮询、持久收件队列、文本解析、cursor 与 `context_token`；用持久 `bot_id + message_id` 构造原始入站 request ID。入站消息先原子保存和去重，再推进 cursor；每次提交走 `SubmitRemote` 的原始回执核对。已发布上游 monitor **先保存 cursor，再调用消息处理**，不能直接继承这一丢失窗口。恢复前明确拒绝与已派发但结果未知必须分开；未知结果只查原收据。
2. 复用同一 resident Bot 的普通自然语言入口；现有后端的 Worker 协调可由 Bot 自行决定，微信不需要线程/工作区/模型新导航。只允许本机确认的扫描者私聊触发。微信发信人身份是传输授权事实，不注入人格/任务身份；本地草稿不被远端输入覆盖。第一阶段审批继续由 Mac 原生面板处理，远端仅给简短状态，不用生成文字模拟批准。
3. 发信先做 **最终文本一次发送**，大文本需经限制内分段且每段都有稳定 delivery key；打字状态可做独立短生命周期控制。不存在已证实的 Telegram 式编辑 API，因此不要设计“流式可编辑”承诺。若后来验证可发送生成态/完成态，另行定义回执和中断语义。所有发送先落盘意图、记录一次派发与 `client_id`；明确失败才能安全重试，网络超时/崩溃/服务端 `ret=0` 但无用户可见确认应展示“结果未确认”，不自动重复发。
4. 渠道总览加真实微信状态与详情；二维码、验证码、过期/已绑定提示、本机确认、暂停/恢复/移除与连接错误应由 native DTO 驱动。token、二维码授权材料、context token 不进入渲染 DTO/日志；macOS 用 Keychain，Windows 后续用系统私有凭据存储。移除时清除本地状态，提示服务端撤销尚待核实。未来若上游明确支持，再扩媒体/引用/主动推送。媒体需要 CDN 上传下载及 AES，首版不接。
5. `internal/botskills/skills/bot-core/` 在真正新增微信能力时应补充远端工作与恢复指导；本轮没有行为变更，无需改。验证按 `make check`, `make smoke`, `make build`，原生设置 UI 必须在测试数据实例实际查看；Windows 不由 macOS 构建推断。

## 未确认项与 POC 验收

**官方边界：** 腾讯是否允许不以 OpenClaw 为宿主的独立客户端直接访问 iLink，`ilink_appid=bot` 的适用范围、是否需要独立 App ID/审核、账户地区灰度、频率和授权撤销流程均无一手肯定答案。MIT 解决源码复制/改作的版权许可，不解决服务端使用权或微信账号服务条款；须取得官方依据。对独立客户端的稳定性与长期维护应按未公开服务接口管理：锁版本/兼容矩阵、监测上游协议变化、保留停用开关和降级路径。

**技术边界：** 扫码返回字段、`context_token` 生存期/绑定对象、主动发送窗口、`client_id` 是否幂等、重复消息或 cursor 重置、服务端限速/大小、可见投递确认、引用消息支持与媒体文件上限，都需要后续实测；源码或 TypeScript 类型不足以证明。用户可否在微信侧撤销并立即使本地 token 失效，也待验。

**下一轮小 POC（本轮不执行账号操作）：**

1. 无真实账号：固定 2.1.4/2.4.9 证据与许可证；用本地模拟 HTTP 服务验证二维码状态机、重定向、验证码、长轮询断线、入站重复/乱序、cursor 与收件记录原子性、非本人拦截、原始回执恢复、未知发送不重发和停用取消。
2. 官方支持/许可有依据后，用户显式选择隔离测试账号，在 Dev 数据目录由用户本人扫码；观察 bot ID/扫描者 ID、同一消息只提交一次、自然语言与 Worker 委派/回报、最终文字一份、长文本、断线重启、暂停/移除。只记录脱敏 ID/时间/原始收据，不记录私聊内容或 token。
3. 实机矩阵单独验证：主动提醒与时间窗口、限流/错误码、旧 context token、媒体/引用（若进入下一阶段）、微信侧撤销、macOS 原生设置窗口与签名包行为；Windows 11 x64 等产品具备原生外壳后再验。

本轮未运行 `make check/smoke/build`：没有代码/配置行为变更，且不得启动生产 Bot 或做账号验收。仅完成 npm 发布包 integrity 校验、静态源码阅读及工作树状态核对。
