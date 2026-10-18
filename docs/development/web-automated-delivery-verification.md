# 自动交付：接续验证记录（隔离流程通过，安全验收未通过）

日期：2026-10-18。规格与计划见 [设计](../superpowers/specs/2026-10-17-automated-delivery-design.md)／[计划](../superpowers/plans/2026-10-17-automated-delivery.md)。

## 当前结论

- 用户要求核对进度、提交已有成果并继续；已有 Tasks 1–12 后端代码／测试／文档本地提交为 `1c76c0a`，81 文件，未推送。两项历史 `extra-*.txt` 测试残留排除出提交并保留。
- Task 13 主要界面与 10 条真实服务浏览器场景已通过，包含历史分页及回执丢失核查；配置保存未知回执等续接项尚未关闭。Task 14 独立后端审查已完成但不通过。主控已修复部分问题，仍有 2 Critical、14 Important（含部分修复项）及 1 Minor，详见[原始审查](web-automated-delivery-review.md)与[裁决／续接清单](web-automated-delivery-review-resolution.md)。**不得上线或启用真实自动上传**。
- 未调用真实模型／GitHub／Actions，未配置个人凭据，未启动或更新 7331。所有 Git 提交、合并和 Push 测试只在临时项目和 bare 中运行。
- UI 实现实际通道为当前 Paseo Workspace 中 Pi `cliproxy/gemini-3.8-flash-high`；主控负责集成与验收，独立后端审查为 Pi `openai-codex/gpt-6.1-sol`。没有启动外部 OpenCode CLI，没有新建 Workspace/worktree。

## 新增服务级证据

`server/internal/delivery/testdata/webfixture/` 使用真实 `Store`、`agent.Manager`、`delivery.Manager` 和 `webapi`，由真实假 pi JSONL 子进程完成原会话轮次。Git 远程依赖注入仅把冻结 GitHub URL 映射到临时 bare；Actions RoundTripper 只有在 bare 确实存在请求的 ref/SHA 时才产生对应合成运行。必需 job 与 run attempt、SHA 绑定。

测试控制只改变假外部 CI 是否继续运行，不修改产品交付状态；只读证据返回实际 refs／推送记录／所查 Actions 请求。

`TestRealServiceDeliveryFromUnpreparedTask`：

| 场景 | 实际核对 |
| --- | --- |
| normal | 新任务无预制工作区；任务＋main 恰好两次 Push；main 为候选 SHA |
| acceptance | 先等待候选效果验收，再集成与 main CI，不伪造旧 ConfirmResult |
| repair-once | 原会话系统修复一次，重跑门禁，实际三次 Push |
| repair-limit | 三次额度停止，第四次派发不发生，无 main Push |
| main-repair | 主 CI 失败后原任务修复，四次 Push，最终 main 与新候选一致 |
| main-isolation | 第一任务停在 main CI，第二任务到 FIFO 队列等待；放行外部 CI 后两者按序完成 |

最终六场景整套普通测试及 race 均通过。

## 串联发现与 RED → GREEN 修正

| 回归测试 | 初次失败与根因 | 修正 |
| --- | --- | --- |
| TestDeliveryStartDispatchOwnedByWorker | handler 直接 Send，未装配 worker 也保存并启动周期 | 有 DeliveryID 仅 Wake；无 worker 的已启用项目返回 503，不产生周期 |
| TestDeliveryDispatchPrecedesWorkspacePreparation | worker 在 Send 创建工作区之前要求工作区存在 | queued 允许初次准备，先冻结本地主分支基线；后续候选与租约门禁不放宽 |
| 真实服务 normal 场景 | integrating 停滞；真实 Agent 拒绝 `integration` scope | 使用已有受保护的 `project_task` 租约 |
| TestDeliveryPolicyReadOverSocketAndPreflightWireContract | Socket 不接受配置读取；预检 JSON 大小写不符 | 固定只读查询映射、拒绝多余参数，lowerCamelCase 预检字段 |
| TestDeliveryCandidateWritesRequirePause | CI／验收／集成等阶段仍允许普通消息抢写候选 | 活动周期非 paused 时拒绝普通 send/resume；先暂停后沿用原会话修改 |

## 本次实际命令

包含本轮审查修正（真实模型对象握手、干净候选私密历史、真实 index／commit tree、预览交权与提交身份）的后端版本：

- `GOTOOLCHAIN=local go -C server test ./...`：通过。
- `GOTOOLCHAIN=local go -C server test -race ./...`：通过。
- `GOTOOLCHAIN=local go -C server vet ./...`：通过。
- `GOTOOLCHAIN=local go -C server mod verify`：`all modules verified`。
- `GOTOOLCHAIN=local go -C server build -o /tmp/kaiwu-delivery-verify ./cmd/kaiwu-server`：通过。
- `gofmt -l server/cmd server/internal`：无输出。
- `GOTOOLCHAIN=local go -C server test -race ./internal/delivery/testdata/webfixture -count=1`：最终六场景通过（含必需 job 校验与两卡隔离）。
- `GOTOOLCHAIN=local go -C server test ./internal/delivery -run '^TestDelivery(CrashAfterMainPushBeforeReceipt|RecoveryCommitAndRepairWindows)$' -count=10`：通过。

- `env` 去除工作台及 GitHub 凭据变量后执行 `python3 server/scripts/smoke_test.py /tmp/kaiwu-delivery-verify`：SIGINT／SIGTERM 两次实际二进制烟测通过，退出为 0、监听释放。未使用 7331。

最终详细输出在 `/tmp/kaiwu-delivery-final-{go,race,fixture-race}.log`，崩溃重复在 `/tmp/kaiwu-delivery-crash-repeat.log`；不提交真实会话日志。

## 浏览器与前端

- 既有前端基线 16/16 通过；修正后主控独立复跑前端 27/27、类型检查与生产 build 通过。最终完整 Playwright 42/42（原有 32＋交付 10）通过，不用这些绿灯覆盖未测的生产安全边界。
- 主控编写真实 fixture 的 Playwright `delivery` project：项目配置、人工效果验收、一次修复、三次停止、主 CI 修复、两卡隔离、暂停后旧候选失效、回执丢失、1440/768/360px 与下一轮模型；不注入绿色快照。
- 真实浏览器首轮 RED：填写授权后保存仍失败，后端提示 JSON 内容无效，证明前端保存载荷未适配真实 API。另发现旧模型命令／旧确认入口、未知回执恢复、历史分页与额度操作等缺口，已交回原 UI 实现者修正。
- 测试自身缺 Origin 的请求已修正，未因此放宽生产 CSRF。假主 CI 故障计数改为按任务身份计算，不让上一个测试的 main Push 数替后一个场景清除故障。
- 最终 42 项完整命令：`npm --prefix apps/web run build` 后 `npm --prefix apps/web run test:browser`；输出 `/tmp/kaiwu-delivery-final-browser.log`；前端单测及构建分别在 `/tmp/kaiwu-delivery-final-web-{tests,build}.log`。其中已有模型测试按获批的“下一轮可选”规则从启动后禁用改为显示下一轮入口，未改滚动／登录／预览／合并原回归。
- “丢验收回执后确实查询原 operations/id”的断言通过；“较早交付步骤完整分页”先 RED 确认摘要只有20条、真实历史超过20条却无加载入口，原实现者修正为从 after=0 读取真实 items/next、去重排序并按周期隔离迟到结果，主控最终 GREEN。
- 合成项目实际服务截图：[桌面](../design/web-service/automated-delivery-desktop.png)／[手机](../design/web-service/automated-delivery-mobile.png)。仅证明界面及隔离流程，不是安全或真实模型验收。

## 前端续接限制（不冒充全部要求已满足）

- 配置保存当前采用正确的嵌套策略载荷，但未知回执时完整策略 payload／operationId 的保留和只读恢复仍需统一；不能用验收／控制的回执测试替代该路径。
- 配置弹窗同项目重新打开的迟到回调隔离、更多页／迟到历史与网络异常组合尚未逐一覆盖；本次42项不是所有边界验收。
- 后端追加预算后仍 blocked 等真实继续能力，以安全审查续接清单为准，界面有按钮不等于后端恢复已实现。

## 授权与运行限制

生产自动交付默认关闭，旧任务不追溯上传。任务分支先上传再效果验收；启用项目时必须明确授权实际仓库与已有 workflow 的外部副作用。缺凭据、无适用 workflow、权限／Secrets／冲突等不能靠源码修复绕过。

隔离测试不证明真实 HTTPS credential helper／SSH、真实 Actions API 版本／触发、跨 provider/OAuth、稳定 Go 工具链或跨平台执行。真实联调需另行明确测试仓库、分支、额度与外部副作用。PC 三平台及 Android 首版目标保留。

团队协作和通用可配置编排引擎仍是未来方向；当前优先把这条固定流程验收完成，不扩展两套新系统。
