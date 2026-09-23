# Caelis 通用应用联调验收

历史确定性基线。最新 `4a3c059` 真实模型结果及修复见 [实测报告](caelis-live-acceptance.md)。

日期：2026-09-23。状态：本地工作树已完成适配与下面列出的验收，未提交、未发布。
保留已有改动，没有改动 Caelis 源码、日常 Store 或安装，也没有恢复旧 Bot Mode。

## 固定输入与验证层级

- Core 源码：`3e4675abe595f4fbe29a79811fecbbf6ab892371`。
- 固定二进制：`/tmp/caelis-session-completion-pdsramj_/caelis`。
- 二进制 SHA-256：`d24c3d4602f000f1aac8006c38cf679d31c23948fb683c01b23f3cc9744e87ca`。
- Core 报告：`/tmp/caelis-session-completion-pdsramj_/implementation-report.md`；二进制在提交前构建，
  其旧 dirty build identity 不作为源码不一致的判断依据，按已核对的源码提交与摘要固定。
- Bot 协议清单：`protocol/caelis/manifest.json`；公开 Go wire、OpenAPI 原样固定，仅生成包名为 `wire`。
- 本轮使用 Bot 自身 HTTP/SSE adapter 驱动独立 Host。临时 HOME、Store、Notebook 和 worker 目录；
  provider 是 loopback 合成服务，名为 `openai/gpt-5.4-mini` / `openai/gpt-5.4` 的测试配置，**不是实际调用这些模型**。
- 测试真正执行 macOS 的原生文件和命令工具；故障注入单测另检验丢响应、恢复和来源边界。
  真实模型、真实 GUI 操作、Windows 和发布安装不在本轮通过范围内。

## B01–B12 对照

编号含义沿用 [续作 handoff](caelis-application-runtime-followup-handoff.md)。
“通过”仅针对该行明示的验证层级，不表示真实模型、所有故障组合或整个产品发行通过。

| 编号 | Bot 侧结果 | 证据与界限 |
| --- | --- | --- |
| B01 | macOS 原生通过 | `TestNativeHostIntegration/B01_B02_native_notebook`：原生 Write/Read/RunCommand，核对实际 MEMORY.md 和命令产物字节；握手要求六项能力，不使用 tools-only 替代 |
| B02 | Host + 原生通过 | 同测试绑定 Notebook CWD，写核心文件/日期笔记、下一 Turn 读取真实内容、INDEX 自动刷新；CWD AGENTS 哨兵不进入请求；worker 无秘书指令/Notebook 上下文。普通 CLI/TUI 不受影响仅采用 Core 报告证据 |
| B03 | 主要配置通过；Fast 成功路径待验 | `B03_B04_B05_B06_hot_configuration`：同一 Session 实际请求中的 model、reasoning.effort、instructions、工具 schema 更新。测试 endpoint 不支持 priority，只验证拒绝；不把此结果称为 Fast 可用 |
| B04 | Host 确定性交错通过 | 在模型请求入口阻塞旧请求后提交配置，返回期望 revision 与旧 last_request；释放旧请求后同一 Turn 下一请求采用新 revision，原 Turn ID 保持 |
| B05 | Host + 故障单测 + race 通过 | 旧目录调用只执行旧 handler 一次；新 schema 进入下一请求；跨 Turn 重用 provider call ID 不合并 native invocation。丢 claim 不执行，丢 result 只补回执，伪造 item 被拒绝 |
| B06 | 请求/配置证据通过 | 同值 update revision 不变；原生 Notebook 写读前后配置 revision 不变，不强制 compact。仅证明调用语义，不声称供应商 KV Cache 命中 |
| B07 | Host 重启 + 故障单测通过 | `B07_restart` 保持 Session、revision 和历史；`TestConfigurationLostResponseKeepsExactReceiptAndExplicitClear` 丢响应后只读原 operation 精确回执，历史回执不回滚最新缓存；显式清空数组、冲突和 unsupported 均有测试 |
| B08 | Host 双 worker 通过 | `B08_B09_B11_workers_background` 在独立目录启动两个并发 worker；取消 A、B 保持执行；adapter detach/reopen 后 B 完成，结果和原生归属保持 |
| B09 | Host + 产品单测通过 | 用户 callback 获取 grant，后台 prompt 来源为 authorized_background；同 ID 激活实际模型请求只有一次，撤销后新激活拒绝。`TestGrantedRemindersDispatchIndividuallyWithoutUserSubmit` 验证两条到期计划分别唤醒，不走用户 Submit |
| B10 | Host + macOS 字节闭环通过 | `B10_resources`：上传 → ReadResource → Read/RunCommand → PublishArtifact → Bot 下载项 → 公开下载；验证字节/size/SHA-256，同 Host 另一应用读取被拒绝；错误摘要不返回字节。验证对象是常驻会话，worker 产物汇报仍有产品缺口 |
| B11 | Host + 故障恢复通过 | detach 不取消 B、不关闭 Host；取消 A 使用精确 target；`TestWorkerStartRecoversLostCreateAndPromptWithoutRedispatch` 跨 adapter 重建恢复创建和 prompt，两次 POST 各仅一次；未知用户 prompt 只查询不重发 |
| B12 | Bot 全仓回归通过；Core 范围引用上游证据 | `make check`、相关 Go race、`make smoke`、`make build`。普通 CLI/TUI/ACP、Workspace Memory、Core 审批/replay 的回归来自固定 Core 实施报告，本轮没有重复运行 sibling 仓库测试。真实登录/模型、GUI/Windows 未验 |

## 修复及落点

1. **通用协议接入**：移除桌面上的旧 Bot 专用执行实现，更新 schema、wire、六项能力协商，
   接通 `BotToolBinder`、`WorkRuntime`、`ReportSubmitter` 与后台授权端口。设置不再固定显示“未接入”。
2. **原生 Notebook**：应用绑定 Notebook 目录与专属 skill；文件行为使用 Caelis 原生工具。
   worker 绑定独立目录和工作角色，不继承秘书身份内容。
3. **热配置回执**：新增 `configuration.go`，区分持久化期望与 last_request；保存空数组语义；
   旧工具版本按原调用路由，HTTP 500 + unsupported 作为明确拒绝，避免永久挂在未知更新。
4. **任务与后台来源**：`workers.go` / `background.go` 保存启动各阶段和授权事实；丢创建/prompt 回执
   可按原操作继续恢复。修复产品提醒批量合并与单 grant 激活冲突，其他到期计划留待后续空闲 tick。
5. **资源与呈现**：`resources.go` / `projection.go` 接入上传、摘要校验与下载缓存。
   原生 tool content 是数组，消息 content 是对象；PublishArtifact 的 JSON 文本在 rawOutput.result 中，
   旧的统一解析遗漏了下载项。已按 union 修复，投影版本升级仅重建派生历史，不删除身份或 intent。
6. **持久化大小**：确认的 typed mutation 只留 digest/receipt，清除上传 base64 请求体，
   避免每个已成功附件永久扩充绑定文件；未知操作仍保留原请求供核对。

主要 owning tests：

- `internal/backend/caelis/integration_test.go`
- `internal/backend/caelis/application_test.go`
- `internal/backend/caelis/session_test.go`、`setup_test.go`
- `internal/bot/runtime_test.go`

测试过程中发现的 artifact 映射和提醒合并失败已修复；新加的 schema 断言最初误按
`type: integer` 匹配，而 Core 的 Responses 严格 schema 将可选项投影为 `type: [integer, null]`。
已按实际公开 provider 投影修正断言，未放宽“必须采用 integer 新版目录”的要求。

## 复现命令与本机日志

```bash
cd /Users/xueyongzhi/WorkDir/caelis-labs/caelis-bot
GOWORK=off make check
GOWORK=off make smoke
GOWORK=off make build
bash -c 'source script/env.sh; GOWORK=off go test -race ./internal/backend/caelis ./internal/bot ./internal/app'
CAELIS_BOT_TEST_BINARY=/tmp/caelis-session-completion-pdsramj_/caelis GOWORK=off make smoke-caelis
```

本机临时日志（不含日常会话或真实凭据，临时文件不属于仓库交付）：

- `/tmp/caelis-bot-3e4675-check-final.log`
- `/tmp/caelis-bot-3e4675-native-final.log`
- `/tmp/caelis-bot-3e4675-race-final.log`
- `/tmp/caelis-bot-3e4675-smoke.log`
- `/tmp/caelis-bot-3e4675-build-final.log`

构建产物为 `dist/Caelis Bot.app`，继续使用现有 ad-hoc 签名。保留既有前端 chunk 大小和 macOS
重复 `-lobjc` 非阻断提示。没有启动日常 App，因此构建成功不代表本轮已验证原生 UI。

## 剩余契约问题与后续验收

| 项目 | 当前行为 / 建议 |
| --- | --- |
| Fast / unsupported 错误 | 合成 endpoint 的 priority 返回 HTTP 500 + code unsupported、泛化错误文本；Bot 已识别拒绝。Core 建议返回清晰 4xx 与支持信息；需支持该能力的真实模型验证成功路径 |
| 上传回执完全丢失 | 资源 ID 不透明，Core 资源 API 只有按 ID 读取，没有按 upload operation 的只读查询；Bot 不猜 ID、不自动重传。建议增加 scope 内的上传操作回执查询；这不阻断正常文件闭环 |
| 原生命令未知回执 | cancel/approval 结果不明仍保留未知，不通过新 ID 绕过；需继续完成公共命令恢复链的专项验收 |
| 创建时权限绑定 | 当前模型/指令/工具可实时更新；CWD、inherit、execution、permissions 仍 creation-bound。产品只提供 workspace-write/manual，不静默放宽；如未来需要实时改审批策略，要另补契约 |
| 在途追加与产物汇报 | 当前拒绝 active Turn steer；worker 的 PublishArtifact 已投影，但有限完成报告尚未带主对话下载项，需 Bot 产品层继续补齐 |
| 真实模型 | 本轮未执行。`smoke-caelis-live` 目前仅测试显式隔离 Host 的 Notebook 原生写读；完整热配置、双 worker、授权、资源与失败恢复仍需真实 provider 验证 |
| 平台与发行 | Windows 只有共享边界/编译守卫，无原生实现；未改 Developer ID stash、未 tag/release、未升级日常 Runtime |

本轮采用本机个人助手信任模型，保留应用归属、原生审批、幂等和日志脱敏；没有把同用户进程级
凭据完全隔离重新设成使用门槛。上述限制不通过恢复旧 Bot Mode 或读取 Core 私有 Store 规避。
