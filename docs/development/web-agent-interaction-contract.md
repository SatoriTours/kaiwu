# 开悟标准交互：后端最终契约（Tasks 1–3 + 聚焦修正，2026-10-18 冻结）

实现：`server/internal/workbench/interaction.go`、`agent/interaction.go`、`agent/interaction_native.go`、`pirpc/ui.go`、`webapi/interaction.go`、`delivery/manager.go` 等待态。
本文件是 UI 与验收的唯一消费契约；字段名即实际 Go JSON 标签。后端未修改 `apps/**`、README、roadmap、webui/dist。

## 0. 本轮聚焦修正（相对上一版契约）

1. 阻塞问题不再把自动交付置为终态 `blocked`：交付进入**可恢复等待**，保留原周期、授权快照与候选校验；答案形成的新轮次可靠结束后，同一周期自动继续。用户无需重新发起交付。
2. 多个普通阻塞问题**逐条记录**答复，最后一条答复（或取消）后汇总为**一条**用户消息、只派发一次；未答齐不启动依赖流程。原生等待各自独立回应。
3. `allowCancel` 尊重 AI 的显式能力，不再强制；取消不会伪造答案、不会自行放行开发/交付；可执行路径见 §6。
4. 重启不再把已证实 `accepted` 的送达改写为 `uncertain`；只把 `recorded/dispatching` 等未知窗口归为 `uncertain`。

### 第三轮（验收 D1/M2/M3/M5）

5. **D1 等待预算**：`interaction_pending/unanswered/cycle` 的用户等待窗口从交付激活预算（`activeLimitSeconds`）中扣除；`interaction_answering`（AI 轮次实际运行）照常计入。等待起点与累计扣除持久化在交付行（`waitingSince`/`waitExcludedMillis`，内部字段），多次等待累计、重启不重置；原有已消耗活动时间保留，反复提问不能续命。
6. **M2 原生未知方法**：已知 fire-and-forget（`notify/setStatus/setWidget/setTitle/set_editor_text`）仍静默忽略；带合法 ID 的未知方法向上游发 `cancelled:true` 并在轮次写可见 `interactionNotice`，不再静默挂起。
7. **M5 原生决策内容超限**：确认说明 >4000、标题/选项 >160、编辑预填 >5000、选项 0/>20 等一律按“不可表示”取消并写 `interactionNotice`，**不截断后仍允许答复**；确认说明 161–4000 字时字段标签用“请确认”，完整说明保留在 `description`，不隐藏决策内容。
8. **M3 显式门禁**：`confirm_result` 增加显式 `interaction_pending` 检查（不再仅依赖 `task.status` 的间接拒绝），文案为交互提示。

## 1. 快照（`/api/snapshot`、Socket snapshot/patch）

`Conversation` 新增可选数组 `interactions`；`capabilities.interactions = true`。

```jsonc
{
  "schemaVersion": 1,
  "id": "ix_<hex>",
  "version": 1,                        // 提交必须回传；同来源新内容 => version+1，旧 pending => superseded
  "projectId": "p",
  "conversationId": "c",
  "taskId": "t",                       // 项目会话无此字段
  "roundId": "…",
  "sourceMessageId": "…",              // 普通问题产生消息；原生为空
  "source": "assistant_question" | "agent_ui",
  "title": "确认访问环境",
  "description": "可选",
  "fields": [ /* 见 §2 */ ],
  "blocking": true,
  "allowCancel": true,                 // 普通问题＝AI 输出块的显式 allowCancel；原生＝上游支持取消
  "allowTextFallback": true,           // 仅 assistant_question
  "status": "pending" | "answered" | "cancelled" | "expired" | "superseded",
  "statusReason": "可选",
  "supersededBy": "ix_…",
  "createdAt": "RFC3339",
  "expiresAt": "RFC3339",              // 仅原生请求带上游 timeout
  "dedupeKey": "内部（UI 忽略）",
  "native": {                          // 仅 source=agent_ui
    "method": "confirm" | "select" | "input" | "editor",
    "upstreamId": "pi 原始请求 id",
    "runId": "进程代次（UI 不展示）",
    "generation": 1,
    "choices": ["Allow", "Block"],
    "hasTimeout": true
  },
  "response": {
    "operationId": "…",
    "mode": "form" | "text" | "cancel",
    "answers": { "fieldId": true | "option_id" | ["option_id"] | 3 },
    "others": { "fieldId": "其他补充文本" },
    "comment": "可选",
    "text": "文字替代回答原文（mode=text）",
    "delivery": "recorded" | "dispatching" | "accepted" | "not_sent" | "uncertain",
    "note": "送达/排队说明（可直接展示）",
    "messageId": "答复产生的用户消息 id（普通问题，汇总后才出现）",
    "followUpRoundId": "答复形成的新轮次 id（=messageId；交付跟踪用）",
    "answeredAt": "RFC3339",
    "updatedAt": "RFC3339"
  }
}
```

交付摘要新增：`waitingCode` / `waitingReason`（可恢复等待，非终态）。

## 2. 字段（六类）

```jsonc
{
  "id": "network",                    // ^[a-z][a-z0-9_]{0,63}$
  "type": "confirm|text|textarea|single_select|multi_select|number",
  "label": "≤160", "description": "可选 ≤1000",
  "placeholder": "text/textarea ≤200", "unit": "number ≤16",
  "yesLabel": "允许", "noLabel": "不允许",
  "prefill": "textarea ≤5000", "required": true, "allowEmpty": false,
  "allowOther": false, "integer": false,
  "minLength": 0, "maxLength": 2000, "minSelections": 0, "maxSelections": 3,
  "min": 0, "max": 10,
  "options": [{"id":"same_lan","label":"同一局域网","description":"可选","recommended":false}]
}
```

- 选择值只用 `options[].id`；"其他"放顶层 `others[fieldId]`。单选不能同时给选项和其他；多选可并存并计入数量。
- `confirm` 必须显式 boolean；`number` 允许 0，拒绝 NaN/Infinity，`integer=true` 拒绝小数。
- 上限：12 字段／20 选项／请求与答复各 32 KiB；标题 160、说明 4000、单行 2000、多行 5000、补充说明 2000。
- `recommended` 仅展示；服务端不预选。无关约束被拒绝而不是静默丢弃。

### AI 输出块

```html
<kaiwu-interaction>{"title":"…","description":"…","allowCancel":true,"fields":[…]}</kaiwu-interaction>
```

严格白名单 JSON；每轮最多一个完整块；仅在 acknowledged+settled 边界发布。半截/无效/超限不产生请求，`Round.interactionError` 记录并让自动流程 fail closed。同轮同源同内容去重；内容变化递增版本并 superseded 旧 pending。历史 `report.question` 只在新轮次可靠结束时生成一份 textarea 请求（`answer` 字段、支持文字替代），升级不会批量生成历史问题。

## 3. 命令（`POST /api/commands`；Socket `method:"commands"`）

```jsonc
// 提交表单
{"operationId":"…","revision":0,"type":"answer_interaction","projectId":"p","conversationId":"c","taskId":"t",
 "requestId":"ix_…","requestVersion":1,"answerMode":"form",
 "answers":{...},"others":{...},"comment":"可选"}

// 文字替代（仅 allowTextFallback）
{"operationId":"…","type":"answer_interaction", "...":"…", "answerMode":"text","text":"直接说明"}

// 取消（仅 allowCancel）
{"operationId":"…","type":"cancel_interaction", "...":"…"}

// 重试（仅 response.delivery==="not_sent"）
{"operationId":"…","type":"retry_interaction_answer", "...":"…"}
```

- 交互命令不校验全局 `revision`，以 `requestId+requestVersion` 防伪冲突；缺身份/版本 400。
- 同 `operationId` 重放返回原回执；同 id 不同内容 409；重复答复/旧版本/过期/superseded 409。

成功响应＝`Receipt` 字段扁平 + 最新 `interaction`：

```jsonc
{
  "revision": 124, "type": "answer_interaction", "conversationId": "c",
  "createdId": "汇总用户消息 id（尚未汇总时为空）",
  "interactionId": "ix_…", "interactionVersion": 1,
  "delivery": "recorded", "notice": "可选说明",
  "interaction": { /* 用同 id 项替换快照 */ }
}
```

### 多问题排队与汇总（新）

- 同一会话多个普通阻塞问题时，答复逐条持久化，**不立即派发**；`response.delivery="recorded"`，`note` 说明还剩几个问题，`messageId` 为空，`createdId` 为空。
- 最后一条待答问题被答复（或最后一个问题被取消，且已有已记录答复）时，后端把所有已答复项汇总成**一条**用户消息，只创建一轮、只派发一次；所有相关 `response.messageId/followUpRoundId` 指向该消息。`createdId` 只在这次汇总回执中出现。
- 已记录但未汇总/未送达期间，普通聊天与开始开发会被拒绝（提示先答完/重试），避免半答一轮。
- 原生请求不参与汇总：每个请求绑自己的 `upstreamId+runId`，答案直接写回原等待者。

### 送达语义

- `recorded`：已持久化；可能还在等其他问题、等当前轮次结束，或尚未派发。
- `dispatching`：用户消息已创建、原 pi 尚未确认。
- `accepted`：普通问题＝原 pi 已接受该轮 prompt；原生＝响应已完整写入原等待请求（pi 无 ack，这是可证明上限）。都不表示成果完成。
- `not_sent`：可证明未送出；仅此状态可 `retry_interaction_answer`（复用原消息/上游 id）。
- `uncertain`：可能已送出；禁止自动重发，需核对原会话；普通聊天不被它永久阻塞。
- 服务重启：只把 `recorded`/`dispatching` 的未知窗口归 `uncertain`；已 `accepted` 的写入与普通来源的本地取消保持 `accepted`。普通 pending 问题保留；原生 pending → `expired`。
- 取消普通问题＝本地取消（`response.delivery="accepted"`，note 说明不向 AI 发送同意/拒绝）；取消原生＝向上游发送 `cancelled:true`。

## 4. 任务/轮次/交付等待状态

- 普通问题发布时：`Task.status="waiting_user"`、`Execution.phase="awaiting_user"`、`Round.phase="awaiting_user"`，不是 `needs_confirmation`；`Execution.finishedAt/summary` 为空。
- 答复汇总形成的新轮次结束后，才回到 `needs_confirmation` + `awaiting_review` + `Round.phase="finished"`。
- `Round.interactionError` 非空：原文保留，可聊天回复；自动交付以 `interaction_invalid` 阻塞（终态）。
- `Round.interactionNotice`：原生能力限制的可见说明（未知方法/决策内容超限已取消），**仅提示，不单独阻塞交付**；UI 可展示。
- 交付 `DeliverySummary.waitingCode`（周期仍 active，phase 不变）：
  - `interaction_pending`：还有可回答的阻塞问题（**包括运行中轮次里的原生对话框**：pi 被对话框阻塞时 `runState` 仍是 running，但这段时间是用户思考，不是 AI 活动）；
  - `interaction_answering`：已收到答复，依赖轮次正在实际运行（计入激活预算）；
  - `interaction_unanswered`：只剩取消/过期，AI 未收到答案，等待用户新的说明；
  - `interaction_cycle`：依赖链异常，保持等待。
- 等待期间释放任务写租约，让答复能真正运行；恢复时同一阶段重新获取并重新校验候选/指纹。原 `phase`、`policy` 快照、`candidateSHA/tree`、`RoundID`、`Cycle` 不变；不新建周期。
- **激活预算口径**：`ActiveLimitSeconds` 只统计 AI 活动时间 = 墙钟时间 − 已结束/进行中的用户等待（`interaction_pending/unanswered/cycle`）。`interaction_answering` 是 AI 轮次实际运行，计入预算。等待起点/累计扣除持久化，多次等待累计、重启保留；`repair_deadline` 不会因用户思考而误触发。
- **用户等待段的精确结束**：任何新的用户 AI 轮次开始（`send_message`、`start_task`、形成轮次的 `answer_interaction`、`retry_interaction_answer`、运行中答复的边界 flush）时，存储层在命令提交后通知交付管理器该轮的**开始时刻**；等待段在该时刻结束（`interaction_answering`），随后 AI 运行全部计入预算。即使 worker 只在新轮已结束后才发现（例如取消→新说明、失败→重试），也按轮次开始时刻结算，不会把 AI 运行当作等待扣除。取消后如果没有新的用户轮次，等待段保持（`interaction_unanswered`）；新轮结束后若仍有未答问题，则从该观察点重新开始新的用户等待段。
- **原生对话框等待（D3）**：原生 pending 即使会话 `runState=running` 也保持用户等待段（`waitingCode=interaction_pending`）；用户答复/取消/重试的**原始响应实际写入 pi**（`accepted`）时，agent 通过 `Store.NotifyUserTurn` 记录该时刻，等待段在此结束，之后同一轮/后续轮的真实 AI 运行计入预算，即使 worker 很晚才观察到。同一轮连续多个原生对话框各自开启/结束等待段，互不抹除已消耗活动。
- 用户手动 `pause`/`cancel` 不会被答案覆盖：`pause` 仍暂停，`cancel` 结束后不再因答案恢复。

## 5. 服务端门禁

存在 `pending` 且未过期的阻塞问题（普通或原生）时，以下返回 409：

- `confirm_result`（成果验收）、`start_task`（含"开始开发"的 send_message）、合并执行、交付验收 `delivery/accept`、交付恢复 `delivery/control resume`；
- 自动交付所有副作用阶段不再置终态，而是进入上面的 `waitingCode` 等待，零提交/零推送/零修复派发；
- 该周期轮次 `interactionError` 非空 → `blocked(interaction_invalid)`（终态，需人工）。

答案形成的新轮次可靠结束后，同一周期自动继续；不要求用户重新发起交付。

## 6. 取消语义（新）

- 只有 `allowCancel===true` 才展示/接受取消（UI 只显示确实支持的取消）。
- 取消不产生答案、不启动新轮；若它是最后一个待答问题且已有已记录的答复，会触发这些答复的汇总发送（被取消的问题不会出现在消息里）。
- 取消后用户发送的新说明轮次（或失败答复的重试轮次）一旦开始运行，即结束用户等待段；该轮真实运行时间计入激活预算，短轮仍可让同一周期继续。
- 若所有问题都被取消：不发送任何消息；依赖流程继续等待。**可执行路径**：用户在聊天中发送新的明确说明（此时允许）→ 新轮次可靠结束 → 同一交付周期继续。只取消而没有新用户轮次时，开发/交付不会继续。
- 取消与"拒绝/不同意"不等价；确认题的否定必须提交 `answers:{field:false}`。

## 6.1 UI 状态补充（本轮）

- 过期判断由响应式时钟驱动：`pending` 但 `expiresAt` 已过的请求移入历史，只读查看，不再显示可提交卡片；历史显示“已失效”，不提供重试。
- confirm 字段的字段名使用 group 标签，两个决定按钮的可访问名称分别是 `yesLabel`/`noLabel`（不再被字段 label 覆盖）；无 JSON 变化。
- 命令经 Socket `commands` 发送时，客户端以 `reply.status` 与 `reply.data` 判断回执；丢回执后只查询 `operations/:id`，404 时以**同一 operationId** 重试，不新造身份。

## 7. 原生 pi 适配（pi 1.3.0 已核验）

- 支持 `confirm/select/input/editor`；已知 fire-and-forget（`notify/setStatus/setWidget/setTitle/set_editor_text`）不生成表单、不需响应。
- 带合法 ID 的未知方法、无法映射的对话框（选项 0/>20、标题/选项 >160、确认说明 >4000、编辑预填 >5000）→ 立即向上游发 `{"type":"extension_ui_response","id":…,"cancelled":true}` + 轮次 `interactionNotice` 可见说明；不伪造支持、不静默挂起、不截断后继续确认。
- 响应只写原始 record（不生成 `req_N`、不等待普通 response），与命令共用有界 stdin 单写队列；写完成＝`accepted`。
- 绑定 `upstreamId+runId+generation`；进程更换/重启后旧答复不会发给新等待者；多个原生等待各自独立回应，不参与普通问题汇总。
- `select` 选项 id 为 `o0…oN`，提交后转回上游原始字符串；`confirm` 提交 `confirmed:false`；`input/editor` 允许空字符串。
