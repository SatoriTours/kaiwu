# 任务卡片轻量改版 · 本地验证

## 授权与当前结论

用户先要求仔细设计、继续优化，再明确“改代码”，本轮依据第二轮[设计](../superpowers/specs/2026-11-06-task-card-refinement-design.md)与[实施计划](../superpowers/plans/2026-11-06-task-card-refinement.md)实现。当前：**本地实现、自测及独立功能／视觉验收通过**，两项Minor已修复并复验关闭。无提交、推送、部署或真实模型调用；7331未触碰。

## 改动

- KanbanBoard.vue：标题与状态为首行，原文不改写；编号／实际Git同底栏，合并／主动作／更多在右侧连续排列，移除每卡πpi身份。
- 标题按钮保留原“选择任务”名称，增加真实状态描述和完整title；新增回复点使用无障碍名称，不把文字塞进5px圆点。
- 原SubagentBadge与交付状态放可选辅助行，Git、任务与交付状态相互独立；不新增任何派发／自动交付能力。
- 新board-cards.css只作用于看板：14px内边距、7px圆角、14/22字重500标题、平衡三行换行、弱选中、不整面染黄；深浅／系统主题保留，聊天／终端共享主题未改。
- 手机主动作／合并／更多44px；保留现有专题选择和独立滚动。
- 菜单保持原DOM、键盘与显式完成流程，改fixed定位并限制视口边距；列滚动、看板横滚、窗口resize时关闭，卸载移除新增监听，防首卡菜单被列裁切。
- 生产任务状态机、动作门禁、Go接口、认证、会话恢复和Git动作均未修改。

## 自测命令与证据

证据统一：`~/.local/state/agent-artifacts/kaiwu/2026-11-06-task-card-design/implementation/`；六文件冻结于manifest.json，完整增量change.diff。

| 命令／范围 | 结果 | 证据 |
| --- | --- | --- |
| npm --prefix apps/web run build | 两项修复后通过；已有bundle>500k提示保留 | build-final.log |
| npm --prefix apps/web test | 两项修复后默认108项前端测试通过 | unit-final.log |
| cd apps/web && npx playwright test --config playwright.task-cards.config.ts | 两项修复后新增6项通过 | cards-final.log |
| cd apps/web && npx playwright test --project=theme --project=drawer | 首轮13项通过；修复后复跑受影响抽屉6项通过，未变主题7项复用 | regression.log／drawer-final.log |
| ui-consistency 项目的 D1／D3（命令见下） | 专题选择／菜单Escape相关2项通过 | controls.log |

控件定向命令：`cd apps/web && npx playwright test --project=ui-consistency --grep 'D1:|D3:'`。

新增检查：
1. 正式组件标题先于编号、身份精简、右侧操作、无障碍名称／状态；深浅色与系统实时跟随不改变聊天白底。
2. 320／360／390px手机、248px桌面列，无横溢；44px点击范围、完整标题和键盘焦点。
3. 真实Go＋隔离fake pi执行、原会话保留、编号／Git同区，首卡桌面／手机菜单实际命中可点；完成取消不改变状态或另一个任务。
4. **明确view-only**的七状态、双待回答、子Agent和交付暂停显示样本，核状态背景克制、状态／Git不混淆、运行／等待的合并门禁；不把注入状态作为真实执行、Git、CI或子Agent派发证据。

5. 明确view-only的新回复快照触发真实未读计算，248px列（包含320／390px手机视口，仍保留窄列设置）、两主题核圆点实际绘制命中、无障碍描述及完整title。
6. 440px短视口键盘打开菜单后resize与真实列滚动关闭，焦点回触发器、不抢外部焦点、不跳回原滚动位置。

13项回归覆盖主题持久化／跟随、暗色聊天与Markdown、抽屉布局和焦点、草稿／会话隔离、关抽屉不停fake pi、显式完成取消与确认且Git实际不变。没有运行会写既有设计图的ui-consistency整套，避免覆盖任务开始前的未知图片改动；只运行不写这些图片的D1／D3，主题和抽屉截图均重定向证据路径。

## 红灯与限制

- red.log：初始测试长标题超过实际80字上限，被真实API拒绝；修正为合法长标题后再跑，不改产品限制。
- red-layout.log：真实旧界面编号在标题上方，布局断言失败；实现后新增检查通过。
- 设计稿阶段的file页面检查与本轮真实Vue浏览器结果分开保留，不互相替代。
- 浏览器用真实Go服务、SQLite和隔离fake pi，不加载个人扩展、不耗真实模型额度；测试服务器由Playwright管理，17331／17332结束后已核无监听。
- 本次是前端受影响范围复验，未执行Go全量、提交全量门禁或实际7331部署验收，不声称这些通过。
- 目录为共享main而非worktree；仅当前实现者写本批文件，既有修改图片和未知extra文件不纳入本批。卡片测试使用过默认test-results输出目录（由Playwright管理），未验证其中旧临时结果完整保留，不作后续清理；其他回归输出已定向到任务证据目录。

## 独立验收

首轮independent-review.md：功能、视觉通过，Critical0／Important0／Minor2；已实际查看深浅桌面／手机／七状态6张图，没有视觉能力阻塞。两项Minor均已复现、修复，原验收端已定向复验关闭；最终independent-review-final.md结论：功能／视觉通过，无新增问题或阻塞。

- 长标题末尾新回复提示被三行截断：圆点移为标题文字旁的独立flex项，附无障碍描述；248px列及320／390px视口深浅色测试使用真实未读计算（手机视口仍保留窄列设置，不代表自然满宽未读截图），核圆点实际命中与完整标题保留。未读消息为明确view-only快照，不调用模型。
- 滚动／resize关闭菜单失焦：仅在焦点仍处于将关闭菜单内时，返回仍连接的触发器，preventScroll；新增真实列滚动、窗口resize和外部焦点不抢夺测试。

review-fixes-red.log留存两项红灯；cards-final.log共6项绿灯。一次夹具旧revision未能覆盖已有patch、导致title保持原值的失败保留cards-review-attempt-1.log；仅在view-only夹具增加revision消除竞态，不改生产合并逻辑。最终修复截图review-fixes/cards-unread-{light,dark}-248.png等。未实测200%浏览器缩放，不声称该边界通过。

## 用户反馈修正：恢复专题列背景

用户指出卡片后方整列背景消失。根因是board-cards.css覆盖了原有.column、第二列及末列的背景，将其置为transparent；不涉及图片资源丢失。已删除这条覆盖，沿用style.css的theme-inset：深色#333a43、浅色#f3f5f3，保留原列圆角、整列高度及卡片新样式。

- column-background/red.log：新增背景期望真实失败，实际为透明。
- column-background/build.log：修复后构建通过。
- column-background/browser.log：卡片6项＋主题7项共13项通过，包含显式深浅色及系统跟随的列背景断言。
- column-background/browser-attempt-1.log：首轮12过1失败，既有焦点测试按创建id取卡片，但服务器map次序不保证创建顺序，可能选到屏外卡片，自动滚动关闭菜单。测试改取实际首张可见卡片后通过，未为此改生产菜单逻辑。
- column-background/cards-{light,dark}-{desktop,mobile}.png：修复后实际页面；旧图作为历史留存。
- 本次生产增量仅移除列背景透明覆盖，任务／会话／Git和卡片样式不变。column-background/independent-review.md定向功能／视觉复验通过，实际查看深浅桌面／手机四图，无新增问题或阻塞；未提交或部署。

## 资源

没有新增常驻服务。Playwright按自身生命周期停止隔离Go服务与浏览器；主目录、7331与其他会话不动。必要失败及设计旧稿留在统一证据根。


## 用户“提交代码”后的交付范围
上述未提交／未部署是开发验收时的历史状态；用户现已授权本批提交、主分支推送与既有入口交付。两轮卡片／列背景和手机列头共用同一main checkout，原主会话唯一写入；Pi只读reviewer无独立成果空间，未创建分支或worktree。不夹带旧ui-consistency／web-service图片、delivery extra文件或test-results。
GitHub main与配置部署主分支均已核实，仓库Actions工作流数为0，不冒称CI通过。既有reference-transaction钩子从提交快照干净归档执行Go vet／Go全量／前端单元与构建／分生命周期浏览器／版本套件，然后备份并更新既有入口。新增卡片与手机列头套件另作干净暂存树补验，不修改认证限制，不消耗真实模型。
交付证据沿用任务证据目录mobile-toolbar/implementation/delivery/，最终SHA、推送／全量结果和实际入口验证写入任务唯一status.md，不在提交前预测通过。共享checkout保留其他任务成果，不归档或清理未知空间。
