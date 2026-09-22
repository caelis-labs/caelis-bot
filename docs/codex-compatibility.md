# Codex App Server 兼容策略

用户本机 CLI 不按发行版本设白名单、最小值或最大值。较旧、较新、预发布版本只要满足
所需协议就可接入。自动发现、手动路径、共享 Unix socket 使用一致的协议判断。
`toolchain.json` / `TestedVersion` 的 0.153.4 仅为仓库 schema 与回归证据的可复现基线。

当前官方 App Server 并未在 initialize 中协商独立的 protocolVersion，也没有返回完整
服务端能力列表。clientInfo.version 是客户端版本；schema 的 v1/v2 目录也不能作为
已协商的协议版本。不能用 CLI 版本号替代它们，或虚构支持的数字协议范围。

实际兼容切面：

- 连接通过标准 initialize → initialized，保留 experimentalApi 客户端能力声明。
  检查 userAgent 的必要形状，不要求未使用的 codexHome/platform 元数据；新增字段忽略。
- 配置检测额外只读 account/read，校验所需认证字段后才允许保存。检测不发起模型请求；
  账户记录不代表 token 有效性或模型调用权限。握手通过不等于全部扩展均通过验收。
- 真正连接 Bot 时继续通过 thread/start 或 thread/resume 等原生接口验证当前所需语义。
  缺失方法、参数不被支持、必要字段不合法均明确报错；不为兼容而改用宽松审批策略，
  不重新发送未知结果的 prompt/审批，不丢弃原有绑定。
- 扩展字段与不关注的通知可忽略；未知服务端请求仍明确返回 -32601。新的审批选项或
  权限语义不能根据字符串或角色反馈推断。experimentalApi 不表示支持所有实验能力。

开发验证保留一份固定 schema，make schema 仍要求对应基线生成器并审查差异。
运行时和无模型 smoke 不调用 --version 作为准入条件，smoke 的 testedCodex 只报告测试
基线。契约回归覆盖不同发行号但协议相同、没有版本命令、最小/扩展握手响应可以接入，
以及基线发行号但协议错误仍拒绝、配置不保存、自有进程被回收。
这些 fixture 证明判断切面，不冒充每个真实历史/未来版本的完整功能验收。

依据：[官方 App Server 初始化与能力声明](https://developers.openai.com/codex/app-server/#initialization)
及仓库内固定 InitializeParams/InitializeResponse schema。未来官方若增加明确的协议版本
协商，再以官方字段扩展兼容策略；不提前猜测字段或版本含义。
