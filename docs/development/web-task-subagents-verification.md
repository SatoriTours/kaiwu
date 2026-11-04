# 卡片子 Agent 公共观测切片独立验收

日期：2026-10-30
验收者：独立功能验收会话（非实现者）
基线：`/home/jimxl/projects/kaiwu`，main，HEAD `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`，git-dir `.git`，index `.git/index`，Workspace `wks_9e9b9697cff7f277`（local_checkout，同目录不隔离）。
范围：公共后端记录/查询契约 + 卡片/聊天三级只读 UI。不验收真实子 Agent 启动桥（Task 4 未实现，执行写权限仍待用户答复）。

## 结论

| 项 | 结论 |
| --- | --- |
| 公共切片（记录 + 只读查询 + 三级 UI）功能验收 | **通过**（关键断言独立复核，含真实 Go/SQLite/HTTP/socket 与真实浏览器） |
| 截图视觉验收（4 张桌面/360px 截图） | **阻塞**：当前验收模型不支持图片输入，未做视觉判断 |
| 整项“真实子 Agent 派发” | **未完成**（仍缺权限答复与启动桥；不得称真实派发已接通） |

通过不等于整项完成，也不代表 `completed` 子 Agent 已被用户验收。

## 实际命令与结果

全部在 cwd `/home/jimxl/projects/kaiwu`（前端命令在 `apps/web`）执行，未提交、未合并、未推送、未部署、未新建分支/空间、未调用真实 pi 模型。

| # | 命令 | 退出码 | 覆盖 |
| --- | --- | --- | --- |
| 1 | `cd server && GOCACHE=/tmp/kaiwu-go-cache GOMODCACHE=/tmp/kaiwu-go-mod-cache GOTOOLCHAIN=local go test -count=1 ./internal/workbench ./internal/webapi` | 0 | 强制重跑（非缓存）：workbench 24.779s、webapi 10.070s；16 个 Store 测试 + 2 个 HTTP/socket 测试 |
| 2 | `cd apps/web && node tests/subagents.test.ts` | 0 | 7/7 新单元测试（筛选/三级徽标/排序/状态文案/事件去重/版本增量/迟到代次） |
| 3 | `cd apps/web && npm run build` | 0 | `vue-tsc --noEmit` 通过，vite 构建 127 modules，616ms |
| 4 | `cd apps/web && npx playwright test --config playwright.subagents.config.ts` | 0 | 3/3：桌面列表/详情/分页/键盘/作用域隔离；迟到回复/失败保留+重试；360px/离线/重连/真实 Store→socket patch 增量 |
| 5 | `cd apps/web && npx playwright test --project=drawer`（回归，可选） | 0 | 6/6：既有聊天抽屉、项目/任务聊天、draft 与会话身份、移动/深色/嵌套弹窗、冷启动聊天 |
| 6 | 独立黑盒 HTTP 脚本（真实 fixture 二进制 + 临时 SQLite，见下） | 0 | 登录/401/403、摘要不泄露正文、详情分页、越权 404、limit 边界、写路由 404、读操作无副作用 |
| 7 | 独立 UI 断言 `verify/selection.spec.ts`（artifacts 内） | 0 | 卡片徽标打开面板不改变 URL/卡片选择/聊天抽屉；列表优先级与“待核查”文案；Esc 关闭并回焦徽标 |

未重复执行：`npm test` 全量、race、既有全部浏览器套件（主控已跑，按任务边界不并行浏览器）。主控证据仍在 artifacts 同名 `*.log`。

## 契约核对（源码 + 独立行为）

### 摘要与传输

- `State.subagents` 为唯一子 Agent 投影；快照与 patch 只含轻量摘要。黑盒核对：`/api/snapshot` 中每个 run 仅 14 个摘要字段，无 `request`、无工具输入输出、无事件文本；详情才返回 `request` 与 `events`。
- 认证：无 Cookie `POST /api/subagents/detail` → 401；缺 Origin/X-Kaiwu-Client → 403 `csrf`；GET → 404；未知字段/类型错误 → 400 `invalid_json`；`limit` 101 或 -1 → 400 `invalid_input`；跨项目/跨会话/未知 run → 404 `not_found`。
- 只读边界：`/api/subagents/create|append|control` → 404；`/api/commands` 的 `create_subagent` → 400 且不产生记录；socket 写方法 → 400 `socket_error`（Go 测试）。
- 分页：默认 50、上限 100；实测 100 条 → `next=100, hasMore=true`，`after=100` 取到 8 条、`next=108`、`hasMore=false`，两页 sequence 无交集、单调不重放。
- 无副作用：多次详情读取前后 `revision`、`subagents`、`conversations`、`tasks` 完全一致。

### 存储、上限与恢复

- 幂等/冲突：同 ID 同原文（含被截断原文）重放不增 version；同 ID 异原文 409；终态不复活、`unknown` 不自动恢复（Go 测试 + 契约）。
- 截断：单事件 16 KiB、每 run 1 MiB、每 run 2000 条；截断仍保存后续状态与终态；事件上限后状态转换仍写入摘要并保留 ≤4 条受限指纹（Go 测试）。
- 容量：每会话 1000、全库 10000，达到上限拒绝新建且不删历史（Go 测试）。
- 重启：残留 `starting`/`running` → `unknown`（version+1、写 finishedAt），父身份与历史保留；终态不变；schema 3 迁移保留登录、交付、任务/会话/执行历史，旧库可直接新建 run；新于 3 的库拒绝打开。
- 原子性：事件写失败时摘要 version/revision 不前进、无残留事件行（触发器注入测试）。
- UI 无轮询/后台监听：`subagents.ts`/`SubagentBadge.vue`/`SubagentPanel.vue` 内无 `setInterval`/`setTimeout`/`requestAnimationFrame`；详情仅按 `run.version` 增量请求，切范围/关闭即丢弃迟到响应。

### 三级 UI

- 徽标：卡片底部与聊天输入框上方共用；`running>0`、仅 `starting`、有 `unknown`（显式“待核查”，不呈现为零）、仅历史“子 Agent · N”、无记录不渲染；离线追加“· 离线”。
- 列表：running → starting → unknown → 历史，其后按 startedAt 倒序。实测顺序为“运行中 / 待核查 / 已结束”，标题安全显示（`<script>` 仅作为文本，面板内无 script/img 节点）。
- 详情：任务要求、可见回复、工具名/输入/输出、失败标记、截断提示；`completed` 只标“已结束 · 运行结束不代表验收通过”；无模型思考流字段。
- 交互：Esc/返回/关闭、Tab 焦点陷阱、关闭回焦触发元素；360px 面板宽度实测 360；打开详情不改变任务选择、不产生 Agent 生命周期写入（前后 `sessionId/runState` 与 `subagents` 一致）。
- 断线：保留已读内容并显示“连接已断开…重连后更新”，重连后由权威快照 + version 增量补取，实测出现新事件“实时增量”。

## 发现（交还主控，均非阻塞）

1. **（轻微文案偏差）running + unknown 时徽标丢失“子 Agent”**：`formatSubagentBadgeText` 对 `[running, unknown]` 返回 `◉ 1 个运行中 (1待核查)`，Spec 的固定文案是 `◉ N 个子 Agent 运行中`（未知提示本身已满足“不得呈现为零”）。最小复现：`node tests/subagents.test.ts` 第 111–120 行断言值即当前行为；实测卡片徽标文本为 `◉ 1 个运行中 (1待核查)`。建议统一为 `◉ 1 个子 Agent 运行中 (1 待核查)`；不阻塞功能验收。
2. **（仅测试夹具）** `server/internal/webapi/testdata/subagentfixture/main.go` 暴露 `POST /fixture/append` 直接写 Store 且不经过认证，仅用于真实 Store→socket patch 的浏览器验证；它位于 testdata、不会进入生产服务。保留但不得被复制进产品入口。
3. **（记录）** 详情页 `truncated` 只给整条 run 提示，单个事件被截断时不逐条标注；与 Spec“截断必须标识”在 run 级满足，事件级未细分。可作为后续打磨项。

## 未验 / 阻塞

- **视觉验收阻塞**：验收模型不支持图片输入（`read` 返回 “Current model does not support images”）。已核对 4 张截图的 SHA256 与存在性，但未做视觉判断；不以像素脚本、DOM 断言或代码阅读替代。需要具备视觉能力的验收者复核：`list-desktop.png`、`detail-desktop.png`、`list-mobile.png`、`detail-mobile-offline.png`（主控 artifacts 目录，未被我覆盖；我自己的重跑截图写入 `verify/` 子目录）。
- **整项未完成**：真实子 Agent 启动桥（Task 4）、只读/可写权限分支、真实 pi 连接与模型调用、子 Agent 独立 worktree/租约/生命周期/8 驻留总限、真实故障与重启边界均未实现或未验证；本次所有事件均为合成事件，不构成真实派发证据。
- **未由我执行**：`npm test` 全量、`go test -race`、其余浏览器套件（主控负责，避免并行浏览器与重复资源）。
- 未更新 7331 运行服务，未部署构建产物。

## 内容 hash（SHA256）

HEAD：`d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`

```
2778ff3a1526759cc14c46851b6f979c4c7d9b3471dac50aa0a380cea4bd5d9a  apps/web/src/subagents.ts
5837c077228760e503e150f496eda95758811388ba0cfa2feab3b41cb6e4633f  apps/web/src/SubagentBadge.vue
b3914e4a966cbc5e56cc115e728017e78dc112a34e7fde326ed9bd1afc4c95e1  apps/web/src/SubagentPanel.vue
126a4f57c9a12bdd45572540a43d6fe24b5c33d265921af390cf986e3a353c79  apps/web/src/subagents.css
5817d9e2b86334678461a471e16898fcbeaeac992ff4be0087473b0250a8aaae  apps/web/src/App.vue
d3285bdc92e78bbf7d2d0e2663df84b4321b8267f9aa17959433027f88fc9e4c  apps/web/src/KanbanBoard.vue
e3edcd58649f9aa1562612003513dd8977062a19b81edb4c561e1593febfe5aa  apps/web/src/ChatPanel.vue
010281c60b30814e28c6fcdb0268d456d454c95999e6aeeade3379eecd2a0687  apps/web/src/types.ts
790e659816b06b1edb053404bba07de73103a61c3e46cc1cbcc353717f29efe5  apps/web/tests/subagents.test.ts
12ff8a45f2e1bbd7f82b971381fd06e9231d7d4887dd0890ebecf5b62eb4b0e8  apps/web/tests/subagents.browser.spec.ts
956217cfd5a43a1f94329c851e9101266df7dd5df6c81503afa0edb493792581  apps/web/playwright.subagents.config.ts
33fddb31deb71add0bb414b645cfc394504fbcb39dde631b6d8551a621270791  server/internal/workbench/subagents.go
c1685de1dc1f99ef371262a6646def7b0d0e7a1d0396fd2a4a79835b92bdb1b5  server/internal/workbench/subagents_store.go
524b0b2c2cdcff2cc17ece28f56010314403832ee99af11cfbc78aa3dc172e7b  server/internal/workbench/subagents_test.go
8a6b5436f01de9fd59223c25f6aad2a38293701abcb25c3a4e54b6d45661b100  server/internal/webapi/subagents.go
795cad120553776401a7a30fb9483d159219ff545505f7403db87de7b1a19677  server/internal/webapi/subagents_test.go
bb97d4bdc9025917c377ae606c835afe1c7f10acd8b8f95d83b6adc4b2c541a7  server/internal/webapi/testdata/subagentfixture/main.go
6154f9b751405c30fc7f0181ac76ffb3dd5a46fe127165cd32bb4dd6bf8e89d9  docs/development/web-task-subagents-contract.md

a261029de13885b9cdcc7a722a2c8ed13a95862782e00912d9ad9bb1e3851ed8  detail-desktop.png
5634ea51f287620486930da8eb086c5daee467d352727ab920b8fc62584dd827  detail-mobile-offline.png
946ac4193a074e778af3d27fe123d01ea97b3f13c337c8836a3bea18f58160fa  list-desktop.png
69e0d45abfe1f47a0570451de51e611aeb0693d42c476be5293a7a5f1036f89b  list-mobile.png
```

## 证据与临时资源

- 主控证据：`~/.local/state/agent-artifacts/kaiwu/2026-10-30-subagents/`（`*.log` 与 4 张截图，未覆盖）。
- 本次独立证据：同目录 `verify/`（子 Agent 重跑截图、drawer 回归截图、`selection.config.ts`、`selection.spec.ts`）、`/tmp/kaiwu-snapshot.json`、`/tmp/kaiwu-snap-a.json`、`/tmp/kaiwu-snap-b.json`、`/tmp/kaiwu-subagent-fixture.log`（黑盒核对输出）。
- 临时资源已释放：fixture 进程 `SIGTERM` 退出码 0；临时 root 目录与临时二进制已删除；Playwright webServer 随套件退出；`17331/17333` 无监听，无残留 `subagentfixture/playwright/chromium/server.mjs` 进程；未新增后台服务或长期轮询。
- 写入范围：仅本文件与 `verify/` 证据；未修改实现、测试或构建配置。验收期间主控另行更新了 `README.md`/`docs/roadmap.md`（文档），非本次验收写入。

## 整项状态

公共观测切片（记录 + 只读查询 + 三级 UI）按契约功能验收通过，视觉项阻塞。真实子 Agent 派发从未完成：启动桥未实现、执行写权限待用户答复、无真实 pi 连接/模型调用证据。整项保持“未完成”，不据此更新 7331 或宣称可用子 Agent 功能。

## 独立视觉补验（2026-10-30，Codex）

验收者：独立视觉验收 Agent `477bb409-8cc7-4bd2-8604-4a1caa07aab5`（当前 Codex 模型，非实现者、非主控、非前述功能验收者）。本次仅补四张既有原图的视觉判断，保留上文视觉阻塞的历史记录；不重跑功能验收、不修改实现。

**结论：四图视觉补验通过，原“模型不支持图片输入”的视觉证据缺口已在本次范围内补齐。真实子 Agent 派发整项仍未完成，未部署。**

### 实际观察

以 `view_image` 的 `original` 模式逐一实际打开下表四张原图，并对照 Spec“已确认展示”和本报告“三级 UI”核查；哈希仅用于标识证据，未使用像素脚本替代看图。

- **桌面列表（1440×900）**：右侧白色面板与变暗的工作台区分明确，标题、关闭 `×` 和三条列表之间留白充足；每条的任务标题、简短任务、时间与右侧状态有清晰层级。运行中、待核查、已结束均能直接辨认，未见状态与标题重叠。底层卡片仍可辨认，面板保持简洁。
- **桌面详情（1440×900）**：顶部“返回列表”及“执行详情”清楚；任务标题、当前运行状态、运行结束不代表验收通过的说明、任务要求和执行记录依次可读。工具 `read` 的展开输入 `{"path":"README.md"}`、工具输出、回复与时间/类型分隔清楚，未见明显横向溢出或覆盖。
- **手机列表（360×780）**：面板占满屏幕宽度，无桌面背景挤占内容；标题和关闭 `×` 可见。三项标题、状态、说明、时间在左右留白内排列，分隔线清楚；包括含 `<script>` 的较长标题在内，未见文字与状态碰撞或被右边缘裁切。“待核查”保持显式。
- **手机离线详情（360×780）**：顶部“返回列表”、详情标题与“连接已断开，记录可能不是最新，重连后更新。”均清楚可见；离线提示与正文分区明确，已读内容仍完整可读。任务要求中的 `<img ...>` 测试文本自然换行；状态、工具名、收起的工具输入入口、工具输出和回复在屏内清晰排列，未见明显横向溢出或遮挡。记录向首屏下方延伸属于纵向长内容，未将其误判为横向溢出。
- **安全显示的视觉证据**：`<script>…</script>`、`<img ... onerror=...>` 在任务标题、要求或输出中均呈现为可见文本，未在画面中显示为嵌入图片。它们是合成安全测试资料；静态图像本身不能证明脚本从未执行，安全行为仍以此前独立功能验收的 DOM/行为证据为准。

### 缺陷与限制

- 四张指定截图中未发现新的阻塞视觉缺陷。时间、类型和状态采用较小灰字，原图中仍可辨认；本次未做色彩对比度量化或真机可读性测试。
- 既有发现 1 的混合状态徽标省略“子 Agent”、`1待核查` 间距紧，在桌面背景卡片中仍可观察到；沿用上文非阻塞文案打磨结论，不在本次修改。
- 本次为既有截图的独立视觉补验，未再次执行点击、Esc、焦点、滚动、浏览器/build/Go 测试；交互结论沿用前述独立功能验收，四图之外的视口/状态未新增验证。手机详情的工具输入在图中处于收起态，其展开输入仅在桌面详情原图可见。
- 所有截图来自合成事件场景，不构成真实 pi/模型调用、真实子 Agent 派发、权限分支、实际 7331 入口或部署验收证据；原报告相关未完成项继续有效。

### 原图证据与源码对应

证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-10-30-subagents/`。原图未修改或覆盖。

| 原图 | 尺寸 | SHA256 |
| --- | --- | --- |
| `list-desktop.png` | 1440×900 | `946ac4193a074e778af3d27fe123d01ea97b3f13c337c8836a3bea18f58160fa` |
| `detail-desktop.png` | 1440×900 | `a261029de13885b9cdcc7a722a2c8ed13a95862782e00912d9ad9bb1e3851ed8` |
| `list-mobile.png` | 360×780 | `69e0d45abfe1f47a0570451de51e611aeb0693d42c476be5293a7a5f1036f89b` |
| `detail-mobile-offline.png` | 360×780 | `5634ea51f287620486930da8eb086c5daee467d352727ab920b8fc62584dd827` |

执行 `timeout 30s sha256sum -c ~/.local/state/agent-artifacts/kaiwu/2026-10-30-subagents/source.sha256`，退出码 0，13 项全部匹配。当前 checkout 仍为 main、HEAD `d916c2ae08d6a6e3f0293667f6d92ad229cf41f7`、git-dir `.git`、index `.git/index`；既有混合改动保留。

本次唯一写入为向本报告追加本段；未修改源码、测试、配置或截图，未派生 Agent，未新建空间/分支，未提交/推送/部署。未启动临时服务、浏览器或其他需释放的运行资源；无待执行写入工具。
