# 聊天待处理弹层与卡片联动 V2

## 范围与授权

用户查看 `docs/design/agent-interaction-v2.html` 的桌面／手机截图后回复“好的，按照你设计的来”，批准本次实现。

- 沿用主目录／当前分支，不新建 Workspace、worktree 或分支。
- 只改交互呈现、任务状态优先级、草稿／收起记忆与对应回归；继续使用原 WebSocket 命令及原 pi 会话，未改后端请求协议。
- 实现阶段未提交／推送／部署；用户随后于 2026-10-26 明确下达“提交代码”，授权按既有流程提交本批、推送实际主分支并更新／核验现有 Web 部署。当前交付进行中，不能以本地静态生成物冒充线上版本。
- 当前共享 checkout 包含其他历史成果，`types.ts` 等既有差异不属于本轮实现，不回退、不清理。

## 实现

- `interaction.ts` 按字段语义区分“待你回答／待你选择／待你确认”，不根据正文问号猜测授权；收起按 conversation/request/version 隔离，并与草稿一起在退出时清除。
- `InteractionCard.vue`／`interaction.css`：聊天内非模态琥珀色处理卡，确认与输入采用不同提交文案；收起、Esc、稍后处理保留请求和草稿，取消仍是独立显式操作。
- `InteractionIcon.vue`／`TaskActionIcon.vue`／`task-state.ts`／`KanbanBoard.vue`：有效阻塞请求优先于进程 running 和成果验收，使用待处理图标与数量；点击进入所属任务并展开问题，不启动第二个执行会话。
- `ChatPanel.vue`／`InteractionHistory.vue`／`App.vue`：顶部与对应助手轮次保留打开入口，回答后只读折叠历史；无法匹配消息的旧记录单独保留。查看记录不重发答案。
- 原生 waiter 与普通结构化提问仍走不同服务端派发路径；消息被接受、送达未知及实际恢复执行继续分开。

## 自测与证据

基线 `e3912cb3562fede5c1b1eacc00f3b6dfe2c771dc`，验收对象为未提交文件快照，不以 HEAD 代替工作树。

证据目录：`~/.local/state/agent-artifacts/kaiwu/2026-10-25-interaction-design/implementation/`。

| 命令／检查 | 实际结果 |
| --- | --- |
| `node --test apps/web/tests/interaction.test.ts` 初次新增断言 | 红：原生请求等待被 running 遮盖；保留 `kaiwu-interaction-v2-red.log` |
| 原生不确定送达新增断言 | 红：原显示“执行中”；保留 `kaiwu-interaction-v2-delivery-red.log` |
| 2 条 V2 浏览器初始断言 | 红：尚无新状态／打开入口；另一次失败为抽屉入场动画尚未结束即量测，改为等待动画，不放宽产品宽度 |
| `npm --prefix apps/web test` | exit 0，70/70 |
| `node --test apps/web/tests/interaction.test.ts apps/web/tests/interaction-command.test.ts` | 首轮 exit 0，24/24；活动轮窄修后 exit 0，26/26 |
| `npm --prefix apps/web run build` | exit 0，含 `vue-tsc --noEmit` 与 Vite 正式构建 |
| 真实 Go＋隔离 fake pi 的 2 条 V2 浏览器 | exit 0，2/2；独立服务生命周期，未调用真实模型 |
| 本批精确文件 `git diff --check` | exit 0；全 checkout 的既有 `types.ts` EOF 空行不属本批，未顺带改动 |

浏览器配置为 `apps/web/playwright.interaction.config.ts`，实际使用 Chromium。V2 用例覆盖卡片待确认图标／点击展开、聊天内 Esc 不关闭抽屉、显式否定、折叠历史、收起跨刷新、任务切换与草稿隔离。输入页面覆盖 390px／360px。

实际实现截图：`native-dark-desktop.png`、`history-dark-desktop.png`、`input-light-390.png`、`input-light-360.png`。主控已逐张读图检查，**不替代独立视觉验收**。均为真实 Go 服务＋合成 fake pi 场景，不是用户现场或真实模型截图。

## 独立验收与待闭合项

独立验收 `c50da012-c580-4249-ab3e-7e56f5a94a3a` 已派发，按实际 profile 使用 `pi / magpie/group/deepseek-flash-4.1`；同 Workspace 只读，不得写实现或断言。报告约定为证据目录的 `acceptance.md`。

- [x] 原 8 项真实 Go＋fake pi 交互回归，exit 0（与新增 2 项分服务生命周期，保留 8 驻留上限）。
- [x] 抽屉 3 项、正式串行分片聊天 22 项回归及代码审查，均 exit 0。
- [x] 独立视觉补验通过，原图像能力阻塞已解除。用户于 2026-10-26 要求解释并修复该阻塞后，独立只读验收 `c1627582-ff7a-49f4-8a78-74b6106fab67` 使用实际 `pi / cliproxy/gemini-3.8-flash-high / high`，成功逐张读懂最终实现四图并对照获批设计；桌面暗色确认／折叠历史、浅色 390／360px 输入均通过，Critical／Important 为 0。只写证据目录 `visual-acceptance.md`，未改代码／未重复功能全套；原会话核对报告、13 项 manifest 与最终 dist 身份未变。
- [x] 历史不确定送达遮盖新轮／已结束任务、原生取消未送达仍显示执行中已先红后绿修复。独立 26/26 交互／命令与 70/70 基线单元、只读边界探针确认高／中风险关闭。
- [x] 最末筛选回归窄复验通过：独立发现 P4——“只看我的问题”隐藏对应助手记录，已先红（记录数 0 而非 1）后绿修复。`ChatPanel.detachedHistory` 按可见消息兜底；返回全部不重复，追加到原 V2 浏览器用例。独立验收在最终构建实跑 2/2，exit 0（7.5s），日志 `acceptance-logs/final-v2-postfix.log`。
- [x] 最终构建与主控受影响自测：`kaiwu-interaction-v2-final-build.log` exit 0，70＋26 单元通过，`filter-green.log` 2/2 浏览器通过，包含活动轮与筛选窄修。`dist/index.html` SHA256 为 `9e9a189473385fa557e2be0182e8b85b8962af7e433baeecb1617d462cbf327d`。最终 manifest 13 项核验通过，原清单保留为 `manifest-initial.sha256`。

**本批限定范围的本地功能与截图视觉验收均通过，原独立视觉阻塞已解除。** 这不等于已部署、已上传代码或已完成真实模型联调；视觉报告的“全部通过／交付就绪”仅适用于已提供工件与本批范围。 原验收者已在最终构建实际跑通 2 条 V2 浏览器；此前 8／3／22 回归对应窄修前快照，仅受影响范围补验，不冒称在最终构建重跑全套。真实 pi 扩展／真实模型、真机输入法及屏幕阅读器、7331 入口均未在本批测试；不据假进程测试宣称真实模型联调完成。

### 低危遗留与裁决

- P3：历史 `not_sent` 重试按钮未完全对齐后端 `expired/native==nil`。当前服务器合法记录不能产生“过期＋not_sent”，原生绑定缺失亦不属正常写入；后端仍拒绝过期／无原生身份的派发，未扩大修改协议。
- P5：缺失必填 `fields` 的非法快照可能使看板抛错。服务器契约保证该字段，本轮将旧测试 stub 补齐正式类型，不为非法数据新增另一层兼容协议。
- 请求缺 `roundId` 但会话有 `rounds` 的旧非法数据不推断原 waiter；原生取消未送达仍沿用“回答待发送”文案，同轮多条未决显示最早一条的标签。均为已知低危，不掩盖未验证项。

## 生命周期与台账

主控自测的 Playwright 浏览器及回环服务已随命令结束，验后 17441／17331／17445 无监听；未启动 7331。独立验收首轮、活动轮及最终 2 条窄复验均已报告测试服务／浏览器无残留，主控核对最终日志、13 项 manifest、dist 身份与测试端口无监听；验收者已停止工具。保留共享服务、主会话和用户原进程。当前可用 Paseo 工具只有取消运行／归档（软删除），无独立关闭运行时 API；不为释放资源擅自归档，不把 idle 声称成运行时已释放。

2026-10-26 用户续接时核对：13 项源码 manifest 与最终 dist 身份未变；独立视觉补验未启动浏览器、服务或构建，无新增测试运行资源；验收者已停止工具，报告与历史保留。此前只检查开发机 7331 无监听，误将其描述为服务未运行；收到“提交代码”后按实际部署配置核实，现有受监督服务在配置的远端正常运行，健康 HTTP 200、current 为 `e3912cb` 发布；已向用户纠正，本批沿用实际配置目标，不新建服务或更改监听端口。

原会话为本批唯一源码实现负责人，完成首批测试／代码、自测和普通修复；独立端只验收。输入／缓存／输出用量与费用未知，不估算成实际账单。

## 提交代码：关联空间与交付核对

- 仅一个关联空间：`wks_ecc67f723697ff7f`，cwd `/home/jimxl/projects/kaiwu`，local 不隔离，`refs/heads/main`，git-dir `.git`、index `.git/index`。协调会话 `ab21ceb9-6b2b-4410-af6e-89c05d894134`；功能验收 `c50da012-c580-4249-ab3e-7e56f5a94a3a`；视觉验收 `c1627582-ff7a-49f4-8a78-74b6106fab67`。没有本批独立 worktree 或待整合分支。
- 仅提交本批 13 项源码／测试、5 项获批合成设计资产、README／路线图／本验证记录，以及下面交付门禁的 2 脚本＋部署说明。其他子 Agent 后端、原截图差异、工具／附件设计及临时测试材料保留，不夹带。
- 真实远端 `origin` 与默认主分支已核对；提交前 `origin/main` 与本地基线一致。GitHub 仓库 workflows=0，不把无 Actions 称为 CI 通过；推送后仍按最终 SHA 核实。
- 新增 V2 后交互套件共 10 项，既有部署门禁需沿用已验的 8＋2 独立服务，避免超 8 驻留。`scripts/deploy.sh` 改用互补筛选，两步任一失败均阻断部署，不跳断言；新增两条脚本测试先红（150 过／2 败），修后 `bash -n` 与 152 项隔离脚本测试通过。独立窄复验通过：脚本哈希一致，独立语法检查及 152／152 通过，并在临时副本独立复现 150／2 红例；证据 `deploy-gate-{red,green}.log`、`deploy-gate-acceptance.md`。
- 提交触发现有 reference-transaction 部署钩子，流水线从精确已提交 SHA 的干净 Git archive 运行完整门禁，未提交混合成果不会构建／部署。随后正常 push，核该 SHA 的 Actions／实际部署与入口。交付最终结果另存证据目录 `delivery.md`。
- 当前目录仍有其他任务的待保留成果与共享空间引用，不满足安全归档条件；不删除／搬移这些文件以凑归档。成功交付后保留本空间并报告原因。
