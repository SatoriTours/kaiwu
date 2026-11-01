# 网页终端精修实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans after implementation is explicitly authorized. 当前会话串行实现，完成后独立验收；不逐文件派发。

**Goal:** 先按已选 A 完成终端视觉切片；整体验收仍须补齐自动创建、原会话恢复及真实多终端，不能以视觉切片冒充全部完成。

**Architecture:** Vue 顶栏承载终端工具组，原 Go PTY／WebSocket 和 xterm 不因样式变更而更换。运行层多终端设计是独立的必需前置门禁，未过门禁不解除每工作区一个终端的限制。

**Tech Stack:** 既有 Vue 3、TypeScript、xterm.js、Go、Playwright；不新增运行时或全局工具。

**Spec:** `docs/superpowers/specs/2026-11-01-terminal-refinement-design.md`

## Global Constraints

- 用户已回复“开始”，授权本批产品实现；不提交或部署，保留共享目录其他未提交成果。
- 全站顶栏48px；A标签28px高、12px普通界面字体、6px圆角；正文14px等宽字体、约17px行高、四周零边距。
- 关闭浏览器不结束Shell；不重放输入，不自动重试送达未知的启动请求。
- 不删除工作区互斥或单进程归属保护来绕过多终端门禁。

## Review Focus

- 同名终端必须按ID识别，工作区迟到回执不能串到新范围。
- 标签溢出和长工作区名称不能挤掉新建、更多及全局入口。
- 悬停和键盘焦点均可发现关闭操作，关闭不触发标签切换或误终止其他Shell。
- 深浅色实时切换不能重建终端或丢输入。
- 断线及服务重启不得自动重放旧指令，原结果截断需明确可见。

## Task 1：先补运行层门禁设计（不是删限制）

**相关文件：** `server/internal/agent/project_tools.go`、`server/internal/projecttools/terminal.go`、`server/internal/webapi/project_tools.go`及现有租约实现与测试。

- [x] 核实每个进程及其后代的归属、锁继承、异常终止与可靠回收；形成可评审的多终端组租约方案，说明每成员记录及最终释放条件。
- [x] 组租约细化补入规格；实施授权沿用用户“开始”。保持现有工作区单写入者、容量及认证；每成员持久身份，不将旧单进程租约直接共享。
- [x] 为多终端、自动创建与恢复单列可执行运行层计划；覆盖并发创建防重、逐个结束、最后退出、崩溃、AI／合并互斥、送达未知查询、原ID接回。此项完成前不承诺整个功能可进入实现。

## Task 2：A方案视觉切片（获实现授权后）

**文件边界：**
- 新建 `apps/web/src/TerminalToolbar.vue`：仅顶栏视图，无进程管理或网络副作用。
- 修改 `apps/web/src/ProjectTools.vue`：继续持有终端列表／连接，移除终端独立卡片外壳，不影响文件视图。
- 修改 `apps/web/src/App.vue`：提供明确的顶栏挂载位置，仅终端视图呈现工具组。
- 修改 `apps/web/src/TerminalSurface.vue`、`apps/web/src/theme.css`、`apps/web/src/style.css`：主题、字号和零边距；保持xterm首帧消费后才启用输入。
- 测试 `apps/web/tests/terminal.browser.spec.ts`；必要回归 `project-tools.browser.spec.ts`、`theme.browser.spec.ts`。

**接口方向：** 工具栏消费服务器终端ID／运行状态、当前ID、工作区选项与连接／忙碌状态；仅发出选择、新建、重连、结束请求和读屏切换意图。精确TypeScript接口与运行层方案一起冻结，不让组件独立启动Shell。

- [x] 先增加失败浏览器断言：顶栏48px、正文无上下左右外留白；A标签非等宽字体、轻底色且无下划线；360／390／1024／1440px不出现整页横向溢出。
- [x] 用真实Shell独立ID覆盖标签切换／关闭确认／不冒泡，独立8终端验证标签局部溢出及全局入口；方向键焦点问题经红绿回归修复。后端证据不依赖静态列表。
- [x] 实现工具栏与挂载位置，保留原项目视图／对话／主题／设置入口；标签局部滚动，新建紧随标签。
- [x] xterm按当前主题动态更新色板与背景；保持输入、ACK、resize、WebGL回退，去掉外部inset，不以额外空行模拟行距。
- [x] 更新原浏览器辅助定位到新菜单和可访问标签，保留中文／emoji、补全／历史、中断、清屏、尺寸、超长输入原子拒绝及切页接回断言。

## Task 3：自测与独立验收

- [x] `npm --prefix apps/web test` 与 `npm --prefix apps/web run build`。
- [x] `cd apps/web && npx playwright test --project=terminal --project=project-tools --project=theme`，显式有限timeout，失败定点定位。用隔离Go服务和真实Shell，不调用模型、不更新7331。
- [x] 若运行层也进入本批：`go -C server test ./internal/agent ./internal/projecttools ./internal/webapi`，并做受影响race及新增生命周期验证；证据绑定最终源快照。
- [x] 保存桌面／360／390px深浅色图及真实Shell测试证据，组织独立关键路径和读图验收，再由原会话核diff。
- [x] 更新README与路线图时分别报告视觉／自动创建／恢复／多终端的真实完成状态。未完成运行层不得宣称整体完成；不提交或部署，除非另获授权。

## 当前停点

用户“开始”后由原会话连续实现：运行层终端组／成员持久归属、ensure及操作幂等先红后绿，再接A工具条、主题和选择恢复。前端默认测试／构建、Go受影响四包race及最终projecttools race、首轮11项真实Shell／文件／主题浏览器自测通过。独立发现自然退出恢复、键盘焦点、ensure别名消解及实际行距问题后已集中修复，新增4项先红后绿、再补1项过期库存／核查回执对齐反例后，最终16项以15＋1分组通过（合并最后一项触及既有登录限流，新生命周期仅复跑该项通过），最终Go两包race通过；新hash独立窄复验通过，F1–F4关闭，功能及桌面／手机深浅色视觉验收通过；全项目全量／Actions未执行，不提交、不更新7331。统一证据与剩余项见 `~/.local/state/agent-artifacts/kaiwu/2026-10-31-terminal-design/status.md`。
