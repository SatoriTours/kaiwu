# 手机专题列头实施计划

> 执行方式：原会话串行实现／自测，再独立验收。用户“ok，按照这个来”已批准推荐方案，不重复审批，不创建worktree，不提交／部署。
**目标：** 手机将专题切换并入列头，新建专题进入菜单，新建任务在当前专题；PC、列底色、卡片和状态机保持。
**架构：** 保留一个稳定挂载的CustomSelect，将它移入.board内的mobile-board-header，手机与所选列背景连续，原列头只在桌面显示，避免切专题时卸载触发器丢焦。CustomSelect新增可选trigger／option／footer插槽；默认用户展示不变，footer在listbox外。
**技术：** 现有Vue3／TS／CSS、Playwright、Go＋fake pi，无依赖变化。
**规格：** docs/superpowers/specs/2026-11-07-mobile-board-toolbar-design.md。

## 约束与裁定
- 当前main共享checkout，只有主会话写本批；保留其他图片与未知文件。基线HEAD b2f29701ccd0ed4ed5f97baffbc6386d0bdd72d9，.git/index。
- 手机沿用599px断点（原生resize同步、卸载移除监听）、44px命中、原theme-inset列背景与圆角／满高，PC既有工具栏／列头不改。
- 原手机非保护专题的删除入口不能丢：菜单footer保留删除当前专题图标，仍走原确认窗口，不直接删除；默认保护“其他”不显示。此为保留已有能力，不新增删除权限。
- 同一个选择器常驻，焦点／键盘沿用CustomSelect；footer新建按钮在listbox之外，可Tab到达，Escape只关闭弹层；默认无footer时保留旧Tab行为。
- 不修改Go/认证/Agent/任务/会话/Git状态逻辑；真实模型不调用；仅127.0.0.1隔离测试，不动7331。
- 统一证据根沿用任务mobile-toolbar/implementation/；只有一份status.md。不写会覆盖既有图片的回归截图路径。

## 文件与单一可验收切片
修改src/KanbanBoard.vue、CustomSelect.vue、board-cards.css；新tests/mobile-toolbar.browser.spec.ts及独立playwright配置，在主配置登记；默认组件与原卡片回归同步。维护README/路线图/验证记录与规格批准状态。

- [x] 测试先行：新浏览器用例覆盖手机单头／无重复、320/360/390/430深浅长名/零任务/44px、专题切换不发会话生命周期请求、当前专题新建任务、菜单新建/删除显式确认、Tab和Escape焦点、桌面不变、等待计数和离线门禁。构建当前基线、保存预期红灯。
- [x] 可选CustomSelect插槽／listbox外footer，保留默认渲染与键盘；Kanban稳定移动列头和既有事件；手机局部CSS匹配稿、背景不动。
- [x] 构建／默认前端测试、新浏览器、原卡片／主题／抽屉和相关共享控件回归（重定向截图），保存实际深浅手机／菜单／长名图。
- [x] 独立只读关键路径源码／日志／实际图片验收；原实现者修必要问题并定向复验。
- [x] 最终核diff／hash／资源，更新README路线图与验证记录；无提交部署。

## 复核重点
1. footer不是option，键盘能够到达新建且不会误选专题或误关外层窗口。
2. 切专题稳定触发器回焦，不操作会话；新建任务topicId准确。
3. 保护专题不删除、非保护专题仍显式确认，离线禁新建但能本地切显示。
4. 原无footer的模型选择/空选项/动态options保持原行为。
5. 短视口菜单不裁切，长专题名不挤掉计数／动作；列背景不再透明化，等待计数不丢。

预检：只有一个任务切片；组件插槽供看板消费，统一实现和验证，无并发接口交接。
