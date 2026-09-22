# P1 原生验收记录 — 2026-09-19

## 环境与资产

- 基础提交 `3c0b958`；检查点已提交 `d67f0f2`，本轮打磨和 P1.5 补验仍未提交，无 push。
- macOS 27.0 (26A428)，Apple M4 / Mac16,1，24 GiB。
- 单块 Retina：1800x1169 logical points，backing scale 2；当次 visibleFrame 为
  `{x:0,y:76,width:1800,height:1054}`，包含菜单栏/Dock 的工作区避让。
- Wails v3.0.0-beta.6，静态 `frontend/public/models/stick.glb`，Three.js 0.186.0。
  火柴人包含 11 个 mesh，无动画；GLB validator 0 errors / 0 warnings。
- 启动使用 `script/build_and_run.sh --verify`，原生截图来自运行中的 `.app`。

## 结果与证据级别

| 场景 | 结果 | 证据 / 限制 |
| --- | --- | --- |
| 冷启动 | 通过 | 原生宿主就绪；旧欢迎窗不出现；初始 petKey=false、panelVisible=false；accessory policy=1，LSUIElement=true |
| 透明 GPU 角色 | 通过 | 原生可见火柴人，用户确认周边透明区可操作下方应用；截图（历史记录私有归档） |
| 原生拖动 | 通过 | 用户真实鼠标确认；日志有 `(1512,216) → (1385,156)` 等实际移动，拖动前后前台保持原应用、petKey=false |
| 局部穿透 | 通过（用户实机确认） | 用户回答“可以拖动，透明区能点击下方应用”；命中切换日志可追溯。不以 Three.js alpha 本身或 app 定向自动点击作通过证据 |
| 最小/标准/最大尺寸 | 通过 | 实际窗口为 117x156、180x240、288x384 points；比例始终 3:4，模型与命中 mask 同比例映射；新输入胶囊固定可读尺寸 |
| 位置/比例/隐藏偏好恢复 | 通过 | 重启恢复 scale=0.65、visible=false 及坐标；后续菜单恢复 scale=1 并保存；最后重启恢复标准尺寸与用户移动后的位置 |
| 输入/焦点 | 通过（有限范围） | 显式打开 panelKey=true；收起后原前台应用恢复、petKey=false；中文文本粘贴可显示，未验中文 IME 候选/组词全过程 |
| 胶囊入口 | 通过 | 用户选定 Bot 下方输入胶囊；实际截图（历史记录私有归档），无标题卡片，空闲状态无常开输入框；本次加号与连续滑杆见下文 |
| 草稿与后端状态 | 通过 | 收起/再次打开保留临时中文草稿；发送禁用且辅助说明标明未连接；本轮已移除常驻提示文字；退出/renderer reload 不承诺保留 |
| 隐藏与收起 | 通过 | 隐藏 pet 后 panel 可保留；关闭最后一个面板后 noWindowsAvailable，但 app 进程仍存在；trace 记录两个 visible=false |
| 再次唤回 | 通过 | `--recall` 走 single-instance，隐藏后恢复 pet；不创建第二个常驻 owner，不抢输入焦点 |
| 明确退出 | 通过 | 原生菜单“退出 Caelis Bot”触发 shutdown 记录，随后 `pgrep -x caelis-bot` 无结果；退出后已重新启动供查看 |
| 状态栏 | 用户确认新版菜单正常 | 首页 favicon；AppKit 原生菜单内直接拖动滑杆，不跳转到桌宠输入框。旧 label/预设菜单已移除 |
| 屏幕移除回收 | 逻辑测试通过，硬件未测 | 测试覆盖负坐标屏幕、原屏移除、无交集回主屏、非有限坐标；不能替代真实拔屏/DPI 切换 |
| 原生全屏跟随 | 新策略单屏实测通过 | Probe 全屏期间 pet/input 均 onActiveSpace=true，过渡后 petOcclusionVisible=1、petKey=false；输入可主动唤起，隐藏后切换保持隐藏；见末尾新记录 |
| 普通 Space 跟随与拖动 | 用户实机确认通过 | 单窗口修复后，三指切换后继续拖动顺滑、点击/穿透正常；睡眠和硬件热插拔仍待验 |

初期自动化计数窗口曾在 Bot 未运行时产生计数，此结果**不计入穿透验收**。
定向窗口自动化也可能绕过系统光标路由，最终穿透依据为用户真实鼠标反馈。
运行期间发现的非激活窗口问题已通过原生 NSPanel 承载解决；失败的动态改类实验
未保留在实现中。启动验证改为宿主就绪事件，避免短暂存活被当作成功。

检查点时胶囊原生校正为 420x82 points，展开加号为 420x340；这些是旧版尺寸。
本轮删除常驻说明和混合设置后为 420x64，展开附件为 420x117。

## 绘制与性能

静态角色没有 requestAnimationFrame / interval / animation mixer 循环。WebGL 只在
加载、尺寸变化、重新显示和 context recovery 时绘制。窗口平移由 macOS 合成，
不逐帧重绘模型；隐藏没有持续绘制。该策略也意味着当前没有 idle 动画。

一次短期 host-only 样本为 CPU 0.0%、约 27 MiB、21 threads、sleeping；此前在
交互和诊断开启期间，三个 1 秒样本为 0.0% / 1.4% / 2.7%、约 36 MiB。
这些不是长期性能预算，且**不包含 WebKit 渲染/GPU 子进程**。未实测合成帧率、
长期 GPU 功耗或耗电；不能据此宣称零功耗。模型静止时不产生应用动画帧。

## 复现

```sh
make check
make smoke
make build
CAELIS_BOT_DESKTOP_TRACE="$PWD/.cache/desktop-trace.jsonl" ./script/build_and_run.sh --verify
./script/build_and_run.sh --recall
```

`script/run-desktop-probe.sh` 构建独立的临时原生计数/输入窗口（产物在 `.cache`），
可供真实鼠标放在桌宠下方验收。它不是产品 UI、后台能力或必须依赖；本轮测试窗口
已关闭。自动化点击计数不等于 OS 穿透证明。

原生 trace 默认关闭，开启后只含几何、焦点、屏幕、前台应用标识和事件名；
不记录按键、草稿、对话或凭据。`--verify` 检查宿主就绪和进程，视觉/交互另行验收。

## 剩余门槛

P1.1–P1.4 核心路径可以继续使用。P1.5 已补单屏原生全屏往返；多屏/混合 DPI、真实拔屏、
睡眠恢复、IME 候选过程与完整能耗仍须补充。P1 不标记为全场景完成；P2 真后端
尚未开始。没有正式角色、复杂动画或创作技能。

## 检查点历史：连续缩放与加号菜单（本轮已替换设置入口）

- 原生拖动滑杆得到 `scale=1.28674426685198`，实际 pet frame 232x309 points
  （AppKit 像素取整）。保存文件保留完整浮点比例；重启后滑杆显示 129%，底层值保持不变。
- 原生菜单“桌宠大小 → 连续调节…”能直接打开加号选项；截图（历史记录私有归档）
  来自当时检查点构建，包含输入草稿与重启恢复的比例。较长文件名标签使用省略与原名提示。
- native file sheet 选择仓库 README.md 后显示本地标签并回到输入焦点；收起/重新打开
  保留标签，重启后选择清除。无上传、无文件内容读取或后端请求。
- 独立 Probe 应用变为前台时记录 `panel-dismiss`，panelVisible=false、petKey=false，
  前台保留 Probe；最终版本把日志细分为 global/local/deactivate 以区分原因。
- 定向 Probe 自动点击增加计数但不一定触发系统全局鼠标事件，因此不计作完整外部点击证明。
  **用户已用真实鼠标确认：“会收起，点击和焦点都正常”。外部点击收起与目标焦点通过。**
- 右键菜单已实现打开/大小/隐藏；自动化右键未能可靠到达独立输入层，未标记为实测通过。
- 展开选项时焦点进入添加文件；完整 Tab/方向键流程在本次自动化中被外部点击收起打断，
  不标记通过。此前 Esc/Cmd+W 路径仍保留；IME 候选检查继续列为待验。
- 新增测试验证任意浮点比例、脚部锚点、预览不写盘、最终提交、慢桥接顺序与合并、
  原子附件选择/去重/取消/上限/移除、选择器不占生命周期锁、退出后迟到选择被拒绝。
- 本轮 `make check`、`make smoke`、`make build`、race 测试和原生 Run 就绪检查通过。
  一次沙箱 make build 报 Go module stat cache 写入权限提示但退出码为 0；随后原生 Run
  在获准环境下构建成功。既有 bundle 大小和重复 -lobjc 提示仍在。

## 检查点之后：P1.5 全屏与资源范围（旧不跟随策略）

检查点 commit：`d67f0f235993a3f08d1f468a67a8a5588a6ae53e`。以下是提交后继续推进的
增量，保留未提交；没有第二次 commit 或 push。

通过公开 AppKit 的 `onActiveSpace` 与 `occlusionState` 区分“窗口已 order-in”和
“位于当前桌面且可见”，不引入私有 Space API、轮询或全屏识别猜测。原有可选 trace
增加这些字段、遮挡状态变化与 wake/active-space 事件标签。正常产品不显示诊断面板。

本机使用项目自带 Probe 的 Toggle full screen，进入/退出原生全屏一次：

| 观测 | 全屏中 | 返回普通桌面 |
| --- | --- | --- |
| pet / input onActiveSpace | false / false | true / true |
| pet occlusion visible | false | true |
| pet key window | false | false |
| 输入胶囊 visible / key | false / false | false / false |
| pet origin / frame | (1367,475), 213x284 | 未变 |
| 前台应用 | 验收 Probe | 验收 Probe |

原生字段摘录（历史记录私有归档）、Probe 全屏截图（历史记录私有归档）、
返回后的角色截图（历史记录私有归档）。截图是指定窗口捕获，不能单独证明
整个桌面没有覆盖层；判断使用实际全屏操作与 AppKit 状态记录，两者明确分开。
验收临时窗口已关闭，Caelis Bot 保持运行。

另以 `top -pid <host PID> -l 6 -s 2 -stats pid,command,cpu,mem,threads,state` 采样约10秒。
去掉首个累计样本，宿主 CPU 为 0.4/0.4/1.3/1.6/1.3%，top MEM 为 28–29M，
18–19 threads，均 sleeping。采样时开启诊断且桌面仍在使用，不称为严格空闲或长期预算。
WebKit 服务的 PPID 为 launchd；仅凭名字/启动时间不足以确定归属。`launchctl procinfo`
要求 root，因此未提升到 root、未合并不确定的 WebKit 用量；GPU/整体功耗仍待正式测量。

本次已再次请求真实鼠标右键和中文候选 Esc 检查，尚待用户反馈，不能从自动化推断通过。

## 基础交互打磨（检查点之后，未提交）

- 按本轮用户要求，输入胶囊从 420×82 收紧为 **420×64**；展开附件为 420×117。
  移除常驻“未连接/本机草稿”说明，+ 只提供添加文件；尺寸/隐藏不再混入输入框。
  插件引用等待真实能力接通，不保留占位配置项。发送仍禁用，没有伪造已发送状态。
- 原生状态栏使用首页 `assets/favicon-32.png` 的原样副本，来源/哈希记录在
  `internal/desktop/assets/README.md`。状态栏和右键各有原位 65%–160% NSSlider，
  无跳转、预设比例或重置；右键没有打开输入框。显示/隐藏为独立菜单项。
- 用户真实鼠标反馈：**菜单正常，拖动仍偏慢**。该反馈对应第一轮减少手动移动开销的版本。
  之后已改用 Window Server 原生拖动，由输入父窗带动渲染子窗；四点阈值只区分点击与拖动，
  不调整鼠标速度。AppKit 可能不投递 mouse-up，使用移动通知及仅拖动期间的释放状态观察
  结束 capture，松手后保存实际位置。用户对此替换重新实测后确认：**明显更跟手，松手和点击都正常**，
  问题明确包含角色点击与透明区域点击。没有把第一轮优化当作成功。
- 原生尺寸完成事件与随后隐藏/退出通过顺序队列处理。测试覆盖任意精度最终几何、
  非法值/退出后拒绝、服务等待 AppKit 时事件入队不阻塞及 resize → hide → quit 的保存顺序。
- 已实机查看新版胶囊/加号，选择并移除仓库 `package.json`；原生 sheet 保持，完成后
  选项收起、焦点回到输入框，Esc 收起并回到前应用。未向任何后端发送文件。
- 当前截图：紧凑胶囊（历史记录私有归档）、
  附件菜单（历史记录私有归档）。旧 `p1-plus-menu.png` 仅作历史。
- 检查：`make check`、`make smoke`、原生 build/run 和 race 检查已通过；smoke 在受限
  sandbox 中的 App Server 启动退出，原命令在本机权限下通过 initialize 和资产验证。
  Vite bundle 大小及重复 `-lobjc` 警告仍存在。原生菜单未单独获得自动截图，不冒充视觉证据。

原生拖动依据：[Apple performWindowDragWithEvent:](https://developer.apple.com/documentation/appkit/nswindow/performdrag%28with%3A%29?language=objc)。

当轮回归（旧策略，已被末尾记录取代）：窗口拖动所有权调整后再次测试单屏全屏往返，pet/input 的 onActiveSpace
仍是 false/false → true/true，几何保持 `(1355,485,207,276)`，petKey=false。
正常退出后 `pgrep` 无进程；重新经 Run 启动后位置/可见性不变，比例完整保留
`1.1493639873745836`，窗口尺寸仅有 AppKit 像素取整。
打磨验收摘录（历史记录私有归档） 记录此次结果；临时 Probe 已关闭，新版仍运行。

## 输入快捷键与角色点击切换

- Enter 对齐发送按钮，Shift+Enter 保留 textarea 原生换行；isComposing 或 keyCode 229
  时不处理发送/收起，长按 Enter 不重复触发。发送当前仍禁用，不清空草稿或伪造消息。
- 两行中文草稿经原生 WKWebView 验证：Shift+Enter 插入换行，Enter 保持相同草稿且不
  插入新行。增至七行后编辑区最多显示约五行并滚动；清空后回到 420×64。
  两行输入截图（历史记录私有归档） 为本轮运行中的原生输入框。
- 键盘回归覆盖可发送路径的单次派发、禁用、长按、Shift 换行、IME 边界 229 和 composing。
  真正后端发送未实现，IME 完整候选过程仍待实机，不把上述测试等同于已接通对话。
- 本体点击（含辅助功能入口）改为 TogglePanel；状态栏 Open 仍只展开。可见性由原生
  窗口判定，外部收起后不会使用过期 Go 状态；sheet 期间不收起。服务测试覆盖连续切换、
  原生外部收起后的再打开、状态栏重复打开及退出后迟到点击。
- 新版已运行；本体真实鼠标收起/再展开已请求确认。自动化通过 renderer 按钮打开，
  不冒充独立输入层的完整鼠标证据。
- 本轮 `make check`（含键盘回归）、`make smoke`、`make build`、race、原生 Run 均通过。
  已有 Vite 大 bundle / duplicate -lobjc 警告不变。无新 commit/push。

IME 边界处理依据：[MDN keydown 与 IME](https://developer.mozilla.org/en-US/docs/Web/API/Element/keydown_event#keydown_events_with_ime)。

## 桌面/全屏跟随修复

用户反馈三指滑动切换内置屏幕上的 macOS 桌面后宠物没有跟随。之前“不在全屏 Space
显示”的记录验证的是旧策略，不能作为新需求已完成的证据。

- 实际 pet/input NSPanel 都采用 CanJoinAllSpaces + FullScreenAuxiliary；13+ 加
  CanJoinAllApplications。保持 nonactivating/floating，没有提高到屏保层级或使用私有 API。
- Space 通知不再走几何回收/保存；只收起输入、按原显示偏好 order-in、刷新命中状态。
  不唤回主动隐藏的宠物、不取得焦点。真正显示器变化和 wake 仍走原有位置回收。
- 新构建经 Run 启动，已查看真实火柴人和全屏中的输入胶囊。原生全屏进入时两层
  onActiveSpace=true，随后 occlusionVisible=1；前台保持 Probe、petKey=false。
  全屏内主动打开输入 panelKey=true，关闭后归还；隐藏后返回普通桌面仍 visible=false，
  Space 通知确实收起面板。最后显式恢复宠物并关闭临时 Probe，几何全程未变。
- 跟随证据摘录（历史记录私有归档） 来自本次公开 AppKit 遥测，
  不冒充整屏叠加截图。三指滑动经过用户现有普通桌面的实测已请求用户确认。
- `make check`、六目标共享编译、`make smoke`、`make build`、race、原生 Run 通过。
  Windows/Linux 原生 GUI 未实现；详见[跨平台基线](platform-baseline.md)。

策略依据：[Apple all Spaces](https://developer.apple.com/documentation/appkit/nswindow/collectionbehavior-swift.struct/canjoinallspaces)、
[all applications](https://developer.apple.com/documentation/appkit/nswindow/collectionbehavior-swift.struct/canjoinallapplications)；
FullScreenAuxiliary 与 FullScreenNone 互斥性同时核对本机公开 SDK `NSWindow.h`。

## Space 跟随后的拖动回退修复（当前版本）

用户报告运行版拖动非常卡。对比上一轮和当前 trace：双窗版本松手时绘制窗仍在旧坐标，
随后持久化的 apply 才移动约 150–240 个逻辑点。新实现将 WebKit 和原生输入视图放入
同一个非激活 NSPanel；Window Server 直接移动可见角色，不再依赖父子窗口同时移动。
点击分类与拖动状态分开，原生 mouse-up 不会把普通点击当作完成拖动保存。

新构建已原生查看；辅助功能点击打开输入、Esc 收起通过。用户进一步使用真实鼠标与
触控板确认：**“拖动恢复顺畅，切桌面后也正常，点击/穿透正常”**。
新版拖动结束与 Go apply 的坐标一致，未再出现绘制窗被保存回写搬动的跳变。
故障对比和验收摘录（历史记录私有归档） 保留前后数据。
`make check`、race、原生 build/run 通过；多屏/DPI/拔屏、睡眠、IME 候选与长期能耗仍未验收。
