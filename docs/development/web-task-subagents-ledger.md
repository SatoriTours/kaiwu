# 子 Agent 概览执行台账

项目：kaiwu；任务族：卡片子Agent真实接入/三级观察；原会话：a8279960-1b08-43bd-81a5-66fe32b069d2。

## 已确认与边界

- 用户批准真实接入及数量→列表→具体执行内容；子Agent执行是否可写尚待答复，不因协调通知重复催问。
- 整体写权已接手，80文件hash与交接一致；当前main，HEAD d6f861401af8813f118903b33f44e44b3d0138b6。继承未提交成果均保留。
- 内容基线副本 `/tmp/kaiwu-subagents-baseline-2134`（369文件hash，源码/文档副本），相对该快照评估本任务新增diff，不把旧改动归为本任务。
- 无提交/合并/推送/部署/空间创建授权。不启用个人扩展、不调用真实模型。

## 执行阶段

| 阶段 | 角色/执行端 | 状态 | 修复轮次 | 证据 |
| --- | --- | --- | --- | --- |
| 需求/公共契约/计划 | 原会话 | 完成公共切片设计；真实启动权限待定 | 0 | specs/2026-10-18-task-subagents-design.md；plans/2026-10-18-task-subagents.md |
| 公共记录与只读API | 常规编码 d5438194-f1c4-4a9c-8c71-1d0df1cae572 | 聚焦修正已自测；代码写端停止，待整体独立验收 | 1 | backend/contract报告；两包test/race/vet、build exit0；status/截断/cap后幂等覆盖补齐 |
| 三级UI | UI实现 8b4ccafd-d3c3-4310-8857-0c10832da77f | 首次结束无成果/报告，已向原Agent续接原任务，不派验收或备用 | 0 | 实际仅读取/基线测试；requested high/effective off，未改设置；无明确模型/工具错误 |
| 公共切片独立验收 | 功能验收 | 等待实现 | 0 | 未运行 |
| 真实启动桥/整项验收 | 未派发 | 阻塞：执行写权限待答 | 0 | 不冒充接入完成 |

## 自审/续接

- Task1输出Summary/Detail API，Task2消费相同字段；依赖串行，不能提前猜接口。
- Task2/Task3共享前端文件与测试产物，仅在开发者停写后验收。
- Task1/Task3测试范围一致，以内容hash关联，避免重复已有交互批全套验收。
- 旧schema2→3为公共切片唯一数据库变更；无生产部署，不改原日志或登录口径。
- 输出超限必须仍能保存终态，事件数量有硬上限；若实现发现契约不可满足应先报告原会话，不自行放开权限。

## 派发环境阻塞及恢复

- 已实时读取6个profiles及全部notes，选择常规编码 `pi/opencode-go/deepseek-v4.1-flash`，thinking=high；不是额度/限流替补。
- 使用agent-scoped create_agent显式指定当前会话workspaceId时返回 `Workspace wks_05003fcc8c78f3ec not found`，没有创建Agent或执行代码工具。
- 再查自身get_agent_status仍报告该workspaceId，但list_workspaces的活动列表确实没有它；因此是会话/Workspace归属不一致，而非模型错误。
- 未猜测使用其它同路径Workspace、未新建/恢复空间、未换原生subagent/外部CLI、未重启daemon。需用户恢复本会话可用Workspace或明确指定可复用的现有空间，再核实身份后续接。
- 失败时仅新增本任务spec/plan/ledger三份文档，无代码测试/构建。
- 用户随后明确授权复用现有 `wks_0a6880df1b60b7e7`（prj_f982fe1b7c0edff8，local_checkout，同一cwd而非隔离），不恢复旧Workspace/不修改会话绑定。
- 重新读取最新6个profiles完整notes，并核实目标空间/main/git-dir/index及55源码manifest（exit0）后，显式传入有效workspaceId创建成功，Agent d5438194-f1c4-4a9c-8c71-1d0df1cae572，回执cwd与workspace均正确。没有模型/通道替换，没有改全局配置或服务；只有该Agent为代码写端。

## 公共后端修正轮次1

- 原开发者报告公共记录/只读API完成，未启动真实子进程；主控实际核对相对于/tmp基线的新增diff，旧80成果未改写。
- 允许例外：delivery_store_test.go中schema期望2→3单行配套更新，符合本次已批准迁移，不降低断言。禁止整文件回退，原报告危险checkout建议已要求移除。
- 缺陷证据：AppendSubagentEvent先校验迁移后查event ID，已接受running/completed事件原样重放会因同态/终态被拒；持久截断正文却按该正文重新算指纹，大正文原样重放与原参数不一致。
- 交原开发者先补失败复现，再按原始内容指纹去重与校验顺序聚焦修正，同时覆盖cap后仅摘要终态的有界幂等；不新派备用写端、不重跑全部旧批次。

- 修正1回报：先按原始事件指纹幂等查询，再验证新事件状态；事件表新增fingerprint保存截断前指纹；cap后的已接受状态指纹限≤4，不为被丢弃正文维护无限去重表。已报告红复现/绿两包test+race+vet+build exit0，相关源码与契约已读核对，终验仍交独立角色。
- UI继后端停写后串行启动，无并发实现；允许测试专用subagents_browser_test.go夹具，但禁止生产seed入口/实际派发或7331变更。

- UI首次finished无回复，核对实际文件无Subagent组件/测试/报告，get_activity仅显示读取/既有测试。get_status=idle/activeTurn=null，无待权限。已原Agent续接完整剩余任务并要求说明提前结束原因，禁止以通知当成功；不是缺陷修复轮次、不是额度问题，不换模型。

## 消耗

按角色/修复阶段记录；目前各端非缓存输入/缓存输入/输出/日志估价均未知，不推断账单。无需为记账扫描全量历史。

## 2026-10-30 总目标续接

- 前一目标轮次未留下可核实的完成结论；本轮按实际文件核查，后端公共切片仍在，UI 仅有失败测试，旧 UI Agent 权威状态为 closed。当前总目标持续 active，不把历史计划或 finished 当作已完成。
- 当前协调／实现端 c6eec4f2-37ed-41ad-9961-7964f4f580fb，Workspace `wks_9e9b9697cff7f277`，cwd `/home/jimxl/projects/kaiwu`，main，HEAD `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`，git-dir `.git`／index `.git/index`；local checkout 不隔离。只在当前目录续接；原有混合改动和未知产物均保留。
- 原会话按当前总目标直接完成共用观察 UI：`subagents.ts`、徽标／面板／样式及 App／看板／聊天接线；所有详情文本使用 Vue 安全文本插值，未解析私有思考、未增加浏览器写入口。保留原会话／选择／生命周期，打开详情不启动 Agent。
- 初始 RED：既有 `subagents.test.ts` 因实现模块缺失失败；实现后 7 项新单元及正式 `npm --prefix apps/web test` 退出 0。新测试已纳入 npm test。
- 真实 Go 夹具独立位于 `webapi/testdata/subagentfixture`，临时 SQLite／合成记录，无 pi 子进程或个人配置。专用浏览器配置使用回环 17333，不占用实际 7331。第一次运行受沙箱监听／缓存限制，获测试范围自动批准后执行；夹具状态类型编译问题修正。
- 浏览器发现并修正：离线切换未立即反映原 WebSocket 状态，App 增加成对注册／释放网络事件并沿用原重连；旧通用 dialog 样式覆盖新面板，增加专用样式优先级，360px 全屏。保留 browser2／browser3 原始失败；最终 browser4 三项全通过。
- 自测命令：`npm --prefix apps/web test`、`npm --prefix apps/web run build`；`GOTOOLCHAIN=local go -C server test ./internal/workbench ./internal/webapi`；`go -C server test -race ./internal/workbench ./internal/webapi -run Subagent`；`cd apps/web && npx playwright test --config playwright.subagents.config.ts`。以上最终退出码均 0。测试使用临时 GOCACHE/GOMODCACHE，浏览器及回环监听使用受审批准的测试权限。没有声称全仓 Go／浏览器全量、真实模型或部署验收。
- 浏览器覆盖：卡片→列表→安全任务与工具详情／分页，running+unknown 与历史，跨任务归属、嵌套聊天 Esc／焦点、无会话状态副作用；受控延迟真实 socket 回复不串 run；读取失败保留已有记录并重试；360px、离线保留与重连后真实 Store→socket patch→增量显示。
- 唯一持久证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-10-30-subagents/`。含 `source.sha256`、unit／Go／race／build 日志、browser 原始失败与最终成功日志及四张专用截图。未覆盖旧截图。主控已读图核对桌面详情与手机列表／离线详情，不能替代独立视觉验收。
- 独立验收：已读取最新 6 个 profiles 全部 notes，功能验收配置 `pi/magpie/group/deepseek-flash-4.1`，实际 provider available；创建 `afcd88fc-aae5-4501-88cd-2d4aa7ab3879` 于同 Workspace，未派生空间或分支。实现源码已停止写入，仅主控更新状态文档。要求核功能、必要回归和实际图像能力；不支持图片须标视觉阻塞，不换通道冒充通过。
- 整项仍未完成：真实子 Agent 启动桥／执行权限分支／真实生命周期与模型联调未完成，未提交、推送或更新 7331。后续优先关闭自动交付 C2/I1 凭据与远程边界、C3 检查定义冻结；保留现有禁用 worker。PC／Android、配置模板、通用编排和发布要求继续保留，不以本次观察切片缩小总目标。
- 当前消耗未知；功能独立验收结束，夹具 SIGTERM 退出 0、临时 root／二进制已删除，17331／17333 无监听，无新增后台服务。视觉补验未启动测试服务，无需释放新的运行资源；保留原会话与验收 Agent 历史。
- 当前轮次同时核对实际服务：本执行机及旧文档入口未监听／连接失败，但被忽略的 `.kaiwu/local-deployment.conf` 已指向另一既有目标，其实际 `/healthz` 与 `/` 均返回 HTTP 200。真实地址只保留于本地配置和用户会话，不据此修改既有部署或认证；未读登录凭据，未声称已核认证后的版本／模型调用。本批产物没有上线。
- 新 Go 浏览器夹具 `go vet ./internal/webapi/testdata/subagentfixture` 退出 0，日志 `vet.log` 已留存；最终 `source.sha256` 全部匹配，源码停写后未变。主控自测结束时临时 17333 无监听，不停止独立验收可能重新启动的资源。
- 独立功能验收结论：公共切片通过；实际强制重跑 Go 两包、7 项新增单元、构建、3 项专用浏览器及 6 项抽屉回归，补黑盒 HTTP 认证／归属／分页／只读无副作用、卡片选择与焦点核查。原验收模型返回不支持图片，未冒充视觉通过；一项混合状态徽标文案偏差为非阻塞发现。
- 用户明确允许当前 Codex 在现有 Workspace 补做独立视觉验收；Agent `477bb409-8cc7-4bd2-8604-4a1caa07aab5` 以当前 `codex/gpt-6.1-sol`、auto-review／ultra 配置只读验收，实际逐一打开四张原图。桌面列表／详情与 360px 手机列表／离线详情视觉通过，原视觉证据缺口已补齐；静态截图不扩大为真实模型、派发或部署证据。
- 原会话已核对具名补验段、四图 SHA256 与 `source.sha256` 全部 13 项匹配，报告 SHA256 `6415957d67b004baa7f77142773e67a02b674cf19f9fb20e22db03108f03dbee`。证据仍为唯一目录及其 `verify/`，报告为 `web-task-subagents-verification.md`（保留原阻塞历史及补验限制）。验收 Agent idle、activeTurn=null、无待权限；源码／截图未被验收修改。
