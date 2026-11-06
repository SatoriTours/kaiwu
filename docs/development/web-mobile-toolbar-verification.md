# 手机专题列头 · 实现验证

## 授权与当前结论
用户“ok，按照这个来”已批准[设计方案3](../superpowers/specs/2026-11-07-mobile-board-toolbar-design.md)：手机切换器融入列头，新建专题移入菜单。当前实现和自测通过，独立功能／视觉验收通过，唯一列头跨断点焦点Minor已修正并独立复验关闭；无提交／部署，7331保持原样。

## 范围与裁定
- KanbanBoard.vue：手机只保留专题名称／数量／箭头与＋任务。单个稳定CustomSelect位于board内，与所选列背景连成完整容器；不随专题切换重建触发器。599px断点沿用，resize时对应控件回焦；保存移除控件前的焦点，避免media query先隐藏控件导致activeElement变body而丢位置。监听卸载清理。
- 原桌面工具栏／各列头不变。手机有待处理任务时才显示原项目级计数行，不丢掉其他专题的待回答提醒。
- CustomSelect.vue新增可选trigger／option／footer插槽和ariaDescribedby；默认调用者沿用原文字和键盘。footer位于listbox外，Tab可到新建，Escape可回触发器；无footer时仍保留原Tab关闭行为。
- 保留原非保护专题的删除入口于菜单footer，仍走原确认窗口，保护专题不显示；没有直接删除或新权限。新增任务使用当前专题id，经真实Go命令核对。
- board-cards.css仅追加手机局部样式：56px列头、32px视觉表面／44px命中、长名省略与完整标题／无障碍描述、菜单长名换行／数量分列。theme-inset列背景和8px圆角／满高延续，不再透底；卡片新样式保留。
- 新增mobile-toolbar浏览器项目与独立配置；ui-consistency截图增加可选KAIWU_UI_ARTIFACTS目录，避免覆盖工作目录既有图片。
- 不修改Go、认证、会话、任务、Git、模型／交付状态机；不加载个人pi扩展、没有真实模型调用。

## 自测证据
统一路径：`~/.local/state/agent-artifacts/kaiwu/2026-11-06-task-card-design/mobile-toolbar/implementation/`。
manifest.json冻结7个源码／测试文件；before-*.vue/css及mobile-change.diff为本轮相对先前卡片版本的精确增量，不把其他未提交改动归本轮。

| 检查 | 实际结果 | 文件 |
| --- | --- | --- |
| npm --prefix apps/web run build | 通过，已有bundle>500k提示保留 | build-focus-final.log |
| npm --prefix apps/web test | 默认前端108项通过 | unit-focus-final.log |
| Playwright独立mobile-toolbar配置 | 6项通过 | browser-focus-final.log |
| 原ui-consistency4／theme7／drawer6／task-cards6 | 首次组合15过8受登录限流；8项隔离复验全过，最终覆盖23项 | regression.log／regression-recheck.log |

新增6项覆盖：
1. 320／360／390／430深浅手机、长名／零任务、44px、无横溢与列背景、桌面4列头及工具栏保持。
2. 切换专题不发HTTP写或WebSocket commands、执行与会话快照不变，键盘回焦；真实创建任务归入所选专题，不启动执行。
3. footer不是option，Tab可到新建、真实创建专题；保护专题无删除，非保护专题取消删除不改任务归属。
4. 320×440短视口菜单footer实际命中不被裁，离线禁新建但允许切换本地专题。
5. **明确view-only**待回答样本，显示整个项目计数；跨599px断点保留相应控件键盘位置，不作为真实模型或扩展请求依据。

6. 桌面列头（非顶部工具栏）任务／删除按钮缩至手机：对应入口保持焦点；若原焦点专题不同则仅切本地显示专题，保持语义一致；不打开菜单／确认窗口、不修改后端数据。列头使用column-new-task／column-delete-topic命名，避免与全页唯一新任务入口data-action冲突。

实际页面截图：mobile-toolbar-{dark,light}.png、mobile-toolbar-long-{dark,light}.png、mobile-toolbar-menu.png、mobile-toolbar-pending.png；原卡片／主题／抽屉／控件新截图均存本证据目录，不覆盖任务前已有PNG。静态设计稿另留，不等于正式组件结果。

修正后影响范围：重新构建、默认108项及新增6项均通过；focus-fix-regression.log复验共享控件D1／D2／D3及原卡片滚动／resize焦点共4项，focus-fix-ui-render.log补验共享控件完整模型／任务窗口路径1项。未变主题／抽屉及卡片其他路径复用初审前成功证据，最终覆盖6＋23＝29个不同浏览器用例。
最新实际页面图保存focus-fix/，原初审图片仍保留；样式与CustomSelect未变，不重复宣称新视觉方案。

## 红灯及失败保留
- red.log：旧界面缺少合并列头，真实布局失败。
- browser-attempt-1.log：3过1失败，测试将topics数组误按id索引；修正测试，不改产品数据。
- browser-attempt-2.log：4过1失败，断点切换导致先隐藏原焦点控件，已据此修复回焦并通过。
- regression.log：第一次组合23项中后8项在登录时被真实rate_limited拒绝。未修改／绕过认证门禁，未无界重试或睡眠等待；按测试fixture隔离启动新服务，复验失败8项，regression-recheck.log全部通过。其余15项成功证据复用，不宣称首次一次全绿。

- column-focus-red.log：新增桌面列头缩屏测试复现初审Minor，手机任务入口未获焦点。
- browser-focus-attempt-1.log：修正后5过1失败，测试连续setViewport未等Vue处理桌面断点就马上缩回，跳过了所需桌面状态；补桌面工具栏可见及原按钮已获焦点前置检查，不放松最终断言，browser-focus-final.log六项全过。

## 验收与边界
独立初审independent-review.md：Critical／Important各0，功能与实际六张图视觉通过，1项Minor为桌面列头按钮缩屏丢焦；已修并新增真实红灯与浏览器复验。independent-review-final.md确认Minor关闭、功能与视觉最终通过，无新增问题；复验同时实际查看最新深色图，样式未变，原六图验收仍有效。已核可选插槽默认路径／listbox与footer语义／真实操作及无副作用／响应焦点和实际深浅手机、菜单、长名图。
本次前端受影响范围验证，不等于Go全量、提交全量门禁或7331部署验收。未实测200%浏览器缩放。
测试服务由Playwright在127.0.0.1:17331／17332受监督启动和结束，无新常驻服务或公网入口；不清理其他任务资源及既有未知文件。


## 用户“提交代码”后的交付范围
上述未提交／未部署是开发验收时的历史状态；用户现已授权本批提交、主分支推送与既有入口交付。两轮卡片／列背景和手机列头共用同一main checkout，原主会话唯一写入；Pi只读reviewer无独立成果空间，未创建分支或worktree。不夹带旧ui-consistency／web-service图片、delivery extra文件或test-results。
GitHub main与配置部署主分支均已核实，仓库Actions工作流数为0，不冒称CI通过。既有reference-transaction钩子从提交快照干净归档执行Go vet／Go全量／前端单元与构建／分生命周期浏览器／版本套件，然后备份并更新既有入口。新增卡片与手机列头套件另作干净暂存树补验，不修改认证限制，不消耗真实模型。
交付证据沿用任务证据目录mobile-toolbar/implementation/delivery/，最终SHA、推送／全量结果和实际入口验证写入任务唯一status.md，不在提交前预测通过。共享checkout保留其他任务成果，不归档或清理未知空间。
