# CaelisBot 无桌面服务与 Web

本实现基于 Bot v0.17.1 / `2d94f101638a46321209c1ad5ebe2784d68f7d4c`。
一个常驻 `app.Application` 拥有 Bot、渠道、schedule、任务与回执；Web 与桌面使用同一套 React 聊天、设置、审批、问题卡片和样式。`desktop.ts` 选择 transport，不建立第二个 Runtime/Core Host。

## 本机开始

源码构建需要 Go（见 go.mod）、Node 24（见 package.json）和 Python 3；预构建归档无需这些工具。Linux 主机需提供 bubblewrap 和可用的非特权 user namespaces，Core 不会在沙箱不可用时降级执行。Rocky 体验环境已具备这些条件。不需要显示器、Wails、GTK 或 Docker。

```sh
./script/npm.sh ci
./script/build-headless.sh          # Linux arm64/amd64，默认构建主机架构
# BOT_HEADLESS_ARCH=amd64 ./script/build-headless.sh 可交叉构建
# 保留整个目录；也可解压 dist/headless/caelisbot-*-linux-*.tar.gz
./dist/headless/linux-arm64/caelisbot serve                    # 默认 127.0.0.1:9317
```

打开 `http://127.0.0.1:9317` 即可使用，无需配对。只有绑定 loopback 的监听器、真实 loopback TCP 客户端及精确 localhost Origin 同时满足，才自动建立 HttpOnly 会话；仍检查 Host、Origin 和 CSRF。任何 Forwarded/X-Forwarded-* 声明都不能建立本机信任。

远程访问才需要从启动提示的私有文件读取一次性配对码并填入页面。码有效 10 分钟，使用一次后销毁；过期可运行 `caelisbot pair`，仅输出文件位置。不要把码放进 URL、shell 参数、日志或共享文档。

- 默认数据目录 `$XDG_DATA_HOME/caelisbot`，未设则 `~/.local/share/caelisbot`；必须为 0700。`--data-dir /absolute/private/path` 可指定独立 profile。
- 一个目录只允许一个 headless 进程。**不要与桌面 App 共用正在使用的数据目录**，也不要给容器挂载用户整个 HOME/密钥目录。
- `Ctrl-C` 或服务管理器 SIGTERM 停止服务。关闭浏览器仅断开显示；已接受的 Bot/Worker 工作继续由原 owner 管理。
- `serve --web=false` 只运行后台服务。`pair` 必须使用与 `serve` 相同的 `--data-dir`。
- 归档和 Docker 均携带 `resources/caelis-runtime/release.json` 锁定的 Caelis v0.72.2。发行目录必须保留 `Helpers/caelis`、`Resources/Caelis-Runtime.json` 和许可证；构建核对归档、可执行文件 SHA-256 与 ELF 架构。选择默认连接后沿用 App 的自动准备和模型配置，无需另装 Caelis。
- 显式 CLI 路径优先，其次是程序自身发行目录，最后才是 PATH/用户安装。随包程序禁止自更新，随 Bot 完整归档/镜像更新；缺失或损坏时不悄悄改用系统 CLI。不会向系统 PATH 安装或覆盖已有 Caelis。
- 程序暂不可用时设置和恢复入口仍可达。所有检测、程序路径与数据目录均属于**执行主机**，不是浏览器电脑；Linux 不伪装桌面文件选择器。
- Codex 不随包交付。App 的自动安装器当前仅支持 macOS；Web 使用官方安装指引，在服务主机安装后重新检测。检测复用现有 PATH/用户目录和标准 App Server 握手。已安装时可在服务主机执行 `codex login --device-auth` 后重新检测，或在首启使用 API Key；已有认证直接进入模型选择。App 的浏览器登录依赖服务主机 localhost 回调，不把远程浏览器冒充该主机。API Key 只传给原登录操作，Web 不返回密钥。
- `--data-dir` 隔离 Bot 数据，不改变 CLI 的既有 Store 规则。需要完全隔离测试/服务时另设独立 HOME/CODEX_HOME；Docker 的 HOME=/data，Caelis Store 与可选 Codex 登录随专用卷持久化，不挂载生产用户 HOME。

## Docker：发行兼容待解决

镜像已包含锁定 CLI、CA 证书和 bubblewrap，但本机 OrbStack Docker 默认 seccomp/systempaths 保护阻止 Core 的内部沙箱启动；**尚未通过 Docker 完整验收，当前请用 Linux VM/Rocky 体验**。不能用 `--privileged`、`systempaths=unconfined` 或 Core 的跳过权限参数绕过。以下仅说明镜像构建与 HTTP 入口边界，不承诺 Docker 内模型/任务可用。

Docker 的 [seccomp 默认规则](https://docs.docker.com/engine/security/seccomp/)与 [bubblewrap 的 namespace 需求](https://github.com/containers/bubblewrap)需在后续发行兼容工作中协调；本轮未修改 Docker 或宿主全局安全配置。

### guest loopback 不等于 host loopback

容器的 TCP peer 通常是 NAT 网关，不能据此判断浏览器在宿主。提供两种入口：

- 简单认证入口：容器 `--listen 0.0.0.0:9317 --origin http://127.0.0.1:9318`，宿主 `-p 127.0.0.1:9318:9317`。此原始端口保留远程配对规则。
- **推荐本机免配对入口**：附带 `caelisbot-local-proxy` 在宿主 loopback 接收浏览器请求，向上面的 host-only 端口附加私有证明。此 relay 只转发 HTTP，无 Bot/Worker owner；拒绝外部 Host/Origin、非 loopback peer、转发头和浏览器伪造的证明。

```sh
docker build -f deploy/headless/Dockerfile -t caelisbot:local .
GOWORK=off CGO_ENABLED=0 go build -o ./caelisbot-local-proxy ./cmd/caelisbot-local-proxy
# 私有 proof 文件：两端使用同一份随机 32 字节十六进制值，0600；不要输出值。
install -d -m 700 ./local-profile
(umask 077; openssl rand -hex 32 > ./local-profile/proxy-key)
docker volume create caelisbot-data
# 仅初始化专用卷；短生命周期 helper 不联网，proof 保持 0600。
docker run --rm --network none --user 0 --entrypoint sh \
  -v caelisbot-data:/data \
  -v "$PWD/local-profile/proxy-key:/run/proxy-key:ro" \
  caelisbot:local -c 'cp /run/proxy-key /data/local-proxy-key && chmod 600 /data/local-proxy-key && chown 10001:10001 /data/local-proxy-key'
docker run -d --name caelisbot-web \
  -p 127.0.0.1:9318:9317 -v caelisbot-data:/data \
  caelisbot:local serve --data-dir /data --listen 0.0.0.0:9317 \
  --origin http://127.0.0.1:9317 --origin http://127.0.0.1:9318 \
  --local-proxy-key-file /data/local-proxy-key
./caelisbot-local-proxy --listen 127.0.0.1:9317 \
  --upstream http://127.0.0.1:9318 --key-file ./local-profile/proxy-key
```

浏览器打开 relay 的 `http://127.0.0.1:9317`，无需配对。proof 不发给浏览器、不入 URL/日志。停止 relay 用 Ctrl-C；停止/启动原服务用 `docker stop/start caelisbot-web`，保留数据卷。若已有 secret 管理器，可使用非 root 可读的只读 secret mount 替代初始化 helper。验收交付目录提供已配置的专用启动脚本。

按 [Docker 端口发布文档](https://docs.docker.com/engine/network/port-publishing/)，宿主映射必须显式为 127.0.0.1。**不要把 relay 放到容器内后发布到所有接口，不要将 relay 接到 Tailscale/公网代理**；远程代理走原始服务的认证入口。镜像已带 Caelis，未带 Codex 或真实凭据；可按官方方式扩展镜像安装 Codex，保持同一非 root 用户。不要挂载 Docker socket。

## VM / WSL

完整原生 Linux 发行目录为首选；VM 中运行与本机相同。可以使用 SSH 本地转发：

```sh
# guest: 默认 loopback；browser origin 为转发后的地址
caelisbot serve --origin http://127.0.0.1:9317
# host: 显式建立转发，不修改防火墙
ssh -N -L 127.0.0.1:9317:127.0.0.1:9317 user@guest
```

本轮 Rocky 体验采用更明确的上游边界：guest 仅监听 loopback 并启用 `--require-pairing`，宿主 SSH 仅转发 `127.0.0.1:19318`，上述 host-only relay 在 `127.0.0.1:19317` 加私有证明。浏览器本机免配对，直接访问原始转发端口仍需认证；不依赖 SSH peer=loopback 来信任任意代理来源。

也可显式 `--listen <guest私网IP>:9317 --origin http://<guest私网IP>:9317`，仅适用于可信宿主/VM 私网；配对仍必需。不得默认无认证监听全部网卡。

WSL 使用对应架构 Linux 发行目录。[Microsoft 网络文档](https://learn.microsoft.com/en-us/windows/wsl/networking)说明 localhost 与私网访问的区别。先在 WSL 内检查监听和页面，再在 Windows 浏览器实际验证 localhost 转发。若当前 WSL 网络模式没有转发，使用已验证的私网地址或 SSH 转发；本文不自动修改 WSL 网络、防火墙、登录或系统设置，也不宣称已完成 WSL 实机验收。WSL Windows GUI 桥接和 Linux/WSL DTW 电脑操作均不支持。

## Tailscale 等私网

推荐现有 [Tailscale Serve](https://tailscale.com/docs/features/tailscale-serve) / HTTPS 反向代理将私网 HTTPS origin 转给本机 loopback：

```sh
caelisbot serve --require-pairing --listen 127.0.0.1:9317 --origin https://your-machine.your-tailnet.ts.net
```

任何转发给远程客户端的 loopback upstream 都须启用 `--require-pairing`，不能把默认信任本机的入口公开。代理必须保留该外部 Host，转发 SSE，禁用响应缓冲，并只在私网提供服务。具体 Tailscale 命令应按本机已安装版本配置；本实现不安装、不登录、不开放公网 Funnel。后端不信任 `X-Forwarded-*` 来扩大允许 origin。

若显式监听 Tailscale IP，可配置精确 HTTP origin；流量安全依赖 Tailscale 加密，仍使用 CaelisBot 配对/会话认证。共享 LAN 或非加密远端链路用 HTTPS 代理。私网成员身份不自动等于 Bot 使用权限。

## 安全与恢复契约

| 边界 | 实现 |
|---|---|
| 浏览器权限 | 真实本机自动建立会话；远程一次性配对 → 7 天 HttpOnly / SameSite=Strict cookie；HTTPS origin 设置 Secure。没有账户平台或多租户 |
| 请求来源 | 精确 Host/Origin、拒绝 cross-site、写请求 CSRF + 自定义请求头；无通配 CORS |
| 网络 API | `internal/websurface/routes.go` 固定路径、签名和能力 allowlist；没有任意 Wails RPC、shell 或 Core API 代理 |
| 密钥 | 写入既有配置 owner；只持久化 ingress 参数摘要，原参数不入 Web 操作账本；会话 token 只保存哈希 |
| 写入不确定 | 每次操作携带 ingress ID；先落盘再调用既有方法。相同 ID+参数回读原结果，pending 不重放。原 Submit/审批/问题 request ID 原样传递 |
| SSE | 一份有界最新投影供多 tab；重连先 reset 再全量快照，保留原业务 ID，不声称是 durable event log。断流不取消任务 |
| 多 tab | 一个全局草稿，显式接管输入；服务端 30 秒租约 + epoch 校验，10 秒续约。所有 tab 可读；审批仍由原 owner 校验，重复/过期不能复活 |
| 附件 | 最多 8 个、每个 20 MiB；从上传字节创建私有文件、原不透明 ID 入现有附件/发送回执；接受或移除后按原 owned-file 生命周期清理 |
| 下载 | 原 backend ArtifactResolver 校验归属，拒绝符号链接/非普通文件；下载强制附件，不接受任意主机路径；单文件 256 MiB 上限 |
| 内容包 | 浏览器上传 `.caelispack`，复用原 Registry 校验和资源路由，不引入执行脚本 |
| 权限 | 浏览器不获得服务端桌面权限。终端、系统快捷键、桌宠窗口及 Linux/WSL DTW 的无效入口隐藏，能力标记仍明确不可用；任务结果不依赖终端窗口 |

Web 状态位于 `<data>/Web`，sessions / operations 用既有原子 localstate 写入。遇到未知写入，保留原操作/业务回执，查看当前状态，不用新 ID 自动重试。Ingress 当前保留最多 4096 条记录，不按时间删除后重新接受旧 ID；达到上限会停止接纳新副作用，需要后续有安全高水位的账本压缩，不能手删 operations 来“恢复”。上传暂存目录总量上限 512 MiB；未知结果先查看原草稿附件。

## 部署与更新

`deploy/headless/caelisbot.service` 是用户级 systemd 示例。确认路径后复制到用户 unit 目录，再启用；需要退出登录后常驻时，由用户选择是否开启 linger。本次实现不改系统配置。WSL 有 systemd 时可用同一 unit；否则在 WSL 会话或用户既有进程管理器中启动，不保证关闭 WSL 实例后继续运行。

更新时停止 headless owner、备份数据目录、替换完整发行目录 / container、重启。没有 Web 自更新执行入口。只新增 Web 私有状态；既有 Notebook、schedule、渠道、Runtime 数据和 receipt 格式不迁移。历史 Memory 退役与 Notebook 无关。

## 施工接缝与验证

- 新包 `internal/headless`：参数、私有目录、进程锁、信号；根入口增加 `serve/pair`，原桌面及工具子进程入口保留。
- 新包 `internal/localproxy` / `cmd/caelisbot-local-proxy`：可选宿主 loopback relay，只有 HTTP ingress 权限，无业务 owner。
- 新包 `internal/websurface`：认证、allowlist、操作 ID、SSE、上传下载；执行/恢复仍调用既有 owner。
- `internal/desktop/headless.go`：复用原装配；附件增加 `owned` staging 参数，原桌面调用保持 false；Web 上传走原持久化/清理。
- `internal/app/web_surface.go`：只读委托已有任务 manager，不改任务账本或恢复逻辑。
- 前端：同一个 `History/Settings/RuntimePreparation/QuestionPrompt/ApprovalCard`；新增 transport、紧凑齿轮/任务入口和平台能力过滤。没有新 Web 聊天业务模型。
- `cmd/web-fixture` 和 `internal/desktop/web_fixture.go` 仅 `webfixture` tag 可编译，不进入发行 binary；显式模拟模型，复用真实服务投影/草稿/附件。
- Bot guidance 补充 surface 与工具执行主机的区别；不接入新的 async/MCP 实验 feature。

验收证据见 [headless-web-acceptance.md](headless-web-acceptance.md)。无真实模型凭据时，fixture 成功不能当作真实模型、渠道送达、Worker Runtime 或 GUI 验收。

### Gateway addresses in a VM

Provider URLs are resolved by the **CaelisBot server**, not by the browser.
`127.0.0.1` inside Rocky/WSL/container refers to that guest. An unavailable gateway
or an empty model catalog stays in the connection form; retry it after correcting
the address or starting the gateway. It does not require reloading the application.

On this OrbStack demo, `host.orb.internal:3425` was verified read-only against
Mac's existing loopback Magpie. This uses OrbStack's [documented Mac-server
address](https://docs.orbstack.dev/machines/network#connecting-to-mac-servers);
it is not a generic Docker/WSL guarantee. Magpie's Mac listener was not changed.
The dedicated demo directory contains a loopback bridge so the existing default
`http://127.0.0.1:3425/v1` works in Rocky. Its target is fixed, it permits only
model listing and model inference paths, rejects Origin/forwarding headers and
unexpected Host, and exposes no gateway administration. It is limited to trusted
processes in this private VM; do not publish that bridge or use it as a LAN proxy.
It copies no account credentials. The experience start/stop scripts also own its
lifetime. General remote deployments must use their own explicit gateway address
and gateway authentication; the product does not auto-trust VM hostnames.

The default Docker bubblewrap compatibility issue remains unresolved. No
`systempaths=unconfined`, outer sandbox relaxation or Core sandbox bypass was
introduced by this follow-up. Real WSL and Tailscale hardware are still untested.

### First-run recovery and installation evidence

Restoring a **missing** automatically discovered Codex executable is an explicit
installation action. It does not reconnect the previous owner or replay work, so
an unavailable old owner must not block putting the executable back. Updates and
configured external executables retain their existing guards and priority.

First onboarding can select a different Runtime when the Codex adapter proves
that its exact private process has exited and its thread was never submitted,
with no requests, receipts, approvals, workers, stop operations or older owners.
The original ledger remains intact. This narrow setup guard is rechecked under
the existing switch fences; ordinary recovery and #204 owner contracts remain
unchanged. Returning to the same lost owner still uses its explicit recovery path.

The introduction form checks the selected active Runtime, retains a tab-local
draft through failed reads/navigation, and presents failures in place. Rechecking
connection state does not repeat activation, login or the introduction. Completed
native initialization keeps its original request identity. Known pre-dispatch
setup refusals have explicit Web receipts; old unknown receipts are not rewritten
or discarded by an upgrade.

Installer progress journals now include an allowlisted error category and, when
available, numeric HTTP status / exit code. Raw installer stderr is bounded in
memory and classified, never persisted or displayed. An older failure without
these fields is reported as unconfirmed cause, not guessed network/disk failure.
Only explicit retry starts a new installation after a definite failure.

This does not change Bot-facing capabilities or tools, so no additional bot-core
skill update is needed for this repair. Session limits are unchanged: do not
bulk-delete sessions with unknown ownership to make acceptance clients connect.
