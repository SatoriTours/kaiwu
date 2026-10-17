# 任务效果预览与确认完成：最终独立验收

**结论：本批独立验收通过。最终稳定源码未发现未关闭的 Critical / Important；保留 1 项已接受的非阻塞 Minor。**

最终核对时间：2026-10-17 03:02（本机时区）。本结论覆盖当前代码只读审查、完整 Go 单测/race/vet、补充故障测试及前端状态测试；最终浏览器集成验收由原协调 Agent 单独负责，不以 Go 测试替代浏览器、上线或真实模型验收。

## 范围与操作边界

- 原协调会话：`e2a5cc41-db4a-41a6-b882-55a1ada60e42`。
- 验收 Agent：`3ac85615-1d18-4527-a6b3-31e940bd71f7`。
- 工作区：`wks_14c980b5a0d2d1ef`；`/home/jimxl/.paseo/worktrees/3e51f239/task-result-preview`。
- 已阅读 AGENTS、项目必读上下文、任务 Spec/Plan；以当前文件、本批变更列表及 `/tmp/kaiwu-preview-baseline` 为依据，未将相对 Git HEAD 的全部未提交功能当作本批。
- 未修改产品代码，未创建 Agent/workspace，未提交/合并/推送，未调用真实 pi/模型，未运行浏览器或占用 17331。
- 自编补充测试仅通过 `/tmp/kaiwu-preview-review-tests/overlay.json` 注入，不写入仓库。

## Critical

未发现。

## Important：已关闭事项

### I1：实际执行命令未与用户看到的配置绑定——已修正、独立复验通过

- 修正位置：`server/internal/preview/manager.go:178,239-306`；`apps/web/src/App.vue:721-723`；`apps/web/src/ResultPreview.vue:31`。
- 原触发：Status 展示命令 A 后，外部将 `kaiwu-preview.json` 改为 B；用户点击原启动按钮，服务直接执行未展示的 B。
- 原影响：越过“只执行用户刚看到并确认的命令”边界；本验收初始 overlay 实际执行未展示的 `/bin/sh` 命令，在隔离目录生成 `preview-unapproved`，明确复现副作用。
- 当前实现：TaskPreview 提供 SHA256 `manifestId`，前端提交当前展示值；后端在状态清理、停止空闲 pi、获取 lease 和启动命令之前拒绝缺失/不匹配值；operationId 绑定整个请求；lease 后再次核验配置。
- 最终复验：`TestReviewDisplayedManifestMustMatchLaunch`、`TestReviewRejectedApprovalPreservesExistingPreview` 均通过。后者验证拒绝旧指纹不会停止既有预览或撤销能力链接。正式回归 `TestDisplayedManifestChangeRejectsWithoutSideEffectsThenAllowsRefresh` 也通过并重复 10 次。

### I2：FIFO manifest 可阻塞预览全局互斥锁——已修正、独立复验通过

- 修正位置：`server/internal/preview/process_linux.go:21-22`；`server/internal/preview/manifest.go:33-44`。
- 原触发：工作区 `kaiwu-preview.json` 为无写入端 FIFO，Status 在持有预览 Manager.mu 时先调用阻塞 Open，再准备检查普通文件类型。
- 原影响：阻塞同一 Manager 的状态、停止、继续开发协调和 Close；不是已声明兼容性限制。
- 开发者自查新增该回归的 RED 阶段曾被本验收命令实际跑到，明确失败；最终实现采用 `O_NOFOLLOW|O_NONBLOCK` 打开，然后检查普通文件和大小。
- 最终复验：`TestManifestRejectsFIFOWithoutBlocking` 通过并重复 10 次；完整 Go/race/vet 通过。

同批路径防护修正也已核查：`manifest.go:62-79` 拒绝解码后的目录穿越/反斜杠，`gateway.go:86-97` 对 resolved Location 再检查站内路径并加回能力前缀；4 项 `TestManifestPathCannotEscapeGatewayPrefix` 回归通过并重复 10 次。此前这些 RED 不等同于已成功突破父工作台隔离：前端原有 URL 校验仍是另一层防护。当前没有待关闭阻塞项。

## Minor（已接受的非阻塞限制）

### M1：能力链接过期或被另一客户端替换后，状态轮询不会自动续签

- 位置：`apps/web/src/App.vue:720-727`；`server/internal/preview/manager.go:491`。
- 触发：结果视图保持超过 30 分钟后 iframe 再请求资源；或另一客户端在两次轮询间停止并重启同一轮预览。
- 影响：状态仍可能是 running，前端因旧 URL 语法合法而复用；iframe 请求可能 401，单纯轮询/重连不换链接。
- 处理：用户点击“刷新状态”可取得新链接，不影响进程、确认结果、原会话或 Git。协调 Agent 明确接受本批手动刷新，不增加续签机制。
- 证据边界：静态前后端数据流核查；没有另行执行 30 分钟浏览器等待测试。

## 六项重点核查结果

1. **父工作台与认证隔离**：`ResultPreview.vue:29` 仅 `sandbox=allow-scripts`；`gateway.go:99-108` 的 CSP 再次 sandbox，脚本/连接/资源限能力路径。`gateway.go:70-72` 不转发 Cookie、Authorization、Origin、Referer 等，只白名单传 Accept；upstream Set-Cookie/CORS/Refresh/Link 等不透传。`webapi/preview.go:47-64` 仅能力路径容许 Origin:null，普通 API/登录/socket 的 Origin/CSRF/认证不放宽；能力 token 不是工作台登录方式。外部重定向被拒绝，站内跳转重新绑定 token 前缀。
2. **进程归属与回收**：Linux 检查回环监听、socket inode 和实际进程组，转发前后再核实；不按保存 PID 杀进程。`process_linux.go:31-59` 使用 Waitid(WNOWAIT)，leader reap 前完成组信号，stop/wait 互斥退休，retired 后不再按数值 PGID 发信号。停止失败保留 entry/lease，不把未回收工作区当空闲；退出 goroutine 自动清理，无需浏览器轮询。自然退出、继承 flock、第二 Manager 防重、容量释放和 Close 回收测试均通过。
3. **原 pi/工作区与写入者**：`agent/task_preview.go:38-57` 回收空闲 pi 后转移同一 execution lease，保留 session ID/file、消息和结果。`agent/manager.go:54-60`、`agent/integration.go:274-278` 在新开发/合并接管锁前停止预览；回调不在 Agent/Store 锁内调用，未发现反向同时持锁环。`TestTaskPreviewRetainsSessionAndStopsBeforeNewDevelopment`、`TestTaskMergeStopsPreviewBeforeTakingWriterLease` 在最终单测及 race 均通过。
4. **轮次与确认**：仅最新已结束/已确认轮次可启动，核对身份、owner.json、实际 worktree/分支。前端 epoch、返回 taskId/roundId 和切换 watcher 丢弃迟到响应；历史只显示记录，不将当前工作区冒充历史效果。确认使用原精确 roundId 协议，完成文字和角标来自持久状态，不改写验证报告、不提交/自动合并。选择/继续修改不自行发送消息。
5. **无配置正常退化**：Status 对无配置返回 unavailable，保留成果、实际输出与原结果确认。open/status 不启动进程；“补充预览说明”只填草稿并聚焦原输入，不调用模型。缺少有效配置/指纹的 start 拒绝执行。确认、切卡片和关闭浏览器不停止预览。
6. **断线不重放**：SocketClient 对断线/未知回执直接拒绝；重连、定时和可见性变化仅 status（需要时 open），不自动重发 start/stop。后端 operationId 绑定输入，重复 start 不重新执行；新增 `TestReplayedStopCannotTerminateLaterExplicitStart` 验证旧 stop 不杀后续新进程，最终重复 10 次通过。

## 实际执行的最终验证

以下均在后端正式交付“最终稳定源码”通知后执行，全部 exit 0；Go 默认缓存被允许复用，无源码不匹配的旧结果替代：

```text
GOCACHE=/tmp/kaiwu-go-review-cache GOTOOLCHAIN=local go -C server test ./...
GOCACHE=/tmp/kaiwu-go-review-cache GOTOOLCHAIN=local go -C server test -race ./...
GOCACHE=/tmp/kaiwu-go-review-cache GOTOOLCHAIN=local go -C server vet ./...
GOCACHE=/tmp/kaiwu-go-review-cache GOTOOLCHAIN=local go -C server test \
  -overlay=/tmp/kaiwu-preview-review-tests/overlay.json ./internal/preview \
  -run '^TestReview' -count=1
GOCACHE=/tmp/kaiwu-go-review-cache GOTOOLCHAIN=local go -C server test ./internal/preview \
  -run 'TestManifestRejectsFIFOWithoutBlocking|TestManifestPathCannotEscapeGatewayPrefix|TestDisplayedManifestChangeRejectsWithoutSideEffectsThenAllowsRefresh|TestReplayedStopCannotTerminateLaterExplicitStart|TestNaturalLeaderExitKeepsIdentityUntilDescendantsStopped' -count=10
npm --prefix apps/web test
```

- 完整 Go 单测、完整 race、vet：通过。
- 独立补充 overlay：2/2 通过。
- 上述关键故障/授权/防重回归重复 10 次：通过。
- 前端状态：13/13 通过。
- `gofmt -l` 检查 preview 目录及新增 agent/webapi 预览文件：无输出。
- 初审另曾执行自然 leader 退出/免轮询释放 lease 两项重复 50 次：通过；初始命令未绑定的 overlay 为预期 RED，已在当前实现 GREEN。

**未独立执行**：浏览器、移动端、截图、build、7331 部署、真实模型和跨平台。原协调 Agent 已告知真实 HTTP/opaque 父隔离/相对 fetch/确认后保留与显式停止，以及 manifest 变更拒绝后刷新启动的定向浏览器场景通过；这些不计为本 Agent 亲跑结果。最终 27 项浏览器由原协调 Agent 负责。

## 已声明初版范围（不列为缺陷）

Linux 进程组与 `/proc` 归属；最多 4 个预览；10 秒启动超时、64 KiB 诊断；只读 GET/HEAD，无应用登录、WebSocket/HMR、跨源应用或任意外部代理，根绝对资源可能不兼容。关闭浏览器/切卡片/确认不停止；继续开发、合并和服务退出停止。重启不自动恢复或重放启动，通过锁/归属防重；不存档每轮运行环境。工作区/进程组隔离不等于恶意代码的系统沙箱。token 30 分钟后的手动刷新如 M1。

**最终签署：本批代码及上述独立验证范围验收通过，无新增阻塞；可交原协调 Agent 完成浏览器集成终验。此结论不构成 Git 提交/合并/推送或真实模型调用授权。**
