# 全站 UI 风格统一

## 需求与边界

用户要求以项目菜单为参照，修正创建任务中原生下拉框的割裂，并统一整个网页风格。沿用现有紧凑灰绿视觉，不重新设计主布局，不改变业务流程／后端／会话隔离，不引入 UI 框架。

范围：任务专题、模型提供商／模型／思考级别、历史记录、交付设置与手机专题选择；统一输入、按钮、弹窗、焦点／悬停／选中／禁用状态、必要的卡片与聊天配色。自定义下拉必须兼顾表单值、键盘、标签、焦点、滚动边界与移动端。

## 基线与执行空间

- 基线：`8b54309`，`/home/jimxl/projects/kaiwu`，分支 `main`，git-dir `.git`，index `.git/index`。
- 当前为单功能串行实施，其他现有 Agent 查询均 idle；复用当前 Workspace，不新建空间。
- 已有子 Agent 后端批次、`src/types.ts`、11张旧截图和测试残留均为原地保留成果，不纳入本任务、不清理、不覆盖。
- 开发期间不提交／推送／部署；验收后用户明确要求“提交代码，push 到 GitHub”，因此仅本批 UI 代码／测试／新截图／记录获准提交当前 main 并正常推送实际 origin，不包含混合子Agent后端或旧截图。未授权更新7331，禁止将混合后端目录直接上线。

## 阶段清单

- [x] 核对需求、现有风格、控件分布、实际 Git 状态及 profiles。
- [x] 在原对话说明沿用项目菜单的统一方案；按用户既有约定直接推进现有视觉修正。
- [x] UI 角色实现与自测：优先补失败交互测试，提取可复用选择组件，替换各处可见原生下拉并收敛样式。
- [x] 功能验收角色独立检查：首次不通过，修复轮1关闭D1/D2并补展开视觉，修复轮2关闭D3；最终全部必需范围通过，见独立报告。
- [x] 原会话核对实际 diff、源码／产物hash、验收覆盖与代表截图、7331仍原进程健康200；README／路线图已同步；用户随后明确授权本批提交推送，未部署。

## 验收标准

1. 上述各选择器展开后与项目菜单同一视觉语言，不出现系统蓝色原生选项浮层。
2. 任务保存专题、手机切专题、模型联动、历史轮次、修复次数原行为不变，禁用／空列表／长选项可靠。
3. 键盘方向键、Home／End、Enter／Space、Escape、Tab及外点关闭；弹窗内 Escape 优先关闭下拉，不误关父弹窗；焦点可辨认。
4. 1440px与360px（必要时390px）无横向溢出、菜单裁切、遮挡关键操作；记录实际截图，不用单元测试冒充视觉验证。
5. 前端构建与受影响单元／浏览器测试提供实际命令、退出码和内容 hash；固定假 pi，不调用真实模型。

## 角色与成本

- UI 实现：最新 UI profile `pi / cliproxy/gemini-3.8-flash-high`，thinking `high`；具体有效设置以工具实际返回为准。
- 独立验收：`100021cd-ee17-412b-ad4b-b34cfaa46279`，按最新 profile `pi / opencode-go/glm-5.3-flash / high`，只读实现与聚焦真实浏览器反例；报告 `web-ui-consistency-acceptance.md`。开发者自测不是独立验收。
- token 与估价：未知；后续有实际可用记录再填，不当作零消耗。

## 实施／验收证据

以下 1–4 为开发者自测报告，非独立结论。首轮主控核对发现专属浏览器仅一条桌面用例、截图拍于菜单关闭时，故未采信为完整视觉验收；随后由独立角色补齐桌面／360px展开、边界、键盘与表单检查，发现D1/D2/D3并交原端修复、聚焦复验关闭。最终结论以[独立验收报告](web-ui-consistency-acceptance.md)的最新综合结论为准。

开发者为运行旧回归再次写入11张原路径截图（已有混合改动），未遵守仅写新截图目录的边界；已要求后续仅写本批独立路径，不恢复／清理现有旧图，不把它们纳入本批交付。

### 1. 交互与纯函数逻辑测试
- `node --test apps/web/tests/ui-consistency.test.ts`
  - 覆盖 `calculateNextIndex`（方向键循环、Home/End、禁用项跳过）、`getSelectAriaAttributes`、`filterOptions`、`formatOptionLabel`。
  - 结果：5 passed, 0 failed, 退出码 0。
- `npm --prefix apps/web test` (node tests/workbench.test.ts)
  - 结果：29 passed, 0 failed, 退出码 0。
- `node --test apps/web/tests/interaction.test.ts`
  - 结果：12 passed, 0 failed, 退出码 0。

### 2. 前端构建与类型检查
- `npm --prefix apps/web run build` (`vue-tsc --noEmit && vite build && node scripts/copy-build.mjs`)
  - 首轮结果：transform 87 modules，dist/index.html 0.41 kB，index-CJVqmbox.css／index-BMlRcOSd.js，退出码 0；修复影响产物，后续必要重建均exit0。最终产物为 index-CawbAscf.css（SHA256 `5777592a7f57903198c3812e1e866a1f43d745aaba3c574170f05542e320c3ae`）与 index-Cw6Y5egh.js（`ba1517fb7f41e9cf579e78edfa29cb581ab937e49f8e68b6cc0d5a0d6706a800`），独立验收核对两份dist一致并直接使用，未重复构建。

### 3. 自定义下拉与 UI 风格测试
- `npm --prefix apps/web run test:browser -- --project=ui-consistency`
  - 覆盖 CustomSelect 灰绿视觉渲染、弹窗内无原生 select、Escape 优先关闭下拉不关闭弹窗、键盘方向键导航与 Enter 选中、任务对话框功能专题下拉。
  - D1 缺陷验证：移动断点 599px（非 820px），桌面 1440px 下手机专题选择器占位隐藏不可见，移动端 360px 下可见并可交互。
  - D2 缺陷验证：选项键盘聚焦时具有清晰 DOM 焦点，且 computed outlineColor 显式断言为灰绿 `rgb(132, 158, 144)`，outlineStyle 为 `solid`，已移除旧有字符串匹配断言。
  - D3 缺陷验证：默认模型弹窗中输入无匹配搜索关键字使 options 变空（显示“无可用选项”），焦点留在 trigger 上；按 Escape 时仅关闭下拉菜单、阻止事件冒泡与默认行为，外层 dialog 保持可见且 trigger 保留焦点；清空搜索后模型选项动态恢复；菜单闭合状态下按 Escape 正常关闭 dialog。
  - 截图输出：
    - `docs/design/ui-consistency/custom-select-dialog.png`（默认模型 CustomSelect 与弹窗效果）
    - `docs/design/ui-consistency/custom-select-keynav-focus.png`（键盘移动选项灰绿焦点环渲染截图）
    - `docs/design/ui-consistency/kanban-gray-green-tokens.png`（看板与输入框灰绿主题焦点及悬停效果）
  - 结果：4 passed (4.7s)，退出码 0。

### 4. 交付与既有回归套件
- `npm --prefix apps/web run test:browser -- --project=delivery`
  - 结果：10 passed (41.2s)，退出码 0。
- `npm --prefix apps/web run test:browser -- --project=existing --grep-invert "result history UI"`
  - 结果：32 passed (40.7s)，退出码 0。
- `npm --prefix apps/web run test:browser -- --project=existing --grep "result history UI"`
  - 结果：1 passed (4.1s)，退出码 0。注：由于产品既定 8 个驻留会话上限、不自动空闲回收设计（详见 `docs/development/web-agent-interaction-regression.md` 记录），第 33 项会话测试在全新生命周期隔离下独立通过。

### 5. 并行环境事件记录
- 时间：2026-10-19 00:32:09
- 事件说明：用户转告并行 worktree `kaiwu-deployment-version` 的开发 Agent 9b5a860f在 00:32:09 误终止了 17331/17332/17441 上的测试进程，可能影响本任务；对应窗口 ECONNREFUSED 可关联此环境事件，不据此认定代码必然退化。对方已禁止终止他人进程并改独立端口，未改主目录 UI、尚未提交／合并／部署；不要求重跑已稳定通过套件，也不能仅凭此通知断言代码无退化。

### 6. 现用服务恢复（非 UI 部署）

- 用户要求开发期间保持服务可测试。主控只读检查发现7331无监听、无kaiwu-server进程，Paseo终端列表为空；退出根因尚未确认，本次UI修改未停止该服务。
- 已备份 `.kaiwu/backups/before-ui-session-recovery-20261018T155546Z/workbench.db`，确认零活动会话／交付与schema2；使用原启动器、原令牌、原数据库恢复已验二进制 `kaiwu-server-interaction-history-20261018`，SHA256 `61c11b05a905824a0ae424c373c557788c59c9af480a7a0c16315c975e87835b`。
- Paseo托管终端 `7e7fc362-1134-4097-b0ed-ec13958714c8`。实际 `http://192.168.2.122:7331` 健康／首页／JS／CSS均200；index hash `3ec564874eb567aaeb83f36881f98c8cbd2c63dd52ce99f3c5538081556e8559`，仍是原版本，不含新UI。
- 原登录表全行、消息与会话身份保留，SQLite integrity ok、schema2；state仅revision启动后改变。未自动重发用户操作、未启用交付worker，继续禁止UI执行／验收端触碰现用服务与数据库。
