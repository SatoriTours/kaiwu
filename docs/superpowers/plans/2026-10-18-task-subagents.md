# 卡片子 Agent 运行概览实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement task-by-task. 按项目规则复用Paseo角色、串行写入及独立功能验收，不自动提交或建空间。

**Goal:** 接入真实子Agent，并提供卡片数量→列表→执行详情；先完成不依赖执行写权限选择的观测公共切片。

**Architecture:** Go Store原子维护轻量摘要和独立SQLite事件详情，摘要走原snapshot/patch；认证socket按需分页查询日志。Vue共用徽标及只读面板，独立于原任务状态/聊天。真实启动桥等用户明确子Agent写权限后实施，不虚报闭环。

**Tech Stack:** 既有Go/SQLite/net-http/coder-websocket；Vue3/TypeScript/Vite/Playwright。公共切片不添加依赖、不调用模型。

**Spec:** `docs/superpowers/specs/2026-10-18-task-subagents-design.md`

## Global Constraints

- 用户已批准总体方案；只剩子Agent执行写权限待答，不重问总体方案或执行方法。
- cwd=/home/jimxl/projects/kaiwu，main；混合80文件基线已逐项核对，不reset/stash/提交/推送/发布/另建空间。
- 公共切片不修改agent launch/manager及交付逻辑；不启用MCP、不启动实际子进程、不提供浏览器创建/控制接口。
- Snapshot只含摘要，正文分表；completed不是用户验收通过，重启活动状态为unknown。
- 同一checkout串行写入；开发自测后独立验收，原会话只核对差异和证据，不重复全部测试。

## Review Focus

1. 同ID异参数/重复事件/终态迟到：拒绝冲突、不重复计数、不复活。
2. 旧数据库重开：旧登录/历史/交付不丢，活动run归unknown而非0或完成。
3. 巨量Unicode输出/空事件：字节、数量、总量有界且结束状态仍可保存。
4. 篡改项目/会话/run归属或无登录：HTTP与socket一致拒绝，日志不跨任务。
5. 关闭/切卡/断线后迟到详情：不覆盖新目标，不产生任何Agent生命周期副作用。

---

### Task 1: 持久记录和只读查询公共契约

**Files:** Create `server/internal/workbench/subagents.go`, `subagents_test.go`, `server/internal/webapi/subagents.go`, `subagents_test.go`; Modify `workbench/types.go`, `store.go`, `migration.go`, `webapi/handler.go`, `socket.go`。如需拆分小型类型文件可在这两包新增subagents命名前缀文件。不得改其它任务已有业务逻辑。

**Interfaces:** 按Spec定义 `SubagentSummary`, `SubagentEvent`, `SubagentDetailPage` 和 `(*Store).CreateSubagentRun`, `AppendSubagentEvent`, `SubagentDetail`；输出实际JSON契约到 `docs/development/web-task-subagents-contract.md`，后续UI以它为准。摘要State.subagents + 原子persistWith写分表；重启unknown；认证POST/socket `subagents/detail`。无runtime接入或浏览器写API。

- [ ] 先写并运行失败测试：真实Store创建和身份关联、同ID幂等冲突、事件序列去重/终态、只读分页/限制、Unicode和空事件上限、schema2升级旧数据保留与重启unknown；有效结束状态不会被输出上限阻止。
- [ ] 实现最小数据类型/SQLite存储，使用既有锁与persistWith事务，禁止db事务内回调Store死锁。
- [ ] 先写失败HTTP/socket测试：正确登录+Origin可读；无登录/跨项目/会话/未知字段/大分页拒绝；snapshot/patch只含摘要，不含request和日志；详情无副作用。
- [ ] 实现路由与固定socket方法，不改变Commander接口；测试公共数据不能以AI回复创建。
- [ ] 运行 `cd server && GOTOOLCHAIN=local go test ./internal/workbench ./internal/webapi`，再对这两包race/vet；报告真实命令/退出码/未测。不要重复既有全部浏览器套件。
- [ ] 写契约、测试日志路径、修改清单/内容hash及无待处理写工具声明；不commit。普通缺陷由原开发者修。

### Task 2: 卡片和聊天共用三级只读界面

**Files:** Create `apps/web/src/subagents.ts`, `SubagentBadge.vue`, `SubagentPanel.vue`, `subagents.css`, `tests/subagents.test.ts`; Modify `src/types.ts`, `KanbanBoard.vue`, `ChatPanel.vue`, `App.vue`。按需新增独立Playwright用例/配置/fixture服务，不能重写已有8驻留上限/旧断言或覆盖旧截图。

**Interfaces:** 消费Task1实际契约、Snapshot.subagents和socket `subagents/detail`。纯函数筛选/计数/排序，只读详情分页；父组件传入当前project/conversation和run.version，事件回调不改变选择/Agent生命周期。

- [ ] 先写失败单元测试：running/starting/unknown/仅历史/无记录、按conversation隔离、恶意标题安全文本、迟到请求代次拒绝及分页去重。
- [ ] 实现徽标、列表、详情。键盘可达、Esc/返回/关闭焦点、移动360px可用。unknown/离线显式可见，不把工具结果当成已通过，不显示私有思考。
- [ ] 按version增量更新打开的详情，不加轮询；失败保留已读内容+重试；切任务/项目关闭或重置，迟到响应丢弃。
- [ ] 运行 `cd apps/web && node tests/subagents.test.ts && npm test && npm run build`。先仅影响范围测试；报告实际结果，构建产物保留，不能部署。
- [ ] 新增针对性浏览器测试及专用截图，真实Go+临时SQLite合成事件、不调用模型；覆盖桌面/360px/键盘/离线重连/切卡隔离。不得以合成事件冒充真实派发。
- [ ] 返回变更清单、测试证据、已停止写入；不commit。

### Task 3: 公共切片独立验收与状态记录

**Files:** `docs/development/web-task-subagents-verification.md`（验收仅写此报告，默认不改实现）。原会话阶段更新README/roadmap和本任务台账，不扩大旧批结论。

**Interfaces:** 读取Spec、Task1契约、开发者报告及相对于交接快照的新增diff。当前只验公共切片，整项必须标未完成/权限待答。

- [ ] 独立执行真实SQLite/HTTP/socket关键验证及受影响Go测试；核对登录与既有交互门禁不回退。
- [ ] 独立执行Vue单元/构建和桌面360px浏览器关键交互，实际检查专用截图，报告未验证视觉项。
- [ ] 每个发现有最小复现和失败断言，交原开发者修正；同类根因两轮不收敛再升级，不能无限修补。
- [ ] 结论区分公共切片通过/不通过/阻塞，整项未完成；证据关联内容hash，不称真实模型/pi桥完成、不更新7331。

### Task 4: 真实子Agent启动桥（等待权限答复，不派发）

- [ ] 读取用户关于只读/可写的答复，补写该分支的cwd/worktree/租约/当前未提交修改基线方案和真实pi兼容契约。
- [ ] 保持Go后端及第三方piCLI，隔离内建MCP候选、parent/run代次鉴权、8驻留总限、不得再派生、事件持久化和父会话结果回收。
- [ ] 用真实pi无模型协议探测+固定fakepi先测，真实模型额度调用另行说明并取得对应许可；真实子进程/故障/重启边界通过后才整项独立验收。
