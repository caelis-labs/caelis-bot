# Caelis v0.61.0 正式版联调

2026-09-23 至 2026-09-24，macOS ARM64。使用用户已经安装的正式 Caelis，未重编译 Core、
未导入兄弟仓库，也未更换日常 Runtime。此前聊天窗口与透明背景修复已提交为 `7114e40`。

## 发行身份与协议

- 安装路径：`/Users/xueyongzhi/.local/bin/caelis`。
- 官方发布：[v0.61.0](https://github.com/caelis-labs/caelis/releases/tag/v0.61.0)，非 draft、非 prerelease。
- commit：`5e2546f4954eab1d0f8fcfcc57f54939ad465125`。
- CLI 与公开 `/initialize` 均返回 `v0.61.0`、`build_kind=release`，BuildID 为
  `5e2546f4954eab1d0f8fcfcc57f54939ad465125@2026-09-23T15:08:15Z`。
- 本机二进制 SHA-256：`2188fe1b10dc0bb8ec9675b1dbf33b66e401266c9758256a49c388a8dddb99e9`。
- 官方 `caelis_0.61.0_darwin_arm64.tar.gz` SHA-256：
  `32be7577309e91c3e524c1acabce4fa9cad7c243979d098ef041463709790b9a`。
  下载包匹配发布页 checksums，包内二进制匹配本机文件；未执行覆盖安装。
- 从正式提交读取公开 OpenAPI 与生成 Go wire，wire 仅变更 package 名后与本仓库逐字节一致。
  manifest 推进至正式提交，schema/wire 哈希无需变化。六项 application 能力协商全部存在。

## 已通过范围

| 路径 | 实际证据 |
| --- | --- |
| 隔离 Host 与合成 provider | `TestNativeHostIntegration` 开启 race、禁用缓存，5 个子项全部通过，11.63 秒；真实 macOS 文件/命令、热配置、双 worker、background grant、资源闭环及 Host 重启 |
| 完整 Bot 工具目录 | live fixture 注入全部 9 个产品工具，实际调用无参数 `bot_clock` 一次；同时保留专用验收回调，避免只测合成 schema |
| 真实模型 | MiMo `mimo-v2.6-flash` 与 Codex `gpt-6-luna`，最终 7 个子项全部通过、无跳过，111.25 秒，开启 race |
| Notebook | 原生读写 MEMORY 与日期笔记、INDEX 完成刷新；普通文件更新不改变配置 revision |
| 资源与审批 | 上传、ReadResource、原生命令、PublishArtifact、下载字节/SHA-256 一致；待审批重连保留精确目标，只执行一次测试 marker 命令 |
| 并行与恢复 | 两个真实 worker 同时运行，取消 A 后 B 完成；秘书绑定保持；grant 激活幂等，撤销后拒绝；Host 重启后原会话继续真实读取笔记 |
| 热配置与 Fast | 同一 Turn 下一请求从 MiMo 切 Luna、指令/工具版本切换各执行一次；同值 patch 不增加 revision；Luna priority 有实际回执与模型完成结果 |
| 原生 GUI | 自动发现 v0.61.0、选择独立 Store、启用并重启；修复后完成身份写入/读回和产品 `bot_clock`，聊天出现 `RELEASE_GUI_OK` |
| GUI 模型设置 | 设置中切换 Luna / low / Fast 并保存；聊天出现 `RELEASE_LUNA_FAST_OK`，公开回执确认 `gpt-6-luna`、`low`、`priority`、revision 3 |
| 常规回归 | `make check`、`make smoke`、相关 Go race 全部通过；经 `script/build_and_run.sh --verify` 完成原生构建和启动，实际检查窗口 |

Fast 验收仅证明原生请求配置与真实完成，不证明供应商账单档位或速度提升。

## 联调发现与修复

首次运行时，介绍提交已被接受，但请求重试后失败，界面只留下用户消息。
这次失败没有计作成功初始化，也没有自动重复发送原始介绍。

1. `bot_clock`、`bot_tasks` 的无参数 schema 将 Go nil slice 编码成 `required:null`，不符合 JSON Schema。
   改为合法空数组；新增完整目录 required 字段回归，并让真实模型 fixture 携带产品目录。
   修复后在同一 GUI 绑定中明确发送继续初始化请求，原生 MEMORY 文件含 `Caelis Release QA`，
   `bot_clock` callback 完成，真实回复成功。
2. Caelis adapter 原先丢弃 `caelis/error` 和 failed lifecycle reason，导致请求失败时没有可见解释。
   现在保留当前请求失败原因，聊天显示失败；重新连接仍可恢复，新请求开始后清除旧错误，
   participant 与审批审查错误不污染秘书状态。没有详细错误的 failed head 也有通用提示。
   投影版本推进至 4，仅重建派生内容，保留原生绑定、身份和操作日志。

## 隔离与证据

真实模型使用原先已配置的隔离 Store：`/private/tmp/caelis-bot-live-4a3c059/store`。
没有从日常目录复制新凭据；测试创建临时 HOME、Notebook 和 worker 目录，只有合成测试内容。
GUI 使用 `/private/tmp/caelis-bot-release-gui.XyVHB9`，测试后恢复日常 Bot，关闭测试专属 Host。
日常 Runtime 选择、认证、对话和 Notebook 未覆盖。没有注册新的系统服务或永久提醒。

本机证据位于忽略目录，不随代码提交：

- `.cache/caelis-v0.61.0-release/verification.json`：官方包与本机二进制摘要。
- `.cache/caelis-v0.61.0-native-final.log`、`.cache/caelis-v0.61.0-live-final.log`：最终联调。
- `/var/folders/hn/r4ffst5510s89657cwrcjj6w0000gn/T/caelis-bot-live-evidence-3759264516/evidence.json`：
  正式 initialize、各子项结果、完整产品目录、热配置/资源/审批/重启证据，不含凭据。
- `.cache/caelis-v0.61.0-gui-replay.json`：首次失败的测试会话回放。
- `.cache/caelis-v0.61.0-gui-receipt.json`、`.cache/caelis-v0.61.0-gui-success.png`：实际模型配置与窗口。
- `.cache/caelis-v0.61.0-check.log`、`.cache/caelis-v0.61.0-smoke.log`、
  `.cache/caelis-v0.61.0-gui-fixed-launch.log`、`.cache/caelis-v0.61.0-restore-daily.log`：检查及原生构建/恢复。

重跑命令见 [接入指引](caelis-integration.md)。不要上传隔离 Store，其模型认证仍是私有数据。

## 保留边界

这轮验证官方二进制的消费路径，不重复测试官方安装器/更新器；Bot 仍为 ad-hoc 签名预览，未发布。
新建 Bot 的介绍表单与运行时选择已经实际走过；修复后的身份写入验证是同一绑定中的明确继续请求，
没有将首次失败改写成全新安装一次成功的证据。
GUI 覆盖设置、聊天与模型切换，文件选择/审批点击/附件下载等其余原生路径未在本轮全部复跑。
系统物理快捷键、长期常驻、外接显示器和 Windows 也不由本轮结果代替。

上传响应完全丢失后的 opaque ID 恢复、cancel/approval 未知结果的完整恢复、worker 产物汇总为
主聊天下载项、active Turn steer 等既有缺口仍见 [接入契约](caelis-integration.md)。
完整秘书业务编排和长期用户使用仍需继续验证，不能将本轮有限成功等同产品全量发行验收。
