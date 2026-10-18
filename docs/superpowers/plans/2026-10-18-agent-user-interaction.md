# AI 标准用户交互实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement task-by-task. 本项目已确认流程优先：设计已获用户批准，不重复索要计划或执行方式批准；不自行 commit。

**Goal:** 在每个项目／任务的原聊天内处理确认、输入、选择和组合表单，持久化请求／答复，正确续接原 Agent，不混淆成果验收或系统授权。

**Architecture:** Go 维护 conversation 下的标准交互及答复状态，可靠结束边界发布普通 AI 问题，原生 pi 事件按运行代次适配。Vue 以非模态聊天内卡片呈现，沿用 commands／Socket、operationId 与权威快照；后端与 UI 顺序交接，独立验收不写实现。

**Tech Stack:** 现有 Go、SQLite、pi JSONL、Vue 3／TypeScript／Vite／Playwright；不新增依赖或 pi 扩展。

**Spec:** `docs/superpowers/specs/2026-10-18-agent-user-interaction-design.md`（用户回复 ok 已批准方案与推进实现）

## Global Constraints

- 当前 cwd `/home/jimxl/projects/kaiwu`，不另建 Workspace／worktree；不提交／合并／推送／部署／重启7331，不调用真实模型。
- 发现另一会话正在修改成果历史 UI：先协调其释放边界，不覆盖其修改，不重复生成已有截图。后端实现须确认无另一 server 写端；UI 待后端契约稳定且前端文件释放后启动。
- Go 服务所有自有后端用 Go；六类有界字段，不允许任意脚本、工具执行器或普通表单收集密钥。
- 普通问题不是权限审批；结果完成／Git／自动交付门禁由可信服务端控制；等待用户不等于开发已完成。
- 命令持久幂等；不确定送达不自动重发，不承诺无法证明的恰好一次。
- 原生方法仅接已核验版本实际支持者；原进程消失即失效，不把原生请求偷偷改成普通 prompt。
- 开发者自测后独立功能验收，UI 包含实际浏览器交互与视觉证据；真实模型与局域网部署另行说明，不冒充已经验证。

## Review Focus

1. 流式块未结束／模型随后继续调用工具：只在可靠轮次结束后激活普通请求，不因早到报告误启动下一轮。
2. 先保存答复随后崩溃／丢回执：持久记录与送达分开，未知窗口不自动重发。
3. 阻塞问题与自动交付并存：后端门禁阻止修复、提交、集成、推送和成果验收绕过，不只禁用 UI。
4. 切卡片／多设备／过期表单：捕获归属和版本，第一次成功写入获胜，迟到结果不串范围。
5. 原生 request ID 重用／进程死亡／上游超时：绑定运行代次，旧响应不可发到新等待者，取消与拒绝有清晰差异。

---

## 文件结构与接口约定

- 新增 `server/internal/workbench/interaction*.go`：类型、严格解析／校验、状态与幂等答案，测试放同包。
- 修改 `server/internal/workbench/{types,store,commands,report,execution}.go`：Conversation 可选 interactions 数组、持久化兼容、命令入口、请求门禁。
- 新增 `server/internal/agent/interaction*.go`：普通结束边界／原生事件适配与送达生命周期；修改 events／manager／launch 及必要任务／Git／delivery 入口。
- 新增 `server/internal/pirpc/ui*.go`：共享有界单写队列下发送原始 UI 响应，不走普通 Call；补假进程与竞态测试。
- 修改 `server/internal/webapi/handler.go` 及新增交互测试：commands 回执及原会话派发；沿用现有 Socket 白名单 commands。
- 新增 `apps/web/src/{interaction.ts,InteractionCard.vue,interaction.css}`：纯状态／校验／草稿、六类字段／组合卡片、局部样式。
- 修改 `apps/web/src/{types,App,ChatPanel,KanbanBoard,task-state}.ts/.vue` 及必要 ProgressCard：捕获范围提交、任务角标、阻止误成果验收；尊重成果历史批次已有改动。
- 新增 `apps/web/tests/interaction*.{test.ts,browser.spec.ts}` 与必要独立假服务／配置，避免覆盖正在被其他批次修改的 browser.spec.ts。
- 维护 README／roadmap 和本批验证文档；其他批次文档与成果不归本任务。

建议公共协议：Conversation `interactions?: InteractionRequest[]`；请求含 `schemaVersion,id,version,projectId,conversationId,taskId?,roundId,sourceMessageId?,source,title,description?,fields,blocking,allowCancel,status,createdAt,expiresAt?,response?`。服务端生成归属／身份；响应含 answers、comment、送达状态与对应消息 ID。字段 `confirm,text,textarea,single_select,multi_select,number`；`answers` 为 fieldId → JSON 类型值（boolean/string/string[]/number）。

命令 `answer_interaction`、支持来源的 `cancel_interaction`、可证明未发送时 `retry_interaction_answer`；参数 `operationId,projectId,conversationId,taskId?,requestId,requestVersion,answers?,comment?`。文字替代答案只对普通来源开放并保存显式关联。开发者可因实际现有接口调整内部名字，但须交付完整最终 Go／TS JSON 样例与命令语义，UI 不得猜契约。

上限固定：12 字段／每字段20选项／请求及答复各32KiB；标题160字符／说明4000字符／单行2000字符／多行5000字符／补充说明2000字符。选项 ID 稳定唯一；推荐项不默认选择；默认无超时。确认值必须显式给 boolean，包括 false，不把 false 当缺省。数字禁止 NaN／Infinity，支持整数及上下限。

## Task 1：Go 交互模型、持久化与校验（后端写端）

**Interfaces:** 产出标准请求／字段／响应类型、严格 ParseInteraction、ValidateAnswers、HasBlockingInteraction 等确定接口与最终 JSON 样例。

- [ ] 编写红测：六类字段、false、0、空选项、其他值、混合表单、重复 field／option ID、未知字段、超限／半截 JSON、恶意来源／归属、原数据库兼容。
- [ ] 运行 `GOTOOLCHAIN=local go -C server test ./internal/workbench -run 'Interaction' -count=1`，记录真实失败，再实现白名单解析／类型校验和可选数组持久化。
- [ ] 答复与 operation receipt 同事务保存：请求版本、第一写入胜出、重复相同操作回放、同操作不同值冲突、非所属项目／会话拒绝；新命令依请求版本而非无关全局 revision 防伪冲突。
- [ ] 同命令及 SQLite 重开／sync replay 测试通过，确认历史 question 不批量生成 pending。

## Task 2：原会话普通问题闭环及服务端门禁（后端写端）

**Interfaces:** 消费 Task 1 类型；产出 `<kaiwu-interaction>` 完整结构解析与普通问题 answer／retry 命令；提供 UI 可消费契约。

- [ ] 红测真实 Manager＋隔离假 pi：完整轮次结束创建请求、流式半块不发布、后续 message／工具不被误判暂停、同源去重、新 legacy question 转自由文本、历史仅只读、 malformed 交互 fail closed。
- [ ] 仅在 acknowledged＋settled 边界发布普通请求，隐藏合法结构块但保留原文；请求取代“成果待验收”状态，保留实际执行／会话身份。
- [ ] 红测回答原会话／worktree、拒绝 false、多个排队请求、明确文字替代、普通聊天不消除 pending、恢复失败保留答案、丢回执／双设备／重启窗口；实现持久送达意图与可证明未发送才重试。
- [ ] 红测并实现成果确认、开始开发、Git 审核执行、自动交付／修复的后端门禁。多请求未答完不得因其中一个答案重新启动依赖全部答案的任务；原生响应按各上游独立等待语义处理。
- [ ] `GOTOOLCHAIN=local go -C server test ./internal/{workbench,agent,webapi,delivery} -count=1` 通过，输出 API 样例／范围／不确定发送策略供 UI 交接。

## Task 3：pi 原生交互适配（同一后端写端，串行）

**Interfaces:** 产出当前版本确实支持的 confirm/select/input/editor 映射，原请求 ID 专用响应队列及运行代次校验；不支持方法明确分类。

- [ ] 完整阅读当前安装 pi 的 rpc.md、rpc-extension-ui.md、相关命令与类型实现及必要交叉文档；记录版本及 schema 证据，不加载个人扩展／真实模型。
- [ ] 红测专用原始响应发送：不生成 req_N／普通 response 等待者，共用现有单写队列、大小／超时／关闭／取消分类，避免并发写 stdin。
- [ ] 实现并测试 confirm、select、input、editor 映射；无响应要求的 notify 等不生成待处理表单；无法映射方法不能静默声称已支持。
- [ ] 假进程测试取消、false、空字符串的上游合法性、timeout、重复 ID、运行代次、进程死亡／服务重启后失效、未知送达不重发。明确等待器超时上游可能自行取消，不能宣称 UI 控制整个 Agent 授权。
- [ ] 运行 `GOTOOLCHAIN=local go -C server test -race ./internal/{pirpc,agent,workbench,webapi} -count=1`；与 Tasks 1–2 集成后冻结接口交给 UI。

## Task 4：聊天内标准交互 UI（后端稳定且文件释放后由 UI 写端执行）

**Interfaces:** 严格消费后端交接 JSON／命令，单独纯函数和组件不自创另一套协议。

- [ ] 红测字段前端校验、pending 排队、草稿 requestId＋version＋conversation 隔离、关闭／刷新恢复、多设备已处理／过期、false 和0非空、未知状态安全展示。
- [ ] 实现非模态局部卡片，六类字段及组合表单、显式“发送答复／稍后处理”、来源支持的取消及普通来源文字替代，Markdown 复用既有安全组件。无全屏遮罩／body teleport／inert／全局焦点锁。
- [ ] App 请求捕获完整归属和 operationId，丢回执查询原操作不自动重发；显示记录／送达／不确定，不用 selectedTask 迟读取归属。
- [ ] ChatPanel 新请求不抢输入焦点、收起不反复展开，当前任务以外只角标；保留手机返回／标题／滚动。登录退出清理交互草稿。
- [ ] Kanban 与任务概况区分待你处理、待验收；已结束交互形成只读聊天记录。旧报告问题不激活历史弹窗，不覆盖成果历史改动。
- [ ] `npm --prefix apps/web test`、独立 interaction 测试、`npm --prefix apps/web run build` 通过；浏览器测试六类型／组合、选择后手动提交、切卡片迟到事件、双设备、刷新、断线及桌面1440／手机360布局。测试隔离服务不触碰7331。

## Task 5：集成自测、独立验收与交付（原会话协调）

- [ ] 原会话核对 diff 范围及其他批次成果，补接口／证据缺口，不默认重跑未变快照全套。
- [ ] 开发稳定后运行 Go 全量、race、vet、构建及前端／浏览器受影响测试；实际命令与退出码写入本批验证记录。
- [ ] 读取最新 profiles，由功能验收角色只读独立检查需求、边界与实际浏览器视觉；普通缺陷交原开发者修复并聚焦复验。
- [ ] README／roadmap 更新开发事实，区分本机通过、原生协议假进程验证、未调用真实模型及未部署7331。不得以测试通过声称已更新用户当前页面。
- [ ] 最终向用户说明实现／验证／阻塞与部署状态；不提交、推送或重启服务。
