# 卡片子 Agent 只读数据契约（公共切片 Task 1）

状态：公共切片（记录 + 只读查询）实际实现契约，由 `server/internal/workbench`、`server/internal/webapi` 的测试固定。真实子 Agent 启动桥（Task 4）尚未实现，执行写权限仍待用户答复；本契约不承诺任何真实派发能力。

- Go Store 是唯一事实源；运行记录只能由受控服务器端 runtime 通过内部 Go API 写入。
- 浏览器只有只读能力：没有创建/修改/控制 run 的 HTTP 或 socket 方法。
- 摘要走既有 snapshot/patch；正文（完整任务 + 事件）单独持久化，按需分页查询。

## 摘要：`State.subagents`

`GET /api/snapshot` 与 socket `snapshot`/`patch` 中的 `state.subagents` 为 `Record<runId, SubagentSummary>`，键为 `runId`。

```json
{
  "id": "run-api",
  "projectId": "59568f11f290c97479917b254e80d1ae",
  "conversationId": "dc463a68df1e35b526a898ae330b67c7",
  "taskId": "eacad08cd30998e577829c4a38e70234",
  "roundId": "a3fdbdf57376215640ac0e076535e527",
  "parentSessionId": "kaiwu_dc463a68df1e35b526a898ae330b67c7",
  "title": "检查接口",
  "taskSummary": "只读详情",
  "status": "completed",
  "profile": { "provider": "test", "model": "fake", "thinking": "high" },
  "startedAt": "2026-10-18T13:56:42Z",
  "updatedAt": "2026-10-18T13:56:42Z",
  "finishedAt": "2026-10-18T13:56:42Z",
  "version": 6,
  "truncated": false
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | string | runId，`^[A-Za-z0-9_.:-]{1,100}$` |
| `projectId` / `conversationId` | string | 归属范围，详情查询必须同时匹配 |
| `taskId` | string? | 省略表示项目对话（无卡片）；由会话绑定推导 |
| `roundId` | string | 父轮次 ID |
| `parentSessionId` | string | 父会话 SessionID，不含 session 文件路径 |
| `title` | string | ≤160 字 |
| `taskSummary` | string | ≤400 字 |
| `status` | string | `starting`/`running`/`completed`/`failed`/`interrupted`/`unknown` |
| `profile` | object | provider/model/thinking 快照；不含凭据（`credentialSource` 永远为空） |
| `startedAt` / `updatedAt` / `finishedAt` | RFC3339 string | `finishedAt` 仅在终态出现（omitempty） |
| `version` | number | 每次摘要变化 +1；详情按它判断是否需要增量刷新；同 ID 同原文重放不变 |
| `truncated` | boolean | 内容被截断，或达到事件上限后事件被丢弃；始终出现 |

摘要绝不包含完整任务、事件正文、工具输入输出、session 文件路径、环境或令牌。`completed` 只表示运行结束，不代表用户验收通过。

## 详情：`POST /api/subagents/detail`

请求（未知字段、错误类型一律 400）：

```json
{ "projectId": "...", "conversationId": "...", "runId": "...", "after": 0, "limit": 50 }
```

- `after`：已消费的 `sequence`，省略/0 表示从第一条开始。分页单调，不重放。
- `limit`：默认 50，上限 100；负值或 >100 → 400。

响应 200（实测样例，节选自真实 HTTP 响应）：

```json
{
  "run": { "...": "SubagentSummary，见上" },
  "request": "SECRET-API-REQUEST-完整任务正文",
  "events": [
    { "id": "ev-1", "sequence": 1, "at": "2026-10-18T13:56:42Z", "kind": "text", "text": "可见回复" },
    { "id": "ev-2", "sequence": 2, "at": "2026-10-18T13:56:42Z", "kind": "tool_start", "toolId": "tool-a", "toolName": "read", "input": "{\"path\":\"a.go\"}" },
    { "id": "ev-3", "sequence": 3, "at": "2026-10-18T13:56:42Z", "kind": "tool_end", "toolId": "tool-a", "toolName": "read", "output": "ok" },
    { "id": "ev-4", "sequence": 4, "at": "2026-10-18T13:56:42Z", "kind": "status", "status": "running" },
    { "id": "ev-5", "sequence": 5, "at": "2026-10-18T13:56:42Z", "kind": "status", "status": "completed" }
  ],
  "next": 5,
  "hasMore": false
}
```

事件字段：`kind` ∈ `text|tool_start|tool_update|tool_end|status`；`sequence` 与 `at` 由服务端分配（调用者传入值一律被覆盖）；`text?`/`toolId?`/`toolName?`/`input?`/`output?`/`failed?`/`status?` 按需出现。`status` 只出现在 `kind=status` 的事件上。不返回模型私有思考流。

幂等与重放（按调用者的原文判定）：

- 事件指纹取自调用者原文（完整的 `text`/`input`/`output`/`toolId`/`toolName`/`failed`/`status`/`kind`/`id`），不含服务端分配的 `sequence`/`at`。
- 已录入事件的同 ID 同原文重放返回当前摘要，不增加 `version`/revision；即使存储时内容已被截断（单事件 16 KiB、每 run 1 MiB、toolId/toolName 超长），原样重放也幂等。
- 已接受的状态事件原样重放同样幂等，不因当前状态已经推进（`running→running`）或已是终态（`completed→completed`）而被拒，也不会复活或改写状态。
- 同 ID 异原文一律 409 `conflict`（已录入事件与达到上限后仍被接受的状态事件都留指纹）。
- 达到每 run 2000 事件上限后被丢弃的非状态事件不录入、不留指纹：重复发送只会再次被丢弃（可能置 `truncated=true`），服务端不声称能判别其同 ID 冲突。

`next` = 本页最后一条 `sequence`（无事件时等于请求的 `after`）；`hasMore` 为 true 时以 `next` 继续查询。

错误：

| 场景 | HTTP | code |
| --- | --- | --- |
| 无登录 | 401 | `unauthorized` |
| 缺少 Origin / X-Kaiwu-Client | 403 | `csrf` |
| 未知字段、错误类型、limit 越界 | 400 | `invalid_json` / `invalid_input` |
| 跨项目 / 跨会话 / 未知 run | 404 | `not_found` |
| 非 POST（如 GET） / 未知路径 | 404 | `not_found` |

## 详情：socket `subagents/detail`

与方法 `commands`、`delivery/*` 相同：`{"id":"...","method":"subagents/detail","body":{...}}`，`body` 与 HTTP 请求体一致，回复 `{"type":"reply","id":"...","status":200,"data":<上面的响应体>}`。业务错误沿用真实 HTTP 状态与 `error.code`（例如 404 `not_found`）；不支持的 socket 方法返回 400 `socket_error`。

## 上限与截断（服务端固定）

| 项 | 值 |
| --- | --- |
| 标题 / 任务摘要 | 160 / 400 字（超出 400 `invalid_input`） |
| 完整任务 `request` | ≤32 KiB（超出 400；不静默截断） |
| 单事件正文合计 | 16 KiB（按 UTF-8 边界截断并置 `truncated=true`） |
| 每 run 正文累计 | 1 MiB（同上；截断后仍保存后续状态与结束证据） |
| 每 run 事件数 | 硬上限 2000；超限后非状态事件被丢弃、不录入（置 `truncated=true`），状态转换仍写入摘要并保留受限指纹（≤4 条，run 内状态迁移最多两次），终态不丢且可幂等重放 |
| 事件 ID | 幂等：同 ID 同原文（按调用者内容指纹，含截断前原文）不增加 `version`；同 ID 异内容 409 `conflict`；被丢弃事件无指纹，不承诺冲突判别 |
| 每会话 run 数 / 全库 run 数 | 1000 / 10000；达到上限拒绝新建，不删除历史 |
| 状态迁移 | `starting→running/failed/interrupted/unknown`，`running→completed/failed/interrupted/unknown`；终态不复活，`unknown` 不自动恢复 |
| 重启 | 残留 `starting`/`running` → `unknown`（`version+1`，写 `finishedAt`），历史与父身份保留，不自动重启 |

## 内部存储（不对外暴露）

- 运行行 `subagent_runs.body`：创建指纹、完整任务、累计内容字节、以及在事件上限后仍被接受的**状态事件受限指纹**（≤4 条）。
- 事件行 `subagent_events`：`(run_id, sequence)` 主键、`(run_id, event_id)` 唯一，`fingerprint` 列保存调用者原文指纹（用于幂等判定），`body` 保存已截断但可直接返回的事件 JSON。
- schema 版本 3（未发布）；本切片只新增上述两张表，不改动既有表。

## 写边界

- 内部 Go API（仅服务器端 runtime 可用）：`(*workbench.Store).CreateSubagentRun`、`AppendSubagentEvent`、`SubagentDetail`。
- 无公开写入口：`POST /api/subagents/create|append|control` 一律 404；`POST /api/commands` 的 `create_subagent` 等类型返回 400 且不创建记录；socket 同样无写方法。
- 摘要是 snapshot/patch 的唯一子 Agent 内容；`request` 与事件只在详情接口返回。
