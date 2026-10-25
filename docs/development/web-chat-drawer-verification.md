# Web 独立聊天抽屉

## 获批范围（2026-10-25）

用户要求右侧聊天不再常驻挤占页面，并批准：默认收起，点击任务卡片或「项目对话」从右侧滑入；桌面宽720px，手机全屏；作为独立浮层覆盖全宽看板。关闭按钮、遮罩与Esc均可收起，隐藏不停止任务／会话、不清空草稿。沿用三档主题与灰绿风格。这取代此前固定看板＋常驻聊天的比例，不扩大为编辑器、IDE或后端功能。

## 实现与生命周期

- `ChatDrawer.vue` 使用原生modal dialog/top layer；通过`showModal()`／`close()`控制显示，聊天slot始终挂载。遮罩关闭要求按下和抬起都在外侧，避免内部拖动误关闭；键盘限制焦点、关闭返回原入口。正常模式有位移动画，尊重reduced-motion。
- `App.vue` 将项目／任务聊天放进抽屉，卡片与项目对话入口打开；直接任务路由及返回该任务的历史路由打开，项目初始路由收起。关闭只修改显示状态，不发送任务／会话命令；退出登录收起。
- 看板与结果页使用主体全宽；手机打开聊天不卸载底层看板或结果。桌面结果页也提供「返回任务对话」，避免关闭常驻分栏后没有入口。
- 原表单dialog样式排除聊天抽屉，保留嵌套模型／审核窗口的独立语义。模型窗口先响应Esc，再由聊天抽屉响应下一次Esc。
- HTTP复制备用textarea挂到当前modal内部，避免body在原生modal打开时inert导致复制失败；复制后仍恢复焦点与选区。浏览器实测真实Clipboard读取，未用假成功回执。
- `tests/view-navigation.ts` 让原业务用例通过真实入口打开／关闭抽屉，再正常点击；不force点击inert背景，不绕过认证或直接写产品状态。新建任务等待真实回执后的完整任务标题；退出动画仍绘制不等于dialog仍open。

## 自测与失败记录

证据统一在`~/.local/state/agent-artifacts/kaiwu/2026-10-25-chat-drawer/`。使用Git archive基线`a3a29d171340bdb68243225f92afde4b173a7e47`＋`files.txt`正向叠加的隔离副本；不复制共享checkout的subagents、types、后端、旧截图或私有配置。真实Go服务、临时Git和隔离fakepi；没有真实模型请求，不提高8驻留上限。端口17331／17332／17445串行，不调整workers、产品限制、测试timeout或retries。

- RED：原页面`#chat`默认仍visible，`red.log`实际失败。
- 新抽屉3项：build及`green-r2.log`通过，后续强化正常动画／reduced-motion断言，并由完整回归再覆盖。
- 主配置按既有生命周期串行分组：`group1-self-final.log` **4/4**、`group2-self-r3.log` **53/53**；共57项（原47＋主题7＋抽屉3）。后者包括实际HTTP复制、模型、Markdown、交付设置／交付生命周期及窄屏键盘交互。
- `unit.log` **70项通过**。Vue/TS/Vite build在两组浏览器运行前重新执行成功。
- 聊天回归仍使用正式`npm run test:chat-turn`发现22项、6个新鲜串行服务；自测各项已有相应分片通过证据，但完整runner曾在旧导航／键盘交互处失败，修正后又遇一次unknown merge receipt的执行者占用拒绝。尾部修正后`chat-turn-shard5-final.log` **4/4**、`chat-turn-shard6-final.log` **2/2**；不把多次分片拼作单次22项全绿，最终完整独立结果另列。
- 早期导航迁移的条件解析错误、漏等任务回执、将退出动画误作仍打开、直接聚焦inert聊天等失败保留在`legacy-*`／`group*-self-r*`／`chat-turn-self-*`日志。尝试用reduced-motion定位超时未解决，已撤回；业务回归仍使用正常动画。不删断言或放松安全门禁。
- 原HTTP预览API断言曾出现空白（`group1-self-r4.log`），随后最终4项通过。上一主题阶段在纯HEAD也复现同类波动；本轮不顺带修后端、不宣称稳定性统计。上述Git执行者占用拒绝也保留，不把保护性拒绝改成通过。

3项专门抽屉用例覆盖：

1. 默认收起、1440px全宽看板、720px独立覆盖；关闭按钮／Esc／遮罩、焦点返回与Tab首尾限制。
2. fakepi真实流式回复：关闭时仍running、隐藏后自然结束，重开完整回复／草稿／session ID与文件不变；任务间上下文隔离；直接记录真实WS及HTTP，关闭／重开无生命周期写入，后续revision不变。
3. 1440／768／390／360／320px宽度／高度、无横溢出、暗色、reduced-motion及嵌套模型dialog的Esc顺序。

## 独立验收

- **独立视觉最终通过**：原只读视觉Tab实际读取6图并报告通过；主控抽看发现`project-light-desktop.png`截于第三次打开的首帧，仅有遮罩，不能证明完整浅色抽屉。其余桌面暗色／390／360／320px截图保留；旧首帧另存`project-light-desktop-first-frame.png`。仅加强shot helper等待真实动画结束，生产未改；原视觉Tab已实际补读更新浅色图，在`visual-review.md`§4保留更正并追加最终通过，主控已核报告与实际图。不将静态截图当成动画流畅性的证明。报告核对`ChatDrawer.vue` hash `b937e82586cd8ec36fa3eb77d06b2ee614298a8437a156d53bf4e4b3f973d69d`及`App.vue` hash `3ed07d40f41628c8ba8fb501ac18411b3ed0783fbae61b4d153b62a602b66a9f`；此后这两份源码未变。
- **独立功能／回归通过**：原功能Tab基于`hashes.txt`重建，`independent-group1.log` **4/4**、`independent-group2.log` **53/53**、`independent-unit.log` **70/70**，正式`independent-chat-turn.log` **22/22（4/4/4/4/4/2六个串行服务）**；主控已读原日志与`independent-review.md`，31个哈希前后一致、当前HEAD/index未变。原HTTP预览与unknown merge receipt自测失败保留，本次独立完整运行没有复现，不宣称稳定性保证。
- 截图helper增量经原Tab独立第一项窄跑及完整抽屉3项通过；新浅色图已生成并由主控实际读图，右侧项目对话完整显示。原视觉Tab仅补读这一张并通过，视觉证据缺口已关闭。几何／像素探针只作功能辅助，不替代独立语义读图。桌面结果页返回聊天入口额外2/2通过；不把探针的初次错误当成产品缺陷，不再扩展探针。

## 交付边界

用户在本地验收后明确要求「提交代码」，授权本批主题与抽屉按既有流程精确提交、推送并核验部署；不建Tag、不夹带其他任务成果。提交前另核必需交互套件，发现切卡用例仍直接点击modal背景；仅复用closeChat并选择真实任务入口，业务断言未改，RED及8项自测GREEN保留在`delivery-interaction-*.log`，独立窄验另记。精确SHA、Actions及实际入口结果留在本批证据目录交付台账，未完成前不预称上线。未调用真实模型、操作实际产品任务或验证Safari／Firefox／原生客户端。共享checkout其他成果原样保留，不创建／迁移／归档空间，不扩大Git或认证权限。
