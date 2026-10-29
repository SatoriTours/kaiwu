# 任务成果预览与确认完成验证

> 最新：基线 `4f36acc` 已通过完整门禁并部署。2026-10-29 的被动状态／前台Open并发修正另见[文末](#2026-10-29-被动状态回执与前台open并发)，不以历史通过替代新快照验收。

日期：2026-10-17。状态：该历史批次实现、最终浏览器验证、独立验收与 7331 更新均已通过。

> 后续 2026-10-24 扩展另见[文末](#2026-10-24-产物直达与双合并增量)；以下旧通过／部署结论不覆盖新批次。

## 授权与执行范围

用户在评审结果预览设计与原型后明确要求“开始修改”。实现沿用 Vue 3/TypeScript 前端、纯 Go 后端、SQLite/双向 WebSocket 与原 pi 会话；没有新增运行依赖，没有向真实模型发出产品联调请求。独立任务使用 Paseo 托管 worktree：`wks_14c980b5a0d2d1ef`，分支 `feat/task-result-preview`，路径 `/home/jimxl/.paseo/worktrees/3e51f239/task-result-preview`，由原会话 `e2a5cc41-db4a-41a6-b882-55a1ada60e42` 协调。

当前 main 上有此前功能未提交改动，已复制非忽略源码作为实际开发基线，逐文件快照 `/tmp/kaiwu-preview-baseline`，精确任务记录在主目录忽略的 `.kaiwu/task-result-preview-workspace.json`。没有自动提交／合并／推送，也未归档本任务空间。

首次按最新用途配置派发前端及日常编码，两者使用的 `opencode-go/deepseek-v4.1-flash-free` 均返回 Model is unavailable，尚未产生修改。后端升级至已配置“高消耗工作”，原协调 Agent 接手前端；独立验收使用最新“方案设计，架构思考”配置。同一任务空间的文件范围互不覆盖。

## 实际实现

- 开发结束显示“待验收”，主要入口为“查看效果”，同时可继续修改；确认按钮明确为“确认完成”。确认后持续显示“你已确认本轮结果”，保留成果、效果入口、修改入口及实际 Git 状态／审核合并入口。刷新保留确认状态，不把缺失验证改成通过、不自动提交合并。
- 桌面将看板位置切为成果／效果视图，右侧仍是原任务聊天；手机提供独立效果视图和返回任务对话。历史轮次展示原成果与输出，不把当前工作区效果冒充历史结果。切任务迟到响应不能覆盖新目标。
- 无网页配置时展示真实回复、成果和工具输出，明确暂无网页预览。“补充预览说明”只预填并聚焦原任务草稿，不自动发送或调用模型。
- Go 提供 HTTP／WebSocket `previews/status`、`previews/start`、`previews/stop`、`previews/open`。状态读取与打开入口不启动；用户点击启动才执行工作区的 `kaiwu-preview.json` argv。启动请求携带界面展示配置的 SHA256 `manifestId`，缺失或变化时拒绝并要求刷新，避免执行用户没看过的新命令。
- 核对任务／轮次／worktree 归属、回环监听及实际 socket 进程组归属，健康通过才显示运行。复用执行锁并安全回收空闲 pi，保留原 SessionID／SessionFile／结果；用户新消息开发或合并前安全停止预览。正常服务退出回收，异常退出由继承 flock 与归属记录防重，不凭旧 PID 强杀。
- 预览使用短时任务能力网关、sandbox iframe（仅 allow-scripts）及响应 CSP，工作台凭据不进入任务进程；Origin:null 只在预览能力路径用于相对 GET 资源，其他 API 仍保持同源与登录检查。退出／登录撤销使能力失效，禁止外部重定向。

## 已实际执行的验证

- 实施前完整 Go 基线与前端 11 项状态测试通过。
- 新增结果轮次测试在原实现上失败（resultRoundId 缺失），实现后前端 13/13 通过；网关 URL 测试覆盖外部目标、点段与编码分隔符。
- 新增成果浏览器场景在原实现上失败（缺少查看效果入口）。实现后 Chromium 最终全量 27/27 通过（44.3s，包含配置指纹修正后的外部修改无副作用断言），包含真实任务 HTTP 页面、相对 GET 接口、父工作台隔离、确认后保留、手机返回、显式停止、迟到响应和无配置时不调用模型。
- Vue 类型检查与 Vite 静态构建通过，未新增依赖。浏览器使用真实 Go 服务、隔离假 pi 和临时 Python HTTP 应用；不是假网页或模型口头报告。
- 集成 Go `test ./...`、`test -race ./...`、`vet ./...` 已通过一轮；最终稳定源码已由独立验收再次执行完整 Go `test ./...`、完整 `test -race ./...`、`vet ./...`，全部 exit 0；[独立验收报告](web-task-result-preview-review.md)明确其实际范围。
- 新增浏览器测试夹具起初触发累计登录限流与 8 驻留会话上限，已改为复用有效 Cookie、将撤销登录检查放最后及切换到未启动任务，不放宽生产认证或容量规则。

## 审查与修正

- 主协调审查发现自然退出 leader 先被 reap 后仍按数字 PGID 杀组的风险。回归先失败，再用 Waitid(WNOWAIT) 保留 leader、清理后代、退休信号入口、最后 reap；停止与等待使用小锁避免竞态。自然退出自动释放锁，不依赖页面轮询。
- 独立验收复现 Important：展示命令 A 后修改配置为 B，原 Start 会执行未展示的 B 并产生文件副作用。已新增 `manifestId` 绑定，原 overlay 与旧指纹不停止现有预览／不撤销 token 的复验通过；真实浏览器“改配置拒绝无副作用 → 刷新 → 明确启动”场景通过。
- 后端自查新增 FIFO 配置阻塞及普通／编码点段路径逃逸的失败回归，已完成最小修复，后端最终全量 Go、三包 race 与 vet 通过，独立最终复验通过；manifest/FIFO/路径/旧 stop/自然退出关键回归重复 10 次通过。

## 已知边界

- 当前执行预览限 Linux；最多 4 个预览，启动等待 10 秒，日志尾部限 64 KiB。配置限 16 KiB，使用 `{port}` 参数且只接受有效站内路径。
- 仅 GET/HEAD HTTP 预览；应用登录、WebSocket/HMR、跨源资源与根绝对资源路径可能不兼容；不提供源码／diff／编辑器，也不自动安装应用依赖或猜测任意启动命令。
- 预览链接最长有效 30 分钟。当前不自动续签；过期或另一客户端停止再启动后，点“刷新状态”重新获取入口。独立审查将其列为非阻塞 Minor，本批保留这一限制。
- 旧任务没有 `kaiwu-preview.json` 时先查看运行结果，按需补充预览说明。不会重发旧模型指令、自动补配置或启动旧服务。
- 本机工具链与 Chromium 结果不代表 PC 原生三平台／Android／GitHub Actions 或真实 pi 模型联调验收。

[桌面截图](../design/web-service/task-result-desktop.png) · [手机截图](../design/web-service/task-result-mobile.png) · [设计](../superpowers/specs/2026-10-16-task-result-preview-design.md) · [实施计划](../superpowers/plans/2026-10-17-task-result-preview.md)。截图仅含合成验收项目。

## 7331 实际更新与数据保留

- 原 PID 记录已失效，7331 无实际监听，未终止其他运行任务。将验证后的本批 46 个源码／文档／截图增量与开始时快照核对，确认原文件没有外部修改后同步回主目录；逐文件内容与验收 worktree 一致，既有未提交改动保留。
- 在主目录重新执行 Vue 类型检查／Vite build 与 Go build，成功生成 `server/bin/kaiwu-server-task-result-preview`。
- 原会话 workspace `wks_39792bb6ebe02841` 的 Paseo 托管终端 `45ea7aa9-fc69-4887-8c5f-cf874fe5ba20` 运行现有 Go 服务，固定 `192.168.2.122:7331`，原令牌／SQLite／数据目录不变；`/healthz` 返回 200，实际进程路径已核对。
- 更新前后及临时检查会话退出后核对：1 项目、1 任务、8 消息、19 登录记录，数量一致。实际页面只读烟测能打开原任务成果视图，0 页面脚本错误；没有发送 commands／previews/start／previews/stop，没有调用模型，也没有改写原任务结果。仅撤销临时检查登录，不影响既有用户登录。真实会话未截图或写入公共文档。
- 已保存[最终独立验收报告](web-task-result-preview-review.md)，无未关闭 Critical／Important；保留 30 分钟预览链接手动刷新的 Minor。全部计划步骤已完成。
- 未获 Git 提交、合并、推送授权，因此代码保留未提交状态、执行 worktree 与分支保留，不自动归档。任务记录与精确增量补丁保存在原目录忽略的 `.kaiwu/`，原协调会话继续作为入口。

## 后续修正：已确认任务的多余中断提示（2026-10-17）

- 用户手机截图中，已确认结果上方仍显示“结果已保留／中断原因”。原因是进展卡直接依据后台 CLI 断开状态渲染恢复警告，未排除最新结果已确认的情况。
- 调整 `ProgressCard.vue` 显示条件：最新结果已确认时隐藏该恢复警告；未完成任务与新的失败轮次仍保留原恢复提示，自动接回语义不变。
- 浏览器回归先复现失败（已确认＋会话断开时警告数量为 1），修正后 Chromium 全量 27/27、状态测试 13/13、Vue 类型检查／Vite build、Go build、diff 检查通过。此次未修改 Go 源码，未重复完整 Go／race／vet。
- 7331 空闲时安全重启，使用原数据库与令牌。实际手机视口检查：刷新前后无恢复警告、已确认结果及效果入口可用、0 页面错误、0 任务或模型命令；临时检查登录已退出，项目／任务／消息／登录记录计数保持一致。Paseo 服务终端更新为 `be3a3b03-1b31-4816-8442-6604f9fc9293`。
- 原开发 worktree 路径已不存在，本次小修直接在当前项目处理；没有重新创建、归档工作区或执行 Git 提交／合并／推送。

## 后续修正：聊天输入、汇总、中文与链接（2026-10-17）

- 输入框改为 Enter 发送、Shift + Enter 换行；保留 Ctrl／⌘ + Enter。中文输入法 composing 与 keyCode 229 不发送，输入框／预览之外按钮的普通回车仍由按钮自身处理。界面提示同步更新。
- 连续 assistant progress 消息合并为一条“执行进展”，显示更新数与最新简述；用户新轮次和最终回复各自独立。展开保留全部原文、报告和记录，历史定位能找到分组内原消息，存储不合并或改写。
- 收起的长回复与过程更新优先使用真实结构化报告摘要，无报告时提取原回复几个段落。摘要改用已有安全 Markdown 渲染，保留段落、列表和行内格式；旧中文长句按句号分段。纯报告没有正文时也显示摘要，不再只有空的展开入口。
- HTTP/HTTPS 地址单列完整链接，在新标签页打开并带 noopener/noreferrer；摘要截短不截断此入口。链接去重并处理中文标点与 Markdown 尾括号，不把 javascript 或带账号口令的 URL 变为该入口。原完整回复仍按已有 Markdown 安全策略展示；没有替用户访问回复中的应用 URL，不据此声称该应用始终在线。
- 中文要求除了统一启动参数，现在也随每次明确用户 prompt 发送，包含过程更新、最终回复、报告自然语言、Markdown 段落／列表和完整可点击 URL。首次、常驻、恢复会话均适用；不自动发送语言纠正或翻译请求，不额外调用模型。旧英文原文保留，不能将新约束描述为已翻译历史。

实际回归先失败再通过：普通回车未发送、五条独立进展没有合并、最新只有报告时误取旧说明、报告无正文缺少摘要、URL 后中文标点被误当作路径。最终 Chromium 全量 **28/28**、前端状态测试 **16/16**、Vue 类型检查／Vite build、Go 全量 `test ./...`、Agent `test -race`、全量 `vet ./...`、Go build、gofmt 与 diff 检查通过。独立假进程实际捕获三次 RPC 请求，核对首次及常驻复用／原身份恢复后均携带要求，只有三次明确请求、原用户文字不变。假 pi 夹具识别该内部后缀，继续测试真实调用生命周期；不将假进程当作模型语言遵循验证。

固定 7331 已更新，Paseo 托管终端为 `29271df3-7321-439c-b667-ae70737ce54e`。上线前确认无进行中调用、旧进程退出，再使用原数据和令牌启动。忽略目录下的启动探针补充 SO_REUSEADDR，避免 TCP TIME_WAIT 被误认为活跃监听。实际 390px 手机检查通过：进展组与原消息对应、摘要可见、展开保留更新、完整 URL 链接属性正确、输入提示正确、无横向溢出或页面异常；0 任务／预览启动停止／模型命令。检查登录已退出，更新前后 1 项目、1 任务、19 消息、19 登录记录保持一致。没有执行真实模型联调或 Git 提交／合并／推送。

## 提交前集成复验（2026-10-17）

用户明确要求“提交代码，push 到 github”。当前源码已在本地主分支 main，origin 为 https://github.com/SatoriTours/kaiwu.git，推送前远端没有分支。将此前已完成的结构化进展／双向 WebSocket、自动接回、结果预览、聊天交互及项目菜单一并核验：Go 全量 test、vet、Go build、前端状态 16/16、Vue 类型检查／Vite build、Chromium 全量 32/32（38.7s）、diff 检查均通过。源码散列核对确认测试期间没有并发修改；浏览器使用隔离假 pi，不调用真实模型。

检查待提交树和最初本地提交，未发现实际访问令牌或已检查的密钥模式；本地 .kaiwu 数据、Cookie／登录凭据、会话日志、私有截图、构建产物和 node_modules 均保留忽略。原会话及用户创建的本地工作区保留；原任务托管 worktree 已不在实际路径及活动工作区列表，不按旧记录猜测归属或归档其他空间。没有创建 PR、Tag 或 Release。

## 2026-10-24 产物直达与双合并增量

**状态：本地实现与分项功能／质量验收；用户已要求提交，收尾交付进行中。** 下面保留提交指令前的历史状态，不据此推断实际远端或部署结果。 在当前项目目录与同一 Workspace 延续，不使用以上旧批次的 worktree／发布授权。最新阶段见根目录 `TODO.md` 与 `docs/superpowers/plans/2026-10-24-task-result-actions.md`。

- 静态 HTML／相对资源直接进入新标签页；拥有的未导航空白页在失效时关闭，不关闭用户已有页面。命令预览仍须授权，running 只 Open；未知 Start 仅只读核查，不因 Stop／无回执而再启动。历史轮次不访问当前树。
- v2 manifest 可用 `kind: "files"` 与非空 `artifacts` 声明文件，或在 `kind: "static"` 网页旁添加文件。每项为相对**任务 worktree** 的 `path`、可选下载 `name` 和白名单 `type`，不是相对 static root。最多 16 项／32MiB 每文件；不遍历或列出任意目录。
- Status 仅元数据；显式 Open 才提供本源 `/preview/<cap>/~download/<id>`。files-only 显示下载列表，没有伪网页／iframe／Start。HTML 一律 attachment，不在工作台执行；保留会话／草稿／工作台地址。
- 下载后端独立首验复现旧 cap 在重读 manifest 窗口读取新声明 ID；必须修，未按“此刻已声明”判通过。R1 使用同一已校验 manifest 快照贯穿解析，独立实际 gateway 复验得到窗口内 404、终态旧 cap 409、新 cap 200。同 ID 更名亦须重绑，原 NOFOLLOW／MIME／大小／登录／轮次安全项保留。
- 审核后显式选择本地合并或合并并推送；仅服务端 `PushOnly` 真证明才能显示“推送到远程”。绑定最新已结束轮次及对应 review ID，认可随接纳原子记录，不强迫先按确认完成，也不改写验证证据。远程不可用仍可本地操作；未知回执保持原 operation ID，只读核查、不重放提交／合并／push。Git、本地／远端、CI／部署事实分列，CI 未测仍 `not_checked`。
- 前端 `npm test` 68 项（35＋9＋15＋3＋6）、`vue-tsc --noEmit` 与 Vite 构建通过。新增六个 Chromium 自测在真实 Go／隔离 fakepi／临时 Git 上通过：本地合并及原子认可、真实 PDF／HTML 下载、两个 **mock remote transport** UI 场景、真实本地接纳后丢回执只读核查、静态网页与文件共存／只读轮询。远程 mock 不转发写入，不能当作真实推送验收。
- 下载实际检查 Chromium `download`、中文空格 `suggestedFilename`、保存字节、HTML 不执行、Start=0与非空草稿；这不是操作系统保存对话框验收。桌面／360／390及键盘 Enter 已自测，主控实看六张截图，360 合并取消按钮断行为已记 Minor，未见遮挡；独立验收端无图像能力时仍必须明确**独立视觉验收受阻**，不能用这些开发者截图或 DOM 尺寸代替。
- F5 历史独立组合 exit1 保留，重登录根因尚未实证。当前含 F5 的四例自测通过，7 次登录记录显示当时 UI ready；未改生产认证、未升 timeout／删断言。F5b 已用可见可用的自然点击，Stop 后 Status 增加并显示未知启动解释，零新增 Start，不再靠 inert／force 点击证明成功。

首轮前端冻结源集合散列 `bebaa880bbebd33ab138e1884ff18d6461138a858903f2ffbee29e4c8cbd1521`（14 源文件，M1 后的测试集合见下）；下载后端 R1：`artifacts d99ef152`、`gateway 92d6b630`、`manager d5b797d4`、测试 `4a978481`。独立前端 spec／quality 验收进行中；最终结论以本批追加记录为准。

本批没有真实 pi 模型联调、真实 GitHub／Actions／生产部署／跨平台验收；保留 15 个历史 fakepi，不以清理他人成果换取全绿。原逐项红／失败／绿日志、hash 与独立报告保存在本任务单份 agent-artifacts 留存目录，未把未部署说成已经生效。

### 2026-10-25 M1：登录同步与有界夹具生命周期

独立首验找到真实夹具问题：加载期立即 `isVisible()` 会跳过令牌表单；整文件登录又触发产品 10 次／分钟限流。真实 `/api/snapshot` 401 回包延迟 0／250／400ms 的新用例先 **3 fail（exit1）**，等待 token 或工作台真实 ready 后 **3 pass（exit0）**。worker 仅内存复用自有 fixture Cookie，不回放 localStorage／草稿；显式 logout 仍真撤销、401 后新登录。生产认证未改。

随后整文件单服务又触产品 8 驻留上限（13 pass／6 fail）。正式全量入口为：

```sh
cd apps/web
npm run test:chat-turn
```

入口自动发现全部用例，每片最多四例；`fullyParallel` 只用于个案分片，实际始终 `workers:1`，各片全新服务、串行使用原 17445。无手写 grep／跳例／上调产品限制，保留有限超时及 SIGTERM 收尾。正式执行 **19／19 pass，exit0**（4＋4＋4＋4＋3；每片成功登录 3／1／1／1／4）。补真 local dialog 初始焦点、说明 Space、Esc 零写、重开 Enter 一次写；敏感历史 UI 理由／隐藏 push 是明确 mock 合同检查，不当真实远端联调。

旧 F3 locator 调用时间不再当 DOM 激活时间，正式 fixture 采用先前已验的 trusted native DOM 方法；未改产品 700ms／intent。主控 ready 后 122／255ms，末击距首击 423ms＜700，无新增 Open／tab，900ms 后再开成功；独立补跑末击距首击 452ms，可信／enabled 断言同样通过。不新开已裁定产品根因调查。

最新集合 **`eb8e49020cbc1049be78575e31cb1dde7123d704143e70abeb5e218a14a8bc1e`**（16 文件），仅四测试相关文件变化，生产 TS／Go／dist 未变。569 实际独立窄验 **6＋1＋1 全过**（19.4／3.2／5.4 秒），核对全19日志与自动分片完整性，M1关闭，消费侧 **spec／quality分别通过**；报告 `chat-acceptance.md` §16 保留原失败。完整 Tab 循环未测；HTTP／WS长期相位采样／503变体等旧边界仍不作为通过依据。

**独立视觉仍受阻**，已一次补问是否授权既有 UI 配置先核图像能力再只读验六图；未答不切模型、不重复催问。真实 UI 远端联调仍未验，不扩大本轮授权。整体仍阻塞／未完成；未部署、提交或推送。

## 2026-10-29 被动状态回执与前台Open并发

### 证据与因果边界

- 先有界跑3次仅临时候选加观察仪表的单例，全部通过；独立只读复核确认其不足以证明间歇修复，随后停止同策略采样。
- 按原顺序、同一worker／同一服务生命周期，未经DOM注入的原始 result-review + HTTP 两项再跑一次，2/2通过。原CI精确失败帧序仍未复现。
- 静态可控窗口：前台Start回执先写入running但尚无URL，随后等Open1；此时visible事件可启动被动Status，捕获空previousURL并请求Open2。迟到Status把新cap URL赋给以URL为key的iframe，会重建文档、擦除已经API OK的结果；反序则会在前台Open完成前抢先创建文档。
- 用现有WebSocket probe仅暂扣真实Go回执，模拟一次visible环境事件并按两种顺序释放；未注入预览DOM脚本、伪造HTTP／API响应或修改超时。真实API先返回API OK，再释放迟来的Status/Open回执，旧代码确定性变空p；新两项回归在旧App均红。

### 最小修正

`App.vue`仅增加3行：被动Status最终发布前，若仍有前台intent，或当前taskPreview引用已经不是该请求发出时的previous，则丢弃这一过时／抢先回执。现有finally仍释放busy并按原生命周期续读。显式Refresh（forceOpen）、Start、Stop及任务／轮次／epoch校验不变；不放宽认证、capability、CSP、sandbox或8驻留限制。

新增两项真实Go／HTTP回归覆盖回执早／晚顺序：保留allow-scripts与父文档拒绝、真实API OK、live URL不变、只Start一次、显式Stop及iframe移除。旧HTTP／历史／离线／完成后预览断言未删改。

### 实际验证

- 两回归旧App 2红 → 修正后5项定向全绿；前端构建、73项单元通过。
- 曾直接把49项放同一服务生命周期，新增末尾两项在会话未启动（容量／配置不可用）处失败，外层180秒终止；失败快照／trace保留，只结束已核实自有测试服务，未称全绿。
- 将新两项纳入既有HISTORY_GREP及互补grep-invert，仍原7个门禁组、不删用例、不提高会话上限。部署路由断言先160通过／3失败，调整后163/163通过；自测样本预览6/6与互补业务43/43分别通过（总49，非直接raw49共享生命周期通过）。
- 独立复验：4文件hash一致，构建／73单元／163路由与新两序回归通过；预览组仅5/6，原HTTP空结果断言仍失败（5秒），一次单跑通过不覆盖该失败。该失败仅error-context，无trace／网络记录，不臆测其请求或文档时序。修正自身目标通过，但关键旧HTTP回归未全过，不能作为整体验收成功。
- 后续只做一次无DOM注入／无trace snapshots的被动网络与导航记录原始pair，2/2通过（HTTP 15.4秒）；原失败仍未抓到边界，停止采样。新7路径未提交／未部署；基线4f36acc及79项既有成功保持，不冒充新快照完整证据。

证据复用 `~/.local/state/agent-artifacts/kaiwu/2026-10-27-cold-chat-implementation/`：`submission/http-diagnostic-summary.json`（最新阶段）、`http-original-pair.log`、`http-controlled-red*`、`http-regression-red*`、`http-fix-{build,unit,preview-six,business-complement}*`、`http-routing-{red,green}.log`、`http-fix-source-manifest.json`。所有测试使用隔离fixture／假pi，无真实业务模型或新增图像调用。本修正解决已证明的被动回执文档竞态，不宣称排除慢上游等其他未验证机制。


### 原HTTP失败留证续查（2026-10-29，未完成）

- 新增仅该HTTP用例使用的 `tests/preview-http-evidence.ts`：被动记录预览请求／响应／导航／detach和点击阶段，响应到达立即缓存 `API OK` 的6字节内容及SHA，其他正文仅SHA；cap掩码，不存Cookie／token／任意错误正文。记录源码、两侧构建及实际下发JS／CSS哈希、Node／浏览器／worker／trace状态。失败才有界后验，原异常重抛；原API成功、隔离、Stop／reopen与两序断言不变，无DOM注入／前置就绪等待。
- 先在临时候选真实Go／HTTP成功后故意断言失败，证明确有failed记录、即时API缓存与后验处理器证据。最终helper版本的机制证明通过；人为失败不当作产品回归。4209独立只读审计确认留证与脱敏机制合格（无独立新测试／服务）。
- 复制独立条件（full-SHA前端构建、单worker、原6项同服务、trace off）后抓到两次原HTTP **5/6** 失败：点击begin／returned均落盘，原5秒断言收到空结果，**没有 `/api/hello` 请求，点击后没有iframe导航／detach，也无pageerror**。真实下发资产与两侧dist哈希一致；因此不能再把本组失败归到资产混用、已发生的CORS失败或文档重挂。历史CI精确帧序仍未知。
- 第二次失败的5秒后验是parent iframe hover／active、child无hover／focus；当时后验误用 `#api`，其handler:null不是缺处理器证据，已改 `#ping`。三个helper源码快照均按各run的完整SHA恢复留存，不以新版解释旧版差异。
- 有界原生input取证两组均6绿；只过滤既有CDP日志的一组6绿；前台／后台显式焦点对照各3次真实API均过；点击返回后一次只读geometry组6绿。均不冲销两红，不据此加sleep／改超时／弱化断言或把bringToFront当修复。几何记录是期望中心而非实际输入坐标；原生仪表可能扰动时序，但其通过的因果未证明。
- **仍未定位实际点击坐标、命中或输入投递故障；没有新的生产修正，不宣称原HTTP已修复。** 现8路径（原7＋helper）未提交／未部署，生产保持4f36acc。浏览器系统152与Playwright声明bundled156的兼容性未排除，未安装／换浏览器或依赖，未改权限／认证／sandbox／八驻留限制。

最新证据仍统一在既有根索引及 `submission/http-diagnostic-summary.json`；关键增量：`http-capture-{sha-group,proof-final}*`、`http-click-postmortem-two*`、`http-helper-snapshots/`、`independent-http-capture-audit.md`、`http-native-{input,early}*`、`http-focus-control*`、`http-wire-input*`、`http-after-click*`。本轮无真实业务模型或图像请求。

## 2026-10-30 原生输入几何与测试点击分阶段（本地关键路径通过，未部署）

- 自包含真实HTTP／opaque OOPIF夹具里，原生暖机点击及API成功后，父滚动128px并同步读取新DOM布局。正确新点会送父文档，故意旧点却仍以旧child原点命中按钮；可信事件、实际Input与真实API计数交叉证明“DOM已新而native输入几何仍旧”的窗口。尚未区分Viz、widget visual properties或scroll offset，不称原Go／CI精确帧序已复现。
- 用户批准后仅在私有cache安装SDK匹配Chrome156；未改系统152、依赖／锁、生产launcher。相同冻结夹具／参数的有界版本对照，两版本都出现同类失败，否定“只换匹配版本即可解决”；故意旧点成功是机制判别，不是功能通过。capture参考在156也有红，不把截图当修复。
- `tests/view-navigation.ts`只改跨文档分支：先一次普通`target.hover()`，再原来的普通`target.click()`。无force、JS click、固定sleep、timeout增加、点击重放或新增DOM监听。悬停阶段与最终正常点击各自经过原生输入流程；最终仍以真实API／结果断言判断成功，ACK／CSS `:hover`均不当新事件送达证明。
- 中途尝试过CSS `:hover`门禁，受控暖机后会残留旧hover状态，不能证明新Move送达，已撤除这一未经证实的门禁／重试。中间源码及日志仍存证，不拿其4绿当当前方案证据。第一次误用`npm test -- <file>`并未选择新测试，已标为wrong-command；使用现有Node原生test runner，无新增Vitest依赖。
- 四项单元在原始helper确定性4红，当前4绿，验证分离原生悬停／单次点击及错误不重放；这只是driver边界单元，不冒充原生投递证明。已加入默认`npm test`，只改script，不改依赖／锁。
- 当前helper在152／156受控新滚动窗口各2/2通过；真实API计数1→2，子文档新增一次可信click，父文档无新增按下／点击。单独hover会先落父IFRAME的情况仍真实留证，不能谎称第一hover就一定送到child。
- 新永久浏览器回归`opaque preview pointer:`使用自有Node HTTP服务、同URL popup、`sandbox=allow-scripts`，校验父访问blocked、暖机后真实API增量及可信目标／父文档无按下；不是Go业务／capability验收替代。已纳入现有HISTORY_GREP及互补invert，仍7个生命周期组与8驻留上限。
- 自测干净`4f36acc`归档仅叠11路径（原8＋helper／单元／npm script）：前端77单元、构建、163路由通过；系统152的预览／历史7/7和互补业务43/43分别通过，总50，不称raw50共用一个服务通过。保留之前原Go两次5/6及其他所有红；尚不能用这些绿承诺任何间歇绝对消失。
- 独立干净候选关键4/4通过（原Go HTTP、两回执顺序、原生窗口）；并核源码／下发资产、77单元／163路由／7＋43原始证据，不机械重复全套。只代表限定关键路径通过，不宣称所有间歇消失；见`independent-browser-pointer-fix.md`。当前11路径未提交、未推送、未部署；7331实际发布入口未新复验（上次交付为`4f36acc`），无真实业务模型／图像调用或新最终SHA全量门禁。

复用既有证据目录`submission/`：`browser-input-matched156-plan.json`、`browser-input-paired{152,156}-*`、`browser-input-fixed-pointer-{152,156}`、`browser-input-pointer-unit-{baseline-red,red,green}.log`、`browser-input-pointer-guard-*`（历史撤回尝试）、`browser-input-pointer-fix-{manifest.json,critical.log,complement.log,unit.log,build.log,deploy-test.log}`。最新状态仍看根`index.md`及`http-diagnostic-summary.json`，不以历史段落“未下载”代替本节已授权下载事实。

### 本次交付指令

2026-10-30 用户明确要求“提交代码”。以上未提交／未部署是指令前快照；本次进入既有最终SHA全量门禁、推送与7331实际入口核验流程，结果以外部证据索引及运行修订号为准，不预先宣称交付成功。保留间歇问题尚未证明完全消除的边界。
