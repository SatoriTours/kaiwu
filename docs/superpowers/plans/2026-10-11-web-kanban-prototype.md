# Web 功能专题看板交互原型实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. 本项目的 Paseo 编排与授权边界优先：一个编码 Agent 串行完成全批，不为每个 Task 创建编码者或评审者；完成后一次独立验收。未经授权不提交、不清理未提交成果。

**Goal:** 将现有旧布局原型改为可操作的功能专题看板，验证列增删／其他收纳、任务五态角标及右侧项目／任务聊天隔离。

**Architecture:** 沿用 `docs/design/` 的本地无依赖原型，不创建生产 Web 工程。纯浏览器状态模块负责稳定专题／任务／Conversation ID 和确定性转换，HTML 负责 DOM、布局、表单及显式模拟异步回复；Node 内置测试只作开发验证，不是 Node 后端。

**Tech Stack:** 原生 HTML/CSS/浏览器 JavaScript；本机 Node v24.20.0 的 `node:test`；本机 Chromium，通过 CDP pipe 测试 `file://` 页面，不安装 npm 依赖、不启 HTTP 服务或调试端口。

**Spec:** [Web 工作台设计](../specs/2026-10-11-web-workbench-design.md)，本批只实现“本批范围与已确认规则”、§4 和 §5.3 的原型范围；其余真实业务属于后续批次。

**Status:** 用户在本会话明确要求“继续开发”，本地原型已实现，Task 1 的 15 项测试和 Task 2 的 44 项浏览器检查通过。Task 3 独立验收和修复复验完成，0 Critical／0 Important／1 焦点 Minor，见 [本批验证](../../development/web-kanban-prototype-verification.md)。未接入生产业务或真实模型，Go 未修改。

**实际执行调整：** 默认 Paseo daemon 停止／不可连接，未启动它或声称沿用无法查询的 profiles；当前协调 Agent 串行完成编码，使用现有工具的同一个只读审查 Agent 做独立验收和修复复验。仓库无 HEAD，保留当前目录单写入例外，不创建 worktree／提交；用手工 ledger 和文件 SHA 记录。下方 profile／会话 ID 是准备阶段的信息，不能据此猜当前执行目标。状态测试直接运行测试文件可显示全部 15 个 `node:test` 案例，`node --test` 在本环境仅显示文件级汇总。

**步骤证据边界：** 状态模块缺失、旧 HTML 缺少看板及手机阅读位置反例均观察到 RED 后修复；部分 UI 场景先实现后补充集成检查，并未逐项独立执行 RED。Task 2 Step 6 中“每个新行为先观察失败”的严格要求未全部执行，以下勾选仅表示功能与检查实际完成。Esc、原生 Enter 和截图刷新另修正 CDP 驱动事件等待，不冒充产品修复。

Task 2 未勾选项的实际范围：Step 4 的名称空白／长度边界在纯状态层验证、未逐一在 DOM 验证；Step 6 的集成行为已测，但并非逐项先 RED；Step 7 的原生键盘选择、组合输入、Esc 和响应式已实现，DOM 重建后焦点仍有 Minor，未做完整无障碍验收。

## Global Constraints

- 列是功能专题，可手动添加／删除；不是“待办／进行中／已完成”状态列。
- 每张卡片是一个任务，右上角角标显示 **未开始、执行中、需要确认、执行完成可查看结果、执行失败** 五种状态；状态变化不改变专题归属。
- 增加“其他”专题；删除一个专题时，其所有任务转入“其他”，只改变分组，不删任务、不停执行、不改任务对话、草稿或结果，也不删 worktree。
- 未选卡片显示项目对话；选卡片显示该任务对话。不同范围不能串消息。
- 本批制作模拟原型，不连接现有 Go 服务或 pi，不发送真实请求，不执行 Git。
- PC（Windows/macOS/Linux）与 Android 首版目标保留。
- 生产前端选型仍待确认，原型不需先安装 Vue／Vite；自有后端全部 Go。
- 所有页面和结果持续注明模拟数据；刷新重置。不得读取个人配置、凭据或真实会话。
- 不改 Go 源码／依赖、生产协议／API、Leaf 或原生工程，不安装全局工具、不进行真实模型产品联调。
- 不提交、合并、推送、打 Tag、发布或删除任何既有工作区。仓库 unborn main；不为 worktree 擅自创建基线提交。

## 原型实现细节（随本计划审核）

- 保底列 ID 固定为 `other`，显示名“其他”，不可删除；新专题追加在它前面。专题名称去首尾空白，1–40 个 Unicode 码点，拒绝同名；所有文本按不可信内容渲染。
- 新建任务选择专题；需求必填且最多 5000 个 UTF-16 单元，标题可选且最多 80 个 UTF-16 单元，未填标题取需求前 20 个 Unicode 码点。默认 `other`、状态未开始；失效专题回退其他并显示提示。创建后选中任务，保留原范围草稿。
- 本批不实现专题重命名、拖拽、手动移卡片、路由、筛选／归档或多项目管理。原有模型、审批、停止等示例不继续冒充已实现能力；必要说明可保留，但不是本批测试或真实执行入口。
- 提供“演示状态”控件切换选中任务的五种角标；执行完成显示固定结果样例，执行失败显示固定失败说明。不自动生成验收／Git 授权，不因普通发送消息改变任务状态。
- 项目聊天与每个任务聊天独立保存内存历史、草稿及阅读位置；显式“返回项目对话”。重复点击同一卡片不清草稿；点看板空白不取消选择。
- 模拟发送以点击时的 Conversation ID、文本和草稿版本捕获归属；300ms 后模拟接受，再过 800ms 模拟回复。晚到的接受／回复只更新原范围，不能清另一范围草稿或抢回当前选择；原范围新草稿也不能被旧接受清空。
- 宽度 ≥1024px 为看板＋右侧聊天两栏，聊天约 380–420px；小于 1024px 为看板／聊天两视图。360px 用专题选择控件看单列，切专题不换发送范围。页面不能整体横向溢出。
- 顶部保留模拟连接切换；断线保留内容、选择和草稿，禁用写动作，不取消已捕获模拟发送。独立草稿仅内存保存，不新增浏览器持久化。

## 文件职责

| 文件 | 改动 | 职责 |
| --- | --- | --- |
| `docs/design/web-prototype-state.js` | 新增 | 无 DOM、网络或计时器的状态工厂／动作转换；经典脚本供页面使用，同一导出供 Node 测试 |
| `docs/design/tests/web-prototype-state.test.cjs` | 新增 | 专题／任务／Conversation 的行为回归，用 Node 内置测试 |
| `docs/design/web-prototype.html` | 修改 | 替换旧聊天中心布局，加载同目录状态脚本，提供看板、右侧聊天与显式模拟交互 |
| `docs/design/tests/web-prototype-browser.cjs` | 新增 | 自管独立 Chromium 临时 profile、CDP pipe、真实 DOM／键盘／响应式验证及截图 |
| `docs/design/web-desktop.png`、`web-mobile.png` | 更新 | 本批验证后生成新截图；原有旧布局的检查记录仍保留其历史范围 |
| `docs/development/web-kanban-prototype-verification.md` | 新增 | 当前原型的命令、结果、独立验收和未验证项；不覆盖历史 20 项结果 |
| `README.md`、`TODO.md`、`apps/web/README.md`、`docs/roadmap.md`、Web 规格／历史验证文档 | 修改 | 同步真实完成状态、入口和历史证据边界 |

## Review Focus

1. 删除正在聊天／有迟到回复的任务所在专题，任务仍归入其他且聊天／草稿／结果不丢：Task 1 的 `delete_topic_preserves_task_and_conversation`、Task 2 的真实 DOM 场景。
2. 模拟接受晚于切范围或同范围的新输入，不能错清草稿：Task 1 的 `late_accept_keeps_new_draft`、Task 2 的 A→B 与 A 新草稿场景。
3. 表单／确认面板打开后对象失效，不能用当前选择替代原目标：Task 1 的失效专题回退与失效 ID 测试，Task 2 的捕获目标 DOM 检查。
4. 许多专题、长 Unicode／HTML 文本、五种角标，在手机和桌面仍可操作且不执行 HTML：Task 1 的命名边界、Task 2 的文本注入／长标题／多列宽度检查。
5. 脚本异常、断线期间待完成模拟回调、键盘／组合输入和焦点恢复，不能静默改变范围或产生意外发送：Task 2 的运行异常、断线、键盘和 IME 检查。

---

### Task 1: 稳定专题、任务和聊天状态

**Files:** Create `docs/design/web-prototype-state.js`; Test `docs/design/tests/web-prototype-state.test.cjs`.

**Interfaces:**
- Produces: 同一 API 以浏览器全局 `KaiwuPrototypeState` 和 CommonJS `module.exports` 导出，不要求 module loader／本地服务。
- `createDemoState(): State`：单项目 `projectId='kaiwu'`，`selectedTaskId=null`；示例专题和至少五张覆盖五态的任务；保底 `other`。任务 ID、专题 ID 和 Conversation ID 独立稳定。
- `currentConversationId(state): string`：项目为 `project:kaiwu`；任务为 `task:kaiwu:<taskId>`。
- `dispatch(state, action): {state: State, error: null|string, createdId?: string, notice?: string}`：不改输入 state；错误返回原 state，使用稳定错误码；成功返回更新后的 state。
- `State` 包含 `topics: {id,name}[]`、`tasks: {id,topicId,title,request,status,result}[]`、`selectedTaskId: string|null`、`conversations: Record<id,{messages: {id,role,text}[],draft:string,draftRevision:number}>` 及单调生成 ID 的计数器。阅读位置由页面按 Conversation ID 保存，不放入纯状态模块。
- `status` 仅原型值 `idle | running | needs_confirmation | completed | failed`，对应五种角标；不作生产协议。
- 动作：`add_topic{name}`、`delete_topic{topicId}`、`add_task{topicId,title,request}`、`set_task_status{taskId,status}`、`select_task{taskId:string|null}`、`set_draft{conversationId,text}`、`send_message{conversationId,text,draftRevision}`、`receive_reply{conversationId,messageId,text}`。动作类型由 `type` 字段传入。
- 删除专题只更新相关 task.topicId 后移除列；保护其他。新建任务失效专题回退其他，返回 notice。发送追加 user 消息，仅当前草稿版本匹配才清空；接收只向显式 conversationId 追加 assistant 消息，不改选择或草稿。重复 reply messageId 不重复追加；send_message 返回 createdId 作为该 user 消息 ID，页面据此生成稳定的 reply messageId。

- [x] **Step 1: 写失败状态测试。** 覆盖 `starts_in_project_scope`、`add_topic_appends_before_other`、`topic_name_validation`（空白、40／41 码点、同名、HTML 作为普通名称）、`delete_topic_preserves_task_and_conversation`、`delete_topic_cancel_is_no_dispatch`、`protect_other`、`delete_empty_topic`、`stale_topic_new_task_falls_back`、`five_statuses_never_move_topics`、`invalid_task_or_status_keeps_state`、`independent_conversation_drafts`、`late_accept_keeps_new_draft`、`late_reply_stays_in_original_scope`、`duplicate_reply_is_ignored`。删除后深比较原任务除 topicId 外所有字段及 Conversation；逐态断言专题 ID 不变。
- [x] **Step 2: 运行 RED。** Run: `node --test docs/design/tests/web-prototype-state.test.cjs`。Expected: 因状态模块／导出尚未实现而失败；记录实际原因，不能以语法错作为 RED。
- [x] **Step 3: 实现上述 API。** 不访问 DOM、网络、存储、个人配置或计时器；错误码为 `invalid_name`、`duplicate_name`、`protected_topic`、`not_found`、`invalid_input`、`invalid_status`。状态转换不引用 UI 当前选中对象来替代动作 ID。
- [x] **Step 4: 运行 GREEN。** 同一 `node --test` 命令 Expected: 全部测试通过；`node --check docs/design/web-prototype-state.js` Expected: 无语法错误。
- [x] **Step 5: 保存任务记录。** 记录 RED／GREEN 与命令；不 git commit。Task 2 使用这里的 API，不再复制状态转换。

### Task 2: 专题看板、右侧双范围聊天与真实浏览器检查

**Files:** Modify `docs/design/web-prototype.html`; Create `docs/design/tests/web-prototype-browser.cjs`; Test Task 1 suite + browser checks.

**Interfaces:**
- Consumes: Task 1 的 `KaiwuPrototypeState`、State、Conversation ID、八种动作及五个原型 status 值。
- Produces: 直接打开 `docs/design/web-prototype.html` 可操作；没有 HTTP/WebSocket 业务连接，页面持续声明模拟数据／刷新重置。
- Produces: `node docs/design/tests/web-prototype-browser.cjs --screenshots <directory>` 自管浏览器，成功退出码 0；失败退出非 0，并输出具名检查与错误。截图写入显式目录 `web-desktop.png`、`web-mobile.png`。
- 测试用 `CHROMIUM_BIN`（缺省 `chromium`）选择已有二进制；`--remote-debugging-pipe`，独立临时 user-data-dir，禁止加载个人 profile／扩展。不启调试端口；每项 CDP 请求／条件等待最长 8s，整轮最长 90s。脚本 try/finally 请求关闭浏览器，最长 5s 无退出则终止自己的浏览器子进程并等待退出，再清理它自己创建的临时目录，失败保留诊断；不能清理来源不明目录。
- DOM 动作、表单和异步回调携带稳定对象 ID；render 拆分看板／当前聊天，更新隐藏范围不能重建正在输入的其他范围编辑器。

- [x] **Step 1: 先编写浏览器驱动与失败断言。** 首屏应为专题看板＋项目聊天并可找到“＋专题”；选择任务后可找到“返回项目对话”；至少五卡片右上角可读状态覆盖五态。驱动监听 `Runtime.exceptionThrown`／页面 HTTP 请求并将意外异常、业务网络请求判失败。
- [x] **Step 2: 对现有旧 HTML 运行 RED。** Run: `node docs/design/tests/web-prototype-browser.cjs --screenshots .superpowers/sdd/2026-10-11-web-kanban-prototype/red-screenshots`。Expected: 浏览器成功启动／加载，随后在“专题看板／项目聊天尚未实现”断言失败；浏览器无法启动不是有效功能 RED，先解决验证环境，不能跳过。
- [x] **Step 3: 实现宽屏看板与专题管理。** 加载经典状态脚本，删除旧常驻执行详情栏；实现专题创建／错误、删除确认／取消、其他保护、任务创建／默认专题，所有文字转义或 textContent，表单目标 ID 由打开时捕获，不读取临时当前选择。
- [ ] **Step 4: 增补专题／卡片浏览器场景并运行。** 空专题增删、删除含五态任务专题到其他且仍选中同一任务、取消删除无变化、其他不可删、空白／重复／过长名称、HTML 文本不生成元素、五态角标变化不搬列、完成／失败摘要可查看、普通发送不升级执行状态。失效专题表单回退由 Task 1 单位反例覆盖，不为制造此场景新增跨标签功能或产品测试 hook。
- [x] **Step 5: 实现聊天隔离与有标识的模拟发送。** 项目／任务历史、草稿和滚动按 Conversation ID 保存；捕获发送内容与版本后模拟定时接受／回复；页面按 Conversation ID 跟踪待接受发送，只禁用该范围的重复发送（按钮和快捷键共用检查），不能阻止其他范围聊天。显式返回、同卡重复点击、模拟离线及恢复；刷新不承诺恢复。
- [ ] **Step 6: 增补并运行聊天失败反例。** 项目／A／B 三份草稿与历史；发给 A 后切 B、B 新输入在 A 接受／回复后不变；发给 A 后 A 又写新草稿，旧接受不清新字；删除 A 所在专题后迟到消息仍归 A；点击空白／重复点击不切范围；断线禁发并保留已捕获回调归属；滚动独立恢复。每个新行为先观察失败再实现。
- [ ] **Step 7: 实现响应式与键盘操作。** 卡片可用 Enter／Space 选择，Ctrl/Cmd+Enter 发送、Enter 换行、`isComposing` 不提交；删除／创建对话框焦点限制、Esc 取消、关闭归还可用触发点。小屏两视图和专题选择，切视图／专题不改聊天范围。
- [x] **Step 8: 增补并运行布局／输入检查。** 1440×1100、1024×900、768×1024、360×900；多列、长标题与角标不重叠、不造成 body 横向溢出。对话框在360px可见且键盘可用；焦点可见、选择用文字／图标而非只靠颜色；中文组合输入与快捷键不重复发送。用 DOM 条件等待而非把固定 sleep 作为成功证明。
- [x] **Step 9: 完整 GREEN 与截图。** Run: `node --test docs/design/tests/web-prototype-state.test.cjs`；`node docs/design/tests/web-prototype-browser.cjs --screenshots .superpowers/sdd/2026-10-11-web-kanban-prototype/screenshots`。Expected: 两套全部通过、无意外页面异常／业务网络请求、浏览器退出回收；截图显示当前布局。截图需 read 工具人工查看，不以文件生成代替检查。
- [x] **Step 10: 保存任务记录。** 记录逐组 RED／GREEN、实际浏览器版本／尺寸；不 git commit。无真实 Agent、Go 业务或跨浏览器通过声明。

### Task 3: 实际记录、独立验收和本地交付

**Files:** 新验证文档、两张截图及文件职责表中明确的入口文档；允许针对审查反例修复 Task 1／2 文件。

**Interfaces:**
- Consumes: Tasks 1／2 的全部实际测试输出、截图和编码成果；旧 20 项检查只作历史。
- Produces: README 和 Web 入口可定位当前专题看板原型与新验证记录，生产工程／真实业务未完成项保留。

- [x] **Step 1: 整理交付和冻结。** 建立 `docs/development/web-kanban-prototype-verification.md`，写实际命令与输出、通过／失败、截图人工检查、原型限制；更新两张截图和入口，将旧检查与新结果分开。任务未实际执行的项目不得勾选。
- [x] **Step 2: 执行新一轮全量检查。** 运行 Task 2 Step 9 的两个命令、状态模块／浏览器驱动语法检查及仓库 Markdown 本地链接检查。Expected: 测试／语法通过、链接损坏为0；记录数量以真实输出为准。核对 Go 源码、旧 RPC 数据和非本批文件未被改动。
- [x] **Step 3: 一次独立验收。** 使用最新“方案设计，架构思考”配置，优先核对已有候选 `27abc3d6-d302-4b49-85f4-88d89718aecc` 的准确 cwd、状态和实际设置；本轮 CLI 列表未返回该候选，不能声称已复用。不可用时仅创建一个最新架构配置的只读验收 Agent。提供规格／计划、冻结文件列表和测试日志，要求只读，实际重跑两套测试并检查五项 Review Focus。不新建 worktree、不调用产品 pi、不在 reviewer 侧直接改代码。
- [x] **Step 4: 处理真实问题。** Critical／Important 交回原编码 Agent 一次修复，每个问题新增反例 RED→GREEN 并重跑两套测试；未解决则明确阻塞，不声称通过。Minor 记录给用户，不无授权扩展范围。
- [x] **Step 5: 汇总交付。** 明确编码自测、协调核对、独立验收分别执行了什么；报告未做 Firefox／Safari／真实手机／完整无障碍／生产业务／真实 Agent 联调。不提交、推送、归档或清理本批成果。

## 推荐执行方式与资源边界

- 当前协调 Agent 已承担需求／架构角色，直接完成本计划，无需另派同职责规划者。
- 最新实际前端 profile 为 **“opencode deepseek”**，用途说明“方案设计好之后，前端界面的绘制、交互的设计”；pi provider、模型 `opencode-go/deepseek-v4.1-flash`、high，未指定 mode。执行前重新读取实际配置并沿用其值，不依据“Gemini”旧名称猜模型。
- 建议由一个前端 Agent 串行完成 Tasks 1–2 和 Task 3 文档交付；架构配置做一次只读独立验收。复用同一编码者修复，不为每个任务创建新 Agent。
- 空仓库无 HEAD，不创建 worktree、不制造提交。计划审核同时确认本批在当前 `/home/jimxl/projects/kaiwu` local workspace 串行编码的例外；编码期间协调 Agent 不并发编辑主 checkout，只协调／阅读。
- Paseo 派发使用当前 local workspace `wks_0a6880df1b60b7e7`，原会话 `4d13b530-a48e-4980-831d-671d0fff4713` 始终协调交付；不改固定 workspace 名称，不要求用户转移会话。开始前再次核对无其他编码者。
- 独立验收配置当前为 pi／`openai-codex/gpt-6.1-sol`／high，未指定 mode；实际复用设置必须核对。编程／验收 Agent 会消耗相应模型额度，应随本计划执行方式一并确认；不等于产品真实 pi 联调。
- 独立 scratch／ledger 路径 `.superpowers/sdd/2026-10-11-web-kanban-prototype/`。保留未提交日志／记录，不读取、清理或覆盖别的计划 scratch；因无提交，不使用要求 HEAD／commit range 的辅助脚本冒充审查范围。

## 计划自查

- 覆盖：专题管理／其他／五态是 Task 1–2；独立范围、草稿版本和迟到回复是 Task 1–2；真实 DOM、输入、宽度和截图是 Task 2；独立验收／历史边界／入口是 Task 3。
- 接口：Task 2 只消费 Task 1 的八种动作和固定 ID，不再写第二份 reducer；没有把列名或当前选中任务用作异步目标。
- 执行边界：未冻结生产 API／Task enum，没有接入 Go、身份／存储／MCP／worktree 生命周期，也没有审批／模型／停止的新生产实现。
- 五项 Review Focus 各有具名单位或 DOM 场景；环境失败、生成截图、历史浏览器结果不代替本批通过。
- Git／工作区／模型额度与项目授权边界明确；步骤不要求 commit、worktree 或全局安装。
