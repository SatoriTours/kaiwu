# 自动交付独立审查：主控裁决与未关闭问题

日期：2026-10-18。原始只读报告保留于 [review](web-automated-delivery-review.md)，不回写、抹除原审查结论。本文件是后续修复与证据索引，不是整体通过报告。

## 当前门禁：不通过

独立报告列出 4 Critical、18 Important、1 Minor。主控逐项核对后，本轮已修复 2 Critical、3 Important；1 Important 的所述路径被现有实现和新增测试证伪；1 Important 仅消除其中的幽灵派发问题，容量排队等余项仍未关闭。

**仍有 2 Critical、14 Important（含部分修复项）及 1 Minor 待处理，不具备真实联调或上线条件。** 不因本地 Go／浏览器通过而放行真实上传，不更新 7331。现有生产代码不是安全验收完成版本，不能仅设置凭据并启用自动交付。

## 已修复并实际验证

| ID | 裁决与证据 |
| --- | --- |
| C1 | 已修复：真实 `get_state.model` 按嵌套对象 `{id,provider,...}` 解码，而非字符串。只读核对本机 pi `RpcSessionState` 类型；将 fake 改为真实对象形状后 `TestIndependentLazySessionsAndDurableReplies` 先失败，再修复通过。普通会话及 `TestDeliveryNextModelAtRoundBoundary` 通过；下一轮模型未报告／不匹配时拒发，不静默回旧模型。未调用真实模型。 |
| C4 | 已修复：干净候选也无条件检查 Base→HEAD 私密历史；`CaptureCandidate` 和 `SyncCandidate` 允许传入实际服务数据根，delivery 生产调用均传 DataDir。`TestDeliveryCleanCandidateStillRejectsPrivateHistory` 与 `TestDeliveryCandidateProtectsCustomServiceDataRoot` 先失败再通过；最终已删除的 `.env` 仍被拒绝上传。 |
| I3 | 已修复：提交前比较真实 index 的 write-tree 与已检查 tree，提交后复核实际 HEAD parent/tree。`TestDeliveryCommitRejectsIndexOnlyChangeAfterAdd` 通过 Runner 在 add 后仅替换干净文件的 index blob，原实现先失败，修复后拒绝且 HEAD 未前进。异常不 reset，保留现场不上传。 |
| I13 | 已修复：交付获取租约前在 Agent／Store 锁外调用任务预览可靠停止，再 reserve／取得项目及任务租约。`TestDeliveryStopsRealPreviewBeforeTakingIntegrationLease` 以真实 preview.Manager＋真实 HTTP 子进程先复现锁阻塞，修复后通过，并核对预览不再运行。 |
| I16 | 已修复：自动候选提交沿用本地身份读取及缺省 `Kaiwu <kaiwu@localhost>`，仍校验危险字符。`TestDeliveryOptionalCommitIdentityKeepsExistingFallback` 先失败后通过。 |

上述修正后 `GOTOOLCHAIN=local go -C server test ./...` 再次通过；真实服务六场景整套亦通过。最终 race／浏览器命令与后续改动以[验证记录](web-automated-delivery-verification.md)为准。

## 部分修复，不关闭

- **I15**：`stageQueued` 派发拒绝时改用持久意图后的新版本落 blocked，且仅成功 Send 才记 dispatched。`TestDeliveryRefusedDispatchCannotBecomeGhostDevelopment` 原实现先失败（旧版本 CAS 丢失），修复后明确停止，不会下一次扫描凭空进入 developing。
- 剩余：容量不足的有界排队、可证明未派发的完整状态清理及继续操作仍需单独收口；不能把“停止而不误报”当作已实现容量排队。

## 不采纳的单项（有证据，不等于整体通过）

- **I14 所述聊天别名 nil Wake 路径未成立**：实际 `webapi/handler.go` 在无 worker／平台门禁之前已将 `send_message`＋`IsStartRequest` 规范化为 `start_task`，并从 conversation 补 taskID。`TestDeliveryStartDispatchOwnedByWorker` 已扩展 direct/chat × worker/unavailable 四种情况、各重复两次；全部通过，未装配时不保存周期，不直接 Send 或调用 nil Wake。
- 代价：这仅证明精确受支持的“开始开发”别名路径，不泛化为所有命令接纳路径已无问题。若将来修改归一化顺序，回归测试必须保留。

## 尚待处理的安全／正确性工作

| 优先级 | ID | 未关闭内容 |
| --- | --- | --- |
| 必须阻止真实上传 | C2、I1 | 生产远程入口冻结 URL／rewrite／helper 校验、白名单环境、真实 HTTPS helper 协议／FD 及 SSH 接线 |
| 必须阻止门禁降级 | C3 | 冻结并复核实际 workflow 与明确检查定义，不仅比较 policy JSON 或依赖模型提示 |
| 主线与仓库隔离 | I4、I5、I6、I7、M1 | 远程基线 fetch／跨 clone 安全同步、主 CI 收尾重查远程、旧手动入口共享 lane、成功与释放槽原子化、真实 HoldsTurn 投影 |
| 本地失败和安全交权 | I2、I11、I12、I15 | 首次本地失败身份与租约交还、恢复调度屏障／非 push 步骤 intent、暂停撤销的实际启动点核验、容量排队 |
| CI 与预算 | I8、I9、I10、I18 | 多 workflow 304 聚合、累计活动时间／触发窗口、瞬断退避与可核查继续、源码正向证据分类 |
| 效果验收绑定 | I17 | 受控交权下重核实际 HEAD/tree/base／最新成果轮次，统一修复和恢复令牌失效 |

以上多数是只读审查中的明确反例推演，尚未全部建立动态失败测试；不得在下一会话仅根据表格写“已修复”。应按有界批次补隔离 RED→GREEN，继续独立核对必要接口。优先关闭远程凭据与保护定义，再进行真实联调申请。

## 其余未审范围的裁决

原审查的 Declined to judge 均保留：UI 由主控浏览器验证，不将此后端报告作为 UI 独立验收；真实 API 版本／Git 权限／SSH／模型费用仍未联调；不提供恶意仓库 OS 沙箱；不声明 Windows/macOS／PC／Android 原生验证；不能自动证明任意测试语义未削弱；不是全仓性能或完整安全审计。这些限制不能豁免上表的可机械保护点。

两个未来方向仍为团队协作、通用可配置编排；当前先完成固定交付流程的安全验收。
