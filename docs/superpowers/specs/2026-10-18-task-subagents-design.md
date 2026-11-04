# 卡片子 Agent 运行概览与执行详情

## 状态与授权

用户已确认真实子 Agent 接入及三级只读展示；原会话负责计划、串行实现和独立验收，不再审批执行方式。2026-10-18 整体写权交接已核对：cwd `/home/jimxl/projects/kaiwu`，main，HEAD `d6f861401af8813f118903b33f44e44b3d0138b6`；80 个混合改动逐文件 SHA 全部匹配 `/tmp/kaiwu-user-interaction-handoff.json`，该 HEAD 不是工作内容快照。

**仍待用户回答的产品权限边界**：子 Agent 仅做只读分析/审查，还是也可在独立 worktree 修改代码。此前已提问，不随通知重复催问；不能把详情只读误解为子 Agent 执行只读，也不能默认为其开放写权限。先实现二者共用的记录、查询和展示，启动/派发桥在明确权限后实现。公共切片完成不表示整个功能完成。

禁止提交、合并、推送、部署、新建执行空间、清理旧成果、加载个人扩展。沿用当前安全 checkout，同组文件仅一个写端；实现、修复、验收复用功能空间 Agent Tab。

## 用户目标

用户在每张任务卡片和任务聊天中直接看见有多少子 Agent 正在运行；点击数量查看列表，再点一个查看其任务及实际执行内容。轻量、实时、不打断原聊天，不提供对子 Agent 独立发消息、暂停、取消按钮，不扩展为源码/diff 编辑器。

## 已确认展示

- 卡片底部、任务聊天输入框上方共用轻量入口。running >0 显示“◉ N 个子 Agent 运行中”；只有 starting 时显示“N 个子 Agent 启动中”；有 unknown 时必须显式提示待核查，不能呈现为准确零。
- 无活动但有历史显示“子 Agent · N”；完全无记录不展示。
- 列表按运行/启动/待核查优先，其后历史；每项显示标题、简短任务、明确状态和时间。completed 只标“已结束”，不意味着用户验收通过。
- 详情展示完整保存的任务、可见回复、工具名称/输入/输出/错误与结果；不显示模型私有思考流，不因缺数据编造内容。截断必须标识。
- 桌面轻量弹层/抽屉，手机适配全屏或底部面板；返回列表、关闭、Esc、键盘焦点可用。打开详情不改变任务选择，不启动/恢复/终止任何 Agent。
- 切项目/任务和迟到请求不得串内容；断线保留可读历史并显示离线/可能过期，重连以权威快照恢复。所有内容作为文本/已有安全 Markdown 渲染。

## 公共数据契约（首个可独立验收切片）

Go Store 为唯一事实源；不解析 AI 自述或进程名猜数量。受控 runtime 将来使用内部 Store API 写记录；浏览器只有只读能力，不能创建/修改 run。

State 增加可选 `subagents: Record<runId, SubagentSummary>`，仅含轻量元数据。Summary 字段：`id, projectId, conversationId, taskId?, roundId, parentSessionId, title, taskSummary, status, profile, startedAt, updatedAt, finishedAt?, version, truncated`。status 白名单 `starting|running|completed|failed|interrupted|unknown`。Profile 复用既有 provider/model/thinking 快照，不包含凭据；不暴露 session 文件、环境或内部令牌。title ≤160 字，taskSummary ≤400 字；由可信 runtime 提供的完整任务 ≤32 KiB，单独持久化。

SQLite 独立表保存详情与事件，避免每次聊天快照携带完整日志。与 summary 的 version/state revision 原子提交。已有数据库增量迁移到 schema 3，保留登录、交付、历史和所有现有字段；新于 3 的数据库拒绝。每会话最多1000条运行记录、全库10000条，达到上限拒绝新建，不静默删除历史。

内部接口在 `workbench/subagents.go`：
- `CreateSubagentRun(summary SubagentSummary, request string) (SubagentSummary, error)`：校验 parent conversation/project/task/session/round 归属、枚举、大小、ID；相同 ID/相同初始参数幂等，异内容冲突。不得由浏览器调用。
- `AppendSubagentEvent(conversationID, runID string, event SubagentEvent) (SubagentSummary, error)`：事件 ID 幂等；服务端分配顺序及时间；不得改 parent 身份。事件 kind `text|tool_start|tool_update|tool_end|status`；字段 `id,sequence,at,kind,text?,toolId?,toolName?,input?,output?,failed?,status?`。状态只能从 starting 到 running/failed/interrupted/unknown，running 到 completed/failed/interrupted/unknown；终态不复活，unknown 不自动恢复。重复同内容不增加 revision，不同内容同 ID 冲突。不存在/归属不符拒绝。
- `SubagentDetail(projectID, conversationID, runID string, after uint64, limit int) (SubagentDetailPage, error)`：返回 `{run,request,events,next,hasMore}`。limit 默认50、上限100、负值或超限拒绝；after 为已消费 sequence，单调分页无重放。

单事件文本合计16KiB、每run内容累计1MiB；超限按UTF-8边界保留有界内容并标记 `truncated=true`，仍允许保存后续状态/结束证据，不能因输出大而停止消费/假报完成。具体截断实现及常量由测试固定；事件数也需有界（每run最多2000条，超限只保留状态转换且总事件数仍有固定上限，或单独保存最终状态）；不可无限空事件绕过字节上限。

重启前残留 starting/running → unknown，并保留完整历史与 parent 身份；不自动重启、不重放、不把未核实的进程说成已退出。已有终态保持。当前切片不创建子进程，所以不能宣称真实 pi 派发已接通。

## 只读传输

新增认证 `POST /api/subagents/detail`，socket 固定方法 `subagents/detail`，请求 `{projectId,conversationId,runId,after?,limit?}`。响应同 Store Page。沿用现有 Cookie、Host、Origin、X-Kaiwu-Client、登录失效检查；缺登录、跨项目/会话伪造、未知字段或超限分页均拒绝。无 start/update/control API。

摘要沿用已有 snapshot/patch 发布；打开详情时按 `run.version` 变化查询增量，并用请求代次绑定 run/c，关闭后停止后续查询，不加定时轮询/后台监听器。请求失败保留已显示内容并提供重试；详情变化时不要强制打断手动上翻阅读。

## 真实接入的已知技术边界（权限答复后实施）

现有 launch 明确 `--no-extensions --no-mcp`，pi 内建无原生 subagent，不能只加计数按钮。纯 Go 受控 MCP 是候选；本机文档确认 `--no-extensions -e builtin:mcp` 可仅启用内建 MCP、direct 工具可无 codemode 使用；隔离 Agent 目录只配置产品自己的服务器，不继承个人 MCP/扩展。尚未做真实连接/模型调用验证，不假定候选已兼容。

运行拥有独立会话身份、实际事件流与服务器生命周期，浏览器断开不能停任务；8个驻留上限不能被子Agent绕过。子Agent不得继续派生，旧令牌/旧进程代次不得派发到新任务。readonly 与 writable 的 worktree/租约/基线交接方案需依据待答权限选择；任何路线均不得无授权自动提交/复制凭据/伪装隔离。真实模型消耗先说明并取相应授权；公共协议/E2E用固定fakepi/事件，不冒充真实模型联调。

## 验收

公共切片：真实SQLite创建/事件幂等/分页/边界/重启unknown/原子性/旧库保留；认证HTTP及socket详情、快照patch；前端徽标/排序/未知离线状态/切换隔离/无副作用；真实Go服务合成事件的桌面与360px浏览器/键盘/截图视觉。明确这是可观察链路，不是已完成真实派发。

整项完成还要求权限分支确定、真实pi桥能力检查、受控子Agent启动到结束/错误/中断/容量/恢复/生命周期验证及相应独立验收。任何必需项无证据结论为阻塞/未完成。
