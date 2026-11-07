# 创建任务双入口

## 首轮获批范围与实施计划

设计依据：`docs/design/task-create-start-mockup.html`，用户确认单需求框、移除两段说明、等宽创建按钮。

1. 先补红灯回归：单输入、创建不启动、创建并开始、启动失败保留卡片；桌面/手机深浅色及下拉内部垂直对齐。
2. TaskDialog 传递明确 submitter 意图，App 保存成功后仅对返回任务调用原 start；仅创建及键盘默认提交不启动。不自动重放未知启动结果。
3. Go 自动标题提取首句、清除有限礼貌前缀、Unicode 限长；保留完整需求。采用本地文本提取，不宣称模型语义总结，不额外调用模型。
4. 修正全局弹窗 label 选择器误命中 CustomSelect 内部 span；任务弹窗单独布局，保留其他弹窗按钮行为。
5. 更新受影响旧浏览器用例；构建、自测后独立关键路径及视觉验收。不提交、不部署、不调用真实模型。

## 首轮状态（后续优化见下）

本地实现及独立限定验收通过；未提交、未部署，未调用真实模型。已有无关截图、delivery extra 文件及 test-results 保持原样。既有浏览器测试会写文档截图，本次执行前保留原图，测试后恢复原字节；本次截图另留证据目录。

## 实现

- `TaskDialog.vue`：仅需求框；无提示段落；关闭/取消、等宽创建/创建并开始。submitter 明确区分启动意图，隐式提交只创建；保存期间禁止重复提交和关闭。
- `App.vue`：创建完成才针对回执中的新卡片复用 start_task；启动失败保留卡片并显示错误，不再次创建。不把启动意图混入创建命令；缺模型时绑定该卡片到模型窗口，保存模型后启动，取消不启动。
- `style.css`：弹窗标签样式改为直接子 span，消除下拉内部文字多余底部间距；新弹窗单独响应式布局，桌面等宽132px/40px高，手机等宽112px/44px高。
- `task_title.go`：本地首句/首行提取，有限礼貌前缀清理、Unicode 24字限长；完整需求保留，旧显式 title API 兼容。这不是理解整段需求的模型语义总结。

## 验证与证据

统一证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-11-07-task-create/`。

- 红灯：标题单测首次因函数未实现失败；`red.log` 记录旧界面无双按钮/仍有标题输入的浏览器失败。首次启动失败夹具使用不存在主分支被设置校验先拒绝，改用明确的发送前注入；首轮绿灯又暴露测试创建项目 revision 被运行更新打断，夹具改用 structureRevision，不改产品校验。
- `npm --prefix apps/web run build`：通过（保留既有大 chunk 提醒），见 `build.log`。
- `npm --prefix apps/web test`：默认前端测试全部退出0，见 `unit.log`。
- `go -C server test ./internal/workbench ./internal/webapi`：两包通过，见 `go.log`；独立验收另跑标题单测通过。
- `npx playwright test --config playwright.task-create.config.ts`：6/6，见 `browser-final.log`。覆盖单框、等大按钮、下拉对齐/键盘、桌面/390/360深浅色、仅创建不启动、创建并开始、发送前启动失败留卡、空白输入、隐式提交及在途重复提交。
- `npx playwright test --project theme --project mobile-toolbar`：13/13，见 `regression.log`。
- `npx playwright test --project existing --grep 'real build:|merge icon and chat|structured progress uses|result history UI:'`：4/4，见 `existing.log`。
- `npx playwright test --project ui-consistency`：4/4，见 `controls.log`。合计27项浏览器通过（分组服务，不是全套浏览器）。新增套件已注册默认 Playwright project。
- 独立验收报告 `review.md`：源码、原始日志、桌面深浅/手机深色实际读图通过，无阻塞缺陷。后续两组旧入口/控件8项由主控补验。
- `git diff --check`：通过；测试服务由Playwright回收，17331/17332核验无监听。主服务未操作。

## 边界

独立验收保留Minor覆盖缺口：缺模型保存/取消、创建期间认证失效/切项目、创建成功回执丢失后的故障注入，以及有旧任务时仅启动新任务，当前以静态核验为主；不能称全部故障场景已验。发送前失败不替代进程启动后失败/回执丢失恢复测试。未跑完整仓库/全部浏览器、真实模型、真实手机键盘、实际 `.91:7331` 验收；本批未获提交或部署口令。


## 2026-11-07 后续优化：语义标题、启动反馈与异常恢复

用户明确批准继续完成三项优化；本节取代首轮“只有本地首句标题”的当前状态描述。尚未提交／部署或真实模型测试。

### 实现及授权边界

- Web 新任务显式 `autoTitle=true`。Go 先保存完整需求与临时摘要，再使用任务所选 provider/model/thinking 做一次真正的完整需求标题总结；旧API任务、显式title、旧卡片不批量调用。
- `pimodel/title.go` 使用已核实的 pi 1.5.0 RPC：`--no-session --no-tools`，关闭扩展、MCP、skills、上下文及个人运行钩子；独立临时 cwd/HOME/agentDir，不借用开发会话、不读项目文件。25秒超时，等待 `agent_settled` 后读取最终assistant消息并校验终止状态和24字单行标题；不把prompt接受当成功。保留任务配置，不改全局模型或思考预算。
- 只有 credentialSource=server_pi 才读取服务器来源；仅所选provider凭据文件与对应API环境变量，拒绝命令凭据；临时资源在进程回收核实后删除，回收不确定时保留，不输出原始模型错误。
- `webapi/task_title.go` 先持久化claim，同服务最多2个标题调用，无无限队列／自动重试；请求断开不停止服务器标题任务，关闭服务取消并等待。缺模型进入needs_model，补选后针对原任务调用。异常／繁忙保留摘要，卡片显式提示。重启时遗留pending和running均收敛为失败，不自动耗费额度重放。
- 启动反馈在聊天抽屉内部：正在启动、缺模型、已建卡但启动失败及安全重试；未知送达只提供状态核查、不直接再起任务。创建按钮在提交时只对实际点击入口显示“创建中”。
- 回执恢复确认已创建后关闭原创建表单并定位原卡片，不把恢复当成新开发意图；原 operationId 保留。修复“已落库但回执丢失，旧表单重试可能重复建卡”的窗口。

### 最新验证

证据仍统一在 `~/.local/state/agent-artifacts/kaiwu/2026-11-07-task-create/`。

- `optimization-build.log`：类型检查／生产构建通过，保留既有chunk大小提示。`optimization-unit.log`：默认前端测试通过，新增明确失败／未知送达状态单测。
- `optimization-go-final.log`：pimodel、webapi、workbench、app、agent 五包race通过；随后M1恢复修正及凭据拒载测试的定向race见 `pending-green.log`、`optimization-title-final.log`。非全仓库测试。
- `optimization-browser14.log`：14/14。覆盖完整需求模型总结（fakepi返回非首句标题）、无开发消息污染、启动提示及原卡片重试、缺模型取消／保存、创建回执丢失恢复不重复建卡、启动回执丢失不二次派发、旧任务保留、认证失效不创建／启动。
- 浏览器缺模型使用受控快照，恢复后强制全量同步以避免“服务器值本来相同、无patch”的夹具假失败；服务真实needs_model→补选、claim一次性、关闭及重启不重放由Go测试覆盖。
- 旧theme/drawer/mobile组合运行19项，15通过、4项因既有10次/分钟登录限流失败，见 `optimization-regression.log`。未调整产品限流；手机列头独立服务补跑6/6，见 `optimization-mobile-isolated.log`。合计19项不同旧路径均有通过证据，不称同服务一次全绿。
- 独立审查 `optimization-review.md`：功能／安全／桌面与手机读图限定通过，发现Minor M1（创建落库后、claim前重启导致pending滞留）。补红绿回归 `pending-red.log` / `pending-green.log` 后，pending/running统一改为中断失败，不重发；窄复验 `optimization-review.e0ae639e.md` 独立通过，M1关闭。
- 真实新UI截图 `optimization/startup-failure-{1440,390}-{light,dark}.png`；提示处于抽屉内，手机按钮可点、无横向裁切。初次放在抽屉外导致遮罩阻挡的测试失败已修，不将旧图当新UI证据。
- 测试服务由Playwright关闭，17331/17332无监听；主7331未操作。所有旧失败日志保留；未动无关源码／截图修改。

### 未验与剩余边界

标题已接真实pi模型调用链路，但本轮仅隔离fakepi验证，未花费真实模型额度，不声称实际总结质量、所有提供商／OAuth刷新兼容性已验。仍未测真实手机软键盘、其他浏览器、完整仓库全量与实际 `.91:7331`。极端主机崩溃以持久状态边界测试而非真实断电验证；标题繁忙或失败当前保留可见摘要，不自动反复调用。用户切换项目期间的迟到回执以项目／身份守卫核查为主，没有本轮新专项浏览器用例。


## 提交交付授权（2026-11-07）

用户已下达“提交代码”。仅本主目录的本批源码／测试／设计稿与记录纳入；原有截图、delivery extra文本及test-results不纳入。本次无其他写入空间，Pi子Agent仅只读验收。GitHub main与本地main基线一致，仓库无Actions工作流，以既有干净归档Go vet／全量Go／前端／分组浏览器门禁交付。新增创建任务14项套件已接入部署脚本的独立服务生命周期，脚本隔离测试164项通过；其余门禁及认证／8驻留上限不变。部署目标沿用`.91:7331`，提交前只读核查活动对话与执行均为0。实际SHA、全量门禁及部署验收记录见外部同任务status.md，不以前述开发期局部测试代替。
