# AI 标准用户交互：实施与验证记录

## 授权与交付状态

- 用户已批准[总体设计](../superpowers/specs/2026-10-18-agent-user-interaction-design.md)，回复“ok”后原会话推进[实施计划](../superpowers/plans/2026-10-18-agent-user-interaction.md)。不重复请求计划／角色／执行方式批准。
- 当前 **交互＋成果历史限定切片验收及7331实际入口烟测通过，独立候选部署已收口**；本批本地提交、未推送。真实模型／真实pi扩展未联调，自动交付安全整体不通过，Go全量保留旧崩溃注入窗口失败，不称全套通过。原切片独立结论见[验收报告](web-agent-interaction-acceptance.md)，最新部署／范围以文末22:12起收口记录为准。
- 当前实现不是通用工具审批／沙箱，也不以会话内“同意”代替 Git、成果或交付授权。

## 写入协调与角色

- 原协调会话：`000262f7-e2ae-406f-a640-2c933bb89ff0`，负责设计、计划、边界、实际 diff 与验收组织。
- 后端开发：`acc143cd-af67-4762-afec-57ad4adeb93d`，按最新 profiles 选择复杂编码角色 `pi / deepseek/deepseek-flash / high`。选择原因是持久请求状态、并发回答及原生RPC生命周期，不是额度备用。职责 Tasks1–3，唯一写入 `server/**` 源码／测试，排除 `server/internal/webui/dist/**`；报告 `/tmp/kaiwu-interaction-backend-report.md`。
- UI 实现：`05892edd-6d5b-4b7b-9f05-a10fcec2cb3c`，按最新profile选择 `pi / cliproxy/gemini-3.8-flash-high / high`，仅写已释放的 ChatPanel／App／Kanban／types／task-state／DeliveryStatus／delivery 及新增专属交互组件和测试。另一批次仍占用的ProgressCard等文件及dist不触碰；构建暂用独立tmp，不copy到webui/dist。报告 `/tmp/kaiwu-interaction-ui-report.md`。
- 后端独立功能验收：`e268463d-d0ad-42b3-8189-bffe7fc9c39e`，按最新profile选择 `pi / opencode-go/glm-5.3-flash / high`，只读检查后端及聚焦反例，不修改checkout；报告 `/tmp/kaiwu-interaction-backend-acceptance.md`。后续复用其做UI完整验收，不以开发者自评代替。

另一会话 `9327b4fd-95c3-421d-91c4-999fd189d018` 已确认其成果历史批次不写后端源码，前端唯一写端为 `e11ec034-1b4b-4d95-8635-116c90d3cf0d`，占用 `ProgressCard.vue`、`ResultPreview.vue`、`progress.css`、`result-history.ts`、原浏览器／状态测试及构建产物。隔离测试端口17331／17332归其使用。本任务不覆盖这些现有修改或截图，不自动清理无关未跟踪文件；前端未释放前不修改。

### 后续写权交接请求

会话 `a8279960-1b08-43bd-81a5-66fe32b069d2`（卡片子 Agent 运行概览）请求待本批全部写端停止后交接。其当前只读设计，不改代码、不派编码；本批无需为其提前中断、commit、另建空间或额外验收。原协调会话须在后端、UI、必要修复均停止且无待处理写工具后，通知该会话实际 HEAD hash、未提交／未跟踪变更文件、本批剩余写入计划与明确可交接范围；HEAD hash 不能代表含未提交修改的完整快照。其他会话仍占用的文件必须单列，不得一并释放。

## 计划核对与实施决定

| 检查 | 结果 |
| --- | --- |
| Task1→Task2：字段／请求类型、状态与答案校验 | 同一后端执行端串行完成，冻结最终JSON后再交UI，不让UI猜字段 |
| Task2→Task3：普通消息与原生等待响应 | 共用产品交互外观，保持不同派发通道；原生必须按原ID＋运行代次 |
| Tasks1–3→Task4：类型／命令与状态同步 | 先交接后接入，UI不提前并行猜接口 |
| Task4→Task5：前端文件与browser产物 | 先确认其他写端停止，自测后独立验收；不覆盖另一批成果历史改动 |
| Task1：限制与数据兼容 | 可选conversation交互数组，旧数据库／消息／会话身份保留，32KiB/12字段/20选项 |
| Task2：轮次结束与完成验收 | acknowledged＋settled后才激活普通询问，阻塞不是awaiting_review |
| Task3：原生方法能力与取消 | 先核验当前pi文档／类型，再适配实际confirm/select/input/editor；未知方法不伪装已支持 |
| Task4：任务隔离与局部对话框 | 非模态聊天内卡片，切卡不串目标，false/0不丢失，收起不等于拒绝 |
| Task5：测试与部署事实 | 测试命令和退出码有证据才记通过；无部署授权不动7331 |

## 进度

- [x] 总体设计与一次整体确认。
- [x] 实施计划、profile核对和共享checkout写入协调。
- [x] 后端模型／校验／持久化及命令首轮实现及开发者自测（尚未独立验收）。
- [x] 原会话普通询问／答复闭环与交付门禁首轮及四项聚焦修正开发者自测：已支持原周期可恢复等待、多请求汇总、取消不伪造答案；后端独立验收进行中。
- [x] pi原生交互适配与假进程协议开发者自测，已修正重启保留accepted（尚未独立验收）。
- [x] 聊天内标准卡片、六类字段、草稿／历史／任务隔离：非视觉集成完成，独立8项真实Go＋假pi浏览器及桌面／360键盘视觉验收通过。
- [x] 交互切片开发者自测与稳定快照聚焦回归；全共享快照existing＋delivery收尾另列，不互相替代。
- [x] 交互切片独立功能、D1–D3、浏览器视觉及键盘已验范围通过；真实模型／扩展／真机／屏幕阅读器深验未执行。
- [x] README／路线图更新、实际diff及证据核对完成；43项按独立服务生命周期1＋42分组通过、成果历史1440／390／360视觉补验通过；未提交／部署。

## 测试证据

后端首轮开发报告：`/tmp/kaiwu-interaction-backend-report.md`，消费契约：`/tmp/kaiwu-interaction-contract.md`。以下是开发者实际执行记录，**不是独立验收结论**；后续修正后仅复验受影响范围，再对稳定快照做一次必要回归。

| 命令（均 GOTOOLCHAIN=local） | 开发者记录 |
| --- | --- |
| `go -C server test ./internal/workbench -run 'Interaction' -count=1` | exit 0 |
| `go -C server test ./internal/workbench ./internal/agent ./internal/webapi ./internal/delivery ./internal/pirpc -count=1` | 全部ok，exit 0 |
| `go -C server test ./... -count=1` | 全部ok，exit 0 |
| `go -C server test -race ./internal/pirpc ./internal/agent ./internal/workbench ./internal/webapi -count=1` | 全部ok，exit 0 |
| `go -C server vet ./...` | exit 0 |
| `go -C server build ./...` | exit 0 |

### 原会话首轮核对与修正交接

主控已查看实际 `manager.go/store.go` diff 和 `agent/interaction.go`、`interaction_native.go`、`webapi/interaction.go`、`workbench/interaction.go` 及交付入口关键实现，不以成功声明替代核对。发现并已交原后端开发端修正：

1. `interaction_pending` 不应使原自动交付周期终态blocked并要求用户重建；回答后应在同周期／授权与安全候选约束下续接，不能自动覆盖手动暂停或未知副作用。
2. 多个普通blocking问题未答齐前不能因第一份答案立即启动依赖任务；应排队记录后确定汇总续接，原生等待者按其独立契约回答。
3. 不可强制开启普通问题取消能力并以本地清门禁冒充得到依赖答案；尊重请求取消能力，保持取消／收起／拒绝语义区分。
4. 原生答复已持久accepted的写入证据不能因服务重启一律改uncertain；只对真正未知窗口如此归一。

### 聚焦修正后开发者记录

四项修正已由原开发端完成并停止后端写入；契约已归档为[实际协议](web-agent-interaction-contract.md)。开发报告末尾“聚焦修正”章节优先于首轮限制。修正后再次执行受影响五包、交互稳定性 `-count=2`、四包race、Go全量、vet／build，均报告exit 0。已新增真实Store＋假Agent／remote／CI的“问→等待→答→同cycle继续、零重派发”与全部取消后等待新用户轮次回归。

本轮UI独占已释放文件；未触碰其他批次仍占用的组件、原测试、截图及生成目录。UI／浏览器／双设备、真实模型与真实扩展交互尚无通过证据，7331未部署。

### 后端独立验收首轮：不通过，交回原开发者

报告 `/tmp/kaiwu-interaction-backend-acceptance.md`。验收者完成五包交互聚焦测试、delivery门禁／续接聚焦及其race，exit 0；使用 `/tmp/kaiwu-acceptance-probe/overlay.json` 与注入clock的独立反例确定性复现 D1（普通及race命令exit 1，无race报告）。未改checkout。

**D1必修**：修复轮（repairing）提问时，90分钟用户等待被 `ActiveSince` 预算计入，回答后同周期下一可修复失败误入终态 `blocked(repair_deadline)`。这是规格必需项，不因其余覆盖项通过而将总体标为通过；已要求验收者修正其“通过含缺陷”的报告结论为“不通过”。

已交原后端写端 `acc143cd…` 聚焦修复；server源码重新进入写入状态，已通知另一个协调会话。修复必须扣除用户等待窗口并保留此前真实活动消耗，不能简单把 `ActiveSince` 重置到now来绕过预算；需覆盖多次等待、重启、pending→answering及答复后AI活动计时。附带同源M2未知原生方法不静默挂起、M5超限决策内容不静默截断后仍允许用户答复；M1基线死字段不扩大清理。修后交原验收者聚焦复验，不机械重跑全部验收。

### D1／M2／M3／M5 开发修正完成，待独立复验

原开发者已报告停止server写入，并交回原验收者复验；已同步另一协调会话后端恢复只读。开发者先以原验收overlay确认D1修前exit 1，修后普通／race overlay均exit 0；受影响delivery／agent／workbench／webapi测试、计时稳定性count=2及聚焦race、vet/build均报告exit 0，未重复Go全套。

实现新增持久 `waitingSince/waitExcludedMillis`，活动预算扣除真实用户等待，不重置 `ActiveSince`；`interaction_answering` 仍计入AI活动。新增长等待、反复等待不续命、答复运行超限、手动pause/cancel回归。主控已看实际manager／repair／types／report diff，要求独立复验核对等待窗口与ActiveSince基线，避免跨计时区间错误扣除（ActiveSince赋值实际在stageQueued→developing，前验收首报的函数归属已通知更正）。

M2/M5改为明确取消不可表示的原生请求并发布 `Round.interactionNotice`；M3已补显式成果门禁与错误文案测试。契约已更新归档，UI写端已收到新增notice展示要求。**在独立复验完成前，不更新“不通过”结论为通过。**

### UI首轮核对：尚未完成，退回原开发者

开发者报告 `/tmp/kaiwu-interaction-ui-report.md` 声称8项专属单元、28项基线、2项浏览器及正式build通过。主控已读实际App／ChatPanel diff、InteractionCard提交代码、interaction纯逻辑、Playwright fixture／配置与copy-build脚本：

- 两条浏览器用例仅注入Socket快照验证渲染／客户端校验，不提交有效答复；实际fixture是5类字段（缺text），没有真实Go交互请求到答复的端到端证据。不能据此宣称六类型闭环或完整视觉验收。
- `other`占位符直接进入answers，与Go真实选项ID＋others契约不符，正常“其他”提交会被服务端拒绝；且可能与合法ID叫other的选项冲突。另有过期请求、原生空输入、草稿／失效身份、未知操作重试及可访问标签缺口，已一次交回原UI开发者处理。
- 历史尚非折叠完整问答、展开时普通composer未按设计收起；ProgressCard旧问题的最后集成继续等共享文件释放，不能提前修改。

**写入边界异常**：UI开发者在被明确禁止正式build/copy时仍执行 `npm run build --prefix apps/web`；该命令经copy-build.mjs会写 `server/internal/webui/dist`。主控实查dist/index.html mtime为2026-10-18 19:29:58 +0800，与apps/web/dist一致。因此其“未写webui/dist”报告不成立，已要求更正并立即停止正式build/copy，不自行回滚／清理产物。已通知占有dist的另一协调会话；这些生成物不是稳定验收／交付基线，待共同冻结后统一重建。7331未更新。

原UI写端继续按原允许apps边界聚焦修复；测试产物改为专属/tmp，不写全局test-results，不越界编译最终Go服务。最终真实Go＋隔离假pi闭环、双设备／断线和桌面手机视觉证据仍待补齐，当前不能判Task4通过。

### 第三轮后端独立复验与短冻结记录

- 独立复验确认D1关闭，M2／M3／M5通过；原overlay反例转绿，长等待扣除、多轮不续命、实际答复轮计时及既有边界通过。queued跨计时基线经产品入口核查不可达，不扩大修复。
- **D2新缺陷**：全部取消后处于 `interaction_unanswered`，新用户指令轮真实运行62分钟仍被计入用户等待扣除，导致该超预算修复没有触发 `repair_deadline`。独立overlay确定性复现，后端总体仍不通过。需识别该新采用轮次的running/starting为answering，及时关闭用户等待段。
- 19:36:19+08:00收到后端/UI两端无运行或待处理写工具的停止声明后，批准另一会话最长15分钟共同冻结，供其只读验收并统一build；本批未趁冻结修D2。另一方门禁运行29项单元通过，但 `vue-tsc` 因本批半成品 `InteractionCard.vue:119` 缺 `InteractionDraft.otherFlags` 退出2，未执行Playwright／截图／服务、未更新dist。
- 另一方19:38:34+08:00明确结束冻结并释放其6文件及dist写权，无运行/待处理工具；报告 `/tmp/kaiwu-result-history-acceptance.md` 为阻塞，非通过。六文件hash/HEAD交接 `/tmp/kaiwu-result-history-handoff.json`，HEAD `d6f861401af8813f118903b33f44e44b3d0138b6` 不代表含未提交成果的完整工作区。
- 原协调会话明确恢复 `acc143cd…` 后端D2修复，`05892edd…` UI接入/类型/真实闭环修复及必要ProgressCard集成；均要求保留6文件当前成果，禁止恢复HEAD覆盖。UI稳定后允许一次正式build统一生成dist、17441/17442隔离测试，本批专属截图／产物到tmp，7331不动；原批次完整浏览器／视觉复验待后续共同稳定窗口。
- `a8279960…` 已通知继续只读；此次局部释放不推导全checkout交权。本批全部写端停止前不整体交接。

### D2开发修正完成，独立聚焦复验中

开发者第四轮报告：Store提交并解锁后的user-turn hook通知Delivery注入Clock记录新用户轮开始时刻；取消／失败重试／边界flush产生的新轮均及时结束用户等待扣除，`ActiveSince`不重置。原独立overlay与五次稳定性、取消新轮长短活动／失败重试长短活动回归均报告exit 0，受影响包和聚焦race、vet/build通过；未重复Go全套。已确认server写入停止，源码转只读复验；UI独占其允许前端范围和dist。

主控核对了Store hook锁外调用及manager／repair实际diff，再交原验收者复验D2、计时与新增hook的锁／生命周期；当前尚未获独立通过结论。消费契约已同步归档。

### D2复验关闭，D3原生等待计量回归待修

第四轮独立复验确认D2关闭：取消新轮长／短活动、失败重试长／短活动、D1预算不续命与既有边界通过，新增hook锁外／持久化后触发与受影响race未发现问题。

新发现必修 **D3**：`interactionWait` 的RunState覆盖把“原生对话框pending且pi轮次仍running”的90分钟用户思考当AI活动，`waitingSince`未打开，后续误 `repair_deadline`；独立探针3/3确定性失败。后端总体仍不通过。主控已核对manager实际判定顺序，交同一复杂编码端在前端任务安全点优先修复；除原生pending优先外，要求精确处理原生答复／取消／重试恢复AI后的计时边界（原生不创建新轮不能遗漏），补长等待＋短活动、答复后62min活动仍超限、同轮连续提问预算不续命，并保留普通D1/D2。不是以当前服务禁用extensions为由跳过批准范围。

### UI第二轮核对：必需闭环仍缺证据，准备非视觉实现交接

主控再次读取报告、实际browser spec、共享命令调度模块及6文件SHA256：除ProgressCard本任务必要集成外，另外5个成果历史文件与原交接hash一致，已保留。类型／正式构建和单元通过仅采作开发者证据。

同一缺口仍在：浏览器继续用routeWebSocket注入后端不存在的 `ix_1001`，仅新增lastCommandFrame断言，未验证服务端reply status／持久accepted／原会话下一轮；因此不能称“真实Go＋fakepi闭环”。fixture仍5类缺text，8项单元未补dispatch命令反例；ChatPanel仍直接按status过滤而非带响应式过期判断。已要求原UI写端停止并确认无待处理工具、更正报告后再交接；同类必需项连续两轮无实质闭环，拟将非视觉状态／测试集成交复杂编码角色，原因是有证据的无进展，不是额度切换。

主控实际查看 `/tmp/kaiwu-interaction-desktop-filled.png` 与 `…mobile-360.png`，可见单选控件被拉宽、选项文字挤向右侧、“其他”换行错位，当前截图不构成视觉通过。需UI角色对局部样式修正并实机检查；复杂编码交接不得掩盖此项。UI写端明确停止前，不另开同文件写端。

### 非视觉集成写权交接已生效

`05892edd…` 已明确停止全部工具/后台进程、无挂起写入并纠正其报告为“开发部分完成，真实Go闭环与完整验收未完成”。原会话再次读取最新profiles/notes后，将非视觉集成、命令生命周期和真实端到端测试交给同任务复杂编码执行端 `acc143cd…`（实际 `pi / deepseek/deepseek-flash / high`），复用其后端上下文。**原因是同类必需项两轮无实质进展及复杂状态集成，不是额度／限流切换。**

- `acc143cd…` 唯一写入对应交互TS/Vue、专属测试／配置及必要ProgressCard集成；允许扩 `server/internal/agent/testdata/fakepi/main.go` 固定测试场景，不修改D2复验中的Go生产源；报告 `/tmp/kaiwu-interaction-integration-report.md`。
- `05892edd…` 仅独占 `interaction.css` 小范围视觉修复（radio/checkbox被全局input样式拉宽），不改TS/Vue/测试，不运行build／服务／截图，不写dist。完成CSS后停写，由集成端统一build和实际截图。
- 已通知独立验收者D2生产源保持不变，UI在开发中不提前验收；集成冻结后再交其做完整UI验收。两端文件不重叠，生成物只有集成端可写。
- 不新增Workspace，不改模型默认，不由任何开发端代交全checkout写权；成果历史另5文件SHA256仍与交接一致。

### 20:25 用户要求继续后的实际恢复

用户重复要求继续后，原会话一次核对实际状态（非定时轮询）：后端／集成执行端 `acc143cd…` 与独立验收 `e268…` 均为closed、无待权限，并非仍在后台运行。集成报告尚不存在，backend report仍为19:44第四轮，acceptance停在19:59的D3未通过。不能把无完成通知或上轮已派发当作持续执行／已完成。

已通过Paseo向原开发Agent发送续接，工具返回running，复用原session与已保留修改、不新建端／换模型。其为当前唯一实现写端，先读新diff、识别上次失败点，再修D3并完成真实Go闭环；要求在同一报告保存可验收切片状态，实际恢复／模型错误必须报告。

续接内容hash（20:25检查）：delivery/manager.go `ae20aacae9c6005d7ecdab59bc8ea9ce228a0c0d44bd72e061656c7206cc573f`；workbench/interaction.go `d36bebfef5fc57b7219061bde20113ce74d9275103cedc4edfe7cf0e1e0b7c02`；interaction-command.ts `43fe63790226debd35239a7547484be11be1d9e79f06d191c4a695d786278d80`；ChatPanel.vue `284fc7096a2bb97eeb0a593d5c9bd1023eb18aac813f82b5b4388212cefd011f`；interaction.browser.spec.ts `6028c5e9702287a43fea3baf6f81a73d7aeb637413b7becd7a80d6015b5ef03c`；Go fakepi fixture `466e1ccf3ea33176ed7929105398c9d8fc85e9e79a2ee6a85241ca40684672f1`。

同次进程检查未见kaiwu／Playwright／Vite进程；原会话未启动、停止或部署任何网页服务，不能据此前历史PID推断当前服务仍运行。未跟踪 `server/fakepi` 是续接时已有生成物，保留不擅自清理。

### 20:34 集成切片完成：真实闭环自测通过，独立全验进行中

复杂编码端已交 `/tmp/kaiwu-interaction-integration-report.md` 并确认全部源码／测试／dist写工具停止、17441/17442释放。D3已修，等待独立复验；验收方原probe3保持原样，其旧等待码断言和缺少修复编辑夹具需由验收者独立判断调整，不能由开发者自证通过。

主控已读取实际8项E2E代码与报告，核对旧注入 `ix_1001` 已移除：正常业务来自真实Go＋固定fakepi，`page.on(websocket)`仅观察真实status/data；仅丢回执场景使用代理模拟网络故障。自测18项交互／命令单元、29项基线及build exit 0；8项浏览器通过（14.6s），包含6字段200/answered/accepted与原Session续轮/刷新历史、原生false/空串、时钟过期、多问汇总、丢回执、切卡草稿、第二真实API抢答200/409、360长卡片。双设备测试并非两个真实浏览器UI、手机不是真机，报告已区分；完整键盘／屏幕阅读器未由开发者验收。

主控实际查看新桌面env和mobile360截图，旧radio拉宽／文字挤右已消失，移动端按钮可见；这不替代独立验收。成果历史5文件hash仍与原交接一致，ProgressCard仅增加本任务等待／旧question去重集成。

当前冻结源码快照：HEAD `d6f861401af8813f118903b33f44e44b3d0138b6`＋55个修改／新增源码清单 `/tmp/kaiwu-interaction-final-manifest.sha256`，清单内容hash `558b6aa9105c5085056567579c4dfd39387e28ccac8d5f1df3f5460513323f43`；两处dist/index.html SHA256均 `3ec564874eb567aaeb83f36881f98c8cbd2c63dd52ce99f3c5538081556e8559`。

已续接原独立验收 `e268…`：D3及受影响预算／hook聚焦复验＋最终真实Go UI／键盘／桌面手机视觉，报告 `/tmp/kaiwu-interaction-final-acceptance.md`。另一成果历史协调会话已获通知可申请同一只读快照的17331/17332基线／视觉复验窗口，本批验收用17441/17442，不自动并发构建或写源码。此时仍非最终通过／未部署（后续独立结果见下节）。

### 最终交互切片验收通过；共享长测试根因已证实

已读并归档原验收者最终报告[web-agent-interaction-acceptance.md](web-agent-interaction-acceptance.md)：D1/D2/D3关闭；其独立27项overlay探针、16项预算回归及三包race通过，18项交互单元／29项基线／8项真实Go＋假piE2E、桌面与360实际截图、Tab/Enter/非模态可操作性均通过。未真实模型／真实扩展／真机／部署，双设备为第二真实API模拟，未冒称两台真实设备或全产品通过。

原existing＋delivery整套先后两次仍27过1败15未跑。原作者仅修browser.spec.ts的项目绑定（hash从66191…改为9aeee…，其余源码/dist不变）未消除失败，停止继续改selector。一次trace证实start_task200/rev291先创建轮次后rev292失败，定位Send同步失败；不是按钮隐藏或Git“未开始”等价于轮次未启动。

21:12:30原后端作者在/tmp使用Go overlay仅增加error/Count日志，effective一次existing原顺序运行47.4s，27过1败5未跑，得到精确证据：`errType=*errors.errorString err="session capacity reached" instances=8`。不是从10份历史sessionFile推导，排除了模型／provider配置路径。本轮属于共享测试服务生命周期累计到产品驻留上限，不能为测试提高8上限或增加自动回收。

已授权成果历史协调方优先不改源码，分两次独立服务生命周期复验：原history新增用例单独1项，停止并新启服务，再跑原43排除该1项的其余42项；全部原断言和其余相对顺序保留，总计43项。两批都有证据才能称分组回归通过，不能冒称单服务连续43通过；遇首败停止。通过后补其本批1440/390/360视觉。报告根因 `/tmp/kaiwu-start-failure-diagnosis.md`，原诊断trace/log/哈希均保留。

### 验收执行边界事件

原独立验收者e268披露约21:06–21:08曾对未确认归属PID4090794执行kill、并执行名称级 `pkill -f chrome-headless-shell`（自身shell exit143），可能影响另一trace浏览器；已停止并如实补录在归档验收报告§五。不能冒称清理目标均为自己，也不从粗略时间／未见TargetClosed就断言无影响。另一方已保存时间线及补录；21:05:35.785真实服务端failed patch与上述粗时段仅并列留证。后续只允许清理有可证归属的进程句柄，不按名字／端口全局清理；不为补录重复测试。

/tmp只读overlay诊断对原472项哈希核查：生产源／测试／dist／原截图／指针不变，仅README／roadmap为主控21:12文档状态更新。此阶段尚未完成分组全回归／成果历史视觉收尾（后续完成情况如下）；无人因该阶段报告获得部署、提交或推送权限。

### 21:30 最终回归收尾与交付判断

已读并归档[隔离分组回归与视觉报告](web-agent-interaction-regression.md)：精确history1项在fresh Go服务／data运行1/1过（2.9s），停止后再fresh服务运行其余32 existing＋10 delivery共42/42过（76.6s），零skip／flaky。保留全部原断言、相对顺序和产品8会话限制。只能称**43项隔离分组通过**，不能称同服务连续43通过；原失败及精确Send观测仍留档。

成果历史额外视觉首脚本误期望task.request而非Round对应消息已记录为脚本错误，未修改产品。独立期望直接从真实snapshot按firstRoundId读取Message后，新/tmp脚本5检查／5图／0页面错误通过，1440／390／360历史选择、标题、旧记录禁确认／返回最新及完成条已实际检查。旧错误脚本／四张先期截图保留。21:30:28所有测试／browser／服务工具停止，17331/17332/17441/17442/18999无监听，473文件前后零差异；源码55项清单仍 `f852deed…157a50`，dist两处index仍 `3ec56487…e8559`。

原会话已核对实际diff、新旧协议与测试代码、原成果历史5文件hash、两端独立证据和实际截图；不重跑未变化快照全部测试，不将开发者声明当验收。已归档本交互独立桌面／手机合成页面截图：[桌面](../design/web-service/agent-interaction-desktop.png)／[手机](../design/web-service/agent-interaction-mobile.png)。这些不是现用网页截图。

最终 `git diff --check` exit 2，仅 `progress.css:63`、`types.ts:383` 各一处新增EOF空行；非功能故障，记录为低危格式观察，未为追求全绿改动已验快照或清理其他批次。`server/fakepi` 为本任务续接前已有生成物，`delivery/extra-*.txt` 为原先未跟踪基线，未擅删、未计入功能交付代码。

**最终范围**：确认、输入、选择／组合表单、任务隔离、草稿历史、原会话答复续接、原生协议和等待生命周期已完成隔离功能／浏览器／键盘视觉验收；真实模型、真实pi扩展、现用网页服务、真实远程交付、真机输入法及屏幕阅读器深验未执行，不因此声称全产品已验收。当前没有本任务待执行源码／测试／构建写操作；提交／推送／部署需后续相应授权。

### 角色消耗记录（不为记账重扫会话）

| 角色／阶段 | 非缓存输入 | 缓存输入 | 输出 | 日志估价／说明 |
| --- | --- | --- | --- | --- |
| 原协调、复杂编码／后端及集成、UI／两轮修复与CSS | 未知 | 未知 | 未知 | 未取得可归属完整用量，不把未知记0 |
| 独立验收 e268，21:02工具返回的累计快照（非最终账单） | 未确认；工具inputTokens原值1,715,365 | cachedInputTokens 28,588,740 | outputTokens 110,574 | totalCostUsd 1.17025395为工具日志估价；是否非缓存口径及最终累计未确认，不能当实际付费或全部任务总价 |

缺陷／阶段记录：D1→D2→D3为同族等待计时的可复现修复影响，均由原开发者修复、原验收者聚焦复验关闭；UI真实闭环同类必需项两轮未收敛后在原端停止条件下交复杂编码集成。测试环境会话容量故障和越界清理／build事件与产品新增需求分别留档，不用续接或改名重置计数。

### 全checkout后续交接条件

原协作会话已明确所有执行端停止，成果历史方最终回执无待写工具。用户后续任务 `a8279960…` 可由原协调者提供HEAD＋内容hash／全部变更文件、已验范围、未验项和无待写计划后明确串行交接；不是自动提交／合并／推送／部署授权。新任务必须保留当前未提交混合成果和原基线文件，后续若本批需修复或部署先重新协调写权，不恢复HEAD或擅自清理。


### 22:12 用户批准统一收口：恢复已验版本，候选与第三批隔离

- 22:14 已在Paseo托管终端恢复原 `server/bin/kaiwu-server-ddc811f`，SHA256仍为 `c8730a7e11b0e2cc3fbc092fab2e06b620f59daa45daabbce1bfab1c75a17a4c`；实际7331健康200。备份 `.kaiwu/backups/before-recovery-20261018T141346Z/`，SQLite schema2、原消息及会话身份、原19条登录及原令牌核对保留。进程无 `KAIWU_GITHUB_*`，worker不装配；停服日志只有20:07的正常stopped记录，原因未确认，不冒称已修根因。
- 功能验收角色独立实测真实入口，资源／关键导航／交付启用禁用及preflight503、刷新登录通过；报告 `/tmp/kaiwu-recovery-live-acceptance.md`，手机完成几何与控件检查但未逐张视觉细查，未把无数据的已合并状态冒称实机覆盖。
- 两批最终交接 `/tmp/kaiwu-user-interaction-handoff.json`（80文件，content hash `0ae6d7efc985ba69d98f5767a83e6acbe05911d7c0b34f404b1990bb00ec405c`）；55源码manifest `f852deed…7a50`与独立候选逐项匹配。原执行／验收已停止，复用其证据不重复1＋42。
- 第三批概览协调者安全停写，精确7修改＋11新增及共享hunk交接 `/tmp/kaiwu-subagents-stop-20261018/`；不回退／移动主目录。候选取 `/tmp/kaiwu-subagents-baseline-2134` 中前两批版本，缺失文件仅从HEAD或当前内容按既有manifest hash核对补齐。schema3、Store、State、API与前端类型／红测试成组排除。候选目录 `/tmp/kaiwu-release-20261018-interaction-history` 仅为打包副本，非新Git仓库／Workspace，不修改主checkout生成目录。
- 候选66个预期改动文件：前两批源码／测试／说明及两张明确交互图；排除11张旧截图随机更新、`server/fakepi`和两份extra文本，原地保留。前端29项基线＋18项交互单元、类型及Vite构建exit0。
- 最终Go全量首次exit1：`TestRepeatedNativeQuestionsDoNotRenewBudget` 报 `native waits not accumulated: 5400001ms`；已交原开发端在候选内定位，禁止放宽179分钟／62分钟断言或重置ActiveSince。另一个pimodel失败为独立候选无.git引发fixture VCS stamping；明确临时 `GOFLAGS=-buildvcs=false` 后该包exit0，不修改产品测试或初始化Git。
- 原开发者只修 `interaction_wait_accounting_test.go` 同步：等待第一段结算、第二段新WaitingSince，保留179分钟下界、62分钟活动、ActiveSince不重置与修复次数断言，新增排除上界；20次重复及race5次、delivery包通过。独立验收另跑3次及race2次通过，核对生产manager/repair未改。单槽resume边界的调度停滞风险不因此关闭，详见自动交付安全清单。
- 主控最终全量Go再次exit1：原连续问题测试已绿，但 `TestDeliveryRecoveryCrashAndRestart/after-main-receipt` 子进程观察到succeeded，错过预定main_ci窗口。原HEAD相同测试／注入源码独立聚焦12次、候选聚焦15次均通过；不因此把全量失败改写成通过，也未重跑全套。该项交独立验收判断限定切片影响；自动交付依旧禁用。
- 候选vet／Go构建exit0；新构建产物真实Go＋假pi交互8项及成果历史1项浏览器均exit0，不重复旧43项分组。候选二进制 `server/bin/kaiwu-server-interaction-history-20261018`，SHA256 `61c11b05a905824a0ae424c373c557788c59c9af480a7a0c16315c975e87835b`；三份静态资源完整hash保留在 `.kaiwu/interaction-history-candidate.json`，index仍3ec56487…；构建未伪造VCS stamp。
- 前两批及状态说明69个文件精确内容已暂存，7个共享文件的第三批hunk仍仅在工作树；18文件第三批hash无变化并额外备份至 `.kaiwu/backups/third-batch-paused-20261018/`。11张无关截图、假pi二进制、2份extra残留仍原地未暂存。`git diff --cached --check`只报progress.css/types.ts末尾空行两项低危观察，未为清洁度改动共享类型或重复功能验收。
- 独立验收对旧崩溃注入瞬态窗口作出限定裁决：基线及候选注入／integration测试源码相同、无交互路径为no-op，未能确定复现偶发；不因此抹去全量失败。该自动交付路径在worker彻底未装配的当前7331不可达，不阻塞本交互＋历史切片发布，自动交付整体仍不通过。完整裁决 `/tmp/kaiwu-release-sync-acceptance.md`，后续聚焦107次通过不替代失败归因实证。
- 22:43 备份 `.kaiwu/backups/before-interaction-history-20261018T144302Z/` 后，核验旧进程身份及零活动会话／周期，SIGTERM正常结束；同一Paseo终端 `65cb4cc1-899a-40e1-ae1f-c7f462560056` 切换为候选二进制。真实7331健康200，首页／JS／CSS三资源逐字节hash与候选一致，schema仍2；消息／会话身份／登录行／原令牌保留，进程无KAIWU_GITHUB环境。记录 `.kaiwu/interaction-history-deployment.json`。最终独立实际入口烟测已通过（`/tmp/kaiwu-interaction-history-live-acceptance.md`）：1440／390／360导航／溢出／交付禁用及preflight503、历史折叠时间摘要、真实已完成／已合并紧凑条、刷新登录均有证据，13张实际截图逐张检查；无pending交互、旧历史切换和预览深度未在live执行，不伪造生产交互链路验证。主控再比对最初备份中的19条原登录行全部保留，消息／会话身份不变、schema2、SQLite integrity ok；当前24条含独立测试留下的登录记录，不将其说成原始总数仍19。本批仅本地提交，不推送。
- 回滚：保留原ddc811f二进制及两次私有SQLite备份，本次无schema迁移。不能直接构建当前含第三批的checkout来替换候选；后续schema3部署须另行授权迁移和回滚策略，不擅改PRAGMA降库。
- 自动交付安全项仍未关闭，不启用真实上传，不调用真实模型。阶段消耗按既有角色／修复轮次记录，输入／缓存／输出及估价本批未知，不猜测账单。
