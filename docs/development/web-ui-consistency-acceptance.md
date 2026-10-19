# 全站 UI 风格统一 · 独立功能验收

- 验收人：独立功能验收角色（未参与实现）
- 时间：2026-10-19 00:40–01:30 (+08)
- 基线：main `8b54309f03dd75759291be48ff3f3b766ab89b5e`，cwd `/home/jimxl/projects/kaiwu`，git-dir `.git`，index `.git/index`（已核验）
- 对象 hash：`apps/web/src/CustomSelect.vue` = `ea0d806a152065d84e13ac9fe84f29e5feb590e67508f699e07d463ba70556e2`；`apps/web/src/select-logic.ts` = `e92fdee304ed164246168d39863c8ffaedd11e976b3c14c421f89355c2488d46`（与任务给定一致）
- 结论：**不通过**（1 项必需缺陷 D1：手机专题选择器泄漏到桌面；另 1 项低严重性视觉问题 D2；其余已执行项通过，但仍有 3 项必需覆盖缺口待补验——DeliverySettings 展开态、ProgressCard 历史 360 展开态、空列表真实触发边界——补验完成前不扩大通过范围，不以成本理由豁免用户验收范围）
- 开发者在 `docs/development/web-ui-consistency.md` 中勾选的“功能验收”未实际执行，本次为首次真实独立验收；该文档中的测试与构建证据仅作参考输入，未被采信为本验收结论。

## 验收环境与方法

- 复用 `apps/web/tests/server.mjs` 起隔离真实 Go 服务 + 固定假 pi（隔离临时数据目录，不调用真实模型，不触碰 7331/真实数据库）；验收端为 `/tmp/kaiwu-ui-acc/` 下自有 Playwright 用例（不入库，不改仓库实现/现有测试），端口 17331 仅归属本验收子进程，结束后已按 PID 归属清理，无残留进程。
- 浏览器：Chromium 152.0.7977.82 headless，Playwright 1.64.0。
- 构建产物核验：`apps/web/dist/assets/index-BMlRcOSd.js`、`index-CJVqmbox.css` 均晚于全部前端源文件（00:43 > 00:22），且与 `server/internal/webui/dist/` 内副本 hash 一致——直接以现成产物验证，未重新构建、未写 dist。
- 实际跑通用例 8 条（全新生命周期最终轮：7 通过 1 失败，失败即下述缺陷 D1）；全部截图输出于 `docs/design/ui-consistency/acceptance/`（新目录，未动旧截图——`docs/design/ui-consistency/*.png` 与 `docs/design/web-service/*.png` mtime 均早于本次验收）。
- 实现文件未被修改：验收前后 `apps/web/src/` diff 与 `CustomSelect.vue`/`select-logic.ts` hash 不变。

## 缺陷

### D1（必需，中等严重性）：手机专题选择器泄漏到桌面布局

- 复现：桌面 1440×900 打开任一项目看板，工具栏「＋ 任务」右侧出现宽度 706.4×26px 的「当前看板专题」下拉（截图 `acc-desktop1440-mobile-topic-leak.png`，bounding box x=235.6, y=70.1）。要求：手机专题选择不得出现在桌面。
- 根因（实测定位）：`KanbanBoard.vue` 将根类 `custom-select` 与 `mobile-topic` 叠在同一元素。`CustomSelect.vue` scoped 规则 `.custom-select[data-v-75a1f09c]{display:inline-block}`（specificity 0,2,0）压过 `style.css:541` `.mobile-switch,.mobile-topic{display:none}`（0,1,0）；`.toolbar` 为 flex 容器，inline-block 被合法 blockify 为计算值 `block`（注意：不能以计算值 display 判断该缺陷，应以占位可见性为准）。`style.css:708` 媒体内的 `display:block` 在桌面不参与。桌面与移动端该元素始终可见。
- 修复方向（交回原开发者，未代改）：提高桌面隐藏规则优先级（如 `.custom-select.mobile-topic{display:none}` 或 `:where()` 降低 scoped 权重），或将“桌面隐藏”放进组件/容器条件渲染。
- 影响：桌面工具栏出现手机控件，破坏统一布局；不阻断其他功能。
- 证据：`acc-desktop1440-mobile-topic-leak.png`；aria 快照桌面含 `combobox "当前看板专题"`；`acc-desktop-model-provider-expanded.png` 等多张桌面截图工具栏均可见该下拉。

### D2（低严重性，视觉）：键盘导航时选项焦点环为浏览器默认蓝色

- 复现：键盘展开下拉并用方向键移动（如默认模型弹窗提供商下拉），第二项出现蓝色 UA 默认焦点环（`acc-desktop-focus-ring-blue-during-keynav.png`），而组件设计为 `:focus-visible` 灰绿 `#849e90`。脚本式 `.focus()` 后 Chromium 启发式未判定 focus-visible，灰绿样式未生效。
- 影响：与全站灰绿统一目标不符的可访问性焦点样式不一致；无功能影响。
- 证据：同一截图；`.custom-select-option:focus-visible` 规则存在于 `CustomSelect.vue` scoped 样式。

## 已验证通过项（附证据）

1. 业务联动与 FormData：
   - 任务弹窗专题选择经 hidden input `name="topicId"` 进入 FormData；保存后快照中新建任务 `topicId` 与 hidden input 值一致、专题名一致（用例 2，注意 commands 走 WebSocket，故以最终快照验证）。
   - 模型弹窗：改提供商后模型重置为占位、思考级别仅对 reasoning 模型出现；模型下拉未选提供商时禁用（用例 1）。
   - 手机 360/390 专题切换生效：选择后看板过滤/选中状态与 trigger 文案更新（用例 4）。
   - 历史轮次：第二轮后「往期记录」出现，展开含 ≥2 轮，Home+Enter 切回第 1 轮成功（用例 6，截图 `acc-desktop-history-expanded/switched.png`）。
2. 键盘与关闭行为（桌面 1440）：ArrowDown/ArrowUp 打开与循环、Home/End 跳转、Enter/Space 选中并关闭、Escape 仅关下拉不关父弹窗且焦点返还 trigger、Tab/Shift+Tab 关下拉不关弹窗、外点关闭；active 类与 DOM 焦点同步（用例 1）。
3. 视觉（实际看图，非几何断言代替）：展开浮层为灰绿同一视觉语言，选中项对勾 + 绿底、无系统蓝色原生选项浮层；360px 长专题名两行换行不溢出、无横向滚动（`scrollWidth−innerWidth ≤ 0`）；360×500 短视口下弹窗内下拉不被永久裁切、选项可滚入并点击；390 宽同样通过（截图 8 张逐张查看）。
4. hover 与键盘焦点反例：hover 改变 `.active`，DOM 焦点留在原项；Enter 选中 active（hover）项——符合 combobox 惯例，非功能缺陷（用例 7，截图 `acc-desktop-hover-vs-focus.png`）。
5. 空/禁用：模型下拉在未选提供商时 disabled；空列表「无可用选项」本轮仅单元级覆盖，真实 UI 可经「搜索模型输入无匹配关键字」自然触发，列入待补验缺口（见下节）。
6. 开发者既有证据核对：`ui-consistency` 1 条、`delivery` 10 条、`existing` 32+1 分、纯函数/交互/workbench 单测与 build 的退出码记录在任务文档中，与本轮产物 hash 一致，未机械重跑；本轮 8 条专属用例为新覆盖。

## 环境事件（不计为代码退化）

- 本验收早前轮次出现 2 类环境问题并已排除：① 连续登录触发服务端“登录尝试过多”限流（改为一次性登录 + storageState）；② 服务实例内驻留会话达产品 8 个上限后，新任务「开始开发」执行失败（与 `docs/development/web-ui-consistency.md` 第 5 节记录的 32+1 分机制一致）。重启本验收自有测试服务后全新生命周期一次通过，非代码退化。
- 并行事件按用户通知完整记录：并行 worktree `kaiwu-deployment-version` 的开发 Agent 9b5a860f 于 00:32:09 误杀 17331/17332/17441 三个端口上的测试进程，对应测试窗口出现偶发 ECONNREFUSED，对方此后改用独立端口。该事件采信任务文档记录；本验收时段（00:40 起）未观察到三端口再受外部干扰，本验收自有子进程在 17331 的启停均为自身归属清理，不将本时段任何失败归因于该事件，也不据此外推本时段因果。

## 待补验缺口（随修复轮 1 一并补验，属必需覆盖，不以成本理由豁免）

1. DeliverySettings「最大自动修复次数」CustomSelect 展开态实际截图：1440 与 360 各一张（进入交付向导第 3 步后展开）。
2. `ProgressCard` 历史下拉 360px 展开态实际截图（复用历史轮次用例改为 360 视口）。
3. 空列表真实触发与关闭边界：默认模型弹窗中先选提供商，再在「搜索模型」输入无匹配关键字使 `modelOptions` 为空，展开模型下拉应显示「无可用选项」；验证空列表时 focus 保持在 trigger、Escape 只关菜单且不误关 dialog（后续可正常 Tab/取消退出弹窗）。
4. 以上补验 + D1（桌面 1440 截图 + 占位不可见断言）+ D2（键盘焦点环截图）为修复轮 1 复验范围；不重跑已稳全套。
- 真实模型联调、7331 实服、构建重跑：按授权边界不执行（非本缺口范围）。

## 结论

**不通过**。必需项中“手机专题选择不出现在桌面”实测为缺陷（D1）；D2 为低严重性视觉问题；另有 3 项必需覆盖缺口（DeliverySettings 展开态 1440/360、ProgressCard 历史 360 展开态、空列表真实触发关闭边界）待补验，补验完成前不对这些项作出通过结论。已执行项的通过结论均有实际命令、截图与退出码证据。

## 修复轮 1 复验结果（2026-10-19 01:25–01:40）

新快照 hash 核验一致：`CustomSelect.vue` `0c01d606…ebc46a`、`style.css` `1d904b04…631`、`select-logic.ts` `e92fdee3…`（未变）、专属浏览器测试 `33b76bd6…710a35`、`dist/index.html` `4ebd9565…84cf6`；dist（01:25:36）晚于源码（01:23）且 `server/internal/webui/dist` 同步，直接以现成产物验证，未重建。隔离假 pi 服务（17331，自身子进程）全新生命周期跑复验用例，结束已按 PID 清理，7331（PID 107951）未触碰。

### 逐项结果

| 项 | 结论 | 证据 |
|---|---|---|
| D1 手机专题泄漏 | **通过** | 真实占位判断：1440/1023/600 隐藏、599/360 可见；`r1-desktop1440-mobile-topic-hidden.png`（工具栏无泄漏下拉）对照修复前 `acc-desktop1440-mobile-topic-leak.png`；`r1-599-mobile-topic-visible.png`。修复实现为全局 `.custom-select.mobile-topic{display:none}`（style.css:544）+ 599px 媒体同选择器 `display:block`（style.css:710，实际为 599px，开发者报告写 820px 不符）；同 specificity 依赖 cascade 顺序生效，已按真实可见性而非 computed display 验证 599/600/1023/1440 四边界 |
| D2 键盘焦点环 | **通过** | computed `outlineColor = rgb(132, 158, 144)`（#849e90）、`outlineStyle solid`（真实读取，非采信开发者声明）；截图 `r1-focus-ring-gray-green.png` 绿色焦点环清晰可见 |
| 缺口1 DeliverySettings maxRepairs | **通过** | 向导第 3 步展开实图 `r1-delivery-maxrepairs-1440-expanded.png` / `r1-delivery-maxrepairs-360-expanded.png`：灰绿选项 3 项、选中项对勾+焦点环、360 无横向溢出、选「3 次」后 trigger 文案与授权摘要同步 |
| 缺口2 ProgressCard 历史 360 | **通过** | `r1-history-360-expanded.png`：往期记录浮层 2 轮、灰绿、视口内无裁切无溢出，Home+Enter 切轮成功（`r1-history-360-switched.png`） |
| 缺口3 空列表边界 | **不通过 → D3** | 「搜索模型」无匹配可自然触发空列表：展开显示「无可用选项」（`r1-empty-list-open.png`），focus 留在 trigger（`aria-expanded=true`，activeElement=trigger）✓；但 **Escape 一次同时关闭菜单与整个 dialog，焦点掉到 BODY**（CDP 级状态：`{listbox:false, dialog:false, activeEl:BODY}`，`r1-empty-list-after-esc.png` 为空背景）——预期只关菜单；清空搜索后选项动态恢复可选 ✓（该部分在非空路径验证通过） |

### D3（新确认，中等严重性）：空列表展开时 Escape 误关整个弹窗

- 复现：默认模型弹窗 → 提供商已选 → 搜索模型输入无匹配关键字 → 展开模型下拉（空列表）→ 按 Escape 一次 → 菜单与整个「默认模型」弹窗同时关闭，表单状态丢失。
- 根因（读源码定位）：空列表时无 option 可聚焦，焦点留在 trigger；`onTriggerKeydown`（CustomSelect.vue:113-124）只处理 ArrowDown/ArrowUp/Enter/Space，Escape 未拦截，直达 `ModelDialog.vue` 的 `<dialog @cancel.prevent="emit('close')">`。非空列表时焦点在 option 上被 `onListboxKeydown` 拦截，故仅空列表受影响。
- 修复方向（交回原开发者）：trigger 展开态下也拦截 Escape（如 `onTriggerKeydown` 增加 `if (event.key === "Escape" && expanded.value)` → 关菜单、stopPropagation）。
- 影响：键盘用户在空列表场景丢失整个弹窗输入状态；触发条件为「空列表 + 焦点在 trigger」，当前仅模型搜索无匹配自然触发。
- 证据：`r1-empty-list-open.png`（Esc 前）、`r1-empty-list-after-esc.png`（Esc 后无弹窗）、控制台状态序列（FOCUS BEFORE ESC: custom-select-trigger / expanded true → AFTER ESC: listbox false, dialog false）。

## 修复轮 2 复验结果（D3，2026-10-19 01:38–01:45）

快照核验一致：`CustomSelect.vue` `6f749131…38beb`（新增：`onTriggerKeydown` 展开态 Escape → `preventDefault`/`stopPropagation`/`close(true)`，闭合态不拦截，dialog cancel 保留）、`style.css` 仍 `1d904b04…` 未变、新产物 `index-Cw6Y5egh.js` `ba1517fb…a800`、`index-CawbAscf.css` `5777592a…c3ae`；dist（01:34:52）晚于源码且 `server/internal/webui/dist` 同步，直接用现成产物未重建。隔离假 pi 服务全新生命周期，结束已按 PID 清理；7331（PID 107951）与实际 DB 未动。

### D3 复验：**通过**

真实操作序列（非预填）：键盘真实选择提供商 → 「搜索模型」输入无匹配关键字 → 模型下拉展开显示「无可用选项」→ 断言焦点留在 trigger（`aria-expanded=true`）→ 一次 Escape：
- 菜单关闭（listbox 消失）✓
- **dialog 保留可见**（`dialog[open]=true`，对照修复轮 1 的 `dialog:false`）✓
- 焦点返还 trigger（`activeElement=custom-select-trigger`，`aria-expanded=false`）✓
- 截图 `r2-empty-list-after-esc-dialog-kept.png`：弹窗在、菜单关、灰绿焦点环在 trigger
- 清空搜索后选项动态恢复（2 项）并可选 ✓；闭合态按 Escape 正常关闭 dialog（cancel 行为保留）✓

### D2 专属测试证据核对（只读）

`apps/web/tests/ui-consistency.browser.spec.ts`：`hasFocusRule` 已删除（0 处命中）；现断言 `activeOption.evaluate` 读取 computed 后 `expect(isFocused).toBe(true)`、`expect(outlineColor).toBe("rgb(132, 158, 144)")`、`expect(outlineStyle).toBe("solid")`（第 88–101 行）——真实断言成立，主控指出的“仅收集未断言/误命中”问题已消除。本验收上轮已独立以 computed+截图验证过运行时行为，本轮仅核对文件，未重跑该套件。

## 最终综合结论（全部必需范围）

**通过**。全部必需验收项均有实际证据：

1. 选择器业务联动与任务 FormData topicId（轮 0）✓
2. 键盘/焦点/关闭行为、disabled、动态选项（轮 0）✓
3. 桌面 1440 与移动 360/390/短视口展开态无裁切溢出（轮 0 + 轮 1 缺口 1/2 补图）✓
4. 手机专题选择不在桌面、599/600/1023/1440 边界正确（轮 1 D1）✓
5. 键盘焦点环灰绿（轮 1 D2，computed + 截图；专属测试断言已修正核对）✓
6. DeliverySettings 修复次数 1440/360 展开态（轮 1 缺口 1）✓；ProgressCard 历史 360 展开态（轮 1 缺口 2）✓
7. 空列表真实触发边界：focus 留 trigger、Esc 只关菜单不误关 dialog、动态恢复、闭合态 Esc 关 dialog（轮 2 D3）✓
8. 构建产物 hash 关联、固定假 pi 无真实模型调用（轮 0–2）✓

遗留说明：① DeliverySettings 向导仅进入第 3 步截图未保存草稿（保存动作属业务另测，开发者 delivery 套件覆盖）；② 空列表 Esc 修复后未重跑 D1/D2/视觉套件——CSS 规则未变（仅 scope id 变更），符合本轮范围约定；③ 交付设置概览等非下拉 UI 不在本任务范围。全部自定义下拉验收目标达成，可交付主会话核对 diff 与发布边界。

```
cd /home/jimxl/projects/kaiwu/apps/web
node tests/server.mjs &            # 隔离假 pi 服务（17331，全新生命周期）
node_modules/.bin/playwright test -c /tmp/kaiwu-ui-acc/playwright.config.ts ui-acceptance
```

---

### 修订记录

- 2026-10-19 初版：8 用例首验，结论不通过（D1/D2）。
- 2026-10-19 修订：按协调者意见撤销“其余全部通过”的扩大表述，将 DeliverySettings 展开态、ProgressCard 历史 360 展开态、空列表真实触发（模型搜索无匹配路径）列为必需待补验项；并行事件补齐 17331/17332/17441 完整端口记录并限定本时段因果；新增修复轮 1 复验计划。本修订不改源码、不改原测试、不改 `web-ui-consistency.md` 勾选状态。
- 2026-10-19 修复轮 1 复验：新快照 hash 核验后跑 11 条聚焦用例。D1（599/600/1023/1440 真实可见性边界）、D2（computed outline rgb(132,158,144) + 截图）、缺口 1（DeliverySettings 1440/360 展开实图）、缺口 2（历史 360 展开实图）通过；缺口 3 不通过——新确认 D3（空列表展开时 Esc 误关整个弹窗，含 CDP 状态与截图证据），交回原开发者。整体仍不通过；开发者“outlineColor 明确 assert 灰绿”声明不成立一事已按主控核实：原专属测试仅收集未断言，本轮以真实 computed 读取替代采信。
- 2026-10-19 修复轮 2（D3）：快照核验后复验空列表 Esc 边界——菜单关、dialog 保留、焦点返还、动态恢复、闭合态 Esc 关 dialog 全部通过；D2 专属测试文件核对（hasFocusRule 已删、真实断言成立）。最终综合结论：全部必需范围**通过**。已停止全部工具与自有服务（17331 无监听），无实现写入。
